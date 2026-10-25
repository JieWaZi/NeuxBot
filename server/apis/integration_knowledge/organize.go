package integration_knowledge

import (
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
	knowledgeservice "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 整理公开知识原件
type PublishedOrganize struct {
	ginx.MethodPost
	// 知识库标识
	LibraryID string `in:"path" name:"id" validate:"required"`
	// 整理要求
	Body PublishedOrganizeInput `in:"body"`
}

// 公开整理要求
type PublishedOrganizeInput struct {
	// 请求标识
	RequestID string `json:"request_id,omitempty"`
	// 已读库版本
	ExpectedVersion int64 `json:"expected_version" validate:"min=1"`
	// 已读目录版本
	StructureVersion int64 `json:"structure_version" validate:"min=1"`
	// 明确来源版本
	SourceVersionIDs []string `json:"source_version_ids"`
	// 整理要求
	Instruction string `json:"instruction" validate:"max=16000"`
	// 重建原件
	Rebuild bool `json:"rebuild,omitempty"`
}

// 公开作业接纳结果
type PublishedJobAcceptance struct {
	// 接纳回执
	Receipt *core.Receipt `json:"receipt"`
	// 无新增来源
	Noop bool `json:"noop"`
}

func (*PublishedOrganize) Path() string { return "/knowledge-bases/:id/organize" }
func (o *PublishedOrganize) Output(ctx *gin.Context) (interface{}, error) {
	return organize(ctx, o.LibraryID, o.Body)
}

func organize(ctx *gin.Context, libraryID string, input PublishedOrganizeInput) (interface{}, error) {
	d := dependencies(ctx)
	if d.knowledge == nil || d.options.References == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	if len(input.SourceVersionIDs) > 1000 {
		return nil, status_error.KnowledgeInvalid
	}
	originalID, requestID, err := scopedRequest(ctx, libraryID, input.RequestID)
	if err != nil {
		return nil, err
	}
	auth, _ := middleware.IntegrationAuthorization(ctx.Request.Context())
	lib, err := d.engine.GetLibrary(ctx.Request.Context(), libraryID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	raw, err := json.Marshal(integrationservice.KnowledgeRecovery{Origin: "direct_api", Subject: auth.Identity.KeyID, Permission: core.PermissionSnapshot{PolicyVersion: lib.PolicyVersion, Scope: auth.Scope, Operations: []string{"organize"}}, Authorization: auth})
	if err != nil {
		return nil, status_error.KnowledgeInvalid
	}
	session := models.KnowledgeTaskConfiguration{ID: requestID, RemoteSubject: auth.Identity.KeyID, RemoteAuthorization: raw}
	// 已冻结任务沿用原授权信封，轮换只影响后续提交，不改写请求和成果归属。
	if old, e := d.options.References.GetTaskConfiguration(ctx.Request.Context(), requestID); e == nil {
		session.RemoteAuthorization = old.RemoteAuthorization
	}
	receipt, _, err := d.knowledge.Organize(core.WithActivityActor(ctx.Request.Context(), core.ActivityActor{UserID: auth.Identity.KeyID, Trigger: "api"}), knowledgeservice.OrganizeCommand{RequestID: requestID, LibraryID: libraryID, ExpectedVersion: input.ExpectedVersion, StructureVersion: input.StructureVersion, SourceVersionIDs: input.SourceVersionIDs, ExplicitSources: input.SourceVersionIDs != nil, Rebuild: input.Rebuild, Message: input.Instruction, Configuration: session})
	if err != nil {
		return nil, err
	}
	return PublishedJobAcceptance{Receipt: publicReceipt(receipt, originalID), Noop: receipt == nil || receipt.State == "no-op"}, nil
}
