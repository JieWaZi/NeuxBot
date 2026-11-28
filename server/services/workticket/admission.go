package workticket

import (
	"context"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Admission 是宿主已经解析原引用和工作范围后提交的一次明确委托或补充。
type Admission struct {
	// Address 是准许发起及接收工单输入和结果的会话。
	Address Address
	// TicketID 指向明确引用的已有工单，新委托为空。
	TicketID string
	// MessageID 是本次输入在原会话内的身份。
	MessageID string
	// SourceMessageID 是本次输入在实际工作记录内的身份。
	SourceMessageID string
	// SourceReplyMessageID 是本次从工作记录中引用的原消息。
	SourceReplyMessageID string
	// WorkChatID 是本轮执行真正使用的工作 Chat。
	WorkChatID string
	// ExecutionID 是宿主分配的本轮执行。
	ExecutionID string
	// SubmissionID 是用户稳定的消息提交键。
	SubmissionID string
	// SubmissionHash 固定本次输入及执行选项。
	SubmissionHash string
	// OwnerKind 表示担责主体类型。
	OwnerKind string
	// OwnerID 是主体稳定标识。
	OwnerID string
	// OwnerWakerID 是实际获准执行的伙伴。
	OwnerWakerID string
	// ProjectID 是当前选择的项目。
	ProjectID string
	// ProjectScope 固定项目目录与指令授权版本。
	ProjectScope string
	// WorkingDirectory 是当前授权的临时目录。
	WorkingDirectory string
	// AllowedDirectories 是当前实际授权的目录集合。
	AllowedDirectories []string
	// PermissionLevel 是当前授予的执行权限。
	PermissionLevel constants.PermissionLevel
	// ResumeAfterExecutionID 只标记宿主消费已落盘直接回件的续办及其前轮。
	ResumeAfterExecutionID string
	// DelegationReplyID 指向宿主派给本子单的原问题答复。
	DelegationReplyID string
	// DelegationQuestionID 指向唤醒本父单的直接子单等待报告。
	DelegationQuestionID string
	// OriginReplyDigest 核对转交后由原普通会话提交的用户输入。
	OriginReplyDigest string
	// OriginReply 是接纳后需要投递到原普通会话的独立消息。
	OriginReply *eventlog.MessageCreatedData
	// EventSubscription 只由已匹配的来源规则在首次派发时签发。
	EventSubscription *models.WorkTicketEventSubscription
	// EventDeliveryID 是已经耐久登记并核对规则授权的续办事件。
	EventDeliveryID string
	// EventPreviousExecutionID 固定本次续办实际面向的原执行。
	EventPreviousExecutionID string
	// VerifyScope 用当前数据库事务重验待接纳输入的项目配置。
	VerifyScope func(context.Context) error
}

// Binding 固定宿主能以何身份报告当前工作。
type Binding struct {
	// Ticket 保存原回址及接纳时冻结的工作范围。
	Ticket models.WorkTicket
	// ExecutionID 是本轮的执行身份。
	ExecutionID string
	// WorkChatID 定位该轮接纳时的实际工作记录。
	WorkChatID string
	// Generation 是绑定的尝试代次。
	Generation int64
	// Replayed 表示提交已经有同一输入回执。
	Replayed bool
	// Confirmed 表示原回执已同权威会话日志核对。
	Confirmed bool
}

// LockAdmission 将预留、权威日志写入和确认与同一工单的停止串行化。
func (s *Service) LockAdmission(in Admission) func() {
	id := in.TicketID
	if id == "" {
		id = in.MessageID
	}
	return s.lockSettlement(id)
}

// PrepareAdmission 在权威聊天写入之前，原子预留原授权、工单、输入与其尝试。
func (s *Service) PrepareAdmission(ctx context.Context, in Admission) (binding Binding, err error) {
	if !in.Address.valid() || in.WorkChatID == "" || in.ExecutionID == "" || in.SubmissionID == "" || in.SubmissionHash == "" ||
		in.MessageID == "" || in.OwnerID == "" || in.OwnerWakerID == "" {
		return binding, status_error.BadRequest
	}
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		input, err := s.store.Submission(tx, in.Address.Kind, in.Address.ID, in.SubmissionID)
		if err != nil {
			return err
		}
		if input.ExecutionID != "" {
			binding, err = s.replayAdmission(tx, in, input)
			return err
		}
		if in.VerifyScope != nil {
			if err := in.VerifyScope(tx); err != nil {
				return err
			}
		}
		var ticket models.WorkTicket
		var attempt models.WorkTicketAttempt
		if in.TicketID == "" {
			ticket, attempt, err = s.prepareTicket(tx, in)
		} else {
			ticket, attempt, err = s.prepareContinuation(tx, in)
		}
		if err != nil {
			return err
		}
		input = models.WorkTicketInput{
			ExecutionID: in.ExecutionID, TicketID: ticket.ID, Generation: attempt.Generation,
			OriginKind: in.Address.Kind, OriginID: in.Address.ID, SubmissionID: in.SubmissionID, SubmissionHash: in.SubmissionHash,
			MessageID: in.MessageID, WorkChatID: in.WorkChatID, SourceMessageID: in.SourceMessageID,
			ConsumesDelegationReturns: in.consumesDelegationReturns(),
			DelegationReplyID:         in.DelegationReplyID,
			DelegationQuestionID:      in.DelegationQuestionID,
			OriginReplyDigest:         in.OriginReplyDigest,
			OriginReply:               in.OriginReply,
			EventSubscription:         in.EventSubscription,
			EventDeliveryID:           in.EventDeliveryID,
		}
		if err := s.store.CreateInput(tx, input); err != nil {
			return err
		}
		binding = inputBinding(ticket, input, false)
		return nil
	})
	return binding, err
}

func inputBinding(ticket models.WorkTicket, input models.WorkTicketInput, replayed bool) Binding {
	return Binding{Ticket: ticket, ExecutionID: input.ExecutionID, WorkChatID: input.WorkChatID,
		Generation: input.Generation, Replayed: replayed, Confirmed: input.ConfirmedAt != nil}
}

func (in Admission) consumesDelegationReturns() bool {
	return in.ResumeAfterExecutionID != "" && in.DelegationReplyID == "" && in.DelegationQuestionID == ""
}

func (s *Service) replayAdmission(ctx context.Context, in Admission, input models.WorkTicketInput) (Binding, error) {
	ticket, err := s.store.Ticket(ctx, input.TicketID)
	if err != nil {
		return Binding{}, err
	}
	if ticket.ID == "" || !sameScope(ticket, in) || input.SubmissionHash != in.SubmissionHash ||
		input.ConsumesDelegationReturns != in.consumesDelegationReturns() || input.DelegationReplyID != in.DelegationReplyID ||
		input.DelegationQuestionID != in.DelegationQuestionID || input.OriginReplyDigest != in.OriginReplyDigest ||
		input.EventDeliveryID != in.EventDeliveryID ||
		(in.TicketID != "" && in.TicketID != ticket.ID) ||
		(in.TicketID == "" && input.MessageID != ticket.ID) {
		return Binding{}, status_error.CreateRequestConflict
	}
	// 权威日志尚无回执时，宿主只能用预留的身份重试写入。
	if input.ConfirmedAt == nil && (input.ExecutionID != in.ExecutionID || input.MessageID != in.MessageID || input.SourceMessageID != in.SourceMessageID) {
		return Binding{}, status_error.CreateRequestConflict
	}
	return inputBinding(ticket, input, true), nil
}

func (s *Service) prepareTicket(ctx context.Context, in Admission) (models.WorkTicket, models.WorkTicketAttempt, error) {
	ticket := models.WorkTicket{
		ID: in.MessageID, OriginKind: in.Address.Kind, OriginID: in.Address.ID,
		WorkChatID: in.WorkChatID, OwnerKind: in.OwnerKind, OwnerID: in.OwnerID,
		OwnerWakerID: in.OwnerWakerID, ProjectID: in.ProjectID, ProjectScope: in.ProjectScope, WorkingDirectory: in.WorkingDirectory,
		AllowedDirectories: slices.Clone(in.AllowedDirectories), PermissionLevel: in.PermissionLevel,
		Status: constants.WorkTicketStatusActive, PendingExecutionID: in.ExecutionID,
	}
	allowed, err := s.store.Authorizes(ctx, ticket, true)
	if err != nil {
		return ticket, models.WorkTicketAttempt{}, err
	}
	if !allowed {
		return ticket, models.WorkTicketAttempt{}, status_error.WorkTicketCannotContinue
	}
	if err := s.store.CreateTicket(ctx, &ticket); err != nil {
		return ticket, models.WorkTicketAttempt{}, err
	}
	attempt, err := s.createAttempt(ctx, ticket)
	return ticket, attempt, err
}

func (s *Service) prepareContinuation(ctx context.Context, in Admission) (models.WorkTicket, models.WorkTicketAttempt, error) {
	ticket, err := s.store.Ticket(ctx, in.TicketID)
	if err != nil {
		return ticket, models.WorkTicketAttempt{}, err
	}
	if ticket.ID == "" || ticketAddress(ticket) != in.Address {
		return ticket, models.WorkTicketAttempt{}, status_error.WorkTicketNotFound
	}
	if pendingTransferAdmission(ticket, in.ExecutionID) {
		return s.prepareTransferredAdmission(ctx, ticket, in)
	}
	if ticket.ParentTicketID != "" && ticket.Generation == 0 {
		managedOnly := len(in.AllowedDirectories) == 0 || len(in.AllowedDirectories) == 1 && in.AllowedDirectories[0] == in.WorkingDirectory
		if ticket.Status != constants.WorkTicketStatusActive || ticket.PendingExecutionID != "" || ticket.ID != in.MessageID ||
			ticket.ID != in.ExecutionID || ticket.ID != in.SubmissionID || ticket.WorkChatID != in.WorkChatID ||
			ticket.OwnerKind != in.OwnerKind || ticket.OwnerID != in.OwnerID || ticket.OwnerWakerID != in.OwnerWakerID ||
			in.ProjectID != "" || in.ProjectScope != "" || !managedOnly || in.PermissionLevel != ticket.PermissionLevel ||
			(ticket.WorkingDirectory != "" && ticket.WorkingDirectory != in.WorkingDirectory) {
			return ticket, models.WorkTicketAttempt{}, status_error.WorkTicketCannotContinue
		}
		next := ticket
		next.PendingExecutionID, next.WorkingDirectory, next.UpdatedAt = in.ExecutionID, in.WorkingDirectory, ticketTime(ticket)
		next.AllowedDirectories = slices.Clone(in.AllowedDirectories)
		if err := s.authorizeDelegationChain(ctx, next); err != nil {
			return ticket, models.WorkTicketAttempt{}, err
		}
		attempt, err := s.createAttempt(ctx, ticket)
		if err != nil {
			return ticket, attempt, err
		}
		return next, attempt, s.store.ReserveDelegationExecution(ctx, ticket, next)
	}
	if ticket.Status == constants.WorkTicketStatusStopped || ticket.PendingExecutionID != "" || ticket.Generation == 0 || !sameScope(ticket, in) {
		return ticket, models.WorkTicketAttempt{}, status_error.WorkTicketCannotContinue
	}
	if in.EventDeliveryID != "" {
		delivery, err := s.store.EventDelivery(ctx, in.EventDeliveryID)
		if err != nil {
			return ticket, models.WorkTicketAttempt{}, err
		}
		if delivery.ID == "" || delivery.TicketID != ticket.ID || delivery.ExecutionID != "" || delivery.ErrorCode != "" ||
			ticket.CurrentExecutionID != in.EventPreviousExecutionID || ticket.Status == constants.WorkTicketStatusCompleted ||
			(delivery.RecoveryAfterExecutionID != "" && ticket.CurrentExecutionID != delivery.RecoveryAfterExecutionID) ||
			in.SubmissionID != EventDispatchIdentity(delivery) || in.ExecutionID != EventDispatchIdentity(delivery) {
			return ticket, models.WorkTicketAttempt{}, status_error.WorkTicketAttemptUnavailable
		}
		if delivery.WaitID != "" {
			wait, err := s.store.EventWait(ctx, delivery.WaitID)
			if err != nil {
				return ticket, models.WorkTicketAttempt{}, err
			}
			if wait.TicketID != ticket.ID || wait.WakeDeliveryID != delivery.ID ||
				(delivery.Retry == 0 && (wait.Generation != ticket.Generation || wait.ExecutionID != ticket.CurrentExecutionID)) {
				return ticket, models.WorkTicketAttempt{}, status_error.WorkTicketAttemptUnavailable
			}
		}
	}
	if in.ResumeAfterExecutionID != "" {
		if ticket.CurrentExecutionID != in.ResumeAfterExecutionID {
			return ticket, models.WorkTicketAttempt{}, status_error.WorkTicketAttemptUnavailable
		}
		if in.DelegationReplyID != "" {
			if in.ExecutionID != in.DelegationReplyID || in.MessageID != in.DelegationReplyID || in.SubmissionID != in.DelegationReplyID {
				return ticket, models.WorkTicketAttempt{}, status_error.WorkTicketAttemptUnavailable
			}
			if _, err := s.delegationReplyForLaunch(ctx, ticket, in.DelegationReplyID); err != nil {
				return ticket, models.WorkTicketAttempt{}, err
			}
		} else {
			resume, err := s.delegationResume(ctx, ticket)
			if err != nil || len(resume.Reports) == 0 || resume.QuestionID != in.DelegationQuestionID {
				if err == nil {
					err = status_error.WorkTicketAttemptUnavailable
				}
				return ticket, models.WorkTicketAttempt{}, err
			}
			if questionID := in.DelegationQuestionID; questionID != "" {
				id := DelegationQuestionExecutionID(ticket, questionID)
				if in.ExecutionID != id || in.MessageID != id || in.SubmissionID != id {
					return ticket, models.WorkTicketAttempt{}, status_error.WorkTicketAttemptUnavailable
				}
			}
		}
	}
	if in.SourceReplyMessageID != "" {
		quoted, err := s.store.SourceInput(ctx, in.WorkChatID, in.SourceReplyMessageID)
		if err != nil {
			return ticket, models.WorkTicketAttempt{}, err
		}
		if quoted.TicketID != ticket.ID {
			return ticket, models.WorkTicketAttempt{}, status_error.WorkTicketCannotContinue
		}
	}
	allowed, err := s.store.Authorizes(ctx, ticket, false)
	if err != nil {
		return ticket, models.WorkTicketAttempt{}, err
	}
	if !allowed {
		return ticket, models.WorkTicketAttempt{}, status_error.WorkTicketCannotContinue
	}
	previous, err := s.store.Input(ctx, ticket.CurrentExecutionID)
	if err != nil {
		return ticket, models.WorkTicketAttempt{}, err
	}
	if previous.FinishedAt == nil {
		return ticket, models.WorkTicketAttempt{}, status_error.WorkTicketCannotContinue
	}
	attempt, err := s.store.Attempt(ctx, ticket.ID, previous.Generation)
	if err != nil {
		return ticket, attempt, err
	}
	switch attempt.Status {
	case constants.WorkTicketAttemptStatusEnded:
		attempt, err = s.createAttempt(ctx, ticket)
	case constants.WorkTicketAttemptStatusRunning:
	default:
		return ticket, attempt, status_error.WorkTicketAttemptUnavailable
	}
	if err != nil {
		return ticket, attempt, err
	}
	next := ticket
	next.PendingExecutionID = in.ExecutionID
	return next, attempt, s.store.ChangeTicket(ctx, ticket, next)
}

func sameScope(ticket models.WorkTicket, in Admission) bool {
	return ticket.WorkChatID == in.WorkChatID && ticket.OwnerKind == in.OwnerKind && ticket.OwnerID == in.OwnerID &&
		ticket.OwnerWakerID == in.OwnerWakerID && ticket.ProjectID == in.ProjectID && ticket.ProjectScope == in.ProjectScope &&
		ticket.WorkingDirectory == in.WorkingDirectory && ticket.PermissionLevel == in.PermissionLevel &&
		slices.Equal(ticket.AllowedDirectories, in.AllowedDirectories)
}

func (s *Service) createAttempt(ctx context.Context, ticket models.WorkTicket) (models.WorkTicketAttempt, error) {
	attempt := models.WorkTicketAttempt{TicketID: ticket.ID, Generation: ticket.Generation + 1, Status: constants.WorkTicketAttemptStatusPending}
	return attempt, s.store.CreateAttempt(ctx, attempt)
}

// ConfirmAdmission 在宿主已提交输入和执行事实后，激活原预留并公开消息关联。
func (s *Service) ConfirmAdmission(ctx context.Context, binding Binding) error {
	return s.tx.Transaction(ctx, func(tx context.Context) error {
		input, err := s.store.Input(tx, binding.ExecutionID)
		if err != nil {
			return err
		}
		if !matchesBinding(input, binding) {
			return status_error.WorkTicketAttemptUnavailable
		}
		return s.confirmAdmission(tx, input)
	})
}

// ConfirmExecution 用宿主已经核验的权威执行回执补齐原预留。
func (s *Service) ConfirmExecution(ctx context.Context, executionID string, status ...constants.ExecutionStatus) error {
	var binding Binding
	err := s.tx.Transaction(ctx, func(tx context.Context) error {
		binding = Binding{}
		input, err := s.store.Input(tx, executionID)
		if err != nil || input.ExecutionID == "" {
			return err
		}
		if err := s.confirmAdmission(tx, input); err != nil {
			return err
		}
		if len(status) > 0 && terminalExecution(status[0]) {
			ticket, err := s.store.Ticket(tx, input.TicketID)
			if err != nil {
				return err
			}
			binding = inputBinding(ticket, input, true)
		}
		return nil
	})
	if err != nil || binding.ExecutionID == "" {
		return err
	}
	return s.FinishTurn(ctx, binding, status[0])
}

func matchesBinding(input models.WorkTicketInput, binding Binding) bool {
	return input.ExecutionID != "" && input.ExecutionID == binding.ExecutionID && input.TicketID == binding.Ticket.ID &&
		input.Generation == binding.Generation
}

func (s *Service) confirmAdmission(ctx context.Context, input models.WorkTicketInput) error {
	if input.ConfirmedAt != nil {
		return nil
	}
	ticket, err := s.store.Ticket(ctx, input.TicketID)
	if err != nil {
		return err
	}
	if ticket.ID == "" || ticket.PendingExecutionID != input.ExecutionID || ticket.Status == constants.WorkTicketStatusStopped {
		return status_error.WorkTicketAttemptUnavailable
	}
	if ticket.ParentTicketID != "" {
		if err := s.authorizeDelegationChain(ctx, ticket); err != nil {
			return err
		}
	}
	// 有效接纳由宿主传入已经核验的权威日志，首次输入的 SQLite Chat 投影可能尚未建好。
	allowed, err := s.store.Authorizes(ctx, ticket, ticket.Generation == 0)
	if err != nil {
		return err
	}
	if !allowed {
		return status_error.WorkTicketCannotContinue
	}
	if input.Generation > ticket.Generation || pendingTransferAdmission(ticket, input.ExecutionID) {
		attempt, err := s.store.Attempt(ctx, ticket.ID, input.Generation)
		if err != nil {
			return err
		}
		if attempt.Status != constants.WorkTicketAttemptStatusPending {
			return status_error.WorkTicketAttemptUnavailable
		}
		if err := s.store.ChangeAttempt(ctx, attempt, constants.WorkTicketAttemptStatusRunning); err != nil {
			return err
		}
	}
	now := ticketTime(ticket)
	input.ConfirmedAt = &now
	if err := s.store.ChangeInput(ctx, input); err != nil {
		return err
	}
	next := ticket
	next.PendingExecutionID, next.CurrentExecutionID = "", input.ExecutionID
	next.Generation, next.Status, next.UpdatedAt = input.Generation, constants.WorkTicketStatusActive, now
	if input.ConsumesDelegationReturns {
		next.AwaitingDelegationReturns = false
	}
	if input.DelegationReplyID != "" {
		reply, err := s.delegationReplyForLaunch(ctx, ticket, input.DelegationReplyID)
		if err != nil {
			return err
		}
		reply.MessageID = input.MessageID
		if err := s.store.ConfirmReport(ctx, reply); err != nil {
			return err
		}
	}
	if input.Generation > ticket.Generation {
		next.Summary = ""
		if ticket.Transferred && next.AwaitingDelegationReturns {
			if err := s.store.TransferDelegations(ctx, ticket.ID, ticket.Generation, input.Generation); err != nil {
				return err
			}
		}
	}
	if err := s.store.ChangeTicket(ctx, ticket, next); err != nil {
		return err
	}
	if input.EventSubscription != nil {
		subscription := *input.EventSubscription
		subscription.TicketID = ticket.ID
		if err := s.confirmEventSubscription(ctx, subscription); err != nil {
			return err
		}
	}
	if input.EventDeliveryID != "" {
		if err := s.store.SettleEventDelivery(ctx, input.EventDeliveryID, input.ExecutionID, ""); err != nil {
			return err
		}
	}
	s.notify(ctx, ticketAddress(ticket))
	return nil
}

// AbortAdmission 仅供宿主确认原日志未接纳时撤销未公开的预留。
func (s *Service) AbortAdmission(ctx context.Context, binding Binding) error {
	return s.tx.Transaction(ctx, func(tx context.Context) error {
		input, err := s.store.Input(tx, binding.ExecutionID)
		if err != nil || input.ExecutionID == "" {
			return err
		}
		if !matchesBinding(input, binding) || input.ConfirmedAt != nil {
			return status_error.WorkTicketAttemptUnavailable
		}
		ticket, err := s.store.Ticket(tx, input.TicketID)
		if err != nil {
			return err
		}
		if ticket.ID == "" || ticket.PendingExecutionID != input.ExecutionID {
			return status_error.WorkTicketAttemptUnavailable
		}
		return s.store.AbortAdmission(tx, ticket, input)
	})
}
