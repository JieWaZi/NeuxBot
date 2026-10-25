package chats

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 对话列表
type ListChats struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"query" name:"wakerId" validate:"required"`
	// 时间游标
	BeforeUpdatedAt int64 `in:"query" name:"beforeUpdatedAt"`
	// 标识游标
	BeforeID string `in:"query" name:"beforeId"`
	// 分页上限
	Limit int `in:"query" name:"limit"`
}

// 对话列表响应
type ListChatsResponse struct {
	// 资源列表
	Resources []models.Chat `json:"resources"`
	// 最近项目
	RecentProjectID string `json:"recentProjectId,omitempty"`
	// 下页时间游标
	NextBeforeUpdatedAt int64 `json:"nextBeforeUpdatedAt,omitempty"`
	// 下页标识游标
	NextBeforeID string `json:"nextBeforeId,omitempty"`
	// 是否有更多
	HasMore bool `json:"hasMore"`
}

// Path 返回对话列表接口路径。
func (l *ListChats) Path() string {
	return ""
}

// Validate 校验分页范围。
func (l *ListChats) Validate(*gin.Context) error {
	if strings.TrimSpace(l.WakerID) == "" ||
		l.BeforeUpdatedAt < 0 ||
		(l.BeforeUpdatedAt == 0) != (strings.TrimSpace(l.BeforeID) == "") ||
		l.Limit < 0 ||
		l.Limit > 100 {
		return ginx.WithStack(status_error.BadRequest)
	}
	if l.Limit == 0 {
		l.Limit = 50
	}
	l.BeforeID = strings.TrimSpace(l.BeforeID)
	return nil
}

// Output 返回最近活动排序的对话。
func (l *ListChats) Output(ctx *gin.Context) (interface{}, error) {
	result, err := global.ChatService.ListChats(
		ctx,
		chat.ListChatsInput{
			WakerID:         l.WakerID,
			BeforeUpdatedAt: l.BeforeUpdatedAt,
			BeforeID:        l.BeforeID,
			Limit:           l.Limit,
		},
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ListChatsResponse{
		Resources:           result.Resources,
		RecentProjectID:     result.RecentProjectID,
		NextBeforeUpdatedAt: result.NextBeforeUpdatedAt,
		NextBeforeID:        result.NextBeforeID,
		HasMore:             result.HasMore,
	}, nil
}
