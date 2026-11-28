package execution

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
)

// StartTransferredWork 只把用户明确提交的交接内容和新目标的配置交给独立执行。
func StartTransferredWork(ctx context.Context, core Service, launch workticket.TransferLaunch, options WorkOptions, main MainConversationMessages) error {
	ticket, receipt := launch.Ticket, launch.Receipt
	noInheritedCapabilities := []string{}
	options.EntryPoint, options.PermissionLevel = MainConversationSourceEntryPoint, ticket.PermissionLevel
	options.AllowedAgentDomains = &noInheritedCapabilities
	userMessageID := (requestidempotency.Identity{ID: receipt.ExecutionID}).ResourceID("work-ticket-input", ticket.WorkChatID)
	_, err := core.Start(ctx, WorkIntent{
		Chat:    ChatTarget{ChatID: ticket.WorkChatID, MainConversationSource: &CreateChatSpec{WakerID: ticket.OwnerWakerID}},
		Input:   WorkInput{Actor: constants.WorkTicketHostActor, SubmissionID: receipt.ExecutionID, Content: receipt.Content},
		Options: options, ExecutionID: receipt.ExecutionID, UserMessageID: userMessageID, Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
		Ticket: WorkTicketInput{ResumeTicket: ticket, Address: workticket.Address{Kind: ticket.OriginKind, ID: ticket.OriginID},
			OwnerKind: ticket.OwnerKind, OwnerID: ticket.OwnerID, PublicMessageID: receipt.ExecutionID, MainConversation: main},
	})
	return err
}

type transferredWorkPublisher interface {
	PublishTransferredWorkReport(context.Context, models.WorkTicket, string, string, string) (models.MainConversationMessage, bool, error)
}
