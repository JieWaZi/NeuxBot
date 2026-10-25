package channel

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

var errRepairOrphanMembership = errors.New("repair orphan membership")

func TestDetachWakerRemovesMemberAndReassignsLead(t *testing.T) {
	t.Parallel()
	fixture := newWakerDetachFixture(t)
	ctx := context.Background()
	channelID := "11111111111111111111111111111111"
	leadID := "22222222222222222222222222222222"
	memberID := "33333333333333333333333333333333"
	otherID := "44444444444444444444444444444444"
	seedWakers(t, fixture, leadID, memberID, otherID)
	if err := fixture.channels.Create(ctx, models.Channel{
		ID: channelID, Name: "交付团队", LeadWakerID: leadID,
	}, []models.ChannelMember{
		{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: channelID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
		{ChannelID: channelID, WakerID: otherID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 2},
	}); err != nil {
		t.Fatalf("Create(channel) error = %v", err)
	}

	if err := fixture.service.DetachWaker(ctx, leadID); err != nil {
		t.Fatalf("DetachWaker() error = %v", err)
	}
	channel, err := fixture.channels.Get(ctx, channelID)
	if err != nil {
		t.Fatalf("Get(channel) error = %v", err)
	}
	if channel.LeadWakerID != memberID {
		t.Fatalf("LeadWakerID = %q, want %q", channel.LeadWakerID, memberID)
	}
	members, err := fixture.channels.ListMembers(ctx, channelID)
	if err != nil || len(members) != 2 {
		t.Fatalf("ListMembers() = %+v, error = %v", members, err)
	}
	for _, member := range members {
		if member.WakerID == leadID {
			t.Fatalf("removed waker still in channel members: %+v", members)
		}
	}
}

func TestDetachWakerDeletesTeamWhenOnlyOneMemberRemains(t *testing.T) {
	t.Parallel()
	fixture := newWakerDetachFixture(t)
	ctx := context.Background()
	channelID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	leadID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	memberID := "cccccccccccccccccccccccccccccccc"
	seedWakers(t, fixture, leadID, memberID)
	if err := fixture.channels.Create(ctx, models.Channel{
		ID: channelID, Name: "两人团队", LeadWakerID: leadID,
	}, []models.ChannelMember{
		{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: channelID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("Create(channel) error = %v", err)
	}

	if err := fixture.service.DetachWaker(ctx, memberID); err != nil {
		t.Fatalf("DetachWaker() error = %v", err)
	}
	if _, err := fixture.channels.Get(ctx, channelID); !hasStatusErrorCode(
		err,
		status_error.ChannelNotFound.Code(),
	) {
		t.Fatalf("Get(deleted channel) error = %v, want ChannelNotFound", err)
	}
}

func TestDetachWakerKeepsMembershipWhenActiveWorkBlocksTeamDeletion(t *testing.T) {
	t.Parallel()
	fixture := newWakerDetachFixture(t)
	ctx := context.Background()
	channelID := "abababababababababababababababab"
	threadID := "bcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbc"
	leadID := "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
	memberID := "dddd1111111111111111111111111111"
	seedWakers(t, fixture, leadID, memberID)
	if err := fixture.channels.Create(ctx, models.Channel{
		ID: channelID, Name: "活跃团队", LeadWakerID: leadID,
	}, []models.ChannelMember{
		{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: channelID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("Create(channel) error = %v", err)
	}
	readyAt := int64(1)
	if err := fixture.service.threads.Create(ctx, models.ChannelThread{
		ID: threadID, ChannelID: channelID, Title: "活跃会话", LeadWakerID: leadID,
		LeadChatID: "efefefefefefefefefefefefefefefef", WorkspaceDirectory: "/tmp/team",
		AdditionalWorkingDirectories: []string{}, MaxParallel: 1, MaxTasks: 2,
		MaxFailureAttempts: 1, ReadyAt: &readyAt,
	}, []models.ChannelThreadMember{
		{ThreadID: threadID, WakerID: leadID, ChatID: "efefefefefefefefefefefefefefefef", IsLead: true, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ThreadID: threadID, WakerID: memberID, ChatID: "fafafafafafafafafafafafafafafafa", RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("Create(thread) error = %v", err)
	}
	if err := fixture.db.Create(&models.ChannelTask{
		ID: "01010101010101010101010101010101", ThreadID: threadID,
		AssigneeWakerID: memberID, Subject: "运行中", Description: "阻止删除",
		Status: constants.ChannelTaskStatusPending, ChangedPaths: []string{},
	}).Error; err != nil {
		t.Fatalf("Create(task) error = %v", err)
	}

	if err := fixture.service.DetachWaker(ctx, memberID); !errors.Is(
		err,
		status_error.ChannelHasActiveWork,
	) {
		t.Fatalf("DetachWaker(active) error = %v, want ChannelHasActiveWork", err)
	}
	members, err := fixture.channels.ListMembers(ctx, channelID)
	if err != nil || len(members) != 2 {
		t.Fatalf("ListMembers() = %+v, error = %v", members, err)
	}
}

func TestRepairOrphanMembershipRemovesMissingWakerReferences(t *testing.T) {
	t.Parallel()
	fixture := newWakerDetachFixture(t)
	ctx := context.Background()
	channelID := "dddddddddddddddddddddddddddddddd"
	leadID := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	memberID := "ffffffffffffffffffffffffffffffff"
	orphanID := "99999999999999999999999999999999"
	seedWakers(t, fixture, leadID, memberID)
	if err := fixture.channels.Create(ctx, models.Channel{
		ID: channelID, Name: "脏数据团队", LeadWakerID: leadID,
	}, []models.ChannelMember{
		{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: channelID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
		{ChannelID: channelID, WakerID: orphanID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 2},
	}); err != nil {
		t.Fatalf("Create(channel) error = %v", err)
	}

	if err := fixture.service.repairOrphanMembership(ctx); err != nil {
		t.Fatalf("repairOrphanMembership() error = %v", err)
	}
	members, err := fixture.channels.ListMembers(ctx, channelID)
	if err != nil || len(members) != 2 {
		t.Fatalf("ListMembers() = %+v, error = %v", members, err)
	}
	for _, member := range members {
		if member.WakerID == orphanID {
			t.Fatalf("orphan member still present: %+v", members)
		}
	}
}

func TestRepairOrphanMembershipReturnsDetachFailure(t *testing.T) {
	t.Parallel()
	controller := &failingOrphanChannelController{}
	service := &service{channels: controller}

	err := service.repairOrphanMembership(context.Background())
	if !errors.Is(err, errRepairOrphanMembership) {
		t.Fatalf("repairOrphanMembership() error = %v, want %v", err, errRepairOrphanMembership)
	}
	if controller.deleteCalls != 0 {
		t.Fatalf("Delete() calls = %d, want 0", controller.deleteCalls)
	}
}

// failingOrphanChannelController 为启动修复注入稳定的孤儿成员清理失败。
type failingOrphanChannelController struct {
	controllers.ChannelController
	// deleteCalls 记录修复故障后是否错误扩大为团队删除。
	deleteCalls int
}

// ListOrphanMemberWakerIDs 返回需要修复的孤儿成员。
func (c *failingOrphanChannelController) ListOrphanMemberWakerIDs(context.Context) ([]string, error) {
	return []string{"orphan-waker"}, nil
}

// ListIDsByMemberWaker 返回包含孤儿成员的团队。
func (c *failingOrphanChannelController) ListIDsByMemberWaker(context.Context, string) ([]string, error) {
	return []string{"channel-with-orphan"}, nil
}

// Get 注入团队修复查询失败。
func (c *failingOrphanChannelController) Get(context.Context, string) (models.Channel, error) {
	return models.Channel{}, errRepairOrphanMembership
}

// Delete 记录修复错误是否被错误转换为团队删除。
func (c *failingOrphanChannelController) Delete(context.Context, string) error {
	c.deleteCalls++
	return nil
}

// ListIDsWithOrphanLead 返回空的孤儿负责人集合。
func (c *failingOrphanChannelController) ListIDsWithOrphanLead(context.Context) ([]string, error) {
	return []string{}, nil
}

type wakerDetachFixture struct {
	// db 是用于构造团队活跃工作的临时数据库。
	db *gorm.DB
	// channels 提供团队配置的真实临时仓储。
	channels controllers.ChannelController
	// wakers 提供伙伴资料的真实临时仓储。
	wakers controllers.WakerController
	// service 是待验证的团队成员解绑服务。
	service *service
}

func newWakerDetachFixture(t *testing.T) wakerDetachFixture {
	t.Helper()
	deletionFixture := newDeletionFixture(t)
	return wakerDetachFixture{
		db:       deletionFixture.db,
		channels: deletionFixture.channels,
		wakers:   controllers.NewWakerController(deletionFixture.db),
		service: &service{
			channels:     deletionFixture.channels,
			threads:      deletionFixture.threads,
			transactions: deletionFixture.service.transactions,
			chats:        deletionFixture.chats,
			wakers:       controllers.NewWakerController(deletionFixture.db),
		},
	}
}

func seedWakers(t *testing.T, fixture wakerDetachFixture, wakerIDs ...string) {
	t.Helper()
	ctx := context.Background()
	for index, wakerID := range wakerIDs {
		if err := fixture.wakers.Create(ctx, models.Waker{
			ID:       wakerID,
			Name:     "伙伴-" + wakerID[:4],
			RoleName: "角色",
		}); err != nil {
			t.Fatalf("Create(waker %d) error = %v", index, err)
		}
	}
}
