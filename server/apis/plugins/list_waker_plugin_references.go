package plugins

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取伙伴插件引用
type ListWakerPluginReferences struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
}

// Path 返回当前伙伴输入区的轻量插件候选入口。
func (*ListWakerPluginReferences) Path() string { return "/:wakerId/plugin-references" }

// Output 固定三个窄查询的一致版本，不执行管理详情或授权健康检查。
func (o *ListWakerPluginReferences) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := global.WakerControllerManager.Get(ctx, o.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	result := []models.PluginReference{}
	err := global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		var err error
		result, err = global.PluginControllerManager.InstalledReferences(txctx, o.WakerID)
		return err
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}
