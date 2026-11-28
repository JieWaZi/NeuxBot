package workticket

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// TransferInput 保存原会话用户明确提交的责任移交请求。
type TransferInput struct {
	// Address 定位保留的原会话。
	Address Address
	// TicketID 定位原工单。
	TicketID string
	// ExpectedGeneration 核对用户看到的当前责任代次。
	ExpectedGeneration int64
	// RequestID 是用于幂等重投的命令身份。
	RequestID string
	// OwnerKind 是接手主体类型。
	OwnerKind string
	// OwnerID 定位接手伙伴或团队。
	OwnerID string
	// OwnerWakerID 是接手主体当前有资格执行的伙伴。
	OwnerWakerID string
	// WorkChatID 是目标主体拥有的独立工作记录。
	WorkChatID string
	// Content 是用户同意交给接手人的正文。
	Content string
	// ResolveTarget 在排除重投后为实际接手人预留独立工作记录。
	ResolveTarget func(ctx context.Context, executionID string) (ownerWakerID, workChatID string, err error) `json:"-"`
	// RecordNotice 固定原会话的移交回执和接手名称，主会话正文在同一次事务提交。
	RecordNotice func(context.Context, models.WorkTicket, *models.WorkTicketTransfer) error `json:"-"`
}

// TransferLaunch 保存待执行接纳的耐久移交。
type TransferLaunch struct {
	// Ticket 保存原回址和当前接单授权。
	Ticket models.WorkTicket
	// Receipt 保存用户移交授权和派发重试使用的唯一执行身份。
	Receipt models.WorkTicketTransfer
}

// Transfer 在同一次事务里封存旧尝试、更换负责人并移交仍属于本单的直接回件。
func (s *Service) Transfer(ctx context.Context, in TransferInput) (receipt models.WorkTicketTransfer, err error) {
	in.RequestID, in.Content = strings.TrimSpace(in.RequestID), strings.TrimSpace(in.Content)
	if !in.Address.valid() || in.TicketID == "" || in.ExpectedGeneration <= 0 || (in.OwnerKind != "waker" && in.OwnerKind != "team") ||
		in.OwnerID == "" || in.Content == "" || len(in.Content) > maximumDelegationText || !utf8.ValidString(in.Content) {
		return receipt, status_error.BadRequest
	}
	command := in
	command.OwnerWakerID, command.WorkChatID = "", ""
	identity, err := requestidempotency.NewSubmission(in.RequestID, command)
	if err != nil {
		return receipt, err
	}
	defer s.lockSettlement(in.TicketID)()
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		receipt = models.WorkTicketTransfer{}
		ticket, err := s.store.Ticket(tx, in.TicketID)
		if err != nil {
			return err
		}
		if ticket.ID == "" || ticketAddress(ticket) != in.Address {
			return status_error.WorkTicketNotFound
		}
		attempt, err := s.store.TransferAttempt(tx, ticket.ID, identity.ID)
		if err != nil {
			return err
		}
		if attempt.Transfer != nil {
			if attempt.Transfer.RequestDigest != identity.Hash {
				return status_error.CreateRequestConflict
			}
			receipt = *attempt.Transfer
			return nil
		}
		recovering := ticket.Transferred && ticket.Status == constants.WorkTicketStatusBlocked && ticket.CurrentExecutionID == ""
		if ticket.Generation != in.ExpectedGeneration || ticket.PendingExecutionID != "" || (ticket.CurrentExecutionID == "" && !recovering) ||
			ticket.Status == constants.WorkTicketStatusCompleted || ticket.Status == constants.WorkTicketStatusStopped ||
			(!recovering && ticket.OwnerKind == in.OwnerKind && ticket.OwnerID == in.OwnerID) {
			return status_error.WorkTicketCannotContinue
		}
		executionID := identity.ResourceID("work-ticket-transfer", ticket.ID)
		owner, source := in.OwnerWakerID, in.WorkChatID
		if in.ResolveTarget != nil {
			owner, source, err = in.ResolveTarget(tx, executionID)
			if err != nil {
				return err
			}
		}
		if owner == "" || source == "" || ticket.WorkChatID == source {
			return status_error.WorkTicketCannotContinue
		}
		next := ticket
		next.OwnerKind, next.OwnerID, next.OwnerWakerID, next.WorkChatID = in.OwnerKind, in.OwnerID, owner, source
		next.Transferred, next.DelegationFailed, next.Summary = true, false, ""
		next.ProjectID, next.ProjectScope, next.WorkingDirectory, next.AllowedDirectories = "", "", "", nil
		next.PermissionLevel = constants.PermissionLevelDefault
		allowed, err := s.store.Authorizes(tx, next, true)
		if err != nil || !allowed {
			if err == nil {
				err = status_error.WorkTicketCannotContinue
			}
			return err
		}
		if ticket.ParentTicketID != "" {
			candidate := next
			candidate.Generation = 0 // 接手工作 Chat 此时仅有预留。
			if err := s.authorizeDelegationChain(tx, candidate); err != nil {
				return err
			}
		}
		if err := s.validateTransferDescendants(tx, ticket, next); err != nil {
			return err
		}
		if err := s.endAttempt(tx, ticket.ID, ticket.Generation); err != nil {
			return err
		}
		next.Generation, next.Status, next.UpdatedAt = ticket.Generation+1, constants.WorkTicketStatusActive, ticketTime(ticket)
		next.PendingExecutionID, next.CurrentExecutionID = executionID, ""
		receipt = models.WorkTicketTransfer{TicketID: ticket.ID, Generation: next.Generation, ExecutionID: next.PendingExecutionID,
			PreviousExecutionID: ticket.CurrentExecutionID, PreviousWorkChatID: ticket.WorkChatID,
			PreviousOwnerKind: ticket.OwnerKind, PreviousOwnerID: ticket.OwnerID, PreviousOwnerWakerID: ticket.OwnerWakerID,
			RequestDigest: identity.Hash, Content: in.Content}
		if in.RecordNotice != nil {
			if err := in.RecordNotice(tx, next, &receipt); err != nil {
				return err
			}
		}
		if err := s.store.CreateAttempt(tx, models.WorkTicketAttempt{TicketID: ticket.ID, Generation: next.Generation,
			Status: constants.WorkTicketAttemptStatusPending, TransferRequestID: identity.ID, Transfer: &receipt}); err != nil {
			return err
		}
		if err := s.store.TransferOwner(tx, ticket, next); err != nil {
			return err
		}
		if err := s.store.TransferDelegations(tx, ticket.ID, ticket.Generation, next.Generation); err != nil {
			return err
		}
		s.notify(tx, in.Address)
		return nil
	})
	if err != nil {
		return models.WorkTicketTransfer{}, err
	}
	return receipt, nil
}

// PendingTransfers 按稳定工单标识分页读取原移交派发意图。
func (s *Service) PendingTransfers(ctx context.Context, afterID string, limit int) ([]TransferLaunch, error) {
	if limit < 1 || limit > 100 {
		return nil, status_error.BadRequest
	}
	var launches []TransferLaunch
	err := s.tx.Transaction(ctx, func(tx context.Context) error {
		launches = nil
		cursor := afterID
		for len(launches) < limit {
			rows, err := s.store.PendingTransfers(tx, cursor, limit-len(launches))
			if err != nil || len(rows) == 0 {
				return err
			}
			attempts, err := s.store.PendingTransferAttempts(tx, rows)
			if err != nil {
				return err
			}
			byTicket := make(map[string]*models.WorkTicketTransfer, len(attempts))
			for _, attempt := range attempts {
				byTicket[attempt.TicketID] = attempt.Transfer
			}
			for _, ticket := range rows {
				if receipt := byTicket[ticket.ID]; receipt != nil && receipt.ExecutionID == ticket.PendingExecutionID {
					launches = append(launches, TransferLaunch{Ticket: ticket, Receipt: *receipt})
				}
			}
			cursor = rows[len(rows)-1].ID
		}
		return nil
	})
	return launches, err
}

func pendingTransferAdmission(ticket models.WorkTicket, executionID string) bool {
	return executionID != "" && awaitingTransferOwner(ticket) && ticket.PendingExecutionID == executionID
}

func awaitingTransferOwner(ticket models.WorkTicket) bool {
	return ticket.Transferred && ticket.CurrentExecutionID == ""
}

// BlockTransfer 将确定失效且尚未接纳的移交标为受阻，保留原会话的重新转交和停止权。
func (s *Service) BlockTransfer(ctx context.Context, launch TransferLaunch, cause error) (bool, error) {
	reason := transferLaunchBlocker(cause)
	if reason == "" {
		return false, nil
	}
	defer s.lockSettlement(launch.Ticket.ID)()
	err := s.tx.Transaction(ctx, func(tx context.Context) error {
		ticket, err := s.store.Ticket(tx, launch.Ticket.ID)
		if err != nil || ticket.Generation != launch.Ticket.Generation || !pendingTransferAdmission(ticket, launch.Receipt.ExecutionID) {
			return err
		}
		if err := s.endAttempt(tx, ticket.ID, ticket.Generation); err != nil {
			return err
		}
		next := ticket
		next.Status, next.PendingExecutionID, next.Summary, next.UpdatedAt = constants.WorkTicketStatusBlocked, "", reason, ticketTime(ticket)
		if err := s.store.ChangeTicket(tx, ticket, next); err != nil {
			return err
		}
		s.notify(tx, ticketAddress(ticket))
		return nil
	})
	return err == nil, err
}

func transferLaunchBlocker(err error) string {
	for _, code := range []status_error.StatusError{status_error.WakerNotFound, status_error.ChannelNotFound,
		status_error.AgentOperationForbidden, status_error.WorkTicketAttemptUnavailable, status_error.WorkTicketCannotContinue} {
		if util.HasStatusCode(err, code) {
			return "The transfer recipient is no longer authorized. Reassign or stop this work ticket."
		}
	}
	for _, code := range []status_error.StatusError{status_error.RuntimeUnavailable, status_error.AIAccountNotFound,
		status_error.AIAccountConfigurationInvalid, status_error.RuntimeAuthRequired, status_error.BadRequest} {
		if util.HasStatusCode(err, code) {
			return "The transfer recipient's runtime or account is unavailable. Reassign or stop this work ticket."
		}
	}
	return ""
}

func (s *Service) prepareTransferredAdmission(ctx context.Context, ticket models.WorkTicket, in Admission) (models.WorkTicket, models.WorkTicketAttempt, error) {
	var empty models.WorkTicketAttempt
	if ticket.Status != constants.WorkTicketStatusActive || in.MessageID != in.ExecutionID || in.SubmissionID != in.ExecutionID {
		return ticket, empty, status_error.WorkTicketCannotContinue
	}
	working := ticket
	if working.WorkingDirectory == "" {
		managedOnly := len(in.AllowedDirectories) == 0 || (len(in.AllowedDirectories) == 1 && in.AllowedDirectories[0] == in.WorkingDirectory && in.WorkingDirectory != "")
		if !managedOnly {
			return ticket, empty, status_error.WorkTicketCannotContinue
		}
		working.WorkingDirectory = in.WorkingDirectory
		working.AllowedDirectories = slices.Clone(in.AllowedDirectories)
	}
	if !sameScope(working, in) {
		return ticket, empty, status_error.WorkTicketCannotContinue
	}
	if err := s.authorizeDelegationChain(ctx, working); err != nil {
		return ticket, empty, err
	}
	attempt, err := s.store.Attempt(ctx, ticket.ID, ticket.Generation)
	if err != nil {
		return ticket, attempt, err
	}
	if attempt.Transfer == nil || attempt.Transfer.ExecutionID != in.ExecutionID || attempt.Status != constants.WorkTicketAttemptStatusPending {
		return ticket, attempt, status_error.WorkTicketAttemptUnavailable
	}
	if ticket.WorkingDirectory != working.WorkingDirectory {
		if err := s.store.FreezeTransferDirectory(ctx, working); err != nil {
			return ticket, attempt, err
		}
	}
	return working, attempt, nil
}

// OrdinaryReportTarget locates the original assistant row that may receive the report.
func (s *Service) OrdinaryReportTarget(ctx context.Context, executionID string) (models.WorkTicketInput, error) {
	input, err := s.store.Input(ctx, executionID)
	if err != nil {
		return input, err
	}
	return s.ordinaryReportTarget(ctx, input)
}

func (s *Service) ordinaryReportTarget(ctx context.Context, input models.WorkTicketInput) (models.WorkTicketInput, error) {
	if input.OriginKind != constants.WorkTicketOriginChat || input.ConfirmedAt == nil {
		return input, status_error.WorkTicketAttemptUnavailable
	}
	if input.WorkChatID == input.OriginID {
		return input, nil
	}
	original, err := s.store.PublicInput(ctx, input.OriginKind, input.OriginID, input.TicketID)
	if err == nil && (original.TicketID != input.TicketID || original.WorkChatID != input.OriginID || original.ExecutionID == "" || original.ConfirmedAt == nil) {
		err = status_error.WorkTicketAttemptUnavailable
	}
	return original, err
}
