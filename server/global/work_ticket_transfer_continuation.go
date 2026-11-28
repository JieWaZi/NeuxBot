package global

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
)

// PrepareTransferredWorkContinuation selects the current owner's isolated work source for a reply to the original conversation.
func PrepareTransferredWorkContinuation(ctx context.Context, address workticket.Address, ticket models.WorkTicket, replyID, publicMessageID string, input execution.WorkInput) (execution.WorkIntent, error) {
	if ticket.ID == "" || !ticket.Transferred || ticket.OriginKind != address.Kind || ticket.OriginID != address.ID || replyID == "" ||
		input.SubmissionID == "" || input.SteerExecutionID != "" || input.Workflow != nil || len(input.SkillIDs)+len(input.PluginIDs)+len(input.ConnectorIDs)+len(input.CodeReferences) != 0 {
		return execution.WorkIntent{}, status_error.BadRequest
	}
	if ticket.CurrentExecutionID == "" || ticket.PendingExecutionID != "" || ticket.Status == constants.WorkTicketStatusStopped {
		return execution.WorkIntent{}, status_error.WorkTicketCannotContinue
	}
	if err := ValidateWorkTicketOrigin(ctx, address); err != nil {
		return execution.WorkIntent{}, err
	}
	options, err := delegatedWorkOptions(ctx, ticket)
	if err != nil {
		return execution.WorkIntent{}, err
	}
	noInheritedCapabilities := []string{}
	options.EntryPoint, options.PermissionLevel = execution.MainConversationSourceEntryPoint, ticket.PermissionLevel
	options.AllowedAgentDomains = &noInheritedCapabilities
	input.ReplyToMessageID = ""
	return execution.WorkIntent{
		Chat: execution.ChatTarget{ChatID: ticket.WorkChatID}, Input: input, Options: options,
		Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
		Ticket: execution.WorkTicketInput{Address: address, PublicReplyID: replyID, PublicMessageID: publicMessageID,
			OwnerKind: ticket.OwnerKind, OwnerID: ticket.OwnerID, MainConversation: MainConversationService},
	}, nil
}

func dispatchTransferredMainConversationInput(ctx context.Context, origin models.MainConversation, item models.MainConversationInput, mark func(context.Context) error) (models.Execution, error) {
	address := workticket.Address{Kind: constants.WorkTicketOriginMain, ID: origin.ID}
	ticket, found, err := WorkTicketService.ForMessage(ctx, address, item.TicketReplyID)
	if err != nil {
		return models.Execution{}, err
	}
	if !found || !ticket.Transferred || ticket.WorkChatID != item.WorkChatID || item.StartWork || item.PublicReplyID != item.TicketReplyID ||
		ticket.PendingExecutionID != "" || ticket.CurrentExecutionID == "" || ticket.Status == constants.WorkTicketStatusStopped {
		return models.Execution{}, status_error.WorkTicketCannotContinue
	}
	if err := ValidateWorkTicketOrigin(ctx, address); err != nil {
		return models.Execution{}, err
	}
	options := queuedMainConversationOptions(item.Options)
	noInheritedCapabilities := []string{}
	options.EntryPoint, options.AllowedAgentDomains = execution.MainConversationSourceEntryPoint, &noInheritedCapabilities
	result, err := ExecutionService.Start(ctx, execution.WorkIntent{
		Chat: execution.ChatTarget{ChatID: item.WorkChatID}, Input: item.Input, Options: options,
		ExecutionID: item.ExecutionID, UserMessageID: item.SourceMessageID,
		Admission: constants.ExecutionAdmissionCommitAtQueuedNativeGate, BeforeQueuedNativeCommit: mark,
		Ticket: execution.WorkTicketInput{Address: address, PublicReplyID: item.TicketReplyID, PublicMessageID: item.PublicMessageID,
			OwnerKind: ticket.OwnerKind, OwnerID: ticket.OwnerID, MainConversation: MainConversationService},
		AdmissionBoundary: TransferredMainConversationAdmission(origin, ticket, item),
	})
	return result.Execution, err
}

// TransferredMainConversationAdmission preserves the user's reply address through a direct or queued admission.
func TransferredMainConversationAdmission(origin models.MainConversation, ticket models.WorkTicket, item models.MainConversationInput) execution.AdmissionBoundary {
	return func(ctx context.Context, commit func(context.Context) error) error {
		_, err := MainConversationService.AdmitTransferredUser(ctx, origin, ticket, models.MainConversationMessage{
			AdmissionSubmissionID: item.Input.SubmissionID, Content: item.PublicContent, ReplyToMessageID: item.PublicReplyID, InputDigest: item.RequestHash,
			SourceChatID: item.WorkChatID, SourceExecutionID: item.ExecutionID, SourceMessageID: item.SourceMessageID, SourceReadOnly: true,
		}, commit)
		return err
	}
}
