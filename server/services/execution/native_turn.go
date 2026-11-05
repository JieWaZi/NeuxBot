package execution

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

// nativeResourceProjection is the public description of resources actually owned by one connection.
type nativeResourceProjection struct {
	// plugins identifies delivered plugin versions.
	plugins []plugins.Snapshot
	// connectors identifies the materialized connector set.
	connectors []agent.Connector
	// updateUsage assigns or clears the current admitted execution on shared plugin leases.
	updateUsage func(...authorization.Usage)
}

func nativeProvider(environment models.RuntimeEnvironment) string {
	if environment.BaseRuntimeID != "" {
		return environment.BaseRuntimeID
	}
	if environment.Detection.Runtime.BaseRuntimeID != "" {
		return environment.Detection.Runtime.BaseRuntimeID
	}
	return environment.RuntimeID
}

func nativeThreadRequest(chatID string, plan agent.SessionPlan, request pendingMessage, environment models.RuntimeEnvironment, resources agent.ResourceSelection, key string) agent.ThreadRequest {
	accountID := request.runtime.AccountID
	if accountID == "" {
		accountID = agent.SystemAccountID
	}
	executable := environment.Detection.ResolvedBinary
	if executable == "" {
		executable = environment.Binary
	}
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		if name, value, ok := strings.Cut(entry, "="); ok {
			env[name] = value
		}
	}
	mode := "agent"
	if plan.Policy.Execution.WorkspaceReadOnly || plan.Policy.Execution.PermissionLevel == constants.PermissionLevelDefault {
		mode = "read-only"
	} else if plan.Policy.Execution.PermissionLevel == constants.PermissionLevelFullAccess {
		mode = "agent-full-access"
	}
	digest := sha256.Sum256([]byte(key))
	// A workspace already supplied as cwd is not an additional native capability.
	additional := make([]string, 0, len(plan.Workspace.AdditionalWritableDirectories))
	for _, directory := range plan.Workspace.AdditionalWritableDirectories {
		if filepath.Clean(directory) != filepath.Clean(plan.Workspace.Path) {
			additional = append(additional, directory)
		}
	}
	thread := agent.ThreadRequest{
		ChatID: chatID,
		Connection: agent.ProviderConnectionRequest{
			Account: agent.AccountRequest{Executable: executable, PrefixArgs: environment.PrefixArgs, Provider: nativeProvider(environment), AccountID: accountID, CWD: plan.Workspace.Path, Model: plan.Policy.Model},
			Launch: agent.ProviderLaunch{
				Executable: executable, PrefixArgs: environment.PrefixArgs, ConfigHome: environment.ConfigHome, BaseEnvironment: env,
				CodexConfig: map[string]json.RawMessage{
					"features.goals": json.RawMessage("false"),
				},
			},
			Resources: resources,
		},
		Session: agent.AgentSessionOptions{AdditionalDirectories: additional},
		Mode:    mode, ResourceGeneration: binary.BigEndian.Uint64(digest[:8]),
		ForceNew:     request.runtime.SessionPolicy == constants.RuntimeSessionPolicyNew,
		Summarize:    request.runtime.HistoryMode == constants.HistoryModeSummary,
		HistoryTurns: request.runtime.HistoryTurns,
	}
	thread.Selection = &agent.TurnSelection{Reasoning: plan.Policy.Reasoning, Fast: request.runtime.Fast}
	if !request.runtime.ConfigurationResolved {
		thread.Selection.DefaultFast = environment.DefaultFast
	}
	switch thread.Connection.Account.Provider {
	case constants.RuntimeIDClaude, constants.RuntimeIDWorkBuddy, constants.RuntimeIDQoder, constants.RuntimeIDQoderCN:
		thread.Mode = "default"
		if plan.Policy.Execution.PermissionLevel == constants.PermissionLevelAuto {
			thread.Mode = "auto"
		}
		if plan.Policy.Execution.PermissionLevel == constants.PermissionLevelFullAccess {
			thread.Mode = "bypassPermissions"
			if thread.Connection.Account.Provider == constants.RuntimeIDQoder || thread.Connection.Account.Provider == constants.RuntimeIDQoderCN {
				thread.Mode = "yolo"
			}
		}
	case constants.RuntimeIDCursor:
		// Cursor's ACP policy is process-scoped; it is not a session task mode.
		thread.Mode = ""
		thread.Connection.Launch.CursorPermissionMode = "default"
		if plan.Policy.Execution.PermissionLevel == constants.PermissionLevelAuto {
			thread.Connection.Launch.CursorPermissionMode = "auto"
		}
		if plan.Policy.Execution.PermissionLevel == constants.PermissionLevelFullAccess {
			thread.Connection.Launch.CursorPermissionMode = "full-access"
		}
	case constants.RuntimeIDKimi, constants.RuntimeIDGemini, constants.RuntimeIDGrok, constants.RuntimeIDOpenCode, constants.RuntimeIDPi:
		thread.Mode = ""
		thread.Connection.Launch.ProcessPermissionMode = string(plan.Policy.Execution.PermissionLevel)
	}
	provider := thread.Connection.Account.Provider
	if accountID != agent.SystemAccountID && (provider == constants.RuntimeIDOpenCode || provider == constants.RuntimeIDPi) {
		// The native provider namespace is transport detail; account validation and
		// public choices continue to use the user's configured model IDs.
		thread.Connection.Account.Model = accountconfig.NativeModelID(provider, plan.Policy.Model)
	}

	return thread
}

// nativeUsageSource classifies the host entry point from admission evidence.
func nativeUsageSource(input pendingMessage, state messageChat) string {
	switch input.message.Actor {
	case "workflow":
		return "workflow"
	case constants.AutomaticTaskActor:
		return "automatic"
	case string(constants.ChannelMessageActorLead), string(constants.ChannelMessageActorMemberReport), string(constants.ChannelMessageActorSystem):
		return "team"
	}
	if input.automaticRunID != "" {
		return "automatic"
	}
	if state.threadWorkspaceID != "" {
		return "team"
	}
	if !state.chat.HiddenFromWakerHistory {
		return "chat"
	}
	return ""
}

// nativeTurnPrompt projects only the current business input. Thread owns any handoff prefix.
func nativeTurnPrompt(turn agent.TurnInput) string {
	var prompt strings.Builder
	prompt.WriteString(turn.Message.Text)
	for _, attachment := range turn.Message.Attachments {
		fmt.Fprintf(&prompt, "\n\nAttachment %q is available at %q.", attachment.Name, attachment.Path)
	}
	for _, reference := range turn.Message.CodeReferences {
		if reference.StartLine == 0 {
			fmt.Fprintf(&prompt, "\n\nReferenced file %q.", reference.Path)
		} else {
			fmt.Fprintf(&prompt, "\n\nReferenced lines %d-%d of %q:\n%s", reference.StartLine, reference.EndLine, reference.Path, reference.Content)
		}
	}
	if len(turn.SelectedSkills) > 0 {
		fmt.Fprintf(&prompt, "\n\nSelected skills: %s.", strings.Join(turn.SelectedSkills, ", "))
	}
	if len(turn.SelectedConnectors) > 0 {
		fmt.Fprintf(&prompt, "\n\nSelected connectors: %s.", strings.Join(turn.SelectedConnectors, ", "))
	}
	return prompt.String()
}

// nativeTurnInput includes bounded native image data when the detected runtime advertises support.
func nativeTurnInput(id string, input agent.TurnInput, detection agent.DetectionResult) (agent.TurnRequest, error) {
	request := agent.TurnRequest{HostInvocationID: id, ExecutionID: id, AssistantMessageID: id, Text: nativeTurnPrompt(input)}
	if !detection.Capabilities[agent.CapabilityImagePath] {
		return request, nil
	}
	const attachmentLimit = 20 << 20
	for _, attachment := range input.Message.Attachments {
		if !strings.HasPrefix(attachment.MediaType, "image/") {
			continue
		}
		file, err := os.Open(attachment.RuntimePath)
		if err != nil {
			return agent.TurnRequest{}, status_error.AgentInputInvalid
		}
		data, readErr := io.ReadAll(io.LimitReader(file, attachmentLimit+1))
		if err := errors.Join(readErr, file.Close()); err != nil || len(data) > attachmentLimit {
			return agent.TurnRequest{}, status_error.AgentInputInvalid
		}
		if len(request.Content) == 0 {
			request.Content = []agent.AgentContent{{Kind: "text", Text: request.Text}}
			request.Text = ""
		}
		request.Content = append(request.Content, agent.AgentContent{Kind: "image", MIMEType: attachment.MediaType,
			Data: base64.StdEncoding.EncodeToString(data)})
	}
	return request, nil
}

func (m *service) startNativeExecution(
	ctx, executionContext context.Context,
	state messageChat, request pendingMessage, executionID, userMessageID string,
	files *preparedMessageFiles, enabledSkills []models.SkillWithState, enabledConnectors []models.MCPConnector,
	environment models.RuntimeEnvironment, frozen frozenPluginResources, releaseReservation *bool,
) (AcceptedExecution, error) {
	journal, ok := m.journal.(chatservice.NativeJournal)
	if !ok || m.threads == nil {
		return AcceptedExecution{}, status_error.RuntimeUnavailable
	}

	plan, input := m.buildSessionPlan(state, request, files, enabledSkills, enabledConnectors, environment)
	plan.Target.AccountID = request.runtime.AccountID
	input.Message.Text += pluginSelectionHint(frozen.selected)
	activate, revoke, err := m.prepareAgent(&plan, &input,
		state.chat, executionID, request.toolCapabilities, request.allowedAgentDomains, request.automaticRunID)
	if err != nil {
		return AcceptedExecution{}, err
	}
	if revoke != nil {
		defer func() {
			if *releaseReservation {
				revoke()
			}
		}()
	}

	selection := agent.ResourceSelection{Plugins: make(map[string]string, len(frozen.declarations))}
	for _, item := range frozen.declarations {
		selection.Plugins[item.ID] = item.VersionID
	}
	for _, connector := range plan.Connectors {
		selection.ConnectorIDs = append(selection.ConnectorIDs, connector.ID)
	}
	slices.Sort(selection.ConnectorIDs)
	metadata, err := json.Marshal([]any{plan.Instructions, plan.Skills, plan.Connectors})
	if err != nil {
		return AcceptedExecution{}, status_error.RuntimeConfigurationInvalid
	}
	key := sessionResourceKey(frozen.key, string(metadata), enabledConnectors)
	thread := nativeThreadRequest(state.chat.ID, plan, request, environment, selection, key)
	thread.Connection.Account, err = m.resolveAccount(ctx, thread.Connection.Account, thread.Connection.Launch.ConfigHome)
	if err != nil {
		return AcceptedExecution{}, err
	}

	if thread.Connection.Account.ConfigHome != "" {
		thread.Connection.Launch.ConfigHome = thread.Connection.Account.ConfigHome
	}

	work := nativeExecutionWork{
		chat: state.chat, state: state, files: files, journal: journal,
		input: request, execution: newExecutionIndex(state, request, executionID, userMessageID),
		activate: activate, revoke: revoke, started: make(chan executionStartResult, 1),
	}
	ports := work.ports(m)
	ports.AcquireResources = func(ctx context.Context, _ agent.ResourceSelection) (agent.ResourceLease, error) {
		return m.acquireNativeResources(ctx, state.chat.ID, plan, enabledConnectors, frozen, input.LaunchEnvironment)
	}
	nativeInput, err := nativeTurnInput(executionID, input, environment.Detection)
	if err != nil {
		return AcceptedExecution{}, err
	}
	nativeInput.UsageWakerID = state.chat.WakerID
	nativeInput.UsageSource = nativeUsageSource(request, state)
	turn, err := m.threads.Prepare(ctx, thread, nativeInput, ports)
	if err != nil {
		return AcceptedExecution{}, err
	}
	defer func() {
		if *releaseReservation {
			if err := turn.Close(context.WithoutCancel(ctx)); err != nil {
				logx.Errorf("close unstarted native turn: execution_id=%s error=%v", executionID, err)
			}
		}
	}()
	if thread.Selection != nil {
		request.runtime.Fast = turn.RequestedFast()
		work.input.runtime.Fast = request.runtime.Fast
		work.execution.Fast = request.runtime.Fast
	}
	rawResources, exists := m.nativeResources.Load(state.chat.ID)
	if !exists {
		return AcceptedExecution{}, status_error.RuntimeUnavailable
	}
	work.resources = rawResources.(*nativeResourceProjection)
	if !selectedRuntimeConnectorsAreEnabled(request.message.ConnectorIDs, work.resources.connectors) {
		return AcceptedExecution{}, status_error.BadRequest
	}
	for _, id := range request.message.PluginIDs {
		index := slices.IndexFunc(work.resources.plugins, func(snapshot plugins.Snapshot) bool { return snapshot.ID == id })
		if index < 0 {
			return AcceptedExecution{}, status_error.PluginNotReady
		}
		work.selectedPlugins = append(work.selectedPlugins, work.resources.plugins[index])
	}
	work.execution.PluginResources = slices.Clone(work.resources.plugins)
	work.skills = selectedSkillReferences(request.message.SkillIDs, enabledSkills)
	work.connectors = selectedConnectorReferences(request.message.ConnectorIDs, work.resources.connectors)

	// A reliable entrypoint commits the pending job before dispatch. The native admission
	// batch then adds execution.started and the actual input identity together.
	if request.admission == constants.ExecutionAdmissionCommitBeforeDispatch {
		accepted, err := journal.CommitAcceptedTurn(ctx, work.admission(""))
		if err != nil {
			return AcceptedExecution{}, err
		}
		files.commit()
		work.chat, work.execution, work.precommitted = accepted.Chat, accepted.Execution, true
		*releaseReservation = false
		m.executions.launch(state.chat.ID, executionID, executionContext, func(ctx context.Context) { m.runNativeExecution(ctx, &work, turn) })
		return AcceptedExecution{Chat: accepted.Chat, Execution: accepted.Execution}, nil
	}
	*releaseReservation = false
	m.executions.launch(state.chat.ID, executionID, executionContext, func(ctx context.Context) { m.runNativeExecution(ctx, &work, turn) })
	select {
	case result := <-work.started:
		return AcceptedExecution{Chat: result.chat, Execution: result.execution}, result.err
	case <-ctx.Done():
		m.executions.cancel(state.chat.ID)
		result := <-work.started
		return AcceptedExecution{Chat: result.chat, Execution: result.execution}, result.err
	}
}

func (m *service) acquireNativeResources(
	ctx context.Context, chatID string, plan agent.SessionPlan,
	baseConnectors []models.MCPConnector, frozen frozenPluginResources, launchEnvironment map[string]string,
) (agent.ResourceLease, error) {
	materialized, err := m.acquirePluginResources(ctx, frozen, false)
	projection := &nativeResourceProjection{}
	lease := agent.ResourceLease{Close: func(context.Context) error {
		m.nativeResources.CompareAndDelete(chatID, projection)
		if materialized.release != nil {
			materialized.release()
		}
		return nil
	}}
	if err != nil {
		return lease, err
	}
	projection.plugins = materialized.snapshots
	projection.updateUsage = materialized.updateUsage
	plan.Skills, err = mergePluginSkills(plan.Skills, materialized.skills)
	if err != nil {
		return lease, err
	}
	if err := m.workspacePreparer.Prepare(plan); err != nil {
		return lease, err
	}
	projection.connectors = runtimeConnectorResources(append(slices.Clone(baseConnectors), materialized.connectors...))
	for _, declaration := range plan.Connectors {
		if declaration.ID == "neuxbot-internal-capabilities" {
			projection.connectors = append(projection.connectors, agent.Connector{ID: declaration.ID, Name: declaration.Name,
				RuntimeKey: declaration.RuntimeKey, Transport: declaration.Transport, Command: declaration.Command, Arguments: declaration.Arguments})
		}
	}
	for _, connector := range projection.connectors {
		name := connector.RuntimeKey
		if name == "" {
			name = connector.Name
		}
		transport := string(connector.Transport)
		if connector.Transport == constants.RuntimeConnectorTransportStreamableHTTP {
			transport = "http"
		}
		lease.MCPServers = append(lease.MCPServers, agent.AgentMCPServer{Name: name, Transport: transport,
			Command: connector.Command, Arguments: connector.Arguments, Environment: connector.Environment, URL: connector.URL, Headers: connector.Headers})
	}
	for index := range projection.connectors {
		projection.connectors[index].Environment = nil
		projection.connectors[index].Headers = nil
	}

	lease.Environment = materialized.environment
	for name, value := range launchEnvironment {
		lease.Environment[name] = value
	}
	if cli := launchEnvironment[constants.AgentCLIBinaryEnvironment]; cli != "" {
		path := lease.Environment["PATH"]
		if path == "" {
			path = os.Getenv("PATH")
		}
		lease.Environment["PATH"] = filepath.Dir(cli) + string(os.PathListSeparator) + path
	}
	m.nativeResources.Store(chatID, projection)
	return lease, nil
}

// nativeExecutionWork contains the business state carried across the native admission boundary.
type nativeExecutionWork struct {
	// journal owns all durable Chat business facts and public projections.
	journal chatservice.NativeJournal
	// state freezes the Chat before its current admission.
	state messageChat
	// chat is the current committed business projection after acceptance.
	chat models.Chat
	// execution holds the pending or admitted execution projection.
	execution models.Execution
	// input is the frozen business input, without native prompt additions.
	input pendingMessage
	// files remain reversible until their first business fact commits.
	files *preparedMessageFiles
	// skills are the current message's selected skills.
	skills []eventlog.MessageSkillReference
	// connectors are the current message's selected connectors.
	connectors []eventlog.MessageConnectorReference
	// selectedPlugins are the current message's selected plugin versions.
	selectedPlugins []plugins.Snapshot
	// resources is the native connection's acquired resource projection.
	resources *nativeResourceProjection
	// activate enables a grant only after durable admission and projection.
	activate func()
	// revoke retires this input's grant.
	revoke func()
	// started reports the first durable admission or pre-admission failure to Start.
	started chan executionStartResult
	// precommitted records a reliable entrypoint's durable pending execution.
	precommitted bool
	// projected ensures body observers never wake ahead of business admission.
	projected atomic.Bool
	// admissionReceipt records the first durable admission callback for canceled authorization.
	admissionReceipt atomic.Pointer[agent.CommitReceipt]
	// admissionFacts are prepared by Admit before any durable observation is called.
	admissionFacts chatservice.StartedTurn
	// recording captures workspace changes around the admitted turn.
	recording *chatservice.FileChangesRecording
}

func (work *nativeExecutionWork) admission(invocationID string) chatservice.StartedTurn {
	admission := chatservice.StartedTurn{NewChat: work.state.isNew && !work.precommitted, Chat: work.chat, Execution: work.execution}
	if !work.precommitted {
		admission.Facts = startedMessageEvents(work.state, work.input, work.execution, invocationID,
			work.files.attachmentReferences, work.files.codeReferences, work.skills, work.connectors, work.selectedPlugins)
	}
	if invocationID != "" {
		admission.Facts = append(admission.Facts, eventlog.AppendInput{Type: constants.ChatEventTypeExecutionStarted,
			ExecutionID: work.execution.ID, InvocationID: invocationID})
	}
	return admission
}

func (work *nativeExecutionWork) ports(m *service) agent.TurnPorts {
	chatID, executionID := work.chat.ID, work.execution.ID
	ports := agent.TurnPorts{
		Admit: func(ctx context.Context, admitted agent.TurnAdmission) (agent.ConversationBatch, error) {
			work.admissionFacts = work.admission(admitted.Input.HostInvocationID)
			return work.journal.PrepareTurnAdmission(ctx, work.admissionFacts, "turn:"+admitted.Input.HostInvocationID)
		},
		Authorize: func(ctx context.Context, _ agent.TurnAdmission, receipt agent.CommitReceipt) (func(), error) {
			if err := work.projectAdmission(context.WithoutCancel(ctx), receipt); err != nil {
				return nil, err
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if work.activate != nil {
				work.activate()
			}
			if work.resources.updateUsage != nil {
				work.resources.updateUsage(authorization.Usage{ChatID: chatID, ExecutionID: executionID,
					WakerID: work.chat.WakerID, Title: work.chat.Title})
			}
			return work.revoke, nil
		},
		Committed: func(receipt agent.CommitReceipt) {
			if work.projected.Load() {
				work.journal.ObserveCommitted(chatID, receipt)
			} else {
				work.admissionReceipt.CompareAndSwap(nil, &receipt)
			}
		},
		Input: func(ctx context.Context, _ agent.AgentScope, input agent.AgentInputRequest) (agent.AgentInputResponse, error) {
			return nativeInput(ctx, m.elicitations.handler(chatID, executionID), executionID, input)
		},
	}
	if !work.input.runtime.WorkspaceReadOnly {
		ports.Approve = func(ctx context.Context, _ agent.AgentScope, request agent.AgentApproval) (string, error) {
			return nativeApproval(ctx, m.approvals.handler(chatID, executionID), executionID, request)
		}
	}
	return ports
}

func (work *nativeExecutionWork) projectAdmission(ctx context.Context, receipt agent.CommitReceipt) error {
	// The original admission is authoritative even if cancellation interrupted projection.
	work.files.commit()
	accepted, err := work.journal.ProjectTurnAdmission(ctx, work.admissionFacts, receipt)
	if err != nil {
		return err
	}
	work.chat, work.execution = accepted.Chat, accepted.Execution
	work.projected.Store(true)
	if work.recording != nil {
		if err := work.recording.PublishPending(ctx); err != nil {
			logx.Warnf("publish pending file result failed: chat_id=%s execution_id=%s error=%v", work.chat.ID, work.execution.ID, err)
		}
	}
	if !work.precommitted {
		work.started <- executionStartResult{chat: work.chat, execution: work.execution}
	}
	return nil
}

func nativeApproval(ctx context.Context, handler agent.ApprovalHandler, executionID string, request agent.AgentApproval) (string, error) {
	input := agent.ApprovalRequest{InvocationID: executionID, ToolCallID: request.Tool.ID}
	if request.Tool.Title != nil {
		input.Title = *request.Tool.Title
	}
	if request.Tool.Kind != nil {
		input.Kind = *request.Tool.Kind
	}
	for _, location := range request.Tool.Locations {
		input.Locations = append(input.Locations, agent.ApprovalLocation{Path: location.Path, Line: location.Line})
	}
	for _, option := range request.Options {
		input.Options = append(input.Options, agent.ApprovalOption{ID: option.ID, Name: option.Name, Kind: option.Kind})
	}
	decision, err := handler.Request(ctx, input)
	return decision.OptionID, err
}

func nativeInput(ctx context.Context, handler agent.ElicitationHandler, executionID string, input agent.AgentInputRequest) (agent.AgentInputResponse, error) {
	request := agent.ElicitationRequest{InvocationID: executionID, Mode: "form", Message: input.Message, URL: input.URL}
	if input.URL != "" {
		request.Mode = "url"
	} else if err := json.Unmarshal(input.Schema, &request.RequestedSchema); err != nil {
		return agent.AgentInputResponse{Action: "cancel"}, status_error.AgentInputInvalid
	}
	decision, err := handler.Request(ctx, request)
	response := agent.AgentInputResponse{Action: "cancel"}
	if decision.Accepted {
		response.Action, response.Content = "accept", decision.Content
	} else if decision.Declined {
		response.Action = "decline"
	}
	return response, err
}

func (m *service) runNativeExecution(ctx context.Context, work *nativeExecutionWork, turn *agent.Turn) {
	chatID, executionID := work.chat.ID, work.execution.ID
	finalize := context.WithoutCancel(ctx)
	var admitted bool
	defer func() {
		recording := work.recording
		if recording == nil {
			m.executions.freezeFileSnapshots(chatID, executionID)
			return
		}
		if !admitted && !work.precommitted {
			defer m.executions.freezeFileSnapshots(chatID, executionID)
			if err := recording.Discard(finalize, work.state.isNew); err != nil {
				logx.Warnf("discard unaccepted file result: chat_id=%s execution_id=%s error=%v", chatID, executionID, err)
			}
			return
		}
		if err := recording.Freeze(finalize); err != nil {
			_ = recording.Close()
			m.executions.freezeFileSnapshots(chatID, executionID)
			logx.Warnf("freeze native file result: chat_id=%s execution_id=%s error=%v", chatID, executionID, err)
			return
		}
		m.executions.freezeFileSnapshots(chatID, executionID)
		if err := recording.Finish(finalize); err != nil {
			logx.Warnf("finish native file result: chat_id=%s execution_id=%s error=%v", chatID, executionID, err)
		}
	}()
	defer func() {
		if err := turn.Close(finalize); err != nil {
			logx.Warnf("close native turn: execution_id=%s error=%v", executionID, err)
		}
	}()
	if err := m.executions.waitForFileSnapshots(ctx, ctx, chatID, executionID); err != nil {
		work.settle(m, agent.TurnResult{OperationError: err})
		return
	}
	if recorder, ok := m.journal.(chatservice.FileChangesRecorder); ok {
		var err error
		work.recording, err = recorder.BeginFileChanges(ctx, work.chat, executionID, work.state.threadWorkspaceID, work.state.projectSources)
		if err != nil {
			logx.Warnf("prepare native file result: chat_id=%s execution_id=%s error=%v", chatID, executionID, err)
		}
	}
	result := turn.Run(ctx)
	admitted = result.BusinessAdmitted
	work.settle(m, result)
}

func (work *nativeExecutionWork) settle(m *service, result agent.TurnResult) {
	if work.revoke != nil {
		work.revoke()
	}
	if work.resources.updateUsage != nil {
		work.resources.updateUsage()
	}
	m.elicitations.finishQuestions(work.chat.ID, work.execution.ID)
	if result.BusinessAdmitted && !work.projected.Load() {
		receipt := work.admissionReceipt.Load()
		if receipt == nil {
			result.StorageError = errors.Join(result.StorageError, status_error.ChatLogError)
		} else if err := work.projectAdmission(context.Background(), *receipt); err != nil {
			result.StorageError = errors.Join(result.StorageError, err)
		}
	}
	outcome := nativeExecutionOutcome(result)
	if !work.precommitted && !work.projected.Load() {
		// Reconcile the file journal before the caller is allowed to roll back a new Chat.
		if work.recording != nil && !result.BusinessAdmitted {
			_ = work.recording.Discard(context.Background(), work.state.isNew)
			work.recording = nil
		}
		work.started <- executionStartResult{err: outcome.err}
		if !result.BusinessAdmitted {
			return
		}
	}
	if work.execution.Status == constants.ExecutionStatusPending && result.BusinessAdmitted {
		work.execution.Status = constants.ExecutionStatusRunning
	}
	m.finishExecution(work.chat, work.execution, outcome.status, outcome.code, outcome.message)
}

// nativeExecutionStatus maps native settlement to the existing business status.
type nativeExecutionStatus struct {
	// status is the durable business outcome.
	status constants.ExecutionStatus
	// code is the stable public failure code.
	code string
	// message is a safe displayable failure description.
	message string
	// err is the admission failure returned to an unaccepted request.
	err error
}

func nativeExecutionOutcome(result agent.TurnResult) nativeExecutionStatus {
	out := nativeExecutionStatus{status: constants.ExecutionStatusFailed, err: status_error.RuntimeExecutionFailed}
	failure := errors.Join(result.StorageError, result.DeliveryError, result.CleanupError)
	if failure == nil && result.Outcome != nil {
		failure = result.Outcome.NativeFailure
		switch result.Outcome.Turn.Status {
		case acpmeta.NativeTurnCompleted:
			if failure == nil && result.OperationError == nil {
				return nativeExecutionStatus{status: constants.ExecutionStatusCompleted}
			}
		case acpmeta.NativeTurnCancelled:
			return nativeExecutionStatus{status: constants.ExecutionStatusCanceled, code: string(constants.RuntimeFailureCodeContextCanceled),
				message: context.Canceled.Error(), err: status_error.OperationCanceled}
		}
	}
	failure = errors.Join(failure, result.OperationError)
	if result.StorageError == nil && result.DeliveryError == nil && result.CleanupError == nil && errors.Is(failure, context.Canceled) {
		return nativeExecutionStatus{status: constants.ExecutionStatusCanceled, code: string(constants.RuntimeFailureCodeContextCanceled),
			message: context.Canceled.Error(), err: status_error.OperationCanceled}
	}
	var safe status_error.StatusError
	if errors.As(failure, &safe) {
		out.err = safe
	}
	out.code, out.message = string(constants.RuntimeFailureCodeExecutionFailed), out.err.Error()
	switch {
	case result.StorageError != nil || result.DeliveryError != nil:
		out.code = string(constants.RuntimeFailureCodeEventDelivery)
	case errors.Is(failure, status_error.RuntimeAuthRequired):
		out.code = string(constants.RuntimeFailureCodeAuthRequired)
	case errors.Is(failure, status_error.RuntimePolicyUnsupported):
		out.code = string(constants.RuntimeFailureCodePolicyUnsupported)
	case errors.Is(failure, status_error.RuntimeProtocolIncompatible):
		out.code = string(constants.RuntimeFailureCodeProtocolIncompatible)
	case errors.Is(failure, status_error.RuntimeUnavailable):
		out.code = string(constants.RuntimeFailureCodeRuntimeUnavailable)
	case errors.Is(failure, context.DeadlineExceeded):
		out.code = string(constants.RuntimeFailureCodeRequestTimeout)
	}
	if result.Outcome != nil && result.Outcome.NativeErrorKind == "rate_limit" &&
		result.StorageError == nil && result.DeliveryError == nil && result.CleanupError == nil {
		out.code = string(constants.RuntimeFailureCodeRateLimited)
	}
	return out
}
