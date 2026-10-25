package system

import (
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 健康检查响应
type HealthResponse struct {
	// 状态
	Status string `json:"status"`
	// 版本号
	Version string `json:"version"`
	// 响应时间
	Time int64 `json:"time"`
	// 索引诊断
	ChatIndexes chat.RecoveryHealth `json:"chatIndexes"`
}

// 健康检查
type GetHealth struct {
	ginx.MethodGet
}

// Path 返回健康接口路径。
func (g *GetHealth) Path() string {
	return "/health"
}

// Output 返回健康状态。
func (g *GetHealth) Output(*gin.Context) (interface{}, error) {
	indexHealth := chat.RecoveryHealth{
		Chats: []chat.RecoveryDiagnostic{},
	}
	if global.ChatService != nil {
		indexHealth = global.ChatService.RecoveryHealth()
	}
	status := "ok"
	if indexHealth.Isolated > 0 {
		status = "degraded"
	}
	return HealthResponse{
		Status:      status,
		Version:     constants.ApplicationVersion,
		Time:        time.Now().UnixMilli(),
		ChatIndexes: indexHealth,
	}, nil
}
