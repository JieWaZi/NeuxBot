package global

import (
	"context"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
)

// stopWorkTicketCollaboration 先结束委派恢复，再关闭其依赖的 Core 和会话存储。
var stopWorkTicketCollaboration func()

func startWorkTicketCollaboration(ctx context.Context, wake <-chan struct{}, core execution.Service) {
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunWorkTicketCollaboration(workerCtx, wake, core)
	}()
	stopWorkTicketCollaboration = func() { cancel(); <-done }
}

// ResolveWorkTicketDelegationTarget 固定明确子单对应的本地目标和独立工作来源。
func ResolveWorkTicketDelegationTarget(ctx context.Context, delegationID, kind, subjectID string) (workticket.DelegationInput, error) {
	var target workticket.DelegationInput
	conversation, err := MainConversationService.Ensure(ctx, kind, subjectID)
	if err != nil {
		return target, err
	}
	owner := subjectID
	if kind == "team" {
		team, err := ChannelControllerManager.Get(ctx, subjectID)
		if err != nil {
			return target, err
		}
		owner = team.LeadWakerID
	}
	work, err := MainConversationService.ReserveWork(ctx, kind, subjectID, owner, "delegation:"+delegationID)
	if err != nil {
		return target, err
	}
	return workticket.DelegationInput{Target: workticket.Address{Kind: constants.WorkTicketOriginMain, ID: conversation.ID},
		OwnerKind: kind, OwnerID: subjectID, OwnerWakerID: owner, WorkChatID: work.WorkChatID}, nil
}

func delegatedWorkOptions(ctx context.Context, ticket models.WorkTicket) (execution.WorkOptions, error) {
	var options execution.WorkOptions
	if ticket.OwnerKind == "team" {
		team, err := ChannelControllerManager.Get(ctx, ticket.OwnerID)
		if err != nil {
			return options, err
		}
		if !ticket.Transferred && team.LeadWakerID != ticket.OwnerWakerID {
			return options, status_error.WorkTicketAttemptUnavailable
		}
		members, err := ChannelControllerManager.ListMembers(ctx, team.ID)
		if err != nil {
			return options, err
		}
		if !team.AuthorizesWorkActor(ticket.OwnerWakerID, members) {
			return options, status_error.WorkTicketAttemptUnavailable
		}
		for _, member := range members {
			if member.WakerID == ticket.OwnerWakerID && member.RuntimeID != "" {
				return execution.WorkOptions{RuntimeID: member.RuntimeID, AccountID: member.AccountID, Model: member.Model,
					Reasoning: member.Reasoning, Fast: member.Fast}, nil
			}
		}
	}
	waker, err := WakerControllerManager.Get(ctx, ticket.OwnerWakerID)
	if err != nil {
		return options, err
	}
	if model := waker.DefaultModel; model != nil {
		options.RuntimeID, options.AccountID, options.Model, options.Reasoning = model.RuntimeID, model.AccountID, model.Model, model.Reasoning
	}
	environments, err := RuntimeEnvironmentControllerManager.List(ctx)
	if err != nil {
		return options, err
	}
	for _, environment := range environments {
		if environment.Enabled && (environment.RuntimeID == options.RuntimeID || (options.RuntimeID == "" && environment.IsDefault)) {
			options.RuntimeID = environment.RuntimeID
			return options, nil
		}
	}
	return options, status_error.RuntimeUnavailable
}

// RunWorkTicketCollaboration 派发耐久子单并消费已结算的直接回件，直到 Core 结束。
func RunWorkTicketCollaboration(ctx context.Context, wake <-chan struct{}, core execution.Service) {
	// 唯一工作者以已提交的变更唤醒，启动时也读取原待派发和待消费回件。
	const retryDelay = 5 * time.Second
	var retry <-chan time.Time
	for {
		retry = nil
		if drainWorkTicketCollaboration(ctx, core) {
			retry = time.After(retryDelay)
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-retry:
		}
	}
}

func drainWorkTicketCollaboration(ctx context.Context, core execution.Service) (retry bool) {
	retry = drainTransferredWork(ctx, core)
	if err := recoverWorkTicketOriginReplies(ctx); err != nil && ctx.Err() == nil {
		logx.Warnf("recover work ticket origin replies: error=%v", err)
		retry = true
	}
	const pageSize = 32
	for after := ""; ctx.Err() == nil; {
		children, err := WorkTicketService.PendingDelegations(ctx, after, pageSize)
		if err != nil {
			logx.Warnf("read pending delegated work: error=%v", err)
			return true
		}
		for _, child := range children {
			after = child.ID
			options, err := delegatedWorkOptions(ctx, child)
			if err == nil {
				err = execution.StartDelegatedWork(ctx, core, child, options, MainConversationService)
			}
			if err != nil && ctx.Err() == nil {
				logx.Warnf("start delegated work: ticket_id=%s error=%v", child.ID, err)
				retry = true
			}
		}
		if len(children) < pageSize {
			break
		}
	}
	if err := WorkTicketService.ReadyDelegationReplies(ctx, func(launch workticket.DelegationReplyLaunch) error {
		if core.IsExecutionActive(launch.Ticket.WorkChatID, launch.Reply.ExecutionID) {
			retry = true
			return nil
		}
		previous, err := ChatService.ReadExecution(ctx, launch.Ticket.WorkChatID, launch.Reply.ExecutionID)
		if err == nil {
			err = execution.ResumeDelegatedQuestion(ctx, core, launch, previous, MainConversationService)
		}
		if err != nil && ctx.Err() == nil {
			logx.Warnf("resume work after direct question reply: ticket_id=%s error=%v", launch.Ticket.ID, err)
			retry = true
		}
		return nil
	}); err != nil && ctx.Err() == nil {
		logx.Warnf("read pending delegated question replies: error=%v", err)
		retry = true
	}
	for after := ""; ctx.Err() == nil; {
		parents, err := WorkTicketService.ReadyDelegationParents(ctx, after, pageSize)
		if err != nil {
			logx.Warnf("read delegated replies ready for consumption: error=%v", err)
			return true
		}
		for _, parent := range parents {
			ticket := parent.Ticket
			after = ticket.ID
			if core.IsExecutionActive(ticket.WorkChatID, ticket.CurrentExecutionID) {
				retry = true
				continue
			}
			previous, err := ChatService.ReadExecution(ctx, ticket.WorkChatID, ticket.CurrentExecutionID)
			if err == nil {
				err = execution.ResumeDelegatingWork(ctx, core, parent, previous, MainConversationService)
			}
			if err != nil && ctx.Err() == nil {
				logx.Warnf("resume work with direct delegated results: ticket_id=%s error=%v", ticket.ID, err)
				retry = true
			}
		}
		if len(parents) < pageSize {
			break
		}
	}
	return retry
}
