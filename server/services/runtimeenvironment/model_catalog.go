package runtimeenvironment

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
)

// AccountModelCatalog 是指定智能体和账号的原生目录快照。
type AccountModelCatalog struct {
	// ModelCatalogSnapshot 保存原生模型和发现状态。
	agent.ModelCatalogSnapshot
	// AccountID 是显式账号选择，系统登录使用 system。
	AccountID string
	// AuthGeneration 是产生该快照的认证代次。
	AuthGeneration uint64
}

// ReadModelCatalog 只发现所选 Codex 账号的模型，不创建 Chat 或发起推理。
func (s *service) ReadModelCatalog(ctx context.Context, runtimeID, accountID string, refresh bool) (AccountModelCatalog, error) {
	environment, err := s.environments.Get(ctx, runtimeID)
	if err != nil {
		return AccountModelCatalog{}, err
	}
	if environment.BaseRuntimeID != constants.RuntimeIDCodex {
		return AccountModelCatalog{}, status_error.RuntimePolicyUnsupported
	}
	return s.readModelCatalog(ctx, environment, accountID, refresh), nil
}

func (s *service) InvalidateModelCatalog(accountID string, throughGeneration uint64) {
	if s.modelCatalog != nil {
		s.modelCatalog.InvalidateAccount(accountID, throughGeneration)
	}
}

func (s *service) readModelCatalog(ctx context.Context, environment models.RuntimeEnvironment, accountID string, refresh bool) AccountModelCatalog {
	if accountID == "" {
		accountID = environment.DefaultAccountID
	}
	if accountID == "" {
		accountID = agent.SystemAccountID
	}
	result := AccountModelCatalog{AccountID: accountID, ModelCatalogSnapshot: agent.ModelCatalogSnapshot{
		State: agent.CatalogUnavailable, ErrorCode: "catalog_unavailable",
	}}
	if s.modelCatalog == nil || s.accountWorkspaceDirectory == "" {
		return result
	}
	request := runtimeDetectRequest(environment.Detection.Runtime, environment)
	request.RuntimeID = environment.RuntimeID
	binary, err := s.runtimeCatalog.ResolveExecutable(request)
	if err != nil {
		result.ErrorCode = "runtime_unavailable"
		return result
	}
	cwd := filepath.Join(s.accountWorkspaceDirectory, constants.RuntimeIDCodex)
	if err := os.MkdirAll(cwd, 0700); err != nil {
		return result
	}
	resolve := func(ctx context.Context) (agent.AccountRequest, error) {
		selected := agent.AccountRequest{Provider: constants.RuntimeIDCodex, AccountID: accountID, CWD: cwd}
		if accountID == agent.SystemAccountID {
			return aiaccount.ResolveCodexSystemAccount(ctx, selected, environment.ConfigHome)
		}
		if s.resolveCodexAccount == nil {
			return agent.AccountRequest{}, status_error.AIAccountConfigurationInvalid
		}
		return s.resolveCodexAccount(ctx, selected, environment.ConfigHome)
	}
	selected, err := resolve(ctx)
	if err != nil {
		if errors.Is(err, status_error.RuntimeAuthRequired) {
			result.State, result.ErrorCode = agent.CatalogSignedOut, "sign_in_required"
		}
		return result
	}
	result.AuthGeneration = selected.AuthGeneration
	var fingerprint string
	var apiKeyModels []string
	if accountID != agent.SystemAccountID && s.accounts != nil {
		account, err := s.accounts.Get(ctx, accountID)
		if err != nil {
			return result
		}
		if account.AuthMode == constants.AccountAuthModeAPIKey {
			apiKeyModels = account.SelectedModels
			encoded, _ := json.Marshal([]any{account.SelectedModels, account.DefaultModel, account.ModelReasoningLevels, account.ModelDisplayNames, account.Endpoints})
			fingerprint = fmt.Sprintf("%x", sha256.Sum256(encoded))
		}
	}
	env := make(map[string]string)
	for _, entry := range request.BaseEnvironment {
		if name, value, ok := strings.Cut(entry, "="); ok {
			env[name] = value
		}
	}
	snapshot := s.modelCatalog.ReadForRuntime(ctx, environment.RuntimeID, agent.ProviderConnectionRequest{
		Account: selected,
		Launch: agent.ProviderLaunch{
			Executable: binary, PrefixArgs: environment.PrefixArgs, ConfigHome: selected.ConfigHome, BaseEnvironment: env,
		},
	}, environment.Detection.Version, fingerprint, refresh)
	// External system login changes can race a read without emitting a host event.
	current, err := resolve(ctx)
	if err != nil || current != selected {
		if errors.Is(err, status_error.RuntimeAuthRequired) {
			result.State, result.ErrorCode = agent.CatalogSignedOut, "sign_in_required"
		}
		return result
	}
	if apiKeyModels != nil {
		snapshot.Models = slices.DeleteFunc(snapshot.Models, func(model agent.AgentChoice) bool {
			return !slices.Contains(apiKeyModels, model.ID)
		})
		if !slices.Contains(apiKeyModels, snapshot.DefaultID) {
			snapshot.DefaultID = ""
		}
	}
	result.ModelCatalogSnapshot = snapshot
	return result
}

func (s *service) detectCodex(ctx context.Context, summary agentapi.RuntimeSummary, environment models.RuntimeEnvironment, request agentapi.DetectRequest) agentapi.DetectionResult {
	result := agentapi.DetectionResult{
		Runtime: summary, State: agentapi.DetectionStateUnavailable, Auth: agentapi.AuthStatusUnknown,
	}
	result.ResolvedBinary, _ = s.runtimeCatalog.ResolveExecutable(request)
	if result.ResolvedBinary == "" {
		return result
	}
	snapshot := s.readModelCatalog(ctx, environment, environment.DefaultAccountID, true)
	result.Version = snapshot.Capabilities.Version
	if snapshot.Capabilities.Version != "" {
		result.State = agentapi.DetectionStateReady
	}
	if snapshot.State == agent.CatalogSignedOut {
		result.State, result.Auth = agentapi.DetectionStateReady, agentapi.AuthStatusMissing
	}
	if snapshot.State != agent.CatalogFresh && snapshot.State != agent.CatalogStale {
		result.Diagnostics = []agentapi.Diagnostic{{
			Code: agentapi.DiagnosticCodeACPOptionsUnavailable, Level: agentapi.DiagnosticLevelWarning, Message: snapshot.ErrorCode,
		}}
		return result
	}
	result.State, result.Auth = agentapi.DetectionStateReady, agentapi.AuthStatusOK
	if snapshot.State == agent.CatalogStale {
		result.Diagnostics = []agentapi.Diagnostic{{
			Code: agentapi.DiagnosticCodeACPOptionsUnavailable, Level: agentapi.DiagnosticLevelWarning, Message: snapshot.ErrorCode,
		}}
	}
	result.Capabilities = agentapi.CapabilitySet{
		agentapi.CapabilityDynamicModels:         true,
		agentapi.CapabilityPartialMessages:       true,
		agentapi.CapabilityResumeSession:         snapshot.Capabilities.Resume,
		agentapi.CapabilityAddDirectory:          snapshot.Capabilities.AdditionalDirectories,
		agentapi.CapabilityImagePath:             snapshot.Capabilities.ImageInput,
		agentapi.CapabilitySkillDirectory:        true,
		agentapi.CapabilityMCPStreamableHTTP:     true,
		agentapi.CapabilityStreamingUserMessages: snapshot.Capabilities.Steer,
	}
	permissions := CodexPermissionLevels()
	result.DetectedDefaultModel = snapshot.DefaultID
	result.Models = make([]agentapi.ModelOption, 0, len(snapshot.Models))
	for _, model := range snapshot.Models {
		option := agentapi.ModelOption{ID: model.ID, Name: model.Name, PermissionLevels: permissions}
		if model.Config != nil {
			option.ReasoningOptions = []agentapi.ReasoningOption{}
			for _, setting := range model.Config.Options {
				if setting.ID != "reasoning_effort" {
					continue
				}
				for _, choice := range setting.Choices {
					option.ReasoningOptions = append(option.ReasoningOptions, agentapi.ReasoningOption{ID: choice.ID, Name: choice.Name})
				}
				if model.ID == snapshot.DefaultID {
					result.DetectedDefaultReasoning = setting.Value.ID
				}
			}
		}
		result.Models = append(result.Models, option)
	}
	return result
}

// CodexPermissionLevels 映射原生审批和沙箱预设，权限与账号及模型无关。
func CodexPermissionLevels() []constants.PermissionLevel {
	return []constants.PermissionLevel{constants.PermissionLevelDefault, constants.PermissionLevelAuto, constants.PermissionLevelFullAccess}
}
