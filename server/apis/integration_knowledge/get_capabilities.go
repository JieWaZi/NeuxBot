package integration_knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 集成能力明细
type CapabilityStatus struct {
	// 协议支持
	Supported bool `json:"supported"`
	// 实际授权
	Permission string `json:"permission"`
	// 可用条件
	Condition string `json:"condition"`
}

// 集成知识能力
type Capabilities struct {
	// 服务标识
	ServiceKind string `json:"service_kind"`
	// 协议版本
	ProtocolVersion string `json:"protocol_version"`
	// 处理模式
	UploadMode string `json:"upload_mode"`
	// 条件写保证
	AtomicConditionalWrite bool `json:"atomic_conditional_write"`
	// 原回执恢复
	ReceiptRecovery bool `json:"receipt_recovery"`
	// 可用操作
	Operations map[string]bool `json:"operations"`
	// 操作明细
	Details map[string]CapabilityStatus `json:"details"`
	// 兼容合同
	Profile string `json:"profile"`
	// 服务端配置
	Profiles []ExecutionProfile `json:"profiles"`
}

// 服务端执行配置
type ExecutionProfile struct {
	// 配置标识
	ID string `json:"id"`
	// 配置名称
	Name string `json:"name"`
}

// 读取知识 API 能力
type PublishedGetCapabilities struct {
	ginx.MethodGet
	// 知识库标识
	ID string `in:"path" name:"id" validate:"required"`
}

// Path 是能力扩展，不冒充 WeKnora 已有接口。
func (*PublishedGetCapabilities) Path() string {
	return "/knowledge-bases/:id/integration-capabilities"
}

// Output 分别返回协议支持、当前 Key 授权和执行条件，不泄漏账号与模型配置。
func (o *PublishedGetCapabilities) Output(ctx *gin.Context) (interface{}, error) {
	d := dependencies(ctx)
	identity, _ := middleware.IntegrationPrincipal(ctx.Request.Context())
	result := Capabilities{ServiceKind: "neuxally", ProtocolVersion: "1", UploadMode: "separate", AtomicConditionalWrite: true, ReceiptRecovery: true, Profile: "neuxally-integration-v1", Operations: map[string]bool{}, Details: map[string]CapabilityStatus{}, Profiles: []ExecutionProfile{}}
	for _, name := range []string{"read", "history", "themes", "sourceRead", "sourceDownload", "graph", "write", "rawOnly", "organize", "rebuild", "maintain", "themeWrite", "atomicRestore", "sourceArchive", "sourcePurge", "libraryDelete"} {
		required := []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve}
		supported, condition := true, "ready"
		switch name {
		case "write", "rawOnly", "themeWrite":
			required = append(required, constants.IntegrationCapabilityIngest)
		case "organize", "rebuild", "maintain":
			required = append(required, constants.IntegrationCapabilityOrganize)
			condition = d.knowledge.ExecutionCondition(ctx.Request.Context(), o.ID)
		case "atomicRestore", "sourceArchive", "sourcePurge", "libraryDelete":
			supported, condition = false, "not_published"
		}
		permission := "allowed"
		if _, err := d.auth.Authorize(ctx.Request.Context(), identity, constants.IntegrationResourceKindKnowledge, o.ID, required); err != nil {
			permission = "denied"
		}
		result.Details[name] = CapabilityStatus{Supported: supported, Permission: permission, Condition: condition}
		result.Operations[name] = supported && permission == "allowed" && condition == "ready"
	}
	return result, nil
}
