package memory

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取记忆状态
type GetMemoryStatus struct {
	ginx.MethodGet
}

func (g *GetMemoryStatus) Path() string { return "/status" }

// Output 返回记忆服务状态。
func (g *GetMemoryStatus) Output(ctx *gin.Context) (interface{}, error) {
	status, err := global.MemoryService.Status(ctx)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return memoryStatusResource(status), nil
}
