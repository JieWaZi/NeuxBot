package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取远端知识库活动
type ListRemoteLibraryActivity struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
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

func (o *ListRemoteLibraryActivity) Path() string {
	return "/:connectionId/libraries/:libraryId/activity"
}

// Output 保留远端配置编辑授权、筛选与实际活动游标。
func (o *ListRemoteLibraryActivity) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	rows, cursor, err := c.custom.ListLibraryActivity(ctx, core.ActivityQuery{AfterID: o.AfterID, Limit: o.Limit, Action: core.AuditAction(o.Action), Outcome: core.AuditOutcome(o.Outcome), ActorUserID: o.Actor})
	if err != nil {
		return nil, remoteError(err)
	}
	out := knowledge.LibraryActivityList{Data: make([]knowledge.LibraryActivity, 0, len(rows)), NextCursor: cursor}
	for _, row := range rows {
		out.Data = append(out.Data, customActivity(row))
	}
	return out, nil
}
