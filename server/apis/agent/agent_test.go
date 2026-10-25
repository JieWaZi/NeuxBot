package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/gin-gonic/gin"
)

func TestReadUsesExecutionAuthenticationAndWritesOneProtocolEnvelope(t *testing.T) {
	previous := global.AgentCapabilities
	registry := agentcapability.New(nil)
	global.AgentCapabilities = registry
	t.Cleanup(func() { global.AgentCapabilities = previous; registry.Close() })
	grant, activate, revoke, err := registry.Prepare(agentcapability.GrantSpec{ExecutionID: "active", Bindings: []agentcapability.Binding{{Operation: "im.targets.list", Call: func(context.Context, agentcapability.Request) (agentcapability.Result, error) {
		return agentcapability.Result{}, nil
	}}}, Validate: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer revoke()
	activate()
	for _, test := range []struct {
		// name 说明当前认证或来源反例。
		name string
		// token 是本次 HTTP 实际提供的凭据。
		token string
		// origin 模拟浏览器来源；命令协议不接受浏览器请求。
		origin string
		// remote 限定真实 TCP 对端，忽略转发头。
		remote string
		// status 是预期 HTTP 状态。
		status int
	}{
		{"execution grant", grant.Token, "", "127.0.0.1:1234", http.StatusOK},
		{"management token is not a grant", "desktop-management-token", "", "127.0.0.1:1234", http.StatusUnauthorized},
		{"browser origin", grant.Token, "http://127.0.0.1", "127.0.0.1:1234", http.StatusUnauthorized},
		{"forwarded loopback cannot hide remote peer", grant.Token, "", "192.0.2.1:1234", http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			writer := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(writer)
			ctx.Request = httptest.NewRequest(http.MethodGet, constants.AgentCLIPath+"/capabilities", nil)
			ctx.Request.RemoteAddr = test.remote
			ctx.Request.Header.Set("Authorization", "Bearer "+test.token)
			ctx.Request.Header.Set("Origin", test.origin)
			ctx.Request.Header.Set("X-Forwarded-For", "127.0.0.1")
			if _, err := (&Read{}).Output(ctx); err != nil {
				t.Fatal(err)
			}
			var response agentcapability.Response
			if writer.Code != test.status || !ctx.IsAborted() || json.Unmarshal(writer.Body.Bytes(), &response) != nil || response.ProtocolVersion != constants.AgentCLIProtocolVersion || response.OK != (test.status == http.StatusOK) {
				t.Fatalf("unexpected protocol output: status=%d body=%s", writer.Code, writer.Body)
			}
		})
	}
}
