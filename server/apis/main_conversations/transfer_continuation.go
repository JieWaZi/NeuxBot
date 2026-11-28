package main_conversations

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/apis/uploads"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
	"github.com/gin-gonic/gin"
)

func (op *SendMainConversationMessage) continueTransferredWork(ctx context.Context, httpCtx *gin.Context, origin models.MainConversation, receipt models.MainConversationMessage, ticket models.WorkTicket, body MainConversationInput, input execution.WorkInput, content, digest string) (MainConversationAcceptance, error) {
	if receipt.ID != "" {
		if receipt.InputDigest != digest || receipt.Content != content || receipt.ReplyToMessageID != body.ReplyToMessageID {
			return MainConversationAcceptance{}, status_error.CreateRequestConflict
		}
		accepted, err := global.ChatService.ReadSubmission(ctx, receipt.SourceChatID, body.SubmissionID)
		if err != nil {
			return MainConversationAcceptance{}, err
		}
		if accepted != nil {
			message, err := global.MainConversationService.ConfirmUserReceipt(ctx, receipt, *accepted)
			if err == nil {
				err = global.WorkTicketService.ConfirmExecution(ctx, accepted.ID, accepted.Status)
			}
			return MainConversationAcceptance{Message: message, ExecutionID: accepted.ID}, err
		}
		if receipt.SourceChatID != ticket.WorkChatID {
			return MainConversationAcceptance{}, status_error.WorkTicketCannotContinue
		}
	}
	quoted, err := global.MainConversationService.Message(ctx, origin.ID, body.ReplyToMessageID)
	if util.HasStatusCode(err, status_error.ChatMessageNotFound) {
		return MainConversationAcceptance{}, status_error.ChatReplyTargetUnavailable
	}
	if err != nil {
		return MainConversationAcceptance{}, err
	}
	if quoted.AdmissionPending || quoted.RemovedAt != 0 {
		return MainConversationAcceptance{}, status_error.ChatReplyTargetUnavailable
	}
	quote, err := publicReplyInstruction(quoted)
	if err != nil {
		return MainConversationAcceptance{}, err
	}
	input.SubmissionID = body.SubmissionID
	input.ContextInstructions = []workinput.Instruction{quote}
	publicID := mainconversation.UserMessageID(origin.ID, body.SubmissionID)
	intent, err := global.PrepareTransferredWorkContinuation(ctx, workticket.Address{Kind: constants.WorkTicketOriginMain, ID: origin.ID}, ticket, quoted.ID, publicID, input)
	if err != nil {
		return MainConversationAcceptance{}, err
	}
	item := models.MainConversationInput{
		ConversationID: origin.ID, WorkChatID: ticket.WorkChatID, PublicMessageID: publicID,
		ExecutionID: publicID, SourceMessageID: richSourceMessageID(publicID, input), PublicReplyID: quoted.ID,
		TicketReplyID: quoted.ID, EntryPoint: models.MainConversationTransferredReply,
		PublicContent: content, RequestHash: digest, Input: intent.Input,
	}
	if global.MainConversationInputQueue != nil && receipt.ID == "" {
		queued, err := enqueueMainConversationInput(ctx, httpCtx, intent.Options, item)
		return MainConversationAcceptance{QueuedInput: queued}, err
	}
	intent.ExecutionID, intent.UserMessageID = item.ExecutionID, item.SourceMessageID
	intent.ClaimAttachments = func(files []execution.AttachmentInput) (func(bool), error) {
		return uploads.ClaimWork(httpCtx, &execution.WorkInput{SubmissionID: input.SubmissionID, Attachments: files})
	}
	intent.AdmissionBoundary = global.TransferredMainConversationAdmission(origin, ticket, item)
	accepted, err := global.ExecutionService.Start(ctx, intent)
	if err != nil {
		return MainConversationAcceptance{}, err
	}
	message, err := global.MainConversationService.Message(ctx, origin.ID, publicID)
	return MainConversationAcceptance{Message: message, ExecutionID: accepted.Execution.ID}, err
}
