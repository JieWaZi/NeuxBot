package execution

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// ForkChat 由 Chat 所有者管理操作身份、发布和对账，Thread 执行真实原生分叉。
func (m *service) ForkChat(ctx context.Context, input chatservice.ForkInput) (string, error) {
	chats, ok := m.journal.(interface {
		ForkChat(context.Context, chatservice.ForkInput, chatservice.NativeFork) (string, error)
	})
	if !ok || m.threads == nil {
		return "", status_error.RuntimeUnavailable
	}
	return chats.ForkChat(ctx, input, chatservice.NativeFork{
		Execute: func(ctx context.Context, input chatservice.NativeForkInput) (agent.NativeBinding, error) {
			thread, acquire, err := m.prepareNativeFork(ctx, input)
			if err != nil {
				return agent.NativeBinding{}, err
			}
			return m.threads.ForkSession(ctx, thread, acquire)
		},
	})
}

// prepareNativeFork 为冻结的原生历史位置取得当前有效账号与实际资源。
func (m *service) prepareNativeFork(ctx context.Context, input chatservice.NativeForkInput) (agent.ThreadRequest, func(context.Context, agent.ResourceSelection) (agent.ResourceLease, error), error) {
	fail := func(err error) (agent.ThreadRequest, func(context.Context, agent.ResourceSelection) (agent.ResourceLease, error), error) {
		return agent.ThreadRequest{}, nil, err
	}
	if input.Boundary.HistoryIdentity == "" {
		return fail(status_error.ChatForkUnavailable)
	}
	config := input.Configuration
	request := pendingMessage{chatID: input.ChatID, runtime: runtimeSelection{ConfigurationResolved: true, AccountID: config.AccountID, RuntimeID: config.RuntimeID, Model: config.Model, Reasoning: config.Reasoning, Fast: config.Fast, ContextWindow: config.ContextWindow, PermissionLevel: config.PermissionLevel, WorkspaceReadOnly: config.WorkspaceReadOnly, SessionPolicy: constants.RuntimeSessionPolicyContinue}}
	if m.runtimeEnvironments == nil {
		return fail(status_error.RuntimeUnavailable)
	}
	environment, err := m.runtimeEnvironments.Get(ctx, config.RuntimeID)
	if err != nil {
		return fail(err)
	}
	if !environment.Enabled || nativeProvider(environment) != input.Boundary.Session.Provider {
		return fail(status_error.ChatForkUnavailable)
	}
	if _, err := m.resolveConfiguration(ctx, ExecutionConfiguration{Resolved: true, RuntimeID: config.RuntimeID, AccountID: config.AccountID, Model: config.Model, Reasoning: config.Reasoning, PermissionLevel: config.PermissionLevel}, environment); err != nil {
		return fail(err)
	}
	state, err := m.resolveMessageChat(ctx, request, "")
	if err != nil {
		return fail(err)
	}
	if state.threadWorkspaceID != "" {
		return fail(status_error.ChatForkUnavailable)
	}
	instructions, err := m.prepareNativeInstructions(ctx, state, config.RuntimeID)
	if err != nil {
		return fail(err)
	}
	workspace := nativeWorkspace(state, request, &preparedMessageFiles{executionWorkspaceDirectory: input.Workspace})
	var enabledSkills []models.SkillWithState
	if m.skillController != nil {
		enabledSkills, err = m.skillController.ListEnabled(ctx, state.chat.WakerID)
		if err != nil {
			return fail(err)
		}
	}
	var enabledConnectors []models.MCPConnector
	if m.connectorController != nil {
		enabledConnectors, err = m.connectorController.ExecutionDeclarations(ctx, state.chat.WakerID)
		if err != nil {
			return fail(err)
		}
	}
	enabledConnectors = slices.DeleteFunc(enabledConnectors, func(c models.MCPConnector) bool {
		supported, _ := agent.ConnectorCompatibility(nativeProvider(environment), agent.ConnectorTransport(c.Transport))
		return !supported
	})
	frozen, err := m.freezePluginResources(ctx, state.chat.WakerID, request, environment.Detection)
	if err != nil {
		return fail(err)
	}
	skills, connectors := m.nativeSkills(enabledSkills), runtimeConnectorDeclarations(enabledConnectors)
	// Ordinary forks acquire no execution-scoped grants because they submit no user input.
	if m.agent != nil {
		skill := m.systemSkillDirectory
		if skill == "" {
			return fail(status_error.AgentContextStorageError)
		}
		skills = append(skills, agent.Skill{Name: constants.AgentCLISkillName, Path: skill, Digest: filepath.Base(skill)})
		m.appendAgentInstructions(&instructions, false, builtinskills.CLIPath(skill), skill)
	}
	selection := agent.ResourceSelection{Plugins: make(map[string]string, len(frozen.declarations))}
	for _, item := range frozen.declarations {
		selection.Plugins[item.ID] = item.VersionID
	}
	for _, item := range connectors {
		selection.ConnectorIDs = append(selection.ConnectorIDs, item.ID)
	}
	slices.Sort(selection.ConnectorIDs)
	pluginSkills, err := m.selectNativePluginSkills(ctx, frozen, skills...)
	if err != nil {
		return fail(err)
	}
	mergedSkills, err := mergePluginSkills(skills, pluginSkills)
	if err != nil {
		return fail(err)
	}
	selection.Skills, err = agent.FreezeSkills(environment.Detection, workspace, mergedSkills, selection.Plugins)
	if err != nil {
		return fail(err)
	}
	metadata, err := json.Marshal([]any{instructions, skills, connectors, workspace, frozen.native})
	if err != nil {
		return fail(status_error.RuntimeConfigurationInvalid)
	}
	key := sessionResourceKey(frozen.key, string(metadata), enabledConnectors)
	thread := nativeThreadRequest(input.ChatID, workspace, instructions, request, environment, selection, key)
	thread.Connection.Account, err = m.resolveAccount(ctx, thread.Connection.Account, thread.Connection.Launch.ConfigHome)
	if err != nil {
		return fail(err)
	}
	if thread.Connection.Account.Subject != input.Boundary.Session.Subject {
		return fail(status_error.ChatForkUnavailable)
	}
	if thread.Connection.Account.ConfigHome != "" {
		thread.Connection.Launch.ConfigHome = thread.Connection.Account.ConfigHome
	}
	thread.Fork = &agent.AgentForkRequest{HistoryIdentity: input.Boundary.HistoryIdentity, SourceSessionID: input.Boundary.Session.SessionID, SourceCWD: input.Workspace, Position: input.Boundary.Position, ReceiptDirectory: input.ReceiptDirectory, ReconcileOnly: input.ReconcileOnly, BeforeFork: input.BeforeFork}
	acquire := func(ctx context.Context, selection agent.ResourceSelection) (agent.ResourceLease, error) {
		return m.acquireNativeResources(ctx, input.ChatID, state.chat.WakerID, workspace, environment.Detection, skills, connectors, enabledConnectors, frozen, selection)
	}
	return thread, acquire, nil
}
