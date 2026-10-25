package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// DeleteChatFile 隐藏会话文件并删除受管附件。
type DeleteChatFile struct {
	// 删除方法
	ginx.MethodDelete
	// 范围标识
	ScopeID string `in:"query" name:"scopeId" validate:"required"`
	// 工作根目录
	Root string `in:"query" name:"root"`
	// 相对路径
	RelativePath string `in:"query" name:"relativePath" validate:"required"`
}

// Path 返回会话文件地址。
func (*DeleteChatFile) Path() string { return "/chat-files" }

// Output 保留项目文件并更新会话隐藏记录。
func (g *DeleteChatFile) Output(ctx *gin.Context) (interface{}, error) {
	if err := files.DeleteChatFile(middleware.AuthSubject(ctx), g.ScopeID, g.Root, g.RelativePath); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
