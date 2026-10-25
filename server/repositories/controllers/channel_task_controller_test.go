package controllers

import (
	"context"
	"sync"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestChannelTaskDispatchClaimIsAtomicAndQuotaBound(t *testing.T) {
	database := newTestDB(t)
	controller := NewChannelTaskController(database)
	ctx := context.Background()
	threadID := "11111111111111111111111111111111"
	wakerID := "22222222222222222222222222222222"
	tasks := []models.ChannelTask{
		{
			ID:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ThreadID:        threadID,
			AssigneeWakerID: wakerID,
			Subject:         "任务一",
			Description:     "先领取",
			Status:          constants.ChannelTaskStatusPending,
			ChangedPaths:    []string{},
		},
		{
			ID:              "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			ThreadID:        threadID,
			AssigneeWakerID: "33333333333333333333333333333333",
			Subject:         "任务二",
			Description:     "受并行配额限制",
			Status:          constants.ChannelTaskStatusPending,
			ChangedPaths:    []string{},
		},
	}
	for _, task := range tasks {
		if err := controller.Create(ctx, task, nil); err != nil {
			t.Fatalf("create task %s: %v", task.ID, err)
		}
	}

	var wait sync.WaitGroup
	results := make(chan bool, 2)
	errors := make(chan error, 2)
	for index := 0; index < 2; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			claimed, err := controller.ClaimForDispatch(
				ctx,
				tasks[0].ID,
				threadID,
				wakerID,
				0,
				1,
				1,
				100,
			)
			results <- claimed
			errors <- err
		}()
	}
	wait.Wait()
	close(results)
	close(errors)
	claimedCount := 0
	for claimed := range results {
		if claimed {
			claimedCount++
		}
	}
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent claim error: %v", err)
		}
	}
	if claimedCount != 1 {
		t.Fatalf("successful claims = %d, want 1", claimedCount)
	}
	secondClaimed, err := controller.ClaimForDispatch(
		ctx,
		tasks[1].ID,
		threadID,
		tasks[1].AssigneeWakerID,
		0,
		1,
		1,
		200,
	)
	if err != nil {
		t.Fatalf("quota-bound claim error: %v", err)
	}
	if secondClaimed {
		t.Fatal("second task claimed despite max_parallel=1")
	}
}

func TestChannelTaskDispatchSnapshotBatchesDependenciesAndActiveAssignees(t *testing.T) {
	database := newTestDB(t)
	controller := NewChannelTaskController(database)
	ctx := context.Background()
	threadID := "11111111111111111111111111111111"
	completedTask := models.ChannelTask{
		ID:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ThreadID:        threadID,
		AssigneeWakerID: "22222222222222222222222222222222",
		Subject:         "准备输入",
		Description:     "提供下游任务需要的输入。",
		Status:          constants.ChannelTaskStatusCompleted,
		ChangedPaths:    []string{},
	}
	runningTask := models.ChannelTask{
		ID:              "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ThreadID:        threadID,
		AssigneeWakerID: "33333333333333333333333333333333",
		Subject:         "正在执行",
		Description:     "占用一个成员执行名额。",
		Status:          constants.ChannelTaskStatusInProgress,
		ChangedPaths:    []string{},
	}
	pendingTask := models.ChannelTask{
		ID:              "cccccccccccccccccccccccccccccccc",
		ThreadID:        threadID,
		AssigneeWakerID: "44444444444444444444444444444444",
		Subject:         "等待调度",
		Description:     "依赖完成后可以领取。",
		Status:          constants.ChannelTaskStatusPending,
		ChangedPaths:    []string{},
	}
	for _, task := range []models.ChannelTask{completedTask, runningTask} {
		if err := controller.Create(ctx, task, nil); err != nil {
			t.Fatalf("create task %s: %v", task.ID, err)
		}
	}
	if err := controller.Create(ctx, pendingTask, []models.ChannelTaskDependency{
		{TaskID: pendingTask.ID, DependsOnTaskID: completedTask.ID, CreatedAt: 1},
		{TaskID: pendingTask.ID, DependsOnTaskID: runningTask.ID, CreatedAt: 2},
	}); err != nil {
		t.Fatalf("create pending task: %v", err)
	}

	snapshot, err := controller.LoadDispatchSnapshot(ctx, threadID)
	if err != nil {
		t.Fatalf("LoadDispatchSnapshot() error = %v", err)
	}
	if len(snapshot.Tasks) != 3 || snapshot.ActiveCount != 1 {
		t.Fatalf("dispatch snapshot tasks=%d active=%d", len(snapshot.Tasks), snapshot.ActiveCount)
	}
	if _, exists := snapshot.ActiveAssigneeWakerIDs[runningTask.AssigneeWakerID]; !exists {
		t.Fatalf("active assignees = %v", snapshot.ActiveAssigneeWakerIDs)
	}
	dependencies := snapshot.DependenciesByTaskID[pendingTask.ID]
	if len(dependencies) != 2 || dependencies[0].ID != completedTask.ID ||
		dependencies[1].ID != runningTask.ID {
		t.Fatalf("pending dependencies = %+v", dependencies)
	}
}

func TestChannelTaskReservationReleaseRestoresOwnedFollowUps(t *testing.T) {
	database := newTestDB(t)
	controller := NewChannelTaskController(database)
	ctx := context.Background()
	task := models.ChannelTask{
		ID:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ThreadID:        "11111111111111111111111111111111",
		AssigneeWakerID: "22222222222222222222222222222222",
		Subject:         "恢复补充",
		Description:     "启动失败后重新排队",
		Status:          constants.ChannelTaskStatusPending,
		ChangedPaths:    []string{},
	}
	if err := controller.Create(ctx, task, nil); err != nil {
		t.Fatalf("create task: %v", err)
	}
	claimed, err := controller.ClaimForDispatch(ctx, task.ID, task.ThreadID, task.AssigneeWakerID, 0, 1, 3, 100)
	if err != nil || !claimed {
		t.Fatalf("claim task = %t, error = %v", claimed, err)
	}
	attempt := models.ChannelTaskAttempt{
		ID:              "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		TaskID:          task.ID,
		ChatID:          "33333333333333333333333333333333",
		ExecutionID:     "44444444444444444444444444444444",
		AssigneeWakerID: task.AssigneeWakerID,
		Fence:           1,
		Reason:          constants.ChannelAttemptReasonInitial,
		Status:          constants.ExecutionStatusPending,
	}
	if err := controller.CreateAttempt(ctx, attempt); err != nil {
		t.Fatalf("create attempt: %v", err)
	}
	followUp := models.ChannelTaskFollowUp{
		ID:     "55555555555555555555555555555555",
		TaskID: task.ID,
		Text:   "请补充测试",
		Actor:  constants.ChannelMessageActorUser,
	}
	if err := controller.CreateFollowUp(ctx, followUp); err != nil {
		t.Fatalf("create follow-up: %v", err)
	}
	if err := controller.MarkFollowUpsDelivered(ctx, []string{followUp.ID}, attempt.ID, 200); err != nil {
		t.Fatalf("claim follow-up: %v", err)
	}
	released, err := controller.ReleaseDispatchClaim(
		ctx,
		task.ID,
		attempt.ID,
		attempt.Fence,
		constants.ExecutionStatusCanceled,
		"not started",
		300,
	)
	if err != nil || !released {
		t.Fatalf("release claim = %t, error = %v", released, err)
	}
	if err := controller.ReleaseAttemptFollowUps(ctx, attempt.ID); err != nil {
		t.Fatalf("release follow-ups: %v", err)
	}
	pending, err := controller.ListPendingFollowUps(ctx, task.ID)
	if err != nil || len(pending) != 1 || pending[0].ID != followUp.ID {
		t.Fatalf("pending follow-ups = %+v, error = %v", pending, err)
	}
	current, err := controller.Get(ctx, task.ID)
	if err != nil || current.Status != constants.ChannelTaskStatusPending {
		t.Fatalf("released task = %+v, error = %v", current, err)
	}
}

func TestChannelTaskWatcherFailureUsesCurrentFenceOnce(t *testing.T) {
	database := newTestDB(t)
	controller := NewChannelTaskController(database)
	ctx := context.Background()
	task := models.ChannelTask{
		ID:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ThreadID:        "11111111111111111111111111111111",
		AssigneeWakerID: "22222222222222222222222222222222",
		Subject:         "监听补偿",
		Description:     "连续失败后按当前围栏提交失败。",
		Status:          constants.ChannelTaskStatusInProgress,
		CurrentFence:    2,
		ChangedPaths:    []string{},
	}
	attempt := models.ChannelTaskAttempt{
		ID:              "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		TaskID:          task.ID,
		ChatID:          "33333333333333333333333333333333",
		ExecutionID:     "44444444444444444444444444444444",
		AssigneeWakerID: task.AssigneeWakerID,
		Fence:           task.CurrentFence,
		Reason:          constants.ChannelAttemptReasonInitial,
		Status:          constants.ExecutionStatusRunning,
	}
	if err := controller.Create(ctx, task, nil); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := controller.CreateAttempt(ctx, attempt); err != nil {
		t.Fatalf("create attempt: %v", err)
	}
	transitioned, err := controller.FailRunningAttempt(
		ctx,
		task.ID,
		attempt.ID,
		attempt.Fence,
		"watcher retry exhausted",
		100,
	)
	if err != nil || !transitioned {
		t.Fatalf("FailRunningAttempt() transitioned=%t error=%v", transitioned, err)
	}
	transitioned, err = controller.FailRunningAttempt(
		ctx,
		task.ID,
		attempt.ID,
		attempt.Fence,
		"late duplicate",
		200,
	)
	if err != nil || transitioned {
		t.Fatalf("duplicate FailRunningAttempt() transitioned=%t error=%v", transitioned, err)
	}
	current, err := controller.Get(ctx, task.ID)
	if err != nil || current.Status != constants.ChannelTaskStatusFailed ||
		current.ErrorMessage != "watcher retry exhausted" {
		t.Fatalf("failed task = %+v, error = %v", current, err)
	}
	currentAttempt, err := controller.GetCurrentAttempt(ctx, task.ID, task.CurrentFence)
	if err != nil || currentAttempt.Status != constants.ExecutionStatusFailed ||
		currentAttempt.ErrorMessage != "watcher retry exhausted" {
		t.Fatalf("failed attempt = %+v, error = %v", currentAttempt, err)
	}
}

func TestChannelThreadTaskSlotsRespectPersistentLimit(t *testing.T) {
	database := newTestDB(t)
	controller := NewChannelThreadController(database)
	ctx := context.Background()
	readyAt := int64(1)
	thread := models.ChannelThread{
		ID:                 "11111111111111111111111111111111",
		ChannelID:          "22222222222222222222222222222222",
		Title:              "配额测试",
		LeadWakerID:        "33333333333333333333333333333333",
		LeadChatID:         "44444444444444444444444444444444",
		WorkspaceDirectory: "/tmp/team-quota",
		MaxParallel:        3,
		MaxTasks:           2,
		MaxFailureAttempts: 2,
		ReadyAt:            &readyAt,
	}
	members := []models.ChannelThreadMember{{
		ThreadID:        thread.ID,
		WakerID:         thread.LeadWakerID,
		ChatID:          thread.LeadChatID,
		IsLead:          true,
		RuntimeID:       "codex",
		PermissionLevel: constants.PermissionLevelAuto,
	}}
	if err := controller.Create(ctx, thread, members); err != nil {
		t.Fatalf("create thread: %v", err)
	}
	for index, want := range []bool{true, true, false} {
		reserved, err := controller.ReserveTaskSlot(ctx, thread.ID)
		if err != nil {
			t.Fatalf("reserve slot %d: %v", index, err)
		}
		if reserved != want {
			t.Fatalf("reserve slot %d = %t, want %t", index, reserved, want)
		}
	}
}

func TestChannelTaskControllerCountsOnlyFailedAttempts(t *testing.T) {
	database := newTestDB(t)
	controller := NewChannelTaskController(database)
	ctx := context.Background()
	taskID := "11111111111111111111111111111111"
	attempts := []models.ChannelTaskAttempt{
		{
			ID:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			TaskID:          taskID,
			ChatID:          "chat-a",
			ExecutionID:     "execution-a",
			AssigneeWakerID: "waker-a",
			Fence:           1,
			Reason:          constants.ChannelAttemptReasonInitial,
			Status:          constants.ExecutionStatusCompleted,
		},
		{
			ID:              "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			TaskID:          taskID,
			ChatID:          "chat-a",
			ExecutionID:     "execution-b",
			AssigneeWakerID: "waker-a",
			Fence:           2,
			Reason:          constants.ChannelAttemptReasonUserFollowUp,
			Status:          constants.ExecutionStatusFailed,
		},
		{
			ID:              "cccccccccccccccccccccccccccccccc",
			TaskID:          taskID,
			ChatID:          "chat-a",
			ExecutionID:     "execution-c",
			AssigneeWakerID: "waker-a",
			Fence:           3,
			Reason:          constants.ChannelAttemptReasonRetry,
			Status:          constants.ExecutionStatusCanceled,
		},
	}
	for _, attempt := range attempts {
		if err := controller.CreateAttempt(ctx, attempt); err != nil {
			t.Fatalf("CreateAttempt(%s) error = %v", attempt.ID, err)
		}
	}
	count, err := controller.CountFailureAttempts(ctx, taskID)
	if err != nil {
		t.Fatalf("CountFailureAttempts() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("CountFailureAttempts() = %d, want 1", count)
	}
}

func TestChannelTaskControllerWritesJSONChangedPaths(t *testing.T) {
	database := newTestDB(t)
	controller := NewChannelTaskController(database)
	ctx := context.Background()
	threadID := "11111111111111111111111111111111"
	task := models.ChannelTask{
		ID:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ThreadID:        threadID,
		AssigneeWakerID: "waker-a",
		Subject:         "实现功能",
		Description:     "完成并报告改动文件",
		Status:          constants.ChannelTaskStatusCompleted,
		ChangedPaths:    []string{},
	}
	if err := controller.Create(ctx, task, nil); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	legacyPath := "projects/share/helloworld-ruby/README.md"
	if err := controller.UpdateFields(ctx, task.ID, map[string]interface{}{
		"changed_paths": []string{legacyPath},
	}); err != nil {
		t.Fatalf("UpdateFields() error = %v", err)
	}
	var stored string
	if err := database.Raw(
		"SELECT changed_paths FROM channel_tasks WHERE id = ?",
		task.ID,
	).Scan(&stored).Error; err != nil {
		t.Fatalf("read stored changed paths: %v", err)
	}
	if stored != `["projects/share/helloworld-ruby/README.md"]` {
		t.Fatalf("stored changed_paths = %q, want JSON array", stored)
	}
	tasks, err := controller.ListByThread(ctx, threadID)
	if err != nil {
		t.Fatalf("ListByThread() error = %v", err)
	}
	if len(tasks) != 1 || len(tasks[0].ChangedPaths) != 1 ||
		tasks[0].ChangedPaths[0] != legacyPath {
		t.Fatalf("ChangedPaths = %v, want [%s]", tasks[0].ChangedPaths, legacyPath)
	}
}

func TestChannelDeliveryRecoveryPreservesDurableQueues(t *testing.T) {
	database := newTestDB(t)
	channels := NewChannelController(database)
	threads := NewChannelThreadController(database)
	tasks := NewChannelTaskController(database)
	ctx := context.Background()
	channelID := "00000000000000000000000000000000"
	threadID := "11111111111111111111111111111111"
	leadID := "22222222222222222222222222222222"
	memberID := "33333333333333333333333333333333"
	if err := channels.Create(ctx, models.Channel{
		ID: channelID, Name: "恢复队列团队", LeadWakerID: leadID,
	}, []models.ChannelMember{
		{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: channelID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("create recovery channel: %v", err)
	}
	readyAt := int64(1)
	if err := threads.Create(ctx, models.ChannelThread{
		ID: threadID, ChannelID: channelID, Title: "恢复队列", LeadWakerID: leadID,
		LeadChatID: "44444444444444444444444444444444", WorkspaceDirectory: "/tmp/recovery",
		MaxParallel: 3, MaxTasks: 12, MaxFailureAttempts: 2, ReadyAt: &readyAt,
	}, []models.ChannelThreadMember{
		{ThreadID: threadID, WakerID: leadID, ChatID: "55555555555555555555555555555555", IsLead: true, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ThreadID: threadID, WakerID: memberID, ChatID: "66666666666666666666666666666666", RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("create recovery thread: %v", err)
	}
	payload := workinput.Input{Actor: string(constants.ChannelMessageActorUser), Content: "补充原任务"}

	for _, input := range []models.ChannelLeadInput{
		{ID: "input-pending", ThreadID: threadID, Payload: payload, Status: constants.ChannelInputStatusPending},
		{ID: "input-delivering", ThreadID: threadID, Payload: payload, Status: constants.ChannelInputStatusDelivering, LeadExecutionID: "execution-old"},
	} {
		if err := threads.CreateLeadInput(ctx, input); err != nil {
			t.Fatalf("CreateLeadInput(%s) error = %v", input.ID, err)
		}
	}
	threadIDs, err := threads.RecoverLeadInputs(ctx, 100)
	if err != nil {
		t.Fatalf("RecoverLeadInputs() error = %v", err)
	}
	if len(threadIDs) != 1 || threadIDs[0] != threadID {
		t.Fatalf("pending input thread IDs = %v, want [%s]", threadIDs, threadID)
	}
	inputs, err := threads.ListLeadInputs(ctx, threadID, []constants.ChannelInputStatus{
		constants.ChannelInputStatusConsumed,
	}, 10)
	if err != nil || len(inputs) != 1 || inputs[0].ID != "input-delivering" {
		t.Fatalf("recovered consumed inputs = %+v, error = %v", inputs, err)
	}

	for _, notification := range []models.ChannelLeadNotification{
		{ID: "notification-pending", ThreadID: threadID, TaskID: "task-a", AttemptID: "attempt-a", Status: constants.ChannelNotificationStatusPending, Payload: "pending"},
		{ID: "notification-delivering", ThreadID: threadID, TaskID: "task-b", AttemptID: "attempt-b", Status: constants.ChannelNotificationStatusDelivering, Payload: "delivering", LeadExecutionID: "execution-old"},
	} {
		if err := tasks.CreateNotification(ctx, notification); err != nil {
			t.Fatalf("CreateNotification(%s) error = %v", notification.ID, err)
		}
	}
	open, err := tasks.HasLeadProcessingNotifications(ctx, threadID, "")
	if err != nil || !open {
		t.Fatalf("HasLeadProcessingNotifications() = %t, error = %v, want true", open, err)
	}
	processingOld, err := tasks.HasLeadProcessingNotifications(ctx, threadID, "execution-old")
	if err != nil || !processingOld {
		t.Fatalf("HasLeadProcessingNotifications(execution-old) = %t, error = %v, want true", processingOld, err)
	}
	processingUser, err := tasks.HasLeadProcessingNotifications(ctx, threadID, "execution-user")
	if err != nil || processingUser {
		t.Fatalf("HasLeadProcessingNotifications(execution-user) = %t, error = %v, want false", processingUser, err)
	}
	notificationThreadIDs, err := tasks.RecoverNotifications(ctx, 200)
	if err != nil {
		t.Fatalf("RecoverNotifications() error = %v", err)
	}
	if len(notificationThreadIDs) != 1 || notificationThreadIDs[0] != threadID {
		t.Fatalf("pending notification thread IDs = %v, want [%s]", notificationThreadIDs, threadID)
	}
	notifications, err := tasks.ListPendingNotifications(ctx, threadID, 10)
	if err != nil || len(notifications) != 2 || notifications[1].LeadExecutionID != "" {
		t.Fatalf("recovered notifications = %+v, error = %v", notifications, err)
	}
	ids := []string{notifications[0].ID, notifications[1].ID}
	if err := tasks.UpdateNotifications(
		ctx,
		ids,
		constants.ChannelNotificationStatusDelivering,
		"execution-new",
		300,
	); err != nil {
		t.Fatalf("mark notifications delivering: %v", err)
	}
	if err := tasks.UpdateNotifications(
		ctx,
		ids,
		constants.ChannelNotificationStatusConsumed,
		"execution-new",
		400,
	); err != nil {
		t.Fatalf("consume notifications: %v", err)
	}
	open, err = tasks.HasLeadProcessingNotifications(ctx, threadID, "")
	if err != nil || open {
		t.Fatalf("HasLeadProcessingNotifications() = %t, error = %v, want false", open, err)
	}
}

func TestChannelStopDeliveryStateCannotBeOverwrittenByLateWatcher(t *testing.T) {
	database := newTestDB(t)
	threads := NewChannelThreadController(database)
	tasks := NewChannelTaskController(database)
	ctx := context.Background()
	threadID := "11111111111111111111111111111111"
	input := models.ChannelLeadInput{
		ID:              "input-delivering",
		ThreadID:        threadID,
		Payload:         workinput.Input{Actor: string(constants.ChannelMessageActorUser), Content: "补充"},
		Status:          constants.ChannelInputStatusDelivering,
		LeadExecutionID: "execution-a",
	}
	if err := threads.CreateLeadInput(ctx, input); err != nil {
		t.Fatalf("CreateLeadInput() error = %v", err)
	}
	if err := threads.CancelOpenLeadInputs(ctx, threadID, 100); err != nil {
		t.Fatalf("CancelOpenLeadInputs() error = %v", err)
	}
	if err := threads.UpdateLeadInputs(ctx, []string{input.ID}, constants.ChannelInputStatusConsumed, "execution-a", 200); err != nil {
		t.Fatalf("late UpdateLeadInputs() error = %v", err)
	}
	cancelled, err := threads.ListLeadInputs(ctx, threadID, []constants.ChannelInputStatus{
		constants.ChannelInputStatusCancelled,
	}, 10)
	if err != nil || len(cancelled) != 1 {
		t.Fatalf("cancelled inputs = %+v, error = %v", cancelled, err)
	}

	notification := models.ChannelLeadNotification{
		ID:              "notification-delivering",
		ThreadID:        threadID,
		TaskID:          "task-a",
		AttemptID:       "attempt-a",
		Status:          constants.ChannelNotificationStatusDelivering,
		Payload:         "result",
		LeadExecutionID: "execution-a",
	}
	if err := tasks.CreateNotification(ctx, notification); err != nil {
		t.Fatalf("CreateNotification() error = %v", err)
	}
	if err := tasks.ConsumeOpenNotifications(ctx, threadID, 100); err != nil {
		t.Fatalf("ConsumeOpenNotifications() error = %v", err)
	}
	if err := tasks.UpdateNotifications(ctx, []string{notification.ID}, constants.ChannelNotificationStatusPending, "", 200); err != nil {
		t.Fatalf("late UpdateNotifications() error = %v", err)
	}
	pending, err := tasks.ListPendingNotifications(ctx, threadID, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("late watcher restored notifications = %+v, error = %v", pending, err)
	}
}
