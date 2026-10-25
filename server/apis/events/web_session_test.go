package events

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/localweb"
	eventhub "github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebLogoutClosesEventConnection(t *testing.T) {
	hub := eventhub.New()
	defer hub.Close()
	raw, _ := (&middleware.LocalTokenAuth{}).Output(nil)
	engine := gin.New()
	engine.Use(raw.(gin.HandlerFunc))
	engine.GET("/api/v1/events", func(ctx *gin.Context) { _ = serve(ctx, hub, nil) })
	server := httptest.NewServer(engine)
	defer server.Close()
	m, err := localweb.New(strings.TrimPrefix(server.URL, "http://"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	old := global.LocalWeb
	global.LocalWeb = m
	defer func() { m.Close(); global.LocalWeb = old }()
	r := httptest.NewRequest("POST", server.URL+"/api/web/session", nil)
	r.Header.Set("Origin", server.URL)
	w := httptest.NewRecorder()
	_, status := m.CreateSession(w, r)
	if status != 200 {
		t.Fatal(status)
	}
	cookie := w.Result().Cookies()[0]
	dialer := websocket.Dialer{Subprotocols: []string{eventhub.Protocol}}
	headers := http.Header{"Origin": []string{server.URL}, "Cookie": []string{cookie.String()}}
	connection, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/events", headers)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	r.AddCookie(cookie)
	m.Logout(httptest.NewRecorder(), r)
	connection.SetReadDeadline(time.Now().Add(time.Second))
	_, _, err = connection.ReadMessage()
	if err == nil {
		t.Fatal("logged out WS still active")
	}
	if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
		t.Fatal("WS was not closed on revocation")
	}
}
