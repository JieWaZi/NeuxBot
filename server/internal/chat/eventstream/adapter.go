// Package eventstream 将权威对话事件适配为前端可安全消费的统一实时协议。
package eventstream

import (
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"

	"github.com/JieWaZi/NeuxBot/server/constants"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
)

// PublicEvent 是聊天事件历史和 SSE 共用的安全事件信封。
type PublicEvent struct {
	// SchemaVersion 是解释当前事件信封所需的格式版本。
	SchemaVersion int `json:"schemaVersion"`
	// Sequence 是 Chat 内从 1 开始严格递增的游标。
	Sequence int64 `json:"sequence,omitempty"`
	// Transient 表示事件来自尚未形成持久化 checkpoint 的实时消息视图。
	Transient bool `json:"transient,omitempty"`
	// RecordedAt 是事件提交到权威日志时的 Unix 毫秒时间。
	RecordedAt int64 `json:"recordedAt"`
	// Type 是事件的稳定业务类型。
	Type constants.ChatEventType `json:"type"`
	// ExecutionID 是事件所属执行标识；Chat 级事件为空。
	ExecutionID string `json:"-"`
	// Message 是该事件产生的安全消息增量快照。
	Message *messageview.Message `json:"message,omitempty"`
	// ExecutionStatus 是执行生命周期事件的公开状态。
	ExecutionStatus constants.ExecutionStatus `json:"executionStatus,omitempty"`
	// ErrorCode 是失败时供界面映射的稳定错误码。
	ErrorCode string `json:"errorCode,omitempty"`
	// ErrorMessage 是失败时供用户诊断的简明说明。
	ErrorMessage string `json:"errorMessage,omitempty"`
	// ApprovalRequest 是审批请求事件携带的安全交互内容。
	ApprovalRequest *eventlog.ApprovalRequestedData `json:"approvalRequest,omitempty"`
	// ApprovalResolution 是审批处理事件携带的安全权威结果。
	ApprovalResolution *eventlog.ApprovalResolvedData `json:"approvalResolution,omitempty"`
	// ElicitationRequest 是结构化输入请求事件携带的安全交互内容。
	ElicitationRequest *eventlog.ElicitationRequestedData `json:"elicitationRequest,omitempty"`
	// ElicitationResolution 是结构化输入处理事件携带的安全权威结果。
	ElicitationResolution *eventlog.ElicitationResolvedData `json:"elicitationResolution,omitempty"`
}

// Adapter 将各 Runtime 的权威日志统一转换为前端事件，封装增量状态与脱敏边界。
type Adapter struct {
	// messageView 保存截至当前游标的消息展示状态。
	messageView *messageview.MessageViewBuilder
	// events 保存已经生成且可按游标重放的公开事件。
	events []PublicEvent
	// lastSequence 保存最近成功应用的权威事件序号。
	lastSequence int64
	// messageRevisions 保存每条消息已经观察到的最大修订号，供订阅拒绝落后实时快照。
	messageRevisions map[string]int64
	// retainEvents 表示调用方仍需要公开历史；实时订阅取完积压后关闭保留。
	retainEvents bool
}

// NewAdapter 创建一个空的前端事件适配器。
func NewAdapter() *Adapter {
	return &Adapter{
		messageView:      messageview.NewPublicMessageViewBuilder(),
		events:           make([]PublicEvent, 0),
		messageRevisions: make(map[string]int64),
		retainEvents:     true,
	}
}

// Replay 使用有序权威记录恢复事件适配器状态。
func Replay(records []eventlog.Record) (*Adapter, error) {
	return ReplayAfter(records, 0)
}

// ReplayAfter 恢复游标前状态并构建游标之后的公开事件。
func ReplayAfter(records []eventlog.Record, after int64) (*Adapter, error) {
	if after < 0 {
		return nil, status_error.ChatEventCursorInvalid
	}
	adapter := NewAdapter()
	for _, record := range records {
		if _, err := adapter.apply(record, record.Sequence > after); err != nil {
			return nil, err
		}
	}
	return adapter, nil
}

// Apply 增量应用一条记录并返回对应的安全公开事件。
func (a *Adapter) Apply(record eventlog.Record) (PublicEvent, error) {
	return a.apply(record, true)
}

// apply 始终恢复权威状态并校验事件；游标前不物化公开消息的中间快照。
func (a *Adapter) apply(record eventlog.Record, emit bool) (PublicEvent, error) {
	if a == nil || a.messageView == nil {
		return PublicEvent{}, status_error.ChatLogError
	}
	if record.Sequence <= a.lastSequence {
		return PublicEvent{}, status_error.ChatLogError
	}
	messageID, err := a.messageView.Apply(record)
	if err != nil {
		return PublicEvent{}, err
	}
	event := PublicEvent{
		SchemaVersion: record.SchemaVersion,
		Sequence:      record.Sequence,
		RecordedAt:    record.RecordedAt,
		Type:          record.Type,
		ExecutionID:   record.ExecutionID,
	}
	if isExecutionTerminalRecord(record.Type) {
		var finished eventlog.ExecutionFinishedData
		if err := json.Unmarshal(record.Data, &finished); err != nil {
			return PublicEvent{}, status_error.ChatLogError
		}
		event.ExecutionStatus = finished.Status
		event.ErrorCode = finished.ErrorCode
		event.ErrorMessage = finished.ErrorMessage
	}
	if record.Type == constants.ChatEventTypeApprovalRequested {
		var requested eventlog.ApprovalRequestedData
		if err := json.Unmarshal(record.Data, &requested); err != nil {
			return PublicEvent{}, status_error.ChatLogCorrupted
		}
		event.ApprovalRequest = &requested
	}
	if record.Type == constants.ChatEventTypeApprovalResolved {
		var resolved eventlog.ApprovalResolvedData
		if err := json.Unmarshal(record.Data, &resolved); err != nil {
			return PublicEvent{}, status_error.ChatLogCorrupted
		}
		event.ApprovalResolution = &resolved
	}
	if record.Type == constants.ChatEventTypeElicitationRequested {
		var requested eventlog.ElicitationRequestedData
		if err := json.Unmarshal(record.Data, &requested); err != nil {
			return PublicEvent{}, status_error.ChatLogCorrupted
		}
		event.ElicitationRequest = &requested
	}
	if record.Type == constants.ChatEventTypeElicitationResolved {
		var resolved eventlog.ElicitationResolvedData
		if err := json.Unmarshal(record.Data, &resolved); err != nil {
			return PublicEvent{}, status_error.ChatLogCorrupted
		}
		event.ElicitationResolution = &resolved
	}
	if messageID != "" {
		a.messageRevisions[messageID] = max(a.messageRevisions[messageID], a.messageView.MessageRevision(messageID))
		if emit {
			event.Message = a.messageView.PublicMessage(messageID)
		}
	}
	if emit && a.retainEvents {
		a.events = append(a.events, event)
	}
	a.lastSequence = record.Sequence
	return ClonePublicEvent(event), nil
}

// MessageRevisions 返回每条消息已观察到的最高修订号，不建立历史中间快照。
func (a *Adapter) MessageRevisions() map[string]int64 {
	revisions := make(map[string]int64, len(a.messageRevisions))
	for id, revision := range a.messageRevisions {
		revisions[id] = revision
	}
	return revisions
}

// ReleaseHistory 释放已交付的公开事件快照；后续 Apply 仅返回当前事件，不再积累历史。
func (a *Adapter) ReleaseHistory() {
	a.events = nil
	a.retainEvents = false
}

// EventsAfter 返回游标之后的公开事件；limit 为零时不限制数量。
func (a *Adapter) EventsAfter(after int64, limit int) []PublicEvent {
	if a == nil || limit < 0 {
		return nil
	}
	events := make([]PublicEvent, 0)
	for _, event := range a.events {
		if event.Sequence <= after {
			continue
		}
		events = append(events, ClonePublicEvent(event))
		if limit > 0 && len(events) >= limit {
			break
		}
	}
	return events
}

func isExecutionTerminalRecord(eventType constants.ChatEventType) bool {
	return eventType == constants.ChatEventTypeExecutionCompleted ||
		eventType == constants.ChatEventTypeExecutionFailed ||
		eventType == constants.ChatEventTypeExecutionCanceled
}

// ClonePublicEvent 深拷贝公开事件中包含的可变消息数据。
func ClonePublicEvent(event PublicEvent) PublicEvent {
	clone := event
	if event.ApprovalRequest != nil {
		request := *event.ApprovalRequest
		request.Options = append([]eventlog.ApprovalOptionData(nil), request.Options...)
		request.Locations = make([]eventlog.ApprovalLocationData, len(event.ApprovalRequest.Locations))
		for index, location := range event.ApprovalRequest.Locations {
			request.Locations[index] = location
			if location.Line != nil {
				line := *location.Line
				request.Locations[index].Line = &line
			}
		}
		clone.ApprovalRequest = &request
	}
	if event.ApprovalResolution != nil {
		resolution := *event.ApprovalResolution
		clone.ApprovalResolution = &resolution
	}
	if event.ElicitationRequest != nil {
		request := *event.ElicitationRequest
		request.RequestedSchema = append(json.RawMessage(nil), event.ElicitationRequest.RequestedSchema...)
		clone.ElicitationRequest = &request
	}
	if event.ElicitationResolution != nil {
		resolution := *event.ElicitationResolution
		if resolution.Content != nil {
			resolution.Content = cloneStringAnyMap(resolution.Content)
		}
		clone.ElicitationResolution = &resolution
	}
	if event.Message != nil {
		message := *event.Message
		if event.Message.Fork != nil {
			fork := *event.Message.Fork
			message.Fork = &fork
		}
		message.Attachments = append([]eventlog.AttachmentReference(nil), event.Message.Attachments...)
		message.Skills = append([]eventlog.MessageSkillReference(nil), event.Message.Skills...)
		message.Connectors = append([]eventlog.MessageConnectorReference(nil), event.Message.Connectors...)
		message.CodeReferences = append([]eventlog.CodeReference(nil), event.Message.CodeReferences...)
		message.DisplayParts = append([]messageview.MessageDisplayPart(nil), event.Message.DisplayParts...)
		for index, part := range message.DisplayParts {
			if part.ToolSafeSummary != nil {
				summary := *part.ToolSafeSummary
				summary.Files = append(
					[]messageview.ToolFileChangeSummary(nil),
					part.ToolSafeSummary.Files...,
				)
				message.DisplayParts[index].ToolSafeSummary = &summary
			}
		}
		if event.Message.RunMetrics != nil {
			metrics := *event.Message.RunMetrics
			message.RunMetrics = &metrics
		}
		clone.Message = &message
	}
	return clone
}

func cloneStringAnyMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
