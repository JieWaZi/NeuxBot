package mainconversation

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestScopedPublishingAndReplay(t *testing.T) {
	service, wakers, _, _, _ := newTestService(t)
	ctx := context.Background()
	for _, id := range []string{"p", "q"} {
		if err := wakers.Create(ctx, models.Waker{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := service.Ensure(ctx, "waker", "p")
	if err != nil {
		t.Fatal(err)
	}
	again, err := service.Ensure(ctx, "waker", "p")
	if err != nil || again.ID != first.ID {
		t.Fatalf("main: %#v %v", again, err)
	}
	a, err := service.ReserveWork(ctx, "waker", "p", "p", "project A")
	if err != nil {
		t.Fatal(err)
	}
	same, err := service.ReserveWork(ctx, "waker", "p", "p", "project A")
	if err != nil || same.WorkChatID != a.WorkChatID {
		t.Fatalf("same scope: %#v %v", same, err)
	}
	b, err := service.ReserveWork(ctx, "waker", "p", "p", "project B")
	if err != nil || b.WorkChatID == a.WorkChatID {
		t.Fatalf("new scope: %#v %v", b, err)
	}
	if _, err = service.Source(ctx, "waker", "q", a.WorkChatID); err == nil {
		t.Fatal("other partner opened source")
	}
	u, err := service.AdmitUser(ctx, "waker", "p", "request", "hello", "", a.WorkChatID, "", "", "/chosen", "", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	refetched, err := service.AdmitUser(ctx, "waker", "p", "request", "hello", "", a.WorkChatID, "", "", "/chosen", "", func(context.Context) error { return nil })
	if err != nil || refetched.ID != u.ID || refetched.RequestedDirectory != "/chosen" {
		t.Fatalf("original directory receipt: %#v %v", refetched, err)
	}
	if _, err := service.AdmitUser(ctx, "waker", "p", "request", "hello", "", a.WorkChatID, "", "", "/other", "", func(context.Context) error { return nil }); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("changed user directory selection: %v", err)
	}
	if _, err = service.AdmitUser(ctx, "waker", "p", "request", "hello", "", b.WorkChatID, "", "", "/chosen", "", func(context.Context) error { return nil }); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("conflicting user replay: %v", err)
	}
	if _, err := service.AdmitUser(ctx, "waker", "p", "request", "hello", "missing", a.WorkChatID, "", "", "/chosen", "", func(context.Context) error { return nil }); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("conflicting replay checked mutable target: %v", err)
	}
	if _, _, err := service.PublishWithReceipt(ctx, b.WorkChatID, "exec", "cross-project", "other", u.ID); !errors.Is(err, status_error.ChatReplyTargetUnavailable) {
		t.Fatalf("cross-project reference: %v", err)
	}
	p, _, err := service.PublishWithReceipt(ctx, a.WorkChatID, "exec", "call", "first", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	replay, _, err := service.PublishWithReceipt(ctx, a.WorkChatID, "exec", "call", "first", u.ID)
	if err != nil || p.ID != replay.ID {
		t.Fatalf("replayed: %#v %v", replay, err)
	}
	if _, _, err = service.PublishWithReceipt(ctx, a.WorkChatID, "exec", "call", "different", u.ID); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("conflicting tool replay: %v", err)
	}
	p2, _, err := service.PublishWithReceipt(ctx, a.WorkChatID, "exec", "call2", "second", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	recent, more, err := service.Messages(ctx, first.ID, 0, 2)
	if err != nil || !more || len(recent) != 2 || recent[0].ID != p.ID || recent[1].ID != p2.ID {
		t.Fatalf("page: %#v %t %v", recent, more, err)
	}
	older, more, err := service.Messages(ctx, first.ID, recent[0].Sequence, 2)
	if err != nil || more || len(older) != 1 || older[0].ID != u.ID {
		t.Fatalf("older: %#v %t %v", older, more, err)
	}
	if err = wakers.Delete(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.PublishWithReceipt(ctx, a.WorkChatID, "exec", "late", "late", u.ID); err == nil {
		t.Fatal("deleted subject accepted late publication")
	}
}

func TestQuotedPublicationUsesVisibleTimelineAtFirstCommit(t *testing.T) {
	ctx := t.Context()
	service, wakers, _, _, hub := newTestService(t)
	if err := wakers.Create(ctx, models.Waker{ID: "p", Name: "P"}); err != nil {
		t.Fatal(err)
	}
	work, err := service.ReserveWork(ctx, "waker", "p", "p", "project")
	if err != nil {
		t.Fatal(err)
	}
	admit := func(key, replyID string) models.MainConversationMessage {
		t.Helper()
		row, err := service.AdmitUser(ctx, "waker", "p", key, key, replyID, work.WorkChatID, key, "", "", "", func(context.Context) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	assignment := admit("assignment", "")
	removed := admit("removed", "")
	if err := service.RemoveMessage(ctx, "waker", "p", removed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdmitUser(ctx, "waker", "p", "pending", "pending", "", work.WorkChatID, "pending", "", "", "", func(context.Context) error {
		return status_error.ChatLogError
	}); !errors.Is(err, status_error.ChatLogError) {
		t.Fatal(err)
	}
	post := func(key, content string) (models.MainConversationMessage, bool) {
		t.Helper()
		row, replay, err := service.PublishQuotedWithReceipt(ctx, work.WorkChatID, "assignment", key, content, assignment.ID)
		if err != nil || row.ReplyToMessageID != assignment.ID {
			t.Fatalf("publication lost its assigned input: %+v, %t, %v", row, replay, err)
		}
		return row, replay
	}
	first, replay := post("first", "Adjacent answer")
	if replay || first.QuoteReply {
		t.Fatalf("removed and pending messages do not make a quote useful: %+v", first)
	}
	second, replay := post("second", "Later finding")
	if replay || !second.QuoteReply {
		t.Fatalf("an intervening visible reply should make the requested quote visible: %+v", second)
	}
	userReply := admit("follow-up", second.ID)
	if userReply.ReplyToMessageID != second.ID {
		t.Fatalf("the adjacent user reply lost its explicit target: %+v", userReply)
	}
	for _, row := range []models.MainConversationMessage{first, second, userReply} {
		if err := service.RemoveMessage(ctx, "waker", "p", row.ID); err != nil {
			t.Fatal(err)
		}
	}
	sub := hub.Subscribe("main-conversations", events.Resource{Kind: "waker", ID: "p"})
	defer sub.Close()
	if row, replay := post("second", "Later finding"); !replay || row.ID != second.ID || !row.QuoteReply {
		t.Fatalf("replay recomputed a previously visible quote: %+v", row)
	}
	assertNoReplayEvent := func() {
		t.Helper()
		select {
		case event := <-sub.Events:
			t.Fatalf("retry emitted a second public event: %+v", event)
		default:
		}
	}
	assertNoReplayEvent()
	admit("intervening", "")
	select {
	case <-sub.Events:
	default:
		t.Fatal("new input did not emit a public event")
	}
	if row, replay := post("first", "Adjacent answer"); !replay || row.ID != first.ID || row.QuoteReply {
		t.Fatalf("replay recomputed a previously suppressed quote: %+v", row)
	}
	if _, _, err := service.PublishWithReceipt(ctx, work.WorkChatID, "assignment", "first", first.Content, assignment.ID); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("changing the original quote request should conflict: %v", err)
	}
	assertNoReplayEvent()
}

func TestDeletedOriginReportReplayChecksOriginalWorkIdentity(t *testing.T) {
	ctx := t.Context()
	service, wakers, _, _, _ := newTestService(t)
	if err := wakers.Create(ctx, models.Waker{ID: "partner", Name: "Partner"}); err != nil {
		t.Fatal(err)
	}
	origin := models.MainConversationOriginalWork{ChatID: "original", TicketID: "ticket", Title: "Original chat"}
	ticket := models.WorkTicket{ID: origin.TicketID, OriginKind: constants.WorkTicketOriginChat, OriginID: origin.ChatID, OwnerWakerID: "partner"}
	report := models.WorkTicketReport{ID: "stable-report", TicketID: ticket.ID, Content: "Result"}
	first, err := service.PublishDeletedOriginReport(ctx, "partner", ticket, report, origin)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.PublishDeletedOriginReport(ctx, "partner", ticket, report, origin)
	if err != nil || replayed.ID != first.ID || replayed.Sequence != first.Sequence {
		t.Fatalf("same original work changed the report: %+v %v", replayed, err)
	}
	origin.ChatID = "different-chat"
	ticket.OriginID = origin.ChatID
	if _, err := service.PublishDeletedOriginReport(ctx, "partner", ticket, report, origin); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("same report key accepted a different origin: %v", err)
	}
}

func TestMainReportReusesOnlyPublicationFromTheSameExecution(t *testing.T) {
	ctx := t.Context()
	service, wakers, _, _, _ := newTestService(t)
	if err := wakers.Create(ctx, models.Waker{ID: "partner", Name: "Partner"}); err != nil {
		t.Fatal(err)
	}
	work, err := service.ReserveWork(ctx, "waker", "partner", "partner", "source")
	if err != nil {
		t.Fatal(err)
	}
	posted, _, err := service.PublishWithReceipt(ctx, work.WorkChatID, "first-execution", "post", "Verified result.", "")
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := service.PublishReportWithReceipt(ctx, work.WorkChatID, "first-execution", "report", posted.Content, "")
	if err != nil || first.ID != posted.ID {
		t.Fatalf("did not retain the published message: was=%+v report=%+v err=%v", posted, first, err)
	}
	second, _, err := service.PublishReportWithReceipt(ctx, work.WorkChatID, "second-execution", "report", posted.Content, "")
	if err != nil || second.ID == posted.ID || second.SourceExecutionID != "second-execution" {
		t.Fatalf("report reused another execution's message: %+v, %v", second, err)
	}
}

func TestConcurrentMainAndCommittedChanges(t *testing.T) {
	service, wakers, _, _, hub := newTestService(t)
	ctx := context.Background()
	if err := wakers.Create(ctx, models.Waker{ID: "p", Name: "P"}); err != nil {
		t.Fatal(err)
	}
	w, err := service.ReserveWork(ctx, "waker", "p", "p", "managed")
	if err != nil {
		t.Fatal(err)
	}
	eventsSub := hub.Subscribe("main-conversations", events.Resource{Kind: "waker", ID: "p"})
	defer eventsSub.Close()
	if err = service.tx.Transaction(ctx, func(tx context.Context) error {
		if _, err := service.AdmitUser(tx, "waker", "p", "rolled", "rollback", "", w.WorkChatID, "", "", "", "", func(context.Context) error { return nil }); err != nil {
			return err
		}
		select {
		case <-eventsSub.Events:
			t.Error("notified before commit")
		default:
		}
		return status_error.BadRequest
	}); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("rollback: %v", err)
	}
	user, err := service.AdmitUser(ctx, "waker", "p", "send", "ok", "", w.WorkChatID, "", "", "", "", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != UserMessageID(w.ConversationID, "send") || user.Sequence != 1 {
		t.Fatalf("user identity %#v", user)
	}
	repeated, err := service.AdmitUser(ctx, "waker", "p", "send", "ok", "", w.WorkChatID, "", "", "", "", func(context.Context) error { return nil })
	if err != nil || repeated.ID != user.ID {
		t.Fatalf("replay: %#v %v", repeated, err)
	}
	select {
	case e := <-eventsSub.Events:
		if e.Type != "main-conversations.changed" {
			t.Fatalf("event %#v", e)
		}
	default:
		t.Fatal("no committed notification")
	}
	select {
	case <-eventsSub.Events:
		t.Fatal("notified twice")
	default:
	}
	var group sync.WaitGroup
	start := make(chan struct{})
	receipts := make([]models.MainConversationMessage, 2)
	replays := make([]bool, 2)
	for i := range receipts {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			var err error
			receipts[i], replays[i], err = service.PublishWithReceipt(ctx, w.WorkChatID, "execution", "call", "ready", user.ID)
			if err != nil {
				t.Errorf("concurrent post: %v", err)
			}
		}()
	}
	close(start)
	group.Wait()
	if replays[0] == replays[1] || receipts[0].ID == "" || receipts[0].ID != receipts[1].ID {
		t.Fatalf("duplicate receipt: %v %v", receipts, replays)
	}
	receipt, found, err := service.Published(ctx, w.WorkChatID, "execution", "call")
	if err != nil || !found || receipt.ID != receipts[0].ID {
		t.Fatalf("recover original receipt: %#v %t %v", receipt, found, err)
	}
}

// interruptedAdmissionStore 模拟来源日志已接纳后公开确认写入中断。
type interruptedAdmissionStore struct {
	// MainConversationStore 提供同一真实 SQLite 仓储。
	controllers.MainConversationStore
}

func (s interruptedAdmissionStore) ConfirmUserAdmission(context.Context, string, string) (models.MainConversationMessage, error) {
	return models.MainConversationMessage{}, status_error.DatabaseError
}

func TestRecoverUserAdmissionsRequiresAnExactDurableReceipt(t *testing.T) {
	service, wakers, _, chats, hub := newTestService(t)
	ctx := t.Context()
	if err := wakers.Create(ctx, models.Waker{ID: "p", Name: "P"}); err != nil {
		t.Fatal(err)
	}
	work, err := service.ReserveWork(ctx, "waker", "p", "p", "project")
	if err != nil {
		t.Fatal(err)
	}
	sub := hub.Subscribe("main-conversations", events.Resource{Kind: "waker", ID: "p"})
	defer sub.Close()
	originalStore := service.store
	service.store = interruptedAdmissionStore{originalStore}
	row, err := service.AdmitUser(ctx, "waker", "p", "accepted", "keep once", "", work.WorkChatID, "first", "", "", "", func(context.Context) error { return nil })
	if err != nil || !row.AdmissionPending {
		t.Fatalf("the durable work must remain accepted while publication is pending: %+v %v", row, err)
	}
	_, err = service.AdmitUser(ctx, "waker", "p", "rejected", "retry later", "", work.WorkChatID, "second", "", "", "", func(context.Context) error {
		return status_error.ChatLogError
	})
	if !errors.Is(err, status_error.ChatLogError) {
		t.Fatal(err)
	}
	select {
	case <-sub.Events:
		t.Fatal("a reservation produced a public event")
	default:
	}
	service.store = originalStore
	later, err := service.AdmitUser(ctx, "waker", "p", "later", "published earlier", "", work.WorkChatID, "third", "", "", "", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	read := func(_ context.Context, chatID, submissionID string) (*models.Execution, error) {
		if chatID != work.WorkChatID || submissionID != "accepted" {
			return nil, nil
		}
		return &models.Execution{ID: "first", ChatID: chatID, SubmissionID: submissionID}, nil
	}
	if err := chats.Create(ctx, models.Chat{ID: work.WorkChatID, WakerID: "p", HiddenFromWakerHistory: true, ActiveExecutionID: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := service.RecoverUserAdmissions(ctx, work.ConversationID, read); err != nil {
		t.Fatal(err)
	}
	if repaired, err := service.Message(ctx, work.ConversationID, row.ID); err != nil || repaired.AdmissionPending {
		t.Fatalf("opening the active conversation must repair the admitted input: %+v %v", repaired, err)
	}
	for range 2 {
		if err := service.RecoverUserAdmissions(ctx, "", read); err != nil {
			t.Fatal(err)
		}
	}
	rows, more, err := service.Messages(ctx, work.ConversationID, 0, 1)
	if err != nil || !more || len(rows) != 1 || rows[0].ID != row.ID || rows[0].Sequence <= later.Sequence {
		t.Fatalf("recovery must publish the old identity at the current head: %+v %v %v", rows, more, err)
	}
	rows, more, err = service.Messages(ctx, work.ConversationID, rows[0].Sequence, 1)
	if err != nil || more || len(rows) != 1 || rows[0].ID != later.ID {
		t.Fatalf("unaccepted input affected the visible page: %+v %v %v", rows, more, err)
	}
}

func TestOriginalExecutionPublicationConfirmsPendingInputFirst(t *testing.T) {
	service, wakers, _, _, _ := newTestService(t)
	ctx := t.Context()
	if err := wakers.Create(ctx, models.Waker{ID: "p", Name: "P"}); err != nil {
		t.Fatal(err)
	}
	work, err := service.ReserveWork(ctx, "waker", "p", "p", "project")
	if err != nil {
		t.Fatal(err)
	}
	originalStore := service.store
	service.store = interruptedAdmissionStore{originalStore}
	input, err := service.AdmitUser(ctx, "waker", "p", "submission", "first", "", work.WorkChatID, "original", "", "", "", func(context.Context) error { return nil })
	if err != nil || !input.AdmissionPending {
		t.Fatalf("expected a pending public projection: %+v %v", input, err)
	}
	service.store = originalStore
	if _, _, err := service.PublishWithReceipt(ctx, work.WorkChatID, "another", "call", "wrong turn", input.ID); !errors.Is(err, status_error.ChatReplyTargetUnavailable) {
		t.Fatalf("another execution confirmed the pending input: %v", err)
	}
	post, _, err := service.PublishWithReceipt(ctx, work.WorkChatID, "original", "call", "answer", input.ID)
	if err != nil {
		t.Fatal(err)
	}
	rows, more, err := service.Messages(ctx, work.ConversationID, 0, 10)
	if err != nil || more || len(rows) != 2 || rows[0].ID != input.ID || rows[0].AdmissionPending || rows[1].ID != post.ID {
		t.Fatalf("public input and answer must appear in order: %+v %v %v", rows, more, err)
	}
}
