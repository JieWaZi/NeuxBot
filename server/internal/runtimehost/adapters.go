package runtimehost

import (
	"context"
	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	acpdriver "github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	acpclaude "github.com/JieWaZi/acp-go/pkg/claude"
	acpcursor "github.com/JieWaZi/acp-go/pkg/cursor"
	acpgemini "github.com/JieWaZi/acp-go/pkg/gemini"
	acpgrok "github.com/JieWaZi/acp-go/pkg/grok"
	acpkimi "github.com/JieWaZi/acp-go/pkg/kimi"
	"github.com/JieWaZi/acp-go/pkg/nativeacp"
	acpopencode "github.com/JieWaZi/acp-go/pkg/opencode"
	acppi "github.com/JieWaZi/acp-go/pkg/pi"
	acpworkbuddy "github.com/JieWaZi/acp-go/pkg/workbuddy"
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
	runtimes.RuntimeIDCodex:     {completion: acpdriver.CompletionNativePhase, modes: []string{"agent-full-access"}},
	runtimes.RuntimeIDClaude:    {completion: acpdriver.CompletionLastAssistant, modes: []string{"bypassPermissions"}},
	runtimes.RuntimeIDKimi:      {completion: acpdriver.CompletionLastAssistant, modes: []string{"yolo"}, singleModeFallback: "default"},
	runtimes.RuntimeIDCursor:    {completion: acpdriver.CompletionLastAssistant, modes: []string{"agent"}},
	runtimes.RuntimeIDPi:        {completion: acpdriver.CompletionLastAssistant},
	runtimes.RuntimeIDOpenCode:  {completion: acpdriver.CompletionLastAssistant},
	runtimes.RuntimeIDGemini:    {completion: acpdriver.CompletionLastAssistant, modes: []string{"yolo"}},
	runtimes.RuntimeIDGrok:      {completion: acpdriver.CompletionLastAssistant},
	runtimes.RuntimeIDWorkBuddy: {completion: acpdriver.CompletionLastAssistant, modes: []string{"bypassPermissions"}},
	runtimes.RuntimeIDQoder:     {completion: acpdriver.CompletionLastAssistant, modes: []string{constants.RuntimeQoderYoloModeID, constants.RuntimeQoderBypassModeID}},
	runtimes.RuntimeIDQoderCN:   {completion: acpdriver.CompletionLastAssistant, modes: []string{constants.RuntimeQoderYoloModeID, constants.RuntimeQoderBypassModeID}},
}

func nativeModelID(base, account, model string) string {
	if account != "" && account != constants.AiAccountSystemID {
		return accountconfig.NativeModelID(base, model)
	}
	return model
}

// nativeAgent 只把已冻结的事实交给公开适配器，不复制私有协议或全局配置合并。
func (p *credentialProvider) nativeAgent(ctx context.Context, request c.OpenRequest, values map[string]string) (acp.Agent, error) {
	return p.nativeAgentWithPermission(ctx, request, values, runtimes.PermissionLevelFullAccess)
}

// nativeAgentWithPermission 冻结产品已经授权的权限，不在工厂扩张到完全访问。
func (p *credentialProvider) nativeAgentWithPermission(ctx context.Context, request c.OpenRequest, values map[string]string, level runtimes.PermissionLevel) (acp.Agent, error) {
	path, args, environment := p.launch.Executable, append([]string(nil), p.launch.PrefixArgs...), environmentList(values)
	cwd, permission, logger := request.Workspace.Root, string(level), slog.Default()
	switch p.environment.BaseRuntimeID {
	case runtimes.RuntimeIDClaude:
		if p.target.ClaudeSettingsPath != "" {
			args = append(args, "--settings", p.target.ClaudeSettingsPath)
		}
		for _, directory := range p.target.NativePluginDirectories {
			args = append(args, "--plugin-dir", directory)
		}
		return acpclaude.NewAgent(ctx, acpclaude.Config{Logger: logger, ClaudePath: path, PrefixArgs: args, Environment: environment})
	case runtimes.RuntimeIDKimi:
		return acpkimi.NewAgent(ctx, acpkimi.Config{Logger: logger, KimiPath: path, PrefixArgs: args, Environment: environment, WorkingDirectory: cwd, StateDirectory: filepath.Join(p.workspace, "kimi-state"), PermissionMode: permission})
	case runtimes.RuntimeIDCursor:
		return acpcursor.NewAgent(ctx, acpcursor.Config{Logger: logger, CursorPath: path, PrefixArgs: args, Environment: environment, WorkingDirectory: cwd, StateDirectory: filepath.Join(p.workspace, "cursor-state"), StateDirectoryForWorkspace: func(cwd string) string { return filepath.Join(filepath.Dir(cwd), "cursor-state") }, PermissionMode: permission})
	case runtimes.RuntimeIDPi:
		return acppi.NewAgent(ctx, acppi.Config{Logger: logger, PiPath: path, PrefixArgs: args, Environment: environment, WorkingDirectory: cwd, PermissionMode: permission, MCPModulePath: values["PI_MCP_MODULE_PATH"]})
	case runtimes.RuntimeIDOpenCode:
		return acpopencode.NewAgent(ctx, acpopencode.Config{Logger: logger, OpenCodePath: path, PrefixArgs: args, Environment: environment, WorkingDirectory: cwd, PermissionMode: permission})
	case runtimes.RuntimeIDGemini:
		return acpgemini.NewAgent(ctx, acpgemini.Config{Logger: logger, GeminiPath: path, PrefixArgs: args, Environment: environment, WorkingDirectory: cwd, StateDirectory: values["GEMINI_CLI_HOME"], PermissionMode: permission})
	case runtimes.RuntimeIDGrok:
		return acpgrok.NewAgent(ctx, acpgrok.Config{Logger: logger, GrokPath: path, PrefixArgs: args, Environment: environment, WorkingDirectory: cwd, PermissionMode: permission})
	case runtimes.RuntimeIDWorkBuddy:
		return acpworkbuddy.NewAgent(ctx, acpworkbuddy.Config{Logger: logger, WorkBuddyPath: path, PrefixArgs: args, Environment: environment, WorkingDirectory: cwd, PermissionMode: permission})
	case runtimes.RuntimeIDQoder, runtimes.RuntimeIDQoderCN:
		return nativeacp.NewAgent(ctx, nativeacp.Config{Logger: logger, Command: path, Args: append(args, "--acp"), VersionArgs: append(append([]string(nil), p.launch.PrefixArgs...), "--version"), Environment: environment, WorkingDirectory: cwd, RuntimeName: p.target.RuntimeID, PromptFIFO: true})
	default:
		return nil, status_error.RuntimeUnavailable
	}
}
