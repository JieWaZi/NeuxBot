package workticket_test

import (
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/eventsource"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
)

func TestEventDiscussionFansOutOnceToEachTicket(t *testing.T) {
	db, service, address := newTicketChat(t, nil)
	root := admit(t, service, ticketInput(address, "follow-pr-42"))
	contextOnly := admit(t, service, ticketInput(address, "reference-pr-42"))
	first := sourceEvent("opened-42", "org/repo#42", eventsource.PullOpened)
	if err := service.SubscribeEventTopic(t.Context(), root, "rule", "", first, true, true); err != nil {
		t.Fatal(err)
	}
	if err := service.SubscribeEventTopic(t.Context(), contextOnly, "rule", "context", first, false, true); err != nil {
		t.Fatal(err)
	}
	if deliveries, owner, err := service.ReceiveTopicEvent(t.Context(), "rule", first, true); err != nil || len(deliveries) != 0 || !owner {
		t.Fatalf("initial event replay dispatched again: %+v %v %v", deliveries, owner, err)
	}
	contextChange := sourceEvent("review-context-only", first.Topic, eventsource.PullReviewed)
	if deliveries, owner, err := service.ReceiveTopicEvent(t.Context(), "rule", contextChange, false); err != nil || len(deliveries) != 1 ||
		deliveries[0].TicketID != contextOnly.Ticket.ID || owner {
		t.Fatalf("context-only subscriptions must not claim responsibility: %+v %v %v", deliveries, owner, err)
	}
	review := sourceEvent("review-100", first.Topic, eventsource.PullReviewed)
	deliveries, owner, err := service.ReceiveTopicEvent(t.Context(), "rule", review, true)
	if err != nil || !owner || len(deliveries) != 2 || deliveries[0].ID == deliveries[1].ID {
		t.Fatalf("per-ticket event fan-out = %+v %v %v", deliveries, owner, err)
	}
	// The queue and wait identity survive reconstruction of the service.
	service = ticketService(db, nil)
	replay, _, err := service.ReceiveTopicEvent(t.Context(), "rule", review, true)
	if err != nil || len(replay) != 2 || replay[0].ID != deliveries[0].ID || replay[1].ID != deliveries[1].ID {
		t.Fatalf("replayed delivery identities = %+v %v", replay, err)
	}
	other := sourceEvent("review-101", "org/another#42", eventsource.PullReviewed)
	if deliveries, owner, err := service.ReceiveTopicEvent(t.Context(), "rule", other, true); err != nil || len(deliveries) != 0 || owner {
		t.Fatalf("matched a PR number from another repo: %+v %v %v", deliveries, owner, err)
	}
}

func TestEventWaitWakesOnlyOnFirstMatchingChange(t *testing.T) {
	_, service, address := newTicketChat(t, nil)
	binding := admit(t, service, ticketInput(address, "await-ci"))
	initial := sourceEvent("opened-ci", "org/repo#42", eventsource.PullOpened)
	if err := service.SubscribeEventTopic(t.Context(), binding, "rule", "", initial, true, true); err != nil {
		t.Fatal(err)
	}
	wait, err := service.WaitForTopicEvent(t.Context(), binding, "wait-for-ci", eventsource.ChecksPassed, nil)
	if err != nil {
		t.Fatal(err)
	}
	failed := sourceEvent("review-not-ci", initial.Topic, eventsource.PullReviewed)
	if deliveries, owner, err := service.ReceiveTopicEvent(t.Context(), "rule", failed, true); err != nil || !owner || len(deliveries) != 0 {
		t.Fatalf("nonmatching event woke original work: %+v %v %v", deliveries, owner, err)
	}
	passed := sourceEvent("checks-passed", initial.Topic, eventsource.ChecksPassed)
	for range 2 {
		deliveries, owner, err := service.ReceiveTopicEvent(t.Context(), "rule", passed, true)
		if err != nil || !owner || len(deliveries) != 1 || deliveries[0].WaitID != wait.ID || deliveries[0].ExecutionID != "" {
			t.Fatalf("matched wait delivery: %+v %v %v", deliveries, owner, err)
		}
	}
	if _, err := service.Stop(t.Context(), address, binding.Ticket.ID); err != nil {
		t.Fatal(err)
	}
	later := sourceEvent("checks-passed-after-stop", initial.Topic, eventsource.ChecksPassed)
	deliveries, _, err := service.ReceiveTopicEvent(t.Context(), "rule", later, true)
	if err != nil || len(deliveries) != 1 || deliveries[0].ExecutionID != "" || deliveries[0].ErrorCode == "" {
		t.Fatalf("manually stopped work did not retain an explicit fallback: %+v %v", deliveries, err)
	}
}

func sourceEvent(id, topic, kind string) eventsource.Event {
	return eventsource.Event{ID: id, Topic: topic, Kind: kind, URL: "https://github.com/org/repo/pull/42", Title: "Fixture"}
}

func TestEventDeliveryRecoveryKeepsOriginalIdentity(t *testing.T) {
	db, service, address := newTicketChat(t, nil)
	ctx := t.Context()
	first := ticketInput(address, "event-root")
	binding := admit(t, service, first)
	opened := sourceEvent("opened", "org/repo#42", eventsource.PullOpened)
	if err := service.SubscribeEventTopic(ctx, binding, "rule", "", opened, true, true); err != nil {
		t.Fatal(err)
	}
	if err := service.FinishTurn(ctx, binding, constants.ExecutionStatusCompleted); err != nil {
		t.Fatal(err)
	}
	review := sourceEvent("review-recovery", opened.Topic, eventsource.PullReviewed)
	readDelivery := func() models.WorkTicketEventDelivery {
		t.Helper()
		rows, owner, err := service.ReceiveTopicEvent(ctx, "rule", review, true)
		if err != nil || !owner || len(rows) != 1 {
			t.Fatalf("restore event delivery: %+v %v %v", rows, owner, err)
		}
		return rows[0]
	}
	firstDelivery := readDelivery()
	admitEvent := func(delivery models.WorkTicketEventDelivery, previous string) workticket.Binding {
		t.Helper()
		id := workticket.EventDispatchIdentity(delivery)
		next := first
		next.TicketID, next.EventDeliveryID, next.EventPreviousExecutionID = binding.Ticket.ID, delivery.ID, previous
		next.ExecutionID, next.MessageID, next.SourceMessageID, next.SubmissionID = id, id, id, id
		return admit(t, service, next)
	}
	accepted := admitEvent(firstDelivery, binding.ExecutionID)
	durable := readDelivery()
	if durable.ExecutionID != accepted.ExecutionID || durable.ReadAt != nil {
		t.Fatalf("durable delivery and native read were conflated: %+v", durable)
	}
	if err := service.FinishTurn(ctx, accepted, constants.ExecutionStatusFailed); err != nil {
		t.Fatal(err)
	}
	service = ticketService(db, nil)
	if err := service.RecoverUnreadEvent(ctx, durable, true, "Interrupted"); err != nil {
		t.Fatal(err)
	}
	recovered := readDelivery()
	if recovered.ID != durable.ID || recovered.ExecutionID != "" || recovered.RecoveryAfterExecutionID != durable.ExecutionID ||
		workticket.EventDispatchIdentity(recovered) == accepted.ExecutionID {
		t.Fatalf("retry changed the event rather than renewing its proven unsent dispatch: %+v", recovered)
	}
	retry := admitEvent(recovered, accepted.ExecutionID)
	if err := service.MarkEventInputRead(ctx, retry.ExecutionID); err != nil {
		t.Fatal(err)
	}
	if got := readDelivery(); got.ID != durable.ID || got.ExecutionID != retry.ExecutionID || got.ReadAt == nil {
		t.Fatalf("lost read-in evidence for the original event: %+v", got)
	}
}
