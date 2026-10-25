package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// ListChatFiles 列出会话生成文件和隐藏记录。
type ListChatFiles struct {
	// 查询方法
	ginx.MethodGet
	// 范围标识
	ScopeID string `in:"query" name:"scopeId" validate:"required"`
	// 工作根目录
	Root string `in:"query" name:"root"`
}

// Path 返回会话文件地址。
func (*ListChatFiles) Path() string { return "/chat-files" }

// Output 返回会话文件快照。
func (g *ListChatFiles) Output(ctx *gin.Context) (interface{}, error) {
	result, err := files.ListChatFiles(ctx, middleware.AuthSubject(ctx), g.ScopeID, g.Root)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}
