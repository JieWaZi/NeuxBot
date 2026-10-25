package terminals

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"os"
	"path/filepath"
	"strings"
)

// 创建终端请求
type StartRequest struct {
	// 对话标识
	ChatID string `json:"chatId" validate:"required,max=128"`
	// 工作目录
	Root string `json:"root" validate:"required,max=4096"`
	// 内容标识
	OwnerKey string `json:"ownerKey" validate:"required,max=256"`
	// 允许创建
	CreateWhenMissing bool `json:"createWhenMissing"`
}

// 终端身份
type StartResponse struct {
	// 会话标识
	SessionID string `json:"sessionId"`
	// 终端程序
	Shell string `json:"shell"`
}

// 创建交互终端
type StartTerminal struct {
	// ginx.MethodPost 声明接口方法。
	ginx.MethodPost
	// 请求体
	Body StartRequest `in:"body"`
}

func (*StartTerminal) Path() string { return "" }
func (op *StartTerminal) Output(ctx *gin.Context) (interface{}, error) {
	if !global.TerminalRuntime.Available() {
		return nil, status_error.TerminalUnavailable
	}
	if global.ChatService == nil {
		return nil, status_error.TerminalInvalid
	}
	summary, err := global.ChatService.GetChatSummary(ctx.Request.Context(), op.Body.ChatID)
	if err != nil {
		return nil, err
	}
	roots := append([]string{summary.Chat.WorkspaceDirectory}, summary.Chat.AdditionalWorkingDirectories...)
	root, err := authorizedRoot(op.Body.Root, roots)
	if err != nil {
		return nil, err
	}
	result, err := global.TerminalRuntime.Start(ctx.Request.Context(), middleware.AuthLifetime(ctx), middleware.AuthSubject(ctx), op.Body.OwnerKey, root, op.Body.CreateWhenMissing)
	if err != nil {
		return nil, status_error.TerminalInvalid
	}
	return StartResponse{SessionID: result.SessionID, Shell: result.Shell}, nil
}

func authorizedRoot(requested string, roots []string) (string, error) {
	if !filepath.IsAbs(requested) || strings.ContainsRune(requested, '\x00') {
		return "", status_error.TerminalInvalid
	}
	canonical, err := filepath.EvalSymlinks(requested)
	if err != nil {
		return "", status_error.TerminalInvalid
	}
	canonical = filepath.Clean(canonical)
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return "", status_error.TerminalInvalid
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		allowed, err := filepath.EvalSymlinks(root)
		if err == nil && filepath.Clean(allowed) == canonical {
			return canonical, nil
		}
	}
	return "", status_error.TerminalForbidden
}
