package channel

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	channeltool "github.com/JieWaZi/NeuxBot/server/internal/teamcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

func TestStartPublishesAdmittedInitializingThread(t *testing.T) {
	for _, origin := range []string{"desktop", "im", "automatic", "im-live-reconciliation"} {
		t.Run(origin, func(t *testing.T) {
			ctx := t.Context()
			fixture := newDeletionFixture(t)
			wakers := controllers.NewWakerController(fixture.db)
			createChannelTestWakers(t, ctx, wakers, "lead", "member")
			team := models.Channel{ID: "team", Name: "恢复首发", LeadWakerID: "lead"}
			if err := fixture.channels.Create(ctx, team, []models.ChannelMember{
				{ChannelID: team.ID, WakerID: "lead", RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelDefault},
				{ChannelID: team.ID, WakerID: "member", RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelDefault, Position: 1},
			}); err != nil {
				t.Fatal(err)
			}
			input := CreateThreadInput{
				ChannelID: team.ID, ClientRequestID: "first-request",
				Input: execution.WorkInput{Actor: "user", Content: "处理这份材料", SubmissionID: "first-request"},
			}
			switch origin {
			case "im", "im-live-reconciliation":
				input.Input.SubmissionID, input.Input.IMReplyLinkID = "inbox-message", input.ClientRequestID
				input.MentionWakerID = "member"
			case "automatic":
				input.AutomaticRunID = input.ClientRequestID
				input.ExecutionID = util.New()
				input.Input.Actor, input.Input.SubmissionID = constants.AutomaticTaskActor, ""
			}
			identity, err := createThreadRequestIdentity(input)
			if err != nil {
				t.Fatal(err)
			}
			thread := models.ChannelThread{
				ID: util.New(), ChannelID: team.ID, LeadWakerID: "lead", LeadChatID: util.New(), ProvisioningID: util.New(),
				ClientRequestID: &identity.ID, ClientRequestHash: identity.Hash, Title: input.Input.Content,
				AutomaticRunID: input.AutomaticRunID, MaxParallel: 2, MaxTasks: 12,
			}
			memberChatID := util.New()
			root := t.TempDir()
			layout, err := chatstorage.NewLayout(root)
			if err != nil {
				t.Fatal(err)
			}
			teamWorkspace, err := layout.TeamWorkspace(thread.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := workspace.EnsureThreadExecutionWorkspace(teamWorkspace, thread.ID, "lead"); err != nil {
				t.Fatal(err)
			}
			members := []models.ChannelThreadMember{
				{ThreadID: thread.ID, WakerID: "lead", ChatID: thread.LeadChatID, IsLead: true, RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelDefault},
				{ThreadID: thread.ID, WakerID: "member", ChatID: memberChatID, Position: 1, RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelDefault},
			}
			prepared := make([]chatservice.DormantChat, 0, len(members))
			for _, member := range members {
				cwd, err := layout.TeamMemberWorkspace(thread.ID, member.WakerID, member.IsLead)
				if err != nil {
					t.Fatal(err)
				}
				if err := workspace.EnsureThreadMemberExecutionWorkspace(cwd, thread.ID, member.ChatID, member.WakerID); err != nil {
					t.Fatal(err)
				}
				prepared = append(prepared, chatservice.DormantChat{ThreadWorkspaceID: thread.ID, Chat: models.Chat{
					ID: member.ChatID, WakerID: member.WakerID, Title: thread.Title, WorkspaceDirectory: cwd,
					AdditionalWorkingDirectories: []string{teamWorkspace}, HiddenFromWakerHistory: true,
				}})
			}
			thread.WorkspaceDirectory = prepared[0].Chat.WorkspaceDirectory
			thread.AdditionalWorkingDirectories = prepared[0].Chat.AdditionalWorkingDirectories
			if input.MentionWakerID != "" {
				thread.TaskCount = 1
			}
			if err := fixture.threads.Create(ctx, thread, members); err != nil {
				t.Fatal(err)
			}
			tasks := controllers.NewChannelTaskController(fixture.db)
			if input.MentionWakerID != "" {
				if err := tasks.Create(ctx, models.ChannelTask{ID: "first-task", ThreadID: thread.ID,
					AssigneeWakerID: input.MentionWakerID, Subject: thread.Title, Description: input.Input.Content,
					DirectInput: &input.Input, DirectDelivery: true, IMReplyLinkID: input.Input.IMReplyLinkID, Status: constants.ChannelTaskStatusPending,
				}, nil); err != nil {
					t.Fatal(err)
				}
			}

			chatConfig := chatservice.Config{StorageDirectory: root, Projects: controllers.NewProjectController(fixture.db),
				QueryIndexes: chatservice.QueryIndexDependencies{
					Chats: controllers.NewChatController(fixture.db), Reading: controllers.NewChatReadingController(fixture.db),
					Transactions: fixture.service.transactions, DeletionIntents: controllers.NewChatDeletionIntentController(fixture.db),
				},
			}
			journal, err := chatservice.New(chatConfig)
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			if _, err := journal.CreateDormantChats(ctx, prepared); err != nil {
				t.Fatal(err)
			}
			executionID := input.ExecutionID
			if executionID == "" {
				executionID = util.New()
			}
			marshal := func(value any) json.RawMessage {
				t.Helper()
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				return data
			}
			created := eventlog.ExecutionCreatedData{UserMessageID: "first-message", RuntimeID: constants.RuntimeIDCodex,
				SubmissionID: input.Input.SubmissionID, IMReplyLinkID: input.Input.IMReplyLinkID, AutomaticRunID: input.AutomaticRunID}
			// 模拟权威日志已经接纳，进程在团队发布事务前退出。
			if _, err := journal.CommitAcceptedTurn(ctx, chatservice.StartedTurn{
				Chat: prepared[0].Chat, Execution: models.Execution{ID: executionID, ChatID: thread.LeadChatID, UserMessageID: created.UserMessageID, Status: constants.ExecutionStatusPending},
				Facts: []eventlog.AppendInput{
					{Type: constants.ChatEventTypeUserMessageCreated, MessageID: created.UserMessageID,
						Data: marshal(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Actor: input.Input.Actor, Content: input.Input.Content})},
					{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: executionID, Data: marshal(created)},
				},
			}); err != nil {
				t.Fatal(err)
			}
			journal.Close()
			restarted, err := chatservice.New(chatConfig)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(restarted.Close)
			if err := restarted.Start(ctx); err != nil {
				t.Fatal(err)
			}
			before, err := restarted.ReadExecutionOutcome(ctx, thread.LeadChatID, executionID)
			if err != nil || before.Execution.Status != constants.ExecutionStatusFailed {
				t.Fatalf("Chat 重启未记录原首轮中断：%+v %v", before, err)
			}
			captured := &postTurnExecutionFake{}
			sut := fixture.service
			sut.tasks, sut.wakers, sut.chats, sut.executions = tasks, wakers, restarted, captured
			sut.capabilities, sut.watchers = channeltool.NewRegistry(), make(map[string]struct{})
			if origin == "im-live-reconciliation" {
				sut.lifecycleContext, sut.lifecycleCancel = context.WithCancel(ctx)
				err = sut.ReconcileThread(ctx, thread.ID)
			} else {
				err = sut.Start(ctx)
			}
			t.Cleanup(sut.Close)
			if err != nil {
				t.Fatal(err)
			}
			ready, found, err := fixture.threads.GetByClientRequestID(ctx, team.ID, input.ClientRequestID)
			if err != nil || !found || ready.ID != thread.ID || ready.ReadyAt == nil {
				t.Fatalf("团队未发布原已接纳首发：%+v %v", ready, err)
			}
			replay, err := sut.SubmitMessage(ctx, SubmitMessageInput{Create: &input})
			if err != nil || replay.Thread == nil || !replay.Thread.Replayed || replay.Thread.Thread.ID != thread.ID {
				t.Fatalf("同键首发未返回原团队对话：%+v %v", replay, err)
			}
			after, err := restarted.ReadExecutionOutcome(ctx, thread.LeadChatID, executionID)
			if err != nil || after.Execution.ID != executionID || after.Execution.AutomaticRunID != input.AutomaticRunID || after.Execution.IMReplyLinkID != input.Input.IMReplyLinkID || after.Execution.Status != before.Execution.Status {
				t.Fatalf("团队发布改写了原执行：%+v %v", after, err)
			}
			if input.Input.SubmissionID != "" {
				receipt, err := restarted.ReadSubmission(ctx, thread.LeadChatID, input.Input.SubmissionID)
				if err != nil || receipt == nil || receipt.ID != executionID {
					t.Fatalf("原输入收据丢失：%+v %v", receipt, err)
				}
			}
			if captured.cancelCount() != 0 || captured.startCount() != thread.TaskCount || (thread.TaskCount == 1 && captured.firstStart().Chat.ChatID != memberChatID) {
				t.Fatalf("首发恢复重启或停止了已有负责人执行：starts=%d cancels=%d", captured.startCount(), captured.cancelCount())
			}
			events, err := fixture.threads.ListEvents(ctx, thread.ID, "", 20)
			if err != nil {
				t.Fatal(err)
			}
			var threadCreated, taskCreated int
			for _, event := range events {
				switch event.Type {
				case constants.ChannelEventThreadCreated:
					threadCreated++
				case constants.ChannelEventTaskCreated:
					taskCreated++
				}
			}
			if threadCreated != 1 || taskCreated != thread.TaskCount {
				t.Fatalf("首发事件未随真实发布写入一次：%+v", events)
			}
		})
	}
}

func TestSameKeyRetriesReclaimUnadmittedThread(t *testing.T) {
	ctx := t.Context()
	fixture := newDeletionFixture(t)
	sut := fixture.service
	sut.wakers = controllers.NewWakerController(fixture.db)
	createChannelTestWakers(t, ctx, sut.wakers, "lead", "member")
	if err := fixture.channels.Create(ctx, models.Channel{ID: "team", Name: "首发重试", LeadWakerID: "lead"}, []models.ChannelMember{
		{ChannelID: "team", WakerID: "lead", RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelDefault},
		{ChannelID: "team", WakerID: "member", RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelDefault},
	}); err != nil {
		t.Fatal(err)
	}
	input := CreateThreadInput{ChannelID: "team", ClientRequestID: "original-request", Input: execution.WorkInput{Actor: "user", Content: "检查工作目录"}}
	identity, err := createThreadRequestIdentity(input)
	if err != nil {
		t.Fatal(err)
	}
	abandoned := models.ChannelThread{ID: "abandoned-thread", ChannelID: input.ChannelID, ClientRequestID: &identity.ID,
		ClientRequestHash: identity.Hash, ProvisioningID: "abandoned-provisioning", LeadWakerID: "lead", LeadChatID: "abandoned-lead"}
	if err := fixture.threads.Create(ctx, abandoned, []models.ChannelThreadMember{
		{ThreadID: abandoned.ID, WakerID: "lead", ChatID: abandoned.LeadChatID, IsLead: true},
		{ThreadID: abandoned.ID, WakerID: "member", ChatID: "abandoned-member"},
	}); err != nil {
		t.Fatal(err)
	}
	chats := &postTurnChatFake{summaries: make(map[string]chatservice.ChatSummary)}
	provisional := &stopChatFake{Service: chats}
	captured := &postTurnExecutionFake{}
	sut.chats, sut.executions, sut.chatGroups = provisional, captured, &leaderTurnChatGroupFake{chats: chats}
	sut.tasks, sut.capabilities = controllers.NewChannelTaskController(fixture.db), channeltool.NewRegistry()
	t.Cleanup(sut.capabilities.Close)
	if err := sut.ReconcileThread(ctx, abandoned.ID); !errors.Is(err, status_error.CreateRequestInProgress) || provisional.provisionalThreadID != "" {
		t.Errorf("普通对账仅应等待无接纳回执的首发：%v", err)
	}
	result, err := sut.SubmitMessage(ctx, SubmitMessageInput{Create: &input})
	if err != nil || result.Thread == nil || result.Thread.Thread.ID == abandoned.ID || result.Thread.Thread.ReadyAt == nil || captured.startCount() != 1 {
		t.Fatalf("同键重试没有回收未接纳计划后完成首轮：%+v %v", result, err)
	}
	if _, err := fixture.threads.Get(ctx, abandoned.ID); !hasStatusErrorCode(err, status_error.ChannelThreadNotFound.Code()) || provisional.provisionalThreadID != abandoned.ID {
		t.Fatalf("原首发计划未被清理：%v", err)
	}
}
