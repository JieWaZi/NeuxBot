package controllers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"gorm.io/gorm"
)

const eventPublisherSetting = "ally:committed-event-publisher"

func eventsResource(kind, id string) events.Resource { return events.Resource{Kind: kind, ID: id} }

// WithEventPublisher 给这份数据库装配注入窄事件端口，不注册 ORM 广播回调。
func WithEventPublisher(database *gorm.DB, publisher events.Publisher) *gorm.DB {
	// Set 返回可复用 Statement 的实例；再建立会话，避免不同 Model 继承同一可变 Schema。
	return database.Set(eventPublisherSetting, publisher).Session(&gorm.Session{})
}

// publishCommittedChange 由拥有领域写入的方法在成功后明确登记语义变化。
func publishCommittedChange(ctx context.Context, database *gorm.DB, topic string, resource events.Resource) {
	value, exists := database.Get(eventPublisherSetting)
	if !exists {
		return
	}
	publisher, ok := value.(events.Publisher)
	if !ok {
		return
	}
	events.AfterCommit(ctx, func() {
		_ = events.Emit(publisher, topic, topic+".changed", resource, resource)
		if topic == "work-board" {
			_ = events.Emit(publisher, "entities", "entities.changed", events.Resource{Kind: "global"}, resource)
			_ = events.Emit(publisher, "automatic-tasks", "automatic-tasks.changed", events.Resource{Kind: "global"}, resource)
		}
	})
}

// publishCommittedNoticeIDs 仅登记真实新增/失效身份，不能由有限预览差集推算。
func publishCommittedNoticeIDs(ctx context.Context, database *gorm.DB, upserts, removals []string) {
	value, exists := database.Get(eventPublisherSetting)
	publisher, ok := value.(events.Publisher)
	if !exists || !ok || len(upserts)+len(removals) == 0 {
		return
	}
	upserts = append([]string{}, upserts...)
	removals = append([]string{}, removals...)
	events.AfterCommit(ctx, func() {
		_ = events.Emit(publisher, "notification-state", "changed", events.Resource{Kind: "global"}, map[string]any{"noticeIds": upserts, "removedIds": removals})
	})
}
