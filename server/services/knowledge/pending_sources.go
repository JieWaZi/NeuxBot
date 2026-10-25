package knowledge

import (
	"context"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"sort"
)

// pendingSourceHeads 冻结每个 SourceID 的当前头；默认整理包含 pending、failed 与 cancelled。
func (s *Service) pendingSourceHeads(ctx context.Context, library string) ([]string, error) {
	heads := map[string]*core.SourceVersion{}
	for offset := 0; ; offset += 100 {
		rows, total, err := s.config.Engine.ListSources(ctx, library, offset, 100)
		if err != nil {
			return nil, bridge.MapError(err)
		}
		// 全部已接纳任务共同投影来源占用，默认范围不能重复包含队列中的资料。
		rows, err = s.config.Engine.SourceActivity(ctx, library, rows)
		if err != nil {
			return nil, bridge.MapError(err)
		}
		for _, source := range rows {
			old := heads[source.SourceID]
			if old == nil || source.Version > old.Version {
				heads[source.SourceID] = source
			}
		}
		if int64(offset+100) >= total {
			break
		}
	}
	keys := make([]string, 0, len(heads))
	for key := range heads {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []string{}
	for _, key := range keys {
		source := heads[key]
		if organizableSourceState(source.State) && !source.Archived && !source.OrganizeBlocked {
			out = append(out, source.ID)
		}
	}
	return out, nil
}

func organizableSourceState(state string) bool {
	switch state {
	case "pending", "failed", "cancelled":
		return true
	default:
		return false
	}
}
