package global

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// TryPublishWorkTicketOriginReplyBySubmission 尽快公开已接纳的原普通会话输入。
func TryPublishWorkTicketOriginReplyBySubmission(ctx context.Context, originID, submissionID string) {
	row, err := WorkTicketService.OriginReply(ctx, originID, submissionID)
	if err != nil {
		wakeOriginReplyRecovery(err)
		return
	}
	TryPublishWorkTicketOriginReply(ctx, row)
}

// TryPublishWorkTicketOriginReply 保留暂时无法写入原会话的耐久输入供后台重试。
func TryPublishWorkTicketOriginReply(ctx context.Context, row models.WorkTicketInput) {
	if err := publishWorkTicketOriginReply(ctx, row); err != nil {
		wakeOriginReplyRecovery(err)
	}
}

func wakeOriginReplyRecovery(err error) {
	logx.Warnf("publish work ticket origin reply: error=%v", err)
	if wakeWorkTicketTransfers != nil {
		wakeWorkTicketTransfers()
	}
}

func publishWorkTicketOriginReply(ctx context.Context, row models.WorkTicketInput) error {
	if row.OriginReply == nil {
		return nil
	}
	release, err := chatstorage.AcquireReferences(Config.ManagedFilesDirectory, row.OriginID)
	if err != nil {
		return err
	}
	defer release()
	return WorkTicketService.PublishOriginReply(ctx, row, ChatService.AppendExecutionFacts)
}

func recoverWorkTicketOriginReplies(ctx context.Context) error {
	return WorkTicketService.RecoverOriginReplies(ctx, func(ctx context.Context, row models.WorkTicketInput) error {
		row, err := WorkTicketService.ReconcileOriginReply(ctx, row, ChatService.ReadSubmission)
		if err != nil {
			return err
		}
		return publishWorkTicketOriginReply(ctx, row)
	})
}
