package execution

import (
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/eventsource"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestTrackedEventRunsInOriginalNativeWorkAndReportsOnce(t *testing.T) {
	ctx := t.Context()
	manager := newTicketExecutionFixture(t)
	core := manager.execution.(*service)
	publisher := mainconversation.New(controllers.NewMainConversationController(manager.db),
		controllers.NewTransactionController(dbhelper.NewGormTransactionManager(manager.db)), manager.wakers, controllers.NewChannelController(manager.db))
	source, err := publisher.ReserveExactWork(ctx, "waker", "waker-1", "waker-1", "tracked-event", "00000000000000000000000000000141")
	if err != nil {
		t.Fatal(err)
	}
	address := workticket.Address{Kind: constants.WorkTicketOriginMain, ID: source.ConversationID}
	opened := eventsource.Event{ID: "opened:42", Topic: "github:demo/repo:pull:42", Kind: eventsource.PullOpened,
		Title: "Initial review", URL: "https://github.com/demo/repo/pull/42"}
	runID := "00000000000000000000000000000142"
	first, err := core.Start(ctx, WorkIntent{
		AutomaticRunID: runID, ExecutionID: "00000000000000000000000000000143", Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
		Chat:    ChatTarget{ChatID: source.WorkChatID, MainConversationSource: &CreateChatSpec{WakerID: "waker-1"}},
		Input:   WorkInput{Actor: constants.AutomaticTaskActor, SubmissionID: runID, Content: "Review PR 42."},
		Options: WorkOptions{EntryPoint: MainConversationSourceEntryPoint, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		Ticket: WorkTicketInput{Start: true, Address: address, OwnerKind: "waker", OwnerID: "waker-1", PublicMessageID: mainconversation.AutomaticReportMessageID(runID),
			MainConversation: publisher, EventSubscription: &models.WorkTicketEventSubscription{
				RuleID: "rule", Topic: opened.Topic, InitialEventID: opened.ID, Primary: true, ChecksAllowed: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, source.WorkChatID)
	beforeReport, _, err := publisher.Messages(ctx, address.ID, 0, 10)
	if err != nil || len(beforeReport) != 0 {
		t.Fatalf("first work bypassed the routine publisher: %+v %v", beforeReport, err)
	}
	published, err := publisher.PublishTrackedEventReport(ctx, models.AutomaticRun{ID: runID, WakerID: "waker-1", ChatID: source.WorkChatID, ExecutionID: first.Execution.ID}, "Initial PR report.")
	if err != nil || published.SourceChatID != source.WorkChatID || published.SourceExecutionID != first.Execution.ID {
		t.Fatalf("routine result lost source work: %+v %v", published, err)
	}
	ticket, found, err := core.workTickets.ForMessage(ctx, address, published.ID)
	if err != nil || !found || ticket.ID != mainconversation.AutomaticReportMessageID(runID) {
		t.Fatalf("the initial report does not reference the work ticket: %+v %v", ticket, err)
	}
	if replay, owner, err := core.workTickets.ReceiveTopicEvent(ctx, "rule", opened, true); err != nil || !owner || len(replay) != 0 {
		t.Fatalf("the original source event was not idempotent: %+v %v %v", replay, owner, err)
	}
	review := opened
	review.ID, review.Kind, review.Title = "reviewed:111", eventsource.PullReviewed, "Review returned"
	deliveries, owner, err := core.workTickets.ReceiveTopicEvent(ctx, "rule", review, true)
	if err != nil || !owner || len(deliveries) != 1 {
		t.Fatalf("review receipt: %+v %v %v", deliveries, owner, err)
	}
	previous, err := core.journal.ReadExecution(ctx, source.WorkChatID, first.Execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := ResumeEventWork(ctx, core, ticket, previous, deliveries[0], publisher); err != nil {
			t.Fatal(err)
		}
		waitForChatTerminal(t, manager, source.WorkChatID)
	}
	history, _, err := publisher.Messages(ctx, address.ID, 0, 10)
	if err != nil || len(history) != 2 || history[1].SourceChatID != source.WorkChatID || history[1].SourceExecutionID != deliveries[0].ID ||
		history[1].ReplyToMessageID != published.ID || !strings.Contains(history[1].Content, opened.URL) {
		t.Fatalf("review created duplicate output or lost the original source: %+v %v", history, err)
	}
	replay, _, err := core.workTickets.ReceiveTopicEvent(ctx, "rule", review, true)
	if err != nil || len(replay) != 1 || replay[0].ExecutionID != deliveries[0].ID || replay[0].ReadAt == nil {
		t.Fatalf("lifecycle receipts do not show native consumption: %+v %v", replay, err)
	}
}
