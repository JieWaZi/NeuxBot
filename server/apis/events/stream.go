package events

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	eventhub "github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/shrewx/ginx"
)

const (
	// controlBufferSize 为恢复与确认保留少量发送预算，全部队列合计仍不超过256帧。
	controlBufferSize = 16
	// businessBufferBytes 在1MiB总预算内预留16KiB控制帧空间。
	businessBufferBytes = (1 << 20) - (16 << 10)
)

// Router 只装配固定地址的受限全局事件连接。
var Router = ginx.NewRouter(ginx.Group("/events"))

func init() { Router.Register(&StreamEvents{}) }

// StreamEvents 是全局事件升级入口，认证仍由根中间件拥有。
type StreamEvents struct {
	// MethodGet 声明该连接只能通过 GET 升级。
	ginx.MethodGet
}

func (*StreamEvents) Path() string { return "" }
func (*StreamEvents) Output(ctx *gin.Context) (interface{}, error) {
	if global.EventHub == nil {
		ctx.AbortWithStatus(http.StatusServiceUnavailable)
		return nil, nil
	}
	subject := middleware.AuthSubject(ctx)
	return nil, serve(ctx, global.EventHub, func(child context.Context, command control) (source, error) {
		if command.Topic == "workspace" {
			return newWorkspaceSource(child, subject, command)
		}
		return newSource(child, command)
	})
}

// source 拥有已经注册的原事件订阅及其取消边界。
type source struct {
	// run 从权威 backlog 衔接实时事件；成功发送不代表业务处理。
	run func(context.Context, func(frame, bool) bool)
	// close 幂等释放该资源订阅。
	close func()
}

// sourceFactory 只在格式及权限校验后建立一个逻辑资源订阅。
type sourceFactory func(context.Context, control) (source, error)

// queued 绑定发送帧与原订阅生命周期，退订后不发送旧积压。
type queued struct {
	// ctx 标识拥有该帧的订阅，取消后丢弃并要求原权威恢复。
	ctx context.Context
	// frame 保存不含连接交付序号的安全内容。
	frame frame
	// bytes 是已占用的有界发送预算。
	bytes int64
}

// session 拥有唯一 writer 和全部逻辑资源 relay。
type session struct {
	// ctx 是当前物理连接生命周期。
	ctx context.Context
	// cancel 结束读写与资源子订阅，不取消原业务执行。
	cancel context.CancelFunc
	// connection 是完成认证和来源校验的 WebSocket。
	connection *websocket.Conn
	// hub 提供 Core epoch 与退出信号。
	hub *eventhub.Hub
	// output 是最多256帧的业务发送队列。
	output chan queued
	// control 是专用恢复/确认队列，不与大载荷争用最后一个位置。
	control chan frame
	// space 唤醒等候发送预算的有限历史补齐。
	space chan struct{}
	// bytes 保证业务队列总载荷不超过1MiB。
	bytes atomic.Int64
	// wait 等待唯一 writer 及所有 relay 完整退出。
	wait sync.WaitGroup
	// subscriptions 只由读循环拥有，保存资源的取消句柄。
	subscriptions map[string]context.CancelFunc
	// factory 在原授权边界建立业务订阅。
	factory sourceFactory
}

func serve(ctx *gin.Context, hub *eventhub.Hub, factory sourceFactory) error {
	supported := false
	for _, protocol := range websocket.Subprotocols(ctx.Request) {
		if protocol == eventhub.Protocol {
			supported = true
		}
	}
	if !supported {
		ctx.AbortWithStatus(http.StatusBadRequest)
		return nil
	}
	upgrader := websocket.Upgrader{Subprotocols: []string{eventhub.Protocol}, CheckOrigin: trustedOrigin}
	connection, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		return nil
	}
	root, cancel := context.WithCancel(ctx.Request.Context())
	current := &session{ctx: root, cancel: cancel, connection: connection, hub: hub, output: make(chan queued, eventhub.BufferSize-controlBufferSize), control: make(chan frame, controlBufferSize), space: make(chan struct{}, 1), subscriptions: make(map[string]context.CancelFunc), factory: factory}
	connection.SetReadLimit(eventhub.MaximumControlBytes)
	current.wait.Add(1)
	go current.write()
	defer func() {
		cancel()
		_ = connection.Close()
		for _, stop := range current.subscriptions {
			stop()
		}
		current.wait.Wait()
	}()
	for {
		kind, data, err := connection.ReadMessage()
		if err != nil {
			return nil
		}
		if kind != websocket.TextMessage {
			current.reply(control{}, "error", map[string]any{"code": "INVALID_CONTROL"})
			continue
		}
		command, err := decodeControl(data)
		if err != nil {
			current.reply(command, "error", map[string]any{"requestId": command.RequestID, "code": "INVALID_CONTROL"})
			continue
		}
		current.handle(command)
	}
}
func (s *session) reply(command control, kind string, payload any) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		s.cancel()
		return
	}
	value := frame{Topic: command.Topic, Type: kind, Resource: command.Resource, EventID: util.New(), Payload: encoded}
	if !allowedTopics[value.Topic] || !validResourceShape(value.Topic, value.Resource) {
		value.Topic = "system"
		value.Resource = eventhub.Resource{Kind: "global"}
	}
	select {
	case s.control <- value:
	case <-s.ctx.Done():
	default:
		s.cancel()
		_ = s.connection.Close()
	}
}
func (s *session) handle(command control) {
	key := resourceKey(command.Topic, command.Resource)
	if command.Type == "unsubscribe" {
		if stop := s.subscriptions[key]; stop != nil {
			stop()
			delete(s.subscriptions, key)
		}
		s.reply(command, "unsubscribed", map[string]any{"requestId": command.RequestID, "subscriptionId": key})
		return
	}
	if stop := s.subscriptions[key]; stop != nil {
		if command.Type == "subscribe" {
			s.reply(command, "subscribed", map[string]any{"requestId": command.RequestID, "subscriptionId": key, "restored": true})
			return
		}
		stop()
		delete(s.subscriptions, key)
	}
	if len(s.subscriptions) >= eventhub.MaximumSubscriptions {
		s.reply(command, "error", map[string]any{"requestId": command.RequestID, "code": "SUBSCRIPTION_LIMIT"})
		return
	}
	child, cancel := context.WithCancel(s.ctx)
	stream, err := s.factory(child, command)
	if err != nil {
		canceled := child.Err() != nil
		cancel()
		if canceled || s.ctx.Err() != nil {
			return
		}
		code, retryable := subscriptionFailure(err)
		s.reply(command, "error", map[string]any{"requestId": command.RequestID, "code": code, "retryable": retryable})
		return
	}
	s.subscriptions[key] = cancel
	s.reply(command, "subscribed", map[string]any{"requestId": command.RequestID, "subscriptionId": key, "restored": true})
	s.wait.Add(1)
	go func() {
		defer s.wait.Done()
		defer cancel()
		defer stream.close()
		stream.run(child, func(value frame, backlog bool) bool { return s.enqueue(child, command, value, backlog) })
	}()
}
func (s *session) enqueue(ctx context.Context, command control, value frame, backlog bool) bool {
	value.Topic = command.Topic
	if value.Resource.Kind == "" {
		value.Resource = command.Resource
	}
	if value.EventID == "" {
		value.EventID = util.New()
	}
	if value.Type == "resync_required" {
		select {
		case s.control <- value:
			return true
		case <-s.ctx.Done():
			return false
		default:
			s.cancel()
			_ = s.connection.Close()
			return false
		}
	}
	size := int64(len(value.Payload) + 1024)
	if size > eventhub.MaximumFrameBytes {
		s.reply(command, "resync_required", map[string]string{"reason": "overflow"})
		return false
	}
	for {
		if ctx.Err() != nil {
			return false
		}
		reserved := s.bytes.Add(size)
		if reserved <= businessBufferBytes {
			select {
			case s.output <- queued{ctx: ctx, frame: value, bytes: size}:
				return true
			default:
			}
		}
		s.bytes.Add(-size)
		if !backlog {
			s.reply(command, "resync_required", map[string]string{"reason": "overflow"})
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-s.space:
		}
	}
}
func (s *session) write() {
	defer s.wait.Done()
	defer s.cancel()
	defer s.connection.Close()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	connectionID := util.New()
	sequence := uint64(0)
	send := func(value frame) bool {
		sequence++
		value.ProtocolVersion = 1
		value.Epoch = s.hub.Epoch()
		value.ConnectionID = connectionID
		value.DeliverySequence = sequence
		_ = s.connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return s.connection.WriteJSON(value) == nil
	}
	for {
		// 控制恢复优先于普通积压，只有实际发出的帧消耗交付序号。
		select {
		case value := <-s.control:
			if !send(value) {
				return
			}
			continue
		default:
		}
		select {
		case <-s.ctx.Done():
			return
		case <-s.hub.Done():
			return
		case value := <-s.control:
			if !send(value) {
				return
			}
		case <-heartbeat.C:
			if !send(frame{Topic: "system", Type: "heartbeat", Resource: eventhub.Resource{Kind: "global"}, EventID: util.New(), Payload: json.RawMessage(`{}`)}) {
				return
			}
		case value := <-s.output:
			s.bytes.Add(-value.bytes)
			select {
			case s.space <- struct{}{}:
			default:
			}
			if value.ctx.Err() == nil && !send(value.frame) {
				return
			}
		}
	}
}

// subscriptionFailure 只允许明确的暂时读取失败重试，取消与失效原身份需要用户或业务更新。
func subscriptionFailure(err error) (string, bool) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status_error.OperationCanceled.Key(), false
	}
	code := util.ErrorCode(err, status_error.InternalServerError.Key())
	for _, canceled := range []status_error.StatusError{status_error.DatabaseOperationCancelled, status_error.OperationCanceled, status_error.WorkflowCancelled} {
		if util.HasStatusCode(err, canceled) {
			return code, false
		}
	}
	for _, transient := range []status_error.StatusError{status_error.DatabaseBusy, status_error.DatabaseError, status_error.InternalServerError, status_error.ChatLogError, status_error.ChatServiceNotStarted, status_error.ChatEventStreamUnavailable, status_error.RuntimeEnvironmentServiceUnavailable, status_error.IMServiceUnavailable} {
		if util.HasStatusCode(err, transient) {
			return code, true
		}
	}
	return code, false
}
