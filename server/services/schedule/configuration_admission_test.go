package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	channelservice "github.com/JieWaZi/NeuxBot/server/services/channel"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// configurationAdmissionExecutions 替代原生派发及可变化的默认选择，持久接纳仍走真实调度服务。
type configurationAdmissionExecutions struct {
	// defaults 表示当前智能体设置，不属于已接纳的机会。
	defaults execution.ExecutionConfiguration
	// cancel 使第一次派发在接纳结果未知时取消，保留机会供恢复。
	cancel context.CancelFunc
	// intents 保存真正交给执行入口的输入配置。
	intents []execution.WorkIntent
	// states 保存已成功派发的首次执行权威收据。
	states map[string]chat.InitialExecutionState
}

// ResolveConfiguration 在首次接纳时解析默认值，已经解析的空值保持原生默认。
func (b *configurationAdmissionExecutions) ResolveConfiguration(_ context.Context, value execution.ExecutionConfiguration) (execution.ExecutionConfiguration, error) {
	if !value.Resolved {
		if value.AccountID == "" {
			value.AccountID = b.defaults.AccountID
		}
		if value.Model == "" {
			value.Model = b.defaults.Model
		}
		if value.Reasoning == "" {
			value.Reasoning = b.defaults.Reasoning
		}
		if value.Fast == nil && b.defaults.Fast != nil {
			fast := *b.defaults.Fast
			value.Fast = &fast
		}
	}
	value.Resolved = true
	return value, nil
}

// ValidateConfiguration 复用同一解析规则，但不保存计划尚未接纳的默认值。
func (b *configurationAdmissionExecutions) ValidateConfiguration(ctx context.Context, value execution.ExecutionConfiguration) error {
	_, err := b.ResolveConfiguration(ctx, value)
	return err
}

// Start 保留可靠派发身份，第一次取消不伪造已接纳的原生收据。
func (b *configurationAdmissionExecutions) Start(_ context.Context, intent execution.WorkIntent) (execution.AcceptedExecution, error) {
	b.intents = append(b.intents, intent)
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
		return execution.AcceptedExecution{}, status_error.OperationCanceled
	}
	id := (requestidempotency.Identity{ID: intent.ClientRequestID}).ResourceID("chat", intent.Chat.Create.WakerID)
	accepted := execution.AcceptedExecution{Chat: models.Chat{ID: id, WakerID: intent.Chat.Create.WakerID, ActiveExecutionID: intent.ExecutionID}, Execution: models.Execution{ID: intent.ExecutionID, Status: constants.ExecutionStatusRunning}}
	b.states[id] = chat.InitialExecutionState{Chat: accepted.Chat, CreateRequestID: intent.ClientRequestID, InitialExecution: &accepted.Execution}
	return accepted, nil
}

// configurationAdmissionChats 只替代执行权威读取，任务和机会使用真实临时数据库。
type configurationAdmissionChats struct {
	// Chats 提供本测试不调用的聚合删除接口。
	Chats
	// executions 提供派发后已有的首次执行收据。
	executions *configurationAdmissionExecutions
}

// ReadInitialExecution 明确区分尚未派发与已接纳的机会。
func (b configurationAdmissionChats) ReadInitialExecution(_ context.Context, id string) (chat.InitialExecutionState, error) {
	if state, found := b.executions.states[id]; found {
		return state, nil
	}
	return chat.InitialExecutionState{}, status_error.ChatNotFound
}

// TestPendingRunKeepsAdmissionConfiguration 验证计划不提前固定默认，已创建机会恢复不重新继承当前设置。
func TestPendingRunKeepsAdmissionConfiguration(t *testing.T) {
	on, off := true, false
	for _, selection := range []struct {
		// name 说明模型与 Fast 的接纳选择。
		name string
		// nativeDefault 表示接纳时模型与强度使用原生默认。
		nativeDefault bool
		// fast 是接纳时默认 Fast；nil 表示原生默认。
		fast *bool
	}{
		{name: "默认关闭后开启", fast: &off},
		{name: "默认开启后关闭", nativeDefault: true, fast: &on},
		{name: "原生默认后配置开启", nativeDefault: true},
	} {
		t.Run(selection.name, func(t *testing.T) {
			nativeDefault := selection.nativeDefault
			db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "schedule.db"), Migrate: true})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := db.DB.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sqlDB.Close() })
			wakers := controllers.NewWakerController(db.DB)
			if err := wakers.Create(t.Context(), models.Waker{ID: "waker", Name: "test"}); err != nil {
				t.Fatal(err)
			}
			planFast := !nativeDefault
			boundary := &configurationAdmissionExecutions{defaults: execution.ExecutionConfiguration{AccountID: constants.AiAccountSystemID, Model: "plan-time-model", Reasoning: "low", Fast: &planFast}, states: map[string]chat.InitialExecutionState{}}
			service := New(Config{Tasks: controllers.NewAutomaticTaskController(db.DB), Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)), Wakers: wakers, Executions: boundary, Chats: configurationAdmissionChats{executions: boundary}})
			service.ctx = t.Context()
			saved, err := service.SaveConfiguration(t.Context(), "waker", "", 0, models.AutomaticTaskConfiguration{Name: "task", Instruction: "run", Trigger: constants.AutomaticTaskTriggerAPI, RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelDefault, Enabled: true}, true)
			if err != nil {
				t.Fatal(err)
			}
			task, err := service.config.Tasks.Get(t.Context(), saved.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			if task.Model != "" || task.Reasoning != "" || task.Fast != nil {
				t.Fatalf("plan prematurely froze runtime defaults: %+v", task.AutomaticTaskConfiguration)
			}
			fast := selection.fast
			boundary.defaults.Fast = fast
			model, reasoning := "admission-model", "high"
			if nativeDefault {
				model, reasoning = "", ""
			}
			boundary.defaults.Model, boundary.defaults.Reasoning = model, reasoning
			firstContext, cancel := context.WithCancel(t.Context())
			defer cancel()
			boundary.cancel = cancel
			command := AdmissionCommand{Task: task, Source: constants.AutomaticRunSourceManual, OccurrenceKey: "opportunity", RequestID: "request"}
			if _, err := service.Admit(firstContext, command); !errors.Is(err, status_error.OperationCanceled) {
				t.Fatalf("first dispatch did not leave a recoverable opportunity: %v", err)
			}
			pending, exists, err := service.config.Tasks.FindRun(t.Context(), task.ID, command.OccurrenceKey)
			if err != nil || !exists || pending.Admission != constants.AutomaticRunAdmissionPending || pending.Configuration.Model != model || pending.Configuration.Reasoning != reasoning || pending.Configuration.AccountID != constants.AiAccountSystemID || (pending.Configuration.Fast == nil) != (fast == nil) || fast != nil && *pending.Configuration.Fast != *fast {
				t.Fatalf("opportunity did not persist its effective configuration: %+v exists=%v error=%v", pending, exists, err)
			}
			boundary.defaults.Model, boundary.defaults.Reasoning = "later-model", "max"
			laterFast := true
			if fast != nil {
				laterFast = !*fast
			}
			boundary.defaults.Fast = &laterFast
			recovered := New(service.config)
			recovered.ctx = t.Context()
			if err := recovered.recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			run, err := recovered.Admit(t.Context(), command)
			if err != nil {
				t.Fatal(err)
			}
			if run.ID != pending.ID || len(boundary.intents) != 2 {
				t.Fatalf("recovery changed opportunity identity: %+v intents=%d", run, len(boundary.intents))
			}
			for _, intent := range boundary.intents {
				if !intent.Options.ConfigurationResolved || intent.Options.Model != model || intent.Options.Reasoning != reasoning || intent.Options.AccountID != constants.AiAccountSystemID || (intent.Options.Fast == nil) != (fast == nil) || fast != nil && *intent.Options.Fast != *fast {
					t.Fatalf("pending dispatch re-inherited runtime defaults: %+v", intent.Options)
				}
			}
		})
	}
}

// configurationAdmissionTeams 只替代团队派发，机会配置和团队成员快照仍写入真实仓储。
type configurationAdmissionTeams struct {
	// cancel 让第一次派发保留尚未接纳的持久机会。
	cancel context.CancelFunc
	// inputs 保存每次真正派发的成员配置。
	inputs []channelservice.CreateThreadInput
	// threads 保存恢复时能够核对的团队对话收据。
	threads controllers.ChannelThreadController
	// states 提供已接纳 Chat 的权威首次执行收据。
	states map[string]chat.InitialExecutionState
}

// SubmitMessage 模拟原生派发取消或成功，保留调度服务的真实接纳与恢复流程。
func (b *configurationAdmissionTeams) SubmitMessage(ctx context.Context, value channelservice.SubmitMessageInput) (channelservice.SubmitMessageResult, error) {
	input := *value.Create
	b.inputs = append(b.inputs, input)
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
		return channelservice.SubmitMessageResult{}, status_error.OperationCanceled
	}
	ready := int64(1)
	thread := models.ChannelThread{ID: util.New(), ChannelID: input.ChannelID, LeadWakerID: "lead", LeadChatID: util.New(), ReadyAt: &ready, ClientRequestID: &input.ClientRequestID, AutomaticRunID: input.AutomaticRunID}
	members := []models.ChannelThreadMember{}
	for index, config := range input.MemberConfigs {
		chatID := util.New()
		if config.WakerID == thread.LeadWakerID {
			chatID = thread.LeadChatID
		}
		members = append(members, models.ChannelThreadMember{ThreadID: thread.ID, WakerID: config.WakerID, ChatID: chatID,
			RuntimeID: config.RuntimeID, AccountID: config.AccountID, Model: config.Model, Reasoning: config.Reasoning,
			PermissionLevel: config.PermissionLevel, Position: index, IsLead: config.WakerID == thread.LeadWakerID})
		b.states[chatID] = chat.InitialExecutionState{Chat: models.Chat{ID: chatID, WakerID: config.WakerID}}
	}
	if err := b.threads.Create(ctx, thread, members); err != nil {
		return channelservice.SubmitMessageResult{}, err
	}
	executed := models.Execution{ID: input.ExecutionID, ChatID: thread.LeadChatID, Status: constants.ExecutionStatusRunning}
	b.states[thread.LeadChatID] = chat.InitialExecutionState{Chat: models.Chat{ID: thread.LeadChatID, WakerID: "lead", ActiveExecutionID: input.ExecutionID}, InitialExecution: &executed}
	return channelservice.SubmitMessageResult{Thread: &channelservice.ThreadResult{Thread: thread, Members: members, LeadExecution: &executed}}, nil
}

func TestPendingTeamRunFreezesOverridesAndInheritedNativeDefaults(t *testing.T) {
	on, off := true, false
	for _, fast := range []*bool{nil, &on, &off} {
		t.Run(fmt.Sprintf("fast=%v", fast), func(t *testing.T) {

			database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "team-admission.db"), Migrate: true})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := database.DB.DB()
			t.Cleanup(func() { _ = sqlDB.Close() })
			ctx := t.Context()
			wakers := controllers.NewWakerController(database.DB)
			for _, id := range []string{"lead", "member"} {
				if err := wakers.Create(ctx, models.Waker{ID: id, Name: id}); err != nil {
					t.Fatal(err)
				}
			}
			accounts := controllers.NewAIAccountController(database.DB)
			oldAccountID, futureAccountID := util.New(), util.New()
			for _, id := range []string{oldAccountID, futureAccountID} {
				if err := accounts.Create(ctx, models.AIAccount{ID: id, Name: id, AuthMode: constants.AccountAuthModeAPIKey,
					Endpoint:       models.AIAccountEndpoint{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL},
					SelectedModels: []string{"plan-model", "admission-model", "future-model"}, DefaultModel: "plan-model"}, "test-key"); err != nil {
					t.Fatal(err)
				}
			}
			environments := controllers.NewRuntimeEnvironmentController(database.DB)
			environment := models.RuntimeEnvironment{ID: util.New(), RuntimeID: "codex", BaseRuntimeID: "codex", Enabled: true,
				DefaultAccountID: oldAccountID, DefaultModel: "plan-model", DefaultReasoning: "low"}
			if err := environments.Ensure(ctx, environment); err != nil {
				t.Fatal(err)
			}
			transactions := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
			projects := controllers.NewProjectController(database.DB)
			storage := t.TempDir()
			journal, err := chat.New(chat.Config{StorageDirectory: storage, Projects: projects,
				QueryIndexes: chat.QueryIndexDependencies{Chats: controllers.NewChatController(database.DB), Transactions: transactions, DeletionIntents: controllers.NewChatDeletionIntentController(database.DB)}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(journal.Close)
			resolver, err := execution.New(execution.Config{Journal: journal, Threads: &agentapi.ThreadLifecycle{},
				ResolveAccount: func(_ context.Context, request agentapi.AccountRequest, _ string) (agentapi.AccountRequest, error) {
					return request, nil
				},
				Accounts: aiaccount.New(accounts), Wakers: wakers, Profiles: controllers.NewProfileDocumentController(database.DB), Projects: projects,
				RuntimeEnvironments: environments, StorageDirectory: storage,
				RuntimeSummaries: []agentapi.RuntimeSummary{{ID: "codex", BaseRuntimeID: "codex", InstructionFileName: "AGENTS.md"}}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(resolver.Close)
			channels := controllers.NewChannelController(database.DB)
			team := models.Channel{ID: "team", Name: "已保存成员配置", LeadWakerID: "lead"}
			members := []models.ChannelMember{
				{ChannelID: team.ID, WakerID: "lead", RuntimeID: "codex", AccountID: constants.AiAccountSystemID, PermissionLevel: constants.PermissionLevelDefault},
				{ChannelID: team.ID, WakerID: "member", RuntimeID: "codex", AccountID: constants.AiAccountSystemID, Model: "saved-member", PermissionLevel: constants.PermissionLevelDefault, Position: 1},
			}
			if err := channels.Create(ctx, team, members); err != nil {
				t.Fatal(err)
			}
			threads := controllers.NewChannelThreadController(database.DB)
			stateOwner := &configurationAdmissionExecutions{states: map[string]chat.InitialExecutionState{}}
			teams := &configurationAdmissionTeams{threads: threads, states: stateOwner.states}
			service := New(Config{Tasks: controllers.NewAutomaticTaskController(database.DB), Transactions: transactions, Wakers: wakers, Projects: projects,
				Executions: resolver, Chats: configurationAdmissionChats{executions: stateOwner}, Channels: channels, Threads: threads,
				TeamTasks: controllers.NewChannelTaskController(database.DB), Teams: teams})
			service.ctx = ctx
			saved, err := service.SaveConfiguration(ctx, "lead", "", 0, models.AutomaticTaskConfiguration{Name: "冻结团队机会", Instruction: "run", Trigger: constants.AutomaticTaskTriggerAPI,
				ChannelID: team.ID, Enabled: true, MemberConfigs: []models.AutomaticTaskMemberConfiguration{{WakerID: "member", RuntimeID: "codex", PermissionLevel: constants.PermissionLevelDefault}}}, true)
			if err != nil {
				t.Fatal(err)
			}
			task, err := service.config.Tasks.Get(ctx, saved.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			if len(task.MemberConfigs) != 1 || task.MemberConfigs[0].AccountID != "" || task.MemberConfigs[0].Model != "" {
				t.Fatalf("任务保存提前解析了未来机会覆盖: %+v", task.MemberConfigs)
			}
			environment.DefaultModel, environment.DefaultReasoning, environment.DefaultFast = "admission-model", "high", fast
			if err := environments.UpdateConfiguration(ctx, environment); err != nil {
				t.Fatal(err)
			}
			firstContext, cancel := context.WithCancel(ctx)
			defer cancel()
			teams.cancel = cancel
			command := AdmissionCommand{Task: task, Source: constants.AutomaticRunSourceManual, OccurrenceKey: "team-opportunity", RequestID: "request"}
			if _, err := service.Admit(firstContext, command); !errors.Is(err, status_error.OperationCanceled) {
				t.Fatalf("首次派发没有保留可恢复机会: %v", err)
			}
			pending, exists, err := service.config.Tasks.FindRun(ctx, task.ID, command.OccurrenceKey)
			if err != nil || !exists || len(pending.Configuration.MemberConfigs) != 2 {
				t.Fatalf("机会没有冻结完整成员配置: %+v exists=%v error=%v", pending.Configuration.MemberConfigs, exists, err)
			}
			for _, member := range pending.Configuration.MemberConfigs {
				expectedFast := fast
				if member.WakerID == "lead" {
					expectedFast = nil
				}
				data, _ := json.Marshal(member)
				var snapshot struct {
					// Fast 是从真实机会载荷读取的可空加速选择。
					Fast *bool `json:"fast"`
				}
				if err := json.Unmarshal(data, &snapshot); err != nil {
					t.Fatal(err)
				}
				if (snapshot.Fast == nil) != (expectedFast == nil) || expectedFast != nil && *snapshot.Fast != *expectedFast {
					t.Fatalf("机会未冻结成员 Fast: %s want=%v", data, expectedFast)
				}
				if member.WakerID == "lead" && (member.AccountID != constants.AiAccountSystemID || member.Model != "" || member.Reasoning != "") {
					t.Fatalf("继承的已解析原生空值被默认覆盖: %+v", member)
				}
				if member.WakerID == "member" && (member.AccountID != oldAccountID || member.Model != "admission-model" || member.Reasoning != "high") {
					t.Fatalf("未解析覆盖没有采用接纳时默认值: %+v", member)
				}
			}
			laterFast := fast == nil || !*fast
			environment.DefaultFast = &laterFast
			environment.DefaultAccountID, environment.DefaultModel, environment.DefaultReasoning = futureAccountID, "future-model", "max"
			if err := environments.UpdateConfiguration(ctx, environment); err != nil {
				t.Fatal(err)
			}
			members[0].Model, members[0].Reasoning = "future-native", "high"
			if err := channels.Update(ctx, team, members); err != nil {
				t.Fatal(err)
			}
			recovered := New(service.config)
			recovered.ctx = ctx
			view, err := recovered.Admit(ctx, command)
			if err != nil || view.ID != pending.ID || len(teams.inputs) != 2 {
				t.Fatalf("恢复改变机会身份或失败: %+v calls=%d error=%v", view, len(teams.inputs), err)
			}
			for _, input := range teams.inputs {
				for _, member := range input.MemberConfigs {
					expectedFast := fast
					if member.WakerID == "lead" {
						expectedFast = nil
					}
					if (member.Fast == nil) != (expectedFast == nil) || expectedFast != nil && *member.Fast != *expectedFast {
						t.Fatalf("恢复派发改变 Fast: %+v want=%v", member, expectedFast)
					}
				}
				if !input.ConfigurationResolved {
					t.Fatal("已接纳的团队成员配置未标记为明确选择")
				}
				if !reflect.DeepEqual(input.MemberConfigs, teamMemberOverrides(pending.Configuration)) {
					t.Fatalf("恢复派发改写了已接纳成员选择: %+v", input.MemberConfigs)
				}
			}
		})
	}
}
