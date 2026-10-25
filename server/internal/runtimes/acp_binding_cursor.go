package runtimes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	acpcursor "github.com/JieWaZi/acp-go/pkg/cursor"
	acp "github.com/coder/acp-go-sdk"
)

// cursorACPRuntimeBinding 组合 Cursor 的聊天独占进程与参数化模型策略。
func cursorACPRuntimeBinding() acpRuntimeBinding {
	binding := nativeACPRuntimeBinding()
	binding.baseRuntimeID = RuntimeIDCursor
	binding.displayName = "Cursor"
	binding.retainedProcess = true
	binding.permissionLevels = cursorACPPermissionLevels
	binding.modeID = func(policy ExecutionPolicy, _ *acp.SessionModeState) (acp.SessionModeId, error) {
		if policy.WorkspaceReadOnly {
			return "plan", nil
		}
		return "agent", nil
	}
	binding.resolveSelections = cursorResolveSelections
	binding.skipSelection = cursorSkipSelection
	binding.validateSelection = cursorValidateSelection
	binding.supportsPersistent = func(capabilities acp.AgentCapabilities) bool { return capabilities.LoadSession }
	binding.retainPromptFailure = func(code FailureCode) bool {
		return code == FailureCodePlanUpgradeRequired ||
			code == FailureCodeQuotaExhausted ||
			code == FailureCodeRateLimited ||
			code == FailureCodeUpstreamUnavailable
	}
	binding.failureCode = cursorACPFailureCode
	binding.agentScope = cursorACPAgentScope
	binding.stateDirectory = cursorStateDirectory
	binding.newAgent = func(ctx context.Context, config acpAdapterConfig) (acp.Agent, error) {
		return acpcursor.NewAgent(ctx, acpcursor.Config{
			StateDirectory:             config.StateDirectory,
			StateDirectoryForWorkspace: func(cwd string) string { return filepath.Join(filepath.Dir(cwd), "cursor-state") },
			Logger:                     acpAdapterLogger(ctx),
			CursorPath:                 config.Executable,
			PrefixArgs:                 cloneStrings(config.PrefixArgs),
			Environment:                append([]string(nil), config.Environment...),
			WorkingDirectory:           config.WorkingDirectory,
			PermissionMode:             config.PermissionMode,
		})
	}
	return binding
}

// cursorACPFailureCode 优先识别 Cursor 原生错误码，再回退到共享 ACP 分类。
func cursorACPFailureCode(err error) FailureCode {
	var requestError *acp.RequestError
	if errors.As(err, &requestError) {
		metadata, ok := requestError.Data.(map[string]any)
		if ok {
			cursorErrorCode, _ := metadata[constants.RuntimeACPMetaKeyCursorErrorCode].(string)
			if cursorErrorCode == constants.RuntimeACPCursorErrorCodeUpgrade {
				return FailureCodePlanUpgradeRequired
			}
		}
	}
	return acpFailureCode(err)
}

// cursorACPPermissionLevels 仅在 Cursor 声明 agent 模式时公开三档受管权限。
func cursorACPPermissionLevels(options []acp.SessionConfigOption, modes []acp.SessionMode) []PermissionLevel {
	if hasAnyMode(acpAvailableModeIDs(options, modes), "agent") {
		return []PermissionLevel{PermissionLevelDefault, PermissionLevelAuto, PermissionLevelFullAccess}
	}
	return []PermissionLevel{}
}

// cursorResolveSelections 在缺少显式选择时回退到会话创建时的 CLI 默认值。
func cursorResolveSelections(agent *embeddedACPAgent, sessionID acp.SessionId, request RunRequest) RunRequest {
	agent.sessionMutex.Lock()
	defer agent.sessionMutex.Unlock()
	if request.Policy.Model == "" {
		request.Policy.Model = string(agent.sessionDefaultModels[sessionID])
	}
	if request.Policy.Reasoning == "" {
		model := acp.SessionConfigValueId(request.Policy.Model)
		request.Policy.Reasoning = string(agent.sessionDefaultReasoning[sessionID][model])
	}
	return request
}

// cursorSkipSelection 避免对进程级模型重复写入相同值。
func cursorSkipSelection(agent *embeddedACPAgent, sessionID acp.SessionId, id acp.SessionConfigId, value string) bool {
	if id != agent.binding.modelConfigID {
		return false
	}
	agent.sessionMutex.Lock()
	defer agent.sessionMutex.Unlock()
	return agent.sessionModels[sessionID] == acp.SessionConfigValueId(value)
}

// cursorValidateSelection 验证参数化模型与推理选项均得到上游确认。
func cursorValidateSelection(
	agent *embeddedACPAgent,
	sessionID acp.SessionId,
	request RunRequest,
	id acp.SessionConfigId,
	value string,
	options []acp.SessionConfigOption,
) error {
	if acpConfigCurrentValue(options, id) != acp.SessionConfigValueId(value) {
		return status_error.RuntimeProtocolIncompatible
	}
	modelMismatch := request.Policy.Model != "" &&
		acpConfigCurrentValue(options, agent.binding.modelConfigID) != acp.SessionConfigValueId(request.Policy.Model)
	if id == agent.binding.reasoningConfigID && modelMismatch {
		return status_error.RuntimeProtocolIncompatible
	}
	if id == agent.binding.modelConfigID {
		agent.sessionMutex.Lock()
		agent.sessionModels[sessionID] = acp.SessionConfigValueId(value)
		agent.sessionMutex.Unlock()
	}
	return nil
}

// cursorStateDirectory 返回 Chat 级 Cursor 持久状态目录，供冷恢复复用 store.db。
func cursorStateDirectory(request acpRunRequest) string {
	if !request.Request.Workspace.Managed {
		return ""
	}
	workspace := filepath.Clean(request.Request.Workspace.Path)
	if workspace == "" || workspace == "." {
		return ""
	}
	return filepath.Join(filepath.Dir(workspace), "cursor-state")
}

// cursorACPAgentScope 为聊天身份与授权范围生成进程隔离键。
func cursorACPAgentScope(request acpRunRequest) string {
	identity := "invocation:" + request.InvocationID
	if request.Request.Session.Mode == SessionModeResume {
		identity = "session:" + request.Request.Session.ID
	}
	scope, _ := json.Marshal([]any{
		request.Request.Workspace,
		request.Request.Policy.Execution,
		request.Request.Connectors,
		request.Request.Instructions,
		request.Request.Skills,
	})
	digest := sha256.Sum256(scope)
	return identity + "\x00" + hex.EncodeToString(digest[:])
}
