package global

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
)

func recordWorkTicketTransferNotice(ctx context.Context, ticket models.WorkTicket, receipt *models.WorkTicketTransfer) error {
	if ticket.OwnerKind == "team" {
		target, err := ChannelControllerManager.Get(ctx, ticket.OwnerID)
		if err != nil {
			return err
		}
		receipt.TargetName = target.Name
	} else {
		target, err := WakerControllerManager.Get(ctx, ticket.OwnerWakerID)
		if err != nil {
			return err
		}
		receipt.TargetName = target.Name
	}
	if strings.TrimSpace(receipt.TargetName) == "" {
		receipt.TargetName = ticket.OwnerID
	}
	if ticket.OriginKind != constants.WorkTicketOriginMain {
		return nil
	}
	origin, err := MainConversationControllerManager.Get(ctx, ticket.OriginID)
	if err != nil {
		return err
	}
	_, _, err = MainConversationControllerManager.Append(ctx, models.MainConversationMessage{
		ID: transferNoticeID(*receipt), ConversationID: origin.ID,
		Role: string(constants.MessageRoleAssistant), Content: transferNoticeContent(receipt.TargetName),
		ReplyToMessageID: ticket.ID, SourceWakerID: receipt.PreviousOwnerWakerID,
	}, origin.SubjectType, origin.SubjectID)
	return err
}

func publishOrdinaryTransferNotice(ctx context.Context, address workticket.Address, receipt models.WorkTicketTransfer) (err error) {
	if address.Kind != constants.WorkTicketOriginChat || receipt.TargetName == "" {
		return nil
	}
	defer func() {
		if err != nil {
			if deleted, lookupErr := WorkTicketService.HasDeletedOrdinaryOrigin(ctx, address, receipt.TicketID); lookupErr == nil && deleted {
				err = nil
			}
		}
	}()
	release, err := chatstorage.AcquireReferences(Config.ManagedFilesDirectory, address.ID)
	if err != nil {
		return err
	}
	defer release()
	data, err := eventlog.MarshalData(eventlog.MessageCreatedData{
		Role: constants.MessageRoleAssistant, Actor: constants.WorkTicketHandoffActor,
		Content: transferNoticeContent(receipt.TargetName),
	})
	if err != nil {
		return err
	}
	_, err = ChatService.AppendExecutionFacts(ctx, address.ID, []eventlog.AppendInput{{
		Type: constants.ChatEventTypeUserMessageCreated, MessageID: transferNoticeID(receipt), Data: data,
	}})
	return err
}

func transferNoticeID(receipt models.WorkTicketTransfer) string {
	return (requestidempotency.Identity{ID: receipt.ExecutionID}).ResourceID("work-ticket-handoff")
}

func transferNoticeContent(name string) string {
	return "Transferred to " + strings.Join(strings.Fields(name), " ") + "."
}
