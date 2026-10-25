// Package notification 从原领域事实建立应用通知摘要，不拥有第二套工作历史。
package notification

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Snapshot 是 REST 与 WS 共同读取的带版本通知摘要。
type Snapshot struct {
	// 生命周期
	Epoch string `json:"epoch"`
	// 摘要已同步
	Synced bool `json:"synced"`
	// 投影版本
	Revision int64 `json:"revision"`
	// 待处理数
	ActionCount int64 `json:"actionCount"`
	// 未读成果数
	UnreadResultCount int64 `json:"unreadResultCount"`
	// 未读失败数
	UnreadFailureCount int64 `json:"unreadFailureCount"`
	// 总关注数
	TotalCount int64 `json:"totalCount"`
	// 待处理预览
	ActionPreview []models.NotificationItem `json:"actionPreview"`
	// 待查看预览
	UnreadPreview []models.NotificationItem `json:"unreadPreview"`
	// 生成时间
	GeneratedAt int64 `json:"generatedAt"`
}

// Update 是同一版本的准确摘要和真实变化身份。
type Update struct {
	// 通知摘要
	Snapshot Snapshot `json:"snapshot"`
	// 更新通知
	Upserts []models.NotificationItem `json:"upserts"`
	// 移除身份
	Removals []string `json:"removals"`
	// 影响范围
	AffectedResources []events.Resource `json:"affectedResources"`
}

// Service 串行重建可恢复的有界通知摘要。
type Service struct {
	// facts 读取原请求、成果与自动失败组合。
	facts controllers.NotificationController
	// results 写入原成果确认，不建立第二套回执。
	results controllers.WorkResultController
	// transactions 使成果与无成果失败清空共用短事务。
	transactions controllers.TransactionController
	// hub 接收已提交变化并输出通知更新。
	hub *events.Hub
	// mutex 保护快照与读取状态。
	mutex sync.RWMutex
	// snapshot 是 REST 和 WS 共用的当前摘要。
	snapshot Snapshot
	// ready 表示至少读取过一份合法权威快照。
	ready bool
	// cancel 取消后台聚合生命周期。
	cancel context.CancelFunc
	// wait 等待聚合流程释放定时器和订阅。
	wait sync.WaitGroup
}

// New 仅装配复用端口，不启动后台协程。
func New(facts controllers.NotificationController, results controllers.WorkResultController, transactions controllers.TransactionController, hub *events.Hub) *Service {
	return &Service{facts: facts, results: results, transactions: transactions, hub: hub}
}

// Start 先登记提交监听，再读取初始摘要，初始化期间变化保留在队列中。
func (s *Service) Start(ctx context.Context) error {
	subscription := s.hub.Subscribe("", events.Resource{Kind: "global"})
	facts, err := s.facts.Snapshot(ctx)
	if err != nil {
		subscription.Close()
		return err
	}
	s.replace(facts)
	ctx, s.cancel = context.WithCancel(ctx)
	s.wait.Add(1)
	go func() { defer s.wait.Done(); s.run(ctx, subscription, facts.NextDeadline) }()
	return nil
}

// Snapshot 只复制带版本缓存，不因消费者或心跳查询数据库。
func (s *Service) Snapshot() (Snapshot, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if !s.ready {
		return Snapshot{}, status_error.InternalServerError
	}
	value := s.snapshot
	value.ActionPreview = append([]models.NotificationItem{}, value.ActionPreview...)
	value.UnreadPreview = append([]models.NotificationItem{}, value.UnreadPreview...)
	return value, nil
}

func (s *Service) replace(facts models.NotificationFacts) Snapshot {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.snapshot = Snapshot{Epoch: s.hub.Epoch(), Synced: true, Revision: s.snapshot.Revision + 1, ActionCount: facts.ActionCount, UnreadResultCount: facts.UnreadResultCount, UnreadFailureCount: facts.UnreadFailureCount, TotalCount: facts.ActionCount + facts.UnreadResultCount + facts.UnreadFailureCount, ActionPreview: facts.ActionPreview, UnreadPreview: facts.UnreadPreview, GeneratedAt: time.Now().UnixMilli()}
	s.ready = true
	return s.snapshot
}

// markUnsynced 保留准确观察过的计数，并告知消费者当前无法完成权威对账。
func (s *Service) markUnsynced() {
	s.mutex.Lock()
	if !s.ready || !s.snapshot.Synced {
		s.mutex.Unlock()
		return
	}
	s.snapshot.Synced = false
	s.snapshot.Revision++
	value := s.snapshot
	s.mutex.Unlock()
	_ = events.Emit(s.hub, "notifications", "notifications.updated", events.Resource{Kind: "global"}, Update{Snapshot: value, Upserts: []models.NotificationItem{}, Removals: []string{}, AffectedResources: []events.Resource{{Kind: "global"}}})
}

// run 由提交或精确期限唤醒，禁止固定周期扫描全部通知。
func (s *Service) run(ctx context.Context, subscription events.Subscription, deadline int64) {
	defer func() { subscription.Close() }()
	var refresh *time.Timer
	var refreshC <-chan time.Time
	var expiration *time.Timer
	var expirationC <-chan time.Time
	resetDeadline := func(at int64) {
		if expiration != nil {
			expiration.Stop()
		}
		expirationC = nil
		if at > 0 {
			expiration = time.NewTimer(max(time.Until(time.UnixMilli(at)), time.Millisecond))
			expirationC = expiration.C
		}
	}
	resetDeadline(deadline)
	defer func() {
		if refresh != nil {
			refresh.Stop()
		}
		if expiration != nil {
			expiration.Stop()
		}
	}()
	dirty := map[events.Resource]bool{}
	newIDs := map[string]bool{}
	removedIDs := map[string]bool{}
	retryDelay := time.Second
	retry := func() {
		s.markUnsynced()
		refresh = time.NewTimer(retryDelay)
		refreshC = refresh.C
		retryDelay = min(retryDelay*2, 30*time.Second)
	}
	schedule := func() {
		if refreshC == nil {
			refresh = time.NewTimer(250 * time.Millisecond)
			refreshC = refresh.C
		}
	}
refreshLoop:
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.hub.Done():
			return
		case event, open := <-subscription.Events:
			if !open {
				select {
				case <-s.hub.Done():
					return
				default:
				}
				subscription.Close()
				subscription = s.hub.Subscribe("", events.Resource{Kind: "global"})
				dirty[events.Resource{Kind: "global"}] = true
				schedule()
				continue
			}
			switch event.Topic {
			case "work-board", "work-results", "entities", "automatic-tasks", "notification-state":
				dirty[event.Resource] = true
				if event.Topic == "notification-state" {
					var identities struct {
						// NoticeIDs 是生产者提交后确认新增的稳定身份。
						NoticeIDs []string `json:"noticeIds"`
						// RemovedIDs 是原请求解决或版本确认后的真实失效身份。
						RemovedIDs []string `json:"removedIds"`
					}
					if json.Unmarshal(event.Payload, &identities) == nil {
						for _, id := range identities.NoticeIDs {
							newIDs[id] = true
							delete(removedIDs, id)
						}
						for _, id := range identities.RemovedIDs {
							removedIDs[id] = true
							delete(newIDs, id)
						}
					}
				}
				schedule()
			}
		case <-expirationC:
			expirationC = nil
			dirty[events.Resource{Kind: "global"}] = true
			schedule()
		case <-refreshC:
			refreshC = nil
			facts, err := s.facts.Snapshot(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logx.Errorf("refresh notification snapshot failed: error=%v", err)
				retry()
				continue
			}
			update := Update{Upserts: []models.NotificationItem{}, Removals: []string{}, AffectedResources: []events.Resource{}}
			// 大批次仍推准确摘要与范围，不发送无界正文，也不把预览换位误作新通知。
			if len(newIDs) <= 50 {
				for id := range newIDs {
					item, readErr := s.facts.Get(ctx, id)
					if readErr == nil {
						update.Upserts = append(update.Upserts, item)
					} else if !errors.Is(readErr, status_error.BadRequest) {
						if ctx.Err() != nil {
							return
						}
						// 故障保留真实变化身份，恢复前不能丢掉待提示的新通知。
						retry()
						continue refreshLoop
					}
				}
			}
			if len(removedIDs) <= 100 {
				for id := range removedIDs {
					update.Removals = append(update.Removals, id)
				}
			}
			retryDelay = time.Second
			current := s.replace(facts)
			update.Snapshot = current
			for resource := range dirty {
				update.AffectedResources = append(update.AffectedResources, resource)
			}
			if len(update.AffectedResources) > events.MaximumSubscriptions {
				update.AffectedResources = []events.Resource{{Kind: "global"}}
			}
			if encoded, encodeErr := json.Marshal(update); encodeErr != nil || len(encoded) > events.MaximumFrameBytes-1024 {
				update.Upserts = []models.NotificationItem{}
				update.Removals = []string{}
				update.AffectedResources = []events.Resource{{Kind: "global"}}
			}
			clear(dirty)
			clear(newIDs)
			clear(removedIDs)
			_ = events.Emit(s.hub, "notifications", "notifications.updated", events.Resource{Kind: "global"}, update)
			resetDeadline(facts.NextDeadline)
		}
	}
}

// ViewResult 供原成果入口和统一通知入口共同确认准确观察版本。
func (s *Service) ViewResult(ctx context.Context, kind constants.WorkResultOwnerKind, ownerID string, version int64, reference models.WorkResultReference) error {
	if version < 1 || (kind != constants.WorkResultOwnerKindChat && kind != constants.WorkResultOwnerKindTeamThread && kind != constants.WorkResultOwnerKindWorkflowRun) {
		return status_error.BadRequest
	}
	receipt, found, err := s.results.Get(ctx, kind, ownerID)
	if err != nil {
		return err
	}
	if !found || version > receipt.LatestVersion {
		return status_error.BadRequest
	}
	// 当前引用已更新时忽略过期观察，不能用旧引用确认新的交付。
	if version < receipt.LatestVersion {
		return nil
	}
	if reference != receipt.Reference {
		return status_error.BadRequest
	}
	ok, err := s.results.MarkViewed(ctx, kind, ownerID, version, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	if !ok {
		return status_error.BadRequest
	}
	return nil
}

// ClearUnread 仅确认本次短事务中的待查看，保留真实请求和新到达版本。
func (s *Service) ClearUnread(ctx context.Context) error {
	return s.transactions.Transaction(ctx, func(ctx context.Context) error {
		at := time.Now().UnixMilli()
		if err := s.results.ClearUnread(ctx, at); err != nil {
			return err
		}
		return s.facts.ClearFailures(ctx, at)
	})
}

// Close 停止聚合，不删除原请求、成果或用户回执。
func (s *Service) Close() {
	if s.cancel != nil {
		s.cancel()
		s.wait.Wait()
	}
}
