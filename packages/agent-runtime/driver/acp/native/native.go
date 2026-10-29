package native

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	"github.com/JieWaZi/acp-go/pkg/claude"
	"github.com/JieWaZi/acp-go/pkg/codex"
	"github.com/JieWaZi/acp-go/pkg/cursor"
	"github.com/JieWaZi/acp-go/pkg/gemini"
	"github.com/JieWaZi/acp-go/pkg/grok"
	"github.com/JieWaZi/acp-go/pkg/kimi"
	"github.com/JieWaZi/acp-go/pkg/nativeacp"
	"github.com/JieWaZi/acp-go/pkg/opencode"
	"github.com/JieWaZi/acp-go/pkg/pi"
	"github.com/JieWaZi/acp-go/pkg/workbuddy"
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
	// PermissionMode 是请求的原生权限模式。
	PermissionMode string
	// StateDirectoryForWorkspace 根据工作区返回 Cursor 状态目录。
	StateDirectoryForWorkspace func(string) string
	// Logger 接收适配器日志。
	Logger *slog.Logger
	// DefaultModeRequestUserInput 控制 Codex 默认模式的用户输入请求。
	DefaultModeRequestUserInput bool
}
type factory func(context.Context, Config) (sdk.Agent, error)

var factories = map[string]factory{
	"codex": func(ctx context.Context, c Config) (sdk.Agent, error) {
		return adapt(codex.NewAgent(ctx, codex.Config{Logger: c.Logger, CodexPath: c.Executable, PrefixArgs: c.PrefixArgs, Environment: c.Environment, DefaultModeRequestUserInput: c.DefaultModeRequestUserInput}))
	},
	"claude": func(ctx context.Context, c Config) (sdk.Agent, error) {
		return adapt(claude.NewAgent(ctx, claude.Config{Logger: c.Logger, ClaudePath: c.Executable, PrefixArgs: c.PrefixArgs, Environment: c.Environment}))
	},
	"kimi": func(ctx context.Context, c Config) (sdk.Agent, error) {
		return adapt(kimi.NewAgent(ctx, kimi.Config{Logger: c.Logger, KimiPath: c.Executable, PrefixArgs: c.PrefixArgs, Environment: c.Environment, WorkingDirectory: c.WorkingDirectory, StateDirectory: c.StateDirectory, PermissionMode: c.PermissionMode}))
	},
	"cursor": func(ctx context.Context, c Config) (sdk.Agent, error) {
		return adapt(cursor.NewAgent(ctx, cursor.Config{Logger: c.Logger, CursorPath: c.Executable, PrefixArgs: c.PrefixArgs, Environment: c.Environment, WorkingDirectory: c.WorkingDirectory, StateDirectory: c.StateDirectory, StateDirectoryForWorkspace: c.StateDirectoryForWorkspace, PermissionMode: c.PermissionMode}))
	},
	"pi": func(ctx context.Context, c Config) (sdk.Agent, error) {
		return adapt(pi.NewAgent(ctx, pi.Config{Logger: c.Logger, PiPath: c.Executable, PrefixArgs: c.PrefixArgs, Environment: c.Environment, WorkingDirectory: c.WorkingDirectory, PermissionMode: c.PermissionMode, MCPModulePath: environmentValue(c.Environment, "PI_MCP_MODULE_PATH")}))
	},
	"opencode": func(ctx context.Context, c Config) (sdk.Agent, error) {
		return adapt(opencode.NewAgent(ctx, opencode.Config{Logger: c.Logger, OpenCodePath: c.Executable, PrefixArgs: c.PrefixArgs, Environment: c.Environment, WorkingDirectory: c.WorkingDirectory, PermissionMode: c.PermissionMode}))
	},
	"gemini": func(ctx context.Context, c Config) (sdk.Agent, error) {
		return adapt(gemini.NewAgent(ctx, gemini.Config{Logger: c.Logger, GeminiPath: c.Executable, PrefixArgs: c.PrefixArgs, Environment: c.Environment, WorkingDirectory: c.WorkingDirectory, StateDirectory: c.StateDirectory, PermissionMode: c.PermissionMode}))
	},
	"grok": func(ctx context.Context, c Config) (sdk.Agent, error) {
		return adapt(grok.NewAgent(ctx, grok.Config{Logger: c.Logger, GrokPath: c.Executable, PrefixArgs: c.PrefixArgs, Environment: c.Environment, WorkingDirectory: c.WorkingDirectory, PermissionMode: c.PermissionMode}))
	},
	"workbuddy": func(ctx context.Context, c Config) (sdk.Agent, error) {
		return adapt(workbuddy.NewAgent(ctx, workbuddy.Config{Logger: c.Logger, WorkBuddyPath: c.Executable, PrefixArgs: c.PrefixArgs, Environment: c.Environment, WorkingDirectory: c.WorkingDirectory, PermissionMode: c.PermissionMode}))
	},
	"qoder": qoder, "qoder-cn": qoder,
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
	return adapt(nativeacp.NewAgent(ctx, nativeacp.Config{Logger: c.Logger, Command: c.Executable, Args: append(append([]string(nil), c.PrefixArgs...), "--acp"), VersionArgs: append(append([]string(nil), c.PrefixArgs...), "--version"), Environment: c.Environment, WorkingDirectory: c.WorkingDirectory, RuntimeName: c.RuntimeID, PromptFIFO: true}))
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
	f, ok := factories[c.RuntimeID]
	if !ok {
		return nil, &contract.Fault{Code: contract.FaultUnsupported, Stage: "configure", Message: "unsupported native runtime: " + c.RuntimeID}
	}
	c.PrefixArgs = append([]string(nil), c.PrefixArgs...)
	c.Environment = slices.Clone(c.Environment)
	return f(ctx, c)
}
