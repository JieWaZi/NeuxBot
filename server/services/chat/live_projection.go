package chat

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// catchUpLiveProjection 只追平权威日志尾部，不执行启动恢复的中断取消。
// Runtime 消息与原生提问可以同时提交；晚到的查询投影不能隐藏已持久化的待答请求。
// 本次读取固定日志快照；并发提交推进游标后只补齐剩余事件，不追赶后来追加的日志。
func (m *service) catchUpLiveProjection(ctx context.Context, chatID string) error {
	return m.catchUpProjectionThrough(ctx, chatID, 0)
}

func (m *service) catchUpProjectionThrough(ctx context.Context, chatID string, through int64) error {
	if err := ctx.Err(); err != nil {
		return chatContextStatus(err)
	}
	indexed, err := m.queryIndexes.chatController.Get(ctx, chatID)
	if err != nil {
		return err
	}
	limit := 0
	if through > 0 {
		if through <= indexed.LatestSequence {
			return nil
		}
		limit = int(through - indexed.LatestSequence)
	}
	tail, err := m.eventCommitter.store.ReadAfter(chatID, indexed.LatestSequence, limit)
	if err != nil {
		if hasStatusErrorCode(err, status_error.ChatEventCursorInvalid.Code()) {
			return status_error.ChatLogCorrupted
		}
		return err
	}
	for len(tail) > 0 {
		if err := ctx.Err(); err != nil {
			return chatContextStatus(err)
		}
		projection := currentProjection{chat: indexed}
		if err := applyProjectionTail(&projection, tail); err != nil {
			return err
		}
		fields := chatProjectionFields(projection.chat)
		if m.queryIndexes.reading == nil {
			err = m.queryIndexes.transactionController.Transaction(ctx, func(tx context.Context) error {
				return m.queryIndexes.chatController.AdvanceProjection(tx, chatID, indexed.LatestSequence, fields)
			})
		} else {
			err = m.queryIndexes.advanceChatReading(ctx, chatID, indexed.LatestSequence, fields, tail)
		}
		if err == nil {
			return nil
		}
		if !hasStatusErrorCode(err, status_error.Conflict.Code()) {
			return err
		}
		if err := ctx.Err(); err != nil {
			return chatContextStatus(err)
		}
		indexed, err = m.queryIndexes.chatController.Get(ctx, chatID)
		if err != nil {
			return err
		}
		// CAS 冲突表示旧游标已失效；保留其他提交者的状态，只重放尚未覆盖的快照尾部。
		for len(tail) > 0 && tail[0].Sequence <= indexed.LatestSequence {
			tail = tail[1:]
		}
	}
	return nil
}

func chatProjectionFields(current models.Chat) map[string]any {
	return map[string]any{
		"title": current.Title, "active_execution_id": current.ActiveExecutionID,
		"last_account_id": current.LastAccountID, "last_runtime_id": current.LastRuntimeID,
		"last_model": current.LastModel, "last_reasoning": current.LastReasoning, "last_fast": current.LastFast,
		"last_context_window": current.LastContextWindow, "last_permission_level": current.LastPermissionLevel,
		"waiting_approval": current.WaitingApproval, "pending_approvals": string(marshalData(current.PendingApprovals)),
		"waiting_elicitation": current.WaitingElicitation, "pending_elicitations": string(marshalData(current.PendingElicitations)),
		"latest_sequence": current.LatestSequence, "updated_at": current.UpdatedAt,
		"last_ended_execution_id":     current.LastEndedExecutionID,
		"last_ended_execution_status": current.LastEndedExecutionStatus,
		"last_ended_execution_at":     current.LastEndedExecutionAt,
	}
}
