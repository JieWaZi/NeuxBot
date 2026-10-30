package execution

import (
	"context"
	runtimediagnostic "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/diagnostic"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestRecentRuntimeHistoryKeepsFiveCompletedFinalTurns(t *testing.T) {
	messages := make([]messageview.Message, 0, 16)
	for index := 0; index < 7; index++ {
		text := string(rune('a' + index))
		messages = append(messages,
			messageview.Message{Role: constants.MessageRoleUser, Content: "question-" + text},
			messageview.Message{
				Role: constants.MessageRoleAssistant, Status: constants.ExecutionStatusCompleted,
				Content: "process-" + text + "answer-" + text,
				DisplayParts: []messageview.MessageDisplayPart{
					{Type: constants.MessageDisplayPartProcessCommentary, Text: "process-" + text},
					{Type: constants.MessageDisplayPartText, Text: "answer-" + text},
				},
			},
		)
	}
	messages = append(messages, messageview.Message{Role: constants.MessageRoleUser, Content: "pending"})
	history := recentRuntimeHistory(messages, 0, false)
	if len(history) != 10 || history[0].Content != "question-c" || history[9].Content != "answer-g" {
		t.Fatalf("history = %+v", history)
	}
	for _, item := range history {
		if strings.Contains(item.Content, "process") || strings.Contains(item.Content, "pending") {
			t.Fatalf("history contains process or pending input: %+v", history)
		}
	}
}

func TestRecentRuntimeHistoryBoundsLargeMessages(t *testing.T) {
	messages := []messageview.Message{
		{Role: constants.MessageRoleUser, Content: strings.Repeat("你", 30000)},
		{Role: constants.MessageRoleAssistant, Status: constants.ExecutionStatusCompleted,
			DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartText, Text: strings.Repeat("答", 30000)}}},
	}
	history := recentRuntimeHistory(messages, 5, false)
	if len(history) != 2 || len(history[0].Content)+len(history[1].Content) > constants.RuntimeHistoryMaximumBytes {
		t.Fatalf("history exceeds bound: %+v", history)
	}
}

func TestRecentTeamHistoryKeepsFinalReplyOnly(t *testing.T) {
	messages := []messageview.Message{
		{Role: constants.MessageRoleUser, Actor: string(constants.ChannelMessageActorMemberReport), Content: "member result"},
		{Role: constants.MessageRoleAssistant, Status: constants.ExecutionStatusCompleted,
			Content: "thinking final", DisplayParts: []messageview.MessageDisplayPart{
				{Type: constants.MessageDisplayPartProcessCommentary, Text: "thinking"},
				{Type: constants.MessageDisplayPartText, Text: "final"},
			}},
	}
	history := recentRuntimeHistory(messages, 5, true)
	if len(history) != 2 || history[1].Content != "final" {
		t.Fatalf("team history = %+v", history)
	}
}

func TestRecentTeamHistorySkipsUnfinishedTurns(t *testing.T) {
	messages := []messageview.Message{
		{Role: constants.MessageRoleUser, Content: "completed input"},
		{Role: constants.MessageRoleAssistant, Status: constants.ExecutionStatusCompleted,
			DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartText, Text: "completed answer"}}},
		{Role: constants.MessageRoleUser, Content: "failed input"},
		{Role: constants.MessageRoleAssistant, Status: constants.ExecutionStatusFailed,
			DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartText, Text: "failed answer"}}},
		{Role: constants.MessageRoleUser, Content: "pending input"},
	}
	history := recentRuntimeHistory(messages, 5, true)
	if len(history) != 2 || history[0].Content != "completed input" || history[1].Content != "completed answer" {
		t.Fatalf("team history includes unfinished turns: %+v", history)
	}
}

func TestBuildRunRequestStartsNewSessionWithinSameChat(t *testing.T) {
	messages := []messageview.Message{
		{Role: constants.MessageRoleUser, Content: "old question"},
		{Role: constants.MessageRoleAssistant, Status: constants.ExecutionStatusCompleted,
			ExecutionID: "old-execution", DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartText, Text: "old answer"}}},
		{Role: constants.MessageRoleUser, Content: "recent question"},
		{Role: constants.MessageRoleAssistant, Status: constants.ExecutionStatusCompleted,
			ExecutionID: "recent-execution", DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartText, Text: "recent answer"}}},
	}
	state := messageChat{
		chat:             models.Chat{ID: "same-chat", LastAccountID: "old-account"},
		previousMessages: messages,
		runtimeSessions: map[string]models.RuntimeSession{
			"codex": {SessionID: "old-session", LastExecutionID: "recent-execution"},
		},
	}
	request := (&service{}).buildRunRequest(state, pendingMessage{
		message: userMessage{Text: "next question"},
		runtime: runtimeSelection{RuntimeID: "codex", AccountID: "new-account", HistoryTurns: 1},
	}, &preparedMessageFiles{}, nil, nil, models.RuntimeEnvironment{})
	if request.Session.Mode != runtimecontract.SessionModeNew || len(request.History) != 2 ||
		request.History[0].Content != "recent question" || request.History[1].Content != "recent answer" {
		t.Fatalf("new session request = %+v", request)
	}
}

// TestColdNewSessionFindsSuccessfulTurnPastFailedTail 验证配置轮数计算完整成功轮，而非末尾原始消息数量。
func TestColdNewSessionFindsSuccessfulTurnPastFailedTail(t *testing.T) {
	runtime := &fakeRuntime{results: []runtimecontract.Result{{}, {Failure: &runtimecontract.Failure{Code: runtimediagnostic.FailureCodeProcessExited, Stage: "runtime", Message: "fixture failed", Transience: runtimediagnostic.FailureTransiencePermanent}}}}
	manager := newMessageTestService(t, t.TempDir(), runtime)
	ctx := context.Background()
	core := manager.execution.(*service)
	first, err := core.Start(ctx, WorkIntent{ClientRequestID: "complete-history", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}}, Input: WorkInput{Actor: "user", Content: "successful context"}, Options: WorkOptions{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	if _, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", Content: "failed later input"}, Options: WorkOptions{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}}); err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	observed := &readBudgetJournal{Journal: core.journal}
	core.journal = observed
	if _, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", Content: "new session"}, Options: WorkOptions{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyNew, HistoryTurns: 1}}); err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	if observed.historyBodies.Load() != 1 || observed.fullReads.Load() != 0 {
		t.Fatalf("history body reads=%d full=%d", observed.historyBodies.Load(), observed.fullReads.Load())
	}
	history := runtime.requests[2].History
	if len(history) != 2 || history[0].Content != "successful context" || history[1].Content != "执行完成" {
		t.Fatalf("lost last complete successful turn: %+v", history)
	}
}

// TestColdHistorySkipsNonUserAndOmittedRounds 验证被省略及非用户成功输入不挤占最近完整轮。
func TestColdHistorySkipsNonUserAndOmittedRounds(t *testing.T) {
	runtime := &fakeRuntime{}
	manager := newMessageTestService(t, t.TempDir(), runtime)
	core := manager.execution.(*service)
	ctx := context.Background()
	first, err := core.Start(ctx, WorkIntent{ClientRequestID: "history-filter-first", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}}, Input: WorkInput{Actor: "user", Content: "keep completed context"}, Options: WorkOptions{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	omitted, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", Content: "already included"}, Options: WorkOptions{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	if _, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "workflow", Content: "nonuser input"}, Options: WorkOptions{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}}); err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	observed := &readBudgetJournal{Journal: core.journal}
	core.journal = observed
	if _, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", Content: "next"}, Options: WorkOptions{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyNew, HistoryTurns: 1, HistoryMessageIDsToOmit: []string{omitted.Execution.UserMessageID, omitted.Execution.ID}}}); err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	history := runtime.requests[3].History
	if len(history) != 2 || history[0].Content != "keep completed context" || observed.historyBodies.Load() != 2 || observed.fullReads.Load() != 0 {
		t.Fatalf("history=%+v bodies=%d full=%d", history, observed.historyBodies.Load(), observed.fullReads.Load())
	}
}

// TestColdHistoryPairsInheritedMessagesAcrossCandidatePages 验证一轮继承输入和最终答复跨候选页时仍完整交接。
func TestColdHistoryPairsInheritedMessagesAcrossCandidatePages(t *testing.T) {
	runtime := &fakeRuntime{}
	manager := newMessageTestService(t, t.TempDir(), runtime)
	core := manager.execution.(*service)
	ctx := context.Background()
	first, err := core.Start(ctx, WorkIntent{ClientRequestID: "inherited-history", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}}, Input: WorkInput{Actor: "user", Content: "older input"}, Options: WorkOptions{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	if _, err := core.journal.AppendExecutionFacts(ctx, first.Chat.ID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeMessageInherited, MessageID: "inherited-user", Data: marshalData(messageview.InheritedMessage{Message: messageview.Message{ID: "inherited-user", Role: constants.MessageRoleUser, Actor: "user", Content: "inherited input"}})},
		{Type: constants.ChatEventTypeMessageInherited, MessageID: "inherited-assistant", Data: marshalData(messageview.InheritedMessage{Message: messageview.Message{ID: "inherited-assistant", Role: constants.MessageRoleAssistant, Content: "inherited final", Status: constants.ExecutionStatusCompleted, DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartText, Text: "inherited final"}}}})},
	}); err != nil {
		t.Fatal(err)
	}
	messages, err := core.readRecentHistory(ctx, messageChat{chat: first.Chat}, pendingMessage{runtime: runtimeSelection{HistoryTurns: 1}})
	history := recentRuntimeHistory(messages, 1, false)
	if err != nil || len(history) != 2 || history[0].Content != "inherited input" || history[1].Content != "inherited final" {
		t.Fatalf("inherited history=%+v %v", history, err)
	}
}
