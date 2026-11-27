package capability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// TestConversationPostOnlyAcceptsThePublicBody checks that a tool invocation cannot set its own destination, sender, or return address.
func TestConversationPostOnlyAcceptsThePublicBody(t *testing.T) {
	op, found := Find("conversations.messages.post")
	if !found {
		t.Fatal("a conversation worker cannot post a public message")
	}
	if op.Effect != constants.AgentCLIEffectMutation || op.Completion != constants.AgentCLICompletionImmediate {
		t.Fatal("public posting must require a mutation request ID and confirm the committed message")
	}
	if err := op.Validate(map[string]any{"content": "发现一项需要处理的变更。\n\n这是证据。"}); err != nil {
		t.Fatal(err)
	}
	for _, override := range []string{"conversationId", "workChatId", "executionId", "wakerId", "replyToMessageId"} {
		if op.Validate(map[string]any{"content": "message", override: "other"}) == nil {
			t.Fatalf("accepted a model-supplied %s", override)
		}
	}
	if op.Validate(map[string]any{"content": ""}) == nil {
		t.Fatal("accepted empty public content")
	}
}

// TestConversationHistoryOnlyAcceptsPaging checks that listing history can only narrow its page and cannot choose another conversation.
func TestConversationHistoryOnlyAcceptsPaging(t *testing.T) {
	op, found := Find("conversations.messages.list")
	if !found || op.Effect != constants.AgentCLIEffectRead {
		t.Fatal("a bound worker cannot read its public history")
	}
	if err := op.Validate(map[string]any{"limit": 2, "beforeSequence": 42}); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []map[string]any{
		{"workChatId": "private"}, {"conversationId": "another"}, {"beforeSequence": -1}, {"limit": 0}, {"limit": 101},
	} {
		if op.Validate(invalid) == nil {
			t.Fatalf("accepted unauthorized or invalid history parameters: %v", invalid)
		}
	}
}

func TestConversationPostAttemptsIncludeRejectedCommands(t *testing.T) {
	registry := New(nil)
	defer registry.Close()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "http://127.0.0.1:18080" + constants.AgentCLIPath
	firstPath, activateFirst, finishFirst, err := registry.PrepareConnection(root, "source", endpoint, GrantSpec{
		ExecutionID: "previous",
		Validate:    func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	activateFirst()
	finishFirst()
	denied := false
	attempts := 0
	path, activate, revoke, err := registry.PrepareConnection(root, "source", endpoint, GrantSpec{
		ExecutionID: "work",
		Bindings: []Binding{{Operation: constants.ConversationMessagePostOperation,
			Attempt: func() { attempts++ },
			Receipt: func(context.Context, string) (Result, error) { return Result{}, nil },
			Call: func(context.Context, Request) (Result, error) {
				t.Fatal("invalid command entered publisher")
				return Result{}, nil
			},
		}},
		Validate: func(context.Context) error {
			if denied {
				return status_error.AgentContextUnauthorized
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer revoke()
	activate()
	if path != firstPath {
		t.Fatal("the next turn did not reuse the connection")
	}
	connection, err := ReadConnection(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"previous execution", "missing execution", "invalid content", "denied grant"} {
		request := httptest.NewRequest(http.MethodPost, constants.AgentCLIPath+"/commands", strings.NewReader(`{"operation":"conversations.messages.post","params":{"content":""},"requestId":"one"}`))
		request.RemoteAddr = "127.0.0.1:12345"
		request.Header.Set("Authorization", "Bearer "+connection.Token)
		request.Header.Set(constants.AgentCLIConnectionHeader, connection.GrantID)
		want := attempts
		switch invalid {
		case "previous execution":
			request.Header.Set(constants.AgentCLIExecutionHeader, "previous")
		case "missing execution":
		default:
			request.Header.Set(constants.AgentCLIExecutionHeader, "work")
			want++
		}
		denied = invalid == "denied grant"
		out := httptest.NewRecorder()
		registry.ServeHTTP(out, request)
		if out.Code < 400 || attempts != want {
			t.Fatalf("%s: status=%d attempts=%d", invalid, out.Code, attempts)
		}
	}
}
