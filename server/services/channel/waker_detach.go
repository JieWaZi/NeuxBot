package channel

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// wakerDetachmentPlan 保存一个团队在伙伴删除时需要原子提交的变更。
type wakerDetachmentPlan struct {
	// channelID 是需要更新或删除的团队标识。
	channelID string
	// chatIDs 是删除整个团队时需要一并删除的成员 Chat 标识。
	chatIDs []string
	// deleteChannel 表示移除无效成员后团队不足两人，需要整体删除。
	deleteChannel bool
	// newLeadWakerID 非空时表示需要更换的团队负责人。
	newLeadWakerID string
	// removedWakerIDs 是需要从保留团队配置中删除的无效成员标识。
	removedWakerIDs []string
}

// ValidateWakerDetachment 校验伙伴退出全部团队时不会被活跃工作阻止。
func (s *service) ValidateWakerDetachment(ctx context.Context, wakerID string) error {
	_, err := s.prepareWakerDetachment(ctx, wakerID)
	return err
}

// DetachWaker 从全部团队中移除指定伙伴，并在成员不足时级联删除团队。
func (s *service) DetachWaker(ctx context.Context, wakerID string) error {
	channelIDs, err := s.channels.ListIDsByMemberWaker(ctx, wakerID)
	if err != nil {
		return err
	}
	sort.Strings(channelIDs)
	for _, id := range channelIDs {
		locked, release, lockErr := teamwork.Acquire(ctx, id)
		if lockErr != nil {
			return lockErr
		}
		ctx = locked
		defer release()
	}
	plans, err := s.prepareWakerDetachment(ctx, wakerID)
	if err != nil {
		return err
	}
	chatIDs := []string{}
	for _, plan := range plans {
		chatIDs = append(chatIDs, plan.chatIDs...)
	}
	updatedAt := time.Now().UnixMilli()
	return s.chats.DeleteChatsAtomically(
		ctx,
		chatIDs,
		func(transactionContext context.Context) error {
			for _, plan := range plans {
				if plan.deleteChannel {
					if err := s.threads.DeleteByChannel(
						transactionContext,
						plan.channelID,
					); err != nil {
						return err
					}
					if err := s.channels.Delete(transactionContext, plan.channelID); err != nil {
						return err
					}
					continue
				}
				for _, removedWakerID := range plan.removedWakerIDs {
					if err := s.channels.RemoveMember(
						transactionContext,
						plan.channelID,
						removedWakerID,
					); err != nil {
						return err
					}
				}
				if plan.newLeadWakerID == "" {
					continue
				}
				if err := s.channels.UpdateLead(
					transactionContext,
					plan.channelID,
					plan.newLeadWakerID,
					updatedAt,
				); err != nil {
					return err
				}
			}
			return nil
		},
	)
}

func (s *service) prepareWakerDetachment(
	ctx context.Context,
	wakerID string,
) ([]wakerDetachmentPlan, error) {
	wakerID = strings.TrimSpace(wakerID)
	if wakerID == "" {
		return nil, status_error.BadRequest
	}
	channelIDs, err := s.channels.ListIDsByMemberWaker(ctx, wakerID)
	if err != nil {
		return nil, err
	}
	plans := make([]wakerDetachmentPlan, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		plan, planErr := s.prepareChannelWakerDetachment(ctx, channelID, wakerID)
		if planErr != nil {
			return nil, planErr
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func (s *service) prepareChannelWakerDetachment(
	ctx context.Context,
	channelID string,
	wakerID string,
) (wakerDetachmentPlan, error) {
	channel, err := s.channels.Get(ctx, channelID)
	if err != nil {
		return wakerDetachmentPlan{}, err
	}
	members, err := s.channels.ListMembers(ctx, channelID)
	if err != nil {
		return wakerDetachmentPlan{}, err
	}
	plan := wakerDetachmentPlan{
		channelID:       channelID,
		chatIDs:         []string{},
		removedWakerIDs: []string{},
	}
	remaining := make([]models.ChannelMember, 0, len(members))
	for _, member := range members {
		if member.WakerID == wakerID {
			plan.removedWakerIDs = append(plan.removedWakerIDs, member.WakerID)
			continue
		}
		if _, getErr := s.wakers.Get(ctx, member.WakerID); getErr != nil {
			if !hasStatusErrorCode(getErr, status_error.WakerNotFound.Code()) {
				return wakerDetachmentPlan{}, getErr
			}
			plan.removedWakerIDs = append(plan.removedWakerIDs, member.WakerID)
			continue
		}
		remaining = append(remaining, member)
	}
	if len(remaining) >= 2 {
		if channel.LeadWakerID == wakerID || containsWakerID(
			plan.removedWakerIDs,
			channel.LeadWakerID,
		) {
			plan.newLeadWakerID = remaining[0].WakerID
		}
		return plan, nil
	}
	active, err := s.channels.CountActiveWork(ctx, channelID)
	if err != nil {
		return wakerDetachmentPlan{}, err
	}
	if active > 0 {
		return wakerDetachmentPlan{}, status_error.ChannelHasActiveWork
	}
	threadIDs, err := s.threads.ListIDsByChannel(ctx, channelID)
	if err != nil {
		return wakerDetachmentPlan{}, err
	}
	for _, threadID := range threadIDs {
		ids, listErr := s.threads.ListChatIDs(ctx, threadID)
		if listErr != nil {
			return wakerDetachmentPlan{}, listErr
		}
		plan.chatIDs = append(plan.chatIDs, ids...)
	}
	plan.deleteChannel = true
	return plan, nil
}

func containsWakerID(wakerIDs []string, target string) bool {
	for _, wakerID := range wakerIDs {
		if wakerID == target {
			return true
		}
	}
	return false
}

// repairOrphanMembership 清理成员或负责人引用已不存在伙伴的脏数据。
func (s *service) repairOrphanMembership(ctx context.Context) error {
	orphanWakerIDs, err := s.channels.ListOrphanMemberWakerIDs(ctx)
	if err != nil {
		return err
	}
	for _, wakerID := range orphanWakerIDs {
		if err := s.detachOrphanWaker(ctx, wakerID); err != nil {
			return err
		}
	}
	orphanLeadChannelIDs, err := s.channels.ListIDsWithOrphanLead(ctx)
	if err != nil {
		return err
	}
	for _, channelID := range orphanLeadChannelIDs {
		if err := s.repairOrphanLead(ctx, channelID); err != nil {
			return err
		}
	}
	return nil
}

// detachOrphanWaker 在启动修复时移除已不存在伙伴的成员引用，不因活跃工作阻塞启动。
func (s *service) detachOrphanWaker(ctx context.Context, wakerID string) error {
	channelIDs, err := s.channels.ListIDsByMemberWaker(ctx, wakerID)
	if err != nil {
		return err
	}
	for _, channelID := range channelIDs {
		if err := s.reconcileChannelAfterOrphanRemoval(ctx, channelID); err != nil {
			return err
		}
	}
	return nil
}

// repairOrphanLead 为负责人已不存在但仍保留有效成员的团队重新指定负责人。
func (s *service) repairOrphanLead(ctx context.Context, channelID string) error {
	channel, err := s.channels.Get(ctx, channelID)
	if err != nil {
		return err
	}
	if _, err := s.wakers.Get(ctx, channel.LeadWakerID); err == nil {
		return nil
	}
	members, err := s.listExistingMembers(ctx, channelID)
	if err != nil {
		return err
	}
	if len(members) < 2 {
		return s.deleteChannelIgnoringActiveWork(ctx, channelID)
	}
	return s.channels.UpdateLead(
		ctx,
		channelID,
		members[0].WakerID,
		time.Now().UnixMilli(),
	)
}

func (s *service) reconcileChannelAfterOrphanRemoval(
	ctx context.Context,
	channelID string,
) error {
	channel, err := s.channels.Get(ctx, channelID)
	if err != nil {
		return err
	}
	members, err := s.listExistingMembers(ctx, channelID)
	if err != nil {
		return err
	}
	if len(members) < 2 {
		return s.deleteChannelIgnoringActiveWork(ctx, channelID)
	}
	if containsChannelMember(members, channel.LeadWakerID) {
		return nil
	}
	return s.channels.UpdateLead(
		ctx,
		channelID,
		members[0].WakerID,
		time.Now().UnixMilli(),
	)
}

func containsChannelMember(members []models.ChannelMember, wakerID string) bool {
	for _, member := range members {
		if member.WakerID == wakerID {
			return true
		}
	}
	return false
}

func (s *service) listExistingMembers(
	ctx context.Context,
	channelID string,
) ([]models.ChannelMember, error) {
	members, err := s.channels.ListMembers(ctx, channelID)
	if err != nil {
		return nil, err
	}
	existing := make([]models.ChannelMember, 0, len(members))
	for _, member := range members {
		if _, err := s.wakers.Get(ctx, member.WakerID); err != nil {
			if !hasStatusErrorCode(err, status_error.WakerNotFound.Code()) {
				return nil, err
			}
			if removeErr := s.channels.RemoveMember(ctx, channelID, member.WakerID); removeErr != nil {
				return nil, removeErr
			}
			continue
		}
		existing = append(existing, member)
	}
	return existing, nil
}

func (s *service) deleteChannelIgnoringActiveWork(ctx context.Context, channelID string) error {
	threadIDs, err := s.threads.ListIDsByChannel(ctx, channelID)
	if err != nil {
		return err
	}
	chatIDs := make([]string, 0)
	for _, threadID := range threadIDs {
		ids, listErr := s.threads.ListChatIDs(ctx, threadID)
		if listErr != nil {
			return listErr
		}
		chatIDs = append(chatIDs, ids...)
	}
	return s.chats.DeleteChatsAtomically(ctx, chatIDs, func(transactionContext context.Context) error {
		if err := s.threads.DeleteByChannel(transactionContext, channelID); err != nil {
			return err
		}
		return s.channels.Delete(transactionContext, channelID)
	})
}
