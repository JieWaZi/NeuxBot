package agentcapability

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestGrantActivationScopeAndRevocation(t *testing.T) {
	registry := New(nil)
	defer registry.Close()
	entered := make(chan struct{}, 1)
	left := make(chan struct{}, 1)
	grant, activate, revoke, err := registry.Prepare(GrantSpec{ExecutionID: "one", Bindings: []Binding{{Operation: "im.targets.list", Call: func(ctx context.Context, _ Request) (Result, error) {
		entered <- struct{}{}
		<-ctx.Done()
		left <- struct{}{}
		return Result{}, ctx.Err()
	}}}, Validate: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	send := func(method, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, constants.AgentCLIPath+path, strings.NewReader(body))
		request.RemoteAddr = "127.0.0.1:12345"
		request.Header.Set("Authorization", "Bearer "+grant.Token)
		response := httptest.NewRecorder()
		registry.ServeHTTP(response, request)
		return response
	}
	if response := send(http.MethodGet, "/capabilities", ""); response.Code != 401 {
		t.Fatalf("inactive grant accepted: %d", response.Code)
	}
	activate()
	response := send(http.MethodGet, "/capabilities", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), "im.targets.list") || strings.Contains(response.Body.String(), "workflow.source.save") {
		t.Fatalf("scope leaked: %s", response.Body)
	}
	if response := send(http.MethodGet, "/operations/workflow.source.save", ""); response.Code != 403 {
		t.Fatal("cross-domain discovery accepted")
	}
	if response := send(http.MethodGet, "/operations/workflow.authoring.prepare", ""); response.Code != 403 {
		t.Fatal("authoring preparation leaked outside the creation grant")
	}
	if response := send(http.MethodPost, "/commands", `{"operation":"im.targets.list","params":{},"inputDigests":{"/file":"forged"}}`); response.Code != 400 {
		t.Fatalf("caller supplied server-only input digests: %d %s", response.Code, response.Body)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		send(http.MethodPost, "/commands", `{"operation":"im.targets.list","params":{}}`)
	}()
	<-entered
	revoke()
	<-left
	<-done
	activate()
	if response := send(http.MethodGet, "/capabilities", ""); response.Code != 401 {
		t.Fatal("activation resurrected revoked grant")
	}
	registry.Close()
	_, _, _, err = registry.Prepare(GrantSpec{ExecutionID: "two", Validate: func(context.Context) error { return nil }})
	if err == nil {
		t.Fatal("closed registry issued grant")
	}
}

func TestCurrentConnectionAndRemoteAddressRejection(t *testing.T) {
	base := Connection{ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: "http://127.0.0.1:8432/agent/v1", CoreEpoch: "epoch", GrantID: "grant", Token: "private"}
	for _, endpoint := range []string{"http://example.com:8432/agent/v1", "http://127.0.0.1:8432/api/v1", "https://127.0.0.1:8432/agent/v1", "http://user:password@127.0.0.1:8432/agent/v1", "http://127.0.0.1:8432/agent/v1?forward=1"} {
		value := base
		value.Endpoint = endpoint
		if value.Validate() == nil {
			t.Fatalf("unsafe endpoint accepted: %s", endpoint)
		}
	}
	var response Response
	if json.Unmarshal([]byte(`{"protocolVersion":1,"ok":true,"data":{"state":"failed"}}`), &response) != nil || !response.OK {
		t.Fatal("observation confused with resource success")
	}
	busy := ErrorResponse("team.tasks.list", Reject(status_error.DatabaseBusy, "The task board is temporarily unavailable."))
	if busy.Error == nil || busy.Error.Code != status_error.DatabaseBusy.Key() || busy.Error.RetryAdvice != "same_request" || busy.Error.EffectState != "not_accepted" {
		t.Fatalf("temporary failure lost its recovery advice: %+v", busy)
	}
}

func TestInvalidContextRecoveryRequiresHostAuthorization(t *testing.T) {
	response := ErrorResponse("workflow.source.get", status_error.AgentContextUnauthorized)
	if response.Error == nil || response.Error.RetryAdvice != "refresh_context" || !strings.Contains(response.Error.Message, "host must provide") || !strings.Contains(response.Error.Message, "Do not retry") {
		t.Fatalf("失效上下文缺少明确恢复边界: %+v", response)
	}
}
