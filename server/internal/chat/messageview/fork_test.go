package messageview

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

func TestForkCapabilityFollowsCanonicalResultAndNativeSession(t *testing.T) {
	builder := NewPublicMessageViewBuilder()
	sequence := int64(0)
	apply := func(typ constants.ChatEventType, execution string, data any) {
		t.Helper()
		sequence++
		encoded, _ := json.Marshal(data)
		if _, err := builder.Apply(eventlog.Record{Sequence: sequence, Type: typ, ExecutionID: execution, Data: encoded}); err != nil {
			t.Fatal(err)
		}
	}
	apply(constants.ChatEventTypeCreated, "", eventlog.ChatCreatedData{WorkspaceDirectory: "/workspace"})
	apply(constants.ChatEventTypeExecutionCreated, "one", eventlog.ExecutionCreatedData{RuntimeID: "runtime"})
	builder.SetNativeResult("one", &agent.NativeResult{Key: agent.NativeResultKey{Session: agent.NativeSessionKey{Provider: "codex", Subject: "fixture", SessionID: "private-native-session"}}, ForkMode: "latest"})
	builder.SetCurrentForkSession("private-native-session")
	apply(constants.ChatEventTypeExecutionCompleted, "one", eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})
	assertFork := func(wantAvailable bool, wantReason string) {
		t.Helper()
		encoded, _ := json.Marshal(builder.PublicMessage("one"))
		var message struct {
			// Fork 是公开消息中供用户操作的能力。
			Fork *struct {
				// Available 表示原生事实允许分叉。
				Available bool
				// LatestOnly 表示必须仍是最后回合。
				LatestOnly bool
				// ReasonCode 是安全禁用原因。
				ReasonCode string
			}
		}
		if err := json.Unmarshal(encoded, &message); err != nil {
			t.Fatal(err)
		}
		if message.Fork == nil || message.Fork.Available != wantAvailable || !message.Fork.LatestOnly || message.Fork.ReasonCode != wantReason {
			t.Fatalf("wrong safe fork capability: %s", encoded)
		}
		if strings.Contains(string(encoded), "private-native-session") {
			t.Fatal("native handle leaked")
		}
	}
	assertFork(true, "")
	builder.SetCurrentForkSession("")
	assertFork(false, "session_changed")
}
