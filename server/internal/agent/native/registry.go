package native

import (
	"context"
	"fmt"
	"github.com/JieWaZi/acp-go/pkg/claude"
	"github.com/JieWaZi/acp-go/pkg/codex"
	"github.com/JieWaZi/acp-go/pkg/cursor"
	"github.com/JieWaZi/acp-go/pkg/gemini"
	"github.com/JieWaZi/acp-go/pkg/grok"
	"github.com/JieWaZi/acp-go/pkg/kimi"
	"github.com/JieWaZi/acp-go/pkg/nativeacp"
	"github.com/JieWaZi/acp-go/pkg/opencode"
	"github.com/JieWaZi/acp-go/pkg/pi"
	qoderadapter "github.com/JieWaZi/acp-go/pkg/qoder"
	"github.com/JieWaZi/acp-go/pkg/workbuddy"
	sdk "github.com/JieWaZi/acp-go/sdk"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// AgentConfig 描述调用方已准备的原生适配器启动配置。
type AgentConfig struct {
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

func qoder(ctx context.Context, c AgentConfig) (sdk.Agent, error) {
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

// NewAgent 根据运行时标识构造公开 ACP 适配器。
func NewAgent(ctx context.Context, c AgentConfig) (sdk.Agent, error) {
	descriptor, ok := descriptors[c.RuntimeID]
	if !ok {
		return nil, &Fault{Code: FaultUnsupported, Stage: "configure", Message: "unsupported native runtime: " + c.RuntimeID}
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

// ModeIDs 记录原生会话的权限语义候选，不决定产品授予哪档权限。
type ModeIDs struct {
	// Negotiate 要求按实际目录协商候选，不将首选值固定为 Profile 模式。
	Negotiate bool
	// Default 是需要逐项确认的原生模式候选。
	Default []string
	// Automatic 是允许原生自动审查的模式候选。
	Automatic []string
	// Unrestricted 是允许完全访问的模式候选。
	Unrestricted []string
	// ReadOnly 是只读工作区的模式候选；为空表示没有该能力。
	ReadOnly []string
	// SingleModeFallback 只匹配旧 CLI 唯一模式，不替代新版多模式目录。
	SingleModeFallback string
}

// Descriptor 是同一原生工厂使用的安装、协议和配置技术声明。
type Descriptor struct {
	// ID 是基础原生运行时身份。
	ID string
	// Name 是原生程序的目录名称。
	Name string
	// BinaryCandidates 是可执行文件候选，按优先级排列。
	BinaryCandidates []string
	// UserBinaryCandidates 是相对用户目录的官方原生程序路径。
	UserBinaryCandidates []string
	// InstructionFileName 是项目根指令文件名。
	InstructionFileName string
	// SkillDirectoryName 是项目原生技能目录名。
	SkillDirectoryName string
	// ConfigHomeEnvironmentVariable 是原生配置根的环境变量名。
	ConfigHomeEnvironmentVariable string
	// StateDirectoryName 是宿主状态根中的原生会话子目录。
	StateDirectoryName string
	// StateDirectoryEnvironmentVariable 是已有原生状态根的环境变量名。
	StateDirectoryEnvironmentVariable string
	// ForkStateDirectoryName 是目标工作区相邻的持久分叉状态目录。
	ForkStateDirectoryName string
	// Completion 是该适配器可证明的最终回答来源。
	Completion CompletionPolicy
	// Modes 记录原生权限模式，不持有产品授权。
	Modes ModeIDs
	// ModelConfigID 是缺少标准类别时读取模型目录的配置标识。
	ModelConfigID sdk.SessionConfigId
	// ReasoningConfigID 是缺少标准类别时读取推理目录的配置标识。
	ReasoningConfigID sdk.SessionConfigId
	// MCPTransports 是原生工厂支持的连接器传输。
	MCPTransports []MCPTransport
	// SkillPrefix 是本轮显式技能选择的原生调用前缀。
	SkillPrefix string
	// ManagedModelPrefix 是受管账号模型在原生配置中的命名空间。
	ManagedModelPrefix string
	// NativeSystemInstructions 表示独立调用可通过原生配置安装系统角色。
	NativeSystemInstructions bool
	// SkillDirectory 表示适配器读取项目原生技能目录。
	SkillDirectory bool
	// NativeProcess 表示协议由外部原生进程承载。
	NativeProcess bool
	// NativeVersionMetadataOnly 要求版本来自原生元数据，不能使用适配器构建号。
	NativeVersionMetadataOnly bool
	// ManagedPermissions 表示原生工厂在会话模式之外实现三档权限语义。
	ManagedPermissions bool
	// InspectModelOptions 允许临时切换模型补探专属配置。
	InspectModelOptions bool
	// AllowEmptyModelCatalog 允许原生会话不公开模型目录。
	AllowEmptyModelCatalog bool
	// newAgent 是该技术声明唯一对应的公开适配器构造器。
	newAgent factory
}

// Lookup 返回不会污染注册表的技术声明副本。
func Lookup(id string) (Descriptor, bool) {
	descriptor, ok := descriptors[id]
	return cloneDescriptor(descriptor), ok
}

// Descriptors 按基础身份返回全部不可变技术声明。
func Descriptors() []Descriptor {
	ids := make([]string, 0, len(descriptors))
	for id := range descriptors {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]Descriptor, 0, len(ids))
	for _, id := range ids {
		result = append(result, cloneDescriptor(descriptors[id]))
	}
	return result
}

func cloneDescriptor(d Descriptor) Descriptor {
	d.BinaryCandidates = slices.Clone(d.BinaryCandidates)
	d.UserBinaryCandidates = slices.Clone(d.UserBinaryCandidates)
	d.MCPTransports = slices.Clone(d.MCPTransports)
	d.Modes.Default = slices.Clone(d.Modes.Default)
	d.Modes.Automatic = slices.Clone(d.Modes.Automatic)
	d.Modes.Unrestricted = slices.Clone(d.Modes.Unrestricted)
	d.Modes.ReadOnly = slices.Clone(d.Modes.ReadOnly)
	return d
}

type factory func(context.Context, AgentConfig) (sdk.Agent, error)

var descriptors = map[string]Descriptor{
	"codex": {
		SkillPrefix:                   "$",
		NativeSystemInstructions:      true,
		ID:                            "codex",
		Name:                          "Codex CLI",
		BinaryCandidates:              []string{"codex"},
		InstructionFileName:           "AGENTS.md",
		SkillDirectoryName:            ".agents",
		ConfigHomeEnvironmentVariable: "CODEX_HOME",
		Completion:                    CompletionNativePhase,
		Modes:                         ModeIDs{Default: []string{"read-only"}, Automatic: []string{"agent"}, Unrestricted: []string{"agent-full-access"}, ReadOnly: []string{"read-only"}},
		ModelConfigID:                 "model",
		ReasoningConfigID:             "reasoning_effort",
		MCPTransports:                 []MCPTransport{MCPStdio, MCPHTTP},
		SkillDirectory:                true,
		InspectModelOptions:           true,
		newAgent: func(ctx context.Context, c AgentConfig) (sdk.Agent, error) {
			return adapt(codex.NewAgent(ctx, codex.Config{Logger: c.Logger, CodexPath: c.Executable, PrefixArgs: c.PrefixArgs, Environment: c.Environment, DefaultModeRequestUserInput: c.DefaultModeRequestUserInput}))
		},
	},
	"claude": {
		ID:                            "claude",
		Name:                          "Claude Code",
		BinaryCandidates:              []string{"claude"},
		InstructionFileName:           "CLAUDE.md",
		SkillDirectoryName:            ".claude",
		ConfigHomeEnvironmentVariable: "CLAUDE_CONFIG_DIR",
		Completion:                    CompletionLastAssistant,
		Modes:                         ModeIDs{Default: []string{"default"}, Automatic: []string{"auto"}, Unrestricted: []string{"bypassPermissions"}, ReadOnly: []string{"default"}},
		ModelConfigID:                 "model",
		ReasoningConfigID:             "effort",
		MCPTransports:                 []MCPTransport{MCPStdio, MCPHTTP, MCPSSE},
		SkillDirectory:                true,
		InspectModelOptions:           true,
		newAgent: func(ctx context.Context, c AgentConfig) (sdk.Agent, error) {
			return adapt(claude.NewAgent(ctx, claude.Config{Logger: c.Logger, ClaudePath: c.Executable, PrefixArgs: c.PrefixArgs, Environment: c.Environment}))
		},
	},
	"kimi": {
		SkillPrefix:            "/skill:",
		ID:                     "kimi",
		StateDirectoryName:     "kimi-state",
		Name:                   "Kimi",
		BinaryCandidates:       []string{"kimi"},
		InstructionFileName:    "AGENTS.md",
		SkillDirectoryName:     ".agents",
		Completion:             CompletionLastAssistant,
		Modes:                  ModeIDs{Negotiate: true, Default: []string{"default"}, Automatic: []string{"auto"}, Unrestricted: []string{"yolo"}, ReadOnly: []string{"plan"}, SingleModeFallback: "default"},
		ModelConfigID:          "model",
		ReasoningConfigID:      "reasoning",
		MCPTransports:          []MCPTransport{MCPStdio, MCPHTTP},
		SkillDirectory:         true,
		NativeProcess:          true,
		AllowEmptyModelCatalog: true,
		newAgent: func(ctx context.Context, c AgentConfig) (sdk.Agent, error) {
			return adapt(kimi.NewAgent(ctx, kimi.Config{Logger: c.Logger, KimiPath: c.Executable, PrefixArgs: c.PrefixArgs, Environment: c.Environment, WorkingDirectory: c.WorkingDirectory, StateDirectory: c.StateDirectory, PermissionMode: c.PermissionMode}))
		},
	},
	"cursor": {
		ID:                     "cursor",
		StateDirectoryName:     "cursor-state",
		ForkStateDirectoryName: "cursor-state",
		Name:                   "Cursor",
		BinaryCandidates:       []string{"cursor-agent", "agent"},
		InstructionFileName:    "AGENTS.md",
		SkillDirectoryName:     ".cursor",
		Completion:             CompletionLastAssistant,
		Modes:                  ModeIDs{Default: []string{"agent"}, Automatic: []string{"agent"}, Unrestricted: []string{"agent"}, ReadOnly: []string{"plan"}},
		ModelConfigID:          "model",
		ReasoningConfigID:      "reasoning",
		MCPTransports:          []MCPTransport{MCPStdio, MCPHTTP},
		SkillDirectory:         true,
		NativeProcess:          true,
		AllowEmptyModelCatalog: true,
		newAgent: func(ctx context.Context, c AgentConfig) (sdk.Agent, error) {
			return adapt(cursor.NewAgent(ctx, cursor.Config{Logger: c.Logger, CursorPath: c.Executable, PrefixArgs: c.PrefixArgs, Environment: c.Environment, WorkingDirectory: c.WorkingDirectory, StateDirectory: c.StateDirectory, StateDirectoryForWorkspace: c.StateDirectoryForWorkspace, PermissionMode: c.PermissionMode}))
		},
	},
	"pi": {
		SkillPrefix:                   "/skill:",
		ID:                            "pi",
		Name:                          "Pi",
		BinaryCandidates:              []string{"pi"},
		InstructionFileName:           "AGENTS.md",
		SkillDirectoryName:            ".pi",
		ConfigHomeEnvironmentVariable: "PI_CODING_AGENT_DIR",
		Completion:                    CompletionLastAssistant,
		ModelConfigID:                 "model",
		ReasoningConfigID:             "reasoning",
		MCPTransports:                 []MCPTransport{MCPStdio, MCPHTTP, MCPSSE},
		SkillDirectory:                true,
		NativeProcess:                 true,
		AllowEmptyModelCatalog:        true,
		newAgent: func(ctx context.Context, c AgentConfig) (sdk.Agent, error) {
			return adapt(pi.NewAgent(ctx, pi.Config{Logger: c.Logger, PiPath: c.Executable, PrefixArgs: c.PrefixArgs, Environment: c.Environment, WorkingDirectory: c.WorkingDirectory, PermissionMode: c.PermissionMode, MCPModulePath: environmentValue(c.Environment, "PI_MCP_MODULE_PATH")}))
		},
	},
	"opencode": {
		ManagedModelPrefix:            "ally/",
		ID:                            "opencode",
		Name:                          "OpenCode",
		BinaryCandidates:              []string{"opencode"},
		InstructionFileName:           "AGENTS.md",
		SkillDirectoryName:            ".opencode",
		ConfigHomeEnvironmentVariable: "OPENCODE_CONFIG_DIR",
		Completion:                    CompletionLastAssistant,
		ModelConfigID:                 "model",
		ReasoningConfigID:             "reasoning",
		MCPTransports:                 []MCPTransport{MCPStdio, MCPHTTP, MCPSSE},
		SkillDirectory:                true,
		NativeProcess:                 true,
		NativeVersionMetadataOnly:     true,
		AllowEmptyModelCatalog:        true,
		newAgent: func(ctx context.Context, c AgentConfig) (sdk.Agent, error) {
			return adapt(opencode.NewAgent(ctx, opencode.Config{Logger: c.Logger, OpenCodePath: c.Executable, PrefixArgs: c.PrefixArgs, Environment: c.Environment, WorkingDirectory: c.WorkingDirectory, PermissionMode: c.PermissionMode}))
		},
	},
	"gemini": {
		ID:                                "gemini",
		ManagedPermissions:                true,
		StateDirectoryEnvironmentVariable: "GEMINI_CLI_HOME",
		Name:                              "Gemini CLI",
		BinaryCandidates:                  []string{"gemini"},
		InstructionFileName:               "GEMINI.md",
		SkillDirectoryName:                ".gemini",
		ConfigHomeEnvironmentVariable:     "GEMINI_CLI_HOME",
		Completion:                        CompletionLastAssistant,
		Modes:                             ModeIDs{Default: []string{"default"}, Automatic: []string{"default"}, Unrestricted: []string{"yolo"}, ReadOnly: []string{"default"}},
		ModelConfigID:                     "model",
		ReasoningConfigID:                 "reasoning",
		MCPTransports:                     []MCPTransport{MCPStdio, MCPHTTP, MCPSSE},
		SkillDirectory:                    true,
		NativeProcess:                     true,
		NativeVersionMetadataOnly:         true,
		AllowEmptyModelCatalog:            true,
		newAgent: func(ctx context.Context, c AgentConfig) (sdk.Agent, error) {
			return adapt(gemini.NewAgent(ctx, gemini.Config{Logger: c.Logger, GeminiPath: c.Executable, PrefixArgs: c.PrefixArgs, Environment: c.Environment, WorkingDirectory: c.WorkingDirectory, StateDirectory: c.StateDirectory, PermissionMode: c.PermissionMode}))
		},
	},
	"grok": {
		ID:                            "grok",
		Name:                          "Grok Build",
		BinaryCandidates:              []string{"grok"},
		InstructionFileName:           "AGENTS.md",
		SkillDirectoryName:            ".grok",
		ConfigHomeEnvironmentVariable: "GROK_HOME",
		Completion:                    CompletionLastAssistant,
		ModelConfigID:                 "model",
		ReasoningConfigID:             "reasoning",
		MCPTransports:                 []MCPTransport{MCPStdio, MCPHTTP, MCPSSE},
		SkillDirectory:                true,
		NativeProcess:                 true,
		NativeVersionMetadataOnly:     true,
		AllowEmptyModelCatalog:        true,
		newAgent: func(ctx context.Context, c AgentConfig) (sdk.Agent, error) {
			return adapt(grok.NewAgent(ctx, grok.Config{Logger: c.Logger, GrokPath: c.Executable, PrefixArgs: c.PrefixArgs, Environment: c.Environment, WorkingDirectory: c.WorkingDirectory, PermissionMode: c.PermissionMode}))
		},
	},
	"workbuddy": {
		ID:                            "workbuddy",
		Name:                          "WorkBuddy",
		BinaryCandidates:              []string{"codebuddy", "cbc"},
		InstructionFileName:           "CODEBUDDY.md",
		SkillDirectoryName:            ".codebuddy",
		ConfigHomeEnvironmentVariable: "CODEBUDDY_CONFIG_DIR",
		Completion:                    CompletionLastAssistant,
		Modes:                         ModeIDs{Default: []string{"default"}, Automatic: []string{"auto"}, Unrestricted: []string{"bypassPermissions"}, ReadOnly: []string{"plan"}},
		ModelConfigID:                 "model",
		ReasoningConfigID:             "reasoning",
		MCPTransports:                 []MCPTransport{MCPStdio, MCPHTTP, MCPSSE},
		SkillDirectory:                true,
		NativeProcess:                 true,
		NativeVersionMetadataOnly:     true,
		InspectModelOptions:           true,
		AllowEmptyModelCatalog:        true,
		newAgent: func(ctx context.Context, c AgentConfig) (sdk.Agent, error) {
			// 聊天标题由 NeuxBot 管理；关闭 CLI 自动标题，避免辅助模型任务阻塞真实 Prompt 回执。
			c.Environment = nativeacp.WithEnvironment(c.Environment, "CODEBUDDY_CODE_DISABLE_TERMINAL_TITLE", "1")
			return adapt(workbuddy.NewAgent(ctx, workbuddy.Config{Logger: c.Logger, WorkBuddyPath: c.Executable, PrefixArgs: c.PrefixArgs, Environment: c.Environment, WorkingDirectory: c.WorkingDirectory, PermissionMode: c.PermissionMode}))
		},
	},
	"qoder": {
		ID:                            "qoder",
		Name:                          "Qoder",
		BinaryCandidates:              []string{"qoder", "qodercli"},
		UserBinaryCandidates:          []string{".qoder/bin/qodercli/qodercli"},
		InstructionFileName:           "AGENTS.md",
		SkillDirectoryName:            ".qoder",
		ConfigHomeEnvironmentVariable: "QODER_CONFIG_DIR",
		Completion:                    CompletionLastAssistant,
		Modes:                         ModeIDs{Negotiate: true, Default: []string{"default"}, Automatic: []string{"auto"}, Unrestricted: []string{"yolo", "bypassPermissions"}},
		ModelConfigID:                 "model",
		ReasoningConfigID:             "reasoning",
		MCPTransports:                 []MCPTransport{MCPStdio, MCPHTTP, MCPSSE},
		SkillDirectory:                true,
		NativeProcess:                 true,
		AllowEmptyModelCatalog:        true,
		newAgent:                      qoder,
	},
	"qoder-cn": {
		ID:                            "qoder-cn",
		Name:                          "Qoder CN",
		BinaryCandidates:              []string{"qodercn", "qoder-cn", "qoderclicn"},
		UserBinaryCandidates:          []string{".qoder-cn/bin/qoderclicn/qoderclicn"},
		InstructionFileName:           "AGENTS.md",
		SkillDirectoryName:            ".qoder",
		ConfigHomeEnvironmentVariable: "QODERCN_CONFIG_DIR",
		Completion:                    CompletionLastAssistant,
		Modes:                         ModeIDs{Negotiate: true, Default: []string{"default"}, Automatic: []string{"auto"}, Unrestricted: []string{"yolo", "bypassPermissions"}},
		ModelConfigID:                 "model",
		ReasoningConfigID:             "reasoning",
		MCPTransports:                 []MCPTransport{MCPStdio, MCPHTTP, MCPSSE},
		SkillDirectory:                true,
		NativeProcess:                 true,
		AllowEmptyModelCatalog:        true,
		newAgent:                      qoder,
	},
}

// Message 是独立调用交给原生适配器的角色化输入。
type Message struct {
	Role    string // Role 仅接受 system、user 或 assistant。
	Content string // Content 是本条角色输入的完整正文。
}

// TaskInput 保存原生角色转换结果，不拥有产品请求或资源。
type TaskInput struct {
	Text         string   // Text 是交给 ACP Prompt 的本轮正文。
	Instructions []string // Instructions 是需要由原生配置安装的系统指令。
	Limitations  []string // Limitations 是原生角色语义的明确限制。
}

// CompileTaskMessages 根据技术声明转换角色，不在 Host 散落 CLI 分支。
func CompileTaskMessages(runtimeID string, messages []Message) (TaskInput, error) {
	descriptor, ok := Lookup(runtimeID)
	if !ok {
		return TaskInput{}, &Fault{Code: FaultUnsupported, Stage: "input", Message: "native runtime is not registered"}
	}
	if len(messages) == 0 {
		return TaskInput{}, &Fault{Code: FaultInvalidSpec, Stage: "input", Message: "task messages are required"}
	}
	result := TaskInput{}
	remaining := make([]Message, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case "system":
			result.Instructions = append(result.Instructions, message.Content)
		case "user", "assistant":
			remaining = append(remaining, message)
			if message.Role == "assistant" {
				result.Limitations = append(result.Limitations, "assistant_history_serialized_as_text: ACP cannot install native assistant history")
			}
		default:
			return TaskInput{}, &Fault{Code: FaultInvalidSpec, Stage: "input", Message: "unsupported task message role"}
		}
	}
	if !descriptor.NativeSystemInstructions && len(result.Instructions) > 0 {
		remaining = messages
		result.Instructions = nil
		result.Limitations = append(result.Limitations, "native_system_role_unavailable: task messages serialized into one user text input")
	} else if len(remaining) == 1 && remaining[0].Role == "user" {
		result.Text = remaining[0].Content
		return result, nil
	} else {
		result.Limitations = append(result.Limitations, "native_message_roles_unavailable: ACP serializes user/assistant roles into one user text input")
	}
	var text strings.Builder
	for _, message := range remaining {
		fmt.Fprintf(&text, "<message role=%q>\n%s\n</message>\n", message.Role, message.Content)
	}
	result.Text = text.String()
	return result, nil
}

// ConfigureTaskInstructions 在明确支持原生系统指令的工厂安装文件配置。
// 文件属于调用方已持有的工作区，随同一会话资源释放，不另建清理所有者。
func ConfigureTaskInstructions(config AgentConfig, instructions []string) (AgentConfig, error) {
	descriptor, ok := Lookup(config.RuntimeID)
	if !ok {
		return config, &Fault{Code: FaultUnsupported, Stage: "configure", Message: "native runtime is not registered"}
	}
	if !descriptor.NativeSystemInstructions || len(instructions) == 0 {
		return config, nil
	}
	path := filepath.Join(config.WorkingDirectory, "model-instructions.md")
	if !filepath.IsAbs(config.WorkingDirectory) {
		return config, &Fault{Code: FaultInvalidSpec, Stage: "configure", Message: "native instruction workspace must be absolute"}
	}
	if err := os.WriteFile(path, []byte(strings.Join(instructions, "\n\n")), 0600); err != nil {
		return config, &Fault{Code: FaultPreparation, Stage: "configure", Message: "native model instructions cannot be written", Cause: err}
	}
	config.CodexConfiguration = maps.Clone(config.CodexConfiguration)
	if config.CodexConfiguration == nil {
		config.CodexConfiguration = make(map[string]string)
	}
	config.CodexConfiguration["model_instructions_file"] = strconv.Quote(path)
	return config, nil
}

// ModelID 根据原生技术声明投射受管账号模型，系统账号保持原生标识。
func ModelID(runtimeID string, managed bool, model string) string {
	descriptor, ok := Lookup(runtimeID)
	if ok && managed && model != "" {
		return descriptor.ManagedModelPrefix + model
	}
	return model
}
