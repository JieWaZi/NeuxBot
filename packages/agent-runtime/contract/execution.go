// Package contract 定义不依赖宿主与具体协议的执行契约。
package contract

import "time"

// InvocationRef 标识一次调用及其所属会话修订。
type InvocationRef struct {
	ID        string // ID 是不可重用的调用标识。
	SessionID string // SessionID 是本引擎分配的会话标识。
	Revision  uint64 // Revision 是本次调用的会话修订。
}

// Profile 描述固定的技术选择，不保存凭据明文。
type Profile struct {
	DriverID             string // DriverID 选择已注入的驱动。
	Model                string // Model 是明确请求的模型。
	Reasoning            string // Reasoning 是明确请求的推理配置，优先于 Thinking。
	Thinking             *bool  // Thinking 为 false 时选择原生关闭或最低推理档；nil 保留原生默认。
	NativeMode           string // NativeMode 是请求的原生权限模式，必须由驱动验证回执。
	CredentialRef        string // CredentialRef 由宿主凭据端口解析。
	Subject              string // Subject 标识凭据所属主体。
	CredentialGeneration string // CredentialGeneration 区分凭据代际。
}

// Input 只保存当前回合新增输入。
type Input struct {
	Text        string       // Text 是本轮正文。
	Attachments []Attachment // Attachments 是本轮附件。
}

// Attachment 引用已授权的本地附件。
type Attachment struct {
	Name     string // Name 是稳定显示名称。
	Path     string // Path 是附件的绝对路径。
	MIMEType string // MIMEType 是内容类型。
}

// SeedMessage 是新会话的有界历史播种内容。
type SeedMessage struct {
	Role string // Role 仅接受 user 或 assistant。
	Text string // Text 是完整消息正文。
}

// SessionMode 区分新建与恢复会话。
type SessionMode string

const (
	SessionNew    SessionMode = "new"    // SessionNew 新建会话。
	SessionResume SessionMode = "resume" // SessionResume 恢复本引擎热会话。
)

// SessionDirective 明确会话身份和播种策略。
type SessionDirective struct {
	Mode       SessionMode        // Mode 是本次会话操作。
	Ref        string             // Ref 是本引擎产生的会话标识。
	Seed       []SeedMessage      // Seed 仅 New 可用，最多 40 条、正文共 64 KiB；宿主保证完整回合及顺序。
	Checkpoint *SessionCheckpoint // Checkpoint 表示冷恢复请求，首批明确不支持。
}

// SessionCheckpoint 保存无秘密的恢复事实。
type SessionCheckpoint struct {
	Version          int    // Version 是检查点格式版本。
	EngineID         string // EngineID 是生成检查点的引擎实例。
	SessionID        string // SessionID 是技术会话身份。
	DriverID         string // DriverID 是驱动身份。
	DriverVersion    string // DriverVersion 是驱动实现版本。
	NativeHandle     string // NativeHandle 是驱动提供的原生会话句柄。
	ContextDigest    string // ContextDigest 是静态上下文摘要。
	SecurityScopeKey string // SecurityScopeKey 是安全域摘要。
	Revision         uint64 // Revision 是有效会话修订。
	ForkPosition     string // ForkPosition 是原生不透明分叉位置。
}

// WorkspaceScope 保存已冻结的工作范围。
type WorkspaceScope struct {
	Root        string   // Root 是主工作目录。
	Directories []string // Directories 是额外授权目录。
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

// InteractionPolicy 表达交互端口的使用方式。
type InteractionPolicy string

const (
	InteractionUnattended  InteractionPolicy = "unattended"  // InteractionUnattended 不等待人工输入。
	InteractionInteractive InteractionPolicy = "interactive" // InteractionInteractive 使用本次交互端口。
)

// OutputPolicy 表达最终回答的证据要求。
type OutputPolicy string

const (
	OutputStrict     OutputPolicy = "strict"      // OutputStrict 拒绝无最终证明的正文。
	OutputBestEffort OutputPolicy = "best_effort" // OutputBestEffort 允许明确标注未验证的单条正文。
)

// Policies 保存互相独立的执行策略。
type Policies struct {
	Tools        ToolPolicy         // Tools 是工具策略。
	NativeConfig NativeConfigPolicy // NativeConfig 是原生配置策略。
	Interaction  InteractionPolicy  // Interaction 是交互策略。
	Output       OutputPolicy       // Output 是结果证据策略。
}

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

// ExecutionSpec 只包含描述值，不持有函数或明文凭据。
type ExecutionSpec struct {
	InvocationID  string           // InvocationID 只接受 Runtime.NewInvocationID 预分配值；空值由引擎分配。
	Profile       Profile          // Profile 是冻结技术选择。
	Input         Input            // Input 是本轮增量输入。
	Session       SessionDirective // Session 是会话操作。
	Workspace     WorkspaceScope   // Workspace 是请求工作范围。
	Contributions Contributions    // Contributions 是静态能力贡献。
	Policies      Policies         // Policies 是独立执行策略。
	Limits        Limits           // Limits 是调用预算。
}

// Grants 是宿主授予的权限上界。
type Grants struct {
	Directories   []string // Directories 是可访问目录上界。
	Tools         []string // Tools 是可调用工具上界。
	AllowAllTools bool     // AllowAllTools 明确授予任意工具。
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

// OutputStatus 表达最终答案可用性。
type OutputStatus string

const (
	OutputAvailable     OutputStatus = "available"          // OutputAvailable 表示存在满足策略的答案。
	OutputUnavailable   OutputStatus = "output_unavailable" // OutputUnavailable 表示缺少最终答案证明。
	OutputEmpty         OutputStatus = "empty"              // OutputEmpty 表示已验证但为空。
	OutputRefused       OutputStatus = "refused"            // OutputRefused 表示拒绝回答。
	OutputTruncated     OutputStatus = "truncated"          // OutputTruncated 表示答案被截断。
	OutputLimitExceeded OutputStatus = "limit_exceeded"     // OutputLimitExceeded 表示答案超过预算。
)

// Reliability 表达答案所依据的证明。
type Reliability string

const (
	ReliabilityVerifiedPhase      Reliability = "verified_phase"             // ReliabilityVerifiedPhase 依据明确 final 阶段。
	ReliabilityVerifiedCompletion Reliability = "verified_driver_completion" // ReliabilityVerifiedCompletion 依据驱动明确等价语义。
	ReliabilityUnverified         Reliability = "unverified"                 // ReliabilityUnverified 没有最终证明。
)

// Output 保存有界最终正文。
type Output struct {
	Status      OutputStatus // Status 是正文可用性。
	Text        string       // Text 是满足预算的单条最终消息正文。
	Reliability Reliability  // Reliability 是最终答案证明。
}

// Usage 区分本轮用量和最后上下文快照。
type Usage struct {
	Known             bool  // Known 表示本轮原生用量已报告，false 时不能把零值作为真实用量。
	InputTokens       int64 // InputTokens 是协议报告的本轮输入部分；缓存组成按对应字段保留。
	OutputTokens      int64 // OutputTokens 是本轮输出用量。
	CachedReadTokens  int64 // CachedReadTokens 是本轮缓存读取的输入用量。
	CachedWriteTokens int64 // CachedWriteTokens 是本轮缓存写入的输入用量。
	ContextKnown      bool  // ContextKnown 表示最后上下文快照已报告。
	ContextTokens     int64 // ContextTokens 是最后一次模型请求的上下文快照，不累计回合用量。
}

// Configuration 保存原生目录或配置写入回执确认的实际选择。
type Configuration struct {
	ActualModel       string // ActualModel 是原生确认的模型。
	ActualReasoning   string // ActualReasoning 是原生确认的推理档位。
	ActualMode        string // ActualMode 是原生确认的权限模式。
	ThinkingDisabled  bool   // ThinkingDisabled 只在确认的原生档位明确关闭推理时为 true。
	ReasoningFallback string // ReasoningFallback 记录无法关闭推理时的实际降级。
}

// Effects 保留已发生的事实，不能用于隐式重试。
type Effects struct {
	PromptAccepted bool // PromptAccepted 表示输入已经交付。
	ToolActivity   bool // ToolActivity 表示观察到工具调用。
	OutcomeUnknown bool // OutcomeUnknown 表示取消或故障后副作用未知。
}

// Result 区分执行、输出与交付事实。
type Result struct {
	Invocation    InvocationRef      // Invocation 是本次调用身份。
	Status        ExecutionStatus    // Status 是技术执行状态。
	StopReason    StopReason         // StopReason 是技术结束原因。
	Output        Output             // Output 是最终答案。
	Fault         *Fault             // Fault 是执行或准备故障。
	DeliveryFault *Fault             // DeliveryFault 是观察交付故障。
	CleanupFault  *Fault             // CleanupFault 是资源释放故障。
	Usage         Usage              // Usage 是真实驱动用量。
	Configuration Configuration      // Configuration 是原生确认的技术选择。
	Effects       Effects            // Effects 是真实执行和副作用证据。
	Checkpoint    *SessionCheckpoint // Checkpoint 只在具有真实恢复能力时生成。
}
