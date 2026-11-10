package aiaccount

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
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
	identity, err := config.identity(ctx)
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
	identity, err := config.identity(ctx)
	if err != nil {
		return agent.AccountLease{}, err
	}
	if identity.subject != request.Subject {
		return agent.AccountLease{}, status_error.RuntimeAuthRequired
	}
	lease := agent.AccountLease{ConfigHome: config.configHome, DataHome: config.dataHome, Environment: config.environment}
	lease.VerifyLogin = func(ctx context.Context) error {
		currentConfig, err := cursorSystemConfiguration(request.ConfigHome)
		if err != nil {
			return err
		}
		if currentConfig.dataHome != request.DataHome {
			return status_error.RuntimeConfigurationInvalid
		}
		current, err := currentConfig.identity(ctx)
		if err != nil {
			return err
		}
		if current.subject != request.Subject {
			return status_error.RuntimeAuthRequired
		}
		return nil
	}
	// 额度身份必须来自明确保存的数字用户标识，不能从 JWT 主体猜测。
	// Dashboard 在实际额度读取时再次核对 bearer，不阻断聊天准备。
	if identity.nativeUserID != "" && config.environment["AGENT_CLI_CREDENTIAL_STORE"] != "memory" {
		lease.CursorQuota = &agent.CursorQuotaLease{NativeUserID: identity.nativeUserID,
			ReadAccessToken: func(ctx context.Context) (string, error) {
				return readCursorAccessToken(ctx, config.authPath, config.useKeychain)
			}}
	}

	return lease, nil
}

type cursorAccountIdentity struct {
	// subject 只表达本地选择范围，不证明远端认证成功。
	subject string
	// nativeUserID 是所选本地文件明确保存的额度用户标识，不代表在线认证回执。
	nativeUserID string
}

func (c cursorSystemConfig) identity(ctx context.Context) (cursorAccountIdentity, error) {
	if err := ctx.Err(); err != nil {
		return cursorAccountIdentity{}, err
	}
	if key := c.environment["CURSOR_API_KEY"]; key != "" {
		return cursorAccountIdentity{subject: accountSubject("cursor-system-key", key)}, nil
	}
	store := c.environment["AGENT_CLI_CREDENTIAL_STORE"]
	if c.useKeychain {
		store = "macos-keychain-or-file"
	}
	facts := []string{store, c.authPath}
	// 每次仅有界读取所选本地文件；不为不可观察的 Keychain 或内存登录启动诊断进程。
	if store != "memory" {
		_, data, err := readOptionalNativeCLIFile(c.authPath)
		if err != nil && !errors.Is(err, status_error.RuntimeAuthRequired) {
			return cursorAccountIdentity{}, err
		}
		defer clear(data)
		if len(data) > 0 {
			nativeID := ""
			var credential struct {
				// AccessToken 是原生保存的令牌，仅解析其本地主体供会话隔离。
				AccessToken string `json:"accessToken"`
				// UserInfo 是原生文件可选保存的稳定用户身份。
				UserInfo struct {
					// UserID 是额度接口使用的正数用户标识。
					UserID json.Number `json:"userId"`
				} `json:"userInfo"`
			}
			if json.Unmarshal(data, &credential) == nil {
				if id, err := strconv.ParseInt(string(credential.UserInfo.UserID), 10, 32); err == nil && id > 0 {
					nativeID = strconv.FormatInt(id, 10)
					facts = append(facts, "user-id", nativeID)
				}
				parts := strings.Split(credential.AccessToken, ".")
				if len(parts) == 3 {
					claims, err := base64.RawURLEncoding.DecodeString(parts[1])
					defer clear(claims)
					var identity struct {
						// Subject 标识本地令牌主体；不校验签名或声称认证有效。
						Subject string `json:"sub"`
						// Issuer 隔离不同的令牌签发服务。
						Issuer string `json:"iss"`
					}
					if err == nil && json.Unmarshal(claims, &identity) == nil && identity.Subject != "" {
						facts = append(facts, identity.Issuer, identity.Subject)
						return cursorAccountIdentity{subject: accountSubject("cursor-system-local", facts...), nativeUserID: nativeID}, nil
					}
				}
			}
			if nativeID != "" {
				return cursorAccountIdentity{subject: accountSubject("cursor-system-local", facts...), nativeUserID: nativeID}, nil
			}
			// 无稳定主体时保守隔离原文变更，允许实际 CLI 判断是否可登录。
			facts = append(facts, accountSubject("cursor-local-credential", string(data)))
		}
	}
	return cursorAccountIdentity{subject: accountSubject("cursor-system-local", facts...)}, nil
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
