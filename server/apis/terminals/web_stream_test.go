package terminals

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/localweb"
	"github.com/JieWaZi/NeuxBot/server/internal/terminalruntime"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type terminalChatFixture struct {
	// Module 未使用的业务接口不属于终端协议测试范围。
	chat.Module
	// root 是该聊天冻结的唯一测试目录。
	root string
}

func (f terminalChatFixture) GetChatSummary(context.Context, string) (chat.ChatSummary, error) {
	return chat.ChatSummary{Chat: models.Chat{WorkspaceDirectory: f.root}}, nil
}

func TestRealWebTerminalHTTPStreamAndLogout(t *testing.T) {
	worker := os.Getenv("ALLY_TERMINAL_TEST_WORKER")
	if worker == "" {
		t.Skip("requires built terminal worker")
	}
	t.Setenv("SHELL", "/bin/sh")
	runtime, err := terminalruntime.New("node", worker)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	oldRuntime, oldChat, oldWeb := global.TerminalRuntime, global.ChatService, global.LocalWeb
	global.TerminalRuntime = runtime
	root := t.TempDir()
	global.ChatService = terminalChatFixture{root: root}
	defer func() { global.TerminalRuntime = oldRuntime; global.ChatService = oldChat; global.LocalWeb = oldWeb }()
	raw, _ := (&middleware.LocalTokenAuth{}).Output(nil)
	engine := gin.New()
	engine.Use(raw.(gin.HandlerFunc))
	engine.POST("/api/v1/terminals", func(ctx *gin.Context) {
		op := &StartTerminal{}
		if ctx.ShouldBindJSON(&op.Body) != nil {
			ctx.Status(400)
			return
		}
		result, err := op.Output(ctx)
		if err != nil {
			ctx.Status(400)
			return
		}
		ctx.JSON(200, result)
	})
	engine.GET("/api/v1/terminals/:sessionId/stream", func(ctx *gin.Context) { _, _ = (&StreamTerminal{SessionID: ctx.Param("sessionId")}).Output(ctx) })
	server := httptest.NewServer(engine)
	defer server.Close()
	manager, err := localweb.New(strings.TrimPrefix(server.URL, "http://"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	global.LocalWeb = manager
	defer manager.Close()
	pairRequest := httptest.NewRequest("POST", server.URL+"/api/web/session", nil)
	pairRequest.Header.Set("Origin", server.URL)
	paired := httptest.NewRecorder()
	session, status := manager.CreateSession(paired, pairRequest)
	if status != 200 {
		t.Fatal(status)
	}
	cookie := paired.Result().Cookies()[0]
	body, _ := json.Marshal(StartRequest{ChatID: "chat", Root: root, OwnerKey: "owner", CreateWhenMissing: true})
	request, _ := http.NewRequest("POST", server.URL+"/api/v1/terminals", strings.NewReader(string(body)))
	request.AddCookie(cookie)
	request.Header.Set("Origin", server.URL)
	request.Header.Set("X-Ally-CSRF", session.CSRF)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	var started StartResponse
	if err = json.NewDecoder(response.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	dialer := websocket.Dialer{Subprotocols: []string{"ally.terminal.v1"}}
	connection, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/terminals/"+started.SessionID+"/stream", http.Header{"Origin": []string{server.URL}, "Cookie": []string{cookie.String()}})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	var frame map[string]json.RawMessage
	if err = connection.ReadJSON(&frame); err != nil {
		t.Fatal(err)
	}
	if string(frame["event"]) != `"snapshot"` {
		t.Fatal("stream must begin with snapshot")
	}
	if err = connection.WriteJSON(terminalControl{Type: "write", Data: "printf 'HTTP_TERMINAL_%s\\n' READY\r"}); err != nil {
		t.Fatal(err)
	}
	found := false
	for !found {
		_, data, err := connection.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		found = strings.Contains(string(data), "HTTP_TERMINAL_READY")
	}
	pairRequest.AddCookie(cookie)
	manager.Logout(httptest.NewRecorder(), pairRequest)
	connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		_, _, err = connection.ReadMessage()
		if err != nil {
			break
		}
	}
	if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
		t.Fatal("logout did not close terminal stream")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err = runtime.Start(context.Background(), context.Background(), "web:"+session.ID, "owner", root, false)
		if err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("logout retained PTY owner")
		}
		time.Sleep(time.Millisecond)
	}
}
