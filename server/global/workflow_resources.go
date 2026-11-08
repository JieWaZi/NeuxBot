package global

import "context"

// workflowResources 向创作提供设备真实智能体和账号摘要，不返回启动环境或凭据。
func workflowResources(ctx context.Context) (map[string]any, error) {
	environments, err := RuntimeEnvironmentControllerManager.List(ctx)
	if err != nil {
		return nil, err
	}
	runtimes := []map[string]any{}
	for _, environment := range environments {
		runtimes = append(runtimes, map[string]any{"id": environment.RuntimeID, "name": environment.Name, "enabled": environment.Enabled, "defaultAccountId": environment.DefaultAccountID, "defaultModel": environment.DefaultModel, "defaultReasoning": environment.DefaultReasoning, "defaultPermissionLevel": environment.DefaultPermissionLevel})
	}
	accounts, err := AIAccountControllerManager.List(ctx)
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for _, account := range accounts {
		items = append(items, map[string]any{"id": account.ID, "runtimeId": account.RuntimeID, "name": account.Name, "authMode": account.AuthMode, "models": account.SelectedModels, "customModels": account.CustomModels, "defaultModel": account.DefaultModel, "defaultReasoning": account.DefaultReasoning, "reasoningLevels": account.ModelReasoningLevels})
	}
	targets, err := IMService.Targets(ctx, "", true)
	if err != nil {
		return nil, err
	}
	return map[string]any{"runtimes": runtimes, "accounts": items, "imTargets": targets}, nil
}
