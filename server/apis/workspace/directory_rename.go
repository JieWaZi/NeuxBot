package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// RenameDirectoryRequest 是工作区操作参数。
type RenameDirectoryRequest struct {
	// 范围标识
	ScopeID string `json:"scopeId" validate:"required"`
	// 授权根目录
	Root string `json:"root"`
	// 相对路径
	RelativePath string `json:"relativePath" validate:"required"`
	// 新名称
	NextName string `json:"nextName" validate:"required"`
}

// RenameDirectory 处理工作区操作。
type RenameDirectory struct {
	// 提交方法
	ginx.MethodPost
	// 请求体
	Body RenameDirectoryRequest `in:"body"`
}

// Path 返回操作地址。
func (*RenameDirectory) Path() string { return "/directory/rename" }

// Output 在主体范围内执行操作。
func (g *RenameDirectory) Output(ctx *gin.Context) (interface{}, error) {
	relative, err := files.RenameDirectory(middleware.AuthSubject(ctx), g.Body.ScopeID, g.Body.Root, g.Body.RelativePath, g.Body.NextName)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return PathResponse{RelativePath: relative}, nil
}
