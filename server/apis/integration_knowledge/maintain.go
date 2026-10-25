package integration_knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
	knowledgeservice "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/httpapi"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 公开页面修订要求
type PublishedMaintenanceInput struct {
	// 请求标识
	RequestID string `json:"request_id,omitempty"`
	// 已读库版本
	ExpectedVersion int64 `json:"expected_version" validate:"min=1"`
	// 已读目录版本
	StructureVersion int64 `json:"structure_version" validate:"min=1"`
	// 已读页面版本
	ExpectedPageVersion int `json:"expected_page_version" validate:"min=1"`
	// 修订要求
	Instruction string `json:"instruction" validate:"required,max=16000"`
}

// 修订公开知识页面
type PublishedMaintain struct {
	ginx.MethodPost
	// 知识库标识
	LibraryID string `in:"path" name:"kb_id" validate:"required"`
	// 页面标识
	PageID string `in:"path" name:"pageId" validate:"required"`
	// 修订内容
	Body PublishedMaintenanceInput `in:"body"`
}

func (*PublishedMaintain) Path() string { return "/knowledgebase/:kb_id/wiki/maintenance/:pageId" }
func (o *PublishedMaintain) Output(ctx *gin.Context) (interface{}, error) {
	d := dependencies(ctx)
	if d.knowledge == nil || d.options.References == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	original, request, err := scopedRequest(ctx, o.LibraryID, o.Body.RequestID)
	if err != nil {
		return nil, err
	}
	auth, _ := middleware.IntegrationAuthorization(ctx.Request.Context())
	lib, err := d.engine.GetLibrary(ctx.Request.Context(), o.LibraryID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	selected, err := d.options.References.LibraryExecutionDefaults(ctx.Request.Context(), o.LibraryID)
	if err != nil {
		return nil, err
	}
	selected, err = d.knowledge.ResolveModel(ctx.Request.Context(), selected)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(integrationservice.KnowledgeRecovery{Origin: "direct_api", Subject: auth.Identity.KeyID, Permission: core.PermissionSnapshot{PolicyVersion: lib.PolicyVersion, Scope: auth.Scope, Operations: []string{"organize"}}, Authorization: auth})
	adapter := knowledgeservice.RemoteAdapter{Service: d.knowledge, Profiles: knowledgeservice.ProfileMap{"default": {RuntimeID: selected.RuntimeID, AccountID: selected.AccountID, Model: selected.Model}}}
	receipt, err := adapter.Maintain(ctx.Request.Context(), httpapi.MaintenanceRequest{RequestID: request, LibraryID: o.LibraryID, ExpectedVersion: o.Body.ExpectedVersion, StructureVersion: o.Body.StructureVersion, PageID: o.PageID, ExpectedPageVersion: o.Body.ExpectedPageVersion, Instruction: o.Body.Instruction, ProfileID: "default"}, httpapi.Authorization{Subject: auth.Identity.KeyID, RecoveryData: raw, PublishGuard: func(_ context.Context) (func(), error) { return d.auth.AcquireGuard(ctx.Request.Context(), auth) }})
	if err != nil {
		var api *httpapi.APIError
		if errors.As(err, &api) {
			switch api.Status {
			case 400:
				return nil, status_error.KnowledgeInvalid
			case 403:
				return nil, status_error.IntegrationForbidden
			case 404:
				return nil, status_error.KnowledgeNotFound
			case 409:
				return nil, status_error.KnowledgeConflict
			default:
				return nil, status_error.KnowledgeUnsupported
			}
		}
		return nil, bridge.MapError(err)
	}
	return PublishedJobAcceptance{Receipt: publicReceipt(receipt, original)}, nil
}
