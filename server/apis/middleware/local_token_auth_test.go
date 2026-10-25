package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
)

func TestSubprotocolTokenOnlyGrantsFixedEventUpgrade(t *testing.T) {
	old := global.Config.Security.LocalToken
	global.Config.Security.LocalToken = "local-secret"
	defer func() { global.Config.Security.LocalToken = old }()
	value, err := (&LocalTokenAuth{}).Output(nil)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.Use(value.(gin.HandlerFunc))
	engine.GET("/api/v1/events", func(ctx *gin.Context) { ctx.Status(204) })
	engine.GET("/api/v1/chats", func(ctx *gin.Context) { ctx.Status(204) })
	engine.POST("/api/v1/events", func(ctx *gin.Context) { ctx.Status(204) })
	for key, expected := range map[string]int{"GET /api/v1/events": 204, "POST /api/v1/events": 401, "GET /api/v1/chats": 401} {
		method := key[:3]
		endpoint := key[4:]
		if key[:4] == "POST" {
			method = "POST"
			endpoint = key[5:]
		}
		request := httptest.NewRequest(method, endpoint, nil)
		request.Header.Set("Sec-WebSocket-Protocol", "ally.events.v1, ally.auth.local-secret")
		request.Header.Set("Connection", "Upgrade")
		request.Header.Set("Upgrade", "websocket")
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		if recorder.Code != expected {
			t.Fatalf("%s = %d，期望%d", key, recorder.Code, expected)
		}
	}
	request := httptest.NewRequest("GET", "/api/v1/events", nil)
	request.Header.Set("Authorization", "Bearer wrong")
	request.Header.Set("Sec-WebSocket-Protocol", "ally.auth.local-secret")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	if recorder.Code != 401 {
		t.Fatal("错误的明确Bearer不能由另一头悄悄覆盖")
	}
}
