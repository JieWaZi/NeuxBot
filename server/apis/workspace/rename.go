package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 文件改名请求
type RenameRequest struct {
	// 范围标识
	ScopeID string `json:"scopeId" validate:"required"`
	// 相对路径
	RelativePath string `json:"relativePath" validate:"required"`
	// 授权根目录
	Root string `json:"root"`
	// 新文件名
	NextName string `json:"nextName" validate:"required"`
	// 预期版本
	ExpectedVersion string `json:"expectedVersion" validate:"required"`
}

// 重命名工作文件
type RenameFile struct {
	// 提交方法
	ginx.MethodPost
	// 请求体
	Body RenameRequest `in:"body"`
}

// Path 返回工作文件重命名地址。
func (*RenameFile) Path() string { return "/rename" }

// Output 返回新文件身份，供编辑器替换当前打开路径。
func (g *RenameFile) Output(ctx *gin.Context) (interface{}, error) {
	resource, err := files.Rename(middleware.AuthSubject(ctx), g.Body.ScopeID, g.Body.Root, g.Body.RelativePath, g.Body.NextName, g.Body.ExpectedVersion)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return resource, nil
}
