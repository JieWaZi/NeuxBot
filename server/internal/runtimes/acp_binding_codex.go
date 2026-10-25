package runtimes

import (
	"context"
	"sort"

	"github.com/JieWaZi/NeuxBot/server/constants"
	acpcodex "github.com/JieWaZi/acp-go/pkg/codex"
	acp "github.com/coder/acp-go-sdk"
)

const (
	// codexACPModelConfigID 是 acp-go Codex Adapter 的模型配置标识。
	codexACPModelConfigID acp.SessionConfigId = "model"
	// codexACPReasoningConfigID 是 acp-go Codex Adapter 的推理强度配置标识。
	codexACPReasoningConfigID acp.SessionConfigId = "reasoning_effort"
)

// codexACPRuntimeBinding 组合 Codex 自身的 ACP 能力与产品策略。
func codexACPRuntimeBinding() acpRuntimeBinding {
	binding := standardACPRuntimeBinding()
	binding.baseRuntimeID = RuntimeIDCodex
	binding.displayName = "Codex"
	binding.executableLogKey = "codex_binary"
	binding.modelConfigID = codexACPModelConfigID
	binding.reasoningConfigID = codexACPReasoningConfigID
	binding.elicitationSessionPaths = append(binding.elicitationSessionPaths, []string{"codex", "sessionId"})
	binding.modeID = func(policy ExecutionPolicy, _ *acp.SessionModeState) (acp.SessionModeId, error) {
		if policy.WorkspaceReadOnly || policy.PermissionLevel == PermissionLevelDefault {
			return "read-only", nil
		}
		if policy.PermissionLevel == PermissionLevelFullAccess {
			return "agent-full-access", nil
		}
		return "agent", nil
	}
	binding.selectedSkillPrompt = func(prompt string, selections []skillSelection) string {
		return selectedSkillCommandPrompt("$", prompt, selections)
	}
	binding.messagePhase = codexACPMessagePhase
	binding.newAgent = func(ctx context.Context, config acpAdapterConfig) (acp.Agent, error) {
		refresh := config.RefreshCredentials
		sourceAuth := ""
		if refresh == nil {
			var err error
			sourceAuth, err = codexAuthPathForEnvironment(config.Environment)
			if err != nil {
				return nil, err
			}
			refresh, err = codexFileRefresh(sourceAuth, nil)
			if err != nil {
				return nil, err
			}
		}
		var broker *codexRefreshBroker
		if refresh != nil {
			var err error
			broker, err = newCodexRefreshBroker(refresh)
			if err != nil {
				return nil, err
			}
			environment, err := buildEnvironment(config.Environment, map[string]string{codexRefreshURLVariable: broker.url})
			if err != nil {
				broker.Close()
				return nil, err
			}
			config.Environment = environment.list()
		}
		prefixArgs := cloneStrings(config.PrefixArgs)
		keys := make([]string, 0, len(config.CodexPluginConfiguration))
		for key := range config.CodexPluginConfiguration {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			prefixArgs = append(prefixArgs, "-c", key+"="+config.CodexPluginConfiguration[key])
		}
		agent, err := acpcodex.NewAgent(ctx, acpcodex.Config{
			Logger:                      acpAdapterLogger(ctx),
			CodexPath:                   config.Executable,
			PrefixArgs:                  prefixArgs,
			Environment:                 append([]string(nil), config.Environment...),
			DefaultModeRequestUserInput: constants.RuntimeCodexDefaultModeRequestUserInput,
		})
		if err != nil {
			if broker != nil {
				broker.Close()
			}
			return nil, err
		}
		if broker != nil {
			return &codexCredentialAgent{Agent: agent, broker: broker, sourceAuth: sourceAuth}, nil
		}
		return agent, nil
	}
	return binding
}

// codexACPMessagePhase 读取 acp-go 保留的 Codex 消息阶段。
func codexACPMessagePhase(meta map[string]any) string {
	codexMeta, _ := meta[constants.RuntimeACPMetaKeyCodex].(map[string]any)
	phase, _ := codexMeta[constants.RuntimeACPMetaKeyPhase].(string)
	return phase
}
