package plugins

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取历史插件图标
type ListPluginReferenceIcons struct {
	ginx.MethodGet
	// 插件标识
	PluginIDs []string `in:"query" name:"pluginId" validate:"required,min=1,max=100,dive,required,max=32"`
}

// 插件引用图标
type PluginReferenceIcon struct {
	// 插件标识
	ID string `json:"id"`
	// 插件图标
	Icon string `json:"icon,omitempty"`
	// 主题图标
	Icons *packageplugin.ThemeValues `json:"icons,omitempty"`
}

// Path 返回历史插件引用共享的轻量图标入口。
func (*ListPluginReferenceIcons) Path() string { return "/reference-icons" }

// Output 只读取当前仍保留的资源及其公开图标，略过已删除资源。
func (o *ListPluginReferenceIcons) Output(ctx *gin.Context) (interface{}, error) {
	var refs []models.PluginReference
	err := global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		var err error
		refs, err = global.PluginControllerManager.ReferenceIcons(txctx, o.PluginIDs)
		return err
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	projectPluginReferenceIcons(refs)
	result := make([]PluginReferenceIcon, 0, len(refs))
	for _, ref := range refs {
		result = append(result, PluginReferenceIcon{ID: ref.ID, Icon: ref.Icon, Icons: ref.Icons})
	}
	return result, nil
}
