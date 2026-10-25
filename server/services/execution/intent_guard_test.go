package execution

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

func TestStartIntentGuardRunsBeforeCreationContinuationAndRetry(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir(), &fakeRuntime{})
	manager.execution.(*service).validateIntent = func(context.Context, WorkIntent) error { return status_error.KnowledgeForbidden }
	ctx := context.Background()
	for _, intent := range []WorkIntent{
		{ClientRequestID: "blocked", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}}, Input: WorkInput{Content: "创建"}},
		{Chat: ChatTarget{ChatID: "blocked-chat"}, Input: WorkInput{Content: "续发"}},
		{Chat: ChatTarget{ChatID: "blocked-chat"}, RetryExecutionID: "blocked-execution"},
	} {
		if _, err := manager.execution.Start(ctx, intent); !errors.Is(err, status_error.KnowledgeForbidden) {
			t.Fatalf("intent escaped domain admission guard: %v", err)
		}
	}
	listed, err := manager.chats.ListChats(ctx, chatservice.ListChatsInput{WakerID: "waker-1", Limit: 20})
	if err != nil || len(listed.Resources) != 0 {
		t.Fatalf("rejected creation persisted a chat: %+v %v", listed, err)
	}
}
