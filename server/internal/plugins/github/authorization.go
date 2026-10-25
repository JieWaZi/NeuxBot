// Package github 通过固定官方 CLI 的私有配置与设备流程管理共享 GitHub 身份。
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
)

// Config 只装配固定程序、应用私有目录和可替换的外部进程边界。
type Config struct {
	// Binary 是打包校验过的官方程序。
	Binary string
	// ProfileDirectory 是唯一应用私有官方配置目录。
	ProfileDirectory string
	// Run 执行官方命令；输出不能直接进入日志或 HTTP。
	Run func(context.Context, []string, string, io.Writer, io.Writer) error
	// Changed 发布不包含凭据的状态变化。
	Changed func(authorization.Session)
}

// driver 不另存账号或 token，权威凭据只属于官方私有 profile。
type driver struct {
	// config 保存固定程序和官方配置边界。
	config Config
}

// New 构造公共授权组件，不读取用户机器已有的 gh 身份。
func New(config Config) *authorization.Authorizer {
	if config.Run == nil {
		config.Run = commandRunner(config.Binary, config.ProfileDirectory)
	}
	return authorization.New(authorization.Config{Driver: &driver{config: config}, Scopes: []string{"repo", "read:org", "gist"}, Changed: config.Changed})
}
func (a *driver) Binary() string { return a.config.Binary }
func (a *driver) Environment() map[string]string {
	return ManagedEnvironment(a.config.ProfileDirectory, nil)
}
func (a *driver) Configure(context.Context, string, string) error {
	return status_error.PluginForbidden
}

func (a *driver) profileExists() (bool, error) {
	info, err := os.Lstat(filepath.Join(a.config.ProfileDirectory, "hosts.yml"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		logx.Errorf("inspect private GitHub profile failed: %v", err)
		return false, status_error.PluginStorageError
	}
	if !info.Mode().IsRegular() {
		return false, status_error.PluginStorageError
	}
	return true, nil
}

// Status 只查询已存在的本应用 profile；官方 JSON 不启用 show-token。
func (a *driver) Status(ctx context.Context, _ bool) (authorization.Account, error) {
	result := authorization.Account{Configured: true, Scopes: []string{}}
	exists, err := a.profileExists()
	if err != nil || !exists {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var output limitedOutput
	if err := a.config.Run(ctx, []string{"auth", "status", "--hostname", "github.com", "--active", "--json", "hosts"}, "", &output, io.Discard); err != nil {
		return result, err
	}
	var response struct {
		// Hosts 是官方按域名返回的身份诊断。
		Hosts map[string][]struct {
			// State 是在线验证状态。
			State string `json:"state"`
			// Active 表示该官方 profile 的当前账号。
			Active bool `json:"active"`
			// Host 是官方报告的服务域。
			Host string `json:"host"`
			// Login 是当前账号公开登录名。
			Login string `json:"login"`
			// TokenSource 区分私有配置与外部覆盖来源。
			TokenSource string `json:"tokenSource"`
			// Scopes 是官方验证到的 OAuth 权限。
			Scopes string `json:"scopes"`
		} `json:"hosts"`
	}
	if json.Unmarshal(output.Bytes(), &response) != nil {
		return result, status_error.PluginAuthorizationFailed
	}
	for _, entry := range response.Hosts["github.com"] {
		if !entry.Active {
			continue
		}
		// 只允许官方文件配置，拒绝环境变量与机器上原有的系统钥匙串回退。
		if filepath.Clean(entry.TokenSource) != filepath.Join(a.config.ProfileDirectory, "hosts.yml") {
			return result, status_error.PluginAuthorizationFailed
		}
		result.ID = entry.Login
		result.Name = entry.Login
		result.Scopes = strings.FieldsFunc(entry.Scopes, func(c rune) bool { return c == ',' || c == ' ' })
		if result.Scopes == nil {
			result.Scopes = []string{}
		}
		result.Available = entry.State == "success" && entry.Host == "github.com" && entry.Login != ""
		return result, nil
	}
	return result, nil
}

// Login 在非交互管道中使用官方设备流，CLI 仅输出 URL，不会打开浏览器。
func (a *driver) Login(ctx context.Context, scopes []string, ready func(authorization.Device) error) error {
	if err := os.MkdirAll(a.config.ProfileDirectory, 0700); err != nil {
		return status_error.PluginStorageError
	}
	if err := os.Chmod(a.config.ProfileDirectory, 0700); err != nil {
		return status_error.PluginStorageError
	}
	args := []string{"auth", "login", "--hostname", "github.com", "--web", "--git-protocol", "https", "--skip-ssh-key", "--clipboard=false", "--insecure-storage"}
	if len(scopes) > 0 {
		args = append(args, "--scopes", strings.Join(scopes, ","))
	}
	output := deviceOutput{ready: ready}
	err := a.config.Run(ctx, args, "", io.Discard, &output)
	if err != nil {
		return err
	}
	if !output.reported {
		return status_error.PluginAuthorizationFailed
	}
	// 官方 CLI 写入后收紧权限，不把凭据复制到其他存储。
	for _, name := range []string{"hosts.yml", "config.yml"} {
		if err := os.Chmod(filepath.Join(a.config.ProfileDirectory, name), 0600); err != nil && !os.IsNotExist(err) {
			return status_error.PluginStorageError
		}
	}
	return nil
}

// Logout 仅删除本应用拥有的官方配置，不触及外部 gh profile 或系统钥匙串。
func (a *driver) Logout(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.RemoveAll(a.config.ProfileDirectory); err != nil {
		return status_error.PluginStorageError
	}
	return nil
}

// AuthorizeServer 把私有令牌限于精确官方 MCP 地址，阻断导入包伪装提供者窃取身份。
func (a *driver) AuthorizeServer(ctx context.Context, address string) (map[string]string, error) {
	if address != constants.GitHubMCPURL {
		return nil, status_error.PluginForbidden
	}
	exists, err := a.profileExists()
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, status_error.PluginNotReady
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var output limitedOutput
	if err := a.config.Run(ctx, []string{"auth", "token", "--hostname", "github.com"}, "", &output, io.Discard); err != nil {
		return nil, err
	}
	token := strings.TrimSpace(output.String())
	if token == "" || strings.ContainsAny(token, "\r\n\t ") {
		return nil, status_error.PluginAuthorizationFailed
	}
	return map[string]string{"Authorization": "Bearer " + token}, nil
}

// deviceOutput 只解释固定版本官方的两行公开交互输出，其余原始内容不外泄。
type deviceOutput struct {
	// pending 保存尚未完整到达的一行。
	pending string
	// size 限制整个非交互进程输出。
	size int
	// code 是待用户输入的公开一次性验证码。
	code string
	// address 是官方原始设备确认地址。
	address string
	// reported 保证一个进程只创建一次等待。
	reported bool
	// ready 把确认入口交给唯一授权状态机。
	ready func(authorization.Device) error
}

var codePattern = regexp.MustCompile(`^! First copy your one-time code: ([A-Z0-9]{4}-[A-Z0-9]{4})$`)

func (d *deviceOutput) Write(data []byte) (int, error) {
	d.size += len(data)
	if d.size > 64*1024 {
		return 0, status_error.PluginContentLimitExceeded
	}
	d.pending += string(data)
	for {
		line, rest, ok := strings.Cut(d.pending, "\n")
		if !ok {
			break
		}
		d.pending = rest
		line = strings.TrimSpace(line)
		if match := codePattern.FindStringSubmatch(line); len(match) == 2 {
			d.code = match[1]
		}
		if value, ok := strings.CutPrefix(line, "Open this URL to continue in your web browser: "); ok {
			if value != "https://github.com/login/device" {
				return 0, status_error.PluginAuthorizationFailed
			}
			d.address = value
		}
		if !d.reported && d.code != "" && d.address != "" {
			if err := d.ready(authorization.Device{VerificationURL: d.address, UserCode: d.code, ExpiresAt: time.Now().Add(15 * time.Minute).UnixMilli()}); err != nil {
				return 0, err
			}
			d.reported = true
		}
	}
	return len(data), nil
}

// limitedOutput 限制状态及令牌输出，不能用作日志缓冲。
type limitedOutput struct {
	// Buffer 保存有界官方响应。
	bytes.Buffer
}

func (b *limitedOutput) Write(data []byte) (int, error) {
	if b.Len()+len(data) > 1024*1024 {
		return 0, status_error.PluginContentLimitExceeded
	}
	return b.Buffer.Write(data)
}

func commandRunner(binary, profile string) func(context.Context, []string, string, io.Writer, io.Writer) error {
	return func(ctx context.Context, args []string, stdin string, out, stderr io.Writer) error {
		if binary == "" {
			return status_error.PluginNotReady
		}
		command := exec.CommandContext(ctx, binary, args...)
		for _, entry := range os.Environ() {
			key, _, _ := strings.Cut(entry, "=")
			if !providerEnvironment(key) && key != "NO_COLOR" {
				command.Env = append(command.Env, entry)
			}
		}
		for key, value := range ManagedEnvironment(profile, nil) {
			if value != "" {
				command.Env = append(command.Env, key+"="+value)
			}
		}
		version := exec.CommandContext(ctx, binary, "--version")
		version.Env = command.Env
		var identity limitedOutput
		version.Stdout = &identity
		version.Stderr = io.Discard
		if err := version.Run(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return status_error.PluginNotReady
		}
		if !strings.HasPrefix(identity.String(), "gh version "+constants.GitHubCLIBuildVersion+"\n") && !strings.HasPrefix(identity.String(), "gh version "+constants.GitHubCLIBuildVersion+" ") {
			return status_error.PluginNotReady
		}
		command.Stdin = strings.NewReader(stdin)
		command.Stdout = out
		command.Stderr = stderr
		if err := command.Run(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if os.IsNotExist(err) {
				return status_error.PluginNotReady
			}
			logx.Warnf("fixed GitHub CLI command failed: operation=%s exit=%v", args[0], err)
			return status_error.PluginAuthorizationFailed
		}
		return nil
	}
}

// ScopeGranted 按 GitHub 官方 OAuth 权限层级解释服务端规范化后的集合。
func (a *driver) ScopeGranted(granted []string, required string) bool {
	implications := map[string][]string{
		"repo":      {"repo:status", "repo_deployment", "public_repo", "repo:invite", "security_events", "admin:repo_hook", "write:repo_hook", "read:repo_hook"},
		"admin:org": {"write:org", "read:org"}, "write:org": {"read:org"},
		"admin:repo_hook": {"write:repo_hook", "read:repo_hook"}, "write:repo_hook": {"read:repo_hook"},
		"admin:public_key": {"write:public_key", "read:public_key"}, "write:public_key": {"read:public_key"},
		"admin:gpg_key": {"write:gpg_key", "read:gpg_key"}, "write:gpg_key": {"read:gpg_key"},
		"user": {"read:user", "user:email", "user:follow"}, "project": {"read:project"},
	}
	for _, scope := range granted {
		if scope == required || slices.Contains(implications[scope], required) {
			return true
		}
	}
	return false
}
