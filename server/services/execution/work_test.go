package execution

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"

	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// testMessageInput 统一表达测试中的创建首发和已有对话续发输入。
type testMessageInput struct {
	// ChatID 是续发测试使用的既有对话标识。
	ChatID string
	// ClientRequestID 是首发测试稳定复用的客户端幂等标识；为空时自动生成。
	ClientRequestID string
	// NewChat 是首发测试使用的对话身份和工作范围。
	NewChat *testChatInput
	// Message 是测试提交的用户输入。
	Message userMessage
	// Runtime 是测试提交的 Runtime 选择。
	Runtime runtimeSelection
}

func TestHiddenEntryCreatesStandardChatAndPreservesInitialExecution(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	ctx := context.Background()
	intent := WorkIntent{
		ClientRequestID: "automatic-run-receipt",
		ExecutionID:     util.New(),
		Chat:            ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir(), HiddenFromWakerHistory: true}},
		Input:           WorkInput{Actor: constants.AutomaticTaskActor, Content: "整理项目资料"},
		Options:         WorkOptions{RuntimeID: agent.RuntimeIDCodex, PermissionLevel: agent.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyContinue},
		Admission:       constants.ExecutionAdmissionCommitBeforeDispatch,
	}
	accepted, err := manager.execution.Start(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	module := manager.chats.(chatservice.Module)
	initial, err := module.ReadInitialExecution(ctx, accepted.Chat.ID)
	if err != nil || !initial.Chat.HiddenFromWakerHistory || initial.InitialExecution == nil || initial.InitialExecution.ID != intent.ExecutionID {
		t.Fatalf("hidden standard Chat facts: %+v %v", initial, err)
	}
	listed, err := manager.chats.ListChats(ctx, chatservice.ListChatsInput{WakerID: "waker-1", Limit: 20})
	if err != nil || len(listed.Resources) != 0 {
		t.Fatalf("hidden Chat entered normal history: %+v %v", listed, err)
	}
	replayed, err := manager.execution.Start(ctx, intent)
	if err != nil || !replayed.Replayed || replayed.Chat.ID != accepted.Chat.ID || replayed.Execution.ID != intent.ExecutionID {
		t.Fatalf("stable first admission: %+v %v", replayed, err)
	}
	changed := *intent.Chat.Create
	changed.HiddenFromWakerHistory = false
	changedIntent := intent
	changedIntent.Chat.Create = &changed
	if _, err := manager.execution.Start(ctx, changedIntent); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("changed visibility reused original identity: %v", err)
	}
	continued, err := manager.sendTestMessage(ctx, testMessageInput{ChatID: accepted.Chat.ID, Message: userMessage{Text: "继续整理"}, Runtime: runtimeSelection{RuntimeID: agent.RuntimeIDCodex}})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	after, err := module.ReadInitialExecution(ctx, accepted.Chat.ID)
	if err != nil || after.InitialExecution.ID != intent.ExecutionID || after.InitialExecution.ID == continued.Execution.ID || after.InitialExecution.Status != initial.InitialExecution.Status {
		t.Fatalf("continuation changed first execution: %+v %v", after, err)
	}
}

type testChatInput struct {
	// WakerID 是测试对话绑定的伙伴标识。
	WakerID string
	// ProjectID 是测试选择的项目标识。
	ProjectID string
	// WorkingDirectory 是测试选择的临时目录。
	WorkingDirectory string
}

type messageTestService struct {
	// store owns durable admissions for the fixture.
	store *conversation.Store
	// trace records the actual Codex app-server requests.
	trace string
	// execution 接收入口无关的 WorkIntent 并管理 Runtime 生命周期。
	execution Core
	// chats 提供权威事实查询和 Chat 元数据用例。
	chats chatservice.Service
	// profiles 允许测试在两次执行之间修改当前伙伴档案。
	profiles controllers.ProfileDocumentController
	// projects 允许测试准备带多目录配置的真实项目。
	projects controllers.ProjectController
	// projectSources 允许测试准备项目引用的真实目录索引。
	projectSources controllers.ProjectSourceController
	// wakers 允许测试为团队聊天准备多个伙伴身份。
	wakers controllers.WakerController
}

func (s messageTestService) sendTestMessage(
	ctx context.Context,
	input testMessageInput,
) (AcceptedExecution, error) {
	if input.Runtime.PermissionLevel == "" {
		input.Runtime.PermissionLevel = agent.PermissionLevelAuto
	}
	intent := WorkIntent{
		Chat: ChatTarget{ChatID: input.ChatID},
		Input: WorkInput{
			Actor:          "test-user",
			Content:        input.Message.Text,
			Attachments:    append([]AttachmentInput(nil), input.Message.Attachments...),
			SkillIDs:       append([]string(nil), input.Message.SkillIDs...),
			ConnectorIDs:   append([]string(nil), input.Message.ConnectorIDs...),
			CodeReferences: append([]CodeReferenceInput(nil), input.Message.CodeReferences...),
		},
		Options: WorkOptions{
			AccountID:         input.Runtime.AccountID,
			RuntimeID:         input.Runtime.RuntimeID,
			Model:             input.Runtime.Model,
			Reasoning:         input.Runtime.Reasoning,
			PermissionLevel:   input.Runtime.PermissionLevel,
			WorkspaceReadOnly: input.Runtime.WorkspaceReadOnly,
			SessionPolicy:     constants.RuntimeSessionPolicyContinue,
		},
		Admission: constants.ExecutionAdmissionCommitOnRuntimeAccept,
	}
	if input.NewChat == nil {
		if input.ChatID == "" {
			return AcceptedExecution{}, status_error.BadRequest
		}
		return s.execution.Start(ctx, intent)
	}
	intent.Chat = ChatTarget{Create: &CreateChatSpec{
		WakerID:          input.NewChat.WakerID,
		ProjectID:        input.NewChat.ProjectID,
		WorkingDirectory: input.NewChat.WorkingDirectory,
	}}
	intent.ClientRequestID = strings.TrimSpace(input.ClientRequestID)
	if intent.ClientRequestID == "" {
		intent.ClientRequestID = util.New()
	}
	return s.execution.Start(ctx, intent)
}

type trackingSkillController struct {
	// SkillController 仅补齐消息执行未使用的其他仓储端口。
	controllers.SkillController
	// enabled 是伙伴当前已经持久启用的技能快照。
	enabled []models.SkillWithState
	// upsertCalls 记录是否错误地通过聊天选择修改了持久启停关系。
	upsertCalls int
}

func (c *trackingSkillController) CreatePackage(context.Context, models.SkillPackage) error {
	return nil
}

func (c *trackingSkillController) GetPackage(context.Context, string) (models.SkillPackage, error) {
	return models.SkillPackage{}, nil
}

func (c *trackingSkillController) ListVisible(context.Context, string, string) ([]models.SkillWithState, error) {
	return nil, nil
}

func (c *trackingSkillController) CreateVersion(context.Context, models.SkillVersion, bool, string) error {
	return nil
}

func (c *trackingSkillController) ListVersions(context.Context, string) ([]models.SkillVersion, error) {
	return nil, nil
}

func (c *trackingSkillController) GetVersion(context.Context, string) (models.SkillVersion, error) {
	return models.SkillVersion{}, nil
}

func (c *trackingSkillController) DeleteVersion(context.Context, string, string) (models.SkillVersion, error) {
	return models.SkillVersion{}, nil
}

func (c *trackingSkillController) ListVersionContentPaths(context.Context) ([]string, error) {
	return nil, nil
}

func (c *trackingSkillController) SetCurrentVersion(context.Context, string, string) error {
	return nil
}

func (c *trackingSkillController) EnsureWakerSkill(ctx context.Context, relation models.WakerSkill) error {
	return c.UpsertWakerSkill(ctx, relation)
}

func (c *trackingSkillController) UpsertWakerSkill(context.Context, models.WakerSkill) error {
	c.upsertCalls++
	return nil
}

func (c *trackingSkillController) ListEnabled(context.Context, string) ([]models.SkillWithState, error) {
	return append([]models.SkillWithState(nil), c.enabled...), nil
}

func (c *trackingSkillController) DeleteWakerSkill(context.Context, string, string) error {
	return nil
}

func (c *trackingSkillController) ListPrivateVersionsByWaker(context.Context, string) ([]models.SkillVersion, error) {
	return nil, nil
}

func (c *trackingSkillController) DeleteByWaker(context.Context, string) error {
	return nil
}

func (c *trackingSkillController) DeletePackage(context.Context, string) error {
	return nil
}

func (c *trackingSkillController) UpdatePackageDisplayName(context.Context, string, string) error {
	return nil
}

func TestSendMessagePreStartFailureLeavesNoAuthoritativeMessageOrExecution(
	t *testing.T,
) {
	ctx := context.Background()
	logDirectory := t.TempDir()
	workingDirectory := t.TempDir()
	manager := newMessageTestService(t, logDirectory, messageTestOptions{rejectLaunch: true})

	request := testMessageInput{
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: workingDirectory,
		},
		Message: userMessage{Text: "不要留下半提交消息"},
		Runtime: runtimeSelection{
			RuntimeID: "codex",
			Model:     "gpt-5",
			Reasoning: "high",
		},
	}
	if _, err := manager.sendTestMessage(ctx, request); !errors.Is(
		err,
		status_error.RuntimeUnavailable,
	) {
		t.Fatalf("sendTestMessage() error = %v, want RuntimeUnavailable", err)
	}
	// 再次提交仍然进入 Runtime，证明第一次失败已经释放进程内执行槽位。
	if _, err := manager.sendTestMessage(ctx, request); !errors.Is(
		err,
		status_error.RuntimeUnavailable,
	) {
		t.Fatalf("second sendTestMessage() error = %v, want RuntimeUnavailable", err)
	}
	entries, err := manager.store.ListChatIDs()
	if err != nil {
		t.Fatalf("ReadDir(logDirectory) error = %v", err)
	}
	for _, chatID := range entries {
		version, err := manager.store.ReadVersion(ctx, chatID)
		if err != nil || version != 0 {
			t.Fatalf("unadmitted chat %s committed version %d: %v", chatID, version, err)
		}
		path, err := manager.store.LogPath(chatID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unadmitted chat created a log file %s: %v", path, err)
		}
	}
}

func TestCreateChatReplaysSameFirstRequestAndRejectsChangedContent(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	request := testMessageInput{
		ClientRequestID: "stable-create-request",
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: t.TempDir(),
		},
		Message: userMessage{Text: "只创建一次对话"},
		Runtime: runtimeSelection{
			RuntimeID:       constants.RuntimeIDCodex,
			PermissionLevel: constants.PermissionLevelAuto,
		},
	}
	first, err := manager.sendTestMessage(context.Background(), request)
	if err != nil {
		t.Fatalf("first send error = %v", err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	replayed, err := manager.sendTestMessage(context.Background(), request)
	if err != nil {
		t.Fatalf("replay send error = %v", err)
	}
	if !replayed.Replayed || replayed.Chat.ID != first.Chat.ID ||
		replayed.Execution.ID != first.Execution.ID || replayed.Execution.CompletedAt == nil {
		t.Fatalf("replayed execution = %+v, first = %+v", replayed, first)
	}
	if calls := manager.nativeCalls(t, "turn/start"); len(calls) != 1 {
		t.Fatalf("native submissions = %d, want 1", len(calls))
	}
	request.Message.Text = "不能复用同一个标识发送其他内容"
	if _, err := manager.sendTestMessage(context.Background(), request); !errors.Is(
		err,
		status_error.CreateRequestConflict,
	) {
		t.Fatalf("changed replay error = %v, want CreateRequestConflict", err)
	}
}

func TestSendMessageRollsBackNewWorkspaceWhenWholeFileReferenceIsOutsideProject(t *testing.T) {
	project := t.TempDir()
	outsideFile := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outsideFile, []byte("package outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	logDirectory := t.TempDir()
	manager := newMessageTestService(
		t,
		logDirectory,
	)

	_, err := manager.sendTestMessage(context.Background(), testMessageInput{
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: project,
		},
		Message: userMessage{
			Text: "检查范围外文件",
			CodeReferences: []CodeReferenceInput{{
				Path: outsideFile,
			}},
		},
		Runtime: runtimeSelection{RuntimeID: agent.RuntimeIDCodex},
	})
	if !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("sendTestMessage() error = %v, want BadRequest", err)
	}
	entries, readErr := filepath.Glob(filepath.Join(logDirectory, "chat", "*", "workspace"))
	if readErr != nil {
		t.Fatalf("ReadDir(logDirectory) error = %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("chat workspace entries = %v, want none", entries)
	}
}

func TestPrepareMessageFilesKeepsUnchangedProfileInstructionsStable(t *testing.T) {
	ctx := context.Background()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("项目约束"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := newMessageTestService(t, t.TempDir())
	accepted, err := manager.sendTestMessage(ctx, testMessageInput{
		NewChat: &testChatInput{WakerID: "waker-1", WorkingDirectory: project},
		Message: userMessage{Text: "第一次执行"},
		Runtime: runtimeSelection{RuntimeID: agent.RuntimeIDCodex},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	core := manager.execution.(*service)
	state, err := core.resolveMessageChat(ctx, pendingMessage{chatID: accepted.Chat.ID}, util.New())
	if err != nil {
		t.Fatal(err)
	}
	firstFiles, err := core.prepareMessageFiles(ctx, state, util.New(), nil, nil, agent.RuntimeIDCodex)
	if err != nil {
		t.Fatal(err)
	}
	first := firstFiles.instructions
	firstFiles.rollback()
	if len(first) != 4 || !hasInstructionContent(first, "项目约束") {
		t.Fatalf("initial profile and project instructions = %+v", first)
	}
	// 反复经过真实档案读取和工作区准备，覆盖 map 遍历顺序变化造成的静态配置抖动。
	for attempt := 0; attempt < 64; attempt++ {
		files, err := core.prepareMessageFiles(ctx, state, util.New(), nil, nil, agent.RuntimeIDCodex)
		if err != nil {
			t.Fatal(err)
		}
		files.rollback()
		if !slices.Equal(files.instructions, first) {
			t.Fatalf("unchanged profile changed instructions on attempt %d: first=%+v current=%+v", attempt, first, files.instructions)
		}
	}
}

func hasInstructionContent(instructions []agent.Instruction, content string) bool {
	for _, instruction := range instructions {
		if instruction.Content == content {
			return true
		}
	}
	return false
}

func TestSelectedConnectorReferencesRequireEnabledConnectors(t *testing.T) {
	enabled := []models.MCPConnector{{
		ID:   "connector-browser",
		Name: "浏览器自动化",
	}}
	if !selectedConnectorsAreEnabled([]string{"connector-browser"}, enabled) {
		t.Fatal("enabled connector selection was rejected")
	}
	if selectedConnectorsAreEnabled([]string{"connector-disabled"}, enabled) {
		t.Fatal("disabled connector selection was accepted")
	}
	references := selectedConnectorReferences(
		[]string{"connector-browser"},
		runtimeConnectorResources(enabled),
	)
	if len(references) != 1 || references[0].ID != "connector-browser" ||
		references[0].Name != "浏览器自动化" {
		t.Fatalf("connector references = %+v", references)
	}
}

func TestChatPublicQueriesObserveMessageAndRename(t *testing.T) {
	manager := newMessageTestService(
		t,
		t.TempDir(),
	)
	result, err := manager.sendTestMessage(context.Background(), testMessageInput{
		NewChat: &testChatInput{
			WakerID:          "waker-1",
			WorkingDirectory: t.TempDir(),
		},
		Message: userMessage{Text: "查询对话"},
		Runtime: runtimeSelection{RuntimeID: agent.RuntimeIDCodex},
	})
	if err != nil {
		t.Fatalf("sendTestMessage() error = %v", err)
	}
	waitForChatTerminal(t, manager, result.Chat.ID)
	if err := manager.chats.RenameChat(
		context.Background(),
		result.Chat.ID,
		"  新的   对话标题  ",
	); err != nil {
		t.Fatalf("RenameChat() error = %v", err)
	}
	summary, err := manager.chats.GetChatSummary(
		context.Background(),
		result.Chat.ID,
	)
	if err != nil || summary.Chat.Title != "新的 对话标题" {
		t.Fatalf("GetChatSummary() = %+v, %v", summary, err)
	}
	list, err := manager.chats.ListChats(context.Background(), chatservice.ListChatsInput{
		WakerID: " waker-1 ",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("ListChats() error = %v", err)
	}
	if len(list.Resources) != 1 || list.Resources[0].Title != "新的 对话标题" {
		t.Fatalf("ListChats() = %+v", list)
	}
	if _, err := manager.chats.ListChats(context.Background(), chatservice.ListChatsInput{
		WakerID: "waker-1",
		Limit:   0,
	}); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("invalid ListChats() error = %v", err)
	}
	if err := manager.chats.RenameChat(
		context.Background(),
		result.Chat.ID,
		"",
	); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("invalid RenameChat() error = %v", err)
	}
}

func waitForChatTerminal(t *testing.T, manager messageTestService, chatID string) {
	t.Helper()
	core := manager.execution.(*service)
	// 此等待只核对最终事实，不能把并行采集与 SQLite 调度时间当成产品延迟断言。
	deadline := time.Now().Add(10 * time.Second)
	for {
		summary, err := manager.chats.GetChatSummary(context.Background(), chatID)
		if err == nil && summary.Chat.ActiveExecutionID == "" {
			core.executions.mutex.Lock()
			_, active := core.executions.active[chatID]
			core.executions.mutex.Unlock()
			if !active {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("execution did not reach terminal state: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newMessageTestService(
	t *testing.T,
	logDirectory string,
	options ...messageTestOptions,
) messageTestService {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "chat-message.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get test sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	wakers := controllers.NewWakerController(database.DB)
	profiles := controllers.NewProfileDocumentController(database.DB)
	projects := controllers.NewProjectController(database.DB)
	projectSources := controllers.NewProjectSourceController(database.DB)
	if err := wakers.Create(context.Background(), models.Waker{
		ID:   "waker-1",
		Name: "测试伙伴",
	}); err != nil {
		t.Fatalf("create test waker: %v", err)
	}
	documents := make([]models.WakerDocument, 0, 4)
	for index, kind := range []constants.ProfileDocumentKind{
		constants.ProfileDocumentKindIdentity,
		constants.ProfileDocumentKindPersona,
		constants.ProfileDocumentKindPlaybook,
		constants.ProfileDocumentKindMemory,
	} {
		documents = append(documents, models.WakerDocument{
			ID:      "document-" + string(rune('1'+index)),
			WakerID: "waker-1",
			Kind:    kind,
			Content: "# Test",
		})
	}
	if err := profiles.CreateWakerDocuments(context.Background(), documents); err != nil {
		t.Fatalf("create test profile documents: %v", err)
	}

	var selectedOptions messageTestOptions
	if len(options) > 0 {
		selectedOptions = options[0]
	}
	if selectedOptions.skillDataDirectory == "" {
		selectedOptions.skillDataDirectory = t.TempDir()
	}
	transactions := controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	store, err := conversation.NewStore(logDirectory)
	if err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(t.TempDir(), "wire.jsonl")
	t.Setenv("NEUX_EXECUTION_FIXTURE_TRACE", trace)
	provider := agent.NewProviderRuntime(agent.ProviderRuntimeOptions{AcquireAccount: func(context.Context, agent.AccountRequest) (agent.AccountLease, error) {
		if selectedOptions.rejectLaunch {
			return agent.AccountLease{}, status_error.RuntimeUnavailable
		}
		return agent.AccountLease{}, nil
	}})
	threads := agent.NewThreadLifecycle(provider, store)
	chatModule, err := chatservice.New(chatservice.Config{
		Conversations:    store,
		StorageDirectory: logDirectory,
		Projects:         projects,
		QueryIndexes: chatservice.QueryIndexDependencies{
			Chats: controllers.NewChatController(database.DB),
			RuntimeSessions: controllers.NewRuntimeSessionController(
				database.DB,
			),
			DeletionIntents: controllers.NewChatDeletionIntentController(database.DB),
			Transactions:    transactions,
		},
	})
	if err != nil {
		t.Fatalf("new Chat module: %v", err)
	}
	if err := chatModule.Start(context.Background()); err != nil {
		t.Fatalf("start Chat module: %v", err)
	}
	runtimeEnvironments := selectedOptions.runtimeEnvironments
	if runtimeEnvironments == nil {
		runtimeEnvironments = readyRuntimeEnvironmentReader{}
	}
	runtimeEnvironments = nativeFixtureEnvironmentReader{source: runtimeEnvironments}
	summaries := []agent.RuntimeSummary{fixtureCodexSummary(), {ID: agent.RuntimeIDClaude, BaseRuntimeID: agent.RuntimeIDClaude, InstructionFileName: "CLAUDE.md", SkillDirectoryName: ".claude"}}
	executionCore, err := New(Config{
		Threads: threads, RuntimeSummaries: summaries,
		ResolveAccount: func(_ context.Context, request agent.AccountRequest, _ string) (agent.AccountRequest, error) {
			request.Subject = "fixture-subject"
			return request, nil
		},
		Journal:             chatModule,
		DormantChats:        chatModule,
		Wakers:              wakers,
		Profiles:            profiles,
		Projects:            projects,
		Skills:              selectedOptions.skills,
		Plugins:             selectedOptions.plugins,
		RuntimeEnvironments: runtimeEnvironments,
		SkillDataDirectory:  selectedOptions.skillDataDirectory,
		StorageDirectory:    logDirectory,
	})
	if err != nil {
		t.Fatalf("new Execution Core: %v", err)
	}
	if err := executionCore.Open(context.Background()); err != nil {
		t.Fatalf("open Execution Core: %v", err)
	}
	t.Cleanup(func() {
		executionCore.Close()
		if err := threads.Close(context.Background()); err != nil {
			t.Error(err)
		}
		if err := provider.Close(context.Background()); err != nil {
			t.Error(err)
		}
		chatModule.Close()
		store.Close()
	})
	return messageTestService{
		store: store, trace: trace, execution: executionCore,
		chats:          chatModule,
		profiles:       profiles,
		projects:       projects,
		projectSources: projectSources,
		wakers:         wakers,
	}
}

type readyRuntimeEnvironmentReader struct{}

func (readyRuntimeEnvironmentReader) Get(
	context.Context,
	string,
) (models.RuntimeEnvironment, error) {
	return models.RuntimeEnvironment{
		RuntimeID:    "codex",
		Enabled:      true,
		Binary:       "codex",
		DefaultModel: "default",
		Detection: agent.DetectionResult{
			Runtime: agent.RuntimeSummary{
				ID:            "codex",
				BaseRuntimeID: agent.RuntimeIDCodex,
			},
			State:          agent.DetectionStateReady,
			ResolvedBinary: "codex",
			Capabilities: agent.CapabilitySet{
				agent.CapabilityMCPStreamableHTTP: true,
				agent.CapabilitySkillDirectory:    true,
			},
			Models: []agent.ModelOption{
				readyTestModel("default"),
				readyTestModel("gpt-5"),
				readyTestModel("gpt-5.1"),
			},
		},
	}, nil
}

func readyTestModel(id string) agent.ModelOption {
	return agent.ModelOption{
		ID:   id,
		Name: id,
		PermissionLevels: []agent.PermissionLevel{
			agent.PermissionLevelDefault,
			agent.PermissionLevelAuto,
			agent.PermissionLevelFullAccess,
		},
	}
}

type messageTestOptions struct {
	// rejectLaunch simulates an unavailable provider before any admission.
	rejectLaunch bool
	// plugins 提供安装快照的只读端口。
	plugins controllers.PluginController
	// skills 提供伙伴已启用技能的只读快照。
	skills controllers.SkillController
	// skillDataDirectory 是断言权威技能版本绝对路径时使用的数据目录。
	skillDataDirectory string
	// runtimeEnvironments 提供权限目录差异测试使用的 Runtime 快照。
	runtimeEnvironments runtimeEnvironmentReader
}

func TestIntegrationAutomaticTaskExecutionProvenanceSurvivesJournalReplay(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	ctx := context.Background()
	intent := WorkIntent{AutomaticRunID: "original-automatic-run", ClientRequestID: "automatic-provenance", ExecutionID: util.New(), Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir(), HiddenFromWakerHistory: true}}, Input: WorkInput{Actor: constants.AutomaticTaskActor, Content: "整理正文"}, Options: WorkOptions{RuntimeID: agent.RuntimeIDCodex, PermissionLevel: agent.PermissionLevelAuto}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch}
	accepted, err := manager.execution.Start(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	initial, err := manager.chats.(chatservice.Module).ReadInitialExecution(ctx, accepted.Chat.ID)
	if err != nil || initial.InitialExecution.AutomaticRunID != intent.AutomaticRunID {
		t.Fatal("journal lost trusted automatic origin", initial, err)
	}
	replay, err := manager.execution.Start(ctx, intent)
	if err != nil || replay.Execution.AutomaticRunID != intent.AutomaticRunID {
		t.Fatal(replay, err)
	}
	changed := intent
	changed.AutomaticRunID = "another-run"
	if _, err = manager.execution.Start(ctx, changed); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatal("same execution changed its automatic origin", err)
	}
	continued, err := manager.sendTestMessage(ctx, testMessageInput{ChatID: accepted.Chat.ID, Message: userMessage{Text: "用户后续操作"}, Runtime: runtimeSelection{RuntimeID: agent.RuntimeIDCodex}})
	if err != nil {
		t.Fatal(err)
	}
	if continued.Execution.AutomaticRunID != "" {
		t.Fatal("manual continuation inherited API caller", continued.Execution)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
}

// nativeFixtureEnvironmentReader replaces only the external process; model and permission
// snapshots are still supplied by the test exercising product validation.
type nativeFixtureEnvironmentReader struct {
	// source is the runtime environment used by the business path.
	source runtimeEnvironmentReader
}

func fixtureCodexSummary() agent.RuntimeSummary {
	return agent.RuntimeSummary{ID: agent.RuntimeIDCodex, BaseRuntimeID: agent.RuntimeIDCodex, InstructionFileName: "AGENTS.md", SkillDirectoryName: ".agents"}
}

func (reader nativeFixtureEnvironmentReader) Get(ctx context.Context, id string) (models.RuntimeEnvironment, error) {
	entry, err := reader.source.Get(ctx, id)
	if err != nil {
		return entry, err
	}
	binary, err := os.Executable()
	if err != nil {
		return entry, err
	}
	entry.Binary, entry.Detection.ResolvedBinary = binary, binary
	entry.PrefixArgs = []string{"-test.run=^TestNativeExecutionCodexChild$", "--"}
	if entry.BaseRuntimeID == "" {
		entry.BaseRuntimeID = entry.Detection.Runtime.BaseRuntimeID
	}
	if entry.BaseRuntimeID == agent.RuntimeIDCodex {
		entry.Detection.Runtime = fixtureCodexSummary()
	}
	return entry, nil
}

func (s messageTestService) nativeCalls(t *testing.T, method string) []json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(s.trace)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var calls []json.RawMessage
	for _, line := range strings.Split(string(data), "\n") {
		var request struct {
			// Method is the app-server method name.
			Method string `json:"method"`
			// Params is the app-server request payload.
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal([]byte(line), &request) == nil && request.Method == method {
			calls = append(calls, request.Params)
		}
	}
	return calls
}

type recordedRun struct {
	// SessionPlan captures the projected capabilities and managed workspace instructions.
	agent.SessionPlan
	// TurnInput captures the prompt assembly.
	agent.TurnInput
}
