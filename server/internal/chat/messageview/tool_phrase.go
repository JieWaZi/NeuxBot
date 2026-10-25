package messageview

import (
	"fmt"
	pathpkg "path"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

// buildToolPhrase 将不同 Runtime 的工具结构归一为不泄露结果正文的完成态和加载态短语。
func buildToolPhrase(data map[string]interface{}, scope workspacePathScope) (string, string) {
	rawToolName := displayToolName(data)
	toolName := strings.ToLower(rawToolName)
	action := displayToolAction(data)
	pathName := displayToolPathName(data, scope)

	if label := userInputToolPhrase(data, rawToolName); label != "" {
		return completedAndLoadingPhrase(label)
	}

	switch toolName {
	case "read", "readfile", "read_file", "view", "open_file":
		label := "读取文件"
		if pathName != "" {
			label = "读取 " + pathName
			if offset, ok := optionalInt(data, "offset"); ok {
				if limit, hasLimit := optionalInt(data, "limit"); hasLimit {
					label += fmt.Sprintf(" 第 %d–%d 行", offset, offset+limit)
				} else {
					label += fmt.Sprintf(" 从第 %d 行", offset)
				}
			}
		}
		return completedAndLoadingPhrase(label)
	case "write":
		return completedAndLoadingPhrase(toolPhraseWithTarget("写入文件", "写入", pathName))
	case "edit", "multiedit", "notebookedit", "file_change", "file_modify", "apply_patch":
		return completedAndLoadingPhrase(toolPhraseWithTarget("修改文件", "修改", pathName))
	case "glob", "list", "list_files":
		pattern := firstNestedDisplayString(data, "pattern")
		if pattern != "" {
			label := "搜索文件 " + truncateToolPhrase(pattern, 80)
			if searchPath := firstNestedDisplayString(data, "path"); searchPath != "" {
				label += " in " + truncateToolPhrase(searchPath, 60)
			}
			return completedAndLoadingPhrase(label)
		}
		return completedAndLoadingPhrase(toolPhraseWithTarget("浏览文件", "浏览", pathName))
	case "grep", "search", "find", "file_search":
		query := firstNestedDisplayString(data, "pattern", "query")
		if query != "" {
			label := "搜索内容 /" + truncateToolPhrase(query, 60) + "/"
			if target := firstNestedDisplayString(data, "glob", "path"); target != "" {
				label += " in " + truncateToolPhrase(target, 60)
			}
			return completedAndLoadingPhrase(label)
		}
		return completedAndLoadingPhrase("搜索内容")
	case "webfetch", "web_fetch":
		rawURL := firstNestedDisplayString(data, "url")
		if rawURL != "" {
			return completedAndLoadingPhrase("抓取 " + truncateToolPhrase(rawURL, 60))
		}
		return completedAndLoadingPhrase("抓取网页")
	case "websearch", "web_search", "network":
		if query := webSearchPhraseQuery(data); query != "" {
			return completedAndLoadingPhrase("搜索网页 \"" + truncateToolPhrase(query, 60) + "\"")
		}
		if label := webSearchPhraseAction(data); label != "" {
			return completedAndLoadingPhrase(label)
		}
		return completedAndLoadingPhrase("搜索网页")
	case "skill":
		return completedAndLoadingPhrase(toolPhraseWithTarget("使用技能", "使用技能", firstNestedDisplayString(data, "skill")))
	case "task":
		return completedAndLoadingPhrase(toolPhraseWithTarget("子任务", "子任务", truncateToolPhrase(firstNestedDisplayString(data, "description", "prompt"), 80)))
	case "agent":
		name := firstNestedDisplayString(data, "name")
		description := truncateToolPhrase(firstNestedDisplayString(data, "description", "prompt"), 60)
		label := "Agent"
		if name != "" {
			label += " " + name
		}
		if description != "" {
			label += " · " + description
		}
		return completedAndLoadingPhrase(label)
	case "taskcreate":
		return completedAndLoadingPhrase(toolPhraseWithTarget("创建任务", "创建任务", truncateToolPhrase(firstNestedDisplayString(data, "subject"), 80)))
	case "taskget":
		taskID := firstNestedDisplayString(data, "taskId", "task_id")
		if taskID != "" {
			return completedAndLoadingPhrase("查看任务 #" + taskID)
		}
		return completedAndLoadingPhrase("查看任务")
	case "tasklist":
		return completedAndLoadingPhrase("查看任务列表")
	case "todowrite":
		if todos, ok := firstNestedDisplayValue(data, "todos").([]interface{}); ok {
			return completedAndLoadingPhrase(fmt.Sprintf("更新待办 %d 项", len(todos)))
		}
		return completedAndLoadingPhrase("更新待办")
	case "todoread":
		return completedAndLoadingPhrase("读取待办")
	case "enterplanmode":
		return completedAndLoadingPhrase("进入计划模式")
	case "exitplanmode":
		return completedAndLoadingPhrase("退出计划模式")
	case "generate_image":
		return completedAndLoadingPhrase(toolPhraseWithTarget("生成图片", "生成图片", truncateToolPhrase(firstNestedDisplayString(data, "prompt"), 60)))
	}

	if action == constants.MessageToolActionCallService {
		startupServer, startupTool := mcpServiceAndTool(data, rawToolName)
		if startupTool == "startup" && strings.HasPrefix(firstDisplayString(data, "toolCallId"), "mcp_startup.") {
			return completedAndLoadingPhrase("连接 MCP 服务 " + truncateToolPhrase(startupServer, 60))
		}
		if label := builtinIMToolPhrase(data, rawToolName); label != "" {
			return completedAndLoadingPhrase(label)
		}
		if label := builtinWorkflowToolPhrase(data, rawToolName); label != "" {
			return completedAndLoadingPhrase(label)
		}
		if label := builtinChannelTasksToolPhrase(data, rawToolName); label != "" {
			return completedAndLoadingPhrase(label)
		}
		if label := builtinMemoryToolPhrase(data, rawToolName); label != "" {
			return completedAndLoadingPhrase(label)
		}
		if target := mcpToolPhraseTarget(rawToolName); target != "" {
			if summary := firstMeaningfulToolValue(data); summary != "" {
				target += " " + truncateToolPhrase(summary, 60)
			}
			return completedAndLoadingPhrase(target)
		}
		service := firstNestedDisplayString(data, "server")
		tool := firstNestedDisplayString(data, "tool")
		target := strings.Trim(strings.Join([]string{service, tool}, " · "), " ·")
		return completedAndLoadingPhrase(toolPhraseWithTarget("调用外部服务", "调用", truncateToolPhrase(target, 60)))
	}
	isCommand := toolName == "bash" || toolName == "shell" || toolName == "command" || toolName == "command_execution" || toolName == "exec"
	if isCommand || action == constants.MessageToolActionExecuteCommand {
		if description := descriptionOfCommand(data); description != "" {
			return completedAndLoadingPhrase(description)
		}
	}
	if isCommand && action != constants.MessageToolActionExecuteCommand {
		label := toolActionLabel(action)
		if pathName != "" {
			label += " " + pathName
		} else if command := firstNestedDisplayString(data, "command"); command != "" {
			label += " · " + truncateToolPhrase(command, 80)
		}
		return completedAndLoadingPhrase(label)
	}
	if action == constants.MessageToolActionExecuteCommand || isCommand {
		if command := firstNestedDisplayString(data, "command"); command != "" {
			return completedAndLoadingPhrase("执行 " + truncateToolPhrase(command, 80))
		}
	}

	label := toolActionLabel(action)
	if action == constants.MessageToolActionOther && rawToolName != "" && rawToolName != "unknown" {
		label = rawToolName
		if summary := firstMeaningfulToolValue(data); summary != "" {
			label += " " + truncateToolPhrase(summary, 60)
		}
	}
	if pathName != "" && action != constants.MessageToolActionExecuteCommand {
		label += " " + pathName
	}
	return completedAndLoadingPhrase(label)
}

// userInputToolPhrase 将受管及原生问答映射为提问动作，只附带有界的问题摘要。
func userInputToolPhrase(data map[string]interface{}, rawToolName string) string {
	server, tool := mcpServiceAndTool(data, rawToolName)
	managed := strings.EqualFold(server, constants.RuntimeUserInputMCPServerName) &&
		strings.EqualFold(tool, constants.RuntimeUserInputToolName)
	native := server == "" && strings.EqualFold(rawToolName, constants.RuntimeUserInputToolName)
	if !managed && !native {
		return ""
	}

	label := "向你提问"
	questions, _ := firstNestedDisplayValue(data, "questions").([]interface{})
	for _, rawQuestion := range questions {
		question, _ := rawQuestion.(map[string]interface{})
		text := strings.Join(strings.Fields(firstDisplayString(question, "question")), " ")
		if text != "" {
			return label + " · " + truncateToolPhrase(text, 80)
		}
	}
	return label
}

// builtinIMToolPhrase 将内置 IM 协议映射为交付动作，不展示接收目标、正文和原始回执。
func builtinIMToolPhrase(data map[string]interface{}, rawToolName string) string {
	server, tool := mcpServiceAndTool(data, rawToolName)
	if !strings.EqualFold(server, constants.IMBuiltinRuntimeKey) ||
		!strings.EqualFold(tool, constants.IMBuiltinToolName) {
		return ""
	}
	switch strings.ToLower(firstNestedDisplayString(data, "action")) {
	case "targets.list":
		return "查看可发送聊天"
	case "deliveries.create":
		return "提交消息交付"
	case "deliveries.get":
		return "查看交付状态"
	default:
		return "管理消息交付"
	}
}

// builtinChannelTasksToolPhrase 将内置团队任务协议标识转换为面向用户的团队动作。
func builtinChannelTasksToolPhrase(data map[string]interface{}, rawToolName string) string {
	server, tool := mcpServiceAndTool(data, rawToolName)
	if !strings.EqualFold(server, constants.BuiltinChannelTasksMCPConnectorRuntimeKey) ||
		!strings.EqualFold(tool, "channel_tasks") {
		return ""
	}
	switch strings.ToLower(firstNestedDisplayString(data, "action")) {
	case "list":
		return "查看团队任务"
	case "search":
		return "搜索团队任务"
	case "get":
		return "查看任务详情"
	case "create":
		return "分配团队任务"
	default:
		return "管理团队任务"
	}
}

// builtinMemoryToolPhrase 将内置记忆的协议标识转换为面向用户的产品动作。
func builtinMemoryToolPhrase(data map[string]interface{}, rawToolName string) string {
	server, tool := mcpServiceAndTool(data, rawToolName)
	if !strings.EqualFold(server, constants.BuiltinMemoryMCPConnectorRuntimeKey) {
		return ""
	}
	switch strings.ToLower(tool) {
	case "memory_search":
		return "搜索记忆"
	case "memory_write":
		return "保存记忆"
	case "memory_update":
		return "更新记忆"
	case "memory_forget":
		return "遗忘记忆"
	default:
		return ""
	}
}

// mcpServiceAndTool 从标准字段或 Runtime 原生名称解析服务与工具身份。
func mcpServiceAndTool(data map[string]interface{}, rawToolName string) (string, string) {
	server := firstNestedDisplayString(data, "server", "providerIdentifier")
	tool := firstNestedDisplayString(data, "toolName")
	if tool == "" {
		tool = firstNestedDisplayString(data, "tool")
	}
	if parts := strings.Split(rawToolName, "__"); len(parts) >= 3 && strings.EqualFold(parts[0], "mcp") {
		if server == "" {
			server = parts[1]
		}
		if tool == "" {
			tool = strings.Join(parts[2:], "__")
		}
	}
	if server == "" {
		for _, candidate := range []string{tool, rawToolName} {
			if builtinServer, builtinTool := flattenedBuiltinMCPIdentity(candidate); builtinServer != "" {
				return builtinServer, builtinTool
			}
		}
	}
	return server, tool
}

// flattenedBuiltinMCPIdentity 兼容 Runtime 将内置服务和工具压成单个下划线名称的形态。
func flattenedBuiltinMCPIdentity(name string) (string, string) {
	for _, server := range []string{
		legacyWorkflowResultServer,
		legacyWorkflowUseServer,
		constants.WorkflowMCPRuntimeKey,
		constants.BuiltinMemoryMCPConnectorRuntimeKey,
		constants.BuiltinChannelTasksMCPConnectorRuntimeKey,
		constants.IMBuiltinRuntimeKey,
		constants.RuntimeUserInputMCPServerName,
	} {
		prefix := server + "_"
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			return server, name[len(prefix):]
		}
	}
	return "", ""
}

// descriptionOfCommand 返回命令工具携带的意图描述，用于替代裸命令展示。
func descriptionOfCommand(data map[string]interface{}) string {
	description := firstNestedDisplayString(data, "description")
	if description == "" {
		return ""
	}
	return truncateToolPhrase(description, 80)
}

// webSearchPhraseQuery 按已知 Runtime 结构提取网页搜索词。
func webSearchPhraseQuery(data map[string]interface{}) string {
	if query := firstNestedDisplayString(data, "query"); query != "" {
		return query
	}
	action := webSearchActionObject(data)
	if action == nil {
		return ""
	}
	if query := firstDisplayString(action, "query"); query != "" {
		return query
	}
	if queries, ok := action["queries"].([]interface{}); ok {
		for _, rawQuery := range queries {
			if query, ok := rawQuery.(string); ok && query != "" {
				return query
			}
		}
	}
	return ""
}

// webSearchPhraseAction 根据网页搜索动作类型生成浏览或页内查找短语。
func webSearchPhraseAction(data map[string]interface{}) string {
	action := webSearchActionObject(data)
	if action == nil {
		return ""
	}
	switch strings.ToLower(firstDisplayString(action, "type")) {
	case "openpage":
		rawURL := firstDisplayString(action, "url")
		if rawURL == "" {
			return ""
		}
		if domain, ok := safeURLDomain(rawURL); ok {
			return "浏览页面 " + domain
		}
		return "浏览页面 " + truncateToolPhrase(rawURL, 60)
	case "findinpage":
		pattern := firstDisplayString(action, "pattern")
		if pattern == "" {
			return ""
		}
		return "页面内查找 \"" + truncateToolPhrase(pattern, 60) + "\""
	}
	return ""
}

// webSearchActionObject 返回网页搜索动作对象，忽略字符串形式的动作值。
func webSearchActionObject(data map[string]interface{}) map[string]interface{} {
	for _, candidate := range nestedToolObjects(data) {
		if candidate == nil {
			continue
		}
		if action, ok := candidate["action"].(map[string]interface{}); ok {
			return action
		}
	}
	return nil
}

// mcpToolPhraseTarget 将 mcp__server__tool 名称转换为可读服务目标。
func mcpToolPhraseTarget(toolName string) string {
	parts := strings.Split(toolName, "__")
	if len(parts) < 3 || !strings.EqualFold(parts[0], "mcp") || parts[1] == "" {
		return ""
	}
	return strings.ToUpper(parts[1]) + " / " + strings.Join(parts[2:], "_")
}

// firstMeaningfulToolValue 按稳定优先级选取适合公开展示的短参数值。
func firstMeaningfulToolValue(data map[string]interface{}) string {
	for _, key := range []string{"description", "prompt", "query", "command", "name", "subject", "path", "file_path", "url"} {
		if value := firstNestedDisplayString(data, key); value != "" {
			if key == "name" && value == displayToolName(data) {
				continue
			}
			return value
		}
	}
	for _, candidate := range []map[string]interface{}{
		nestedObject(data, "input"),
		nestedObject(data, "rawInput"),
		nestedObject(data, "args"),
		nestedObject(data, "arguments"),
		data,
	} {
		if candidate == nil {
			continue
		}
		keys := make([]string, 0, len(candidate))
		for key := range candidate {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value, ok := candidate[key].(string)
			if !strings.HasPrefix(key, "_") && ok && value != "" && len([]rune(value)) < 200 {
				return value
			}
		}
	}
	return ""
}

// firstNestedDisplayValue 兼容顶层、input、args 和 arguments 四种 Runtime 载荷结构。
func firstNestedDisplayValue(data map[string]interface{}, key string) interface{} {
	for _, candidate := range nestedToolObjects(data) {
		if candidate == nil {
			continue
		}
		if value, ok := candidate[key]; ok {
			return value
		}
	}
	return nil
}

// nestedToolObjects 按跨 Runtime 优先级返回工具输入候选对象。
func nestedToolObjects(data map[string]interface{}) []map[string]interface{} {
	rawInput := nestedObject(data, "rawInput")
	return []map[string]interface{}{
		data,
		nestedObject(data, "input"),
		nestedObject(data, "rawInput"),
		nestedObject(data, "args"),
		nestedObject(data, "arguments"),
		nestedObject(rawInput, "args"),
		nestedObject(rawInput, "arguments"),
	}
}

// toolActionLabel 将稳定工具动作码映射为中文动词短语。
func toolActionLabel(action constants.MessageToolAction) string {
	switch action {
	case constants.MessageToolActionReadFile:
		return "读取文件"
	case constants.MessageToolActionListFiles:
		return "浏览文件"
	case constants.MessageToolActionSearchFiles:
		return "搜索内容"
	case constants.MessageToolActionWriteFile:
		return "写入文件"
	case constants.MessageToolActionModifyFile:
		return "修改文件"
	case constants.MessageToolActionRunTests:
		return "运行测试"
	case constants.MessageToolActionCheckProject:
		return "检查项目"
	case constants.MessageToolActionBuildProject:
		return "构建项目"
	case constants.MessageToolActionFormatCode:
		return "格式化代码"
	case constants.MessageToolActionInspectGit:
		return "检查 Git 变更"
	case constants.MessageToolActionFetchWeb:
		return "抓取网页"
	case constants.MessageToolActionSearchWeb:
		return "搜索网页"
	case constants.MessageToolActionCallService:
		return "调用外部服务"
	case constants.MessageToolActionUseSkill:
		return "使用技能"
	case constants.MessageToolActionViewImage:
		return "查看图片"
	case constants.MessageToolActionExecuteCommand:
		return "执行命令"
	default:
		return "执行操作"
	}
}

// completedAndLoadingPhrase 从同一动作短语生成完成态与进行态文案。
func completedAndLoadingPhrase(label string) (string, string) {
	return label, "正在" + label + "…"
}

// mergeToolPhrase 单调合并工具短语，bare update 不得降级 richer 标签。
func mergeToolPhrase(existingLabel, existingLoading, incomingLabel, incomingLoading string) (string, string) {
	if incomingLabel == "" {
		return existingLabel, existingLoading
	}
	if existingLabel == "" {
		return incomingLabel, incomingLoading
	}
	genericMCPLabel := toolActionLabel(constants.MessageToolActionCallService)
	if existingLabel == genericMCPLabel && incomingLabel != genericMCPLabel {
		return incomingLabel, incomingLoading
	}
	if incomingLabel == genericMCPLabel && existingLabel != genericMCPLabel {
		return existingLabel, existingLoading
	}
	if incomingLabel == toolActionLabel(constants.MessageToolActionOther) &&
		existingLabel != incomingLabel {
		return existingLabel, existingLoading
	}
	if len([]rune(incomingLabel)) > len([]rune(existingLabel)) {
		return incomingLabel, incomingLoading
	}
	return existingLabel, existingLoading
}

// toolPhraseWithTarget 在缺少安全目标时退回不带参数的动作短语。
func toolPhraseWithTarget(fallback string, verb string, target string) string {
	if target == "" {
		return fallback
	}
	return verb + " " + target
}

// displayToolPathName 只展示项目相对路径或文件名，避免泄露绝对路径。
func displayToolPathName(data map[string]interface{}, scope workspacePathScope) string {
	rawPath := firstNestedDisplayString(data, "file_path", "filePath", "path")
	if rawPath == "" {
		rawPath = firstStructuredToolPath(data)
	}
	if rawPath == "" {
		return ""
	}
	if relative, ok := scope.safeRelativePath(rawPath); ok {
		return pathpkg.Base(relative)
	}
	normalized := strings.ReplaceAll(rawPath, "\\", "/")
	name := pathpkg.Base(normalized)
	if name == "." || name == "/" {
		return ""
	}
	return truncateToolPhrase(name, 80)
}

// truncateToolPhrase 按 Unicode 字符数限制工具短语长度。
func truncateToolPhrase(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "…"
}
