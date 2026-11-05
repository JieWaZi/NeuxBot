package aiaccount

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
)

type nativeCLISystemProfile struct {
	// configHome holds the canonical native configuration root.
	configHome string
	// dataHome holds the native data root when separate from configuration.
	dataHome string
	// environment freezes the native roots and credentials supplied at launch.
	environment map[string]string
	// roots pairs the selected lexical roots with their canonical identities.
	roots []string
	// settings holds the evidence that determines model or provider selection.
	settings []string
	// credentials holds locally observable credential evidence.
	credentials []string
}

func validateNativeCLIAccountArgs(args []string) error {
	for _, argument := range args {
		option, _, _ := strings.Cut(argument, "=")
		switch option {
		case "--config", "--config-file", "--config-dir", "--agent-dir", "--session-dir", "--sdk-auth-file",
			"--home", "--cwd", "-w", "--worktree", "--add-dir":
			return status_error.RuntimeConfigurationInvalid
		}
	}
	return nil
}

func resolveNativeCLISystemAccount(ctx context.Context, request agent.AccountRequest, configuredHome string) (agent.AccountRequest, error) {
	selected, _, err := nativeCLISystemSelection(ctx, request, configuredHome)
	return selected, err
}

func acquireNativeCLISystemAccount(ctx context.Context, request agent.AccountRequest) (agent.AccountLease, error) {
	if request.AuthGeneration != systemAccountGeneration || request.Subject == "" {
		return agent.AccountLease{}, status_error.AIAccountConfigurationInvalid
	}
	recheck := func(ctx context.Context) (nativeCLISystemProfile, error) {
		selected, profile, err := nativeCLISystemSelection(ctx, request, request.NativeConfigHome)
		if err != nil {
			return nativeCLISystemProfile{}, err
		}
		if selected.ConfigFingerprint != request.ConfigFingerprint {
			return nativeCLISystemProfile{}, status_error.RuntimeConfigurationInvalid
		}
		if selected.Subject != request.Subject {
			return nativeCLISystemProfile{}, status_error.RuntimeAuthRequired
		}
		if !selected.Equal(request) {
			return nativeCLISystemProfile{}, status_error.RuntimeConfigurationInvalid
		}
		return profile, nil
	}
	profile, err := recheck(ctx)
	if err != nil {
		return agent.AccountLease{}, err
	}
	return agent.AccountLease{
		ConfigHome: profile.configHome, Environment: profile.environment,
		VerifyLogin: func(ctx context.Context) error {
			_, err := recheck(ctx)
			return err
		},
	}, nil
}

func nativeCLISystemSelection(ctx context.Context, request agent.AccountRequest, configuredHome string) (agent.AccountRequest, nativeCLISystemProfile, error) {
	if err := ctx.Err(); err != nil {
		return agent.AccountRequest{}, nativeCLISystemProfile{}, err
	}
	if request.AccountID != agent.SystemAccountID || !filepath.IsAbs(request.CWD) {
		return agent.AccountRequest{}, nativeCLISystemProfile{}, status_error.AIAccountConfigurationInvalid
	}
	if err := validateNativeCLIAccountArgs(request.PrefixArgs); err != nil {
		return agent.AccountRequest{}, nativeCLISystemProfile{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return agent.AccountRequest{}, nativeCLISystemProfile{}, status_error.RuntimeConfigurationInvalid
	}
	canonical, err := systemAccountHome(home, false)
	if err != nil {
		return agent.AccountRequest{}, nativeCLISystemProfile{}, err
	}
	homeVariable := "HOME"
	if runtime.GOOS == "windows" {
		homeVariable = "USERPROFILE"
	}
	profile := nativeCLISystemProfile{environment: map[string]string{homeVariable: home}, roots: []string{home, canonical}}
	if err := profile.selectProvider(ctx, request, home, configuredHome); err != nil {
		return agent.AccountRequest{}, nativeCLISystemProfile{}, err
	}
	environment, _ := json.Marshal(profile.environment)
	defer clear(environment)
	environmentDigest := sha256.Sum256(environment)
	request.ConfigHome, request.NativeConfigHome, request.DataHome = profile.configHome, configuredHome, profile.dataHome
	request.ConfigFingerprint = accountSubject("native-cli-system-config-v1", append(profile.roots, profile.settings...)...)
	// This opaque identity fences observable changes. The native CLI remains the
	// authority for signed-out state and for credentials stored outside these paths.
	request.Subject = accountSubject("native-cli-system-scope-v1", append([]string{
		request.Provider, request.ConfigHome, request.DataHome, hex.EncodeToString(environmentDigest[:]),
	}, profile.credentials...)...)
	request.AuthGeneration = systemAccountGeneration
	return request, profile, nil
}

func (p *nativeCLISystemProfile) selectProvider(ctx context.Context, request agent.AccountRequest, home, configuredHome string) error {
	var rootKey, defaultRoot string
	switch request.Provider {
	case constants.RuntimeIDKimi:
		rootKey, defaultRoot = "KIMI_CODE_HOME", ".kimi-code"
	case constants.RuntimeIDGemini:
		rootKey, defaultRoot = "GEMINI_CLI_HOME", "."
	case constants.RuntimeIDGrok:
		rootKey, defaultRoot = "GROK_HOME", ".grok"
	case constants.RuntimeIDPi:
		rootKey, defaultRoot = "PI_CODING_AGENT_DIR", filepath.Join(".pi", "agent")
	case constants.RuntimeIDOpenCode:
		return p.selectOpenCode(ctx, home, configuredHome)
	default:
		return status_error.RuntimeNotRegistered
	}
	var err error
	p.configHome, err = p.selectRoot(rootKey, configuredHome, filepath.Join(home, defaultRoot))
	if err != nil {
		return err
	}
	p.dataHome = p.configHome
	switch request.Provider {
	case constants.RuntimeIDKimi:
		return p.selectKimi(home, configuredHome)
	case constants.RuntimeIDGemini:
		p.capture("GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_GEMINI_BASE_URL", "GOOGLE_VERTEX_BASE_URL", "GOOGLE_GENAI_API_VERSION",
			"GOOGLE_GENAI_USE_VERTEXAI", "GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION", "GOOGLE_APPLICATION_CREDENTIALS",
			"GEMINI_DEFAULT_AUTH_TYPE", "GEMINI_MODEL")
		profile := filepath.Join(p.configHome, ".gemini")
		if err := p.readGeminiAuthSettings(filepath.Join(profile, "settings.json")); err != nil {
			return err
		}
		credentialPath := p.environment["GOOGLE_APPLICATION_CREDENTIALS"]
		if credentialPath != "" && !filepath.IsAbs(credentialPath) {
			credentialPath = filepath.Join(request.CWD, credentialPath)
		}
		if err := p.readRefreshCredential(filepath.Join(profile, "oauth_creds.json")); err != nil {
			return err
		}
		return p.readFiles(&p.credentials, filepath.Join(profile, "google_accounts.json"), filepath.Join(profile, "google_credentials.json"), credentialPath)
	case constants.RuntimeIDGrok:
		p.capture("XAI_API_KEY", "GROK_API_KEY", "GROK_MODELS_BASE_URL", "GROK_BASE_URL")
		if err := p.readProviderTOML(filepath.Join(p.configHome, "config.toml"), "model", "env_key"); err != nil {
			return err
		}
		return p.readRefreshCredential(filepath.Join(p.configHome, "auth.json"))
	case constants.RuntimeIDPi:
		p.capture("ANTHROPIC_API_KEY", "ANTHROPIC_OAUTH_TOKEN", "OPENAI_API_KEY", "GEMINI_API_KEY", "GROQ_API_KEY",
			"CEREBRAS_API_KEY", "XAI_API_KEY", "OPENROUTER_API_KEY", "MISTRAL_API_KEY", "AZURE_OPENAI_API_KEY")
		if os.Getenv("PI_CODING_AGENT_SESSION_DIR") != "" {
			p.dataHome, err = p.selectRoot("PI_CODING_AGENT_SESSION_DIR", "", "")
			if err != nil {
				return err
			}
		}
		if err := p.readPiModelConfig(filepath.Join(p.configHome, "models.json")); err != nil {
			return err
		}
		return p.readFiles(&p.credentials, filepath.Join(p.configHome, "auth.json"))
	}
	return nil
}

func (p *nativeCLISystemProfile) selectRoot(key, override, defaultPath string) (string, error) {
	path := override
	if path == "" {
		path = os.Getenv(key)
	}
	if path == "" {
		path = defaultPath
	}
	canonical, err := systemAccountHome(path, true)
	if err != nil {
		return "", err
	}
	p.environment[key] = path
	p.roots = append(p.roots, key, path, canonical)
	return canonical, nil
}

func (p *nativeCLISystemProfile) capture(keys ...string) {
	for _, key := range keys {
		if _, frozen := p.environment[key]; frozen {
			continue
		}
		if value := os.Getenv(key); value != "" {
			p.environment[key] = value
		}
	}
}

func (p *nativeCLISystemProfile) selectKimi(home, configuredHome string) error {
	legacy, err := p.selectRoot("KIMI_SHARE_DIR", configuredHome, filepath.Join(home, ".kimi"))
	if err != nil {
		return err
	}
	p.dataHome = legacy
	p.capture("KIMI_API_KEY", "KIMI_BASE_URL", "KIMI_CODE_BASE_URL", "KIMI_CODE_OAUTH_HOST",
		"KIMI_MODEL_NAME", "KIMI_MODEL_PROVIDER_TYPE", "KIMI_MODEL_API_KEY", "KIMI_MODEL_BASE_URL", "KIMI_MODEL_MAX_CONTEXT_SIZE",
		"KIMI_MODEL_CAPABILITIES", "KIMI_MODEL_DISPLAY_NAME", "KIMI_MODEL_MAX_OUTPUT_SIZE", "KIMI_MODEL_REASONING_KEY",
		"KIMI_MODEL_THINKING_EFFORT", "KIMI_MODEL_ADAPTIVE_THINKING", "KIMI_MODEL_MAX_COMPLETION_TOKENS")
	roots := []string{p.configHome}
	if legacy != p.configHome {
		roots = append(roots, legacy)
	}
	for _, path := range roots {
		if err := p.readProviderTOML(filepath.Join(path, "config.toml"), "providers", "api_key_env"); err != nil {
			return err
		}
		if err := p.readRefreshCredential(filepath.Join(path, "credentials", "kimi-code.json")); err != nil {
			return err
		}
	}
	return nil
}

func (p *nativeCLISystemProfile) readProviderTOML(path, table, variable string) error {
	path, data, err := readOptionalNativeCLIFile(path)
	if err != nil || data == nil {
		return err
	}
	defer clear(data)
	config := map[string]any{}
	if err := toml.Unmarshal(data, &config); err != nil {
		return status_error.RuntimeConfigurationInvalid
	}
	providers, _ := config[table].(map[string]any)
	for _, entry := range providers {
		fields, _ := entry.(map[string]any)
		if key, ok := fields[variable].(string); ok && key != "" {
			p.capture(key)
		}
	}
	// The native model setter can persist the default choice. Provider definitions
	// and endpoint credentials must stay frozen independently of that preference.
	p.settings = append(p.settings, path)
	p.recordJSON(&p.credentials, []any{providers, config["auth"]})
	return nil
}

func (p *nativeCLISystemProfile) readGeminiAuthSettings(path string) error {
	_, data, err := readOptionalNativeCLIFile(path)
	if err != nil || data == nil {
		return err
	}
	defer clear(data)
	var config struct {
		// Security contains native credential selection and gateway settings.
		Security struct {
			// Auth is the account-related part of the mutable native settings.
			Auth json.RawMessage `json:"auth"`
		} `json:"security"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return status_error.RuntimeConfigurationInvalid
	}
	defer clear(config.Security.Auth)
	if len(config.Security.Auth) > 0 {
		p.recordJSON(&p.credentials, config.Security.Auth)
	}
	return nil
}

func (p *nativeCLISystemProfile) readPiModelConfig(path string) error {
	path, data, err := readOptionalNativeCLIFile(path)
	if err != nil || data == nil {
		return err
	}
	defer clear(data)
	var config struct {
		// Providers can reference an environment variable in the apiKey field.
		Providers map[string]struct {
			// APIKey is a literal, a variable name, or a native command.
			APIKey string `json:"apiKey"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return status_error.RuntimeConfigurationInvalid
	}
	for _, provider := range config.Providers {
		if provider.APIKey != "" {
			p.capture(provider.APIKey)
		}
	}
	digest := sha256.Sum256(data)
	p.settings = append(p.settings, path, hex.EncodeToString(digest[:]))
	return nil
}

func (p *nativeCLISystemProfile) recordJSON(target *[]string, value any) {
	encoded, _ := json.Marshal(value)
	defer clear(encoded)
	digest := sha256.Sum256(encoded)
	*target = append(*target, hex.EncodeToString(digest[:]))
}

// readRefreshCredential identifies the saved grant without tying it to access
// tokens that the CLI can refresh during a native session.
func (p *nativeCLISystemProfile) readRefreshCredential(path string) error {
	path, data, err := readOptionalNativeCLIFile(path)
	if err != nil || data == nil {
		return err
	}
	defer clear(data)
	var identity struct {
		// RefreshToken identifies the local OAuth grant when reported.
		RefreshToken string `json:"refresh_token"`
		// Issuer separates credentials issued by different native login services.
		Issuer string `json:"issuer"`
	}
	p.credentials = append(p.credentials, path)
	if json.Unmarshal(data, &identity) == nil && identity.RefreshToken != "" {
		p.recordJSON(&p.credentials, identity)
	} else {
		digest := sha256.Sum256(data)
		p.credentials = append(p.credentials, hex.EncodeToString(digest[:]))
	}
	return nil
}

func (p *nativeCLISystemProfile) readFiles(target *[]string, paths ...string) error {
	for _, path := range paths {
		if path == "" {
			continue
		}
		canonical, data, err := readOptionalNativeCLIFile(path)
		if err != nil {
			return err
		}
		if data != nil {
			digest := sha256.Sum256(data)
			clear(data)
			*target = append(*target, canonical, hex.EncodeToString(digest[:]))
		}
	}
	return nil
}

func readOptionalNativeCLIFile(path string) (string, []byte, error) {
	if !filepath.IsAbs(path) {
		return "", nil, status_error.RuntimeConfigurationInvalid
	}
	canonical, err := filepath.EvalSymlinks(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, status_error.AIAccountCredentialError
	}
	data, err := readNativeAccountFile(canonical)
	return canonical, data, err
}

func (p *nativeCLISystemProfile) selectOpenCode(ctx context.Context, home, configuredHome string) error {
	var err error
	xdgConfig, err := p.selectRoot("XDG_CONFIG_HOME", "", filepath.Join(home, ".config"))
	if err != nil {
		return err
	}
	xdgData, err := p.selectRoot("XDG_DATA_HOME", "", filepath.Join(home, ".local", "share"))
	if err != nil {
		return err
	}
	p.dataHome, err = systemAccountHome(filepath.Join(xdgData, "opencode"), true)
	if err != nil {
		return err
	}
	for _, root := range []struct {
		// key is the XDG variable read by OpenCode.
		key string
		// fallback is the home-relative default for the XDG directory.
		fallback string
	}{{"XDG_CACHE_HOME", ".cache"}, {"XDG_STATE_HOME", filepath.Join(".local", "state")}} {
		if _, err := p.selectRoot(root.key, "", filepath.Join(home, root.fallback)); err != nil {
			return err
		}
	}
	configRoots := []string{filepath.Join(xdgConfig, "opencode")}
	p.configHome, err = systemAccountHome(configRoots[0], true)
	if err != nil {
		return err
	}
	if configuredHome != "" || os.Getenv("OPENCODE_CONFIG_DIR") != "" {
		p.configHome, err = p.selectRoot("OPENCODE_CONFIG_DIR", configuredHome, "")
		if err != nil {
			return err
		}
		configRoots = append(configRoots, p.configHome)
	}
	p.capture("OPENCODE_CONFIG", "OPENCODE_CONFIG_CONTENT", "OPENCODE_AUTH_CONTENT", "OPENCODE_DB", "OPENCODE_DISABLE_PROJECT_CONFIG",
		"OPENCODE_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GOOGLE_GENERATIVE_AI_API_KEY",
		"XAI_API_KEY", "OPENROUTER_API_KEY", "DEEPSEEK_API_KEY", "MISTRAL_API_KEY", "GROQ_API_KEY")
	for _, root := range configRoots {
		if err := p.readFiles(&p.settings, filepath.Join(root, "opencode.json"), filepath.Join(root, "opencode.jsonc")); err != nil {
			return err
		}
	}
	if err := p.readFiles(&p.settings, p.environment["OPENCODE_CONFIG"]); err != nil {
		return err
	}
	if err := p.readFiles(&p.credentials, filepath.Join(p.dataHome, "auth.json")); err != nil {
		return err
	}
	database := p.environment["OPENCODE_DB"]
	if database == "" {
		database = filepath.Join(p.dataHome, "opencode.db")
	}
	active, err := readOpenCodeActiveAccounts(ctx, database)
	if err != nil {
		return err
	}
	p.credentials = append(p.credentials, active...)
	return nil
}
