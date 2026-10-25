// Package im 拥有平台连接、耐久输入路由及结果交付的应用生命周期。
package im

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	protocol "github.com/JieWaZi/NeuxBot/server/internal/im"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/channel"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// Config 提供 IM 的稳定依赖，实际执行仍由现有内核拥有。
type Config struct {
	// Store 持久保存连接、授权、输入及投递。
	Store controllers.IMController
	// Transactions 提供平台批次和路由提交的原子边界。
	Transactions controllers.TransactionController
	// Executions 接纳普通伙伴工作并处理现有工具交互。
	Executions execution.Service
	// Chats 读取权威消息和执行事实。
	Chats chat.Module
	// Teams 接纳既有团队的正式工作。
	Teams channel.Service
	// Channels 读取团队与成员默认配置。
	Channels controllers.ChannelController
	// Threads 读取首发及负责人队列的实际关系。
	Threads controllers.ChannelThreadController
	// TeamTasks 读取成员报告已直达或已被负责人消费的权威判定。
	TeamTasks controllers.ChannelTaskController
	// DataDirectory 是 IM 输入和输出受管暂存位置。
	DataDirectory string
	// Platforms 可替换各平台真实传输边界，用于不联网的验收。
	Platforms map[constants.IMPlatform]protocol.Platform
	// Events 在内存扫码进度生效后交付安全事件，不替代平台认证或连接事实。
	Events *events.Hub
	// Now 是可替换时钟，缺省使用系统时间。
	Now func() time.Time
}

// connectionWorker 保存一份平台连接的可取消生命周期。
type connectionWorker struct {
	// revision 是启动时冻结的配置版本。
	revision int64
	// cancel 终止对应平台接收流程。
	cancel context.CancelFunc
	// done 在 SDK 和回调完全退出后关闭。
	done chan struct{}
}

// Service 共用平台输入和主动交付机制，不建立第二份 Agent 执行器。
type Service struct {
	// config 是构造时固定的业务及传输依赖。
	config Config
	// gate 串行保护聊天指针及冻结路由，只覆盖短数据库操作。
	gate sync.Mutex
	// deliveryGate 保护冻结文件与唯一投递提交，避免同请求并发改变文件。
	deliveryGate sync.Mutex
	// life 保护 Start、Close 和后台登记。
	life sync.Mutex
	// ctx 是 Core 取消时一同终止的子上下文。
	ctx context.Context
	// cancel 终止全部连接、登录与投递任务。
	cancel context.CancelFunc
	// workers 等待所有已登记 goroutine 结束。
	workers sync.WaitGroup
	// connections 保存当前真实收取任务。
	connections map[string]*connectionWorker
	// dispatching 标记正在向某个会话接纳输入，避免同会话乱序。
	dispatching map[string]bool
	// dispatchConnections 记录各连接正在接纳的输入数量，删除时保护已开始的接纳。
	dispatchConnections map[string]int
	// delivering 标记当前已领取的投递，避免每轮重复启动。
	delivering map[string]bool
	// logins 保存未完成的扫码流程，凭据不进入公开进度。
	logins map[string]*loginSession
	// suspended 表示系统已报告休眠，唤醒时重新建立实际平台连接。
	suspended bool
}

// New 构建 IM 生命周期，构造过程不启动后台任务。
func New(config Config) *Service {
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Platforms == nil {
		config.Platforms = map[constants.IMPlatform]protocol.Platform{constants.IMPlatformWeixin: &protocol.Weixin{}, constants.IMPlatformFeishu: &protocol.Feishu{}, constants.IMPlatformWecom: &protocol.Wecom{}}
	}
	return &Service{config: config, connections: map[string]*connectionWorker{}, dispatching: map[string]bool{}, dispatchConnections: map[string]int{}, delivering: map[string]bool{}, logins: map[string]*loginSession{}}
}

// Start 恢复独立投递状态后启动耐久收件、路由及连接维护。
func (s *Service) Start(ctx context.Context) error {
	s.life.Lock()
	defer s.life.Unlock()
	if s.cancel != nil {
		return nil
	}
	if err := s.config.Store.RecoverDeliveries(ctx); err != nil {
		return err
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			s.tick()
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return nil
}

// Close 取消平台连接、登录及后台处理并等待真实资源退出。
func (s *Service) Close() {
	s.life.Lock()
	if s.cancel == nil {
		s.life.Unlock()
		return
	}
	cancel := s.cancel
	cancel()
	s.life.Unlock()
	s.workers.Wait()
	s.life.Lock()
	s.cancel = nil
	s.connections = map[string]*connectionWorker{}
	s.life.Unlock()
}

// SetSuspended 同步 OS 休眠并关闭原连接，恢复只重连和处理真实积压。
func (s *Service) SetSuspended(value bool) {
	s.life.Lock()
	defer s.life.Unlock()
	s.suspended = value
	if value {
		for _, worker := range s.connections {
			worker.cancel()
		}
	}
}

// KeepAwake 返回启用连接实际请求的系统运行策略。
func (s *Service) KeepAwake(ctx context.Context) (bool, error) {
	connections, err := s.config.Store.Connections(ctx)
	if err != nil {
		return false, err
	}
	for _, connection := range connections {
		if connection.Enabled && connection.KeepAwake {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) tick() {
	if s.ctx.Err() != nil {
		return
	}
	s.maintainConnections()
	s.life.Lock()
	suspended := s.suspended
	s.life.Unlock()
	if suspended {
		return
	}
	for _, state := range []constants.IMInboxState{constants.IMInboxStatePending, constants.IMInboxStateRouted} {
		inputs, err := s.config.Store.Inbox(s.ctx, state, constants.IMPageSize)
		if err != nil {
			continue
		}
		seen := map[string]bool{}
		for _, input := range inputs {
			if s.ctx.Err() != nil {
				return
			}
			if state == constants.IMInboxStatePending {
				if err := s.route(s.ctx, input); err != nil {
					logx.Errorf("im route failed: input_id=%s error=%v", input.ID, err)
				}
			} else {
				command, _ := parseCommand(input.Content)
				if command == "" && seen[input.LinkID] {
					continue
				}
				if command == "" {
					seen[input.LinkID] = true
				}
				s.dispatch(input)
			}
		}
	}
	s.projectResults()
	deliveries, _, err := s.config.Store.Deliveries(s.ctx, constants.IMDeliveryStatePending, constants.IMPageSize, 0)
	if err != nil {
		return
	}
	seenTargets := map[string]bool{}
	for _, delivery := range deliveries {
		if seenTargets[delivery.BindingID] {
			continue
		}
		seenTargets[delivery.BindingID] = true
		s.startDelivery(delivery)
	}
}

func (s *Service) maintainConnections() {
	configurations, err := s.config.Store.Connections(s.ctx)
	if err != nil {
		return
	}
	s.life.Lock()
	defer s.life.Unlock()
	for _, configuration := range configurations {
		existing := s.connections[configuration.ID]
		if existing != nil {
			if !configuration.Enabled || s.suspended || existing.revision != configuration.Revision {
				existing.cancel()
			}
			select {
			case <-existing.done:
				delete(s.connections, configuration.ID)
				existing = nil
			default:
				continue
			}
		}
		if !configuration.Enabled || s.suspended || s.ctx.Err() != nil {
			continue
		}
		// 登录失效及被替换的企微连接保持人工恢复状态，避免反复抢占机器人。
		if configuration.State == constants.IMConnectionStateLoginRequired || configuration.ErrorCode == status_error.IMConnectionReplaced.Key() {
			continue
		}
		ctx, cancel := context.WithCancel(s.ctx)
		worker := &connectionWorker{revision: configuration.Revision, cancel: cancel, done: make(chan struct{})}
		s.connections[configuration.ID] = worker
		s.workers.Add(1)
		go func() {
			defer s.workers.Done()
			defer close(worker.done)
			defer cancel()
			s.runConnection(ctx, configuration)
		}()
	}
}

func (s *Service) runConnection(ctx context.Context, configuration models.IMConnection) {
	connection, err := s.config.Store.Connection(ctx, configuration.ID)
	if err != nil {
		return
	}
	platform := s.config.Platforms[connection.Platform]
	if platform == nil {
		return
	}
	state := func(value constants.IMConnectionState, code string) {
		if ctx.Err() == nil {
			if err := s.config.Store.ConnectionState(ctx, connection.ID, value, code); err != nil {
				logx.Errorf("im state commit failed: connection_id=%s error=%v", connection.ID, err)
			}
		}
	}
	state(constants.IMConnectionStateConnecting, "")
	err = platform.Run(ctx, connection, func(ctx context.Context, messages []protocol.Message, cursor string) error {
		return s.Receive(ctx, connection, messages, cursor)
	}, state)
	if ctx.Err() != nil {
		return
	}
	code := errorCode(err)
	if hasCode(err, status_error.IMLoginRequired) {
		state(constants.IMConnectionStateLoginRequired, code)
	} else {
		state(constants.IMConnectionStateFailed, code)
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// Receive 在执行接纳前原子提交平台输入；飞书 ACK 和微信游标推进在提交之后。
func (s *Service) Receive(ctx context.Context, connection models.IMConnection, messages []protocol.Message, cursor string) error {
	return s.config.Transactions.Transaction(ctx, func(ctx context.Context) error {
		current, err := s.config.Store.Connection(ctx, connection.ID)
		if err != nil {
			return err
		}
		if !current.Enabled || current.Revision != connection.Revision {
			return status_error.IMConfigurationConflict
		}
		for _, message := range messages {
			if message.ID == "" || message.ChatID == "" || message.SenderID == "" {
				return status_error.IMProtocolInvalid
			}
			bindingID := protocol.StableID("binding", connection.ID, message.ChatID)
			name := message.ChatName
			if name == "" {
				name = message.ChatID
			}
			binding := models.IMBinding{ID: bindingID, ConnectionID: connection.ID, ExternalChatID: message.ChatID, Kind: message.Kind, Name: name, State: constants.IMBindingStatePending, Revision: 1, ContextToken: message.ContextToken, ControllerIDs: []string{connection.OwnerID}}
			if connection.OwnerID == "" {
				binding.ControllerIDs = []string{message.SenderID}
			}
			if err := s.config.Store.DiscoverBinding(ctx, binding); err != nil {
				return err
			}
			sentAt := message.SentAt
			if sentAt == 0 {
				sentAt = s.config.Now().UnixMilli()
			}
			input := models.IMInbox{ID: protocol.StableID("input", connection.ID, message.ID), ConnectionID: connection.ID, BindingID: bindingID, ExternalMessageID: message.ID, ExternalChatID: message.ChatID, SenderID: message.SenderID, QuoteMessageID: message.QuoteID, Content: message.Content, Files: message.Files, State: constants.IMInboxStatePending, ReceivedAt: sentAt}
			if err := s.config.Store.SaveInbox(ctx, input); err != nil {
				return err
			}
		}
		if cursor != "" {
			return s.config.Store.SaveCursor(ctx, connection.ID, cursor)
		}
		return nil
	})
}

// WithBinding 串行保护桌面编辑与 IM 命令共同修改的聊天指针。
func (s *Service) WithBinding(ctx context.Context, id string, change func(context.Context, models.IMBinding) error) error {
	s.gate.Lock()
	defer s.gate.Unlock()
	return s.config.Transactions.Transaction(ctx, func(ctx context.Context) error {
		binding, err := s.config.Store.Binding(ctx, id)
		if err != nil {
			return err
		}
		return change(ctx, binding)
	})
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	var stable statuserror.CommonError
	if errors.As(err, &stable) {
		return stable.Key()
	}
	return "IMServiceUnavailable"
}
func hasCode(err error, expected status_error.StatusError) bool {
	if errors.Is(err, expected) {
		return true
	}
	var stable statuserror.CommonError
	return errors.As(err, &stable) && stable.Code() == expected.Code()
}
func partIdentity(deliveryID string, index int) string {
	return protocol.StableID("part", deliveryID, strconv.Itoa(index))
}

// WithConnectionRemoval 保护删除事务与路由、接纳、发送和认证的生命周期边界。
func (s *Service) WithConnectionRemoval(ctx context.Context, id string, change func(context.Context) error) error {
	s.gate.Lock()
	defer s.gate.Unlock()
	s.deliveryGate.Lock()
	defer s.deliveryGate.Unlock()
	s.life.Lock()
	defer s.life.Unlock()
	if s.dispatchConnections[id] > 0 {
		return status_error.IMConnectionInUse
	}
	bindings, _, err := s.config.Store.Bindings(ctx, id, 0, 0)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if s.delivering["target:"+binding.ID] {
			return status_error.IMConnectionInUse
		}
	}
	if err := s.config.Transactions.Transaction(ctx, change); err != nil {
		return err
	}
	if worker := s.connections[id]; worker != nil {
		worker.cancel()
		delete(s.connections, id)
	}
	for _, session := range s.logins {
		if session.progress.ConnectionID == id {
			session.cancel()
			session.progress.Status = "cancelled"
		}
	}
	return nil
}
