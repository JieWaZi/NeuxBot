package messageview

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
)

// MessageViewBuilder 将权威日志记录逐条折叠为跨 Runtime 的消息展示快照。
type MessageViewBuilder struct {
	// publicOnly 只维护安全展示状态，不保留供分叉和文件详情使用的 Runtime 原始事件或 Diff。
	publicOnly bool
	// agentCommands 保存当前执行中由 Core 确认的命令事实，不能从工具输出建立。
	agentCommands map[string]map[string]eventlog.AgentCommandData
	// agentMatches 防止同一命令回执在多个原生工具行上重复变成业务活动。
	agentMatches map[string]map[string]string
	// messages 保存按创建顺序排列的当前消息快照。
	messages []Message
	// workspaceDirectory 保存 Chat 锁定的对话执行工作区根目录。
	workspaceDirectory string
	// projectSources 保存真实项目目录到工作区 projects 入口的冻结映射。
	projectSources []eventlog.ProjectSourceSnapshot
	// messageIndexes 保存消息标识到 messages 下标的映射。
	messageIndexes map[string]int
	// executionMessages 保存执行标识到 Assistant 消息标识的映射。
	executionMessages map[string]string
	// toolIndexes 按消息与调用复合身份定位工具展示部件。
	toolIndexes map[string]map[string]int
	// toolStates 只保留当前工具的安全展示事实，不保留输入正文、输出或历史参数。
	toolStates map[string]map[string]*toolProjectionState
	// latestToolKeys 为缺少 Invocation 的旧审批事实定位最近的原生工具身份。
	latestToolKeys map[string]map[string]string
	// approvalResults 保存可能早于工具展示事件到达的审批结果。
	approvalResults map[string]map[string]ToolApprovalResult
	// textBuilders 保存正文和展示段的唯一可编辑文本缓冲；消息字段只引用不可变前缀视图。
	textBuilders map[string]map[string]*strings.Builder
	// pendingTextIndexes 保存尚未确定为过程说明或最终正文的文本部件下标。
	pendingTextIndexes map[string][]int
	// provisionalTextIndexes 保存工具仍在运行时暂归入过程区的尾部文本下标。
	provisionalTextIndexes map[string][]int
	// failureMessages 保存调用事实中可用于补全历史终态的错误说明。
	failureMessages map[string]string
}

// ReplayMessages 重放权威日志，构建截至最新序号的消息展示视图。
func ReplayMessages(records []eventlog.Record) ([]Message, error) {
	return replayMessages(records, false)
}

// ReplayPublicMessages 构建公开历史快照，跳过仅供分叉和文件详情使用的完整 Diff。
func ReplayPublicMessages(records []eventlog.Record) ([]Message, error) {
	return replayMessages(records, true)
}

func replayMessages(records []eventlog.Record, public bool) ([]Message, error) {
	builder := NewMessageViewBuilder()
	if public {
		builder = NewPublicMessageViewBuilder()
	}
	for _, record := range records {
		if _, err := builder.Apply(record); err != nil {
			return nil, err
		}
	}
	return builder.snapshotMessages(public), nil
}

// NewMessageViewBuilder 建立一个可复用的增量消息视图构建器。
func NewMessageViewBuilder() *MessageViewBuilder {
	return &MessageViewBuilder{
		agentCommands:          make(map[string]map[string]eventlog.AgentCommandData),
		agentMatches:           make(map[string]map[string]string),
		messages:               make([]Message, 0),
		messageIndexes:         make(map[string]int),
		executionMessages:      make(map[string]string),
		toolIndexes:            make(map[string]map[string]int),
		toolStates:             make(map[string]map[string]*toolProjectionState),
		latestToolKeys:         make(map[string]map[string]string),
		approvalResults:        make(map[string]map[string]ToolApprovalResult),
		textBuilders:           make(map[string]map[string]*strings.Builder),
		pendingTextIndexes:     make(map[string][]int),
		provisionalTextIndexes: make(map[string][]int),
		failureMessages:        make(map[string]string),
	}
}

// NewPublicMessageViewBuilder 建立只维护安全公开投影的增量构建器，避免流式工具参数长期驻留。
func NewPublicMessageViewBuilder() *MessageViewBuilder {
	builder := NewMessageViewBuilder()
	builder.publicOnly = true
	return builder
}

// Apply 应用一条权威记录，并返回发生公开快照变化的消息标识。
func (p *MessageViewBuilder) Apply(record eventlog.Record) (string, error) {
	switch record.Type {
	case constants.ChatEventTypeAgentCommandCommitted:
		return "", p.applyAgentCommand(record)
	case constants.ChatEventTypeMessageInherited:
		var inherited InheritedMessage
		if json.Unmarshal(record.Data, &inherited) != nil {
			return "", status_error.ChatLogCorrupted
		}
		message := inherited.Message
		if !p.publicOnly {
			message.FileChangeDiffs = inherited.Diffs
		}
		message.ID = record.MessageID
		message.CreatedSequence = record.Sequence
		message.Revision = record.Sequence
		p.messageIndexes[message.ID] = len(p.messages)
		p.messages = append(p.messages, message)
		return message.ID, nil
	case constants.ChatEventTypeCreated:
		return "", p.applyChatCreated(record)
	case constants.ChatEventTypeUserMessageCreated:
		return p.applyMessageCreated(record)
	case constants.ChatEventTypeExecutionCreated:
		return p.applyExecutionCreated(record)
	case constants.ChatEventTypeExecutionStarted:
		return p.applyExecutionStarted(record), nil
	case constants.ChatEventTypeRuntimeEvent:
		return p.applyRuntimeEvent(record)
	case constants.ChatEventTypeApprovalResolved:
		return p.applyApprovalResolved(record)
	case constants.ChatEventTypeAssistantMessage,
		constants.ChatEventTypeToolMessage:
		return p.applyRuntimeMessage(record)
	case constants.ChatEventTypeInvocationFinished:
		return "", p.applyInvocationFinished(record)
	case constants.ChatEventTypeExecutionCompleted,
		constants.ChatEventTypeExecutionFailed,
		constants.ChatEventTypeExecutionCanceled:
		return p.applyExecutionFinished(record)
	default:
		return "", nil
	}
}

// applyInvocationFinished 保留历史调用事实中的失败原因，供旧终态记录补全展示。
func (p *MessageViewBuilder) applyInvocationFinished(record eventlog.Record) error {
	var finished eventlog.InvocationFinishedData
	if err := json.Unmarshal(record.Data, &finished); err != nil {
		return status_error.ChatLogError
	}
	if message := runtimecontract.RecordedFailureDisplayMessage(finished.FailureMessage); message != "" {
		p.failureMessages[record.ExecutionID] = message
	}
	return nil
}

// applyApprovalResolved 将安全审批结果附着到对应工具展示部件。
func (p *MessageViewBuilder) applyApprovalResolved(record eventlog.Record) (string, error) {
	var resolved eventlog.ApprovalResolvedData
	if err := json.Unmarshal(record.Data, &resolved); err != nil {
		return "", status_error.ChatLogCorrupted
	}
	if resolved.ToolCallID == "" || resolved.Result == "" {
		return "", status_error.ChatLogCorrupted
	}
	result := ToolApprovalResult{
		Result:     resolved.Result,
		OptionKind: resolved.OptionKind,
		OptionName: resolved.OptionName,
		Command:    resolved.Command,
	}
	results := p.approvalResults[record.ExecutionID]
	if results == nil {
		results = make(map[string]ToolApprovalResult)
		p.approvalResults[record.ExecutionID] = results
	}
	messageID := p.executionMessages[record.ExecutionID]
	key := toolProjectionKey(record.InvocationID, resolved.ToolCallID)
	if record.InvocationID == "" {
		key = p.latestToolKeys[messageID][resolved.ToolCallID]
		if key == "" {
			key = resolved.ToolCallID
		}
	}
	results[key] = result
	messageIndex, messageExists := p.messageIndexes[messageID]
	toolIndex, toolExists := p.toolIndexes[messageID][key]
	if !messageExists || !toolExists {
		return "", nil
	}
	message := &p.messages[messageIndex]
	message.DisplayParts[toolIndex].ToolApprovalResult = cloneToolApprovalResult(result)
	message.Revision = max(message.Revision+1, record.Sequence)
	message.UpdatedAt = record.RecordedAt
	return message.ID, nil
}

// Messages 返回与构建器内部状态隔离的消息快照；公开构建器不提供私有 Runtime 事件与 Diff。
func (p *MessageViewBuilder) Messages() []Message {
	return p.snapshotMessages(false)
}

func (p *MessageViewBuilder) snapshotMessages(public bool) []Message {
	messages := make([]Message, len(p.messages))
	for index, message := range p.messages {
		messages[index] = p.snapshotMessage(message, public)
	}
	return messages
}

// snapshotMessage 在读取边界派生文件视图，避免每条事件都重扫此前的工具历史。
func (p *MessageViewBuilder) snapshotMessage(message Message, public bool) Message {
	if len(message.RuntimeEvents) > 0 || p.publicOnly && len(p.toolIndexes[message.ID]) > 0 {
		message.FileChanges = messageFileChanges(message.DisplayParts)
		if !public && !p.publicOnly {
			message.FileChangeDiffs = messageFileChangeDiffs(
				message.RuntimeEvents, message.DisplayParts, message.FileChanges, p.workspacePathScope(),
			)
		}
	}
	// 继承消息没有 Runtime 事件或增量工具索引，直接保留权威继承快照中的文件摘要。
	return cloneMessage(message, public)
}

// PublicMessage 返回不携带 Runtime 原始事件的指定消息快照。
func (p *MessageViewBuilder) PublicMessage(messageID string) *Message {
	index, exists := p.messageIndexes[messageID]
	if !exists {
		return nil
	}
	message := p.snapshotMessage(p.messages[index], true)
	return &message
}

// MessageRevision 读取当前消息修订号，供事件游标恢复去重使用，不生成完整快照。
func (p *MessageViewBuilder) MessageRevision(messageID string) int64 {
	index, exists := p.messageIndexes[messageID]
	if !exists {
		return 0
	}
	return p.messages[index].Revision
}

func (p *MessageViewBuilder) applyChatCreated(record eventlog.Record) error {
	var data eventlog.ChatCreatedData
	if err := json.Unmarshal(record.Data, &data); err != nil {
		return status_error.ChatLogError
	}
	p.workspaceDirectory = data.WorkspaceDirectory
	p.projectSources = append([]eventlog.ProjectSourceSnapshot(nil), data.ProjectSources...)
	return nil
}

// workspacePathScope 返回当前 Chat 的消息文件逻辑路径映射。
func (p *MessageViewBuilder) workspacePathScope() workspacePathScope {
	return workspacePathScope{
		workspaceDirectory: p.workspaceDirectory,
		projectSources:     p.projectSources,
	}
}

func (p *MessageViewBuilder) applyMessageCreated(record eventlog.Record) (string, error) {
	var data eventlog.MessageCreatedData
	if err := json.Unmarshal(record.Data, &data); err != nil {
		return "", status_error.ChatLogError
	}
	message := Message{
		ID:              record.MessageID,
		Role:            data.Role,
		Actor:           data.Actor,
		SourceWakerID:   data.SourceWakerID,
		SourceChatID:    data.SourceChatID,
		Truncated:       data.Truncated,
		Content:         data.Content,
		Attachments:     append([]eventlog.AttachmentReference(nil), data.Attachments...),
		Plugins:         append([]plugins.Snapshot(nil), data.Plugins...),
		Skills:          append([]eventlog.MessageSkillReference(nil), data.Skills...),
		Connectors:      append([]eventlog.MessageConnectorReference(nil), data.Connectors...),
		CodeReferences:  append([]eventlog.CodeReference(nil), data.CodeReferences...),
		Revision:        record.Sequence,
		CreatedSequence: record.Sequence,
		CreatedAt:       record.RecordedAt,
		UpdatedAt:       record.RecordedAt,
	}
	if data.Content != "" {
		message.DisplayParts = []MessageDisplayPart{{
			ID:   displayPartID(record.MessageID, 1),
			Type: constants.MessageDisplayPartText,
			Text: data.Content,
		}}
		p.pendingTextIndexes[record.MessageID] = []int{0}
	}
	p.messageIndexes[message.ID] = len(p.messages)
	p.messages = append(p.messages, message)
	return message.ID, nil
}

func (p *MessageViewBuilder) applyExecutionCreated(record eventlog.Record) (string, error) {
	var data eventlog.ExecutionCreatedData
	if err := json.Unmarshal(record.Data, &data); err != nil {
		return "", status_error.ChatLogError
	}
	messageID := record.ExecutionID
	if messageID != "" {
		p.executionMessages[record.ExecutionID] = messageID
		if index, exists := p.messageIndexes[messageID]; exists {
			message := &p.messages[index]
			message.ExecutionID = record.ExecutionID
			message.Status = constants.ExecutionStatusPending
			message.Revision = max(message.Revision+1, record.Sequence)
			message.UpdatedAt = record.RecordedAt
			return message.ID, nil
		}
		message := Message{
			ID:              messageID,
			Role:            constants.MessageRoleAssistant,
			Revision:        record.Sequence,
			CreatedSequence: record.Sequence,
			ExecutionID:     record.ExecutionID,
			Status:          constants.ExecutionStatusPending,
			CreatedAt:       record.RecordedAt,
			UpdatedAt:       record.RecordedAt,
		}
		p.messageIndexes[message.ID] = len(p.messages)
		p.messages = append(p.messages, message)
		return message.ID, nil
	}
	return "", nil
}

func (p *MessageViewBuilder) applyExecutionStarted(record eventlog.Record) string {
	messageID := p.executionMessages[record.ExecutionID]
	if index, exists := p.messageIndexes[messageID]; exists {
		message := &p.messages[index]
		message.RunMetrics = &MessageRunMetrics{StartedAt: record.RecordedAt}
	}
	if p.updateStatus(messageID, constants.ExecutionStatusRunning, record, "", "", 0) {
		return messageID
	}
	return ""
}

func (p *MessageViewBuilder) applyRuntimeMessage(record eventlog.Record) (string, error) {
	index, exists := p.messageIndexes[record.MessageID]
	if !exists {
		return "", nil
	}
	var runtimeMessage eventlog.RuntimeMessageData
	if err := json.Unmarshal(record.Data, &runtimeMessage); err != nil {
		return "", status_error.ChatLogError
	}
	if runtimeMessage.MessageRevision <= 0 {
		return "", status_error.ChatLogError
	}
	message := &p.messages[index]
	for _, content := range runtimeMessage.Content {
		if err := p.applyRuntimeContent(record, message, content.Type, content.Data); err != nil {
			return "", err
		}
	}
	message.Revision = runtimeMessage.MessageRevision
	message.UpdatedAt = record.RecordedAt
	return message.ID, nil
}

// applyRuntimeEvent 只供未持久化的 transient Runtime 事件构建实时消息快照。
func (p *MessageViewBuilder) applyRuntimeEvent(record eventlog.Record) (string, error) {
	index, exists := p.messageIndexes[record.MessageID]
	if !exists {
		return "", nil
	}
	var runtimeEvent eventlog.RuntimeEventData
	if err := json.Unmarshal(record.Data, &runtimeEvent); err != nil {
		return "", status_error.ChatLogError
	}
	if runtimeEvent.MessageRevision <= 0 {
		return "", status_error.ChatLogError
	}
	message := &p.messages[index]
	if err := p.applyRuntimeContent(record, message, runtimeEvent.Type, runtimeEvent.Data); err != nil {
		return "", err
	}
	message.Revision = runtimeEvent.MessageRevision
	message.UpdatedAt = record.RecordedAt
	return message.ID, nil
}

func (p *MessageViewBuilder) applyRuntimeContent(
	record eventlog.Record,
	message *Message,
	contentType constants.RuntimeEventType,
	data json.RawMessage,
) error {
	event := MessageRuntimeEvent{
		Sequence:     record.Sequence,
		RecordedAt:   record.RecordedAt,
		InvocationID: record.InvocationID,
		Type:         contentType,
		Data:         data,
	}
	if !p.publicOnly {
		event.Data = append(json.RawMessage(nil), data...)
		message.RuntimeEvents = append(message.RuntimeEvents, event)
	}
	if err := p.applyDisplayEvent(message, event); err != nil {
		return err
	}
	return nil
}

func (p *MessageViewBuilder) applyExecutionFinished(record eventlog.Record) (string, error) {
	var finished eventlog.ExecutionFinishedData
	if err := json.Unmarshal(record.Data, &finished); err != nil {
		return "", status_error.ChatLogError
	}
	messageID := p.executionMessages[record.ExecutionID]
	if index, exists := p.messageIndexes[messageID]; exists {
		message := &p.messages[index]
		message.Retryable = finished.Retryable
		if len(finished.Usage) > 0 {
			if err := p.applyUsage(message, finished.Usage); err != nil {
				return "", status_error.ChatLogError
			}
		}
		p.releaseProvisionalText(message)
		if message.RunMetrics == nil {
			message.RunMetrics = &MessageRunMetrics{}
		}
		if message.RunMetrics.StartedAt > 0 && record.RecordedAt >= message.RunMetrics.StartedAt {
			message.RunMetrics.DurationMs = record.RecordedAt - message.RunMetrics.StartedAt
		}
	}
	errorMessage := runtimecontract.RecordedFailureDisplayMessage(finished.ErrorMessage)
	if errorMessage == "" {
		errorMessage = p.failureMessages[record.ExecutionID]
	}
	delete(p.failureMessages, record.ExecutionID)
	if p.updateStatus(
		messageID,
		finished.Status,
		record,
		finished.ErrorCode,
		errorMessage,
		finished.MessageRevision+1,
	) {
		return messageID, nil
	}
	return "", nil
}

// updateStatus 将执行终态同步到对应 Assistant 消息，并保证修订号单调递增。
func (p *MessageViewBuilder) updateStatus(
	messageID string,
	status constants.ExecutionStatus,
	record eventlog.Record,
	errorCode string,
	errorMessage string,
	minimumRevision int64,
) bool {
	index, exists := p.messageIndexes[messageID]
	if !exists {
		return false
	}
	message := &p.messages[index]
	message.Status = status
	message.ErrorCode = errorCode
	message.ErrorMessage = errorMessage
	message.Revision = max(message.Revision+1, record.Sequence, minimumRevision)
	message.UpdatedAt = record.RecordedAt
	return true
}

// applyDisplayEvent 将跨 Runtime 的规范事件分派到对应展示状态转换。
func (p *MessageViewBuilder) applyDisplayEvent(message *Message, event MessageRuntimeEvent) error {
	if event.Type == "usage" {
		if err := p.applyUsage(message, event.Data); err != nil {
			return status_error.ChatLogError
		}
		return nil
	}
	var data map[string]interface{}
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return status_error.ChatLogError
	}
	switch event.Type {
	case "text_delta":
		p.applyTextDelta(message, data)
	case "commentary_delta":
		p.applyCommentaryDelta(message, data)
	case "thinking_delta":
		p.applyThinkingDelta(message, data)
	case constants.RuntimeEventTypeToolUse, constants.RuntimeEventTypeToolInputSnapshot, constants.RuntimeEventTypeToolUpdate:
		p.applyToolUse(message, event, data)
	case "tool_result":
		p.applyToolResult(message, event, data)
	}
	return nil
}

// applyTextDelta 暂存工具执行期间的尾部文本，直到后续事件能确定其展示语义。
func (p *MessageViewBuilder) applyTextDelta(message *Message, data map[string]interface{}) {
	text, _ := data["text"].(string)
	message.Content = p.appendTextValue(message.ID, "", message.Content, text)
	if p.hasRunningTool(message) {
		p.appendProvisionalText(message, text)
		return
	}
	p.appendText(message, constants.MessageDisplayPartText, text, true)
}

// applyCommentaryDelta 将 Runtime 明确标记的过程说明直接写入过程展示区。
func (p *MessageViewBuilder) applyCommentaryDelta(message *Message, data map[string]interface{}) {
	text, _ := data["text"].(string)
	message.Content = p.appendTextValue(message.ID, "", message.Content, text)
	p.appendText(message, constants.MessageDisplayPartProcessCommentary, text, false)
}

// applyThinkingDelta 在推理段开始前把尚未定性的文本归入过程说明。
func (p *MessageViewBuilder) applyThinkingDelta(message *Message, data map[string]interface{}) {
	p.classifyPendingText(message)
	p.appendText(
		message,
		constants.MessageDisplayPartReasoning,
		firstDisplayString(data, "text", "thinking"),
		false,
	)
}

// applyToolUse 按当前工具事实重新派生展示，不把部分参数的早期推断固化为最终动作。
func (p *MessageViewBuilder) applyToolUse(message *Message, event MessageRuntimeEvent, data map[string]interface{}) {
	p.classifyPendingText(message)
	part, state := p.toolPart(message, event, data)
	state.apply(data, p.workspacePathScope())
	p.projectToolPart(message.ExecutionID, part, state)
	p.applyPendingApprovalResult(message, part, part.ID)
}

// toolPart 使用 Invocation 与原生工具身份定位部件，公开 ToolCallID 仍供审批和原生定位使用。
func (p *MessageViewBuilder) toolPart(message *Message, event MessageRuntimeEvent, data map[string]interface{}) (*MessageDisplayPart, *toolProjectionState) {
	nativeID := displayToolID(data, event)
	key := toolProjectionKey(event.InvocationID, nativeID)
	indexes := p.toolDisplayIndexes(message.ID)
	if p.toolStates[message.ID] == nil {
		p.toolStates[message.ID] = make(map[string]*toolProjectionState)
	}
	if p.latestToolKeys[message.ID] == nil {
		p.latestToolKeys[message.ID] = make(map[string]string)
	}
	p.latestToolKeys[message.ID][nativeID] = key
	state := p.toolStates[message.ID][key]
	if state == nil {
		state = &toolProjectionState{fields: make(map[string]interface{})}
		p.toolStates[message.ID][key] = state
	}
	if index, exists := indexes[key]; exists {
		return &message.DisplayParts[index], state
	}
	p.classifyPendingText(message)
	indexes[key] = len(message.DisplayParts)
	message.DisplayParts = append(message.DisplayParts, MessageDisplayPart{ID: key, Type: constants.MessageDisplayPartTool, ToolCallID: nativeID, ToolStatus: constants.MessageToolStatusRunning})
	return &message.DisplayParts[len(message.DisplayParts)-1], state
}

// projectToolPart 仅使用当前的有界展示事实，省略字段由状态保留，显式空值由新事实清除。
func (p *MessageViewBuilder) projectToolPart(executionID string, part *MessageDisplayPart, state *toolProjectionState) {
	data := state.projectionData()
	part.ToolCategory = displayToolCategoryForData(data)
	part.ToolAction = displayToolAction(data)
	part.ToolLabel, part.ToolLoadingLabel = buildToolPhrase(data, p.workspacePathScope())
	part.ToolSafeSummary = mergeToolSafeSummary(buildToolSafeSummary(data, p.workspacePathScope()), state.contentSummary)
	p.applyCommandCaption(executionID, state.commandID, part)
}

// applyPendingApprovalResult 把先于工具展示事件到达的审批结果补到对应调用。
func (p *MessageViewBuilder) applyPendingApprovalResult(message *Message, part *MessageDisplayPart, key string) {
	results := p.approvalResults[message.ExecutionID]
	result, exists := results[key]
	if !exists {
		result, exists = results[part.ToolCallID]
		if exists {
			delete(results, part.ToolCallID)
			results[key] = result
		}
	}
	if exists {
		part.ToolApprovalResult = cloneToolApprovalResult(result)
	}
}

// cloneToolApprovalResult 返回与构建器内部状态隔离的审批结果。
func cloneToolApprovalResult(result ToolApprovalResult) *ToolApprovalResult {
	clone := result
	return &clone
}

// applyToolResult 在同一调用的当前事实基础上提交终态；结果不携带的输入和 Diff 继续保留。
func (p *MessageViewBuilder) applyToolResult(message *Message, event MessageRuntimeEvent, data map[string]interface{}) {
	part, state := p.toolPart(message, event, data)
	state.apply(data, p.workspacePathScope())
	p.projectToolPart(message.ExecutionID, part, state)
	part.ToolStatus = displayToolResultStatus(data)
	p.applyPendingApprovalResult(message, part, part.ID)
	p.matchAgentCommand(message.ExecutionID, part, data)
	if !p.hasRunningTool(message) {
		p.releaseProvisionalText(message)
	}
}

// toolDisplayIndexes 返回消息私有的工具调用索引，并在首次使用时初始化。
func (p *MessageViewBuilder) toolDisplayIndexes(messageID string) map[string]int {
	indexes := p.toolIndexes[messageID]
	if indexes == nil {
		indexes = make(map[string]int)
		p.toolIndexes[messageID] = indexes
	}
	return indexes
}

// applyUsage 将 Runtime SDK 的统一用量数据投影到公开运行指标。
func (p *MessageViewBuilder) applyUsage(message *Message, data json.RawMessage) error {
	var usage runtimecontract.UsageData
	if err := json.Unmarshal(data, &usage); err != nil {
		return status_error.ChatLogError
	}
	if message.RunMetrics == nil {
		message.RunMetrics = &MessageRunMetrics{}
	}
	metrics := message.RunMetrics
	if usage.Turn != nil {
		metrics.InputTokens = usage.Turn.InputTokens
		metrics.OutputTokens = usage.Turn.OutputTokens
		metrics.ReasoningOutputTokens = usage.Turn.ReasoningOutputTokens
		metrics.CacheReadTokens = usage.Turn.CacheReadTokens
		metrics.CacheCreationTokens = usage.Turn.CacheCreationTokens
	}
	if usage.Context != nil {
		metrics.ContextUsedTokens = usage.Context.UsedTokens
		metrics.ContextWindowTokens = usage.Context.WindowTokens
	}
	return nil
}

// appendText 合并相邻同类文本，并可标记为等待后续事件定性的正文候选。
func (p *MessageViewBuilder) appendText(
	message *Message,
	partType constants.MessageDisplayPartType,
	value string,
	pending bool,
) {
	if value == "" {
		return
	}
	lastIndex := len(message.DisplayParts) - 1
	if lastIndex >= 0 && message.DisplayParts[lastIndex].Type == partType {
		part := &message.DisplayParts[lastIndex]
		part.Text = p.appendTextValue(message.ID, part.ID, part.Text, value)
		return
	}
	message.DisplayParts = append(message.DisplayParts, MessageDisplayPart{
		ID:   displayPartID(message.ID, int64(len(message.DisplayParts)+1)),
		Type: partType,
		Text: value,
	})
	if pending {
		p.pendingTextIndexes[message.ID] = append(p.pendingTextIndexes[message.ID], len(message.DisplayParts)-1)
	}
}

// appendTextValue 只追加新的字节；空 partID 表示正文，其他键对应稳定展示段身份。
// Builder.String 返回不可变前缀视图，后续追加不会改写旧公开快照已经引用的字节。
func (p *MessageViewBuilder) appendTextValue(messageID, partID, current, value string) string {
	if value == "" {
		return current
	}
	builders := p.textBuilders[messageID]
	if builders == nil {
		builders = make(map[string]*strings.Builder)
		p.textBuilders[messageID] = builders
	}
	builder := builders[partID]
	if builder == nil {
		builder = &strings.Builder{}
		builder.WriteString(current)
		builders[partID] = builder
	}
	builder.WriteString(value)
	return builder.String()
}

// classifyPendingText 在出现推理或工具边界时，将此前正文候选确定为过程说明。
func (p *MessageViewBuilder) classifyPendingText(message *Message) {
	for _, index := range p.pendingTextIndexes[message.ID] {
		if index >= 0 && index < len(message.DisplayParts) && message.DisplayParts[index].Type == constants.MessageDisplayPartText {
			message.DisplayParts[index].Type = constants.MessageDisplayPartProcessCommentary
		}
	}
	p.pendingTextIndexes[message.ID] = nil
	for _, index := range p.provisionalTextIndexes[message.ID] {
		if index >= 0 && index < len(message.DisplayParts) {
			message.DisplayParts[index].Type = constants.MessageDisplayPartProcessCommentary
		}
	}
	p.provisionalTextIndexes[message.ID] = nil
}

// appendProvisionalText 暂存工具运行期间语义未定的尾部文本。
func (p *MessageViewBuilder) appendProvisionalText(message *Message, value string) {
	if value == "" {
		return
	}
	indexes := p.provisionalTextIndexes[message.ID]
	lastIndex := len(message.DisplayParts) - 1
	if len(indexes) > 0 && indexes[len(indexes)-1] == lastIndex {
		part := &message.DisplayParts[lastIndex]
		part.Text = p.appendTextValue(message.ID, part.ID, part.Text, value)
		return
	}
	message.DisplayParts = append(message.DisplayParts, MessageDisplayPart{
		ID:   displayPartID(message.ID, int64(len(message.DisplayParts)+1)),
		Type: constants.MessageDisplayPartProcessCommentary,
		Text: value,
	})
	p.provisionalTextIndexes[message.ID] = append(indexes, len(message.DisplayParts)-1)
}

// releaseProvisionalText 在工具全部结束后把尾部文本恢复为正文候选。
func (p *MessageViewBuilder) releaseProvisionalText(message *Message) {
	for _, index := range p.provisionalTextIndexes[message.ID] {
		if index < 0 || index >= len(message.DisplayParts) {
			continue
		}
		message.DisplayParts[index].Type = constants.MessageDisplayPartText
		p.pendingTextIndexes[message.ID] = append(p.pendingTextIndexes[message.ID], index)
	}
	p.provisionalTextIndexes[message.ID] = nil
}

// hasRunningTool 判断消息是否仍有未收到结果的工具调用。
func (p *MessageViewBuilder) hasRunningTool(message *Message) bool {
	for _, part := range message.DisplayParts {
		if part.Type == constants.MessageDisplayPartTool && part.ToolStatus == constants.MessageToolStatusRunning {
			return true
		}
	}
	return false
}

// displayPartID 为同一消息内展示部件生成稳定标识。
func displayPartID(messageID string, sequence int64) string {
	return fmt.Sprintf("%s:part:%d", messageID, sequence)
}

// cloneMessage 深拷贝可变字段，并按公开视图要求移除 Runtime 原始事件。
func cloneMessage(message Message, public bool) Message {
	clone := message
	clone.Attachments = append([]eventlog.AttachmentReference(nil), message.Attachments...)
	clone.Skills = append([]eventlog.MessageSkillReference(nil), message.Skills...)
	clone.Connectors = append([]eventlog.MessageConnectorReference(nil), message.Connectors...)
	clone.CodeReferences = append([]eventlog.CodeReference(nil), message.CodeReferences...)
	clone.DisplayParts = append([]MessageDisplayPart(nil), message.DisplayParts...)
	clone.FileChanges = append([]ToolFileChangeSummary(nil), message.FileChanges...)
	for index, part := range clone.DisplayParts {
		if part.ToolSafeSummary != nil {
			summary := *part.ToolSafeSummary
			summary.Files = append([]ToolFileChangeSummary(nil), part.ToolSafeSummary.Files...)
			clone.DisplayParts[index].ToolSafeSummary = &summary
		}
		if part.ToolApprovalResult != nil {
			result := *part.ToolApprovalResult
			clone.DisplayParts[index].ToolApprovalResult = &result
		}
	}
	if message.RunMetrics != nil {
		metrics := *message.RunMetrics
		clone.RunMetrics = &metrics
	}
	if public {
		clone.RuntimeEvents = nil
		clone.FileChangeDiffs = nil
	} else {
		clone.RuntimeEvents = append([]MessageRuntimeEvent(nil), message.RuntimeEvents...)
		clone.FileChangeDiffs = append([]MessageFileChangeDiff(nil), message.FileChangeDiffs...)
	}
	return clone
}
