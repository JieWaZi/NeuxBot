// Package feishu 通过固定版本官方 CLI 管理应用私有共享身份与设备授权。
package feishu

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
)

// Config 是官方 CLI 的程序、共享 profile 和外部边界装配。
type Config struct {
	// Binary 是已校验固定版本的程序绝对路径。
	Binary string
	// ProfileDirectory 是应用私有共享配置位置。
	ProfileDirectory string
	// Scopes 是插件声明的最小权限集合。
	Scopes []string
	// Run 是可替换的外部进程边界；空值使用受控真实 CLI。
	Run func(context.Context, []string, string) ([]byte, error)
	// Initialize 运行官方扫码创建应用流程；空值使用固定 CLI 的流式输出。
	Initialize func(context.Context, func(authorization.Device) error) error
	// Changed 发布安全状态投影，不能携带设备代码或 token。
	Changed func(authorization.Session)
}

// driver 仅解释官方飞书 CLI 的协议，不拥有独立会话或账号状态。
type driver struct {
	// config 保存固定程序与官方配置目录。
	config Config
}

// New 构造共用授权组件，官方流程由飞书适配器提供。
func New(config Config) *authorization.Authorizer {
	if config.Run == nil {
		config.Run = commandRunner(config.Binary, config.ProfileDirectory)
	}
	if config.Initialize == nil {
		config.Initialize = initializeRunner(config.Binary, config.ProfileDirectory)
	}
	return authorization.New(authorization.Config{Driver: &driver{config: config}, Scopes: append([]string{"offline_access"}, config.Scopes...), Changed: config.Changed})
}

func (a *driver) Environment() map[string]string {
	return ManagedEnvironment(a.config.ProfileDirectory, nil)
}
func (a *driver) Binary() string { return a.config.Binary }

// Configure 通过官方非交互命令配置应用，密钥只从 stdin 交付。
func (a *driver) Configure(ctx context.Context, appID, secret string) error {
	if strings.TrimSpace(appID) == "" || secret == "" {
		return status_error.PluginNotReady
	}
	if err := os.MkdirAll(a.config.ProfileDirectory, 0o700); err != nil {
		logx.Errorf("create plugin provider profile failed: %v", err)
		return status_error.PluginStorageError
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := a.config.Run(ctx, []string{"config", "init", "--app-id", appID, "--app-secret-stdin", "--brand", "feishu", "--lang", "zh_cn"}, secret+"\n")
	if err != nil {
		return err
	}
	return nil
}

// Initialize 使用官方应用扫码流程，应用凭据只由 CLI 写入私有 profile。
func (a *driver) Initialize(ctx context.Context, ready func(authorization.Device) error) error {
	if err := os.MkdirAll(a.config.ProfileDirectory, 0o700); err != nil {
		logx.Errorf("create plugin provider profile failed: %v", err)
		return status_error.PluginStorageError
	}
	return a.config.Initialize(ctx, ready)
}

// Logout 通过官方命令撤销本机共享身份，不复制或直接编辑 token。
func (a *driver) Logout(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(a.config.ProfileDirectory, "config.json")); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		logx.Errorf("read plugin provider profile before logout failed: %v", err)
		return status_error.PluginStorageError
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := a.config.Run(ctx, []string{"auth", "logout"}, "")
	if err != nil {
		return err
	}
	return nil
}

// Status 查询官方权威身份；verify 明确要求服务端验证而非只读缓存。
func (a *driver) Status(ctx context.Context, verify bool) (authorization.Account, error) {
	result := authorization.Account{Scopes: []string{}}
	if _, err := os.Stat(filepath.Join(a.config.ProfileDirectory, "config.json")); os.IsNotExist(err) {
		return result, nil
	} else if err != nil {
		logx.Errorf("read plugin provider profile failed: %v", err)
		return result, status_error.PluginStorageError
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	args := []string{"auth", "status", "--json"}
	if verify {
		args = append(args, "--verify")
	}
	output, err := a.config.Run(ctx, args, "")
	if err != nil {
		return result, err
	}
	var status struct {
		// AppID 是官方 profile 的应用标识。
		AppID string `json:"appId"`
		// Identities 是官方区分的用户和机器人身份。
		Identities struct {
			// User 是用户委托身份的诊断。
			User struct {
				// Available 是官方判断的可用状态。
				Available bool `json:"available"`
				// Verified 是外部服务验证结果。
				Verified *bool `json:"verified"`
				// OpenID 是用户标识。
				OpenID string `json:"openId"`
				// UserName 是用户展示名。
				UserName string `json:"userName"`
				// Scope 是已授予权限。
				Scope string `json:"scope"`
			} `json:"user"`
		} `json:"identities"`
	}
	if json.Unmarshal(output, &status) != nil || status.AppID == "" {
		return result, status_error.PluginAuthorizationFailed
	}
	user := status.Identities.User
	result = authorization.Account{Configured: true, Available: user.Available, AppID: status.AppID, ID: user.OpenID, Name: user.UserName, Scopes: strings.Fields(user.Scope)}
	if result.Scopes == nil {
		result.Scopes = []string{}
	}
	if verify && (user.Verified == nil || !*user.Verified) {
		result.Available = false
	}
	granted := map[string]bool{}
	for _, scope := range result.Scopes {
		granted[scope] = true
	}
	for _, scope := range a.config.Scopes {
		if !granted[scope] {
			result.Available = false
		}
	}
	return result, nil
}

// Login 在 Core 的唯一等待中运行官方分段设备授权。
func (a *driver) Login(ctx context.Context, scopes []string, ready func(authorization.Device) error) error {
	args := []string{"auth", "login", "--no-wait", "--json"}
	if len(scopes) > 0 {
		args = append(args, "--scope", strings.Join(scopes, " "))
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	output, err := a.config.Run(bounded, args, "")
	cancel()
	if err != nil {
		return err
	}
	var device struct {
		// URL 是官方原始确认链接。
		URL string `json:"verification_url"`
		// Code 只在当前进程中用于继续设备等待。
		Code string `json:"device_code"`
		// Expires 是官方等待秒数。
		Expires int64 `json:"expires_in"`
	}
	if json.Unmarshal(output, &device) != nil || device.Code == "" || device.Expires <= 0 || device.Expires > 1800 {
		return status_error.PluginAuthorizationFailed
	}
	parsed, err := url.Parse(device.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return status_error.PluginAuthorizationFailed
	}
	if err := ready(authorization.Device{VerificationURL: device.URL, ExpiresAt: time.Now().Add(time.Duration(device.Expires) * time.Second).UnixMilli()}); err != nil {
		return err
	}
	output, err = a.config.Run(ctx, []string{"auth", "login", "--device-code", device.Code, "--json"}, "")
	if err != nil {
		return err
	}
	var result struct {
		// Event 是官方授权完成事件。
		Event string `json:"event"`
		// Missing 是官方确认仍未授予的权限。
		Missing []string `json:"missing"`
	}
	if json.Unmarshal(output, &result) != nil || result.Event != "authorization_complete" || len(result.Missing) > 0 {
		return status_error.PluginAuthorizationFailed
	}
	return nil
}

// boundedOutput 限制外部程序输出，避免异常 CLI 占满 Core 内存。
type boundedOutput struct {
	// buffer 保存有界 JSON 输出。
	buffer bytes.Buffer
}

func (b *boundedOutput) Write(data []byte) (int, error) {
	if b.buffer.Len()+len(data) > 1024*1024 {
		return 0, status_error.PluginContentLimitExceeded
	}
	return b.buffer.Write(data)
}
func commandRunner(binary, profile string) func(context.Context, []string, string) ([]byte, error) {
	return func(ctx context.Context, args []string, stdin string) ([]byte, error) {
		if binary == "" {
			return nil, status_error.PluginNotReady
		}
		if _, err := os.Stat(binary); err != nil {
			if os.IsNotExist(err) {
				return nil, status_error.PluginNotReady
			}
			logx.Errorf("inspect fixed plugin provider failed: %v", err)
			return nil, status_error.PluginStorageError
		}
		command := providerCommand(ctx, binary, profile, args)
		command.Stdin = strings.NewReader(stdin)
		command.Stderr = io.Discard
		var output boundedOutput
		command.Stdout = &output
		if err := command.Run(); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			logx.Warnf("fixed Feishu CLI command failed: operation=%s exit=%v", args[0], err)
			return nil, status_error.PluginAuthorizationFailed
		}
		return output.buffer.Bytes(), nil
	}
}

// providerCommand 为认证和首次扫码使用同一个受管环境，不启动 shell。
func providerCommand(ctx context.Context, binary, profile string, args []string) *exec.Cmd {
	command := exec.CommandContext(ctx, binary, args...)
	command.Env = make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		if !providerEnvironment(strings.SplitN(value, "=", 2)[0]) {
			command.Env = append(command.Env, value)
		}
	}
	for key, value := range ManagedEnvironment(profile, nil) {
		if value != "" {
			command.Env = append(command.Env, key+"="+value)
		}
	}

	return command
}

// initializeRunner 只提取官方 stderr 中单独一行的扫码 URL，不保留其他输出与凭据。
func initializeRunner(binary, profile string) func(context.Context, func(authorization.Device) error) error {
	return func(ctx context.Context, ready func(authorization.Device) error) error {
		if binary == "" {
			return status_error.PluginNotReady
		}
		work, cancel := context.WithCancel(ctx)
		defer cancel()
		command := providerCommand(work, binary, profile, []string{"config", "init", "--new", "--brand", "feishu", "--lang", "zh_cn"})
		command.Stdout = io.Discard
		output, err := command.StderrPipe()
		if err != nil {
			return status_error.PluginAuthorizationFailed
		}
		if err := command.Start(); err != nil {
			return status_error.PluginAuthorizationFailed
		}
		scanErr := readInitializationURL(output, ready)
		if scanErr != nil {
			cancel()
		}
		waitErr := command.Wait()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if scanErr != nil {
			return scanErr
		}
		if waitErr != nil {
			return status_error.PluginAuthorizationFailed
		}
		return nil
	}
}

func readInitializationURL(output io.Reader, ready func(authorization.Device) error) error {
	limited := &io.LimitedReader{R: output, N: 1024 * 1024}
	scanner := bufio.NewScanner(limited)
	found := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		parsed, err := url.Parse(line)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "open.feishu.cn" || parsed.User != nil || parsed.Path != "/page/cli" || parsed.Query().Get("user_code") == "" || found {
			continue
		}
		found = true
		// CLI 不输出官方到期秒数；这里展示本机十分钟等待上限，官方进程仍拥有实际期限。
		if err := ready(authorization.Device{VerificationURL: line, ExpiresAt: time.Now().Add(10 * time.Minute).UnixMilli()}); err != nil {
			return err
		}
	}
	if limited.N == 0 {
		return status_error.PluginContentLimitExceeded
	}
	if scanner.Err() != nil || !found {
		return status_error.PluginAuthorizationFailed
	}
	return nil
}
