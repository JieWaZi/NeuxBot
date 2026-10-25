package chat

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// RecoveryDiagnostic 保存一个 Chat 最近一次启动恢复的可查询结果。
type RecoveryDiagnostic struct {
	// ChatID 是被检查的 Chat 标识。
	ChatID string `json:"chatId"`
	// State 是 ChatRecoveryState 枚举值。
	State constants.ChatRecoveryState `json:"state"`
	// Issues 是本次检查发现的 ChatRecoveryIssue 问题码列表。
	Issues []constants.ChatRecoveryIssue `json:"issues,omitempty"`
	// LogLatestSequence 是权威 JSONL 的最终事件序号。
	LogLatestSequence int64 `json:"logLatestSequence"`
	// IndexedSequence 是检查前 Chat SQLite 当前投影的事件序号。
	IndexedSequence int64 `json:"indexedSequence"`
	// TailBytesRemoved 是本次或此前最近一次安全尾行修复删除的字节数。
	TailBytesRemoved int64 `json:"tailBytesRemoved,omitempty"`
	// TailRepairedAt 是最近一次安全尾行修复完成的 Unix 毫秒时间。
	TailRepairedAt int64 `json:"tailRepairedAt,omitempty"`
	// Error 是隔离时不包含秘密值的技术错误说明。
	Error string `json:"error,omitempty"`
}

// RecoveryHealth 汇总最近一次启动时全部 Chat 当前投影的健康状态。
type RecoveryHealth struct {
	// CheckedAt 是本次恢复检查完成的 Unix 毫秒时间。
	CheckedAt int64 `json:"checkedAt"`
	// Chats 是按 Chat 标识排序的逐项检查结果。
	Chats []RecoveryDiagnostic `json:"chats"`
	// Missing 是检查时缺失 SQLite 当前投影的 Chat 数量。
	Missing int `json:"missing"`
	// Lagging 是检查时存在落后 SQLite 当前投影的 Chat 数量。
	Lagging int `json:"lagging"`
	// Corrupt 是存在无法安全读取 JSONL 的 Chat 数量。
	Corrupt int `json:"corrupt"`
	// Isolated 是本次启动中被隔离的 Chat 数量。
	Isolated int `json:"isolated"`
	// Repaired 是完成增量追平、尾行修复或中断执行收尾的数量。
	Repaired int `json:"repaired"`
	// Rebuilt 是从 JSONL 完成全量投影或文件索引重建的数量。
	Rebuilt int `json:"rebuilt"`
}

// recoveryCoordinator 隔离启动恢复职责，正常路径只读取 SQLite 游标后的 JSONL 尾部。
type recoveryCoordinator struct {
	// logStore 读取、修复并索引权威 Chat JSONL。
	logStore *eventlog.Store
	// queryIndexes 提供可从 JSONL 重建的 SQLite 当前投影。
	queryIndexes *queryIndexStore
	// healthMutex 保护最近一次恢复健康快照。
	healthMutex sync.RWMutex
	// health 保存最近一次完整启动恢复结果。
	health RecoveryHealth
}

// currentProjection 是正常启动增量恢复使用的有界当前状态。
type currentProjection struct {
	// chat 是一 Chat 一行的当前查询状态。
	chat models.Chat
	// runtimeSessions 是每个 Runtime 最多一行的当前续接状态。
	runtimeSessions map[string]models.RuntimeSession
	// result 是事务外准备的原成果引用，不承载正文。
	result *models.WorkResultReceipt
}

// newRecoveryCoordinator 创建仅在 Chat 模块启动阶段运行的恢复器。
func newRecoveryCoordinator(
	logStore *eventlog.Store,
	queryIndexes *queryIndexStore,
) *recoveryCoordinator {
	return &recoveryCoordinator{logStore: logStore, queryIndexes: queryIndexes}
}

// RecoveryHealth 返回最近一次启动恢复结果的防御性副本。
func (m *service) RecoveryHealth() RecoveryHealth {
	if m == nil || m.recovery == nil {
		return RecoveryHealth{Chats: []RecoveryDiagnostic{}}
	}
	return m.recovery.healthSnapshot()
}

// healthSnapshot 返回恢复结果的深拷贝。
func (c *recoveryCoordinator) healthSnapshot() RecoveryHealth {
	c.healthMutex.RLock()
	defer c.healthMutex.RUnlock()
	health := c.health
	health.Chats = append([]RecoveryDiagnostic(nil), health.Chats...)
	for index := range health.Chats {
		health.Chats[index].Issues = append(
			[]constants.ChatRecoveryIssue(nil),
			health.Chats[index].Issues...,
		)
	}
	return health
}

// reconcileAll 逐 Chat 隔离恢复，单个损坏日志不会阻止 Core 启动。
func (c *recoveryCoordinator) reconcileAll(ctx context.Context) error {
	chatIDs, err := c.logStore.ListChatIDs()
	if err != nil {
		return status_error.ChatLogError
	}
	health := RecoveryHealth{Chats: make([]RecoveryDiagnostic, 0, len(chatIDs))}
	for _, chatID := range chatIDs {
		diagnostic, recoveryErr := c.reconcileChat(ctx, chatID)
		if recoveryErr != nil {
			diagnostic.ChatID = chatID
			diagnostic.State = constants.ChatRecoveryStateIsolated
			diagnostic.Issues = appendIssue(
				diagnostic.Issues,
				constants.ChatRecoveryIssueLogCorruptOrRecoveryFailed,
			)
			diagnostic.Error = recoveryErr.Error()
			health.Corrupt++
			health.Isolated++
			logx.Errorf(
				"isolate chat recovery failure: chat_id=%s error=%v",
				chatID,
				recoveryErr,
			)
		}
		accumulateRecoveryHealth(&health, diagnostic)
		health.Chats = append(health.Chats, diagnostic)
	}
	health.CheckedAt = time.Now().UnixMilli()
	c.healthMutex.Lock()
	c.health = health
	c.healthMutex.Unlock()
	return nil
}

// reconcileChat 校验文件索引后，从 SQLite checkpoint 增量读取 JSONL 尾部。
func (c *recoveryCoordinator) reconcileChat(
	ctx context.Context,
	chatID string,
) (RecoveryDiagnostic, error) {
	diagnostic := RecoveryDiagnostic{ChatID: chatID, State: constants.ChatRecoveryStateHealthy}
	indexRecovery, err := c.logStore.RecoverIndex(chatID)
	if err != nil {
		return diagnostic, err
	}
	diagnostic.LogLatestSequence = indexRecovery.LatestSequence
	diagnostic.TailBytesRemoved = indexRecovery.TailBytesRemoved
	diagnostic.TailRepairedAt = indexRecovery.TailRepairedAt
	if indexRecovery.Rebuilt {
		diagnostic.Issues = appendIssue(diagnostic.Issues, constants.ChatRecoveryIssueFileIndexRebuilt)
		diagnostic.State = constants.ChatRecoveryStateRepaired
	}
	if indexRecovery.TailBytesRemoved > 0 {
		diagnostic.Issues = appendIssue(diagnostic.Issues, constants.ChatRecoveryIssueTailRepaired)
		diagnostic.State = constants.ChatRecoveryStateRepaired
	}

	indexed, missing, err := c.queryIndexes.inspectChat(ctx, chatID)
	if err != nil {
		return diagnostic, err
	}
	if missing {
		diagnostic.Issues = appendIssue(diagnostic.Issues, constants.ChatRecoveryIssueChatProjectionMissing)
		return c.rebuildProjection(ctx, chatID, diagnostic)
	}
	diagnostic.IndexedSequence = indexed.LatestSequence
	if indexed.LatestSequence > indexRecovery.LatestSequence {
		diagnostic.Issues = appendIssue(diagnostic.Issues, constants.ChatRecoveryIssueChatProjectionAhead)
		return c.rebuildProjection(ctx, chatID, diagnostic)
	}
	if valid, validateErr := c.staticProjectionMatches(chatID, indexed); validateErr != nil {
		return diagnostic, validateErr
	} else if !valid {
		diagnostic.Issues = appendIssue(diagnostic.Issues, constants.ChatRecoveryIssueChatProjectionContentMismatch)
		return c.rebuildProjection(ctx, chatID, diagnostic)
	}

	sessions, err := c.queryIndexes.runtimeSessionController.ListByChat(ctx, chatID)
	if err != nil {
		return diagnostic, err
	}
	projection := currentProjection{
		chat:            indexed,
		runtimeSessions: runtimeSessionsByID(sessions),
	}
	tail, err := c.logStore.ReadAfter(chatID, indexed.LatestSequence, 0)
	if err != nil {
		return diagnostic, err
	}
	if len(tail) > 0 {
		diagnostic.Issues = appendIssue(diagnostic.Issues, constants.ChatRecoveryIssueChatProjectionLagging)
		diagnostic.State = constants.ChatRecoveryStateRepaired
		if err := applyProjectionTail(&projection, tail); err != nil {
			return c.rebuildProjection(ctx, chatID, diagnostic)
		}
	}
	if projection.chat.ActiveExecutionID != "" {
		canceled, appendErr := c.logStore.Append(chatID, []eventlog.AppendInput{{
			Type:        constants.ChatEventTypeExecutionCanceled,
			ExecutionID: projection.chat.ActiveExecutionID,
			Data: marshalData(eventlog.ExecutionFinishedData{
				Status:   constants.ExecutionStatusCanceled,
				ExitCode: -1,
			}),
		}})
		if appendErr != nil {
			return diagnostic, appendErr
		}
		if err := applyProjectionTail(&projection, canceled); err != nil {
			return diagnostic, err
		}
		diagnostic.LogLatestSequence = canceled[len(canceled)-1].Sequence
		diagnostic.Issues = appendIssue(
			diagnostic.Issues,
			constants.ChatRecoveryIssueInterruptedExecutionCanceled,
		)
		diagnostic.State = constants.ChatRecoveryStateRepaired
	}
	if len(tail) > 0 || projection.chat.LatestSequence != indexed.LatestSequence {
		if err := c.queryIndexes.replaceCurrentProjection(ctx, projection); err != nil {
			return diagnostic, err
		}
	}
	return diagnostic, nil
}

// rebuildProjection 仅在 SQLite 当前投影缺失或不可信时全量重放 JSONL。
func (c *recoveryCoordinator) rebuildProjection(
	ctx context.Context,
	chatID string,
	diagnostic RecoveryDiagnostic,
) (RecoveryDiagnostic, error) {
	records, err := c.logStore.ReadAll(chatID)
	if err != nil {
		return diagnostic, err
	}
	state, err := replayChatState(records)
	if err != nil {
		return diagnostic, err
	}
	if state.chat.ActiveExecutionID != "" {
		canceled, appendErr := c.logStore.Append(chatID, []eventlog.AppendInput{{
			Type:        constants.ChatEventTypeExecutionCanceled,
			ExecutionID: state.chat.ActiveExecutionID,
			Data: marshalData(eventlog.ExecutionFinishedData{
				Status:   constants.ExecutionStatusCanceled,
				ExitCode: -1,
			}),
		}})
		if appendErr != nil {
			return diagnostic, appendErr
		}
		records = append(records, canceled...)
		state, err = replayChatState(records)
		if err != nil {
			return diagnostic, err
		}
		diagnostic.Issues = appendIssue(
			diagnostic.Issues,
			constants.ChatRecoveryIssueInterruptedExecutionCanceled,
		)
	}
	projection := currentProjection{
		chat:            state.chat,
		runtimeSessions: state.runtimeSessions,
	}
	if err := c.queryIndexes.replaceCurrentProjection(ctx, projection); err != nil {
		return diagnostic, err
	}
	diagnostic.LogLatestSequence = state.chat.LatestSequence
	diagnostic.State = constants.ChatRecoveryStateRebuilt
	return diagnostic, nil
}

// staticProjectionMatches 以常量级首行读取校验不会变化的 Chat 身份和工作范围。
func (c *recoveryCoordinator) staticProjectionMatches(
	chatID string,
	indexed models.Chat,
) (bool, error) {
	created, err := c.logStore.ReadAfter(chatID, 0, 1)
	if err != nil || len(created) != 1 || created[0].Type != constants.ChatEventTypeCreated {
		return false, err
	}
	var data eventlog.ChatCreatedData
	if err := json.Unmarshal(created[0].Data, &data); err != nil {
		return false, err
	}
	return indexed.ID == chatID &&
		indexed.WakerID == data.WakerID &&
		indexed.ProjectID == data.ProjectID &&
		indexed.ProjectName == data.ProjectName &&
		indexed.WorkspaceDirectory == data.WorkspaceDirectory &&
		indexed.HiddenFromWakerHistory == data.HiddenFromWakerHistory &&
		slices.Equal(
			indexed.AdditionalWorkingDirectories,
			data.AdditionalWorkingDirectories,
		) && indexed.CreatedAt == created[0].RecordedAt, nil
}

// applyProjectionTail 把 checkpoint 后记录应用到有界当前投影。
func applyProjectionTail(projection *currentProjection, records []eventlog.Record) error {
	for _, record := range records {
		if record.Sequence <= projection.chat.LatestSequence {
			return status_error.ChatLogError
		}
		switch record.Type {
		case constants.ChatEventTypeTitleUpdated:
			var data eventlog.ChatTitleUpdatedData
			if err := json.Unmarshal(record.Data, &data); err != nil {
				return err
			}
			projection.chat.Title = data.Title
		case constants.ChatEventTypeExecutionCreated:
			if projection.chat.ActiveExecutionID != "" {
				return status_error.ChatLogError
			}
			var data eventlog.ExecutionCreatedData
			if err := json.Unmarshal(record.Data, &data); err != nil {
				return err
			}
			projection.chat.ActiveExecutionID = record.ExecutionID
			projection.chat.LastAccountID = data.AccountID
			projection.chat.LastRuntimeID = data.RuntimeID
			projection.chat.LastModel = data.Model
			projection.chat.LastReasoning = data.Reasoning
			projection.chat.LastPermissionLevel = data.PermissionLevel
		case constants.ChatEventTypeApprovalRequested:
			var data eventlog.ApprovalRequestedData
			if err := json.Unmarshal(record.Data, &data); err != nil {
				return err
			}
			projection.chat.PendingApprovals = append(
				projection.chat.PendingApprovals,
				pendingApprovalProjection(data, record.RecordedAt, record.Sequence),
			)
			projection.chat.WaitingApproval = true
		case constants.ChatEventTypeApprovalResolved:
			var data eventlog.ApprovalResolvedData
			if err := json.Unmarshal(record.Data, &data); err != nil {
				return err
			}
			remaining := projection.chat.PendingApprovals[:0]
			for _, approval := range projection.chat.PendingApprovals {
				if approval.ApprovalID != data.ApprovalID {
					remaining = append(remaining, approval)
				}
			}
			projection.chat.PendingApprovals = remaining
			projection.chat.WaitingApproval = len(remaining) > 0
		case constants.ChatEventTypeElicitationRequested:
			var data eventlog.ElicitationRequestedData
			if err := json.Unmarshal(record.Data, &data); err != nil {
				return err
			}
			projection.chat.PendingElicitations = append(
				projection.chat.PendingElicitations,
				pendingElicitationProjection(data, record.RecordedAt, record.Sequence),
			)
			projection.chat.WaitingElicitation = true
		case constants.ChatEventTypeElicitationResolved:
			var data eventlog.ElicitationResolvedData
			if err := json.Unmarshal(record.Data, &data); err != nil {
				return err
			}
			remaining := projection.chat.PendingElicitations[:0]
			for _, elicitation := range projection.chat.PendingElicitations {
				if elicitation.ElicitationID != data.ElicitationID {
					remaining = append(remaining, elicitation)
				}
			}
			projection.chat.PendingElicitations = remaining
			projection.chat.WaitingElicitation = len(remaining) > 0
		case constants.ChatEventTypeExecutionCompleted,
			constants.ChatEventTypeExecutionFailed,
			constants.ChatEventTypeExecutionCanceled:
			if projection.chat.ActiveExecutionID != record.ExecutionID {
				return status_error.ChatLogError
			}
			var ended eventlog.ExecutionFinishedData
			if err := json.Unmarshal(record.Data, &ended); err != nil {
				return err
			}
			projection.chat.LastEndedExecutionID = record.ExecutionID
			projection.chat.LastEndedExecutionStatus = ended.Status
			at := record.RecordedAt
			projection.chat.LastEndedExecutionAt = &at
			projection.chat.ActiveExecutionID = ""
			projection.chat.PendingApprovals = []models.PendingApproval{}
			projection.chat.WaitingApproval = false
			projection.chat.PendingElicitations = []models.PendingElicitation{}
			projection.chat.WaitingElicitation = false
		case constants.ChatEventTypeNativeSessionUpdated,
			constants.ChatEventTypeNativeSessionCleared:
			var data eventlog.RuntimeSessionData
			if err := json.Unmarshal(record.Data, &data); err != nil {
				return err
			}
			if record.Type == constants.ChatEventTypeNativeSessionCleared {
				delete(projection.runtimeSessions, data.RuntimeID)
				break
			}
			projection.runtimeSessions[data.RuntimeID] = models.RuntimeSession{
				ChatID:          record.ChatID,
				RuntimeID:       data.RuntimeID,
				SessionID:       data.SessionID,
				LastExecutionID: data.LastExecutionID,
				UpdatedAt:       record.RecordedAt,
			}
		}
		projection.chat.LatestSequence = record.Sequence
		projection.chat.UpdatedAt = record.RecordedAt
	}
	return nil
}

// runtimeSessionsByID 把数据库结果转换为按 Runtime 标识索引的当前状态。
func runtimeSessionsByID(sessions []models.RuntimeSession) map[string]models.RuntimeSession {
	result := make(map[string]models.RuntimeSession, len(sessions))
	for _, session := range sessions {
		result[session.RuntimeID] = session
	}
	return result
}

// inspectChat 区分当前投影缺失和数据库读取故障。
func (p *queryIndexStore) inspectChat(
	ctx context.Context,
	chatID string,
) (models.Chat, bool, error) {
	indexed, err := p.chatController.Get(ctx, chatID)
	missing := hasStatusErrorCode(err, status_error.ChatNotFound.Code())
	if err != nil && !missing {
		return models.Chat{}, false, err
	}
	return indexed, missing, nil
}

// replaceCurrentProjection 在一个事务内替换一 Chat 的全部有界 SQLite 当前状态。
func (p *queryIndexStore) replaceCurrentProjection(
	ctx context.Context,
	projection currentProjection,
) error {
	if projection.result == nil && projection.chat.LastEndedExecutionID != "" {
		var err error
		projection.result, err = p.prepareResult(projection.chat.ID, projection.chat.LastEndedExecutionID)
		if err != nil {
			return err
		}
	}
	return p.transactionController.Transaction(ctx, func(transactionContext context.Context) error {
		if err := p.runtimeSessionController.DeleteByChat(
			transactionContext,
			projection.chat.ID,
		); err != nil {
			return err
		}
		if err := p.chatController.Delete(transactionContext, projection.chat.ID); err != nil {
			return err
		}
		if err := p.chatController.Create(transactionContext, projection.chat); err != nil {
			return err
		}
		for _, session := range projection.runtimeSessions {
			if err := p.runtimeSessionController.Upsert(transactionContext, session); err != nil {
				return err
			}
		}
		return p.publishResult(transactionContext, projection.result)
	})
}

// hasStatusErrorCode 判断仓储错误是否为指定稳定业务码。
func hasStatusErrorCode(err error, code int64) bool {
	var commonErr statuserror.CommonError
	return errors.As(err, &commonErr) && commonErr.Code() == code
}

// appendIssue 追加不重复的 ChatRecoveryIssue 问题码。
func appendIssue(
	issues []constants.ChatRecoveryIssue,
	issue constants.ChatRecoveryIssue,
) []constants.ChatRecoveryIssue {
	for _, existing := range issues {
		if existing == issue {
			return issues
		}
	}
	return append(issues, issue)
}

// accumulateRecoveryHealth 将稳定问题码归并为健康接口的聚合计数。
func accumulateRecoveryHealth(health *RecoveryHealth, diagnostic RecoveryDiagnostic) {
	rebuilt := diagnostic.State == constants.ChatRecoveryStateRebuilt
	for _, issue := range diagnostic.Issues {
		switch issue {
		case constants.ChatRecoveryIssueChatProjectionMissing:
			health.Missing++
		case constants.ChatRecoveryIssueChatProjectionLagging:
			health.Lagging++
		case constants.ChatRecoveryIssueFileIndexRebuilt:
			rebuilt = true
		}
	}
	if rebuilt {
		health.Rebuilt++
	}
	switch diagnostic.State {
	case constants.ChatRecoveryStateRebuilt:
		health.Repaired++
	case constants.ChatRecoveryStateRepaired:
		health.Repaired++
	}
}
