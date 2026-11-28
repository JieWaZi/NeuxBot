package main_conversations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/gin-gonic/gin"
)

const nativeRouterFixtureRoot = "NEUX_MAIN_ROUTER_TEST_ROOT"

func TestMainConversationAcceptsWhileNativeRouterIsBlockedAndContinuesSameSession(t *testing.T) {
	root := t.TempDir()
	configureNativeMainConversationAt(t, root, "", true)
	t.Setenv(nativeRouterFixtureRoot, root)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	queue := newNativeMainQueueFixture(t, root)
	if err := queue.Open(ctx); err != nil {
		t.Fatal(err)
	}
	release := filepath.Join(root, "release-router")
	var requests sync.WaitGroup
	defer func() {
		_ = os.WriteFile(release, nil, 0600)
		requests.Wait()
	}()

	const partner = "native-main-partner"
	firstRequest, cancelFirst := context.WithCancel(ctx)
	defer cancelFirst()
	first := sendNativeRouterFixture(&requests, firstRequest, partner, "router-first", "router fixture first")
	waitMainQueueFixture(t, ctx, func() bool {
		_, err := os.Stat(filepath.Join(root, "router-entered"))
		return err == nil
	})
	firstReceipt := requireNativeRouterReceipt(t, first)
	cancelFirst()
	secondReceipt := requireNativeRouterReceipt(t, sendNativeRouterFixture(&requests, ctx, partner, "router-second", "router fixture second"))
	retry := requireNativeRouterReceipt(t, sendNativeRouterFixture(&requests, ctx, partner, "router-first", "router fixture first"))
	if firstReceipt.ID == secondReceipt.ID || retry.ID != firstReceipt.ID {
		t.Fatalf("unexpected pending input identities: first=%+v second=%+v replay=%+v", firstReceipt, secondReceipt, retry)
	}
	snapshot := func() MainConversationResponse {
		httpCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
		httpCtx.Request = httptest.NewRequest("GET", "/", nil).WithContext(ctx)
		raw, err := (&GetMainConversation{SubjectType: "waker", SubjectID: partner}).Output(httpCtx)
		if err != nil {
			t.Fatal(err)
		}
		return raw.(MainConversationResponse)
	}
	pending := snapshot().PendingInputs
	if len(pending) != 2 || pending[0].ID != firstReceipt.ID || pending[1].ID != secondReceipt.ID {
		t.Fatalf("blocked Router did not leave two durable pending inputs: %+v", pending)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}

	var page MainConversationResponse
	waitMainQueueFixture(t, ctx, func() bool {
		page = snapshot()
		return len(page.Messages) >= 4 && len(page.PendingInputs) == 0
	})
	if len(page.Messages) != 4 || page.Messages[0].ID != firstReceipt.ID || page.Messages[2].ID != secondReceipt.ID ||
		page.Messages[1].Content != "First answer." || page.Messages[1].ReplyToMessageID != firstReceipt.ID ||
		page.Messages[3].Content != "Same native session." || page.Messages[3].ReplyToMessageID != secondReceipt.ID ||
		page.Messages[1].SourceChatID != "" || page.Messages[3].SourceChatID != "" {
		t.Fatalf("expected two ordered direct answers on one native session, got %+v", page.Messages)
	}
}

func TestMainConversationDirectRecordQuoteRetainsItsOlderQuestion(t *testing.T) {
	configureNativeMainConversation(t)
	ctx := t.Context()
	const partner = "native-main-partner"
	const question = "What should I name the milestone for the original migration?"
	service := global.MainConversationService
	conv, err := service.Ensure(ctx, "waker", partner)
	if err != nil {
		t.Fatal(err)
	}
	appendRow := func(row models.MainConversationMessage) models.MainConversationMessage {
		t.Helper()
		row.ConversationID = conv.ID
		var saved models.MainConversationMessage
		if err := global.TransactionControllerManager.Transaction(ctx, func(tx context.Context) error {
			var err error
			saved, _, err = global.MainConversationControllerManager.Append(tx, row, "waker", partner)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return saved
	}
	opener := appendRow(models.MainConversationMessage{ID: "original-question", Role: "user", Content: question})
	reply := appendRow(models.MainConversationMessage{ID: "original-answer", Role: "assistant", Content: "Call it Meridian.", ReplyToMessageID: opener.ID, SourceWakerID: partner})
	for index := 0; index < mainconversation.ProjectRoutingHistoryLimit; index++ {
		appendRow(models.MainConversationMessage{ID: fmt.Sprintf("later-%d", index), Role: "user", Content: "An unrelated conversation."})
	}
	history, previous, _, err := service.RouterContext(ctx, conv.ID, 0, reply)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.PlanInput(ctx, global.ProjectControllerManager, partner, func(_ context.Context, _, input string) (string, error) {
		for _, body := range []string{input, mainconversation.RouterTurnInput(input)} {
			var payload map[string]json.RawMessage
			if err := json.Unmarshal([]byte(body), &payload); err != nil {
				t.Fatal(err)
			}
			var quotedQuestion map[string]string
			if err := json.Unmarshal(payload["quoteInput"], &quotedQuestion); err != nil || quotedQuestion["content"] != question {
				t.Fatalf("explicit record quote lost its original question: %s (%v)", payload["quoteInput"], err)
			}
		}
		return "{\"action\":\"answer\",\"message\":\"Confirmed.\"}", nil
	}, execution.WorkInput{Content: "Remind me why."}, false, history, reply, previous)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMainConversationPublishesUnsupportedReminderReplyWithoutWork(t *testing.T) {
	root := t.TempDir()
	configureNativeMainConversationAt(t, root, "", true)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	const partner = "native-main-partner"
	const reminder = "明天提醒我 看下新闻"
	const unavailable = "主会话目前还不能设置定时提醒。"
	service := global.MainConversationService
	conv, err := service.Ensure(ctx, "waker", partner)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟旧版已将时间追问写入原主会话，再从正常发送入口接纳用户补充。
	if _, err := service.PublishRoute(ctx, conv, partner, "legacy-reminder", "", reminder, "legacy-digest", execution.WorkInput{},
		mainconversation.ProjectRoute{Message: "明天几点提醒你看新闻？", Clarification: true}); err != nil {
		t.Fatal(err)
	}
	queue := newNativeMainQueueFixture(t, root)
	if err := queue.Open(ctx); err != nil {
		t.Fatal(err)
	}
	for i, content := range []string{"9点", reminder} {
		var requests sync.WaitGroup
		receipt := requireNativeRouterReceipt(t, sendNativeRouterFixture(&requests, ctx, partner, fmt.Sprintf("reminder-%d", i), content))
		requests.Wait()
		var messages []models.MainConversationMessage
		waitMainQueueFixture(t, ctx, func() bool {
			var err error
			messages, _, err = service.Messages(ctx, conv.ID, 0, 10)
			if err != nil {
				t.Fatal(err)
			}
			return len(messages) >= 4+i*2
		})
		user, reply := messages[len(messages)-2], messages[len(messages)-1]
		if user.ID != receipt.ID || user.Content != content || reply.Content != unavailable ||
			reply.ReplyToMessageID != receipt.ID || reply.SourceChatID != "" || reply.SourceExecutionID != "" {
			t.Fatalf("reminder did not settle as a direct main conversation reply: %+v, %+v", user, reply)
		}
	}
}

type nativeRouterFixtureReceipt struct {
	// acceptance 是发送入口给出的持久回执。
	acceptance MainConversationAcceptance
	// err 是发送入口返回的失败。
	err error
}

func sendNativeRouterFixture(requests *sync.WaitGroup, ctx context.Context, partner, submission, content string) <-chan nativeRouterFixtureReceipt {
	receipt := make(chan nativeRouterFixtureReceipt, 1)
	requests.Add(1)
	go func() {
		defer requests.Done()
		httpCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
		httpCtx.Request = httptest.NewRequest("POST", "/", nil).WithContext(ctx)
		op := SendMainConversationMessage{SubjectType: "waker", SubjectID: partner, Body: MainConversationInput{SubmissionID: submission, Content: content}}
		raw, err := op.Output(httpCtx)
		acceptance, _ := raw.(MainConversationAcceptance)
		receipt <- nativeRouterFixtureReceipt{acceptance: acceptance, err: err}
	}()
	return receipt
}

func requireNativeRouterReceipt(t *testing.T, receipt <-chan nativeRouterFixtureReceipt) *MainConversationQueuedInput {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case result := <-receipt:
		pending := result.acceptance.QueuedInput
		if result.err != nil || pending == nil || pending.ID == "" || pending.WorkChatID != "" {
			t.Fatalf("expected a durable receipt before a source exists: %+v (%v)", result.acceptance, result.err)
		}
		return pending
	case <-timer.C:
		t.Fatal("main conversation send blocked on the native Router turn")
		return nil
	}
}

func holdNativeRouterFixture(t *testing.T) {
	t.Helper()
	root := os.Getenv(nativeRouterFixtureRoot)
	if root == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(root, "router-entered"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	waitMainQueueFixture(t, ctx, func() bool {
		_, err := os.Stat(filepath.Join(root, "release-router"))
		return err == nil
	})
}
