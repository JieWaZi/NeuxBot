package channel

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
	channeltool "github.com/JieWaZi/NeuxBot/server/internal/teamcapability"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// taskBoardFailure 在真实团队适配链中注入仓储稳定错误。
type taskBoardFailure struct {
	// ChannelTaskController 保留未使用的仓储端口，误调用会直接暴露测试错误。
	controllers.ChannelTaskController
	// failure 是读取任务板时原样返回的稳定故障。
	failure error
}

func (f taskBoardFailure) ListByThread(context.Context, string) ([]models.ChannelTask, error) {
	return nil, f.failure
}

func TestChannelTaskCapabilityRequiresBoardReadAndRejectsLeadAssignee(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "channel-capability.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get SQL database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := sqlDB.Close(); closeErr != nil {
			t.Errorf("close test database: %v", closeErr)
		}
	})

	channels := controllers.NewChannelController(database.DB)
	threads := controllers.NewChannelThreadController(database.DB)
	tasks := controllers.NewChannelTaskController(database.DB)
	wakers := controllers.NewWakerController(database.DB)
	transactions := controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	ctx := context.Background()
	channelID := "11111111111111111111111111111111"
	threadID := "22222222222222222222222222222222"
	leadID := "33333333333333333333333333333333"
	memberID := "44444444444444444444444444444444"
	replacementID := "55555555555555555555555555555555"
	for _, waker := range []models.Waker{
		{
			ID:       leadID,
			Name:     "林负责人",
			RoleName: "技术负责人",
			Bio:      "擅长后端架构与跨领域交付协调。",
			Expertise: []profile.Expertise{
				{Title: "后端架构", Description: "设计服务边界、持久化和并发控制。"},
			},
		},
		{
			ID:       memberID,
			Name:     "周前端",
			RoleName: "前端工程师",
			Bio:      "负责桌面端界面与交互实现。",
			Expertise: []profile.Expertise{
				{Title: "前端交互", Description: "设计并实现可访问的桌面端组件。"},
			},
			WorkStyle: []string{"PRIVATE_WORK_STYLE_SENTINEL"},
			WorkMethods: []profile.WorkMethod{
				{Name: "PRIVATE_WORK_METHOD_SENTINEL", Steps: []string{"不得进入 roster"}},
			},
		},
		{
			ID:       replacementID,
			Name:     "陈后端",
			RoleName: "后端工程师",
			Bio:      "负责接口实现和数据库测试。",
			Expertise: []profile.Expertise{
				{Title: "Go 服务", Description: "实现后端 API 与任务调度。"},
			},
		},
	} {
		if err := wakers.Create(ctx, waker); err != nil {
			t.Fatalf("create waker %s: %v", waker.ID, err)
		}
	}
	if err := channels.Create(ctx, models.Channel{
		ID:          channelID,
		Name:        "交付团队",
		LeadWakerID: leadID,
	}, []models.ChannelMember{
		{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: channelID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
		{ChannelID: channelID, WakerID: replacementID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 2},
	}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	thread := models.ChannelThread{
		ID:                 threadID,
		ChannelID:          channelID,
		Title:              "完成团队模式",
		LeadWakerID:        leadID,
		LeadChatID:         "lead-chat",
		WorkspaceDirectory: "/tmp/workspace",
		MaxParallel:        3,
		MaxTasks:           12,
		MaxFailureAttempts: 2,
		ReadyAt:            testTimestamp(1),
	}
	if err := threads.Create(ctx, thread, []models.ChannelThreadMember{
		{
			ThreadID: threadID, WakerID: leadID, DisplayName: "林负责人", RoleName: "技术负责人",
			Bio: "擅长后端架构与跨领域交付协调。", Expertise: []profile.Expertise{
				{Title: "后端架构", Description: "设计服务边界、持久化和并发控制。"},
			},
			ChatID: "lead-chat", IsLead: true, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto,
		},
		{
			ThreadID: threadID, WakerID: memberID, DisplayName: "周前端", RoleName: "前端工程师",
			Bio: "负责桌面端界面与交互实现。", Expertise: []profile.Expertise{
				{Title: "前端交互", Description: "设计并实现可访问的桌面端组件。"},
			},
			ChatID: "member-chat", RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1,
		},
		{
			ThreadID: threadID, WakerID: replacementID, DisplayName: "陈后端", RoleName: "后端工程师",
			Bio: "负责接口实现和数据库测试。", Expertise: []profile.Expertise{
				{Title: "Go 服务", Description: "实现后端 API 与任务调度。"},
			},
			ChatID: "replacement-chat", RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 2,
		},
	}); err != nil {
		t.Fatalf("create channel thread: %v", err)
	}

	chatFake := &postTurnChatFake{
		summaries: map[string]chatservice.ChatSummary{
			"lead-chat":        {Chat: models.Chat{ID: "lead-chat", ActiveExecutionID: "lead-running"}},
			"member-chat":      {Chat: models.Chat{ID: "member-chat"}},
			"replacement-chat": {Chat: models.Chat{ID: "replacement-chat", ActiveExecutionID: "member-running"}},
		},
		leadChatID:        "lead-chat",
		subscribeFailures: map[string]int{"lead-chat": 1},
		leadTerminal: chatservice.PublicEvent{Message: &messageview.Message{
			ExecutionID: "lead-running",
			Status:      constants.ExecutionStatusCompleted,
		}},
	}
	executionFake := &postTurnExecutionFake{}
	lifecycleContext, lifecycleCancel := context.WithCancel(ctx)
	service := &service{
		channels:         channels,
		threads:          threads,
		tasks:            tasks,
		transactions:     transactions,
		wakers:           wakers,
		chats:            chatFake,
		executions:       executionFake,
		lifecycleContext: lifecycleContext,
		lifecycleCancel:  lifecycleCancel,
		watchers:         make(map[string]struct{}),
		capabilities:     channeltool.NewRegistry(),
	}
	t.Cleanup(service.Close)
	leadCapability, leadCapabilityToken, err := service.mintLeadCapability(ctx, thread, "lead-running", true)
	if err != nil {
		t.Fatalf("mint lead capability: %v", err)
	}
	if len(leadCapability.Bindings) != 8 {
		t.Fatalf("lead operation scope = %+v", leadCapability)
	}
	readonly, readonlyLease, err := service.mintLeadCapability(ctx, thread, "lead-running", false)
	if err != nil || len(readonly.Bindings) != 3 {
		t.Fatalf("readonly scope = %+v %v", readonly, err)
	}
	service.capabilities.Revoke(readonlyLease)
	teamMD, err := service.teamInstruction(ctx, thread, leadID)
	if err != nil {
		t.Fatalf("build lead TEAM.md: %v", err)
	}
	service.capabilities.Revoke(leadCapabilityToken)
	for _, expected := range []string{
		"Collaboration Priority",
		"Role: Lead",
		"**you**",
		"`" + leadID + "`",
		"assigneeWakerId",
		"Waker ID shown in backticks",
		memberID,
		"前端工程师",
		"负责桌面端界面与交互实现。",
		"前端交互: 设计并实现可访问的桌面端组件。",
		"Match tasks semantically to member roles, biographies, and expertise",
		"the lead cannot be a task assignee",
		"The final summary is the lead's coordination closeout after receiving member results",
		"Describe exactly one clear deliverable per task",
		"Build the complete task graph in one pass",
		"allycli guide team.lead",
		"Team Workspace",
		"/tmp/workspace",
	} {
		if !strings.Contains(teamMD.Content, expected) {
			t.Fatalf("lead TEAM.md missing %q:\n%s", expected, teamMD.Content)
		}
	}
	for _, privateProjection := range []string{
		"PRIVATE_WORK_STYLE_SENTINEL",
		"PRIVATE_WORK_METHOD_SENTINEL",
		"不得进入 roster",
	} {
		if strings.Contains(teamMD.Content, privateProjection) {
			t.Fatalf("lead TEAM.md leaked unrelated profile field %q", privateProjection)
		}
	}
	memberMD, err := service.teamInstruction(ctx, thread, memberID)
	if err != nil {
		t.Fatalf("build member TEAM.md: %v", err)
	}
	for _, unexpected := range []string{
		"## Workflow",
		"`" + string(constants.ChannelTaskToolActionCreate) + "`",
	} {
		if strings.Contains(memberMD.Content, unexpected) {
			t.Fatalf("member TEAM.md unexpectedly contains lead workflow %q:\n%s", unexpected, memberMD.Content)
		}
	}
	for _, expected := range []string{
		"Role: Member",
		"## allycli team tasks",
		"`report`",
		"complete result in `report.text`",
		"Never repeat the report",
		"readyForUser=true",
		"factual final response",
		"system commits task status automatically after the run ends",
	} {
		if !strings.Contains(memberMD.Content, expected) {
			t.Fatalf("member TEAM.md missing %q:\n%s", expected, memberMD.Content)
		}
	}
	_, err = service.capabilities.Mint(threadID, leadID, "lead-running", true)
	if err != nil {
		t.Fatalf("mint capability: %v", err)
	}
	t.Run("CLI preserves repository failure classification", func(t *testing.T) {
		original := service.tasks
		defer func() { service.tasks = original }()
		scope, _ := service.capabilities.Resolve(threadID)
		for _, cause := range []error{status_error.DatabaseBusy, status_error.DatabaseError, status_error.DatabaseOperationCancelled} {
			service.tasks = taskBoardFailure{ChannelTaskController: original, failure: cause}
			for _, binding := range service.agentTeamBindings(threadID, scope.LeaseID) {
				if binding.Operation != "team.tasks.list" {
					continue
				}
				_, err := binding.Call(ctx, agentcapability.Request{Operation: binding.Operation, Params: map[string]any{}})
				if !errors.Is(err, cause) {
					t.Fatalf("repository classification lost: wanted %v got %v", cause, err)
				}
				got := agentcapability.ErrorResponse(binding.Operation, err)
				expected := agentcapability.ErrorResponse(binding.Operation, cause)
				if got.Error.Code != expected.Error.Code || got.Error.Message != "The task board is temporarily unavailable." {
					t.Fatalf("unsafe or reclassified failure: %+v", got.Error)
				}
			}
		}
	})
	createInput := channeltool.TaskInput{
		Action:          "create",
		AssigneeWakerID: memberID,
		Subject:         "实现任务面板",
		Description:     "实现并验证任务面板。",
	}
	output, err := service.toolChannelTasks(ctx, threadID, createInput)
	if err == nil || output.Message == "" {
		t.Fatalf("create before list output = %+v, error = %v, want tool error", output, err)
	}
	output, err = service.toolChannelTasks(ctx, threadID, channeltool.TaskInput{Action: "list"})
	if err != nil || output.Message != "" || len(output.Tasks) != 0 {
		t.Fatalf("list output = %+v, error = %v", output, err)
	}
	output, err = service.toolChannelTasks(ctx, threadID, createInput)
	if err != nil || output.Message != "" || output.Task == nil ||
		output.Task.SenderWakerID != leadID || output.Task.AssigneeWakerID != memberID {
		t.Fatalf("create output = %+v, error = %v", output, err)
	}
	if output.Task.Status != constants.ChannelTaskStatusPending {
		t.Fatalf("created task status = %s, want pending", output.Task.Status)
	}
	if _, err := tasks.GetCurrentAttempt(ctx, output.Task.ID, output.Task.CurrentFence); err == nil {
		t.Fatal("tool-created task dispatched before the lead turn ended")
	}
	createdTaskID := output.Task.ID
	youAliasInput := channeltool.TaskInput{
		Action:          constants.ChannelTaskToolActionCreate,
		AssigneeWakerID: "you",
		Subject:         "不应接受 you 别名",
		Description:     "assigneeWakerId 必须使用 Members 反引号中的 Waker ID。",
	}
	output, err = service.toolChannelTasks(ctx, threadID, youAliasInput)
	if err == nil || output.Message == "" || output.Task != nil {
		t.Fatalf("create with you alias output = %+v, error = %v, want tool error", output, err)
	}
	if !strings.Contains(output.Message, "Waker ID") {
		t.Fatalf("create with you alias message = %q, want Waker ID guidance", output.Message)
	}
	priority := 7
	output, err = service.toolChannelTasks(ctx, threadID, channeltool.TaskInput{
		Action:      "update",
		TaskID:      createdTaskID,
		Subject:     "实现并验证任务面板",
		Description: "实现任务面板并完成回归验证。",
		Priority:    &priority,
	})
	if err != nil || output.Message != "" || output.Task == nil ||
		output.Task.Subject != "实现并验证任务面板" || output.Task.Priority != priority {
		t.Fatalf("update output = %+v, error = %v", output, err)
	}
	output, err = service.toolChannelTasks(ctx, threadID, channeltool.TaskInput{
		Action:          "update",
		TaskID:          createdTaskID,
		AssigneeWakerID: leadID,
	})
	if err == nil || output.Message == "" || output.Task != nil {
		t.Fatalf("update to lead output = %+v, error = %v, want tool error", output, err)
	}
	unchangedTask, err := tasks.Get(ctx, createdTaskID)
	if err != nil || unchangedTask.AssigneeWakerID != memberID {
		t.Fatalf("task after rejected lead update = %+v, error = %v", unchangedTask, err)
	}

	failedTask := models.ChannelTask{
		ID:              "66666666666666666666666666666666",
		ThreadID:        threadID,
		AssigneeWakerID: memberID,
		Subject:         "首次尝试失败",
		Description:     "由负责人决定更换伙伴后重试。",
		Status:          constants.ChannelTaskStatusFailed,
		CurrentFence:    1,
	}
	if err := tasks.Create(ctx, failedTask, nil); err != nil {
		t.Fatalf("create failed task: %v", err)
	}
	if err := tasks.CreateAttempt(ctx, models.ChannelTaskAttempt{
		ID:              "77777777777777777777777777777777",
		TaskID:          failedTask.ID,
		ChatID:          "member-chat",
		ExecutionID:     "88888888888888888888888888888888",
		AssigneeWakerID: memberID,
		Fence:           1,
		Reason:          constants.ChannelAttemptReasonInitial,
		Status:          constants.ExecutionStatusFailed,
	}); err != nil {
		t.Fatalf("create failed attempt: %v", err)
	}
	output, err = service.toolChannelTasks(ctx, threadID, channeltool.TaskInput{
		Action:          "retry",
		TaskID:          failedTask.ID,
		AssigneeWakerID: leadID,
	})
	if err == nil || output.Message == "" || output.Task != nil {
		t.Fatalf("retry with lead output = %+v, error = %v, want tool error", output, err)
	}
	failedAfterLeadRetry, err := tasks.Get(ctx, failedTask.ID)
	if err != nil || failedAfterLeadRetry.Status != constants.ChannelTaskStatusFailed ||
		failedAfterLeadRetry.AssigneeWakerID != memberID {
		t.Fatalf("task after rejected lead retry = %+v, error = %v", failedAfterLeadRetry, err)
	}
	output, err = service.toolChannelTasks(ctx, threadID, channeltool.TaskInput{
		Action:          "retry",
		TaskID:          failedTask.ID,
		AssigneeWakerID: replacementID,
	})
	if err != nil || output.Message != "" || output.Task == nil ||
		output.Task.Status != constants.ChannelTaskStatusPending ||
		output.Task.AssigneeWakerID != replacementID {
		t.Fatalf("retry and reassign output = %+v, error = %v", output, err)
	}
	createInput.AssigneeWakerID = leadID
	createInput.Subject = "不应创建的负责人汇总任务"
	createInput.Description = "最终汇总必须由负责人协调回合直接完成。"
	output, err = service.toolChannelTasks(ctx, threadID, createInput)
	if err == nil || output.Message == "" || output.Task != nil {
		t.Fatalf("lead assignee output = %+v, error = %v, want tool error", output, err)
	}
	service.watchLeadExecution(thread, "lead-running", nil, nil, "")
	deadline := time.Now().Add(2 * time.Second)
	for executionFake.startCount() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if executionFake.startCount() != 1 {
		t.Fatalf("post-turn member executions = %d, want 1", executionFake.startCount())
	}
	chatFake.mutex.Lock()
	leadSummary := chatFake.summaries[thread.LeadChatID]
	leadSummary.Chat.ActiveExecutionID = "read-only-running"
	chatFake.summaries[thread.LeadChatID] = leadSummary
	chatFake.mutex.Unlock()
	_, err = service.capabilities.Mint(threadID, leadID, "read-only-running", false)
	if err != nil {
		t.Fatalf("mint read-only capability: %v", err)
	}
	if output, err = service.toolChannelTasks(ctx, threadID, channeltool.TaskInput{Action: "list"}); err != nil || output.Message != "" {
		t.Fatalf("read-only list output = %+v, error = %v", output, err)
	}
	createInput.AssigneeWakerID = memberID
	output, err = service.toolChannelTasks(ctx, threadID, createInput)
	if err == nil || output.Message == "" {
		t.Fatalf("read-only create output = %+v, error = %v, want tool error", output, err)
	}
	chatFake.mutex.Lock()
	leadSummary = chatFake.summaries[thread.LeadChatID]
	leadSummary.Chat.ActiveExecutionID = "mutation-running"
	chatFake.summaries[thread.LeadChatID] = leadSummary
	chatFake.mutex.Unlock()
	_, err = service.capabilities.Mint(threadID, leadID, "mutation-running", true)
	if err != nil {
		t.Fatalf("mint mutation capability: %v", err)
	}
	if output, err = service.toolChannelTasks(ctx, threadID, channeltool.TaskInput{Action: "list"}); err != nil || output.Message != "" {
		t.Fatalf("mutation list output = %+v, error = %v", output, err)
	}

	coordinatedTask := models.ChannelTask{
		ID:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ThreadID:        threadID,
		AssigneeWakerID: memberID,
		Subject:         "负责人补充成员任务",
		Description:     "验证内部作者语义",
		Status:          constants.ChannelTaskStatusInProgress,
		CurrentFence:    1,
		ChangedPaths:    []string{},
	}
	if err := tasks.Create(ctx, coordinatedTask, nil); err != nil {
		t.Fatalf("create coordinated task: %v", err)
	}
	output, err = service.toolChannelTasks(ctx, threadID, channeltool.TaskInput{
		Action: "follow_up",
		TaskID: coordinatedTask.ID,
		Text:   "负责人要求补充验收证据",
	})
	if err != nil || output.Message != "" {
		t.Fatalf("lead follow-up output = %+v, error = %v", output, err)
	}
	followUps, err := tasks.ListPendingFollowUps(ctx, coordinatedTask.ID)
	if err != nil || len(followUps) != 1 || followUps[0].Actor != constants.ChannelMessageActorLead {
		t.Fatalf("lead follow-ups = %+v, error = %v", followUps, err)
	}
	workInput, err := taskWorkInput(coordinatedTask, nil, followUps)
	if err != nil || workInput.Actor != string(constants.ChannelMessageActorLead) {
		t.Fatalf("lead follow-up work input = %+v, error = %v", workInput, err)
	}
	if strings.Contains(workInput.Content, "[Team workspace:") {
		t.Fatalf("dispatch content still embeds workspace hint: %q", workInput.Content)
	}

	reviewTask := models.ChannelTask{
		ID:                  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ThreadID:            threadID,
		AssigneeWakerID:     memberID,
		Subject:             "等待用户验收",
		Description:         "负责人不得取消",
		Status:              constants.ChannelTaskStatusInReview,
		RequiresHumanReview: true,
		ChangedPaths:        []string{},
	}
	if err := tasks.Create(ctx, reviewTask, nil); err != nil {
		t.Fatalf("create review task: %v", err)
	}
	output, err = service.toolChannelTasks(ctx, threadID, channeltool.TaskInput{
		Action: "cancel",
		TaskID: reviewTask.ID,
	})
	if err == nil || output.Message == "" {
		t.Fatalf("lead cancel in-review output = %+v, error = %v", output, err)
	}
	unchanged, getErr := tasks.Get(ctx, reviewTask.ID)
	if getErr != nil || unchanged.Status != constants.ChannelTaskStatusInReview {
		t.Fatalf("in-review task after lead cancel = %+v, error = %v", unchanged, getErr)
	}
}

// TestLeaderWorkOptionsPreserveSelectedPermission 验证协调回合不在用户权限之外叠加只读限制。
func TestLeaderWorkOptionsPreserveSelectedPermission(t *testing.T) {
	for _, runtimeID := range []string{"claude", "codex"} {
		for _, level := range []constants.PermissionLevel{constants.PermissionLevelDefault, constants.PermissionLevelAuto, constants.PermissionLevelFullAccess} {
			t.Run(runtimeID+"/"+string(level), func(t *testing.T) {
				for _, direct := range []bool{false, true} {
					options := leaderWorkOptions(runtimeID, "model", "default", level, execution.ToolCapability{}, runtimecontract.Instruction{}, direct)
					if options.PermissionLevel != level || options.WorkspaceReadOnly {
						t.Fatalf("direct=%t selected permission=%s, actual permission=%s workspaceReadOnly=%t", direct, level, options.PermissionLevel, options.WorkspaceReadOnly)
					}
				}
			})
		}
	}
}
