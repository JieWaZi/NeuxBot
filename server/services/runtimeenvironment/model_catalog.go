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
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
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

// ReadModelCatalog 优先读取目录快照，首次缺失或显式刷新时探测并保存。
func (s *service) ReadModelCatalog(ctx context.Context, runtimeID, accountID string, refresh bool) (AccountModelCatalog, error) {
	environment, err := s.environments.Get(ctx, runtimeID)
	if err != nil {
		return AccountModelCatalog{}, err
	}
	if !usesNativeCatalog(environment.BaseRuntimeID) {
		return AccountModelCatalog{}, status_error.RuntimePolicyUnsupported
	}
	return s.readModelCatalog(ctx, environment, accountID, refresh)
}

func (s *service) InvalidateModelCatalog(accountID string, throughGeneration uint64) {
	if s.modelCatalog != nil {
		s.modelCatalog.InvalidateAccount(accountID, throughGeneration)
	}
}

func (s *service) readModelCatalog(ctx context.Context, environment models.RuntimeEnvironment, accountID string, refresh bool) (AccountModelCatalog, error) {
	startedAt := time.Now().UnixNano()
	if accountID == "" {
		accountID = environment.DefaultAccountID
	}
	if accountID == "" {
		accountID = agent.SystemAccountID
	}
	result := AccountModelCatalog{Provider: environment.BaseRuntimeID, AccountID: accountID, ModelCatalogSnapshot: agent.ModelCatalogSnapshot{
		State: agent.CatalogUnavailable, ErrorCode: "catalog_unavailable",
	}}
	var accountFingerprint string
	if accountID != agent.SystemAccountID {
		if s.accounts == nil {
			return result, nil
		}
		account, err := s.accounts.Get(ctx, accountID)
		if err != nil {
			return result, err
		}
		if err := aiaccount.ValidateRuntimeAccount(account, environment); err != nil {
			return result, err
		}
		result.AuthGeneration = account.AuthGeneration
		if !account.HasCredential {
			result.State, result.ErrorCode = agent.CatalogSignedOut, "sign_in_required"
			return result, nil
		}
		accountFingerprint = accountModelFingerprint(account)
	}
	fingerprint := persistedModelCatalogFingerprint(environment, result.AuthGeneration, accountFingerprint)
	var cached models.RuntimeModelCatalog
	if s.modelCatalogs != nil {
		var err error
		cached, err = s.modelCatalogs.Get(ctx, environment.RuntimeID, accountID, fingerprint)
		if err != nil {
			return result, err
		}
	}
	if !refresh && cached.ID != "" {
		result.ModelCatalogSnapshot = cached.Snapshot
		result.AuthGeneration = cached.AuthGeneration
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	key := strings.Join([]string{environment.RuntimeID, accountID, fingerprint}, "\x00")
	done := s.modelCatalogRefreshes.DoChan(key, func() (any, error) {
		// 多个页面可等待同一次探测，切走的页面只取消自己的等待。
		parent := s.lifecycleContext
		if parent == nil {
			parent = context.Background()
		}
		probeCtx, cancel := context.WithTimeout(parent, 45*time.Second)
		defer cancel()
		if !refresh && s.modelCatalogs != nil {
			latest, err := s.modelCatalogs.Get(probeCtx, environment.RuntimeID, accountID, fingerprint)
			if err != nil {
				return result, err
			}
			if latest.ID != "" {
				ready := result
				ready.ModelCatalogSnapshot, ready.AuthGeneration = latest.Snapshot, latest.AuthGeneration
				return ready, nil
			}
		}
		return s.refreshModelCatalog(probeCtx, environment, result, cached, fingerprint, startedAt)
	})
	select {
	case <-ctx.Done():
		return result, ctx.Err()
	case reply := <-done:
		return reply.Val.(AccountModelCatalog), reply.Err
	}
}

func (s *service) refreshModelCatalog(ctx context.Context, environment models.RuntimeEnvironment, result AccountModelCatalog, cached models.RuntimeModelCatalog, fingerprint string, startedAt int64) (AccountModelCatalog, error) {
	probed := s.probeModelCatalog(ctx, environment, result)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if result.AccountID != agent.SystemAccountID && probed.AuthGeneration != result.AuthGeneration {
		return result, nil
	}
	if probed.State == agent.CatalogUnavailable && cached.Snapshot.DetectedAt > 0 {
		previous := cached.Snapshot
		previous.State, previous.ErrorCode = agent.CatalogStale, probed.ErrorCode
		previous.QoderLoginVerified = probed.QoderLoginVerified
		probed.ModelCatalogSnapshot = previous
	}
	if s.modelCatalogs != nil {
		if err := s.modelCatalogs.Store(ctx, models.RuntimeModelCatalog{
			RuntimeID: environment.RuntimeID, AccountID: result.AccountID, Fingerprint: fingerprint, AuthGeneration: probed.AuthGeneration,
			ProbeStartedAt: startedAt, Snapshot: probed.ModelCatalogSnapshot,
		}); err != nil {
			return result, err
		}
	}
	return probed, nil
}

func persistedModelCatalogFingerprint(environment models.RuntimeEnvironment, authGeneration uint64, accountFingerprint string) string {
	// CLI 版本是探测结果，启动写回版本时目录仍属于同一启动配置。
	scope := []any{environment.BaseRuntimeID, environment.Binary, environment.PrefixArgs,
		environment.ConfigHome, authGeneration, accountFingerprint}
	switch environment.BaseRuntimeID {
	case constants.RuntimeIDQoder, constants.RuntimeIDQoderCN:
		// 旧快照已丢弃 SDK 可用标记，必须重新读取才能排除不可用模型。
		scope = append(scope, "qoder-selectable-models")
	case constants.RuntimeIDClaude, constants.RuntimeIDPi:
		// 旧目录未记录进程或桥接组件能提供的原生权限档。
		scope = append(scope, "native-permission-modes")
	}
	body, _ := json.Marshal(scope)
	return fmt.Sprintf("%x", sha256.Sum256(body))
}

func (s *service) probeModelCatalog(ctx context.Context, environment models.RuntimeEnvironment, result AccountModelCatalog) AccountModelCatalog {
	accountID := result.AccountID
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
	}, environment.Detection.Version, fingerprint, true)
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

func (s *service) detectNative(ctx context.Context, summary agent.RuntimeSummary, environment models.RuntimeEnvironment, request agent.DetectRequest) agent.DetectionResult {
	result := agent.DetectionResult{
		Runtime: summary, State: agent.DetectionStateUnavailable, Auth: agent.AuthStatusUnknown,
	}
	result.ResolvedBinary, _ = s.runtimeCatalog.ResolveExecutable(request)
	if result.ResolvedBinary == "" {
		return result
	}
	snapshot, err := s.readModelCatalog(ctx, environment, environment.DefaultAccountID, true)
	if err != nil {
		logx.Warnf("refresh runtime model catalog: runtime_id=%s error=%v", environment.RuntimeID, err)
	}
	result.Version = snapshot.Capabilities.Version
	if snapshot.Capabilities.Version != "" {
		result.State = agent.DetectionStateReady
	}
	if snapshot.QoderLoginVerified {
		result.Auth = agent.AuthStatusOK
	}
	if snapshot.State == agent.CatalogSignedOut {
		result.State, result.Auth = agent.DetectionStateReady, agent.AuthStatusMissing
	}
	if snapshot.State != agent.CatalogFresh && snapshot.State != agent.CatalogStale {
		result.Diagnostics = []agent.Diagnostic{{
			Code: agent.DiagnosticCodeACPOptionsUnavailable, Level: agent.DiagnosticLevelWarning, Message: snapshot.ErrorCode,
		}}
		return result
	}
	result.State, result.Auth = agent.DetectionStateReady, agent.AuthStatusOK
	if environment.BaseRuntimeID != constants.RuntimeIDCodex && environment.BaseRuntimeID != constants.RuntimeIDClaude &&
		environment.BaseRuntimeID != constants.RuntimeIDCursor && !snapshot.QoderLoginVerified {
		// Other ACP providers may expose models before authentication.
		result.Auth = agent.AuthStatusUnknown
	}
	if snapshot.State == agent.CatalogStale {
		if !snapshot.QoderLoginVerified {
			result.Auth = agent.AuthStatusUnknown
		}
		result.Diagnostics = []agent.Diagnostic{{
			Code: agent.DiagnosticCodeACPOptionsUnavailable, Level: agent.DiagnosticLevelWarning, Message: snapshot.ErrorCode,
		}}
	}
	result.Capabilities = agent.CapabilitySet{
		agent.CapabilityDynamicModels:         true,
		agent.CapabilityPartialMessages:       true,
		agent.CapabilityResumeSession:         snapshot.Capabilities.Resume || snapshot.Capabilities.Load,
		agent.CapabilityAddDirectory:          snapshot.Capabilities.AdditionalDirectories,
		agent.CapabilityImagePath:             snapshot.Capabilities.ImageInput,
		agent.CapabilitySkillDirectory:        summary.SkillDirectoryName != "",
		agent.CapabilityMCPStreamableHTTP:     snapshot.Capabilities.MCPHTTP,
		agent.CapabilityStreamingUserMessages: snapshot.Capabilities.Steer,
	}
	result.DetectedDefaultModel = snapshot.DefaultID
	result.Models = make([]agent.ModelOption, 0, len(snapshot.Models))
	for _, model := range snapshot.Models {
		option := agent.ModelOption{ID: model.ID, Name: model.Name, PermissionLevels: NativePermissionLevels(environment.BaseRuntimeID, model)}
		if model.Config != nil {
			option.ReasoningOptions = []agent.ReasoningOption{}
			for _, setting := range model.Config.Options {
				if setting.Category != "thought_level" && setting.ID != "reasoning_effort" {
					continue
				}
				for _, choice := range setting.Choices {
					option.ReasoningOptions = append(option.ReasoningOptions, agent.ReasoningOption{ID: choice.ID, Name: choice.Name})
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
		levels := []constants.PermissionLevel{constants.PermissionLevelDefault}
		if slices.Contains(model.PermissionModes, "auto") {
			levels = append(levels, constants.PermissionLevelAuto)
		}
		return append(levels, constants.PermissionLevelFullAccess)
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
	if provider == constants.RuntimeIDClaude {
		if model.AutoMode {
			levels = append(levels, constants.PermissionLevelAuto)
		}
		if !slices.Contains(model.PermissionModes, "bypassPermissions") {
			return levels
		}
	}
	return append(levels, constants.PermissionLevelFullAccess)
}

func accountModelFingerprint(account models.AIAccount) string {
	encoded, _ := json.Marshal([]any{account.SelectedModels, account.DefaultModel, account.ModelReasoningLevels, account.ModelDisplayNames, account.ClaudeModelRoles, account.Endpoints})
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}
