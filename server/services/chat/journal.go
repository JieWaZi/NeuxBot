package chat

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"

	"github.com/JieWaZi/NeuxBot/server/constants"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	eventstream "github.com/JieWaZi/NeuxBot/server/internal/chat/eventstream"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// ExecutionContext 是 Execution Core 准备一次工作所需的权威 Chat 快照。
type ExecutionContext struct {
	// SubmittedExecutions 为续发入口提供来自权威日志的稳定收据。
	SubmittedExecutions map[string]models.Execution
	// Chat 是从 JSONL 重放得到的 Chat 当前状态。
	Chat models.Chat
	// CreateRequestID 是普通 Chat 首次发送的客户端幂等标识。
	CreateRequestID string
	// CreateRequestHash 是普通 Chat 首次发送规范化业务内容的摘要。
	CreateRequestHash string
	// InitialExecution 是普通 Chat 首次发送建立的第一条 Execution；团队空 Chat 为空。
	InitialExecution *models.Execution
	// PreviousMessages 是构建 Runtime 历史所需的完整消息投影。
	PreviousMessages []messageview.Message
	// RuntimeSessions 是按 Runtime 标识索引的当前原生 Session。
	RuntimeSessions map[string]models.RuntimeSession
	// ProjectSources 是 Chat 创建时冻结的项目来源映射。
	ProjectSources []eventlog.ProjectSourceSnapshot
	// ThreadWorkspaceID 非空时表示 Chat 绑定 thread 级共享执行工作区。
	ThreadWorkspaceID string
}

// StartedTurn 描述 Runtime 接受输入后需要原子提交的首批事实和投影。
type StartedTurn struct {
	// NewChat 表示本批次需要建立新的 Chat JSONL。
	NewChat bool
	// Chat 是提交成功后需要建立或更新的 Chat 查询投影。
	Chat models.Chat
	// Execution 是本批次创建的执行查询投影。
	Execution models.Execution
	// Facts 是已经完成业务校验且保持稳定顺序的权威事实。
	Facts []eventlog.AppendInput
}

// DormantChat 描述只建立身份和冻结工作范围、尚未产生消息或执行的 Chat。
type DormantChat struct {
	// Chat 是需要建立的空闲 Chat 查询投影。
	Chat models.Chat
	// ProjectSources 是 Chat 工作区项目入口的权威冻结快照。
	ProjectSources []eventlog.ProjectSourceSnapshot
	// ThreadWorkspaceID 非空时表示 Chat 绑定 thread 级共享执行工作区。
	ThreadWorkspaceID string
}

// AcceptedTurn 是首批事实提交成功后的稳定结果。
type AcceptedTurn struct {
	// Chat 是已经推进到本批次最终 Sequence 的 Chat 投影。
	Chat models.Chat
	// Execution 是已经进入 running 状态的执行投影。
	Execution models.Execution
}

// TerminalFact 描述一个 Execution 的唯一终态事实及其查询投影变化。
type TerminalFact struct {
	// Execution 是进入终态前的执行投影。
	Execution models.Execution
	// Type 是 completed、failed 或 canceled 终态事件类型。
	Type constants.ChatEventType
	// Data 是终态事实的完整 JSON 载荷。
	Data json.RawMessage
	// Status 是执行最终状态。
	Status constants.ExecutionStatus
	// ErrorCode 是失败时保存到查询投影的稳定错误码。
	ErrorCode string
}

// TerminalCommit 保存已经 durable、尚未公开的终态事实。
type TerminalCommit struct {
	// records 是等待在 Execution Registry 释放后发布的已提交记录。
	records []eventlog.Record
	// publishNotifications 在 Registry 释放后公开已提交的通知与结果变化。
	publishNotifications func()
}

// Journal 是 Execution Core 写入和读取 Chat 权威事实的内部接口。
//
// 调用方负责工作校验、Workspace、Context 和 Runtime 生命周期；Journal 负责 JSONL、
// SQLite 窄投影和 committed SSE 的一致顺序。
type Journal interface {
	// ForkChat 创建独立原生会话并发布可恢复的继承历史。
	ForkChat(ctx context.Context, input ForkInput, fork NativeFork) (string, error)
	// LoadExecutionChat 返回追平权威日志后的 Chat 身份和当前执行状态，不加载历史消息。
	LoadExecutionChat(ctx context.Context, chatID string) (models.Chat, error)
	// LoadExecutionContext 返回已有 Chat 的权威执行准备快照。
	LoadExecutionContext(ctx context.Context, chatID string) (ExecutionContext, error)
	// CommitStartedTurn 原子提交 Runtime 已接受输入后的首批事实。
	CommitStartedTurn(ctx context.Context, turn StartedTurn) (AcceptedTurn, error)
	// CommitAcceptedTurn 在可靠入口派发前提交 pending Execution 和输入事实。
	CommitAcceptedTurn(ctx context.Context, turn StartedTurn) (AcceptedTurn, error)
	// MarkExecutionStarted 在预先接纳的 Execution 获得 Runtime Invocation 后提交启动事实。
	MarkExecutionStarted(ctx context.Context, execution models.Execution, invocationID string) (models.Execution, error)
	// ReadExecutionRecords 返回 Chat 创建记录和指定执行的完整权威事实。
	ReadExecutionRecords(chatID string, executionID string) ([]eventlog.Record, error)
	// ReadRecords 返回完整 Chat 的权威记录。
	ReadRecords(chatID string) ([]eventlog.Record, error)
	// AppendExecutionFacts 提交完整 Runtime 消息、Invocation 或 Session 事实。
	AppendExecutionFacts(ctx context.Context, chatID string, facts []eventlog.AppendInput) ([]eventlog.Record, error)
	// AppendApprovalFact durable 提交单个审批事实，但暂不更新查询投影或发布 SSE。
	AppendApprovalFact(chatID string, fact eventlog.AppendInput) (eventlog.Record, error)
	// PublishApprovalFact 更新未决审批投影并发布已 durable 的审批事实。
	PublishApprovalFact(ctx context.Context, record eventlog.Record, pending []models.PendingApproval)
	// AppendElicitationFact durable 提交单个结构化输入事实，但暂不更新查询投影或发布 SSE。
	AppendElicitationFact(chatID string, fact eventlog.AppendInput) (eventlog.Record, error)
	// PublishElicitationFact 更新未决结构化输入投影并发布已 durable 的事实。
	PublishElicitationFact(ctx context.Context, record eventlog.Record, pending []models.PendingElicitation)
	// CommitTerminal 先 durable 提交终态并追平 SQLite，但暂不发布 committed SSE。
	CommitTerminal(ctx context.Context, fact TerminalFact) (TerminalCommit, error)
	// PublishTerminal 在 Execution Registry 已释放后公开 durable 终态。
	PublishTerminal(commit TerminalCommit)
	// PublishLive 发布不进入 JSONL 的实时消息快照。
	PublishLive(chatID string, event eventstream.PublicEvent)
}

// CreateDormantChats 批量建立只有 chat.created 首事件的独立 Chat。
func (m *service) CreateDormantChats(
	ctx context.Context,
	inputs []DormantChat,
) ([]models.Chat, error) {
	if len(inputs) == 0 {
		return nil, status_error.BadRequest
	}
	createdChats := make([]models.Chat, 0, len(inputs))
	createdRecords := make([][]eventlog.Record, 0, len(inputs))
	rollbackLogs := func() {
		for _, chat := range createdChats {
			if err := m.logStore.Delete(chat.ID); err != nil {
				// 删除失败会由下次启动恢复成可查询孤儿 Chat，不能覆盖原始业务错误。
				continue
			}
		}
	}
	for _, input := range inputs {
		chat := input.Chat
		if strings.TrimSpace(chat.ID) == "" ||
			strings.TrimSpace(chat.WakerID) == "" ||
			strings.TrimSpace(chat.WorkspaceDirectory) == "" ||
			len(chat.AdditionalWorkingDirectories) == 0 ||
			chat.ActiveExecutionID != "" {
			rollbackLogs()
			return nil, status_error.BadRequest
		}
		threadID := strings.TrimSpace(input.ThreadWorkspaceID)
		if threadID != "" {
			if err := m.logStore.RegisterTeamChat(threadID, chat.ID); err != nil {
				rollbackLogs()
				return nil, err
			}
		}
		records, err := m.eventCommitter.create(chat.ID, []eventlog.AppendInput{{
			Type: constants.ChatEventTypeCreated,
			Data: marshalData(eventlog.ChatCreatedData{
				WakerID:                      chat.WakerID,
				ProjectID:                    chat.ProjectID,
				ProjectName:                  chat.ProjectName,
				WorkspaceDirectory:           chat.WorkspaceDirectory,
				AdditionalWorkingDirectories: append([]string(nil), chat.AdditionalWorkingDirectories...),
				Title:                        chat.Title,
				ProjectSources:               append([]eventlog.ProjectSourceSnapshot(nil), input.ProjectSources...),
				HiddenFromWakerHistory:       chat.HiddenFromWakerHistory,
				ThreadWorkspaceID:            strings.TrimSpace(input.ThreadWorkspaceID),
			}),
		}})
		if err != nil {
			_ = m.logStore.Delete(chat.ID)
			rollbackLogs()
			return nil, err
		}
		chat.LatestSequence = records[len(records)-1].Sequence
		chat.CreatedAt = records[0].RecordedAt
		chat.UpdatedAt = records[len(records)-1].RecordedAt
		createdChats = append(createdChats, chat)
		createdRecords = append(createdRecords, records)
	}
	if err := m.queryIndexes.createChats(ctx, createdChats); err != nil {
		rollbackLogs()
		return nil, err
	}
	for _, records := range createdRecords {
		m.eventCommitter.publishCommitted(records)
	}
	return createdChats, nil
}

// CommitAcceptedTurn 在 Runtime 派发前原子提交 pending Execution 和输入事实。
func (m *service) CommitAcceptedTurn(
	ctx context.Context,
	turn StartedTurn,
) (AcceptedTurn, error) {
	records, err := m.commitTurnFacts(turn)
	if err != nil {
		return AcceptedTurn{}, err
	}
	last := records[len(records)-1]
	chat := turn.Chat
	chat.LatestSequence = last.Sequence
	chat.ActiveExecutionID = turn.Execution.ID
	chat.LastAccountID = turn.Execution.AccountID
	chat.LastRuntimeID = turn.Execution.RuntimeID
	chat.LastModel = turn.Execution.Model
	chat.LastReasoning = turn.Execution.Reasoning
	chat.LastPermissionLevel = turn.Execution.PermissionLevel
	chat.UpdatedAt = last.RecordedAt
	m.queryIndexes.indexStartedMessage(
		ctx,
		turn.NewChat,
		chat,
		turn.Execution,
		records[0].Sequence-1,
	)
	m.eventCommitter.publishCommitted(records)
	return AcceptedTurn{Chat: chat, Execution: turn.Execution}, nil
}

// LoadExecutionChat 返回追平权威日志后的 Chat 身份，不构建历史消息或私有 Diff。
func (m *service) LoadExecutionChat(ctx context.Context, chatID string) (models.Chat, error) {
	summary, err := m.GetChatSummary(ctx, chatID)
	if err == nil {
		return summary.Chat, nil
	}
	if !hasStatusErrorCode(err, status_error.ChatNotFound.Code()) {
		return models.Chat{}, err
	}
	// 查询索引可以重建；仅明确缺失时回到权威状态，数据库故障不能退回陈旧授权。
	_, state, err := m.loadChatState(chatID)
	if err != nil {
		return models.Chat{}, err
	}
	m.queryIndexes.ensureChatState(ctx, state)
	return state.chat, nil
}

// LoadExecutionContext 返回已有 Chat 的权威执行准备快照。
func (m *service) LoadExecutionContext(
	ctx context.Context,
	chatID string,
) (ExecutionContext, error) {
	detail, err := m.loadChatDetail(ctx, chatID)
	if err != nil {
		return ExecutionContext{}, err
	}
	return ExecutionContext{
		Chat:                detail.Chat,
		SubmittedExecutions: detail.SubmittedExecutions,
		CreateRequestID:     detail.CreateRequestID,
		CreateRequestHash:   detail.CreateRequestHash,
		InitialExecution:    cloneExecution(detail.InitialExecution),
		PreviousMessages:    append([]messageview.Message(nil), detail.Messages...),
		RuntimeSessions:     cloneRuntimeSessions(detail.RuntimeSessions),
		ProjectSources:      append([]eventlog.ProjectSourceSnapshot(nil), detail.ProjectSources...),
		ThreadWorkspaceID:   detail.ThreadWorkspaceID,
	}, nil
}

// CommitStartedTurn 原子提交 Runtime 已接受输入后的首批事实。
func (m *service) CommitStartedTurn(
	ctx context.Context,
	turn StartedTurn,
) (AcceptedTurn, error) {
	records, err := m.commitTurnFacts(turn)
	if err != nil {
		return AcceptedTurn{}, err
	}
	last := records[len(records)-1]
	chat := turn.Chat
	chat.LatestSequence = last.Sequence
	chat.ActiveExecutionID = turn.Execution.ID
	chat.LastAccountID = turn.Execution.AccountID
	chat.LastRuntimeID = turn.Execution.RuntimeID
	chat.LastModel = turn.Execution.Model
	chat.LastReasoning = turn.Execution.Reasoning
	chat.LastPermissionLevel = turn.Execution.PermissionLevel
	chat.UpdatedAt = last.RecordedAt
	m.queryIndexes.indexStartedMessage(
		ctx,
		turn.NewChat,
		chat,
		turn.Execution,
		records[0].Sequence-1,
	)

	execution := turn.Execution
	execution.Status = constants.ExecutionStatusRunning
	execution.Revision++
	execution.UpdatedAt = last.RecordedAt
	m.eventCommitter.publishCommitted(records)
	return AcceptedTurn{Chat: chat, Execution: execution}, nil
}

// MarkExecutionStarted 为已经 pending 的 Execution 提交 Runtime 启动事实。
func (m *service) MarkExecutionStarted(
	_ context.Context,
	execution models.Execution,
	invocationID string,
) (models.Execution, error) {
	records, err := m.eventCommitter.append(execution.ChatID, []eventlog.AppendInput{{
		Type:         constants.ChatEventTypeExecutionStarted,
		ExecutionID:  execution.ID,
		InvocationID: invocationID,
	}})
	if err != nil {
		return models.Execution{}, err
	}
	last := records[len(records)-1]
	execution.Status = constants.ExecutionStatusRunning
	execution.Revision++
	execution.UpdatedAt = last.RecordedAt
	m.queryIndexes.indexExecutionStarted(execution, last)
	m.eventCommitter.publishCommitted(records)
	return execution, nil
}

// commitTurnFacts 按新建或续发语义提交一批已校验事实。
func (m *service) commitTurnFacts(turn StartedTurn) ([]eventlog.Record, error) {
	if len(turn.Facts) == 0 {
		return nil, status_error.BadRequest
	}
	if turn.NewChat {
		return m.eventCommitter.create(turn.Chat.ID, turn.Facts)
	}
	return m.eventCommitter.append(turn.Chat.ID, turn.Facts)
}

// ReadExecutionRecords 通过消息索引读取指定执行，保留实时投影所需的 Chat 路径范围。
func (m *service) ReadExecutionRecords(chatID string, executionID string) ([]eventlog.Record, error) {
	chatID = strings.TrimSpace(chatID)
	executionID = strings.TrimSpace(executionID)
	if chatID == "" || executionID == "" {
		return nil, status_error.BadRequest
	}
	records, err := m.logStore.ReadMessage(chatID, executionID)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if record.Type == constants.ChatEventTypeExecutionCreated && record.ExecutionID == executionID {
			return records, nil
		}
	}
	return nil, status_error.ExecutionNotFound
}

// ReadRecords 返回实时消息投影所需的当前权威记录。
func (m *service) ReadRecords(chatID string) ([]eventlog.Record, error) {
	return m.readChatRecords(chatID)
}

// AppendExecutionFacts 提交非终态执行事实并推进对应的 SQLite 窄投影。
func (m *service) AppendExecutionFacts(
	_ context.Context,
	chatID string,
	facts []eventlog.AppendInput,
) ([]eventlog.Record, error) {
	if len(facts) == 0 {
		return nil, status_error.BadRequest
	}
	var session *eventlog.RuntimeSessionData
	lastFact := facts[len(facts)-1]
	if lastFact.Type == constants.ChatEventTypeNativeSessionUpdated ||
		lastFact.Type == constants.ChatEventTypeNativeSessionCleared {
		decoded := eventlog.RuntimeSessionData{}
		if err := json.Unmarshal(lastFact.Data, &decoded); err != nil {
			return nil, err
		}
		session = &decoded
	}
	records, err := m.eventCommitter.append(chatID, facts)
	if err != nil {
		return nil, err
	}
	last := records[len(records)-1]
	afterSequence := records[0].Sequence - 1
	switch last.Type {
	case constants.ChatEventTypeNativeSessionUpdated:
		m.queryIndexes.indexRuntimeSessionSaved(
			chatID,
			session.RuntimeID,
			session.SessionID,
			session.LastExecutionID,
			afterSequence,
			last,
		)
	case constants.ChatEventTypeNativeSessionCleared:
		m.queryIndexes.indexRuntimeSessionCleared(
			chatID,
			session.RuntimeID,
			afterSequence,
			last,
		)
	default:
		m.queryIndexes.indexChatCursor(chatID, afterSequence, last)
	}
	m.eventCommitter.publishCommitted(records)
	return records, nil
}

// AppendApprovalFact durable 提交单个审批事实，但暂不公开不完整的内存投影。
func (m *service) AppendApprovalFact(
	chatID string,
	fact eventlog.AppendInput,
) (eventlog.Record, error) {
	records, err := m.eventCommitter.appendDurable(chatID, []eventlog.AppendInput{fact})
	if err != nil {
		return eventlog.Record{}, err
	}
	return records[0], nil
}

// PublishApprovalFact 在审批协调器完成内存状态转换后推进查询投影并发布 SSE。
func (m *service) PublishApprovalFact(
	ctx context.Context,
	record eventlog.Record,
	pending []models.PendingApproval,
) {
	m.queryIndexes.updateChatIndex(ctx, record.ChatID, record.Sequence-1, map[string]any{
		"waiting_approval":  len(pending) > 0,
		"pending_approvals": string(marshalData(pending)),
		"latest_sequence":   record.Sequence,
		"updated_at":        record.RecordedAt,
	})
	m.eventCommitter.publishCommitted([]eventlog.Record{record})
}

// AppendElicitationFact durable 提交单个结构化输入事实，但暂不公开不完整的内存投影。
func (m *service) AppendElicitationFact(
	chatID string,
	fact eventlog.AppendInput,
) (eventlog.Record, error) {
	records, err := m.eventCommitter.appendDurable(chatID, []eventlog.AppendInput{fact})
	if err != nil {
		return eventlog.Record{}, err
	}
	return records[0], nil
}

// PublishElicitationFact 在结构化输入协调器完成内存状态转换后推进查询投影并发布 SSE。
func (m *service) PublishElicitationFact(
	ctx context.Context,
	record eventlog.Record,
	pending []models.PendingElicitation,
) {
	m.queryIndexes.updateChatIndex(ctx, record.ChatID, record.Sequence-1, map[string]any{
		"waiting_elicitation":  len(pending) > 0,
		"pending_elicitations": string(marshalData(pending)),
		"latest_sequence":      record.Sequence,
		"updated_at":           record.RecordedAt,
	})
	m.eventCommitter.publishCommitted([]eventlog.Record{record})
}

// CommitTerminal 先 durable 提交终态并追平 SQLite，但暂不发布 committed SSE。
func (m *service) CommitTerminal(
	_ context.Context,
	fact TerminalFact,
) (TerminalCommit, error) {
	records, err := m.eventCommitter.appendDurable(fact.Execution.ChatID, []eventlog.AppendInput{{
		Type:        fact.Type,
		ExecutionID: fact.Execution.ID,
		Data:        append(json.RawMessage(nil), fact.Data...),
	}})
	if err != nil {
		return TerminalCommit{}, err
	}
	notificationContext, publishNotifications := events.HoldCommitEvents(context.Background())
	// 命令和 Runtime 消息可以并发追加，逐事件 CAS 可能落后；从耐久尾部补齐全部字段。
	if err := m.catchUpLiveProjection(notificationContext, fact.Execution.ChatID); err != nil {
		logx.Errorf("index ended execution: chat_id=%s error=%v", fact.Execution.ChatID, err)
	}
	return TerminalCommit{records: records, publishNotifications: publishNotifications}, nil
}

// PublishTerminal 在 Execution Registry 已释放后公开 durable 终态。
func (m *service) PublishTerminal(commit TerminalCommit) {
	m.eventCommitter.publishCommitted(commit.records)
	if commit.publishNotifications != nil {
		commit.publishNotifications()
	}
}

// PublishLive 发布不进入 JSONL 的实时消息快照。
func (m *service) PublishLive(chatID string, event eventstream.PublicEvent) {
	m.subscriptions.publishLive(chatID, event)
}

func cloneRuntimeSessions(
	sessions map[string]models.RuntimeSession,
) map[string]models.RuntimeSession {
	cloned := make(map[string]models.RuntimeSession, len(sessions))
	for runtimeID, session := range sessions {
		cloned[runtimeID] = session
	}
	return cloned
}

// cloneExecution 复制可选的首次执行，避免调用方修改 Chat Service 的重放状态。
func cloneExecution(execution *models.Execution) *models.Execution {
	if execution == nil {
		return nil
	}
	clone := *execution
	if execution.CompletedAt != nil {
		completedAt := *execution.CompletedAt
		clone.CompletedAt = &completedAt
	}
	return &clone
}
