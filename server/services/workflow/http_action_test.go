package workflow

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestHTTPActionUsesRealRequestsAndKeepsFailureReceipts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			if r.Method != "POST" || r.Header.Get("X-Trace") != "workflow" {
				t.Error("request configuration lost")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"actual":true}`))
		case "/failed":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(409)
			_, _ = w.Write([]byte(`{"reason":"conflict"}`))
		case "/invalid":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`not JSON`))
		case "/redirect":
			http.Redirect(w, r, "http://127.0.0.1/escape", http.StatusFound)
		case "/slow":
			<-r.Context().Done()
		}
	}))
	defer server.Close()
	// 仅 DNS/连接目标作为窄测试接缝；请求、响应、重定向和 context 都使用真实 HTTP。
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "workflow.example:80" {
			return nil, status_error.WorkflowResourceInvalid
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	service, _ := workflowTestService(t)
	declaration := workflowdsl.ActionDeclaration{ID: "http", Type: "http", AllowedOrigins: []string{"http://workflow.example"}, Methods: []string{"POST", "GET"}}
	for _, item := range []struct {
		// path 选择实际服务器行为。
		path string
		// code 是期望的结构化错误身份，空值表示成功。
		code string
	}{{"ok", ""}, {"failed", status_error.WorkflowEffectFailed.Key()}, {"invalid", status_error.WorkflowSchemaInvalid.Key()}, {"redirect", status_error.WorkflowEffectFailed.Key()}} {
		result, unknown, err := service.executeHTTPWithDial(context.Background(), models.WorkflowRun{}, declaration, map[string]any{"url": "http://workflow.example/" + item.path, "method": "POST", "headers": map[string]any{"X-Trace": "workflow"}, "body": map[string]any{"input": 1}}, dial)
		if unknown {
			t.Fatalf("complete HTTP response became unknown: %s", item.path)
		}
		if item.code == "" && err != nil {
			t.Fatal(err)
		}
		if item.code != "" && workflowstate.ErrorKey(err) != item.code {
			t.Fatalf("%s: %v", item.path, err)
		}
		if item.path == "failed" || item.path == "invalid" {
			if result.(map[string]any)["http"] == nil {
				t.Fatal("failure lost real receipt")
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := service.executeHTTPWithDial(ctx, models.WorkflowRun{}, declaration, map[string]any{"url": "http://workflow.example/slow"}, dial); !errors.Is(err, status_error.WorkflowTimeout) {
		t.Fatal("request deadline did not stop HTTP", err)
	}
}

func TestHTTPDestinationRejectsLocalAndSpecialNetworks(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "::1", "10.0.0.1", "192.168.2.1", "100.64.0.1", "169.254.169.254", "192.0.2.1", "2001:db8::1", "::ffff:127.0.0.1"} {
		if publicAddress(netip.MustParseAddr(value)) {
			t.Fatal("special address treated as public", value)
		}
	}
	if !publicAddress(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public address rejected")
	}
	if _, err := publicDial(context.Background(), "tcp", "localhost:80"); workflowstate.ErrorKey(err) != status_error.WorkflowCapabilityUnsupported.Key() {
		t.Fatal("resolved localhost accepted", err)
	}
	for _, value := range []string{"http://127.0.0.1", "http://user:pass@workflow.example", "http://workflow.example/#fragment", "http://other.example"} {
		if _, err := validateHTTPDestination(value, []string{"http://workflow.example", "http://127.0.0.1"}); err == nil {
			t.Fatal("unsafe destination accepted", value)
		}
	}
}
