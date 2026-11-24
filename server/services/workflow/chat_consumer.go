package workflow

import (
	"context"
	"path/filepath"
	"slices"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// ChatRunSource 定位提交原消息及其答复，随运行接纳原子保存。
type ChatRunSource struct {
	// ChatID 是来源的真实会话。
	ChatID string
	// ExecutionID 是原回合的后端执行身份。
	ExecutionID string
	// MessageID 是当前输入消息的身份。
	MessageID string
	// ThreadID 是归属的团队线程，普通伙伴聊天为空。
	ThreadID string
	// WakerID 是调用工作流的伙伴。
	WakerID string
	// Name 是本次调用时使用的稳定标题。
	Name string
	// RunID 是完整重跑或重试所引用的源运行。
	RunID string
	// Workbench 标明用户在工作台内手动重试，复用展示锚点但不绑定原执行。
	Workbench bool
}

// FreezeWorkflowSelection 以发送接纳为边界读取当前可运行版本，已冻结队列保留原选择。
func (s *Service) FreezeWorkflowSelection(ctx context.Context, selected *workinput.WorkflowReference) (*workinput.WorkflowReference, error) {
	if selected == nil {
		return nil, nil
	}
	if selected.ID == "" || len(selected.ID) > 32 {
		return nil, status_error.BadRequest
	}
	if selected.RevisionID != "" {
		if _, err := s.store.GetRevision(ctx, selected.ID, selected.RevisionID); err != nil {
			return nil, err
		}
		return selected, nil
	}
	definition, err := s.store.GetDefinition(ctx, selected.ID)
	if err != nil {
		return nil, err
	}
	if definition.CurrentRevisionID == "" {
		return nil, status_error.WorkflowStateConflict.WithField("workflowId", selected.ID)
	}
	return &workinput.WorkflowReference{ID: definition.ID, RevisionID: definition.CurrentRevisionID, Name: definition.Title}, nil
}

// BindWorkflowTurn 为公开伙伴、团队负责人或成员的准确回合签发使用能力。
func (s *Service) BindWorkflowTurn(ctx context.Context, chat models.Chat, executionID string, selected *workinput.WorkflowReference) (execution.WorkflowTurnBinding, error) {
	if selected == nil {
		if chat.LatestSequence == 0 {
			return execution.WorkflowTurnBinding{}, nil
		}
		hasHistory, err := s.runs.HasChatRunSources(ctx, chat.ID)
		if err != nil || !hasHistory {
			return execution.WorkflowTurnBinding{}, nil
		}
	}
	bound, err := s.bindChatWorkflowTurn(ctx, chat, executionID, selected)
	if err != nil && selected == nil {
		return execution.WorkflowTurnBinding{}, nil
	}
	return bound, err
}

func (s *Service) bindChatWorkflowTurn(ctx context.Context, chat models.Chat, executionID string, selected *workinput.WorkflowReference) (execution.WorkflowTurnBinding, error) {
	var bound execution.WorkflowTurnBinding
	if s.consumers == nil {
		return bound, status_error.WorkflowCapabilityUnsupported
	}
	var threadID string
	if chat.HiddenFromWakerHistory {
		if s.channelThreads != nil {
			thread, found, err := s.channelThreads.FindByChatID(ctx, chat.ID)
			if err != nil {
				return bound, err
			}
			if found {
				threadID = thread.ID
			}
		}
		if threadID == "" {
			if selected != nil {
				return bound, status_error.WorkflowCapabilityUnsupported
			}
			return bound, nil
		}
	}
	reference, err := s.FreezeWorkflowSelection(ctx, selected)
	if err != nil {
		return bound, err
	}
	fingerprint, err := requestidempotency.New(executionID, []any{chat.ID, threadID, reference})
	if err != nil {
		return bound, err
	}
	lease, err := s.persistConsumer(ctx, models.WorkflowConsumerLease{ID: executionID, ChatID: chat.ID, Selection: reference, ThreadID: threadID, PayloadDigest: fingerprint.Hash})
	if err != nil {
		return bound, err
	}
	bound.Reference = lease.Selection
	bound.Capability = &execution.ToolCapability{ID: "workflow.consumer", Name: "工作流使用", Bindings: s.consumerAgentBindings(lease)}
	bound.DeveloperInstruction = agent.WorkflowConsumerInstructions
	return bound, nil
}

func chatRunScope(chat models.Chat) (models.WorkflowScopeBinding, []string, error) {
	var directories []string
	for _, candidate := range chat.AdditionalWorkingDirectories {
		if directory, err := workflowfiles.EnsureScopeDirectory(candidate); err == nil && directory != "" && directory == filepath.Clean(candidate) && !slices.Contains(directories, directory) {
			directories = append(directories, directory)
		}
	}
	if len(directories) > 0 {
		return models.WorkflowScopeBinding{ProjectID: chat.ProjectID, WorkingDirectory: directories[0]}, directories[1:], nil
	}
	if chat.WorkspaceDirectory != "" {
		if directory, err := workflowfiles.EnsureScopeDirectory(chat.WorkspaceDirectory); err == nil && directory == filepath.Clean(chat.WorkspaceDirectory) {
			return models.WorkflowScopeBinding{WorkingDirectory: directory}, nil, nil
		}
	}
	return models.WorkflowScopeBinding{}, nil, status_error.WorkflowResourceInvalid.WithField("field", "workingDirectory")
}

func consumerChatSource(lease models.WorkflowConsumerLease, actual models.Execution, chat models.Chat, name, sourceRunID string) *ChatRunSource {
	return &ChatRunSource{ChatID: lease.ChatID, ExecutionID: actual.ID, MessageID: actual.UserMessageID, ThreadID: lease.ThreadID, WakerID: chat.WakerID, Name: name, RunID: sourceRunID}
}

func (s *Service) validateChatRunAdmission(ctx context.Context, command StartRunCommand) error {
	origin := command.ChatSource
	if origin.ChatID == "" || origin.ExecutionID == "" || origin.MessageID == "" {
		return status_error.WorkflowStateConflict
	}
	if origin.RunID != "" {
		source, err := s.runs.GetRun(ctx, origin.RunID)
		if err != nil {
			return err
		}
		if source.HistoryDeletedAt != nil || source.SourceChatID != origin.ChatID || source.DefinitionID != command.DefinitionID || source.RevisionID != command.RevisionID {
			return status_error.WorkflowNotFound
		}
		if origin.Workbench && (source.SourceExecutionID != origin.ExecutionID || source.SourceMessageID != origin.MessageID) {
			return status_error.WorkflowStateConflict
		}
	}
	if origin.Workbench {
		return nil
	}
	if s.consumers == nil {
		return status_error.WorkflowStateConflict
	}
	var lease models.WorkflowConsumerLease
	var err error
	if command.localConsumer != nil {
		candidate := *command.localConsumer
		fingerprint, fingerprintErr := requestidempotency.New(candidate.ID, []any{candidate.ChatID, candidate.Selection})
		if fingerprintErr != nil {
			return fingerprintErr
		}
		candidate.PayloadDigest = fingerprint.Hash
		lease, err = s.persistConsumer(ctx, candidate)
	} else {
		lease, err = s.consumers.GetConsumer(ctx, origin.ExecutionID)
	}
	if err != nil {
		return err
	}
	if lease.ID != command.ConsumerLeaseID || lease.ChatID != origin.ChatID || lease.ThreadID != origin.ThreadID || lease.StoppedAt != nil {
		return status_error.WorkflowStateConflict
	}
	if lease.ThreadID != "" {
		if err := s.ensureTeamWorkflowWritable(ctx, lease.ThreadID); err != nil {
			return err
		}
	}
	if lease.Selection != nil && (origin.RunID != "" || lease.Selection.ID != command.DefinitionID || lease.Selection.RevisionID != command.RevisionID) || lease.Selection == nil && origin.RunID == "" {
		return status_error.AgentOperationForbidden
	}
	previous, err := s.runs.ListChatRunSources(ctx, origin.ChatID, []string{origin.ExecutionID})
	if err != nil {
		return err
	}
	for _, run := range previous {
		if !run.SourceWorkbench {
			return status_error.WorkflowRequestConflict
		}
	}
	return nil
}

func (s *Service) repeatChatRun(ctx context.Context, lease models.WorkflowConsumerLease, actual models.Execution, chat models.Chat, source models.WorkflowRun, requestID string, local bool) (models.WorkflowRun, error) {
	if requestID == "" || len(requestID) > 128 {
		return models.WorkflowRun{}, status_error.BadRequest
	}
	directory := source.ScopeBinding.WorkingDirectory
	if directory == "" || directory != chat.WorkspaceDirectory && !slices.Contains(chat.AdditionalWorkingDirectories, directory) {
		return models.WorkflowRun{}, status_error.WorkflowResourceInvalid.WithField("field", "workingDirectory")
	}
	canonical, err := workflowfiles.EnsureScopeDirectory(directory)
	if err != nil || canonical != directory {
		return models.WorkflowRun{}, status_error.WorkflowResourceInvalid.WithField("field", "workingDirectory")
	}
	for _, root := range source.AdditionalWorkingDirectories {
		if !slices.Contains(chat.AdditionalWorkingDirectories, root) {
			return models.WorkflowRun{}, status_error.WorkflowResourceInvalid.WithField("field", "workingDirectory")
		}
	}
	command := StartRunCommand{DefinitionID: source.DefinitionID, RevisionID: source.RevisionID, Args: source.Args,
		Defaults: constrainWorkflowDefaults(source.Defaults, workflowExecutionDefaults(actual)), WorkerConfigurations: source.WorkerConfigurations, ApprovedScope: &source.ScopeBinding,
		RequestID: requestID, AutomaticRunID: actual.AutomaticRunID, ConsumerLeaseID: lease.ID,
		ChatSource: consumerChatSource(lease, actual, chat, source.SourceName, source.ID), ExpectedSourceVersion: source.Version,
	}
	if local {
		command.localConsumer = &lease
	}
	if source.Status == constants.WorkflowRunStatusSucceeded {
		command.ReplaySourceRunID = source.ID
		command.TriggerSource = "agent"
	} else {
		command.RetrySourceRunID = source.ID
	}
	return s.StartRun(ctx, command)
}

func constrainWorkflowDefaults(defaults, upperBound models.WorkflowExecutionDefaults) models.WorkflowExecutionDefaults {
	if workflowPermissions(upperBound.PermissionLevel) < workflowPermissions(defaults.PermissionLevel) {
		defaults.PermissionLevel = upperBound.PermissionLevel
	}
	defaults.WorkspaceReadOnly = defaults.WorkspaceReadOnly || upperBound.WorkspaceReadOnly
	return defaults
}

func workflowPermissions(level constants.PermissionLevel) int {
	switch level {
	case constants.PermissionLevelFullAccess:
		return 2
	case constants.PermissionLevelAuto:
		return 1
	default:
		return 0
	}
}

// StopChatWorkflowTurn 撤销确切回合的使用权限，并停止该回合已经接纳的运行。
func (s *Service) StopChatWorkflowTurn(ctx context.Context, chatID, executionID string) error {
	if executionID == "" || s.runs == nil || s.consumers == nil {
		return nil
	}
	if err := s.consumers.StopConsumer(ctx, chatID, executionID, time.Now().UnixMilli()); err != nil {
		return err
	}
	runs, err := s.runs.ListChatRunSources(ctx, chatID, []string{executionID})
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.SourceWorkbench || workflowstate.TerminalRun(run.Status) || run.Status == constants.WorkflowRunStatusCancelling {
			continue
		}
		if err := s.CancelRun(ctx, run.ID); err != nil {
			return err
		}
	}
	return nil
}
