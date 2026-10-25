package im

import (
	"context"
	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	protocol "github.com/JieWaZi/NeuxBot/server/internal/im"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
)

func TestResultProjectionSkipsDeletedChatWithoutChangingDeliveryHistory(t *testing.T) {
	service, db, _ := newIMFixture(t)
	attachConversationReaders(t, service, db)
	ctx := context.Background()
	chatID := protocol.StableID("chat", "deleted-result")
	if _, err := createAdmissionChat(ctx, service.config.Chats, service.config.DataDirectory, chatID, "waker"); err != nil {
		t.Fatal(err)
	}
	binding, err := service.config.Store.Binding(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	link := models.IMConversationLink{ID: "deleted-result-link", BindingID: binding.ID, ChatID: chatID, Configuration: binding, EventCursor: 17, PendingReportIDs: []string{"previous-report"}}
	if err := service.config.Store.SaveLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	delivery, err := service.CreateDelivery(ctx, DeliveryRequest{RequestID: "previous-result", TargetID: binding.ID, LinkID: link.ID, ChatID: chatID, Content: "已经确定的结果"})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err = service.config.Store.Delivery(ctx, delivery.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.config.Chats.DeleteChat(ctx, chatID); err != nil {
		t.Fatal(err)
	}
	for tick := 0; tick < 3; tick++ {
		if err := service.projectLink(ctx, link); err != nil {
			t.Errorf("第 %d 轮结果同步仍将已删除对话作为可重试错误: %v", tick+1, err)
		}
	}
	retained, err := service.config.Store.Link(ctx, link.ID)
	if err != nil || retained.ChatID != link.ChatID || retained.EventCursor != link.EventCursor || !reflect.DeepEqual(retained.PendingReportIDs, link.PendingReportIDs) {
		t.Fatalf("已删除会话的关联或投递游标被改写: %+v %v", retained, err)
	}
	retainedDelivery, err := service.config.Store.Delivery(ctx, delivery.ID)
	if err != nil || !reflect.DeepEqual(retainedDelivery, delivery) {
		t.Fatalf("原冻结投递事实被改写: %+v %v", retainedDelivery, err)
	}
	deliveries, total, err := service.config.Store.Deliveries(ctx, constants.IMDeliveryStateUnspecified, 20, 0)
	if err != nil || total != 1 || len(deliveries) != 1 {
		t.Fatalf("结果同步重复创建投递: %+v %d %v", deliveries, total, err)
	}
}

func TestResultProjectionDoesNotSkipAuthorityWhenQueryIndexIsMissing(t *testing.T) {
	service, db, _ := newIMFixture(t)
	attachConversationReaders(t, service, db)
	ctx := context.Background()
	chatID := protocol.StableID("chat", "existing-result-authority")
	if _, err := createAdmissionChat(ctx, service.config.Chats, service.config.DataDirectory, chatID, "waker"); err != nil {
		t.Fatal(err)
	}
	link := models.IMConversationLink{ID: "existing-result-link", BindingID: "b", ChatID: chatID}
	if err := service.config.Store.SaveLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", chatID).Delete(&models.Chat{}).Error; err != nil {
		t.Fatal(err)
	}
	// 事件来自权威日志，摘要来自查询投影；后者缺失仍需报告，不能静默跳过整条关联。
	if err := service.projectLink(ctx, link); !hasCode(err, status_error.ChatNotFound) {
		t.Fatalf("查询摘要缺失被忽略或返回了其他故障: %v", err)
	}
	retained, err := service.config.Store.Link(ctx, link.ID)
	if err != nil || retained.EventCursor == 0 || retained.ChatID != chatID {
		t.Fatalf("真实对话未继续同步权威事件: %+v %v", retained, err)
	}
}

// failingResultProjectionChats 在公开事件读取边界模拟明确的日志与数据库故障。
type failingResultProjectionChats struct {
	// Module 提供当前回归不应触及的其他会话端口。
	chat.Module
	// err 是本轮公开事件读取返回的稳定错误。
	err error
}

func (c failingResultProjectionChats) ListEvents(context.Context, string, int64, int) ([]chat.PublicEvent, error) {
	return nil, c.err
}

func TestResultProjectionPreservesReadFailures(t *testing.T) {
	for _, failure := range []status_error.StatusError{status_error.DatabaseError, status_error.ChatLogError, status_error.ChatLogIndexCorrupted} {
		t.Run(failure.Key(), func(t *testing.T) {
			service := New(Config{Chats: failingResultProjectionChats{err: failure}})
			if err := service.projectLink(context.Background(), models.IMConversationLink{ChatID: "existing-chat"}); !hasCode(err, failure) {
				t.Fatalf("真实读取故障被当作会话删除: %v", err)
			}
		})
	}
}
