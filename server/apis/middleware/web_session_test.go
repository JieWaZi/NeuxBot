package middleware

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/localweb"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
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
	code, _ := m.RotatePairing()
	pair := httptest.NewRequest("POST", "http://127.0.0.1:8432/api/web/session", nil)
	pair.Header.Set("Origin", "http://127.0.0.1:8432")
	recorder := httptest.NewRecorder()
	session, status := m.Pair(recorder, pair, code)
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
