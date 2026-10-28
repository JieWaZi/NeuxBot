package execution

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
)

func TestStartChatSelectsExplicitRuntimeAndSharedStartKeepsExistingRunner(t *testing.T) {
	legacy, chat := &fakeRuntime{}, &fakeRuntime{}
	manager := newMessageTestService(t, t.TempDir(), legacy)
	core := manager.execution.(*service)
	core.chatRuntime = chat
	intent := WorkIntent{ClientRequestID: "chat-route", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}}, Input: WorkInput{Actor: "user", Content: "ordinary"}, Options: WorkOptions{RuntimeID: runtimes.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto}}
	accepted, err := core.StartChat(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	if len(chat.requests) != 1 || len(legacy.requests) != 0 {
		t.Fatalf("ordinary route chat=%d shared=%d", len(chat.requests), len(legacy.requests))
	}
	intent.ClientRequestID = "shared-route"
	accepted, err = core.Start(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	if len(chat.requests) != 1 || len(legacy.requests) != 1 {
		t.Fatalf("shared route chat=%d shared=%d", len(chat.requests), len(legacy.requests))
	}
}

func TestStartChatKeepsAuthoritativeTeamGroupOnSharedRunner(t *testing.T) {
	legacy, chat := &fakeRuntime{}, &fakeRuntime{}
	manager := newMessageTestService(t, t.TempDir(), legacy)
	manager.execution.(*service).chatRuntime = chat
	group, err := manager.execution.PrepareChatGroup(context.Background(), PrepareChatGroupIntent{ThreadID: "thread-group", Title: "团队路由", ProvisioningID: "provision-route", Scope: CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}, Members: []PrepareChatMember{{ChatID: "11111111111111111111111111111111", WakerID: "waker-1", RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}}})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := manager.execution.StartChat(context.Background(), WorkIntent{Chat: ChatTarget{ChatID: group.Chats[0].ID}, Input: WorkInput{Actor: "user", Content: "manual team member input"}, Options: WorkOptions{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	if len(legacy.requests) != 1 || len(chat.requests) != 0 {
		t.Fatalf("team shared=%d chat=%d", len(legacy.requests), len(chat.requests))
	}
}

// leasedChatRuntime 模拟原生消费者关闭失败时保留的同一认证租约。
type leasedChatRuntime struct {
	accountRuntime                             // accountRuntime 复用真实接纳及配置目录投射。
	release        func(context.Context) error // release 保存技术宿主已接管的账号释放入口。
	closeError     bool                        // closeError 表示已有原生消费者尚未退出。
}

func (r *leasedChatRuntime) RunWithLease(ctx context.Context, request runtimes.RunRequest, sink runtimes.EventSink, release func(context.Context) error) runtimes.Result {
	r.release = release
	return r.Run(ctx, request, sink)
}
func (r *leasedChatRuntime) CloseWorkspace(ctx context.Context, _ string) error {
	if r.closeError {
		return status_error.RuntimeShutdownFailed
	}
	if r.release != nil {
		if err := r.release(ctx); err != nil {
			return err
		}
		r.release = nil
	}
	return nil
}
func TestStartChatManagedAccountWaitsForNativeCloseBeforeRewritingProjection(t *testing.T) {
	runtime := &leasedChatRuntime{}
	manager := newMessageTestService(t, t.TempDir(), &runtime.accountRuntime, messageTestOptions{runtimeEnvironments: accountRuntimeEnvironmentReader{}})
	core := manager.execution.(*service)
	core.chatRuntime = runtime
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
	core.accounts = accountService
	t.Cleanup(func() { runtime.closeError = false; _ = runtime.CloseWorkspace(context.Background(), "") })
	firstAccount := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", Name: "first", AuthMode: constants.AccountAuthModeAPIKey, Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL}}, SelectedModels: []string{"gpt-5"}}
	secondAccount := firstAccount
	secondAccount.ID = "1123456789abcdef0123456789abcdef"
	secondAccount.Name = "second"
	if err := accounts.Create(context.Background(), firstAccount, "first-key"); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Create(context.Background(), secondAccount, "second-key"); err != nil {
		t.Fatal(err)
	}
	first, err := core.StartChat(context.Background(), WorkIntent{ClientRequestID: "managed-chat", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}}, Input: WorkInput{Actor: "user", Content: "first"}, Options: WorkOptions{RuntimeID: "codex", AccountID: firstAccount.ID, Model: "gpt-5", PermissionLevel: constants.PermissionLevelAuto}})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	if len(runtime.requests) != 1 || !runtime.requests[0].Target.CloseAfterRun || runtime.release == nil {
		t.Fatalf("target=%+v lease=%v", runtime.requests, runtime.release != nil)
	}
	authPath := filepath.Join(accountconfig.Home(runtime.requests[0].Workspace.Path, runtimes.RuntimeIDCodex), "auth.json")
	before, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	runtime.closeError = true
	_, err = core.StartChat(context.Background(), WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", Content: "switch"}, Options: WorkOptions{RuntimeID: "codex", AccountID: secondAccount.ID, Model: "gpt-5", PermissionLevel: constants.PermissionLevelAuto}})
	if !errors.Is(err, status_error.RuntimeShutdownFailed) {
		t.Fatalf("close failure=%v", err)
	}
	after, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || strings.Contains(string(after), "second-key") || len(runtime.requests) != 1 {
		t.Fatalf("overwrote credentials before native close, requests=%d", len(runtime.requests))
	}
}
