package native

import (
	"context"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	"github.com/JieWaZi/acp-go/pkg/nativeacp"
	qoderadapter "github.com/JieWaZi/acp-go/pkg/qoder"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// Config 描述调用方已准备的原生适配器启动配置。
type Config struct {
	// RuntimeID 是原生运行时标识。
	RuntimeID string
	// Executable 是可执行文件路径。
	Executable string
	// PrefixArgs 是启动命令的前置参数。
	PrefixArgs []string
	// Environment 是启动进程的环境变量。
	Environment []string
	// WorkingDirectory 是工作目录。
	WorkingDirectory string
	// StateDirectory 是运行时状态目录。
	StateDirectory string
	// StateRootDirectory 是宿主拥有的原生状态根，具体子目录由技术声明选择。
	StateRootDirectory string
	// PermissionMode 是请求的原生权限模式。
	PermissionMode string
	// StateDirectoryForWorkspace 根据工作区返回 Cursor 状态目录。
	StateDirectoryForWorkspace func(string) string
	// Logger 由宿主注入并交给原生适配器，当前工厂不创建或选择日志输出。
	Logger *slog.Logger
	// DefaultModeRequestUserInput 控制 Codex 默认模式的用户输入请求。
	DefaultModeRequestUserInput bool
	// CodexConfiguration 是只对本次进程生效的原生点路径覆盖。
	CodexConfiguration map[string]string
	// ClaudeSettingsPath 是本次进程使用的受管设置文件。
	ClaudeSettingsPath string
	// NativePluginDirectories 是本次进程加载的固定原生插件目录。
	NativePluginDirectories []string
}

// adapt 保留构造器返回的资源所有者，避免空指针转换为非空接口。
func adapt[T interface {
	sdk.Agent
	comparable
}](agent T, err error) (sdk.Agent, error) {
	var zero T
	if agent == zero {
		return nil, err
	}
	return agent, err
}

func qoder(ctx context.Context, c Config) (sdk.Agent, error) {
	return adapt(nativeacp.NewAgent(ctx, nativeacp.Config{Logger: c.Logger, Command: c.Executable, Args: append(append([]string(nil), c.PrefixArgs...), "--acp"), VersionArgs: append(append([]string(nil), c.PrefixArgs...), "--version"), Environment: c.Environment, WorkingDirectory: c.WorkingDirectory, RuntimeName: c.RuntimeID, PromptFIFO: true, SessionAdapter: qoderadapter.NewSessionAdapter()}))
}
func environmentValue(env []string, key string) string {
	var v string
	for _, e := range env {
		k, value, ok := strings.Cut(e, "=")
		if ok && k == key {
			v = value
		}
	}
	return v
}

// New 根据运行时标识构造公开 ACP 适配器。
func New(ctx context.Context, c Config) (sdk.Agent, error) {
	descriptor, ok := descriptors[c.RuntimeID]
	if !ok {
		return nil, &contract.Fault{Code: contract.FaultUnsupported, Stage: "configure", Message: "unsupported native runtime: " + c.RuntimeID}
	}
	c.PrefixArgs = append([]string(nil), c.PrefixArgs...)
	c.Environment = slices.Clone(c.Environment)
	c.CodexConfiguration = maps.Clone(c.CodexConfiguration)
	c.NativePluginDirectories = slices.Clone(c.NativePluginDirectories)
	if c.StateDirectory == "" {
		if descriptor.StateDirectoryName != "" && c.StateRootDirectory != "" {
			c.StateDirectory = filepath.Join(c.StateRootDirectory, descriptor.StateDirectoryName)
		} else if descriptor.StateDirectoryEnvironmentVariable != "" {
			c.StateDirectory = environmentValue(c.Environment, descriptor.StateDirectoryEnvironmentVariable)
		}
	}
	if c.StateDirectoryForWorkspace == nil && descriptor.ForkStateDirectoryName != "" {
		c.StateDirectoryForWorkspace = func(cwd string) string {
			return filepath.Join(filepath.Dir(cwd), descriptor.ForkStateDirectoryName)
		}
	}
	switch c.RuntimeID {
	case "codex":
		keys := make([]string, 0, len(c.CodexConfiguration))
		for key := range c.CodexConfiguration {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			c.PrefixArgs = append(c.PrefixArgs, "-c", key+"="+c.CodexConfiguration[key])
		}
	case "claude":
		if c.ClaudeSettingsPath != "" {
			c.PrefixArgs = append(c.PrefixArgs, "--settings", c.ClaudeSettingsPath)
		}
		for _, directory := range c.NativePluginDirectories {
			c.PrefixArgs = append(c.PrefixArgs, "--plugin-dir", directory)
		}
	}
	return descriptor.newAgent(ctx, c)
}
