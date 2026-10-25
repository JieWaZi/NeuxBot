package knowledge_connections

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/remote"
	"github.com/gin-gonic/gin"
	"time"
)

// withLibrarySummaries 批量补充已观察到的展示数据，不为列表逐库访问远端。
func withLibrarySummaries(ctx context.Context, c *remoteClients, items []knowledge.Library, total int64) (knowledge.LibraryList, error) {
	out := knowledge.LibraryList{Items: items, Total: total}
	if global.KnowledgeControllerManager == nil {
		return out, nil
	}
	ids := make([]string, 0, len(items))
	for _, v := range items {
		ids = append(ids, v.ID)
	}
	rows, err := global.KnowledgeControllerManager.LibrarySummaries(ctx, c.connection.Backend, c.connection.ID, c.connection.Revision, ids)
	if err != nil {
		return out, err
	}
	byID := map[string]models.KnowledgeLibrarySummary{}
	for _, v := range rows {
		byID[v.KnowledgeBaseID] = v
	}
	for i := range out.Items {
		if v, ok := byID[out.Items[i].ID]; ok {
			count := v.PageTotal
			out.Items[i].PageTotal = &count
			out.Items[i].PageTotalObservedAt = v.ObservedAt
		}
	}
	return out, nil
}

// recordPageTotal 只在远端成功响应且没有搜索、类型或主题筛选时记录总数。
func (o *ListRemotePages) recordPageTotal(ctx context.Context, c *remoteClients, total, observedAt int64) error {
	if o.Query != "" || (o.PageType != "" && o.PageType != "all") || o.ThemeID != nil || global.KnowledgeControllerManager == nil {
		return nil
	}
	return global.KnowledgeControllerManager.RecordLibrarySummary(ctx, models.KnowledgeLibrarySummary{Backend: c.connection.Backend, ConnectionID: c.connection.ID, ConnectionRevision: c.connection.Revision, KnowledgeBaseID: o.LibraryID, PageTotal: total, ObservedAt: observedAt})
}

// libraryWithPageTotal 详情读取时以最小分页刷新总量；统计失败保留未知，不伪造零值。
func libraryWithPageTotal(ctx *gin.Context, c *remoteClients, item knowledge.Library) (knowledge.Library, error) {
	observedAt := time.Now().UnixMilli()
	var total int64
	if c.custom != nil {
		v, err := c.custom.ListPageDirectory(ctx, 0, 1)
		if err != nil {
			return item, nil
		}
		total = v.Total
	} else {
		v, err := c.official.ListPages(ctx, item.ID, remote.PageOptions{Page: 1, PageSize: 1})
		if err != nil {
			return item, nil
		}
		total = v.Total
	}
	item.PageTotal = &total
	item.PageTotalObservedAt = observedAt
	err := (&ListRemotePages{LibraryID: item.ID}).recordPageTotal(ctx, c, total, observedAt)
	return item, err
}
