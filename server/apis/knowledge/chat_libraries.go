package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	service "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 当前聊天知识目标
type ChatLibraryList struct {
	// 知识库列表
	Items []service.ChatLibrary `json:"items"`
}

// 获取当前聊天可访问的通用知识库。
type ChatLibraries struct {
	ginx.MethodGet
	// 聊天标识
	ChatID string `in:"query" name:"chatId"`
	// 伙伴标识
	WakerID string `in:"query" name:"wakerId"`
}

func (o *ChatLibraries) Path() string { return "/chat-libraries" }
func (o *ChatLibraries) Output(ctx *gin.Context) (interface{}, error) {
	if global.KnowledgeService == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	values, err := global.KnowledgeService.ChatLibraries(ctx, o.ChatID, o.WakerID)
	if err != nil {
		return nil, err
	}
	return ChatLibraryList{Items: values}, nil
}
func init() { Router.Register(&ChatLibraries{}) }
