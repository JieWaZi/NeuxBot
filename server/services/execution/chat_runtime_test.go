package execution

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"encoding/base64"
	"encoding/json"
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
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
)

// TestStartUsesOneRuntimeForChatAndTeam 验证普通输入、团队直接输入和团队调度复用同一宿主，并保留聚合接纳。
func TestStartUsesOneRuntimeForChatAndTeam(t *testing.T) {
	runtime := &fakeRuntime{}
	manager := newMessageTestService(t, t.TempDir(), runtime)
	core := manager.execution.(*service)
	ordinary, err := core.Start(context.Background(), WorkIntent{ClientRequestID: "ordinary", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}}, Input: WorkInput{Actor: "user", Content: "ordinary"}, Options: WorkOptions{RuntimeID: runtimeconfig.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto}})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, ordinary.Chat.ID)
	group, err := core.PrepareChatGroup(context.Background(), PrepareChatGroupIntent{ThreadID: "thread-group", Title: "团队执行", ProvisioningID: "provision-runtime", Scope: CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}, Members: []PrepareChatMember{{ChatID: "11111111111111111111111111111111", WakerID: "waker-1", RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}}})
	if err != nil {
		t.Fatal(err)
	}
	var admitted []bool
	var released int
	core.acquireTeamChatInput = func(ctx context.Context, chatID string, direct bool) (context.Context, func(), error) {
		if chatID != group.Chats[0].ID {
			t.Fatalf("unexpected team boundary %s", chatID)
		}
		admitted = append(admitted, direct)
		return ctx, func() { released++ }, nil
	}
	for _, actor := range []string{string(constants.ChannelMessageActorUser), string(constants.ChannelMessageActorLead)} {
		accepted, err := core.Start(context.Background(), WorkIntent{Chat: ChatTarget{ChatID: group.Chats[0].ID}, Input: WorkInput{Actor: actor, Content: actor}, Options: WorkOptions{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch})
		if err != nil {
			t.Fatal(err)
		}
		waitForChatTerminal(t, manager, accepted.Chat.ID)
	}
	if len(runtime.requests) != 3 || len(admitted) != 2 || !admitted[0] || admitted[1] || released != 2 {
		t.Fatalf("requests=%d team admission=%v released=%d", len(runtime.requests), admitted, released)
	}
	if runtime.requests[1].Workspace.Path != group.Chats[0].WorkspaceDirectory || runtime.requests[2].Workspace.Path != group.Chats[0].WorkspaceDirectory || runtime.requests[0].Workspace.Path == group.Chats[0].WorkspaceDirectory {
		t.Fatalf("runtime lost authoritative workspace: %+v", runtime.requests)
	}
	core.acquireTeamChatInput = func(context.Context, string, bool) (context.Context, func(), error) {
		return nil, nil, status_error.ChannelHasActiveWork
	}
	_, err = core.Start(context.Background(), WorkIntent{Chat: ChatTarget{ChatID: group.Chats[0].ID}, Input: WorkInput{Actor: "user", Content: "blocked direct input"}, Options: WorkOptions{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}})
	if !errors.Is(err, status_error.ChannelHasActiveWork) || len(runtime.requests) != 3 {
		t.Fatalf("team admission failure=%v requests=%d", err, len(runtime.requests))
	}
	history, err := manager.chats.ReadMessageHistory(context.Background(), group.Chats[0].ID, 0, 10)
	if err != nil || len(history.Resources) != 4 {
		t.Fatalf("team admission changed history: %+v %v", history, err)
	}

}

// leasedChatRuntime 模拟原生消费者关闭失败时保留的同一认证租约。
type leasedChatRuntime struct {
	accountRuntime                             // accountRuntime 复用真实接纳及配置目录投射。
	release        func(context.Context) error // release 保存技术宿主已接管的账号释放入口。
	closeError     bool                        // closeError 表示已有原生消费者尚未退出。
	// resources 保存仍受真实账号租约保护的实际资源。
	resources runtimecontract.SessionResources
	// resourceKey 是当前原生消费者使用的资源配置摘要。
	resourceKey string
}

// PrepareSession 先占用消费者，只有冷准备才由接纳层取得资源。
func (r *leasedChatRuntime) PrepareSession(ctx context.Context, plan runtimecontract.SessionPlan, session runtimecontract.SessionDirective) (runtimecontract.PreparedChatRun, error) {
	cold := r.release == nil
	if !cold && (plan.ResourceKey != r.resourceKey || session.Mode == runtimecontract.SessionModeNew || plan.ReadOnlyHandoff) {
		if err := r.CloseWorkspace(ctx, plan.Workspace.Path); err != nil {
			return nil, err
		}
		cold = true
	}
	prepared, err := prepareSynchronousRuntime(ctx, plan, session, r.accountRuntime.Run)
	if err != nil {
		return nil, err
	}
	prepared.bind = func(resources runtimecontract.SessionResources) {
		r.resources, r.resourceKey, r.release = resources, plan.ResourceKey, resources.Close
	}
	if !cold {
		_ = prepared.Bind(ctx, r.resources)
		// 热消费者沿用本轮续接动作，资源快照中的冷启动动作已经消费。
		prepared.session = session
	}
	prepared.close = func(ctx context.Context) error {
		if plan.ReadOnlyHandoff || (!prepared.started && cold) {
			return r.CloseWorkspace(ctx, plan.Workspace.Path)
		}
		return nil
	}
	return prepared, nil
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
		r.resources = runtimecontract.SessionResources{}
		r.resourceKey = ""
	}
	return nil
}
func TestStartManagedAccountWaitsForNativeCloseBeforeRewritingProjection(t *testing.T) {
	for _, scope := range []string{"ordinary", "team"} {
		t.Run(scope, func(t *testing.T) {
			runtime := &leasedChatRuntime{}
			manager := newMessageTestService(t, t.TempDir(), &runtime.accountRuntime, messageTestOptions{runtimeEnvironments: accountRuntimeEnvironmentReader{}})
			core := manager.execution.(*service)
			core.runtime = runtime
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
			chatTarget := ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}}
			if scope == "team" {
				group, err := core.PrepareChatGroup(context.Background(), PrepareChatGroupIntent{ThreadID: "account-thread", Title: "团队账号租约", ProvisioningID: "account-provision", Scope: *chatTarget.Create, Members: []PrepareChatMember{{ChatID: "11111111111111111111111111111111", WakerID: "waker-1", RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}}})
				if err != nil {
					t.Fatal(err)
				}
				chatTarget = ChatTarget{ChatID: group.Chats[0].ID}
			}
			clientRequestID := ""
			if chatTarget.Create != nil {
				clientRequestID = "managed-chat"
			}
			first, err := core.Start(context.Background(), WorkIntent{ClientRequestID: clientRequestID, Chat: chatTarget, Input: WorkInput{Actor: "user", Content: "first"}, Options: WorkOptions{RuntimeID: "codex", AccountID: firstAccount.ID, Model: "gpt-5", PermissionLevel: constants.PermissionLevelAuto}})
			if err != nil {
				t.Fatal(err)
			}
			waitForChatTerminal(t, manager, first.Chat.ID)
			if len(runtime.requests) != 1 || runtime.requests[0].Resources.Target.CloseAfterRun || runtime.release == nil {
				t.Fatalf("target=%+v lease=%v", runtime.requests, runtime.release != nil)
			}
			authPath := filepath.Join(accountconfig.Home(runtime.requests[0].Workspace.Path, runtimeconfig.RuntimeIDCodex), "auth.json")
			before, err := os.ReadFile(authPath)
			if err != nil {
				t.Fatal(err)
			}
			runtime.closeError = true
			_, err = core.Start(context.Background(), WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", Content: "switch"}, Options: WorkOptions{RuntimeID: "codex", AccountID: secondAccount.ID, Model: "gpt-5", PermissionLevel: constants.PermissionLevelAuto}})
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
		})
	}
}

// TestStartKeepsManagedSessionAcrossCredentialRevision 证明凭据并发版本变化不等同于原生静态配置变化。
func TestStartKeepsManagedSessionAcrossCredentialRevision(t *testing.T) {
	for _, authMode := range []constants.AccountAuthMode{constants.AccountAuthModeAPIKey, constants.AccountAuthModeSubscription} {
		t.Run(string(authMode), func(t *testing.T) {
			runtime := &leasedChatRuntime{}
			runtime.result.Session = runtimecontract.SessionResult{OutputID: "native-session", State: runtimecontract.SessionStateCreated}
			manager := newMessageTestService(t, t.TempDir(), &runtime.accountRuntime, messageTestOptions{runtimeEnvironments: accountRuntimeEnvironmentReader{}})
			core := manager.execution.(*service)
			core.runtime = runtime
			database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "accounts.db"), Migrate: true})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := database.DB.DB()
			t.Cleanup(func() { _ = sqlDB.Close() })
			accounts := controllers.NewAIAccountController(database.DB)
			accountService := aiaccount.New(accounts)
			core.accounts = accountService
			t.Cleanup(func() { runtime.closeError = false; _ = runtime.CloseWorkspace(context.Background(), "") })
			account := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", Name: "managed", AuthMode: authMode, Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL}}, SelectedModels: []string{"gpt-5"}}
			secret := "first-key"
			if authMode == constants.AccountAuthModeSubscription {
				payload, _ := json.Marshal(map[string]any{"sub": "user", "exp": time.Now().Add(time.Hour).Unix(), "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "workspace"}})
				token := "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
				encoded, _ := json.Marshal(map[string]any{"tokens": map[string]string{"id_token": token, "access_token": token, "refresh_token": "native-refresh"}, "last_refresh": time.Now().UTC()})
				secret = string(encoded)
			}
			if err := accounts.Create(context.Background(), account, secret); err != nil {
				t.Fatal(err)
			}
			options := WorkOptions{RuntimeID: "codex", AccountID: account.ID, Model: "gpt-5", PermissionLevel: constants.PermissionLevelAuto}
			first, err := core.Start(context.Background(), WorkIntent{ClientRequestID: "managed-credential-revision", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}}, Input: WorkInput{Actor: "user", Content: "first"}, Options: options})
			if err != nil {
				t.Fatal(err)
			}
			waitForChatTerminal(t, manager, first.Chat.ID)
			account, err = accounts.Get(context.Background(), account.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := accounts.StoreCredential(context.Background(), account.ID, account.Revision, secret, ""); err != nil {
				t.Fatal(err)
			}
			// 旧消费者若被错误重建，这里会报告关闭失败。
			runtime.closeError = true
			_, err = core.Start(context.Background(), WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", Content: "second"}, Options: options})
			if err != nil {
				t.Fatalf("credential revision invalidated hot session: %v", err)
			}
			waitForChatTerminal(t, manager, first.Chat.ID)
			if len(runtime.requests) != 2 {
				t.Fatalf("requests=%d", len(runtime.requests))
			}
			account, err = accounts.Get(context.Background(), account.ID)
			if err != nil {
				t.Fatal(err)
			}
			account.ModelContextSizes = map[string]int{"gpt-5": 64000}
			if err := accounts.Update(context.Background(), account, ""); err != nil {
				t.Fatal(err)
			}
			_, err = core.Start(context.Background(), WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", Content: "changed configuration"}, Options: options})
			if !errors.Is(err, status_error.RuntimeShutdownFailed) {
				t.Fatalf("static account configuration reused old consumer: %v", err)
			}
		})
	}
}
