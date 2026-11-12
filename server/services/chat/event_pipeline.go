package chat

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// eventCommitter 固化权威事件的提交后顺序：JSONL、派生文件索引、SSE。
//
// JSONL 写入成功即表示业务事实已经提交；派生文件索引由 Store 内部同步维护并可重建，
// SSE 只能发布已经提交的 Record。
type eventCommitter struct {
	// store 持久化每个 Chat 的权威追加日志。
	store chatJournalStorage
	// subscriptions 广播已提交事件给进程内订阅者。
	subscriptions *subscriptionHub
}

// newEventCommitter 创建权威事件提交器。
func newEventCommitter(
	store chatJournalStorage,
	subscriptions *subscriptionHub,
) *eventCommitter {
	return &eventCommitter{
		store:         store,
		subscriptions: subscriptions,
	}
}

// create 原子建立新 Chat 的首批权威事实，调用方完成当前投影后再公开。
func (c *eventCommitter) create(
	chatID string,
	inputs []eventlog.AppendInput,
) ([]eventlog.Record, error) {
	records, err := c.store.Create(chatID, inputs)
	if err != nil {
		return nil, c.commitError(chatID, err)
	}
	return records, nil
}

// append 向已有 Chat 追加权威事实，调用方完成当前投影后再公开。
func (c *eventCommitter) append(
	chatID string,
	inputs []eventlog.AppendInput,
) ([]eventlog.Record, error) {
	records, err := c.appendDurable(chatID, inputs)
	if err != nil {
		return nil, err
	}
	return records, nil
}

// appendDurable 只将事件同步追加到权威日志，允许调用方在公开事件前完成终态交接。
func (c *eventCommitter) appendDurable(
	chatID string,
	inputs []eventlog.AppendInput,
) ([]eventlog.Record, error) {
	records, err := c.store.Append(chatID, inputs)
	if err != nil {
		return nil, c.commitError(chatID, err)
	}
	return records, nil
}

// publishCommitted 广播已经持久化且已尝试推进当前投影的权威事件。
func (c *eventCommitter) publishCommitted(records []eventlog.Record) {
	c.afterCommit(records)
}

// afterCommit 仅处理允许由启动恢复补偿的提交后副作用。
func (c *eventCommitter) afterCommit(records []eventlog.Record) {
	if len(records) == 0 {
		return
	}
	c.subscriptions.publishCommitted(records[0].ChatID, records)
}

// commitError 记录底层文件错误并收敛为稳定业务错误。
func (c *eventCommitter) commitError(chatID string, err error) error {
	logx.Errorf(
		"append chat log failed: chat_id=%s error=%v",
		chatID,
		err,
	)
	return status_error.ChatLogError
}

// queryIndexStore 集中持有全部可从 Chat JSONL 重建的 SQLite 查询索引。
//
// 该类型不保存权威业务状态，也不定义新的持久化 seam；它只把共同遵守“可落后、
// 可重建”语义的 Repository Controller 收口，避免 Service 重复散列这些依赖。
type queryIndexStore struct {
	// reading 保存可读索引及独立的用户阅读事实。
	reading controllers.ChatReadingController
	// failed 记录已提交的权威日志尚未投影。
	failed func(string, error)
	// deleted 清除已永久删除会话的恢复登记。
	deleted func(string)
	// logStore 通过文件索引读取单条结果，文件读取在 SQLite 事务前完成。
	logStore chatJournalStorage
	// chatController 持久化对话查询索引。
	chatController controllers.ChatController
	// transactionController 提供跨查询索引写入的数据库事务边界。
	transactionController controllers.TransactionController
}

// newQueryIndexStore 创建可重建的 SQLite 查询索引集合。
func newQueryIndexStore(
	chats controllers.ChatController,
	transactions controllers.TransactionController,
) *queryIndexStore {
	return &queryIndexStore{
		chatController:        chats,
		transactionController: transactions,
	}
}

// writeInitialIndexes 以单个 SQLite 事务建立 Chat 的初始查询索引。
func (p *queryIndexStore) writeInitialIndexes(
	ctx context.Context,
	chat models.Chat,
) {
	if err := p.createChats(ctx, []models.Chat{chat}); err != nil {
		p.reportProjectionFailure(chat.ID, err)
	}
}

// createChats 在一个 SQLite 事务内建立一组已经写入权威日志的 Chat 查询索引。
func (p *queryIndexStore) createChats(ctx context.Context, chats []models.Chat) error {
	return p.createChatsIfMissing(ctx, chats, false)
}

func (p *queryIndexStore) createChatsIfMissing(ctx context.Context, chats []models.Chat, missingOnly bool) error {
	prepared := make([]readingChanges, len(chats))
	if p.reading != nil {
		for index, chat := range chats {
			records, err := p.logStore.ReadAfter(chat.ID, 0, int(chat.LatestSequence))
			if err != nil {
				return err
			}
			prepared[index], err = p.prepareReading(ctx, chat.ID, records, chat.LatestSequence)
			if err != nil {
				return err
			}
		}
	}
	return p.transactionController.Transaction(ctx, func(tx context.Context) error {
		for index, chat := range chats {
			_, missing, err := p.inspectChat(tx, chat.ID)
			if err != nil {
				return err
			}
			if !missing {
				if missingOnly {
					return status_error.Conflict
				}
				continue
			}
			changes := prepared[index]
			latest, err := p.projectReadableMessages(tx, chat.ID, changes, true, &changes.inheritedThrough)
			if err != nil {
				return err
			}
			chat.LatestReadableSequence = latest
			if err := p.chatController.Create(tx, chat); err != nil {
				return err
			}
		}
		return nil
	})
}

// updateChatIndex 以尽力而为方式更新 Chat 查询索引。
func (p *queryIndexStore) updateChatIndex(ctx context.Context, chatID string, afterSequence int64, fields map[string]any) {
	if err := p.advanceChatReading(ctx, chatID, afterSequence, fields, nil); err != nil {
		p.reportProjectionFailure(chatID, err)
	}
}

func (p *queryIndexStore) reportProjectionFailure(chatID string, err error) {
	logx.Errorf("update chat projection failed: chat_id=%s error=%v", chatID, err)
	if p.failed != nil {
		p.failed(chatID, err)
	}
}

// advanceChatReading 以同一事务更新阅读索引和原 Chat checkpoint。
func (p *queryIndexStore) advanceChatReading(ctx context.Context, chatID string, afterSequence int64, fields map[string]any, tail []eventlog.Record) error {
	if p.reading == nil {
		return p.chatController.AdvanceProjection(ctx, chatID, afterSequence, fields)
	}
	through, err := readProjectionSequence(fields)
	if err != nil {
		return err
	}
	if tail == nil {
		tail, err = p.logStore.ReadAfter(chatID, afterSequence, int(through-afterSequence))
		if err != nil {
			return err
		}
	}
	if len(tail) == 0 || tail[len(tail)-1].Sequence != through {
		return status_error.ChatLogIndexCorrupted
	}
	changes, err := p.prepareReading(ctx, chatID, tail, through)
	if err != nil {
		return err
	}
	return p.transactionController.Transaction(ctx, func(tx context.Context) error {
		changed := false
		if len(changes.rows)+len(changes.remove) > 0 {
			latest, projectionChanged, err := p.reading.ProjectWithChange(tx, chatID, changes.remove, changes.rows, false)
			changed = projectionChanged
			if err != nil {
				return err
			}
			fields["latest_readable_sequence"] = latest
		}
		if err := p.chatController.AdvanceProjection(tx, chatID, afterSequence, fields); err != nil {
			return err
		}
		if changed {
			p.reading.ProjectionChanged(tx, chatID)
		}
		return nil
	})
}

// getSummary 返回打开 Chat 所需的有界查询摘要。
func (p *queryIndexStore) getSummary(
	ctx context.Context,
	chatID string,
) (ChatSummary, error) {
	chat, err := p.chatController.Get(ctx, chatID)
	if err != nil {
		return ChatSummary{}, err
	}
	return ChatSummary{
		Chat: chat,
	}, nil
}

// readChatList 读取稳定游标页。
func (p *queryIndexStore) readChatList(
	ctx context.Context,
	input ListChatsInput,
) (ChatList, error) {
	resources, err := p.chatController.ListByWakerBefore(
		ctx,
		input.WakerID,
		input.BeforeUpdatedAt,
		input.BeforeID,
		input.Limit+1,
	)
	if err != nil {
		return ChatList{}, err
	}
	result := ChatList{Resources: resources}
	result.HasMore = len(result.Resources) > input.Limit
	if result.HasMore {
		result.Resources = result.Resources[:input.Limit]
	}
	if result.HasMore && len(result.Resources) > 0 {
		last := result.Resources[len(result.Resources)-1]
		result.NextBeforeUpdatedAt = last.UpdatedAt
		result.NextBeforeID = last.ID
	}
	return result, nil
}

// ensureChatState 在主查询索引缺失时以完整权威状态尽力重建。
func (p *queryIndexStore) ensureChatState(
	ctx context.Context,
	state chatState,
) {
	_, err := p.chatController.Get(ctx, state.chat.ID)
	if err == nil {
		return
	}
	// 只有明确缺失才能触发重建；数据库故障不能被误判为可覆盖的索引空洞。
	if !hasStatusErrorCode(err, status_error.ChatNotFound.Code()) {
		logx.Errorf(
			"inspect chat query index failed: chat_id=%s error=%v",
			state.chat.ID,
			err,
		)
		return
	}
	if err := p.createChats(ctx, []models.Chat{state.chat}); err != nil {
		logx.Errorf(
			"restore missing chat query index failed: chat_id=%s error=%v",
			state.chat.ID,
			err,
		)
	}
}

// indexStartedMessage 以权威提交结果建立或推进本轮消息对应的查询索引。
func (p *queryIndexStore) indexStartedMessage(
	ctx context.Context,
	isNew bool,
	chat models.Chat,
	execution models.Execution,
	afterSequence int64,
) {
	if isNew {
		p.writeInitialIndexes(ctx, chat)
		return
	}
	p.updateChatIndex(ctx, chat.ID, afterSequence, map[string]any{
		"latest_sequence":       chat.LatestSequence,
		"active_execution_id":   execution.ID,
		"last_runtime_id":       execution.RuntimeID,
		"last_account_id":       execution.AccountID,
		"last_model":            execution.Model,
		"last_reasoning":        execution.Reasoning,
		"last_fast":             execution.Fast,
		"last_context_window":   execution.ContextWindow,
		"last_permission_level": execution.PermissionLevel,
		"updated_at":            chat.UpdatedAt,
	})
}

// indexExecutionStarted 推进执行状态和 Chat 权威游标。
func (p *queryIndexStore) indexExecutionStarted(
	execution models.Execution,
	record eventlog.Record,
) {
	p.updateChatIndex(context.Background(), execution.ChatID, record.Sequence-1, map[string]any{
		"latest_sequence": record.Sequence,
		"updated_at":      record.RecordedAt,
	})
}

// indexChatCursor 只推进不改变查询业务字段的权威事件游标。
func (p *queryIndexStore) indexChatCursor(
	chatID string,
	afterSequence int64,
	record eventlog.Record,
) {
	p.updateChatIndex(context.Background(), chatID, afterSequence, map[string]any{
		"latest_sequence": record.Sequence,
		"updated_at":      record.RecordedAt,
	})
}

// renameChat 保存标题查询字段和对应权威游标。
func (p *queryIndexStore) renameChat(
	ctx context.Context,
	chatID string,
	title string,
	record eventlog.Record,
) error {
	err := p.advanceChatReading(ctx, chatID, record.Sequence-1, map[string]any{
		"title":           title,
		"latest_sequence": record.Sequence,
		"updated_at":      record.RecordedAt,
	}, []eventlog.Record{record})
	if err != nil {
		p.reportProjectionFailure(chatID, err)
	}
	return err
}

// deleteChats 在一个事务中删除全部 Chat 查询索引并提交可选聚合事实清理。
func (p *queryIndexStore) deleteChats(
	ctx context.Context,
	chatIDs []string,
	finalize func(context.Context) error,
) error {
	return p.transactionController.Transaction(ctx, func(transactionContext context.Context) error {
		for _, chatID := range chatIDs {
			if p.deleted != nil {
				events.AfterCommit(transactionContext, func() { p.deleted(chatID) })
			}
			if p.reading != nil {
				if err := p.reading.DeleteReading(transactionContext, chatID); err != nil {
					return err
				}
			}
			if err := p.chatController.Delete(transactionContext, chatID); err != nil {
				return err
			}
		}
		if finalize != nil {
			return finalize(transactionContext)
		}
		return nil
	})
}
