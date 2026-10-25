package events

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	eventhub "github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func testServer(t *testing.T, hub *eventhub.Hub, factory sourceFactory) *httptest.Server {
	t.Helper()
	previous := global.Config.Security.LocalToken
	global.Config.Security.LocalToken = "test-secret"
	t.Cleanup(func() { global.Config.Security.LocalToken = previous })
	handler, err := (&middleware.LocalTokenAuth{}).Output(nil)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.Use(handler.(gin.HandlerFunc))
	engine.GET("/api/v1/events", func(ctx *gin.Context) { _ = serve(ctx, hub, factory) })
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	t.Cleanup(hub.Close)
	return server
}
func dial(t *testing.T, server *httptest.Server) *websocket.Conn {
	t.Helper()
	dialer := websocket.Dialer{Subprotocols: []string{eventhub.Protocol, "ally.auth.test-secret"}}
	connection, response, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.Header.Get("Sec-WebSocket-Protocol") != eventhub.Protocol {
		t.Fatal("服务端必须只回显协议，不能回显令牌")
	}
	t.Cleanup(func() { _ = connection.Close() })
	return connection
}
func read(t *testing.T, connection *websocket.Conn) *frame {
	t.Helper()
	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	var result frame
	if err := connection.ReadJSON(&result); err != nil {
		t.Fatal(err)
	}
	return &result
}
func command(t *testing.T, connection *websocket.Conn, kind, topic string, resource eventhub.Resource, after json.RawMessage) {
	t.Helper()
	if err := connection.WriteJSON(control{Type: kind, RequestID: kind, Topic: topic, Resource: resource, After: after}); err != nil {
		t.Fatal(err)
	}
}
func TestSingleWriterSubscriptionsResyncAndCoreShutdown(t *testing.T) {
	hub := eventhub.New()
	var created atomic.Int64
	var closed atomic.Int64
	factory := func(ctx context.Context, request control) (source, error) {
		created.Add(1)
		subscription := hub.Subscribe(request.Topic, request.Resource)
		return source{close: func() { subscription.Close(); closed.Add(1) }, run: func(ctx context.Context, emit func(frame, bool) bool) {
			initial, _ := encodedFrame("notifications.snapshot", request.Resource, map[string]any{"snapshot": map[string]int{"revision": int(created.Load())}})
			if !emit(initial, true) {
				return
			}
			for {
				select {
				case <-ctx.Done():
					return
				case event, open := <-subscription.Events:
					if !open {
						return
					}
					if !emit(fromHub(event), false) {
						return
					}
				}
			}
		}}, nil
	}
	server := testServer(t, hub, factory)
	connection := dial(t, server)
	resource := eventhub.Resource{Kind: "global"}
	command(t, connection, "subscribe", "notifications", resource, nil)
	first, second := read(t, connection), read(t, connection)
	if first.DeliverySequence != 1 || second.DeliverySequence != 2 || first.ConnectionID != second.ConnectionID || first.Epoch != hub.Epoch() {
		t.Fatalf("发送信封不连续：%+v %+v", first, second)
	}
	if first.Type != "subscribed" || second.Type != "notifications.snapshot" {
		t.Fatalf("初始确认/快照=%s/%s", first.Type, second.Type)
	}
	command(t, connection, "subscribe", "notifications", resource, nil)
	ack := read(t, connection)
	if ack.Type != "subscribed" || created.Load() != 1 {
		t.Fatal("重复订阅不应创建另一资源流")
	}
	if err := eventhub.Emit(hub, "notifications", "notifications.updated", resource, map[string]int{"revision": 2}); err != nil {
		t.Fatal(err)
	}
	update := read(t, connection)
	if update.Type != "notifications.updated" || update.DeliverySequence != 4 {
		t.Fatalf("更新=%+v", update)
	}
	command(t, connection, "resync", "notifications", resource, nil)
	read(t, connection)
	read(t, connection)
	if created.Load() != 2 {
		t.Fatal("恢复应重新注册并读取当前权威快照")
	}
	command(t, connection, "unsubscribe", "notifications", resource, nil)
	if read(t, connection).Type != "unsubscribed" {
		t.Fatal("缺少释放确认")
	}
	deadline := time.Now().Add(time.Second)
	for closed.Load() != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if closed.Load() != 2 {
		t.Fatal("退订未释放原监听")
	}
	hub.Close()
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := connection.ReadMessage(); err == nil {
		t.Fatal("Core退出必须关闭物理连接")
	}
}
func TestWebSocketTokenOriginAndControlValidation(t *testing.T) {
	hub := eventhub.New()
	var called atomic.Int64
	server := testServer(t, hub, func(context.Context, control) (source, error) { called.Add(1); return source{}, nil })
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/events"
	for _, origin := range []string{"http://evil.example", "http://localhost:1420", "ally://other"} {
		header := http.Header{"Origin": []string{origin}}
		dialer := websocket.Dialer{Subprotocols: []string{eventhub.Protocol, "ally.auth.test-secret"}}
		connection, response, err := dialer.Dial(endpoint, header)
		if connection != nil {
			connection.Close()
		}
		if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
			t.Fatalf("不可信Origin=%q未被拒绝", origin)
		}
	}
	wrong := websocket.Dialer{Subprotocols: []string{eventhub.Protocol, "ally.auth.wrong"}}
	_, response, err := wrong.Dial(endpoint, nil)
	if err == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatal("错误令牌未拒绝")
	}
	connection := dial(t, server)
	if err := connection.WriteJSON(map[string]any{"type": "run", "requestId": "invalid", "topic": "notifications", "resource": map[string]string{"kind": "global"}}); err != nil {
		t.Fatal(err)
	}
	if read(t, connection).Type != "error" || called.Load() != 0 {
		t.Fatal("未声明的业务控制不能进入来源工厂")
	}
}
func TestQueueOverflowRequiresAuthorityRecoveryAndKeepsControl(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	current := &session{ctx: ctx, cancel: cancel, output: make(chan queued, 1), control: make(chan frame, 2), space: make(chan struct{}, 1)}
	request := control{Topic: "chat", Resource: eventhub.Resource{Kind: "chat", ID: "chat"}}
	output := frame{Type: "chat.event", Payload: json.RawMessage(`{"sequence":1}`)}
	if !current.enqueue(ctx, request, output, false) {
		t.Fatal("首帧应进入有界队列")
	}
	if current.enqueue(ctx, request, output, false) {
		t.Fatal("积压超过上限不得假装发送成功")
	}
	recovery := <-current.control
	if recovery.Type != "resync_required" {
		t.Fatal("积压超限必须显式要求权威恢复")
	}
	if !current.enqueue(ctx, request, resyncFrame(request, "gap"), false) {
		t.Fatal("恢复帧必须能通过控制预算发送")
	}
}
func TestPublicChatPayloadAndOriginalCursors(t *testing.T) {
	event := chat.PublicEvent{SchemaVersion: 1, Sequence: 4, Type: constants.ChatEventTypeAssistantMessage, ExecutionID: "private-execution"}
	value, err := encodedFrame("chat.event", eventhub.Resource{Kind: "chat", ID: "chat"}, event)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(value.Payload), "private-execution") || strings.Contains(string(value.Payload), "executionId") {
		t.Fatal("必须复用公开投影，不能暴露内部执行身份")
	}
	value.SourceCursor = numericJSON(event.Sequence)
	if string(value.SourceCursor) != "4" {
		t.Fatal("Chat恢复序号不是数字")
	}
	value.SourceCursor = stringJSON("opaque-cursor")
	if string(value.SourceCursor) != `"opaque-cursor"` {
		t.Fatal("Team恢复游标必须保留不透明字符串")
	}
}
func TestControlRejectsResourceForgeryAndCursorCoercion(t *testing.T) {
	for _, raw := range []string{
		`{"type":"subscribe","requestId":"a","topic":"chat","resource":{"kind":"global"}}`,
		`{"type":"subscribe","requestId":"a","topic":"team","resource":{"kind":"thread","id":"thread"}}`,
		`{"type":"subscribe","requestId":"a","topic":"notifications","resource":{"kind":"global"},"url":"https://evil.example"}`,
	} {
		if _, err := decodeControl([]byte(raw)); err == nil {
			t.Fatalf("伪造控制被接受：%s", raw)
		}
	}
	if _, err := numberCursor(json.RawMessage(`"2"`)); err == nil {
		t.Fatal("数字游标不能接受字符串强制转换")
	}
	if _, err := stringCursor(json.RawMessage(`2`)); err == nil {
		t.Fatal("Team游标不能接受数字")
	}
}

// chatRequest 交付一次真实领域订阅的游标与可控安全事件源。
type chatRequest struct {
	// after 是服务收到的原 JSONL 游标。
	after int64
	// events 由测试作为唯一发送者控制后续公开事件。
	events chan chat.PublicEvent
}

// chatEventsFake 只替换公开订阅端口，其余业务入口不会由 WS 调用。
type chatEventsFake struct {
	// Module 保持完整聊天模块契约，不为传输测试实现无关业务。
	chat.Module
	// requests 记录并交付已建立的订阅，避免测试与 reader 共享可变切片。
	requests chan chatRequest
}

func (f *chatEventsFake) SubscribeEvents(_ context.Context, _ string, after int64) (chat.Subscription, error) {
	stream := make(chan chat.PublicEvent, 4)
	f.requests <- chatRequest{after: after, events: stream}
	backlog := []chat.PublicEvent{}
	if after < 5 {
		backlog = append(backlog, chat.PublicEvent{SchemaVersion: 1, Sequence: 5, Type: constants.ChatEventTypeAssistantMessage, ExecutionID: "private-execution"})
	}
	return chat.Subscription{Backlog: backlog, Events: stream, Close: func() {}}, nil
}
func TestChatSourceRestoresOriginalCursorAndTransientDoesNotAdvance(t *testing.T) {
	previous := global.ChatService
	fake := &chatEventsFake{requests: make(chan chatRequest, 4)}
	global.ChatService = fake
	t.Cleanup(func() { global.ChatService = previous })
	hub := eventhub.New()
	server := testServer(t, hub, chatSource)
	connection := dial(t, server)
	resource := eventhub.Resource{Kind: "chat", ID: "chat"}
	command(t, connection, "subscribe", "chat", resource, numericJSON(4))
	request := <-fake.requests
	if request.after != 4 {
		t.Fatal("服务未收到原领域数字游标")
	}
	read(t, connection)
	backlog := read(t, connection)
	if backlog.Type != "chat.event" || string(backlog.SourceCursor) != "5" || strings.Contains(string(backlog.Payload), "private-execution") {
		t.Fatalf("公开backlog或游标不符：%+v", backlog)
	}
	request.events <- chat.PublicEvent{SchemaVersion: 1, Transient: true, Type: constants.ChatEventTypeRuntimeEvent}
	transient := read(t, connection)
	if !transient.Transient || len(transient.SourceCursor) != 0 {
		t.Fatal("实时快照不能携带持久恢复游标")
	}
	request.events <- chat.PublicEvent{SchemaVersion: 1, Sequence: 6, Type: constants.ChatEventTypeExecutionCompleted}
	durable := read(t, connection)
	if string(durable.SourceCursor) != "6" {
		t.Fatal("最终checkpoint未交付原持久游标")
	}
	command(t, connection, "resync", "chat", resource, numericJSON(6))
	restored := <-fake.requests
	if restored.after != 6 {
		t.Fatal("恢复没有使用最新权威游标")
	}
	read(t, connection)
}

func TestStateSourceEstablishesListenerBeforeReadyAndKeepsFollowingChange(t *testing.T) {
	hub := eventhub.New()
	previous := global.EventHub
	global.EventHub = hub
	t.Cleanup(func() { hub.Close(); global.EventHub = previous })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := control{Topic: "entities", Resource: eventhub.Resource{Kind: "global"}}
	stream, err := stateSource(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.close()
	if err := eventhub.Emit(hub, "entities", "entities.changed", eventhub.Resource{Kind: "waker", ID: "new-partner"}, map[string]string{"id": "new-partner"}); err != nil {
		t.Fatal(err)
	}
	output := make(chan frame, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		stream.run(ctx, func(value frame, _ bool) bool { output <- value; return true })
	}()
	select {
	case ready := <-output:
		if ready.Type != "entities.ready" {
			t.Fatalf("missing authority-ready boundary: %+v", ready)
		}
	case <-time.After(time.Second):
		t.Fatal("ready frame missing")
	}
	select {
	case changed := <-output:
		if changed.Type != "entities.changed" || changed.Resource.ID != "new-partner" {
			t.Fatalf("change after registration lost: %+v", changed)
		}
	case <-time.After(time.Second):
		t.Fatal("queued submitted change missing")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("source did not release lifecycle")
	}
}

func TestResourceFailureCarriesStableCodeAndRetryability(t *testing.T) {
	for name, err := range map[string]error{
		"DatabaseBusy":               status_error.DatabaseBusy.WithField("retry", "bounded"),
		"DatabaseError":              status_error.DatabaseError,
		"ChatNotFound":               status_error.ChatNotFound,
		"BadRequest":                 status_error.BadRequest,
		"DatabaseOperationCancelled": status_error.DatabaseOperationCancelled,
		"AIAccountLoginFailed":       status_error.AIAccountLoginFailed,
	} {
		code, retryable := subscriptionFailure(err)
		if code != name {
			t.Fatalf("原稳定错误身份丢失：%s -> %s", name, code)
		}
		expected := name == "DatabaseBusy" || name == "DatabaseError"
		if retryable != expected {
			t.Fatalf("%s重试资格=%v，期望%v", name, retryable, expected)
		}
	}
	hub := eventhub.New()
	var attempts atomic.Int64
	server := testServer(t, hub, func(ctx context.Context, request control) (source, error) {
		if attempts.Add(1) == 1 {
			return source{}, status_error.DatabaseBusy
		}
		return source{close: func() {}, run: func(ctx context.Context, emit func(frame, bool) bool) {
			ready, _ := encodedFrame("entities.ready", request.Resource, map[string]int{"authorityRevision": 2})
			emit(ready, true)
			<-ctx.Done()
		}}, nil
	})
	connection := dial(t, server)
	resource := eventhub.Resource{Kind: "global"}
	command(t, connection, "subscribe", "entities", resource, nil)
	failure := read(t, connection)
	var payload map[string]any
	if err := json.Unmarshal(failure.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if failure.Type != "error" || payload["code"] != "DatabaseBusy" || payload["retryable"] != true {
		t.Fatalf("恢复资格未明确交付：%s", failure.Payload)
	}
	command(t, connection, "subscribe", "entities", resource, nil)
	ack, ready := read(t, connection), read(t, connection)
	if ack.Type != "subscribed" || ready.Type != "entities.ready" || ack.ConnectionID != failure.ConnectionID || ready.DeliverySequence != 3 {
		t.Fatal("恢复未在原物理连接内建立权威订阅")
	}
}
