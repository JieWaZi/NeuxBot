package execution

import (
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
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
	if request.Session.Mode != runtimes.SessionModeNew || len(request.History) != 2 ||
		request.History[0].Content != "recent question" || request.History[1].Content != "recent answer" {
		t.Fatalf("new session request = %+v", request)
	}
}
