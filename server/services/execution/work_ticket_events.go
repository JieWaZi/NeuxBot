package execution

import (
	"context"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/eventsource"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
)

// WorkTicketEventRuleResolver 核验当前主体能否引用规则已经观察的来源讨论。
type WorkTicketEventRuleResolver func(context.Context, models.WorkTicket, string, string) (eventsource.Event, bool, error)

// ResumeEventWork 提交已经登记的原工单事件，复用原执行选择、范围和权威接纳。
func ResumeEventWork(ctx context.Context, core Service, ticket models.WorkTicket, previous models.Execution,
	delivery models.WorkTicketEventDelivery, main MainConversationMessages,
) error {
	if ticket.ID != delivery.TicketID || ticket.CurrentExecutionID != previous.ID || ticket.WorkChatID != previous.ChatID {
		return status_error.WorkTicketAttemptUnavailable
	}
	options := WorkOptions{EntryPoint: WorkbenchEntryPoint, RuntimeID: previous.RuntimeID, AccountID: previous.AccountID,
		Model: previous.Model, Reasoning: previous.Reasoning, Fast: previous.Fast, ContextWindow: previous.ContextWindow,
		ConfigurationResolved: true, PermissionLevel: previous.PermissionLevel, WorkspaceReadOnly: previous.WorkspaceReadOnly}
	if ticket.OriginKind == constants.WorkTicketOriginMain {
		options.EntryPoint = MainConversationSourceEntryPoint
	} else {
		main = nil
	}
	content := "Update the original work from the verified source event. Deliver a useful result without changing the external source."
	if delivery.WaitID != "" {
		content = "The source event for the original wait has arrived. Continue the original work using the verified event."
	}
	body, err := json.Marshal(delivery.Event)
	if err != nil {
		return status_error.AgentInputInvalid
	}
	executionID := workticket.EventDispatchIdentity(delivery)
	accepted, err := core.Start(ctx, WorkIntent{
		Chat: ChatTarget{ChatID: ticket.WorkChatID}, Options: options,
		Input: WorkInput{Actor: constants.WorkTicketHostActor, SubmissionID: executionID, Content: content,
			ContextInstructions: []workinput.Instruction{{Name: "Verified source event", Content: string(body)}}},
		ExecutionID: executionID, UserMessageID: collaborationSourceInputID(executionID, ticket.WorkChatID),
		Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
		Ticket: WorkTicketInput{ResumeTicket: ticket, EventDelivery: &delivery,
			Address:   workticket.Address{Kind: ticket.OriginKind, ID: ticket.OriginID},
			OwnerKind: ticket.OwnerKind, OwnerID: ticket.OwnerID, PublicMessageID: executionID, MainConversation: main},
	})
	if accepted.Execution.ID == executionID {
		return nil
	}
	return err
}

func (turn *workTicketTurn) eventWaitBinding() capability.Binding {
	return capability.Binding{Operation: constants.WorkTicketEventWaitOperation,
		Call: func(ctx context.Context, request capability.Request) (capability.Result, error) {
			kind, _ := request.Params["kind"].(string)
			wait, err := turn.tickets.WaitForTopicEvent(ctx, turn.binding, request.RequestID, kind, func(ctx context.Context, subscription models.WorkTicketEventSubscription) error {
				if turn.resolveEventRule == nil {
					return status_error.AgentOperationForbidden
				}
				_, canWait, err := turn.resolveEventRule(ctx, turn.binding.Ticket, subscription.RuleID, "")
				if err == nil && !canWait {
					err = status_error.AgentOperationForbidden
				}
				return err
			})
			return capability.Result{CommandID: wait.ID, Data: wait}, err
		},
		Receipt: func(ctx context.Context, requestID string) (capability.Result, error) {
			wait, err := turn.tickets.EventWaitReceipt(ctx, turn.binding, requestID)
			return capability.Result{CommandID: wait.ID, Data: wait, Replayed: true}, err
		},
	}
}

func (turn *workTicketTurn) eventSubscriptionBinding() capability.Binding {
	return capability.Binding{Operation: constants.WorkTicketEventSubscribeOperation,
		Call: func(ctx context.Context, request capability.Request) (capability.Result, error) {
			ruleID, _ := request.Params["ruleId"].(string)
			observationID, _ := request.Params["observationId"].(string)
			event, canWait, err := turn.resolveEventRule(ctx, turn.binding.Ticket, ruleID, observationID)
			if err != nil {
				return capability.Result{}, err
			}
			if err := turn.tickets.SubscribeEventTopic(ctx, turn.binding, ruleID, request.RequestID, event, false, canWait); err != nil {
				return capability.Result{}, err
			}
			return capability.Result{CommandID: turn.binding.Ticket.ID,
				Data: map[string]any{"topic": event.Topic}}, nil
		},
		Receipt: func(ctx context.Context, requestID string) (capability.Result, error) {
			subscription, err := turn.tickets.EventSubscriptionReceipt(ctx, turn.binding, requestID)
			return capability.Result{CommandID: subscription.TicketID, Replayed: true, Data: map[string]any{"topic": subscription.Topic}}, err
		},
	}
}
