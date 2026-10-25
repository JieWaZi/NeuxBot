package terminals

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/apis/events"
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/shrewx/ginx"
	"net/http"
	"time"
)

// 连接终端字节流
type StreamTerminal struct {
	// ginx.MethodGet 声明接口方法。
	ginx.MethodGet
	// 会话标识
	SessionID string `in:"path" name:"sessionId" validate:"required,max=128"`
}

// terminalControl 只接受当前 WS 附件的输入或尺寸。
type terminalControl struct {
	// Type 是 write 或 resize。
	Type string `json:"type"`
	// Data 是 UTF-8 终端输入。
	Data string `json:"data"`
	// Cols 是可见列数。
	Cols int `json:"cols"`
	// Rows 是可见行数。
	Rows int `json:"rows"`
}

func (*StreamTerminal) Path() string { return "/:sessionId/stream" }
func (op *StreamTerminal) Output(ctx *gin.Context) (interface{}, error) {
	if !global.TerminalRuntime.Available() {
		ctx.AbortWithStatus(http.StatusServiceUnavailable)
		return nil, nil
	}
	supported := false
	for _, protocol := range websocket.Subprotocols(ctx.Request) {
		if protocol == "ally.terminal.v1" {
			supported = true
		}
	}
	if !supported || !events.TrustedOrigin(ctx.Request) {
		ctx.AbortWithStatus(http.StatusForbidden)
		return nil, nil
	}
	subject := middleware.AuthSubject(ctx)
	snapshot, attachment, err := global.TerminalRuntime.Attach(ctx.Request.Context(), subject, op.SessionID)
	if err != nil {
		ctx.AbortWithStatus(http.StatusNotFound)
		return nil, nil
	}
	defer global.TerminalRuntime.Detach(subject, op.SessionID, attachment)
	upgrader := websocket.Upgrader{Subprotocols: []string{"ally.terminal.v1"}, CheckOrigin: events.TrustedOrigin}
	connection, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		return nil, nil
	}
	defer connection.Close()
	connection.SetReadLimit(128 << 10)
	connection.SetReadDeadline(time.Now().Add(45 * time.Second))
	connection.SetPongHandler(func(string) error { return connection.SetReadDeadline(time.Now().Add(45 * time.Second)) })
	// 唯一 writer 先交付屏幕，之后交付序号更大的有界输出。
	streamContext, cancel := context.WithCancel(attachment.Context)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer connection.Close()
		connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if connection.WriteJSON(map[string]any{"event": "snapshot", "payload": snapshot}) != nil {
			return
		}
		heartbeat := time.NewTicker(15 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case <-streamContext.Done():
				return
			case data := <-attachment.Events:
				connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
				err := connection.WriteMessage(websocket.TextMessage, data)
				attachment.Consumed(len(data))
				if err != nil {
					return
				}
			case <-heartbeat.C:
				if connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
					return
				}
			}
		}
	}()
	defer func() { cancel(); connection.Close(); <-done }()
	for {
		var control terminalControl
		if connection.ReadJSON(&control) != nil {
			return nil, nil
		}
		if global.TerminalRuntime.Control(ctx.Request.Context(), subject, op.SessionID, attachment, control.Type, control.Data, control.Cols, control.Rows) != nil {
			return nil, nil
		}
	}
}
