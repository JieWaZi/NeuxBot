package knowledge

import (
	"encoding/json"
	"time"

	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 知识库活动
type LibraryActivity struct {
	// 事件标识
	ID uint64 `json:"id"`
	// 请求标识
	RequestID string `json:"requestId"`
	// 持久操作
	Operation string `json:"operation"`
	// 发起用户
	ActorUserID string `json:"actorUserId"`
	// 发起角色
	ActorRole string `json:"actorRole"`
	// 动作
	Action string `json:"action"`
	// 作用域类型
	ScopeType string `json:"scopeType"`
	// 知识库标识
	ScopeID string `json:"scopeId"`
	// 对象类型
	TargetType string `json:"targetType"`
	// 对象标识
	TargetID string `json:"targetId"`
	// 执行结果
	Outcome string `json:"outcome"`
	// 安全摘要
	Details json.RawMessage `json:"details"`
	// 发生时间
	CreatedAt time.Time `json:"createdAt"`
}

// 知识库活动页
type LibraryActivityList struct {
	// 活动列表
	Data []LibraryActivity `json:"data"`
	// 下页游标
	NextCursor uint64 `json:"nextCursor"`
}

// 获取知识库活动记录
type ListLibraryActivity struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 上页游标
	AfterID uint64 `in:"query" name:"afterId"`
	// 每页数量
	Limit int `in:"query" name:"limit" validate:"min=0"`
	// 动作筛选
	Action string `in:"query" name:"action"`
	// 结果筛选
	Outcome string `in:"query" name:"outcome"`
	// 发起人筛选
	Actor string `in:"query" name:"actor"`
}

func (o *ListLibraryActivity) Path() string { return "/libraries/:libraryId/activity" }
func (o *ListLibraryActivity) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	entries, cursor, err := global.KnowledgeEngine.ListLibraryActivity(ctx, o.LibraryID, core.ActivityQuery{AfterID: o.AfterID, Limit: o.Limit, Action: core.AuditAction(o.Action), Outcome: core.AuditOutcome(o.Outcome), ActorUserID: o.Actor})
	if err != nil {
		return nil, bridge.MapError(err)
	}
	result := LibraryActivityList{Data: make([]LibraryActivity, 0, len(entries)), NextCursor: cursor}
	for _, entry := range entries {
		result.Data = append(result.Data, LibraryActivity{ID: entry.ID, RequestID: entry.RequestID, Operation: entry.Operation, ActorUserID: entry.ActorUserID, ActorRole: entry.ActorRole, Action: string(entry.Action), ScopeType: entry.ScopeType, ScopeID: entry.ScopeID, TargetType: entry.TargetType, TargetID: entry.TargetID, Outcome: string(entry.Outcome), Details: json.RawMessage(entry.Details), CreatedAt: entry.CreatedAt})
	}
	return result, nil
}
