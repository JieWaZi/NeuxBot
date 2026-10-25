package knowledge

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
)

// sourceActivityDTOs 与列表筛选复用核心状态投影，详情不另行判定活动任务状态。
func sourceActivityDTOs(ctx context.Context, libraryID string, sources []*core.SourceVersion) ([]Source, error) {
	rows, err := global.KnowledgeEngine.SourceActivity(ctx, libraryID, sources)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	items := make([]Source, 0, len(rows))
	for _, source := range rows {
		items = append(items, sourceDTO(source))
	}
	return items, nil
}
