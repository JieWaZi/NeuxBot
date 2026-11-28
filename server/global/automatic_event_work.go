package global

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/eventsource"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/JieWaZi/NeuxBot/server/services/schedule"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
)

func bindAutomaticEventWork(ctx context.Context, run models.AutomaticRun, intent execution.WorkIntent) (execution.WorkIntent, error) {
	if run.SourceEvent == nil || run.Configuration.Event == nil || !run.Configuration.Event.TrackDiscussion ||
		run.Configuration.ChannelID != "" || run.Configuration.ReportDestination != constants.AutomaticReportDestinationMainConversation {
		return intent, nil
	}
	_, owner, err := WorkTicketService.ReceiveTopicEvent(ctx, run.TaskID, *run.SourceEvent, true)
	if err != nil || owner {
		// A fallback run retains the event and its reason without reclaiming an existing ticket.
		return intent, err
	}
	spec, _, err := FreezeMainConversationWorkScope(ctx, run.WakerID, run.Configuration.ProjectID, run.Configuration.WorkingDirectory)
	if err != nil {
		return intent, err
	}
	chatID := schedule.EventRunChatID(run)
	source, err := MainConversationService.ReserveExactWork(ctx, "waker", run.WakerID, run.WakerID, "event-run:"+run.ID, chatID)
	if err != nil {
		return intent, err
	}
	intent.ClientRequestID = ""
	intent.Chat = execution.ChatTarget{ChatID: chatID, MainConversationSource: spec}
	intent.Input.SubmissionID = run.ID
	intent.Options.EntryPoint = execution.MainConversationSourceEntryPoint
	intent.Ticket = execution.WorkTicketInput{
		Start: true, Address: workticket.Address{Kind: constants.WorkTicketOriginMain, ID: source.ConversationID},
		OwnerKind: "waker", OwnerID: run.WakerID, PublicMessageID: mainconversation.AutomaticReportMessageID(run.ID), MainConversation: MainConversationService,
		EventSubscription: &models.WorkTicketEventSubscription{RuleID: run.TaskID, Topic: run.SourceEvent.Topic, InitialEventID: run.SourceEvent.ID,
			Primary: true, ChecksAllowed: slices.Contains(run.Configuration.Event.Kinds, eventsource.ChecksPassed)},
	}
	return intent, nil
}

func followAutomaticEvent(ctx context.Context, task models.AutomaticTask, observation models.AutomaticEventObservation) (schedule.EventFollowUpResult, error) {
	result := schedule.EventFollowUpResult{}
	if task.Event == nil {
		return result, nil
	}
	deliveries, owner, err := WorkTicketService.ReceiveTopicEvent(ctx, task.ID, observation.Event, task.Event.TrackDiscussion)
	if err != nil {
		return result, err
	}
	result.Handled = owner
	for _, delivery := range deliveries {
		if delivery.ErrorCode != "" {
			result.ErrorCode, result.Handled = delivery.ErrorCode, false
			continue
		}
		if delivery.ExecutionID != "" {
			if delivery.ReadAt == nil {
				code, err := reconcileUnreadAutomaticEvent(ctx, delivery)
				if err != nil {
					return result, err
				}
				if code != "" {
					result.ErrorCode, result.Handled = code, false
				}
			}
			continue
		}
		if err := dispatchAutomaticEventFollowUp(ctx, task, delivery); err != nil {
			if errors.Is(err, status_error.ChatHasActiveExecution) || errors.Is(err, status_error.CreateRequestInProgress) {
				return result, status_error.AutomaticTaskBusy
			}
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			if errors.Is(err, status_error.DatabaseBusy) || errors.Is(err, status_error.DatabaseError) || errors.Is(err, status_error.ChatLogError) {
				return result, err
			}
			result.ErrorCode, result.Handled = util.ErrorCode(err, status_error.WorkTicketCannotContinue.Key()), false
			if err := WorkTicketService.SettleEventDelivery(ctx, delivery.ID, "", result.ErrorCode); err != nil {
				return result, err
			}
		} else {
			// Retain the source observation until the work either reads the event or settles with a reason.
			return result, status_error.AutomaticTaskBusy
		}
	}
	return result, nil
}

func reconcileUnreadAutomaticEvent(ctx context.Context, delivery models.WorkTicketEventDelivery) (string, error) {
	input, err := WorkTicketService.EventDeliveryInput(ctx, delivery)
	if util.HasStatusCode(err, status_error.WorkTicketAttemptUnavailable) {
		code := status_error.WorkTicketCannotContinue.Key()
		return code, WorkTicketService.RecoverUnreadEvent(ctx, delivery, false, code)
	}
	if err != nil {
		return "", err
	}
	if ExecutionService.IsExecutionActive(input.WorkChatID, input.ExecutionID) {
		return "", status_error.AutomaticTaskBusy
	}
	current, err := ChatService.ReadExecution(ctx, input.WorkChatID, input.ExecutionID)
	if util.HasStatusCode(err, status_error.ChatNotFound) || util.HasStatusCode(err, status_error.ExecutionNotFound) {
		code := status_error.WorkTicketCannotContinue.Key()
		return code, WorkTicketService.RecoverUnreadEvent(ctx, delivery, false, code)
	}
	if err != nil {
		return "", err
	}
	switch current.Status {
	case constants.ExecutionStatusPending, constants.ExecutionStatusRunning:
		return "", status_error.AutomaticTaskBusy
	case constants.ExecutionStatusCompleted:
		complete, _, err := AgentThreads.RecoverCompletedAnswer(ctx, input.WorkChatID, input.ExecutionID)
		if err != nil {
			return "", err
		}
		if complete {
			return "", WorkTicketService.MarkEventInputRead(ctx, input.ExecutionID)
		}
	}
	code := current.ErrorCode
	if code == "" {
		code = status_error.WorkTicketCannotContinue.Key()
	}
	restartBeforeSubmit := code == status_error.ReadingExecutionInterrupted.Key() && delivery.Retry == 0
	if restartBeforeSubmit {
		facts, err := ChatService.ReadExecutionRecords(input.WorkChatID, input.ExecutionID)
		if err != nil {
			return "", err
		}
		for _, fact := range facts {
			if fact.ExecutionID == input.ExecutionID && fact.Type == constants.ChatEventTypeExecutionStarted {
				restartBeforeSubmit = false
				break
			}
		}
	}
	if err := WorkTicketService.RecoverUnreadEvent(ctx, delivery, restartBeforeSubmit, code); err != nil {
		return "", err
	}
	if restartBeforeSubmit {
		return "", status_error.AutomaticTaskBusy
	}
	return code, nil
}

func resolveWorkTicketEventRule(ctx context.Context, ticket models.WorkTicket, ruleID, observationID string) (eventsource.Event, bool, error) {
	var empty eventsource.Event
	task, err := AutomaticTaskControllerManager.Get(ctx, ruleID)
	if err != nil {
		return empty, false, err
	}
	owner, ownerKind := task.WakerID, "waker"
	if task.ChannelID != "" {
		owner, ownerKind = task.ChannelID, "team"
	}
	if owner != ticket.OwnerID || ownerKind != ticket.OwnerKind || task.Trigger != constants.AutomaticTaskTriggerEvent ||
		task.Event == nil || !task.Enabled || task.Deleting {
		return empty, false, status_error.AgentOperationForbidden
	}
	checks := slices.Contains(task.Event.Kinds, eventsource.ChecksPassed)
	if observationID == "" {
		return empty, checks, nil
	}
	row, err := controllers.NewAutomaticEventController(DB).Observation(ctx, ruleID, observationID)
	if err != nil {
		return empty, false, err
	}
	repository := "github:" + task.Event.Repository + ":pull:"
	if row.ID == "" || !strings.HasPrefix(row.Event.Topic, repository) || !row.Event.Valid() ||
		!slices.Contains(task.Event.Kinds, row.Event.Kind) || !task.Event.Matches(row.Event) {
		return empty, false, status_error.AgentOperationForbidden
	}
	return row.Event, checks, nil
}

func dispatchAutomaticEventFollowUp(ctx context.Context, task models.AutomaticTask, delivery models.WorkTicketEventDelivery) error {
	ticket, err := WorkTicketService.EventDeliveryTarget(ctx, delivery)
	if err != nil {
		return err
	}
	if (task.ChannelID == "" && (ticket.OwnerKind != "waker" || ticket.OwnerID != task.WakerID)) ||
		(task.ChannelID != "" && (ticket.OwnerKind != "team" || ticket.OwnerID != task.ChannelID)) {
		return status_error.AgentOperationForbidden
	}
	chat, err := ChatService.LoadExecutionChat(ctx, ticket.WorkChatID)
	if err != nil {
		return err
	}
	if chat.ActiveExecutionID != "" || ExecutionService.IsExecutionActive(ticket.WorkChatID, ticket.CurrentExecutionID) {
		return status_error.ChatHasActiveExecution
	}
	previous, err := ChatService.ReadExecution(ctx, ticket.WorkChatID, ticket.CurrentExecutionID)
	if err != nil {
		return err
	}
	return execution.ResumeEventWork(ctx, ExecutionService, ticket, previous, delivery, MainConversationService)
}
