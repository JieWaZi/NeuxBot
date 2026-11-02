package native

import (
	"context"
	"encoding/json"
	"time"
)

// InvocationRef 标识一次调用及其所属会话修订。
type InvocationRef struct {
	ID        string // ID 是不可重用的调用标识。
	SessionID string // SessionID 是本引擎分配的会话标识。
	Revision  uint64 // Revision 是本次调用的会话修订。
}

// Profile 描述固定的技术选择，不保存凭据明文。
type Profile struct {
	DriverID   string // DriverID 是实际交接驱动的稳定身份。
	Model      string // Model 是明确请求的模型。
	Reasoning  string // Reasoning 是明确请求的推理配置，优先于 Thinking。
	Thinking   *bool  // Thinking 为 false 时选择原生关闭或最低推理档；nil 保留原生默认。
	NativeMode string // NativeMode 是请求的原生权限模式，必须由驱动验证回执。
	Subject    string // Subject 标识凭据所属主体。
}

// Input 只保存当前回合新增输入。
type Input struct {
	Text           string          // Text 是本轮正文。
	Attachments    []Attachment    // Attachments 是本轮附件。
	CodeReferences []CodeReference // CodeReferences 是发送时固定的整文件引用或代码快照。
	SelectedSkills []string        // SelectedSkills 是本轮明确选择的已贡献技能名称。
	SelectedMCP    []string        // SelectedMCP 是本轮明确选择的已贡献连接名称。
}

// Attachment 引用已授权的本地附件。
type Attachment struct {
	Name        string // Name 是稳定显示名称。
	Path        string // Path 是附件的绝对路径。
	DisplayPath string // DisplayPath 是模型可读的工作区路径，空值时使用 Path。
	MIMEType    string // MIMEType 是内容类型。
}

// CodeReference 固定一次输入选择的文件位置和代码文本。
type CodeReference struct {
	Path      string // Path 是整文件路径或片段发送时的显示路径。
	StartLine int    // StartLine 是片段一基起始行，整文件引用为零。
	EndLine   int    // EndLine 是片段一基结束行，整文件引用为零。
	Content   string // Content 是发送时的片段快照，整文件引用为空。
}

// SeedMessage 是新会话的有界历史播种内容。
type SeedMessage struct {
	Role           string          // Role 仅接受 user 或 assistant。
	Text           string          // Text 是完整消息正文。
	CodeReferences []CodeReference // CodeReferences 是该历史消息发送时固定的内容引用。
}

// SessionMode 区分新建与恢复会话。
type SessionMode string

const (
	SessionNew    SessionMode = "new"    // SessionNew 新建会话。
	SessionResume SessionMode = "resume" // SessionResume 使用已拥有的热会话或持久化原生句柄恢复会话。
)

// SessionDirective 明确新建或恢复操作；原生身份由宿主持久化。
type SessionDirective struct {
	Mode         SessionMode // Mode 是本次会话操作。
	NativeHandle string      // NativeHandle 是恢复时使用的真实原生身份；新建时为空。
}

// WorkspaceScope 保存宿主已授权并冻结的工作范围，不声明原生目录沙箱保证。
type WorkspaceScope struct {
	Root        string   // Root 是主工作目录。
	Directories []string // Directories 是宿主额外授权目录；驱动按原生能力协商额外目录参数。
	ReadOnly    bool     // ReadOnly 要求原生工作区只读，驱动必须按已验证能力执行。
}

// Instruction 是具有稳定名称与版本的指令贡献。
type Instruction struct {
	Name    string // Name 是合并冲突键。
	Version string // Version 标识内容版本。
	Text    string // Text 是模型可见内容。
}

// Skill 是由宿主解析的技能资源身份。
type Skill struct {
	Name       string // Name 是稳定技能名称。
	Version    string // Version 是固定版本。
	ResourceID string // ResourceID 由资源端口物化。
	Optional   bool   // Optional 表示缺失资源可跳过。
}

// MCP 是由宿主解析的工具连接贡献。
type MCP struct {
	Name       string // Name 是稳定服务名称。
	Version    string // Version 是固定版本。
	ResourceID string // ResourceID 由资源端口物化。
	Optional   bool   // Optional 表示缺失资源可跳过。
}

// Contributions 保存按固定顺序合并的能力内容。
type Contributions struct {
	Instructions []Instruction // Instructions 是静态指令贡献。
	Skills       []Skill       // Skills 是固定技能贡献。
	MCP          []MCP         // MCP 是固定工具连接贡献。
}

// ToolMode 表达原生工具授权方式。
type ToolMode string

const (
	ToolsDenyAll   ToolMode = "deny_all"   // ToolsDenyAll 禁止所有工具。
	ToolsAllowList ToolMode = "allow_list" // ToolsAllowList 只允许明确名称。
	ToolsAllowAll  ToolMode = "allow_all"  // ToolsAllowAll 请求宿主授予的全部工具。
)

// ToolPolicy 不与输出或会话策略绑定。
type ToolPolicy struct {
	Mode  ToolMode // Mode 是请求的授权方式。
	Allow []string // Allow 是请求的工具名称。
}

// NativeConfigPolicy 表达原生配置继承范围。
type NativeConfigPolicy string

const (
	NativeConfigExplicitOnly NativeConfigPolicy = "explicit_only"  // NativeConfigExplicitOnly 只允许显式资源。
	NativeConfigInherit      NativeConfigPolicy = "inherit_native" // NativeConfigInherit 允许已批准的原生配置。
)

// Limits 为单次调用提供有界预算；零值采用 Runtime 默认值。
type Limits struct {
	MaxOutputBytes     int           // MaxOutputBytes 是最终候选正文上限。
	MaxEventBytes      int           // MaxEventBytes 是单个规范事件上限。
	MaxEvents          int           // MaxEvents 是本轮事件数量上限。
	MaxTotalEventBytes int           // MaxTotalEventBytes 是本轮事件累计字节上限。
	DeliveryTimeout    time.Duration // DeliveryTimeout 是一次观察与交互交付期限。
	InteractionTimeout time.Duration // InteractionTimeout 是等待人工交互的独立期限。
	InactivityTimeout  time.Duration // InactivityTimeout 是 Prompt 无原生活动且无待处理交互时的最长等待。
}

// ExecutionStatus 表达技术执行是否完成。
type ExecutionStatus string

const (
	StatusCompleted ExecutionStatus = "completed" // StatusCompleted 表示驱动完成执行。
	StatusFailed    ExecutionStatus = "failed"    // StatusFailed 表示技术执行失败。
	StatusCancelled ExecutionStatus = "cancelled" // StatusCancelled 表示有效取消。
)

// StopReason 表达执行收敛原因。
type StopReason string

const (
	StopEndTurn             StopReason = "end_turn"             // StopEndTurn 表示正常回合完成。
	StopCancelled           StopReason = "cancelled"            // StopCancelled 表示取消。
	StopRefused             StopReason = "refused"              // StopRefused 表示模型拒绝。
	StopMaxTokens           StopReason = "max_tokens"           // StopMaxTokens 表示原生长度截断。
	StopInteractionRequired StopReason = "interaction_required" // StopInteractionRequired 表示需要用户输入。
	StopFailed              StopReason = "failed"               // StopFailed 表示其他技术失败。
)

// Usage 区分本轮用量和最后上下文快照。
type Usage struct {
	Known               bool  // Known 表示本轮原生用量已报告，false 时不能把零值作为真实用量。
	InputTokens         int64 // InputTokens 是协议报告的本轮输入部分；缓存组成按对应字段保留。
	OutputTokens        int64 // OutputTokens 是本轮输出用量。
	ThoughtTokens       int64 // ThoughtTokens 是原生报告的本轮推理输出用量。
	CachedReadTokens    int64 // CachedReadTokens 是本轮缓存读取的输入用量。
	CachedWriteTokens   int64 // CachedWriteTokens 是本轮缓存写入的输入用量。
	ContextKnown        bool  // ContextKnown 表示最后上下文快照已报告。
	ContextTokens       int64 // ContextTokens 是最后一次模型请求的上下文快照，不累计回合用量。
	ContextWindowTokens int64 // ContextWindowTokens 是原生报告的上下文窗口容量，零值表示未提供。
}

// Configuration 保存原生目录或配置写入回执确认的实际选择。
type Configuration struct {
	ActualModel       string // ActualModel 是原生确认的模型。
	ActualReasoning   string // ActualReasoning 是原生确认的推理档位。
	ActualMode        string // ActualMode 是原生确认的权限模式。
	ThinkingDisabled  bool   // ThinkingDisabled 只在确认的原生档位明确关闭推理时为 true。
	ReasoningFallback string // ReasoningFallback 记录无法关闭推理时的实际降级。
}

// NativeSessionInfo 聚合公开适配器实际报告的原生会话能力与位置。
type NativeSessionInfo struct {
	Handle       string // Handle 是原生返回的会话身份。
	Resumable    bool   // Resumable 表示本次原生握手明确支持恢复或加载。
	ForkMode     string // ForkMode 是公开元数据声明的原生分叉方式。
	ForkPosition string // ForkPosition 是本轮公开回执中的原生分叉位置。
}

// SessionPolicies 保存影响原生会话兼容性的授权与配置策略。
type SessionPolicies struct {
	Tools        ToolPolicy         // Tools 是会话有效工具授权。
	NativeConfig NativeConfigPolicy // NativeConfig 是原生配置继承范围。
}

// OpenRequest 只在原生启动边界携带物化资源与凭据。
type OpenRequest struct {
	Invocation    InvocationRef     // Invocation 是本次调用身份。
	Profile       Profile           // Profile 是固定技术选择。
	Session       SessionDirective  // Session 是新建或原生句柄恢复要求。
	Seed          []SeedMessage     // Seed 仅供新建会话的有界历史播种。
	Workspace     WorkspaceScope    // Workspace 是有效授权工作范围。
	Contributions Contributions     // Contributions 是冻结的静态贡献。
	Policies      SessionPolicies   // Policies 是有效执行策略。
	Resources     []ResourceBinding // Resources 是会话租约保护的资源视图。
	Credential    Credential        // Credential 是仅在启动边界使用的秘密。
}

// TurnRequest 只传递当前回合输入，不重复静态上下文。
type TurnRequest struct {
	Invocation     InvocationRef  // Invocation 是有效调用及修订。
	Input          Input          // Input 是本轮新增输入。
	Limits         Limits         // Limits 是本次调用已经规范化的预算。
	UserInputState UserInputState // UserInputState 是本轮外部工具发起的真实人工等待。
}

// TurnResult 保存原生执行终态，不猜测业务成功。
type TurnResult struct {
	Status        ExecutionStatus   // Status 是真实技术执行状态。
	StopReason    StopReason        // StopReason 是原生结束原因。
	Usage         Usage             // Usage 是本轮用量。
	Configuration Configuration     // Configuration 是原生确认的技术选择。
	Fault         *Fault            // Fault 是驱动故障。
	NativeSession NativeSessionInfo // NativeSession 是原生握手和本轮响应明确提供的会话事实。
}

// EventSink 提供同步有界事实交付和独立交互端口。
// Accept 的来源 Context 不改变已发生事实的交付所有权；Observer 受本轮 Context 与 DeliveryTimeout 限制。
// Interact 同时遵守单请求与本轮 Context；单请求撤销不自动取消健康整轮。
// Accept 返回错误后驱动必须停止本轮；Interact 的单请求撤销仅作本请求失效。
type EventSink interface {
	Accept(context.Context, Event) error
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

// Event 是协议无关的单个事件。
type Event struct {
	Kind    EventKind         // Kind 决定有效载荷。
	Message *AssistantMessage // Message 是助手文本事实。
	Tool    *ToolEvent        // Tool 是工具事实。
	Usage   *Usage            // Usage 是用量事实。
	Text    string            // Text 是推理等过程文本。
}

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

// UserInputState 观察宿主工具产生的人工等待，供原生无活动计时器暂停和恢复。
// Changed 返回当前状态改变的通知；实现须允许并发读取。
type UserInputState interface {
	Pending() bool
	Changed() <-chan struct{}
}

// CompletionPolicy 声明驱动允许采用的最终回答证明来源。
type CompletionPolicy string

const (
	// CompletionNativePhase 仅接收公开适配器验证的原生最终阶段。
	CompletionNativePhase CompletionPolicy = "native_phase"
	// CompletionLastAssistant 在成功回合排空通知后验证最后一个有效助手片段。
	CompletionLastAssistant CompletionPolicy = "last_assistant"
)

// ResourceBinding 是已物化、由租约保护的资源。
type ResourceBinding struct {
	ID   string         // ID 是与请求匹配的稳定资源身份。
	Path string         // Path 是物化后的技能目录。
	MCP  *MCPConnection // MCP 是仅交给原生启动边界的连接配置。
}

// MCPTransport 区分公开 MCP 的连接传输。
type MCPTransport string

const (
	MCPStdio MCPTransport = "stdio" // MCPStdio 通过本地命令连接。
	MCPHTTP  MCPTransport = "http"  // MCPHTTP 通过 Streamable HTTP 连接。
	MCPSSE   MCPTransport = "sse"   // MCPSSE 通过 SSE 连接。
)

// MCPConnection 是宿主租约保护的实际连接，不进入静态会话声明。
type MCPConnection struct {
	Transport   MCPTransport      // Transport 是本连接的实际传输。
	Command     string            // Command 是本地连接命令。
	Arguments   []string          // Arguments 是本地命令有序参数。
	Environment map[string]string // Environment 是仅供连接子进程使用的环境。
	URL         string            // URL 是远程连接地址。
	Headers     map[string]string // Headers 是仅供远程连接使用的请求头。
}

// Credential 只在启动边界传递，不能写入静态会话声明或日志。
type Credential struct {
	Environment map[string]string // Environment 是仅供原生进程的秘密环境。
}

// FaultCode 是执行内核的结构化技术错误码。
type FaultCode string

const (
	FaultAuthenticationRequired FaultCode = "authentication_required" // FaultAuthenticationRequired 表示认证缺失或身份已经变化。
	FaultConfigurationInvalid   FaultCode = "configuration_invalid"   // FaultConfigurationInvalid 表示本机原生配置或认证文件不合法。
	FaultInvalidSpec            FaultCode = "invalid_spec"            // FaultInvalidSpec 表示描述不合法。
	FaultUnsupported            FaultCode = "unsupported"             // FaultUnsupported 表示能力尚未支持。
	FaultNotStarted             FaultCode = "not_started"             // FaultNotStarted 表示未开放接纳。
	FaultClosed                 FaultCode = "closed"                  // FaultClosed 表示已停止接纳。
	FaultCapacity               FaultCode = "capacity_exceeded"       // FaultCapacity 表示保护预算耗尽。
	FaultSessionBusy            FaultCode = "session_busy"            // FaultSessionBusy 表示同会话重入。
	FaultSessionMissing         FaultCode = "session_missing"         // FaultSessionMissing 表示会话不存在。
	FaultSessionMismatch        FaultCode = "session_mismatch"        // FaultSessionMismatch 表示静态或安全范围不兼容。
	FaultDuplicateInvocation    FaultCode = "duplicate_invocation"    // FaultDuplicateInvocation 表示活动调用身份冲突。
	FaultPreparation            FaultCode = "preparation_failed"      // FaultPreparation 表示资源或原生准备失败。
	FaultDriver                 FaultCode = "driver_failed"           // FaultDriver 表示驱动执行失败。
	FaultDelivery               FaultCode = "delivery_failed"         // FaultDelivery 表示观察交付失败。
	FaultLimit                  FaultCode = "limit_exceeded"          // FaultLimit 表示事件预算耗尽。
	FaultInteractionRequired    FaultCode = "interaction_required"    // FaultInteractionRequired 表示需要人工输入。
	FaultInactivityTimeout      FaultCode = "inactivity_timeout"      // FaultInactivityTimeout 表示原生调用超过无活动期限。
	FaultCancelled              FaultCode = "cancelled"               // FaultCancelled 表示调用取消。
	FaultCleanup                FaultCode = "cleanup_failed"          // FaultCleanup 表示释放失败。
	FaultProtocol               FaultCode = "protocol_error"          // FaultProtocol 表示驱动契约损坏。
)

// Fault 在模块边界保留准确技术语义，宿主只转换一次。
type Fault struct {
	Code    FaultCode // Code 是稳定技术错误码。
	Stage   string    // Stage 是故障发生阶段。
	Message string    // Message 是无秘密的英文技术说明。
	Cause   error     `json:"-"` // Cause 是仅供根因判断的底层错误，不进入默认 JSON。
}

// Error 返回可脱敏的技术说明，不拼接底层秘密。
func (f *Fault) Error() string {
	if f == nil {
		return ""
	}
	return string(f.Code) + ": " + f.Message
}

// Unwrap 暴露底层根因供 errors.Is 判断。
func (f *Fault) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.Cause
}
