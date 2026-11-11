package aiaccount

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func (service *Service) resolveClaudeSystemAccount(ctx context.Context, request agent.AccountRequest, home string) (agent.AccountRequest, error) {
	if home == "" {
		home = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	selected, err := claudeSystemConfiguration(home)
	if err != nil {
		return agent.AccountRequest{}, err
	}
	request.ConfigHome, request.NativeConfigHome = selected.configHome, home
	request.ConfigFingerprint = selected.provider.ModelFingerprint
	request.Subject, err = selected.subject(ctx, request)
	if err != nil {
		return agent.AccountRequest{}, err
	}
	request.AuthGeneration = systemAccountGeneration
	return request, nil
}

func (service *Service) acquireClaudeSystemAccount(ctx context.Context, request agent.AccountRequest) (agent.AccountLease, error) {
	if request.Subject == "" || request.AuthGeneration != systemAccountGeneration || !filepath.IsAbs(request.ConfigHome) {
		return agent.AccountLease{}, status_error.AIAccountConfigurationInvalid
	}
	selected, err := claudeSystemConfiguration(request.NativeConfigHome)
	if err != nil {
		return agent.AccountLease{}, err
	}
	if selected.configHome != request.ConfigHome {
		return agent.AccountLease{}, status_error.RuntimeAuthRequired
	}
	if selected.provider.ModelFingerprint != request.ConfigFingerprint {
		return agent.AccountLease{}, status_error.RuntimeConfigurationInvalid
	}
	verify := func(ctx context.Context) error {
		subject, err := selected.subject(ctx, request)
		if err != nil {
			return err
		}
		if subject != request.Subject {
			return status_error.RuntimeAuthRequired
		}
		return nil
	}
	if err := verify(ctx); err != nil {
		return agent.AccountLease{}, err
	}
	lease := agent.AccountLease{ConfigHome: selected.configHome, Environment: selected.environment}
	if selected.provider.CredentialVariable == "" {
		lease.VerifyLogin = verify
	}
	return lease, nil
}

type claudeSystemSelection struct {
	// configHome is the canonical directory used for compatibility.
	configHome string
	// provider contains the explicitly selected provider settings.
	provider accountconfig.ClaudeSystemProvider
	// environment is the exact launch environment contribution for this selection.
	environment map[string]string
}

// claudeSystemConfiguration keeps an explicit CLI config path byte-for-byte. Claude
// derives the macOS Keychain service from that selection, including the unset default.
func claudeSystemConfiguration(nativeHome string) (claudeSystemSelection, error) {
	environment := claudeAccountEnvironment(nativeHome)
	effectiveHome := nativeHome
	if effectiveHome == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return claudeSystemSelection{}, status_error.RuntimeConfigurationInvalid
		}
		if runtime.GOOS == "windows" {
			environment["USERPROFILE"] = userHome
		} else {
			environment["HOME"] = userHome
		}
		effectiveHome = filepath.Join(userHome, ".claude")
	}
	canonical, err := systemAccountHome(effectiveHome, true)
	if err != nil {
		return claudeSystemSelection{}, err
	}
	settings, err := readClaudeSystemSettings(canonical)
	if err != nil {
		return claudeSystemSelection{}, err
	}
	defer clear(settings)
	provider, err := accountconfig.SelectClaudeSystemProvider(settings, os.Getenv)
	if err != nil {
		return claudeSystemSelection{}, err
	}
	maps.Copy(environment, provider.Environment)
	return claudeSystemSelection{configHome: canonical, provider: provider, environment: environment}, nil
}

func readClaudeSystemSettings(home string) ([]byte, error) {
	path, err := filepath.EvalSymlinks(filepath.Join(home, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, status_error.AIAccountCredentialError
	}
	return readNativeAccountFile(path)
}

// readClaudeSystemMetadata 保留原生 CLI 的 16 MiB 元数据上限；凭据仍由独立入口限制为 1 MiB。
func readClaudeSystemMetadata(path string) ([]byte, error) {
	canonical, err := filepath.EvalSymlinks(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, status_error.AIAccountCredentialError
	}
	return readNativeAccountFileBounded(canonical, 16<<20)
}

// claudeAccountEnvironment clears ambient overrides before selecting native subscription or managed API authentication.
func claudeAccountEnvironment(home string) map[string]string {
	environment := accountconfig.HomeEnvironment(constants.RuntimeIDClaude, home)
	if home != "" {
		environment["CLAUDE_SECURESTORAGE_CONFIG_DIR"] = home
	}
	for _, key := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL",
		"CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"} {
		environment[key] = ""
	}
	return environment
}

func (s claudeSystemSelection) subject(ctx context.Context, request agent.AccountRequest) (string, error) {
	nativeHome := request.NativeConfigHome
	if nativeHome == "" {
		userHome := s.environment["HOME"]
		if runtime.GOOS == "windows" {
			userHome = s.environment["USERPROFILE"]
		}
		nativeHome = filepath.Join(userHome, ".claude")
	}
	canonical, err := systemAccountHome(nativeHome, true)
	if err != nil || canonical != request.ConfigHome {
		return "", status_error.RuntimeAuthRequired
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if key := s.provider.CredentialVariable; key != "" {
		endpoint := s.environment["ANTHROPIC_BASE_URL"]
		if endpoint == "" {
			endpoint = constants.AIAccountAnthropicURL
		}
		return accountSubject("claude-system-environment", request.ConfigHome, request.NativeConfigHome, key, endpoint, s.environment[key]), nil
	}
	facts := []string{request.ConfigHome, request.NativeConfigHome}
	// 系统登录只观察本地元数据，不要求宿主读取默认 Keychain 或证明在线状态。
	// account/org 保持令牌刷新兼容；缺少可观察身份时原生 CLI 自行认证。
	metadataRoot := request.NativeConfigHome
	if metadataRoot == "" {
		metadataRoot = s.environment["HOME"]
		if runtime.GOOS == "windows" {
			metadataRoot = s.environment["USERPROFILE"]
		}
	}
	for _, path := range []string{filepath.Join(request.ConfigHome, constants.ClaudeLegacyMetadataFileName), filepath.Join(metadataRoot, constants.ClaudeAccountMetadataFileName)} {
		data, err := readClaudeSystemMetadata(path)
		if err != nil {
			return "", err
		}
		defer clear(data)
		if data == nil {
			continue
		}
		account, organization := claudeMetadataIdentity(data)
		if account != "" && organization != "" {
			return accountSubject("claude-system-local", append(facts, account, organization)...), nil
		}
		// 原生优先使用已存在的 legacy 文件，不用另一份元数据补造身份。
		facts = append(facts, path, accountSubject("claude-local-metadata", string(data)))
		break
	}
	return accountSubject("claude-system-local", facts...), nil
}

func (s *Service) resolveManagedClaudeAccount(ctx context.Context, request agent.AccountRequest) (agent.AccountRequest, error) {
	if err := s.retryClaudeLeaseCleanups(ctx, request.AccountID); err != nil {
		return agent.AccountRequest{}, err
	}
	if !isManagedAccountID(request.AccountID) {
		return agent.AccountRequest{}, status_error.AIAccountConfigurationInvalid
	}
	release, err := s.acquireLease(ctx, request.AccountID, false, request.WaitForLease)
	if err != nil {
		return agent.AccountRequest{}, err
	}
	defer release()
	resolved, _, _, err := s.resolveManagedClaude(ctx, request)
	return resolved, err
}

func (s *Service) resolveManagedClaude(ctx context.Context, request agent.AccountRequest) (agent.AccountRequest, models.AIAccount, models.AIAccountEndpoint, error) {
	account, err := s.accounts.Get(ctx, request.AccountID)
	if err != nil {
		return agent.AccountRequest{}, account, models.AIAccountEndpoint{}, err
	}
	endpoint, ok := accountEndpoint(account, constants.RuntimeIDClaude)
	_, modelAllowed := ManagedClaudeModelTarget(account, request.Model)
	if !ok || (account.AuthMode != constants.AccountAuthModeSubscription && (account.AuthMode != constants.AccountAuthModeAPIKey || !modelAllowed)) {
		return agent.AccountRequest{}, account, endpoint, status_error.AIAccountConfigurationInvalid
	}
	if !account.HasCredential {
		return agent.AccountRequest{}, account, endpoint, status_error.RuntimeAuthRequired
	}
	if err := accountconfig.ValidateEndpoint(endpoint.Protocol, endpoint.BaseURL); err != nil {
		return agent.AccountRequest{}, account, endpoint, err
	}
	home, err := s.managedAccountHome(constants.RuntimeIDClaude, account, false)
	if err != nil {
		return agent.AccountRequest{}, account, endpoint, err
	}
	if account.AuthMode == constants.AccountAuthModeSubscription {
		snapshot, err := s.claudeStoredCredential(ctx, account)
		if err != nil {
			return agent.AccountRequest{}, account, endpoint, err
		}
		request.Subject = accountSubject("claude-managed-subscription", account.ID, snapshot.AccountID, snapshot.OrganizationID)
	} else {
		request.Subject = accountSubject("claude-managed-api", account.ID)
	}
	request.ConfigHome, request.AuthGeneration = home, account.AuthGeneration
	request.ConfigFingerprint = ManagedClaudeModelFingerprint(account)
	return request, account, endpoint, nil
}

func (s *Service) acquireManagedClaudeAccount(ctx context.Context, request agent.AccountRequest) (agent.AccountLease, error) {
	if err := s.retryClaudeLeaseCleanups(ctx, request.AccountID); err != nil {
		return agent.AccountLease{}, err
	}
	if !isManagedAccountID(request.AccountID) {
		return agent.AccountLease{}, status_error.AIAccountConfigurationInvalid
	}
	release, err := s.acquireLease(ctx, request.AccountID, false, request.WaitForLease)
	if err != nil {
		return agent.AccountLease{}, err
	}
	lease, err := s.prepareManagedClaudeAccount(ctx, request)
	if err != nil {
		if lease.Close != nil {
			s.retainClaudeLeaseCleanup(request.AccountID, lease.Close, release)
		} else {
			release()
		}
		return agent.AccountLease{}, err
	}
	innerClose := lease.Close
	var closeMutex sync.Mutex
	closed := false
	lease.Close = func(ctx context.Context) error {
		closeMutex.Lock()
		defer closeMutex.Unlock()
		if closed {
			return nil
		}
		if innerClose != nil {
			if err := innerClose(ctx); err != nil {
				return err
			}
		}
		release()
		closed = true
		return nil
	}
	return lease, nil
}

func (s *Service) prepareManagedClaudeAccount(ctx context.Context, request agent.AccountRequest) (agent.AccountLease, error) {
	resolved, account, endpoint, err := s.resolveManagedClaude(ctx, request)
	if err != nil {
		return agent.AccountLease{}, err
	}
	if resolved.ConfigFingerprint != request.ConfigFingerprint {
		return agent.AccountLease{}, status_error.RuntimeConfigurationInvalid
	}
	if !resolved.Equal(request) {
		return agent.AccountLease{}, status_error.RuntimeAuthRequired
	}
	if account.AuthMode == constants.AccountAuthModeSubscription {
		return s.prepareClaudeSubscription(ctx, request, account)
	}
	key, err := s.accounts.Credential(ctx, account.ID)
	if err != nil {
		return agent.AccountLease{}, err
	}
	if strings.TrimSpace(key) == "" {
		return agent.AccountLease{}, status_error.RuntimeAuthRequired
	}
	home, err := s.managedAccountHome(constants.RuntimeIDClaude, account, true)
	if err != nil {
		return agent.AccountLease{}, err
	}
	if home != request.ConfigHome {
		return agent.AccountLease{}, status_error.RuntimeAuthRequired
	}
	spec := managedClaudeModelSpec(account)
	spec.APIKey, spec.BaseURL = key, endpoint.BaseURL
	env := claudeAccountEnvironment(home)
	env["CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST"] = "1"
	maps.Copy(env, accountconfig.ClaudeAPIEnvironment(spec))
	return agent.AccountLease{ConfigHome: home, Environment: env}, nil
}

// ManagedClaudeModelTarget matches a native catalog or turn model to the account's
// configured actual models. An omitted selection returns the configured default.
func ManagedClaudeModelTarget(account models.AIAccount, model string) (string, bool) {
	return accountconfig.ClaudeModelTarget(managedClaudeModelSpec(account), model)
}

// ManagedClaudeModelFingerprint freezes the model selection and native projection
// independently of credential generation, using the same model spec as the lease.
func ManagedClaudeModelFingerprint(account models.AIAccount) string {
	spec := managedClaudeModelSpec(account)
	encoded, _ := json.Marshal(accountconfig.ClaudeAPIEnvironment(spec))
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// managedClaudeModelSpec stays the same when the session selects another model.
// The native setter, rather than the account default environment, applies that selection.
func managedClaudeModelSpec(account models.AIAccount) accountconfig.Spec {
	selected := slices.DeleteFunc(slices.Clone(account.SelectedModels), func(model string) bool {
		return model == "" || model == "default"
	})
	model := ""
	if len(selected) > 0 {
		model = selected[0]
	}
	return accountconfig.Spec{
		Model: model, SelectedModels: selected,
		ClaudeModelRoles: account.ClaudeModelRoles, ModelDisplayNames: account.ModelDisplayNames,
	}
}
