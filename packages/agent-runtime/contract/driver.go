package contract

import (
	"context"
	"encoding/json"
)

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
	ColdResume     bool   // ColdResume 证明驱动能按握手能力尝试原生会话恢复。
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
	Invocation     InvocationRef  // Invocation 是有效调用及修订。
	Input          Input          // Input 是本轮新增输入。
	Limits         Limits         // Limits 是本次调用已经规范化的预算。
	UserInputState UserInputState // UserInputState 是本轮外部工具发起的真实人工等待。
}

// DriverSession 拥有一个热原生会话；Prompt 返回前必须停止本轮生产并排空已接收事件。
// Close 成功必须证明原生使用者已停止使用依赖；失败由 Runtime 保留租约并允许重试。
type DriverSession interface {
	Prompt(context.Context, TurnRequest, DriverSink) DriverResult
	Close(context.Context) error
}

// DriverResult 保存原生执行终态，不猜测业务成功。
type DriverResult struct {
	Status        ExecutionStatus   // Status 是真实技术执行状态。
	StopReason    StopReason        // StopReason 是原生结束原因。
	Usage         Usage             // Usage 是本轮用量。
	Configuration Configuration     // Configuration 是原生确认的技术选择。
	Fault         *Fault            // Fault 是驱动故障。
	NativeSession NativeSessionInfo // NativeSession 是原生握手和本轮响应明确提供的会话事实。
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
	MessageID          string         // MessageID 是本轮内稳定的消息身份。
	Phase              MessagePhase   // Phase 是原生明确提供的阶段。
	Delta              string         // Delta 是当前追加文本。
	Content            []ContentBlock // Content 是文本之外的公开规范内容。
	Complete           bool           // Complete 表示此消息的正文已完成。
	CompletionVerified bool           // CompletionVerified 表示驱动验证了等价最终回答完成语义。
}

// Location 保存工具声明的文件位置。
type Location struct {
	Path string // Path 是工具声明的文件路径。
	Line *int   // Line 是可选的一基行号。
}

// Diff 保存公开工具内容中的文件前后文本。
type Diff struct {
	Path    string // Path 是变更文件路径。
	OldText string // OldText 是原文本，空值也可表示新建文件。
	NewText string // NewText 是变更后的文本。
}

// ContentBlock 保存公开协议内容，不携带厂商元数据。
type ContentBlock struct {
	Type       string // Type 是 text、image、audio、resource、resource_link、diff 或 terminal。
	Text       string // Text 是正文或文本资源内容。
	MIMEType   string // MIMEType 是内容类型。
	Data       string // Data 是公开协议提供的 Base64 内容。
	URI        string // URI 是公开资源地址。
	Name       string // Name 是资源显示名称。
	Diff       *Diff  // Diff 是文件变更内容。
	TerminalID string // TerminalID 是公开终端引用。
}

// ToolEvent 保存工具调用及其增量公开事实。
type ToolEvent struct {
	ID        string          // ID 是本轮工具调用身份。
	Name      string          // Name 是可证明的稳定工具名，缺失时保持为空。
	Title     string          // Title 是本次更新中的工具显示标题。
	Kind      string          // Kind 是公开工具类别。
	Status    string          // Status 是原生活动阶段。
	Text      string          // Text 是有界活动摘要。
	Update    bool            // Update 表示既有调用的更新，false 表示首次调用。
	Input     json.RawMessage // Input 是公开工具参数 JSON，nil 表示未提供。
	Output    json.RawMessage // Output 是公开工具结果 JSON，nil 表示未提供。
	Content   []ContentBlock  // Content 是标准工具正文、差异及终端引用。
	Locations []Location      // Locations 是工具声明的文件位置。
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

// ApprovalOption 保留原生可选项的准确身份与类别。
type ApprovalOption struct {
	ID   string // ID 是必须原样回传的选项身份。
	Name string // Name 是选项显示名称。
	Kind string // Kind 是公开选项类别，未知值保持不变。
}

// ApprovalRequest 保存人工审批需要的安全展示事实。
type ApprovalRequest struct {
	ToolCallID string           // ToolCallID 是对应工具调用身份。
	Title      string           // Title 是原生操作标题。
	Kind       string           // Kind 是公开工具类别。
	Summary    string           // Summary 是标准内容形成的操作摘要。
	Command    string           // Command 是标准参数中的命令。
	Locations  []Location       // Locations 是操作声明的文件位置。
	Options    []ApprovalOption // Options 是可原样回传的全部有效选项。
}

// InputRequest 保存公开结构化输入请求。
type InputRequest struct {
	Mode            string         // Mode 是 form 或 url。
	RequestedSchema map[string]any // RequestedSchema 是公开表单 JSON Schema。
	URL             string         // URL 是需要访问的公开输入地址。
}

// InteractionRequest 保存具有调用身份的交互请求。
type InteractionRequest struct {
	Invocation InvocationRef    // Invocation 是有效调用及修订。
	ID         string           // ID 是本轮唯一请求身份。
	Kind       InteractionKind  // Kind 是交互类型。
	ToolName   string           // ToolName 是可证明的工具授权名称。
	Prompt     string           // Prompt 是需要展示的说明或问题。
	Approval   *ApprovalRequest // Approval 是工具审批的标准展示事实。
	Input      *InputRequest    // Input 是表单或 URL 输入的标准描述。
}

// InteractionAction 是用户对本次请求的明确决定。
type InteractionAction string

const (
	InteractionAccept  InteractionAction = "accept"  // InteractionAccept 表示提交选项或内容。
	InteractionDecline InteractionAction = "decline" // InteractionDecline 表示明确拒绝。
	InteractionCancel  InteractionAction = "cancel"  // InteractionCancel 表示未形成有效回应。
)

// InteractionResponse 回显调用身份，并返回唯一的明确交互决定。
type InteractionResponse struct {
	Invocation InvocationRef     // Invocation 是被回答的调用及修订。
	ID         string            // ID 是被回答的交互请求。
	Action     InteractionAction // Action 是提交、拒绝或取消。
	OptionID   string            // OptionID 是审批选中的原生选项身份。
	Content    map[string]any    // Content 是表单提交内容。
}

// InteractionHandler 是独立于观察流的交互端口，实现必须响应 Context 撤销。
// 单请求撤销只使该问题失效，迟到回应不会成为授权，也不自动取消健康整轮。
type InteractionHandler interface {
	Handle(context.Context, InteractionRequest) (InteractionResponse, error)
}

// UserInputState 观察宿主工具产生的人工等待，供原生无活动计时器暂停和恢复。
// Changed 返回当前状态改变的通知；实现须允许并发读取。
type UserInputState interface {
	Pending() bool
	Changed() <-chan struct{}
}

// RunOptions 保存本次调用函数端口，不参与摘要。
type RunOptions struct {
	Observer           Observer           // Observer 是可选的同步观察端口。
	InteractionHandler InteractionHandler // InteractionHandler 是可选的人工交互端口。
	UserInputState     UserInputState     // UserInputState 是宿主工具发起的外部人工等待。
}
