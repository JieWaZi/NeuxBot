package role_presets

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 预设列表
type ListRolePresets struct {
	ginx.MethodGet
	// 搜索关键字
	Query string `in:"query" name:"query"`
	// 页码
	PageNum int `in:"query" name:"pageNum"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize"`
}

// 预设列表响应
type ListRolePresetsResponse struct {
	// 资源列表
	Resources []models.RolePreset `json:"resources"`
	// 页码
	PageNum int `json:"pageNum"`
	// 每页数量
	PageSize int `json:"pageSize"`
	// 总数
	TotalSize int64 `json:"totalSize"`
}

// Path 返回角色预设列表接口路径。
func (l *ListRolePresets) Path() string { return "" }

// Validate 校验分页范围并补充默认值。
func (l *ListRolePresets) Validate(*gin.Context) error {
	if l.PageNum < 0 || l.PageSize < 0 || l.PageSize > 100 {
		return ginx.WithStack(status_error.BadRequest)
	}
	if l.PageNum == 0 {
		l.PageNum = 1
	}
	if l.PageSize == 0 {
		l.PageSize = 50
	}
	return nil
}

// Output 返回角色预设分页列表。
func (l *ListRolePresets) Output(ctx *gin.Context) (interface{}, error) {
	query := strings.TrimSpace(l.Query)
	resources, err := global.RolePresetControllerManager.List(
		ctx,
		query,
		l.PageSize,
		(l.PageNum-1)*l.PageSize,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	total, err := global.RolePresetControllerManager.Count(ctx, query)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ListRolePresetsResponse{
		Resources: resources,
		PageNum:   l.PageNum,
		PageSize:  l.PageSize,
		TotalSize: total,
	}, nil
}
