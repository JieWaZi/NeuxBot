package execution

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// failingRuntimeEnvironmentReader 为团队 Chat 准备测试保留底层环境查询错误。
type failingRuntimeEnvironmentReader struct{}

// Get 返回数据库读取失败，用于区分环境不可用与持久化故障。
func (failingRuntimeEnvironmentReader) Get(
	context.Context,
	string,
) (models.RuntimeEnvironment, error) {
	return models.RuntimeEnvironment{}, status_error.DatabaseError
}

func TestPrepareChatGroupUsesLeadMemberChatAsMainChat(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir(), &fakeRuntime{})
	if err := manager.wakers.Create(context.Background(), models.Waker{
		ID:   "waker-2",
		Name: "执行伙伴",
	}); err != nil {
		t.Fatalf("create second waker: %v", err)
	}
	threadID := "thread-shared-workspace"
	leadChatID := "11111111111111111111111111111111"
	memberChatID := "22222222222222222222222222222222"
	group, err := manager.execution.PrepareChatGroup(
		context.Background(),
		PrepareChatGroupIntent{
			ThreadID:       threadID,
			ProvisioningID: "provision-shared-workspace",
			Scope: CreateChatSpec{
				WakerID:          "waker-1",
				WorkingDirectory: t.TempDir(),
			},
			Title: "实现团队能力",
			Members: []PrepareChatMember{
				{
					ChatID:          leadChatID,
					WakerID:         "waker-1",
					RuntimeID:       "codex",
					PermissionLevel: constants.PermissionLevelAuto,
				},
				{
					ChatID:          memberChatID,
					WakerID:         "waker-2",
					RuntimeID:       "codex",
					PermissionLevel: constants.PermissionLevelAuto,
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("prepare team chats: %v", err)
	}
	if len(group.Chats) != 2 {
		t.Fatalf("member chat count = %d, want 2", len(group.Chats))
	}
	if group.Chats[0].WakerID != "waker-1" || group.Chats[1].WakerID != "waker-2" {
		t.Fatalf("member chat order or identity changed: %+v", group.Chats)
	}
	if group.Chats[0].ID != leadChatID || group.Chats[1].ID != memberChatID {
		t.Fatalf("prepared chat identities changed: %+v", group.Chats)
	}
	if group.Chats[0].WorkspaceDirectory == group.Chats[1].WorkspaceDirectory {
		t.Fatalf(
			"member chats must use isolated runtime cwd: %q vs %q",
			group.Chats[0].WorkspaceDirectory,
			group.Chats[1].WorkspaceDirectory,
		)
	}
	if filepath.Base(group.Chats[0].WorkspaceDirectory) != "leader" ||
		filepath.Base(group.Chats[1].WorkspaceDirectory) != "waker-2" {
		t.Fatalf(
			"member workspace directories = %q and %q",
			group.Chats[0].WorkspaceDirectory,
			group.Chats[1].WorkspaceDirectory,
		)
	}
	leadProjects, err := filepath.EvalSymlinks(filepath.Join(group.Chats[0].WorkspaceDirectory, "projects"))
	if err != nil {
		t.Fatalf("resolve lead projects: %v", err)
	}
	memberProjects, err := filepath.EvalSymlinks(filepath.Join(group.Chats[1].WorkspaceDirectory, "projects"))
	if err != nil {
		t.Fatalf("resolve member projects: %v", err)
	}
	if leadProjects != memberProjects {
		t.Fatalf("member project roots differ: %q vs %q", leadProjects, memberProjects)
	}
	accepted, err := manager.execution.Start(context.Background(), WorkIntent{
		Chat:  ChatTarget{ChatID: group.Chats[1].ID},
		Input: WorkInput{Actor: string(constants.ChannelMessageActorLead), Content: "执行成员任务"},
		Options: WorkOptions{
			RuntimeID:       constants.RuntimeIDCodex,
			PermissionLevel: constants.PermissionLevelAuto,
			SessionPolicy:   constants.RuntimeSessionPolicyContinue,
		},
		Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
	})
	if err != nil || accepted.Execution.ChatID != group.Chats[1].ID {
		t.Fatalf("start member execution = %+v, error = %v", accepted, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		summary, summaryErr := manager.chats.GetChatSummary(context.Background(), group.Chats[1].ID)
		if summaryErr == nil && summary.Chat.ActiveExecutionID == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("member execution did not finish: %v", summaryErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	history, err := manager.chats.ReadMessageHistory(context.Background(), group.Chats[1].ID, 0, 10)
	if err != nil || len(history.Resources) != 2 || history.Resources[1].Content != "执行完成" {
		t.Fatalf("member chat history = %+v, error = %v", history.Resources, err)
	}
	otherHistory, err := manager.chats.ReadMessageHistory(context.Background(), group.Chats[0].ID, 0, 10)
	if err != nil || len(otherHistory.Resources) != 0 {
		t.Fatalf("unassigned member chat history = %+v, error = %v", otherHistory.Resources, err)
	}
}

func TestPrepareChatGroupPreservesRuntimeEnvironmentReadError(t *testing.T) {
	manager := newMessageTestService(
		t,
		t.TempDir(),
		&fakeRuntime{},
		messageTestOptions{runtimeEnvironments: failingRuntimeEnvironmentReader{}},
	)
	_, err := manager.execution.PrepareChatGroup(context.Background(), PrepareChatGroupIntent{
		ThreadID:       "thread-runtime-error",
		ProvisioningID: "provision-runtime-error",
		Scope:          CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()},
		Title:          "验证 Runtime 环境错误",
		Members: []PrepareChatMember{{
			ChatID:          "33333333333333333333333333333333",
			WakerID:         "waker-1",
			RuntimeID:       "codex",
			PermissionLevel: constants.PermissionLevelAuto,
		}},
	})
	if !errors.Is(err, status_error.DatabaseError) {
		t.Fatalf("PrepareChatGroup() error = %v, want DatabaseError", err)
	}
}
