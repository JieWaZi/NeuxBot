package execution

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

type finalReplyPublisher struct {
	MainConversationMessages
	// chatID 是获授权的来源对话。
	chatID string
	// executionID 是该发言授权对应的执行。
	executionID string
	// writes 记录发往主会话的正文。
	writes []string
	// reject 模拟公开发言被拒绝。
	reject bool
	// notify 登记公开提交后唤醒订阅者的事件。
	notify func()
}

func (p *finalReplyPublisher) PublishWithReceipt(ctx context.Context, chatID, executionID, key, content, replyID string) (models.MainConversationMessage, bool, error) {
	if p.reject {
		return models.MainConversationMessage{}, false, status_error.AgentOperationForbidden
	}
	if replyID != "input" || chatID != p.chatID || executionID != p.executionID {
		return models.MainConversationMessage{}, false, errors.New("wrong destination")
	}
	p.writes = append(p.writes, content)
	if p.notify != nil {
		events.AfterCommit(ctx, p.notify)
	}
	return models.MainConversationMessage{ID: "post"}, false, nil
}

func TestMainConversationFinalUsesOnlyUnpublishedAnswer(t *testing.T) {
	for _, attempt := range []string{"none", "posted", "denied", "invalid"} {
		t.Run(attempt, func(t *testing.T) {
			publisher := &finalReplyPublisher{chatID: "source", executionID: "work"}
			grant := MainConversationTools("source", "work", "input", publisher)[0]
			for _, op := range grant.Bindings {
				if op.Operation != constants.ConversationMessagePostOperation || attempt == "none" {
					continue
				}
				op.Attempt()
				if attempt != "invalid" {
					publisher.reject = attempt == "denied"
					_, _ = op.Call(t.Context(), capability.Request{RequestID: "send", Params: map[string]any{"content": "published"}})
					publisher.reject = false
				}
			}
			if err := grant.publishFinal(t.Context(), func(context.Context) (string, error) {
				if attempt != "none" {
					t.Fatal("read an answer after an explicit send attempt")
				}
				return "final", nil
			}); err != nil {
				t.Fatal(err)
			}
			if attempt == "none" && (len(publisher.writes) != 1 || publisher.writes[0] != "final") ||
				attempt == "posted" && (len(publisher.writes) != 1 || publisher.writes[0] != "published") ||
				(attempt == "invalid" || attempt == "denied") && len(publisher.writes) != 0 {
				t.Fatalf("unexpected deliveries: %v", publisher.writes)
			}
		})
	}
}

func TestMainConversationDeliversNativeFinalAfterSuccessfulAdmission(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "botcli")
	if err := os.WriteFile(binary, []byte("fixture CLI"), 0700); err != nil {
		t.Fatal(err)
	}
	registry := capability.New(nil)
	t.Cleanup(registry.Close)
	manager := newMessageTestService(t, t.TempDir(), messageTestOptions{agent: registry, agentBinary: binary})
	chatID, executionID := util.New(), util.New()
	activeAtNotification := make(chan bool, 1)
	publisher := &finalReplyPublisher{
		chatID: chatID, executionID: executionID,
		notify: func() { activeAtNotification <- manager.execution.IsExecutionActive(chatID, executionID) },
	}
	_, err := manager.execution.Start(t.Context(), WorkIntent{
		Chat:        ChatTarget{ChatID: chatID, MainConversationSource: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}},
		Input:       WorkInput{Actor: "user", Content: "inspect-temp-files", SubmissionID: "input"},
		Options:     WorkOptions{RuntimeID: "codex", EntryPoint: MainConversationEntryPoint, PermissionLevel: constants.PermissionLevelAuto, ToolCapabilities: MainConversationTools(chatID, executionID, "input", publisher)},
		ExecutionID: executionID, Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, chatID)
	select {
	case active := <-activeAtNotification:
		if active {
			t.Fatal("final answer notification preceded release of the execution")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("final answer notification was never delivered")
	}
	if len(publisher.writes) != 1 || publisher.writes[0] != "fixture reply" {
		t.Fatalf("delivered native final %q", publisher.writes)
	}
	page, err := manager.chats.ReadMessageTimeline(t.Context(), chatID, 0, 0, 10)
	if err != nil || len(page.Resources) != 2 {
		t.Fatalf("read source transcript: messages=%d err=%v", len(page.Resources), err)
	}
	for _, part := range page.Resources[1].DisplayParts {
		if part.Type == constants.MessageDisplayPartTool {
			return
		}
	}
	t.Fatal("source transcript lost the command inspection")
}

func TestMainConversationReportsUndeliveredNativeFinal(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "botcli")
	if err := os.WriteFile(binary, []byte("fixture CLI"), 0700); err != nil {
		t.Fatal(err)
	}
	registry := capability.New(nil)
	t.Cleanup(registry.Close)
	manager := newMessageTestService(t, t.TempDir(), messageTestOptions{agent: registry, agentBinary: binary})
	chatID, executionID := util.New(), util.New()
	publisher := &finalReplyPublisher{chatID: chatID, executionID: executionID, reject: true}
	_, err := manager.execution.Start(t.Context(), WorkIntent{
		Chat:        ChatTarget{ChatID: chatID, MainConversationSource: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}},
		Input:       WorkInput{Actor: "user", Content: "inspect-temp-files", SubmissionID: "input"},
		Options:     WorkOptions{RuntimeID: "codex", EntryPoint: MainConversationEntryPoint, PermissionLevel: constants.PermissionLevelAuto, ToolCapabilities: MainConversationTools(chatID, executionID, "input", publisher)},
		ExecutionID: executionID, Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, chatID)
	outcome, err := manager.chats.ReadExecutionOutcome(t.Context(), chatID, executionID)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Execution.Status != constants.ExecutionStatusFailed || outcome.Execution.ErrorCode != status_error.RuntimeExecutionFailed.Key() ||
		outcome.FailureCode != constants.RuntimeFailureCodeEventDelivery || outcome.FailureMessage != "" ||
		outcome.ErrorMessageSource != constants.ErrorMessageSourceLocalized {
		t.Fatalf("undelivered answer was not recorded for the user: %+v", outcome)
	}
	if outcome.InvocationID == "" || outcome.TerminationReason != constants.RuntimeTerminationReasonNormal {
		t.Fatalf("original native outcome was lost: %+v", outcome)
	}
	page, err := manager.chats.ReadMessageTimeline(t.Context(), chatID, 0, 0, 10)
	if err != nil || len(page.Resources) != 2 || !strings.Contains(page.Resources[1].Content, "fixture reply") {
		t.Fatalf("undelivered answer missing from its source: %+v, %v", page.Resources, err)
	}
}
