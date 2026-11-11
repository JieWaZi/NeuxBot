package capability

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
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

func TestPreparedGrantOnlyExposesMCPToolCatalog(t *testing.T) {
	registry := New(nil)
	defer registry.Close()
	validations, calls := 0, 0
	grant, _, revoke, err := registry.Prepare(GrantSpec{ExecutionID: "preparing", Bindings: []Binding{{
		Operation: "workflow.source.get",
		Call: func(context.Context, Request) (Result, error) {
			calls++
			return Result{}, nil
		},
	}}, Validate: func(context.Context) error {
		validations++
		return status_error.AgentContextUnauthorized
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer revoke()
	send := func(method, path, token, origin string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, constants.AgentCLIPath+path, strings.NewReader(`{"operation":"workflow.source.get","params":{}}`))
		request.RemoteAddr = "127.0.0.1:12345"
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		registry.ServeHTTP(response, request)
		return response
	}
	response := send(http.MethodGet, constants.AgentCLIMCPCatalogPath, grant.Token, "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "workflow.source.get") || strings.Contains(response.Body.String(), "workflow.source.save") {
		t.Fatalf("prepared grant did not expose exactly its tool contracts: %d %s", response.Code, response.Body)
	}
	for _, path := range []string{"/capabilities", "/operations/workflow.source.get", "/guides/workflow.authoring", "/receipts?operation=workflow.source.save&requestId=one", constants.AgentCLIMCPCatalogPath + "/", "/other" + constants.AgentCLIMCPCatalogPath} {
		if response := send(http.MethodGet, path, grant.Token, ""); response.Code != http.StatusUnauthorized {
			t.Fatalf("prepared grant accessed %s: %d", path, response.Code)
		}
	}
	for _, path := range []string{"/commands", constants.AgentCLIMCPCatalogPath} {
		if response := send(http.MethodPost, path, grant.Token, ""); response.Code != http.StatusUnauthorized {
			t.Fatalf("prepared grant posted to %s: %d", path, response.Code)
		}
	}
	for _, token := range []string{"", "unknown"} {
		if response := send(http.MethodGet, constants.AgentCLIMCPCatalogPath, token, ""); response.Code != http.StatusUnauthorized {
			t.Fatal("tool catalog accepted invalid authentication")
		}
	}
	if response := send(http.MethodGet, constants.AgentCLIMCPCatalogPath, grant.Token, "https://example.com"); response.Code != http.StatusUnauthorized {
		t.Fatal("tool catalog accepted a browser origin")
	}
	if validations != 0 || calls != 0 {
		t.Fatalf("catalog bootstrap touched business state: validations=%d calls=%d", validations, calls)
	}
	revoke()
	if response := send(http.MethodGet, constants.AgentCLIMCPCatalogPath, grant.Token, ""); response.Code != http.StatusUnauthorized {
		t.Fatal("revoked grant retained its tool catalog")
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

func TestObserveSubscribesBeforeReadingAndReleasesOnCompletion(t *testing.T) {
	hub := events.New()
	defer hub.Close()
	resource := events.Resource{Kind: "job", ID: "one"}
	subscribed, closed, reads := false, false, 0
	result, err := Observe(context.Background(), map[string]any{"timeoutMs": 1000}, func() events.Subscription {
		subscribed = true
		subscription := hub.Subscribe("job", resource)
		return events.Subscription{Events: subscription.Events, Close: func() { closed = true; subscription.Close() }}
	}, func(context.Context) (any, bool, error) {
		if !subscribed {
			t.Fatal("authority was read before subscribing")
		}
		reads++
		if reads == 1 {
			hub.Publish(events.Event{Topic: "job", Resource: resource})
			return "pending", false, nil
		}
		return "failed", true, nil
	})
	if err != nil || reads != 2 || !closed {
		t.Fatalf("observation lost transition or subscription: result=%+v err=%v reads=%d closed=%v", result, err, reads, closed)
	}
	data := result.Data.(map[string]any)
	if data["completed"] != true || data["resource"] != "failed" {
		t.Fatalf("terminal failure was confused with observation failure: %+v", result)
	}
}

func TestObserveBoundsWaitAndDistinguishesInvalidation(t *testing.T) {
	for _, test := range []struct {
		// name distinguishes the actual reason an observation ended.
		name string
		// cancelled reports whether invocation authorization was revoked.
		cancelled bool
		// invalidated reports whether the event buffer is unrecoverable.
		invalidated bool
		// want is the stable error for the termination reason; reaching the deadline is not an error.
		want error
	}{
		{name: "timeout"},
		{name: "revoked", cancelled: true, want: status_error.AgentContextUnauthorized},
		{name: "invalidated", invalidated: true, want: status_error.AgentConnectionUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			queue := make(chan events.Event)
			if test.invalidated {
				close(queue)
			}
			if test.cancelled {
				cancel()
			}
			window := int64(60000)
			if test.want == nil {
				window = 1
			}
			closed := false
			result, err := Observe(ctx, map[string]any{"timeoutMs": window}, func() events.Subscription {
				return events.Subscription{Events: queue, Close: func() { closed = true }}
			}, func(context.Context) (any, bool, error) { return "running", false, nil })
			if !errors.Is(err, test.want) || !closed {
				t.Fatalf("observation cause lost: result=%+v err=%v closed=%v", result, err, closed)
			}
			if test.want == nil {
				data := result.Data.(map[string]any)
				if data["completed"] != false || data["resource"] != "running" {
					t.Fatalf("deadline changed resource state: %+v", result)
				}
			}
		})
	}
}

func TestConnectionPublicationAndPrivateFileBoundary(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	value := Connection{ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: "http://127.0.0.1:8432/agent/v1", CoreEpoch: "epoch", GrantID: "grant", Token: "private"}
	path, err := WriteConnection(root, "connection.json", value)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := ReadConnection(path)
	if err != nil || actual != value {
		t.Fatalf("published connection cannot be read: actual=%+v err=%v", actual, err)
	}
	if _, err := WriteConnection(root, "../escaped.json", value); !errors.Is(err, status_error.AgentContextStorageError) {
		t.Fatalf("connection escaped private directory: %v", err)
	}
	if _, err := ReadConnection("connection.json"); !errors.Is(err, status_error.AgentContextStorageError) {
		t.Fatalf("relative connection was accepted: %v", err)
	}
	if runtime.GOOS == "windows" {
		// Windows controls access through the dedicated directory's inherited DACL; POSIX modes are checked only on Unix.
		return
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadConnection(path); !errors.Is(err, status_error.AgentContextStorageError) {
		t.Fatalf("readable-by-others connection was accepted: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "other.json"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteConnection(root, "connection.json", value); !errors.Is(err, status_error.AgentContextStorageError) {
		t.Fatalf("symlink connection was replaced: %v", err)
	}
}

// receiptMemoryStore fakes only the domain repository port, not command orchestration or receipt digests.
type receiptMemoryStore map[string]Receipt

func (s receiptMemoryStore) Find(_ context.Context, id string) (Receipt, bool, error) {
	value, found := s[id]
	return value, found, nil
}

func (s receiptMemoryStore) Create(_ context.Context, value Receipt) error {
	s[value.ID] = value
	return nil
}

func TestReceiptCommitPreservesAdmissionAndRejectsChangedInput(t *testing.T) {
	store := receiptMemoryStore{}
	request := Request{Operation: "im.deliveries.create", RequestID: "report-01", Params: map[string]any{"targetId": "target", "content": "report"}, InputDigests: map[string]string{"/report.md": "first"}}
	calls := 0
	result, err := Execute(context.Background(), store, "scope", request, func(ctx context.Context) error {
		calls++
		if !Active(ctx) {
			t.Fatal("domain cannot detect required command receipt")
		}
		if err := Commit(ctx, map[string]any{"deliveryId": "delivery", "state": "pending"}); err != nil {
			return err
		}
		return status_error.DatabaseError
	})
	if err != nil || !result.Replayed || result.CommandID == "" || result.Data.(map[string]any)["deliveryId"] != "delivery" {
		t.Fatalf("post-commit failure overturned admission: result=%+v err=%v", result, err)
	}
	replay, err := Execute(context.Background(), store, "scope", request, func(context.Context) error { calls++; return nil })
	if err != nil || !replay.Replayed || replay.CommandID != result.CommandID || calls != 1 {
		t.Fatalf("same identity was reexecuted: result=%+v err=%v calls=%d", replay, err, calls)
	}
	request.InputDigests["/report.md"] = "changed"
	if _, err := Execute(context.Background(), store, "scope", request, func(context.Context) error { calls++; return nil }); !errors.Is(err, status_error.AgentCommandRequestConflict) || calls != 1 {
		t.Fatalf("changed file content reused prior identity: err=%v calls=%d", err, calls)
	}
	if _, err := Lookup(context.Background(), store, "other-scope", request.Operation, request.RequestID); !errors.Is(err, status_error.AgentReceiptNotFound) {
		t.Fatalf("receipt leaked across scope: %v", err)
	}
	lookup, err := Lookup(context.Background(), store, "scope", request.Operation, request.RequestID)
	if err != nil || !lookup.Replayed || lookup.CommandID != result.CommandID {
		t.Fatalf("receipt lookup lost original identity: result=%+v err=%v", lookup, err)
	}
}

func TestReceiptRequiresExplicitDomainCommit(t *testing.T) {
	store := receiptMemoryStore{}
	request := Request{Operation: "im.deliveries.create", RequestID: "report-01", Params: map[string]any{"targetId": "target"}}
	_, err := Execute(context.Background(), store, "scope", request, func(context.Context) error { return nil })
	if !errors.Is(err, status_error.AgentCommandOutcomeUnknown) || len(store) != 0 {
		t.Fatalf("uncommitted call reported acceptance: err=%v receipts=%v", err, store)
	}
	if err := Commit(context.Background(), map[string]any{"deliveryId": "ordinary"}); err != nil || len(store) != 0 {
		t.Fatalf("ordinary entry created command receipt: err=%v receipts=%v", err, store)
	}
}
