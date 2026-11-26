package execution

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/changes"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventstream"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	usageservice "github.com/JieWaZi/NeuxBot/server/services/usage"
	acp "github.com/JieWaZi/acp-go-sdk"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	"github.com/google/uuid"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestNativeTurnPromptPreservesBusinessReferences(t *testing.T) {
	turn := nativePromptFixture()
	text := turn.prompt()
	for _, want := range []string{"review this", "report.csv", "/tmp/report.csv", "main.go", "return 42", "search-guide"} {
		if !strings.Contains(text, want) {
			t.Errorf("prompt lost %q: %s", want, text)
		}
	}
	if !strings.Contains(text, `{"path":"go.mod"}`) || strings.Contains(text, "previous answer") {
		t.Fatalf("native input includes unnecessary history or file fields: %s", text)
	}
}

func TestNativeTurnResultUsesProviderTerminalEvidence(t *testing.T) {
	result := agent.TurnResult{BusinessAdmitted: true, Admission: "accepted", Submission: "sent", OperationError: context.Canceled}
	outcome := nativeExecutionOutcome(result)
	if string(outcome.status) != "canceled" {
		t.Fatalf("canceled native wait projected as %s", outcome.status)
	}
}

func TestNativeTurnQuotaFailureUsesActionableCode(t *testing.T) {
	for _, result := range []agent.TurnResult{
		{Outcome: &agent.AgentNativeOutcome{NativeFailure: status_error.RuntimeExecutionFailed, NativeErrorKind: "quota_exhausted"}},
		{OperationError: status_error.RuntimeQuotaExhausted},
	} {
		outcome := nativeExecutionOutcome(result)
		if outcome.code != "RuntimeQuotaExhausted" {
			t.Fatalf("quota failure became generic: %+v", outcome)
		}
	}
}

func TestNativeTurnFailurePreservesAPIMeaning(t *testing.T) {
	err := status_error.RuntimeQuotaExhausted.WithMessage("Free usage limit reached. Check your OpenCode plan.")
	outcome := nativeExecutionOutcome(agent.TurnResult{OperationError: err})
	if outcome.message != "Free usage limit reached. Check your OpenCode plan." || outcome.err != err || outcome.code != "RuntimeQuotaExhausted" || !errors.Is(outcome.err, status_error.RuntimeQuotaExhausted) {
		t.Fatalf("message and HTTP error disagree: %+v", outcome)
	}
}

func nativePromptFixture() recordedRun {
	return recordedRun{
		Message: agent.Message{
			Text:           "review this",
			Attachments:    []agent.Attachment{{Name: "report.csv", Path: "artifacts/report.csv", RuntimePath: "/tmp/report.csv"}},
			CodeReferences: []agent.CodeReference{{Path: "main.go", StartLine: 2, EndLine: 2, Content: "return 42"}, {Path: "go.mod"}},
		},
		SelectedSkills: []string{"search-guide"},
	}
}

type nativeProductFixture struct {
	// root 是 Core 重启时复用的存储根目录。
	root string
	// trace 是原生子进程接收的请求日志。
	trace string
	// accounts 记录原生连接获取账号租约的次数。
	accounts *atomic.Int32
	// failAccountAcquire 指定冷账号获取失败的租约序号，零表示正常获取。
	failAccountAcquire *atomic.Int32
	// failAccountClose 指定清理失败的真实账号租约序号，零表示正常释放。
	failAccountClose *atomic.Int32
	// usage 提供从原生终态重建用量查询的 SQLite 投影。
	usage *controllers.UsageController
	// open 重新装配独立的 Core、Thread、Provider 和 ConversationStore。
	open func() (Core, chatservice.Module, *conversation.Store, func())
}

func newNativeProductFixture(t *testing.T, ctx context.Context, providerID, child string, permission constants.PermissionLevel) nativeProductFixture {
	t.Helper()
	return newNativeProductFixtureWithAccounts(t, ctx, providerID, child, permission, nil)
}

func newNativeProductFixtureWithAccounts(t *testing.T, ctx context.Context, providerID, child string, permission constants.PermissionLevel, accountService *aiaccount.Service) nativeProductFixture {
	t.Helper()
	root, trace := t.TempDir(), filepath.Join(t.TempDir(), providerID+"-trace.jsonl")
	t.Setenv("NEUX_EXECUTION_FIXTURE_TRACE", trace)
	t.Setenv("PWD", filepath.Join(root, "first-host-directory"))
	t.Setenv("NEUX_EXECUTION_FIXTURE_BASE_VALUE", "inherited-first")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "execution.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	db := database.DB
	sql, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sql.Close() })
	wakers := controllers.NewWakerController(db)
	profiles := controllers.NewProfileDocumentController(db)
	projects := controllers.NewProjectController(db)
	if err := wakers.Create(ctx, models.Waker{ID: "native-waker", Name: providerID + " fixture"}); err != nil {
		t.Fatal(err)
	}
	var documents []models.WakerDocument
	for i, kind := range []constants.ProfileDocumentKind{constants.ProfileDocumentKindIdentity, constants.ProfileDocumentKindPersona, constants.ProfileDocumentKindPlaybook, constants.ProfileDocumentKindMemory} {
		documents = append(documents, models.WakerDocument{ID: fmt.Sprintf("fixture-document-%d", i), WakerID: "native-waker", Kind: kind, Content: "Fixture instructions."})
	}
	if err := profiles.CreateWakerDocuments(ctx, documents); err != nil {
		t.Fatal(err)
	}
	summary := agent.RuntimeSummary{ID: providerID, BaseRuntimeID: providerID, InstructionFileName: "AGENTS.md", SkillDirectoryName: ".agents"}
	switch providerID {
	case "claude":
		summary.InstructionFileName, summary.SkillDirectoryName = "CLAUDE.md", ".claude"
	case "cursor":
		summary.SkillDirectoryName = ".cursor"
	case "workbuddy":
		summary.SkillDirectoryName = ".codebuddy"
	case "qoder":
		summary.SkillDirectoryName = ".qoder"
	}
	model := agent.ModelOption{ID: "fixture-model", PermissionLevels: []constants.PermissionLevel{permission}}
	if providerID == constants.RuntimeIDPi {
		model.ID = "test/fixture-model"
	}
	standardACP := providerID != "codex" && providerID != "claude" && providerID != "cursor"
	if standardACP {
		model.PermissionLevels = []constants.PermissionLevel{constants.PermissionLevelDefault, constants.PermissionLevelAuto, constants.PermissionLevelFullAccess}
	}
	if providerID != "codex" {
		model.ReasoningOptions = []agent.ReasoningOption{{ID: "high"}, {ID: "low"}}
	}
	fastOn := true
	environment := nativeFixtureEnvironment{entry: models.RuntimeEnvironment{
		DefaultFast: &fastOn,
		RuntimeID:   providerID, BaseRuntimeID: providerID, Enabled: true, Binary: executable, PrefixArgs: []string{"-test.run=^" + child + "$", "--"},
		Detection: agent.DetectionResult{State: agent.DetectionStateReady, Runtime: summary, ResolvedBinary: executable,
			Models: []agent.ModelOption{model}},
	}}
	if standardACP {
		environment.entry.DefaultFast = nil
	}
	if providerID == constants.RuntimeIDCodex {
		environment.entry.ConfigHome = os.Getenv("NEUX_EXECUTION_FIXTURE_CODEX_HOME")
	}
	accounts := new(atomic.Int32)
	failAccountClose := new(atomic.Int32)
	failAccountAcquire := new(atomic.Int32)
	open := func() (Core, chatservice.Module, *conversation.Store, func()) {
		t.Helper()
		store, err := conversation.NewStore(root)
		if err != nil {
			t.Fatal(err)
		}
		provider := agent.NewProviderRuntime(agent.ProviderRuntimeOptions{
			AcquireAccount: func(ctx context.Context, account agent.AccountRequest) (agent.AccountLease, error) {
				if account.Provider != providerID || accountService == nil && account.Subject != "fixture-subject" {
					return agent.AccountLease{}, status_error.AIAccountConfigurationInvalid
				}
				leaseNumber := accounts.Add(1)
				if failAccountAcquire.Load() == leaseNumber {
					return agent.AccountLease{}, status_error.RuntimeUnavailable
				}
				if accountService != nil {
					return accountService.AcquireAccount(ctx, account)
				}
				if providerID == constants.RuntimeIDPi {
					return agent.AccountLease{Environment: map[string]string{"PI_CODING_AGENT_DIR": filepath.Join(root, "pi-fixture-account")}}, nil
				}

				return agent.AccountLease{DataHome: account.DataHome, Close: func(context.Context) error {
					if failAccountClose.Load() == leaseNumber {
						return status_error.RuntimeUnavailable
					}
					return nil
				}}, nil
			},
		})
		threads := agent.NewThreadLifecycle(provider, store)
		chats, err := chatservice.New(chatservice.Config{Conversations: store, StorageDirectory: root, Projects: projects,
			QueryIndexes: chatservice.QueryIndexDependencies{Chats: controllers.NewChatController(db),
				Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db)), DeletionIntents: controllers.NewChatDeletionIntentController(db)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := chats.Start(ctx); err != nil {
			t.Fatal(err)
		}
		core, err := New(Config{Threads: threads, ResolveAccount: func(ctx context.Context, request agent.AccountRequest, home string) (agent.AccountRequest, error) {
			if accountService != nil {
				return accountService.ResolveAccount(ctx, request, home)
			}
			request.Subject = "fixture-subject"
			request.DataHome = os.Getenv("NEUX_EXECUTION_FIXTURE_ACCOUNT_HOME")
			return request, nil
		}, Journal: chats, DormantChats: chats, Wakers: wakers, Profiles: profiles, Projects: projects, RuntimeSummaries: []agent.RuntimeSummary{summary},
			RuntimeEnvironments: environment, SkillDataDirectory: root, StorageDirectory: root})
		if err != nil {
			t.Fatal(err)
		}
		if err := core.Open(ctx); err != nil {
			t.Fatal(err)
		}
		return core, chats, store, func() {
			core.Close()
			if err := threads.Close(context.Background()); err != nil {
				t.Error(err)
			}
			if err := provider.Close(context.Background()); err != nil {
				t.Error(err)
			}
			chats.Close()
			store.Close()
		}
	}
	return nativeProductFixture{root: root, trace: trace, accounts: accounts, failAccountClose: failAccountClose, failAccountAcquire: failAccountAcquire, usage: controllers.NewUsageController(db), open: open}
}

func TestNativeProductHostConnectionSurvivesHotContinuation(t *testing.T) {
	t.Run("explicit_with_unavailable_fallback", func(t *testing.T) { testNativeProductHostConnection(t, false) })
	t.Run("filtered", func(t *testing.T) { testNativeProductHostConnection(t, true) })
}

func testNativeProductHostConnection(t *testing.T, filtered bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	t.Setenv("NEUX_EXECUTION_FIXTURE_CHECK_GRANT", "1")
	if filtered {
		home := t.TempDir()
		config := "[shell_environment_policy]\ninclude_only = ['USER_*', 'PATH']\n[shell_environment_policy.set]\nUSER_MARKER = 'preserved'\n"
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("NEUX_EXECUTION_FIXTURE_CODEX_HOME", home)
	}
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	name := constants.AgentCLIBinaryName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	if filtered {
		build := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/botcli")
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("构建受管命令失败: %v\n%s", err, output)
		}
	} else if err := os.WriteFile(binary, []byte("test CLI file fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	skill := publishedTestSystemSkill(t, fixture.root, binary)
	bundledCLI := builtinskills.CLIPath(skill)
	var inheritedPath string
	if filtered {
		t.Setenv("NEUX_EXECUTION_FIXTURE_BOTCLI", bundledCLI)
		// 放入同名 CLI，证明原生进程优先选择系统技能随附的版本。
		inheritedPath = strings.Join([]string{filepath.Dir(binary), os.Getenv("PATH")}, string(filepath.ListSeparator))
		t.Setenv("PATH", inheritedPath)
	} else {
		contextRoot := filepath.Join(fixture.root, constants.AgentCLIContextDirectory)
		if err := os.MkdirAll(contextRoot, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(contextRoot, "codex-threads"), []byte("unavailable fallback directory"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	open := func() (Core, *conversation.Store, func()) {
		core, _, store, closeRuntime := fixture.open()
		registry := capability.New(nil)
		manager := core.(*service)
		manager.agent = registry
		manager.agentEndpoint = "http://127.0.0.1:18080" + constants.AgentCLIPath
		manager.systemSkillDirectory = skill
		closeEndpoint := func() {}
		if filtered {
			endpoint := httptest.NewServer(registry)
			manager.agentEndpoint = endpoint.URL + constants.AgentCLIPath
			manager.automaticTaskBindings = func(models.Chat, string, ExecutionConfiguration) []capability.Binding {
				return []capability.Binding{
					agentPreparationBinding(t, constants.AutomaticTaskCreateOperation),
					agentPreparationBinding(t, constants.AutomaticTaskListOperation),
					agentPreparationBinding(t, constants.AutomaticTaskUpdateOperation),
				}
			}
			closeEndpoint = endpoint.Close
		}
		return core, store, func() {
			closeRuntime()
			registry.Close()
			closeEndpoint()
		}
	}
	core, store, closeRuntime := open()
	defer func() { closeRuntime() }()
	options := WorkOptions{RuntimeID: "codex", Model: "fixture-model", PermissionLevel: constants.PermissionLevelAuto}
	if filtered {
		options.EntryPoint = WorkbenchEntryPoint
	}
	intent := WorkIntent{ClientRequestID: "host-grant", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}}, Input: WorkInput{Actor: "user", Content: "first clean input"}, Options: options}
	run := func(message string) agent.NativeSessionKey {
		t.Helper()
		intent.Input.Content = message
		accepted, err := core.Start(ctx, intent)
		if err != nil {
			t.Fatal(err)
		}
		waitNativeProductStatus(t, ctx, store, accepted, constants.ExecutionStatusCompleted)
		if err := core.CancelExecutionAndWait(ctx, accepted.Chat.ID, accepted.Execution.ID); err != nil {
			t.Fatal(err)
		}
		intent.ClientRequestID = ""
		intent.Chat = ChatTarget{ChatID: accepted.Chat.ID}
		binding, err := store.ReadBinding(ctx, accepted.Chat.ID)
		if err != nil || binding == nil {
			t.Fatalf("读取原生绑定失败: %v", err)
		}
		return binding.Session
	}
	first, second := run("first clean input"), run("second clean input")
	if first != second || fixture.accounts.Load() != 1 {
		t.Fatal("热续聊重新创建了原生会话或进程")
	}
	wantTurns := 2
	if filtered {
		old, err := capability.ReadCodexShellConnection(bundledCLI, first.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		closeRuntime()
		if _, err := capability.ReadCodexShellConnection(bundledCLI, first.SessionID); !errors.Is(err, status_error.AgentContextStorageError) {
			t.Fatalf("关闭 Core 后残留旧的 Shell 入口: %v", err)
		}
		core, store, closeRuntime = open()
		resumed := run("continued after Core restart")
		current, err := capability.ReadCodexShellConnection(bundledCLI, resumed.SessionID)
		if err != nil || resumed != first || current.CoreEpoch == old.CoreEpoch || current.GrantID == old.GrantID || fixture.accounts.Load() != 2 {
			t.Fatalf("Core 重启后没有在原生会话中绑定新凭据: %v", err)
		}
		wantTurns = 3
	}
	trace, err := os.ReadFile(fixture.trace)
	if err != nil {
		t.Fatal(err)
	}
	var grants []string
	for _, line := range strings.Split(string(trace), "\n") {
		if grant, ok := strings.CutPrefix(line, "host grant "); ok {
			grants = append(grants, grant)
		}
		if strings.Contains(line, `"method":"turn/start"`) && strings.Contains(line, "neuxbot-turn-context") {
			t.Fatalf("host context entered native user input: %s", line)
		}
	}
	if len(grants) != wantTurns || grants[0] != grants[1] {
		t.Fatalf("heated continuation did not reuse the process and connection: grants=%d launches=%d", len(grants), fixture.accounts.Load())
	}
	if filtered && strings.Count(string(trace), "filtered botcli succeeded\n") != wantTurns {
		t.Fatalf("各回合的受限 Shell 都应能直接调用随附命令: %s", trace)
	}
	if filtered && strings.Count(string(trace), "filtered executable fallback succeeded\n") != wantTurns {
		t.Fatalf("Shell 移除 PATH 时仍应能调用随附命令: %s", trace)
	}
	if filtered && os.Getenv("PATH") != inheritedPath {
		t.Fatal("原生执行修改了宿主进程的 PATH")
	}
	if filtered {
		assertNativeAutomaticTaskInstructions(t, trace)
	}
}

func assertNativeAutomaticTaskInstructions(t *testing.T, trace []byte) {
	t.Helper()
	var methods, prompts []string
	for _, line := range strings.Split(string(trace), "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var frame struct {
			// Method 是 Codex 原生协议的方法名。
			Method string `json:"method"`
			// Params 是原生线程或回合的请求参数。
			Params struct {
				// DeveloperInstructions 是线程级开发者指令。
				DeveloperInstructions string `json:"developerInstructions"`
				// Input 是本轮发送的输入块。
				Input []struct {
					// Text 是输入块的正文。
					Text string `json:"text"`
				} `json:"input"`
			} `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatal(err)
		}
		switch frame.Method {
		case "thread/start", "thread/resume":
			methods = append(methods, frame.Method)
			if strings.Count(frame.Params.DeveloperInstructions, agent.AutomaticTaskCreationInstructions) != 1 ||
				strings.Count(frame.Params.DeveloperInstructions, agent.AutomaticTaskEditingInstructions) != 1 {
				t.Errorf("%s must carry the automatic task instructions exactly once", frame.Method)
			}
		case "turn/start":
			methods = append(methods, frame.Method)
			if len(frame.Params.Input) != 1 {
				t.Fatalf("expected only the current user input: %+v", frame.Params.Input)
			}
			prompts = append(prompts, frame.Params.Input[0].Text)
		}
	}
	if want := []string{"thread/start", "turn/start", "turn/start", "thread/resume", "turn/start"}; !slices.Equal(methods, want) {
		t.Errorf("automatic task capability changed the native session lifecycle: got %v, want %v", methods, want)
	}
	if want := []string{"first clean input", "second clean input", "continued after Core restart"}; !slices.Equal(prompts, want) {
		t.Errorf("automatic task capability changed user text: got %q, want %q", prompts, want)
	}
}

func TestNativeProductWorkflowResumesAfterToolConnectionChanges(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	binary := filepath.Join(t.TempDir(), "botcli")
	if err := os.WriteFile(binary, []byte("test CLI file fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	skill := publishedTestSystemSkill(t, fixture.root, binary)
	open := func() (Core, *conversation.Store, func()) {
		core, _, store, closeRuntime := fixture.open()
		registry := capability.New(nil)
		core.(*service).agent = registry
		core.(*service).agentEndpoint = "http://127.0.0.1:18080" + constants.AgentCLIPath
		core.(*service).systemSkillDirectory = skill
		return core, store, func() {
			closeRuntime()
			registry.Close()
		}
	}
	options := WorkOptions{RuntimeID: "codex", Model: "fixture-model", PermissionLevel: constants.PermissionLevelAuto,
		ToolCapabilities: []ToolCapability{{ID: "workflow.sample", Name: "Workflow sample", Bindings: []capability.Binding{
			agentPreparationBinding(t, "workflow.source.get"),
			agentPreparationBinding(t, "workflow.source.save"),
			agentPreparationBinding(t, "workflow.authoring.prepare"),
		}}},
	}
	run := func(core Core, store *conversation.Store, intent WorkIntent) (AcceptedExecution, agent.NativeSessionKey) {
		t.Helper()
		accepted, err := core.Start(ctx, intent)
		if err != nil {
			t.Fatal(err)
		}
		waitNativeProductStatus(t, ctx, store, accepted, constants.ExecutionStatusCompleted)
		if err := core.CancelExecutionAndWait(ctx, accepted.Chat.ID, accepted.Execution.ID); err != nil {
			t.Fatal(err)
		}
		input, _, err := store.ReadHostInputConsumption(ctx, agent.HostInputRef{ChatID: accepted.Chat.ID, HostInvocationID: accepted.Execution.ID})
		if err != nil || input.Session.SessionID == "" {
			t.Fatalf("read native binding: %+v %v", input, err)
		}
		return accepted, input.Session
	}
	core, store, closeRuntime := open()
	defer func() { closeRuntime() }()
	first, session := run(core, store, WorkIntent{ClientRequestID: "workflow-core-restart", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}}, Input: WorkInput{
		Actor: "user", Content: "first workflow input",
		ContextInstructions: []workinput.Instruction{{Name: "WORKFLOW_CANVAS_REFERENCES.md", Content: `[{"revisionId":"test-revision","kind":"node","id":"test-node"}]`}},
	}, Options: options})
	closeRuntime()
	core, store, closeRuntime = open()
	intent := WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", Content: "second workflow input"}, Options: options}
	_, resumed := run(core, store, intent)
	if resumed != session || fixture.accounts.Load() != 2 {
		t.Fatalf("new tool connection changed native history: first=%+v resumed=%+v launches=%d", session, resumed, fixture.accounts.Load())
	}
	intent.Options.ToolCapabilities[0].Bindings = append(intent.Options.ToolCapabilities[0].Bindings, agentPreparationBinding(t, "workflow.definitions.list"))
	_, changed := run(core, store, intent)
	if changed == resumed {
		t.Fatal("Codex reused a native session with a different tool catalog")
	}
	trace, err := os.ReadFile(fixture.trace)
	if err != nil {
		t.Fatal(err)
	}
	var methods, prompts []string
	for _, line := range strings.Split(string(trace), "\n") {
		var frame struct {
			// Method 是 Codex 原生协议的方法名。
			Method string `json:"method"`
			// Params 是发往原生线程或回合的参数。
			Params struct {
				// DeveloperInstructions 是原生线程收到的开发者指令。
				DeveloperInstructions string `json:"developerInstructions"`
				// Input 是原生回合收到的输入块。
				Input []struct {
					// Text 是输入块中的文本。
					Text string `json:"text"`
				} `json:"input"`
			} `json:"params"`
		}
		if !strings.HasPrefix(line, "{") {
			continue
		}
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatal(err)
		}
		switch frame.Method {
		case "thread/start", "thread/resume":
			methods = append(methods, frame.Method)
			if strings.Count(frame.Params.DeveloperInstructions, "Create or modify a reusable NeuxBot workflow in this chat.") != 1 {
				t.Errorf("%s did not receive exactly one workflow authoring bootstrap", frame.Method)
			}
		case "turn/start":
			methods = append(methods, frame.Method)
			blocks := frame.Params.Input
			if len(blocks) == 0 {
				t.Fatal("workflow turn lost the user input")
			}
			switch len(prompts) {
			case 0:
				if len(blocks) != 2 || !strings.Contains(blocks[1].Text, `"Current workflow references":`) || !strings.Contains(blocks[1].Text, "test-node") {
					t.Errorf("selected canvas reference must travel in a separate context block: %+v", blocks)
				}
			case 1:
				if len(blocks) != 1 {
					t.Errorf("resumed turn without a selection must send only the user input: %+v", blocks)
				}
			case 2:
				if len(blocks) != 2 || !strings.Contains(blocks[1].Text, `"Recent conversation history":`) || strings.Contains(blocks[1].Text, `"Current workflow references":`) {
					t.Errorf("replacement thread must carry history without stale or empty references: %+v", blocks)
				}
			}
			prompts = append(prompts, blocks[0].Text)
		}
	}
	want := []string{"thread/start", "turn/start", "thread/resume", "turn/start", "thread/start", "turn/start"}
	if !slices.Equal(methods, want) {
		t.Errorf("workflow authoring lifecycle: got %v, want %v", methods, want)
	}
	if want := []string{"first workflow input", "second workflow input", "second workflow input"}; !slices.Equal(prompts, want) {
		t.Errorf("workflow authoring changed user text: got %q, want %q", prompts, want)
	}
}

func TestNativeProductLocalWorkflowSendsOnlyCurrentSelection(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, _, store, closeRuntime := fixture.open()
	defer closeRuntime()
	manager, tools := core.(*service), agentPreparationFixture(t)
	manager.agent, manager.agentEndpoint, manager.systemSkillDirectory = tools.agent, tools.agentEndpoint, tools.systemSkillDirectory
	workflowBinding := agentPreparationBinding(t, "workflow.runs.start")
	manager.bindLocalWorkflowTurn = func(_ context.Context, _ models.Chat, _ string, selected *workinput.WorkflowReference, _ string) (WorkflowTurnBinding, error) {
		bound := WorkflowTurnBinding{Capability: &ToolCapability{ID: "workflow.local", Bindings: []capability.Binding{workflowBinding}}}
		if selected != nil {
			bound.Reference = &workinput.WorkflowReference{ID: selected.ID, RevisionID: "selected-v1"}
		}
		return bound, nil
	}
	intent := WorkIntent{
		ClientRequestID: "workflow-clean-turns",
		Chat:            ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}},
		Input:           WorkInput{Actor: "user"},
		Options:         WorkOptions{RuntimeID: "codex", Model: "fixture-model", PermissionLevel: constants.PermissionLevelAuto},
	}
	want := []string{"你好", "搜集今日新闻", "运行选中的工作流", "继续回答"}
	for i, message := range want {
		intent.Input.Content = message
		intent.Input.Workflow = nil
		if i == 2 {
			intent.Input.Workflow = workinput.SelectWorkflow("selected-definition")
		}
		accepted, err := core.Start(ctx, intent)
		if err != nil {
			t.Fatal(err)
		}
		waitNativeProductStatus(t, ctx, store, accepted, constants.ExecutionStatusCompleted)
		if err := core.CancelExecutionAndWait(ctx, accepted.Chat.ID, accepted.Execution.ID); err != nil {
			t.Fatal(err)
		}
		intent.ClientRequestID = ""
		intent.Chat = ChatTarget{ChatID: accepted.Chat.ID}
	}
	trace, err := os.ReadFile(fixture.trace)
	if err != nil {
		t.Fatal(err)
	}
	starts := 0
	var prompts []string
	for _, line := range strings.Split(string(trace), "\n") {
		switch {
		case strings.Contains(line, `"method":"thread/start"`):
			starts++
			if !strings.Contains(line, `"developerInstructions"`) || !strings.Contains(line, "bot_capabilities") ||
				strings.Contains(line, "Follow the user's request to read, edit, or run a workflow") || strings.Contains(line, "workflow.source.save receipt proves saving") {
				t.Fatal("ordinary chat must keep its tools without workflow-specific instructions")
			}
		case strings.Contains(line, `"method":"turn/start"`):
			var frame struct {
				// Params 保存发往真实协议子进程的原生回合参数。
				Params struct {
					// Input 是将写入原生用户历史的输入块。
					Input []struct {
						// Text 是原生用户文本。
						Text string `json:"text"`
					} `json:"input"`
				} `json:"params"`
			}
			if err := json.Unmarshal([]byte(line), &frame); err != nil || len(frame.Params.Input) == 0 {
				t.Fatalf("invalid native user turn: %v", err)
			}
			if len(prompts) == 2 {
				if len(frame.Params.Input) != 2 {
					t.Fatal("selected workflow must travel in a separate context block")
				}
				context := frame.Params.Input[1].Text
				start, end := "\n\n<neuxbot-turn-context>\n", "\n</neuxbot-turn-context>"
				if !strings.HasPrefix(context, start) || !strings.HasSuffix(context, end) ||
					!json.Valid([]byte(strings.TrimSuffix(strings.TrimPrefix(context, start), end))) ||
					!strings.Contains(context, `"Workflow target":{`) || !strings.Contains(context, "selected-v1") ||
					strings.Contains(context, "Use workflow.definitions.list") {
					t.Fatalf("invalid context envelope: %q", context)
				}
			} else if len(frame.Params.Input) != 1 {
				t.Fatal("plain user turn contained extra input blocks")
			}
			prompts = append(prompts, frame.Params.Input[0].Text)
		}
	}
	if starts != 1 || len(prompts) != len(want) || fixture.accounts.Load() != 1 {
		t.Fatalf("workflow selection restarted the conversation: starts=%d turns=%d launches=%d", starts, len(prompts), fixture.accounts.Load())
	}
	for i, prompt := range prompts {
		if prompt != want[i] {
			t.Fatalf("plain native turn %d contains extra text: %q", i+1, prompt)
		}
	}
}

func TestNativeWorkflowTurnGrantsOnlyItsUploadedFileWithoutASelection(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, chats, _, closeRuntime := fixture.open()
	defer closeRuntime()
	registry := capability.New(nil)
	defer registry.Close()
	binary := filepath.Join(t.TempDir(), "botcli")
	if err := os.WriteFile(binary, []byte("CLI fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	manager := core.(*service)
	manager.agent, manager.agentEndpoint = registry, "http://127.0.0.1:18080"+constants.AgentCLIPath
	manager.systemSkillDirectory = publishedTestSystemSkill(t, fixture.root, binary)
	upload := filepath.Join(t.TempDir(), "report.csv")
	if err := os.WriteFile(upload, []byte("actual upload"), 0o600); err != nil {
		t.Fatal(err)
	}
	var paths, roots []string
	start := agentPreparationBinding(t, "workflow.runs.start")
	start.Call = func(ctx context.Context, _ capability.Request) (capability.Result, error) {
		paths, roots = capability.FilePaths(ctx), capability.FileRoots(ctx)
		return capability.Result{}, status_error.AgentOperationForbidden
	}
	accepted, err := core.Start(ctx, WorkIntent{
		ClientRequestID: "named-workflow-upload",
		Chat:            ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}},
		Input:           WorkInput{Actor: "user", Content: "hold-for-stream", Attachments: []AttachmentInput{{SourcePath: upload}}},
		Options: WorkOptions{RuntimeID: "codex", Model: "fixture-model", PermissionLevel: constants.PermissionLevelAuto,
			ToolCapabilities: []ToolCapability{{ID: "workflow.local", Name: "Workflow", Bindings: []capability.Binding{start}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductLive(t, ctx, chats, accepted, "fixture partial")
	defer func() {
		if err := core.CancelExecutionAndWait(ctx, accepted.Chat.ID, accepted.Execution.ID); err != nil {
			t.Error(err)
		}
	}()
	contextRoot := filepath.Join(filepath.Dir(filepath.Dir(manager.systemSkillDirectory)), constants.AgentCLIContextDirectory)
	connections, err := filepath.Glob(filepath.Join(contextRoot, "*", "*", "connection.json"))
	if err != nil || len(connections) != 1 {
		t.Fatalf("expected one native tool connection: %v %v", connections, err)
	}
	connection, err := capability.ReadConnection(connections[0])
	if err != nil {
		t.Fatal(err)
	}
	body := strings.NewReader(`{"operation":"workflow.runs.start","requestId":"named-workflow","params":{"definitionId":"workflow","revisionId":"revision","args":{}}}`)
	request := httptest.NewRequest(http.MethodPost, constants.AgentCLIPath+"/commands", body)
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Authorization", "Bearer "+connection.Token)
	request.Header.Set(constants.AgentCLIConnectionHeader, connection.GrantID)
	request.Header.Set(constants.AgentCLIExecutionHeader, accepted.Execution.ID)
	response := httptest.NewRecorder()
	registry.ServeHTTP(response, request)
	if len(paths) != 1 || paths[0] == upload || filepath.Base(paths[0]) != filepath.Base(upload) {
		t.Fatalf("expected the one materialized upload path, got %v; tool response %s", paths, response.Body)
	}
	content, err := os.ReadFile(paths[0])
	if err != nil || string(content) != "actual upload" {
		t.Fatalf("granted path did not match uploaded bytes: %q %v", content, err)
	}
	for _, root := range roots {
		relative, err := filepath.Rel(root, paths[0])
		if err == nil && filepath.IsLocal(relative) {
			t.Fatalf("upload parent is accessible through granted directory %q", root)
		}
	}
}

func TestNativeProductDurationStartsBeforePreparation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, chats, store, closeRuntime := fixture.open()
	defer closeRuntime()
	manager := core.(*service)
	validate := manager.validateIntent
	var beforePreparation, afterPreparation int64
	manager.validateIntent = func(ctx context.Context, intent WorkIntent) error {
		beforePreparation = nowMillis()
		time.Sleep(10 * time.Millisecond)
		afterPreparation = nowMillis()
		if validate != nil {
			return validate(ctx, intent)
		}
		return nil
	}
	intent := WorkIntent{ClientRequestID: "preparation-duration", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}},
		Input:   WorkInput{Actor: "user", Content: "first-success"},
		Options: WorkOptions{RuntimeID: "codex", Model: "fixture-model", Reasoning: "high", PermissionLevel: constants.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyContinue}}
	for range 2 {
		accepted, err := core.Start(ctx, intent)
		if err != nil {
			t.Fatal(err)
		}
		waitNativeProductStatus(t, ctx, store, accepted, constants.ExecutionStatusCompleted)
		history, err := chats.ReadMessageHistory(ctx, accepted.Chat.ID, 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		message := history.Resources[len(history.Resources)-1]
		if message.RunMetrics == nil || message.RunMetrics.StartedAt <= 0 || message.RunMetrics.StartedAt > beforePreparation ||
			message.RunMetrics.DurationMs < afterPreparation-beforePreparation {
			t.Fatalf("request preparation excluded: metrics=%+v before=%d after=%d", message.RunMetrics, beforePreparation, afterPreparation)
		}
		intent.ClientRequestID = ""
		intent.Chat = ChatTarget{ChatID: accepted.Chat.ID}
	}
}

func TestNativeProductCodexCompletesStreamsCancelsAndRestarts(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	root, trace, accounts, open := fixture.root, fixture.trace, fixture.accounts, fixture.open
	fastOff := false
	core, chats, store, closeRuntime := open()
	defer func() { closeRuntime() }()
	options := WorkOptions{RuntimeID: "codex", Model: "fixture-model", Reasoning: "high", PermissionLevel: constants.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyContinue}
	first, err := core.Start(ctx, WorkIntent{ClientRequestID: "codex-native-first", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}},
		Input: WorkInput{Actor: "user", Content: "first-success"}, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	if first.Execution.Status != constants.ExecutionStatusRunning || first.Execution.Fast == nil || !*first.Execution.Fast {
		t.Fatalf("start = %+v", first.Execution)
	}
	subscription, err := chats.SubscribeEvents(ctx, first.Chat.ID, first.Chat.LatestSequence)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	waitNativeProductStatus(t, ctx, store, first, constants.ExecutionStatusCompleted)
	if err := core.CancelExecutionAndWait(ctx, first.Chat.ID, first.Execution.ID); err != nil {
		t.Fatal(err)
	}
	outcome, err := chats.ReadExecutionOutcome(ctx, first.Chat.ID, first.Execution.ID)
	if err != nil || outcome.InvocationID == "" || outcome.TerminationReason != constants.RuntimeTerminationReasonNormal {
		t.Fatalf("public native outcome: %+v %v", outcome, err)
	}
	fileChanges, err := chats.GetMessageFileChanges(ctx, first.Chat.ID, first.Execution.ID)
	if err != nil || fileChanges.State != "ready" || !slices.ContainsFunc(fileChanges.Files, func(file changes.File) bool {
		return file.Path == "fixture-change.txt"
	}) {
		t.Fatalf("file capture lost an actual workspace write: %+v %v", fileChanges, err)
	}

	input, consumed, err := store.ReadHostInputConsumption(ctx, agent.HostInputRef{ChatID: first.Chat.ID, HostInvocationID: first.Execution.ID})
	if err != nil || input.Session.SessionID == "" || consumed == nil {
		t.Fatalf("durable admission/consumption: input=%+v consumption=%+v err=%v", input, consumed, err)
	}
	result, err := store.ReadNativeResult(ctx, consumed.Result)
	if err != nil || result.Turn.Usage == nil || result.Turn.Usage.InputTokens == nil || *result.Turn.Usage.InputTokens != 15 ||
		result.Turn.Usage.Complete || result.Turn.Usage.Coverage != "last_request" {
		t.Fatalf("canonical partial usage = %+v, err = %v", result.Turn.Usage, err)
	}
	history, err := chats.ReadMessageHistory(ctx, first.Chat.ID, 0, 10)
	if err != nil || len(history.Resources) != 2 || history.Resources[1].Content != "fixture reply" ||
		history.Resources[1].RunMetrics == nil || history.Resources[1].RunMetrics.StartedAt == 0 || history.Resources[1].RunMetrics.InputTokens != 0 {
		t.Fatalf("Chat history: %+v %v", history, err)
	}
	assertContext := func(metrics *messageview.MessageRunMetrics) {
		t.Helper()
		if metrics == nil || metrics.ContextUsedTokens != 26 || metrics.ContextWindowTokens != 100 {
			t.Fatalf("Codex context snapshot = %+v", metrics)
		}
	}
	assertContext(history.Resources[1].RunMetrics)
	checkTerminal := func(event chatservice.PublicEvent) bool {
		if event.Type != constants.ChatEventTypeExecutionCompleted || event.ExecutionID != first.Execution.ID {
			return false
		}
		if event.Message == nil {
			t.Fatal("terminal event lost the Codex message")
		}
		assertContext(event.Message.RunMetrics)
		return true
	}
	terminalReceived := slices.ContainsFunc(subscription.Backlog, checkTerminal)
	if !terminalReceived {
		for event := range subscription.Events {
			if checkTerminal(event) {
				terminalReceived = true
				break
			}
		}
	}
	subscription.Close()
	if !terminalReceived {
		t.Fatal("no terminal message reached the Chat subscription")
	}

	options.Model, options.Reasoning, options.Fast = "fixture-other", "medium", &fastOff
	held, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID},
		Input: WorkInput{Actor: "user", Content: "hold-for-stream"}, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	liveContext, cancelLive := context.WithTimeout(ctx, time.Second)
	live := waitNativeProductLive(t, liveContext, chats, held, "fixture partial")
	cancelLive()
	reconnectContext, cancelReconnect := context.WithTimeout(ctx, time.Second)
	replayed := waitNativeProductLive(t, reconnectContext, chats, held, "fixture partial")
	cancelReconnect()
	if replayed.Revision != live.Revision {
		t.Fatalf("reconnect lost live coverage: %+v vs %+v", replayed, live)
	}
	waitNativeProductStream(t, ctx, chats, held, "fixture partial")
	if err := core.CancelExecutionAndWait(ctx, first.Chat.ID, held.Execution.ID); err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, held, constants.ExecutionStatusCanceled)
	heldInput, _, err := store.ReadHostInputConsumption(ctx, agent.HostInputRef{ChatID: held.Chat.ID, HostInvocationID: held.Execution.ID})
	if err != nil || heldInput.Session != input.Session || held.Execution.Fast == nil || *held.Execution.Fast {
		t.Fatalf("model/Fast edit replaced the thread or lost explicit false: %+v %+v %v", heldInput, held.Execution, err)
	}

	failed, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID},
		Input: WorkInput{Actor: "user", Content: "provider-failure"}, Options: options, Admission: constants.ExecutionAdmissionCommitBeforeDispatch})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, failed, constants.ExecutionStatusFailed)
	if got := accounts.Load(); got != 1 {
		t.Fatalf("compatible hot sends took %d native account leases", got)
	}

	closeRuntime()
	// A restarted host has a different inherited snapshot and the same managed launch.
	t.Setenv("PWD", filepath.Join(root, "second-host-directory"))
	t.Setenv("NEUX_EXECUTION_FIXTURE_BASE_VALUE", "inherited-second")
	core, chats, store, closeRuntime = open()
	reopenedHistory, err := chats.ReadMessageHistory(ctx, first.Chat.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	firstReply := slices.IndexFunc(reopenedHistory.Resources, func(message messageview.Message) bool {
		return message.ID == first.Execution.ID
	})
	if firstReply < 0 {
		t.Fatal("first Codex reply missing after restart")
	}
	assertContext(reopenedHistory.Resources[firstReply].RunMetrics)
	reloaded, err := chats.LoadExecutionChat(ctx, first.Chat.ID)
	if err != nil || reloaded.LastFast == nil || *reloaded.LastFast {
		t.Fatalf("Chat restart lost explicit Fast=false: %+v %v", reloaded.LastFast, err)
	}
	resumed, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID},
		Input: WorkInput{Actor: "user", Content: "cold-resume-current-only"}, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, resumed, constants.ExecutionStatusCompleted)
	if err := core.CancelExecutionAndWait(ctx, resumed.Chat.ID, resumed.Execution.ID); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "child inherited inherited-second") {
		t.Fatal("restarted process lost its inherited environment")
	}
	if !strings.Contains(string(body), `"method":"thread/resume"`) || accounts.Load() != 2 {
		t.Fatalf("native trace did not resume once: %s", body)
	}
	traceLines := strings.Split(string(body), "\n")
	if !slices.ContainsFunc(traceLines, func(line string) bool {
		return strings.Contains(line, "first-success") && strings.Contains(line, `"effort":"high"`) && strings.Contains(line, `"serviceTier":"fixture-priority"`)
	}) || !slices.ContainsFunc(traceLines, func(line string) bool {
		return strings.Contains(line, "cold-resume-current-only") && strings.Contains(line, `"model":"fixture-other"`) && strings.Contains(line, `"serviceTier":null`)
	}) {
		t.Fatalf("requested model, effort or Fast did not reach the actual turn/start frame: %s", body)
	}
	for _, line := range traceLines {
		if strings.Contains(line, "cold-resume-current-only") && strings.Contains(line, "first-success") {
			t.Fatalf("resume replayed previous input: %s", line)
		}
	}
	rotationOptions := options
	rotationOptions.SessionPolicy = constants.RuntimeSessionPolicyNew
	rotationOptions.HistoryMode = constants.HistoryModeSummary
	rotated, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID},
		Input: WorkInput{Actor: "user", Content: "rotate-with-summary"}, Options: rotationOptions})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, rotated, constants.ExecutionStatusCompleted)
	binding, err := store.ReadBinding(ctx, first.Chat.ID)
	if err != nil || binding == nil || binding.Session.SessionID == input.Session.SessionID {
		t.Fatalf("explicit replacement did not replace the native session: %+v %v", binding, err)
	}
	body, err = os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	hiddenDelivered := slices.ContainsFunc(strings.Split(string(body), "\n"), func(line string) bool {
		return strings.Contains(line, "rotate-with-summary") && strings.Contains(line, "private fixture handoff")
	})
	if !hiddenDelivered {
		t.Fatal("replacement did not receive the old native session's private summary")
	}
	history, err = chats.ReadMessageHistory(ctx, first.Chat.ID, 0, 30)
	if err != nil || slices.ContainsFunc(history.Resources, func(message messageview.Message) bool {
		return strings.Contains(message.Content, "private fixture handoff")
	}) {
		t.Fatalf("private summary entered public history: %+v %v", history, err)
	}

}

func TestNativeProductCapturesOnlyTheAvailableFrozenWorkspace(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, chats, store, closeRuntime := fixture.open()
	defer closeRuntime()
	project, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	replacement := t.TempDir()
	options := WorkOptions{RuntimeID: "codex", Model: "fixture-model", PermissionLevel: constants.PermissionLevelAuto}
	first, err := core.Start(ctx, WorkIntent{ClientRequestID: "freeze-original-workspace", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker", WorkingDirectory: project}},
		Input: WorkInput{Actor: "user", Content: "open original working directory"}, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, first, constants.ExecutionStatusCompleted)
	if err := core.CancelExecutionAndWait(ctx, first.Chat.ID, first.Execution.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(project); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(replacement, project); err != nil {
		t.Fatal(err)
	}
	continued, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID},
		Input: WorkInput{Actor: "user", Content: "first-success"}, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, continued, constants.ExecutionStatusCompleted)
	if err := core.CancelExecutionAndWait(ctx, continued.Chat.ID, continued.Execution.ID); err != nil {
		t.Fatal(err)
	}
	summary, err := chats.GetMessageFileChanges(ctx, continued.Chat.ID, continued.Execution.ID)
	if err != nil || summary.State != "ready" || len(summary.Roots) != 1 || len(summary.Files) != 1 || summary.Files[0].Path != "fixture-change.txt" {
		t.Fatalf("file capture must use the same available workspace as the continued turn: %+v %v", summary, err)
	}
}

func TestNativeProductCapturesExplicitOldArtifactAtItsOriginalPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, chats, store, closeRuntime := fixture.open()
	defer closeRuntime()
	manager := core.(*service)
	root, err := manager.storage.AgentWorkspace("native-waker")
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.EnsureAgentWorkspace(root, "native-waker"); err != nil {
		t.Fatal(err)
	}
	selected, sibling := filepath.Join(root, "report.txt"), filepath.Join(root, "sibling.txt")
	for _, path := range []string{selected, sibling} {
		if err := os.WriteFile(path, []byte("original\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("NEUX_EXECUTION_FIXTURE_ARTIFACT", selected)
	t.Setenv("NEUX_EXECUTION_FIXTURE_ARTIFACT_SIBLING", sibling)
	accepted, err := core.Start(ctx, WorkIntent{ClientRequestID: "explicit-old-artifact", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}},
		Input: WorkInput{Actor: "user", Content: "revise-explicit-artifact", CodeReferences: []CodeReferenceInput{
			{Path: selected}, {Path: selected, StartLine: 1, EndLine: 1, Content: "original"},
		}}, Options: WorkOptions{RuntimeID: "codex", Model: "fixture-model", PermissionLevel: constants.PermissionLevelAuto}})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, accepted, constants.ExecutionStatusCompleted)
	if err := core.CancelExecutionAndWait(ctx, accepted.Chat.ID, accepted.Execution.ID); err != nil {
		t.Fatal(err)
	}
	summary, err := chats.GetMessageFileChanges(ctx, accepted.Chat.ID, accepted.Execution.ID)
	actual, _ := filepath.EvalSymlinks(selected)
	if err != nil || summary.State != "ready" || len(summary.Files) != 2 || !slices.ContainsFunc(summary.Files, func(file changes.File) bool { return file.AbsolutePath == actual && file.Status == "modified" }) {
		t.Fatalf("native execution lost exact original artifact: %+v %v", summary, err)
	}
	if data, err := os.ReadFile(sibling); err != nil || string(data) != "sibling also changed\n" {
		t.Fatalf("fixture did not exercise unselected sibling edit: %q %v", data, err)
	}
}

func TestNativeProductClaudeAndCursorContinueAndRecover(t *testing.T) {
	for _, provider := range []string{"claude", "cursor"} {
		t.Run(provider, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			permission := constants.PermissionLevelAuto
			if provider == "cursor" {
				permission = constants.PermissionLevelFullAccess
			}
			t.Setenv("NEUX_EXECUTION_FIXTURE_PROVIDER", provider)
			fixture := newNativeProductFixture(t, ctx, provider, "TestNativeExecutionClaudeCursorChild", permission)
			core, chats, store, closeRuntime := fixture.open()
			defer func() { closeRuntime() }()
			fastOff := false
			options := WorkOptions{RuntimeID: provider, Model: "fixture-model", Reasoning: "high", PermissionLevel: permission, SessionPolicy: constants.RuntimeSessionPolicyContinue}
			start := func(target ChatTarget, text string) AcceptedExecution {
				t.Helper()
				intent := WorkIntent{Chat: target, Input: WorkInput{Actor: "user", Content: text}, Options: options}
				if target.Create != nil {
					intent.ClientRequestID = "native-create-" + provider
				}
				work, err := core.Start(ctx, intent)
				if err != nil {
					trace, _ := os.ReadFile(fixture.trace)
					t.Fatalf("start %q: %v\n%s", text, err, trace)
				}
				return work
			}
			verify := func(work AcceptedExecution, want constants.ExecutionStatus) agent.NativeResultKey {
				t.Helper()
				waitNativeProductStatus(t, ctx, store, work, want)
				if err := core.CancelExecutionAndWait(ctx, work.Chat.ID, work.Execution.ID); err != nil {
					t.Fatal(err)
				}
				input, consumed, err := store.ReadHostInputConsumption(ctx, agent.HostInputRef{ChatID: work.Chat.ID, HostInvocationID: work.Execution.ID})
				if err != nil || consumed == nil || input.Session.SessionID == "" {
					t.Fatalf("durable consumption: %+v %+v %v", input, consumed, err)
				}
				result, err := store.ReadNativeResultMetadata(ctx, consumed.Result)
				if err != nil || result.Attribution == nil || result.Attribution.Source != "chat" ||
					result.Attribution.WakerID != "native-waker" || result.Attribution.ChatID != work.Chat.ID || result.Attribution.AccountID != agent.SystemAccountID {
					t.Fatalf("native terminal attribution: %+v %v", result, err)
				}
				nativeStatus := acpmeta.NativeTurnCompleted
				if want == constants.ExecutionStatusCanceled {
					nativeStatus = acpmeta.NativeTurnCancelled
				}
				if result.Turn.Status != nativeStatus || !slices.Equal(result.Turn.ConsumedInputIDs, []string{input.AdapterInputID}) {
					t.Fatalf("native status or exact input receipt: %+v", result.Turn)
				}
				reported := result.Turn.Usage
				if provider == "claude" && (reported == nil || reported.InputTokens == nil || *reported.InputTokens != 43) {
					t.Fatalf("Claude lost provider-reported usage: %+v", reported)
				}
				if provider == "cursor" && reported != nil {
					t.Fatalf("Cursor invented usage for a response without counters: %+v", reported)
				}
				return consumed.Result
			}
			create := ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}}
			readOnly := options
			readOnly.WorkspaceReadOnly = true
			_, err := core.Start(ctx, WorkIntent{ClientRequestID: "native-reject-read-only", Chat: create,
				Input: WorkInput{Actor: "user", Content: "do-not-dispatch-read-only"}, Options: readOnly})
			if !errors.Is(err, status_error.RuntimePolicyUnsupported) || fixture.accounts.Load() != 0 {
				t.Fatalf("unsupported read-only policy reached native preparation: err=%v leases=%d", err, fixture.accounts.Load())
			}
			first := start(create, "first-question")
			if first.Execution.Fast == nil || !*first.Execution.Fast {
				t.Fatalf("model-supported default Fast was not applied: %+v", first.Execution.Fast)
			}
			firstResult := verify(first, constants.ExecutionStatusCompleted)
			target := ChatTarget{ChatID: first.Chat.ID}
			options.Fast, options.Reasoning = &fastOff, "low"
			second := start(target, "second-question")
			if second.Execution.Fast == nil || *second.Execution.Fast {
				t.Fatalf("explicit Fast off was not preserved: %+v", second.Execution.Fast)
			}
			secondResult := verify(second, constants.ExecutionStatusCompleted)
			if secondResult.Session != firstResult.Session || secondResult == firstResult || fixture.accounts.Load() != 1 {
				t.Fatalf("compatible turns did not reuse one session with distinct native invocations: %+v %+v leases=%d", firstResult, secondResult, fixture.accounts.Load())
			}
			history, err := chats.ReadMessageHistory(ctx, first.Chat.ID, 0, 10)
			if err != nil || len(history.Resources) != 4 || history.Resources[1].Content != "native fixture answer" ||
				history.Resources[2].Content != "second-question" || history.Resources[3].Content != "native fixture answer" {
				t.Fatalf("public history: %+v %v", history, err)
			}

			options.Fast, options.Reasoning, options.ConfigurationResolved = nil, "", true
			held := start(target, "hold-for-native-cancel")
			if held.Execution.Fast != nil {
				t.Fatalf("native Fast default stored as an explicit override: %+v", held.Execution.Fast)
			}
			waitNativeProductStream(t, ctx, chats, held, "native fixture partial")
			if err := core.CancelExecutionAndWait(ctx, first.Chat.ID, held.Execution.ID); err != nil {
				t.Fatal(err)
			}
			verify(held, constants.ExecutionStatusCanceled)

			closeRuntime()
			core, chats, store, closeRuntime = fixture.open()
			loaded, err := chats.LoadExecutionChat(ctx, first.Chat.ID)
			if err != nil || loaded.LastFast != nil {
				t.Fatalf("reloaded settings: %+v %v", loaded.LastFast, err)
			}
			metrics, err := usageservice.NewService(store, fixture.usage).Summary(ctx, usageservice.Filter{
				From: time.Now().Add(-time.Minute).UnixMilli(), To: time.Now().Add(time.Minute).UnixMilli(),
				Timezone: "UTC", ChatID: first.Chat.ID, Source: "chat", AccountID: agent.SystemAccountID,
			}, "")
			if err != nil || metrics.Metrics.Invocations != 3 {
				t.Fatalf("reopened usage should count each native call once: %+v %v", metrics, err)
			}
			if provider == "claude" {
				input := metrics.Metrics.KnownTokens.Input
				if input == nil || *input != 129 || metrics.Metrics.MissingUsageInvocations != 0 {
					t.Fatalf("reopened Claude usage: %+v", metrics)
				}
			} else if metrics.Metrics.KnownTokens.Input != nil || metrics.Metrics.MissingUsageInvocations != 3 {
				t.Fatalf("reopened Cursor calls with unknown usage: %+v", metrics)
			}

			resumed := start(target, "current-after-restart")
			if result := verify(resumed, constants.ExecutionStatusCompleted); result.Session != firstResult.Session || fixture.accounts.Load() != 2 {
				t.Fatalf("restarted Core did not recover the original native session: %+v leases=%d", result, fixture.accounts.Load())
			}
			assertNativeProviderTrace(t, provider, fixture.trace, firstResult.Session.SessionID)
			if provider == "claude" {
				// The native model setter downgrades Auto when the target model lacks it.
				// This must be detected before admitting another prompt.
				options.Model, options.Reasoning, options.Fast = "fixture-default", "", nil
				for attempt := 1; attempt <= 2; attempt++ {
					_, err := core.Start(ctx, WorkIntent{Chat: target, Options: options,
						Input: WorkInput{Actor: "user", Content: "reject-auto-on-unsupported-model"}})
					trace, readErr := os.ReadFile(fixture.trace)
					if err == nil || readErr != nil || !strings.Contains(string(trace), `"subtype":"set_model","model":"fixture-default"`) ||
						strings.Contains(string(trace), "reject-auto-on-unsupported-model") {
						t.Fatalf("Claude submitted a prompt after losing Auto mode on attempt %d: err=%v traceErr=%v\n%s", attempt, err, readErr, trace)
					}
				}
			}
		})
	}
}

func assertNativeProviderTrace(t *testing.T, provider, trace, sessionID string) {
	t.Helper()
	body, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(body)
	requireOrder := func(parts ...string) {
		t.Helper()
		rest := wire
		for _, part := range parts {
			_, next, found := strings.Cut(rest, part)
			if !found {
				t.Fatalf("missing %q in native sequence %q:\n%s", part, parts, wire)
			}
			rest = next
		}
	}
	if provider == "claude" {
		requireOrder(`"subtype":"set_model","model":"fixture-model"`, `"effortLevel":"high"`, `"fastMode":true`,
			`"subtype":"set_permission_mode","mode":"auto"`, "first-question", `"effortLevel":"low"`, `"fastMode":false`, "second-question",
			`"effortLevel":null`, `"fastMode":null`, "hold-for-native-cancel", `"subtype":"interrupt"`, "--resume="+sessionID, `"subtype":"set_permission_mode","mode":"auto"`, "current-after-restart")
		if strings.Count(wire, `"fastMode":null`) != 1 {
			t.Fatalf("Claude should clear Fast exactly once after the explicit override: %s", wire)
		}
	} else {
		requireOrder("launch --force acp", `"configId":"model","sessionId":"`+sessionID+`","value":"fixture-model"`, `"value":"high"`, `"value":"true"`,
			"first-question", `"value":"low"`, `"value":"false"`, "second-question", `"value":"medium"`, `"value":"true"`, "hold-for-native-cancel", `"method":"session/cancel"`,
			"launch --force acp", `"method":"session/resume"`, `"value":"medium"`, `"value":"true"`, "current-after-restart")
	}
	for _, line := range strings.Split(wire, "\n") {
		if strings.Contains(line, "second-question") || strings.Contains(line, "current-after-restart") {
			if strings.Contains(line, "first-question") || strings.Contains(line, "Recent conversation history") || strings.Contains(line, "native fixture answer") {
				t.Fatalf("follow-up prompt replayed previous turns: %s", line)
			}
		}
	}
	for _, question := range []string{"first-question", "second-question", "hold-for-native-cancel", "current-after-restart"} {
		if strings.Count(wire, question) != 1 {
			t.Fatalf("prompt %q should reach the native process once: %s", question, wire)
		}
	}
}

func waitNativeProductStream(t *testing.T, ctx context.Context, chats chatservice.Module, held AcceptedExecution, text string) {
	t.Helper()
	subscription, err := chats.SubscribeEvents(ctx, held.Chat.ID, held.Chat.LatestSequence)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	for {
		select {
		case event, ok := <-subscription.Events:
			if !ok {
				t.Fatal("SSE closed before native checkpoint")
			}
			if event.Message != nil && strings.Contains(event.Message.Content, text) {
				return
			}
		case <-ctx.Done():
			t.Fatal("no native body reached the public subscription")
		}
	}
}

func waitNativeProductLive(t *testing.T, ctx context.Context, chats chatservice.Module, held AcceptedExecution, text string) *eventstream.LiveMessageIncrement {
	t.Helper()
	subscription, err := chats.SubscribeEvents(ctx, held.Chat.ID, held.Chat.LatestSequence)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	for event := range subscription.Events {
		if !event.Transient || event.Live == nil || event.Live.MessageID != held.Execution.ID {
			continue
		}
		var body strings.Builder
		for _, update := range event.Live.Updates {
			for _, part := range update.Parts {
				body.WriteString(part.Text)
			}
		}
		if body.String() == text && event.Sequence == 0 {
			return event.Live
		}
	}
	t.Fatalf("no transient %q before the durable checkpoint", text)
	return nil
}

type nativeFixtureEnvironment struct {
	// entry 是固定到本地测试程序的智能体环境。
	entry models.RuntimeEnvironment
}

func (r nativeFixtureEnvironment) Get(context.Context, string) (models.RuntimeEnvironment, error) {
	return r.entry, nil
}

func waitNativeProductStatus(t *testing.T, ctx context.Context, store *conversation.Store, accepted AcceptedExecution, want constants.ExecutionStatus) {
	t.Helper()
	for {
		rows, err := store.ReadTurn(ctx, accepted.Chat.ID, accepted.Execution.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			fact := row.Change.Fact
			if fact == nil {
				continue
			}
			switch fact.Type {
			case constants.ChatEventTypeExecutionCompleted, constants.ChatEventTypeExecutionCanceled, constants.ChatEventTypeExecutionFailed:
				var finished eventlog.ExecutionFinishedData
				if err := json.Unmarshal(fact.Data, &finished); err != nil || finished.Status != want {
					t.Fatalf("terminal = %+v, want %s, err = %v", finished, want, err)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(fact.Data, &fields); err != nil {
					t.Fatal(err)
				}
				if usage, exists := fields["usage"]; exists {
					t.Fatalf("duplicated native usage in business terminal: %s", usage)
				}
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("no durable %s terminal for %s", want, accepted.Execution.ID)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestNativeExecutionCodexChild(t *testing.T) {
	trace := os.Getenv("NEUX_EXECUTION_FIXTURE_TRACE")
	if trace == "" {
		return
	}
	file, err := os.OpenFile(trace, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(2)
	}
	defer file.Close()
	_, _ = fmt.Fprintln(file, "child inherited", os.Getenv("NEUX_EXECUTION_FIXTURE_BASE_VALUE"))
	scanner, out := bufio.NewScanner(os.Stdin), json.NewEncoder(os.Stdout)
	thread, turnID := "", ""
	serial := 0
	emit := func(method string, params any) {
		_ = out.Encode(map[string]any{"method": method, "params": params})
	}
	turn := func(status string) map[string]any {
		return map[string]any{"threadId": thread, "turn": map[string]any{"id": turnID, "status": status}}
	}
	for scanner.Scan() {
		line := scanner.Text()
		_, _ = fmt.Fprintln(file, line)
		var frame map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			os.Exit(2)
		}
		ack := func(result any) {
			_ = out.Encode(map[string]any{"id": frame["id"], "result": result})
		}
		switch frame["method"] {
		case "initialize":
			ack(map[string]any{"userAgent": "codex/0.163.0"})
		case "account/read":
			ack(map[string]any{"account": nil, "requiresOpenaiAuth": false})
		case "model/list":
			models := []any{
				map[string]any{"id": "default", "model": "default", "displayName": "Default"},
				map[string]any{"id": "gpt-5", "model": "gpt-5", "displayName": "Test model"},
				map[string]any{"id": "gpt-5.1", "model": "gpt-5.1", "displayName": "Test model"},
				map[string]any{"id": "fixture-model", "model": "fixture-model", "displayName": "Fixture", "isDefault": true,
					"supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": "high"}},
					"serviceTiers":              []any{map[string]any{"id": "fixture-priority", "name": "Fast"}}},
				map[string]any{"id": "fixture-other", "model": "fixture-other", "displayName": "Other",
					"supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": "medium"}},
					"serviceTiers":              []any{map[string]any{"id": "other-priority", "name": "Fast"}}},
			}
			ack(map[string]any{"data": models})
		case "config/read":
			ack(map[string]any{"config": map[string]any{}})
		case "skills/list", "skills/extraRoots/set":
			ack(map[string]any{})
		case "thread/fork":
			if os.Getenv("NEUX_EXECUTION_FIXTURE_FORK_UNCERTAIN") == "1" {
				continue
			}
			thread = fmt.Sprintf("fixture-fork-%d", os.Getpid())
			ack(map[string]any{"thread": map[string]any{"id": thread}, "model": "fixture-model"})
		case "thread/start", "thread/resume":
			params, _ := frame["params"].(map[string]any)
			thread, _ = params["threadId"].(string)
			if thread == "" {
				thread = fmt.Sprintf("fixture-thread-%d", os.Getpid())
				if os.Getenv("NEUX_EXECUTION_FIXTURE_CHECK_GRANT") == "1" {
					thread = uuid.NewString()
				}
			}
			ack(map[string]any{"thread": map[string]any{"id": thread}, "model": "fixture-model"})
		case "turn/start":
			if os.Getenv("NEUX_EXECUTION_FIXTURE_CHECK_GRANT") == "1" {
				connection, err := capability.ReadConnection(os.Getenv(constants.AgentCLIContextEnvironment))
				if err != nil {
					os.Exit(4)
				}
				_, _ = fmt.Fprintln(file, "host grant", connection.GrantID)
			}
			if binary := os.Getenv("NEUX_EXECUTION_FIXTURE_BOTCLI"); binary != "" {
				checkFilteredCodexCLI(t.Context(), file, binary, thread)
			}
			if strings.Contains(line, "revise-explicit-artifact") {
				selected, sibling := os.Getenv("NEUX_EXECUTION_FIXTURE_ARTIFACT"), os.Getenv("NEUX_EXECUTION_FIXTURE_ARTIFACT_SIBLING")
				if selected == "" || sibling == "" || os.WriteFile(selected, []byte("revised original\n"), 0600) != nil || os.WriteFile(sibling, []byte("sibling also changed\n"), 0600) != nil {
					os.Exit(3)
				}
			}
			if strings.Contains(line, "first-success") {
				cwd, err := os.Getwd()
				if err != nil || os.WriteFile(filepath.Join(cwd, "fixture-change.txt"), []byte("native write"), 0600) != nil {
					os.Exit(3)
				}
			}
			serial++
			turnID = fmt.Sprintf("fixture-turn-%d-%d", os.Getpid(), serial)
			ack(turn("inProgress"))
			emit("turn/started", turn("inProgress"))
			tokens := map[string]any{"inputTokens": 19, "outputTokens": 7, "cachedInputTokens": 4, "reasoningOutputTokens": 2, "totalTokens": 26}
			emit("thread/tokenUsage/updated", map[string]any{"threadId": thread, "turnId": turnID,
				"tokenUsage": map[string]any{"last": tokens, "total": tokens, "modelContextWindow": 100}})
			if strings.Contains(line, "provider-failure") {
				emit("turn/completed", turn("failed"))
				continue
			}
			reply := "fixture reply"
			switch {
			case strings.Contains(line, "hold-for-stream"):
				reply = "fixture partial"
			case strings.Contains(line, "Prepare a concise private handoff"):
				reply = "private fixture handoff"
			}
			emit("item/agentMessage/delta", map[string]any{"threadId": thread, "turnId": turnID, "itemId": turnID + "-message", "delta": reply})
			if strings.Contains(line, "hold-for-stream") {
				continue
			}
			emit("item/completed", map[string]any{"threadId": thread, "turnId": turnID,
				"item": map[string]any{"id": turnID + "-message", "type": "agentMessage", "phase": "final_answer", "text": reply}})
			emit("turn/completed", turn("completed"))
		case "turn/interrupt":
			ack(map[string]any{})
			emit("turn/completed", turn("interrupted"))
		}
	}
	os.Exit(0)
}

func checkFilteredCodexCLI(ctx context.Context, trace *os.File, binary, thread string) {
	settings := struct {
		// Policy 是 Codex 从用户配置加载的 Shell 过滤规则。
		Policy struct {
			// Set 在 include_only 过滤前应用用户指定的值。
			Set map[string]string `toml:"set"`
			// IncludeOnly 按模式保留用户允许的环境变量。
			IncludeOnly []string `toml:"include_only"`
		} `toml:"shell_environment_policy"`
	}{}
	if _, err := toml.DecodeFile(filepath.Join(os.Getenv("CODEX_HOME"), "config.toml"), &settings); err != nil {
		_, _ = fmt.Fprintln(trace, "filtered botcli config read failed", err)
		return
	}
	environment := map[string]string{}
	for _, entry := range os.Environ() {
		if name, value, ok := strings.Cut(entry, "="); ok {
			environment[name] = value
		}
	}
	for name, value := range settings.Policy.Set {
		environment[name] = value
	}
	for name := range environment {
		if !slices.ContainsFunc(settings.Policy.IncludeOnly, func(pattern string) bool {
			matches, _ := path.Match(strings.ToUpper(pattern), strings.ToUpper(name))
			return matches
		}) {
			delete(environment, name)
		}
	}
	// 与 Codex 的 Shell 一致，原生线程标识在 include_only 之后写入。
	environment[constants.AgentCLICodexThreadEnvironment] = thread
	if environment["USER_MARKER"] != "preserved" || environment["NEUX_EXECUTION_FIXTURE_BASE_VALUE"] != "" || environment[constants.AgentCLIContextEnvironment] != "" {
		_, _ = fmt.Fprintln(trace, "filtered botcli fixture unexpectedly bypassed the shell allowlist")
		return
	}
	if environment["PATH"] == "" || environment["PATH"] != os.Getenv("PATH") {
		_, _ = fmt.Fprintln(trace, "filtered botcli fixture did not retain native PATH")
		return
	}
	// PATH 保留原生进程的值，Go 在创建命令时使用该值解析程序。
	command := exec.CommandContext(ctx, constants.AgentCLIBinaryName, "capabilities")
	if command.Path != binary {
		_, _ = fmt.Fprintf(trace, "filtered botcli resolved to %q instead of %q\n", command.Path, binary)
		return
	}
	run := func(command *exec.Cmd, label string) {
		for name, value := range environment {
			command.Env = append(command.Env, name+"="+value)
		}
		output, err := command.CombinedOutput()
		var response capability.Response
		if err != nil || json.Unmarshal(output, &response) != nil || !response.OK || response.Operation != "capabilities" {
			_, _ = fmt.Fprintf(trace, "%s failed: %v %s\n", label, err, output)
			return
		}
		_, _ = fmt.Fprintln(trace, label+" succeeded")
	}
	run(command, "filtered botcli")
	delete(environment, "PATH")
	run(exec.CommandContext(ctx, binary, "capabilities"), "filtered executable fallback")
}

func TestNativeExecutionClaudeCursorChild(t *testing.T) {
	provider := os.Getenv("NEUX_EXECUTION_FIXTURE_PROVIDER")
	if provider == "" {
		return
	}
	if slices.Contains(os.Args, "--version") {
		if provider == "claude" {
			fmt.Println("2.1.309 (Claude Code)")
		} else {
			fmt.Println("2026.10.22-27dcf5f")
		}
		os.Exit(0)
	}
	trace, err := os.OpenFile(os.Getenv("NEUX_EXECUTION_FIXTURE_TRACE"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(2)
	}
	fmt.Fprintln(trace, "launch", strings.Join(os.Args[3:], " "))

	scanner, out := bufio.NewScanner(os.Stdin), json.NewEncoder(os.Stdout)
	read := func() (map[string]any, string, bool) {
		if !scanner.Scan() {
			return nil, "", false
		}
		line := scanner.Text()
		fmt.Fprintln(trace, line)
		var frame map[string]any
		if json.Unmarshal(scanner.Bytes(), &frame) != nil {
			os.Exit(2)
		}
		return frame, line, true
	}
	if provider == "claude" {
		serveNativeExecutionClaude(out, read)
	} else {
		serveNativeExecutionCursor(out, read)
	}
	trace.Close()
	os.Exit(0)
}

func serveNativeExecutionClaude(out *json.Encoder, read func() (map[string]any, string, bool)) {
	cwd, _ := os.Getwd()
	var session, active string
	for _, arg := range os.Args {
		for _, prefix := range []string{"--session-id=", "--resume="} {
			if id, ok := strings.CutPrefix(arg, prefix); ok {
				session = id
			}
		}
	}
	model := "fixture-default"
	var effort any
	var fast any = false
	serial := 0
	usage := map[string]any{"input_tokens": 43, "output_tokens": 7, "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0}
	emit := func(body map[string]any) {
		body["session_id"] = session
		_ = out.Encode(body)
	}
	finish := func(text, reason string) {
		emit(map[string]any{"type": "result", "subtype": "success", "uuid": "fixture-result-" + active,
			"result_index": serial, "user_message_uuid": active, "terminal_reason": reason,
			"result": text, "is_error": false, "usage": usage})
		active = ""
		serial++
	}
	for {
		frame, line, ok := read()
		if !ok {
			return
		}
		switch frame["type"] {
		case "control_request":
			request := frame["request"].(map[string]any)
			reply := map[string]any{}
			switch request["subtype"] {
			case "initialize":
				emit(map[string]any{"type": "system", "subtype": "init", "cwd": cwd, "model": model,
					"permissionMode": "default", "claude_code_version": "2.1.309"})
				reply["models"] = []any{
					map[string]any{"value": "fixture-default", "displayName": "Default"},
					map[string]any{"value": "fixture-model", "displayName": "Fixture", "supportsAutoMode": true,
						"supportsEffort": true, "supportedEffortLevels": []string{"high", "low", "medium"}, "supportsFastMode": true},
				}
			case "get_settings":
				appliedEffort, effectiveFast := effort, fast
				if appliedEffort == nil {
					appliedEffort = "low"
				}
				if effectiveFast == nil {
					effectiveFast = false
				}
				reply["applied"] = map[string]any{"model": model, "effort": appliedEffort}
				reply["effective"] = map[string]any{"fastMode": effectiveFast}
			case "set_model":
				model = request["model"].(string)
			case "apply_flag_settings":
				settings := request["settings"].(map[string]any)
				if value, exists := settings["effortLevel"]; exists {
					effort = value
				}
				if value, exists := settings["fastMode"]; exists {
					if value != nil {
						if _, ok := value.(bool); !ok {
							os.Exit(3)
						}
					}
					fast = value
				}
			case "set_permission_mode":
			case "get_context_usage":
				reply["totalTokens"], reply["rawMaxTokens"] = 50, 1000
			case "interrupt":
				finish("", "aborted_streaming")
			default:
				os.Exit(3)
			}
			_ = out.Encode(map[string]any{"type": "control_response", "response": map[string]any{
				"subtype": "success", "request_id": frame["request_id"], "response": reply,
			}})
		case "user":
			active = frame["uuid"].(string)
			held := strings.Contains(line, "hold-for-native-cancel")
			reply := "native fixture answer"
			if held {
				reply = "native fixture partial"
			}
			emit(map[string]any{"type": "assistant", "user_message_uuid": active, "parent_tool_use_id": nil,
				"message": map[string]any{"id": "fixture-message-" + active, "model": model, "usage": usage,
					"content": []any{map[string]any{"type": "text", "text": reply}}}})
			if !held {
				finish(reply, "completed")
			}
		}
	}
}

func serveNativeExecutionCursor(out *json.Encoder, read func() (map[string]any, string, bool)) {
	session, model, effort, fast := "cursor-fixture", "fixture-default", "low", "false"
	var held any
	selectOption := func(id, category, current string, choices ...string) map[string]any {
		options := make([]map[string]string, 0, len(choices))
		for _, value := range choices {
			options = append(options, map[string]string{"value": value, "name": value})
		}
		return map[string]any{"id": id, "name": id, "type": "select", "category": category, "currentValue": current, "options": options}
	}
	settings := func() []any {
		return []any{selectOption("model", "model", model, "fixture-default", "fixture-model"),
			selectOption("effort", "thought_level", effort, "low", "medium", "high"),
			selectOption("fast", "model_config", fast, "false", "true")}
	}
	reply := func(id, result any) {
		_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	for {
		frame, line, ok := read()
		if !ok {
			return
		}
		id := frame["id"]
		params, _ := frame["params"].(map[string]any)
		switch frame["method"] {
		case "initialize":
			reply(id, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{
				"loadSession": true, "sessionCapabilities": map[string]any{"close": map[string]any{}, "resume": map[string]any{}},
			}})
		case "cursor/list_available_models":
			reply(id, map[string]any{"models": []any{
				map[string]any{"value": "fixture-model", "name": "Fixture", "configOptions": []any{
					selectOption("effort", "thought_level", "medium", "low", "medium", "high"),
					selectOption("fast", "model_config", "true", "false", "true"),
				}},
			}})
		case "session/new", "session/resume":
			if frame["method"] == "session/resume" && params["sessionId"] != session {
				os.Exit(3)
			}
			reply(id, map[string]any{"sessionId": session, "configOptions": settings()})
		case "session/set_config_option":
			value := params["value"].(string)
			switch params["configId"] {
			case "model":
				model = value
			case "effort":
				effort = value
			case "fast":
				fast = value
			default:
				os.Exit(3)
			}
			reply(id, map[string]any{"configOptions": settings()})
		case "session/prompt":
			if strings.Contains(line, "reject-with-native-auth-error") {
				_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": acp.NewAuthRequired(nil)})
				continue
			}
			text := "native fixture answer"
			if strings.Contains(line, "hold-for-native-cancel") {
				held, text = id, "native fixture partial"
			}
			_ = out.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
				"sessionId": session, "update": map[string]any{"sessionUpdate": "agent_message_chunk",
					"content": map[string]any{"type": "text", "text": text}},
			}})
			if held == nil {
				reply(id, map[string]any{"stopReason": "end_turn"})
			}
		case "session/cancel":
			if held != nil {
				reply(held, map[string]any{"stopReason": "cancelled"})
				held = nil
			}
		case "session/close":
			reply(id, map[string]any{})
		default:
			if id != nil {
				os.Exit(3)
			}
		}
	}
}

func TestNativeThreadReceivesInstructionsWithoutWorkspaceFiles(t *testing.T) {
	plan := recordedRun{Instructions: []agent.Instruction{{Name: "PARTNER.md", Content: "partner rule"}, {Name: "TEAM.md", Content: "team rule"}}, Workspace: agent.Workspace{Path: t.TempDir(), Managed: true}}
	thread := nativeThreadRequest("chat", plan.Workspace, plan.Instructions, pendingMessage{}, models.RuntimeEnvironment{}, agent.ResourceSelection{}, "key")
	if thread.Session.DeveloperInstructions == nil || !strings.Contains(*thread.Session.DeveloperInstructions, "partner rule") || !strings.Contains(*thread.Session.DeveloperInstructions, "team rule") {
		t.Fatalf("native session lost stable instructions: %+v", thread.Session)
	}
}

func TestNativeFullAccessApprovalsUseFrozenPolicy(t *testing.T) {
	for _, provider := range []string{"codex", "claude", "cursor", "workbuddy", "qoder", "kimi", "gemini", "grok", "opencode", "pi"} {
		t.Run(provider, func(t *testing.T) {
			work := nativeExecutionWork{input: pendingMessage{runtime: runtimeSelection{RuntimeID: provider, PermissionLevel: constants.PermissionLevelFullAccess}}}
			approve := work.ports(&service{}).Approve
			options := []agent.AgentApprovalOption{{ID: "deny", Kind: "reject_once"}, {ID: "persist", Kind: "allow_always"}, {ID: "once", Kind: "allow_once"}}
			choice, err := approve(t.Context(), agent.AgentScope{}, agent.AgentApproval{Options: options})
			if err != nil || choice != "once" {
				t.Fatalf("full access still requires manual approval: choice=%q err=%v", choice, err)
			}
			choice, err = approve(t.Context(), agent.AgentScope{}, agent.AgentApproval{Options: options[:2]})
			if err != nil || choice != "persist" {
				t.Fatalf("native allow-always option lost: choice=%q err=%v", choice, err)
			}
			choice, err = approve(t.Context(), agent.AgentScope{}, agent.AgentApproval{Options: options[:1]})
			if err != nil || choice != "" {
				t.Fatalf("invented approval without native allow option: choice=%q err=%v", choice, err)
			}
			work.input.runtime.PermissionLevel = constants.PermissionLevelAuto
			choice, err = work.ports(&service{}).Approve(t.Context(), agent.AgentScope{}, agent.AgentApproval{Options: options})
			if err != nil || choice != "" {
				t.Fatalf("ordinary permission auto-approved a native command: choice=%q err=%v", choice, err)
			}
			work.input.runtime.PermissionLevel = constants.PermissionLevelFullAccess
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			choice, err = work.ports(&service{}).Approve(ctx, agent.AgentScope{}, agent.AgentApproval{Options: options})
			if !errors.Is(err, context.Canceled) || choice != "" {
				t.Fatalf("canceled invocation approved a command: choice=%q err=%v", choice, err)
			}
			work.input.runtime.WorkspaceReadOnly = true
			if work.ports(&service{}).Approve != nil {
				t.Fatal("read-only invocation exposes approvals")
			}
		})
	}
}

func TestNativeFailureSeparatesApplicationIdentityFromPublicMessage(t *testing.T) {
	for _, failure := range []error{status_error.RuntimeEventQueueOverflow, errors.Join(status_error.RuntimeEventQueueOverflow, status_error.RuntimeExecutionFailed.WithMessage("secondary native reason"))} {
		got := nativeExecutionOutcome(agent.TurnResult{StorageError: failure})
		if got.code != "RuntimeEventQueueOverflow" || got.message != "" {
			t.Fatalf("application failure leaked or lost identity: code=%q message=%q", got.code, got.message)
		}
	}
}

// TestNativeProductAcquiresReservedPluginSkills 验证实际 Execution 冷准备经进程租约发布固定插件技能。
func TestNativeProductAcquiresReservedPluginSkills(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, _, store, closeRuntime := fixture.open()
	defer closeRuntime()
	manager := core.(*service)
	environment := manager.runtimeEnvironments.(nativeFixtureEnvironment)
	environment.entry.Detection.Capabilities = agent.CapabilitySet{agent.CapabilitySkillDirectory: true}
	manager.runtimeEnvironments = environment
	state := executionPlugin(t, fixture.root, "p1", "reserved plugin bytes", false)
	// 纯技能包未声明配置字段，不能携带通用 MCP 夹具的 TOKEN。
	state.Configuration.Secrets = nil
	state.Installations[0].WakerID = "native-waker"
	manager.pluginController = installedPluginFixture{states: []models.PluginState{state}}
	accepted, err := core.Start(ctx, WorkIntent{ClientRequestID: "reserved-native-skills", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}}, Input: WorkInput{Actor: "user", Content: "first-success", PluginIDs: []string{"p1"}}, Options: WorkOptions{RuntimeID: "codex", Model: "fixture-model", Reasoning: "high", PermissionLevel: constants.PermissionLevelAuto}})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, accepted, constants.ExecutionStatusCompleted)
	wire, err := os.ReadFile(fixture.trace)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(wire), "\n") {
		var frame struct {
			// Method 是测试原生端口收到的操作。
			Method string `json:"method"`
			// Params 是实际传给原生进程的启动参数。
			Params struct {
				// CWD 是本次启动的真实工作目录。
				CWD string `json:"cwd"`
			} `json:"params"`
		}
		if json.Unmarshal([]byte(line), &frame) != nil || frame.Method != "thread/start" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(frame.Params.CWD, ".agents", "skills", "office", "SKILL.md"))
		if err != nil || !strings.Contains(string(body), "reserved plugin bytes") {
			t.Fatalf("production resource path did not install reserved plugin: %q %v", body, err)
		}
		return
	}
	t.Fatal("missing native thread startup receipt")
}

func TestNativeProductMCPDisplayRenameKeepsCodexSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, _, store, closeRuntime := fixture.open()
	defer closeRuntime()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "connectors.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	connectors := controllers.NewMCPConnectorController(database.DB)
	connector := models.MCPConnector{ID: "stable-connector", Name: "Original display", RuntimeKey: "stable_native_key", Scope: constants.MCPConnectorScopeBuiltIn, Transport: constants.MCPTransportStreamableHTTP, URL: "https://example.invalid/mcp", TimeoutMS: 10000}
	if err := connectors.Create(ctx, connector); err != nil {
		t.Fatal(err)
	}
	fallback := connector
	fallback.ID, fallback.Name, fallback.RuntimeKey = "fallback-connector", "original_fallback", ""
	if err := connectors.Create(ctx, fallback); err != nil {
		t.Fatal(err)
	}
	manager := core.(*service)
	manager.connectorController = connectors
	environment := manager.runtimeEnvironments.(nativeFixtureEnvironment)
	environment.entry.Detection.Capabilities = agent.CapabilitySet{agent.CapabilityMCPStreamableHTTP: true}
	manager.runtimeEnvironments = environment
	options := WorkOptions{RuntimeID: "codex", Model: "fixture-model", Reasoning: "high", PermissionLevel: constants.PermissionLevelAuto}
	start := func(target ChatTarget, requestID, text string) (agent.NativeSessionKey, string) {
		t.Helper()
		accepted, err := core.Start(ctx, WorkIntent{ClientRequestID: requestID, Chat: target, Input: WorkInput{Actor: "user", Content: text}, Options: options})
		if err != nil {
			t.Fatal(err)
		}
		waitNativeProductStatus(t, ctx, store, accepted, constants.ExecutionStatusCompleted)
		if err := core.CancelExecutionAndWait(ctx, accepted.Chat.ID, accepted.Execution.ID); err != nil {
			t.Fatal(err)
		}
		input, consumed, err := store.ReadHostInputConsumption(ctx, agent.HostInputRef{ChatID: accepted.Chat.ID, HostInvocationID: accepted.Execution.ID})
		if err != nil || consumed == nil || input.Session.SessionID == "" {
			t.Fatalf("missing real native consumption: %+v %+v %v", input, consumed, err)
		}
		return input.Session, accepted.Chat.ID
	}
	first, chatID := start(ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}}, "connector-rename", "before-display-rename")
	connector.Name = "Renamed display"
	if err := connectors.Update(ctx, connector); err != nil {
		t.Fatal(err)
	}
	second, _ := start(ChatTarget{ChatID: chatID}, "", "after-display-rename")
	if second.SessionID != first.SessionID || fixture.accounts.Load() != 1 {
		t.Fatalf("display rename replaced native session: first=%+v second=%+v account acquisitions=%d", first, second, fixture.accounts.Load())
	}
	fallback.Name = "renamed_fallback"
	if err := connectors.Update(ctx, fallback); err != nil {
		t.Fatal(err)
	}
	third, _ := start(ChatTarget{ChatID: chatID}, "", "after-actual-native-name-change")
	if third.SessionID == second.SessionID || fixture.accounts.Load() != 2 {
		t.Fatalf("actual fallback native name change reused the old session: second=%+v third=%+v account acquisitions=%d", second, third, fixture.accounts.Load())
	}
}

// nativeFixtureBusinessText 只按明确宿主上下文边界分离业务正文，不按短句猜测。
func nativeFixtureBusinessText(text string) string {
	business, _, _ := strings.Cut(text, "\n\n<neuxbot-turn-context>\n")
	return business
}
