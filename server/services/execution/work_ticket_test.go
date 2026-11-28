package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/cli"
	"github.com/JieWaZi/NeuxBot/server/internal/projectsource"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestRegularWorkTicketUsesOriginalChatAcrossTurns(t *testing.T) {
	ctx := t.Context()
	manager := newTicketExecutionFixture(t)
	core := manager.execution.(*service)
	options := WorkOptions{EntryPoint: WorkbenchEntryPoint, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}
	projectID := ticketExecutionProject(t, manager)
	first := WorkIntent{
		ClientRequestID: "first-ticket", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", ProjectID: projectID}},
		Input: WorkInput{Actor: "user", Content: "Prepare report A"}, Options: options, Ticket: WorkTicketInput{Start: true},
	}
	start := func(intent WorkIntent, wait bool) AcceptedExecution {
		t.Helper()
		accepted, err := core.Start(ctx, intent)
		if err != nil {
			t.Fatal(err)
		}
		if wait {
			waitForChatTerminal(t, manager, accepted.Chat.ID)
		}
		return accepted
	}
	a := start(first, true)
	address := workticket.Address{Kind: constants.WorkTicketOriginChat, ID: a.Chat.ID}
	ticketFor := func(messageID string) models.WorkTicket {
		t.Helper()
		ticket, found, err := core.workTickets.ForMessage(ctx, address, messageID)
		if err != nil || !found {
			t.Fatalf("ticket missing for %s: %v", messageID, err)
		}
		return ticket
	}
	aTicket := ticketFor(a.Execution.UserMessageID)
	if aTicket.ID != a.Execution.UserMessageID || aTicket.Status != constants.WorkTicketStatusWaiting || aTicket.ProjectID != projectID || aTicket.WorkChatID != a.Chat.ID {
		t.Fatalf("CLI completion closed or displaced the business ticket: %+v", aTicket)
	}
	replayed := start(first, true)
	if !replayed.Replayed || replayed.Execution.ID != a.Execution.ID {
		t.Fatalf("initial request was redispatched: %+v", replayed)
	}
	base := WorkIntent{Chat: ChatTarget{ChatID: a.Chat.ID}, Input: WorkInput{Actor: "user"}, Options: options}
	bInput := base
	bInput.Input.SubmissionID, bInput.Input.Content = "ticket-b", "Prepare report B"
	bInput.Ticket.Start = true
	b := start(bInput, true)
	bTicket := ticketFor(b.Execution.UserMessageID)
	if bTicket.ID == aTicket.ID || bTicket.ProjectID != aTicket.ProjectID {
		t.Fatalf("two inputs to the same project did not create independent tickets: A=%+v B=%+v", aTicket, bTicket)
	}
	plain := base
	plain.Input.SubmissionID, plain.Input.Content = "plain-question", "What is the date?"
	plainResult := start(plain, true)
	if _, found, err := core.workTickets.ForMessage(ctx, address, plainResult.Execution.UserMessageID); found || err != nil {
		t.Fatalf("an ordinary question created a ticket: found=%t err=%v", found, err)
	}
	more := base
	more.Input.SubmissionID, more.Input.Content = "supplement-a", "Use the revised statistics, hold-for-stream"
	more.Input.ReplyToMessageID = a.Execution.UserMessageID
	supplement := start(more, false)
	continued := ticketFor(supplement.Execution.UserMessageID)
	if continued.ID != aTicket.ID || continued.Generation != aTicket.Generation || continued.CurrentExecutionID != supplement.Execution.ID {
		t.Fatalf("continuation did not reuse the same assignment: before=%+v after=%+v", aTicket, continued)
	}
	runTicketReportCLI(t, core, supplement.Execution.ID, "progress-a", "progress", "Verified the source figures for A.")
	result := runTicketReportCLI(t, core, supplement.Execution.ID, "report-a", "completed", "The revised statistics for A are ready.")
	if result.Data.(map[string]any)["messageId"] != supplement.Execution.ID || result.CommandID == "" {
		t.Fatalf("report did not publish to original assistant thread: %+v", result)
	}
	duplicate := runTicketReportCLI(t, core, supplement.Execution.ID, "report-a", "completed", "The revised statistics for A are ready.")
	if !duplicate.Replayed || duplicate.CommandID != result.CommandID {
		t.Fatalf("report replay changed receipt: %+v vs %+v", result, duplicate)
	}
	oldPath := os.Getenv(constants.AgentCLIContextEnvironment)
	oldGrant, err := capability.ReadConnection(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := core.CancelExecutionAndWait(ctx, a.Chat.ID, supplement.Execution.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("the prior report descriptor survived execution cleanup: %v", err)
	}
	if ticket := ticketFor(a.Execution.UserMessageID); ticket.Status != "completed" || ticket.Summary != "The revised statistics for A are ready." {
		t.Fatalf("the host terminal overwrote the report: %+v", ticket)
	}
	if ticket := ticketFor(b.Execution.UserMessageID); ticket.ID != bTicket.ID || ticket.Status != "waiting" || ticket.Summary != "" {
		t.Fatalf("A settled B: %+v", ticket)
	}
	message, err := manager.chats.PublicReadingMessage(ctx, a.Chat.ID, supplement.Execution.ID)
	if err != nil || message == nil || !strings.Contains(message.Content, "Verified the source figures for A.") || !strings.Contains(message.Content, "The revised statistics for A are ready.") {
		t.Fatalf("the report is missing from the original Chat history: %+v, %v", message, err)
	}
	quotedReport, err := core.journal.ReadReplyTarget(ctx, a.Chat.ID, supplement.Execution.ID)
	if err != nil || !strings.Contains(quotedReport.Text, "The revised statistics for A are ready.") {
		t.Fatalf("explicit report is missing from the quote without a native final answer: %+v, %v", quotedReport, err)
	}
	if got := start(more, true); !got.Replayed || got.Execution.ID != supplement.Execution.ID {
		t.Fatalf("supplement replay was redispatched: %+v", got)
	}
	bFollow := base
	bFollow.Input.SubmissionID, bFollow.Input.Content = "supplement-b", "Keep work B open: hold-for-stream"
	bFollow.Input.ReplyToMessageID = b.Execution.UserMessageID
	bRunning := start(bFollow, false)
	runTicketReportCLI(t, core, bRunning.Execution.ID, "report-b", "progress", "Working on B.")
	retainedDirectory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	retainedPath, err := capability.WriteConnection(retainedDirectory, "prior-turn.json", oldGrant)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(constants.AgentCLIContextEnvironment, retainedPath)
	var staleOut, staleErr bytes.Buffer
	code := cli.Execute(ctx, []string{"work", "tickets", "report", "--request-id", "stale-a", "--kind", "completed", "--content", "A late typed result"}, cli.IO{Out: &staleOut, Err: &staleErr})
	if code == 0 {
		t.Fatalf("A's retired CLI context gained B's report grant: %s", staleOut.String())
	}
	if ticket := ticketFor(b.Execution.UserMessageID); ticket.Status != constants.WorkTicketStatusActive || ticket.Summary != "Working on B." {
		t.Fatalf("stale report altered the current ticket: %+v", ticket)
	}
	if err := core.CancelExecutionAndWait(ctx, a.Chat.ID, bRunning.Execution.ID); err != nil {
		t.Fatal(err)
	}
	reopen := base
	reopen.Input.SubmissionID, reopen.Input.Content = "follow-up-a", "Please extend the report."
	reopen.Input.ReplyToMessageID = supplement.Execution.ID
	projectDirectories, err := manager.projects.ListDirectories(ctx, projectID)
	if err != nil || len(projectDirectories) != 1 {
		t.Fatalf("read project scope: %+v, %v", projectDirectories, err)
	}
	projectDirectories[0].LoadInstructions = false
	if err := manager.projects.ReplaceDirectories(ctx, projectID, projectDirectories); err != nil {
		t.Fatal(err)
	}
	if _, err := core.Start(ctx, reopen); !errors.Is(err, status_error.WorkTicketCannotContinue) {
		t.Fatalf("a reopened ticket took a changed project mandate: %v", err)
	}
	projectDirectories[0].LoadInstructions = true
	if err := manager.projects.ReplaceDirectories(ctx, projectID, projectDirectories); err != nil {
		t.Fatal(err)
	}
	opened := start(reopen, true)
	reopened := ticketFor(opened.Execution.UserMessageID)
	if reopened.ID != aTicket.ID || reopened.Generation <= aTicket.Generation || reopened.Status != "waiting" {
		t.Fatalf("explicit report follow-up failed to reopen the same ticket: %+v", reopened)
	}
	if _, err := core.workTickets.Stop(ctx, address, aTicket.ID); err != nil {
		t.Fatal(err)
	}
	reopen.Input.SubmissionID = "stopped-ticket-follow-up"
	if _, err := core.Start(ctx, reopen); !errors.Is(err, status_error.WorkTicketCannotContinue) {
		t.Fatalf("the stopped assignment accepted more work: %v", err)
	}
}

func TestMainWorkTicketReportsBackToTheOriginalPublicRequest(t *testing.T) {
	ctx := t.Context()
	manager := newTicketExecutionFixture(t)
	core := manager.execution.(*service)
	publisher := mainconversation.New(controllers.NewMainConversationController(manager.db),
		controllers.NewTransactionController(dbhelper.NewGormTransactionManager(manager.db)), manager.wakers, controllers.NewChannelController(manager.db))
	conversation, err := publisher.Ensure(ctx, "waker", "waker-1")
	if err != nil {
		t.Fatal(err)
	}
	work, err := publisher.ReserveWork(ctx, "waker", "waker-1", "waker-1", "managed")
	if err != nil {
		t.Fatal(err)
	}
	address := workticket.Address{Kind: constants.WorkTicketOriginMain, ID: conversation.ID}
	send := func(submission, content, quote string, first bool) AcceptedExecution {
		t.Helper()
		publicID := mainconversation.UserMessageID(conversation.ID, submission)
		intent := WorkIntent{
			ExecutionID: publicID, Input: WorkInput{Actor: "user", SubmissionID: submission, Content: content},
			Chat: ChatTarget{ChatID: work.WorkChatID}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
			Options: WorkOptions{EntryPoint: MainConversationEntryPoint, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto,
				ToolCapabilities: MainConversationTools(work.WorkChatID, publicID, publicID, publisher)},
			Ticket: WorkTicketInput{Start: first, Address: address, OwnerKind: "waker", OwnerID: "waker-1", PublicMessageID: publicID,
				PublicReplyID: quote, MainConversation: publisher},
			AdmissionBoundary: func(ctx context.Context, commit func(context.Context) error) error {
				_, err := publisher.AdmitUser(ctx, "waker", "waker-1", submission, content, quote, work.WorkChatID, publicID, "", "", "", commit)
				return err
			},
		}
		if first {
			intent.Chat.MainConversationSource = &CreateChatSpec{WakerID: "waker-1"}
		}
		accepted, err := core.Start(ctx, intent)
		if err != nil {
			t.Fatal(err)
		}
		return accepted
	}
	origin := send("original-main", "Review the work and report.", "", true)
	waitForChatTerminal(t, manager, work.WorkChatID)
	beforeReport, _, err := publisher.Messages(ctx, conversation.ID, 0, 10)
	if err != nil || len(beforeReport) != 2 || beforeReport[0].ID != origin.Execution.ID ||
		beforeReport[1].Content != "fixture reply" || beforeReport[1].ReplyToMessageID != origin.Execution.ID {
		t.Fatalf("verified native final did not return to the public ticket: %+v, %v", beforeReport, err)
	}
	ongoing := send("follow-up-main", "Include this week's figures: hold-for-stream", origin.Execution.ID, false)
	result := runTicketReportCLI(t, core, ongoing.Execution.ID, "main-report", "completed", "Verified this week's results.")
	reportID, _ := result.Data.(map[string]any)["messageId"].(string)
	message, err := publisher.Message(ctx, conversation.ID, reportID)
	if err != nil || message.ReplyToMessageID != origin.Execution.ID || message.SourceChatID != work.WorkChatID ||
		message.SourceExecutionID != ongoing.Execution.ID || message.Content != "Verified this week's results." {
		t.Fatalf("report did not keep original public address and current source: %+v, %v", message, err)
	}
	ticket, found, err := core.workTickets.ForMessage(ctx, address, message.ID)
	if err != nil || !found || ticket.ID != origin.Execution.ID || ticket.Status != constants.WorkTicketStatusCompleted {
		t.Fatalf("result cannot be used to follow up on the original ticket: %+v, found=%t, %v", ticket, found, err)
	}
	if err := core.CancelExecutionAndWait(ctx, work.WorkChatID, ongoing.Execution.ID); err != nil {
		t.Fatal(err)
	}
}

func TestWorkTicketAdmissionErrorAfterJournalCommitPreservesAcceptedInput(t *testing.T) {
	ctx := t.Context()
	manager := newTicketExecutionFixture(t)
	core := manager.execution.(*service)
	attachment := filepath.Join(t.TempDir(), "evidence.txt")
	if err := os.WriteFile(attachment, []byte("original evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	intent := WorkIntent{
		ClientRequestID: "post-commit-error", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}},
		Input:  WorkInput{Actor: "user", Content: "Look at this evidence.", Attachments: []AttachmentInput{{SourcePath: attachment}}},
		Ticket: WorkTicketInput{Start: true}, Options: WorkOptions{EntryPoint: WorkbenchEntryPoint, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
		AdmissionBoundary: func(ctx context.Context, commit func(context.Context) error) error {
			if err := commit(ctx); err != nil {
				return err
			}
			return status_error.DatabaseBusy
		},
	}
	if _, err := core.Start(ctx, intent); !errors.Is(err, status_error.DatabaseBusy) {
		t.Fatalf("wanted post-journal failure, got %v", err)
	}
	chatID := (requestidempotency.Identity{ID: intent.ClientRequestID}).ResourceID("chat", "waker-1")
	waitForChatTerminal(t, manager, chatID)
	page, err := manager.chats.ReadMessageHistory(ctx, chatID, 0, 10)
	if err != nil || len(page.Resources) != 2 || len(page.Resources[0].Attachments) != 1 {
		t.Fatalf("committed input and attachments disappeared: %+v, %v", page.Resources, err)
	}
	directory, err := core.storage.ChatRoot(chatID)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(page.Resources[0].Attachments[0].Path)))
	if err != nil || string(content) != "original evidence" {
		t.Fatalf("durable attachment was rolled back: %q, %v", content, err)
	}
	accepted, err := core.Start(ctx, intent)
	if err != nil || !accepted.Replayed || accepted.Execution.Status != constants.ExecutionStatusFailed || len(manager.nativeCalls(t, "turn/start")) != 0 {
		t.Fatalf("same request was lost or silently relaunched: %+v, %v", accepted, err)
	}
	address := workticket.Address{Kind: constants.WorkTicketOriginChat, ID: chatID}
	ticket, found, err := core.workTickets.ForMessage(ctx, address, accepted.Execution.UserMessageID)
	if err != nil || !found || ticket.Status != constants.WorkTicketStatusBlocked {
		t.Fatalf("the replay left unlaunched work marked active: %+v, found=%t, %v", ticket, found, err)
	}
}

func TestWorkTicketContinuationUsesRecoveredJournalTerminal(t *testing.T) {
	ctx := t.Context()
	manager := newTicketExecutionFixture(t)
	core := manager.execution.(*service)
	options := WorkOptions{EntryPoint: WorkbenchEntryPoint, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}
	first, err := core.Start(ctx, WorkIntent{
		ClientRequestID: "interrupted-work", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}},
		Input: WorkInput{Actor: "user", Content: "Gather the evidence", ContextInstructions: []workinput.Instruction{{Name: "Fixture", Content: "provider-failure"}}}, Options: options,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	recoveryReceipt, err := manager.chats.ReadExecution(ctx, first.Chat.ID, first.Execution.ID)
	if err != nil || recoveryReceipt.Status != constants.ExecutionStatusFailed {
		t.Fatalf("missing durable terminal for the prior turn: %+v, %v", recoveryReceipt, err)
	}
	// 模拟权威日志已恢复终态，但此前 Core 未执行工单的进程内结束回调。
	address := workticket.Address{Kind: constants.WorkTicketOriginChat, ID: first.Chat.ID}
	binding, err := core.workTickets.PrepareAdmission(ctx, workticket.Admission{
		Address: address, MessageID: recoveryReceipt.UserMessageID, SourceMessageID: recoveryReceipt.UserMessageID,
		WorkChatID: first.Chat.ID, ExecutionID: recoveryReceipt.ID, SubmissionID: recoveryReceipt.SubmissionID,
		SubmissionHash: recoveryReceipt.SubmissionHash, OwnerKind: "waker", OwnerID: first.Chat.WakerID, OwnerWakerID: first.Chat.WakerID,
		WorkingDirectory: first.Chat.WorkspaceDirectory, AllowedDirectories: first.Chat.AdditionalWorkingDirectories, PermissionLevel: recoveryReceipt.PermissionLevel,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := core.workTickets.ConfirmAdmission(ctx, binding); err != nil {
		t.Fatal(err)
	}
	continued, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID}, Options: options,
		Input: WorkInput{Actor: "user", Content: "Please continue collecting evidence.", SubmissionID: "resume-recovered-work", ReplyToMessageID: recoveryReceipt.UserMessageID},
	})
	if err != nil {
		t.Fatalf("durable terminal did not unblock an explicit continuation: %v", err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	ticket, found, err := core.workTickets.ForMessage(ctx, address, continued.Execution.UserMessageID)
	if err != nil || !found || ticket.ID != recoveryReceipt.UserMessageID || ticket.Generation != binding.Generation+1 || ticket.Status != constants.WorkTicketStatusWaiting {
		t.Fatalf("the interruption should advance the attempt on the same ticket: %+v, found=%t, %v", ticket, found, err)
	}
}

func TestOrdinaryWorkTicketQuoteRepairsPendingPublishedReport(t *testing.T) {
	ctx := t.Context()
	manager := newTicketExecutionFixture(t)
	core := manager.execution.(*service)
	options := WorkOptions{EntryPoint: WorkbenchEntryPoint, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}
	first, err := core.Start(ctx, WorkIntent{
		ClientRequestID: "interrupted-publication", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}},
		Input: WorkInput{Actor: "user", Content: "Quantify the evidence."}, Options: options,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	address := workticket.Address{Kind: constants.WorkTicketOriginChat, ID: first.Chat.ID}
	binding, err := core.workTickets.PrepareAdmission(ctx, workticket.Admission{
		Address: address, MessageID: first.Execution.UserMessageID, SourceMessageID: first.Execution.UserMessageID,
		WorkChatID: first.Chat.ID, ExecutionID: first.Execution.ID, SubmissionID: first.Execution.SubmissionID,
		SubmissionHash: first.Execution.SubmissionHash, OwnerKind: "waker", OwnerID: first.Chat.WakerID, OwnerWakerID: first.Chat.WakerID,
		WorkingDirectory: first.Chat.WorkspaceDirectory, AllowedDirectories: first.Chat.AdditionalWorkingDirectories, PermissionLevel: first.Execution.PermissionLevel,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := core.workTickets.ConfirmAdmission(ctx, binding); err != nil {
		t.Fatal(err)
	}
	const report = "The evidence has been checked."
	turn := workTicketTurn{tickets: core.workTickets, journal: core.journal, binding: binding}
	_, err = core.workTickets.Report(ctx, binding, workticket.ReportInput{RequestID: "report-before-interruption", Kind: "completed", Content: report},
		func(ctx context.Context, ticket models.WorkTicket, key string) (string, error) {
			messageID, err := turn.deliverOrdinary(ctx, ticket, key, report)
			if err != nil {
				return "", err
			}
			return messageID, status_error.DatabaseBusy
		})
	if !errors.Is(err, status_error.DatabaseBusy) {
		t.Fatalf("expected failure after durable report publication, got %v", err)
	}
	if _, found, err := core.workTickets.ForMessage(ctx, address, first.Execution.ID); found || err != nil {
		t.Fatalf("report receipt should still be pending: found=%t, err=%v", found, err)
	}
	reply, err := core.journal.ReadReplyTarget(ctx, first.Chat.ID, first.Execution.ID)
	if err != nil || !strings.Contains(reply.Text, report) {
		t.Fatalf("report absent from original chat: %+v, %v", reply, err)
	}
	continued, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID}, Options: options,
		Input: WorkInput{Actor: "user", Content: "Add the supporting sources.", SubmissionID: "quote-recovered-report", ReplyToMessageID: first.Execution.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	ticket, found, err := core.workTickets.ForMessage(ctx, address, continued.Execution.UserMessageID)
	if err != nil || !found || ticket.ID != first.Execution.UserMessageID || ticket.Generation != binding.Generation+1 {
		t.Fatalf("explicit quote bypassed its interrupted report receipt: %+v, found=%t, err=%v", ticket, found, err)
	}
}

func newTicketExecutionFixture(t *testing.T) messageTestService {
	t.Helper()
	registry := capability.New(nil)
	t.Cleanup(registry.Close)
	host := httptest.NewServer(registry)
	t.Cleanup(host.Close)
	binary := filepath.Join(t.TempDir(), "botcli")
	if err := os.WriteFile(binary, []byte("fixture CLI"), 0700); err != nil {
		t.Fatal(err)
	}
	manager := newMessageTestService(t, t.TempDir(), messageTestOptions{agent: registry, agentBinary: binary})
	core := manager.execution.(*service)
	core.agentEndpoint = host.URL + constants.AgentCLIPath
	transactions := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(manager.db))
	core.workTickets = workticket.New(controllers.NewWorkTicketController(manager.db), transactions, nil, nil)
	return manager
}

func ticketExecutionProject(t *testing.T, manager messageTestService) string {
	t.Helper()
	ctx := t.Context()
	project := models.Project{ID: "ticket-project", WakerID: "waker-1", Name: "Work ticket fixture"}
	if err := manager.projects.Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	source, err := projectsource.Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.projectSources.Ensure(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := manager.projects.ReplaceDirectories(ctx, project.ID, []models.ProjectDirectory{{Source: source, IsDefault: true, LoadInstructions: true}}); err != nil {
		t.Fatal(err)
	}
	return project.ID
}

func runTicketReportCLI(t *testing.T, core *service, executionID, key, kind, content string) capability.Result {
	t.Helper()
	contextRoot := filepath.Join(filepath.Dir(filepath.Dir(core.systemSkillDirectory)), constants.AgentCLIContextDirectory)
	connections, err := filepath.Glob(filepath.Join(contextRoot, "*", "*", "connection.json"))
	if err != nil {
		t.Fatal(err)
	}
	var last string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, connection := range connections {
			t.Setenv(constants.AgentCLIContextEnvironment, connection)
			var stdout, stderr bytes.Buffer
			code := cli.Execute(t.Context(), []string{"work", "tickets", "report", "--request-id", key, "--kind", kind, "--content", content}, cli.IO{Out: &stdout, Err: &stderr})
			if code == 0 {
				var response capability.Response
				if err := json.Unmarshal(stdout.Bytes(), &response); err != nil || !response.OK {
					t.Fatalf("invalid report CLI output: %s, %v", stdout.String(), err)
				}
				return response.Result
			}
			last = stderr.String()
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("report command was never accepted for execution %s: %s", executionID, last)
	return capability.Result{}
}
