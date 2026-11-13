// Package workspaceattention 通过原工作事实提供阅读和关注事项查询。
package workspaceattention

import (
	"context"
	"slices"
	"sort"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Query 查询原工作归属、请求和阅读事实。
type Query struct {
	// Board 查询原工作容器与有效请求。
	Board controllers.WorkBoardController
	// Transactions 保证查询快照及批量写入的事务一致性。
	Transactions controllers.TransactionController
	// Incomplete 读取范围内的原会话投影健康。
	Incomplete func([]string) string
	// Isolated 返回无法验证的会话投影身份快照。
	Isolated func() []string
	// Epoch 返回当前 Core 进程身份。
	Epoch func() string
}

// Completeness 组合查询发现的缺口与原投影健康。
func (q *Query) Completeness(ctx context.Context, filter controllers.WorkBoardQuery, reason string) (models.AttentionCompleteness, error) {
	if q.Incomplete != nil {
		reported := q.Incomplete(nil)
		if reported != "" && filter.ExecutorID != "" {
			ids, err := q.Board.ReadingChatIDs(ctx, filter)
			if err != nil {
				return models.AttentionCompleteness{}, err
			}
			reported = q.Incomplete(ids)
		}
		if reported != "" {
			reason = reported
		}
	}
	return models.AttentionCompleteness{Complete: reason == "", IncompleteReason: reason, Epoch: q.Epoch()}, nil
}

func before(a, b models.WorkBoardEntry) bool {
	if a.UpdatedAt != b.UpdatedAt {
		return a.UpdatedAt > b.UpdatedAt
	}
	if a.OwnerKind != b.OwnerKind {
		return a.OwnerKind > b.OwnerKind
	}
	return a.OwnerID > b.OwnerID
}

// ExecutorSummaries 汇总按原执行对象归属的指标及请求定位。
func (q *Query) ExecutorSummaries(ctx context.Context, reader string, ids []string) ([]models.ExecutorAttentionSummary, models.AttentionCompleteness, error) {
	isolated := []string(nil)
	if q.Isolated != nil {
		isolated = q.Isolated()
	}
	var facts controllers.ExecutorAttentionFacts
	err := q.Transactions.Transaction(ctx, func(tx context.Context) error {
		var err error
		facts, err = q.Board.ExecutorAttentionFacts(tx, reader, ids, isolated)
		return err
	})
	if err != nil {
		return nil, models.AttentionCompleteness{}, err
	}
	chatIDs := make([]string, 0, len(facts.Chats))
	reason := ""
	chatsByExecutor := map[string][]models.WorkBoardEntry{}
	for _, item := range facts.Chats {
		chatIDs = append(chatIDs, item.ChatID)
		if _, exists := facts.Readings[item.ChatID]; !exists {
			reason = "isolated"
			continue
		}
		chatsByExecutor[item.ExecutorID] = append(chatsByExecutor[item.ExecutorID], item)
	}
	if q.Incomplete != nil {
		if reported := q.Incomplete(chatIDs); reported != "" {
			reason = reported
		}
	}
	sort.Slice(facts.Requests, func(i, j int) bool {
		a, b := facts.Requests[i], facts.Requests[j]
		if a.CreatedAt != b.CreatedAt {
			return a.CreatedAt < b.CreatedAt
		}
		if a.OwnerKind != b.OwnerKind {
			return a.OwnerKind < b.OwnerKind
		}
		return a.ID < b.ID
	})
	requestsByExecutor := map[string][]models.WorkBoardAttention{}
	for _, request := range facts.Requests {
		requestsByExecutor[request.ExecutorID] = append(requestsByExecutor[request.ExecutorID], request)
	}
	results := make([]models.ExecutorAttentionSummary, 0, len(ids))
	for _, id := range ids {
		entry := models.ExecutorAttentionSummary{ExecutorID: id, Kind: "waker", Chats: []models.ExecutorAttentionChat{}, Requests: []models.WorkBoardAttention{}}
		kinds := map[models.WorkRef][]string{}
		requests := map[models.WorkRef][]models.WorkBoardAttention{}
		for _, request := range requestsByExecutor[id] {
			entry.Requests = append(entry.Requests, request)
			work := models.WorkRef{Kind: constants.WorkOwnerKind(request.OwnerKind), ID: request.OwnerID}
			requests[work] = append(requests[work], request)
			if !slices.Contains(kinds[work], request.Kind) {
				kinds[work] = append(kinds[work], request.Kind)
			}
			if work.Kind == constants.WorkOwnerKindTeamThread {
				entry.Kind = "team"
			}
		}
		entry.WaitingWorkCount = int64(len(kinds))
		entry.PendingRequestCount = int64(len(entry.Requests))
		var latest models.WorkBoardEntry
		for _, item := range chatsByExecutor[id] {
			if latest.OwnerID == "" || before(item, latest) {
				latest = item
			}
			reading := facts.Readings[item.ChatID]
			work := models.WorkRef{Kind: constants.WorkOwnerKind(item.OwnerKind), ID: item.OwnerID}
			if work.Kind == constants.WorkOwnerKindTeamThread {
				entry.Kind = "team"
			}
			if item.Activity == constants.ChannelActivityStateRunning {
				entry.RunningChatCount++
			}
			if reading.UnreadMessageCount > 0 {
				entry.UnreadMessageCount += reading.UnreadMessageCount
				entry.UnreadChatCount++
				entry.AwaitingViewWorkCount++
			}
			if reading.UnreadMessageCount > 0 || len(kinds[work]) > 0 || item.Activity == constants.ChannelActivityStateRunning || item.LastStatus == "failed" {
				entry.Chats = append(entry.Chats, models.ExecutorAttentionChat{Work: work, Title: item.Title, Requests: append([]models.WorkBoardAttention{}, requests[work]...), ChatID: item.ChatID, UnreadMessageCount: reading.UnreadMessageCount, FirstUnreadMessageID: reading.FirstUnreadMessageID, RequestKinds: append([]string{}, kinds[work]...), Activity: item.Activity, LastStatus: item.LastStatus})
			}
		}
		if latest.OwnerID != "" {
			entry.LatestChatID = latest.ChatID
			entry.LatestWork = &models.WorkRef{Kind: latest.OwnerKind, ID: latest.OwnerID}
			entry.LastStatus = latest.LastStatus
		}
		results = append(results, entry)
	}
	return results, models.AttentionCompleteness{Complete: reason == "", IncompleteReason: reason, Epoch: q.Epoch()}, nil
}
