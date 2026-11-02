package eventlog

import (
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// storedRuntimeMessage 只在文件边界使用，公开记录始终保留完整规范事件。
type storedRuntimeMessage struct {
	// Content 是文件中的完整载荷或可逆差量。
	Content []storedRuntimeContent `json:"content"`
	// MessageRevision 保留原消息修订。
	MessageRevision int64 `json:"messageRevision"`
}
type storedRuntimeContent struct {
	// Type 是规范内容类型。
	Type constants.RuntimeEventType `json:"type"`
	// Data 是未压缩的完整载荷。
	Data json.RawMessage `json:"data,omitempty"`
	// Patch 是当前工具上一份载荷的字节替换。
	Patch *runtimeMessagePatch `json:"patch,omitempty"`
}
type runtimeMessagePatch struct {
	// ToolCallID 是宿主规范工具身份，不解释原生协议别名。
	ToolCallID string `json:"toolCallId"`
	// BaseBytes 校验上一份载荷长度。
	BaseBytes int `json:"baseBytes"`
	// PrefixBytes 是不变前缀长度。
	PrefixBytes int `json:"prefixBytes"`
	// SuffixBytes 是不变后缀长度。
	SuffixBytes int `json:"suffixBytes"`
	// Insert 是替换中段，JSON 以 base64 保存任意字节。
	Insert []byte `json:"insert"`
}

func runtimeContentToolID(data json.RawMessage) string {
	var identity struct {
		// ToolCallID 是事件适配器输出的唯一规范身份。
		ToolCallID string `json:"toolCallId"`
	}
	_ = json.Unmarshal(data, &identity)
	return identity.ToolCallID
}

func (s *recordScope) encodeRuntimeMessage(data json.RawMessage) (json.RawMessage, error) {
	var message RuntimeMessageData
	if json.Unmarshal(data, &message) != nil {
		return nil, status_error.ChatLogRecordInvalid
	}
	stored := storedRuntimeMessage{MessageRevision: message.MessageRevision, Content: make([]storedRuntimeContent, 0, len(message.Content))}
	for _, item := range message.Content {
		content := storedRuntimeContent{Type: item.Type, Data: item.Data}
		id := runtimeContentToolID(item.Data)
		if item.Type == constants.RuntimeEventTypeToolUpdate && id != "" {
			base := s.toolUpdates[id]
			if s.toolUpdates == nil {
				s.toolUpdates = map[string]json.RawMessage{}
			}
			s.toolUpdates[id] = append(json.RawMessage(nil), item.Data...)
			if len(base) > 0 {
				prefix, suffix := 0, 0
				for prefix < len(base) && prefix < len(item.Data) && base[prefix] == item.Data[prefix] {
					prefix++
				}
				for suffix < len(base)-prefix && suffix < len(item.Data)-prefix && base[len(base)-1-suffix] == item.Data[len(item.Data)-1-suffix] {
					suffix++
				}
				compact := storedRuntimeContent{Type: item.Type, Patch: &runtimeMessagePatch{ToolCallID: id, BaseBytes: len(base), PrefixBytes: prefix, SuffixBytes: suffix, Insert: append([]byte(nil), item.Data[prefix:len(item.Data)-suffix]...)}}
				encoded, _ := json.Marshal(compact)
				if len(encoded) < len(item.Data) {
					content = compact
				}
			}
		}
		if item.Type == constants.RuntimeEventTypeToolResult {
			delete(s.toolUpdates, id)
		}
		stored.Content = append(stored.Content, content)
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		return nil, status_error.ChatLogRecordInvalid
	}
	return encoded, nil
}

func (s *recordScope) decodeRuntimeMessage(data json.RawMessage) (json.RawMessage, error) {
	var stored storedRuntimeMessage
	if json.Unmarshal(data, &stored) != nil {
		return nil, status_error.ChatLogCorrupted
	}
	message := RuntimeMessageData{MessageRevision: stored.MessageRevision, Content: make([]RuntimeMessageContent, 0, len(stored.Content))}
	for _, content := range stored.Content {
		payload := content.Data
		if patch := content.Patch; patch != nil {
			base := s.toolUpdates[patch.ToolCallID]
			if content.Type != constants.RuntimeEventTypeToolUpdate || len(payload) > 0 || patch.ToolCallID == "" || len(base) == 0 || len(base) != patch.BaseBytes || patch.PrefixBytes < 0 || patch.SuffixBytes < 0 || patch.PrefixBytes > len(base) || patch.SuffixBytes > len(base)-patch.PrefixBytes {
				return nil, status_error.ChatLogCorrupted
			}
			payload = append(json.RawMessage(nil), base[:patch.PrefixBytes]...)
			payload = append(payload, patch.Insert...)
			payload = append(payload, base[len(base)-patch.SuffixBytes:]...)
			if !json.Valid(payload) || runtimeContentToolID(payload) != patch.ToolCallID {
				return nil, status_error.ChatLogCorrupted
			}
		}
		id := runtimeContentToolID(payload)
		if content.Type == constants.RuntimeEventTypeToolUpdate && id != "" {
			if s.toolUpdates == nil {
				s.toolUpdates = map[string]json.RawMessage{}
			}
			s.toolUpdates[id] = append(json.RawMessage(nil), payload...)
		}
		if content.Type == constants.RuntimeEventTypeToolResult {
			delete(s.toolUpdates, id)
		}
		message.Content = append(message.Content, RuntimeMessageContent{Type: content.Type, Data: payload})
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return nil, status_error.ChatLogCorrupted
	}
	return encoded, nil
}
