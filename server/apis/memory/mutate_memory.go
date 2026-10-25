package memory

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建记忆
type CreateMemory struct {
	ginx.MethodPost
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 请求体
	Body MemoryMutationRequest `in:"body"`
}

// 记忆变更请求
type MemoryMutationRequest struct {
	// 记忆范围
	Scope constants.MemoryScope `json:"scope" validate:"required"`
	// 项目标识
	ProjectID string `json:"projectId,omitempty"`
	// 记忆正文
	Content string `json:"content" validate:"required"`
}

func (c *CreateMemory) Path() string { return "/wakers/:wakerId/memories" }

// Output 创建记忆。
func (c *CreateMemory) Output(ctx *gin.Context) (interface{}, error) {
	memoryContext, err := visibleScope(ctx, c.WakerID, c.Body.Scope, c.Body.ProjectID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	_, err = global.MemoryService.Write(ctx, memoryContext, c.Body.Scope, c.Body.Content)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

// 更新记忆
type UpdateMemory struct {
	ginx.MethodPut
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 记忆标识
	MemoryID string `in:"path" name:"memoryId" validate:"required"`
	// 请求体
	Body MemoryMutationRequest `in:"body"`
}

func (u *UpdateMemory) Path() string { return "/wakers/:wakerId/memories/:memoryId" }

// Output 更新记忆正文。
func (u *UpdateMemory) Output(ctx *gin.Context) (interface{}, error) {
	memoryContext, err := visibleScope(ctx, u.WakerID, u.Body.Scope, u.Body.ProjectID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.MemoryService.Update(ctx, memoryContext, u.MemoryID, u.Body.Content); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

// 删除记忆
type DeleteMemory struct {
	ginx.MethodDelete
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 记忆标识
	MemoryID string `in:"path" name:"memoryId" validate:"required"`
	// 记忆范围
	Scope constants.MemoryScope `in:"query" name:"scope" validate:"required"`
	// 项目标识
	ProjectID string `in:"query" name:"projectId"`
}

func (d *DeleteMemory) Path() string { return "/wakers/:wakerId/memories/:memoryId" }

// Output 遗忘记忆并保留时间线事件。
func (d *DeleteMemory) Output(ctx *gin.Context) (interface{}, error) {
	memoryContext, err := visibleScope(ctx, d.WakerID, d.Scope, d.ProjectID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.MemoryService.Forget(ctx, memoryContext, d.MemoryID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
