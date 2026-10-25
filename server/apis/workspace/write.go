package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/internal/workspacefiles"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 文件保存请求
type WriteRequest struct {
	// 范围标识
	ScopeID string `json:"scopeId" validate:"required"`
	// 相对路径
	RelativePath string `json:"relativePath" validate:"required"`
	// 授权根目录
	Root string `json:"root"`
	// 文件内容
	Content string `json:"content"`
	// 预期版本
	ExpectedVersion string `json:"expectedVersion" validate:"required"`
	// 编辑修订
	SaveRevision int `json:"saveRevision"`
}

// 文件保存响应
type WriteResponse struct {
	// 文件信息
	Resource workspacefiles.Resource `json:"resource"`
	// 编辑修订
	SaveRevision int `json:"saveRevision"`
}

// 保存工作文件
type WriteFile struct {
	// 提交方法
	ginx.MethodPost
	// 请求体
	Body WriteRequest `in:"body"`
}

// Path 返回工作文件保存地址。
func (*WriteFile) Path() string { return "/write" }

// Output 返回编辑器后续保存所需的版本，拒绝覆盖外部改动。
func (g *WriteFile) Output(ctx *gin.Context) (interface{}, error) {
	resource, err := files.Write(middleware.AuthSubject(ctx), g.Body.ScopeID, g.Body.Root, g.Body.RelativePath, g.Body.Content, g.Body.ExpectedVersion)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return WriteResponse{Resource: resource, SaveRevision: g.Body.SaveRevision}, nil
}
