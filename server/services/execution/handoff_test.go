package execution

import (
	"context"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestSummarizeCurrentSessionUsesHiddenResume(t *testing.T) {
	runtime := &fakeRuntime{}
	manager := &service{
		runtime:             runtime,
		runtimeEnvironments: readyRuntimeEnvironmentReader{},
	}
	state := messageChat{
		chat: models.Chat{LastRuntimeID: "codex", LastModel: "gpt-5"},
		previousMessages: []messageview.Message{{
			Role: constants.MessageRoleAssistant, ExecutionID: "previous-execution",
		}},
		runtimeSessions: map[string]models.RuntimeSession{
			"codex": {SessionID: "previous-session", LastExecutionID: "previous-execution"},
		},
	}
	summary, ok := manager.summarizeCurrentSession(
		context.Background(),
		state,
		pendingMessage{runtime: runtimeSelection{SummaryModel: "gpt-5"}},
		&preparedMessageFiles{executionWorkspaceDirectory: t.TempDir()},
	)
	if !ok || summary != "执行完成" || len(runtime.requests) != 1 {
		t.Fatalf("handoff = %q, success = %v, requests = %d", summary, ok, len(runtime.requests))
	}
	request := runtime.requests[0]
	if request.Session.Mode != runtimes.SessionModeResume || request.Session.ID != "previous-session" ||
		request.Policy.Model != "gpt-5" || !request.Policy.Execution.WorkspaceReadOnly ||
		!strings.Contains(request.Message.Text, "Summarize the current conversation") {
		t.Fatalf("hidden handoff request = %+v", request)
	}
}

func TestSummarizeCurrentSessionFallsBackWhenResumeUnavailable(t *testing.T) {
	runtime := &fakeRuntime{}
	manager := &service{runtime: runtime, runtimeEnvironments: readyRuntimeEnvironmentReader{}}
	state := messageChat{
		chat: models.Chat{LastRuntimeID: "codex"},
		previousMessages: []messageview.Message{{
			Role: constants.MessageRoleAssistant, ExecutionID: "newer-execution",
		}},
		runtimeSessions: map[string]models.RuntimeSession{
			"codex": {SessionID: "old-session", LastExecutionID: "old-execution"},
		},
	}
	_, ok := manager.summarizeCurrentSession(
		context.Background(),
		state,
		pendingMessage{},
		&preparedMessageFiles{executionWorkspaceDirectory: t.TempDir()},
	)
	if ok || len(runtime.requests) != 0 {
		t.Fatalf("unavailable resume invoked runtime: %+v", runtime.requests)
	}
}

func TestHandoffStartsNewSessionWithoutShowingSummary(t *testing.T) {
	runtime := &fakeRuntime{results: []runtimes.Result{
		{Session: runtimes.SessionResult{OutputID: "old-session", State: runtimes.SessionStateCreated}},
		{},
		{Session: runtimes.SessionResult{OutputID: "new-session", State: runtimes.SessionStateCreated}},
	}}
	manager := newMessageTestService(t, t.TempDir(), runtime)
	first, err := manager.execution.Start(context.Background(), WorkIntent{
		ClientRequestID: util.New(),
		Chat:            ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}},
		Input:           WorkInput{Actor: "user", Content: "原始问题"},
		Options:         WorkOptions{RuntimeID: "codex", PermissionLevel: runtimes.PermissionLevelAuto},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	second, err := manager.execution.Start(context.Background(), WorkIntent{
		Chat:  ChatTarget{ChatID: first.Chat.ID},
		Input: WorkInput{Actor: "user", Content: "继续工作"},
		Options: WorkOptions{
			RuntimeID: "codex", PermissionLevel: runtimes.PermissionLevelAuto,
			SessionPolicy: constants.RuntimeSessionPolicyNew,
			HistoryMode:   constants.HistoryModeSummary,
			SummaryModel:  "gpt-5",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, second.Chat.ID)
	if len(runtime.requests) != 3 ||
		runtime.requests[1].Session.Mode != runtimes.SessionModeResume ||
		runtime.requests[1].Session.ID != "old-session" ||
		runtime.requests[1].Policy.Model != "gpt-5" ||
		runtime.requests[2].Session.Mode != runtimes.SessionModeNew ||
		len(runtime.requests[2].History) != 0 ||
		!strings.Contains(runtime.requests[2].Message.Text, "执行完成") {
		t.Fatalf("handoff requests = %+v", runtime.requests)
	}
	history, err := manager.chats.ReadMessageHistory(context.Background(), first.Chat.ID, 0, 10)
	if err != nil || len(history.Resources) != 4 {
		t.Fatalf("visible history = %+v, error = %v", history, err)
	}
	for _, message := range history.Resources {
		if strings.Contains(message.Content, "Task handoff") || strings.Contains(message.Content, handoffPrompt) {
			t.Fatalf("hidden handoff leaked into chat: %+v", history.Resources)
		}
	}
}

func TestHandoffResumeFailureUsesRecentTurns(t *testing.T) {
	runtime := &fakeRuntime{results: []runtimes.Result{
		{Session: runtimes.SessionResult{OutputID: "old-session", State: runtimes.SessionStateCreated}},
		{ExitCode: -1, Failure: &runtimes.Failure{
			Code: runtimes.FailureCodeSessionNotFound, Stage: "resume", Message: "session not found",
		}},
		{Session: runtimes.SessionResult{OutputID: "new-session", State: runtimes.SessionStateCreated}},
	}}
	manager := newMessageTestService(t, t.TempDir(), runtime)
	first, err := manager.execution.Start(context.Background(), WorkIntent{
		ClientRequestID: util.New(),
		Chat:            ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}},
		Input:           WorkInput{Actor: "user", Content: "原始问题"},
		Options:         WorkOptions{RuntimeID: "codex", PermissionLevel: runtimes.PermissionLevelAuto},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	second, err := manager.execution.Start(context.Background(), WorkIntent{
		Chat:  ChatTarget{ChatID: first.Chat.ID},
		Input: WorkInput{Actor: "user", Content: "继续工作"},
		Options: WorkOptions{
			RuntimeID: "codex", PermissionLevel: runtimes.PermissionLevelAuto,
			SessionPolicy: constants.RuntimeSessionPolicyNew,
			HistoryMode:   constants.HistoryModeSummary,
			HistoryTurns:  1,
			SummaryModel:  "gpt-5",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, second.Chat.ID)
	if len(runtime.requests) != 3 ||
		runtime.requests[1].Session.Mode != runtimes.SessionModeResume ||
		runtime.requests[2].Session.Mode != runtimes.SessionModeNew ||
		len(runtime.requests[2].History) != 2 ||
		runtime.requests[2].History[0].Content != "原始问题" ||
		runtime.requests[2].History[1].Content != "执行完成" ||
		runtime.requests[2].Message.Text != "继续工作" {
		t.Fatalf("fallback requests = %+v", runtime.requests)
	}
}
