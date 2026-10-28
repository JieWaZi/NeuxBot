package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/localweb"
	"github.com/gin-gonic/gin"
)

func TestWebSessionCannotBypassInvalidBearerAndBindsSubject(t *testing.T) {
	old := global.LocalWeb
	oldToken := global.Config.Security.LocalToken
	m, err := localweb.New("127.0.0.1:8432", "", "")
	if err != nil {
		t.Fatal(err)
	}
	global.LocalWeb = m
	global.Config.Security.LocalToken = "valid"
	t.Cleanup(func() { m.Close(); global.LocalWeb = old; global.Config.Security.LocalToken = oldToken })
	pair := httptest.NewRequest("POST", "http://127.0.0.1:8432/api/web/session", nil)
	pair.Header.Set("Origin", "http://127.0.0.1:8432")
	recorder := httptest.NewRecorder()
	session, status := m.CreateSession(recorder, pair)
	if status != 200 {
		t.Fatal(status)
	}
	cookie := recorder.Result().Cookies()[0]
	raw, _ := (&LocalTokenAuth{}).Output(nil)
	engine := gin.New()
	engine.Use(raw.(gin.HandlerFunc))
	engine.GET("/api/v1/test", func(c *gin.Context) { c.String(200, AuthSubject(c)) })
	for _, header := range []string{"", "Bearer invalid", "Basic invalid", "Bearer valid"} {
		r := httptest.NewRequest("GET", "http://127.0.0.1:8432/api/v1/test", nil)
		r.AddCookie(cookie)
		if header != "" {
			r.Header.Set("Authorization", header)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		switch header {
		case "":
			if w.Code != 200 || w.Body.String() != "web:"+session.ID {
				t.Fatal(w.Code, w.Body.String())
			}
		case "Bearer valid":
			if w.Code != 200 || w.Body.String() != "desktop" {
				t.Fatal(w.Code, w.Body.String())
			}
		default:
			if w.Code != http.StatusUnauthorized {
				t.Fatal("invalid bearer accepted", w.Code)
			}
		}
	}
}

func TestWebCORSDoesNotAllowCredentialedCrossOrigin(t *testing.T) {
	old := global.LocalWeb
	m, _ := localweb.New("127.0.0.1:8432", "", "")
	global.LocalWeb = m
	t.Cleanup(func() { m.Close(); global.LocalWeb = old })
	engine := gin.New()
	engine.Use(CORS())
	engine.GET("/api/v1/test", func(c *gin.Context) { c.Status(200) })
	r := httptest.NewRequest("GET", "http://127.0.0.1:8432/api/v1/test", nil)
	r.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, r)
	if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("cookie CORS exposed")
	}
}

func TestWebStaticRoutingPreservesExecutionCapabilityProtocol(t *testing.T) {
	old := global.LocalWeb
	web, err := localweb.New("127.0.0.1:8432", "", "")
	if err != nil {
		t.Fatal(err)
	}
	global.LocalWeb = web
	t.Cleanup(func() { web.Close(); global.LocalWeb = old })
	registry := agentcapability.New(nil)
	defer registry.Close()
	grant, activate, revoke, err := registry.Prepare(agentcapability.GrantSpec{ExecutionID: "workflow-turn", Validate: func(context.Context) error { return nil }, Bindings: []agentcapability.Binding{{Operation: "workflow.source.get", Call: func(context.Context, agentcapability.Request) (agentcapability.Result, error) {
		return agentcapability.Result{}, nil
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	defer revoke()
	engine := gin.New()
	engine.Use(CORS())
	engine.Any(constants.AgentCLIPath+"/*path", gin.WrapH(registry))
	for _, origin := range []string{"", "http://127.0.0.1:8432"} {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8432"+constants.AgentCLIPath+"/capabilities", nil)
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Authorization", "Bearer "+grant.Token)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		var response agentcapability.Response
		if json.Unmarshal(w.Body.Bytes(), &response) != nil || response.ProtocolVersion != constants.AgentCLIProtocolVersion {
			t.Fatalf("execution protocol routed as static content: status=%d body=%s", w.Code, w.Body)
		}
		expected := http.StatusOK
		if origin != "" {
			expected = http.StatusUnauthorized
		}
		if w.Code != expected || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("execution origin policy changed: %d %v", w.Code, w.Header())
		}
	}
}
