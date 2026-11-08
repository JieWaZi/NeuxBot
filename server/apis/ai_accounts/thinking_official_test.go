package ai_accounts

import (
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/JieWaZi/NeuxBot/server/services/runtimeenvironment"
	"github.com/gin-gonic/gin"
)

// officialThinkingEnvironment 提供真实 HTTP 能力查询使用的固定运行时配置。
type officialThinkingEnvironment struct {
	// RuntimeEnvironmentController 保留本次查询未调用的方法。
	controllers.RuntimeEnvironmentController
	// environment 指向固定官方入口，认证由账号服务准备。
	environment models.RuntimeEnvironment
}

type officialThinkingTransactions struct {
	// TransactionController is unused by the read-only native model query.
	controllers.TransactionController
}

// Get 返回查询选择的 CLI 环境。
func (s officialThinkingEnvironment) Get(context.Context, string) (models.RuntimeEnvironment, error) {
	return s.environment, nil
}

// TestOfficialGeminiThinkingHTTP rejects an invented thinking menu on the pinned native ACP version.
func TestOfficialGeminiThinkingHTTP(t *testing.T) {
	cli := os.Getenv("ALLY_GEMINI_OFFICIAL_CLI")
	if cli == "" {
		t.Skip("requires pinned official Gemini 0.62 CLI")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("catalog query invoked generation: %s", r.URL.Path)
			w.WriteHeader(400)
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-3.8-flash","supportedGenerationMethods":["generateContent"]}]}`))
	}))
	defer provider.Close()
	oldAccounts, oldEnvironments, oldService, oldSDK := global.AIAccountControllerManager, global.RuntimeEnvironmentControllerManager, global.AIAccountService, global.RuntimeCatalog
	oldRuntimeService := global.RuntimeEnvironmentService
	defer func() {
		global.AIAccountControllerManager = oldAccounts
		global.RuntimeEnvironmentControllerManager = oldEnvironments
		global.AIAccountService = oldService
		global.RuntimeCatalog = oldSDK
		global.RuntimeEnvironmentService = oldRuntimeService
	}()
	id := "0123456789abcdef0123456789abcdef"
	global.AIAccountControllerManager = accountCredentialReadStub{account: models.AIAccount{ID: id, RuntimeID: constants.RuntimeIDGemini, Revision: 1, AuthGeneration: 1, AuthMode: constants.AccountAuthModeAPIKey, HasCredential: true, Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolGoogleGenerativeAI, BaseURL: provider.URL + "/v1beta"}}, SelectedModels: []string{"gemini-3.8-flash"}}, secret: "local-fixture-key"}
	global.RuntimeEnvironmentControllerManager = officialThinkingEnvironment{environment: models.RuntimeEnvironment{RuntimeID: agent.RuntimeIDGemini, BaseRuntimeID: agent.RuntimeIDGemini, Enabled: true, Binary: node, PrefixArgs: []string{cli}}}
	global.AIAccountService = aiaccount.New(global.AIAccountControllerManager, t.TempDir())
	sdk, err := agent.NewCatalog(agent.CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	global.RuntimeCatalog = sdk
	owner := agent.NewProviderRuntime(agent.ProviderRuntimeOptions{AcquireAccount: global.AIAccountService.AcquireAccount})
	defer owner.Close(context.Background())
	global.RuntimeEnvironmentService, err = runtimeenvironment.New(runtimeenvironment.Config{
		ProviderRuntime: owner, ResolveAccount: global.AIAccountService.ResolveAccount, Accounts: global.AIAccountService,
		RuntimeCatalog: sdk, Environments: global.RuntimeEnvironmentControllerManager,
		Transactions: officialThinkingTransactions{}, AccountWorkspaceDirectory: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.GET("/api/v1/ai-accounts/:accountId/thinking", func(ctx *gin.Context) {
		if _, limited := ctx.Deadline(); limited {
			t.Error("unexpected API context deadline")
		}
		request := GetAIAccountThinking{AccountID: ctx.Param("accountId"), RuntimeID: ctx.Query("runtimeId"), Model: ctx.Query("model")}
		if err := request.Validate(ctx); err != nil {
			t.Error(err)
			ctx.Status(400)
			return
		}
		result, err := request.Output(ctx)
		if err != nil {
			t.Error(err)
			ctx.Status(500)
			return
		}
		ctx.JSON(200, result)
	})
	server := httptest.NewServer(engine)
	defer server.Close()
	response, err := http.Get(server.URL + "/api/v1/ai-accounts/" + id + "/thinking?runtimeId=gemini&model=gemini-3.8-flash")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result GetAIAccountThinkingResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	// Gemini 0.62 advertises no ACP thinking setter. A user-owned system
	// settings overlay is ignored by its native root-ownership security check.
	if response.StatusCode != 200 || len(result.Options) != 0 {
		t.Fatalf("HTTP invented a configurable native thinking menu: status=%d %+v", response.StatusCode, result)
	}
}
