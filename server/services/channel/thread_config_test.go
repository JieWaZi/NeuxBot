package channel

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestCreateThreadRequestIdentityIgnoresMemberOverrideOrder(t *testing.T) {
	first := CreateThreadInput{
		ChannelID:        "channel-1",
		ClientRequestID:  "request-1",
		WorkingDirectory: "/tmp/project",
		MemberConfigs: []MemberExecutionConfigInput{
			{WakerID: "member-b", RuntimeID: "claude", PermissionLevel: constants.PermissionLevelAuto},
			{WakerID: "member-a", RuntimeID: "codex", PermissionLevel: constants.PermissionLevelDefault},
		},
		Input: execution.WorkInput{Content: "实现功能"},
	}
	second := first
	second.ClientRequestID = "request-2"
	second.MemberConfigs = append([]MemberExecutionConfigInput(nil), first.MemberConfigs...)
	second.MemberConfigs[0], second.MemberConfigs[1] = second.MemberConfigs[1], second.MemberConfigs[0]
	for _, input := range []*CreateThreadInput{&first, &second} {
		configs, err := normalizeMemberExecutionConfigs(input.MemberConfigs)
		if err != nil {
			t.Fatalf("normalize configs: %v", err)
		}
		slices.SortFunc(configs, func(left MemberExecutionConfigInput, right MemberExecutionConfigInput) int {
			return strings.Compare(left.WakerID, right.WakerID)
		})
		input.MemberConfigs = configs
	}
	firstIdentity, err := createThreadRequestIdentity(first)
	if err != nil {
		t.Fatalf("hash first request: %v", err)
	}
	secondIdentity, err := createThreadRequestIdentity(second)
	if err != nil {
		t.Fatalf("hash second request: %v", err)
	}
	if firstIdentity.Hash != secondIdentity.Hash {
		t.Fatalf("equivalent request hashes differ: %q != %q", firstIdentity.Hash, secondIdentity.Hash)
	}
	second.MemberConfigs[0].AccountID = "selected-account"
	accountIdentity, err := createThreadRequestIdentity(second)
	if err != nil || accountIdentity.Hash == firstIdentity.Hash {
		t.Fatalf("account change must alter request identity: %v", err)
	}
	second.Input.Content = "实现另一个功能"
	changedIdentity, err := createThreadRequestIdentity(second)
	if err != nil {
		t.Fatalf("hash changed request: %v", err)
	}
	if changedIdentity.Hash == firstIdentity.Hash {
		t.Fatal("changed request reused the same hash")
	}
}

func TestReplayCreateThreadReturnsReadyAggregateAndRejectsAmbiguousRequests(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "channel-thread-replay.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create replay database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("open replay database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	threads := controllers.NewChannelThreadController(database.DB)
	tasks := controllers.NewChannelTaskController(database.DB)
	now := time.Now().UnixMilli()
	requestID := "request-1"
	thread := models.ChannelThread{
		ID:                           "thread-1",
		ChannelID:                    "channel-1",
		ClientRequestID:              &requestID,
		ClientRequestHash:            "hash-1",
		Title:                        "幂等首发",
		LeadWakerID:                  "lead",
		LeadChatID:                   "chat-lead",
		WorkspaceDirectory:           "/tmp/project",
		AdditionalWorkingDirectories: []string{},
		MaxParallel:                  3,
		MaxTasks:                     12,
		MaxFailureAttempts:           2,
		ReadyAt:                      &now,
		CreatedAt:                    now,
		UpdatedAt:                    now,
	}
	member := models.ChannelThreadMember{
		ThreadID:        thread.ID,
		WakerID:         "lead",
		ChatID:          thread.LeadChatID,
		IsLead:          true,
		RuntimeID:       constants.RuntimeIDCodex,
		PermissionLevel: constants.PermissionLevelAuto,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := threads.Create(context.Background(), thread, []models.ChannelThreadMember{member}); err != nil {
		t.Fatalf("create replay thread: %v", err)
	}
	task := models.ChannelTask{
		ID:              "task-1",
		ThreadID:        thread.ID,
		AssigneeWakerID: member.WakerID,
		Subject:         "实现功能",
		Description:     "完成首发任务",
		Status:          constants.ChannelTaskStatusPending,
		ChangedPaths:    []string{},
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := tasks.Create(context.Background(), task, nil); err != nil {
		t.Fatalf("create replay task: %v", err)
	}
	sut := &service{threads: threads, tasks: tasks}
	identity := requestidempotency.Identity{ID: requestID, Hash: thread.ClientRequestHash}
	result, err := sut.replayCreateThread(context.Background(), thread, identity)
	if err != nil {
		t.Fatalf("replay ready thread: %v", err)
	}
	if !result.Replayed || result.Thread.ID != thread.ID || len(result.Members) != 1 ||
		len(result.Tasks) != 1 || result.LeadExecution != nil {
		t.Fatalf("replayed aggregate = %+v", result)
	}
	conflicting := identity
	conflicting.Hash = "different"
	if _, err := sut.replayCreateThread(context.Background(), thread, conflicting); err == nil || err.Error() != status_error.CreateRequestConflict.Error() {
		t.Fatalf("conflicting replay error = %v", err)
	}
	thread.ReadyAt = nil
	if _, err := sut.replayCreateThread(context.Background(), thread, identity); err == nil || err.Error() != status_error.CreateRequestInProgress.Error() {
		t.Fatalf("initializing replay error = %v", err)
	}
}

func TestApplyThreadMemberConfigOverridesKeepsMembershipAndOrder(t *testing.T) {
	members := []models.ChannelMember{
		{
			WakerID:         "lead",
			RuntimeID:       "codex",
			PermissionLevel: constants.PermissionLevelAuto,
		},
		{
			WakerID:         "member",
			RuntimeID:       "codex",
			PermissionLevel: constants.PermissionLevelAuto,
			Position:        1,
		},
	}
	result, err := ApplyMemberConfigOverrides(
		members,
		[]MemberExecutionConfigInput{{
			WakerID:         "member",
			RuntimeID:       "claude",
			AccountID:       "anthropic-account",
			Model:           "sonnet",
			Reasoning:       "high",
			PermissionLevel: constants.PermissionLevelFullAccess,
		}},
	)
	if err != nil {
		t.Fatalf("apply overrides: %v", err)
	}
	if len(result) != 2 || result[0].WakerID != "lead" || result[1].WakerID != "member" {
		t.Fatalf("membership or order changed: %+v", result)
	}
	if result[1].RuntimeID != "claude" || result[1].AccountID != "anthropic-account" || result[1].Model != "sonnet" ||
		result[1].Reasoning != "high" ||
		result[1].PermissionLevel != constants.PermissionLevelFullAccess {
		t.Fatalf("override not applied: %+v", result[1])
	}
	if members[1].RuntimeID != "codex" {
		t.Fatalf("source defaults were mutated: %+v", members[1])
	}
}

func TestApplyThreadMemberConfigOverridesRejectsNonMember(t *testing.T) {
	_, err := ApplyMemberConfigOverrides(
		[]models.ChannelMember{{
			WakerID:         "lead",
			RuntimeID:       "codex",
			PermissionLevel: constants.PermissionLevelAuto,
		}},
		[]MemberExecutionConfigInput{{
			WakerID:         "outsider",
			RuntimeID:       "codex",
			PermissionLevel: constants.PermissionLevelAuto,
		}},
	)
	if err == nil || err.Error() != status_error.ChannelMemberNotFound.Error() {
		t.Fatalf("error = %v, want ChannelMemberNotFound", err)
	}
}
