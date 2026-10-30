package contract

import (
	"context"
	"maps"
	"slices"

	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
)

// ConnectorDeclaration 是会话兼容判断所需的固定 MCP 声明，不携带连接秘密。
type ConnectorDeclaration struct {
	// ID 是连接器的稳定标识。
	ID string
	// Name 是向智能体展示的固定名称。
	Name string
	// RuntimeKey 是原生配置使用的稳定名称。
	RuntimeKey string
	// Transport 是固定传输方式。
	Transport runtimeconfig.ConnectorTransport
	// Command 是 STDIO 服务的固定程序。
	Command string
	// Arguments 是固定程序参数。
	Arguments []string
	// URL 是固定远程服务地址。
	URL string
	// TimeoutMS 是固定启动超时。
	TimeoutMS int64
}

// SessionTarget 保存无秘密的智能体与原生启动选择，不拥有账号或进程资源。
type SessionTarget struct {
	// RuntimeID 是用户选择的智能体或本地 Profile。
	RuntimeID string
	// AccountID 是冻结的账号主体。
	AccountID string
	// Binary 是固定可执行文件选择。
	Binary string
	// PrefixArgs 是固定启动前置参数。
	PrefixArgs []string
	// ConfigHome 是固定原生配置目录。
	ConfigHome string
	// NativePluginDirectories 是固定原生插件目录。
	NativePluginDirectories []string
	// NativePluginScope 是固定原生插件范围事实。
	NativePluginScope string
	// CodexPluginConfiguration 是不含秘密的原生配置覆盖。
	CodexPluginConfiguration map[string]string
	// ClaudeSettingsPath 是固定模型设置文件。
	ClaudeSettingsPath string
}

// SessionPlan 保存会话兼容判断需要的静态业务事实，不包含本轮输入或资源获取回调。
type SessionPlan struct {
	// Target 是冻结的智能体、账号及原生启动选择。
	Target SessionTarget
	// Detection 是当前可运行探测快照。
	Detection runtimeconfig.DetectionResult
	// Workspace 是已授权的工作范围。
	Workspace Workspace
	// Instructions 是固定伙伴、项目或任务指令。
	Instructions []Instruction
	// Skills 是当前范围声明的固定技能。
	Skills []Skill
	// Connectors 是当前范围声明的固定连接器。
	Connectors []ConnectorDeclaration
	// Policy 是固定模型、推理和权限选择。
	Policy RunPolicy
	// ResourceKey 是账号与插件配置的无秘密兼容依据。
	ResourceKey string
	// PluginIDs 是资源绑定前就必须保护的插件身份。
	PluginIDs []string
	// ReadOnlyHandoff 要求在原生上下文上生成只读交接并关闭临时消费者。
	ReadOnlyHandoff bool
}

// TurnInput 只保存本轮新增消息、显式选择和当前交互端口。
type TurnInput struct {
	// LaunchEnvironment 是本轮软件授权的启动环境，只在冷资源取得时并入资源；热轮更新既有连接文件。
	LaunchEnvironment map[string]string
	// Message 是当前用户输入。
	Message Message
	// History 只在实际新建会话时用于有界历史播种。
	History []HistoryMessage
	// SelectedSkills 是本轮明确选择的固定技能名称。
	SelectedSkills []string
	// SelectedConnectors 是本轮明确选择的固定连接器标识。
	SelectedConnectors []string
	// UserInputState 是本轮软件能力问答的等待状态。
	UserInputState UserInputState
	// ApprovalHandler 是本轮工具审批端口。
	ApprovalHandler ApprovalHandler
	// ElicitationHandler 是本轮结构化输入端口。
	ElicitationHandler ElicitationHandler
}

// SessionResources 保存取得租约后实际冻结的业务资源；首次冷 Bind 即交接全部释放责任。
type SessionResources struct {
	// Target 是账号与原生资源物化后的私有启动事实。
	Target runtimeconfig.RuntimeTarget
	// Session 是资源变化要求的新会话动作；为空时沿用接纳动作。
	Session *SessionDirective
	// Skills 是租约保护的实际技能目录。
	Skills []Skill
	// Connectors 是租约保护的实际连接器配置。
	Connectors []Connector
	// PluginResources 是实际交付的插件版本和能力快照。
	PluginResources []plugins.Snapshot
	// NativePluginScope 是本消费者冻结的原生插件作用域。
	NativePluginScope string
	// CommitScope 在权威接纳后提交原生能力作用域。
	CommitScope func() error
	// UpdateUsage 更新当前消费者的业务执行引用，不改变资源租约。
	UpdateUsage func(...authorization.Usage)
	// Close 在原生消费者退出后释放账号和插件；失败须允许重试。
	Close func(context.Context) error
}

// PreparedChatRun 占用唯一回合，业务层显式取得资源并绑定后才可提交本轮输入。
type PreparedChatRun interface {
	// Context 提供受内核取消约束的资源准备上下文。
	Context() context.Context
	// NeedsResources 表示该冷消费者仍需绑定实际资源。
	NeedsResources() bool
	// Bind 首次冷绑定接管实际或部分资源；失败仍由内核负责释放，重复或已消费调用不接管新资源。
	Bind(context.Context, SessionResources) error
	// Resources 返回同一消费者已经固定的具名业务事实。
	Resources() SessionResources
	// Session 返回当前实际新建或恢复动作。
	Session() SessionDirective
	// Run 提交本轮新增输入；每次准备最多调用一次。
	Run(context.Context, TurnInput, EventSink) Result
	// Close 放弃未提交的准备；热会话资源继续由内核拥有。
	Close(context.Context) error
}

// DeclareTarget 从实际启动值提取无秘密的会话声明。
func DeclareTarget(target runtimeconfig.RuntimeTarget) SessionTarget {
	return SessionTarget{
		RuntimeID: target.RuntimeID, AccountID: target.AccountID, Binary: target.Binary,
		PrefixArgs: slices.Clone(target.PrefixArgs), ConfigHome: target.ConfigHome,
		NativePluginDirectories: slices.Clone(target.NativePluginDirectories), NativePluginScope: target.NativePluginScope,
		CodexPluginConfiguration: maps.Clone(target.CodexPluginConfiguration), ClaudeSettingsPath: target.ClaudeSettingsPath,
	}
}

// LaunchTarget 创建尚未取得账号或进程资源的启动值，实际秘密仅在冷资源准备后补充。
func (target SessionTarget) LaunchTarget() runtimeconfig.RuntimeTarget {
	return runtimeconfig.RuntimeTarget{
		RuntimeID: target.RuntimeID, AccountID: target.AccountID, Binary: target.Binary,
		PrefixArgs: slices.Clone(target.PrefixArgs), ConfigHome: target.ConfigHome,
		NativePluginDirectories: slices.Clone(target.NativePluginDirectories), NativePluginScope: target.NativePluginScope,
		CodexPluginConfiguration: maps.Clone(target.CodexPluginConfiguration), ClaudeSettingsPath: target.ClaudeSettingsPath,
	}
}

// DeclareConnectors 提取会话声明需要的公开固定配置，秘密环境与请求头只留在实际资源中。
func DeclareConnectors(connectors []Connector) []ConnectorDeclaration {
	result := make([]ConnectorDeclaration, 0, len(connectors))
	for _, connector := range connectors {
		result = append(result, ConnectorDeclaration{
			ID: connector.ID, Name: connector.Name, RuntimeKey: connector.RuntimeKey, Transport: connector.Transport,
			Command: connector.Command, Arguments: slices.Clone(connector.Arguments), URL: connector.URL, TimeoutMS: connector.TimeoutMS,
		})
	}
	return result
}

// ForkSessionRequest 保存分叉所需的原生身份、位置及目标执行环境。
type ForkSessionRequest struct {
	// ReceiptDirectory 是宿主持有的非公开持久操作目录。
	ReceiptDirectory string
	// ReconcileOnly 仅查询已执行原生调用的结果，禁止创建第二个分支。
	ReconcileOnly bool
	// SourceSessionID 是来源的原生句柄，不能交给前端。
	SourceSessionID string
	// Position 是适配器在回合终态返回的不透明定位信息；空值表示末尾。
	Position string
	// SourceWorkspace 是原生会话当前绑定的工作区。
	SourceWorkspace string
	// Plan 提供目标工作区、Runtime 配置与权限。
	Plan SessionPlan
}

// ForkSessionResult 是已建立的独立原生分支。
type ForkSessionResult struct {
	// SessionID 是之后续聊和冷恢复使用的新原生句柄。
	SessionID string
	// Mode 是当前实际适配器宣告的分叉精度。
	Mode string
}
