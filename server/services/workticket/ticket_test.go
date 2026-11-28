package workticket_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

func TestAdmissionPreservesOriginAndReplaysAcrossStoreInstances(t *testing.T) {
	db, service, address := newTicketChat(t, nil)
	in := ticketInput(address, "a")
	pending, err := service.PrepareAdmission(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := service.Lookup(t.Context(), address, []string{"a"})
	if err != nil || len(rows) != 0 {
		t.Fatalf("unconfirmed admission visible: %+v, %v", rows, err)
	}
	var assignments int64
	if err := db.Model(&models.WorkTicketAttempt{}).Where("ticket_id = ?", pending.Ticket.ID).Count(&assignments).Error; err != nil || assignments != 1 {
		t.Fatalf("admission did not reserve its first assignment together: count=%d err=%v", assignments, err)
	}
	service = ticketService(db, nil)
	changedIdentity := in
	changedIdentity.ExecutionID = "another-execution"
	if _, err := service.PrepareAdmission(t.Context(), changedIdentity); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("unconfirmed admission switched execution: %v", err)
	}
	replay, err := service.PrepareAdmission(t.Context(), in)
	if err != nil || !replay.Replayed || replay.Generation != pending.Generation || replay.Ticket.ID != "a" || pending.Ticket.ID != "a" {
		t.Fatalf("admission replay changed identity: %+v, %v", replay, err)
	}
	if err := service.ConfirmAdmission(t.Context(), replay); err != nil {
		t.Fatal(err)
	}
	if got, err := service.Lookup(t.Context(), address, []string{"a"}); err != nil || len(got) != 1 || got[0].Ticket.Status != "active" || got[0].Ticket.ID != "a" {
		t.Fatalf("origin lookup: %+v, %v", got, err)
	}
	other := workticket.Address{Kind: constants.WorkTicketOriginChat, ID: "elsewhere"}
	if got, err := service.Lookup(t.Context(), other, []string{"a"}); err != nil || len(got) != 0 {
		t.Fatalf("cross-origin lookup: %+v, %v", got, err)
	}
	in.SubmissionHash = "changed"
	if _, err := service.PrepareAdmission(t.Context(), in); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("changed submission accepted: %v", err)
	}
}

func TestAttemptCrossesWaitingTurnsAndFencesPreviousTurn(t *testing.T) {
	_, service, address := newTicketChat(t, nil)
	a := admit(t, service, ticketInput(address, "a"))
	if err := service.FinishTurn(t.Context(), a, constants.ExecutionStatusCompleted); err != nil {
		t.Fatal(err)
	}
	b := admit(t, service, ticketInput(address, "b"))
	follow := ticketInput(address, "a-follow")
	follow.TicketID = a.Ticket.ID
	next := admit(t, service, follow)
	if next.Generation != a.Generation {
		t.Fatalf("waiting work lost its current assignment: %v => %v", a, next)
	}
	old := workticket.ReportInput{RequestID: "late-a", Kind: "completed", Content: "old turn"}
	if _, err := service.Report(t.Context(), a, old, noDelivery); !errors.Is(err, status_error.WorkTicketAttemptUnavailable) {
		t.Fatalf("previous turn could settle ticket: %v", err)
	}
	if err := service.FinishTurn(t.Context(), a, constants.ExecutionStatusFailed); err != nil {
		t.Fatalf("late native termination must be ignored: %v", err)
	}
	report := workticket.ReportInput{RequestID: "finish-a", Kind: "completed", Content: "A conclusion"}
	result, err := service.Report(t.Context(), next, report, noDelivery)
	if err != nil || result.MessageID != "exec-a-follow" {
		t.Fatalf("first report receipt=%+v %v", result, err)
	}
	replay, err := service.Report(t.Context(), next, report, nil)
	if err != nil || replay.ID != result.ID || replay.MessageID != result.MessageID || !replay.Replayed {
		t.Fatalf("report replay=%+v %v", replay, err)
	}
	report.Content = "changed"
	if _, err := service.Report(t.Context(), next, report, nil); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("changed report accepted: %v", err)
	}
	rows, err := service.Lookup(t.Context(), address, []string{"a", "b", "a-follow", "exec-a-follow"})
	if err != nil || len(rows) != 4 || rows[0].Ticket.Status != "completed" || rows[1].Ticket.Status != "active" || rows[1].Ticket.ID != b.Ticket.ID || rows[2].Ticket.ID != a.Ticket.ID || rows[3].Ticket.ID != a.Ticket.ID {
		t.Fatalf("A and B were not isolated: %+v, %v", rows, err)
	}
}

func TestReopenRequiresOriginalScopeAndStopRevokesAuthorization(t *testing.T) {
	_, service, address := newTicketChat(t, nil)
	binding := admit(t, service, ticketInput(address, "first"))
	report := workticket.ReportInput{RequestID: "first-result", Kind: "completed", Content: "done"}
	if _, err := service.Report(t.Context(), binding, report, noDelivery); err != nil {
		t.Fatal(err)
	}
	if err := service.FinishTurn(t.Context(), binding, constants.ExecutionStatusCompleted); err != nil {
		t.Fatal(err)
	}
	reopen := ticketInput(address, "reopen")
	reopen.TicketID = binding.Ticket.ID
	reopen.AllowedDirectories = []string{"/unexpected"}
	if _, err := service.PrepareAdmission(t.Context(), reopen); !errors.Is(err, status_error.WorkTicketCannotContinue) {
		t.Fatalf("reopened with different scope: %v", err)
	}
	reopen.AllowedDirectories = []string{"/work"}
	next := admit(t, service, reopen)
	if next.Ticket.ID != binding.Ticket.ID || next.Generation != binding.Generation+1 {
		t.Fatalf("reopen did not advance generation: %v => %v", binding, next)
	}
	stopped, err := service.Stop(t.Context(), address, next.Ticket.ID)
	if err != nil || stopped.Status != "stopped" || stopped.CurrentExecutionID != next.ExecutionID || stopped.WorkChatID != address.ID {
		t.Fatalf("stop lost exact cancellation identity: %+v, %v", stopped, err)
	}
	replayed, err := service.Stop(t.Context(), address, next.Ticket.ID)
	if err != nil || replayed.UpdatedAt != stopped.UpdatedAt || replayed.CurrentExecutionID != stopped.CurrentExecutionID {
		t.Fatalf("stop replay: %+v, %v", replayed, err)
	}
	reopen.MessageID, reopen.SourceMessageID, reopen.SubmissionID, reopen.ExecutionID = "forbidden", "forbidden", "forbidden", "exec-forbidden"
	if _, err := service.PrepareAdmission(t.Context(), reopen); !errors.Is(err, status_error.WorkTicketCannotContinue) {
		t.Fatalf("stopped work was reauthorized: %v", err)
	}
	if _, err := service.Report(t.Context(), next, workticket.ReportInput{RequestID: "late", Kind: "completed", Content: "late"}, noDelivery); !errors.Is(err, status_error.WorkTicketAttemptUnavailable) {
		t.Fatalf("stopped work could report: %v", err)
	}
}

func TestReportRollsBackWithPublicDeliveryAndRetryKeepsReceipt(t *testing.T) {
	var changed []workticket.Address
	db, service, address := newTicketMain(t, func(_ context.Context, address workticket.Address) {
		changed = append(changed, address)
	})
	binding := admit(t, service, ticketInput(address, "first"))
	report := workticket.ReportInput{RequestID: "result", Kind: "completed", Content: "conclusion"}
	tx := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db))
	var attemptedID string
	failure := func(ctx context.Context, ticket models.WorkTicket, receiptID string) (string, error) {
		attemptedID = receiptID
		if err := dbhelper.GetCtxDB(ctx, db).Create(&models.Project{ID: receiptID, WakerID: ticket.OwnerWakerID, Name: "result"}).Error; err != nil {
			t.Fatalf("test delivery write: %v", err)
		}
		return "", status_error.BadRequest
	}
	if _, err := service.Report(t.Context(), binding, report, failure); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("delivery error changed: %v", err)
	}
	if got, err := service.Lookup(t.Context(), address, []string{"first"}); err != nil || len(got) != 1 || got[0].Ticket.Status != "active" || len(changed) != 1 {
		t.Fatalf("failed delivery advanced state or emitted a change: rows=%+v changes=%v err=%v", got, changed, err)
	}
	if receipt, found, err := service.ReportReceipt(t.Context(), binding, report.RequestID); found || err != nil {
		t.Fatalf("failed delivery recorded a receipt: %+v, %v, %v", receipt, found, err)
	}
	err := tx.Transaction(t.Context(), func(ctx context.Context) error {
		receipt, err := service.Report(ctx, binding, report, noDelivery)
		if err == nil && (receipt.ID != attemptedID || len(changed) != 1) {
			t.Fatalf("result id changed or event published before outer transaction commit: %+v, changes=%v", receipt, changed)
		}
		return err
	})
	if err != nil || len(changed) != 2 || changed[1] != address {
		t.Fatalf("committed report failed: %v, changes=%v", err, changed)
	}
	var publications int64
	if err := db.Model(&models.Project{}).Where("id = ?", attemptedID).Count(&publications).Error; err != nil || publications != 0 {
		t.Fatalf("failed publication escaped rollback: %v count=%d", err, publications)
	}
}

func TestMainNativeFinalCommitsWithPublicationAndLeavesTicketOpen(t *testing.T) {
	ctx := t.Context()
	db, service, address := newTicketMain(t, nil)
	binding := admit(t, service, ticketInput(address, "native-result"))
	answer := func(ctx context.Context) (string, error) {
		if _, inTransaction := dbhelper.GetCtxDB(ctx, db).Statement.ConnPool.(gorm.TxCommitter); inTransaction {
			t.Fatal("native transcript was read inside the publication transaction")
		}
		return "    Native result", nil
	}
	var receiptID string
	fail := true
	deliver := func(ctx context.Context, ticket models.WorkTicket, receipt, content string) (string, error) {
		if ticket.ID != binding.Ticket.ID || (receiptID != "" && receiptID != receipt) || content != "    Native result" {
			t.Fatalf("changed final delivery: ticket=%s receipt=%s content=%q", ticket.ID, receipt, content)
		}
		receiptID = receipt
		if err := dbhelper.GetCtxDB(ctx, db).Create(&models.Project{ID: receiptID, WakerID: ticket.OwnerWakerID, Name: "publication"}).Error; err != nil {
			t.Fatal(err)
		}
		if fail {
			return "", status_error.DatabaseError
		}
		return "public-result", nil
	}
	if err := service.ReportNativeFinal(ctx, binding, answer, deliver); !errors.Is(err, status_error.DatabaseError) {
		t.Fatalf("fallback delivery error changed: %v", err)
	}
	if ticket, found, err := service.ForMessage(ctx, address, binding.Ticket.ID); err != nil || !found || ticket.Status != constants.WorkTicketStatusActive {
		t.Fatalf("failed fallback changed ticket state: %+v, found=%t, %v", ticket, found, err)
	}
	// Reusing the same id succeeds only if the original publication rolled back.
	fail = false
	if err := service.ReportNativeFinal(ctx, binding, answer, deliver); err != nil {
		t.Fatal(err)
	}
	if ticket, found, err := service.ForMessage(ctx, address, "public-result"); err != nil || !found || ticket.ID != binding.Ticket.ID ||
		ticket.Status != constants.WorkTicketStatusWaiting {
		t.Fatalf("fallback did not link a reply to an open ticket: %+v, found=%t, %v", ticket, found, err)
	}
	if err := service.ReportNativeFinal(ctx, binding, func(context.Context) (string, error) {
		t.Fatal("a replay attempted to load and publish the native result again")
		return "", nil
	}, deliver); err != nil {
		t.Fatal(err)
	}
}

func TestOrdinaryReportRecoveryIsolatesUnavailableSource(t *testing.T) {
	_, service, address := newTicketChat(t, nil)
	ctx := t.Context()
	bad := admit(t, service, ticketInput(address, "isolated-source"))
	good := admit(t, service, ticketInput(address, "available-source"))
	request := workticket.ReportInput{RequestID: "finish-work", Kind: "completed", Content: "saved conclusion"}
	for _, binding := range []workticket.Binding{bad, good} {
		if _, err := service.Report(ctx, binding, request, func(context.Context, models.WorkTicket, string) (string, error) {
			return "", status_error.ChatLogError
		}); !errors.Is(err, status_error.ChatLogError) {
			t.Fatalf("could not simulate destination loss after durable admission: %v", err)
		}
	}
	read := func(_ string, executionID string) ([]eventlog.Record, error) {
		if executionID == bad.ExecutionID {
			return nil, status_error.ChatLogCorrupted
		}
		return nil, nil
	}
	var delivered atomic.Int64
	deliver := func(_ context.Context, _ models.WorkTicket, report models.WorkTicketReport) (string, error) {
		delivered.Add(1)
		if report.ExecutionID != good.ExecutionID || report.Content != "saved conclusion" {
			t.Errorf("recovery rewrote destination or content: %+v", report)
			return "", status_error.BadRequest
		}
		return report.ExecutionID, nil
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			results <- service.RecoverOrdinaryReports(ctx, read, deliver)
		}()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("one isolated source blocked recovery of other chats: %v", err)
		}
	}
	if count := delivered.Load(); count != 1 {
		t.Fatalf("source received %d concurrent deliveries, want 1", count)
	}
	if pending, err := service.HasPendingOrdinaryReport(ctx, address, bad.ExecutionID); err != nil || !pending {
		t.Fatalf("isolated source lost its pending report: pending=%v, %v", pending, err)
	}
	if err := service.RecoverOrdinaryReports(ctx, func(string, string) ([]eventlog.Record, error) {
		return nil, status_error.DatabaseBusy
	}, deliver); !errors.Is(err, status_error.DatabaseBusy) {
		t.Fatalf("infrastructure failure was hidden: %v", err)
	}
}

func TestOrdinaryProgressLeavesDurableRetryKeyWithoutUsingSQLiteTransaction(t *testing.T) {
	db, service, address := newTicketChat(t, nil)
	binding := admit(t, service, ticketInput(address, "first"))
	report := workticket.ReportInput{RequestID: "progress", Kind: "progress", Content: "still working"}
	var attemptedID string
	_, err := service.Report(t.Context(), binding, report, func(ctx context.Context, _ models.WorkTicket, id string) (string, error) {
		if _, transactional := dbhelper.GetCtxDB(ctx, db).Statement.ConnPool.(gorm.TxCommitter); transactional {
			t.Fatal("ordinary message delivery was inside a retryable SQLite transaction")
		}
		attemptedID = id
		return "", status_error.ChatLogVersionConflict
	})
	if !errors.Is(err, status_error.ChatLogVersionConflict) || attemptedID == "" {
		t.Fatalf("failed publication lost its identity: id=%q err=%v", attemptedID, err)
	}
	service = ticketService(db, nil)
	receipt, err := service.Report(t.Context(), binding, report, noDelivery)
	if err != nil || receipt.ID != attemptedID || receipt.MessageID != binding.ExecutionID {
		t.Fatalf("report retry changed identity: %+v, %v", receipt, err)
	}
	rows, err := service.Lookup(t.Context(), address, []string{"first"})
	if err != nil || len(rows) != 1 || rows[0].Ticket.Status != "active" || rows[0].Ticket.Summary != "still working" {
		t.Fatalf("progress did not update active ticket: %+v, %v", rows, err)
	}
}

func TestReconcileTerminalReceiptAdvancesGenerationAfterFailedExecution(t *testing.T) {
	_, service, address := newTicketChat(t, nil)
	start, err := service.PrepareAdmission(t.Context(), ticketInput(address, "first"))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ConfirmExecution(t.Context(), start.ExecutionID, constants.ExecutionStatusFailed); err != nil {
		t.Fatal(err)
	}
	rows, err := service.Lookup(t.Context(), address, []string{"first"})
	if err != nil || len(rows) != 1 || rows[0].Ticket.Status != "blocked" {
		t.Fatalf("recovered failure not reflected in ticket: %+v, %v", rows, err)
	}
	retry := ticketInput(address, "retry")
	retry.TicketID = start.Ticket.ID
	next := admit(t, service, retry)
	if next.Generation != start.Generation+1 {
		t.Fatalf("failure reused ended assignment: %+v, %+v", start, next)
	}
}

func TestMainTicketUsesOriginalSourceAndRechecksRemovedTeamMember(t *testing.T) {
	db, service, address := newTicketMain(t, nil)
	if err := db.Model(&models.MainConversation{}).Where("id = ?", address.ID).UpdateColumns(map[string]any{"subject_type": "team", "subject_id": "team"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&models.Channel{ID: "team", Name: "Team", LeadWakerID: "lead"},
		&models.ChannelMember{ChannelID: "team", WakerID: "owner"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	first := ticketInput(address, "public")
	first.SourceMessageID, first.OwnerKind, first.OwnerID = "hidden-input", "team", "team"
	binding := admit(t, service, first)
	source, err := service.OriginalSourceMessage(t.Context(), binding.Ticket)
	if err != nil || source != "hidden-input" || binding.Ticket.ID != "public" {
		t.Fatalf("public issue and source history identities: ticket=%+v source=%q err=%v", binding.Ticket, source, err)
	}
	found, ok, err := service.ForSourceMessage(t.Context(), address, binding.Ticket.WorkChatID, "hidden-input")
	if err != nil || !ok || found.ID != "public" {
		t.Fatalf("could not link original input inside its work record: %+v, %v, %v", found, ok, err)
	}
	if _, ok, err := service.ForSourceMessage(t.Context(), address, "another-work", "hidden-input"); err != nil || ok {
		t.Fatalf("source ID escaped its authorized work chat: %v %v", ok, err)
	}
	receipt, err := service.Report(t.Context(), binding, workticket.ReportInput{RequestID: "conclusion", Kind: "completed", Content: "done"},
		func(context.Context, models.WorkTicket, string) (string, error) { return "public-result", nil })
	if err != nil || receipt.MessageID != "public-result" {
		t.Fatalf("main report failed: %+v, %v", receipt, err)
	}
	if found, ok, err := service.ForMessage(t.Context(), address, receipt.MessageID); err != nil || !ok || found.ID != "public" {
		t.Fatalf("result did not resolve to its original ticket: %+v, %v, %v", found, ok, err)
	}
	if err := service.FinishTurn(t.Context(), binding, constants.ExecutionStatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("channel_id = ? AND waker_id = ?", "team", "owner").Delete(&models.ChannelMember{}).Error; err != nil {
		t.Fatal(err)
	}
	follow := ticketInput(address, "request-more")
	follow.TicketID, follow.OwnerKind, follow.OwnerID = binding.Ticket.ID, "team", "team"
	if _, err := service.PrepareAdmission(t.Context(), follow); !errors.Is(err, status_error.WorkTicketCannotContinue) {
		t.Fatalf("a removed team member could reopen the old ticket: %v", err)
	}
}

func newTicketMain(t *testing.T, changed func(context.Context, workticket.Address)) (*gorm.DB, *workticket.Service, workticket.Address) {
	t.Helper()
	db, service, _ := newTicketChat(t, changed)
	if err := db.Model(&models.Chat{}).Where("id = ?", "chat").UpdateColumn("hidden_from_waker_history", true).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&models.MainConversation{ID: "main", SubjectType: "waker", SubjectID: "owner"},
		&models.MainConversationWork{WorkChatID: "chat", ConversationID: "main", WakerID: "owner", ScopeKey: "project"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db, service, workticket.Address{Kind: constants.WorkTicketOriginMain, ID: "main"}
}

func newTicketChat(t *testing.T, changed func(context.Context, workticket.Address)) (*gorm.DB, *workticket.Service, workticket.Address) {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "tickets.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, row := range []any{
		&models.Waker{ID: "owner", Name: "Owner"},
		&models.Project{ID: "project", WakerID: "owner", Name: "Project"},
		&models.Chat{ID: "chat", WakerID: "owner", ProjectID: "project", AdditionalWorkingDirectories: []string{"/work"}},
	} {
		if err := database.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	return database.DB, ticketService(database.DB, changed), workticket.Address{Kind: constants.WorkTicketOriginChat, ID: "chat"}
}

func ticketService(db *gorm.DB, changed func(context.Context, workticket.Address)) *workticket.Service {
	return workticket.New(controllers.NewWorkTicketController(db), controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db)), changed, nil)
}

func ticketInput(address workticket.Address, key string) workticket.Admission {
	return workticket.Admission{
		Address: address, MessageID: key, SourceMessageID: key,
		WorkChatID: "chat", ExecutionID: "exec-" + key, SubmissionID: key, SubmissionHash: "digest-" + key,
		OwnerKind: "waker", OwnerID: "owner", OwnerWakerID: "owner", ProjectID: "project",
		AllowedDirectories: []string{"/work"},
	}
}

func admit(t *testing.T, service *workticket.Service, in workticket.Admission) workticket.Binding {
	t.Helper()
	binding, err := service.PrepareAdmission(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ConfirmAdmission(t.Context(), binding); err != nil {
		t.Fatal(err)
	}
	return binding
}

func noDelivery(_ context.Context, ticket models.WorkTicket, _ string) (string, error) {
	return ticket.CurrentExecutionID, nil
}

func TestOrdinaryAssistantOnlyLinksTicketAfterACommittedReport(t *testing.T) {
	_, service, address := newTicketChat(t, nil)
	binding := admit(t, service, ticketInput(address, "initial"))
	if _, found, err := service.ForMessage(t.Context(), address, binding.ExecutionID); err != nil || found {
		t.Fatalf("unreported native assistant was linked to ticket: found=%v err=%v", found, err)
	}
	_, err := service.Report(t.Context(), binding,
		workticket.ReportInput{RequestID: "result", Kind: "completed", Content: "outcome"}, noDelivery)
	if err != nil {
		t.Fatal(err)
	}
	ticket, found, err := service.ForMessage(t.Context(), address, binding.ExecutionID)
	if err != nil || !found || ticket.ID != "initial" {
		t.Fatalf("committed result lost ticket link: ticket=%+v found=%v err=%v", ticket, found, err)
	}
}

func TestOrdinaryJournalEvidenceRecoversResultAfterMetadataAndTurnFailure(t *testing.T) {
	db, service, address := newTicketChat(t, nil)
	binding := admit(t, service, ticketInput(address, "first"))
	const rejectReceipt = `CREATE TRIGGER reject_report_receipt BEFORE UPDATE OF message_id ON work_ticket_reports
		WHEN OLD.message_id = '' AND NEW.message_id <> '' BEGIN SELECT RAISE(ABORT, 'test receipt failure'); END`
	if err := db.Exec(rejectReceipt).Error; err != nil {
		t.Fatal(err)
	}
	var published workticket.PublishedReport
	in := workticket.ReportInput{RequestID: "conclusion", Kind: "completed", Content: "verified"}
	_, err := service.Report(t.Context(), binding, in, func(_ context.Context, _ models.WorkTicket, id string) (string, error) {
		published = workticket.PublishedReport{ID: id, MessageID: binding.ExecutionID, Content: in.Content}
		return published.MessageID, nil
	})
	if err == nil || published.ID == "" {
		t.Fatalf("test did not simulate journal success followed by metadata failure: %+v %v", published, err)
	}
	if pending, err := service.HasPendingOrdinaryReport(t.Context(), address, binding.ExecutionID); err != nil || !pending {
		t.Fatalf("missing pending original report: pending=%v err=%v", pending, err)
	}
	if ticket, found, err := service.ForMessage(t.Context(), address, binding.Ticket.ID); err != nil || !found || ticket.Status != constants.WorkTicketStatusCompleted {
		t.Fatalf("local conclusion must already be committed with pending delivery: %+v, %v, %v", ticket, found, err)
	}
	for _, other := range []workticket.Address{
		{Kind: constants.WorkTicketOriginChat, ID: "another-chat"},
		{Kind: constants.WorkTicketOriginMain, ID: address.ID},
	} {
		if pending, err := service.HasPendingOrdinaryReport(t.Context(), other, binding.ExecutionID); err != nil || pending {
			t.Fatalf("pending report escaped original ordinary chat: address=%+v pending=%v err=%v", other, pending, err)
		}
	}
	progress := workticket.ReportInput{RequestID: "later", Kind: "progress", Content: "still working"}
	_, err = service.Report(t.Context(), binding, progress, func(context.Context, models.WorkTicket, string) (string, error) {
		t.Error("later report attempted publication before the earlier completed report settled")
		return binding.ExecutionID, nil
	})
	if !errors.Is(err, status_error.CreateRequestInProgress) {
		t.Fatalf("later progress was not rejected while completion is pending: %v", err)
	}
	if err := db.Exec("DROP TRIGGER reject_report_receipt").Error; err != nil {
		t.Fatal(err)
	}
	if err := service.FinishTurn(t.Context(), binding, constants.ExecutionStatusFailed); err != nil {
		t.Fatal(err)
	}
	if receipt, found, err := service.ReportReceipt(t.Context(), binding, in.RequestID); err != nil || found {
		t.Fatalf("reserved report became a completed receipt without journal evidence: %+v, %v, %v", receipt, found, err)
	}
	wrong := published
	wrong.Content = "different"
	if err := service.ReconcilePublishedReports(t.Context(), binding.ExecutionID, []workticket.PublishedReport{wrong}); !errors.Is(err, status_error.ChatLogCorrupted) {
		t.Fatalf("mismatched journal evidence accepted: %v", err)
	}
	if err := service.ReconcilePublishedReports(t.Context(), binding.ExecutionID, []workticket.PublishedReport{published}); err != nil {
		t.Fatal(err)
	}
	if pending, err := service.HasPendingOrdinaryReport(t.Context(), address, binding.ExecutionID); err != nil || pending {
		t.Fatalf("recovered report remained pending: pending=%v err=%v", pending, err)
	}
	ticket, found, err := service.ForMessage(t.Context(), address, binding.ExecutionID)
	if err != nil || !found || ticket.Status != "completed" || ticket.Summary != in.Content {
		t.Fatalf("already-published result stayed blocked after recovery: %+v, %v, %v", ticket, found, err)
	}
	replay, err := service.Report(t.Context(), binding, in, nil)
	if err != nil || !replay.Replayed || replay.ID != published.ID || replay.MessageID != published.MessageID {
		t.Fatalf("recovered receipt was not stable: %+v, %v", replay, err)
	}
	follow := ticketInput(address, "reopen")
	follow.TicketID = ticket.ID
	next := admit(t, service, follow)
	if err := service.ReconcilePublishedReports(t.Context(), binding.ExecutionID, []workticket.PublishedReport{published}); err != nil {
		t.Fatal(err)
	}
	row, found, err := service.ForMessage(t.Context(), address, "reopen")
	if err != nil || !found || row.Status != "active" || row.Generation != next.Generation {
		t.Fatalf("reconciliation changed the new current generation: %+v %v %v", row, found, err)
	}
}

func TestLegacyOrdinaryPendingConclusionRepairsBlockedTicket(t *testing.T) {
	db, service, address := newTicketChat(t, nil)
	ctx := t.Context()
	binding := admit(t, service, ticketInput(address, "legacy-pending"))
	report := models.WorkTicketReport{ID: "legacy-receipt", ExecutionID: binding.ExecutionID,
		TicketID: binding.Ticket.ID, Status: constants.WorkTicketStatusCompleted, Content: "verified legacy conclusion"}
	// 旧版本只预留报告。原日志已写入结论但工单文字与结案没有在预留时更新。
	if err := db.Create(&report).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.FinishTurn(ctx, binding, constants.ExecutionStatusFailed); err != nil {
		t.Fatal(err)
	}
	if before, _, err := service.ForMessage(ctx, address, binding.Ticket.ID); err != nil || before.Status != constants.WorkTicketStatusBlocked {
		t.Fatalf("legacy fixture did not have the original blocked state: %+v %v", before, err)
	}
	if err := service.ReconcilePublishedReports(ctx, binding.ExecutionID, []workticket.PublishedReport{{
		ID: report.ID, MessageID: binding.ExecutionID, Content: report.Content,
	}}); err != nil {
		t.Fatal(err)
	}
	if after, found, err := service.ForMessage(ctx, address, binding.ExecutionID); err != nil || !found ||
		after.Status != constants.WorkTicketStatusCompleted || after.Summary != report.Content {
		t.Fatalf("legacy published conclusion did not repair original state: %+v found=%v %v", after, found, err)
	}
}

func TestStopWaitsUntilAdmittedContinuationCanBeCanceledExactly(t *testing.T) {
	_, service, address := newTicketChat(t, nil)
	first := admit(t, service, ticketInput(address, "first"))
	if err := service.FinishTurn(t.Context(), first, constants.ExecutionStatusCompleted); err != nil {
		t.Fatal(err)
	}
	follow := ticketInput(address, "follow")
	follow.TicketID = first.Ticket.ID
	unlock := sync.OnceFunc(service.LockAdmission(follow))
	defer unlock()
	pending, err := service.PrepareAdmission(t.Context(), follow)
	if err != nil {
		t.Fatal(err)
	}
	type stopResult struct {
		// ticket 保留应取消的精确执行。
		ticket models.WorkTicket
		// err 是停止事务返回的错误。
		err error
	}
	stopping := make(chan struct{})
	stopped := make(chan stopResult, 1)
	go func() {
		close(stopping)
		ticket, err := service.Stop(t.Context(), address, follow.TicketID)
		stopped <- stopResult{ticket: ticket, err: err}
	}()
	<-stopping
	select {
	case got := <-stopped:
		t.Fatalf("stop crossed the unconfirmed admission: %+v", got)
	case <-time.After(20 * time.Millisecond):
	}
	if err := service.ConfirmAdmission(t.Context(), pending); err != nil {
		t.Fatal(err)
	}
	unlock()
	got := <-stopped
	if got.err != nil || got.ticket.CurrentExecutionID != pending.ExecutionID || got.ticket.Status != "stopped" {
		t.Fatalf("stop must cancel acknowledged continuation: %+v", got)
	}
}
