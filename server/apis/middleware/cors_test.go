package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCORSAllowsLastEventIDPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(CORS())
	engine.GET("/events", func(ctx *gin.Context) {
		ctx.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodOptions, "/events?after=5", nil)
	request.Header.Set("Origin", "http://tauri.localhost")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	request.Header.Set("Access-Control-Request-Headers", "authorization,last-event-id")
	recorder := httptest.NewRecorder()

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("预检状态码 = %d，期望 %d", recorder.Code, http.StatusNoContent)
	}
	allowedHeaders := strings.ToLower(recorder.Header().Get("Access-Control-Allow-Headers"))
	if !strings.Contains(allowedHeaders, "last-event-id") {
		t.Fatalf("允许请求头 = %q，必须包含 Last-Event-ID", allowedHeaders)
	}
}

func TestCORSAllowsRuntimeLanguagePreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(CORS())
	engine.GET("/api/v1/runtimes", func(ctx *gin.Context) {
		t.Error("预检请求不应执行智能体列表查询")
		ctx.Status(http.StatusOK)
	})

	for _, origin := range []string{"http://127.0.0.1:1420", "bot://app"} {
		t.Run(origin, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodOptions, "/api/v1/runtimes", nil)
			request.Header.Set("Origin", origin)
			request.Header.Set("Access-Control-Request-Method", http.MethodGet)
			request.Header.Set("Access-Control-Request-Headers", "authorization,ginx-lang-header")
			recorder := httptest.NewRecorder()

			engine.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusNoContent {
				t.Fatalf("预检状态码 = %d，期望 %d", recorder.Code, http.StatusNoContent)
			}
			if allowedOrigin := recorder.Header().Get("Access-Control-Allow-Origin"); allowedOrigin != "*" && allowedOrigin != origin {
				t.Fatalf("允许来源 = %q，未授权 %q", allowedOrigin, origin)
			}
			allowedHeaders := strings.Split(recorder.Header().Get("Access-Control-Allow-Headers"), ",")
			for _, requested := range strings.Split(request.Header.Get("Access-Control-Request-Headers"), ",") {
				allowed := false
				for _, header := range allowedHeaders {
					if strings.EqualFold(strings.TrimSpace(header), requested) {
						allowed = true
						break
					}
				}
				if !allowed {
					t.Errorf("预检响应未允许请求头 %q，浏览器将阻止智能体列表请求", requested)
				}
			}
		})
	}
}
