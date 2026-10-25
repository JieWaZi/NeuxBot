// Package events 把领域安全事件复用到一条受认证的全局 WebSocket。
package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	eventhub "github.com/JieWaZi/NeuxBot/server/internal/notification/events"
)

// control 只描述订阅，不承载任何业务命令或任意连接地址。
type control struct {
	// Type 是注册、释放或权威恢复操作。
	Type string `json:"type"`
	// RequestID 关联本次受限操作的回执。
	RequestID string `json:"requestId"`
	// Topic 是编译期允许的领域主题。
	Topic string `json:"topic"`
	// Resource 定位原领域身份及其归属。
	Resource eventhub.Resource `json:"resource"`
	// After 保留原数字或不透明游标，不能使用连接序号替代。
	After json.RawMessage `json:"after,omitempty"`
}

// frame 是唯一 writer 赋予连接序号后的公开信封。
type frame struct {
	// ProtocolVersion 是当前信封结构版本。
	ProtocolVersion int `json:"protocolVersion"`
	// Epoch 标识当前 Core 生命周期。
	Epoch string `json:"epoch"`
	// ConnectionID 标识本条物理连接。
	ConnectionID string `json:"connectionId"`
	// DeliverySequence 只在实际发送时连续递增。
	DeliverySequence uint64 `json:"deliverySequence"`
	// Topic 是注册的业务主题。
	Topic string `json:"topic"`
	// Type 区分控制回执、安全领域事件与状态变化。
	Type string `json:"type"`
	// Resource 是事件对应的原身份，不扩大调用权限。
	Resource eventhub.Resource `json:"resource"`
	// EventID 是本次发布的稳定去重身份。
	EventID string `json:"eventId"`
	// SourceCursor 使用该领域原有恢复序号或字符串游标。
	SourceCursor json.RawMessage `json:"sourceCursor,omitempty"`
	// Transient 不推进原持久游标。
	Transient bool `json:"transient,omitempty"`
	// Payload 只包含原服务的公开安全投影。
	Payload json.RawMessage `json:"payload"`
}

var allowedTopics = map[string]bool{"notifications": true, "work-board": true, "work-results": true, "entities": true, "automatic-tasks": true, "im": true, "runtimes": true, "system": true, "chat": true, "team": true, "workflow": true, "account-login": true, "plugins": true, "resources": true, "knowledge": true, "workspace": true}

func decodeControl(data []byte) (control, error) {
	var result control
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, status_error.BadRequest
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return result, status_error.BadRequest
	}
	if result.Type != "subscribe" && result.Type != "unsubscribe" && result.Type != "resync" {
		return result, status_error.BadRequest
	}
	if result.RequestID == "" || len(result.RequestID) > 100 || !allowedTopics[result.Topic] || !validResourceShape(result.Topic, result.Resource) {
		return result, status_error.BadRequest
	}
	if len(result.After) > 514 {
		return result, status_error.BadRequest
	}
	if len(result.After) > 0 {
		if result.After[0] == '"' {
			var cursor string
			if err := json.Unmarshal(result.After, &cursor); err != nil {
				return result, status_error.BadRequest
			}
		} else if _, err := numberCursor(result.After); err != nil {
			return result, status_error.BadRequest
		}
	}
	return result, nil
}
func validResourceShape(topic string, resource eventhub.Resource) bool {
	if resource.Kind == "" || len(resource.Kind) > 100 || len(resource.ID) > 100 || len(resource.ParentID) > 100 || strings.ContainsAny(resource.Kind+resource.ID+resource.ParentID, "/\\\x00") {
		return false
	}
	if resource.Kind == "global" {
		return resource.ID == "" && resource.ParentID == "" && topic != "chat" && topic != "team" && topic != "workflow" && topic != "account-login" && topic != "workspace"
	}
	if resource.ID == "" {
		return false
	}
	switch topic {
	case "workspace":
		return resource.Kind == "workspace_watch" && resource.ParentID == ""
	case "knowledge":
		return resource.Kind == "knowledge_library" && resource.ParentID != ""
	case "chat":
		return resource.Kind == "chat" && resource.ParentID == ""
	case "team":
		return resource.Kind == "thread" && resource.ParentID != ""
	case "workflow":
		return resource.Kind == "workflow"
	case "account-login":
		return resource.Kind == "account" && resource.ParentID != ""
	case "work-board", "work-results":
		return (resource.Kind == "chat" || resource.Kind == "team_thread" || resource.Kind == "workflow_run") && resource.ParentID == ""
	case "entities":
		return (resource.Kind == "waker" || resource.Kind == "channel") && resource.ParentID == ""
	case "automatic-tasks":
		return (resource.Kind == "automatic_task" || resource.Kind == "waker" || resource.Kind == "channel") && resource.ParentID == ""
	case "im":
		return (resource.Kind == "connection" || resource.Kind == "binding" || resource.Kind == "conversation" || resource.Kind == "login") && resource.ParentID == ""
	case "resources":
		return (resource.Kind == "skill" || resource.Kind == "connector" || resource.Kind == "waker") && resource.ParentID == ""
	case "plugins":
		return resource.Kind == "plugin" && resource.ParentID == ""
	case "runtimes":
		return resource.Kind == "runtime" && resource.ParentID == ""
	default:
		return false
	}
}
func resourceKey(topic string, resource eventhub.Resource) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%s", topic, resource.Kind, resource.ID, resource.ParentID)
}
func numberCursor(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 {
		return 0, nil
	}
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil || value < 0 || value > 9_007_199_254_740_991 {
		return 0, status_error.BadRequest
	}
	return value, nil
}
func stringCursor(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || len(value) > 32 {
		return "", status_error.BadRequest
	}
	return value, nil
}
func numericJSON(value int64) json.RawMessage { return json.RawMessage(strconv.FormatInt(value, 10)) }
func stringJSON(value string) json.RawMessage { data, _ := json.Marshal(value); return data }
