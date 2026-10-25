package runtimes

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

// recordingElicitationHandler 记录请求实际被路由到的 Session。
type recordingElicitationHandler struct {
	// calls 是该处理器收到的结构化输入请求数量。
	calls int
}

// Request 接受测试请求并记录调用次数。
func (h *recordingElicitationHandler) Request(
	context.Context,
	ElicitationRequest,
) (ElicitationDecision, error) {
	h.calls++
	return ElicitationDecision{Accepted: true, Content: map[string]any{"answer": "ok"}}, nil
}

func TestACPElicitationRequiresExactSessionRoute(t *testing.T) {
	client := newACPClient(codexACPRuntimeBinding())
	firstHandler := &recordingElicitationHandler{}
	secondHandler := &recordingElicitationHandler{}
	first := newACPBridge("session-first", "invocation-first", nil, Workspace{}, ExecutionPolicy{}, acpRuntimeBinding{}, 0)
	second := newACPBridge("session-second", "invocation-second", nil, Workspace{}, ExecutionPolicy{}, acpRuntimeBinding{}, 0)
	first.elicitationHandler = firstHandler
	second.elicitationHandler = secondHandler
	if err := client.register(first); err != nil {
		t.Fatalf("register first bridge: %v", err)
	}
	if err := client.register(second); err != nil {
		t.Fatalf("register second bridge: %v", err)
	}

	request := acp.NewUnstableCreateElicitationRequestForm(acp.UnstableElicitationSchema{
		Properties: map[string]any{"answer": map[string]any{"type": "string"}},
	})
	request.Form.Message = "请选择"

	response, err := client.UnstableCreateElicitation(context.Background(), request)
	if err != nil || response.Cancel == nil {
		t.Fatalf("missing-session response = %+v, error = %v, want cancel", response, err)
	}
	request.Form.Meta = map[string]any{"sessionId": "session-unknown"}
	response, err = client.UnstableCreateElicitation(context.Background(), request)
	if err != nil || response.Cancel == nil {
		t.Fatalf("unknown-session response = %+v, error = %v, want cancel", response, err)
	}
	request.Form.Meta = map[string]any{
		"sessionId": "session-first",
		"codex":     map[string]any{"sessionId": "session-second"},
	}
	response, err = client.UnstableCreateElicitation(context.Background(), request)
	if err != nil || response.Cancel == nil {
		t.Fatalf("ambiguous-session response = %+v, error = %v, want cancel", response, err)
	}
	if firstHandler.calls != 0 || secondHandler.calls != 0 {
		t.Fatalf("fail-closed calls = (%d, %d), want (0, 0)", firstHandler.calls, secondHandler.calls)
	}

	request.Form.Meta = map[string]any{"session": map[string]any{"sessionId": "session-second"}}
	response, err = client.UnstableCreateElicitation(context.Background(), request)
	if err != nil || response.Accept == nil {
		t.Fatalf("exact-session response = %+v, error = %v, want accept", response, err)
	}
	if firstHandler.calls != 0 || secondHandler.calls != 1 {
		t.Fatalf("exact-route calls = (%d, %d), want (0, 1)", firstHandler.calls, secondHandler.calls)
	}
}

func TestKnowledgeMCPMetadataDoesNotBypassUserInput(t *testing.T) {
	request := acp.NewUnstableCreateElicitationRequestForm(acp.UnstableElicitationSchema{Type: acp.UnstableElicitationSchemaTypeObject, Properties: map[string]any{}})
	request.Form.Message = "Allow the ally_knowledge MCP server to run tool \"knowledge_stage\"?"
	request.Form.Meta = map[string]any{"codex": map[string]any{"sessionId": "session", "serverName": "ally_knowledge"}}
	bridge := newACPBridge("session", "invocation", nil, Workspace{}, ExecutionPolicy{}, codexACPRuntimeBinding(), 0)
	if response := bridge.createElicitation(context.Background(), request); response.Accept != nil {
		t.Fatal("retired knowledge metadata bypassed user input")
	}
	handler := &recordingElicitationHandler{}
	bridge.elicitationHandler = handler
	if response := bridge.createElicitation(context.Background(), request); response.Accept == nil || handler.calls != 1 {
		t.Fatal("ordinary elicitation no longer reaches its handler")
	}
}
