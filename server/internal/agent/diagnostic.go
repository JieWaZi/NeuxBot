package agent

import (
	"encoding/json"
	"strings"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// RecordedFailureDisplayMessage 从已记录的失败文本或 ACP 错误提取可展示说明。
func RecordedFailureDisplayMessage(message string) string {
	message = strings.TrimSpace(message)
	if !json.Valid([]byte(message)) {
		return message
	}
	var requestError acp.RequestError
	if err := json.Unmarshal([]byte(message), &requestError); err != nil {
		return message
	}
	if detail := acpFailureDetail(requestError.Data); detail != "" {
		return detail
	}
	return strings.TrimSpace(requestError.Message)
}

func acpFailureDetail(data any) string {
	metadata, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"cause", "error", "message", "details"} {
		if detail, ok := metadata[key].(string); ok && strings.TrimSpace(detail) != "" {
			return strings.TrimSpace(detail)
		}
	}
	return ""
}
