package websession

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/localweb"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"net/http"
)

// Web 能力
type Capabilities struct {
	// 工作区文件
	Workspace bool `json:"workspace"`
	// 版本控制
	Git bool `json:"git"`
	// 交互终端
	Terminal bool `json:"terminal"`
}

// 本机会话
type SessionResponse struct {
	// 协议版本
	ProtocolVersion int `json:"protocolVersion"`
	// 请求证明
	CSRFToken string `json:"csrfToken"`
	// 到期时间
	ExpiresAt int64 `json:"expiresAt"`
	// 服务能力
	Capabilities Capabilities `json:"capabilities"`
}

func response(s *localweb.Session) SessionResponse {
	return SessionResponse{ProtocolVersion: 1, CSRFToken: s.CSRF, ExpiresAt: s.ExpiresAt.UnixMilli(), Capabilities: Capabilities{Workspace: true, Git: true, Terminal: global.TerminalRuntime.Available()}}
}

// 获取本机会话
type GetSession struct {
	// MethodGet 声明读取接口。
	ginx.MethodGet
}

func (*GetSession) Path() string { return "/session" }
func (*GetSession) Output(ctx *gin.Context) (interface{}, error) {
	ctx.Header("Cache-Control", "no-store")
	if global.LocalWeb == nil {
		ctx.AbortWithStatus(http.StatusNotFound)
		return nil, nil
	}
	s, ok := global.LocalWeb.Authenticate(ctx.Request, false)
	if !ok {
		ctx.AbortWithStatus(http.StatusUnauthorized)
		return nil, nil
	}
	return response(s), nil
}
