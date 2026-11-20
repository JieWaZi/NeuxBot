package workflow

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

const (
	chatTurnWillStart uint32 = 1 << iota
	chatTurnStopped
)

type chatRunOrigin struct {
	// chatID 是运行归属的原会话。
	chatID string
	// executionID 是该会话发起运行的确切回合。
	executionID string
}

type chatTurnGrant struct {
	// executionID 区分同一聊天先后接纳的回合。
	executionID string
	// state 串行化当前回合的首次启动意图与停止请求。
	state atomic.Uint32
	// savedRevisions 记录该回合为引用的工作流成功保存的新版本。
	savedRevisions sync.Map
}

// chatRunIndex 让普通聊天跳过工作流持久化入口。实际授权和运行归属仍从原记录核对。
type chatRunIndex struct {
	// history 按 Chat 保存已接纳过工作流的派生提示。
	history sync.Map
	// active 保存具有未收敛运行的准确 Chat 和 Execution。
	active sync.Map
	// grants 仅保留每个相关 Chat 最近一次签发的回合。
	grants sync.Map
	// restored 表示已有运行已在本进程恢复。
	restored atomic.Bool
	// teamStopsReady 在团队启动恢复结束后允许用提示跳过无关联的停止请求。
	teamStopsReady atomic.Bool
}

// RestoreChatRunIndex 在恢复已有聊天前加载工作流来源。失败时普通聊天仍可继续接纳。
func (s *Service) RestoreChatRunIndex(ctx context.Context) {
	if s.runs == nil || s.chatTurns.restored.Load() {
		return
	}
	origins, err := s.runs.ListChatRunOrigins(ctx)
	if err != nil {
		logx.Warnf("restore workflow chat origins failed: %v", err)
		return
	}
	for _, origin := range origins {
		s.chatTurns.remember(origin.SourceChatID, origin.SourceExecutionID)
	}
	s.chatTurns.restored.Store(true)
}

func (i *chatRunIndex) remember(chatID, executionID string) {
	i.history.Store(chatID, struct{}{})
	if executionID != "" {
		i.active.Store(chatRunOrigin{chatID: chatID, executionID: executionID}, struct{}{})
	}
}

func (i *chatRunIndex) hasHistory(chatID string) bool {
	_, known := i.history.Load(chatID)
	return known
}

// MayHaveChatRunSources 提示查询是否需要读取运行事实。未恢复或正在接纳首条运行时都保留读取。
func (s *Service) MayHaveChatRunSources(chatID string) bool {
	return !s.chatTurns.restored.Load() || s.chatTurns.mayHaveWork(chatID)
}

func (i *chatRunIndex) mayHaveWork(chatID string) bool {
	if i.hasHistory(chatID) {
		return true
	}
	grant := i.current(chatID)
	return grant != nil && grant.state.Load()&chatTurnWillStart != 0
}

func (i *chatRunIndex) bind(chatID, executionID string, selected bool) *chatTurnGrant {
	if grant := i.current(chatID); grant != nil && grant.executionID == executionID {
		return grant
	}
	grant := &chatTurnGrant{executionID: executionID}
	if selected {
		grant.state.Store(chatTurnWillStart)
	}
	i.grants.Store(chatID, grant)
	return grant
}

func (i *chatRunIndex) current(chatID string) *chatTurnGrant {
	grant, _ := i.grants.Load(chatID)
	if grant == nil {
		return nil
	}
	return grant.(*chatTurnGrant)
}

func (i *chatRunIndex) beginStart(chatID, executionID string) bool {
	grant := i.current(chatID)
	if grant == nil {
		return true
	}
	return grant.executionID == executionID && (grant.state.CompareAndSwap(0, chatTurnWillStart) || grant.state.Load() == chatTurnWillStart)
}

func (i *chatRunIndex) stopped(chatID, executionID string) bool {
	grant := i.current(chatID)
	return grant != nil && grant.executionID == executionID && grant.state.Load()&chatTurnStopped != 0
}

func (i *chatRunIndex) stop(chatID, executionID string) bool {
	if grant := i.current(chatID); grant != nil && grant.executionID == executionID && grant.state.Or(chatTurnStopped)&chatTurnWillStart != 0 {
		return true
	}
	_, linked := i.active.Load(chatRunOrigin{chatID: chatID, executionID: executionID})
	return linked
}

// stopTeamTurns 撤销当前回合的启动意图，只在有待接纳或未结束的运行时要求持久停止。
func (i *chatRunIndex) stopTeamTurns(chatIDs []string) bool {
	mayHaveWork := false
	for _, chatID := range chatIDs {
		if grant := i.current(chatID); grant != nil {
			if state := grant.state.Or(chatTurnStopped); state&chatTurnWillStart != 0 {
				mayHaveWork = true
			}
		}
	}
	if !mayHaveWork {
		i.active.Range(func(key, _ any) bool {
			mayHaveWork = slices.Contains(chatIDs, key.(chatRunOrigin).chatID)
			return !mayHaveWork
		})
	}
	return mayHaveWork
}
