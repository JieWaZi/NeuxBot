package workticket

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/eventsource"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// SubscribeEventTopic 将已经授权的来源讨论与执行中的工单关联。
func (s *Service) SubscribeEventTopic(ctx context.Context, binding Binding, ruleID, requestID string, event eventsource.Event, primary, checksAllowed bool) error {
	if ruleID == "" || !event.Valid() {
		return status_error.BadRequest
	}
	defer s.lockSettlement(binding.Ticket.ID)()
	return s.tx.Transaction(ctx, func(tx context.Context) error {
		input, err := s.store.Input(tx, binding.ExecutionID)
		if err != nil {
			return err
		}
		if _, err := s.reportTarget(tx, binding, input, false); err != nil {
			return err
		}
		return s.confirmEventSubscription(tx, models.WorkTicketEventSubscription{
			TicketID: binding.Ticket.ID, RuleID: ruleID, Topic: event.Topic, InitialEventID: event.ID,
			Primary: primary, ChecksAllowed: checksAllowed, RequestID: requestID,
		})
	})
}

func (s *Service) confirmEventSubscription(ctx context.Context, sub models.WorkTicketEventSubscription) error {
	current, err := s.store.EventSubscription(ctx, sub.TicketID)
	if err != nil || current == sub {
		return err
	}
	if current.TicketID != "" {
		return status_error.CreateRequestConflict
	}
	if sub.Primary {
		subs, err := s.store.EventTopicSubscriptions(ctx, sub.RuleID, sub.Topic)
		if err != nil {
			return err
		}
		for _, existing := range subs {
			if existing.Primary {
				return status_error.WorkTicketCannotContinue
			}
		}
	}
	return s.store.CreateEventSubscription(ctx, sub)
}

// WaitForTopicEvent 登记同一工作回合对来源动态的单次等待。
func (s *Service) WaitForTopicEvent(ctx context.Context, binding Binding, requestID, kind string, authorize func(context.Context, models.WorkTicketEventSubscription) error) (wait models.WorkTicketEventWait, err error) {
	if strings.TrimSpace(requestID) == "" || len(requestID) > 64 || kind != eventsource.ChecksPassed {
		return wait, status_error.AgentInputInvalid
	}
	defer s.lockSettlement(binding.Ticket.ID)()
	id := (requestidempotency.Identity{ID: requestID}).ResourceID(constants.WorkTicketEventWaitOperation, binding.ExecutionID)
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		wait, err = s.store.EventWait(tx, id)
		if err != nil {
			return err
		}
		if wait.ID != "" {
			if wait.Kind == kind && wait.TicketID == binding.Ticket.ID && wait.ExecutionID == binding.ExecutionID {
				return nil
			}
			return status_error.AgentCommandRequestConflict
		}
		sub, err := s.store.EventSubscription(tx, binding.Ticket.ID)
		if err != nil {
			return err
		}
		if sub.TicketID == "" || (authorize == nil && !sub.ChecksAllowed) {
			return status_error.AgentOperationForbidden
		}
		if authorize != nil {
			if err := authorize(tx, sub); err != nil {
				return err
			}
		}
		input, err := s.store.Input(tx, binding.ExecutionID)
		if err != nil {
			return err
		}
		ticket, err := s.reportTarget(tx, binding, input, false)
		if err != nil {
			return err
		}
		existing, err := s.store.ActiveEventWait(tx, ticket)
		if err != nil {
			return err
		}
		if existing.ID != "" {
			return status_error.CreateRequestConflict
		}
		wait = models.WorkTicketEventWait{ID: id, TicketID: ticket.ID, ExecutionID: binding.ExecutionID, Generation: binding.Generation, Kind: kind}
		return s.store.CreateEventWait(tx, wait)
	})
	return wait, err
}

// EventSubscriptionReceipt 恢复当前工单已经提交的精确订阅请求。
func (s *Service) EventSubscriptionReceipt(ctx context.Context, binding Binding, id string) (models.WorkTicketEventSubscription, error) {
	row, err := s.store.EventSubscription(ctx, binding.Ticket.ID)
	if err == nil && (id == "" || row.TicketID == "" || row.RequestID != id) {
		err = status_error.AgentReceiptNotFound
	}
	return row, err
}

// EventWaitReceipt 恢复原执行已经提交的精确事件等待。
func (s *Service) EventWaitReceipt(ctx context.Context, binding Binding, requestID string) (models.WorkTicketEventWait, error) {
	id := (requestidempotency.Identity{ID: requestID}).ResourceID(constants.WorkTicketEventWaitOperation, binding.ExecutionID)
	row, err := s.store.EventWait(ctx, id)
	if err == nil && (row.ID == "" || row.TicketID != binding.Ticket.ID || row.ExecutionID != binding.ExecutionID) {
		err = status_error.AgentReceiptNotFound
	}
	return row, err
}

// EventWaitAvailable reports whether this ticket has authority to await passing checks.
func (s *Service) EventWaitAvailable(ctx context.Context, ticketID string) (bool, error) {
	row, err := s.store.EventSubscription(ctx, ticketID)
	return row.TicketID != "", err
}

// ReceiveTopicEvent 先接纳对该讨论的所有订阅，每单和事件只形成一个持久投递。
func (s *Service) ReceiveTopicEvent(ctx context.Context, ruleID string, event eventsource.Event, followPrimary bool) (deliveries []models.WorkTicketEventDelivery, owner bool, err error) {
	if ruleID == "" || !event.Valid() {
		return nil, false, status_error.BadRequest
	}
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		deliveries, owner = nil, false
		subs, err := s.store.EventTopicSubscriptions(tx, ruleID, event.Topic)
		if err != nil {
			return err
		}
		for _, sub := range subs {
			if sub.Primary && !followPrimary {
				continue
			}
			owner = owner || sub.Primary
			if sub.InitialEventID == event.ID {
				continue
			}
			id := (requestidempotency.Identity{ID: event.ID}).ResourceID("work-ticket-event", sub.TicketID)
			delivery, err := s.store.EventDelivery(tx, id)
			if err != nil {
				return err
			}
			if delivery.ID != "" {
				deliveries = append(deliveries, delivery)
				continue
			}
			ticket, err := s.store.Ticket(tx, sub.TicketID)
			if err != nil {
				return err
			}
			delivery = models.WorkTicketEventDelivery{ID: id, TicketID: sub.TicketID, RuleID: ruleID, Event: event}
			if ticket.ID == "" || ticket.Status == constants.WorkTicketStatusStopped || ticket.Status == constants.WorkTicketStatusCompleted {
				delivery.ErrorCode = status_error.WorkTicketCannotContinue.Key()
			} else {
				wait, err := s.store.ActiveEventWait(tx, ticket)
				if err != nil {
					return err
				}
				if wait.ID != "" && (wait.Kind != event.Kind || wait.WakeDeliveryID != "") {
					continue
				}
				if wait.ID != "" {
					delivery.WaitID = wait.ID
					if err := s.store.WakeEventWait(tx, wait.ID, id); err != nil {
						return err
					}
				}
			}
			if err := s.store.CreateEventDelivery(tx, delivery); err != nil {
				return err
			}
			deliveries = append(deliveries, delivery)
		}
		return nil
	})
	return
}

// EventDeliveryTarget 核对待投事件仍能交还原工作和当前授权尝试。
func (s *Service) EventDeliveryTarget(ctx context.Context, delivery models.WorkTicketEventDelivery) (ticket models.WorkTicket, err error) {
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		stored, err := s.store.EventDelivery(tx, delivery.ID)
		if err != nil {
			return err
		}
		if stored.ID == "" || stored.TicketID != delivery.TicketID || stored.ExecutionID != "" || stored.ErrorCode != "" {
			return status_error.WorkTicketAttemptUnavailable
		}
		ticket, err = s.store.Ticket(tx, delivery.TicketID)
		if err != nil {
			return err
		}
		if ticket.ID == "" || ticket.Transferred || ticket.Status == constants.WorkTicketStatusStopped || ticket.Status == constants.WorkTicketStatusCompleted {
			return status_error.WorkTicketCannotContinue
		}
		if delivery.RecoveryAfterExecutionID != "" && ticket.CurrentExecutionID != delivery.RecoveryAfterExecutionID {
			return status_error.WorkTicketAttemptUnavailable
		}
		if delivery.WaitID != "" {
			wait, err := s.store.EventWait(tx, delivery.WaitID)
			if err != nil {
				return err
			}
			if wait.TicketID != ticket.ID || wait.WakeDeliveryID != delivery.ID ||
				(delivery.Retry == 0 && (wait.ExecutionID != ticket.CurrentExecutionID || wait.Generation != ticket.Generation)) {
				return status_error.WorkTicketAttemptUnavailable
			}
		}
		return nil
	})
	return ticket, err
}

// SettleEventDelivery 记录原会话确认接纳的执行，或留给例行运行的回退原因。
func (s *Service) SettleEventDelivery(ctx context.Context, id, executionID, code string) error {
	return s.store.SettleEventDelivery(ctx, id, executionID, code)
}

// EventDispatchIdentity 派生当前原生执行，事件自身的投递身份在恢复时不变。
func EventDispatchIdentity(delivery models.WorkTicketEventDelivery) string {
	if delivery.Retry == 0 {
		return delivery.ID
	}
	return (requestidempotency.Identity{ID: delivery.ID}).ResourceID("work-ticket-event-recovery", strconv.Itoa(delivery.Retry))
}

// EventDeliveryForExecution 通过权威输入绑定取得本轮已经接纳的事件。
func (s *Service) EventDeliveryForExecution(ctx context.Context, executionID string) (*models.WorkTicketEventDelivery, error) {
	input, err := s.store.Input(ctx, executionID)
	if err != nil || input.EventDeliveryID == "" {
		return nil, err
	}
	delivery, err := s.store.EventDelivery(ctx, input.EventDeliveryID)
	if err != nil || delivery.ExecutionID != executionID {
		return nil, err
	}
	return &delivery, nil
}

// EventDeliveryInput 读取原事件派发后的实际接纳归属，移交后仍定位旧回执。
func (s *Service) EventDeliveryInput(ctx context.Context, delivery models.WorkTicketEventDelivery) (models.WorkTicketInput, error) {
	input, err := s.store.Input(ctx, delivery.ExecutionID)
	if err == nil && (input.ExecutionID == "" || input.EventDeliveryID != delivery.ID || input.ConfirmedAt == nil) {
		err = status_error.WorkTicketAttemptUnavailable
	}
	return input, err
}

// RecoverUnreadEvent 仅允许恢复已确认未派发的原事件，未知结果留可查的失败原因。
func (s *Service) RecoverUnreadEvent(ctx context.Context, delivery models.WorkTicketEventDelivery, provablyUnsent bool, code string) error {
	defer s.lockSettlement(delivery.TicketID)()
	return s.tx.Transaction(ctx, func(tx context.Context) error {
		ticket, err := s.store.Ticket(tx, delivery.TicketID)
		if err != nil {
			return err
		}
		retry := provablyUnsent && delivery.Retry == 0 && ticket.CurrentExecutionID == delivery.ExecutionID &&
			ticket.Status != constants.WorkTicketStatusStopped && ticket.Status != constants.WorkTicketStatusCompleted && !ticket.Transferred
		if retry {
			code = ""
		}
		return s.store.RecoverEventDelivery(tx, delivery, retry, code)
	})
}

// MarkEventInputRead 由持有原生消耗证据的宿主确认已读。
func (s *Service) MarkEventInputRead(ctx context.Context, executionID string) error {
	return s.store.MarkEventInputRead(ctx, executionID, time.Now().UnixMilli())
}
