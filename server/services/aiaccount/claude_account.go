package aiaccount

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	agentclient "github.com/JieWaZi/acp-go/pkg/agent_next"
	claude "github.com/JieWaZi/acp-go/pkg/agent_next/claude"
)

func resolveClaudeSystemAccount(ctx context.Context, request agent.AccountRequest, home string) (agent.AccountRequest, error) {
	if home == "" {
		home = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	canonical, environment, err := claudeSystemConfiguration(home)
	if err != nil {
		return agent.AccountRequest{}, err
	}
	request.ConfigHome, request.NativeConfigHome = canonical, home
	request.Subject, err = readClaudeSystemSubject(ctx, request, environment)
	if err != nil {
		return agent.AccountRequest{}, err
	}
	request.AuthGeneration = systemAccountGeneration
	return request, nil
}

func acquireClaudeSystemAccount(ctx context.Context, request agent.AccountRequest) (agent.AccountLease, error) {
	if request.Subject == "" || request.AuthGeneration != systemAccountGeneration || !filepath.IsAbs(request.ConfigHome) {
		return agent.AccountLease{}, status_error.AIAccountConfigurationInvalid
	}
	canonical, environment, err := claudeSystemConfiguration(request.NativeConfigHome)
	if err != nil {
		return agent.AccountLease{}, err
	}
	if canonical != request.ConfigHome {
		return agent.AccountLease{}, status_error.RuntimeAuthRequired
	}
	subject, err := readClaudeSystemSubject(ctx, request, environment)
	if err != nil {
		return agent.AccountLease{}, err
	}
	if subject != request.Subject {
		return agent.AccountLease{}, status_error.RuntimeAuthRequired
	}
	return agent.AccountLease{ConfigHome: canonical, Environment: environment}, nil
}

// claudeSystemConfiguration keeps an explicit CLI config path byte-for-byte. Claude
// derives the macOS Keychain service from that selection, including the unset default.
func claudeSystemConfiguration(nativeHome string) (string, map[string]string, error) {
	environment := claudeAccountEnvironment(nativeHome)
	effectiveHome := nativeHome
	if effectiveHome == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", nil, status_error.RuntimeConfigurationInvalid
		}
		if runtime.GOOS == "windows" {
			environment["USERPROFILE"] = userHome
		} else {
			environment["HOME"] = userHome
		}
		effectiveHome = filepath.Join(userHome, ".claude")
	}
	canonical, err := systemAccountHome(effectiveHome, true)
	return canonical, environment, err
}

// claudeAccountEnvironment clears ambient overrides before selecting native subscription or managed API authentication.
func claudeAccountEnvironment(home string) map[string]string {
	environment := accountconfig.HomeEnvironment(constants.RuntimeIDClaude, home)
	for _, key := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL",
		"CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"} {
		environment[key] = ""
	}
	return environment
}

func readClaudeSystemSubject(ctx context.Context, request agent.AccountRequest, environment map[string]string) (string, error) {
	client, err := claude.NewClient(ctx, claude.Options{
		Executable: request.Executable, PrefixArgs: request.PrefixArgs, WorkingDirectory: request.CWD,
		Environment: nativeAccountEnvironment(environment),
	}, agentclient.HostCallbacks{})
	if err != nil {
		return "", nativeAccountStatusError(ctx, constants.RuntimeIDClaude, err)
	}
	login, err := client.ReadLoginStatus(ctx)
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if closeErr := client.Close(closeCtx); closeErr != nil {
		logx.Errorf("native account status cleanup failed: provider=%s", constants.RuntimeIDClaude)
		return "", status_error.RuntimeShutdownFailed
	}
	if err != nil {
		return "", nativeAccountStatusError(ctx, constants.RuntimeIDClaude, err)
	}
	if !login.Authenticated || login.Kind != "subscription" || login.AccountID == "" || login.OrganizationID == "" {
		return "", status_error.RuntimeAuthRequired
	}
	if login.ConfigDirectory != "" {
		root, err := systemAccountHome(login.ConfigDirectory, true)
		if err != nil || root != request.ConfigHome {
			return "", status_error.RuntimeAuthRequired
		}
	}
	return accountSubject("claude-system-v1", request.ConfigHome, request.NativeConfigHome, login.AccountID, login.OrganizationID), nil
}

func (s *Service) resolveManagedClaudeAccount(ctx context.Context, request agent.AccountRequest) (agent.AccountRequest, error) {
	if !isManagedAccountID(request.AccountID) {
		return agent.AccountRequest{}, status_error.AIAccountConfigurationInvalid
	}
	release, err := s.acquireLease(ctx, request.AccountID, false, false)
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
	if !ok || account.AuthMode != constants.AccountAuthModeAPIKey || !modelAllowed {
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
	request.Subject = accountSubject("claude-managed-api-v1", account.ID)
	request.ConfigHome, request.AuthGeneration = home, account.AuthGeneration
	return request, account, endpoint, nil
}

func (s *Service) acquireManagedClaudeAccount(ctx context.Context, request agent.AccountRequest) (agent.AccountLease, error) {
	if !isManagedAccountID(request.AccountID) {
		return agent.AccountLease{}, status_error.AIAccountConfigurationInvalid
	}
	release, err := s.acquireLease(ctx, request.AccountID, false, false)
	if err != nil {
		return agent.AccountLease{}, err
	}
	lease, err := s.prepareManagedClaudeAccount(ctx, request)
	if err != nil {
		release()
		return agent.AccountLease{}, err
	}
	lease.Close = func(context.Context) error { release(); return nil }
	return lease, nil
}

func (s *Service) prepareManagedClaudeAccount(ctx context.Context, request agent.AccountRequest) (agent.AccountLease, error) {
	resolved, account, endpoint, err := s.resolveManagedClaude(ctx, request)
	if err != nil {
		return agent.AccountLease{}, err
	}
	if !resolved.Equal(request) {
		return agent.AccountLease{}, status_error.RuntimeAuthRequired
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
	maps.Copy(env, accountconfig.ClaudeAPIEnvironment(spec))
	return agent.AccountLease{ConfigHome: home, Environment: env}, nil
}

// ManagedClaudeModelTarget matches a native catalog or turn model to the account's
// configured actual models. An omitted selection returns the configured default.
func ManagedClaudeModelTarget(account models.AIAccount, model string) (string, bool) {
	return accountconfig.ClaudeModelTarget(managedClaudeModelSpec(account), model)
}

// managedClaudeModelSpec stays the same when the session selects another model.
// The native setter, rather than the account default environment, applies that selection.
func managedClaudeModelSpec(account models.AIAccount) accountconfig.Spec {
	selected := slices.DeleteFunc(slices.Clone(account.SelectedModels), func(model string) bool {
		return model == "" || model == "default"
	})
	model := account.DefaultModel
	if len(selected) > 0 && !slices.Contains(selected, model) {
		model = selected[0]
	}
	return accountconfig.Spec{
		Model: model, SelectedModels: selected,
		ClaudeModelRoles: account.ClaudeModelRoles, ModelDisplayNames: account.ModelDisplayNames,
	}
}
