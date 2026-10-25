package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&IMConnection{})
	dbhelper.RegisterTable(&IMBinding{})
	dbhelper.RegisterTable(&IMConversationLink{})
	dbhelper.RegisterTable(&IMInbox{})
	dbhelper.RegisterTable(&IMDelivery{})
}

// IMConnection 保存一份平台连接及后台收取游标，不公开登录凭据。
type IMConnection struct {
	// ID 是连接的稳定身份。
	ID string `gorm:"size:32;primaryKey" json:"id"`
	// Name 是用户填写的连接名称。
	Name string `json:"name"`
	// Platform 指定平台协议适配器。
	Platform constants.IMPlatform `json:"platform"`
	// Enabled 表示允许后台建立平台连接。
	Enabled bool `json:"enabled"`
	// KeepAwake 表示连接启用时请求系统允许熄屏但维持应用运行。
	KeepAwake bool `json:"keepAwake"`
	// State 是最近的连接事实；启动时重新探测。
	State constants.IMConnectionState `json:"state"`
	// ErrorCode 是可本地化的最近失败原因。
	ErrorCode string `json:"errorCode"`
	// AppID 是飞书应用或微信、企业微信机器人标识。
	AppID string `json:"appId"`
	// OwnerID 是扫码所有者的平台身份，用于单聊及群控制授权。
	OwnerID string `json:"ownerId"`
	// BaseURL 是经过官方域名校验的微信实际服务地址。
	BaseURL string `json:"-"`
	// CredentialCiphertext 是登录令牌、飞书应用密钥或企业微信机器人密钥的加密值。
	CredentialCiphertext string `json:"-"`
	// Credential 是仅在进程内交给适配器的解密值，不参与 ORM。
	Credential string `gorm:"-" json:"-"`
	// Cursor 是与整批收件原子提交的微信增量游标。
	Cursor string `json:"-"`
	// Revision 防止编辑及登录回调覆盖较新连接配置。
	Revision int64 `json:"revision"`
	// ConversationCount 是已建立内部对话的数量，仅供列表查询，不写入数据库。
	ConversationCount int64 `gorm:"->;-:migration" json:"conversationCount"`
	// CreatedAt 是连接创建时间。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是最近修改时间。
	UpdatedAt int64 `gorm:"autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回平台连接表名。
func (IMConnection) TableName() string { return "im_connections" }

// IMBinding 是聊天授权及投递目标，不复制内部对话历史。
type IMBinding struct {
	// ID 同时是模型可发现的投递目标身份。
	ID string `gorm:"size:32;primaryKey" json:"id"`
	// ConnectionID 限定聊天所属平台连接。
	ConnectionID string `gorm:"size:32;not null;uniqueIndex:im_chat_binding" json:"connectionId"`
	// ExternalChatID 是平台单聊或群聊身份。
	ExternalChatID string `gorm:"not null;uniqueIndex:im_chat_binding" json:"externalChatId"`
	// Kind 区分平台单聊与群聊。
	Kind constants.IMChatKind `json:"kind"`
	// Name 是显示及模型发现使用的聊天名称。
	Name string `json:"name"`
	// State 是桌面管理员批准的聊天授权状态。
	State constants.IMBindingState `json:"state"`
	// WakerID 指定独立伙伴，团队目标时为空。
	WakerID string `json:"wakerId"`
	// ChannelID 指定既有团队，独立伙伴目标时为空。
	ChannelID string `json:"channelId"`
	// ProjectID 是由桌面批准的项目，使用受管临时目录时为空。
	ProjectID string `json:"projectId"`
	// WorkingDirectory 是桌面批准的真实目录，IM 不能自行修改。
	WorkingDirectory string `json:"workingDirectory"`
	// Options 保存管理员选择的智能体、模型、账号与权限。
	Options IMExecutionOptions `gorm:"serializer:json;type:text" json:"options"`
	// AllowWorkflowDelivery 表示管理员允许工作流发现并投递到此聊天。
	AllowWorkflowDelivery bool `json:"allowWorkflowDelivery"`
	// ActiveLinkID 是该聊天共同使用的当前对话指针，空值表示下一条任务新建。
	ActiveLinkID string `json:"activeLinkId"`
	// ControllerIDs 指定可切换或停止共享对话的群管理员，不限制普通成员发起任务。
	ControllerIDs []string `gorm:"serializer:json;type:text" json:"controllerIds"`
	// ContextCiphertext 加密保存微信最近发送上下文，管理接口不可读取。
	ContextCiphertext string `json:"-"`
	// ContextToken 是仅供本次发送解密的微信上下文。
	ContextToken string `gorm:"-" json:"-"`
	// Revision 是授权和指针的并发版本。
	Revision int64 `json:"revision"`
	// CreatedAt 是首次发现聊天的时间。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是最近授权或上下文变化的时间。
	UpdatedAt int64 `gorm:"autoUpdateTime:milli" json:"updatedAt"`
}

// IMExecutionOptions 保存聊天新建时冻结的执行选择。
type IMExecutionOptions struct {
	// RuntimeID 是选择的智能体环境。
	RuntimeID string `json:"runtimeId"`
	// AccountID 是选择的账号，空值沿用智能体设置。
	AccountID string `json:"accountId"`
	// Model 是选择的模型。
	Model string `json:"model"`
	// Reasoning 是选择的推理强度。
	Reasoning string `json:"reasoning"`
	// PermissionLevel 是工具执行权限，不受群接入许可替代。
	PermissionLevel constants.PermissionLevel `json:"permissionLevel"`
}

// TableName 返回聊天授权表名。
func (IMBinding) TableName() string { return "im_bindings" }

// IMConversationLink 保存一段 IM 对话与标准 Chat 或 Team Thread 的稳定关系。
type IMConversationLink struct {
	// ID 是可供 /use 选择的会话身份。
	ID string `gorm:"size:32;primaryKey" json:"id"`
	// BindingID 限定可查询和继续的聊天授权范围。
	BindingID string `gorm:"size:32;index" json:"bindingId"`
	// ChatID 是实际伙伴 Chat 或团队负责人的主 Chat。
	ChatID string `gorm:"index" json:"chatId"`
	// ThreadID 是团队对话身份，独立伙伴时为空。
	ThreadID string `gorm:"index" json:"threadId"`
	// TurnCount 是实际用户输入消息的数量，由权威会话日志统计，不写入数据库。
	TurnCount int64 `gorm:"-" json:"turnCount"`
	// Title 是显示用的本地标题。
	Title string `json:"title"`
	// Configuration 冻结该会话的执行目标及工作范围，不随绑定编辑变化。
	Configuration IMBinding `gorm:"serializer:json;type:text" json:"-"`
	// PendingReportIDs 是已发现但尚未决定直达或协调的成员报告。
	PendingReportIDs []string `gorm:"serializer:json;type:text" json:"-"`
	// EventCursor 是负责人主 Chat 已可靠投递登记的持久事件游标。
	EventCursor int64 `json:"-"`
	// CreatedAt 是会话选择第一次成为实际工作的时间。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
}

// TableName 返回对话关联表名。
func (IMConversationLink) TableName() string { return "im_conversation_links" }

// IMFile 保存平台媒体定位及下载后受管路径，不允许外部指定本机文件。
type IMFile struct {
	// Digest 是冻结发送文件字节的 SHA-256，不用于媒体凭据。
	Digest string `json:"digest,omitempty"`
	// Name 是经过文件名清理的附件名称。
	Name string `json:"name"`
	// Kind 区分图片、文件、语音和视频。
	Kind string `json:"kind"`
	// Key 是平台媒体资源的稳定定位。
	Key string `json:"key"`
	// AESKey 是微信媒体解密参数，仅保存在加密收件载荷中，媒体暂存后清除。
	AESKey string `json:"aesKey,omitempty"`
	// Path 是下载或投递暂存后的受管路径。
	Path string `json:"path,omitempty"`
}

// IMInbox 在平台 ACK 前保存外部输入，重启后恢复处理。
type IMInbox struct {
	// ID 是连接范围内外部消息派生的去重身份。
	ID string `gorm:"size:32;primaryKey" json:"id"`
	// ConnectionID 是消息所属平台连接。
	ConnectionID string `gorm:"size:32;index" json:"connectionId"`
	// BindingID 是收取时发现的聊天授权。
	BindingID string `gorm:"size:32;index" json:"bindingId"`
	// ExternalMessageID 是平台消息身份，用于引用回复和资源下载。
	ExternalMessageID string `json:"externalMessageId"`
	// ExternalChatID 是不可由模型改变的实际聊天接收方。
	ExternalChatID string `json:"externalChatId"`
	// SenderID 保存真实平台发起人，群授权不抹去作者。
	SenderID string `json:"senderId"`
	// QuoteMessageID 是平台引用的原消息，可在同绑定内定位历史会话。
	QuoteMessageID string `json:"quoteMessageId"`
	// Content 是用户提交的规范化文字。
	Content string `gorm:"type:text" json:"content"`
	// FilesCiphertext 加密保存平台媒体下载能力及受管暂存路径。
	FilesCiphertext string `json:"-"`
	// Files 是仅供进程内下载和执行准备使用的解密附件。
	Files []IMFile `gorm:"-" json:"-"`
	// State 是收件路由及实际执行接纳阶段。
	State constants.IMInboxState `gorm:"index" json:"state"`
	// LinkID 是已冻结的对话身份，后续 /new 不改变已有输入。
	LinkID string `json:"linkId"`
	// ExecutionID 是该输入预分配并可从权威日志对账的执行身份。
	ExecutionID string `json:"executionId"`
	// ErrorCode 是处理失败或授权撤销的原因。
	ErrorCode string `json:"errorCode"`
	// ReceivedAt 是平台消息的真实发送时间，无时间证据时用接收时间。
	ReceivedAt int64 `json:"receivedAt"`
	// CreatedAt 是本机持久收取时间。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
}

// TableName 返回耐久收件表名。
func (IMInbox) TableName() string { return "im_inbox" }

// IMDelivery 保存恢复发送和幂等所需的冻结内容及当前状态，不另存发送历史。
type IMDelivery struct {
	// ID 是投递的稳定身份，用于派生稳定的片段发送标识。
	ID string `gorm:"size:32;primaryKey" json:"id"`
	// RequestID 是调用者范围内稳定复用的去重身份。
	RequestID string `gorm:"uniqueIndex;not null" json:"-"`
	// RequestHash 拒绝相同请求身份改变正文或目标。
	RequestHash string `json:"-"`
	// BindingID 指向管理员批准的接收聊天。
	BindingID string `gorm:"size:32;index" json:"bindingId"`
	// ConnectionID 冻结实际发送连接身份。
	ConnectionID string `json:"connectionId"`
	// ExternalChatID 冻结发送接收方，绑定编辑不会改变已有投递。
	ExternalChatID string `json:"externalChatId"`
	// LinkID 是原始 IM 工作的对话关系，主动投递可为空。
	LinkID string `json:"linkId"`
	// ChatID 是产生内容的 Chat，管理端可打开完整过程。
	ChatID string `json:"chatId"`
	// ExecutionID 是产生结果的真实执行身份。
	ExecutionID string `json:"executionId"`
	// WorkflowRunID 是工作流主动发送的来源运行。
	WorkflowRunID string `json:"workflowRunId"`
	// WorkflowDefinitionID 是来源工作流定义，管理端可打开对应运行。
	WorkflowDefinitionID string `json:"workflowDefinitionId"`
	// Content 是每次重发保持不变的完整发送正文。
	Content string `gorm:"type:text" json:"content"`
	// Files 是已经冻结到投递目录的实际文件。
	Files []IMFile `gorm:"serializer:json;type:text" json:"files"`
	// State 是外部发送的独立状态。
	State constants.IMDeliveryState `gorm:"index" json:"state"`
	// Receipts 保存各正文片段及文件的已确认平台消息身份。
	Receipts []string `gorm:"serializer:json;type:text" json:"receipts"`
	// NextPart 是下一条待发送片段，已经确认的片段不重复发送。
	NextPart int `json:"nextPart"`
	// Attempts 是进入平台发送的尝试次数。
	Attempts int `json:"attempts"`
	// ErrorCode 是最近失败或未知结果的稳定原因。
	ErrorCode string `json:"errorCode"`
	// CreatedAt 是投递登记时间。
	CreatedAt int64 `gorm:"autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是最近一次发送状态更新时间。
	UpdatedAt int64 `gorm:"autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回耐久发送队列表名。
func (IMDelivery) TableName() string { return "im_deliveries" }

// IMWorkflowDependency 描述当前工作流或活跃运行对一个连接内聊天的引用。
type IMWorkflowDependency struct {
	// DefinitionID 是受影响的工作流身份。
	DefinitionID string `json:"definitionId"`
	// RevisionID 是当前或运行冻结的不可变版本。
	RevisionID string `json:"revisionId"`
	// Title 是用户可识别的工作流名称。
	Title string `json:"title"`
	// NodeID 是引用聊天的源码节点身份。
	NodeID string `json:"nodeId"`
	// Label 是发送节点的业务名称。
	Label string `json:"label"`
	// RunID 非空时表示仍未结束的实际运行，禁止删除连接。
	RunID string `json:"runId,omitempty"`
}

// IMConnectionDependencies 是删除前读取且在删除事务中再次核验的真实影响清单。
type IMConnectionDependencies struct {
	// Workflows 仅包含当前版本及活跃运行的依赖，不包含纯历史引用。
	Workflows []IMWorkflowDependency `json:"workflows"`
	// PendingDeliveries 是尚未完成发送的消息数量。
	PendingDeliveries int64 `json:"pendingDeliveries"`
	// Digest 绑定用户已确认的依赖清单，引用变化后旧确认失效。
	Digest string `json:"digest"`
}
