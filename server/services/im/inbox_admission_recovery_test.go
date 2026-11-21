package im

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/channel"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

func TestIMAdmissionRetriesLinkageFailure(t *testing.T) {
	for _, tc := range []struct {
		// name 是本轮接纳和重新启动时的授权情形。
		name string
		// existing 表示目标 Chat 已经关联。
		existing bool
		// revoked 表示接纳后、重启前撤销了授权。
		revoked bool
		// replayError 表示重新调用执行入口时读取额外信息失败。
		replayError bool
		// attachmentRemoved 模拟重启前附件暂存目录已无法打开。
		attachmentRemoved bool
		// chatDeleted 模拟收件补挂前，用户已删除完成工作的 Chat。
		chatDeleted bool
	}{
		{name: "partner_first_send"},
		{name: "partner_continuation", existing: true},
		{name: "partner_revoked_after_admission", revoked: true},
		{name: "partner_first_send_replay_error", replayError: true},
		{name: "partner_continuation_replay_error", existing: true, replayError: true},
		{name: "partner_first_send_attachment_unavailable", attachmentRemoved: true},
		{name: "partner_first_send_deleted_chat", chatDeleted: true},
		{name: "partner_continuation_deleted_chat", existing: true, chatDeleted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			s, db, _ := newIMFixture(t)
			attachConversationReaders(t, s, db)
			s.ctx = ctx
			firstRuntime := &imRecoveryExecutions{chats: s.config.Chats, directory: s.config.DataDirectory}
			s.config.Executions = firstRuntime
			link := models.IMConversationLink{ID: "im-link", BindingID: "b", Configuration: models.IMConversationConfiguration{WakerID: "waker"}}
			chatID := (requestidempotency.Identity{ID: link.ID}).ResourceID("chat", "waker")
			if tc.existing {
				if _, err := createAdmissionChat(ctx, s.config.Chats, s.config.DataDirectory, chatID, "waker"); err != nil {
					t.Fatal(err)
				}
				link.ChatID = chatID
			}
			if err := s.config.Store.SaveLink(ctx, link); err != nil {
				t.Fatal(err)
			}
			input := models.IMInbox{ID: "im-admission-recovery", ConnectionID: "c", BindingID: "b", LinkID: link.ID, ExecutionID: "execution-recovery", Content: "继续处理", State: constants.IMInboxStateRouted}
			if tc.attachmentRemoved {
				input.Files = []models.IMFile{{Name: "note.txt", Kind: "file"}}
			}
			for _, item := range []models.IMInbox{
				input,
				{ID: "im-next-input", ConnectionID: "c", BindingID: "b", LinkID: link.ID, ExecutionID: "execution-next", Content: "下一条", State: constants.IMInboxStateRouted},
			} {
				if err := s.config.Store.SaveInbox(ctx, item); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Exec(`CREATE TRIGGER im_handoff_failure BEFORE UPDATE OF state ON im_inbox
				WHEN NEW.id = 'im-admission-recovery' AND NEW.state = 'accepted'
				BEGIN SELECT RAISE(ABORT, 'injected handoff failure'); END`).Error; err != nil {
				t.Fatal(err)
			}
			s.dispatch(input)
			s.workers.Wait()
			stored, err := s.config.Store.Link(ctx, link.ID)
			if err != nil || stored.ChatID != link.ChatID {
				t.Fatalf("收件事务失败后对话关联未回滚: %+v %v", stored, err)
			}
			queued, err := s.config.Store.Inbox(ctx, constants.IMInboxStateRouted, 10)
			if err != nil || len(queued) != 1 || queued[0].ID != input.ID || queued[0].ExecutionID != input.ExecutionID {
				t.Fatalf("已接纳的原收件未留在队首: %+v %v", queued, err)
			}
			if _, exists, err := s.config.Store.FindDelivery(ctx, "input-failed:"+input.ID); err != nil || exists {
				t.Fatalf("已接纳工作产生了失败回执: exists=%t error=%v", exists, err)
			}
			if err := db.Exec("DROP TRIGGER im_handoff_failure").Error; err != nil {
				t.Fatal(err)
			}
			if tc.attachmentRemoved {
				mediaPath := queued[0].Files[0].Path
				if err := os.Remove(mediaPath); err != nil {
					t.Fatal(err)
				}
				mediaDirectory := filepath.Dir(mediaPath)
				if err := os.Remove(mediaDirectory); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(mediaDirectory, []byte("inaccessible staging directory"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.chatDeleted {
				receipt, err := s.config.Chats.ReadSubmission(ctx, chatID, input.ID)
				if err != nil || receipt == nil {
					t.Fatalf("读取删除前的原接纳回执: %+v %v", receipt, err)
				}
				data, err := json.Marshal(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusFailed})
				if err != nil {
					t.Fatal(err)
				}
				terminal, err := s.config.Chats.CommitTerminal(ctx, chat.TerminalFact{Type: constants.ChatEventTypeExecutionFailed, Execution: *receipt, Data: data})
				if err != nil {
					t.Fatal(err)
				}
				s.config.Chats.PublishTerminal(terminal)
				if err := s.config.Chats.DeleteChat(ctx, chatID); err != nil {
					t.Fatal(err)
				}
			}
			if tc.revoked {
				if err := db.Model(&models.IMBinding{}).Where("id = ?", input.BindingID).Update("state", constants.IMBindingStateRejected).Error; err != nil {
					t.Fatal(err)
				}
			}
			restarted := New(s.config)
			restarted.ctx = ctx
			attachConversationReaders(t, restarted, db)
			restarted.config.Store = controllers.NewIMController(db)
			nextRuntime := &imRecoveryExecutions{chats: restarted.config.Chats, directory: restarted.config.DataDirectory, replayError: tc.replayError}
			restarted.config.Executions = nextRuntime
			restarted.dispatch(queued[0])
			restarted.workers.Wait()
			stored, err = restarted.config.Store.Link(ctx, link.ID)
			if err != nil || stored.ChatID != chatID {
				t.Fatalf("重启后未恢复原对话关联: %+v %v", stored, err)
			}
			admitted, err := restarted.config.Store.Inbox(ctx, constants.IMInboxStateAccepted, 10)
			if err != nil || len(admitted) != 1 || admitted[0].ID != input.ID || admitted[0].ExecutionID != input.ExecutionID {
				t.Fatalf("重启后未提交原接纳收件: %+v %v", admitted, err)
			}
			if firstRuntime.commits != 1 || nextRuntime.calls != 0 {
				t.Fatalf("恢复时重新调用执行入口: first=%d restored=%d", firstRuntime.commits, nextRuntime.calls)
			}
			if tc.chatDeleted {
				if _, err := restarted.config.Chats.GetChatSummary(ctx, chatID); !hasCode(err, status_error.ChatNotFound) {
					t.Fatalf("删除的原 Chat 被重新创建: %v", err)
				}
				return
			}
			count, err := restarted.config.Chats.CountUserMessages(ctx, chatID)
			if err != nil || count != 1 {
				t.Fatalf("恢复改变了原 Chat 的消息: messages=%d error=%v", count, err)
			}
		})
	}
}

type revokeDuringDownloadPlatform struct {
	// recordingPlatform 提供用例未覆盖的传输行为。
	*recordingPlatform
	// revoke 在下载返回之前提交授权撤销。
	revoke func(context.Context) error
}

func (p *revokeDuringDownloadPlatform) Download(ctx context.Context, _ models.IMConnection, _ string, _ models.IMFile) ([]byte, error) {
	return []byte("note"), p.revoke(ctx)
}

func TestIMRevocationDuringAttachmentDownloadPreventsAdmission(t *testing.T) {
	ctx := t.Context()
	s, db, platform := newIMFixture(t)
	attachConversationReaders(t, s, db)
	s.ctx = ctx
	executions := &imRecoveryExecutions{chats: s.config.Chats, directory: s.config.DataDirectory}
	s.config.Executions = executions
	s.config.Platforms[constants.IMPlatformWeixin] = &revokeDuringDownloadPlatform{recordingPlatform: platform, revoke: func(ctx context.Context) error {
		return s.WithBinding(ctx, "b", func(tx context.Context, binding models.IMBinding) error {
			return s.config.Store.SetBindingState(tx, binding.ID, constants.IMBindingStateRejected)
		})
	}}
	link := models.IMConversationLink{ID: "im-revoke-link", BindingID: "b", Configuration: models.IMConversationConfiguration{WakerID: "waker"}}
	if err := s.config.Store.SaveLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	input := models.IMInbox{ID: "im-revoke-input", ConnectionID: "c", BindingID: "b", LinkID: link.ID, ExecutionID: "execution-revoked", Content: "Read file", State: constants.IMInboxStateRouted, Files: []models.IMFile{{Name: "note.txt", Kind: "file"}}}
	if err := s.config.Store.SaveInbox(ctx, input); err != nil {
		t.Fatal(err)
	}
	s.dispatch(input)
	s.workers.Wait()
	failed, err := s.config.Store.Inbox(ctx, constants.IMInboxStateFailed, 10)
	if err != nil || len(failed) != 1 || failed[0].ErrorCode != status_error.IMChatNotAuthorized.Key() || executions.commits != 0 {
		t.Fatalf("撤销后仍提交接纳: commits=%d failed=%+v error=%v", executions.commits, failed, err)
	}
}

func TestIMTeamQueueCommitsWithInbox(t *testing.T) {
	ctx := t.Context()
	s, db, _ := newIMFixture(t)
	threads := controllers.NewChannelThreadController(db)
	input := models.IMInbox{
		ID: "im-queued", BindingID: "b", LinkID: "team-link", ConnectionID: "c",
		State: constants.IMInboxStateRouted, ExecutionID: "not-a-lead-execution",
	}
	if err := s.config.Store.SaveInbox(ctx, input); err != nil {
		t.Fatal(err)
	}
	queued := models.ChannelLeadInput{
		ID: channel.LeadInputID("team", input.ID), ThreadID: "team", Status: constants.ChannelInputStatusPending,
		Payload: execution.WorkInput{SubmissionID: input.ID, IMReplyLinkID: input.LinkID, Content: "Continue"},
	}
	admit := s.teamQueueAdmission(input, s.bindingAdmission(input.BindingID))
	commit := func(ctx context.Context) error {
		return s.config.Transactions.Transaction(ctx, func(tx context.Context) error {
			return threads.CreateLeadInput(tx, queued)
		})
	}
	if err := db.Exec(`CREATE TRIGGER im_queue_handoff_failure BEFORE UPDATE OF state ON im_inbox
		WHEN NEW.id = 'im-queued' AND NEW.state = 'accepted'
		BEGIN SELECT RAISE(ABORT, 'injected handoff failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := admit.Commit(ctx, commit); !hasCode(err, status_error.DatabaseError) {
		t.Fatalf("预期收件提交失败: %v", err)
	}
	if _, found, err := threads.GetLeadInput(ctx, queued.ID); err != nil || found {
		t.Fatalf("收件回滚后仍有负责人队列记录: exists=%v error=%v", found, err)
	}
	if err := db.Exec("DROP TRIGGER im_queue_handoff_failure").Error; err != nil {
		t.Fatal(err)
	}
	if err := admit.Commit(ctx, commit); err != nil {
		t.Fatal(err)
	}
	if _, found, err := threads.GetLeadInput(ctx, queued.ID); err != nil || !found {
		t.Fatalf("收件已接纳但没有负责人队列记录: exists=%v error=%v", found, err)
	}
	admitted, err := s.config.Store.Inbox(ctx, constants.IMInboxStateAccepted, 10)
	if err != nil || len(admitted) != 1 || admitted[0].ID != input.ID || admitted[0].ExecutionID != "" {
		t.Fatalf("团队排队未同步提交原收件: %+v %v", admitted, err)
	}
}

type blockedAdmissionDownload struct {
	// recordingPlatform 提供其他平台操作的替身。
	*recordingPlatform
	// started 在附件进入下载时通知测试。
	started chan struct{}
	// release 在对话关联检查完成后结束下载。
	release chan struct{}
}

func (p *blockedAdmissionDownload) Download(ctx context.Context, _ models.IMConnection, _ string, _ models.IMFile) ([]byte, error) {
	p.started <- struct{}{}
	select {
	case <-p.release:
		return nil, status_error.IMFileInvalid
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestIMDispatchKeepsInboxOwnershipAfterRelink(t *testing.T) {
	s, db, platform := newIMFixture(t)
	attachConversationReaders(t, s, db)
	ctx := t.Context()
	s.ctx = ctx
	staged := &blockedAdmissionDownload{recordingPlatform: platform, started: make(chan struct{}, 2), release: make(chan struct{})}
	s.config.Platforms[constants.IMPlatformWeixin] = staged
	link := models.IMConversationLink{
		ID: "im-deleted", BindingID: "b", ChatID: "a480c877ac7349039c9d9d5f5fa07cef",
		Configuration: models.IMConversationConfiguration{WakerID: "waker"},
	}
	if err := s.config.Store.SaveLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	input := models.IMInbox{
		ID: "im-relinked", BindingID: "b", LinkID: link.ID, ConnectionID: "c", Content: "Read file",
		State: constants.IMInboxStateRouted, Files: []models.IMFile{{Name: "note.txt", Kind: "file"}},
	}
	if err := s.config.Store.SaveInbox(ctx, input); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.workers.Wait)
	t.Cleanup(func() { close(staged.release) })
	s.dispatch(input)
	select {
	case <-staged.started:
	case <-time.After(5 * time.Second):
		t.Fatal("收件没有进入附件下载")
	}
	queued, err := s.config.Store.Inbox(ctx, constants.IMInboxStateRouted, 10)
	if err != nil || len(queued) != 1 || queued[0].LinkID == link.ID {
		t.Fatalf("收件未保存替代关联: %+v %v", queued, err)
	}
	s.dispatch(queued[0])
	s.life.Lock()
	workers := s.dispatchConnections[input.ConnectionID]
	s.life.Unlock()
	if workers != 1 {
		t.Fatalf("同一收件在替换关联后占用了 %d 个工作者", workers)
	}
}

func TestIMTeamAdmissionUsesOwnDurableReceipt(t *testing.T) {
	for _, tc := range []struct {
		// name 标识首发、续发和派发回执情形。
		name string
		// first 表示团队首发已接纳但 IM 关系尚未建立。
		first bool
		// revoked 表示恢复时已撤销原绑定。
		revoked bool
		// submitError 是团队已接纳后重新提交或派发时的错误。
		submitError error
	}{
		{name: "revoked_first_send", first: true, revoked: true},
		{name: "revoked_continuation", revoked: true},
		{name: "first_send_replay_error", first: true, submitError: status_error.DatabaseError},
		{name: "drained_unrelated_queue_entry"},
		{name: "drain_failed_after_enqueue", submitError: status_error.RuntimeUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			s, db, _ := newIMFixture(t)
			attachConversationReaders(t, s, db)
			s.ctx = ctx
			team := models.ChannelThread{ID: "team-thread", ChannelID: "team", LeadWakerID: "waker", LeadChatID: "0df6b383ac3a439aa36deaab5af91f0a"}
			link := models.IMConversationLink{ID: "team-link", BindingID: "b", Configuration: models.IMConversationConfiguration{ChannelID: team.ChannelID}}
			input := models.IMInbox{ID: "im-team-input", ConnectionID: "c", BindingID: link.BindingID, LinkID: link.ID, ExecutionID: "initial-execution", Content: "处理任务", State: constants.IMInboxStateRouted}
			if _, err := createAdmissionChat(ctx, s.config.Chats, s.config.DataDirectory, team.LeadChatID, team.LeadWakerID); err != nil {
				t.Fatal(err)
			}
			team.ClientRequestID = &link.ID
			ready := int64(1)
			team.ReadyAt = &ready
			if err := s.config.Threads.Create(ctx, team, []models.ChannelThreadMember{{ThreadID: team.ID, WakerID: team.LeadWakerID, ChatID: team.LeadChatID, IsLead: true}}); err != nil {
				t.Fatal(err)
			}
			work := execution.WorkInput{SubmissionID: input.ID, Content: input.Content, IMReplyLinkID: link.ID}
			if tc.first {
				executions := &imRecoveryExecutions{chats: s.config.Chats}
				if _, err := executions.Start(ctx, execution.WorkIntent{Chat: execution.ChatTarget{ChatID: team.LeadChatID}, ExecutionID: input.ExecutionID, Input: work}); err != nil {
					t.Fatal(err)
				}
			} else {
				link.ChatID, link.ThreadID = team.LeadChatID, team.ID
				if err := s.config.Threads.CreateLeadInput(ctx, models.ChannelLeadInput{
					ID: channel.LeadInputID(team.ID, input.ID), ThreadID: team.ID, Payload: work, Status: constants.ChannelInputStatusPending,
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.config.Store.SaveLink(ctx, link); err != nil {
				t.Fatal(err)
			}
			if err := s.config.Store.SaveInbox(ctx, input); err != nil {
				t.Fatal(err)
			}
			if tc.revoked {
				if err := db.Model(&models.IMBinding{}).Where("id = ?", input.BindingID).Update("state", constants.IMBindingStateRejected).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				s.config.Teams = imRecoveryQueuedResult{submitError: tc.submitError}
			}
			s.dispatch(input)
			s.workers.Wait()
			stored, err := s.config.Store.Link(ctx, link.ID)
			if err != nil || stored.ChatID != team.LeadChatID || stored.ThreadID != team.ID {
				t.Fatalf("没有登记原团队关联: %+v %v", stored, err)
			}
			accepted, err := s.config.Store.Inbox(ctx, constants.IMInboxStateAccepted, 10)
			executionID := ""
			if tc.first {
				executionID = input.ExecutionID
			}
			if err != nil || len(accepted) != 1 || accepted[0].ID != input.ID || accepted[0].ExecutionID != executionID {
				t.Fatalf("未记录原首发或续发接纳，或关联了其他排队执行: %+v %v", accepted, err)
			}
		})
	}
}

func TestIMTeamLeadFailureSurvivesOutboxRollbackAndRestart(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		name := "approved"
		if revoked {
			name = "revoked_before_recovery"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			s, db, platform := newIMFixture(t)
			attachConversationReaders(t, s, db)
			s.ctx = ctx
			wakers := controllers.NewWakerController(db)
			for _, id := range []string{"waker", "member"} {
				if err := wakers.Create(ctx, models.Waker{ID: id, Name: id}); err != nil {
					t.Fatal(err)
				}
			}
			channels := controllers.NewChannelController(db)
			team := models.Channel{ID: "team", Name: "测试团队", LeadWakerID: "waker"}
			thread := models.ChannelThread{ID: "team-thread", ChannelID: team.ID, LeadWakerID: team.LeadWakerID, LeadChatID: "0df6b383ac3a439aa36deaab5af91f0a"}
			configured := []models.ChannelMember{
				{ChannelID: team.ID, WakerID: team.LeadWakerID, RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelDefault},
				{ChannelID: team.ID, WakerID: "member", RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelDefault, Position: 1},
			}
			if err := channels.Create(ctx, team, configured); err != nil {
				t.Fatal(err)
			}
			if _, err := createAdmissionChat(ctx, s.config.Chats, s.config.DataDirectory, thread.LeadChatID, thread.LeadWakerID); err != nil {
				t.Fatal(err)
			}
			ready := int64(1)
			thread.ReadyAt = &ready
			if err := s.config.Threads.Create(ctx, thread, []models.ChannelThreadMember{
				{ThreadID: thread.ID, WakerID: thread.LeadWakerID, ChatID: thread.LeadChatID, IsLead: true, RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelDefault},
			}); err != nil {
				t.Fatal(err)
			}
			link := models.IMConversationLink{ID: "original-link", BindingID: "b", ThreadID: thread.ID, ChatID: thread.LeadChatID, Configuration: models.IMConversationConfiguration{ChannelID: team.ID}}
			if err := s.config.Store.SaveLink(ctx, link); err != nil {
				t.Fatal(err)
			}
			current := models.IMConversationLink{ID: "current-link", BindingID: link.BindingID, Configuration: link.Configuration}
			if err := s.config.Store.SaveLink(ctx, current); err != nil {
				t.Fatal(err)
			}
			if err := s.config.Store.SetBindingActiveLink(ctx, link.BindingID, current.ID); err != nil {
				t.Fatal(err)
			}
			executions := &unavailableIMLeadExecution{}
			config := channel.Config{
				Channels: channels, Threads: s.config.Threads, Tasks: controllers.NewChannelTaskController(db),
				Transactions: s.config.Transactions, Wakers: wakers, Chats: s.config.Chats, Executions: executions,
				ChatGroups: executions, RecordIMLeadInputFailure: s.RecordTeamLeadInputFailure,
			}
			original, err := channel.New(config)
			if err != nil {
				t.Fatal(err)
			}
			s.config.Teams = original
			input := models.IMInbox{ID: "lead-input", ConnectionID: "c", BindingID: link.BindingID, LinkID: link.ID, Content: "处理后续要求", State: constants.IMInboxStateRouted}
			if err := s.config.Store.SaveInbox(ctx, input); err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(`CREATE TRIGGER fail_im_team_outbox BEFORE INSERT ON im_deliveries
				BEGIN SELECT RAISE(ABORT, 'injected outbox failure'); END`).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.accept(ctx, &input); err != nil {
				t.Fatalf("未保存原 IM 队列接纳: %v", err)
			}
			inputID := channel.LeadInputID(thread.ID, input.ID)
			queued, found, err := s.config.Threads.GetLeadInput(ctx, inputID)
			if err != nil || !found || queued.Status != constants.ChannelInputStatusPending {
				t.Fatalf("失败交付回滚后原输入未留待恢复: %+v found=%t error=%v", queued, found, err)
			}
			if err := db.Exec("DROP TRIGGER fail_im_team_outbox").Error; err != nil {
				t.Fatal(err)
			}
			if revoked {
				if err := s.config.Store.SetBindingState(ctx, link.BindingID, constants.IMBindingStateRejected); err != nil {
					t.Fatal(err)
				}
			}
			restarted, err := channel.New(config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(restarted.Close)
			if err := restarted.Start(ctx); err != nil {
				t.Fatalf("原 IM 输入故障阻止团队启动: %v", err)
			}
			s.config.Teams = restarted
			if err := s.accept(ctx, &input); err != nil {
				t.Fatalf("无法重放原 IM 队列接纳: %v", err)
			}
			queued, found, err = s.config.Threads.GetLeadInput(ctx, inputID)
			if err != nil || !found || queued.Status != constants.ChannelInputStatusFailed || queued.ErrorCode != status_error.RuntimeUnavailable.Key() || queued.LeadExecutionID != "" {
				t.Fatalf("未接纳输入未收敛到失败: %+v found=%t error=%v", queued, found, err)
			}
			admitted, err := s.config.Store.Inbox(ctx, constants.IMInboxStateAccepted, 10)
			if err != nil || len(admitted) != 1 || admitted[0].ID != input.ID || admitted[0].LinkID != link.ID || admitted[0].ExecutionID != "" {
				t.Fatalf("失败改变了原收件接纳事实: %+v %v", admitted, err)
			}
			receipts, count, err := s.config.Store.Deliveries(ctx, constants.IMDeliveryStateUnspecified, 10, 0)
			if err != nil || revoked && count != 0 || !revoked && (count != 1 || receipts[0].LinkID != link.ID || receipts[0].ExecutionID != "" ||
				!strings.Contains(receipts[0].Content, status_error.RuntimeUnavailable.Key())) {
				t.Fatalf("原 IM 会话未收到唯一授权失败回执: %+v count=%d error=%v", receipts, count, err)
			}
			if len(platform.ids) != 0 {
				t.Fatal("团队队列恢复绕过 Outbox 发送了平台消息")
			}
		})
	}
}

type unavailableIMLeadExecution struct {
	// Service 补齐本用例不调用的执行接口。
	execution.Service
	// ChatGroupPreparer 补齐本用例不调用的首发分组准备接口。
	execution.ChatGroupPreparer
}

// Start 模拟原负责人配置已不可用且未接纳 Chat 输入。
func (*unavailableIMLeadExecution) Start(context.Context, execution.WorkIntent) (execution.AcceptedExecution, error) {
	return execution.AcceptedExecution{}, status_error.RuntimeUnavailable
}

func (*unavailableIMLeadExecution) IsSubmissionStopped(string, string) bool { return false }

// imRecoveryQueuedResult 替代团队派发，队列接纳由真实 SQLite 保持。
type imRecoveryQueuedResult struct {
	// Service 提供用例中未调用的团队端口。
	channel.Service
	// submitError 表示读取团队信息或派发负责人队列时的错误。
	submitError error
}

func (s imRecoveryQueuedResult) SubmitMessage(_ context.Context, input channel.SubmitMessageInput) (channel.SubmitMessageResult, error) {
	if s.submitError != nil {
		return channel.SubmitMessageResult{}, s.submitError
	}
	return channel.SubmitMessageResult{Message: &channel.SendMessageResult{
		QueuedInput: &channel.QueuedLeadInput{ID: channel.LeadInputID(input.Continue.ThreadID, input.Continue.Input.SubmissionID)},
		Execution:   &models.Execution{ID: "other-execution", SubmissionID: "other-queued-input"},
	}}, nil
}

// imRecoveryExecutions 模拟执行入口，用真实 Chat 日志保存与重放接纳回执。
type imRecoveryExecutions struct {
	// Service 提供用例中未调用的执行端口。
	execution.Service
	// chats 保存 IM 与执行共享的权威聊天事实。
	chats chat.Module
	// directory 是首次创建测试 Chat 时使用的受管数据目录。
	directory string
	// calls 记录到达执行入口的次数。
	calls int
	// commits 记录实际提交的新执行数。
	commits int
	// replayError 表示读取接纳回执后，执行入口无法加载回执所需的额外信息。
	replayError bool
}

func (e *imRecoveryExecutions) Start(ctx context.Context, intent execution.WorkIntent) (execution.AcceptedExecution, error) {
	e.calls++
	chatID := intent.Chat.ChatID
	if intent.Chat.Create != nil {
		chatID = (requestidempotency.Identity{ID: intent.ClientRequestID}).ResourceID("chat", intent.Chat.Create.WakerID)
	}
	receipt, err := e.chats.ReadSubmission(ctx, chatID, intent.Input.SubmissionID)
	if receipt != nil && e.replayError {
		return execution.AcceptedExecution{}, status_error.DatabaseError
	}
	var target models.Chat
	if hasCode(err, status_error.ChatNotFound) && intent.Chat.Create != nil {
		target, err = createAdmissionChat(ctx, e.chats, e.directory, chatID, intent.Chat.Create.WakerID)
	} else if err == nil {
		target, err = e.chats.LoadExecutionChat(ctx, chatID)
	}
	if err != nil {
		return execution.AcceptedExecution{}, err
	}
	if receipt != nil {
		return execution.AcceptedExecution{Chat: target, Execution: *receipt, Replayed: true}, nil
	}
	result := models.Execution{ID: intent.ExecutionID, ChatID: chatID, SubmissionID: intent.Input.SubmissionID, IMReplyLinkID: intent.Input.IMReplyLinkID, UserMessageID: "im-user-input"}
	message, err := json.Marshal(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Actor: intent.Input.Actor, SubmissionID: result.SubmissionID, Content: intent.Input.Content})
	if err != nil {
		return execution.AcceptedExecution{}, err
	}
	created, err := json.Marshal(eventlog.ExecutionCreatedData{UserMessageID: result.UserMessageID, SubmissionID: result.SubmissionID, IMReplyLinkID: result.IMReplyLinkID})
	if err != nil {
		return execution.AcceptedExecution{}, err
	}
	turn := chat.StartedTurn{Chat: target, Execution: result, Facts: []eventlog.AppendInput{
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: result.UserMessageID, Data: message},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: result.ID, Data: created},
	}}
	var accepted chat.AcceptedTurn
	if err := intent.AdmissionBoundary.Commit(ctx, func(ctx context.Context) error {
		var err error
		accepted, err = e.chats.CommitAcceptedTurn(ctx, turn)
		return err
	}); err != nil {
		return execution.AcceptedExecution{}, err
	}
	e.commits++
	return execution.AcceptedExecution{Chat: accepted.Chat, Execution: accepted.Execution}, nil
}
