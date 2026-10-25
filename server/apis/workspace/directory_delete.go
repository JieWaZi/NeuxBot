package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// DeleteDirectoryRequest 是工作区操作参数。
type DeleteDirectoryRequest struct {
	// 范围标识
	ScopeID string `json:"scopeId" validate:"required"`
	// 授权根目录
	Root string `json:"root"`
	// 相对路径
	RelativePath string `json:"relativePath" validate:"required"`
}

// DeleteDirectory 处理工作区操作。
type DeleteDirectory struct {
	// 提交方法
	ginx.MethodPost
	// 请求体
	Body DeleteDirectoryRequest `in:"body"`
}

// Path 返回操作地址。
func (*DeleteDirectory) Path() string { return "/directory/delete" }

// Output 在主体范围内执行操作。
func (g *DeleteDirectory) Output(ctx *gin.Context) (interface{}, error) {
	if err := files.DeleteDirectory(middleware.AuthSubject(ctx), g.Body.ScopeID, g.Body.Root, g.Body.RelativePath); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
