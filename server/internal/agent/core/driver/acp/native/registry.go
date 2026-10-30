package native

import (
	"context"
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

	"slices"
	"sort"

	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

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
	Completion contract.CompletionPolicy
	// Modes 记录原生权限模式，不持有产品授权。
	Modes ModeIDs
	// ModelConfigID 是缺少标准类别时读取模型目录的配置标识。
	ModelConfigID sdk.SessionConfigId
	// ReasoningConfigID 是缺少标准类别时读取推理目录的配置标识。
	ReasoningConfigID sdk.SessionConfigId
	// MCPTransports 是原生工厂支持的连接器传输。
	MCPTransports []contract.MCPTransport
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

type factory func(context.Context, Config) (sdk.Agent, error)

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
		Completion:                    contract.CompletionNativePhase,
		Modes:                         ModeIDs{Default: []string{"read-only"}, Automatic: []string{"agent"}, Unrestricted: []string{"agent-full-access"}, ReadOnly: []string{"read-only"}},
		ModelConfigID:                 "model",
		ReasoningConfigID:             "reasoning_effort",
		MCPTransports:                 []contract.MCPTransport{contract.MCPStdio, contract.MCPHTTP},
		SkillDirectory:                true,
		InspectModelOptions:           true,
		newAgent: func(ctx context.Context, c Config) (sdk.Agent, error) {
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
		Completion:                    contract.CompletionLastAssistant,
		Modes:                         ModeIDs{Default: []string{"default"}, Automatic: []string{"auto"}, Unrestricted: []string{"bypassPermissions"}, ReadOnly: []string{"default"}},
		ModelConfigID:                 "model",
		ReasoningConfigID:             "effort",
		MCPTransports:                 []contract.MCPTransport{contract.MCPStdio, contract.MCPHTTP, contract.MCPSSE},
		SkillDirectory:                true,
		InspectModelOptions:           true,
		newAgent: func(ctx context.Context, c Config) (sdk.Agent, error) {
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
		Completion:             contract.CompletionLastAssistant,
		Modes:                  ModeIDs{Negotiate: true, Default: []string{"default"}, Automatic: []string{"auto"}, Unrestricted: []string{"yolo"}, ReadOnly: []string{"plan"}, SingleModeFallback: "default"},
		ModelConfigID:          "model",
		ReasoningConfigID:      "reasoning",
		MCPTransports:          []contract.MCPTransport{contract.MCPStdio, contract.MCPHTTP},
		SkillDirectory:         true,
		NativeProcess:          true,
		AllowEmptyModelCatalog: true,
		newAgent: func(ctx context.Context, c Config) (sdk.Agent, error) {
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
		Completion:             contract.CompletionLastAssistant,
		Modes:                  ModeIDs{Default: []string{"agent"}, Automatic: []string{"agent"}, Unrestricted: []string{"agent"}, ReadOnly: []string{"plan"}},
		ModelConfigID:          "model",
		ReasoningConfigID:      "reasoning",
		MCPTransports:          []contract.MCPTransport{contract.MCPStdio, contract.MCPHTTP},
		SkillDirectory:         true,
		NativeProcess:          true,
		AllowEmptyModelCatalog: true,
		newAgent: func(ctx context.Context, c Config) (sdk.Agent, error) {
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
		Completion:                    contract.CompletionLastAssistant,
		ModelConfigID:                 "model",
		ReasoningConfigID:             "reasoning",
		MCPTransports:                 []contract.MCPTransport{contract.MCPStdio, contract.MCPHTTP, contract.MCPSSE},
		SkillDirectory:                true,
		NativeProcess:                 true,
		AllowEmptyModelCatalog:        true,
		newAgent: func(ctx context.Context, c Config) (sdk.Agent, error) {
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
		Completion:                    contract.CompletionLastAssistant,
		ModelConfigID:                 "model",
		ReasoningConfigID:             "reasoning",
		MCPTransports:                 []contract.MCPTransport{contract.MCPStdio, contract.MCPHTTP, contract.MCPSSE},
		SkillDirectory:                true,
		NativeProcess:                 true,
		NativeVersionMetadataOnly:     true,
		AllowEmptyModelCatalog:        true,
		newAgent: func(ctx context.Context, c Config) (sdk.Agent, error) {
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
		Completion:                        contract.CompletionLastAssistant,
		Modes:                             ModeIDs{Default: []string{"default"}, Automatic: []string{"default"}, Unrestricted: []string{"yolo"}, ReadOnly: []string{"default"}},
		ModelConfigID:                     "model",
		ReasoningConfigID:                 "reasoning",
		MCPTransports:                     []contract.MCPTransport{contract.MCPStdio, contract.MCPHTTP, contract.MCPSSE},
		SkillDirectory:                    true,
		NativeProcess:                     true,
		NativeVersionMetadataOnly:         true,
		AllowEmptyModelCatalog:            true,
		newAgent: func(ctx context.Context, c Config) (sdk.Agent, error) {
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
		Completion:                    contract.CompletionLastAssistant,
		ModelConfigID:                 "model",
		ReasoningConfigID:             "reasoning",
		MCPTransports:                 []contract.MCPTransport{contract.MCPStdio, contract.MCPHTTP, contract.MCPSSE},
		SkillDirectory:                true,
		NativeProcess:                 true,
		NativeVersionMetadataOnly:     true,
		AllowEmptyModelCatalog:        true,
		newAgent: func(ctx context.Context, c Config) (sdk.Agent, error) {
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
		Completion:                    contract.CompletionLastAssistant,
		Modes:                         ModeIDs{Default: []string{"default"}, Automatic: []string{"auto"}, Unrestricted: []string{"bypassPermissions"}, ReadOnly: []string{"plan"}},
		ModelConfigID:                 "model",
		ReasoningConfigID:             "reasoning",
		MCPTransports:                 []contract.MCPTransport{contract.MCPStdio, contract.MCPHTTP, contract.MCPSSE},
		SkillDirectory:                true,
		NativeProcess:                 true,
		NativeVersionMetadataOnly:     true,
		InspectModelOptions:           true,
		AllowEmptyModelCatalog:        true,
		newAgent: func(ctx context.Context, c Config) (sdk.Agent, error) {
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
		Completion:                    contract.CompletionLastAssistant,
		Modes:                         ModeIDs{Negotiate: true, Default: []string{"default"}, Automatic: []string{"auto"}, Unrestricted: []string{"yolo", "bypassPermissions"}},
		ModelConfigID:                 "model",
		ReasoningConfigID:             "reasoning",
		MCPTransports:                 []contract.MCPTransport{contract.MCPStdio, contract.MCPHTTP, contract.MCPSSE},
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
		Completion:                    contract.CompletionLastAssistant,
		Modes:                         ModeIDs{Negotiate: true, Default: []string{"default"}, Automatic: []string{"auto"}, Unrestricted: []string{"yolo", "bypassPermissions"}},
		ModelConfigID:                 "model",
		ReasoningConfigID:             "reasoning",
		MCPTransports:                 []contract.MCPTransport{contract.MCPStdio, contract.MCPHTTP, contract.MCPSSE},
		SkillDirectory:                true,
		NativeProcess:                 true,
		AllowEmptyModelCatalog:        true,
		newAgent:                      qoder,
	},
}
