package chat

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// retentionDeletion 仅在当前会话排他门内持有一次删除的授权和实际提交结果。
type retentionDeletion struct {
	// candidate 固定预览的会话身份与日志版本。
	candidate controllers.ChatRetentionCandidate
	// cutoff 是本轮固定截止时间。
	cutoff int64
	// allow 在进入暂存前重新核对策略版本及用户取消。
	allow func(context.Context, string) (bool, error)
	// committed 标记数据库删除已提交，即使随后回收失败。
	committed bool
}

// retentionProtectionData 表示一次明确的用户选择；缺字段不能还原成取消保护。
type retentionProtectionData struct {
	// Protected 是用户请求的保留选择。
	Protected *bool `json:"protected"`
}

func applyRetentionRecord(chat *models.Chat, record eventlog.Record) error {
	if record.Type == constants.ChatEventTypeRetentionProtectionUpdated {
		var data retentionProtectionData
		if json.Unmarshal(record.Data, &data) != nil || data.Protected == nil {
			return status_error.ChatLogCorrupted
		}
		chat.RetentionProtected = *data.Protected
	}
	if contributesWorkActivity(record) {
		chat.LastActivityAt = max(chat.LastActivityAt, record.RecordedAt)
	}
	return nil
}

func contributesWorkActivity(record eventlog.Record) bool {
	switch record.Type {
	case constants.ChatEventTypeCreated, constants.ChatEventTypeUserMessageCreated, constants.ChatEventTypeExecutionCreated, constants.ChatEventTypeExecutionStarted, constants.ChatEventTypeExecutionCompleted, constants.ChatEventTypeExecutionFailed, constants.ChatEventTypeExecutionCanceled, constants.ChatEventTypeAssistantMessage, constants.ChatEventTypeToolMessage:
		return true
	case constants.ChatEventTypeApprovalResolved:
		var result eventlog.ApprovalResolvedData
		return json.Unmarshal(record.Data, &result) == nil && result.Result == constants.ApprovalResultSelected
	case constants.ChatEventTypeElicitationResolved:
		var result eventlog.ElicitationResolvedData
		return json.Unmarshal(record.Data, &result) == nil && (result.Result == constants.ElicitationResultAccepted || result.Result == constants.ElicitationResultDeclined)
	case constants.ChatEventTypeUnspecified:
		return isNativeCheckpoint(record) || record.InvocationID != ""
	default:
		return false
	}
}

func (m *service) retentionReader(ctx context.Context) (controllers.ChatRetentionReader, error) {
	ids := chatstorage.BusyChatIDs(m.storage.Root())
	if r := m.readingRepair; r != nil {
		r.mu.Lock()
		incomplete := r.overflow || r.scanning
		for id := range r.pending {
			ids = append(ids, id)
		}
		for id := range r.isolated {
			ids = append(ids, id)
		}
		r.mu.Unlock()
		if incomplete {
			return nil, status_error.ReadingProjectionIncomplete
		}
	}
	intents, err := m.deletionIntents.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, intent := range intents {
		ids = append(ids, intent.ChatIDs...)
	}
	err = m.visitForkOperations(func(_ string, op chatstorage.ForkOperation) error {
		if !op.Published && !op.Cancelled {
			ids = append(ids, op.ChatID, op.SourceChatID)
		}
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	return m.queryIndexes.chatController.ExcludingRetentionChats(ids), nil
}

func (m *service) ListRetentionCandidates(ctx context.Context, cutoff, afterActivity int64, afterID string, limit int) ([]controllers.ChatRetentionCandidate, error) {
	reader, err := m.retentionReader(ctx)
	if err != nil {
		return nil, err
	}
	return reader.ListRetentionCandidates(ctx, cutoff, afterActivity, afterID, limit)
}
func (m *service) CountRetentionCandidates(ctx context.Context, cutoff int64) (int64, error) {
	reader, err := m.retentionReader(ctx)
	if err != nil {
		return 0, err
	}
	return reader.CountRetentionCandidates(ctx, cutoff)
}
func (m *service) ListRetentionCandidatesByIDs(ctx context.Context, cutoff int64, ids []string) ([]controllers.ChatRetentionCandidate, error) {
	reader, err := m.retentionReader(ctx)
	if err != nil {
		return nil, err
	}
	return reader.ListRetentionCandidatesByIDs(ctx, cutoff, ids)
}

func (m *service) SetRetentionProtection(ctx context.Context, id string, protected bool) error {
	release, err := chatstorage.AcquireOperation(m.storage.Root(), id)
	if err != nil {
		return err
	}
	defer release()
	_, state, err := m.loadChatState(id)
	if err != nil {
		return err
	}
	if state.chat.RetentionProtected == protected {
		return m.catchUpLiveProjection(ctx, id)
	}
	records, err := m.eventCommitter.append(id, []eventlog.AppendInput{{Type: constants.ChatEventTypeRetentionProtectionUpdated, Data: marshalData(retentionProtectionData{Protected: &protected})}})
	if err != nil {
		return err
	}
	err = m.catchUpProjectionThrough(ctx, id, records[len(records)-1].Sequence)
	if err != nil {
		m.queryIndexes.reportProjectionFailure(id, err)
	}
	m.eventCommitter.publishCommitted(records)
	return err
}

func (m *service) DeleteChatIfExpired(ctx context.Context, candidate controllers.ChatRetentionCandidate, cutoff int64, allow func(context.Context, string) (bool, error)) (bool, error) {
	if allow == nil || candidate.ChatID == "" || cutoff <= 0 {
		return false, status_error.BadRequest
	}
	retention := retentionDeletion{candidate: candidate, cutoff: cutoff, allow: allow}
	err := m.deleteChatsAtomically(ctx, []string{candidate.ChatID}, nil, nil, &retention)
	if !retention.committed && (hasStatusErrorCode(err, status_error.ChatLogVersionConflict.Code()) || hasStatusErrorCode(err, status_error.ChatHasActiveExecution.Code()) || hasStatusErrorCode(err, status_error.ChatNotFound.Code()) || hasStatusErrorCode(err, status_error.ChatLogCorrupted.Code()) || hasStatusErrorCode(err, status_error.ChatDirectoryOwnershipInvalid.Code())) {
		return false, nil
	}
	return retention.committed, err
}

func (m *service) checkRetention(ctx context.Context, candidate controllers.ChatRetentionCandidate, cutoff int64, state chatState) error {
	chat := state.chat
	if chat.LatestSequence != candidate.Sequence || chat.LastActivityAt != candidate.LastActivityAt || chat.LastActivityAt <= 0 || chat.LastActivityAt >= cutoff || chat.RetentionProtected || chat.HiddenFromWakerHistory || chat.WakerID == "" || state.threadWorkspaceID != "" || chat.ActiveExecutionID != "" || chat.WaitingApproval || chat.WaitingElicitation || m.ReadingIncompleteReason([]string{chat.ID}) != "" {
		return status_error.ChatLogVersionConflict
	}
	for _, execution := range state.executions {
		if execution.AutomaticRunID != "" {
			return status_error.ChatLogVersionConflict
		}
	}
	ok, err := m.queryIndexes.chatController.IsRetentionCandidate(ctx, candidate, cutoff)
	if err != nil {
		return err
	}
	if !ok {
		return status_error.ChatLogVersionConflict
	}
	intents, err := m.deletionIntents.List(ctx)
	if err != nil {
		return err
	}
	for _, intent := range intents {
		if slices.Contains(intent.ChatIDs, chat.ID) {
			return status_error.ChatLogVersionConflict
		}
	}
	return m.visitForkOperations(func(_ string, op chatstorage.ForkOperation) error {
		if !op.Published && !op.Cancelled && (op.SourceChatID == chat.ID || op.ChatID == chat.ID) {
			return status_error.ChatLogVersionConflict
		}
		return nil
	})
}
