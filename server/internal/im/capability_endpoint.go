package im

import (
	"bytes"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"io"
)

// ToolInput 是聊天与工作流共用的固定 IM 能力输入。
type ToolInput struct {
	// Action 是目录查询、登记投递或查询投递状态。
	Action string `json:"action"`
	// RequestID 是创建投递时保持不变的调用身份。
	RequestID string `json:"requestId,omitempty"`
	// TargetID 是真实目录中获准的聊天身份。
	TargetID string `json:"targetId,omitempty"`
	// Content 是将冻结发送的正文。
	Content string `json:"content,omitempty"`
	// Files 是当前执行批准目录中的文件路径。
	Files []string `json:"files,omitempty"`
	// DeliveryID 是查询时的投递身份。
	DeliveryID string `json:"deliveryId,omitempty"`
}

// DecodeToolInput 对直接工作流调用和 MCP 输入执行同一严格解析。
func DecodeToolInput(encoded []byte) (ToolInput, error) {
	var input ToolInput
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if len(encoded) > 250000 || decoder.Decode(&input) != nil {
		return input, status_error.IMConfigurationInvalid
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF || len(input.RequestID) > 100 || len(input.Content) > 200000 || len(input.Files) > 10 {
		return input, status_error.IMConfigurationInvalid
	}
	switch input.Action {
	case "targets.list", "deliveries.create", "deliveries.get":
		return input, nil
	default:
		return input, status_error.IMConfigurationInvalid
	}
}
