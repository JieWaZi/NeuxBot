package eventlog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// persistedRecord 是 JSONL 实际写入的作用域式最小信封。
type persistedRecord struct {
	// SchemaVersion 是解释当前日志行所需的格式版本。
	SchemaVersion int `json:"schemaVersion"`
	// RecordedAt 是需要保留时间语义的完整记录提交时间。
	RecordedAt int64 `json:"recordedAt"`
	// Type 是记录的稳定业务类型。
	Type constants.ChatEventType `json:"type"`
	// MessageID 只在稳定消息身份首次出现时写入。
	MessageID string `json:"messageId,omitempty"`
	// ExecutionID 只在执行作用域入口写入。
	ExecutionID string `json:"executionId,omitempty"`
	// InvocationID 只在 Runtime Invocation 作用域发生变化时写入。
	InvocationID string `json:"invocationId,omitempty"`
	// Data 是记录类型定义的完整 JSON 载荷。
	Data json.RawMessage `json:"data,omitempty"`
}

// recordScope 保存从日志前序记录推导出的当前执行上下文。
type recordScope struct {
	// executionID 是当前活跃执行标识。
	executionID string
	// invocationID 是当前活跃 Runtime Invocation 标识。
	invocationID string
	// toolUpdates 仅在存储编解码内保存当前调用各工具的上一份完整载荷。
	toolUpdates map[string]json.RawMessage
}

// encodeRecords 为一次原子追加分配内存序号和确定的文件位置。
func encodeRecords(
	chatID string,
	after int64,
	startOffset int64,
	initialScope recordScope,
	inputs []AppendInput,
) ([]Record, []byte, recordScope, error) {
	now := time.Now().UnixMilli()
	scope := initialScope
	scope.toolUpdates = maps.Clone(initialScope.toolUpdates)
	records := make([]Record, 0, len(inputs))
	var buffer bytes.Buffer
	for index, input := range inputs {
		if input.Type == constants.ChatEventTypeUnspecified {
			return nil, nil, initialScope, status_error.ChatLogRecordInvalid
		}
		if input.Type == constants.ChatEventTypeRuntimeEvent {
			return nil, nil, initialScope, status_error.ChatLogRecordInvalid
		}
		persisted := persistedRecord{
			SchemaVersion: constants.ChatLogSchemaVersion,
			RecordedAt:    now,
			Type:          input.Type,
			Data:          append(json.RawMessage(nil), input.Data...),
		}
		if isMessageIdentityRecord(input.Type) {
			persisted.MessageID = input.MessageID
		}
		if input.Type == constants.ChatEventTypeExecutionCreated {
			if input.ExecutionID == "" {
				return nil, nil, initialScope, status_error.ChatLogRecordInvalid
			}
			persisted.ExecutionID = input.ExecutionID
			scope.executionID = input.ExecutionID
			scope.invocationID = ""
		}
		if input.InvocationID != "" &&
			input.Type != constants.ChatEventTypeExecutionCreated &&
			input.InvocationID != scope.invocationID {
			persisted.InvocationID = input.InvocationID
			scope.invocationID = input.InvocationID
			scope.toolUpdates = nil
		}
		if input.Type == constants.ChatEventTypeExecutionCreated {
			scope.toolUpdates = nil
		}
		if isExecutionMessageRecord(input.Type) {
			var err error
			persisted.Data, err = scope.encodeRuntimeMessage(input.Data)
			if err != nil {
				return nil, nil, initialScope, err
			}
		}
		record := Record{
			SchemaVersion: persisted.SchemaVersion,
			ChatID:        chatID,
			Sequence:      after + int64(index) + 1,
			RecordedAt:    persisted.RecordedAt,
			Type:          input.Type,
			MessageID:     resolvedMessageID(input, scope),
			ExecutionID:   resolvedExecutionID(input, scope),
			InvocationID:  resolvedInvocationID(input, scope),
			Data:          append(json.RawMessage(nil), input.Data...),
		}
		encoded, err := json.Marshal(persisted)
		if err != nil {
			return nil, nil, initialScope, status_error.ChatLogRecordInvalid
		}
		record.Offset = startOffset + int64(buffer.Len())
		record.Length = int64(len(encoded) + 1)
		buffer.Write(encoded)
		buffer.WriteByte('\n')
		records = append(records, record)
		if isExecutionTerminalRecord(input.Type) {
			scope = recordScope{}
		}
	}
	return records, buffer.Bytes(), scope, nil
}

// decodeRecordPayload 是所有读取路径共享的格式校验和作用域恢复入口。
func decodeRecordPayload(
	payload []byte,
	chatID string,
	sequence int64,
	location string,
	scope *recordScope,
) (Record, error) {
	var persisted persistedRecord
	if err := json.Unmarshal(payload, &persisted); err != nil {
		return Record{}, status_error.ChatLogCorrupted
	}
	if persisted.SchemaVersion != constants.ChatLogSchemaVersion {
		return Record{}, status_error.ChatLogCorrupted
	}
	if persisted.Type == constants.ChatEventTypeUnspecified {
		return Record{}, status_error.ChatLogCorrupted
	}
	if persisted.ExecutionID != "" {
		scope.toolUpdates = nil
		scope.executionID = persisted.ExecutionID
		scope.invocationID = ""
	}
	if persisted.Type == constants.ChatEventTypeExecutionCreated {
		if persisted.ExecutionID == "" {
			return Record{}, status_error.ChatLogCorrupted
		}
	}
	if persisted.InvocationID != "" {
		if scope.invocationID != persisted.InvocationID {
			scope.toolUpdates = nil
		}
		scope.invocationID = persisted.InvocationID
	}
	record := Record{
		SchemaVersion: persisted.SchemaVersion,
		ChatID:        chatID,
		Sequence:      sequence,
		RecordedAt:    persisted.RecordedAt,
		Type:          persisted.Type,
		MessageID:     persisted.MessageID,
		ExecutionID:   scope.executionID,
		InvocationID:  scope.invocationID,
		Data:          append(json.RawMessage(nil), persisted.Data...),
	}
	if record.Type == constants.ChatEventTypeExecutionCreated || isExecutionMessageRecord(record.Type) {
		record.MessageID = scope.executionID
	}
	if isExecutionMessageRecord(record.Type) {
		var err error
		record.Data, err = scope.decodeRuntimeMessage(record.Data)
		if err != nil {
			return Record{}, err
		}
	}
	if isExecutionTerminalRecord(record.Type) {
		*scope = recordScope{}
	}
	return record, nil
}

// decodeRecordRange 解码已由消息位置索引选定的连续半开字节区间。
func decodeRecordRange(
	chatID string,
	startOffset int64,
	firstSequence int64,
	content []byte,
) ([]Record, error) {
	if len(content) == 0 {
		return []Record{}, nil
	}
	if content[len(content)-1] != '\n' {
		return nil, status_error.ChatLogCorrupted
	}
	lines := bytes.SplitAfter(content, []byte{'\n'})
	records := make([]Record, 0, len(lines)-1)
	offset := startOffset
	sequence := firstSequence
	scope := recordScope{}
	for index, line := range lines {
		lineLength := int64(len(line))
		payload := bytes.TrimSuffix(line, []byte{'\n'})
		if len(payload) == 0 {
			continue
		}
		record, err := decodeRecordPayload(
			payload,
			chatID,
			sequence,
			fmt.Sprintf("chat log range line %d", index+1),
			&scope,
		)
		if err != nil {
			return nil, err
		}
		record.Offset = offset
		record.Length = lineLength
		records = append(records, record)
		sequence++
		offset += lineLength
	}
	return records, nil
}

func isMessageIdentityRecord(eventType constants.ChatEventType) bool {
	return eventType == constants.ChatEventTypeUserMessageCreated || eventType == constants.ChatEventTypeMessageInherited
}

func isExecutionMessageRecord(eventType constants.ChatEventType) bool {
	return eventType == constants.ChatEventTypeAssistantMessage ||
		eventType == constants.ChatEventTypeToolMessage
}

func isExecutionTerminalRecord(eventType constants.ChatEventType) bool {
	return eventType == constants.ChatEventTypeExecutionCompleted ||
		eventType == constants.ChatEventTypeExecutionFailed ||
		eventType == constants.ChatEventTypeExecutionCanceled
}

func resolvedMessageID(input AppendInput, scope recordScope) string {
	if isMessageIdentityRecord(input.Type) {
		return input.MessageID
	}
	if input.Type == constants.ChatEventTypeExecutionCreated {
		return input.ExecutionID
	}
	if isExecutionMessageRecord(input.Type) {
		return scope.executionID
	}
	return ""
}

func resolvedExecutionID(input AppendInput, scope recordScope) string {
	if input.ExecutionID != "" {
		return input.ExecutionID
	}
	return scope.executionID
}

func resolvedInvocationID(input AppendInput, scope recordScope) string {
	if input.InvocationID != "" {
		return input.InvocationID
	}
	return scope.invocationID
}
