package runtimes

import (
	"context"
	"encoding/json"
	"strings"

	acp "github.com/JieWaZi/acp-go/sdk"
)

const (
	acpApprovalMaxIDLength       = 512
	acpApprovalMaxTextLength     = 4096
	acpApprovalMaxKindLength     = 128
	acpApprovalMaxLocationCount  = 64
	acpApprovalMaxOptionCount    = 64
	acpApprovalMaxOptionNameSize = 1024
	acpApprovalMaxCommandRunes   = 120
)

// requestPermission 自动接纳入口签发的内部控制面范围，其余请求等待 Host 返回用户选择。
func (bridge *acpBridge) requestPermission(
	ctx context.Context,
	request acp.RequestPermissionRequest,
) acp.RequestPermissionResponse {
	if optionID, trusted := trustedControlApprovalOption(
		request,
		bridge.policy.TrustedControlTools,
	); trusted {
		return acp.RequestPermissionResponse{
			Outcome: acp.NewRequestPermissionOutcomeSelected(optionID),
		}
	}
	if bridge.policy.WorkspaceReadOnly {
		return acp.RequestPermissionResponse{
			Outcome: acp.NewRequestPermissionOutcomeCancelled(),
		}
	}
	normalized, valid := normalizeACPApprovalRequest(request)
	if !valid || bridge.approvalHandler == nil {
		return acp.RequestPermissionResponse{
			Outcome: acp.NewRequestPermissionOutcomeCancelled(),
		}
	}
	bridge.setApprovalPending(1)
	defer bridge.setApprovalPending(-1)
	// 权限回调可先于 invocation_started 交付，归属必须取自本 Bridge 而非宿主最新状态。
	normalized.InvocationID = bridge.invocationID
	decision, err := bridge.approvalHandler.Request(ctx, normalized)
	if err != nil || decision.Cancelled || strings.TrimSpace(decision.OptionID) == "" {
		return acp.RequestPermissionResponse{
			Outcome: acp.NewRequestPermissionOutcomeCancelled(),
		}
	}
	for _, option := range normalized.Options {
		if option.ID == decision.OptionID {
			return acp.RequestPermissionResponse{
				Outcome: acp.NewRequestPermissionOutcomeSelected(
					acp.PermissionOptionId(decision.OptionID),
				),
			}
		}
	}
	return acp.RequestPermissionResponse{
		Outcome: acp.NewRequestPermissionOutcomeCancelled(),
	}
}

// trustedControlApprovalOption 只批准精确匹配内部控制面 server/tool/action 的单次选择。
func trustedControlApprovalOption(
	request acp.RequestPermissionRequest,
	trustedTools []TrustedControlTool,
) (acp.PermissionOptionId, bool) {
	if _, valid := normalizeACPApprovalRequest(request); !valid {
		return "", false
	}
	server, tool, action, valid := trustedControlIdentity(request.ToolCall.RawInput)
	if !valid {
		return "", false
	}
	trusted := false
	for _, candidate := range trustedTools {
		if candidate.Server != server || candidate.Tool != tool {
			continue
		}
		for _, allowedAction := range candidate.Actions {
			if allowedAction == action {
				trusted = true
				break
			}
		}
		break
	}
	if !trusted {
		return "", false
	}
	var selected acp.PermissionOptionId
	for _, option := range request.Options {
		if option.Kind != acp.PermissionOptionKindAllowOnce ||
			strings.TrimSpace(string(option.OptionId)) == "" {
			continue
		}
		if selected != "" {
			return "", false
		}
		selected = option.OptionId
	}
	return selected, selected != ""
}

// trustedControlIdentity 严格读取两个 ACP Adapter 共同输出的 MCP 身份和 action。
func trustedControlIdentity(rawInput any) (string, string, string, bool) {
	input, ok := rawInput.(map[string]any)
	if !ok {
		return "", "", "", false
	}
	server, serverOK := input["server"].(string)
	tool, toolOK := input["tool"].(string)
	server = strings.TrimSpace(server)
	tool = strings.TrimSpace(tool)
	if !serverOK || !toolOK || server == "" || tool == "" {
		return "", "", "", false
	}
	arguments, ok := trustedControlArguments(input["arguments"])
	if !ok {
		return "", "", "", false
	}
	action, ok := arguments["action"].(string)
	action = strings.TrimSpace(action)
	if !ok || action == "" {
		return "", "", "", false
	}
	return server, tool, action, true
}

// trustedControlArguments 兼容原始 JSON 与已经解码的对象，但拒绝其他形态。
func trustedControlArguments(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case json.RawMessage:
		var result map[string]any
		if err := json.Unmarshal(typed, &result); err != nil || result == nil {
			return nil, false
		}
		return result, true
	case []byte:
		var result map[string]any
		if err := json.Unmarshal(typed, &result); err != nil || result == nil {
			return nil, false
		}
		return result, true
	default:
		return nil, false
	}
}

// normalizeACPApprovalRequest 只提取 ACP 标准安全展示字段，不读取 rawInput 或扩展元数据。
func normalizeACPApprovalRequest(
	request acp.RequestPermissionRequest,
) (ApprovalRequest, bool) {
	toolCallID := strings.TrimSpace(string(request.ToolCall.ToolCallId))
	title := ""
	if request.ToolCall.Title != nil {
		title = strings.TrimSpace(*request.ToolCall.Title)
	}
	if toolCallID == "" || len(toolCallID) > acpApprovalMaxIDLength ||
		len(title) > acpApprovalMaxTextLength || len(request.Options) == 0 ||
		len(request.Options) > acpApprovalMaxOptionCount ||
		len(request.ToolCall.Locations) > acpApprovalMaxLocationCount {
		return ApprovalRequest{}, false
	}
	summary, valid := acpApprovalSummary(request.ToolCall, title)
	if !valid {
		return ApprovalRequest{}, false
	}
	result := ApprovalRequest{
		ToolCallID: toolCallID,
		Title:      title,
		Summary:    summary,
		Command:    normalizeACPApprovalCommand(request.ToolCall.RawInput),
		Locations:  make([]ApprovalLocation, 0, len(request.ToolCall.Locations)),
		Options:    make([]ApprovalOption, 0, len(request.Options)),
	}
	if request.ToolCall.Kind != nil {
		result.Kind = string(*request.ToolCall.Kind)
		if len(result.Kind) > acpApprovalMaxKindLength {
			return ApprovalRequest{}, false
		}
	}
	for _, location := range request.ToolCall.Locations {
		path := strings.TrimSpace(location.Path)
		if path == "" || len(path) > acpApprovalMaxTextLength {
			return ApprovalRequest{}, false
		}
		var line *int
		if location.Line != nil {
			if *location.Line < 0 {
				return ApprovalRequest{}, false
			}
			lineValue := *location.Line
			line = &lineValue
		}
		result.Locations = append(result.Locations, ApprovalLocation{Path: path, Line: line})
	}
	seen := make(map[string]struct{}, len(request.Options))
	for _, option := range request.Options {
		id := strings.TrimSpace(string(option.OptionId))
		name := strings.TrimSpace(option.Name)
		kind := string(option.Kind)
		if id == "" || len(id) > acpApprovalMaxIDLength || name == "" ||
			len(name) > acpApprovalMaxOptionNameSize ||
			len(kind) > acpApprovalMaxKindLength {
			return ApprovalRequest{}, false
		}
		if _, exists := seen[id]; exists {
			return ApprovalRequest{}, false
		}
		seen[id] = struct{}{}
		result.Options = append(result.Options, ApprovalOption{ID: id, Name: name, Kind: kind})
	}
	return result, true
}

// acpApprovalSummary 展示 ACP 标准文本内容，允许长计划使用正文而非挤入标题。
func acpApprovalSummary(tool acp.ToolCallUpdate, fallback string) (string, bool) {
	var parts []string
	size := 0
	for _, content := range tool.Content {
		if content.Content == nil || content.Content.Content.Text == nil {
			continue
		}
		text := content.Content.Content.Text.Text
		if text == "" {
			continue
		}
		// 超出可展示预算时取消审批，不能让用户批准未展示的计划。
		if size+len(text) > 128*1024 {
			return "", false
		}
		parts = append(parts, text)
		size += len(text)
	}
	if len(parts) == 0 {
		return fallback, true
	}
	return strings.Join(parts, "\n\n"), true
}

// normalizeACPApprovalCommand 只提取并限制 ACP 标准命令字段，避免公开完整原始参数。
func normalizeACPApprovalCommand(rawInput any) string {
	input, ok := rawInput.(map[string]any)
	if !ok {
		return ""
	}
	command, ok := input["command"].(string)
	if !ok {
		return ""
	}
	command = strings.Join(strings.Fields(command), " ")
	runes := []rune(command)
	if len(runes) <= acpApprovalMaxCommandRunes {
		return command
	}
	return string(runes[:acpApprovalMaxCommandRunes-1]) + "…"
}
