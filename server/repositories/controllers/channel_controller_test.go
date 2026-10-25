package controllers

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestChannelControllerCRUDFilteringAndMemberReplacement(t *testing.T) {
	database := newTestDB(t)
	controller := NewChannelController(database)
	ctx := context.Background()
	leadID := "11111111111111111111111111111111"
	memberID := "22222222222222222222222222222222"
	replacementID := "33333333333333333333333333333333"
	firstID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	secondID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	first := models.Channel{
		ID:          firstID,
		Name:        "前后端交付团队",
		Description: "负责界面与接口交付",
		LeadWakerID: leadID,
		CreatedAt:   100,
		UpdatedAt:   100,
	}
	firstMembers := []models.ChannelMember{
		{ChannelID: firstID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: firstID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelDefault, Position: 1},
	}
	if err := controller.Create(ctx, first, firstMembers); err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	second := models.Channel{
		ID:          secondID,
		Name:        "研究团队",
		Description: "负责行业研究",
		LeadWakerID: memberID,
		CreatedAt:   200,
		UpdatedAt:   200,
	}
	if err := controller.Create(ctx, second, []models.ChannelMember{
		{ChannelID: secondID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: secondID, WakerID: replacementID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	if err := controller.Create(ctx, models.Channel{
		ID:          "cccccccccccccccccccccccccccccccc",
		Name:        first.Name,
		LeadWakerID: leadID,
	}, firstMembers); !errors.Is(err, status_error.ChannelNameConflict) {
		t.Fatalf("duplicate Create() error = %v, want ChannelNameConflict", err)
	}

	channels, err := controller.List(ctx, "接口", 10, 0)
	if err != nil || len(channels) != 1 || channels[0].ID != firstID {
		t.Fatalf("List(active query) = %+v, error = %v", channels, err)
	}
	count, err := controller.Count(ctx, "团队")
	if err != nil || count != 2 {
		t.Fatalf("Count(active query) = %d, error = %v", count, err)
	}
	exists, err := controller.NameExists(ctx, first.Name, firstID)
	if err != nil || exists {
		t.Fatalf("NameExists(excluding self) = %t, error = %v", exists, err)
	}
	exists, err = controller.NameExists(ctx, second.Name, firstID)
	if err != nil || !exists {
		t.Fatalf("NameExists(other channel) = %t, error = %v", exists, err)
	}

	first.Name = "全栈交付团队"
	first.Description = "更新后的团队说明"
	first.LeadWakerID = replacementID
	first.UpdatedAt = 300
	replacementMembers := []models.ChannelMember{
		{ChannelID: firstID, WakerID: replacementID, RuntimeID: "codex", Model: "gpt-5.6", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: firstID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelDefault, Position: 1},
	}
	if err := controller.Update(ctx, first, replacementMembers); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	stored, err := controller.Get(ctx, firstID)
	if err != nil || stored.Name != first.Name || stored.LeadWakerID != replacementID {
		t.Fatalf("Get(updated) = %+v, error = %v", stored, err)
	}
	members, err := controller.ListMembers(ctx, firstID)
	if err != nil || len(members) != 2 || members[0].WakerID != replacementID || members[1].WakerID != leadID {
		t.Fatalf("ListMembers(replaced) = %+v, error = %v", members, err)
	}

	page, err := controller.List(ctx, "", 1, 0)
	if err != nil || len(page) != 1 || page[0].ID != firstID {
		t.Fatalf("List(paginated) = %+v, error = %v", page, err)
	}
}

func TestChannelControllerMemberLookupAndRemoval(t *testing.T) {
	database := newTestDB(t)
	controller := NewChannelController(database)
	ctx := context.Background()
	channelID := "11111111111111111111111111111111"
	leadID := "22222222222222222222222222222222"
	memberID := "33333333333333333333333333333333"
	orphanID := "44444444444444444444444444444444"
	if err := controller.Create(ctx, models.Channel{
		ID: channelID, Name: "成员查询团队", LeadWakerID: leadID,
	}, []models.ChannelMember{
		{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: channelID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
		{ChannelID: channelID, WakerID: orphanID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 2},
	}); err != nil {
		t.Fatalf("Create(channel) error = %v", err)
	}
	channelIDs, err := controller.ListIDsByMemberWaker(ctx, memberID)
	if err != nil || len(channelIDs) != 1 || channelIDs[0] != channelID {
		t.Fatalf("ListIDsByMemberWaker() = %+v, error = %v", channelIDs, err)
	}
	if err := controller.RemoveMember(ctx, channelID, memberID); err != nil {
		t.Fatalf("RemoveMember() error = %v", err)
	}
	members, err := controller.ListMembers(ctx, channelID)
	if err != nil || len(members) != 2 {
		t.Fatalf("ListMembers() = %+v, error = %v", members, err)
	}
	if err := controller.UpdateLead(ctx, channelID, orphanID, 300); err != nil {
		t.Fatalf("UpdateLead() error = %v", err)
	}
	stored, err := controller.Get(ctx, channelID)
	if err != nil || stored.LeadWakerID != orphanID {
		t.Fatalf("Get(updated lead) = %+v, error = %v", stored, err)
	}
}

func TestChannelControllerCountsEveryActiveWorkSource(t *testing.T) {
	database := newTestDB(t)
	channels := NewChannelController(database)
	threads := NewChannelThreadController(database)
	tasks := NewChannelTaskController(database)
	ctx := context.Background()
	channelID := "11111111111111111111111111111111"
	threadID := "22222222222222222222222222222222"
	leadID := "33333333333333333333333333333333"
	memberID := "44444444444444444444444444444444"
	leadChatID := "55555555555555555555555555555555"
	memberChatID := "66666666666666666666666666666666"
	if err := channels.Create(ctx, models.Channel{
		ID: channelID, Name: "忙碌团队", LeadWakerID: leadID,
	}, []models.ChannelMember{
		{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: channelID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("Create(channel) error = %v", err)
	}
	readyAt := int64(1)
	if err := threads.Create(ctx, models.ChannelThread{
		ID: threadID, ChannelID: channelID, Title: "执行中", LeadWakerID: leadID,
		LeadChatID: leadChatID, WorkspaceDirectory: "/tmp/team", AdditionalWorkingDirectories: []string{},
		MaxParallel: 3, MaxTasks: 12, MaxFailureAttempts: 2, ReadyAt: &readyAt,
	}, []models.ChannelThreadMember{
		{ThreadID: threadID, WakerID: leadID, ChatID: leadChatID, IsLead: true, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ThreadID: threadID, WakerID: memberID, ChatID: memberChatID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("Create(thread) error = %v", err)
	}
	for _, chat := range []models.Chat{
		{ID: leadChatID, WakerID: leadID, Title: "协调", WorkspaceDirectory: "/tmp/team", AdditionalWorkingDirectories: []string{}, ActiveExecutionID: "lead-execution"},
		{ID: memberChatID, WakerID: memberID, Title: "成员", WorkspaceDirectory: "/tmp/team", AdditionalWorkingDirectories: []string{}, ActiveExecutionID: "member-execution"},
	} {
		if err := database.Create(&chat).Error; err != nil {
			t.Fatalf("create chat %s: %v", chat.ID, err)
		}
	}
	if err := tasks.Create(ctx, models.ChannelTask{
		ID: "88888888888888888888888888888888", ThreadID: threadID, AssigneeWakerID: memberID,
		Subject: "待执行任务", Description: "验证任务计数", Status: constants.ChannelTaskStatusPending, ChangedPaths: []string{},
	}, nil); err != nil {
		t.Fatalf("Create(task) error = %v", err)
	}
	if err := threads.CreateLeadInput(ctx, models.ChannelLeadInput{
		ID: "99999999999999999999999999999999", ThreadID: threadID,
		Payload: workinput.Input{Actor: string(constants.ChannelMessageActorUser), Content: "补充原任务"},
		Status:  constants.ChannelInputStatusPending,
	}); err != nil {
		t.Fatalf("CreateLeadInput() error = %v", err)
	}
	count, err := channels.CountActiveWork(ctx, channelID)
	if err != nil || count != 4 {
		t.Fatalf("CountActiveWork() = %d, error = %v, want 4", count, err)
	}
	summaries, err := channels.ListActivitySummaries(ctx, []string{channelID})
	if err != nil ||
		summaries[channelID].ChannelActivityState() != constants.ChannelActivityStateRunning {
		t.Fatalf("ListActivitySummaries(running) = %+v, error = %v", summaries[channelID], err)
	}
	if summaries[channelID].RunningCount != 1 {
		t.Fatalf("running count = %d, want 1", summaries[channelID].RunningCount)
	}
	if err := database.Model(&models.Chat{}).
		Where("id = ?", memberChatID).
		Update("waiting_approval", true).Error; err != nil {
		t.Fatalf("mark waiting approval: %v", err)
	}
	summaries, err = channels.ListActivitySummaries(ctx, []string{channelID})
	if err != nil ||
		summaries[channelID].ChannelActivityState() != constants.ChannelActivityStateWaiting {
		t.Fatalf("ListActivitySummaries(waiting) = %+v, error = %v", summaries[channelID], err)
	}
	if summaries[channelID].WaitingCount != 1 {
		t.Fatalf("waiting count = %d, want 1", summaries[channelID].WaitingCount)
	}
}
