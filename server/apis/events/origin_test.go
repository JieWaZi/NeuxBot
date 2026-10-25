package events

import (
	"context"
	"net/http"
	"strings"
	"testing"

	eventhub "github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/gorilla/websocket"
)

func TestEventConfiguredExactOriginsStillRequireAuthentication(t *testing.T) {
	t.Setenv("ALLY_EVENT_TRUSTED_ORIGINS", `["http://127.0.0.1:1422","https://ally.example.com"]`)
	hub := eventhub.New()
	server := testServer(t, hub, func(context.Context, control) (source, error) {
		t.Fatal("handshake must not create a subscription")
		return source{}, nil
	})
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/events"
	for _, test := range []struct {
		// origin 是浏览器实际发送的完整来源，不做后缀或端口推断。
		origin string
		// authenticated 决定是否携带独立本机令牌。
		authenticated bool
		// status 是HTTP升级或认证拒绝的真实结果。
		status int
	}{{"http://127.0.0.1:1422", true, 101}, {"https://ally.example.com", true, 101}, {"http://127.0.0.1:1420", true, 101}, {"ally://app", true, 101}, {"", true, 101}, {"http://127.0.0.1:1423", true, 403}, {"https://ally.example.com.evil.invalid", true, 403}, {"https://ally.example.com@evil.invalid", true, 403}, {"http://127.0.0.1:1422", false, 401}} {
		protocols := []string{eventhub.Protocol}
		if test.authenticated {
			protocols = append(protocols, "ally.auth.test-secret")
		}
		dialer := websocket.Dialer{Subprotocols: protocols}
		connection, response, err := dialer.Dial(endpoint, http.Header{"Origin": []string{test.origin}})
		if connection != nil {
			_ = connection.Close()
		}
		if response == nil || response.StatusCode != test.status {
			t.Fatalf("origin=%q authenticated=%v status=%v err=%v", test.origin, test.authenticated, response, err)
		}
		if test.status == 101 && err != nil {
			t.Fatal(err)
		}
		if test.status != 101 && err == nil {
			t.Fatal("rejected handshake unexpectedly succeeded")
		}
	}
}

func TestInvalidEventOriginConfigurationDoesNotGrantAdditionalOrigins(t *testing.T) {
	for _, value := range []string{`not-json`, `null`, `"http://127.0.0.1:1422"`, `["*"]`, `["http://127.0.0.1:*"]`, `["http://127.0.0.1:1422/path"]`, `["http://user@127.0.0.1:1422"]`, `["http://127.0.0.1:1422?token=private"]`, `["http://127.0.0.1:1422#fragment"]`, `["http://127.0.0.1:65536"]`, `["http://127.0.0.1:1422","https://bad host"]`, `["file://127.0.0.1:1422"]`, `["https://"]`, `["https://-invalid.example"]`} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("ALLY_EVENT_TRUSTED_ORIGINS", value)
			request, _ := http.NewRequest("GET", "http://127.0.0.1/api/v1/events", nil)
			request.Header.Set("Origin", "http://127.0.0.1:1422")
			if trustedOrigin(request) {
				t.Fatal("invalid configuration granted a new origin")
			}
			request.Header.Set("Origin", "ally://app")
			if !trustedOrigin(request) {
				t.Fatal("invalid extra origin configuration changed native default")
			}
		})
	}
}

func TestEventOriginDefaultDoesNotAllowArbitraryLoopbackPorts(t *testing.T) {
	t.Setenv("ALLY_EVENT_TRUSTED_ORIGINS", "")
	request, _ := http.NewRequest("GET", "http://127.0.0.1/api/v1/events", nil)
	request.Header.Set("Origin", "http://127.0.0.1:1422")
	if trustedOrigin(request) {
		t.Fatal("unconfigured loopback origin was accepted")
	}
}
