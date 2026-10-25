package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/neux-knowledge/remote"
)

// 远端资源目标
type RemoteTarget struct {
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
}

// 远端页面查询
type RemotePageQuery struct {
	// 语义地址
	Slug string `in:"query" name:"slug"`
}

// 远端整理请求
type RemoteOrganizeInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 当前版本
	ExpectedVersion int64 `json:"expectedVersion" validate:"min=1"`
	// 目录版本
	StructureVersion int64 `json:"structureVersion" validate:"min=1"`
	// 来源版本
	SourceVersionIDs []string `json:"sourceVersionIds"`
	// 服务器配置
	ProfileID string `json:"profileId"`
	// 重建原件
	Rebuild bool `json:"rebuild,omitempty"`
	// 整理要求
	Instruction string `json:"instruction,omitempty" validate:"max=65536"`
}

// 远端页面保存请求
type RemoteWritePageInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 当前版本
	ExpectedVersion int `json:"expectedVersion" validate:"min=0"`
	// 目录版本
	StructureVersion int64 `json:"structureVersion" validate:"min=1"`
	// 页面内容
	Page knowledge.Page `json:"page"`
}

// 远端资源能力
type RemoteCapabilities struct {
	// 服务标识
	ServiceKind string `json:"serviceKind"`
	// 服务版本
	ServiceVersion string `json:"serviceVersion"`
	// 上传行为
	UploadMode string `json:"uploadMode"`
	// 连接版本
	ConnectionRevision int64 `json:"connectionRevision"`
	// 识别依据
	Evidence []string `json:"evidence"`
	// 资源引用
	Ref knowledge.ResourceRef `json:"ref"`
	// 协议版本
	Profile string `json:"profile"`
	// 可用操作
	Operations map[string]bool `json:"operations"`
	// 操作权限
	Permissions map[string]string `json:"permissions"`
	// 能力说明
	Reasons map[string]string `json:"reasons"`
	// 能力明细
	Details map[string]RemoteCapabilityStatus `json:"details"`
	// 服务器配置
	Profiles []RemoteProfile `json:"profiles"`
}

// 远端能力状态
type RemoteCapabilityStatus struct {
	// 协议支持
	Supported bool `json:"supported"`
	// 主体权限
	Permission string `json:"permission"`
	// 资源条件
	Condition string `json:"condition"`
}

// 服务器执行配置
type RemoteProfile struct {
	// 配置标识
	ID string `json:"id"`
	// 配置名称
	Name string `json:"name"`
}

// 远端主题
type RemoteTheme struct {
	knowledge.Theme
	// 子主题存在
	HasChildren bool `json:"hasChildren"`
	// 直接页面数
	PageCount int64 `json:"pageCount"`
}

// 远端主题列表
type RemoteThemeList struct {
	// 完整目录
	Complete bool `json:"complete"`
	// 上级标识
	ParentID string `json:"parentId"`
	// 主题列表
	Items []RemoteTheme `json:"items"`
}

// 远端页面修订
type RemoteRevision struct {
	knowledge.Revision
	// 正文已读取
	ContentLoaded bool `json:"contentLoaded"`
}

// 远端修订列表
type RemoteRevisionList struct {
	// 修订列表
	Items []RemoteRevision `json:"items"`
	// 修订总数
	Total int64 `json:"total"`
	// 当前版本
	CurrentVersion int `json:"currentVersion"`
}

// 远端维护请求
type RemoteMaintenanceInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 当前版本
	ExpectedVersion int64 `json:"expectedVersion" validate:"min=1"`
	// 目录版本
	StructureVersion int64 `json:"structureVersion" validate:"min=1"`
	// 服务器配置
	ProfileID string `json:"profileId"`
	// 维护要求
	Instruction string `json:"instruction" validate:"required"`
	// 页面标识
	PageID string `json:"pageId" validate:"required"`
	// 页面版本
	ExpectedPageVersion int `json:"expectedPageVersion" validate:"min=1"`
}

// 远端关系图
type RemoteGraph struct {
	// 页面句柄
	Nodes []remote.GraphNode `json:"nodes"`
	// 定向关系
	Edges []remote.GraphEdge `json:"edges"`
	// 覆盖范围
	Meta remote.GraphMeta `json:"meta"`
}

// 远端操作引用
type RemoteOperation struct {
	// 发送事实
	TransferState constants.RemoteTransferState `json:"transferState"`
	// 上传模式
	UploadMode string `json:"uploadMode"`
	// 原连接版本
	ConnectionRevision int64 `json:"connectionRevision"`
	// 原生接纳
	NativeUpload bool `json:"nativeUpload"`
	// 资源引用
	Ref knowledge.ResourceRef `json:"ref"`
	// 请求标识
	RequestID string `json:"requestId"`
	// 操作类型
	Operation string `json:"operation"`
	// 回执已确认
	ReceiptConfirmed bool `json:"receiptConfirmed"`
	// 远端明确拒绝
	Rejected bool `json:"rejected"`
	// 稳定错误码
	ErrorCode int64 `json:"errorCode"`
	// 资源标识
	ResourceID string `json:"resourceId"`
	// 任务标识
	JobID string `json:"jobId"`
}

// 待确认远端操作
type RemoteOperationList struct {
	// 操作引用
	Items []RemoteOperation `json:"items"`
	// 待确认总数
	Total int64 `json:"total"`
}
