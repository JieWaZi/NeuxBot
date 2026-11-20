package global

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// workflowResources 只读取创作请求的设备资源类别，返回可选择的公开摘要。
func workflowResources(ctx context.Context, kind string) ([]map[string]any, error) {
	switch kind {
	case "runtimes":
		environments, err := RuntimeEnvironmentControllerManager.List(ctx)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(environments))
		for _, environment := range environments {
			items = append(items, map[string]any{"id": environment.RuntimeID, "name": environment.Name, "enabled": environment.Enabled, "defaultAccountId": environment.DefaultAccountID, "defaultModel": environment.DefaultModel, "defaultReasoning": environment.DefaultReasoning, "defaultPermissionLevel": environment.DefaultPermissionLevel})
		}
		return items, nil
	case "accounts":
		accounts, err := AIAccountControllerManager.List(ctx)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(accounts))
		for _, account := range accounts {
			items = append(items, map[string]any{"id": account.ID, "runtimeId": account.RuntimeID, "name": account.Name, "authMode": account.AuthMode, "models": account.SelectedModels, "customModels": account.CustomModels, "defaultModel": account.DefaultModel, "defaultReasoning": account.DefaultReasoning, "reasoningLevels": account.ModelReasoningLevels})
		}
		return items, nil
	case "imTargets":
		targets, err := IMService.Targets(ctx, "", true)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(targets))
		for _, target := range targets {
			items = append(items, map[string]any{"id": target.ID, "name": target.Name, "platform": target.Platform, "kind": target.Kind})
		}
		return items, nil
	default:
		return nil, status_error.BadRequest
	}
}
