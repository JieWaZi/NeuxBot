package main_conversations

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/apis/chats"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/gin-gonic/gin"
)

func TestMainConversationRouterAnswersAndDispatchesWithoutKeywordGate(t *testing.T) {
	root := t.TempDir()
	configureNativeMainConversationAt(t, root, "", true)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	queue := newNativeMainQueueFixture(t, root)
	if err := queue.Open(ctx); err != nil {
		t.Fatal(err)
	}
	const partner = "native-main-partner"
	request, _ := gin.CreateTestContext(httptest.NewRecorder())
	request.Request = httptest.NewRequest("POST", "/", nil).WithContext(ctx)
	send := func(body MainConversationInput) (string, MainConversationTimelineMessage) {
		t.Helper()
		res, err := (&SendMainConversationMessage{SubjectType: "waker", SubjectID: partner, Body: body}).Output(request)
		if err != nil {
			t.Fatal(err)
		}
		accepted := res.(MainConversationAcceptance)
		id := accepted.Message.ID
		if accepted.QueuedInput != nil {
			id = accepted.QueuedInput.ID
		}
		var reply MainConversationTimelineMessage
		waitMainQueueFixture(t, ctx, func() bool {
			raw, err := (&GetMainConversation{SubjectType: "waker", SubjectID: partner}).Output(request)
			if err != nil {
				t.Fatal(err)
			}
			for _, message := range raw.(MainConversationResponse).Messages {
				if message.Role == "assistant" && (message.ReplyToMessageID == id || message.SourceExecutionID == id) {
					reply = message
					return true
				}
			}
			return false
		})
		return id, reply
	}
	_, first := send(MainConversationInput{SubmissionID: "front-first", Content: "fixture 日本語の挨拶"})
	_, second := send(MainConversationInput{SubmissionID: "front-second", Content: "fixture 別件は作らないで"})
	if first.Content != "こんにちは。" || second.Content != "引き続き同じ会話です。" ||
		first.SourceChatID != "" || second.SourceChatID != "" {
		t.Fatalf("direct answers must share the Router session, not a work Chat: %+v %+v", first, second)
	}
	_, negated := send(MainConversationInput{SubmissionID: "front-negated", Content: "fixture 别另开一个任务"})
	if negated.Content != "不新开任务。" || negated.SourceChatID != "" {
		t.Fatalf("negation resembling an old dispatch keyword opened work: %+v", negated)
	}
	for _, sample := range []MainConversationInput{
		{SubmissionID: "english", Content: "Collect fixture evidence."},
		{SubmissionID: "chinese", Content: "请收集样例证据。"},
		{SubmissionID: "japanese", Content: "サンプルの資料を集めてください。"},
	} {
		id, report := send(sample)
		if report.SourceChatID == "" {
			t.Fatalf("%s did not have a work source: %+v", sample.SubmissionID, report)
		}
		if sameID, replay := send(sample); sameID != id || replay.ID != report.ID {
			t.Fatalf("%s was not resumed from the same submission: %+v", sample.SubmissionID, replay)
		}
		work, err := global.MainConversationService.Source(ctx, "waker", partner, report.SourceChatID)
		if err != nil || mainconversation.RoutedWorkTicketID(work.ScopeKey) != id {
			t.Fatalf("work source not isolated to submission %s: %+v, %v", id, work, err)
		}
		_, continuation := send(MainConversationInput{
			SubmissionID: sample.SubmissionID + "-follow", Content: "More fixture evidence.", ReplyToMessageID: report.ID,
		})
		if continuation.SourceChatID != report.SourceChatID {
			t.Fatalf("quoted work was not continued in the same source: %+v", continuation)
		}
	}
}

func TestLegacyWorkSourceFollowUpStaysInOrdinaryHistory(t *testing.T) {
	root := t.TempDir()
	configureNativeMainConversationAt(t, root, "", true)
	gate := filepath.Join(root, "release-private-source")
	t.Setenv(queueFixtureGate, gate)
	replyProbe := filepath.Join(root, "source-reply-result")
	t.Setenv("NEUX_MAIN_SOURCE_REPLY_RESULT", replyProbe)
	t.Cleanup(func() { _ = os.WriteFile(gate, nil, 0600) })
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	queue := newNativeMainQueueFixture(t, root)
	if err := queue.Open(ctx); err != nil {
		t.Fatal(err)
	}
	const partner = "native-main-partner"
	request, _ := gin.CreateTestContext(httptest.NewRecorder())
	request.Request = httptest.NewRequest("POST", "/", nil).WithContext(ctx)
	// Seed an unticketed legacy source through the old direct admission boundary.
	legacy := SendMainConversationMessage{SubjectType: "waker", SubjectID: partner, Body: MainConversationInput{SubmissionID: "initial", Content: "Hello there."}}
	initial, err := legacy.send(ctx, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	op := GetMainConversation{SubjectType: "waker", SubjectID: partner}
	snapshot := func() MainConversationResponse {
		t.Helper()
		out, err := op.Output(request)
		if err != nil {
			t.Fatal(err)
		}
		return out.(MainConversationResponse)
	}
	var source, sourceReply string
	waitMainQueueFixture(t, ctx, func() bool {
		for _, row := range snapshot().Messages {
			if row.Role == "assistant" && row.ReplyToMessageID == initial.Message.ID {
				source, sourceReply = row.SourceChatID, row.ID
			}
		}
		return source != ""
	})
	// Hold the same Chat in a turn so the source-only input remains durably queued.
	_, err = (&SendMainConversationMessage{SubjectType: "waker", SubjectID: partner,
		Body: MainConversationInput{SubmissionID: "gate", Content: "gate the initial work", ReplyToMessageID: sourceReply}}).Output(request)
	if err != nil {
		t.Fatal(err)
	}
	waitMainQueueFixture(t, ctx, func() bool {
		row, err := global.ChatService.ReadSubmission(ctx, source, "gate")
		return err == nil && row != nil && global.ExecutionService.IsExecutionActive(source, row.ID)
	})
	follow := SendMainConversationSourceMessage{SubjectType: "waker", SubjectID: partner, WorkChatID: source,
		Body: chats.SendMessageRequest{SubmissionID: "private", MessageRequest: chats.MessageRequest{Text: "A question inside the opened Chat.", RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}}}
	accepted, err := follow.Output(request)
	if err != nil {
		t.Fatal(err)
	}
	pending := accepted.(MainConversationSourceAcceptance).QueuedInput
	if pending == nil || !pending.PrivateSource || pending.FromMain {
		t.Fatalf("private input receipt = %+v", accepted)
	}
	for _, row := range snapshot().PendingInputs {
		if row.ID == pending.ID {
			t.Fatalf("private input leaked into the main queue: %+v", row)
		}
	}
	history, err := (&GetMainConversationSource{SubjectType: "waker", SubjectID: partner, WorkChatID: source}).Output(request)
	if err != nil {
		t.Fatal(err)
	}
	sourcePending := history.(MainConversationSourceResponse).PendingInputs
	if len(sourcePending) != 1 || sourcePending[0].ID != pending.ID {
		t.Fatalf("private source pending inputs = %+v", sourcePending)
	}
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	waitMainQueueFixture(t, ctx, func() bool {
		row, err := global.ChatService.ReadSubmission(ctx, source, follow.Body.SubmissionID)
		return err == nil && row != nil && row.Status == constants.ExecutionStatusCompleted
	})
	history, err = (&GetMainConversationSource{SubjectType: "waker", SubjectID: partner, WorkChatID: source}).Output(request)
	if err != nil {
		t.Fatal(err)
	}
	full := history.(MainConversationSourceResponse)
	var foundFollow bool
	for _, message := range full.Resources {
		if message.ID == pending.SourceMessageID && message.Role == constants.MessageRoleUser {
			foundFollow = true
			if message.MainConversationTriggerID != "" {
				t.Fatalf("source-only continuation became a main input: %+v", message)
			}
		}
	}
	if !foundFollow {
		t.Fatal("source-only continuation is missing from its original work history")
	}
	if rows := snapshot().Messages; len(rows) != 4 {
		t.Fatalf("source-only turn entered the main timeline: %+v", rows)
	}

	follow.Body.SubmissionID, follow.Body.Text = "source-reply", "fixture source conversation reply"
	accepted, err = follow.Output(request)
	if err != nil {
		t.Fatal(err)
	}
	queued := accepted.(MainConversationSourceAcceptance).QueuedInput
	if queued == nil || !queued.PrivateSource {
		t.Fatalf("source reply input must still be private: %+v", accepted)
	}
	var reply *models.Execution
	waitMainQueueFixture(t, ctx, func() bool {
		row, err := global.ChatService.ReadSubmission(ctx, source, follow.Body.SubmissionID)
		if err == nil && row != nil && row.Status != constants.ExecutionStatusRunning && row.Status != constants.ExecutionStatusPending {
			reply = row
		}
		return reply != nil
	})
	if reply.Status != constants.ExecutionStatusCompleted {
		probe, _ := os.ReadFile(replyProbe)
		t.Fatalf("source reply could not use the main conversation's publishing tool: %s (%s)", probe, reply.Status)
	}
	rows := snapshot().Messages
	if len(rows) != 5 || rows[4].Content != "Talk soon from the fixture." ||
		rows[4].Role != "assistant" || rows[4].SourceChatID != source ||
		rows[4].SourceExecutionID != reply.ID || rows[4].ReplyToMessageID != "" {
		t.Fatalf("expected only the selected reply to reach main: %+v", rows)
	}
}
