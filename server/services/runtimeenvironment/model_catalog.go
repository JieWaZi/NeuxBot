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
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
)

// AccountModelCatalog 是指定智能体和账号的原生目录快照。
type AccountModelCatalog struct {
	// Provider identifies the native protocol that reported this catalog.
	Provider string
	// ModelCatalogSnapshot 保存原生模型和发现状态。
	agent.ModelCatalogSnapshot
	// AccountID 是显式账号选择，系统登录使用 system。
	AccountID string
	// AuthGeneration 是产生该快照的认证代次。
	AuthGeneration uint64
}

// ReadModelCatalog discovers the selected native account without creating a public Chat or sending a prompt.
func (s *service) ReadModelCatalog(ctx context.Context, runtimeID, accountID string, refresh bool) (AccountModelCatalog, error) {
	environment, err := s.environments.Get(ctx, runtimeID)
	if err != nil {
		return AccountModelCatalog{}, err
	}
	if !usesNativeCatalog(environment.BaseRuntimeID) {
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
	result := AccountModelCatalog{Provider: environment.BaseRuntimeID, AccountID: accountID, ModelCatalogSnapshot: agent.ModelCatalogSnapshot{
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
	cwd := filepath.Join(s.accountWorkspaceDirectory, environment.BaseRuntimeID)
	if err := os.MkdirAll(cwd, 0700); err != nil {
		return result
	}
	resolve := func(ctx context.Context) (agent.AccountRequest, error) {
		selected := agent.AccountRequest{Provider: environment.BaseRuntimeID, AccountID: accountID, CWD: cwd, Executable: binary, PrefixArgs: environment.PrefixArgs}
		if s.resolveAccount == nil && accountID == agent.SystemAccountID && environment.BaseRuntimeID == constants.RuntimeIDCodex {
			return aiaccount.ResolveCodexSystemAccount(ctx, selected, environment.ConfigHome)
		}
		if s.resolveAccount == nil {
			return agent.AccountRequest{}, status_error.AIAccountConfigurationInvalid
		}
		return s.resolveAccount(ctx, selected, environment.ConfigHome)
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
	var managedClaude *models.AIAccount
	if accountID != agent.SystemAccountID && s.accounts != nil {
		account, err := s.accounts.Get(ctx, accountID)
		if err != nil {
			return result
		}
		if account.AuthMode == constants.AccountAuthModeAPIKey {
			if environment.BaseRuntimeID != constants.RuntimeIDClaude {
				apiKeyModels = account.SelectedModels
			}
			if environment.BaseRuntimeID == constants.RuntimeIDClaude {
				if aiaccount.ManagedClaudeModelFingerprint(account) != selected.ConfigFingerprint {
					return result
				}
				managedClaude = &account
			}
			fingerprint = accountModelFingerprint(account)
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
	if err != nil || !current.Equal(selected) {
		if errors.Is(err, status_error.RuntimeAuthRequired) {
			result.State, result.ErrorCode = agent.CatalogSignedOut, "sign_in_required"
		}
		return result
	}
	if fingerprint != "" {
		currentAccount, err := s.accounts.Get(ctx, accountID)
		if err != nil || accountModelFingerprint(currentAccount) != fingerprint {
			return result
		}
	}
	if apiKeyModels != nil {
		snapshot = selectManagedCatalog(environment.BaseRuntimeID, snapshot, apiKeyModels)
	}
	if managedClaude != nil {
		snapshot.Models = slices.DeleteFunc(snapshot.Models, func(model agent.AgentChoice) bool {
			_, ok := aiaccount.ManagedClaudeModelTarget(*managedClaude, model.ID)
			return !ok
		})
		snapshot.DefaultID, _ = aiaccount.ManagedClaudeModelTarget(*managedClaude, "")
		if !slices.ContainsFunc(snapshot.Models, func(model agent.AgentChoice) bool { return model.ID == snapshot.DefaultID }) {
			snapshot.DefaultID = ""
		}
	}
	result.ModelCatalogSnapshot = snapshot
	return result
}

// selectManagedCatalog keeps a CLI's other provider credentials out of this
// account's public choices and removes transport-only provider prefixes.
func selectManagedCatalog(provider string, snapshot agent.ModelCatalogSnapshot, selected []string) agent.ModelCatalogSnapshot {
	snapshot.Models = slices.Clone(snapshot.Models)
	namespaced := provider == constants.RuntimeIDOpenCode || provider == constants.RuntimeIDPi
	snapshot.Models = slices.DeleteFunc(snapshot.Models, func(model agent.AgentChoice) bool {
		publicID := accountconfig.AccountModelID(provider, model.ID)
		return namespaced && publicID == model.ID || !slices.Contains(selected, publicID)
	})
	for i := range snapshot.Models {
		snapshot.Models[i].ID = accountconfig.AccountModelID(provider, snapshot.Models[i].ID)
	}
	if namespaced && snapshot.DefaultID == accountconfig.AccountModelID(provider, snapshot.DefaultID) {
		snapshot.DefaultID = ""
	} else {
		snapshot.DefaultID = accountconfig.AccountModelID(provider, snapshot.DefaultID)
	}
	if !slices.ContainsFunc(snapshot.Models, func(model agent.AgentChoice) bool { return model.ID == snapshot.DefaultID }) {
		snapshot.DefaultID = ""
	}
	return snapshot
}

func (s *service) detectNative(ctx context.Context, summary agentapi.RuntimeSummary, environment models.RuntimeEnvironment, request agentapi.DetectRequest) agentapi.DetectionResult {
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
	if snapshot.QoderLoginVerified {
		result.Auth = agentapi.AuthStatusOK
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
	if environment.BaseRuntimeID != constants.RuntimeIDCodex && environment.BaseRuntimeID != constants.RuntimeIDClaude &&
		environment.BaseRuntimeID != constants.RuntimeIDCursor && !snapshot.QoderLoginVerified {
		// Other ACP providers may expose models before authentication.
		result.Auth = agentapi.AuthStatusUnknown
	}
	if snapshot.State == agent.CatalogStale {
		if !snapshot.QoderLoginVerified {
			result.Auth = agentapi.AuthStatusUnknown
		}
		result.Diagnostics = []agentapi.Diagnostic{{
			Code: agentapi.DiagnosticCodeACPOptionsUnavailable, Level: agentapi.DiagnosticLevelWarning, Message: snapshot.ErrorCode,
		}}
	}
	result.Capabilities = agentapi.CapabilitySet{
		agentapi.CapabilityDynamicModels:         true,
		agentapi.CapabilityPartialMessages:       true,
		agentapi.CapabilityResumeSession:         snapshot.Capabilities.Resume || snapshot.Capabilities.Load,
		agentapi.CapabilityAddDirectory:          snapshot.Capabilities.AdditionalDirectories,
		agentapi.CapabilityImagePath:             snapshot.Capabilities.ImageInput,
		agentapi.CapabilitySkillDirectory:        summary.SkillDirectoryName != "",
		agentapi.CapabilityMCPStreamableHTTP:     snapshot.Capabilities.MCPHTTP,
		agentapi.CapabilityStreamingUserMessages: snapshot.Capabilities.Steer,
	}
	result.DetectedDefaultModel = snapshot.DefaultID
	result.Models = make([]agentapi.ModelOption, 0, len(snapshot.Models))
	for _, model := range snapshot.Models {
		option := agentapi.ModelOption{ID: model.ID, Name: model.Name, PermissionLevels: NativePermissionLevels(environment.BaseRuntimeID, model)}
		if model.Config != nil {
			option.ReasoningOptions = []agentapi.ReasoningOption{}
			for _, setting := range model.Config.Options {
				if setting.Category != "thought_level" && setting.ID != "reasoning_effort" {
					continue
				}
				for _, choice := range setting.Choices {
					option.ReasoningOptions = append(option.ReasoningOptions, agentapi.ReasoningOption{ID: choice.ID, Name: choice.Name})
				}
				if model.ID == snapshot.DefaultID && slices.ContainsFunc(setting.Choices, func(choice agent.AgentChoice) bool { return choice.ID == setting.Value.ID }) {
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

func usesNativeCatalog(provider string) bool {
	return agent.SupportsProvider(provider)
}

// NativePermissionLevels exposes only the native policies supported by this provider and model.
func NativePermissionLevels(provider string, model agent.AgentChoice) []constants.PermissionLevel {
	if provider == constants.RuntimeIDKimi {
		levels := []constants.PermissionLevel{constants.PermissionLevelDefault}
		if slices.Contains(model.PermissionModes, "auto") {
			levels = append(levels, constants.PermissionLevelAuto)
		}
		if slices.Contains(model.PermissionModes, "yolo") {
			levels = append(levels, constants.PermissionLevelFullAccess)
		}
		return levels
	}
	if provider == constants.RuntimeIDPi {
		return []constants.PermissionLevel{constants.PermissionLevelDefault, constants.PermissionLevelAuto, constants.PermissionLevelFullAccess}
	}
	if accountconfig.ACPSystemConfigVariable(provider) != "" {
		levels := []constants.PermissionLevel{}
		if slices.Contains(model.PermissionModes, "default") {
			levels = append(levels, constants.PermissionLevelDefault)
		}
		if slices.Contains(model.PermissionModes, "auto") {
			levels = append(levels, constants.PermissionLevelAuto)
		}
		full := "yolo"
		if provider == constants.RuntimeIDWorkBuddy {
			full = "bypassPermissions"
		}
		if slices.Contains(model.PermissionModes, full) || provider != constants.RuntimeIDWorkBuddy && slices.Contains(model.PermissionModes, "bypassPermissions") {
			levels = append(levels, constants.PermissionLevelFullAccess)
		}
		return levels
	}
	if provider == constants.RuntimeIDCodex {
		return CodexPermissionLevels()
	}
	levels := []constants.PermissionLevel{constants.PermissionLevelDefault}
	if (provider == constants.RuntimeIDClaude || provider == constants.RuntimeIDGrok) && model.AutoMode {
		levels = append(levels, constants.PermissionLevelAuto)
	}
	return append(levels, constants.PermissionLevelFullAccess)
}

func accountModelFingerprint(account models.AIAccount) string {
	encoded, _ := json.Marshal([]any{account.SelectedModels, account.DefaultModel, account.ModelReasoningLevels, account.ModelDisplayNames, account.ClaudeModelRoles, account.Endpoints})
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}
