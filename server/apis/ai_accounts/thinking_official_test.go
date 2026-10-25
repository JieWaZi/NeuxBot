package ai_accounts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/gin-gonic/gin"
)

// officialThinkingEnvironment 提供真实 HTTP 能力查询使用的固定运行时配置。
type officialThinkingEnvironment struct {
	// RuntimeEnvironmentController 保留本次查询未调用的方法。
	controllers.RuntimeEnvironmentController
	// environment 指向固定官方入口，认证由账号服务准备。
	environment models.RuntimeEnvironment
}

// Get 返回查询选择的 CLI 环境。
func (s officialThinkingEnvironment) Get(context.Context, string) (models.RuntimeEnvironment, error) {
	return s.environment, nil
}

// TestOfficialGeminiThinkingHTTP 验证真实 HTTP 用例经过账号服务的冷启动探测，没有短于实际 CLI 初始化的隐藏期限。
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
	oldAccounts, oldEnvironments, oldService, oldSDK := global.AIAccountControllerManager, global.RuntimeEnvironmentControllerManager, global.AIAccountService, global.RuntimeSDK
	defer func() {
		global.AIAccountControllerManager = oldAccounts
		global.RuntimeEnvironmentControllerManager = oldEnvironments
		global.AIAccountService = oldService
		global.RuntimeSDK = oldSDK
	}()
	id := "0123456789abcdef0123456789abcdef"
	global.AIAccountControllerManager = accountCredentialReadStub{account: models.AIAccount{ID: id, Revision: 1, AuthMode: constants.AccountAuthModeAPIKey, HasCredential: true, Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolGoogleGenerativeAI, BaseURL: provider.URL + "/v1beta"}}, SelectedModels: []string{"gemini-3.8-flash"}}, secret: "local-fixture-key"}
	global.RuntimeEnvironmentControllerManager = officialThinkingEnvironment{environment: models.RuntimeEnvironment{RuntimeID: runtimes.RuntimeIDGemini, BaseRuntimeID: runtimes.RuntimeIDGemini, Enabled: true, Binary: node, PrefixArgs: []string{cli}}}
	global.AIAccountService = aiaccount.New(global.AIAccountControllerManager)
	sdk, err := runtimes.New(runtimes.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close(context.Background())
	global.RuntimeSDK = sdk
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
	started := time.Now()
	response, err := http.Get(server.URL + "/api/v1/ai-accounts/" + id + "/thinking?runtimeId=gemini&model=gemini-3.8-flash")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result GetAIAccountThinkingResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || !result.Available {
		t.Fatalf("HTTP capability result: status=%d %+v", response.StatusCode, result)
	}
	for _, option := range result.Options {
		if option.ID == "low" {
			t.Logf("PASS HTTP GetAIAccountThinking → shared DetectModel → official CLI, duration=%s; no application deadline", time.Since(started))
			return
		}
	}
	t.Fatalf("native menu missing: %+v", result)
}
