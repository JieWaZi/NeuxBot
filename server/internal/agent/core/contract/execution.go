// Package contract 定义不依赖宿主与具体协议的执行契约。
package contract

import (
	"context"
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

// Effects 保留已发生的事实，不能用于隐式重试。
type Effects struct {
	PromptAccepted bool // PromptAccepted 表示输入已经交付。
	ToolActivity   bool // ToolActivity 表示观察到工具调用。
	OutcomeUnknown bool // OutcomeUnknown 表示取消或故障后副作用未知。
}

// NativeSessionInfo 聚合公开适配器实际报告的原生会话能力与位置。
type NativeSessionInfo struct {
	Handle       string // Handle 是原生返回的会话身份。
	Resumable    bool   // Resumable 表示本次原生握手明确支持恢复或加载。
	ForkMode     string // ForkMode 是公开元数据声明的原生分叉方式。
	ForkPosition string // ForkPosition 是本轮公开回执中的原生分叉位置。
}

// Result 区分执行、输出与交付事实。
type Result struct {
	Invocation    InvocationRef     // Invocation 是本次调用身份。
	Status        ExecutionStatus   // Status 是技术执行状态。
	StopReason    StopReason        // StopReason 是技术结束原因。
	Output        Output            // Output 是最终答案。
	Fault         *Fault            // Fault 是执行或准备故障。
	DeliveryFault *Fault            // DeliveryFault 是观察交付故障。
	CleanupFault  *Fault            // CleanupFault 是资源释放故障。
	Usage         Usage             // Usage 是真实驱动用量。
	Configuration Configuration     // Configuration 是原生确认的技术选择。
	Effects       Effects           // Effects 是真实执行和副作用证据。
	NativeSession NativeSessionInfo // NativeSession 是本轮可持久化的无秘密原生事实。
}

// SessionConfiguration 保存不含秘密的原生启动配置，兼容摘要由 Runtime 统一计算。
type SessionConfiguration struct {
	Binary            string            // Binary 是固定可执行文件选择。
	Arguments         []string          // Arguments 是固定前置参数。
	ConfigHome        string            // ConfigHome 是用户选择的原生配置目录。
	PluginDirectories []string          // PluginDirectories 是固定原生插件目录。
	PluginScope       string            // PluginScope 是外部原生插件的范围事实。
	Options           map[string]string // Options 是非秘密原生配置覆盖。
	SettingsPath      string            // SettingsPath 是固定模型设置文件。
}

// SessionPolicies 保存影响原生会话兼容性的授权与配置策略。
type SessionPolicies struct {
	Tools        ToolPolicy         // Tools 是会话有效工具授权。
	NativeConfig NativeConfigPolicy // NativeConfig 是原生配置继承范围。
}

// SessionPlan 只保存稳定配置事实，不包含本轮输入、函数或明文凭据。
type SessionPlan struct {
	Profile       Profile              // Profile 是固定技术选择与账号主体。
	Workspace     WorkspaceScope       // Workspace 是宿主冻结的授权工作范围。
	Contributions Contributions        // Contributions 是按稳定顺序声明的静态能力。
	Policies      SessionPolicies      // Policies 是影响会话兼容性的策略。
	ResourceKey   string               // ResourceKey 是宿主冻结的账号及资源配置版本。
	Configuration SessionConfiguration // Configuration 是无秘密原生启动选择。
	Grants        Grants               // Grants 是宿主冻结的权限上界。
	Tags          []string             // Tags 标记会话保护的账号、插件等资源身份。
}

// SessionLifetime 指定技术消费者在回合结束后的保留范围。
type SessionLifetime string

const (
	SessionPersistent SessionLifetime = ""          // SessionPersistent 在能力及终态允许时保留热会话。
	SessionTemporary  SessionLifetime = "temporary" // SessionTemporary 在本轮终态前释放消费者及依赖资源。
)

// PreparationMode 区分普通回合与同一原生上下文上的只读交接。
type PreparationMode string

const (
	PreparationContinue        PreparationMode = ""                  // PreparationContinue 使用普通会话配置兼容规则。
	PreparationReadOnlyHandoff PreparationMode = "read_only_handoff" // PreparationReadOnlyHandoff 冷恢复只读上下文，完成后关闭临时消费者并保留普通配置事实。
)

// PrepareRequest 占用会话，调用者在冷路径顺序取得并绑定资源。
type PrepareRequest struct {
	Plan     SessionPlan      // Plan 是尚未取得实际资源的稳定配置声明。
	Session  SessionDirective // Session 是新建、热复用或原生句柄恢复要求。
	Mode     PreparationMode  // Mode 指定普通准备或只读交接。
	Lifetime SessionLifetime  // Lifetime 是本轮消费者的保留策略。
}

// SessionResources 在首次冷 Bind 时交接资源，验证错误不退回所有权。
// 重复 Bind 或消费句柄后 Bind 属于调用方违约，不转移新资源。
// 部分失败也必须先 Bind 再 Close，不能在宿主提前释放依赖。
type SessionResources struct {
	ActualPlan    *SessionPlan      // ActualPlan 是资源解析后的实际稳定配置，nil 保留声明。
	Session       *SessionDirective // Session 允许实际资源要求新建，不替换宿主持久化原生身份。
	Driver        Driver            // Driver 是本会话独占的驱动，随会话释放。
	Resources     ResourceLease     // Resources 保护物化资源和宿主只读快照。
	Credential    Credential        // Credential 是仅供原生启动的秘密。
	CloseAfterRun bool              // CloseAfterRun 是实际资源要求的额外关闭约束，不能取消只读交接或临时策略。
}

// TurnInput 保存本轮增量输入、预算与交互端口，不参与会话兼容摘要。
type TurnInput struct {
	Input       Input             // Input 是本轮新增正文及资源选择。
	Seed        []SeedMessage     // Seed 只在实际新建会话时播种，恢复会话忽略此字段。
	Limits      Limits            // Limits 是本轮有界预算，零值使用引擎默认。
	Interaction InteractionPolicy // Interaction 是本轮人工交互方式。
	Output      OutputPolicy      // Output 是本轮最终回答证据策略。
	Options     RunOptions        // Options 是本轮观察、交互和人工等待端口。
}

// PreparedRun 是 Runtime 已占用的唯一回合，调用者取得后必须 defer Close。
// 冷路径使用 Context 取得资源，Bind 仅交接所有权，Run 才打开原生会话并提交输入。
type PreparedRun interface {
	Context() context.Context
	NeedsResources() bool
	Bind(context.Context, SessionResources) error
	Invocation() InvocationRef
	Plan() SessionPlan
	Session() SessionDirective
	Resources() ResourceLease
	Run(context.Context, TurnInput) Result
	Close(context.Context) error
}
