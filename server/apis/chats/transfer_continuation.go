package chats

import (
	"context"
	"crypto/sha256"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
)

var ordinarySubmissionLanes = func() [256]chan struct{} {
	var lanes [256]chan struct{}
	for i := range lanes {
		lanes[i] = make(chan struct{}, 1)
	}
	return lanes
}()

// lockOrdinarySubmission 串行核对同一提交键是否已经由原聊天或接手工作记录接纳。
func lockOrdinarySubmission(ctx context.Context, chatID, submissionID string) (func(), error) {
	key := sha256.Sum256([]byte(chatID + "\x00" + submissionID))
	lane := ordinarySubmissionLanes[key[0]]
	select {
	case lane <- struct{}{}:
		return func() { <-lane }, nil
	case <-ctx.Done():
		return nil, status_error.OperationCanceled
	}
}

func replayTransferredWork(ctx context.Context, originID string, identity requestidempotency.Identity) (execution.AcceptedExecution, bool, error) {
	row, err := global.WorkTicketService.OriginReply(ctx, originID, identity.ID)
	if err != nil || row.ExecutionID == "" {
		return execution.AcceptedExecution{}, false, err
	}
	if row.OriginReplyDigest != identity.Hash {
		return execution.AcceptedExecution{}, true, status_error.CreateRequestConflict
	}
	address := workticket.Address{Kind: constants.WorkTicketOriginChat, ID: originID}
	if err := global.ValidateWorkTicketOrigin(ctx, address); err != nil {
		return execution.AcceptedExecution{}, true, err
	}
	row, err = global.WorkTicketService.ReconcileOriginReply(ctx, row, global.ChatService.ReadSubmission)
	if err != nil || row.ExecutionID == "" {
		return execution.AcceptedExecution{}, false, err
	}
	originChat, err := global.ChatControllerManager.Get(ctx, originID)
	if err != nil {
		return execution.AcceptedExecution{}, true, err
	}
	global.TryPublishWorkTicketOriginReply(ctx, row)
	return execution.AcceptedExecution{Chat: originChat, Published: true, Replayed: true}, true, nil
}

// continueTransferredWork 仅将正文与用户明确引用的摘录提交给接手人。
func continueTransferredWork(ctx context.Context, origin workticket.Address, ticket models.WorkTicket, input execution.WorkInput,
	identity requestidempotency.Identity,
) (execution.AcceptedExecution, error) {
	if identity.ID == "" {
		return execution.AcceptedExecution{}, status_error.BadRequest
	}
	release, err := chatstorage.AcquireReferences(global.Config.ManagedFilesDirectory, origin.ID)
	if err != nil {
		return execution.AcceptedExecution{}, err
	}
	defer release()
	quoted, err := global.ChatService.ReadWorkTicketReplyTarget(ctx, origin.ID, input.ReplyToMessageID, false)
	if err != nil {
		return execution.AcceptedExecution{}, err
	}
	originChat, err := global.ChatControllerManager.Get(ctx, origin.ID)
	if err != nil {
		return execution.AcceptedExecution{}, err
	}
	publicID := identity.ResourceID("work-ticket-user-reply", origin.ID)
	reference, err := json.Marshal(map[string]any{
		"messageId": quoted.Reference.MessageID, "role": quoted.Reference.Role, "text": quoted.Text, "truncated": quoted.Truncated,
	})
	if err != nil {
		return execution.AcceptedExecution{}, err
	}
	input.ContextInstructions = []workinput.Instruction{{Name: constants.ChatReplyTargetContextName, Content: string(reference)}}
	intent, err := global.PrepareTransferredWorkContinuation(ctx, origin, ticket, quoted.Reference.MessageID, publicID, input)
	if err != nil {
		return execution.AcceptedExecution{}, err
	}
	intent.Ticket.OriginReplyDigest = identity.Hash
	intent.Ticket.OriginReply = &eventlog.MessageCreatedData{
		SubmissionID: input.SubmissionID, Role: constants.MessageRoleUser, Actor: input.Actor,
		Content: input.Content, ReplyTo: &quoted.Reference,
	}
	intent.AdmissionBoundary = func(ctx context.Context, commit func(context.Context) error) error {
		if err := commit(ctx); err != nil {
			return err
		}
		// 已接纳的工作不因原会话投递暂时失败而被标记未执行。
		global.TryPublishWorkTicketOriginReplyBySubmission(ctx, origin.ID, identity.ID)
		return nil
	}
	accepted, err := global.ExecutionService.Start(ctx, intent)
	if err != nil {
		return execution.AcceptedExecution{}, err
	}
	return execution.AcceptedExecution{Chat: originChat, Published: true, Replayed: accepted.Replayed}, nil
}
