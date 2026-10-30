package chat

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/projectsource"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// chatState 是一份 Chat JSONL 完整重放后的权威业务状态。
type chatState struct {
	// executions 保留每个已接纳回合的权威身份和终态，供稳定身份对账。
	executions map[string]*models.Execution
	// chat 是对话身份、工作范围和当前执行状态。
	chat models.Chat
	// createRequestID 是普通 Chat 首次发送的客户端幂等标识。
	createRequestID string
	// createRequestHash 是普通 Chat 首次发送规范化业务内容的摘要。
	createRequestHash string
	// initialExecution 是首发建立的第一条 Execution，并随终态事件推进。
	initialExecution *models.Execution
	// runtimeSessions 是每个 Runtime 当前仍然有效的原生会话。
	runtimeSessions map[string]models.RuntimeSession
	// projectSources 是工作区 projects/ 下全部来源入口的冻结快照。
	projectSources []eventlog.ProjectSourceSnapshot
	// threadWorkspaceID 非空时表示 Chat 绑定 thread 级共享执行工作区。
	threadWorkspaceID string
}

// ChatSummary 是打开 Chat 时不重放完整 JSONL 的查询摘要。
type ChatSummary struct {
	// Chat 是对话元数据查询结果。
	Chat models.Chat `json:"chat"`
}

// MessageHistoryPage 是按消息创建序号向前读取的一页权威聊天历史。
type MessageHistoryPage struct {
	// 末轮执行标识
	ForkLatestExecutionID string `json:"forkLatestExecutionId,omitempty"`
	// Resources 是按创建顺序升序排列的消息窗口。
	Resources []messageview.Message `json:"resources"`
	// NextBeforeSequence 是继续读取更早消息时使用的稳定游标。
	NextBeforeSequence int64 `json:"nextBeforeSequence,omitempty"`
	// LatestSequence 是最新窗口实际覆盖到的 JSONL 事件游标。
	LatestSequence int64 `json:"latestSequence,omitempty"`
	// HasMore 表示当前窗口之前是否仍有更早消息。
	HasMore bool `json:"hasMore"`
}

// ListChatsInput 保存最近对话稳定游标分页条件。
type ListChatsInput struct {
	// WakerID 是需要筛选的伙伴标识。
	WakerID string
	// BeforeUpdatedAt 是上一页末项的最近活动时间。
	BeforeUpdatedAt int64
	// BeforeID 是相同活动时间下的稳定标识游标。
	BeforeID string
	// Limit 是本次最多返回的 Chat 数量。
	Limit int
}

// ChatList 是最近对话的分页结果。
type ChatList struct {
	// Resources 是按最近活动倒序排列的 Chat 查询结果。
	Resources []models.Chat
	// NextBeforeUpdatedAt 是继续加载更早 Chat 的活动时间游标。
	NextBeforeUpdatedAt int64
	// NextBeforeID 是继续加载时相同活动时间下的稳定标识游标。
	NextBeforeID string
	// HasMore 表示当前页之前仍有更早 Chat。
	HasMore bool
}

// readChatRecords 统一读取一份 Chat 权威日志。
func (m *service) readChatRecords(chatID string) ([]eventlog.Record, error) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return nil, status_error.BadRequest
	}
	records, err := m.logStore.ReadAll(chatID)
	if err != nil {
		return nil, err
	}
	return records, nil
}

// loadChatState 读取并重放权威日志，调用方无需分别解释事件和查询索引。
func (m *service) loadChatState(
	chatID string,
) ([]eventlog.Record, chatState, error) {
	records, err := m.readChatRecords(chatID)
	if err != nil {
		return nil, chatState{}, err
	}
	state, err := replayChatState(records)
	if err != nil {
		logx.Errorf(
			"replay chat authority failed: chat_id=%s error=%v",
			strings.TrimSpace(chatID),
			err,
		)
		return nil, chatState{}, status_error.ChatLogError
	}
	return records, state, nil
}

// GetChatSummary 从查询索引返回打开 Chat 所需的有界摘要。
func (m *service) GetChatSummary(
	ctx context.Context,
	chatID string,
) (ChatSummary, error) {
	chatID = strings.TrimSpace(chatID)
	if err := m.catchUpLiveProjection(ctx, chatID); err != nil {
		return ChatSummary{}, err
	}
	return m.queryIndexes.getSummary(ctx, chatID)
}

// ListChats 返回一位伙伴的最近对话及首屏草稿可复用的最近有效项目。
func (m *service) ListChats(
	ctx context.Context,
	input ListChatsInput,
) (ChatList, error) {
	input.WakerID = strings.TrimSpace(input.WakerID)
	input.BeforeID = strings.TrimSpace(input.BeforeID)
	if input.WakerID == "" || input.BeforeUpdatedAt < 0 ||
		(input.BeforeUpdatedAt == 0) != (input.BeforeID == "") ||
		input.Limit < 1 || input.Limit > 100 {
		return ChatList{}, status_error.BadRequest
	}
	return m.queryIndexes.readChatList(ctx, input)
}

// CountMessagesByWakerGroupedByDay 从每 Chat 派生索引精确统计指定时区的消息活动。
func (m *service) CountMessagesByWakerGroupedByDay(
	ctx context.Context,
	wakerID string,
	startMs int64,
	endMs int64,
	loc *time.Location,
) (map[string]int64, error) {
	wakerID = strings.TrimSpace(wakerID)
	if wakerID == "" || startMs < 0 || endMs < startMs {
		return nil, status_error.BadRequest
	}
	if loc == nil {
		loc = time.Local
	}
	chatIDs, err := m.queryIndexes.chatController.ListIDsByWaker(ctx, wakerID)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int64)
	for _, chatID := range chatIDs {
		if err := ctx.Err(); err != nil {
			return nil, chatContextStatus(err)
		}
		times, readErr := m.logStore.MessageRecordedTimes(chatID, startMs, endMs)
		if readErr != nil {
			return nil, readErr
		}
		for _, recordedAt := range times {
			counts[time.UnixMilli(recordedAt).In(loc).Format("2006-01-02")]++
		}
	}
	return counts, nil
}

// RecentAvailableProject 按 Chat 最近活动顺序查找伙伴仍可使用的项目。
func (m *service) RecentAvailableProject(ctx context.Context, wakerID string) (string, error) {
	wakerID = strings.TrimSpace(wakerID)
	if wakerID == "" {
		return "", status_error.BadRequest
	}
	var beforeUpdatedAt int64
	var beforeID string
	const batchSize = 100
	for {
		rows, err := m.queryIndexes.chatController.ListRecentProjectCandidates(ctx, wakerID, beforeUpdatedAt, beforeID, batchSize)
		if err != nil {
			return "", err
		}
		if len(rows) == 0 {
			return "", nil
		}
		if m.projectController == nil {
			return "", status_error.ProjectServiceUnavailable
		}
		for _, row := range rows {
			project, getErr := m.projectController.Get(ctx, row.ProjectID)
			if getErr != nil || project.WakerID != wakerID {
				continue
			}
			directories, sourceErr := m.projectController.ListDirectories(ctx, row.ProjectID)
			if sourceErr != nil || len(directories) == 0 {
				continue
			}
			available := true
			for _, directory := range directories {
				available = available && projectsource.Available(directory.Source)
			}
			if available {
				return row.ProjectID, nil
			}
		}
		if len(rows) < batchSize {
			return "", nil
		}
		last := rows[len(rows)-1]
		beforeUpdatedAt, beforeID = last.UpdatedAt, last.ID
	}
}

// ReadMessageHistory 按 JSONL 消息创建序号读取一个有界历史窗口。
func (m *service) ReadMessageHistory(
	ctx context.Context,
	chatID string,
	beforeSequence int64,
	limit int,
) (MessageHistoryPage, error) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" || beforeSequence < 0 || limit < 1 || limit > 100 {
		return MessageHistoryPage{}, status_error.BadRequest
	}
	if err := ctx.Err(); err != nil {
		return MessageHistoryPage{}, chatContextStatus(err)
	}
	window, err := m.logStore.ReadMessageWindow(chatID, beforeSequence, limit)
	if err != nil {
		return MessageHistoryPage{}, err
	}
	if len(window.Records) == 0 {
		return MessageHistoryPage{Resources: []messageview.Message{}}, nil
	}
	builder := messageview.NewPublicMessageViewBuilder()
	for _, record := range window.Records {
		if _, err = builder.Apply(record); err != nil {
			break
		}
	}
	messages := builder.Messages()
	if err != nil {
		logx.Errorf(
			"project chat message window failed: chat_id=%s error=%v",
			chatID,
			err,
		)
		return MessageHistoryPage{}, status_error.ChatLogError
	}
	page := MessageHistoryPage{
		ForkLatestExecutionID: builder.LatestForkExecutionID(),
		Resources:             messages,
		NextBeforeSequence:    window.NextBeforeSequence,
		LatestSequence:        window.LatestSequence,
		HasMore:               window.HasMore,
	}
	if beforeSequence > 0 {
		page.LatestSequence = 0
		page.ForkLatestExecutionID = ""
	}
	return page, nil
}

// GetMessageFileChange 从权威 JSONL 重放并返回指定消息的执行级文件 Diff。
func (m *service) GetMessageFileChange(
	ctx context.Context,
	chatID string,
	messageID string,
	path string,
) (messageview.MessageFileChangeDiff, error) {
	chatID = strings.TrimSpace(chatID)
	messageID = strings.TrimSpace(messageID)
	path = strings.TrimSpace(path)
	if chatID == "" || messageID == "" || path == "" {
		return messageview.MessageFileChangeDiff{}, status_error.BadRequest
	}
	if err := ctx.Err(); err != nil {
		return messageview.MessageFileChangeDiff{}, chatContextStatus(err)
	}
	records, err := m.logStore.ReadMessage(chatID, messageID)
	if err != nil {
		return messageview.MessageFileChangeDiff{}, err
	}
	messages, err := messageview.ReplayMessages(records)
	if err != nil {
		logx.Errorf(
			"project chat message file change failed: chat_id=%s message_id=%s error=%v",
			chatID,
			messageID,
			err,
		)
		return messageview.MessageFileChangeDiff{}, status_error.ChatLogError
	}
	for _, message := range messages {
		if message.ID != messageID {
			continue
		}
		for _, diff := range message.FileChangeDiffs {
			if diff.Path == path {
				return diff, nil
			}
		}
		break
	}
	return messageview.MessageFileChangeDiff{},
		status_error.ChatMessageFileChangeNotFound.WithParams(
			map[string]interface{}{"Path": path},
		)
}

// chatContextStatus 把查询 Context 终态收敛为稳定状态错误。
func chatContextStatus(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return status_error.OperationTimeout
	}
	return status_error.OperationCanceled
}

// replayChatState 单次扫描权威日志并恢复所有核心业务状态与不变量。
func replayChatState(records []eventlog.Record) (chatState, error) {
	if len(records) == 0 ||
		records[0].Type != constants.ChatEventTypeCreated {
		return chatState{}, status_error.ChatLogCorrupted
	}
	var data eventlog.ChatCreatedData
	if err := json.Unmarshal(records[0].Data, &data); err != nil {
		return chatState{}, status_error.ChatLogCorrupted
	}
	last := records[len(records)-1]
	state := chatState{
		executions: map[string]*models.Execution{},
		chat: models.Chat{
			ID:                    records[0].ChatID,
			ForkSourceChatID:      data.ForkSourceChatID,
			ForkSourceExecutionID: data.ForkSourceExecutionID,
			WakerID:               data.WakerID,
			ProjectID:             data.ProjectID,
			ProjectName:           data.ProjectName,
			WorkspaceDirectory:    data.WorkspaceDirectory,
			AdditionalWorkingDirectories: append(
				[]string(nil),
				data.AdditionalWorkingDirectories...,
			),
			Title:                  data.Title,
			HiddenFromWakerHistory: data.HiddenFromWakerHistory,
			LatestSequence:         last.Sequence,
			CreatedAt:              records[0].RecordedAt,
			UpdatedAt:              last.RecordedAt,
		},
		createRequestID:   strings.TrimSpace(data.ClientRequestID),
		createRequestHash: strings.TrimSpace(data.ClientRequestHash),
		runtimeSessions:   make(map[string]models.RuntimeSession),
		projectSources:    append([]eventlog.ProjectSourceSnapshot(nil), data.ProjectSources...),
		threadWorkspaceID: strings.TrimSpace(data.ThreadWorkspaceID),
	}
	if data.ForkConfiguration != nil {
		state.chat.LastRuntimeID = data.ForkConfiguration.RuntimeID
		state.chat.LastModel = data.ForkConfiguration.Model
		state.chat.LastReasoning = data.ForkConfiguration.Reasoning
		state.chat.LastPermissionLevel = data.ForkConfiguration.PermissionLevel
	}
	var activeExecution *models.Execution

	// 所有核心状态共享这一次有序扫描，新增事件时只需要维护一套状态转换规则。
	for _, record := range records[1:] {
		switch record.Type {
		case constants.ChatEventTypeUserMessageCreated:
			if record.MessageID == "" {
				return chatState{}, status_error.ChatLogCorrupted
			}
		case constants.ChatEventTypeTitleUpdated:
			var titleData eventlog.ChatTitleUpdatedData
			if err := json.Unmarshal(record.Data, &titleData); err != nil {
				return chatState{}, status_error.ChatLogCorrupted
			}
			if strings.TrimSpace(titleData.Title) == "" {
				return chatState{}, status_error.ChatLogCorrupted
			}
			state.chat.Title = titleData.Title
		case constants.ChatEventTypeExecutionCreated:
			if record.ExecutionID == "" {
				return chatState{}, status_error.ChatLogCorrupted
			}
			if activeExecution != nil || state.chat.ActiveExecutionID != "" {
				return chatState{}, status_error.ChatLogCorrupted
			}
			var executionData eventlog.ExecutionCreatedData
			if err := json.Unmarshal(record.Data, &executionData); err != nil {
				return chatState{}, status_error.ChatLogCorrupted
			}
			execution := models.Execution{AutomaticRunID: executionData.AutomaticRunID,
				ID:                record.ExecutionID,
				ChatID:            record.ChatID,
				UserMessageID:     executionData.UserMessageID,
				PluginResources:   executionData.PluginResources,
				SubmissionID:      executionData.SubmissionID,
				SubmissionHash:    executionData.SubmissionHash,
				ParentExecutionID: executionData.ParentExecutionID,
				AccountID:         executionData.AccountID,
				RuntimeID:         executionData.RuntimeID,
				Model:             executionData.Model,
				Reasoning:         executionData.Reasoning,
				PermissionLevel:   executionData.PermissionLevel,
				WorkspaceReadOnly: executionData.WorkspaceReadOnly,
				Status:            constants.ExecutionStatusPending,
				Revision:          1,
				CreatedAt:         record.RecordedAt,
				UpdatedAt:         record.RecordedAt,
			}
			activeExecution = &execution
			if _, exists := state.executions[execution.ID]; exists {
				return chatState{}, status_error.ChatLogCorrupted
			}
			state.executions[execution.ID] = activeExecution
			if state.initialExecution == nil {
				state.initialExecution = activeExecution
			}
			state.chat.ActiveExecutionID = record.ExecutionID
			state.chat.LastAccountID = executionData.AccountID
			state.chat.LastRuntimeID = executionData.RuntimeID
			state.chat.LastModel = executionData.Model
			state.chat.LastReasoning = executionData.Reasoning
			state.chat.LastPermissionLevel = executionData.PermissionLevel
		case constants.ChatEventTypeExecutionStarted:
			if activeExecution == nil || activeExecution.ID != record.ExecutionID {
				return chatState{}, status_error.ChatLogCorrupted
			}
			activeExecution.Status = constants.ExecutionStatusRunning
			activeExecution.Revision++
			activeExecution.UpdatedAt = record.RecordedAt
		case constants.ChatEventTypeApprovalRequested:
			if state.chat.ActiveExecutionID != record.ExecutionID {
				return chatState{}, status_error.ChatLogCorrupted
			}
			var requested eventlog.ApprovalRequestedData
			if err := json.Unmarshal(record.Data, &requested); err != nil {
				return chatState{}, status_error.ChatLogCorrupted
			}
			if requested.ApprovalID == "" || requested.ToolCallID == "" ||
				len(requested.Options) == 0 {
				return chatState{}, status_error.ChatLogCorrupted
			}
			state.chat.PendingApprovals = append(
				state.chat.PendingApprovals,
				pendingApprovalProjection(requested, record.RecordedAt, record.Sequence),
			)
			state.chat.WaitingApproval = true
		case constants.ChatEventTypeApprovalResolved:
			var resolved eventlog.ApprovalResolvedData
			if err := json.Unmarshal(record.Data, &resolved); err != nil {
				return chatState{}, err
			}
			remaining := state.chat.PendingApprovals[:0]
			found := false
			for _, approval := range state.chat.PendingApprovals {
				if approval.ApprovalID == resolved.ApprovalID {
					found = true
					continue
				}
				remaining = append(remaining, approval)
			}
			if !found {
				return chatState{}, status_error.ChatLogCorrupted
			}
			state.chat.PendingApprovals = remaining
			state.chat.WaitingApproval = len(remaining) > 0
		case constants.ChatEventTypeElicitationRequested:
			if state.chat.ActiveExecutionID != record.ExecutionID {
				return chatState{}, status_error.ChatLogCorrupted
			}
			var requested eventlog.ElicitationRequestedData
			if err := json.Unmarshal(record.Data, &requested); err != nil {
				return chatState{}, status_error.ChatLogCorrupted
			}
			if requested.ElicitationID == "" || requested.Message == "" {
				return chatState{}, status_error.ChatLogCorrupted
			}
			state.chat.PendingElicitations = append(
				state.chat.PendingElicitations,
				pendingElicitationProjection(requested, record.RecordedAt, record.Sequence),
			)
			state.chat.WaitingElicitation = true
		case constants.ChatEventTypeElicitationResolved:
			var resolved eventlog.ElicitationResolvedData
			if err := json.Unmarshal(record.Data, &resolved); err != nil {
				return chatState{}, err
			}
			remaining := state.chat.PendingElicitations[:0]
			found := false
			for _, elicitation := range state.chat.PendingElicitations {
				if elicitation.ElicitationID == resolved.ElicitationID {
					found = true
					continue
				}
				remaining = append(remaining, elicitation)
			}
			if !found {
				return chatState{}, status_error.ChatLogCorrupted
			}
			state.chat.PendingElicitations = remaining
			state.chat.WaitingElicitation = len(remaining) > 0
		case constants.ChatEventTypeExecutionCompleted,
			constants.ChatEventTypeExecutionFailed,
			constants.ChatEventTypeExecutionCanceled:
			if activeExecution == nil || activeExecution.ID != record.ExecutionID {
				return chatState{}, status_error.ChatLogCorrupted
			}
			var finished eventlog.ExecutionFinishedData
			if err := json.Unmarshal(record.Data, &finished); err != nil {
				return chatState{}, status_error.ChatLogCorrupted
			}
			activeExecution.Status = finished.Status
			activeExecution.ErrorCode = finished.ErrorCode
			activeExecution.Revision++
			activeExecution.UpdatedAt = record.RecordedAt
			completedAt := record.RecordedAt
			activeExecution.CompletedAt = &completedAt
			state.chat.LastEndedExecutionID = record.ExecutionID
			state.chat.LastEndedExecutionStatus = finished.Status
			state.chat.LastEndedExecutionAt = &completedAt
			if state.chat.ActiveExecutionID == record.ExecutionID {
				state.chat.ActiveExecutionID = ""
				state.chat.PendingApprovals = []models.PendingApproval{}
				state.chat.WaitingApproval = false
				state.chat.PendingElicitations = []models.PendingElicitation{}
				state.chat.WaitingElicitation = false
			}
			activeExecution = nil
		case constants.ChatEventTypeNativeSessionUpdated,
			constants.ChatEventTypeNativeSessionCleared:
			var session eventlog.RuntimeSessionData
			if err := json.Unmarshal(record.Data, &session); err != nil {
				return chatState{}, status_error.ChatLogCorrupted
			}
			if session.RuntimeID == "" {
				return chatState{}, status_error.ChatLogCorrupted
			}
			if record.Type == constants.ChatEventTypeNativeSessionCleared {
				delete(state.runtimeSessions, session.RuntimeID)
				continue
			}
			if session.SessionID == "" {
				return chatState{}, status_error.ChatLogCorrupted
			}
			state.runtimeSessions[session.RuntimeID] = models.RuntimeSession{
				ChatID:          record.ChatID,
				RuntimeID:       session.RuntimeID,
				SessionID:       session.SessionID,
				LastExecutionID: session.LastExecutionID,
				UpdatedAt:       record.RecordedAt,
			}
		}
	}
	return state, nil
}

func pendingApprovalProjection(
	requested eventlog.ApprovalRequestedData,
	requestedAt int64,
	requestedSequence int64,
) models.PendingApproval {
	locations := make([]models.PendingApprovalLocation, 0, len(requested.Locations))
	for _, location := range requested.Locations {
		var line *int
		if location.Line != nil {
			lineValue := *location.Line
			line = &lineValue
		}
		locations = append(locations, models.PendingApprovalLocation{
			Path: location.Path,
			Line: line,
		})
	}
	options := make([]models.PendingApprovalOption, 0, len(requested.Options))
	for _, option := range requested.Options {
		options = append(options, models.PendingApprovalOption{
			ID: option.ID, Name: option.Name, Kind: option.Kind,
		})
	}
	return models.PendingApproval{
		ApprovalID:        requested.ApprovalID,
		ToolCallID:        requested.ToolCallID,
		Title:             requested.Title,
		Kind:              requested.Kind,
		Summary:           requested.Summary,
		Command:           requested.Command,
		Locations:         locations,
		Options:           options,
		RequestedAt:       requestedAt,
		RequestedSequence: requestedSequence,
	}
}

func pendingElicitationProjection(
	requested eventlog.ElicitationRequestedData,
	requestedAt int64,
	requestedSequence int64,
) models.PendingElicitation {
	return models.PendingElicitation{
		AllowSkip:         requested.AllowSkip,
		ElicitationID:     requested.ElicitationID,
		ToolCallID:        requested.ToolCallID,
		Mode:              requested.Mode,
		Message:           requested.Message,
		RequestedSchema:   append(json.RawMessage(nil), requested.RequestedSchema...),
		URL:               requested.URL,
		RequestedAt:       requestedAt,
		RequestedSequence: requestedSequence,
	}
}

// RenameChat 先记录标题权威事实，再推进可重建的查询索引。
func (m *service) RenameChat(
	ctx context.Context,
	chatID string,
	title string,
) error {
	chatID = strings.TrimSpace(chatID)
	title = strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
	if title == "" || len([]rune(title)) > 120 {
		return status_error.BadRequest
	}
	if _, _, err := m.loadChatState(chatID); err != nil {
		return err
	}
	records, err := m.eventCommitter.append(chatID, []eventlog.AppendInput{{
		Type: constants.ChatEventTypeTitleUpdated,
		Data: marshalData(eventlog.ChatTitleUpdatedData{
			Title: title,
		}),
	}})
	if err != nil {
		return err
	}
	last := records[len(records)-1]
	indexErr := m.queryIndexes.renameChat(ctx, chatID, title, last)
	m.eventCommitter.publishCommitted(records)
	return indexErr
}

// chatDeletionTarget 保存完成统一预检后的单个 Chat 删除范围。
type chatDeletionTarget struct {
	// chatID 是目标 Chat 标识。
	chatID string
	// workspaceDirectory 是归属已经验证的受管执行目录。
	workspaceDirectory string
	// threadWorkspaceID 非空时表示目录属于 thread 级共享执行工作区。
	threadWorkspaceID string
	// threadWorkspaceDirectory 是团队对话工作区聚合根目录。
	threadWorkspaceDirectory string
}

// DeleteChat 删除对话权威日志、查询索引和归属可验证的项目内目录。
func (m *service) DeleteChat(ctx context.Context, chatID string) error {
	return m.DeleteChats(ctx, []string{chatID})
}

// DeleteChats 在统一预检全部目标后批量删除权威日志、查询索引和受管目录。
func (m *service) DeleteChats(ctx context.Context, chatIDs []string) error {
	return m.DeleteChatsAtomically(ctx, chatIDs, nil)
}

// DeleteChatsAtomically 先 staging 完整聚合目录，再原子提交 Chat 索引和上层聚合事实删除。
func (m *service) DeleteChatsAtomically(
	ctx context.Context,
	chatIDs []string,
	finalize func(context.Context) error,
) error {
	releaseOperation, operationErr := chatstorage.AcquireOperation(m.storage.Root(), chatIDs...)
	if operationErr != nil {
		return operationErr
	}
	defer releaseOperation()
	targets := make([]chatDeletionTarget, 0, len(chatIDs))
	seen := make(map[string]struct{}, len(chatIDs))
	threadTargets := make(map[string]struct{})
	for _, value := range chatIDs {
		chatID := strings.TrimSpace(value)
		if chatID == "" {
			return status_error.BadRequest
		}
		if _, duplicated := seen[chatID]; duplicated {
			continue
		}
		seen[chatID] = struct{}{}
		// 删除前从权威日志恢复冻结工作目录，不能信任可能落后的 SQLite。
		_, state, err := m.loadChatState(chatID)
		if hasStatusErrorCode(err, status_error.ChatNotFound.Code()) {
			// 删除以最终不存在为目标，日志已缺失时仍需清理派生索引和上层关联。
			continue
		}
		if err != nil {
			return err
		}
		chat := state.chat
		// Execution Core 必须先完成 durable 终态；Chat 不拥有 Runtime 生命周期。
		if chat.ActiveExecutionID != "" {
			return status_error.ChatHasActiveExecution
		}
		if chat.WorkspaceDirectory == "" {
			return status_error.ChatDirectoryOwnershipInvalid
		}
		threadWorkspaceID := strings.TrimSpace(state.threadWorkspaceID)
		if threadWorkspaceID != "" {
			threadWorkspaceDirectory, pathErr := m.storage.TeamWorkspace(threadWorkspaceID)
			if pathErr != nil {
				return pathErr
			}
			leadWorkspace, leadPathErr := m.storage.TeamMemberWorkspace(threadWorkspaceID, chat.WakerID, true)
			memberWorkspace, memberPathErr := m.storage.TeamMemberWorkspace(threadWorkspaceID, chat.WakerID, false)
			if leadPathErr != nil || memberPathErr != nil ||
				(filepath.Clean(chat.WorkspaceDirectory) != filepath.Clean(leadWorkspace) &&
					filepath.Clean(chat.WorkspaceDirectory) != filepath.Clean(memberWorkspace)) {
				return status_error.ChatDirectoryOwnershipInvalid
			}
			if err := workspace.ValidateThreadMemberExecutionWorkspace(
				chat.WorkspaceDirectory,
				threadWorkspaceID,
				chatID,
				chat.WakerID,
			); err != nil {
				logx.Errorf(
					"validate managed thread member workspace deletion failed: chat_id=%s thread_id=%s error=%v",
					chatID,
					threadWorkspaceID,
					err,
				)
				return status_error.ChatDirectoryOwnershipInvalid
			}
			if err := workspace.ValidateThreadExecutionWorkspaceDeletion(
				threadWorkspaceDirectory,
				threadWorkspaceID,
			); err != nil {
				logx.Errorf(
					"validate managed thread workspace deletion failed: thread_id=%s error=%v",
					threadWorkspaceID,
					err,
				)
				return status_error.ChatDirectoryOwnershipInvalid
			}
			threadTargets[threadWorkspaceID] = struct{}{}
			targets = append(targets, chatDeletionTarget{
				chatID:                   chatID,
				workspaceDirectory:       chat.WorkspaceDirectory,
				threadWorkspaceID:        threadWorkspaceID,
				threadWorkspaceDirectory: threadWorkspaceDirectory,
			})
			continue
		}
		expectedWorkspace, pathErr := m.storage.ChatWorkspace(chatID)
		if pathErr != nil || filepath.Clean(chat.WorkspaceDirectory) != filepath.Clean(expectedWorkspace) {
			return status_error.ChatDirectoryOwnershipInvalid
		}
		if err := workspace.ValidateExecutionWorkspaceDeletion(chat.WorkspaceDirectory, chatID); err != nil {
			logx.Errorf("validate managed chat deletion failed: chat_id=%s error=%v", chatID, err)
			return status_error.ChatDirectoryOwnershipInvalid
		}
		targets = append(targets, chatDeletionTarget{
			chatID:             chatID,
			workspaceDirectory: chat.WorkspaceDirectory,
			threadWorkspaceID:  threadWorkspaceID,
		})
	}
	for threadID := range threadTargets {
		registered, err := m.logStore.ListThreadChatIDs(threadID)
		if err != nil {
			return err
		}
		for _, chatID := range registered {
			if _, exists := seen[chatID]; !exists {
				return status_error.BadRequest
			}
		}
	}
	canonicalChatIDs := uniqueChatIDs(chatIDs)
	if len(targets) == 0 {
		if err := m.queryIndexes.deleteChats(ctx, canonicalChatIDs, finalize); err != nil {
			return err
		}
		m.logStore.Forget(canonicalChatIDs)
		return m.cancelForksFromDeletedChats(seen)
	}
	// 只为仍存在的子 Chat 补发布标记，避免在清理缺失关联时重新创建日志。
	visible := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		visible[target.chatID] = struct{}{}
	}
	if err := m.sealVisibleForks(ctx, visible); err != nil {
		return err
	}
	aggregates := make([]chatstorage.DeletionTarget, 0, len(threadTargets)+len(targets))
	for threadID := range threadTargets {
		aggregates = append(aggregates, chatstorage.DeletionTarget{
			Kind: chatstorage.AggregateKindTeam, AggregateID: threadID,
		})
	}
	for _, target := range targets {
		if target.threadWorkspaceID == "" {
			aggregates = append(aggregates, chatstorage.DeletionTarget{
				Kind: chatstorage.AggregateKindChat, AggregateID: target.chatID,
			})
		}
	}
	if err := m.deleteAggregatesAtomically(ctx, aggregates, canonicalChatIDs, finalize); err != nil {
		return err
	}
	return m.cancelForksFromDeletedChats(seen)
}

// DeleteProvisionalTeam 回滚未 ready 的 Team 聚合和可能只完成一部分的成员 Chat。
func (m *service) DeleteProvisionalTeam(
	ctx context.Context,
	threadID string,
	provisioningID string,
	chatIDs []string,
) error {
	threadID = strings.TrimSpace(threadID)
	provisioningID = strings.TrimSpace(provisioningID)
	expected := uniqueChatIDs(chatIDs)
	if threadID == "" || provisioningID == "" || len(expected) == 0 {
		return status_error.BadRequest
	}
	if err := m.storage.RollbackTeamProvisioning(provisioningID, threadID); err != nil {
		return err
	}
	teamRoot, err := m.storage.TeamRoot(threadID)
	if err != nil {
		return err
	}
	if _, err := os.Stat(teamRoot); errors.Is(err, os.ErrNotExist) {
		m.logStore.Forget(expected)
		return m.queryIndexes.deleteChats(ctx, expected, nil)
	} else if err != nil {
		return status_error.ManagedWorkspaceError
	}
	registered, err := m.logStore.ListThreadChatIDs(threadID)
	if err != nil {
		return err
	}
	expectedSet := make(map[string]struct{}, len(expected))
	for _, chatID := range expected {
		expectedSet[chatID] = struct{}{}
	}
	for _, chatID := range registered {
		if _, allowed := expectedSet[chatID]; !allowed {
			return status_error.ChatDirectoryOwnershipInvalid
		}
		_, state, loadErr := m.loadChatState(chatID)
		if loadErr != nil {
			return loadErr
		}
		if state.threadWorkspaceID != threadID {
			return status_error.ChatDirectoryOwnershipInvalid
		}
		if state.chat.ActiveExecutionID != "" {
			return status_error.ChatHasActiveExecution
		}
	}
	workspaceRoot, err := m.storage.TeamWorkspace(threadID)
	if err != nil {
		return err
	}
	if err := workspace.ValidateThreadExecutionWorkspaceDeletion(workspaceRoot, threadID); err != nil {
		return err
	}
	if err := m.validateProvisionalTeamLogs(threadID, expectedSet); err != nil {
		return err
	}
	return m.deleteAggregatesAtomically(
		ctx,
		[]chatstorage.DeletionTarget{{
			Kind:        chatstorage.AggregateKindTeam,
			AggregateID: threadID,
		}},
		expected,
		nil,
	)
}

// validateProvisionalTeamLogs 拒绝删除计划成员集合以外的任何 Team 日志文件。
func (m *service) validateProvisionalTeamLogs(
	threadID string,
	expected map[string]struct{},
) error {
	logDirectory, err := m.storage.TeamLogDirectory(threadID)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(logDirectory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return status_error.ChatLogError
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return status_error.ChatDirectoryOwnershipInvalid
		}
		extension := filepath.Ext(entry.Name())
		if extension != ".jsonl" && extension != ".idx" {
			return status_error.ChatDirectoryOwnershipInvalid
		}
		chatID := strings.TrimSuffix(entry.Name(), extension)
		if _, allowed := expected[chatID]; !allowed {
			return status_error.ChatDirectoryOwnershipInvalid
		}
	}
	return nil
}

// deleteAggregatesAtomically 用统一 deletion intent 提交已经完成归属预检的聚合删除。
func (m *service) deleteAggregatesAtomically(
	ctx context.Context,
	aggregates []chatstorage.DeletionTarget,
	canonicalChatIDs []string,
	finalize func(context.Context) error,
) error {
	intentID := util.New()
	if err := m.deletionIntents.CreatePrepared(ctx, models.ChatDeletionIntent{
		ID: intentID, State: "prepared", ChatIDs: canonicalChatIDs,
	}); err != nil {
		return err
	}
	staged, err := m.deletions.Stage(intentID, aggregates, canonicalChatIDs)
	if err != nil {
		if cleanupErr := m.deletionIntents.Delete(ctx, intentID); cleanupErr != nil {
			logx.Errorf("cleanup unstaged chat deletion intent failed: intent_id=%s error=%v", intentID, cleanupErr)
		}
		return err
	}
	commitFacts := func(transactionContext context.Context) error {
		if finalize != nil {
			if err := finalize(transactionContext); err != nil {
				return err
			}
		}
		return m.deletionIntents.MarkCommitted(
			transactionContext,
			intentID,
			time.Now().UnixMilli(),
		)
	}
	if err := m.queryIndexes.deleteChats(ctx, canonicalChatIDs, commitFacts); err != nil {
		if rollbackErr := staged.Rollback(); rollbackErr != nil {
			logx.Errorf("rollback failed chat aggregate deletion failed: error=%v", rollbackErr)
			return rollbackErr
		}
		if refreshErr := m.logStore.RefreshCatalog(); refreshErr != nil {
			return refreshErr
		}
		if cleanupErr := m.deletionIntents.Delete(ctx, intentID); cleanupErr != nil {
			logx.Errorf("cleanup rolled back chat deletion intent failed: intent_id=%s error=%v", intentID, cleanupErr)
		}
		return err
	}
	m.logStore.Forget(canonicalChatIDs)
	if err := staged.Commit(); err != nil {
		return err
	}
	if err := m.deletionIntents.Delete(ctx, intentID); err != nil {
		logx.Errorf("cleanup committed chat deletion intent failed: intent_id=%s error=%v", intentID, err)
	}
	return nil
}

func uniqueChatIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// DeleteWakerChats 从权威 JSONL 枚举并删除一位伙伴的全部对话。
func (m *service) DeleteWakerChats(ctx context.Context, wakerID string) error {
	wakerID = strings.TrimSpace(wakerID)
	if wakerID == "" {
		return status_error.BadRequest
	}
	release, err := chatstorage.AcquireOperation(m.storage.Root(), "waker:"+wakerID)
	if err != nil {
		return err
	}
	defer release()
	chatIDs, err := m.logStore.ListChatIDs()
	if err != nil {
		logx.Errorf("list chat authority failed: waker_id=%s error=%v", wakerID, err)
		return status_error.ChatLogError
	}
	targetIDs := make([]string, 0)
	for _, chatID := range chatIDs {
		_, state, loadErr := m.loadChatState(chatID)
		if hasStatusErrorCode(loadErr, status_error.ChatNotFound.Code()) {
			// 全局地址登记可能尚未落日志或已被其他删除清理，不阻断伙伴级联删除。
			continue
		}
		if loadErr != nil {
			return loadErr
		}
		if state.chat.WakerID != wakerID {
			continue
		}
		// 团队 Chat 属于不可拆分的历史聚合，由团队删除流程统一管理。
		if state.threadWorkspaceID != "" {
			continue
		}
		if state.chat.ActiveExecutionID != "" {
			return status_error.WakerHasRunningWork
		}
		targetIDs = append(targetIDs, chatID)
	}
	if err := m.DeleteChats(ctx, targetIDs); err != nil {
		return err
	}
	return m.cancelUnpublishedForksForWaker(wakerID)
}

// CountUserMessages 只统计当前会话实际记录的用户输入，不累计继承消息或平台收件重试。
func (m *service) CountUserMessages(ctx context.Context, chatID string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, chatContextStatus(err)
	}
	return m.logStore.CountUserMessages(chatID)
}
