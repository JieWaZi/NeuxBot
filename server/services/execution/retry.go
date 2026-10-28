package execution

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// runtimeFailureRetryable 只允许明确的短暂拒绝，任何已观察输出、工具或未知会话均阻止重放。
func runtimeFailureRetryable(result runtimes.Result) bool {
	if result.Failure == nil || result.SideEffects.Any() || result.Failure.SideEffects.Any() ||
		result.UndeliveredEvents != 0 || result.UndeliveredBytes != 0 {
		return false
	}
	if result.Failure.Code != runtimes.FailureCodeRateLimited &&
		result.Failure.Code != runtimes.FailureCodeUpstreamUnavailable {
		return false
	}
	return strings.TrimSpace(reliableRuntimeSessionID(result)) != ""
}

// retryExecution 保留失败事实，按父执行派生唯一子执行，重复请求只返回同一个结果。
func (m *service) retryExecution(ctx context.Context, chatID, executionID string, selected ...runtimeRunner) (AcceptedExecution, error) {
	if chatID == "" || executionID == "" {
		return AcceptedExecution{}, status_error.BadRequest
	}
	identity, err := requestidempotency.New(executionID, executionID)
	if err != nil {
		return AcceptedExecution{}, err
	}
	retryID := identity.ResourceID("execution-retry", chatID)
	if !m.createRequests.reserve(retryID) {
		return AcceptedExecution{}, status_error.ExecutionRetryInProgress
	}
	defer m.createRequests.release(retryID)
	snapshot, err := m.journal.LoadExecutionContext(ctx, chatID)
	if err != nil {
		return AcceptedExecution{}, err
	}
	records, err := m.journal.ReadRecords(chatID)
	if err != nil {
		return AcceptedExecution{}, err
	}
	replayed, _, err := executionFromRecords(records, retryID)
	if err != nil {
		return AcceptedExecution{}, err
	}
	if replayed.ID != "" {
		return AcceptedExecution{Chat: snapshot.Chat, Execution: replayed, Replayed: true}, nil
	}
	state := messageChat{
		chat:              snapshot.Chat,
		previousMessages:  snapshot.PreviousMessages,
		runtimeSessions:   snapshot.RuntimeSessions,
		threadWorkspaceID: snapshot.ThreadWorkspaceID,
	}
	previous, message, err := retrySource(state, records, executionID)
	if err != nil {
		return AcceptedExecution{}, err
	}
	runner := m.runtime
	if len(selected) > 0 {
		runner = selected[0]
	}
	request := pendingMessage{automaticRunID: previous.AutomaticRunID,
		runner:            runner,
		chatID:            chatID,
		executionID:       retryID,
		parentExecutionID: executionID,
		retry:             true,
		pluginSnapshots:   previous.PluginResources,
		admission:         constants.ExecutionAdmissionCommitBeforeDispatch,
		runtime: runtimeSelection{
			RuntimeID:         previous.RuntimeID,
			Model:             previous.Model,
			Reasoning:         previous.Reasoning,
			PermissionLevel:   previous.PermissionLevel,
			WorkspaceReadOnly: previous.WorkspaceReadOnly,
			SessionPolicy:     constants.RuntimeSessionPolicyContinue,
		},
		message: userMessage{Actor: message.Actor, Text: message.Content},
	}
	workspaceRoot, err := m.storage.ChatWorkspace(chatID)
	if err != nil {
		return AcceptedExecution{}, err
	}
	for _, attachment := range message.Attachments {
		request.message.Attachments = append(request.message.Attachments, AttachmentInput{
			SourcePath: filepath.Join(workspaceRoot, attachment.Path),
		})
	}
	for _, plugin := range message.Plugins {
		request.message.PluginIDs = append(request.message.PluginIDs, plugin.ID)
	}
	for _, skill := range message.Skills {
		request.message.SkillIDs = append(request.message.SkillIDs, skill.ID)
	}
	for _, connector := range message.Connectors {
		request.message.ConnectorIDs = append(request.message.ConnectorIDs, connector.ID)
	}
	for _, ref := range message.CodeReferences {
		request.message.CodeReferences = append(request.message.CodeReferences, CodeReferenceInput{
			Path: ref.Path, StartLine: ref.StartLine, EndLine: ref.EndLine, Content: ref.Content,
		})
	}
	return m.startMessage(ctx, request)
}

// retrySource 用最新权威状态验证重放资格；调用方在取得执行占位后必须再次校验。
func retrySource(
	state messageChat, records []eventlog.Record, executionID string,
) (models.Execution, messageview.Message, error) {
	var input messageview.Message
	previous, latestID, err := executionFromRecords(records, executionID)
	if err != nil {
		return previous, input, err
	}
	if state.chat.ActiveExecutionID != "" {
		return previous, input, status_error.ChatHasActiveExecution
	}
	if previous.ID == "" || previous.Status != constants.ExecutionStatusFailed ||
		latestID != executionID || state.threadWorkspaceID != "" {
		return previous, input, status_error.ExecutionRetryUnavailable
	}
	// 缺失、清除或已被其他执行推进的原生会话不能退回历史播种后重复原输入。
	session := state.runtimeSessions[previous.RuntimeID]
	if strings.TrimSpace(session.SessionID) == "" || session.LastExecutionID != executionID {
		return previous, input, status_error.ExecutionRetryUnavailable
	}
	allowed := false
	for _, message := range state.previousMessages {
		if message.ExecutionID == executionID && message.Role == constants.MessageRoleAssistant {
			if message.Status != constants.ExecutionStatusFailed || !message.Retryable ||
				message.Content != "" || len(message.DisplayParts) != 0 || len(message.FileChanges) != 0 {
				return previous, input, status_error.ExecutionRetryUnavailable
			}
			allowed = true
		}
		if message.ID == previous.UserMessageID && message.Role == constants.MessageRoleUser {
			input = message
		}
	}
	if !allowed || input.Actor != "user" {
		return previous, input, status_error.ExecutionRetryUnavailable
	}
	return previous, input, nil
}

// executionFromRecords 从权威事件恢复指定执行及最后执行身份，不依赖可能滞后的查询索引。
func executionFromRecords(records []eventlog.Record, executionID string) (models.Execution, string, error) {
	var execution models.Execution
	latestID := ""
	for _, record := range records {
		if record.Type == constants.ChatEventTypeExecutionCreated {
			latestID = record.ExecutionID
		}
		if record.ExecutionID != executionID {
			continue
		}
		switch record.Type {
		case constants.ChatEventTypeExecutionCreated:
			var created eventlog.ExecutionCreatedData
			if json.Unmarshal(record.Data, &created) != nil {
				return execution, latestID, status_error.ChatLogError
			}
			execution = models.Execution{
				PluginResources:   created.PluginResources,
				AccountID:         created.AccountID,
				ID:                executionID,
				ChatID:            record.ChatID,
				UserMessageID:     created.UserMessageID,
				ParentExecutionID: created.ParentExecutionID,
				RuntimeID:         created.RuntimeID,
				Model:             created.Model,
				Reasoning:         created.Reasoning,
				PermissionLevel:   created.PermissionLevel,
				WorkspaceReadOnly: created.WorkspaceReadOnly,
				Status:            constants.ExecutionStatusPending,
				CreatedAt:         record.RecordedAt,
				UpdatedAt:         record.RecordedAt,
			}
		case constants.ChatEventTypeExecutionStarted:
			execution.Status = constants.ExecutionStatusRunning
		case constants.ChatEventTypeExecutionCompleted,
			constants.ChatEventTypeExecutionFailed,
			constants.ChatEventTypeExecutionCanceled:
			var finished eventlog.ExecutionFinishedData
			if json.Unmarshal(record.Data, &finished) != nil {
				return execution, latestID, status_error.ChatLogError
			}
			execution.Status = finished.Status
			execution.ErrorCode = finished.ErrorCode
			execution.UpdatedAt = record.RecordedAt
		}
	}
	return execution, latestID, nil
}
