package messageview

import (
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

// toolProjectionState 只保留当前输入的安全展示事实，不驻留参数正文、工具输出或 Diff 原文。
type toolProjectionState struct {
	// fields 保留当前展示字段；字段省略表示沿用，显式 nil 或空值表示替换和清除。
	fields map[string]interface{}
	// contentSummary 是当前 content 集合派生的安全摘要；显式空集合清除先前摘要。
	contentSummary *ToolSafeSummary
	// commandID 引用已匹配的同执行 Core 命令事实，后续原生元数据不得覆盖其业务短语。
	commandID string
}

// toolProjectionKey 为一个 Runtime 调用内的工具建立无歧义的稳定展示身份。
func toolProjectionKey(invocationID, nativeID string) string {
	encoded, _ := json.Marshal([2]string{invocationID, nativeID})
	return "tool:" + string(encoded)
}

func (s *toolProjectionState) apply(data map[string]interface{}, scope workspacePathScope) {
	shape := make(map[string]interface{}, len(s.fields)+len(data))
	for key, value := range s.fields {
		shape[key] = value
	}
	for key, value := range data {
		shape[key] = value
	}
	allowFallback := toolUsesGenericParameterValue(shape)
	for key, value := range projectToolFacts(data, 0, allowFallback) {
		s.fields[key] = value
	}
	if _, present := data["content"]; present {
		s.contentSummary = buildToolSafeSummary(map[string]interface{}{"content": data["content"]}, scope)
	}
}

// projectionData 移除 ACP 从 rawInput 复制的展示别名，让最新输入替换事实拥有唯一来源。
func (s *toolProjectionState) projectionData() map[string]interface{} {
	data := make(map[string]interface{}, len(s.fields))
	for key, value := range s.fields {
		data[key] = value
	}
	if _, present := data["rawInput"]; present {
		for _, key := range []string{"input", "server", "tool", "arguments", "command", "cwd", "path", "query", "action"} {
			delete(data, key)
		}
	}
	if firstNestedDisplayString(data, "file_path", "filePath", "path") == "" {
		locations, _ := data["locations"].([]interface{})
		if len(locations) > 0 {
			location, _ := locations[0].(map[string]interface{})
			data["path"] = firstDisplayString(location, "path")
		}
	}
	return data
}

// projectToolFacts 仅抽取现有安全短语与摘要消费的字段；输入正文和输出不进入当前状态。
func projectToolFacts(data map[string]interface{}, depth int, allowFallback bool) map[string]interface{} {
	projected := make(map[string]interface{})
	if depth > 3 {
		return projected
	}
	for key, value := range data {
		switch key {
		case "input", "rawInput", "args", "arguments", "action":
			if object, ok := value.(map[string]interface{}); ok {
				projected[key] = projectToolFacts(object, depth+1, allowFallback)
			} else if key == "action" {
				if text, ok := value.(string); ok {
					projected[key] = truncateToolPhrase(text, 80)
				} else {
					projected[key] = nil
				}
			} else {
				projected[key] = nil
			}
		case "_meta":
			if object, ok := value.(map[string]interface{}); ok {
				isMCP, _ := object["is_mcp_tool_call"].(bool)
				projected[key] = map[string]interface{}{"is_mcp_tool_call": isMCP}
			} else {
				projected[key] = nil
			}
		case "locations":
			items, _ := value.([]interface{})
			safe := make([]interface{}, 0, 1)
			if len(items) > 0 {
				if object, ok := items[0].(map[string]interface{}); ok {
					safe = append(safe, map[string]interface{}{"path": firstDisplayString(object, "path")})
				}
			}
			projected[key] = safe
		case "commandActions":
			items, _ := value.([]interface{})
			actionType, path := "", ""
			for _, item := range items {
				object, _ := item.(map[string]interface{})
				if path == "" {
					path = firstDisplayString(object, "path")
				}
				if actionType == "" {
					if kind := firstDisplayString(object, "type"); kind != "unknown" {
						actionType = kind
					}
				}
				if path != "" && actionType != "" {
					break
				}
			}
			safe := make([]interface{}, 0, 1)
			if path != "" || actionType != "" {
				safe = append(safe, map[string]interface{}{"type": actionType, "path": path})
			}
			projected[key] = safe

		case "questions":
			items, _ := value.([]interface{})
			safe := make([]interface{}, 0, 1)
			for _, item := range items {
				object, _ := item.(map[string]interface{})
				if question := strings.Join(strings.Fields(firstDisplayString(object, "question")), " "); question != "" {
					safe = append(safe, map[string]interface{}{"question": truncateToolPhrase(question, 80)})
					break
				}
			}
			projected[key] = safe
		case "todos":
			if items, ok := value.([]interface{}); ok {
				projected["displayTodoCount"] = len(items)
			} else {
				projected["displayTodoCount"] = nil
			}
		case "queries":
			items, _ := value.([]interface{})
			safe := make([]interface{}, 0, 1)
			for _, item := range items {
				if text, ok := item.(string); ok && text != "" {
					safe = append(safe, truncateToolPhrase(text, 80))
					break
				}
			}
			projected[key] = safe
		case "command":
			if text, ok := value.(string); ok {
				projected[key] = truncateToolPhrase(text, 80)
				projected["displayCommandAction"] = string(displayCommandAction(text))
			} else {
				projected[key] = nil
				projected["displayCommandAction"] = nil
			}
		case "toolCallId", "name", "toolName", "tool_name", "type", "kind", "title", "server", "tool", "providerIdentifier", "skill", "taskId", "task_id":
			if text, ok := value.(string); ok {
				projected[key] = truncateToolPhrase(text, constants.MessageToolSafeSummaryMaxRunes)
			} else {
				projected[key] = nil
			}
		case "description", "prompt", "subject", "query", "pattern", "glob":
			if text, ok := value.(string); ok {
				projected[key] = truncateToolPhrase(text, 80)
			} else {
				projected[key] = nil
			}
		case "path", "file_path", "filePath", "url":
			// 路径和 URL 的安全范围由既有摘要校验器检查；这些标量不包含工具正文。
			if text, ok := value.(string); ok {
				projected[key] = text
			} else {
				projected[key] = nil
			}
		case "offset", "limit", "added_lines", "addedLines", "deleted_lines", "deletedLines", "removed_lines", "removedLines":
			switch value.(type) {
			case float64, int, int64:
				projected[key] = value
			default:
				projected[key] = nil
			}
		}
	}
	// 未识别工具仍使用既有的短参数回退，但只保留已选出的展示值。
	if allowFallback && depth > 0 && len(projected) == 0 {
		if value := firstMeaningfulToolValue(data); value != "" {
			projected["displayFallbackValue"] = truncateToolPhrase(value, 80)
		}
	}
	return projected
}

// toolUsesGenericParameterValue 只为现有短语确实消费的通用参数保留一个有界展示值。
func toolUsesGenericParameterValue(data map[string]interface{}) bool {
	name := displayToolName(data)
	if userInputToolPhrase(data, name) != "" || builtinIMToolPhrase(data, name) != "" || builtinMemoryToolPhrase(data, name) != "" || builtinWorkflowToolPhrase(data, name) != "" || builtinChannelTasksToolPhrase(data, name) != "" {
		return false
	}
	switch strings.ToLower(name) {
	case "task", "agent", "taskcreate", "taskget", "tasklist", "todowrite", "todoread", "enterplanmode", "exitplanmode", "generate_image":
		return false
	}
	action := displayToolAction(data)
	return action == constants.MessageToolActionOther || action == constants.MessageToolActionCallService
}
