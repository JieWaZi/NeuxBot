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

// TestStartUsesOneRuntimeForChatAndTeam 验证普通输入、团队直接输入和团队调度复用同一宿主，并保留聚合接纳。
func TestStartUsesOneRuntimeForChatAndTeam(t *testing.T) {
	runtime := &fakeRuntime{}
	manager := newMessageTestService(t, t.TempDir(), runtime)
	core := manager.execution.(*service)
	ordinary, err := core.Start(context.Background(), WorkIntent{ClientRequestID: "ordinary", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}}, Input: WorkInput{Actor: "user", Content: "ordinary"}, Options: WorkOptions{RuntimeID: runtimes.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto}})
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
}

func (r *leasedChatRuntime) RunWithLease(ctx context.Context, request runtimes.RunRequest, sink runtimes.EventSink, release func(context.Context) error) runtimes.Result {
	r.release = release
	return r.accountRuntime.RunWithLease(ctx, request, sink, nil)
}

// RunSummaryWithLease 先结束旧消费者，再同步完成隐藏交接及其资源释放。
func (r *leasedChatRuntime) RunSummaryWithLease(ctx context.Context, request runtimes.RunRequest, sink runtimes.EventSink, release func(context.Context) error) runtimes.Result {
	if err := r.CloseWorkspace(ctx, request.Workspace.Path); err != nil {
		if release != nil {
			_ = release(context.WithoutCancel(ctx))
		}
		return runtimes.Result{Failure: &runtimes.Failure{Code: runtimes.FailureCodeProcessExited, Message: err.Error()}}
	}
	return r.accountRuntime.RunSummaryWithLease(ctx, request, sink, release)
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
			if len(runtime.requests) != 1 || !runtime.requests[0].Target.CloseAfterRun || runtime.release == nil {
				t.Fatalf("target=%+v lease=%v", runtime.requests, runtime.release != nil)
			}
			authPath := filepath.Join(accountconfig.Home(runtime.requests[0].Workspace.Path, runtimes.RuntimeIDCodex), "auth.json")
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
