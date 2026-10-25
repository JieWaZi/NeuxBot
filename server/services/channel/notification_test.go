package channel

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	messageview "github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	channeltool "github.com/JieWaZi/NeuxBot/server/internal/teamcapability"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestFinalResultContentOnlyKeepsUserFacingText(t *testing.T) {
	message := messageview.Message{
		Content: "正在查找资料。最终结论。",
		DisplayParts: []messageview.MessageDisplayPart{
			{Type: constants.MessageDisplayPartProcessCommentary, Text: "正在查找资料。"},
			{Type: constants.MessageDisplayPartReasoning, Text: "内部推理"},
			{Type: constants.MessageDisplayPartText, Text: "最终结论。"},
		},
	}
	if result := finalResultContent(message); result != "最终结论。" {
		t.Fatalf("finalResultContent() = %q, want only final visible text", result)
	}
}

func TestTeamNotificationPromptIncludesMemberReportOnceWithoutTechnicalMetadata(t *testing.T) {
	prompt := teamNotificationPrompt([]announceEntry{{
		MemberDisplayName: "研究伙伴",
		Content:           "最终研究结果",
	}}, "=== Task Board ===\nAll 1 tasks have reached a terminal state.", []models.ChannelTask{{Status: constants.ChannelTaskStatusCompleted}})
	for _, required := range []string{
		"[System message]",
		"1 member result report(s)",
		"one complete final summary",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("announce prompt missing %q: %q", required, prompt)
		}
	}
	if strings.Count(prompt, "最终研究结果") != 1 {
		t.Fatalf("member report should enter the leader prompt exactly once: %q", prompt)
	}
	for _, forbidden := range []string{"attemptId", "taskId", "changedPaths"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("announce prompt leaked technical field %q: %q", forbidden, prompt)
		}
	}
}

func TestTeamTasksStillRunningIncludesPendingAndInProgress(t *testing.T) {
	for _, status := range []constants.ChannelTaskStatus{
		constants.ChannelTaskStatusPending,
		constants.ChannelTaskStatusInProgress,
	} {
		if !teamTasksStillRunning([]models.ChannelTask{{Status: status}}) {
			t.Fatalf("status %s should keep the team board active", status)
		}
	}
	for _, status := range []constants.ChannelTaskStatus{
		constants.ChannelTaskStatusInReview,
		constants.ChannelTaskStatusCompleted,
		constants.ChannelTaskStatusFailed,
		constants.ChannelTaskStatusCancelled,
	} {
		if teamTasksStillRunning([]models.ChannelTask{{Status: status}}) {
			t.Fatalf("status %s should allow a user-facing update", status)
		}
	}
}

func TestTeamNotificationPromptDoesNotClaimCompletionBeforeHumanReview(t *testing.T) {
	tasks := []models.ChannelTask{{Subject: "确认发布", Status: constants.ChannelTaskStatusInReview}}
	prompt := teamNotificationPrompt(
		[]announceEntry{{MemberDisplayName: "发布伙伴", Content: "result"}},
		buildTaskBoardSnapshot(tasks),
		tasks,
	)
	if !strings.Contains(prompt, "awaiting user review") ||
		!strings.Contains(prompt, "do not claim that all team work is complete") {
		t.Fatalf("review prompt does not preserve human-review semantics: %q", prompt)
	}
}

func TestTeamNotificationPromptAllowsFinalSummaryOnlyWhenBoardResolved(t *testing.T) {
	tasks := []models.ChannelTask{
		{Status: constants.ChannelTaskStatusCompleted},
		{Status: constants.ChannelTaskStatusFailed},
	}
	prompt := teamNotificationPrompt(
		[]announceEntry{
			{MemberDisplayName: "甲", Content: "result one"},
			{MemberDisplayName: "乙", Content: "[FAILED] failed"},
		},
		buildTaskBoardSnapshot(tasks),
		tasks,
	)
	if !strings.Contains(prompt, "All 2 tasks have reached a terminal state") ||
		!strings.Contains(prompt, "one complete final summary") {
		t.Fatalf("terminal prompt does not request final summary: %q", prompt)
	}
}

func TestTeammateReportContentCapsPreviewAndMarksItUnactionable(t *testing.T) {
	report, truncated := teammateReportContent(
		constants.ChannelTaskStatusCompleted,
		strings.Repeat("结论", 3_000),
		"",
		[]string{"server/main.go"},
	)
	if !truncated || len([]rune(report)) != constants.ChannelMemberReportMaximumRunes {
		t.Fatalf("truncated report length=%d truncated=%t", len([]rune(report)), truncated)
	}
	if !strings.Contains(report, memberReportTruncationNotice) {
		t.Fatalf("truncated report missing safety notice: %q", report[len(report)-120:])
	}
	prompt := teamNotificationPrompt(
		[]announceEntry{{MemberDisplayName: "研究伙伴", Truncated: true}},
		buildTaskBoardSnapshot([]models.ChannelTask{{Status: constants.ChannelTaskStatusCompleted}}),
		[]models.ChannelTask{{Status: constants.ChannelTaskStatusCompleted}},
	)
	if !strings.Contains(prompt, "Do not draw conclusions") || strings.Contains(prompt, "one complete final summary") {
		t.Fatalf("truncated report prompt is unsafe: %q", prompt)
	}
}

func TestTeammateReportContentPreservesMemberSummaryWithoutInventingSections(t *testing.T) {
	report, truncated := teammateReportContent(
		constants.ChannelTaskStatusCompleted,
		"已完成页面实现，构建与测试均通过。产物位于 app/page.tsx。",
		"",
		[]string{"app/page.tsx"},
	)
	if truncated {
		t.Fatal("short member summary was unexpectedly truncated")
	}
	if report != "已完成页面实现，构建与测试均通过。产物位于 app/page.tsx。" {
		t.Fatalf("member summary was rewritten into a rigid template: %q", report)
	}
}

func TestDispatchTaskContentUsesBoundedPayload(t *testing.T) {
	content := dispatchTaskContent(
		models.ChannelTask{
			Subject:     "实现页面",
			Description: "完成首页布局并验证构建。",
		},
		[]models.ChannelTask{{
			Subject: "设计稿",
			Status:  constants.ChannelTaskStatusCompleted,
			Result:  "设计已确认",
		}},
	)
	for _, unexpected := range []string{
		"你正在作为团队执行伙伴",
		"完成后请在最终答复中简洁总结",
		"只能使用以下五个标题",
		"[Team workspace:",
		"Team Workspace",
	} {
		if strings.Contains(content, unexpected) {
			t.Fatalf("dispatch content still embeds %q: %q", unexpected, content)
		}
	}
	for _, expected := range []string{
		"[Assigned task]: 实现页面",
		"完成首页布局并验证构建。",
		"=== Completed blocker task results ===",
		"设计稿",
		"设计已确认",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("dispatch content missing %q: %q", expected, content)
		}
	}
}

func TestPendingMemberReportProjectsBeforeTheTaskBoardSettles(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "early-member-report.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get SQL database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	channels := controllers.NewChannelController(database.DB)
	threads := controllers.NewChannelThreadController(database.DB)
	tasks := controllers.NewChannelTaskController(database.DB)
	transactions := controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	channelID := "11111111111111111111111111111111"
	threadID := "22222222222222222222222222222222"
	leadID := "33333333333333333333333333333333"
	memberID := "44444444444444444444444444444444"
	leadChatID := "55555555555555555555555555555555"
	memberChatID := "66666666666666666666666666666666"
	if err := channels.Create(ctx, models.Channel{
		ID: channelID, Name: "报告顺序团队", LeadWakerID: leadID,
	}, []models.ChannelMember{
		{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: channelID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	readyAt := int64(1)
	if err := threads.Create(ctx, models.ChannelThread{
		ID: threadID, ChannelID: channelID, Title: "先报告再评审", LeadWakerID: leadID,
		LeadChatID: leadChatID, WorkspaceDirectory: "/tmp/report-order", MaxParallel: 3,
		MaxTasks: 12, MaxFailureAttempts: 2, ReadyAt: &readyAt,
	}, []models.ChannelThreadMember{
		{ThreadID: threadID, WakerID: leadID, DisplayName: "前端工程师", ChatID: leadChatID, IsLead: true, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ThreadID: threadID, WakerID: memberID, DisplayName: "Aa", ChatID: memberChatID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("create thread: %v", err)
	}
	completedTask := models.ChannelTask{
		ID: "77777777777777777777777777777777", ThreadID: threadID,
		AssigneeWakerID: memberID, Subject: "创建页面", Description: "创建 HelloWorld 页面",
		Status: constants.ChannelTaskStatusCompleted, Result: "页面已创建并验证。", ChangedPaths: []string{},
	}
	pendingReview := models.ChannelTask{
		ID: "88888888888888888888888888888888", ThreadID: threadID,
		AssigneeWakerID: leadID, Subject: "前端评审", Description: "评审成员实现",
		Status: constants.ChannelTaskStatusPending, ChangedPaths: []string{},
	}
	for _, task := range []models.ChannelTask{completedTask, pendingReview} {
		if err := tasks.Create(ctx, task, nil); err != nil {
			t.Fatalf("create task %s: %v", task.ID, err)
		}
	}
	if err := tasks.CreateNotification(ctx, models.ChannelLeadNotification{
		ID: "99999999999999999999999999999999", ThreadID: threadID,
		TaskID: completedTask.ID, AttemptID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Status: constants.ChannelNotificationStatusPending, Payload: completedTask.Result,
	}); err != nil {
		t.Fatalf("create member report: %v", err)
	}
	chatFake := &postTurnChatFake{
		summaries: map[string]chatservice.ChatSummary{
			leadChatID: {Chat: models.Chat{ID: leadChatID}},
		},
	}
	executionFake := &postTurnExecutionFake{}
	service := &service{
		channels: channels, threads: threads, tasks: tasks, transactions: transactions,
		chats: chatFake, executions: executionFake, capabilities: channeltool.NewRegistry(),
	}
	if err := service.drainNotifications(ctx, threadID); err != nil {
		t.Fatalf("drain notifications: %v", err)
	}
	if len(chatFake.participantMessages) != 1 ||
		chatFake.participantMessages[0].Content != completedTask.Result {
		t.Fatalf("projected reports = %+v, want completed member report before review starts", chatFake.participantMessages)
	}
	if executionFake.startCount() != 0 {
		t.Fatalf("leader summary executions = %d, want wait until task board settles", executionFake.startCount())
	}
}

func TestSingleUserReadyReportIsDeliveredWithoutLeadExecution(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "direct-delivery.db"), Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get SQL database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	ctx := context.Background()
	channels := controllers.NewChannelController(database.DB)
	threads := controllers.NewChannelThreadController(database.DB)
	tasks := controllers.NewChannelTaskController(database.DB)
	transactions := controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	threadID := "12121212121212121212121212121212"
	channelID := "23232323232323232323232323232323"
	leadID := "34343434343434343434343434343434"
	memberID := "45454545454545454545454545454545"
	leadChatID := "56565656565656565656565656565656"
	memberChatID := "67676767676767676767676767676767"
	taskID := "78787878787878787878787878787878"
	attemptID := "89898989898989898989898989898989"
	result := "完整结果：已核验三项资料，并附上来源。"
	if err := channels.Create(ctx, models.Channel{
		ID: channelID, Name: "直达团队", LeadWakerID: leadID,
	}, []models.ChannelMember{
		{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: channelID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	readyAt := int64(1)
	if err := threads.Create(ctx, models.ChannelThread{
		ID: threadID, ChannelID: channelID, LeadWakerID: leadID, LeadChatID: leadChatID,
		WorkspaceDirectory: "/tmp/direct-delivery", MaxParallel: 3, MaxTasks: 12,
		MaxFailureAttempts: 2, ReadyAt: &readyAt,
	}, []models.ChannelThreadMember{
		{ThreadID: threadID, WakerID: leadID, DisplayName: "负责人", ChatID: leadChatID, IsLead: true, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ThreadID: threadID, WakerID: memberID, DisplayName: "成员", ChatID: memberChatID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if err := tasks.Create(ctx, models.ChannelTask{
		ID: taskID, ThreadID: threadID, AssigneeWakerID: memberID,
		Subject: "单项交付", Description: "提交完整答复", Status: constants.ChannelTaskStatusCompleted,
		DirectDelivery: true, CurrentFence: 1, Result: result, ChangedPaths: []string{},
	}, nil); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := tasks.CreateAttempt(ctx, models.ChannelTaskAttempt{
		ID: attemptID, TaskID: taskID, ChatID: memberChatID, AssigneeWakerID: memberID,
		ExecutionID: "abababababababababababababababab", Fence: 1,
		Reason: constants.ChannelAttemptReasonInitial, Status: constants.ExecutionStatusCompleted,
		Result: result, UserReady: true,
	}); err != nil {
		t.Fatalf("create attempt: %v", err)
	}
	if err := tasks.CreateNotification(ctx, models.ChannelLeadNotification{
		ID: attemptID, ThreadID: threadID, TaskID: taskID, AttemptID: attemptID,
		Status: constants.ChannelNotificationStatusPending, Payload: result,
	}); err != nil {
		t.Fatalf("create notification: %v", err)
	}
	chatFake := &postTurnChatFake{summaries: map[string]chatservice.ChatSummary{
		leadChatID: {Chat: models.Chat{ID: leadChatID}},
	}}
	executionFake := &postTurnExecutionFake{}
	results := controllers.NewWorkResultController(database.DB)
	if err := results.EnableTracking(ctx, 1); err != nil {
		t.Fatal(err)
	}
	service := &service{
		channels: channels, threads: threads, tasks: tasks, transactions: transactions,
		chats: chatFake, executions: executionFake,
		results: results,
		prepareDirectResult: func(_ context.Context, chatID, messageID string) (*models.WorkResultReceipt, error) {
			if chatID != leadChatID || messageID != attemptID || len(chatFake.participantMessages) != 1 {
				t.Fatal("必须先提交原成员报告，才能定位交付结果")
			}
			return &models.WorkResultReceipt{LatestVersion: 10, Reference: models.WorkResultReference{ChatID: chatID, MessageID: messageID}}, nil
		},
	}
	directIDs, err := tasks.ListDirectDeliveredNotificationIDs(ctx, threadID, []string{attemptID})
	if err != nil || len(directIDs) != 0 {
		t.Fatalf("report marked direct before completion: ids=%v error=%v", directIDs, err)
	}
	entry := announceEntry{TaskID: taskID, AttemptID: attemptID, Content: result}
	for _, variant := range []struct {
		// name 标识直达判定的测试场景。
		name string
		// entries 保存本场景待交付的结果通知。
		entries []announceEntry
		// task 保存供直达判定核对的任务事实。
		task models.ChannelTask
	}{
		{name: "not selected by lead", entries: []announceEntry{entry}, task: models.ChannelTask{ID: taskID, Status: constants.ChannelTaskStatusCompleted, CurrentFence: 1}},
		{name: "needs human review", entries: []announceEntry{entry}, task: models.ChannelTask{ID: taskID, Status: constants.ChannelTaskStatusCompleted, DirectDelivery: true, RequiresHumanReview: true, CurrentFence: 1}},
		{name: "failed", entries: []announceEntry{entry}, task: models.ChannelTask{ID: taskID, Status: constants.ChannelTaskStatusFailed, DirectDelivery: true, CurrentFence: 1}},
		{name: "truncated", entries: []announceEntry{{TaskID: taskID, AttemptID: attemptID, Content: result, Truncated: true}}, task: models.ChannelTask{ID: taskID, Status: constants.ChannelTaskStatusCompleted, DirectDelivery: true, CurrentFence: 1}},
		{name: "multiple reports", entries: []announceEntry{entry, entry}, task: models.ChannelTask{ID: taskID, Status: constants.ChannelTaskStatusCompleted, DirectDelivery: true, CurrentFence: 1}},
	} {
		ready, readyErr := service.directDeliveryReady(ctx, variant.entries, []models.ChannelTask{variant.task})
		if readyErr != nil || ready {
			t.Fatalf("%s: direct delivery = %t, error = %v", variant.name, ready, readyErr)
		}
	}
	ready, err := service.directDeliveryReady(ctx, []announceEntry{entry}, []models.ChannelTask{
		{ID: taskID, Status: constants.ChannelTaskStatusCompleted, DirectDelivery: true, CurrentFence: 1},
		{ID: "other-review", Status: constants.ChannelTaskStatusInReview},
	})
	if err != nil || ready {
		t.Fatalf("another task awaits review: direct delivery = %t, error = %v", ready, err)
	}
	if err := tasks.UpdateAttemptFields(ctx, attemptID, map[string]interface{}{"user_ready": false}); err != nil {
		t.Fatalf("clear member confirmation: %v", err)
	}
	ready, err = service.directDeliveryReady(ctx, []announceEntry{entry}, []models.ChannelTask{{ID: taskID, Status: constants.ChannelTaskStatusCompleted, DirectDelivery: true, CurrentFence: 1}})
	if err != nil || ready {
		t.Fatalf("member did not confirm: direct delivery = %t, error = %v", ready, err)
	}
	if err := tasks.UpdateAttemptFields(ctx, attemptID, map[string]interface{}{"user_ready": true}); err != nil {
		t.Fatalf("restore member confirmation: %v", err)
	}
	for range 2 {
		if err := service.drainNotifications(ctx, threadID); err != nil {
			t.Fatalf("drain notifications: %v", err)
		}
	}
	if executionFake.startCount() != 0 {
		t.Fatalf("lead executions = %d, want no summary", executionFake.startCount())
	}
	if len(chatFake.participantMessages) != 1 || chatFake.participantMessages[0].Content != result {
		t.Fatalf("directly delivered report = %+v", chatFake.participantMessages)
	}
	pending, err := tasks.ListPendingNotifications(ctx, threadID, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending notifications = %+v, error = %v", pending, err)
	}
	directIDs, err = tasks.ListDirectDeliveredNotificationIDs(ctx, threadID, []string{attemptID})
	if err != nil || len(directIDs) != 1 || directIDs[0] != attemptID {
		t.Fatalf("directly delivered IDs = %v, error = %v", directIDs, err)
	}
	events, err := threads.ListEvents(ctx, threadID, "", 20)
	if err != nil {
		t.Fatalf("list direct delivery events: %v", err)
	}
	directEvents := 0
	for _, event := range events {
		if event.Type == constants.ChannelEventMemberDirectDelivered {
			directEvents++
		}
	}
	if directEvents != 1 {
		t.Fatalf("direct delivery events = %d, want one", directEvents)
	}
	receipt, found, err := results.Get(ctx, constants.WorkResultOwnerKindTeamThread, threadID)
	if err != nil || !found || receipt.LatestVersion != 10 || receipt.ViewedVersion != 0 || receipt.Reference.MessageID != attemptID {
		t.Fatalf("直达交付未形成团队未读结果: %+v, %v", receipt, err)
	}
	for _, event := range events {
		if event.Type == constants.ChannelEventMemberDirectDelivered && receipt.AvailableAt != event.CreatedAt {
			t.Fatal("结果发布时间必须来自已提交的交付事件")
		}
	}
	service.chats = &timelineHistoryFake{resources: []messageview.Message{
		{
			ID: attemptID, Role: constants.MessageRoleUser,
			Actor:   string(constants.ChannelMessageActorMemberReport),
			Content: result, SourceWakerID: memberID, CreatedAt: 100, CreatedSequence: 1,
		},
		{
			ID: "other-report", Role: constants.MessageRoleUser,
			Actor:   string(constants.ChannelMessageActorMemberReport),
			Content: "仍需负责人汇总的报告", SourceWakerID: memberID, CreatedAt: 200, CreatedSequence: 2,
		},
	}}
	timeline, err := service.ReadTimeline(ctx, TimelineQuery{ThreadID: threadID, Limit: 10})
	if err != nil || len(timeline.Resources) != 2 ||
		!timeline.Resources[0].DirectDelivered || timeline.Resources[1].DirectDelivered {
		t.Fatalf("direct and summary report timeline = %+v, error = %v", timeline.Resources, err)
	}
	stored, err := tasks.Get(ctx, taskID)
	if err != nil || stored.Status != constants.ChannelTaskStatusCompleted {
		t.Fatalf("task status = %s, error = %v", stored.Status, err)
	}
}

func TestMemberReportUsesMCPContentAndKeepsFinalAsConciseAcknowledgement(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "member-mcp-report.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get SQL database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	channels := controllers.NewChannelController(database.DB)
	threads := controllers.NewChannelThreadController(database.DB)
	tasks := controllers.NewChannelTaskController(database.DB)
	transactions := controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	channelID := "10101010101010101010101010101010"
	threadID := "20202020202020202020202020202020"
	leadID := "30303030303030303030303030303030"
	memberID := "40404040404040404040404040404040"
	leadChatID := "50505050505050505050505050505050"
	memberChatID := "60606060606060606060606060606060"
	taskID := "70707070707070707070707070707070"
	attemptID := "80808080808080808080808080808080"
	executionID := "90909090909090909090909090909090"
	if err := channels.Create(ctx, models.Channel{
		ID: channelID, Name: "成员报告团队", LeadWakerID: leadID,
	}, []models.ChannelMember{
		{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: channelID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	readyAt := int64(1)
	if err := threads.Create(ctx, models.ChannelThread{
		ID: threadID, ChannelID: channelID, Title: "MCP 真实报告", LeadWakerID: leadID,
		LeadChatID: leadChatID, WorkspaceDirectory: "/tmp/member-report", MaxParallel: 3,
		MaxTasks: 12, MaxFailureAttempts: 2, ReadyAt: &readyAt,
	}, []models.ChannelThreadMember{
		{ThreadID: threadID, WakerID: leadID, DisplayName: "负责人", ChatID: leadChatID, IsLead: true, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ThreadID: threadID, WakerID: memberID, DisplayName: "成员", ChatID: memberChatID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if err := tasks.Create(ctx, models.ChannelTask{
		ID: taskID, ThreadID: threadID, AssigneeWakerID: memberID,
		Subject: "核验实现", Description: "完成实现并向负责人报告。",
		Status: constants.ChannelTaskStatusInProgress, DirectDelivery: true,
		CurrentFence: 1, ChangedPaths: []string{},
	}, nil); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := tasks.CreateAttempt(ctx, models.ChannelTaskAttempt{
		ID: attemptID, TaskID: taskID, ChatID: memberChatID, ExecutionID: executionID,
		AssigneeWakerID: memberID, Fence: 1, Reason: constants.ChannelAttemptReasonInitial,
		Status: constants.ExecutionStatusRunning,
	}); err != nil {
		t.Fatalf("create attempt: %v", err)
	}
	chatFake := &postTurnChatFake{summaries: map[string]chatservice.ChatSummary{
		leadChatID:   {Chat: models.Chat{ID: leadChatID}},
		memberChatID: {Chat: models.Chat{ID: memberChatID, ActiveExecutionID: executionID}},
	}}
	service := &service{
		channels: channels, threads: threads, tasks: tasks, transactions: transactions,
		chats: chatFake, executions: &postTurnExecutionFake{}, capabilities: channeltool.NewRegistry(),
		lifecycleContext: ctx,
	}
	_, err = service.capabilities.MintMember(
		threadID, memberID, executionID, taskID, attemptID,
	)
	if err != nil {
		t.Fatalf("mint member capability: %v", err)
	}
	denied, err := service.toolChannelTasks(
		ctx,
		channeltool.MemberEndpointKey(threadID, memberID),
		channeltool.TaskInput{Action: constants.ChannelTaskToolActionList},
	)
	if err == nil || denied.Message == "" || len(denied.Tasks) != 0 {
		t.Fatalf("member task-board access output = %+v, error = %v", denied, err)
	}
	detailedReport := "已完成实现与核验；相关测试通过，未发现阻塞问题。"
	output, err := service.toolChannelTasks(
		ctx,
		channeltool.MemberEndpointKey(threadID, memberID),
		channeltool.TaskInput{
			Action:       constants.ChannelTaskToolActionReport,
			Text:         detailedReport,
			ReadyForUser: true,
		},
	)
	if err != nil || output.Message != "" || !output.Reported || output.NextStep == "" {
		t.Fatalf("member report output = %+v, error = %v", output, err)
	}
	storedAttempt, err := tasks.GetCurrentAttempt(ctx, taskID, 1)
	if err != nil || !storedAttempt.UserReady || storedAttempt.Result != detailedReport {
		t.Fatalf("stored member report = %+v, error = %v", storedAttempt, err)
	}
	if len(chatFake.participantMessages) != 1 ||
		chatFake.participantMessages[0].ID != attemptID ||
		chatFake.participantMessages[0].Content != detailedReport {
		t.Fatalf("immediate leader report projection = %+v", chatFake.participantMessages)
	}
	chatFake.mutex.Lock()
	leadSummary := chatFake.summaries[leadChatID]
	leadSummary.Chat.ActiveExecutionID = "review-running"
	chatFake.summaries[leadChatID] = leadSummary
	chatFake.mutex.Unlock()
	if err := service.handleAttemptTerminal(models.ChannelTaskAttempt{
		ID: attemptID, TaskID: taskID, ChatID: memberChatID, ExecutionID: executionID,
		AssigneeWakerID: memberID, Fence: 1,
	}, messageview.Message{
		ExecutionID: executionID,
		Status:      constants.ExecutionStatusCompleted,
		DisplayParts: []messageview.MessageDisplayPart{{
			Type: constants.MessageDisplayPartText,
			Text: "已完成核验并提交报告。",
		}},
	}); err != nil {
		t.Fatalf("commit member terminal: %v", err)
	}
	completed, err := tasks.Get(ctx, taskID)
	if err != nil || completed.Status != constants.ChannelTaskStatusCompleted ||
		completed.Result != detailedReport {
		t.Fatalf("completed task = %+v, error = %v", completed, err)
	}
	notifications, err := tasks.ListPendingNotifications(ctx, threadID, 10)
	if err != nil || len(notifications) != 1 ||
		notifications[0].ID != attemptID || notifications[0].Payload != detailedReport {
		t.Fatalf("member report notifications = %+v, error = %v", notifications, err)
	}

	fallbackTaskID := "a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0"
	fallbackAttemptID := "b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0"
	fallbackExecutionID := "c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0"
	if err := tasks.Create(ctx, models.ChannelTask{
		ID: fallbackTaskID, ThreadID: threadID, AssigneeWakerID: memberID,
		Subject: "兼容旧成员", Description: "未调用 report 时仍提交 final。",
		Status: constants.ChannelTaskStatusInProgress, CurrentFence: 1, ChangedPaths: []string{},
	}, nil); err != nil {
		t.Fatalf("create fallback task: %v", err)
	}
	if err := tasks.CreateAttempt(ctx, models.ChannelTaskAttempt{
		ID: fallbackAttemptID, TaskID: fallbackTaskID, ChatID: memberChatID,
		ExecutionID: fallbackExecutionID, AssigneeWakerID: memberID, Fence: 1,
		Reason: constants.ChannelAttemptReasonInitial, Status: constants.ExecutionStatusRunning,
	}); err != nil {
		t.Fatalf("create fallback attempt: %v", err)
	}
	fallbackReport := "未能调用团队报告工具，但实现与完整验证均已完成。"
	if err := service.handleAttemptTerminal(models.ChannelTaskAttempt{
		ID: fallbackAttemptID, TaskID: fallbackTaskID, ChatID: memberChatID,
		ExecutionID: fallbackExecutionID, AssigneeWakerID: memberID, Fence: 1,
	}, messageview.Message{
		ExecutionID: fallbackExecutionID,
		Status:      constants.ExecutionStatusCompleted,
		DisplayParts: []messageview.MessageDisplayPart{{
			Type: constants.MessageDisplayPartText,
			Text: fallbackReport,
		}},
	}); err != nil {
		t.Fatalf("commit fallback terminal: %v", err)
	}
	fallbackTask, err := tasks.Get(ctx, fallbackTaskID)
	if err != nil || fallbackTask.Result != fallbackReport {
		t.Fatalf("fallback task = %+v, error = %v", fallbackTask, err)
	}
	notifications, err = tasks.ListPendingNotifications(ctx, threadID, 10)
	if err != nil || len(notifications) != 2 ||
		!notificationAttemptExists(notifications, fallbackAttemptID) {
		t.Fatalf("fallback notifications = %+v, error = %v", notifications, err)
	}
}
