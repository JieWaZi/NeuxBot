package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 文件删除请求
type DeleteRequest struct {
	// 范围标识
	ScopeID string `json:"scopeId" validate:"required"`
	// 相对路径
	RelativePath string `json:"relativePath" validate:"required"`
	// 授权根目录
	Root string `json:"root"`
	// 预期版本
	ExpectedVersion string `json:"expectedVersion" validate:"required"`
}

// 删除工作文件
type DeleteFile struct {
	// 提交方法
	ginx.MethodPost
	// 请求体
	Body DeleteRequest `in:"body"`
}

// Path 返回工作文件删除地址。
func (*DeleteFile) Path() string { return "/delete" }

// Output 删除已获准的普通文件。
func (g *DeleteFile) Output(ctx *gin.Context) (interface{}, error) {
	if err := files.Delete(middleware.AuthSubject(ctx), g.Body.ScopeID, g.Body.Root, g.Body.RelativePath, g.Body.ExpectedVersion); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
