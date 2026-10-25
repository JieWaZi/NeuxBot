package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 聊天远端操作引用
type RemoteChatOperationRef struct {
	// 资源引用
	Ref ResourceRef `json:"ref"`
	// 操作标识
	RequestID string `json:"requestId"`
	// 原回执已确认
	ReceiptConfirmed bool `json:"receiptConfirmed"`
	// 已确认任务
	JobID string `json:"jobId"`
}

// 聊天远端操作列表
type RemoteChatOperationList struct {
	// 操作列表
	Items []RemoteChatOperationRef `json:"items"`
	// 总数
	Total int64 `json:"total"`
}

// 获取聊天真实远端知识操作，用于浮层重开与进程重启恢复。
type ListRemoteChatOperations struct {
	ginx.MethodGet
	// 聊天标识
	ChatID string `in:"path" name:"chatId" validate:"required,max=32"`
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0"`
	// 每页条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

func (o *ListRemoteChatOperations) Path() string { return "/chat-operations/:chatId/requests" }
func (o *ListRemoteChatOperations) Output(ctx *gin.Context) (interface{}, error) {
	if global.KnowledgeService == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	offset, limit := pagination(Pagination{Offset: o.Offset, Limit: o.Limit})
	rows, total, err := global.KnowledgeService.ListRemoteChatOperations(ctx, o.ChatID, offset, limit)
	if err != nil {
		return nil, err
	}
	out := RemoteChatOperationList{Items: []RemoteChatOperationRef{}, Total: total}
	for _, row := range rows {
		out.Items = append(out.Items, RemoteChatOperationRef{Ref: ResourceRef{Backend: row.Backend, ConnectionID: row.ConnectionID, KnowledgeBaseID: row.KnowledgeBaseID}, RequestID: row.RequestID, ReceiptConfirmed: row.ReceiptConfirmed, JobID: row.JobID})
	}
	return out, nil
}
func init() { Router.Register(&ListRemoteChatOperations{}) }
