package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// ResolveReferenceRequest 是工作区操作参数。
type ResolveReferenceRequest struct {
	// 范围标识
	ScopeID string `json:"scopeId" validate:"required"`
	// 引用路径
	Path string `json:"path" validate:"required"`
	// 基准目录
	BaseDirectory string `json:"baseDirectory"`
}

// ResolveReference 处理工作区操作。
type ResolveReference struct {
	// 提交方法
	ginx.MethodPost
	// 请求体
	Body ResolveReferenceRequest `in:"body"`
}

// Path 返回操作地址。
func (*ResolveReference) Path() string { return "/resolve" }

// Output 在主体范围内执行操作。
func (g *ResolveReference) Output(ctx *gin.Context) (interface{}, error) {
	result, err := files.ResolveReference(middleware.AuthSubject(ctx), g.Body.ScopeID, g.Body.Path, g.Body.BaseDirectory)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}
