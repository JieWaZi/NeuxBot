package contract

import "context"

// Driver 是注入的协议适配，不参与宿主业务持久化。
type Driver interface {
	ID() string
	Capabilities() DriverCapabilities
	Open(context.Context, OpenRequest) (DriverSession, error)
	Close(context.Context) error
}

// DriverCapabilities 只声明已验证的能力，未知按不支持处理。
type DriverCapabilities struct {
	Version        string // Version 是实现版本。
	ExplicitConfig bool   // ExplicitConfig 证明能限制原生配置来源。
	DenyAllTools   bool   // DenyAllTools 证明准备前可以禁用全部工具。
	ToolAllowList  bool   // ToolAllowList 证明可以限制明确工具集合。
	HotResume      bool   // HotResume 证明原生会话支持增量续聊。
}

// OpenRequest 只在原生启动边界携带物化资源与凭据。
type OpenRequest struct {
	Invocation       InvocationRef     // Invocation 是本次调用身份。
	Profile          Profile           // Profile 是固定技术选择。
	Session          SessionDirective  // Session 是新会话及种子描述。
	Workspace        WorkspaceScope    // Workspace 是有效授权工作范围。
	Contributions    Contributions     // Contributions 是冻结的静态贡献。
	Policies         Policies          // Policies 是有效执行策略。
	Resources        []ResourceBinding // Resources 是会话租约保护的资源视图。
	Credential       Credential        // Credential 是仅在启动边界使用的秘密。
	ContextDigest    string            // ContextDigest 是静态上下文摘要。
	SecurityScopeKey string            // SecurityScopeKey 是安全域摘要。
}

// TurnRequest 只传递当前回合输入，不重复静态上下文。
type TurnRequest struct {
	Invocation InvocationRef // Invocation 是有效调用及修订。
	Input      Input         // Input 是本轮新增输入。
}

// DriverSession 拥有一个热原生会话；Prompt 返回前必须停止本轮生产并排空已接收事件。
// Close 成功必须证明原生使用者已停止使用依赖；失败由 Runtime 保留租约并允许重试。
type DriverSession interface {
	Prompt(context.Context, TurnRequest, DriverSink) DriverResult
	Close(context.Context) error
}

// DriverResult 保存原生执行终态，不猜测业务成功。
type DriverResult struct {
	Status       ExecutionStatus // Status 是真实技术执行状态。
	StopReason   StopReason      // StopReason 是原生结束原因。
	Usage        Usage           // Usage 是本轮用量。
	Fault        *Fault          // Fault 是驱动故障。
	NativeHandle string          // NativeHandle 是可用原生句柄，不证明冷恢复已支持。
}

// DriverSink 提供同步有界事实交付和独立交互端口。
// Accept 的来源 Context 不改变已发生事实的交付所有权；Observer 受本轮 Context 与 DeliveryTimeout 限制。
// Interact 同时遵守单请求与本轮 Context；单请求撤销不自动取消健康整轮。
// Accept 返回错误后驱动必须停止本轮；Interact 的单请求撤销仅作本请求失效。
type DriverSink interface {
	Accept(context.Context, DriverEvent) error
	Interact(context.Context, InteractionRequest) (InteractionResponse, error)
}

// EventKind 是规范事件类型。
type EventKind string

const (
	EventPromptAccepted   EventKind = "prompt_accepted"   // EventPromptAccepted 表示真实输入交付。
	EventAssistantMessage EventKind = "assistant_message" // EventAssistantMessage 表示助手消息片段。
	EventThinking         EventKind = "thinking"          // EventThinking 表示推理进度。
	EventTool             EventKind = "tool"              // EventTool 表示工具活动。
	EventUsage            EventKind = "usage"             // EventUsage 表示用量更新。
)

// MessagePhase 是驱动可证明的消息阶段。
type MessagePhase string

const (
	PhaseUnknown    MessagePhase = "unknown"    // PhaseUnknown 表示阶段未知。
	PhaseCommentary MessagePhase = "commentary" // PhaseCommentary 表示过程消息。
	PhaseFinal      MessagePhase = "final"      // PhaseFinal 表示最终回答消息。
)

// AssistantMessage 保留消息身份、阶段和边界证明。
type AssistantMessage struct {
	MessageID          string       // MessageID 是本轮内稳定的消息身份。
	Phase              MessagePhase // Phase 是原生明确提供的阶段。
	Delta              string       // Delta 是当前追加文本。
	Complete           bool         // Complete 表示此消息的正文已完成。
	CompletionVerified bool         // CompletionVerified 表示驱动验证了等价最终回答完成语义。
}

// ToolEvent 保存工具活动的最小事实。
type ToolEvent struct {
	ID     string // ID 是本轮工具调用身份。
	Name   string // Name 是稳定工具名称；同一 ID 的状态增量可省略。
	Status string // Status 是原生活动阶段。
	Text   string // Text 是有界活动摘要。
}

// DriverEvent 是协议无关的单个事件。
type DriverEvent struct {
	Kind    EventKind         // Kind 决定有效载荷。
	Message *AssistantMessage // Message 是助手文本事实。
	Tool    *ToolEvent        // Tool 是工具事实。
	Usage   *Usage            // Usage 是用量事实。
	Text    string            // Text 是推理等过程文本。
}

// Event 为宿主事件增加调用身份与本轮交付序号。
type Event struct {
	Invocation  InvocationRef // Invocation 是事件所属的有效调用。
	Sequence    uint64        // Sequence 是交付顺序，不是持久日志游标。
	DriverEvent DriverEvent   // DriverEvent 是规范执行事实。
}

// Observer 接受有序事实；实现必须遵守 Context 期限。
type Observer interface {
	Accept(context.Context, Event) error
}

// ObserverFunc 将函数适配为观察端口。
type ObserverFunc func(context.Context, Event) error

// Accept 同步交付一个事件。
func (f ObserverFunc) Accept(ctx context.Context, event Event) error { return f(ctx, event) }

// InteractionKind 区分审批与用户输入。
type InteractionKind string

const (
	InteractionApproval InteractionKind = "approval" // InteractionApproval 表示工具审批。
	InteractionInput    InteractionKind = "input"    // InteractionInput 表示用户输入。
)

// InteractionRequest 保存具有调用身份的交互请求。
type InteractionRequest struct {
	Invocation InvocationRef   // Invocation 是有效调用及修订。
	ID         string          // ID 是本轮唯一请求身份。
	Kind       InteractionKind // Kind 是交互类型。
	ToolName   string          // ToolName 是审批目标。
	Prompt     string          // Prompt 是需要展示的问题。
}

// InteractionResponse 必须回显有效身份，拒绝迟到与错配响应。
type InteractionResponse struct {
	Invocation InvocationRef // Invocation 是被回答的调用及修订。
	ID         string        // ID 是被回答的交互请求。
	Allowed    bool          // Allowed 是审批决定。
	Text       string        // Text 是用户输入答案。
}

// InteractionHandler 是独立于观察流的交互端口，实现必须响应 Context 撤销。
// 单请求撤销只使该问题失效，迟到回应不会成为授权，也不自动取消健康整轮。
type InteractionHandler interface {
	Handle(context.Context, InteractionRequest) (InteractionResponse, error)
}

// RunOptions 保存本次调用函数端口，不参与摘要。
type RunOptions struct {
	Observer           Observer           // Observer 是可选的同步观察端口。
	InteractionHandler InteractionHandler // InteractionHandler 是可选的人工交互端口。
}
