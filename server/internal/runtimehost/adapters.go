package runtimehost

import (
	"context"
	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp"
	"github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp/native"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
	acp "github.com/JieWaZi/acp-go/sdk"
	"log/slog"
	"path/filepath"
)

// runtimeDescriptor 只记录公开工厂之外的原生权限与回答证明差异。
type runtimeDescriptor struct {
	// completion 决定采用原生阶段还是公开 ACP 回合完成证明。
	completion acpdriver.CompletionPolicy
	// modes 是当前调用完全访问权限的原生候选，必须由实际会话目录确认。
	modes []string
	// singleModeFallback 仅兼容旧 CLI 唯一原生模式，不覆盖新版模式目录。
	singleModeFallback string
}

var runtimeDescriptors = map[string]runtimeDescriptor{
	runtimecontract.RuntimeIDCodex:     {completion: acpdriver.CompletionNativePhase, modes: []string{"agent-full-access"}},
	runtimecontract.RuntimeIDClaude:    {completion: acpdriver.CompletionLastAssistant, modes: []string{"bypassPermissions"}},
	runtimecontract.RuntimeIDKimi:      {completion: acpdriver.CompletionLastAssistant, modes: []string{"yolo"}, singleModeFallback: "default"},
	runtimecontract.RuntimeIDCursor:    {completion: acpdriver.CompletionLastAssistant, modes: []string{"agent"}},
	runtimecontract.RuntimeIDPi:        {completion: acpdriver.CompletionLastAssistant},
	runtimecontract.RuntimeIDOpenCode:  {completion: acpdriver.CompletionLastAssistant},
	runtimecontract.RuntimeIDGemini:    {completion: acpdriver.CompletionLastAssistant, modes: []string{"yolo"}},
	runtimecontract.RuntimeIDGrok:      {completion: acpdriver.CompletionLastAssistant},
	runtimecontract.RuntimeIDWorkBuddy: {completion: acpdriver.CompletionLastAssistant, modes: []string{"bypassPermissions"}},
	runtimecontract.RuntimeIDQoder:     {completion: acpdriver.CompletionLastAssistant, modes: []string{constants.RuntimeQoderYoloModeID, constants.RuntimeQoderBypassModeID}},
	runtimecontract.RuntimeIDQoderCN:   {completion: acpdriver.CompletionLastAssistant, modes: []string{constants.RuntimeQoderYoloModeID, constants.RuntimeQoderBypassModeID}},
}

func nativeModelID(base, account, model string) string {
	if account != "" && account != constants.AiAccountSystemID {
		return accountconfig.NativeModelID(base, model)
	}
	return model
}

// nativeAgent 只把已冻结的事实交给公开适配器，不复制私有协议或全局配置合并。
func (p *credentialProvider) nativeAgent(ctx context.Context, request c.OpenRequest, values map[string]string) (acp.Agent, error) {
	return p.nativeAgentWithPermission(ctx, request, values, runtimecontract.PermissionLevelFullAccess)
}

// nativeAgentWithPermission 冻结产品已经授权的权限，不在工厂扩张到完全访问。
func (p *credentialProvider) nativeAgentWithPermission(ctx context.Context, request c.OpenRequest, values map[string]string, level runtimecontract.PermissionLevel) (acp.Agent, error) {
	config := native.Config{
		RuntimeID: p.environment.BaseRuntimeID, Executable: p.launch.Executable,
		PrefixArgs: append([]string(nil), p.launch.PrefixArgs...), Environment: environmentList(values),
		WorkingDirectory: request.Workspace.Root, PermissionMode: string(level), Logger: slog.Default(),
	}
	// 产品文件与持久状态路径由宿主准备，公开工厂只消费冻结的启动事实。
	if p.environment.BaseRuntimeID == runtimecontract.RuntimeIDClaude {
		if p.target.ClaudeSettingsPath != "" {
			config.PrefixArgs = append(config.PrefixArgs, "--settings", p.target.ClaudeSettingsPath)
		}
		for _, directory := range p.target.NativePluginDirectories {
			config.PrefixArgs = append(config.PrefixArgs, "--plugin-dir", directory)
		}
	}
	switch p.environment.BaseRuntimeID {
	case runtimecontract.RuntimeIDKimi:
		config.StateDirectory = filepath.Join(p.workspace, "kimi-state")
	case runtimecontract.RuntimeIDCursor:
		config.StateDirectory = filepath.Join(p.workspace, "cursor-state")
		config.StateDirectoryForWorkspace = func(cwd string) string { return filepath.Join(filepath.Dir(cwd), "cursor-state") }
	case runtimecontract.RuntimeIDGemini:
		config.StateDirectory = values["GEMINI_CLI_HOME"]
	}
	return native.New(ctx, config)
}
