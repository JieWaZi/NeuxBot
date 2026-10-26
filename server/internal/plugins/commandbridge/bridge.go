// Package commandbridge 执行插件自带授权协议，宿主不解析供应商输出。
package commandbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

// Config 固定桥接入口与应用拥有的身份目录。
type Config struct {
	// Command 是已经校验的绝对程序路径。
	Command string
	// Args 是已固定的程序参数。
	Args []string
	// Directory 是不可变包根。
	Directory string
	// DataDirectory 是本插件私有身份目录。
	DataDirectory string
	// PluginID 是宿主签发的稳定身份。
	PluginID string
	// Scopes 是本插件声明的最小范围。
	Scopes []string
	// Environment 是受信程序资产，不来自会话响应。
	Environment []string
}

// Component 是本轮已经获准的稳定组件与目标。
type Component struct {
	// ComponentID 是宿主稳定授权身份。
	ComponentID string `json:"componentId"`
	// Key 是原包组件键。
	Key string `json:"key"`
	// Target 是skill或mcp交付类型。
	Target string `json:"target"`
	// URL 是已获准的原始MCP地址。
	URL string `json:"url,omitempty"`
}

// Binding 的秘密仅在受管执行通道使用。
type Binding struct {
	// ComponentID 限定秘密可交付的组件。
	ComponentID string `json:"componentId"`
	// Environment 是该组件专属环境。
	Environment map[string]string `json:"environment,omitempty"`
	// Headers 是该组件专属请求头。
	Headers map[string]string `json:"headers,omitempty"`
}

// Driver 用JSON-RPC协议适配既有授权租约与状态机。
type Driver struct {
	// config 固定入口、私有身份及受信资产。
	config Config
}

var environmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// New 不启动进程；调用沿显式Context管理。
func New(config Config) *Driver { return &Driver{config: config} }

// Initialize 由同一个auth/start桥接负责多阶段初始化。
func (d *Driver) Initialize(context.Context, func(authorization.Device) error) error { return nil }

// Logout 调用包内协议清理受管身份。
func (d *Driver) Logout(ctx context.Context) error {
	_, err := d.call(ctx, constants.PluginAuthLogoutMethod, "", nil, nil, nil)
	return err
}

// Status 以桥接真实核验结果返回公开身份摘要。
func (d *Driver) Status(ctx context.Context, _ bool) (authorization.Account, error) {
	raw, err := d.call(ctx, constants.PluginAuthStatusMethod, "", nil, nil, nil)
	result := struct {
		// State 是协议核验状态。
		State string `json:"state"`
		// Account 是公开身份摘要。
		Account authorization.Account `json:"account"`
	}{}
	if err != nil {
		return result.Account, err
	}
	if json.Unmarshal(raw, &result) != nil {
		return result.Account, status_error.PluginAuthorizationFailed
	}
	if result.State != "authorized" {
		result.Account.Available = false
	}
	if result.Account.Scopes == nil {
		result.Account.Scopes = []string{}
	}
	return result.Account, nil
}

// Login 兼容既有授权驱动接口，正式流程使用宿主会话身份。
func (d *Driver) Login(ctx context.Context, scopes []string, report func(authorization.Device) error) error {
	return d.LoginSession(ctx, "", scopes, report)
}

// LoginSession 将宿主签发的会话身份传给桥接，拒绝其他会话的通知。
func (d *Driver) LoginSession(ctx context.Context, id string, scopes []string, report func(authorization.Device) error) error {
	_, err := d.call(ctx, constants.PluginAuthStartMethod, id, scopes, nil, report)
	return err
}

// Resolve 只接受本轮获准组件的绑定，不允许修改目标地址与Runtime身份。
func (d *Driver) Resolve(ctx context.Context, components []Component) ([]Binding, error) {
	raw, err := d.call(ctx, constants.PluginAuthResolveMethod, "", nil, components, nil)
	if err != nil {
		return nil, err
	}
	result := struct {
		// Bindings 是组件范围内的秘密投射。
		Bindings []Binding `json:"bindings"`
	}{}
	if json.Unmarshal(raw, &result) != nil {
		return nil, status_error.PluginAuthorizationFailed
	}
	seen := map[string]bool{}
	for _, binding := range result.Bindings {
		index := slices.IndexFunc(components, func(c Component) bool { return c.ComponentID == binding.ComponentID })
		if index < 0 || seen[binding.ComponentID] {
			return nil, status_error.PluginForbidden
		}
		seen[binding.ComponentID] = true
		if len(binding.Headers) > 0 && (components[index].Target != "mcp" || components[index].URL == "") {
			return nil, status_error.PluginForbidden
		}
		for key, value := range binding.Environment {
			if strings.EqualFold(key, "PATH") && !d.allowedPath(value) {
				return nil, status_error.PluginForbidden
			}
			upper := strings.ToUpper(key)
			if len(accountconfig.Environment([]string{key + "=probe"})) == 0 || !environmentNamePattern.MatchString(key) || upper == "USERPROFILE" || upper == "APPDATA" || upper == "LOCALAPPDATA" || upper == "PWD" || upper == "OLDPWD" || upper == "SHELL" || upper == "ENV" || upper == "BASH_ENV" || upper == "ZDOTDIR" || strings.HasPrefix(upper, "LD_") || strings.HasPrefix(upper, "DYLD_") || strings.HasPrefix(upper, "PYTHON") || strings.HasPrefix(upper, "NEUXALLY_") || upper == "HOME" || strings.HasPrefix(upper, "ALLY") || strings.HasPrefix(upper, "CODEX") || strings.HasPrefix(upper, "CLAUDE") || strings.HasPrefix(upper, "NODE_") {
				return nil, status_error.PluginForbidden
			}
			if (strings.HasSuffix(upper, "_DIR") || strings.HasSuffix(upper, "_DIRECTORY") || strings.HasSuffix(upper, "_FILE")) && value != "" {
				if !d.privatePath(value) {
					return nil, status_error.PluginForbidden
				}
			}
		}
		for key, value := range binding.Headers {
			if key == "" || strings.ContainsAny(key, " ()<>@,;:\"/[]?={}\t\r\n") || strings.ContainsAny(value, "\r\n\x00") {
				return nil, status_error.PluginForbidden
			}
		}
	}
	return result.Bindings, nil
}

// frame 是进程内的协议消息，原文从不记录到日志或聊天。
type frame struct {
	// JSONRPC 是消息版本。
	JSONRPC string `json:"jsonrpc"`
	// ID 对应本次请求。
	ID int `json:"id,omitempty"`
	// Method 是允许的通知方法。
	Method string `json:"method,omitempty"`
	// Params 是通知参数。
	Params json.RawMessage `json:"params,omitempty"`
	// Result 是私密协议结果。
	Result json.RawMessage `json:"result,omitempty"`
	// Error 只用于判断失败，不向外回传供应商正文。
	Error json.RawMessage `json:"error,omitempty"`
}

func (d *Driver) call(ctx context.Context, method, id string, scopes []string, components []Component, report func(authorization.Device) error) (json.RawMessage, error) {
	if err := os.MkdirAll(d.config.DataDirectory, 0700); err != nil {
		return nil, status_error.PluginStorageError
	}
	work, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	command := exec.CommandContext(work, d.config.Command, d.config.Args...)
	configureProcessGroup(command)
	command.Cancel = func() error { terminateProcessGroup(command); return nil }
	command.Dir = d.config.Directory
	command.Env = accountconfig.Environment(d.config.Environment)
	for index := len(command.Env) - 1; index >= 0; index-- {
		key, _, _ := strings.Cut(command.Env[index], "=")
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "GH_") || strings.HasPrefix(upper, "GITHUB_") || strings.HasPrefix(upper, "LARKSUITE_CLI_") || strings.HasPrefix(upper, "ALLY") {
			command.Env = append(command.Env[:index], command.Env[index+1:]...)
		}
	}
	command.Stderr = io.Discard
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, status_error.PluginAuthorizationFailed
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, status_error.PluginAuthorizationFailed
	}
	if command.Start() != nil {
		return nil, status_error.PluginNotReady
	}
	// EOF与终止信号先让桥接清理供应商子进程；超时后回收整个进程组。
	defer func() {
		_ = stdin.Close()
		terminateProcessGroup(command)
		finished := make(chan struct{})
		go func() { _ = command.Wait(); close(finished) }()
		select {
		case <-finished:
		case <-time.After(500 * time.Millisecond):
		}
		killProcessGroup(command)
		cancel()
		_ = stdout.Close()
		<-finished
	}()
	if id == "" {
		id = util.New()
	}
	if scopes == nil {
		scopes = slices.Clone(d.config.Scopes)
		if scopes == nil {
			scopes = []string{}
		}
	}
	params := map[string]any{"sessionId": id, "pluginId": d.config.PluginID, "dataDirectory": d.config.DataDirectory, "scopes": scopes, "components": components}
	write := func(requestID int, method string) error {
		return json.NewEncoder(stdin).Encode(map[string]any{"jsonrpc": "2.0", "id": requestID, "method": method, "params": params})
	}
	if write(1, method) != nil {
		return nil, status_error.PluginAuthorizationFailed
	}
	messages := make(chan frame, 8)
	done := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		defer close(done)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			var message frame
			if json.Unmarshal(scanner.Bytes(), &message) != nil || message.JSONRPC != "2.0" {
				return
			}
			select {
			case messages <- message:
			case <-work.Done():
				return
			}
		}
	}()
	defer func() {
		// 子进程可能仍持有stdout，先关闭本端读取再等待扫描器。
		cancel()
		_ = stdout.Close()
		workers.Wait()
	}()
	wait := 30 * time.Second
	if method == constants.PluginAuthStartMethod {
		wait = constants.PluginAuthPreparationTimeoutSeconds * time.Second
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = write(2, constants.PluginAuthCancelMethod)
			// 桥接先有机会取消上游，随后进程组由受管Context回收。
			deadline := time.NewTimer(2 * time.Second)
			select {
			case <-done:
			case <-deadline.C:
			}
			deadline.Stop()
			return nil, status_error.OperationCanceled
		case <-timer.C:
			return nil, context.DeadlineExceeded
		case <-done:
			select {
			case message := <-messages:
				if message.ID == 1 && len(message.Error) == 0 && method != constants.PluginAuthStartMethod {
					return message.Result, nil
				}
			default:
			}
			return nil, status_error.PluginAuthorizationFailed
		case message := <-messages:
			if message.ID == 1 {
				if len(message.Error) > 0 {
					return nil, status_error.PluginAuthorizationFailed
				}
				if method != constants.PluginAuthStartMethod {
					return message.Result, nil
				}
				continue
			}
			if method != constants.PluginAuthStartMethod {
				return nil, status_error.PluginAuthorizationFailed
			}
			var notice struct {
				// SessionID 绑定宿主当前会话。
				SessionID string `json:"sessionId"`
				// ChallengeID 唯一标识一次交互。
				ChallengeID string `json:"challengeId"`
				// Actions 是明确允许的临时动作。
				Actions []authorization.Action `json:"actions"`
				// Notice 保留插件声明的可选本地化提醒。
				Notice json.RawMessage `json:"notice"`
				// ExpiresAt 是官方期限。
				ExpiresAt int64 `json:"expiresAt"`
				// DeadlineAt 是本机等待上限。
				DeadlineAt int64 `json:"deadlineAt"`
				// State 是实际授权阶段。
				State string `json:"state"`
			}
			if json.Unmarshal(message.Params, &notice) != nil || notice.SessionID != id {
				return nil, status_error.PluginForbidden
			}
			switch message.Method {
			case constants.PluginAuthChallengeNotification:
				reminder, err := decodeNotice(notice.Notice)
				if err != nil {
					return nil, err
				}
				if notice.ChallengeID == "" || len(notice.Actions) == 0 {
					return nil, status_error.PluginAuthorizationFailed
				}
				for _, action := range notice.Actions {
					if (action.Type == "open-url" && !safeURL(action.URL)) || (action.Type == "show-qr" && !safeQRValue(action.Value)) || (action.Type == "copy-code" && action.Value == "") || (action.Type != "open-url" && action.Type != "show-qr" && action.Type != "copy-code") {
						return nil, status_error.PluginAuthorizationFailed
					}
				}
				expiry := notice.ExpiresAt
				if expiry == 0 {
					expiry = notice.DeadlineAt
				}
				duration := time.Until(time.UnixMilli(expiry))
				if duration <= 0 || duration > 30*time.Minute {
					return nil, status_error.PluginAuthorizationFailed
				}
				timer.Reset(duration)
				if report != nil {
					if err := report(authorization.Device{ChallengeID: notice.ChallengeID, Actions: notice.Actions, Notice: reminder, ExpiresAt: notice.ExpiresAt, DeadlineAt: notice.DeadlineAt}); err != nil {
						return nil, err
					}
				}
			case constants.PluginAuthStateNotification:
				switch notice.State {
				case "authorized":
					return nil, nil
				case "expired":
					return nil, context.DeadlineExceeded
				case "cancelled":
					return nil, context.Canceled
				case "failed":
					return nil, status_error.PluginAuthorizationFailed
				case "pending":
				default:
					return nil, status_error.PluginAuthorizationFailed
				}
			default:
				return nil, status_error.PluginAuthorizationFailed
			}
		}
	}
}

func safeURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil && !strings.ContainsAny(value, "\x00\r\n")
}

// safeQRValue 保留原始扫码载荷，仅内联 PNG 按受限图片验证，避免把官方原图再次编码成二维码。
func safeQRValue(value string) bool {
	if !strings.HasPrefix(strings.ToLower(value), "data:") {
		return value != ""
	}
	encoded, ok := strings.CutPrefix(value, constants.PluginAuthQRPNGPrefix)
	if !ok || len(encoded) > base64.StdEncoding.EncodedLen(constants.PluginAuthQRPNGMaxBytes) {
		return false
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) > constants.PluginAuthQRPNGMaxBytes || base64.StdEncoding.EncodeToString(data) != encoded {
		return false
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > constants.PluginAuthQRPNGMaxDimension || config.Height > constants.PluginAuthQRPNGMaxDimension {
		return false
	}
	// 完整解码检查像素数据及校验和；先限定尺寸，防止压缩图片放大内存消耗。
	reader := bytes.NewReader(data)
	_, err = png.Decode(reader)
	return err == nil && reader.Len() == 0
}

// ResolveEntry 只允许包内程序或受信Node执行固定包内脚本。
func ResolveEntry(root, command string, args []string) (string, []string, error) {
	result := slices.Clone(args)
	if command == "node" {
		if len(args) == 0 {
			return "", nil, status_error.PluginManifestInvalid
		}
		node, err := exec.LookPath("node")
		if err != nil {
			return "", nil, status_error.PluginNotReady
		}
		path, err := filepath.EvalSymlinks(filepath.Join(root, args[0]))
		if err != nil {
			return "", nil, status_error.PluginManifestInvalid
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", nil, status_error.PluginForbidden
		}
		result[0] = path
		return node, result, nil
	}
	path, err := filepath.EvalSymlinks(filepath.Join(root, command))
	if err != nil {
		return "", nil, status_error.PluginManifestInvalid
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", nil, status_error.PluginForbidden
	}
	return path, result, nil
}

func (d *Driver) allowedPath(value string) bool {
	trusted := []string{}
	for _, entry := range d.config.Environment {
		key, val, _ := strings.Cut(entry, "=")
		if key == "PATH" {
			trusted = filepath.SplitList(val)
		}
	}
	for _, path := range filepath.SplitList(value) {
		if slices.Contains(trusted, path) {
			continue
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return false
		}
		relative, err := filepath.Rel(d.config.DataDirectory, resolved)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return false
		}
	}
	return true
}

// MergePath 合并已接纳插件的程序目录，当前桥接仍只能追加自身私有目录与原受信 PATH。
func (d *Driver) MergePath(current, incoming string) (string, error) {
	if !d.allowedPath(incoming) {
		return "", status_error.PluginForbidden
	}
	trusted := []string{}
	for _, entry := range d.config.Environment {
		if key, value, _ := strings.Cut(entry, "="); key == "PATH" {
			trusted = filepath.SplitList(value)
		}
	}
	private, inherited := []string{}, []string{}
	seen := map[string]bool{}
	for _, directory := range append(filepath.SplitList(current), filepath.SplitList(incoming)...) {
		if seen[directory] {
			continue
		}
		seen[directory] = true
		if slices.Contains(trusted, directory) {
			inherited = append(inherited, directory)
		} else {
			private = append(private, directory)
		}
	}
	// 每个插件的受管程序都优先于系统同名程序；不能把后续插件目录追加到继承 PATH 之后。
	return strings.Join(append(private, inherited...), string(os.PathListSeparator)), nil
}

func (d *Driver) privatePath(value string) bool {
	if !filepath.IsAbs(value) {
		return false
	}
	base, err := filepath.EvalSymlinks(d.config.DataDirectory)
	if err != nil {
		return false
	}
	actual, err := filepath.EvalSymlinks(value)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(base, actual)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// decodeNotice 只接纳有限的纯文本双语提醒，不接受展示样式或供应商专属分支。
func decodeNotice(raw json.RawMessage) (*authorization.Notice, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var object map[string]json.RawMessage
	if !utf8.Valid(raw) || json.Unmarshal(raw, &object) != nil || len(object) != 1 || len(object["messages"]) == 0 {
		return nil, status_error.PluginAuthorizationFailed
	}
	var messages map[string]string
	if json.Unmarshal(object["messages"], &messages) != nil || len(messages) == 0 || len(messages) > 2 {
		return nil, status_error.PluginAuthorizationFailed
	}
	copied := make(map[string]string, len(messages))
	for locale, text := range messages {
		if (locale != constants.PluginLocaleEnglish && locale != constants.PluginLocaleChinese) || strings.TrimSpace(text) == "" || len(text) > 2048 || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
			return nil, status_error.PluginAuthorizationFailed
		}
		copied[locale] = text
	}
	return &authorization.Notice{Messages: copied}, nil
}
