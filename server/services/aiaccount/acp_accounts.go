package aiaccount

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
)

type acpSystemAccount struct {
	// configHome 是原生配置与会话的规范根目录。
	configHome string
	// userHome 定位配置根目录以外的原生账号存储。
	userHome string
	// fingerprint 将原始与规范系统主目录绑定到已冻结选择。
	fingerprint string
	// environment 固定原生进程的配置范围及选中凭据。
	environment map[string]string
}

func (s *Service) resolveACPSystemAccount(ctx context.Context, request agent.AccountRequest, configHome string) (agent.AccountRequest, error) {
	if request.AccountID != agent.SystemAccountID {
		return agent.AccountRequest{}, status_error.AIAccountConfigurationInvalid
	}
	if request.Provider == constants.RuntimeIDQoder && configHome == "" {
		_, variable, _ := accountconfig.QoderNativeScope(request.Executable)
		configHome = os.Getenv(variable)
	} else {
		configHome = acpSystemConfigOverride(request.Provider, configHome)
	}
	config, err := newACPSystemAccount(request, configHome)
	if err != nil {
		return agent.AccountRequest{}, err
	}
	subject, err := s.acpSystemSubject(ctx, request, &config)
	if err != nil {
		return agent.AccountRequest{}, err
	}
	request.ConfigHome, request.NativeConfigHome = config.configHome, configHome
	request.ConfigFingerprint = config.fingerprint
	request.Subject, request.AuthGeneration = subject, systemAccountGeneration
	return request, nil
}

func (s *Service) acquireACPSystemAccount(ctx context.Context, request agent.AccountRequest) (agent.AccountLease, error) {
	if request.AccountID != agent.SystemAccountID || request.Subject == "" ||
		request.AuthGeneration != systemAccountGeneration || !filepath.IsAbs(request.ConfigHome) {
		return agent.AccountLease{}, status_error.AIAccountConfigurationInvalid
	}
	recheck := func(ctx context.Context) (acpSystemAccount, error) {
		config, err := newACPSystemAccount(request, request.NativeConfigHome)
		if err != nil {
			return acpSystemAccount{}, err
		}
		if config.configHome != request.ConfigHome || config.fingerprint != request.ConfigFingerprint {
			return acpSystemAccount{}, status_error.RuntimeConfigurationInvalid
		}
		subject, err := s.acpSystemSubject(ctx, request, &config)
		if err != nil {
			return acpSystemAccount{}, err
		}
		if subject != request.Subject {
			return acpSystemAccount{}, status_error.RuntimeAuthRequired
		}
		return config, nil
	}
	config, err := recheck(ctx)
	if err != nil {
		return agent.AccountLease{}, err
	}
	return agent.AccountLease{
		ConfigHome: config.configHome, Environment: config.environment,
		VerifyLogin: func(ctx context.Context) error {
			_, err := recheck(ctx)
			return err
		},
	}, nil
}

func newACPSystemAccount(request agent.AccountRequest, nativeConfigHome string) (acpSystemAccount, error) {
	for _, argument := range request.PrefixArgs {
		option, _, _ := strings.Cut(argument, "=")
		switch option {
		case "--config-dir", "--sdk-auth-file", "--settings", "--setting-sources",
			"--cwd", "-w", "--worktree", "--add-dir":
			return acpSystemAccount{}, status_error.RuntimeConfigurationInvalid
		}
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return acpSystemAccount{}, status_error.RuntimeConfigurationInvalid
	}
	canonicalHome, err := systemAccountHome(userHome, false)
	if err != nil {
		return acpSystemAccount{}, err
	}
	scopeProvider := request.Provider
	configVariable := accountconfig.ACPSystemConfigVariable(request.Provider)
	tokenVariable := ""
	if request.Provider == constants.RuntimeIDQoder {
		scopeProvider, configVariable, tokenVariable = accountconfig.QoderNativeScope(request.Executable)
	}
	configHome, err := acpSystemConfigRoot(scopeProvider, userHome, nativeConfigHome)
	if err != nil {
		return acpSystemAccount{}, err
	}
	homeVariable := "HOME"
	if runtime.GOOS == "windows" {
		homeVariable = "USERPROFILE"
	}
	config := acpSystemAccount{
		configHome: configHome, userHome: userHome,
		fingerprint: accountSubject("native-acp-system-home", request.Provider, canonicalHome, userHome),
		environment: map[string]string{
			configVariable: nativeConfigHome,
			homeVariable:   userHome,
		},
	}
	if request.Provider == constants.RuntimeIDWorkBuddy {
		// The explicit token has higher priority than the model API key.
		token := os.Getenv("CODEBUDDY_AUTH_TOKEN")
		if token != "" {
			config.environment["CODEBUDDY_AUTH_TOKEN"] = token
		} else if key := os.Getenv("CODEBUDDY_API_KEY"); key != "" {
			config.environment["CODEBUDDY_API_KEY"] = key
		}
		region := os.Getenv("CODEBUDDY_INTERNET_ENVIRONMENT")
		switch region {
		case "", "external", "internal", "ioa":
			config.environment["CODEBUDDY_INTERNET_ENVIRONMENT"] = region
		default:
			return acpSystemAccount{}, status_error.RuntimeConfigurationInvalid
		}
		return config, nil
	}
	if token := os.Getenv(tokenVariable); token != "" {
		config.environment[tokenVariable] = token
	}
	return config, nil
}

func acpSystemConfigOverride(provider, configuredHome string) string {
	if configuredHome != "" {
		return configuredHome
	}
	return os.Getenv(accountconfig.ACPSystemConfigVariable(provider))
}

func acpSystemConfigRoot(provider, userHome, configuredHome string) (string, error) {
	if configuredHome != "" {
		return systemAccountHome(configuredHome, true)
	}
	var defaultDir string
	switch provider {
	case constants.RuntimeIDWorkBuddy:
		defaultDir = ".codebuddy"
	case constants.RuntimeIDQoder:
		defaultDir = ".qoder"
	case constants.RuntimeQoderCNProtocolID:
		defaultDir = ".qoder-cn"
	default:
		return "", status_error.RuntimeNotRegistered
	}
	return systemAccountHome(filepath.Join(userHome, defaultDir), true)
}

func (s *Service) acpSystemSubject(ctx context.Context, request agent.AccountRequest, c *acpSystemAccount) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if request.Provider == constants.RuntimeIDWorkBuddy {
		return c.workBuddySubject(request.CWD)
	}
	_, _, tokenVariable := accountconfig.QoderNativeScope(request.Executable)
	if token := c.environment[tokenVariable]; token != "" {
		// The PAT is exact launch evidence. The CLI status command can describe
		// a different saved login, so it cannot authenticate this PAT for us.
		return accountSubject(request.Provider+"-system-pat", c.configHome, token), nil
	}
	profile := nativeCLISystemProfile{}
	// 原生加密文件不由宿主解密；摘要仅隔离可观察的本地选择变更。
	// 无法区分原生刷新和换号时保守重建，不缓存在线身份回执。
	if err := profile.readFiles(&profile.credentials, filepath.Join(c.configHome, ".auth", "user"), filepath.Join(c.configHome, ".auth", "machine_id")); err != nil {
		return "", err
	}
	return accountSubject(request.Provider+"-system-local", append([]string{c.configHome, c.fingerprint}, profile.credentials...)...), nil
}

func (c acpSystemAccount) workBuddySubject(cwd string) (string, error) {
	if err := validateWorkBuddyCredentialSources(c.configHome, cwd); err != nil {
		return "", err
	}
	region := c.environment["CODEBUDDY_INTERNET_ENVIRONMENT"]
	for _, key := range []string{"CODEBUDDY_AUTH_TOKEN", "CODEBUDDY_API_KEY"} {
		if credential := c.environment[key]; credential != "" {
			return accountSubject("workbuddy-system-env", c.configHome, region, key, credential), nil
		}
	}
	// CodeBuddy Code 2.151.0 stores its default CLI OAuth account in shared
	// platform data, independently of CODEBUDDY_CONFIG_DIR. This identifies a
	// locally saved principal. It does not attest that the token is valid.
	authHome, err := systemAccountHome(filepath.Join(workBuddyPlatformData(c.userHome), "CodeBuddyExtension", "Data", "Public", "auth"), true)
	if err != nil {
		return "", err
	}
	path := filepath.Join(authHome, "Tencent-Cloud.coding-copilot.info")
	if err := checkWorkBuddyLogoutMarker(path); err != nil {
		if errors.Is(err, status_error.RuntimeAuthRequired) {
			return accountSubject("workbuddy-system-local", path, region, "logged-out"), nil
		}
		return "", err
	}
	data, err := readNativeAccountFile(path)
	if errors.Is(err, status_error.RuntimeAuthRequired) {
		return accountSubject("workbuddy-system-local", path, region, "unobserved"), nil
	}
	if err != nil {
		return "", err
	}
	defer clear(data)
	var stored struct {
		// Account contains the selected vendor subject and enterprise scope.
		Account struct {
			// UID is the provider account identifier exposed by the native client.
			UID json.RawMessage `json:"uid"`
			// EnterpriseID distinguishes identities in different enterprise scopes.
			EnterpriseID json.RawMessage `json:"enterpriseId"`
			// Type identifies the credential realm reported by the vendor.
			Type string `json:"type"`
		} `json:"account"`
		// Auth holds opaque local credential material.
		Auth struct {
			// AccessToken is checked only for local presence, including encrypted values.
			AccessToken json.RawMessage `json:"accessToken"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return "", status_error.AIAccountCredentialError
	}
	defer clear(stored.Auth.AccessToken)
	uid := nativeIdentifier(stored.Account.UID)
	if uid == "" || !workBuddyAccessTokenPresent(stored.Auth.AccessToken) {
		return accountSubject("workbuddy-system-local", path, region, accountSubject("workbuddy-native-selection", string(data))), nil
	}
	if err := checkWorkBuddyLogoutMarker(path); err != nil {
		return "", err
	}
	return accountSubject("workbuddy-system-login", path, region, stored.Account.Type, uid, nativeIdentifier(stored.Account.EnterpriseID)), nil
}

func workBuddyAccessTokenPresent(value json.RawMessage) bool {
	value = bytes.TrimSpace(value)
	// Native credentials may be plaintext strings or codec envelopes.
	return len(value) > 2 && (value[0] == '"' || value[0] == '{')
}

func workBuddyPlatformData(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support")
	case "windows":
		return filepath.Join(home, "AppData", "Local")
	default:
		return filepath.Join(home, ".local", "share")
	}
}

func checkWorkBuddyLogoutMarker(path string) error {
	_, err := os.Lstat(path + ".logged-out")
	switch {
	case err == nil:
		return status_error.RuntimeAuthRequired
	case errors.Is(err, os.ErrNotExist):
		return nil
	default:
		return status_error.AIAccountCredentialError
	}
}

func validateWorkBuddyCredentialSources(home, cwd string) error {
	if !filepath.IsAbs(cwd) {
		return status_error.RuntimeConfigurationInvalid
	}
	for _, file := range []string{
		filepath.Join(home, "settings.json"),
		filepath.Join(cwd, ".codebuddy", "settings.json"),
		filepath.Join(cwd, ".codebuddy", "settings.local.json"),
	} {
		if err := validateWorkBuddyCredentialSettings(file); err != nil {
			return err
		}
	}
	return nil
}

func validateWorkBuddyCredentialSettings(path string) error {
	data, err := readNativeAccountFile(path)
	if errors.Is(err, status_error.RuntimeAuthRequired) {
		return nil
	}
	if err != nil {
		return err
	}
	defer clear(data)
	return accountconfig.ValidateWorkBuddyCredentialSettings(data)
}

func nativeIdentifier(value json.RawMessage) string {
	var text string
	if json.Unmarshal(value, &text) == nil {
		return strings.TrimSpace(text)
	}
	var number json.Number
	if json.Unmarshal(value, &number) == nil {
		return number.String()
	}
	return ""
}
