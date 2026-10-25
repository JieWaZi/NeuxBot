package channel

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// interactionChatFake 提供 Team 交互聚合测试所需的 Chat 摘要和事件。
type interactionChatFake struct {
	// Service 补齐本测试不使用的 Chat 接口方法。
	chatservice.Service
	// summaries 按 Chat 标识保存当前未决交互。
	summaries map[string]chatservice.ChatSummary
	// events 按 Chat 标识保存有序公开事件。
	events map[string][]chatservice.PublicEvent
}

// GetChatSummary 返回固定 Chat 摘要。
func (f *interactionChatFake) GetChatSummary(
	_ context.Context,
	chatID string,
) (chatservice.ChatSummary, error) {
	return f.summaries[chatID], nil
}

// ListEvents 返回 after 序号之后的一页事件。
func (f *interactionChatFake) ListEvents(
	_ context.Context,
	chatID string,
	after int64,
	limit int,
) ([]chatservice.PublicEvent, error) {
	result := make([]chatservice.PublicEvent, 0, limit)
	for _, event := range f.events[chatID] {
		if event.Sequence <= after {
			continue
		}
		result = append(result, event)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func TestTeamInteractionsAggregateAllMembersAndMirrorIdempotently(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "interactions.db"), Migrate: true,
	})
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get SQL database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	threads := controllers.NewChannelThreadController(database.DB)
	thread := models.ChannelThread{
		ID: "11111111111111111111111111111111", ChannelID: "22222222222222222222222222222222",
		Title: "交互聚合", LeadWakerID: "lead", LeadChatID: "lead-chat",
		WorkspaceDirectory: t.TempDir(), MaxParallel: 2, MaxTasks: 4, MaxFailureAttempts: 2,
	}
	if err := threads.Create(context.Background(), thread, []models.ChannelThreadMember{
		{ThreadID: thread.ID, WakerID: "lead", ChatID: "lead-chat", IsLead: true, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelDefault},
		{ThreadID: thread.ID, WakerID: "member", ChatID: "member-chat", RuntimeID: "claude", PermissionLevel: constants.PermissionLevelDefault, Position: 1},
	}); err != nil {
		t.Fatalf("create thread: %v", err)
	}
	approval := models.PendingApproval{
		ApprovalID: "approval", ToolCallID: "tool", Title: "运行命令",
		Options:     []models.PendingApprovalOption{{ID: "allow", Name: "允许"}},
		RequestedAt: 100, RequestedSequence: 3,
	}
	chatFake := &interactionChatFake{
		summaries: map[string]chatservice.ChatSummary{
			"lead-chat": {Chat: models.Chat{ID: "lead-chat"}},
			"member-chat": {Chat: models.Chat{
				ID: "member-chat", ActiveExecutionID: "execution", PendingApprovals: []models.PendingApproval{approval},
			}},
		},
		events: map[string][]chatservice.PublicEvent{
			"member-chat": {
				{
					Sequence: 3, RecordedAt: 100, Type: constants.ChatEventTypeApprovalRequested,
					ExecutionID: "execution", ApprovalRequest: &chatservice.PublicApprovalRequest{
						ApprovalID: "approval", ToolCallID: "tool", Title: "运行命令",
					},
				},
				{
					Sequence: 4, RecordedAt: 110, Type: constants.ChatEventTypeApprovalResolved,
					ExecutionID: "execution", ApprovalResolution: &chatservice.PublicApprovalResolution{
						ApprovalID: "approval", ToolCallID: "tool", Result: constants.ApprovalResultSelected,
					},
				},
			},
		},
	}
	service := &service{
		threads: threads, chats: chatFake, interactionCursors: make(map[string]int64),
	}
	interactions, activities, err := service.ListPendingInteractions(context.Background(), thread.ID)
	if err != nil {
		t.Fatalf("ListPendingInteractions() error = %v", err)
	}
	if len(interactions) != 1 || interactions[0].ChatID != "member-chat" ||
		interactions[0].CreatedSequence != 3 || interactions[0].ExecutionID != "execution" {
		t.Fatalf("pending interactions = %+v", interactions)
	}
	if len(activities) != 2 || activities[1].State != "requires_action" {
		t.Fatalf("member activities = %+v", activities)
	}
	if err := service.SyncInteractionEvents(context.Background(), thread.ID); err != nil {
		t.Fatalf("SyncInteractionEvents() error = %v", err)
	}
	service.interactionCursors = make(map[string]int64)
	if err := service.SyncInteractionEvents(context.Background(), thread.ID); err != nil {
		t.Fatalf("replayed SyncInteractionEvents() error = %v", err)
	}
	mirrored, err := threads.ListEvents(context.Background(), thread.ID, "", 20)
	if err != nil || len(mirrored) != 2 || mirrored[0].Type != constants.ChannelEventApprovalRequested ||
		mirrored[1].Type != constants.ChannelEventApprovalResolved {
		t.Fatalf("mirrored events = %+v, error = %v", mirrored, err)
	}
}
