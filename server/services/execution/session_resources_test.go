package execution

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
)

// coldResumeJournal 返回既有配置，避免资源契约测试依赖日志文件与数据库。
type coldResumeJournal struct {
	// agentTestJournal 保留本用例不调用的日志端口。
	agentTestJournal
}

func (*coldResumeJournal) ReadExecutionConfiguration(context.Context, string, string) (eventlog.ExecutionCreatedData, error) {
	return eventlog.ExecutionCreatedData{}, nil
}

func TestColdResourcesPreservePreparedSessionIdentity(t *testing.T) {
	for _, replaceConfiguration := range []bool{false, true} {
		name := "cold_resume"
		if replaceConfiguration {
			name = "changed_configuration"
		}
		t.Run(name, func(t *testing.T) {
			core := &service{journal: &coldResumeJournal{}}
			state := messageChat{
				chat:            models.Chat{ID: "chat", LastRuntimeID: agentapi.RuntimeIDPi, LastPermissionLevel: constants.PermissionLevelFullAccess},
				lastExecutionID: "previous-execution",
				runtimeSessions: map[string]models.RuntimeSession{agentapi.RuntimeIDPi: {SessionID: "persisted-native-session", LastExecutionID: "previous-execution"}},
			}
			input := pendingMessage{runtime: runtimeSelection{RuntimeID: agentapi.RuntimeIDPi, PermissionLevel: constants.PermissionLevelFullAccess, SessionPolicy: constants.RuntimeSessionPolicyContinue}}
			environment := models.RuntimeEnvironment{BaseRuntimeID: agentapi.RuntimeIDPi, Detection: agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{ID: agentapi.RuntimeIDPi, BaseRuntimeID: agentapi.RuntimeIDPi}}}
			plan, _, directive := core.buildSessionPlan(state, input, &preparedMessageFiles{executionWorkspaceDirectory: t.TempDir()}, nil, nil, environment)
			if directive.Mode != agentapi.SessionModeResume || directive.ID != "persisted-native-session" {
				t.Fatalf("production plan lost persisted session: %+v", directive)
			}
			manager := agentapi.NewManager(agentapi.ManagerConfig{Launch: func(agentapi.RuntimeTarget, agentapi.DetectionResult) (agentapi.LaunchFacts, error) {
				return agentapi.LaunchFacts{BaseRuntimeID: agentapi.RuntimeIDPi, Executable: "never-started-fixture"}, nil
			}})
			defer manager.Close(context.Background())
			if replaceConfiguration {
				previous, err := manager.PrepareSession(context.Background(), plan, directive)
				if err != nil {
					t.Fatal(err)
				}
				if err = previous.Close(context.Background()); err != nil {
					t.Fatal(err)
				}
				plan.ResourceKey = "changed-resource-configuration"
			}
			prepared, err := manager.PrepareSession(context.Background(), plan, directive)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close(context.Background())
			want := directive
			if replaceConfiguration {
				want = agentapi.SessionDirective{Mode: agentapi.SessionModeNew}
			}
			if got := prepared.Session(); got != want {
				t.Fatalf("prepare directive=%+v, want %+v", got, want)
			}
			resources, err := core.acquireChatSessionResources(prepared.Context(), state, input, "next-execution", nil, environment, plan, directive, frozenPluginResources{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			// 即使断言失败，也要让同一准备接管并释放真实取得的资源。
			bindErr := prepared.Bind(prepared.Context(), resources)
			if resources.Session == nil || resources.Session.Mode != agentapi.SessionModeResume || resources.Session.ID != "" {
				t.Fatalf("resource receipt must only project mode: %+v; bind=%v", resources.Session, bindErr)
			}
			if bindErr != nil {
				t.Fatalf("production cold resource bind failed: %v", bindErr)
			}
			if got := prepared.Session(); got != want {
				t.Fatalf("resources changed frozen session identity: got %+v, want %+v", got, want)
			}
		})
	}
}

// TestColdResourcesMergeCurrentAuthorizationEnvironment 防止软件授权环境在静态声明分型后丢失或反写本轮输入。
func TestColdResourcesMergeCurrentAuthorizationEnvironment(t *testing.T) {
	t.Setenv("PATH", "/fixture/inherited-bin")
	workspace := t.TempDir()
	binary := filepath.Join(t.TempDir(), "allycli")
	current := map[string]string{constants.AgentCLIContextEnvironment: filepath.Join(workspace, "connection.json"), constants.AgentCLIBinaryEnvironment: binary}
	plan := agentapi.SessionPlan{Target: agentapi.SessionTarget{RuntimeID: agentapi.RuntimeIDPi}, Workspace: agentapi.Workspace{Path: workspace}}
	core := &service{}
	resources, err := core.acquireChatSessionResources(context.Background(), messageChat{}, pendingMessage{}, "execution", nil, models.RuntimeEnvironment{}, plan, agentapi.SessionDirective{}, frozenPluginResources{}, current)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.Close(context.Background())
	if resources.Target.Environment[constants.AgentCLIContextEnvironment] != current[constants.AgentCLIContextEnvironment] || resources.Target.Environment[constants.AgentCLIBinaryEnvironment] != binary || resources.Target.Environment["PATH"] != filepath.Dir(binary)+string(os.PathListSeparator)+"/fixture/inherited-bin" {
		t.Fatalf("cold resources lost current software authorization: %+v", resources.Target.Environment)
	}
	if !reflect.DeepEqual(current, map[string]string{constants.AgentCLIContextEnvironment: filepath.Join(workspace, "connection.json"), constants.AgentCLIBinaryEnvironment: binary}) {
		t.Fatalf("cold resources rewrote turn environment: %+v", current)
	}
}

// partialAccountController 在首次认证回填读取时模拟一次仓储失败。
type partialAccountController struct {
	controllers.AIAccountController                  // AIAccountController 保留当前用例未调用的仓储端口。
	account                         models.AIAccount // account 是合法的受管订阅账号。
	secret                          string           // secret 是受控测试凭据。
	reads                           int              // reads 统计准备与回填读取次数。
	failAt                          int              // failAt 标记首次回填时的一次瞬时读取故障。
}

func (c *partialAccountController) Get(context.Context, string) (models.AIAccount, error) {
	c.reads++
	if c.reads == c.failAt {
		return models.AIAccount{}, status_error.DatabaseError
	}
	return c.account, nil
}
func (c *partialAccountController) List(context.Context) ([]models.AIAccount, error) {
	return []models.AIAccount{c.account}, nil
}
func (c *partialAccountController) Credential(context.Context, string) (string, error) {
	return c.secret, nil
}
func (c *partialAccountController) StoreCredential(_ context.Context, _ string, _ int64, secret, _ string) error {
	c.secret = secret
	c.account.Revision++
	return nil
}

func TestFailedColdPreparationRetainsAccountCleanupOwner(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"sub": "user", "exp": time.Now().Add(time.Hour).Unix(), "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "workspace"}})
	token := "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	encoded, _ := json.Marshal(map[string]any{"tokens": map[string]string{"id_token": token, "access_token": token, "refresh_token": "native-refresh"}, "last_refresh": time.Now().UTC()})
	controller := &partialAccountController{account: models.AIAccount{ID: "managed", Revision: 1, HasCredential: true, AuthMode: constants.AccountAuthModeSubscription, Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL}}}, secret: string(encoded), failAt: 4}
	accounts := aiaccount.New(controller)
	data := t.TempDir()
	pluginState := executionPlugin(t, data, "cleanup", "cleanup skill", false)
	pluginState.Configuration.Secrets = nil
	core := &service{accounts: accounts, skillDataDirectory: data, pluginController: installedPluginFixture{states: []models.PluginState{pluginState}}, pluginContents: plugins.NewContentStore(data, func() ([]string, error) { return nil, nil })}
	workspace := t.TempDir()
	request := pendingMessage{message: userMessage{PluginIDs: []string{"cleanup"}}, runtime: runtimeSelection{AccountID: "managed", RuntimeID: agentapi.RuntimeIDCodex, Model: "gpt-5"}}
	plan := agentapi.SessionPlan{Target: agentapi.SessionTarget{RuntimeID: agentapi.RuntimeIDCodex, Binary: "/missing-native-binary"}, Workspace: agentapi.Workspace{Path: workspace}}
	environment := models.RuntimeEnvironment{BaseRuntimeID: agentapi.RuntimeIDCodex, Detection: agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{ID: agentapi.RuntimeIDCodex, BaseRuntimeID: agentapi.RuntimeIDCodex}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilitySkillDirectory: true}, ResolvedBinary: "/missing-native-binary"}}
	frozen, err := core.freezePluginResources(context.Background(), "w1", request, environment.Detection)
	if err != nil {
		t.Fatal(err)
	}
	partial, err := core.acquireChatSessionResources(context.Background(), messageChat{chat: models.Chat{WakerID: "w1"}}, request, "execution", nil, environment, plan, agentapi.SessionDirective{}, frozen, nil)
	if err == nil {
		t.Fatal("expected native plugin environment preparation failure")
	}
	acquireErr := err
	if partial.Close == nil {
		t.Fatal("failed preparation discarded its account cleanup owner")
	}
	t.Cleanup(func() { controller.failAt = 0; _ = partial.Close(context.Background()) })
	if err = partial.Close(context.Background()); !errors.Is(err, status_error.DatabaseError) {
		t.Fatalf("first account cleanup=%v, resource acquisition=%v", err, acquireErr)
	}
	release, err := accounts.Exclusive("managed")
	if release != nil {
		release()
	}
	if !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("failed cleanup released account ownership: %v", err)
	}
	if err = partial.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	release, err = accounts.Exclusive("managed")
	if release != nil {
		release()
	}
	if err != nil {
		t.Fatalf("retry did not finish original account ownership: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, pluginState.Versions[0].ContentPath)); !os.IsNotExist(err) {
		t.Fatalf("plugin content lease was retained after domain cleanup: %v", err)
	}
}

// canceledPreparationRuntime 模拟技术宿主在领域资源获取前取消准备，入口 Context 仍可用。
type canceledPreparationRuntime struct {
	// fakeRuntime 保留真实接纳测试的原生提交边界。
	fakeRuntime
	// preparation 保存应当接管部分结果并退出的同一准备。
	preparation *synchronousPreparation
}

func (r *canceledPreparationRuntime) PrepareSession(ctx context.Context, plan agentapi.SessionPlan, session agentapi.SessionDirective) (agentapi.PreparedChatRun, error) {
	prepareContext, cancel := context.WithCancel(ctx)
	cancel()
	prepared, err := prepareSynchronousRuntime(prepareContext, plan, session, r.Run)
	r.preparation = prepared
	return prepared, err
}

// TestColdPreparationCancellationReturnsPartialResourcesBeforeAdmission 防止资源获取使用入口 Context 或丢弃部分结果。
func TestColdPreparationCancellationReturnsPartialResourcesBeforeAdmission(t *testing.T) {
	runner := &canceledPreparationRuntime{}
	manager := newMessageTestService(t, t.TempDir(), &runner.fakeRuntime)
	manager.execution.(*service).runtime = runner
	_, err := manager.execution.Start(context.Background(), WorkIntent{
		ClientRequestID: "cancel-preparation",
		Chat:            ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}},
		Input:           WorkInput{Actor: "user", Content: "do not admit a canceled preparation"},
		Options:         WorkOptions{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled preparation was admitted: %v", err)
	}
	if runner.preparation == nil || runner.preparation.needsResources || !runner.preparation.closed || runner.preparation.resources.Close == nil {
		t.Fatalf("partial canceled preparation lost its resource owner: %+v", runner.preparation)
	}
	if runner.starts != 0 || len(runner.requests) != 0 {
		t.Fatalf("canceled preparation submitted input: starts=%d requests=%d", runner.starts, len(runner.requests))
	}
}

// TestFailedPluginPreparationKeepsPartialLeaseUntilConsumerClose 防止插件内部在 Core 接管部分结果前释放已经取得的内容。
func TestFailedPluginPreparationKeepsPartialLeaseUntilConsumerClose(t *testing.T) {
	data := t.TempDir()
	first := executionPlugin(t, data, "a", "first skill", false)
	second := executionPlugin(t, data, "b", "second skill", false)
	first.Configuration.Secrets, second.Configuration.Secrets = nil, nil
	second.Versions[0].ContentPath = filepath.Join("plugins", "content", "missing")
	core := &service{skillDataDirectory: data, pluginController: installedPluginFixture{states: []models.PluginState{first, second}}, pluginContents: plugins.NewContentStore(data, func() ([]string, error) { return nil, nil })}
	request := pendingMessage{message: userMessage{PluginIDs: []string{"a", "b"}}, runtime: runtimeSelection{RuntimeID: "codex"}}
	environment := models.RuntimeEnvironment{Detection: agentapi.DetectionResult{Capabilities: agentapi.CapabilitySet{agentapi.CapabilitySkillDirectory: true}}}
	frozen, err := core.freezePluginResources(context.Background(), "w1", request, environment.Detection)
	if err != nil {
		t.Fatal(err)
	}
	partial, err := core.acquireChatSessionResources(context.Background(), messageChat{}, request, "execution", nil, environment, agentapi.SessionPlan{}, agentapi.SessionDirective{}, frozen, nil)
	if err == nil || partial.Close == nil {
		t.Fatalf("failed preparation omitted its partial lease: %v", err)
	}
	defer func() { _ = partial.Close(context.Background()) }()
	if err := core.pluginContents.Collect(); err != nil {
		t.Fatal(err)
	}
	firstPath := filepath.Join(data, first.Versions[0].ContentPath)
	if _, err := os.Stat(firstPath); err != nil {
		t.Fatalf("partial lease was released before consumer ownership: %v", err)
	}
	if err := partial.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatalf("consumer close did not release the original partial lease: %v", err)
	}
}
