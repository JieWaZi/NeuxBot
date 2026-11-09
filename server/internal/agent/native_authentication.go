package agent

import (
	"context"
	"maps"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	acp "github.com/JieWaZi/acp-go-sdk"
)

// NativeAuthenticationMethod describes a capability verified against the selected native program.
type NativeAuthenticationMethod struct {
	// ID is the native method identity, never a host-invented OAuth flow.
	ID string
	// Name is the native display label.
	Name string
	// Kind is agent, env_var, or terminal.
	Kind string
}

// NativeAuthenticationCapabilities preserves native login and configuration choices.
type NativeAuthenticationCapabilities struct {
	// Methods contains only methods advertised by this initialized installation.
	Methods []NativeAuthenticationMethod
	// APIKeyMode means this process selected a native API credential instead of browser authentication.
	APIKeyMode bool
	// EnvironmentCredential means an explicit native credential takes precedence over browser login.
	EnvironmentCredential bool
}

// NativeAuthenticationClient owns one management process without opening a session.
type NativeAuthenticationClient interface {
	Initialize(context.Context) (NativeAuthenticationCapabilities, error)
	Authenticate(context.Context, string) (constants.RuntimeAuthenticationResult, error)
	Close(context.Context) error
}

type nativeAuthenticationClient struct {
	// provider selects native authentication semantics without inferring from a void RPC.
	provider string
	// client retains the native owner through failed initialization and cleanup.
	client *directAgentClient
	// environmentCredential freezes whether the selected system environment overrides native login.
	environmentCredential bool
}

// NewSystemAuthenticationClient constructs a native system-scope management owner.
// Unlike an execution lease this deliberately preserves the selected native environment,
// including a signed-out configuration root; no managed account is restored or created.
func NewSystemAuthenticationClient(ctx context.Context, account AccountRequest, launch ProviderLaunch) (NativeAuthenticationClient, error) {
	if account.AccountID != SystemAccountID {
		return nil, status_error.RuntimeConfigurationInvalid
	}
	environment := systemAuthenticationEnvironment(account.Provider, launch)
	if account.NativeConfigHome != "" {
		variable := systemAuthenticationHomeVariable(account.Provider)
		if variable == "" {
			return nil, status_error.RuntimeConfigurationInvalid
		}
		environment[variable] = account.NativeConfigHome
		if account.Provider == constants.RuntimeIDClaude {
			environment["CLAUDE_SECURESTORAGE_CONFIG_DIR"] = account.NativeConfigHome
		}
	}
	if account.Provider == constants.RuntimeIDKimi {
		return &kimiAuthenticationClient{account: account, launch: launch, environment: environment}, nil
	}
	client, err := newDirectAgentClient(ctx, account, launch, AccountLease{Environment: environment}, LaunchAuthLease{}, ResourceLease{}, AgentClientCallbacks{})
	if err != nil {
		return nil, err
	}
	return &nativeAuthenticationClient{provider: account.Provider, client: client, environmentCredential: systemAuthenticationHasCredential(account.Provider, environment)}, nil
}

func systemAuthenticationHasCredential(provider string, environment map[string]string) bool {
	var variables []string
	switch provider {
	case constants.RuntimeIDCodex:
		variables = []string{"CODEX_API_KEY", "OPENAI_API_KEY"}
	case constants.RuntimeIDClaude:
		variables = []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"}
	case constants.RuntimeIDCursor:
		variables = []string{"CURSOR_API_KEY", "CURSOR_AUTH_TOKEN"}
	case constants.RuntimeIDWorkBuddy:
		variables = []string{"CODEBUDDY_AUTH_TOKEN", "CODEBUDDY_API_KEY"}
	case constants.RuntimeIDQoder:
		variables = []string{"QODER_PERSONAL_ACCESS_TOKEN"}
	case constants.RuntimeIDKimi:
		variables = []string{"KIMI_API_KEY", "KIMI_MODEL_API_KEY"}
	case constants.RuntimeIDGemini:
		variables = []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_GENAI_USE_VERTEXAI"}
	case constants.RuntimeIDGrok:
		variables = []string{"XAI_API_KEY", "GROK_API_KEY"}
	}
	for key, value := range environment {
		for _, variable := range variables {
			if strings.EqualFold(key, variable) && value != "" {
				if strings.Contains(variable, "_USE_") && (value == "0" || strings.EqualFold(value, "false")) {
					continue
				}
				return true
			}
		}
	}
	return false
}

// systemAuthenticationEnvironment restores only the selected native credential scope
// after the execution environment's ordinary credential stripping.
func systemAuthenticationEnvironment(provider string, launch ProviderLaunch) map[string]string {
	base := maps.Clone(launch.BaseEnvironment)
	if base == nil {
		base = map[string]string{}
	}
	maps.Copy(base, launch.Environment)
	selected := map[string]string{}
	for key, value := range base {
		upper := strings.ToUpper(key)
		keep := false
		switch provider {
		case constants.RuntimeIDCodex:
			keep = strings.HasPrefix(upper, "CODEX_") || strings.HasPrefix(upper, "OPENAI_")
		case constants.RuntimeIDClaude:
			keep = strings.HasPrefix(upper, "CLAUDE_") || strings.HasPrefix(upper, "ANTHROPIC_") || strings.HasPrefix(upper, "AWS_") || strings.HasPrefix(upper, "AZURE_") || strings.HasPrefix(upper, "GOOGLE_")
		case constants.RuntimeIDCursor:
			keep = strings.HasPrefix(upper, "CURSOR_")
		case constants.RuntimeIDWorkBuddy:
			keep = strings.HasPrefix(upper, "CODEBUDDY_")
		case constants.RuntimeIDQoder:
			keep = strings.HasPrefix(upper, "QODER_")
		case constants.RuntimeIDKimi:
			keep = strings.HasPrefix(upper, "KIMI_")
		case constants.RuntimeIDGemini:
			keep = strings.HasPrefix(upper, "GEMINI_") || strings.HasPrefix(upper, "GOOGLE_")
		case constants.RuntimeIDGrok:
			keep = strings.HasPrefix(upper, "GROK_") || strings.HasPrefix(upper, "XAI_")
		case constants.RuntimeIDOpenCode, constants.RuntimeIDPi:
			// These CLIs intentionally select among multiple native model providers.
			keep = strings.HasSuffix(upper, "_API_KEY") || strings.HasSuffix(upper, "_BASE_URL") || strings.HasPrefix(upper, "OPENCODE_") || strings.HasPrefix(upper, "PI_") || strings.HasPrefix(upper, "XDG_")
		}
		if keep {
			selected[key] = value
		}
	}
	return selected
}

func systemAuthenticationHomeVariable(provider string) string {
	switch provider {
	case constants.RuntimeIDCodex:
		return "CODEX_HOME"
	case constants.RuntimeIDClaude:
		return "CLAUDE_CONFIG_DIR"
	case constants.RuntimeIDCursor:
		return "CURSOR_CONFIG_DIR"
	case constants.RuntimeIDWorkBuddy:
		return "CODEBUDDY_CONFIG_DIR"
	case constants.RuntimeIDQoder:
		return "QODER_CONFIG_DIR"
	case constants.RuntimeIDKimi:
		return "KIMI_CODE_HOME"
	case constants.RuntimeIDGemini:
		return "GEMINI_CLI_HOME"
	case constants.RuntimeIDGrok:
		return "GROK_HOME"
	case constants.RuntimeIDOpenCode:
		return "OPENCODE_CONFIG_DIR"
	case constants.RuntimeIDPi:
		return "PI_CODING_AGENT_DIR"
	}
	return ""
}

func (c *nativeAuthenticationClient) Initialize(ctx context.Context) (NativeAuthenticationCapabilities, error) {
	response, err := c.client.native.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientInfo:      &acp.Implementation{Name: "neuxbot", Version: "unknown"},
	})
	if err != nil {
		return NativeAuthenticationCapabilities{}, safeAgentError(err, status_error.RuntimeUnavailable)
	}
	if response.ProtocolVersion != acp.ProtocolVersionNumber {
		return NativeAuthenticationCapabilities{}, status_error.RuntimeProtocolIncompatible
	}
	capabilities := NativeAuthenticationCapabilities{APIKeyMode: response.Meta["defaultAuthMethodId"] == "xai.api_key", EnvironmentCredential: c.environmentCredential}
	for _, method := range response.AuthMethods {
		switch {
		case method.Agent != nil:
			capabilities.Methods = append(capabilities.Methods, NativeAuthenticationMethod{ID: method.Agent.Id, Name: method.Agent.Name, Kind: "agent"})
		case method.EnvVar != nil:
			capabilities.Methods = append(capabilities.Methods, NativeAuthenticationMethod{ID: method.EnvVar.Id, Name: method.EnvVar.Name, Kind: "env_var"})
		case method.Terminal != nil:
			capabilities.Methods = append(capabilities.Methods, NativeAuthenticationMethod{ID: method.Terminal.Id, Name: method.Terminal.Name, Kind: "terminal"})
		}
	}
	return capabilities, nil
}

func (c *nativeAuthenticationClient) Authenticate(ctx context.Context, method string) (constants.RuntimeAuthenticationResult, error) {
	codexSubscription := c.provider == constants.RuntimeIDCodex && method == constants.CodexSubscriptionAuthMethod
	existingLogin := false
	if codexSubscription {
		// This query only classifies the existing context. Codex's Authenticate must
		// still run its refresh=true validation; cached identity alone proves no login.
		before, err := c.client.ReadAccount(ctx)
		existingLogin = err == nil && before.Account != nil && before.Account.Kind == constants.NativeAccountKindAccount
		if ctx.Err() != nil {
			return constants.RuntimeAuthenticationResultUnspecified, ctx.Err()
		}
	}
	if _, err := c.client.native.Authenticate(ctx, acp.AuthenticateRequest{MethodId: method}); err != nil {
		return constants.RuntimeAuthenticationResultUnspecified, safeAgentError(err, status_error.RuntimeAuthRequired)
	}
	if codexSubscription {
		// The adapter can return an empty success for an existing ChatGPT login.
		// Without a public loginId/completion receipt, never claim new authorization.
		after, err := c.client.ReadAccount(ctx)
		if err != nil {
			return constants.RuntimeAuthenticationResultUnspecified, err
		}
		if after.Account == nil || after.Account.Kind != constants.NativeAccountKindAccount {
			return constants.RuntimeAuthenticationResultUnspecified, status_error.RuntimeAuthRequired
		}
		if existingLogin {
			return constants.RuntimeAuthenticationResultExistingLoginVerified, nil
		}
		return constants.RuntimeAuthenticationResultConnectionVerified, nil
	}
	return constants.RuntimeAuthenticationResultAuthenticationCompleted, nil
}

func (c *nativeAuthenticationClient) Close(ctx context.Context) error {
	if err := c.client.native.Close(ctx); err != nil {
		return safeAgentError(err, status_error.RuntimeShutdownFailed)
	}
	return nil
}

// BeginSystemAuthentication fences new system consumers before the host drains idle threads.
// Managed accounts remain independently available. The host must retain release until
// the exact management process has confirmed cleanup, including failed-close retries.
func (r *ProviderRuntime) BeginSystemAuthentication(_ context.Context, provider string) (func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing {
		return nil, status_error.RuntimeUnavailable
	}
	if r.restarting[provider] || r.systemAuthentication[provider] {
		return nil, status_error.AIAccountBusy
	}
	if r.systemAuthentication == nil {
		r.systemAuthentication = map[string]bool{}
	}
	r.systemAuthentication[provider] = true
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			delete(r.systemAuthentication, provider)
			r.mu.Unlock()
		})
	}, nil
}

// CheckSystemAuthenticationIdle confirms that no native consumer still owns this scope.
// The admission fence must already be held by BeginSystemAuthentication.
func (r *ProviderRuntime) CheckSystemAuthenticationIdle(provider string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.systemAuthentication[provider] {
		return status_error.RuntimeConfigurationInvalid
	}
	for _, connection := range r.connections {
		if connection.account.provider == provider && connection.account.id == SystemAccountID {
			return status_error.AIAccountBusy
		}
	}
	return nil
}

// CloseIdleSystemRuntime drains idle system threads while authentication blocks admission.
func (l *ThreadLifecycle) CloseIdleSystemRuntime(ctx context.Context, provider string) error {
	return l.closeIdleConsumers(ctx, status_error.AIAccountBusy, func(state *threadState) bool {
		account := providerAccount{provider: provider, id: SystemAccountID}
		connected := state.connection != nil && state.connection.account == account
		admitted := state.active != nil && state.admittedAccount == account
		return connected || admitted
	})
}
