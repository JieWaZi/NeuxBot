package notifications

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/notification"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestNotificationAPIConfirmExactResultAndKeepPendingRequest(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "notification-api.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	hub := events.New()
	db := controllers.WithEventPublisher(database.DB, hub)
	previousFacts, previousNotices, previousService := global.NotificationControllerManager, global.AttentionNoticeControllerManager, global.NotificationService
	results := controllers.NewWorkResultController(db)
	global.NotificationControllerManager = controllers.NewNotificationController(db)
	global.AttentionNoticeControllerManager = controllers.NewAttentionNoticeController(db)
	global.NotificationService = notification.New(global.NotificationControllerManager, results, controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db)), hub)
	t.Cleanup(func() {
		global.NotificationService.Close()
		hub.Close()
		global.NotificationControllerManager, global.AttentionNoticeControllerManager, global.NotificationService = previousFacts, previousNotices, previousService
	})
	ctx := context.Background()
	wakerID, chatID, approvalID, inputID := util.New(), util.New(), util.New(), util.New()
	if err := db.Create(&models.Waker{ID: wakerID, Name: "通知伙伴"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Chat{ID: chatID, WakerID: wakerID, Title: "成果", ActiveExecutionID: util.New(), WaitingApproval: true, PendingApprovals: []models.PendingApproval{{ApprovalID: approvalID, Title: "待审批", RequestedAt: 5}}, WaitingElicitation: true, PendingElicitations: []models.PendingElicitation{{ElicitationID: inputID, Message: "待回答", RequestedAt: 6}}}).Error; err != nil {
		t.Fatal(err)
	}
	if err := results.EnableTracking(ctx, 1); err != nil {
		t.Fatal(err)
	}
	reference := models.WorkResultReference{ChatID: chatID, ExecutionID: util.New(), MessageID: util.New()}
	if err := results.Publish(ctx, models.WorkResultReceipt{OwnerKind: constants.WorkResultOwnerKindChat, OwnerID: chatID, LatestVersion: 1, AvailableAt: 10, Reference: reference}); err != nil {
		t.Fatal(err)
	}
	if err := global.NotificationService.Start(ctx); err != nil {
		t.Fatal(err)
	}
	callContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	callContext.Request = httptest.NewRequest("POST", "/", nil)
	if _, err := (&DismissNotification{NoticeID: "approval:" + approvalID}).Output(callContext); err != nil {
		t.Fatal(err)
	}
	facts, err := global.NotificationControllerManager.Snapshot(ctx)
	if err != nil || facts.ActionCount != 2 || facts.UnreadResultCount != 1 || !facts.ActionPreview[0].Dismissed {
		t.Fatalf("收起不能清未读/待办: %+v %v", facts, err)
	}
	id := "result:chat:" + chatID + ":1"
	item, err := global.NotificationControllerManager.Get(ctx, id)
	if err != nil || item.Target.Reference == nil || *item.Target.Reference != reference {
		t.Fatalf("通知丢失完整原引用: %+v %v", item, err)
	}
	wrong := reference
	wrong.ExecutionID = util.New()
	wrongTarget := item.Target
	wrongTarget.Reference = &wrong
	if _, err := (&ViewNotification{NoticeID: id, Body: ViewNotificationRequest{Version: 1, Target: &wrongTarget}}).Output(callContext); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("错误执行引用被接受: %v", err)
	}
	if _, err := (&ViewNotification{NoticeID: id, Body: ViewNotificationRequest{Version: 1, Target: &item.Target}}).Output(callContext); err != nil {
		t.Fatal(err)
	}
	facts, err = global.NotificationControllerManager.Snapshot(ctx)
	if err != nil || facts.ActionCount != 2 || facts.UnreadResultCount != 0 {
		t.Fatalf("阅读不应处理待办: %+v %v", facts, err)
	}
	for _, id := range []string{"approval:" + approvalID, "elicitation:" + inputID} {
		if _, err := (&ViewNotification{NoticeID: id, Body: ViewNotificationRequest{Version: 1}}).Output(callContext); !errors.Is(err, status_error.BadRequest) {
			t.Fatalf("待办被当成果阅读: id=%s err=%v", id, err)
		}
	}
	facts, err = global.NotificationControllerManager.Snapshot(ctx)
	if err != nil || facts.ActionCount != 2 {
		t.Fatalf("阅读后丢失待办: %+v err=%v", facts, err)
	}
}
