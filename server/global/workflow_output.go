package global

import (
	"context"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// repairWorkflowOutput 通过共享独立文本通道转换既有候选，不注入工作流业务能力或重新派发 worker。
func repairWorkflowOutput(ctx context.Context, defaults models.WorkflowExecutionDefaults, schema map[string]any, candidate string) (string, error) {
	encoded, err := json.Marshal(map[string]any{"schema": schema, "candidate": candidate})
	if err != nil {
		return "", status_error.WorkflowSchemaInvalid
	}
	return generateWorkflowText(ctx, defaults, "workflow_output_repair", agent.WorkflowOutputRepairInstructions, string(encoded))
}

func summarizeWorkflowOutput(ctx context.Context, defaults models.WorkflowExecutionDefaults, evidence string) (string, error) {
	return generateWorkflowText(ctx, defaults, "workflow_chat_summary", agent.WorkflowOutputSummaryInstructions, evidence)
}

func generateWorkflowText(ctx context.Context, defaults models.WorkflowExecutionDefaults, usageSource, system, evidence string) (string, error) {
	if AgentThreads == nil || AIAccountService == nil {
		return "", status_error.RuntimeUnavailable
	}
	selected, environment, err := detachedModelSelection(ctx, models.LibraryExecutionDefaults{
		RuntimeID: defaults.RuntimeID, AccountID: defaults.AccountID, Model: defaults.Model, Reasoning: defaults.Reasoning,
	})
	if err != nil {
		return "", err
	}
	result, err := AgentThreads.GenerateText(ctx, agent.TextRequest{
		Connection: detachedConnection(selected, environment), ResolveAccount: AIAccountService.ResolveAccount,
		Selection: &agent.TurnSelection{Reasoning: selected.Reasoning, PreferNoThinking: true}, UsageSource: usageSource,
		Messages: []agent.TextMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: evidence},
		}})
	if err != nil {
		return "", err
	}
	return result.Text, nil
}
