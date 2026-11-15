package wakers

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取伙伴
type GetWaker struct {
	// MethodGet 声明伙伴查询接口使用的 HTTP 方法。
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
}

// Path 返回伙伴详情接口路径。
func (g *GetWaker) Path() string {
	return "/:wakerId"
}

// Output 返回伙伴事实和档案展示投影。
func (g *GetWaker) Output(ctx *gin.Context) (interface{}, error) {
	waker, err := global.WakerControllerManager.Get(ctx, g.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	_, waker.IsDefault = builtinrolepreset.DefinitionByWakerID(waker.ID)
	return waker, nil
}
