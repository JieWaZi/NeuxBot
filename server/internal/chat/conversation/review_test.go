package conversation

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
)

func TestReviewRecoveryUsesOnlyTaggedExecutionStarts(t *testing.T) {
	root := t.TempDir()
	store := openTestStore(t, root)
	start := func(id string, tagged bool) agent.ConversationChange {
		value := `{"entryPoint":"workbench","userMessageId":"u"}`
		if tagged {
			value = `{"entryPoint":"workbench","userMessageId":"u","review":{"configuration":"frozen","memory":1}}`
		}
		return agent.ConversationChange{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: id, Data: json.RawMessage(value)}}
	}
	finish := func(id string, terminal constants.ChatEventType) agent.ConversationChange {
		return agent.ConversationChange{Fact: &agent.ChatFact{Type: terminal, ExecutionID: id}}
	}
	writeBatch(t, store, "mixed-history", 0, fact(constants.ChatEventTypeCreated, "", ""),
		start("old", false), finish("old", constants.ChatEventTypeExecutionCompleted),
		start("failure", true), finish("failure", constants.ChatEventTypeExecutionFailed),
		start("eligible", true), finish("eligible", constants.ChatEventTypeExecutionCompleted))
	store.Close()
	store = openTestStore(t, root)
	first, next, err := store.ReadReviewStarts(t.Context(), chatID, 0, 0, 1)
	if err != nil || len(first) != 0 || next != 3 {
		t.Fatalf("old untagged terminal page = %+v, next=%d, err=%v", first, next, err)
	}
	page, _, err := store.ReadReviewStarts(t.Context(), chatID, next, 0, 2)
	if err != nil || len(page) != 1 || page[0].Admission.Change.Fact.ExecutionID != "eligible" || page[0].Terminal != 7 {
		t.Fatalf("review terminals = %+v, err=%v", page, err)
	}
	ids, err := store.ListReviewChatIDs(t.Context())
	if err != nil || len(ids) != 1 || ids[0] != chatID {
		t.Fatalf("review streams = %v, err=%v, index=%s", ids, err, filepath.Join(root, indexFilename))
	}
}
