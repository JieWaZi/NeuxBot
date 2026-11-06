package aiaccount

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	agentclient "github.com/JieWaZi/acp-go/pkg/agentclient"
	cursor "github.com/JieWaZi/acp-go/pkg/cursor"
)

// cursorSystemConfig owns the native settings, session and login locations. Cursor's
// configurable settings root does not isolate authentication.
type cursorSystemConfig struct {
	// configHome holds the settings directory selected by CURSOR_CONFIG_DIR.
	configHome string
	// dataHome holds native conversations and project state.
	dataHome string
	// authPath is the platform-specific credential file, including the macOS fallback.
	authPath string
	// useKeychain records whether the selected store can read the macOS login Keychain.
	useKeychain bool
	// environment pins the location overrides used to resolve the selected login.
	environment map[string]string
}

func cursorSystemConfiguration(configHome string) (cursorSystemConfig, error) {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return cursorSystemConfig{}, status_error.RuntimeConfigurationInvalid
	}
	home, err := systemAccountHome(userHome, false)
	if err != nil {
		return cursorSystemConfig{}, err
	}
	xdgConfig := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configHome == "" {
		configHome = strings.TrimSpace(os.Getenv("CURSOR_CONFIG_DIR"))
	}
	if configHome == "" {
		configHome = filepath.Join(home, ".cursor")
		if xdgConfig != "" {
			configHome = filepath.Join(xdgConfig, "cursor")
		}
	}
	configHome, err = systemAccountHome(configHome, true)
	if err != nil {
		return cursorSystemConfig{}, err
	}
	dataHome := strings.TrimSpace(os.Getenv("CURSOR_DATA_DIR"))
	if dataHome == "" {
		dataHome = filepath.Join(home, ".cursor")
	}
	dataHome, err = systemAccountHome(dataHome, true)
	if err != nil {
		return cursorSystemConfig{}, err
	}
	store := os.Getenv("AGENT_CLI_CREDENTIAL_STORE")
	if store != "file" && store != "memory" {
		store = ""
	}
	environment := map[string]string{
		"CURSOR_CONFIG_DIR":          configHome,
		"CURSOR_DATA_DIR":            dataHome,
		"CURSOR_API_KEY":             strings.TrimSpace(os.Getenv("CURSOR_API_KEY")),
		"AGENT_CLI_CREDENTIAL_STORE": store,
	}
	if runtime.GOOS == "windows" {
		environment["USERPROFILE"] = home
	} else {
		environment["HOME"] = home
	}
	authRoot := filepath.Join(home, ".cursor")
	switch runtime.GOOS {
	case "windows":
		appData := strings.TrimSpace(os.Getenv("APPDATA"))
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		appData, err = systemAccountHome(appData, true)
		if err != nil {
			return cursorSystemConfig{}, err
		}
		environment["APPDATA"] = appData
		authRoot = filepath.Join(appData, "Cursor")
	case "darwin":
		// The CLI selects the login Keychain or ~/.cursor/auth.json on macOS.
	default:
		if xdgConfig == "" {
			xdgConfig = filepath.Join(home, ".config")
		}
		xdgConfig, err = systemAccountHome(xdgConfig, true)
		if err != nil {
			return cursorSystemConfig{}, err
		}
		environment["XDG_CONFIG_HOME"] = xdgConfig
		authRoot = filepath.Join(xdgConfig, "cursor")
	}
	authRoot, err = systemAccountHome(authRoot, true)
	if err != nil {
		return cursorSystemConfig{}, err
	}
	return cursorSystemConfig{
		configHome: configHome, dataHome: dataHome, authPath: filepath.Join(authRoot, "auth.json"),
		useKeychain: runtime.GOOS == "darwin" && store == "", environment: environment,
	}, nil
}

func resolveCursorAccount(ctx context.Context, request agent.AccountRequest, home string) (agent.AccountRequest, error) {
	if request.AccountID != agent.SystemAccountID {
		return agent.AccountRequest{}, status_error.AIAccountConfigurationInvalid
	}
	config, err := cursorSystemConfiguration(home)
	if err != nil {
		return agent.AccountRequest{}, err
	}
	identity, err := config.identity(ctx, request)
	if err != nil {
		return agent.AccountRequest{}, err
	}
	request.ConfigHome, request.DataHome = config.configHome, config.dataHome
	request.Subject, request.AuthGeneration = identity.subject, systemAccountGeneration
	return request, nil
}

func acquireCursorAccount(ctx context.Context, request agent.AccountRequest) (agent.AccountLease, error) {
	if request.AccountID != agent.SystemAccountID || request.Subject == "" ||
		request.AuthGeneration != systemAccountGeneration || !filepath.IsAbs(request.ConfigHome) || !filepath.IsAbs(request.DataHome) {
		return agent.AccountLease{}, status_error.AIAccountConfigurationInvalid
	}
	config, err := cursorSystemConfiguration(request.ConfigHome)
	if err != nil {
		return agent.AccountLease{}, err
	}
	if config.configHome != request.ConfigHome || config.dataHome != request.DataHome {
		return agent.AccountLease{}, status_error.RuntimeConfigurationInvalid
	}
	identity, err := config.identity(ctx, request)
	if err != nil {
		return agent.AccountLease{}, err
	}
	if identity.subject != request.Subject {
		return agent.AccountLease{}, status_error.RuntimeAuthRequired
	}
	lease := agent.AccountLease{ConfigHome: config.configHome, DataHome: config.dataHome, Environment: config.environment}
	if identity.nativeUserID != "" {
		lease.VerifyLogin = func(ctx context.Context) error {
			current, err := config.identity(ctx, request)
			if err != nil {
				return err
			}
			if current.subject != request.Subject {
				return status_error.RuntimeAuthRequired
			}
			return nil
		}
	}
	if identity.nativeUserID != "" && config.environment["AGENT_CLI_CREDENTIAL_STORE"] != "memory" {
		lease.CursorQuota = &agent.CursorQuotaLease{
			NativeUserID: identity.nativeUserID,
			ReadAccessToken: func(ctx context.Context) (string, error) {
				return readCursorAccessToken(ctx, config.authPath, config.useKeychain)
			},
		}
	}
	return lease, nil
}

type cursorAccountIdentity struct {
	// subject is the stable identity used by durable native bindings.
	subject string
	// nativeUserID is the provider identity independently verified for quota reads.
	nativeUserID string
}

func (c cursorSystemConfig) identity(ctx context.Context, request agent.AccountRequest) (cursorAccountIdentity, error) {
	if err := ctx.Err(); err != nil {
		return cursorAccountIdentity{}, err
	}
	if key := c.environment["CURSOR_API_KEY"]; key != "" {
		return cursorAccountIdentity{subject: accountSubject("cursor-system-key-v2", key)}, nil
	}
	client, err := cursor.NewClient(ctx, cursor.Options{
		Executable: request.Executable, PrefixArgs: request.PrefixArgs, WorkingDirectory: request.CWD,
		Environment: nativeAccountEnvironment(c.environment),
	}, agentclient.HostCallbacks{})
	if err != nil {
		return cursorAccountIdentity{}, nativeAccountStatusError(ctx, constants.RuntimeIDCursor, err)
	}
	login, err := client.ReadLoginStatus(ctx)
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if closeErr := client.Close(closeCtx); closeErr != nil {
		logx.Errorf("native account status cleanup failed: provider=%s", constants.RuntimeIDCursor)
		return cursorAccountIdentity{}, status_error.RuntimeShutdownFailed
	}
	if err != nil {
		return cursorAccountIdentity{}, nativeAccountStatusError(ctx, constants.RuntimeIDCursor, err)
	}
	if !login.Authenticated || login.UserID == "" {
		return cursorAccountIdentity{}, status_error.RuntimeAuthRequired
	}
	store := "file"
	if c.useKeychain {
		store = "macos-keychain-or-file"
	}
	return cursorAccountIdentity{
		subject:      accountSubject("cursor-system-login-v2", store, c.authPath, login.UserID),
		nativeUserID: login.UserID,
	}, nil
}

func readCursorAccessToken(ctx context.Context, path string, useKeychain bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	const maxCredentialBytes = 1 << 20
	if useKeychain {
		query, cancel := context.WithTimeout(ctx, 2*time.Second)
		data, err := exec.CommandContext(query, "/usr/bin/security", "find-generic-password", "-a", "cursor-user", "-s", "cursor-access-token", "-w").Output()
		cancel()
		defer clear(data)
		if err == nil && len(data) <= maxCredentialBytes {
			if token := strings.TrimSpace(string(data)); token != "" && !strings.ContainsAny(token, "\r\n") {
				return token, nil
			}
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
	}
	data, err := readNativeAccountFile(path)
	if err != nil {
		return "", err
	}
	defer clear(data)
	var auth struct {
		// AccessToken is the bearer token stored by the native CLI.
		AccessToken string `json:"accessToken"`
	}
	if json.Unmarshal(data, &auth) != nil || strings.ContainsAny(auth.AccessToken, "\r\n") {
		return "", status_error.AIAccountCredentialError
	}
	token := strings.TrimSpace(auth.AccessToken)
	if token == "" {
		return "", status_error.RuntimeAuthRequired
	}
	return token, nil
}
