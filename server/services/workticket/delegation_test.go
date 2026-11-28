package workticket_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
)

func TestDelegationReturnsOnlyToDirectParentsAndFencesConclusion(t *testing.T) {
	ctx := t.Context()
	db, service, origin := newTicketChat(t, nil)
	root := admit(t, service, ticketInput(origin, "root"))
	for _, row := range []any{
		&models.Waker{ID: "b-lead", Name: "B Lead"}, &models.Waker{ID: "c", Name: "C"},
		&models.Channel{ID: "b", Name: "Team B", LeadWakerID: "b-lead"},
		&models.MainConversation{ID: "main-b", SubjectType: "team", SubjectID: "b"},
		&models.MainConversation{ID: "main-c", SubjectType: "waker", SubjectID: "c"},
		&models.MainConversation{ID: "main-a", SubjectType: "waker", SubjectID: "owner"},
		&models.MainConversationWork{ConversationID: "main-b", WorkChatID: "work-b", WakerID: "b-lead", ScopeKey: "b"},
		&models.MainConversationWork{ConversationID: "main-c", WorkChatID: "work-c", WakerID: "c", ScopeKey: "c"},
		&models.MainConversationWork{ConversationID: "main-a", WorkChatID: "work-a", WakerID: "owner", ScopeKey: "a"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	makeChild := func(parent workticket.Binding, input workticket.DelegationInput) models.WorkTicket {
		t.Helper()
		child, err := service.Delegate(ctx, parent, input)
		if err != nil {
			t.Fatal(err)
		}
		return child
	}
	start := func(child models.WorkTicket) workticket.Binding {
		t.Helper()
		if err := db.Create(&models.Chat{ID: child.WorkChatID, WakerID: child.OwnerWakerID, HiddenFromWakerHistory: true}).Error; err != nil {
			t.Fatal(err)
		}
		return admit(t, service, workticket.Admission{Address: workticket.Address{Kind: child.OriginKind, ID: child.OriginID},
			TicketID: child.ID, WorkChatID: child.WorkChatID, MessageID: child.ID, SourceMessageID: child.ID,
			ExecutionID: child.ID, SubmissionID: child.ID, SubmissionHash: "original", OwnerKind: child.OwnerKind, OwnerID: child.OwnerID,
			OwnerWakerID: child.OwnerWakerID, PermissionLevel: constants.PermissionLevelAuto})
	}
	bInput := workticket.DelegationInput{RequestID: "b", Target: workticket.Address{Kind: constants.WorkTicketOriginMain, ID: "main-b"},
		OwnerKind: "team", OwnerID: "b", OwnerWakerID: "b-lead", WorkChatID: "work-b", Content: "Coordinate the fixture findings."}
	b := makeChild(root, bInput)
	if duplicate := makeChild(root, bInput); duplicate.ID != b.ID {
		t.Fatal("same delegation allocated another ticket")
	}
	conflict := bInput
	conflict.Content = "Changed task"
	if _, err := service.Delegate(ctx, root, conflict); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("changed payload reused receipt: %v", err)
	}
	bindingB := start(b)
	cInput := workticket.DelegationInput{RequestID: "c", Target: workticket.Address{Kind: constants.WorkTicketOriginMain, ID: "main-c"},
		OwnerKind: "waker", OwnerID: "c", OwnerWakerID: "c", WorkChatID: "work-c", Content: "Return the fixture observation."}
	bindingC := start(makeChild(bindingB, cInput))
	cycle := cInput
	cycle.Target.ID, cycle.OwnerID, cycle.OwnerWakerID, cycle.WorkChatID, cycle.RequestID = "main-a", "owner", "owner", "work-a", "cycle"
	if _, err := service.Delegate(ctx, bindingC, cycle); !errors.Is(err, status_error.WorkTicketCannotContinue) {
		t.Fatalf("delegation loop allowed: %v", err)
	}
	result := workticket.ReportInput{RequestID: "done", Kind: "completed", Content: "Verified result."}
	var public int
	publish := func(context.Context, models.WorkTicket, string) (string, error) {
		public++
		return root.ExecutionID, nil
	}
	for _, pending := range []workticket.Binding{root, bindingB} {
		if _, err := service.Report(ctx, pending, result, publish); !errors.Is(err, status_error.WorkTicketCannotContinue) {
			t.Fatalf("unsettled direct children did not fence completion: %v", err)
		}
	}
	stale := bindingC
	stale.Generation++
	if _, err := service.Report(ctx, stale, result, publish); !errors.Is(err, status_error.WorkTicketAttemptUnavailable) {
		t.Fatalf("stale child reported: %v", err)
	}
	if err := db.Model(&models.Channel{}).Where("id = ?", "b").Update("lead_waker_id", "owner").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Report(ctx, bindingC, result, publish); !errors.Is(err, status_error.WorkTicketAttemptUnavailable) {
		t.Fatalf("child could report through revoked parent authority: %v", err)
	}
	if err := db.Model(&models.Channel{}).Where("id = ?", "b").Update("lead_waker_id", "b-lead").Error; err != nil {
		t.Fatal(err)
	}
	question, err := service.Report(ctx, bindingC, workticket.ReportInput{RequestID: "which-fixture", Kind: "waiting", Content: "Which fixture should I use?"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReplyToDelegation(ctx, root, "outside-question", question.ID, "wrong parent"); !errors.Is(err, status_error.WorkTicketAttemptUnavailable) {
		t.Fatalf("grandparent could answer a child question: %v", err)
	}
	answer, err := service.ReplyToDelegation(ctx, bindingB, "answer-c", question.ID, "Use fixture Alpha")
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := service.ReplyToDelegation(ctx, bindingB, "answer-c", question.ID, answer.Content); err != nil || replay.ID != answer.ID {
		t.Fatalf("private answer retry changed the pending receipt: %+v %v", replay, err)
	}
	if _, err := service.ReplyToDelegation(ctx, bindingB, "answer-c", question.ID, "a different answer"); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("a changed answer reused the known request key: %v", err)
	}
	var launches []workticket.DelegationReplyLaunch
	collect := func(launch workticket.DelegationReplyLaunch) error {
		launches = append(launches, launch)
		return nil
	}
	if err := service.ReadyDelegationReplies(ctx, collect); err != nil || len(launches) != 0 {
		t.Fatalf("child reply was launched before its original turn finished: %+v %v", launches, err)
	}
	if err := service.FinishTurn(ctx, bindingC, constants.ExecutionStatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := service.ReadyDelegationReplies(ctx, collect); err != nil || len(launches) != 1 || launches[0].Reply.ID != answer.ID {
		t.Fatalf("original child answer not ready: %+v %v", launches, err)
	}
	prior := bindingC
	child := launches[0].Ticket
	bindingC = admit(t, service, workticket.Admission{Address: cInput.Target, TicketID: child.ID, WorkChatID: child.WorkChatID,
		MessageID: answer.ID, SourceMessageID: "private-answer-input", ExecutionID: answer.ID, SubmissionID: answer.ID, SubmissionHash: "answer-input",
		OwnerKind: child.OwnerKind, OwnerID: child.OwnerID, OwnerWakerID: child.OwnerWakerID, PermissionLevel: constants.PermissionLevelAuto,
		ResumeAfterExecutionID: prior.ExecutionID, DelegationReplyID: answer.ID})
	if err := service.ReadyDelegationReplies(ctx, collect); err != nil || len(launches) != 1 {
		t.Fatalf("confirmed answer remained queued again: %+v %v", launches, err)
	}
	receipt, err := service.Report(ctx, bindingC, result, publish)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate, err := service.Report(ctx, bindingC, result, publish); err != nil || !duplicate.Replayed || duplicate.ID != receipt.ID {
		t.Fatalf("child result was not idempotent: %+v, %v", duplicate, err)
	}
	if reports, err := service.DelegationReports(ctx, root, ""); err != nil || len(reports) != 0 {
		t.Fatalf("grandchild skipped direct return: %+v, %v", reports, err)
	}
	if reports, err := service.DelegationReports(ctx, bindingB, ""); err != nil || len(reports) != 2 ||
		(reports[0].ID != receipt.ID && reports[1].ID != receipt.ID) {
		t.Fatalf("direct parent did not receive the original question and the child result: %+v, %v", reports, err)
	}
	if _, err := service.Report(ctx, bindingB, result, publish); err != nil {
		t.Fatal(err)
	}
	if reports, err := service.DelegationReports(ctx, root, ""); err != nil || len(reports) != 1 || reports[0].TicketID != b.ID {
		t.Fatalf("root did not get only Team B's result: %+v, %v", reports, err)
	}
	if _, err := service.Report(ctx, root, result, publish); err != nil || public != 1 {
		t.Fatalf("root did not publish exactly one final report: %d, %v", public, err)
	}
}

func TestDelegationFailedQuestionSettlesButFailedRequiredChildBlocks(t *testing.T) {
	for _, questionOnly := range []bool{false, true} {
		ctx := t.Context()
		db, service, origin := newTicketChat(t, nil)
		root := admit(t, service, ticketInput(origin, "parent"))
		for _, row := range []any{
			&models.Waker{ID: "other", Name: "Other"},
			&models.MainConversation{ID: "private-target", SubjectType: "waker", SubjectID: "other"},
			&models.MainConversationWork{ConversationID: "private-target", WorkChatID: "private-work", WakerID: "other", ScopeKey: "delegation"},
			&models.Chat{ID: "private-work", WakerID: "other", HiddenFromWakerHistory: true},
		} {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
		address := workticket.Address{Kind: constants.WorkTicketOriginMain, ID: "private-target"}
		child, err := service.Delegate(ctx, root, workticket.DelegationInput{RequestID: "direct", Target: address,
			OwnerKind: "waker", OwnerID: "other", OwnerWakerID: "other", WorkChatID: "private-work", Content: "Inspect fixture", QuestionOnly: questionOnly})
		if err != nil {
			t.Fatal(err)
		}
		started := admit(t, service, workticket.Admission{Address: address, TicketID: child.ID, WorkChatID: child.WorkChatID, MessageID: child.ID,
			SourceMessageID: child.ID, ExecutionID: child.ID, SubmissionID: child.ID, SubmissionHash: "child", OwnerKind: "waker", OwnerID: "other",
			OwnerWakerID: "other", PermissionLevel: constants.PermissionLevelAuto})
		wait := workticket.ReportInput{RequestID: "ask", Kind: "waiting", Content: "Which fixture should I use?"}
		if _, err := service.Report(ctx, started, wait, nil); err != nil {
			t.Fatal(err)
		}
		returns, err := service.DelegationReports(ctx, root, "")
		if err != nil || len(returns) != 1 || returns[0].Status != constants.WorkTicketStatusWaiting || returns[0].Content != wait.Content {
			t.Fatalf("waiting question did not reach the direct parent: %+v %v", returns, err)
		}
		if err := service.FinishTurn(ctx, root, constants.ExecutionStatusCompleted); err != nil {
			t.Fatal(err)
		}
		if ready, err := service.ReadyDelegationParents(ctx, "", 10); err != nil || len(ready) != 0 {
			t.Fatalf("pending question released the dependency: %+v %v", ready, err)
		}
		if err := service.FinishTurn(ctx, started, constants.ExecutionStatusFailed); err != nil {
			t.Fatal(err)
		}
		ready, err := service.ReadyDelegationParents(ctx, "", 10)
		if err != nil || (len(ready) == 1) != questionOnly {
			t.Fatalf("wrong release for questionOnly=%t: %+v %v", questionOnly, ready, err)
		}
	}
}

func TestDelegationParallelRequiredChildFailurePreventsParentCompletion(t *testing.T) {
	ctx := t.Context()
	db, service, origin := newTicketChat(t, nil)
	parent := admit(t, service, ticketInput(origin, "parent"))
	start := func(owner string) workticket.Binding {
		t.Helper()
		conversationID, chatID := "private-"+owner, "work-"+owner
		for _, row := range []any{
			&models.Waker{ID: owner, Name: owner},
			&models.MainConversation{ID: conversationID, SubjectType: "waker", SubjectID: owner},
			&models.MainConversationWork{ConversationID: conversationID, WorkChatID: chatID, WakerID: owner, ScopeKey: "delegation"},
			&models.Chat{ID: chatID, WakerID: owner, HiddenFromWakerHistory: true},
		} {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
		address := workticket.Address{Kind: constants.WorkTicketOriginMain, ID: conversationID}
		child, err := service.Delegate(ctx, parent, workticket.DelegationInput{RequestID: owner, Target: address,
			OwnerKind: "waker", OwnerID: owner, OwnerWakerID: owner, WorkChatID: chatID, Content: "Verify the fixture."})
		if err != nil {
			t.Fatal(err)
		}
		return admit(t, service, workticket.Admission{Address: address, TicketID: child.ID, WorkChatID: chatID,
			MessageID: child.ID, SourceMessageID: child.ID, ExecutionID: child.ID, SubmissionID: child.ID, SubmissionHash: "child",
			OwnerKind: "waker", OwnerID: owner, OwnerWakerID: owner, PermissionLevel: constants.PermissionLevelAuto})
	}
	successful, failed := start("successful"), start("failed")
	result := workticket.ReportInput{RequestID: "final", Kind: "completed", Content: "Verified result."}
	success, err := service.Report(ctx, successful, result, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.FinishTurn(ctx, successful, constants.ExecutionStatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := service.FinishTurn(ctx, failed, constants.ExecutionStatusFailed); err != nil {
		t.Fatal(err)
	}
	reports, err := service.DelegationReports(ctx, parent, "")
	if err != nil || len(reports) != 2 {
		t.Fatalf("parent did not retain both direct outcomes: %+v %v", reports, err)
	}
	byChild := make(map[string]models.WorkTicketReport, len(reports))
	for _, report := range reports {
		byChild[report.TicketID] = report
	}
	if byChild[successful.Ticket.ID].ID != success.ID || byChild[successful.Ticket.ID].Status != constants.WorkTicketStatusCompleted ||
		byChild[failed.Ticket.ID].Status != constants.WorkTicketStatusBlocked || byChild[failed.Ticket.ID].ExecutionID != failed.ExecutionID {
		t.Fatalf("parent received wrong child outcomes: %+v", reports)
	}
	publish := func(context.Context, models.WorkTicket, string) (string, error) {
		t.Fatal("parent published a conclusion despite the failed sibling")
		return "", nil
	}
	if _, err := service.Report(ctx, parent, result, publish); !errors.Is(err, status_error.WorkTicketCannotContinue) {
		t.Fatalf("failed required sibling did not fence the parent conclusion: %v", err)
	}
	if err := service.FinishTurn(ctx, parent, constants.ExecutionStatusCompleted); err != nil {
		t.Fatal(err)
	}
	if ready, err := service.ReadyDelegationParents(ctx, "", 10); err != nil || len(ready) != 0 {
		t.Fatalf("mixed outcomes scheduled the parent as successful: %+v %v", ready, err)
	}
}

func TestDelegationResumeFindsFinalBeyondReportHistoryLimit(t *testing.T) {
	ctx := t.Context()
	db, service, origin := newTicketChat(t, nil)
	root := admit(t, service, ticketInput(origin, "parent"))
	for _, row := range []any{
		&models.Waker{ID: "other", Name: "Other"},
		&models.MainConversation{ID: "private-target", SubjectType: "waker", SubjectID: "other"},
		&models.MainConversationWork{ConversationID: "private-target", WorkChatID: "private-work", WakerID: "other", ScopeKey: "delegation"},
		&models.Chat{ID: "private-work", WakerID: "other", HiddenFromWakerHistory: true},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	address := workticket.Address{Kind: constants.WorkTicketOriginMain, ID: "private-target"}
	child, err := service.Delegate(ctx, root, workticket.DelegationInput{RequestID: "direct", Target: address,
		OwnerKind: "waker", OwnerID: "other", OwnerWakerID: "other", WorkChatID: "private-work", Content: "Inspect fixture"})
	if err != nil {
		t.Fatal(err)
	}
	started := admit(t, service, workticket.Admission{Address: address, TicketID: child.ID, WorkChatID: child.WorkChatID, MessageID: child.ID,
		SourceMessageID: child.ID, ExecutionID: child.ID, SubmissionID: child.ID, SubmissionHash: "child", OwnerKind: "waker", OwnerID: "other",
		OwnerWakerID: "other", PermissionLevel: constants.PermissionLevelAuto})
	final, err := service.Report(ctx, started, workticket.ReportInput{RequestID: "final", Kind: "completed", Content: "Verified result."}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// All historical IDs sort ahead of the final, the harmful ordering for a limited general listing.
	rows := make([]models.WorkTicketReport, 115)
	for i := range rows {
		id := fmt.Sprintf("%032x", i)
		rows[i] = models.WorkTicketReport{ID: id, MessageID: id, TicketID: child.ID, ExecutionID: started.ExecutionID,
			Status: constants.WorkTicketStatusBlocked, Content: "Prior blocker"}
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.FinishTurn(ctx, root, constants.ExecutionStatusFailed); err != nil {
		t.Fatal(err)
	}
	ready, err := service.ReadyDelegationParents(ctx, "", 10)
	if err != nil || len(ready) != 1 || ready[0].QuestionID != "" || len(ready[0].Reports) != 1 || ready[0].Reports[0].ID != final.ID {
		t.Fatalf("authoritative final hidden by report history: %+v %v", ready, err)
	}
	resumed := admit(t, service, workticket.Admission{Address: origin, TicketID: root.Ticket.ID, WorkChatID: root.WorkChatID, MessageID: "resume-id",
		SourceMessageID: "resume-source", ExecutionID: "resume-id", SubmissionID: "resume-id", SubmissionHash: "resume", OwnerKind: root.Ticket.OwnerKind,
		OwnerID: root.Ticket.OwnerID, OwnerWakerID: root.Ticket.OwnerWakerID, PermissionLevel: root.Ticket.PermissionLevel,
		ProjectID: root.Ticket.ProjectID, ProjectScope: root.Ticket.ProjectScope, AllowedDirectories: root.Ticket.AllowedDirectories,
		ResumeAfterExecutionID: root.ExecutionID})
	if rows, err := service.DelegationReports(ctx, resumed, final.ID); err != nil || len(rows) != 1 || rows[0].ID != final.ID {
		t.Fatalf("resumed direct parent cannot retrieve exact full final: %+v %v", rows, err)
	}
}

func TestTeamMainDelegationRequiresCurrentMembership(t *testing.T) {
	ctx := t.Context()
	db, service, _ := newTicketChat(t, nil)
	const main = constants.WorkTicketOriginMain
	for _, row := range []any{
		&models.Waker{ID: "member", Name: "Member"},
		&models.Channel{ID: "team", Name: "Team", LeadWakerID: "owner"},
		&models.MainConversation{ID: "team-main", SubjectType: "team", SubjectID: "team"},
		&models.MainConversation{ID: "member-main", SubjectType: "waker", SubjectID: "member"},
		&models.MainConversationWork{ConversationID: "team-main", WorkChatID: "root-work", WakerID: "owner", ScopeKey: "root"},
		&models.MainConversationWork{ConversationID: "member-main", WorkChatID: "member-work", WakerID: "member", ScopeKey: "member"},
		&models.Chat{ID: "root-work", WakerID: "owner", HiddenFromWakerHistory: true},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	root := admit(t, service, workticket.Admission{Address: workticket.Address{Kind: main, ID: "team-main"}, WorkChatID: "root-work", MessageID: "team-root", SourceMessageID: "source", ExecutionID: "run", SubmissionID: "sub", SubmissionHash: "digest", OwnerKind: "team", OwnerID: "team", OwnerWakerID: "owner", PermissionLevel: constants.PermissionLevelAuto})
	in := workticket.DelegationInput{RequestID: "request", Target: workticket.Address{Kind: main, ID: "member-main"}, OwnerKind: "waker", OwnerID: "member", OwnerWakerID: "member", WorkChatID: "member-work", Content: "Check the fixture"}
	if _, err := service.Delegate(ctx, root, in); !errors.Is(err, status_error.AgentOperationForbidden) {
		t.Fatalf("out of team assignment: %v", err)
	}
	member := models.ChannelMember{ChannelID: "team", WakerID: "member"}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	child, err := service.Delegate(ctx, root, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&member).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Delegation(ctx, child.ID); !errors.Is(err, status_error.AgentOperationForbidden) {
		t.Fatalf("removed member can still launch: %v", err)
	}
}
