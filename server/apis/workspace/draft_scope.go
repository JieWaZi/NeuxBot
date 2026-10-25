package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// OpenDraftScopeRequest 是工作区操作参数。
type OpenDraftScopeRequest struct {
	// 选择的工作目录
	Root string `json:"root" validate:"required"`
	// 额外项目目录
	AdditionalRoots []string `json:"additionalRoots"`
}

// OpenDraftScope 处理工作区操作。
type OpenDraftScope struct {
	// 提交方法
	ginx.MethodPost
	// 请求体
	Body OpenDraftScopeRequest `in:"body"`
}

// Path 返回操作地址。
func (*OpenDraftScope) Path() string { return "/draft-scopes" }

// Output 在主体范围内执行操作。
func (g *OpenDraftScope) Output(ctx *gin.Context) (interface{}, error) {
	scope, err := files.Open(middleware.AuthSubject(ctx), g.Body.Root, g.Body.AdditionalRoots)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return scope, nil
}
