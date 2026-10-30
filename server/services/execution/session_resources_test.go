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
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
)

// TestColdResourcesMergeCurrentAuthorizationEnvironment 防止软件授权环境在静态声明分型后丢失或反写本轮输入。
func TestColdResourcesMergeCurrentAuthorizationEnvironment(t *testing.T) {
	t.Setenv("PATH", "/fixture/inherited-bin")
	workspace := t.TempDir()
	binary := filepath.Join(t.TempDir(), "allycli")
	current := map[string]string{constants.AgentCLIContextEnvironment: filepath.Join(workspace, "connection.json"), constants.AgentCLIBinaryEnvironment: binary}
	plan := runtimecontract.SessionPlan{Target: runtimecontract.SessionTarget{RuntimeID: runtimeconfig.RuntimeIDPi}, Workspace: runtimecontract.Workspace{Path: workspace}}
	core := &service{}
	resources, err := core.acquireChatSessionResources(context.Background(), messageChat{}, pendingMessage{}, "execution", nil, models.RuntimeEnvironment{}, plan, runtimecontract.SessionDirective{}, frozenPluginResources{}, current)
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
	request := pendingMessage{message: userMessage{PluginIDs: []string{"cleanup"}}, runtime: runtimeSelection{AccountID: "managed", RuntimeID: runtimeconfig.RuntimeIDCodex, Model: "gpt-5"}}
	plan := runtimecontract.SessionPlan{Target: runtimecontract.SessionTarget{RuntimeID: runtimeconfig.RuntimeIDCodex, Binary: "/missing-native-binary"}, Workspace: runtimecontract.Workspace{Path: workspace}}
	environment := models.RuntimeEnvironment{BaseRuntimeID: runtimeconfig.RuntimeIDCodex, Detection: runtimeconfig.DetectionResult{Runtime: runtimeconfig.RuntimeSummary{ID: runtimeconfig.RuntimeIDCodex, BaseRuntimeID: runtimeconfig.RuntimeIDCodex}, Capabilities: runtimeconfig.CapabilitySet{runtimeconfig.CapabilitySkillDirectory: true}, ResolvedBinary: "/missing-native-binary"}}
	frozen, err := core.freezePluginResources(context.Background(), "w1", request, environment.Detection)
	if err != nil {
		t.Fatal(err)
	}
	partial, err := core.acquireChatSessionResources(context.Background(), messageChat{chat: models.Chat{WakerID: "w1"}}, request, "execution", nil, environment, plan, runtimecontract.SessionDirective{}, frozen, nil)
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

func (r *canceledPreparationRuntime) PrepareSession(ctx context.Context, plan runtimecontract.SessionPlan, session runtimecontract.SessionDirective) (runtimecontract.PreparedChatRun, error) {
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
	environment := models.RuntimeEnvironment{Detection: runtimeconfig.DetectionResult{Capabilities: runtimeconfig.CapabilitySet{runtimeconfig.CapabilitySkillDirectory: true}}}
	frozen, err := core.freezePluginResources(context.Background(), "w1", request, environment.Detection)
	if err != nil {
		t.Fatal(err)
	}
	partial, err := core.acquireChatSessionResources(context.Background(), messageChat{}, request, "execution", nil, environment, runtimecontract.SessionPlan{}, runtimecontract.SessionDirective{}, frozen, nil)
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
