package runtimes

import (
	"context"
	"encoding/json"
	"strings"

	acp "github.com/JieWaZi/acp-go/sdk"
)

const (
	acpElicitationMaxTextLength  = 4096
	acpElicitationMaxSchemaBytes = 65536
)

// createElicitation 等待 Host 返回结构化用户输入；任何无效或不可用交互都安全取消。
func (bridge *acpBridge) createElicitation(
	ctx context.Context,
	request acp.UnstableCreateElicitationRequest,
) acp.UnstableCreateElicitationResponse {
	normalized, valid := normalizeACPElicitationRequest(request)
	if !valid || bridge.elicitationHandler == nil {
		return acp.NewUnstableCreateElicitationResponseCancel()
	}
	bridge.setElicitationPending(1)
	defer bridge.setElicitationPending(-1)
	normalized.InvocationID = bridge.invocationID
	decision, err := bridge.elicitationHandler.Request(ctx, normalized)
	if err != nil || decision.Cancelled {
		return acp.NewUnstableCreateElicitationResponseCancel()
	}
	if decision.Declined {
		return acp.NewUnstableCreateElicitationResponseDecline()
	}
	if !decision.Accepted {
		return acp.NewUnstableCreateElicitationResponseCancel()
	}
	response := acp.NewUnstableCreateElicitationResponseAccept()
	if response.Accept != nil {
		response.Accept.Content = decision.Content
	}
	return response
}

// normalizeACPElicitationRequest 只提取 ACP 标准安全展示字段。
func normalizeACPElicitationRequest(
	request acp.UnstableCreateElicitationRequest,
) (ElicitationRequest, bool) {
	if request.Form != nil {
		message := strings.TrimSpace(request.Form.Message)
		if message == "" || len(message) > acpElicitationMaxTextLength {
			return ElicitationRequest{}, false
		}
		schema, err := json.Marshal(request.Form.RequestedSchema)
		if err != nil || len(schema) == 0 || len(schema) > acpElicitationMaxSchemaBytes {
			return ElicitationRequest{}, false
		}
		var schemaObject map[string]any
		if json.Unmarshal(schema, &schemaObject) != nil {
			return ElicitationRequest{}, false
		}
		return ElicitationRequest{
			Mode:            "form",
			Message:         message,
			RequestedSchema: schemaObject,
		}, true
	}
	if request.Url != nil {
		message := strings.TrimSpace(request.Url.Message)
		url := strings.TrimSpace(request.Url.Url)
		if message == "" || url == "" ||
			len(message) > acpElicitationMaxTextLength ||
			len(url) > acpElicitationMaxTextLength {
			return ElicitationRequest{}, false
		}
		return ElicitationRequest{
			Mode:    "url",
			Message: message,
			URL:     url,
		}, true
	}
	return ElicitationRequest{}, false
}

// acpElicitationSessionID 从 ACP 扩展元数据提取 Session 标识以路由当前 Invocation。
func acpElicitationSessionID(
	request acp.UnstableCreateElicitationRequest,
	binding acpRuntimeBinding,
) (acp.SessionId, bool) {
	meta := acpElicitationMeta(request)
	unique := ""
	for _, path := range binding.elicitationSessionPaths {
		candidate := acpMetaPathString(meta, path)
		if candidate == "" {
			continue
		}
		if unique != "" && candidate != unique {
			return "", false
		}
		unique = candidate
	}
	return acp.SessionId(unique), unique != ""
}

func acpElicitationMeta(request acp.UnstableCreateElicitationRequest) map[string]any {
	if request.Form != nil {
		return request.Form.Meta
	}
	if request.Url != nil {
		return request.Url.Meta
	}
	return nil
}

// acpMetaPathString 读取结构化交互元数据中的嵌套字符串路径。
func acpMetaPathString(meta map[string]any, path []string) string {
	if len(path) == 0 {
		return ""
	}
	current := meta
	for _, key := range path[:len(path)-1] {
		nested, _ := current[key].(map[string]any)
		if nested == nil {
			return ""
		}
		current = nested
	}
	value, _ := current[path[len(path)-1]].(string)
	return strings.TrimSpace(value)
}
