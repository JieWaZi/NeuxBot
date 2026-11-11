package automatic_tasks

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/agentfixture"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"

	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	channelservice "github.com/JieWaZi/NeuxBot/server/services/channel"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/schedule"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// integrationRuntime 仅替代原生连接；接纳、Manager、Session、Chat 与结果投影均使用真实实现。
type integrationRuntime struct {
	// requests 交付产品 Session 实际发送的静态配置与增量输入。
	requests chan integrationPrompt
	// finish 由测试释放一次执行，以验证真实活跃 Chat 的并发和删除保护。
	finish chan struct{}
	// calls 为真实 Prompt 派发计数，用于确认同键重放不会再次提交输入。
	calls atomic.Int64
}

// integrationPrompt retains the exact native launch and increment observed by the fixture.
type integrationPrompt struct {
	// model is the selected model at native admission.
	model string
	// cwd is the process working directory.
	cwd string
	// directories contains the native session's additional authorized roots.
	directories []string
	// input contains the exact adapter prompt.
	input agentapi.AgentPromptRequest
}

func (r *integrationRuntime) newClient(_ context.Context, account agentapi.AccountRequest, _ agentapi.ProviderLaunch, _ agentapi.AccountLease, _ agentapi.LaunchAuthLease, _ agentapi.ResourceLease, _ agentapi.AgentClientCallbacks) (agentapi.ProviderClient, error) {
	client := &agentfixture.Client{}
	client.OnPrompt = func(ctx context.Context, request agentapi.AgentPromptRequest) (*string, error) {
		r.calls.Add(1)
		select {
		case r.requests <- integrationPrompt{model: client.Model, cwd: account.CWD, directories: client.Options.AdditionalDirectories, input: request}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		select {
		case <-r.finish:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		text := "归档完成"
		return &text, nil
	}
	return client, nil
}

// integrationEnvironment 提供已探测模型和一个不同的默认账号，验证任务不会隐式重绑定。
type integrationEnvironment struct{}

func (integrationEnvironment) Get(context.Context, string) (models.RuntimeEnvironment, error) {
	// 只有原生连接被替换，测试进程路径用于满足产品会话的可执行路径校验。
	binary, err := os.Executable()
	if err != nil {
		return models.RuntimeEnvironment{}, err
	}
	sdk, err := agentapi.NewCatalog(agentapi.CatalogConfig{})
	if err != nil {
		return models.RuntimeEnvironment{}, err
	}
	var summary agentapi.RuntimeSummary
	for _, registered := range sdk.Runtimes() {
		if registered.ID == "codex" {
			summary = registered
			break
		}
	}
	return models.RuntimeEnvironment{RuntimeID: "codex", BaseRuntimeID: "codex", Enabled: true, Binary: binary, DefaultModel: "model", DefaultAccountID: "another-default-account",
		Detection: agentapi.DetectionResult{Runtime: summary, State: agentapi.DetectionStateReady, ResolvedBinary: binary, Capabilities: agentapi.CapabilitySet{agentapi.CapabilityMCPStreamableHTTP: true, agentapi.CapabilitySkillDirectory: true},
			Models: []agentapi.ModelOption{{ID: "model", PermissionLevels: []agentapi.PermissionLevel{agentapi.PermissionLevelDefault, agentapi.PermissionLevelAuto, agentapi.PermissionLevelFullAccess}}}}}, nil
}

func TestAutomaticEntriesExecuteAndDeleteThroughRealCore(t *testing.T) {
	// 外部模型由替身处理，测试不依赖本机 CLI 或用户原生配置。
	t.Setenv("PATH", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	ctx := context.Background()
	dataDirectory := t.TempDir()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(dataDirectory, "integration.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := database.DB.DB()
	t.Cleanup(func() { _ = sql.Close() })
	transactions := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	wakers := controllers.NewWakerController(database.DB)
	profiles := controllers.NewProfileDocumentController(database.DB)
	projects := controllers.NewProjectController(database.DB)
	wakerID := util.New()
	if err := wakers.Create(ctx, models.Waker{ID: wakerID, Name: "集成伙伴"}); err != nil {
		t.Fatal(err)
	}
	var documents []models.WakerDocument
	for _, kind := range []constants.ProfileDocumentKind{constants.ProfileDocumentKindIdentity, constants.ProfileDocumentKindPersona, constants.ProfileDocumentKindPlaybook, constants.ProfileDocumentKindMemory} {
		documents = append(documents, models.WakerDocument{ID: util.New(), WakerID: wakerID, Kind: kind, Content: "# 测试伙伴"})
	}
	if err := profiles.CreateWakerDocuments(ctx, documents); err != nil {
		t.Fatal(err)
	}
	store, err := conversation.NewStore(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	chats, err := chat.New(chat.Config{StorageDirectory: dataDirectory, Projects: projects, Conversations: store,
		QueryIndexes: chat.QueryIndexDependencies{Chats: controllers.NewChatController(database.DB), Transactions: transactions, DeletionIntents: controllers.NewChatDeletionIntentController(database.DB)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := chats.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chats.Close)
	runtime := &integrationRuntime{requests: make(chan integrationPrompt, 4), finish: make(chan struct{}, 1)}
	sdk, err := agentapi.NewCatalog(agentapi.CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	providers := agentapi.NewProviderRuntime(agentapi.ProviderRuntimeOptions{
		AcquireAccount: func(context.Context, agentapi.AccountRequest) (agentapi.AccountLease, error) {
			return agentapi.AccountLease{}, nil
		},
		NewClient: runtime.newClient,
	})
	runtimeThreads := agentapi.NewThreadLifecycle(providers, store)
	t.Cleanup(func() { _ = runtimeThreads.Close(context.Background()); _ = providers.Close(context.Background()) })
	var teams channelservice.Service
	var service *schedule.Service
	core, err := execution.New(execution.Config{AcquireTeamChatInput: func(ctx context.Context, chatID string, userInput bool) (context.Context, func(), error) {
		if teams == nil {
			return ctx, func() {}, nil
		}
		return teams.AcquireChatInput(ctx, chatID, userInput)
	}, Threads: runtimeThreads, ResolveAccount: agentfixture.ResolveAccount, RuntimeSummaries: sdk.Runtimes(), Journal: chats, DormantChats: chats, Wakers: wakers, Profiles: profiles, Projects: projects, RuntimeEnvironments: integrationEnvironment{}, StorageDirectory: dataDirectory, SkillDataDirectory: dataDirectory})
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(core.Close)
	channels := controllers.NewChannelController(database.DB)
	threads := controllers.NewChannelThreadController(database.DB)
	teamTasks := controllers.NewChannelTaskController(database.DB)
	teams, err = channelservice.New(channelservice.Config{AutomaticRun: controllers.NewAutomaticTaskController(database.DB).GetRun, BeforeUserInput: func(ctx context.Context, threadID string) error {
		if service == nil {
			return nil
		}
		return service.FreezeTeamRun(ctx, threadID)
	}, Channels: channels, Threads: threads, Tasks: teamTasks, Transactions: transactions, Wakers: wakers, Chats: chats, Executions: core, ChatGroups: core})
	if err != nil {
		t.Fatal(err)
	}
	if err := teams.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(teams.Close)
	var now atomic.Int64
	now.Store(time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC).UnixMilli())
	automaticAPI := taskAPI{config: schedule.Config{Tasks: controllers.NewAutomaticTaskController(database.DB), Transactions: transactions, Wakers: wakers, Projects: projects, Executions: core, Chats: chats, Teams: teams, Channels: channels, Threads: threads, TeamTasks: teamTasks, DataDirectory: dataDirectory, APIHandler: NewLocalHandler, Now: func() time.Time { return time.UnixMilli(now.Load()).UTC() }}}
	service = schedule.New(automaticAPI.config)
	automaticAPI.service = service
	// 集成验收使用独立随机回环端口，不竞争用户正在运行的默认入口。
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	portData, _ := json.Marshal(map[string]any{"ip": "127.0.0.1", "port": port, "enabled": true})
	if err := os.WriteFile(filepath.Join(dataDirectory, "integration-api.json"), portData, 0600); err != nil {
		t.Fatal(err)
	}
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { service.Close() })
	config := models.AutomaticTaskConfiguration{Name: "原始归档", Instruction: "原始指令", Trigger: constants.AutomaticTaskTriggerSchedule,
		Schedule:  models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindOnce, Timezone: "UTC", OnceAt: automaticAPI.config.Now().Add(time.Minute).Format(constants.AutomaticTaskLocalTimeLayout)},
		RuntimeID: "codex", AccountID: "system", Model: "model", PermissionLevel: constants.PermissionLevelDefault, Enabled: true}
	invalidScope := config
	invalidScope.ProjectID = util.New()
	invalidScope.WorkingDirectory = t.TempDir()
	if _, err := service.SaveConfiguration(ctx, wakerID, "", 0, invalidScope, true); !errors.Is(err, status_error.AutomaticTaskWorkspaceInvalid) {
		t.Fatalf("项目和目录仍不能同时选择: %v", err)
	}
	invalidScope.ProjectID = ""
	invalidScope.WorkingDirectory = filepath.Join(t.TempDir(), "missing")
	if _, err := service.SaveConfiguration(ctx, wakerID, "", 0, invalidScope, true); !errors.Is(err, status_error.AutomaticTaskWorkspaceInvalid) {
		t.Fatalf("显式选择的失效目录不能自动降级为临时目录: %v", err)
	}
	scheduled, err := service.SaveConfiguration(ctx, wakerID, "", 0, config, true)
	if err != nil {
		t.Fatal(err)
	}
	manual, err := automaticAPI.TriggerManual(ctx, wakerID, scheduled.TaskID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	receive := func(want string) integrationPrompt {
		t.Helper()
		select {
		case request := <-runtime.requests:
			if request.model != "model" || !strings.Contains(request.input.Text, want) {
				t.Fatalf("runtime frozen request: %+v", request)
			}
			return request
		case <-time.After(5 * time.Second):
			t.Fatal("Core did not dispatch Runtime")
		}
		return integrationPrompt{}
	}
	complete := func(taskID, runID string) schedule.RunView {
		t.Helper()
		runtime.finish <- struct{}{}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			runs, _, err := automaticAPI.Runs(ctx, wakerID, taskID, 20, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, run := range runs {
				if run.ID == runID && run.State == constants.AutomaticRunStateCompleted {
					return run
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("real initial Execution did not complete")
		return schedule.RunView{}
	}
	manualPrompt := receive("原始指令")
	if err := automaticAPI.Delete(ctx, wakerID, scheduled.TaskID); util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key()) != status_error.AutomaticTaskBusy.Key() {
		t.Fatalf("active Chat deletion: %v", err)
	}
	if _, err := automaticAPI.TriggerManual(ctx, wakerID, scheduled.TaskID, "busy"); !errors.Is(err, status_error.AutomaticTaskBusy) {
		t.Fatalf("active slot: %v", err)
	}
	task, _ := service.Get(ctx, wakerID, scheduled.TaskID)
	config.Instruction = "后续指令"
	if _, err := service.SaveConfiguration(ctx, wakerID, scheduled.TaskID, task.ConfigurationVersion, config, true); err != nil {
		t.Fatal(err)
	}
	finished := complete(scheduled.TaskID, manual.ID)
	if finished.Configuration.Instruction != "原始指令" {
		t.Fatal("editing overwrote accepted snapshot")
	}
	state, err := chats.ReadInitialExecution(ctx, manual.ChatID)
	if err != nil || !state.Chat.HiddenFromWakerHistory || state.InitialExecution == nil || state.InitialExecution.ID != manual.ExecutionID {
		t.Fatalf("standard hidden Chat authority: %+v %v", state, err)
	}
	if state.Chat.ProjectID != "" || len(state.Chat.AdditionalWorkingDirectories) != 1 || state.Chat.AdditionalWorkingDirectories[0] != state.Chat.WorkspaceDirectory {
		t.Fatalf("未选工作范围的伙伴任务应冻结伙伴工作目录: %+v", state.Chat)
	}
	if info, err := os.Stat(state.Chat.WorkspaceDirectory); err != nil || !info.IsDir() {
		t.Fatalf("伙伴工作目录未创建: %v", err)
	}
	now.Add(time.Minute.Milliseconds())
	if err := service.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	timedPrompt := receive("后续指令")
	history, total, err := automaticAPI.Runs(ctx, wakerID, scheduled.TaskID, 20, 0)
	if err != nil || total != 2 || history[0].Source != constants.AutomaticRunSourceSchedule {
		t.Fatalf("timer opportunity: %+v %v", history, err)
	}
	timed := complete(scheduled.TaskID, history[0].ID)
	timedState, err := chats.ReadInitialExecution(ctx, timed.ChatID)
	if err != nil {
		t.Fatal(err)
	}
	layout, err := chatstorage.NewLayout(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	agentDirectory, err := layout.AgentWorkspace(wakerID)
	if err != nil {
		t.Fatal(err)
	}
	if timedState.Chat.WorkspaceDirectory != agentDirectory || state.Chat.WorkspaceDirectory != agentDirectory {
		t.Fatalf("同一伙伴普通任务应共享固定 cwd: manual=%s timed=%s want=%s", state.Chat.WorkspaceDirectory, timedState.Chat.WorkspaceDirectory, agentDirectory)
	}
	if manual.ChatID == timed.ChatID || manual.ExecutionID == timed.ExecutionID || manualPrompt.input.SessionID == "" || manualPrompt.input.SessionID == timedPrompt.input.SessionID {
		t.Fatalf("共享 cwd 不能共享 Chat、Execution 或原生会话身份: manual=%+v timed=%+v native=%s/%s", manual, timed, manualPrompt.input.SessionID, timedPrompt.input.SessionID)
	}
	manualRoot, err := layout.ChatRoot(manual.ChatID)
	if err != nil {
		t.Fatal(err)
	}
	timedRoot, err := layout.ChatRoot(timed.ChatID)
	if err != nil {
		t.Fatal(err)
	}
	manualLog, err := store.LogPath(manual.ChatID)
	if err != nil {
		t.Fatal(err)
	}
	timedLog, err := store.LogPath(timed.ChatID)
	if err != nil {
		t.Fatal(err)
	}
	if manualRoot == timedRoot || manualLog == timedLog {
		t.Fatalf("聊天私有日志归属被合并: %s/%s %s/%s", manualRoot, timedRoot, manualLog, timedLog)
	}
	// 同名附件放在各自聊天根中；后续真实删除必须只清理所属聊天。
	manualAttachment := filepath.Join(manualRoot, constants.ExecutionWorkspaceAttachmentsDirectory, "private.txt")
	timedAttachment := filepath.Join(timedRoot, constants.ExecutionWorkspaceAttachmentsDirectory, "private.txt")
	for file, body := range map[string]string{manualAttachment: "manual attachment", timedAttachment: "timed attachment"} {
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"outputs", "tmp"} {
		if _, err := os.Stat(filepath.Join(agentDirectory, name)); !os.IsNotExist(err) {
			t.Fatalf("空间不应自动创建 %s: %v", name, err)
		}
	}
	persistentOutput := filepath.Join(agentDirectory, "report.txt")
	if err := os.WriteFile(persistentOutput, []byte("persistent result"), 0600); err != nil {
		t.Fatal(err)
	}
	config.Trigger = constants.AutomaticTaskTriggerAPI
	apiTask, err := service.SaveConfiguration(ctx, wakerID, "", 0, config, true)
	if err != nil {
		t.Fatal(err)
	}
	url := "/v1/automatic-tasks/" + apiTask.TaskID + "/runs"
	response := callLocal(&automaticAPI, "POST", url, apiTask.Token, "api-key", `{"input":"本次附加"}`)
	if response.Code != 202 {
		t.Fatalf("API admission: %d %s", response.Code, response.Body.String())
	}
	var receipt PublicRun
	if err := json.Unmarshal(response.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	receive("本次附加")
	if response := callLocal(&automaticAPI, "POST", url, apiTask.Token, "api-key", `{"input":"本次附加"}`); response.Code != 202 {
		t.Fatal("durable API receipt did not replay")
	}
	if runtime.calls.Load() != 3 {
		t.Fatalf("same key dispatched again: %d", runtime.calls.Load())
	}
	apiRun := complete(apiTask.TaskID, receipt.RunID)
	response = callLocal(&automaticAPI, "GET", url+"/"+receipt.RunID, apiTask.Token, "", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"completed"`) || strings.Contains(response.Body.String(), "chatId") {
		t.Fatalf("limited final status: %s", response.Body.String())
	}
	// 真实 Chat 尚不存在的持久 pending 只能由显式同键 API 请求恢复。
	pending := persistPending(t, &testHarness{service: service, api: automaticAPI, wakerID: wakerID}, apiTask.TaskID, "api:1:recover", constants.AutomaticRunSourceAPI)
	recovered, err := automaticAPI.TriggerAPI(ctx, apiTask.TaskID, apiTask.Token, "recover", "")
	if err != nil || recovered.ID != pending.ID {
		t.Fatalf("pending API resume through real Core: %+v %v", recovered, err)
	}
	receive("后续指令")
	recovered = complete(apiTask.TaskID, recovered.ID)
	// 用户独立删除已结束的运行 Chat 后，历史仍可读且任务聚合仍可删除。
	if err := chats.DeleteChatsAtomically(ctx, []string{manual.ChatID}, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(manualAttachment); !os.IsNotExist(err) {
		t.Fatalf("删除聊天没有清理自身私有附件: %v", err)
	}
	if body, err := os.ReadFile(timedAttachment); err != nil || string(body) != "timed attachment" {
		t.Fatalf("删除聊天影响了其他聊天的私有附件: %q %v", body, err)
	}
	if body, err := os.ReadFile(persistentOutput); err != nil || string(body) != "persistent result" {
		t.Fatalf("删除聊天清理了伙伴持久成果: %q %v", body, err)
	}
	history, _, err = automaticAPI.Runs(ctx, wakerID, scheduled.TaskID, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundMissing := false
	for _, run := range history {
		if run.ID == manual.ID {
			foundMissing = run.State == constants.AutomaticRunStateInterrupted && run.ErrorCode == status_error.ChatNotFound.Key()
		}
	}
	if !foundMissing {
		t.Fatal("independently deleted Chat made run history unavailable")
	}
	listed, err := chats.ListChats(ctx, chat.ListChatsInput{WakerID: wakerID, Limit: 20})
	if err != nil || len(listed.Resources) != 0 {
		t.Fatalf("automatic history leaked into ordinary history: %+v %v", listed, err)
	}
	for _, taskID := range []string{scheduled.TaskID, apiTask.TaskID} {
		if err := automaticAPI.Delete(ctx, wakerID, taskID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(timedAttachment); !os.IsNotExist(err) {
		t.Fatalf("删除任务没有清理所属聊天的私有附件: %v", err)
	}
	if body, err := os.ReadFile(persistentOutput); err != nil || string(body) != "persistent result" {
		t.Fatalf("删除任务清理了共享伙伴 cwd 中的持久成果: %q %v", body, err)
	}
	for _, chatID := range []string{manual.ChatID, timed.ChatID, apiRun.ChatID, recovered.ChatID} {
		if _, err := chats.ReadInitialExecution(ctx, chatID); !util.HasStatusCode(err, status_error.ChatNotFound) {
			t.Fatalf("deleted Chat authority remains: %s %v", chatID, err)
		}
		if _, err := chats.GetChatSummary(ctx, chatID); !util.HasStatusCode(err, status_error.ChatNotFound) {
			t.Fatalf("deleted Chat projection remains: %s %v", chatID, err)
		}
	}
	t.Run("团队任务继承成员配置并复用真实团队首发与聚合删除", func(t *testing.T) {
		memberID := util.New()
		if err := wakers.Create(ctx, models.Waker{ID: memberID, Name: "团队成员"}); err != nil {
			t.Fatal(err)
		}
		memberDocuments := []models.WakerDocument{}
		for _, kind := range []constants.ProfileDocumentKind{constants.ProfileDocumentKindIdentity, constants.ProfileDocumentKindPersona, constants.ProfileDocumentKindPlaybook, constants.ProfileDocumentKindMemory} {
			memberDocuments = append(memberDocuments, models.WakerDocument{ID: util.New(), WakerID: memberID, Kind: kind, Content: "# 团队成员"})
		}
		if err := profiles.CreateWakerDocuments(ctx, memberDocuments); err != nil {
			t.Fatal(err)
		}
		teamID := util.New()
		members := []models.ChannelMember{{ChannelID: teamID, WakerID: wakerID, RuntimeID: "codex", AccountID: "system", Model: "model", PermissionLevel: constants.PermissionLevelAuto}, {ChannelID: teamID, WakerID: memberID, RuntimeID: "codex", AccountID: "system", Model: "model", PermissionLevel: constants.PermissionLevelDefault, Position: 1}}
		team := models.Channel{ID: teamID, Name: "自动任务团队", LeadWakerID: wakerID}
		if err := channels.Create(ctx, team, members); err != nil {
			t.Fatal(err)
		}
		teamConfig := models.AutomaticTaskConfiguration{Name: "团队归档", Instruction: "核对团队配置", Trigger: constants.AutomaticTaskTriggerAPI, ChannelID: teamID, Enabled: true}
		saved, err := service.SaveConfiguration(ctx, wakerID, "", 0, teamConfig, true)
		if err != nil {
			t.Fatal(err)
		}
		storedTask, err := automaticAPI.config.Tasks.Get(ctx, saved.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		// 仅覆盖一位成员，其余成员在触发时继续继承最新的团队设置。
		teamConfig.MemberConfigs = []models.AutomaticTaskMemberConfiguration{{WakerID: memberID, RuntimeID: "codex", AccountID: "system", Model: "model", PermissionLevel: constants.PermissionLevelAuto}}
		if _, err := service.SaveConfiguration(ctx, wakerID, saved.TaskID, storedTask.ConfigurationVersion, teamConfig, true); err != nil {
			t.Fatal(err)
		}
		storedTask, err = automaticAPI.config.Tasks.Get(ctx, saved.TaskID)
		if err != nil || len(storedTask.MemberConfigs) != 1 || storedTask.MemberConfigs[0].WakerID != memberID {
			t.Fatalf("成员覆盖未持久化: %+v %v", storedTask.MemberConfigs, err)
		}
		invalidConfig := teamConfig
		invalidConfig.MemberConfigs = []models.AutomaticTaskMemberConfiguration{{WakerID: util.New(), RuntimeID: "codex", AccountID: "system", PermissionLevel: constants.PermissionLevelDefault}}
		if _, err := service.SaveConfiguration(ctx, wakerID, "", 0, invalidConfig, true); !util.HasStatusCode(err, status_error.ChannelMemberNotFound) {
			t.Fatalf("非团队成员覆盖未拒绝: %v", err)
		}
		invalidConfig.MemberConfigs = append(teamConfig.MemberConfigs, teamConfig.MemberConfigs...)
		if _, err := service.SaveConfiguration(ctx, wakerID, "", 0, invalidConfig, true); !util.HasStatusCode(err, status_error.ChannelMemberCountInvalid) {
			t.Fatalf("重复成员覆盖未拒绝: %v", err)
		}
		// 保存后团队配置变化时，下一次运行使用新的成员默认值。
		members[0].PermissionLevel = constants.PermissionLevelFullAccess
		team.LeadWakerID = memberID
		if err := channels.Update(ctx, team, members); err != nil {
			t.Fatal(err)
		}
		run, err := automaticAPI.TriggerManual(ctx, wakerID, saved.TaskID, "team-first")
		if err != nil || run.ThreadID == "" || run.ChatID == "" || run.WakerID != memberID {
			t.Fatalf("团队首发: %+v %v", run, err)
		}
		receive("核对团队配置")
		inherited, err := threads.GetMember(ctx, run.ThreadID, wakerID)
		if err != nil || inherited.PermissionLevel != constants.PermissionLevelFullAccess || inherited.Model != "model" {
			t.Fatalf("继承成员配置: %+v %v", inherited, err)
		}
		customized, err := threads.GetMember(ctx, run.ThreadID, memberID)
		if err != nil || customized.PermissionLevel != constants.PermissionLevelAuto {
			t.Fatalf("任务成员覆盖未应用: %+v %v", customized, err)
		}
		leadState, err := chats.ReadInitialExecution(ctx, run.ChatID)
		if err != nil || leadState.InitialExecution.PermissionLevel != constants.PermissionLevelAuto {
			t.Fatalf("负责人未使用覆盖权限: %+v %v", leadState.InitialExecution, err)
		}
		if leadState.Chat.ProjectID != "" || len(leadState.Chat.AdditionalWorkingDirectories) != 1 {
			t.Fatalf("未选工作范围的团队任务应冻结团队会话临时目录: %+v", leadState.Chat)
		}
		teamDirectory := leadState.Chat.AdditionalWorkingDirectories[0]
		if info, err := os.Stat(teamDirectory); err != nil || !info.IsDir() {
			t.Fatalf("团队会话临时目录未创建: %v", err)
		}
		if teamDirectory != filepath.Join(filepath.Dir(leadState.Chat.WorkspaceDirectory), constants.TeamSharedDirectoryName) {
			t.Fatalf("团队临时范围应为共享项目目录，成员执行目录保持独立: %s / %s", leadState.Chat.WorkspaceDirectory, teamDirectory)
		}
		// 首发后更改团队默认值不能改写已有运行快照或幂等重放。
		members[1].PermissionLevel = constants.PermissionLevelFullAccess
		if err := channels.Update(ctx, team, members); err != nil {
			t.Fatal(err)
		}
		replay, err := automaticAPI.TriggerManual(ctx, wakerID, saved.TaskID, "team-first")
		if err != nil || replay.ThreadID != run.ThreadID || replay.ExecutionID != run.ExecutionID {
			t.Fatalf("团队幂等重放: %+v %v", replay, err)
		}
		if err := automaticAPI.Delete(ctx, wakerID, saved.TaskID); util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key()) != status_error.AutomaticTaskBusy.Key() {
			t.Fatalf("活跃团队删除保护: %v", err)
		}
		stored, err := automaticAPI.config.Tasks.GetRun(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		stored.Admission = constants.AutomaticRunAdmissionPending
		stored.ChatID = ""
		stored.ThreadID = ""
		if err := automaticAPI.config.Tasks.UpdateRunAdmission(ctx, stored); err != nil {
			t.Fatal(err)
		}
		service.Close()
		service = schedule.New(automaticAPI.config)
		automaticAPI.service = service
		if err := service.Start(ctx); err != nil {
			t.Fatal(err)
		}
		linked, err := automaticAPI.config.Tasks.GetRun(ctx, run.ID)
		if err != nil || linked.ThreadID != run.ThreadID || linked.ChatID != run.ChatID {
			t.Fatalf("团队首发权威对账: %+v %v", linked, err)
		}

		// 负责人结束首轮后，成员待验收和未交付汇总仍保持运行槽。
		finishedAt := automaticAPI.config.Now().Add(time.Second).UnixMilli()
		review := models.ChannelTask{ID: util.New(), ThreadID: run.ThreadID, AssigneeWakerID: wakerID, Subject: "等待验收", Status: constants.ChannelTaskStatusInReview, FinishedAt: &finishedAt, ChangedPaths: []string{}}
		if err := teamTasks.Create(ctx, review, nil); err != nil {
			t.Fatal(err)
		}
		runtime.finish <- struct{}{}
		deadline := time.Now().Add(5 * time.Second)
		for {
			state, err := chats.ReadInitialExecution(ctx, run.ChatID)
			if err != nil {
				t.Fatal(err)
			}
			if state.InitialExecution.Status == constants.ExecutionStatusCompleted {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("负责人首轮未结束")
			}
			time.Sleep(10 * time.Millisecond)
		}
		waiting, err := service.Get(ctx, wakerID, saved.TaskID)
		if err != nil || waiting.ActiveRun == nil || waiting.ActiveRun.State != constants.AutomaticRunStateWaitingInput || waiting.ActiveRun.CompletedAt != nil {
			t.Fatalf("团队验收等待被提前完成: %+v %v", waiting, err)
		}
		if err := automaticAPI.Delete(ctx, wakerID, saved.TaskID); util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key()) != status_error.AutomaticTaskBusy.Key() {
			t.Fatalf("待验收删除保护: %v", err)
		}
		if err := teamTasks.UpdateFields(ctx, review.ID, map[string]interface{}{"status": constants.ChannelTaskStatusCompleted}); err != nil {
			t.Fatal(err)
		}
		notification := models.ChannelLeadNotification{ID: util.New(), ThreadID: run.ThreadID, TaskID: review.ID, AttemptID: util.New(), Status: constants.ChannelNotificationStatusDelivering, Payload: "等待汇总"}
		if err := teamTasks.CreateNotification(ctx, notification); err != nil {
			t.Fatal(err)
		}
		waiting, err = service.Get(ctx, wakerID, saved.TaskID)
		if err != nil || waiting.ActiveRun == nil || waiting.ActiveRun.State != constants.AutomaticRunStateRunning {
			t.Fatalf("团队汇总被提前完成: %+v %v", waiting, err)
		}
		if err := teamTasks.UpdateNotifications(ctx, []string{notification.ID}, constants.ChannelNotificationStatusConsumed, "", finishedAt); err != nil {
			t.Fatal(err)
		}
		// 首次整体结束后不查询自动任务，直接由 Core 接纳手动续聊，仍须冻结原结果。
		closedAt := finishedAt

		continuation, err := teams.SubmitMessage(ctx, channelservice.SubmitMessageInput{Continue: &channelservice.SendMessageInput{ThreadID: run.ThreadID, Input: execution.WorkInput{Content: "手动续聊"}}})
		if err != nil || continuation.Message == nil || continuation.Message.Execution == nil {
			t.Fatalf("团队用户续聊未接纳: %+v %v", continuation, err)
		}
		receive("手动续聊")
		userExecution, err := chats.ReadExecution(ctx, run.ChatID, continuation.Message.Execution.ID)
		if err != nil || userExecution.AutomaticRunID != "" {
			t.Fatalf("团队普通续聊继承了历史自动来源: %+v %v", userExecution, err)
		}
		continued, err := service.Get(ctx, wakerID, saved.TaskID)
		if err != nil || continued.ActiveRun != nil || continued.LastRun.State != constants.AutomaticRunStateCompleted || continued.LastRun.CompletedAt == nil || *continued.LastRun.CompletedAt < closedAt {
			t.Fatalf("手动续聊改写自动运行: %+v %v", continued, err)
		}
		if err := automaticAPI.Delete(ctx, wakerID, saved.TaskID); util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key()) != status_error.AutomaticTaskBusy.Key() {
			t.Fatalf("手动续聊删除保护: %v", err)
		}
		runtime.finish <- struct{}{}
		deadline = time.Now().Add(5 * time.Second)
		for {
			state, err := chats.ReadInitialExecution(ctx, run.ChatID)
			if err != nil {
				t.Fatal(err)
			}
			if state.Chat.ActiveExecutionID == "" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("手动续聊未结束")
			}
			time.Sleep(10 * time.Millisecond)
		}
		memberRun, err := automaticAPI.TriggerManual(ctx, wakerID, saved.TaskID, "team-member-continuation")
		if err != nil {
			t.Fatal(err)
		}
		receive("核对团队配置")
		runtime.finish <- struct{}{}
		deadline = time.Now().Add(5 * time.Second)
		for {
			state, err := chats.ReadInitialExecution(ctx, memberRun.ChatID)
			if err != nil {
				t.Fatal(err)
			}
			if state.Chat.ActiveExecutionID == "" {
				// 日志终态先于SQLite查询投影收尾；成员续聊前需确认原团队已整体空闲。
				active, err := threads.CountActiveWork(ctx, memberRun.ThreadID)
				if err != nil {
					t.Fatal(err)
				}
				if active == 0 {
					break
				}
			}
			if time.Now().After(deadline) {
				t.Fatal("第二次团队首轮未结束")
			}
			time.Sleep(10 * time.Millisecond)
		}
		member, err := threads.GetMember(ctx, memberRun.ThreadID, wakerID)
		if err != nil {
			t.Fatal(err)
		}
		// 不读取自动任务状态，直接在非负责人 Chat 续聊，原运行也须封闭。
		if _, err := core.Start(ctx, execution.WorkIntent{Chat: execution.ChatTarget{ChatID: member.ChatID}, Input: execution.WorkInput{Actor: "user", Content: "成员手动续聊"}, Options: execution.WorkOptions{RuntimeID: "codex", AccountID: "system", Model: "model", PermissionLevel: constants.PermissionLevelDefault}}); err != nil {
			t.Fatal(err)
		}
		receive("成员手动续聊")
		memberContinued, err := service.Get(ctx, wakerID, saved.TaskID)
		if err != nil || memberContinued.ActiveRun != nil || memberContinued.LastRun.State != constants.AutomaticRunStateCompleted {
			t.Fatalf("成员续聊改写自动运行: %+v %v", memberContinued, err)
		}
		runtime.finish <- struct{}{}
		deadline = time.Now().Add(5 * time.Second)
		for {
			state, err := chats.ReadInitialExecution(ctx, member.ChatID)
			if err != nil {
				t.Fatal(err)
			}
			if state.Chat.ActiveExecutionID == "" {
				// 日志终态先于 SQLite 索引收尾可见；删除要求团队聚合也已空闲。
				active, err := threads.CountActiveWork(ctx, memberRun.ThreadID)
				if err != nil {
					t.Fatal(err)
				}
				if active == 0 {
					break
				}
			}
			if time.Now().After(deadline) {
				t.Fatal("成员续聊未结束")
			}
			time.Sleep(10 * time.Millisecond)
		}
		// 删除创建时负责人不能级联移除团队任务。
		if err := automaticAPI.config.Tasks.DeleteByWaker(ctx, wakerID); err != nil {
			t.Fatal(err)
		}

		views, total, counts, err := service.List(ctx, controllers.AutomaticTaskFilter{ExecutorKind: "team"})
		if err != nil || total != 1 || counts.Total != 1 || len(views) != 1 || views[0].ExecutorName != team.Name {
			t.Fatalf("全局团队筛选: %+v %d %+v %v", views, total, counts, err)
		}
		chatIDs, err := threads.ListChatIDs(ctx, run.ThreadID)
		if err != nil || len(chatIDs) != 2 {
			t.Fatalf("团队成员会话: %v %v", chatIDs, err)
		}
		if err := automaticAPI.Delete(ctx, wakerID, saved.TaskID); err != nil {
			t.Fatal(err)
		}
		for _, chatID := range chatIDs {
			if _, err := chats.ReadInitialExecution(ctx, chatID); !util.HasStatusCode(err, status_error.ChatNotFound) {
				t.Fatalf("团队成员会话删除: %s %v", chatID, err)
			}
		}
		if _, err := threads.Get(ctx, run.ThreadID); !util.HasStatusCode(err, status_error.ChannelThreadNotFound) {
			t.Fatalf("团队结构化事实删除: %v", err)
		}
		disabled, err := service.SaveConfiguration(ctx, memberID, "", 0, teamConfig, true)
		if err != nil {
			t.Fatal(err)
		}
		resetTask, err := automaticAPI.config.Tasks.Get(ctx, disabled.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		teamConfig.MemberConfigs = nil
		if _, err := service.SaveConfiguration(ctx, memberID, disabled.TaskID, resetTask.ConfigurationVersion, teamConfig, true); err != nil {
			t.Fatal(err)
		}
		resetTask, err = automaticAPI.config.Tasks.Get(ctx, disabled.TaskID)
		if err != nil || len(resetTask.MemberConfigs) != 0 {
			t.Fatalf("恢复继承未清空持久覆盖: %+v %v", resetTask.MemberConfigs, err)
		}
		t.Run("负责人和成员并行使用独立原生会话并持久取消终态", func(t *testing.T) {
			created, err := teams.SubmitMessage(ctx, channelservice.SubmitMessageInput{Create: &channelservice.CreateThreadInput{ChannelID: teamID, ClientRequestID: "parallel-native", Input: execution.WorkInput{Actor: "user", Content: "并发负责人"}}})
			if err != nil || created.Thread == nil {
				t.Fatalf("团队并发首发: %+v %v", created, err)
			}
			next := func() integrationPrompt {
				t.Helper()
				select {
				case input := <-runtime.requests:
					return input
				case <-time.After(5 * time.Second):
					t.Fatal("原生连接未接收到输入")
					return integrationPrompt{}
				}
			}
			lead := next()
			mentioned, err := teams.SubmitMessage(ctx, channelservice.SubmitMessageInput{Continue: &channelservice.SendMessageInput{ThreadID: created.Thread.Thread.ID, MentionWakerID: wakerID, Input: execution.WorkInput{Actor: "user", Content: "并发成员"}}})
			if err != nil || mentioned.Message == nil || mentioned.Message.Task == nil {
				t.Fatalf("团队成员任务未接纳: %+v %v", mentioned, err)
			}
			member := next()
			if !strings.Contains(lead.input.Text, "并发负责人") || !strings.Contains(member.input.Text, "并发成员") || lead.cwd == member.cwd || lead.input.HostInvocationID == member.input.HostInvocationID || len(lead.directories) != 1 || len(member.directories) != 1 || lead.directories[0] != member.directories[0] {
				t.Fatalf("并发成员未隔离原生工作区或未共享批准目录: lead=%+v member=%+v", lead, member)
			}
			for _, snapshot := range created.Thread.Members {
				state, err := chats.GetChatSummary(ctx, snapshot.ChatID)
				workspace, pathErr := filepath.EvalSymlinks(state.Chat.WorkspaceDirectory)
				if err != nil || pathErr != nil || state.Chat.ActiveExecutionID == "" || (workspace != lead.cwd && workspace != member.cwd) {
					t.Fatalf("实际准备路径或并行执行事实不符: %+v %v", state.Chat, err)
				}
			}
			if err := teams.StopWork(ctx, teamID, created.Thread.Thread.ID); err != nil {
				t.Fatal(err)
			}
			for _, snapshot := range created.Thread.Members {
				state, err := chats.ReadInitialExecution(ctx, snapshot.ChatID)
				if err != nil || state.Chat.ActiveExecutionID != "" || state.InitialExecution == nil || state.InitialExecution.Status != constants.ExecutionStatusCanceled || state.InitialExecution.CompletedAt == nil {
					t.Fatalf("取消没有写入成员权威终态: %+v %v", state, err)
				}
			}
		})

		if err := channels.Delete(ctx, teamID); err != nil {
			t.Fatal(err)
		}
		invalid, err := service.Get(ctx, memberID, disabled.TaskID)
		if err != nil || invalid.ConfigurationError == "" {
			t.Fatalf("失效团队配置未提示: %+v %v", invalid, err)
		}
		if err := automaticAPI.SetEnabled(ctx, memberID, disabled.TaskID, invalid.ConfigurationVersion, false); err != nil {
			t.Fatalf("失效团队无法停用: %v", err)
		}

	})
}
