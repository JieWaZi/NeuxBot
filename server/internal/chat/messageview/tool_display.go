package messageview

import (
	"fmt"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func firstDisplayString(data map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := data[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func displayToolID(data map[string]interface{}, event MessageRuntimeEvent) string {
	if id := firstDisplayString(
		data,
		"id",
		"tool_use_id",
		"toolCallId",
		"tool_call_id",
		"call_id",
	); id != "" {
		return id
	}
	return fmt.Sprintf("%s:%d", event.InvocationID, event.Sequence)
}

func displayToolName(data map[string]interface{}) string {
	name := firstDisplayString(data, "name", "toolName", "tool_name")
	if title := strings.ToLower(firstDisplayString(data, "title")); strings.HasPrefix(title, "load skill") &&
		firstNestedDisplayString(data, "skill") != "" &&
		(name == "" || name == "unknown" || strings.HasPrefix(strings.ToLower(name), "load skill")) {
		return "skill"
	}
	if name != "" {
		return name
	}
	if eventType := firstDisplayString(data, "type"); eventType != "" {
		return eventType
	}
	return "unknown"
}

func displayToolCategory(toolName string) constants.MessageToolCategory {
	normalized := strings.ToLower(toolName)
	if strings.HasPrefix(normalized, "mcp__") || strings.Contains(normalized, "mcp") ||
		strings.Contains(normalized, "external_service") {
		return constants.MessageToolCategoryMCP
	}
	switch normalized {
	case "read", "readfile", "read_file", "view", "open_file":
		return constants.MessageToolCategoryFileRead
	case "glob", "grep", "search", "find", "file_search":
		return constants.MessageToolCategoryFileSearch
	case "write", "edit", "multiedit", "notebookedit", "file_change",
		"file_modify", "apply_patch":
		return constants.MessageToolCategoryFileModify
	case "bash", "shell", "command", "command_execution", "exec":
		return constants.MessageToolCategoryCommand
	case "webfetch", "websearch", "web_fetch", "web_search", "network":
		return constants.MessageToolCategoryNetwork
	default:
		return constants.MessageToolCategoryOther
	}
}

func displayToolCategoryForData(data map[string]interface{}) constants.MessageToolCategory {
	switch displayToolAction(data) {
	case constants.MessageToolActionReadFile, constants.MessageToolActionViewImage:
		return constants.MessageToolCategoryFileRead
	case constants.MessageToolActionListFiles, constants.MessageToolActionSearchFiles:
		return constants.MessageToolCategoryFileSearch
	case constants.MessageToolActionWriteFile, constants.MessageToolActionModifyFile:
		return constants.MessageToolCategoryFileModify
	case constants.MessageToolActionFetchWeb, constants.MessageToolActionSearchWeb:
		return constants.MessageToolCategoryNetwork
	case constants.MessageToolActionCallService:
		return constants.MessageToolCategoryMCP
	case constants.MessageToolActionUseSkill:
		return constants.MessageToolCategorySkill
	case constants.MessageToolActionRunTests,
		constants.MessageToolActionCheckProject,
		constants.MessageToolActionBuildProject,
		constants.MessageToolActionFormatCode,
		constants.MessageToolActionInspectGit,
		constants.MessageToolActionExecuteCommand:
		return constants.MessageToolCategoryCommand
	default:
		return displayToolCategory(displayToolName(data))
	}
}

func displayToolAction(data map[string]interface{}) constants.MessageToolAction {
	if isMCPToolCall(data) {
		return constants.MessageToolActionCallService
	}
	itemType := strings.ToLower(firstNestedDisplayString(data, "type"))
	if itemType == "imageview" {
		return constants.MessageToolActionViewImage
	}
	if itemType == "fileschange" || itemType == "filechange" {
		return constants.MessageToolActionModifyFile
	}
	if itemType == "websearch" {
		return constants.MessageToolActionSearchWeb
	}
	kind := strings.ToLower(firstDisplayString(data, "kind"))
	title := strings.ToLower(firstDisplayString(data, "title"))
	if kind == "read" && strings.HasPrefix(title, "view image") {
		return constants.MessageToolActionViewImage
	}
	if action := firstCommandActionType(data); action != "" {
		switch action {
		case "read":
			return constants.MessageToolActionReadFile
		case "listfiles":
			return constants.MessageToolActionListFiles
		case "search":
			return constants.MessageToolActionSearchFiles
		}
	}
	normalized := strings.ToLower(displayToolName(data))
	switch normalized {
	case "read", "readfile", "read_file", "view", "open_file":
		return constants.MessageToolActionReadFile
	case "glob", "list", "list_files":
		return constants.MessageToolActionListFiles
	case "grep", "search", "find", "file_search":
		return constants.MessageToolActionSearchFiles
	case "write":
		return constants.MessageToolActionWriteFile
	case "edit", "multiedit", "notebookedit", "file_change", "file_modify", "apply_patch":
		return constants.MessageToolActionModifyFile
	case "webfetch", "web_fetch":
		return constants.MessageToolActionFetchWeb
	case "websearch", "web_search", "network":
		return constants.MessageToolActionSearchWeb
	case "mcp", "external_service":
		return constants.MessageToolActionCallService
	case "skill":
		return constants.MessageToolActionUseSkill
	case "bash", "shell", "command", "command_execution", "exec":
		if action := firstNestedDisplayString(data, "displayCommandAction"); action != "" {
			return constants.MessageToolAction(action)
		}
		return displayCommandAction(firstNestedDisplayString(data, "command"))
	default:
		if strings.HasPrefix(normalized, "mcp__") || strings.Contains(normalized, "mcp") {
			return constants.MessageToolActionCallService
		}
		return constants.MessageToolActionOther
	}
}

// isMCPToolCall 判断标准 ACP 元数据或 Runtime 原生字段是否表达 MCP 调用。
func isMCPToolCall(data map[string]interface{}) bool {
	meta := nestedObject(data, "_meta")
	if meta != nil && meta["is_mcp_tool_call"] == true {
		return true
	}
	server, tool := mcpServiceAndTool(data, displayToolName(data))
	return server != "" && tool != ""
}

func firstCommandActionType(data map[string]interface{}) string {
	actions, _ := data["commandActions"].([]interface{})
	for _, rawAction := range actions {
		action, _ := rawAction.(map[string]interface{})
		if actionType := strings.ToLower(firstDisplayString(action, "type")); actionType != "" && actionType != "unknown" {
			return actionType
		}
	}
	return ""
}

func displayCommandAction(command string) constants.MessageToolAction {
	normalized := strings.ToLower(command)
	switch {
	case containsAny(normalized, "go test", "pnpm test", "npm test", "bun test", "cargo test", "pytest", "vitest", "jest"):
		return constants.MessageToolActionRunTests
	case containsAny(normalized, "go vet", "pnpm check", "api:check", "typecheck", "tsc ", "eslint", "golangci-lint"):
		return constants.MessageToolActionCheckProject
	case containsAny(normalized, "pnpm build", "npm run build", "bun run build", "go build", "cargo build", "vite build"):
		return constants.MessageToolActionBuildProject
	case containsAny(normalized, "gofmt", "prettier", "cargo fmt", "goimports"):
		return constants.MessageToolActionFormatCode
	case containsAny(normalized, "git status", "git diff", "git log", "git show"):
		return constants.MessageToolActionInspectGit
	case containsAny(normalized, " rg ", "grep ", "find "):
		return constants.MessageToolActionSearchFiles
	case containsAny(normalized, " sed ", "cat ", "head ", "tail "):
		return constants.MessageToolActionReadFile
	default:
		return constants.MessageToolActionExecuteCommand
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func displayToolResultStatus(data map[string]interface{}) constants.MessageToolStatus {
	if data["is_error"] == true || data["isError"] == true ||
		data["status"] == "failed" || data["status"] == "error" {
		return constants.MessageToolStatusFailed
	}
	// 原生 ACP 可以把命令结束报告为 completed；失败依据结构化退出码，不能依赖输出正文。
	rawOutput := nestedObject(data, "rawOutput")
	for _, result := range []map[string]interface{}{
		data, rawOutput, nestedObject(rawOutput, "metadata"),
		nestedObject(nestedObject(data, "_meta"), "terminal_exit"),
	} {
		if exitCode, ok := optionalInt(result, "exit_code", "exitCode", "exit"); ok && exitCode != 0 {
			return constants.MessageToolStatusFailed
		}
	}
	return constants.MessageToolStatusCompleted
}
