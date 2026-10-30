package execution

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	runtimeworkspace "github.com/JieWaZi/NeuxBot/server/internal/agent/host/workspace"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// sessionResourceKey 用保存的配置事实区分资源，不把明文凭据或测试结果带入会话声明。
func sessionResourceKey(pluginKey, accountConfiguration string, connectors []models.MCPConnector) string {
	connectors = slices.Clone(connectors)
	slices.SortFunc(connectors, func(a, b models.MCPConnector) int { return strings.Compare(a.ID, b.ID) })
	identities := make([]any, 0, len(connectors))
	for _, connector := range connectors {
		environment, headers := slices.Clone(connector.Environment), slices.Clone(connector.Headers)
		for _, definitions := range [][]models.MCPSecretDefinition{environment, headers} {
			slices.SortFunc(definitions, func(a, b models.MCPSecretDefinition) int { return strings.Compare(a.Name, b.Name) })
		}
		// Value 的 JSON 契约明确排除明文；只纳入持久定义与密文，忽略刷新和探测状态。
		identities = append(identities, []any{connector.ID, connector.Name, connector.RuntimeKey, connector.Transport, connector.Command, connector.Arguments, connector.URL, connector.TimeoutMS, environment, headers})
	}
	body, _ := json.Marshal([]any{pluginKey, accountConfiguration, identities})
	return agentIdentity(string(body))
}

// acquireChatSessionResources 只在新原生消费者取得资源时物化账号和插件。
func (m *service) acquireChatSessionResources(ctx context.Context, state messageChat, request pendingMessage, executionID string, enabledConnectors []models.MCPConnector, runtimeEnvironment models.RuntimeEnvironment, sessionPlan runtimecontract.SessionPlan, session runtimecontract.SessionDirective, frozen frozenPluginResources, launchEnvironment map[string]string) (runtimecontract.SessionResources, error) {
	var accountLease func(context.Context) error
	var releasePlugins func()
	partial := runtimecontract.SessionResources{Target: sessionPlan.Target.LaunchTarget(), Skills: slices.Clone(sessionPlan.Skills)}
	// 部分准备也必须交回同一释放入口；成功步骤立即移出释放栈，失败步骤保留供Core重试。
	partial.Close = func(closeContext context.Context) error {
		if releasePlugins != nil {
			releasePlugins()
			releasePlugins = nil
		}
		if accountLease != nil {
			if err := accountLease(closeContext); err != nil {
				return err
			}
			accountLease = nil
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return partial, err
	}
	pluginPreparationStarted := time.Now()
	pluginResources, err := m.acquirePluginResources(ctx, frozen, authorization.Usage{ChatID: state.chat.ID, ExecutionID: executionID, WakerID: state.chat.WakerID, Title: state.chat.Title})
	if elapsed := time.Since(pluginPreparationStarted); elapsed >= time.Second {
		logx.Warnf("chat plugin preparation slow: execution_id=%s runtime_id=%s duration=%s", executionID, request.runtime.RuntimeID, elapsed)
	}
	releasePlugins = pluginResources.release
	if err != nil {
		return partial, err
	}
	enabledConnectors = append(enabledConnectors, pluginResources.connectors...)
	runtimeConnectors := enabledConnectors
	runtimeConnectors, err = restrictConnectors(runtimeConnectors, request.allowedConnectorIDs)
	if err != nil {
		return partial, err
	}
	if !selectedConnectorsAreEnabled(
		request.message.ConnectorIDs,
		runtimeConnectors,
	) {
		return partial, status_error.BadRequest
	}
	partial.Connectors = runtimeConnectorResources(runtimeConnectors)
	runtimeSource := partial.Target
	agentEnvironment := launchEnvironment
	if request.runtime.AccountID != "" {
		var target runtimeconfig.RuntimeTarget
		var prepareErr error
		target, accountLease, prepareErr = m.accounts.PrepareQueuedLease(ctx, request.runtime.AccountID, runtimeEnvironment.BaseRuntimeID, sessionPlan.Workspace.Path, request.runtime.Model)
		if prepareErr != nil {
			if target.AccountID != "" {
				partial.Target = target
			}
			return partial, prepareErr
		}
		partial.Target.AccountID = target.AccountID
		partial.Target.RefreshCredentials = target.RefreshCredentials
		partial.Target.Environment = target.Environment
		partial.Target.Isolated = target.Isolated
		partial.Target.CloseAfterRun = target.CloseAfterRun
		partial.Target.ConfigHome = ""
	}
	for key, value := range agentEnvironment {
		if key == "PATH" {
			continue
		}
		if partial.Target.Environment == nil {
			partial.Target.Environment = map[string]string{}
		}
		partial.Target.Environment[key] = value
	}
	if binary := agentEnvironment[constants.AgentCLIBinaryEnvironment]; binary != "" {
		base := partial.Target.Environment["PATH"]
		if base == "" {
			base = os.Getenv("PATH")
		}
		partial.Target.Environment["PATH"] = filepath.Dir(binary) + string(os.PathListSeparator) + base
	}

	nativeEnvironment, err := runtimeworkspace.PreparePluginEnvironment(runtimeworkspace.PluginEnvironmentRequest{
		Target: partial.Target, Source: runtimeSource, Detection: runtimeEnvironment.Detection,
		Workspace: sessionPlan.Workspace.Path, DataDirectory: m.skillDataDirectory,
		Native: pluginResources.native, Snapshots: pluginResources.snapshots, Environment: pluginResources.environment,
		AllowExternal: request.allowedConnectorIDs == nil && request.allowedAgentDomains == nil,
	})
	if err != nil {
		return partial, err
	}
	partial.Target = nativeEnvironment.Target
	pluginResources.snapshots = nativeEnvironment.Snapshots
	request.nativePluginScope = partial.Target.NativePluginScope
	nativeScopeChanged := nativeEnvironment.ScopeChanged
	if session.Mode == runtimecontract.SessionModeResume {
		runtimeSession := state.runtimeSessions[request.runtime.RuntimeID]
		previousConfiguration, recordErr := m.journal.ReadExecutionConfiguration(ctx, state.chat.ID, runtimeSession.LastExecutionID)
		if recordErr != nil {
			return partial, recordErr
		}
		previous := previousConfiguration.PluginResources
		previousNativeScope := previousConfiguration.NativePluginScope
		if state.chat.ForkSourceChatID != "" && request.nativePluginScope != "" {
			// 分叉工作区没有来源缓存，必须比较分叉点的冻结事实；未知旧事实不能放行。
			nativeScopeChanged = previousNativeScope == "" || previousNativeScope != request.nativePluginScope
		}
		if nativeScopeChanged || nativePluginScope(previous) != nativePluginScope(pluginResources.snapshots) {
			if state.chat.ForkSourceChatID != "" {
				return partial, status_error.PluginNotReady
			}
			session = runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeNew}
		}
	}
	partial.Skills, err = mergePluginSkills(partial.Skills, pluginResources.skills)
	if err != nil {
		return partial, err
	}
	partial.Session = &session
	partial.PluginResources = slices.Clone(pluginResources.snapshots)
	partial.NativePluginScope = partial.Target.NativePluginScope
	partial.CommitScope = nativeEnvironment.CommitScope
	partial.UpdateUsage = pluginResources.updateUsage
	return partial, nil
}

// runtimeConnectorDeclarations 只声明连接器的固定程序与公开配置，不读取秘密值。
func runtimeConnectorDeclarations(connectors []models.MCPConnector) []runtimecontract.ConnectorDeclaration {
	result := make([]runtimecontract.ConnectorDeclaration, 0, len(connectors))
	for _, connector := range connectors {
		result = append(result, runtimecontract.ConnectorDeclaration{
			ID: connector.ID, Name: connector.Name, RuntimeKey: connector.RuntimeKey,
			Transport: runtimeconfig.ConnectorTransport(connector.Transport), Command: connector.Command,
			Arguments: slices.Clone(connector.Arguments), URL: connector.URL, TimeoutMS: connector.TimeoutMS,
		})
	}
	return result
}

// runtimeConnectorResources 只在冷获取时展开实际连接秘密，本轮选择由 TurnInput 保存。
func runtimeConnectorResources(connectors []models.MCPConnector) []runtimecontract.Connector {
	result := make([]runtimecontract.Connector, 0, len(connectors))
	for _, connector := range connectors {
		result = append(result, runtimecontract.Connector{
			ID: connector.ID, Name: connector.Name, RuntimeKey: connector.RuntimeKey,
			Transport: runtimeconfig.ConnectorTransport(connector.Transport), Command: connector.Command,
			Arguments: slices.Clone(connector.Arguments), URL: connector.URL, TimeoutMS: connector.TimeoutMS,
			Environment: connectorSecretValues(connector.Environment), Headers: connectorSecretValues(connector.Headers),
		})
	}
	return result
}
