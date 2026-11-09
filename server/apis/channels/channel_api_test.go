package channels

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	channelservice "github.com/JieWaZi/NeuxBot/server/services/channel"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// channelConfigExecutionFake 仅提供团队配置写入所需的统一执行配置校验。
type channelConfigExecutionFake struct {
	// Service 补齐本测试不使用的执行接口方法。
	execution.Service
}

// ResolveConfiguration 保留本用例明确构造的选择，不引入默认变化。
func (*channelConfigExecutionFake) ResolveConfiguration(_ context.Context, config execution.ExecutionConfiguration) (execution.ExecutionConfiguration, error) {
	return config, nil
}

// apiTestTimestamp 返回团队 API 测试模型使用的可选毫秒时间。
func apiTestTimestamp(value int64) *int64 { return &value }

func TestChannelAPICreatesConfigurationWithoutChats(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "channel-api.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := sqlDB.Close(); closeErr != nil {
			t.Errorf("close test database: %v", closeErr)
		}
	})

	previousWakers := global.WakerControllerManager
	previousChannels := global.ChannelControllerManager
	previousTransactions := global.TransactionControllerManager
	previousExecutionService := global.ExecutionService
	t.Cleanup(func() {
		global.WakerControllerManager = previousWakers
		global.ChannelControllerManager = previousChannels
		global.TransactionControllerManager = previousTransactions
		global.ExecutionService = previousExecutionService
	})
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.ChannelControllerManager = controllers.NewChannelController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	global.ExecutionService = &channelConfigExecutionFake{}

	ctx, _ := gin.CreateTestContext(nil)
	now := time.Now().UnixMilli()
	lead := models.Waker{
		ID:          util.New(),
		Name:        "负责人",
		RoleName:    "架构师",
		Avatar:      "avatar:lead",
		Bio:         "负责协调",
		Expertise:   nil,
		WorkStyle:   nil,
		WorkMethods: nil,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	member := models.Waker{
		ID:          util.New(),
		Name:        "成员",
		RoleName:    "开发者",
		Avatar:      "avatar:member",
		Bio:         "负责执行",
		Expertise:   nil,
		WorkStyle:   nil,
		WorkMethods: nil,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := global.WakerControllerManager.Create(ctx, lead); err != nil {
		t.Fatalf("create lead: %v", err)
	}
	if err := global.WakerControllerManager.Create(ctx, member); err != nil {
		t.Fatalf("create member: %v", err)
	}

	create := CreateChannel{Body: ChannelInput{
		Name:        " 交付团队 ",
		Description: " 团队说明 ",
		LeadWakerID: lead.ID,
		Members: []ChannelMemberInput{
			{
				WakerID:         member.ID,
				RuntimeID:       "codex",
				AccountID:       "system",
				Model:           "gpt-5.6",
				Reasoning:       "high",
				PermissionLevel: constants.PermissionLevelAuto,
			},
			{
				WakerID:         lead.ID,
				RuntimeID:       "claude",
				Model:           "sonnet",
				Reasoning:       "",
				PermissionLevel: constants.PermissionLevelDefault,
			},
		},
	}}
	if err := create.Validate(ctx); err != nil {
		t.Fatalf("validate create: %v", err)
	}
	createdValue, err := create.Output(ctx)
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	created := createdValue.(ChannelDetail)
	if created.Channel.Name != "交付团队" || created.Channel.LeadWakerID != lead.ID {
		t.Fatalf("unexpected channel: %+v", created.Channel)
	}
	if len(created.Members) != 2 || created.Members[0].AccountID != "system" || created.Members[0].Waker.ID != member.ID || created.Members[1].Waker.ID != lead.ID {
		t.Fatalf("unexpected ordered members: %+v", created.Members)
	}
	var chatCount int64
	if err := database.DB.Model(&models.Chat{}).Count(&chatCount).Error; err != nil {
		t.Fatalf("count chats: %v", err)
	}
	var threadCount int64
	if err := database.DB.Model(&models.ChannelThread{}).Count(&threadCount).Error; err != nil {
		t.Fatalf("count threads: %v", err)
	}
	if chatCount != 0 || threadCount != 0 {
		t.Fatalf("create channel produced chats=%d threads=%d", chatCount, threadCount)
	}

}

func TestChannelInputRequiresTwoDistinctMembersAndExplicitLead(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	base := ChannelMemberInput{
		WakerID:         "member-one",
		RuntimeID:       "codex",
		PermissionLevel: constants.PermissionLevelDefault,
	}
	tests := []struct {
		// name 是子测试名称。
		name string
		// input 是待校验团队配置。
		input ChannelInput
		// expected 是期望返回的稳定状态错误。
		expected error
	}{
		{
			name: "one member",
			input: ChannelInput{
				Name:        "团队",
				LeadWakerID: base.WakerID,
				Members:     []ChannelMemberInput{base},
			},
			expected: status_error.ChannelMemberCountInvalid,
		},
		{
			name: "duplicate members",
			input: ChannelInput{
				Name:        "团队",
				LeadWakerID: base.WakerID,
				Members:     []ChannelMemberInput{base, base},
			},
			expected: status_error.ChannelMemberCountInvalid,
		},
		{
			name: "lead outside members",
			input: ChannelInput{
				Name:        "团队",
				LeadWakerID: "missing-lead",
				Members: []ChannelMemberInput{
					base,
					{
						WakerID:         "member-two",
						RuntimeID:       "codex",
						PermissionLevel: constants.PermissionLevelDefault,
					},
				},
			},
			expected: status_error.ChannelLeadInvalid,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := CreateChannel{Body: test.input}
			if err := request.Validate(ctx); !errors.Is(err, test.expected) {
				t.Fatalf("Validate() error = %v, want %v", err, test.expected)
			}
		})
	}
}

// channelConfigurationExecution 只替换原生启动与 Chat 组准备，配置解析仍使用真实 Execution Core。
type channelConfigurationExecution struct {
	// Service 提供真实账号和默认值解析。
	execution.Service
	// chats 保存为团队生成的独立 Chat 摘要。
	chats *reviewChannelChatFake
	// prepared 保存物化前传入的成员配置快照。
	prepared []execution.PrepareChatMember
	// starts 保存团队实际交给执行入口的请求。
	starts []execution.WorkIntent
}

// PrepareChatGroup 生成隔离的成员 Chat，避免测试启动真实 CLI。
func (f *channelConfigurationExecution) PrepareChatGroup(_ context.Context, input execution.PrepareChatGroupIntent) (execution.PreparedChatGroup, error) {
	result := execution.PreparedChatGroup{}
	f.prepared = append(f.prepared, input.Members...)
	for _, member := range input.Members {
		value := models.Chat{ID: member.ChatID, WakerID: member.WakerID, Title: input.Title, WorkspaceDirectory: input.Scope.WorkingDirectory}
		result.Chats = append(result.Chats, value)
		f.chats.summaries[value.ID] = chatservice.ChatSummary{Chat: value}
	}
	return result, nil
}

// Start 记录真正派发的参数，实际原生运行不属于配置保存测试。
func (f *channelConfigurationExecution) Start(_ context.Context, input execution.WorkIntent) (execution.AcceptedExecution, error) {
	f.starts = append(f.starts, input)
	return execution.AcceptedExecution{Execution: models.Execution{ID: input.ExecutionID, ChatID: input.Chat.ChatID, Status: constants.ExecutionStatusRunning}}, nil
}

func TestChannelAPIFreezesResolvedDefaultsBeforeDispatch(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "saved-defaults.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	previousWakers, previousChannels := global.WakerControllerManager, global.ChannelControllerManager
	previousThreads, previousTasks := global.ChannelThreadControllerManager, global.ChannelTaskControllerManager
	previousTransactions, previousExecutions := global.TransactionControllerManager, global.ExecutionService
	t.Cleanup(func() {
		global.WakerControllerManager, global.ChannelControllerManager = previousWakers, previousChannels
		global.ChannelThreadControllerManager, global.ChannelTaskControllerManager = previousThreads, previousTasks
		global.TransactionControllerManager, global.ExecutionService = previousTransactions, previousExecutions
	})
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.ChannelControllerManager = controllers.NewChannelController(database.DB)
	global.ChannelThreadControllerManager = controllers.NewChannelThreadController(database.DB)
	global.ChannelTaskControllerManager = controllers.NewChannelTaskController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	ctx, _ := gin.CreateTestContext(nil)
	leadID, memberID := util.New(), util.New()
	for _, id := range []string{leadID, memberID} {
		if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	accounts := controllers.NewAIAccountController(database.DB)
	oldAccountID, nextAccountID := util.New(), util.New()
	for _, id := range []string{oldAccountID, nextAccountID} {
		if err := accounts.Create(ctx, models.AIAccount{ID: id, Name: id, AuthMode: constants.AccountAuthModeAPIKey,
			Endpoint:       models.AIAccountEndpoint{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL},
			SelectedModels: []string{"old-model", "next-model", "later-model"}, DefaultModel: "old-model"}, "test-key"); err != nil {
			t.Fatal(err)
		}
	}
	environments := controllers.NewRuntimeEnvironmentController(database.DB)
	fastOn, fastOff := true, false
	managed := models.RuntimeEnvironment{DefaultFast: &fastOn, ID: util.New(), RuntimeID: "codex", BaseRuntimeID: "codex", Enabled: true,
		DefaultAccountID: oldAccountID, DefaultModel: "old-model", DefaultReasoning: "high"}
	native := models.RuntimeEnvironment{DefaultFast: &fastOff, ID: util.New(), RuntimeID: "claude", BaseRuntimeID: "claude", Enabled: true}
	for _, environment := range []models.RuntimeEnvironment{managed, native} {
		if err := environments.Ensure(ctx, environment); err != nil {
			t.Fatal(err)
		}
	}
	storage := t.TempDir()
	projects := controllers.NewProjectController(database.DB)
	journal, err := chatservice.New(chatservice.Config{StorageDirectory: storage, Projects: projects,
		QueryIndexes: chatservice.QueryIndexDependencies{Chats: controllers.NewChatController(database.DB), Transactions: global.TransactionControllerManager, DeletionIntents: controllers.NewChatDeletionIntentController(database.DB)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(journal.Close)
	resolver, err := execution.New(execution.Config{Journal: journal, Threads: &agentapi.ThreadLifecycle{},
		ResolveAccount: func(_ context.Context, request agentapi.AccountRequest, _ string) (agentapi.AccountRequest, error) {
			return request, nil
		},
		Accounts: aiaccount.New(accounts), Wakers: global.WakerControllerManager, Profiles: controllers.NewProfileDocumentController(database.DB),
		Projects: projects, RuntimeEnvironments: environments, StorageDirectory: storage,
		RuntimeSummaries: []agentapi.RuntimeSummary{{ID: "codex", BaseRuntimeID: "codex", InstructionFileName: "AGENTS.md"}, {ID: "claude", BaseRuntimeID: "claude", InstructionFileName: "CLAUDE.md"}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(resolver.Close)
	global.ExecutionService = resolver
	input := ChannelInput{Name: "冻结默认配置", LeadWakerID: leadID, Members: []ChannelMemberInput{
		{WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelDefault},
		{WakerID: memberID, RuntimeID: "claude", AccountID: constants.AiAccountSystemID, PermissionLevel: constants.PermissionLevelDefault},
	}}
	create := CreateChannel{Body: input}
	if err := create.Validate(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := create.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	channelID := result.(ChannelDetail).Channel.ID
	saved, err := global.ChannelControllerManager.ListMembers(ctx, channelID)
	if err != nil {
		t.Fatal(err)
	}
	assertChannelFast(t, saved[0], &fastOn)
	assertChannelFast(t, saved[1], &fastOff)
	if saved[0].AccountID != oldAccountID || saved[0].Model != "old-model" || saved[0].Reasoning != "high" {
		t.Fatalf("创建没有保存已解析默认值: %+v", saved[0])
	}
	if saved[1].AccountID != constants.AiAccountSystemID || saved[1].Model != "" || saved[1].Reasoning != "" {
		t.Fatalf("原生 system 空配置被替换: %+v", saved[1])
	}
	managed.DefaultAccountID, managed.DefaultModel, managed.DefaultReasoning = nextAccountID, "next-model", "low"
	native.DefaultModel, native.DefaultReasoning = "future-native", "high"
	managed.DefaultFast, native.DefaultFast = &fastOff, &fastOn
	for _, environment := range []models.RuntimeEnvironment{managed, native} {
		if err := environments.UpdateConfiguration(ctx, environment); err != nil {
			t.Fatal(err)
		}
	}
	chats := &reviewChannelChatFake{summaries: map[string]chatservice.ChatSummary{}}
	captured := &channelConfigurationExecution{Service: resolver, chats: chats}
	teams, err := channelservice.New(channelservice.Config{Channels: global.ChannelControllerManager, Threads: global.ChannelThreadControllerManager,
		Tasks: global.ChannelTaskControllerManager, Wakers: global.WakerControllerManager, Transactions: global.TransactionControllerManager,
		Chats: chats, Executions: captured, ChatGroups: captured})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(teams.Close)
	thread, err := teams.SubmitMessage(ctx, channelservice.SubmitMessageInput{Create: &channelservice.CreateThreadInput{
		ChannelID: channelID, ClientRequestID: util.New(), WorkingDirectory: t.TempDir(), Input: execution.WorkInput{Content: "继续原配置"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range captured.prepared {
		assertChannelFast(t, member, map[bool]*bool{true: &fastOn, false: &fastOff}[member.WakerID == leadID])
	}
	options := captured.starts[0].Options
	assertChannelFast(t, options, &fastOn)
	if !options.ConfigurationResolved || options.AccountID != oldAccountID || options.Model != "old-model" || options.Reasoning != "high" {
		t.Fatalf("环境默认变化改写了实际派发: %+v", options)
	}
	for _, member := range thread.Thread.Members {
		assertChannelFast(t, member, map[bool]*bool{true: &fastOn, false: &fastOff}[member.WakerID == leadID])
		if member.WakerID == memberID {
			resolved, err := resolver.ResolveConfiguration(ctx, execution.ExecutionConfiguration{Resolved: true, RuntimeID: member.RuntimeID,
				AccountID: member.AccountID, Model: member.Model, Reasoning: member.Reasoning, PermissionLevel: member.PermissionLevel})
			if err != nil || resolved.AccountID != constants.AiAccountSystemID || resolved.Model != "" || resolved.Reasoning != "" {
				t.Fatalf("已保存原生空配置重新继承默认值: %+v error=%v", resolved, err)
			}
		}
	}
	update := UpdateChannel{ChannelID: channelID, Body: input}
	if _, err := update.Output(ctx); err != nil {
		t.Fatal(err)
	}
	saved, err = global.ChannelControllerManager.ListMembers(ctx, channelID)
	if err != nil {
		t.Fatal(err)
	}
	assertChannelFast(t, saved[0], &fastOff)
	if saved[0].AccountID != nextAccountID || saved[0].Model != "next-model" || saved[0].Reasoning != "low" {
		t.Fatalf("修改没有保存当前解析默认值: %+v", saved[0])
	}
	threadUpdate := UpdateChannelThreadMemberConfigs{ChannelID: channelID, ThreadID: thread.Thread.Thread.ID,
		Body: UpdateChannelThreadMemberConfigsRequest{Members: []ChannelMemberInput{{WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelDefault}}}}
	if _, err := threadUpdate.Output(ctx); err != nil {
		t.Fatal(err)
	}
	updated, err := global.ChannelThreadControllerManager.GetMember(ctx, thread.Thread.Thread.ID, leadID)
	assertChannelFast(t, updated, &fastOff)
	if err != nil || updated.AccountID != nextAccountID || updated.Model != "next-model" || updated.Reasoning != "low" {
		t.Fatalf("线程配置没有保存解析默认值: %+v error=%v", updated, err)
	}
	_, err = teams.SubmitMessage(ctx, channelservice.SubmitMessageInput{Create: &channelservice.CreateThreadInput{
		ChannelID: channelID, ClientRequestID: util.New(), WorkingDirectory: t.TempDir(), Input: execution.WorkInput{Content: "覆盖成员配置"},
		MemberConfigs: []channelservice.MemberExecutionConfigInput{{WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelDefault}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	overridden := captured.starts[len(captured.starts)-1].Options
	assertChannelFast(t, overridden, &fastOff)
	if !overridden.ConfigurationResolved || overridden.AccountID != nextAccountID || overridden.Model != "next-model" || overridden.Reasoning != "low" {
		t.Fatalf("首发覆盖没有冻结解析默认值: %+v", overridden)
	}
	frozen, err := teams.SubmitMessage(ctx, channelservice.SubmitMessageInput{Create: &channelservice.CreateThreadInput{
		ConfigurationResolved: true, ChannelID: channelID, ClientRequestID: util.New(), WorkingDirectory: t.TempDir(), Input: execution.WorkInput{Content: "派发已接纳配置"},
		MemberConfigs: []channelservice.MemberExecutionConfigInput{
			{WakerID: leadID, RuntimeID: "codex", AccountID: oldAccountID, Model: "old-model", Reasoning: "high", PermissionLevel: constants.PermissionLevelDefault},
			{WakerID: memberID, RuntimeID: "claude", AccountID: constants.AiAccountSystemID, PermissionLevel: constants.PermissionLevelDefault},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range frozen.Thread.Members {
		assertChannelFast(t, member, nil)
		if member.WakerID == memberID && (member.AccountID != constants.AiAccountSystemID || member.Model != "" || member.Reasoning != "") {
			t.Fatalf("上游冻结的原生空值在团队首发被重解: %+v", member)
		}
	}

	// 真实成员配置更新必须能依次持久化显式开启、显式关闭和 NULL。
	for _, fast := range []*bool{&fastOn, &fastOff, nil} {
		managed.DefaultFast = fast
		if err := environments.UpdateConfiguration(ctx, managed); err != nil {
			t.Fatal(err)
		}
		if _, err := threadUpdate.Output(ctx); err != nil {
			t.Fatal(err)
		}
		updated, err := global.ChannelThreadControllerManager.GetMember(ctx, thread.Thread.Thread.ID, leadID)
		if err != nil {
			t.Fatal(err)
		}
		assertChannelFast(t, updated, fast)
	}
}

// assertChannelFast 检查公开快照与内部派发的 Fast 三态，兼容两个载荷的字段命名。
func assertChannelFast(t *testing.T, value any, expected *bool) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	raw := fields["fast"]
	if len(raw) == 0 {
		raw = fields["Fast"]
	}
	var actual *bool
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &actual); err != nil {
			t.Fatal(err)
		}
	}
	if (actual == nil) != (expected == nil) || expected != nil && *actual != *expected {
		t.Fatalf("Fast mismatch: actual=%s expected=%v payload=%s", raw, expected, data)
	}
}
