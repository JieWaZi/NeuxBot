package execution

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
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
	manager := newMessageTestService(t, t.TempDir())
	if err := manager.wakers.Create(context.Background(), models.Waker{
		ID:   "waker-2",
		Name: "执行伙伴",
	}); err != nil {
		t.Fatalf("create second waker: %v", err)
	}
	threadID := "thread-shared-workspace"
	leadChatID := "11111111111111111111111111111111"
	memberChatID := "22222222222222222222222222222222"
	core := manager.execution.(*service)
	core.acquireTeamChatInput = func(ctx context.Context, chatID string, _ bool) (context.Context, func(), error) {
		if chatID == leadChatID && teamwork.IsMemberReplyRoute(ctx) {
			return nil, nil, status_error.ChatReplyTargetUnavailable
		}
		return ctx, func() {}, nil
	}
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
	shared := filepath.Join(filepath.Dir(group.Chats[0].WorkspaceDirectory), "shared")
	if info, err := os.Lstat(shared); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("team shared directory is not real: %v", err)
	}
	for _, chat := range group.Chats {
		if !slices.Contains(chat.AdditionalWorkingDirectories, shared) {
			t.Fatalf("member cannot access real team shared files: %+v", chat)
		}
		if _, err := os.Lstat(filepath.Join(chat.WorkspaceDirectory, "projects")); !os.IsNotExist(err) {
			t.Fatalf("member cwd retained obsolete project alias: %v", err)
		}
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
	waitForChatTerminal(t, manager, memberChatID)
	history, err := manager.chats.ReadMessageHistory(context.Background(), group.Chats[1].ID, 0, 10)
	if err != nil || len(history.Resources) != 2 || history.Resources[1].Content != "fixture reply" {
		t.Fatalf("member chat history = %+v, error = %v", history.Resources, err)
	}
	otherHistory, err := manager.chats.ReadMessageHistory(context.Background(), group.Chats[0].ID, 0, 10)
	if err != nil || len(otherHistory.Resources) != 0 {
		t.Fatalf("unassigned member chat history = %+v, error = %v", otherHistory.Resources, err)
	}
	reply := replyTestIntent(memberChatID, accepted.Execution.ID)
	crossChat := replyTestIntent(leadChatID, accepted.Execution.ID)
	if _, err := manager.execution.Start(t.Context(), crossChat); !errors.Is(err, status_error.ChatReplyTargetUnavailable) {
		t.Fatalf("另一成员 Chat 的文本成为引用: %v", err)
	}
	followUp, err := manager.execution.Start(t.Context(), reply)
	if err != nil || followUp.Chat.ID != memberChatID {
		t.Fatalf("成员 Chat 引用没有沿原路径发送: %+v, %v", followUp, err)
	}
	waitForChatTerminal(t, manager, memberChatID)
	memberHistory, err := manager.chats.ReadMessageHistory(t.Context(), memberChatID, 0, 10)
	if err != nil || len(memberHistory.Resources) != 4 || memberHistory.Resources[2].ReplyTo == nil ||
		memberHistory.Resources[2].ReplyTo.SourceWakerID != "waker-2" {
		t.Fatalf("成员 Chat 引用丢失原助手作者: %+v, %v", memberHistory.Resources, err)
	}
	if err := manager.chats.AppendParticipantMessages(t.Context(), leadChatID, []chatservice.ParticipantMessageInput{{
		ID: "team-report", Actor: string(constants.ChannelMessageActorMemberReport), Content: "只公开这段成员报告",
		SourceWakerID: "waker-2", SourceChatID: memberChatID,
	}}); err != nil {
		t.Fatal(err)
	}
	leadReply := replyTestIntent(leadChatID, "team-report")
	if _, err := manager.execution.Start(t.Context(), leadReply); !errors.Is(err, status_error.ChatReplyTargetUnavailable) {
		t.Fatalf("成员路径可以引用负责人时间线报告: %v", err)
	}
	leadReply.Options.EntryPoint = ""
	leadReply.Admission = constants.ExecutionAdmissionCommitBeforeDispatch
	leadReply.TeamReplyThreadID = "unrelated-thread"
	if _, err := manager.execution.Start(t.Context(), leadReply); !errors.Is(err, status_error.ChatReplyTargetUnavailable) {
		t.Fatalf("跨团队对话引用成为公开报告: %v", err)
	}
	leadReply.TeamReplyThreadID = threadID
	coordinated, err := manager.execution.Start(t.Context(), leadReply)
	if err != nil || coordinated.Chat.ID != leadChatID {
		t.Fatalf("报告引用没有进入负责人 Chat: %+v, %v", coordinated, err)
	}
	waitForChatTerminal(t, manager, leadChatID)
	leadHistory, err := manager.chats.ReadMessageHistory(t.Context(), leadChatID, 0, 10)
	if err != nil || len(leadHistory.Resources) != 3 {
		t.Fatalf("负责人引用历史: %+v, %v", leadHistory.Resources, err)
	}
	quote := leadHistory.Resources[1].ReplyTo
	if quote == nil || quote.MessageID != "team-report" || quote.Role != constants.MessageRoleAssistant ||
		quote.SourceWakerID != "waker-2" || quote.Preview != "只公开这段成员报告" {
		t.Fatalf("团队公开引用缺少报告作者或正文: %+v", quote)
	}
	genericLeadReply := replyTestIntent(leadChatID, coordinated.Execution.ID)
	genericLeadReply.Input.SubmissionID = "generic-lead-reply"
	if _, err := manager.execution.Start(t.Context(), genericLeadReply); !errors.Is(err, status_error.ChatReplyTargetUnavailable) {
		t.Fatalf("通用接口绕过主对话队列引用了负责人答复: %v", err)
	}
	if calls := manager.nativeCalls(t, "turn/start"); len(calls) != 3 ||
		!strings.Contains(string(calls[2]), `\"role\":\"assistant\",\"sourceWakerId\":\"waker-2\",\"text\":\"只公开这段成员报告\"`) {
		t.Fatalf("原生负责人输入缺少团队报告事实: %s", calls)
	}
}

func TestPrepareChatGroupPreservesRuntimeEnvironmentReadError(t *testing.T) {
	manager := newMessageTestService(
		t,
		t.TempDir(),
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
