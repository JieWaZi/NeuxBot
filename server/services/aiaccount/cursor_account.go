package aiaccount

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	agentclient "github.com/JieWaZi/acp-go/pkg/agent_next"
	cursor "github.com/JieWaZi/acp-go/pkg/agent_next/cursor"
)

// cursorSystemConfig owns the native settings, session and login locations. Cursor's
// configurable settings root does not isolate authentication.
type cursorSystemConfig struct {
	// configHome holds the settings directory selected by CURSOR_CONFIG_DIR.
	configHome string
	// dataHome holds native conversations and project state.
	dataHome string
	// authPath is the platform-specific fallback credential file.
	authPath string
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
	environment := map[string]string{
		"CURSOR_CONFIG_DIR": configHome,
		"CURSOR_DATA_DIR":   dataHome,
		"CURSOR_API_KEY":    strings.TrimSpace(os.Getenv("CURSOR_API_KEY")),
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
	return cursorSystemConfig{configHome: configHome, dataHome: dataHome, authPath: filepath.Join(authRoot, "auth.json"), environment: environment}, nil
}

func resolveCursorAccount(ctx context.Context, request agent.AccountRequest, home string) (agent.AccountRequest, error) {
	if request.AccountID != agent.SystemAccountID {
		return agent.AccountRequest{}, status_error.AIAccountConfigurationInvalid
	}
	config, err := cursorSystemConfiguration(home)
	if err != nil {
		return agent.AccountRequest{}, err
	}
	request.ConfigHome, request.AuthGeneration = config.configHome, systemAccountGeneration
	request.Subject, err = config.subject(ctx, request)
	if err != nil {
		return agent.AccountRequest{}, err
	}
	return request, nil
}

func acquireCursorAccount(ctx context.Context, request agent.AccountRequest) (agent.AccountLease, error) {
	if request.AccountID != agent.SystemAccountID || request.Subject == "" ||
		request.AuthGeneration != systemAccountGeneration || !filepath.IsAbs(request.ConfigHome) {
		return agent.AccountLease{}, status_error.AIAccountConfigurationInvalid
	}
	config, err := cursorSystemConfiguration(request.ConfigHome)
	if err != nil {
		return agent.AccountLease{}, err
	}
	readSubject := func(ctx context.Context) (string, error) {
		return config.subject(ctx, request)
	}
	subject, err := readSubject(ctx)
	if err != nil {
		return agent.AccountLease{}, err
	}
	if subject != request.Subject || config.configHome != request.ConfigHome {
		return agent.AccountLease{}, status_error.RuntimeAuthRequired
	}
	lease := agent.AccountLease{ConfigHome: config.configHome, Environment: config.environment}
	if config.environment["CURSOR_API_KEY"] == "" {
		lease.CursorQuota = bindCursorQuota(ctx, request.Subject, readSubject, func(ctx context.Context) (string, error) {
			return readCursorAccessToken(ctx, config.authPath)
		})
	}
	return lease, nil
}

func (c cursorSystemConfig) subject(ctx context.Context, request agent.AccountRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if key := c.environment["CURSOR_API_KEY"]; key != "" {
		return accountSubject("cursor-system-key-v1", c.configHome, c.dataHome, key), nil
	}
	client, err := cursor.NewClient(ctx, cursor.Options{
		Executable: request.Executable, PrefixArgs: request.PrefixArgs, WorkingDirectory: request.CWD,
		Environment: nativeAccountEnvironment(c.environment),
	}, agentclient.HostCallbacks{})
	if err != nil {
		return "", nativeAccountStatusError(ctx, constants.RuntimeIDCursor, err)
	}
	login, err := client.ReadLoginStatus(ctx)
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if closeErr := client.Close(closeCtx); closeErr != nil {
		logx.Errorf("native account status cleanup failed: provider=%s", constants.RuntimeIDCursor)
		return "", status_error.RuntimeShutdownFailed
	}
	if err != nil {
		return "", nativeAccountStatusError(ctx, constants.RuntimeIDCursor, err)
	}
	if !login.Authenticated || strings.TrimSpace(login.UserID) == "" {
		return "", status_error.RuntimeAuthRequired
	}
	return accountSubject("cursor-system-login-v1", c.configHome, c.dataHome, c.authPath, login.UserID), nil
}

// bindCursorQuota pins the observed native token only for this lease. The CLI exposes
// its stable user ID independently of the access token, so never follow a token rotation
// without a fresh lease. Both native reads must report the same user around the token read.
func bindCursorQuota(ctx context.Context, subject string, readSubject, readToken func(context.Context) (string, error)) *agent.CursorQuotaLease {
	token, err := readToken(ctx)
	if err != nil || token == "" {
		return nil
	}
	current, err := readSubject(ctx)
	if err != nil || current != subject {
		return nil
	}
	expected := sha256.Sum256([]byte(token))
	return &agent.CursorQuotaLease{ReadAccessToken: func(ctx context.Context) (string, error) {
		current, err := readSubject(ctx)
		if err != nil {
			return "", err
		}
		if current != subject {
			return "", status_error.RuntimeAuthRequired
		}
		token, err := readToken(ctx)
		if err != nil {
			return "", err
		}
		if sha256.Sum256([]byte(token)) != expected {
			return "", status_error.RuntimeAuthRequired
		}
		current, err = readSubject(ctx)
		if err != nil {
			return "", err
		}
		if current != subject {
			return "", status_error.RuntimeAuthRequired
		}
		return token, nil
	}}
}

func readCursorAccessToken(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	const maxCredentialBytes = 1 << 20
	if runtime.GOOS == "darwin" {
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
