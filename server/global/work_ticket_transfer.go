package global

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
)

var wakeWorkTicketTransfers func()

// TransferWorkTicket 处理经过本机用户认证的转交请求，提交后才取消旧执行并唤醒调度。
func TransferWorkTicket(ctx context.Context, command workticket.TransferInput) (models.WorkTicketTransfer, error) {
	if err := ValidateWorkTicketOrigin(ctx, command.Address); err != nil {
		return models.WorkTicketTransfer{}, err
	}
	command.ResolveTarget = func(tx context.Context, executionID string) (string, string, error) {
		if err := ValidateWorkTicketOrigin(tx, command.Address); err != nil {
			return "", "", err
		}
		owner := command.OwnerID
		if command.OwnerKind == "team" {
			team, err := ChannelControllerManager.Get(tx, command.OwnerID)
			if err != nil {
				return "", "", err
			}
			owner = team.LeadWakerID
		}
		target := models.WorkTicket{OwnerKind: command.OwnerKind, OwnerID: command.OwnerID, OwnerWakerID: owner,
			Transferred: true, PermissionLevel: constants.PermissionLevelDefault}
		if _, err := transferredWorkOptions(tx, ExecutionService, target); err != nil {
			return "", "", err
		}
		work, err := MainConversationService.ReserveWork(tx, command.OwnerKind, command.OwnerID, owner, "transfer:"+executionID)
		return owner, work.WorkChatID, err
	}
	command.RecordNotice = recordWorkTicketTransferNotice
	receipt, err := WorkTicketService.Transfer(ctx, command)
	if err != nil {
		return receipt, err
	}
	if receipt.PreviousExecutionID != "" {
		ExecutionService.CancelExecution(receipt.PreviousWorkChatID, receipt.PreviousExecutionID)
	}
	if err := publishOrdinaryTransferNotice(ctx, command.Address, receipt); err != nil {
		logx.Warnf("publish work ticket transfer notice: ticket_id=%s error=%v", receipt.TicketID, err)
	}
	if wakeWorkTicketTransfers != nil {
		wakeWorkTicketTransfers()
	}
	return receipt, nil
}

func transferredWorkOptions(ctx context.Context, core execution.Service, ticket models.WorkTicket) (execution.WorkOptions, error) {
	options, err := delegatedWorkOptions(ctx, ticket)
	if err != nil {
		return options, err
	}
	options.PermissionLevel = ticket.PermissionLevel
	return execution.FreezeContinuationOptions(ctx, core, models.Chat{}, false, options)
}

func drainTransferredWork(ctx context.Context, core execution.Service) (retry bool) {
	const pageSize = 32
	for after := ""; ctx.Err() == nil; {
		launches, err := WorkTicketService.PendingTransfers(ctx, after, pageSize)
		if err != nil {
			logx.Warnf("read pending work ticket transfers: error=%v", err)
			return true
		}
		for _, launch := range launches {
			after = launch.Ticket.ID
			if launch.Receipt.PreviousExecutionID != "" {
				core.CancelExecution(launch.Receipt.PreviousWorkChatID, launch.Receipt.PreviousExecutionID)
			}
			address := workticket.Address{Kind: launch.Ticket.OriginKind, ID: launch.Ticket.OriginID}
			if err := publishOrdinaryTransferNotice(ctx, address, launch.Receipt); err != nil {
				logx.Warnf("publish work ticket transfer notice: ticket_id=%s error=%v", launch.Ticket.ID, err)
				retry = true
				continue
			}
			options, err := transferredWorkOptions(ctx, core, launch.Ticket)
			if err == nil {
				err = execution.StartTransferredWork(ctx, core, launch, options, MainConversationService)
			}
			if err != nil && ctx.Err() == nil {
				logx.Warnf("start transferred work: ticket_id=%s error=%v", launch.Ticket.ID, err)
				blocked, err := WorkTicketService.BlockTransfer(ctx, launch, err)
				if err != nil {
					logx.Warnf("block transferred work: ticket_id=%s error=%v", launch.Ticket.ID, err)
				}
				retry = retry || !blocked
			}
		}
		if len(launches) < pageSize {
			break
		}
	}
	return retry
}
