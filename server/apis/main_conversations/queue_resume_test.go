package main_conversations

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/apis/chats"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

const queueFixtureGate = "NEUX_MAIN_QUEUE_TEST_GATE"

func TestMainConversationQueueAcceptsBothSendEntriesWhileWorkerActive(t *testing.T) {
	root := t.TempDir()
	configureNativeMainConversationAt(t, root, "", true)
	gate := filepath.Join(root, "release-worker")
	t.Setenv(queueFixtureGate, gate)
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
	send := SendMainConversationMessage{SubjectType: "waker", SubjectID: partner,
		Body: MainConversationInput{SubmissionID: "first", Content: "gate the initial work", StartWork: true}}
	initial, err := send.Output(request)
	if err != nil {
		t.Fatal(err)
	}
	first := initial.(MainConversationAcceptance)
	if first.QueuedInput == nil || first.QueuedInput.WorkChatID != "" || first.QueuedInput.SourceMessageID != "" {
		t.Fatalf("first routed message did not yield its original source-less receipt: %+v", first)
	}
	conv, err := global.MainConversationService.Ensure(ctx, "waker", partner)
	if err != nil {
		t.Fatal(err)
	}
	var sourceID string
	startFile := filepath.Join(root, "worker-starts")
	workerStarts := func() int {
		data, _ := os.ReadFile(startFile)
		return strings.Count(string(data), "worker\n")
	}
	waitMainQueueFixture(t, ctx, func() bool {
		message, err := global.MainConversationService.Message(ctx, conv.ID, first.QueuedInput.ID)
		if err == nil && !message.AdmissionPending {
			sourceID = message.SourceChatID
		}
		return sourceID != "" && workerStarts() == 1
	})
	send.Body = MainConversationInput{SubmissionID: "from-main", Content: "queued from the main conversation", ReplyToMessageID: first.QueuedInput.ID}
	mainResult, err := send.Output(request)
	if err != nil {
		t.Fatal(err)
	}
	main := mainResult.(MainConversationAcceptance).QueuedInput
	sourceSend := SendMainConversationSourceMessage{SubjectType: "waker", SubjectID: partner, WorkChatID: sourceID,
		Body: chats.SendMessageRequest{SubmissionID: "from-source", MessageRequest: chats.MessageRequest{Text: "queued from source", RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}}}
	sourceResult, err := sourceSend.Output(request)
	if err != nil {
		t.Fatal(err)
	}
	source := sourceResult.(MainConversationSourceAcceptance).QueuedInput
	if main == nil || source == nil || main.ID == source.ID || main.WorkChatID != sourceID || source.WorkChatID != sourceID {
		t.Fatalf("expected two stable receipts for the original Chat, got %+v, %+v", main, source)
	}
	replayed, err := send.Output(request)
	if err != nil || replayed.(MainConversationAcceptance).QueuedInput.ID != main.ID {
		t.Fatalf("main retry did not return the first receipt: %+v, %v", replayed, err)
	}
	send.Body.Content += " different"
	if _, err := send.Output(request); !util.HasStatusCode(err, status_error.CreateRequestConflict) {
		t.Fatalf("conflicting retry = %v", err)
	}
	get := GetMainConversation{SubjectType: "waker", SubjectID: partner}
	snapshot := func() MainConversationResponse {
		t.Helper()
		result, err := get.Output(request)
		if err != nil {
			t.Fatal(err)
		}
		return result.(MainConversationResponse)
	}
	pending := snapshot().PendingInputs
	if len(pending) != 1 || pending[0].ID != main.ID || workerStarts() != 1 {
		t.Fatalf("main queue should only show the public input: %+v", pending)
	}
	sourcePage, err := (&GetMainConversationSource{SubjectType: "waker", SubjectID: partner, WorkChatID: sourceID}).Output(request)
	if err != nil {
		t.Fatal(err)
	}
	privatePending := sourcePage.(MainConversationSourceResponse).PendingInputs
	if len(privatePending) != 1 || privatePending[0].ID != source.ID || !privatePending[0].PrivateSource {
		t.Fatalf("source queue should show its own pending input: %+v", privatePending)
	}
	for _, submission := range []string{"from-main", "from-source"} {
		execution, err := global.ChatService.ReadSubmission(ctx, sourceID, submission)
		if err != nil || execution != nil {
			t.Fatalf("unexpected execution while original process owns input: %+v, %v", execution, err)
		}
	}
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	waitMainQueueFixture(t, ctx, func() bool {
		page := snapshot()
		return len(page.PendingInputs) == 0 && workerStarts() == 3
	})
	assertInputs := func() {
		t.Helper()
		page := snapshot()
		want := []*MainConversationQueuedInput{main, source}
		found := 0
		for _, item := range page.Messages {
			if item.ID == source.ID {
				t.Fatalf("source-only input leaked into main: %+v", item)
			}
			if item.ID != main.ID {
				continue
			}
			if item.Content != main.Content || item.SourceChatID != sourceID || item.SourceMessageID != main.SourceMessageID {
				t.Fatalf("unexpected public input: %+v", item)
			}
			found++
		}
		if found != 1 {
			t.Fatalf("public history only has %d admitted inputs", found)
		}
		history, err := global.ChatService.ReadMessageTimeline(ctx, sourceID, 0, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		var originalUsers, originalAnswers []string
		for _, item := range history.Resources {
			if item.Content == "gate the initial work" && item.Role == constants.MessageRoleUser {
				originalUsers = append(originalUsers, item.ID)
			}
			if item.ID == first.QueuedInput.ID && item.Role == constants.MessageRoleAssistant {
				originalAnswers = append(originalAnswers, item.ID)
			}
		}
		if len(originalUsers) != 1 || len(originalAnswers) != 1 || originalUsers[0] == originalAnswers[0] {
			t.Fatalf("initial routed Worker collided with its user input: users=%v answers=%v", originalUsers, originalAnswers)
		}
		found = 0
		for _, item := range history.Resources {
			if item.ID != main.SourceMessageID && item.ID != source.SourceMessageID {
				continue
			}
			if found >= len(want) || item.ID != want[found].SourceMessageID ||
				item.Role != constants.MessageRoleUser || item.Content != want[found].Content {
				t.Fatalf("unexpected source input: %+v", item)
			}
			found++
		}
		if found != len(want) || workerStarts() != 3 {
			t.Fatalf("source history has %d admitted inputs after %d Worker starts", found, workerStarts())
		}
	}
	assertInputs()
}

func TestMainConversationRichScopeClarificationDoesNotShareQueuedRouterSeat(t *testing.T) {
	root := t.TempDir()
	configureNativeMainConversationAt(t, root, "", true)
	t.Setenv(nativeRouterFixtureRoot, root)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	queue := newNativeMainQueueFixture(t, root)
	if err := queue.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.WriteFile(filepath.Join(root, "release-router"), nil, 0600) }()
	request, _ := gin.CreateTestContext(httptest.NewRecorder())
	request.Request = httptest.NewRequest("POST", "/", nil).WithContext(ctx)
	const partner = "native-main-partner"
	send := SendMainConversationMessage{SubjectType: "waker", SubjectID: partner,
		Body: MainConversationInput{SubmissionID: "plain", Content: "router fixture first project"}}
	if raw, err := send.Output(request); err != nil || raw.(MainConversationAcceptance).QueuedInput == nil {
		t.Fatalf("cannot hold a queued Router turn: %+v %v", raw, err)
	}
	waitMainQueueFixture(t, ctx, func() bool {
		_, err := os.Stat(filepath.Join(root, "router-entered"))
		return err == nil
	})
	richCtx, cancelRich := context.WithTimeout(ctx, 2*time.Second)
	defer cancelRich()
	request.Request = request.Request.WithContext(richCtx)
	send.Body = MainConversationInput{SubmissionID: "rich", Message: &chats.MessageRequest{
		Text: "router fixture second project", SkillIDs: []string{"skill-for-clarification"}, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto,
	}}
	if _, err := send.Output(request); !util.HasStatusCode(err, status_error.MainConversationProjectRequired) {
		t.Fatalf("rich input lost its normal synchronous scope-clarification result: %v", err)
	}
}

func TestMainConversationQueueContinuesAfterPrecommitRejection(t *testing.T) {
	root := t.TempDir()
	configureNativeMainConversationAt(t, root, "", true)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	const rejected = "rejected-before-commit"
	global.ExecutionService = rejectingQueueExecution{Core: global.ExecutionService.(execution.Core), submissionID: rejected}
	request, _ := gin.CreateTestContext(httptest.NewRecorder())
	request.Request = httptest.NewRequest("POST", "/", nil).WithContext(ctx)
	send := SendMainConversationMessage{SubjectType: "waker", SubjectID: "native-main-partner",
		Body: MainConversationInput{SubmissionID: "first", Content: "Wait for the initial work to settle"}}
	value, err := send.Output(request)
	if err != nil {
		t.Fatal(err)
	}
	first := value.(MainConversationAcceptance)
	work := first.Message.SourceChatID
	waitMainQueueFixture(t, ctx, func() bool { return !global.ExecutionService.IsExecutionActive(work, first.ExecutionID) })
	queue := newNativeMainQueueFixture(t, root)
	const next = "next-valid-input"
	for _, submission := range []string{rejected, next} {
		send.Body = MainConversationInput{SubmissionID: submission, Content: submission, ReplyToMessageID: first.Message.ID}
		value, err := send.Output(request)
		if err != nil {
			t.Fatal(err)
		}
		if queued := value.(MainConversationAcceptance).QueuedInput; queued == nil || queued.WorkChatID != work {
			t.Fatalf("unexpected receipt for %s: %+v", submission, queued)
		}
	}
	if err := queue.Open(ctx); err != nil {
		t.Fatal(err)
	}
	waitMainQueueFixture(t, ctx, func() bool {
		receipt, err := queue.Receipt(ctx, first.Message.ConversationID, rejected)
		if err != nil {
			t.Fatal(err)
		}
		switch receipt.Status {
		case constants.MainConversationInputUncertain:
			t.Fatalf("precommit rejection was recorded as an uncertain send: %+v", receipt)
		case constants.MainConversationInputFailed:
			if receipt.ErrorCode != status_error.WorkTicketCannotContinue.Key() {
				t.Fatalf("unexpected rejection: %+v", receipt)
			}
			return true
		}
		return false
	})
	waitMainQueueFixture(t, ctx, func() bool {
		receipt, err := queue.Receipt(ctx, first.Message.ConversationID, next)
		if err != nil {
			t.Fatal(err)
		}
		return receipt.Status == constants.MainConversationInputAccepted
	})
	if receipt, err := global.ChatService.ReadSubmission(ctx, work, rejected); err != nil || receipt != nil {
		t.Fatalf("rejected input reached the native journal: %+v, %v", receipt, err)
	}
}

type rejectingQueueExecution struct {
	// Core 运行真实的原生接纳路径。
	execution.Core
	// submissionID 只拒绝本次测试的原提交。
	submissionID string
}

func (e rejectingQueueExecution) Start(ctx context.Context, intent execution.WorkIntent) (execution.AcceptedExecution, error) {
	if intent.Input.SubmissionID == e.submissionID {
		outer := intent.AdmissionBoundary
		intent.AdmissionBoundary = func(ctx context.Context, _ func(context.Context) error) error {
			// 模拟原消息预留成功后，实际原生批次提交前工单授权失效。
			return outer.Commit(ctx, func(context.Context) error { return status_error.WorkTicketCannotContinue })
		}
	}
	return e.Core.Start(ctx, intent)
}

func newNativeMainQueueFixture(t *testing.T, root string) *mainconversation.InputQueue {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(root, "main-delivery.db")})
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	layout, err := chatstorage.NewLayout(global.Config.ManagedFilesDirectory)
	if err != nil {
		t.Fatal(err)
	}
	queue := global.NewMainConversationInputQueue(controllers.NewMainConversationInputController(database.DB), layout)
	previous := global.MainConversationInputQueue
	global.MainConversationInputQueue = queue
	t.Cleanup(func() {
		queue.Close()
		global.MainConversationInputQueue = previous
	})
	return queue
}

func waitMainQueueFixture(t *testing.T, ctx context.Context, check func() bool) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for !check() {
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for native queue fixture")
		case <-ticker.C:
		}
	}
}

func releaseMainQueueFixtureWorker(t *testing.T, body string) {
	gate := os.Getenv(queueFixtureGate)
	if gate == "" || !strings.Contains(body, "gate the initial work") {
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	waitMainQueueFixture(t, ctx, func() bool {
		_, err := os.Stat(gate)
		return err == nil || !errors.Is(err, os.ErrNotExist)
	})
}
