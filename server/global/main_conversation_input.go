package global

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/projectsource"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
)

// MainConversationInputQueue 接纳主会话待路由和待来源接手的输入。
var MainConversationInputQueue *mainconversation.InputQueue

// NewMainConversationInputQueue 将输入队列接到当前 Core 的会话与执行模块。
func NewMainConversationInputQueue(store *controllers.MainConversationInputController, layout *chatstorage.Layout) *mainconversation.InputQueue {
	executions := ExecutionService
	chats := ChatService
	options := mainconversation.InputQueueOptions{
		Layout:                layout,
		ReadSubmission:        chats.ReadSubmission,
		Dispatch:              dispatchMainConversationInput,
		ReadRoutedPublication: MainConversationService.ReadRoutedPublication,
		Route:                 routeMainConversationInput,
		ConfirmBootAbsence: func(ctx context.Context, item models.MainConversationInput) (bool, error) {
			// Under the exclusive Core lock, a healthy source missing both keys proves that
			// the native gate never allowed the old host to send the original prompt.
			_, err := chats.ReadExecution(ctx, item.WorkChatID, item.ExecutionID)
			if util.HasStatusCode(err, status_error.ExecutionNotFound) {
				return true, nil
			}
			return false, err
		},
		FreezeWorkflow: func(ctx context.Context, selection *workinput.WorkflowReference) (*workinput.WorkflowReference, error) {
			if WorkflowService == nil {
				return nil, status_error.WorkflowCapabilityUnsupported
			}
			return WorkflowService.FreezeWorkflowSelection(ctx, selection)
		},
	}
	if core, ok := executions.(execution.Core); ok {
		options.WaitForIdle = core.WaitForChatIdle
	}
	return mainconversation.NewInputQueue(store, options)
}

// FreezeMainConversationInputOptions 在接纳时固定原来源的本地选择。
func FreezeMainConversationInputOptions(ctx context.Context, chatID string, options execution.WorkOptions) (models.MainConversationInputOptions, error) {
	detail, err := ChatService.LoadExecutionContext(ctx, chatID)
	if err != nil {
		return models.MainConversationInputOptions{}, err
	}
	if err := MainConversationService.AuthorizeWorkActor(ctx, detail.Chat); err != nil {
		return models.MainConversationInputOptions{}, err
	}
	options, err = execution.FreezeContinuationOptions(ctx, ExecutionService, detail.Chat, detail.LastExecutionID != "", options)
	if err != nil {
		return models.MainConversationInputOptions{}, err
	}
	return savedMainConversationOptions(options), nil
}

// FreezeMainConversationRoutingOptions 在路由输入接纳时固定伙伴的非凭据配置。
func FreezeMainConversationRoutingOptions(ctx context.Context, options execution.WorkOptions) (models.MainConversationInputOptions, error) {
	if options.PermissionLevel == "" {
		options.PermissionLevel = constants.PermissionLevelDefault
	}
	options, err := execution.FreezeContinuationOptions(ctx, ExecutionService, models.Chat{}, false, options)
	if err != nil {
		return models.MainConversationInputOptions{}, err
	}
	return savedMainConversationOptions(options), nil
}

func savedMainConversationOptions(options execution.WorkOptions) models.MainConversationInputOptions {
	return models.MainConversationInputOptions{
		AccountID: options.AccountID, RuntimeID: options.RuntimeID, Model: options.Model, Reasoning: options.Reasoning,
		Fast: options.Fast, ContextWindow: options.ContextWindow, PermissionLevel: options.PermissionLevel,
		ConfigurationResolved: options.ConfigurationResolved, SessionPolicy: options.SessionPolicy,
		HistorySourceChatID: options.HistorySourceChatID, HistoryTurns: options.HistoryTurns, HistoryMode: options.HistoryMode,
		SummaryModel: options.SummaryModel, SummaryReasoning: options.SummaryReasoning,
	}
}

func routeMainConversationInput(ctx context.Context, item models.MainConversationInput, recovering bool, mark func(context.Context) error, promote func(context.Context, models.MainConversationInput) error) error {
	conv, err := MainConversationControllerManager.Get(ctx, item.ConversationID)
	if err != nil {
		return err
	}
	options := queuedMainConversationOptions(item.Options)
	authorize := func(ctx context.Context) error {
		if err := MainConversationService.ValidateRouterActor(ctx, conv, item.RouterWakerID); err != nil {
			return err
		}
		return mark(ctx)
	}
	model := func(ctx context.Context, system, input string) (string, error) {
		return GenerateMainConversationRoute(ctx, conv.ID, item.RouterWakerID, options, system, input, recovering, authorize)
	}
	ctx = WithMainConversationRuleManagement(ctx, conv, item.RouterWakerID, options)
	return MainConversationService.ProcessRoutedInput(ctx, ProjectControllerManager, conv, item, model, freezeRoutedMainConversationSource, promote)
}

// FreezeMainConversationWorkScope 使用一次授权快照确定工作 Chat 的执行规格与复用范围。
func FreezeMainConversationWorkScope(ctx context.Context, actor, projectID, workingDirectory string) (*execution.CreateChatSpec, string, error) {
	source := &execution.CreateChatSpec{WakerID: actor, ProjectID: projectID}
	if projectID != "" {
		project, err := execution.CaptureMainProjectScope(ctx, ProjectControllerManager, TransactionControllerManager, actor, projectID)
		if err != nil {
			return nil, "", err
		}
		source.ProjectScope = project
		return source, "project:" + projectID + "@" + project.ScopeKey(), nil
	}
	if workingDirectory != "" {
		directory, err := projectsource.Resolve(workingDirectory)
		if err != nil {
			return nil, "", status_error.ProjectSourceUnavailable
		}
		source.WorkingDirectory = directory.Path
		return source, "directory:" + directory.Path, nil
	}
	return source, "managed", nil
}

func freezeRoutedMainConversationSource(ctx context.Context, actor, projectID, workingDirectory string) (models.MainConversationRoutedSource, error) {
	source, scope, err := FreezeMainConversationWorkScope(ctx, actor, projectID, workingDirectory)
	if err == nil && source.ProjectScope != nil {
		err = source.ProjectScope.ValidateAvailable()
	}
	if err != nil {
		return models.MainConversationRoutedSource{}, err
	}
	return models.MainConversationRoutedSource{ProjectID: source.ProjectID, WorkingDirectory: source.WorkingDirectory, ScopeKey: scope}, nil
}

func dispatchMainConversationInput(ctx context.Context, item models.MainConversationInput, mark func(context.Context) error) (models.Execution, error) {
	conv, err := MainConversationControllerManager.Get(ctx, item.ConversationID)
	if err != nil {
		return models.Execution{}, err
	}
	if item.EntryPoint == models.MainConversationTransferredReply {
		return dispatchTransferredMainConversationInput(ctx, conv, item, mark)
	}
	work, err := MainConversationService.Source(ctx, conv.SubjectType, conv.SubjectID, item.WorkChatID)
	if err != nil {
		return models.Execution{}, err
	}
	if work.ConversationID != item.ConversationID {
		return models.Execution{}, status_error.ChatNotFound
	}
	switch item.EntryPoint {
	case execution.MainConversationEntryPoint, execution.MainConversationSourceEntryPoint:
	default:
		return models.Execution{}, status_error.BadRequest
	}
	target := execution.ChatTarget{ChatID: item.WorkChatID}
	digest := item.RequestHash
	if item.RoutedSource != nil {
		digest = item.InputDigest
		if _, err := ChatService.LoadExecutionContext(ctx, item.WorkChatID); util.HasStatusCode(err, status_error.ChatNotFound) {
			plan := item.RoutedSource
			spec, scope, err := FreezeMainConversationWorkScope(ctx, work.WakerID, plan.ProjectID, plan.WorkingDirectory)
			if err != nil {
				return models.Execution{}, err
			}
			if scope != plan.ScopeKey || work.ScopeKey != mainconversation.SourceScopeKey(item.PublicMessageID, scope, plan.Shared) {
				return models.Execution{}, status_error.ProjectSourceUnavailable
			}
			target.MainConversationSource = spec
		} else if err != nil {
			return models.Execution{}, err
		}
	}
	options := queuedMainConversationOptions(item.Options)
	options.EntryPoint = item.EntryPoint
	if item.RoutedSource != nil && item.RoutedSource.Shared && item.Options.DefaultSourceModel != "" {
		options.Model, options.Reasoning = item.Options.DefaultSourceModel, item.Options.DefaultSourceReasoning
	}
	acknowledgment := ""
	if item.RoutedSource != nil && item.StartWork {
		acknowledgment = item.RoutedSource.Acknowledgment
		if acknowledgment != "" {
			item.Input.ContextInstructions = append(item.Input.ContextInstructions, mainconversation.DispatchAcknowledgmentInstruction())
		}
	}
	if item.StartWork && !item.PrivateSource {
		roster, rosterErr := MainConversationService.DelegationTargets(ctx, conv, work.WakerID)
		if rosterErr != nil {
			return models.Execution{}, rosterErr
		}
		if roster.Content != "" {
			item.Input.ContextInstructions = append(item.Input.ContextInstructions, roster)
		}
	}
	publicID := item.PublicMessageID
	if item.PrivateSource {
		if item.EntryPoint != execution.MainConversationSourceEntryPoint || item.StartWork {
			return models.Execution{}, status_error.BadRequest
		}
		publicID = ""
		item.Input.ContextInstructions = append(item.Input.ContextInstructions, mainconversation.PrivateSourceInstruction())
	}
	options.ToolCapabilities = execution.MainConversationTools(item.WorkChatID, item.ExecutionID, publicID, MainConversationService)
	intent := execution.WorkIntent{
		ExecutionID: item.ExecutionID, UserMessageID: item.SourceMessageID, Chat: target,
		Input: item.Input, Options: options, Admission: constants.ExecutionAdmissionCommitAtQueuedNativeGate,
		BeforeQueuedNativeCommit: mark,
		Ticket: execution.WorkTicketInput{
			Start: item.StartWork, Address: workticket.Address{Kind: constants.WorkTicketOriginMain, ID: item.ConversationID},
			OwnerKind: conv.SubjectType, OwnerID: conv.SubjectID, PublicMessageID: item.PublicMessageID,
			PublicReplyID: item.TicketReplyID, MainConversation: MainConversationService,
		},
		AdmissionBoundary: MainConversationService.WorkAdmission(conv.SubjectType, conv.SubjectID, work, item.RoutedSource != nil,
			func(ctx context.Context, commit func(context.Context) error) error {
				if item.PrivateSource {
					return commit(ctx)
				}
				_, err := MainConversationService.AdmitUser(ctx, conv.SubjectType, conv.SubjectID, item.Input.SubmissionID,
					item.PublicContent, item.PublicReplyID, item.WorkChatID, item.ExecutionID, item.SourceMessageID,
					item.RequestedDirectory, digest, commit)
				if err == nil {
					MainConversationService.AcknowledgeDispatch(ctx, item.WorkChatID, item.ExecutionID, publicID, acknowledgment)
				}
				return err
			}),
	}
	result, err := ExecutionService.Start(ctx, intent)
	return result.Execution, err
}

func queuedMainConversationOptions(selected models.MainConversationInputOptions) execution.WorkOptions {
	return execution.WorkOptions{
		AccountID: selected.AccountID, RuntimeID: selected.RuntimeID, Model: selected.Model,
		Reasoning: selected.Reasoning, Fast: selected.Fast, ContextWindow: selected.ContextWindow,
		PermissionLevel: selected.PermissionLevel, ConfigurationResolved: selected.ConfigurationResolved,
		SessionPolicy: selected.SessionPolicy, HistorySourceChatID: selected.HistorySourceChatID,
		HistoryTurns: selected.HistoryTurns, HistoryMode: selected.HistoryMode,
		SummaryModel: selected.SummaryModel, SummaryReasoning: selected.SummaryReasoning,
	}
}
