package memory

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 设置核心记忆
type SetMemoryCore struct {
	ginx.MethodPut
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 记忆标识
	MemoryID string `in:"path" name:"memoryId" validate:"required"`
	// 请求体
	Body SetMemoryCoreRequest `in:"body"`
}

// 核心记忆设置
type SetMemoryCoreRequest struct {
	// 核心记忆
	Core *bool `json:"core" validate:"required"`
}

func (s *SetMemoryCore) Path() string { return "/wakers/:wakerId/memories/:memoryId/core" }

// Output 将指定条目移入核心记忆或普通记忆。
func (s *SetMemoryCore) Output(ctx *gin.Context) (interface{}, error) {
	if s.Body.Core == nil {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	if _, err := partnerMemoryContext(ctx, s.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.MemoryService.SetCore(ctx, s.WakerID, s.MemoryID, *s.Body.Core); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
