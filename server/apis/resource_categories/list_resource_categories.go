package resource_categories

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取资源分类
type ListResourceCategories struct {
	ginx.MethodGet
	// 资源类型
	Type constants.ResourceCategoryType `in:"query" name:"type" validate:"required"`
}

// Path 返回共享分类选项接口。
func (*ListResourceCategories) Path() string { return "" }

// Output 返回当前类型已有分类，不从其他类型或作者声明派生。
func (o *ListResourceCategories) Output(ctx *gin.Context) (interface{}, error) {
	if o.Type == constants.ResourceCategoryTypeUnspecified {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	items, err := global.ResourceCategoryControllerManager.List(ctx, o.Type)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return items, nil
}
