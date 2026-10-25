package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/workspacefiles"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

var files = workspacefiles.New()

// Files 返回与 HTTP 文件操作共用的工作区范围管理器，供统一事件订阅装配。
func Files() *workspacefiles.Manager { return files }

// 工作范围请求
type ScopeRequest struct {
	// 对话标识
	ChatID string `json:"chatId" validate:"required"`
}

// 打开工作范围
type OpenScope struct {
	// 提交方法
	ginx.MethodPost
	// 请求体
	Body ScopeRequest `in:"body"`
}

// Path 返回工作范围签发地址。
func (*OpenScope) Path() string { return "/scopes" }

// Output 只从 Chat 冻结事实读取授权目录，返回供后续文件操作使用的临时句柄。
func (g *OpenScope) Output(ctx *gin.Context) (interface{}, error) {
	detail, err := global.ChatService.GetChatSummary(ctx, g.Body.ChatID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	scope, err := files.Open(middleware.AuthSubject(ctx), detail.Chat.WorkspaceDirectory, detail.Chat.AdditionalWorkingDirectories)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return scope, nil
}
