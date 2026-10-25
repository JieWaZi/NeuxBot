package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 保存知识库执行偏好请求
type UpdateLibraryExecutionDefaultsInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 当前版本
	ExpectedVersion int64 `json:"expectedVersion" validate:"min=0"`
	// 执行选择
	LibraryExecutionDefaultsInput
}

// 保存知识库执行偏好
type UpdateLibraryExecutionDefaults struct {
	ginx.MethodPut
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 请求体
	Body UpdateLibraryExecutionDefaultsInput `in:"body"`
}

func (o *UpdateLibraryExecutionDefaults) Path() string {
	return "/libraries/:libraryId/execution-defaults"
}
func (o *UpdateLibraryExecutionDefaults) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	library, err := global.KnowledgeEngine.GetLibrary(ctx, o.LibraryID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	// 本接口只拥有本地可整理通用库；归档和退役类型先拒绝，不能先提交宿主偏好再等待审计失败。
	if library.Archived || library.Kind != "general" {
		return nil, status_error.KnowledgeForbidden
	}
	saved, replayed, err := global.KnowledgeControllerManager.ReplayLibraryExecutionDefaults(ctx, o.Body.RequestID, o.Body.ExpectedVersion, libraryExecutionDefaultsValues(o.LibraryID, o.Body.LibraryExecutionDefaultsInput))
	if err != nil {
		return nil, err
	}
	if !replayed {
		selection, resolveErr := resolveLibraryExecutionDefaults(ctx, o.LibraryID, o.Body.LibraryExecutionDefaultsInput)
		if resolveErr != nil {
			return nil, resolveErr
		}
		saved, err = global.KnowledgeControllerManager.UpdateLibraryExecutionDefaults(ctx, o.Body.RequestID, o.Body.ExpectedVersion, selection)
		if err != nil {
			return nil, err
		}
	}
	if err = global.KnowledgeEngine.RecordLibraryExecutionDefaultsActivity(bridge.LocalUserActivity(ctx), o.LibraryID, o.Body.RequestID); err != nil {
		return nil, bridge.MapError(err)
	}
	if global.EventHub != nil {
		_ = events.Emit(global.EventHub, "knowledge", "knowledge.changed", events.Resource{Kind: "knowledge_library", ID: o.LibraryID, ParentID: "local"}, map[string]any{"ref": bridge.LocalRef(o.LibraryID), "requestId": o.Body.RequestID})
	}
	return saved, nil
}
