package execution

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestChatAccountSelectionUsesAccountModels(t *testing.T) {
	runtime := &accountRuntime{}
	manager := newMessageTestService(t, t.TempDir(), runtime, messageTestOptions{runtimeEnvironments: accountRuntimeEnvironmentReader{}})
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "accounts.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	accounts := controllers.NewAIAccountController(database.DB)
	accountService := aiaccount.New(accounts)
	accountService.Start(context.Background())
	t.Cleanup(accountService.Close)
	manager.execution.(*service).accounts = accountService
	account := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", Name: "shared", AuthMode: constants.AccountAuthModeAPIKey, Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL}}, SelectedModels: []string{"gpt-5"}}
	if err := accounts.Create(context.Background(), account, "private-key"); err != nil {
		t.Fatal(err)
	}
	resolved, err := manager.execution.ResolveConfiguration(context.Background(), ExecutionConfiguration{RuntimeID: constants.RuntimeIDCodex, AccountID: account.ID, PermissionLevel: constants.PermissionLevelDefault})
	if err != nil || resolved.AccountID != account.ID || resolved.Model != "gpt-5" {
		t.Fatalf("managed account default was replaced by native validation model: %+v %v", resolved, err)
	}
	accepted, err := manager.sendTestMessage(context.Background(), testMessageInput{
		NewChat: &testChatInput{WakerID: "waker-1", WorkingDirectory: t.TempDir()},
		Message: userMessage{Text: "first"}, Runtime: runtimeSelection{RuntimeID: constants.RuntimeIDCodex, AccountID: account.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	second, err := manager.sendTestMessage(context.Background(), testMessageInput{
		ChatID: accepted.Chat.ID, Message: userMessage{Text: "next"}, Runtime: runtimeSelection{RuntimeID: constants.RuntimeIDCodex},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	if accepted.Execution.AccountID != account.ID || second.Execution.AccountID != account.ID {
		t.Fatal("account did not persist across chat turns")
	}
	if len(runtime.requests) != 2 || !runtime.requests[0].Target.Isolated || !runtime.requests[1].Target.Isolated {
		t.Fatal("runtime did not receive managed configuration")
	}
	if runtime.requests[0].Policy.Model != "gpt-5" || runtime.requests[1].Policy.Model != "gpt-5" {
		t.Fatal("account model was not used across chat turns")
	}
	if _, err := manager.sendTestMessage(context.Background(), testMessageInput{
		ChatID: accepted.Chat.ID, Message: userMessage{Text: "invalid model"}, Runtime: runtimeSelection{RuntimeID: constants.RuntimeIDCodex, AccountID: account.ID, Model: "default"},
	}); err == nil {
		t.Fatal("model outside account selection was accepted")
	}
	if runtime.requests[0].Target.Environment["CODEX_HOME"] == "" || runtime.requests[1].Target.Environment["CODEX_HOME"] == "" {
		t.Fatal("missing native configuration home")
	}
	summary, err := manager.chats.GetChatSummary(context.Background(), accepted.Chat.ID)
	if err != nil || summary.Chat.LastAccountID != account.ID {
		t.Fatalf("account selection missing from chat projection: %v", err)
	}
	third, err := manager.sendTestMessage(context.Background(), testMessageInput{
		ChatID: accepted.Chat.ID, Message: userMessage{Text: "system"}, Runtime: runtimeSelection{RuntimeID: constants.RuntimeIDCodex, AccountID: "system"},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	if third.Execution.AccountID != "" || runtime.requests[2].Target.Isolated {
		t.Fatal("explicit system selection ignored")
	}
	data, _ := json.Marshal(accepted.Execution)
	if strings.Contains(string(data), "private-key") {
		t.Fatal("execution leaked account secret")
	}
	if runtime.requests[0].Session.Mode != runtimes.SessionModeNew {
		t.Fatal("new chat changed session mode")
	}
}

func TestChatAccountModelReachesPiAndKimiNativeConfiguration(t *testing.T) {
	for _, test := range []struct {
		// runtimeID 是本次测试的原生 CLI。
		runtimeID string
		// expectedModel 是 Runtime 接收的内部模型标识。
		expectedModel string
		// environmentKey 是 CLI 受管配置目录对应的环境变量。
		environmentKey string
	}{
		{constants.RuntimeIDPi, "deepseek-flash", "PI_CODING_AGENT_DIR"},
		{constants.RuntimeIDKimi, "deepseek-flash", "KIMI_CODE_HOME"},
	} {
		t.Run(test.runtimeID, func(t *testing.T) {
			runtime := &accountRuntime{}
			manager := newMessageTestService(t, t.TempDir(), runtime, messageTestOptions{runtimeEnvironments: accountRuntimeEnvironmentReader{}})
			database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "accounts.db"), Migrate: true})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := database.DB.DB()
			t.Cleanup(func() { _ = sqlDB.Close() })
			accounts := controllers.NewAIAccountController(database.DB)
			accountService := aiaccount.New(accounts)
			accountService.Start(context.Background())
			t.Cleanup(accountService.Close)
			manager.execution.(*service).accounts = accountService
			account := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", Name: "DeepSeek", AuthMode: constants.AccountAuthModeAPIKey,
				Endpoints:      []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolOpenAICompletions, BaseURL: "https://api.deepseek.com/v1"}},
				SelectedModels: []string{"deepseek-flash"}}
			if err := accounts.Create(context.Background(), account, "private-key"); err != nil {
				t.Fatal(err)
			}
			accepted, err := manager.sendTestMessage(context.Background(), testMessageInput{
				NewChat: &testChatInput{WakerID: "waker-1", WorkingDirectory: t.TempDir()},
				Message: userMessage{Text: "你好"}, Runtime: runtimeSelection{RuntimeID: test.runtimeID, AccountID: account.ID, Model: "deepseek-flash"},
			})
			if err != nil {
				t.Fatal(err)
			}
			waitForChatTerminal(t, manager, accepted.Chat.ID)
			if len(runtime.requests) != 1 || runtime.requests[0].Policy.Model != test.expectedModel || runtime.requests[0].Target.Environment[test.environmentKey] == "" {
				t.Fatal("chat model did not reach the selected native CLI configuration")
			}
		})
	}
}

// accountRuntimeEnvironmentReader 提供与所选智能体一致的真实基础类型。
type accountRuntimeEnvironmentReader struct{}

func (accountRuntimeEnvironmentReader) Get(ctx context.Context, id string) (models.RuntimeEnvironment, error) {
	environment, err := (readyRuntimeEnvironmentReader{}).Get(ctx, id)
	environment.RuntimeID = id
	environment.BaseRuntimeID = id
	environment.Detection.Runtime.ID = id
	environment.Detection.Runtime.BaseRuntimeID = id
	return environment, err
}

// accountRuntime 复用执行事件替身，同时声明两个真实的 workspace 布局。
type accountRuntime struct {
	// fakeRuntime 保存执行输入并提供正常完成事件。
	fakeRuntime
}

// Runtimes 允许契约测试跨 Codex 与 Claude 切换。
func (r *accountRuntime) Runtimes() []runtimes.RuntimeSummary {
	entries := r.fakeRuntime.Runtimes()
	entries = append(entries, runtimes.RuntimeSummary{ID: constants.RuntimeIDClaude, BaseRuntimeID: constants.RuntimeIDClaude, InstructionFileName: "CLAUDE.md", SkillDirectoryName: ".claude"})
	entries = append(entries,
		runtimes.RuntimeSummary{ID: constants.RuntimeIDPi, BaseRuntimeID: constants.RuntimeIDPi, InstructionFileName: "AGENTS.md", SkillDirectoryName: ".agents"},
		runtimes.RuntimeSummary{ID: constants.RuntimeIDKimi, BaseRuntimeID: constants.RuntimeIDKimi, InstructionFileName: "AGENTS.md", SkillDirectoryName: ".agents"},
	)
	return entries
}
