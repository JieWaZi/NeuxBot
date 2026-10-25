package messageview

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

const (
	// legacyWorkflowResultServer 仅识别已保存对话中的旧结果工具身份，不注册运行能力。
	legacyWorkflowResultServer = "ally_workflow_result"
	// legacyWorkflowUseServer 仅识别已保存对话中的旧调用工具身份，不注册运行能力。
	legacyWorkflowUseServer = "ally_workflow_use"
)

// builtinWorkflowToolPhrase 将内部协议操作映射为产品动作，不展示源码、参数和原始结果。
func builtinWorkflowToolPhrase(data map[string]interface{}, rawToolName string) string {
	server, tool := mcpServiceAndTool(data, rawToolName)
	if strings.EqualFold(server, legacyWorkflowResultServer) && strings.EqualFold(tool, "workflow_result") {
		return "提交步骤结果"
	}
	legacyConsumer := strings.EqualFold(server, legacyWorkflowUseServer) && strings.EqualFold(tool, "workflow_use")
	if !legacyConsumer && (!strings.EqualFold(server, constants.WorkflowMCPRuntimeKey) || !strings.EqualFold(tool, constants.WorkflowMCPToolName)) {
		return ""
	}
	switch strings.ToLower(firstNestedDisplayString(data, "action")) {
	case "sdk.get":
		return "了解工作流能力"
	case "resources.list":
		return "查看可用伙伴与资源"
	case "workflows.list":
		return "查看可用工作流"
	case constants.WorkflowActionQuery:
		switch strings.ToLower(firstNestedDisplayString(data, "type")) {
		case constants.WorkflowQueryResources:
			return "查看可用伙伴与资源"
		case constants.WorkflowQueryWorkflows:
			return "查看可用工作流"
		case constants.WorkflowQueryResultSchema:
			return "查看步骤结果格式"
		case constants.WorkflowQueryExamples:
			return "查看工作流示例"
		default:
			return "了解工作流能力"
		}
	case constants.WorkflowActionSourceGet:
		return "读取工作流"
	case constants.WorkflowActionSourceValidate:
		return "检查工作流"
	case constants.WorkflowActionSourceUpsert:
		return "保存工作流"
	case constants.WorkflowActionAnnotationsList:
		return "查看修改要求"
	case constants.WorkflowActionRunsGet:
		return "查看执行记录"
	case constants.WorkflowActionResultSubmit:
		return "提交步骤结果"
	case constants.WorkflowActionRunStart:
		return "运行工作流"
	case constants.WorkflowActionRunGet:
		return "查看执行进度"
	case constants.WorkflowActionRunCancel:
		return "停止工作流"
	default:
		return "编排工作流"
	}
}
