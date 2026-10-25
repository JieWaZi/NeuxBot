package channel

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

// deletionChatFake 记录团队删除请求覆盖的 Chat 集合。
type deletionChatFake struct {
	chatservice.Service
	// calls 保存每次批量删除收到的 Chat 标识。
	calls [][]string
}

// DeleteChatsAtomically 记录批量删除并在同一测试事务上下文执行聚合事实清理。
func (f *deletionChatFake) DeleteChatsAtomically(
	ctx context.Context,
	chatIDs []string,
	finalize func(context.Context) error,
) error {
	f.calls = append(f.calls, append([]string(nil), chatIDs...))
	if finalize != nil {
		return finalize(ctx)
	}
	return nil
}

// deletionFixture 汇集团队删除契约测试使用的真实仓储和服务。
type deletionFixture struct {
	// db 是用于核对级联结果的临时数据库。
	db *gorm.DB
	// channels 是团队配置仓储。
	channels controllers.ChannelController
	// threads 是团队对话仓储。
	threads controllers.ChannelThreadController
	// chats 记录成员 Chat 清理调用。
	chats *deletionChatFake
	// service 是待测试的团队删除用例。
	service *service
}

func newDeletionFixture(t *testing.T) deletionFixture {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "channel-deletion.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create deletion database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("open deletion database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := sqlDB.Close(); closeErr != nil {
			t.Errorf("close deletion database: %v", closeErr)
		}
	})
	channels := controllers.NewChannelController(database.DB)
	threads := controllers.NewChannelThreadController(database.DB)
	chats := &deletionChatFake{}
	transactions := controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	return deletionFixture{
		db:       database.DB,
		channels: channels,
		threads:  threads,
		chats:    chats,
		service: &service{
			channels:     channels,
			threads:      threads,
			transactions: transactions,
			chats:        chats,
		},
	}
}

func (f deletionFixture) seedTeam(t *testing.T) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	channelID := "11111111111111111111111111111111"
	firstThreadID := "22222222222222222222222222222222"
	secondThreadID := "33333333333333333333333333333333"
	leadID := "44444444444444444444444444444444"
	memberID := "55555555555555555555555555555555"
	if err := f.channels.Create(ctx, models.Channel{
		ID: channelID, Name: "删除契约团队", LeadWakerID: leadID,
	}, []models.ChannelMember{
		{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: channelID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	readyAt := int64(1)
	for index, threadID := range []string{firstThreadID, secondThreadID} {
		leadChatID := []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "cccccccccccccccccccccccccccccccc"}[index]
		memberChatID := []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "dddddddddddddddddddddddddddddddd"}[index]
		if err := f.threads.Create(ctx, models.ChannelThread{
			ID: threadID, ChannelID: channelID, Title: "团队对话", LeadWakerID: leadID,
			LeadChatID: leadChatID, WorkspaceDirectory: "/tmp/team", AdditionalWorkingDirectories: []string{"/tmp/source"},
			MaxParallel: 3, MaxTasks: 12, MaxFailureAttempts: 2, ReadyAt: &readyAt,
		}, []models.ChannelThreadMember{
			{ThreadID: threadID, WakerID: leadID, ChatID: leadChatID, IsLead: true, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
			{ThreadID: threadID, WakerID: memberID, ChatID: memberChatID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
		}); err != nil {
			t.Fatalf("seed thread %s: %v", threadID, err)
		}
	}
	return channelID, firstThreadID, secondThreadID
}

func TestDeleteThreadRemovesAllOrchestrationFactsAndKeepsTeam(t *testing.T) {
	fixture := newDeletionFixture(t)
	channelID, threadID, remainingThreadID := fixture.seedTeam(t)
	ctx := context.Background()
	taskID := "66666666666666666666666666666666"
	dependencyID := "77777777777777777777777777777777"
	for _, task := range []models.ChannelTask{
		{ID: taskID, ThreadID: threadID, AssigneeWakerID: "member", Subject: "已完成", Description: "删除", Status: constants.ChannelTaskStatusCompleted, ChangedPaths: []string{}},
		{ID: dependencyID, ThreadID: threadID, AssigneeWakerID: "member", Subject: "上游", Description: "删除", Status: constants.ChannelTaskStatusCompleted, ChangedPaths: []string{}},
	} {
		if err := fixture.db.Create(&task).Error; err != nil {
			t.Fatalf("seed task: %v", err)
		}
	}
	facts := []interface{}{
		&models.ChannelTaskDependency{TaskID: taskID, DependsOnTaskID: dependencyID},
		&models.ChannelTaskAttempt{ID: "88888888888888888888888888888888", TaskID: taskID, ChatID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", AssigneeWakerID: "member", Fence: 1, Reason: constants.ChannelAttemptReasonInitial},
		&models.ChannelTaskFollowUp{ID: "99999999999999999999999999999999", TaskID: taskID, Text: "补充", Actor: constants.ChannelMessageActorUser, Payload: workinput.Input{Actor: string(constants.ChannelMessageActorUser), Content: "补充"}},
		&models.ChannelLeadInput{ID: "10101010101010101010101010101010", ThreadID: threadID, Payload: workinput.Input{Actor: string(constants.ChannelMessageActorUser), Content: "输入"}, Status: constants.ChannelInputStatusConsumed},
		&models.ChannelLeadNotification{ID: "12121212121212121212121212121212", ThreadID: threadID, TaskID: taskID, AttemptID: "88888888888888888888888888888888", Status: constants.ChannelNotificationStatusConsumed, Payload: "报告"},
		&models.ChannelEvent{ID: "13131313131313131313131313131313", ChannelID: channelID, ThreadID: threadID, Type: "test", Payload: []byte(`{}`)},
	}
	for _, fact := range facts {
		if err := fixture.db.Create(fact).Error; err != nil {
			t.Fatalf("seed deletion fact %T: %v", fact, err)
		}
	}
	if err := fixture.service.DeleteThread(ctx, channelID, threadID); err != nil {
		t.Fatalf("DeleteThread() error = %v", err)
	}
	if len(fixture.chats.calls) != 1 || len(fixture.chats.calls[0]) != 2 {
		t.Fatalf("deleted chat calls = %v", fixture.chats.calls)
	}
	if _, err := fixture.channels.Get(ctx, channelID); err != nil {
		t.Fatalf("team removed with one conversation: %v", err)
	}
	if _, err := fixture.threads.Get(ctx, remainingThreadID); err != nil {
		t.Fatalf("other conversation removed: %v", err)
	}
	for name, model := range map[string]interface{}{
		"threads":       &models.ChannelThread{},
		"members":       &models.ChannelThreadMember{},
		"tasks":         &models.ChannelTask{},
		"dependencies":  &models.ChannelTaskDependency{},
		"attempts":      &models.ChannelTaskAttempt{},
		"follow-ups":    &models.ChannelTaskFollowUp{},
		"inputs":        &models.ChannelLeadInput{},
		"notifications": &models.ChannelLeadNotification{},
		"events":        &models.ChannelEvent{},
	} {
		var count int64
		query := fixture.db.Model(model)
		if name == "dependencies" || name == "attempts" || name == "follow-ups" {
			query = query.Where("task_id IN ?", []string{taskID, dependencyID})
		} else if name == "threads" {
			query = query.Where("id = ?", threadID)
		} else {
			query = query.Where("thread_id = ?", threadID)
		}
		if err := query.Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("remaining %s = %d, error = %v", name, count, err)
		}
	}
}

func TestDeleteChannelRejectsActiveWorkThenRemovesEveryThread(t *testing.T) {
	fixture := newDeletionFixture(t)
	channelID, firstThreadID, _ := fixture.seedTeam(t)
	ctx := context.Background()
	pending := models.ChannelTask{
		ID: "14141414141414141414141414141414", ThreadID: firstThreadID,
		AssigneeWakerID: "member", Subject: "运行中", Description: "先停止", Status: constants.ChannelTaskStatusPending,
		ChangedPaths: []string{},
	}
	if err := fixture.db.Create(&pending).Error; err != nil {
		t.Fatalf("seed pending task: %v", err)
	}
	if err := fixture.service.DeleteChannel(ctx, channelID); !errors.Is(err, status_error.ChannelHasActiveWork) {
		t.Fatalf("DeleteChannel(active) error = %v, want ChannelHasActiveWork", err)
	}
	if len(fixture.chats.calls) != 0 {
		t.Fatalf("active team deleted chats: %v", fixture.chats.calls)
	}
	if err := fixture.db.Model(&models.ChannelTask{}).Where("id = ?", pending.ID).
		Updates(map[string]interface{}{"status": constants.ChannelTaskStatusCancelled}).Error; err != nil {
		t.Fatalf("cancel pending task: %v", err)
	}
	if err := fixture.service.DeleteChannel(ctx, channelID); err != nil {
		t.Fatalf("DeleteChannel() error = %v", err)
	}
	if len(fixture.chats.calls) != 1 || len(fixture.chats.calls[0]) != 4 {
		t.Fatalf("deleted team chat calls = %v", fixture.chats.calls)
	}
	if _, err := fixture.channels.Get(ctx, channelID); err == nil {
		t.Fatal("deleted team still exists")
	}
	threadIDs, err := fixture.threads.ListIDsByChannel(ctx, channelID)
	if err != nil || len(threadIDs) != 0 {
		t.Fatalf("remaining team conversations = %v, error = %v", threadIDs, err)
	}
}

// blockedDeletionChats 将删除预检与提交之间的并发窗口固定下来。
type blockedDeletionChats struct {
	// Service 保留测试不需要使用的 Chat 端口。
	chatservice.Service
	// entered 通知测试删除已进入 Chat 聚合提交阶段。
	entered chan struct{}
	// proceed 由测试决定何时完成删除。
	proceed chan struct{}
	// transactions 提供与生产相同的真实数据库事务。
	transactions controllers.TransactionController
}

// DeleteChatsAtomically 在团队保护内暂停，然后通过真实事务提交删除事实。
func (f *blockedDeletionChats) DeleteChatsAtomically(ctx context.Context, _ []string, finalize func(context.Context) error) error {
	close(f.entered)
	<-f.proceed
	return f.transactions.Transaction(ctx, finalize)
}

func TestDeleteThreadSerializesConcurrentAcceptedTask(t *testing.T) {
	fixture := newDeletionFixture(t)
	channelID, threadID, _ := fixture.seedTeam(t)
	fixture.service.tasks = controllers.NewChannelTaskController(fixture.db)
	blocked := &blockedDeletionChats{entered: make(chan struct{}), proceed: make(chan struct{}), transactions: fixture.service.transactions}
	fixture.service.chats = blocked
	deleted := make(chan error, 1)
	go func() { deleted <- fixture.service.DeleteThread(context.Background(), channelID, threadID) }()
	<-blocked.entered
	created := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, err := fixture.service.CreateTask(context.Background(), CreateTaskInput{ThreadID: threadID, AssigneeWakerID: "55555555555555555555555555555555", Subject: "新任务", Description: "删除不能吞掉已接受的任务", DeferDispatch: true})
		created <- err
	}()
	<-started
	select {
	case err := <-created:
		close(blocked.proceed)
		t.Fatalf("task bypassed team deletion protection: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(blocked.proceed)
	if err := <-deleted; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-created:
		if !hasStatusErrorCode(err, status_error.ChannelThreadNotFound.Code()) {
			t.Fatalf("task must reject deleted team conversation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("task waiter did not resume")
	}
}
