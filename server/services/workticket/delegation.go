package workticket

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

const (
	maximumDelegationDepth = 12
	maximumDirectChildren  = 32
	maximumDelegationText  = 16 << 10
)

// DelegationInput 是宿主解析目标后冻结的本次私有委派。
type DelegationInput struct {
	// RequestID 是父执行本次能力调用的稳定键。
	RequestID string
	// Target 是目标主体拥有的主会话地址，仅用来核对私有来源。
	Target Address
	// OwnerKind 表示目标主体为伙伴或团队。
	OwnerKind string
	// OwnerID 定位目标主体。
	OwnerID string
	// OwnerWakerID 是实际执行的目标伙伴。
	OwnerWakerID string
	// WorkChatID 是目标自己独立的工作来源，宿主已经预留。
	WorkChatID string
	// Content 是需要完成的目标。
	Content string
	// Context 是明确转交的纯文本背景。
	Context string
	// QuestionOnly 标记可在确定失败时释放依赖的问答子单。
	QuestionOnly bool
}

// DelegationID 从当前承诺的父执行与能力请求键派生子单和来源预留身份。
func DelegationID(parent Binding, requestID string) string {
	return (requestidempotency.Identity{ID: strings.TrimSpace(requestID)}).ResourceID(constants.WorkTicketDelegateOperation, parent.Ticket.ID, parent.ExecutionID)
}

// DelegationReceipt 只查该父绑定曾经提交的同一委派。
func (s *Service) DelegationReceipt(ctx context.Context, parent Binding, requestID string) (models.WorkTicket, error) {
	input, err := s.store.Input(ctx, parent.ExecutionID)
	if err != nil {
		return models.WorkTicket{}, err
	}
	if !matchesBinding(input, parent) || input.ConfirmedAt == nil {
		return models.WorkTicket{}, status_error.AgentReceiptNotFound
	}
	row, err := s.store.Ticket(ctx, DelegationID(parent, requestID))
	if err == nil && (row.ID == "" || row.ParentTicketID != parent.Ticket.ID) {
		err = status_error.AgentReceiptNotFound
	}
	return row, err
}

// Delegate 原子记录子单、直接父代次和原始启动意图。
func (s *Service) Delegate(ctx context.Context, parent Binding, in DelegationInput) (child models.WorkTicket, err error) {
	in.RequestID, in.Content, in.Context = strings.TrimSpace(in.RequestID), strings.TrimSpace(in.Content), strings.TrimSpace(in.Context)
	if parent.Ticket.ID == "" || parent.ExecutionID == "" || in.RequestID == "" || len(in.RequestID) > 64 ||
		!in.Target.valid() || in.Target.Kind != constants.WorkTicketOriginMain || in.WorkChatID == "" || in.OwnerID == "" || in.OwnerWakerID == "" ||
		(in.OwnerKind != "waker" && in.OwnerKind != "team") || in.Content == "" || len(in.Content)+len(in.Context) > maximumDelegationText ||
		!utf8.ValidString(in.Content) || !utf8.ValidString(in.Context) {
		return child, status_error.BadRequest
	}
	id := DelegationID(parent, in.RequestID)
	defer s.lockSettlement(parent.Ticket.ID)()
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		input, err := s.store.Input(tx, parent.ExecutionID)
		if err != nil {
			return err
		}
		if !matchesBinding(input, parent) || input.ConfirmedAt == nil {
			return status_error.WorkTicketAttemptUnavailable
		}
		child, err = s.store.Ticket(tx, id)
		if err != nil {
			return err
		}
		if child.ID != "" {
			if child.ParentTicketID != parent.Ticket.ID || ticketAddress(child) != in.Target ||
				child.OwnerKind != in.OwnerKind || child.OwnerID != in.OwnerID || child.OwnerWakerID != in.OwnerWakerID || child.WorkChatID != in.WorkChatID ||
				child.DelegationContent != in.Content || child.DelegationContext != in.Context || child.QuestionOnly != in.QuestionOnly {
				return status_error.CreateRequestConflict
			}
			return nil
		}
		current, err := s.reportTicket(tx, parent, input)
		if err != nil {
			return err
		}
		children, err := s.store.Delegations(tx, current.ID, current.Generation, maximumDirectChildren)
		if err != nil {
			return err
		}
		if len(children) >= maximumDirectChildren {
			return status_error.WorkTicketCannotContinue
		}
		child = models.WorkTicket{ID: id, ParentTicketID: current.ID, ParentGeneration: current.Generation,
			OriginKind: in.Target.Kind, OriginID: in.Target.ID, WorkChatID: in.WorkChatID, OwnerKind: in.OwnerKind, OwnerID: in.OwnerID,
			OwnerWakerID: in.OwnerWakerID, Status: constants.WorkTicketStatusActive, PermissionLevel: constants.PermissionLevelAuto,
			DelegationContent: in.Content, DelegationContext: in.Context, QuestionOnly: in.QuestionOnly}
		if err := s.authorizeDelegationChain(tx, child); err != nil {
			return err
		}
		if err := s.store.CreateTicket(tx, &child); err != nil {
			return err
		}
		if current.AwaitingDelegationReturns {
			return nil
		}
		next := current
		next.AwaitingDelegationReturns, next.UpdatedAt = true, ticketTime(current)
		return s.store.ChangeTicket(tx, current, next)
	})
	return child, err
}

// Delegation 验证私有目标和所有祖先仍允许原委派执行。
func (s *Service) Delegation(ctx context.Context, id string) (row models.WorkTicket, err error) {
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		row, err = s.store.Ticket(tx, id)
		if err != nil {
			return err
		}
		if row.ParentTicketID == "" || row.Status == constants.WorkTicketStatusStopped {
			return status_error.WorkTicketAttemptUnavailable
		}
		return s.authorizeDelegationChain(tx, row)
	})
	return row, err
}

// PendingDelegations 按原身份分页返回仍获授权但尚未确认接纳的子单。
func (s *Service) PendingDelegations(ctx context.Context, after string, limit int) ([]models.WorkTicket, error) {
	if limit < 1 || limit > 100 {
		return nil, status_error.BadRequest
	}
	var eligible []models.WorkTicket
	for len(eligible) < limit {
		rows, err := s.store.PendingDelegations(ctx, after, limit-len(eligible))
		if err != nil || len(rows) == 0 {
			return eligible, err
		}
		for _, row := range rows {
			after = row.ID
			checked, err := s.Delegation(ctx, row.ID)
			if errors.Is(err, status_error.WorkTicketAttemptUnavailable) || errors.Is(err, status_error.WorkTicketCannotContinue) || errors.Is(err, status_error.AgentOperationForbidden) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if checked.Generation == 0 {
				eligible = append(eligible, checked)
			}
		}
	}
	return eligible, nil
}

func (s *Service) recordDelegationFailure(ctx context.Context, ticket models.WorkTicket) error {
	if err := s.authorizeDelegationChain(ctx, ticket); err != nil {
		if errors.Is(err, status_error.WorkTicketAttemptUnavailable) {
			return nil
		}
		return err
	}
	id := reportIdentity(ticket.CurrentExecutionID, "\x00delegation-terminal")
	report := models.WorkTicketReport{ID: id, MessageID: id, TicketID: ticket.ID, ExecutionID: ticket.CurrentExecutionID,
		Status: constants.WorkTicketStatusBlocked}
	if err := s.store.CreateReport(ctx, report); err != nil {
		return err
	}
	return s.store.MarkDelegationFailed(ctx, ticket)
}

// DelegationReports 仅让仍获授权的直接父单读取原尝试的子单返回，可按回件身份精确读取。
func (s *Service) DelegationReports(ctx context.Context, parent Binding, reportID string) (rows []models.WorkTicketReport, err error) {
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		input, err := s.store.Input(tx, parent.ExecutionID)
		if err != nil {
			return err
		}
		current, err := s.reportTicket(tx, parent, input)
		if err != nil {
			return err
		}
		if reportID != "" {
			report, err := s.store.Report(tx, reportID)
			if err != nil {
				return err
			}
			child, err := s.store.Ticket(tx, report.TicketID)
			if err != nil {
				return err
			}
			if report.ID == "" || report.ID != report.MessageID || report.ReplyToReportID != "" ||
				child.ParentTicketID != current.ID || child.ParentGeneration != current.Generation {
				return status_error.WorkTicketAttemptUnavailable
			}
			rows = []models.WorkTicketReport{report}
			return nil
		}
		children, err := s.store.Delegations(tx, current.ID, current.Generation, maximumDirectChildren)
		if err != nil || len(children) == 0 {
			return err
		}
		ids := make([]string, 0, len(children))
		for _, child := range children {
			ids = append(ids, child.ID)
		}
		rows, err = s.store.DelegationReports(tx, ids, 100)
		return err
	})
	return rows, err
}

func (s *Service) authorizeDelegationChain(ctx context.Context, child models.WorkTicket) error {
	allowed, err := s.store.Authorizes(ctx, child, child.Generation == 0)
	if err != nil {
		return err
	}
	if !allowed {
		return status_error.WorkTicketAttemptUnavailable
	}
	current := child
	for depth := 0; current.ParentTicketID != ""; depth++ {
		if depth >= maximumDelegationDepth {
			return status_error.WorkTicketCannotContinue
		}
		parent, err := s.store.Ticket(ctx, current.ParentTicketID)
		if err != nil {
			return err
		}
		if parent.ID == "" || parent.Generation != current.ParentGeneration || parent.Status == constants.WorkTicketStatusStopped ||
			parent.Status == constants.WorkTicketStatusCompleted {
			return status_error.WorkTicketAttemptUnavailable
		}
		// 共享团队主会话的根单不授予跨团队派发权限；回复与重放同样验证当前成员资格。
		if parent.ParentTicketID == "" && parent.OwnerKind == "team" && parent.OriginKind == constants.WorkTicketOriginMain {
			if child.OwnerKind != "waker" {
				return status_error.AgentOperationForbidden
			}
			member, err := s.store.AuthorizesTeamDelegate(ctx, parent.OwnerID, child.OwnerWakerID)
			if err != nil {
				return err
			}
			if !member {
				return status_error.AgentOperationForbidden
			}
		}
		if sameDelegationOwner(parent, child) {
			return status_error.WorkTicketCannotContinue
		}
		// 在接手人接纳前，子单只将回件保存到原工单。接手执行仍需独立核对接单授权。
		if !awaitingTransferOwner(parent) {
			allowed, err := s.store.Authorizes(ctx, parent, false)
			if err != nil {
				return err
			}
			if !allowed {
				return status_error.WorkTicketAttemptUnavailable
			}
		}
		current = parent
	}
	return nil
}

func sameDelegationOwner(a, b models.WorkTicket) bool {
	return a.ID == b.ID || a.WorkChatID == b.WorkChatID || a.OwnerWakerID == b.OwnerWakerID ||
		(a.OwnerKind == b.OwnerKind && a.OwnerID == b.OwnerID)
}

func (s *Service) validateTransferDescendants(ctx context.Context, from, to models.WorkTicket) error {
	level := []models.WorkTicket{from}
	for depth := 0; len(level) != 0; depth++ {
		if depth > maximumDelegationDepth {
			return status_error.WorkTicketCannotContinue
		}
		var next []models.WorkTicket
		for batch := range slices.Chunk(level, 100) {
			children, err := s.store.LiveDelegationOwners(ctx, batch)
			if err != nil {
				return err
			}
			for _, child := range children {
				if sameDelegationOwner(to, child) {
					return status_error.WorkTicketCannotContinue
				}
			}
			next = append(next, children...)
		}
		level = next
	}
	return nil
}

func (s *Service) dependencyState(ctx context.Context, parent models.WorkTicket) (hasChildren, unsettled bool, err error) {
	children, err := s.store.Delegations(ctx, parent.ID, parent.Generation, maximumDirectChildren+1)
	if err != nil {
		return false, false, err
	}
	if len(children) > maximumDirectChildren {
		return true, true, nil
	}
	for _, child := range children {
		if child.Status != constants.WorkTicketStatusCompleted && !(child.QuestionOnly && child.DelegationFailed) {
			return true, true, nil
		}
	}
	return len(children) != 0, false, nil
}

// DelegationResume 固定父单可启动的下一次尝试及收件。
type DelegationResume struct {
	// Ticket 是直接接收这些结果的父单。
	Ticket models.WorkTicket
	// Reports 仅包含该父代次已经收到的直接结果。
	Reports []models.WorkTicketReport
	// QuestionID 发起本次续办的直接子单等待报告，空值表示该批已结案。
	QuestionID string
}

// ReadyDelegationParents 逐页核对原生轮次已结束且有待答问题或已结案回件的父单。
func (s *Service) ReadyDelegationParents(ctx context.Context, after string, limit int) ([]DelegationResume, error) {
	if limit < 1 || limit > 100 {
		return nil, status_error.BadRequest
	}
	var ready []DelegationResume
	for len(ready) < limit {
		rows, err := s.store.WaitingDelegationParents(ctx, after, limit-len(ready))
		if err != nil || len(rows) == 0 {
			return ready, err
		}
		for _, parent := range rows {
			after = parent.ID
			var resume DelegationResume
			err := s.tx.Transaction(ctx, func(tx context.Context) error {
				resume = DelegationResume{}
				current, err := s.store.Ticket(tx, parent.ID)
				if err != nil {
					return err
				}
				resume, err = s.delegationResume(tx, current)
				return err
			})
			if err != nil {
				return nil, err
			}
			if resume.Ticket.ID != "" {
				ready = append(ready, resume)
			}
		}
	}
	return ready, nil
}

// DelegationQuestionExecutionID 为每个接手代次安排同一直接问题的唯一答复轮。
func DelegationQuestionExecutionID(parent models.WorkTicket, reportID string) string {
	scope := []string{parent.ID}
	if parent.Transferred {
		scope = append(scope, strconv.FormatInt(parent.Generation, 10))
	}
	return (requestidempotency.Identity{ID: reportID}).ResourceID("work-ticket-delegation-question", scope...)
}

func (s *Service) delegationResume(ctx context.Context, parent models.WorkTicket) (DelegationResume, error) {
	var empty DelegationResume
	if !parent.AwaitingDelegationReturns || parent.Status != constants.WorkTicketStatusWaiting {
		return empty, nil
	}
	var pending models.WorkTicketInput
	if parent.PendingExecutionID != "" {
		var err error
		pending, err = s.store.Input(ctx, parent.PendingExecutionID)
		if err != nil || (!pending.ConsumesDelegationReturns && pending.DelegationQuestionID == "") {
			return empty, err
		}
	}
	input, err := s.store.Input(ctx, parent.CurrentExecutionID)
	if err != nil || input.FinishedAt == nil || input.TicketID != parent.ID || input.Generation != parent.Generation {
		return empty, err
	}
	if err := s.authorizeDelegationChain(ctx, parent); err != nil {
		if errors.Is(err, status_error.WorkTicketAttemptUnavailable) {
			return empty, nil
		}
		return empty, err
	}
	children, err := s.store.Delegations(ctx, parent.ID, parent.Generation, maximumDirectChildren+1)
	if err != nil || len(children) == 0 || len(children) > maximumDirectChildren {
		return empty, err
	}
	finished := make(map[string]models.WorkTicket, len(children))
	for _, child := range children {
		if child.Status == constants.WorkTicketStatusCompleted || (child.QuestionOnly && child.DelegationFailed) {
			finished[child.CurrentExecutionID] = child
		}
	}
	if len(finished) == len(children) && pending.DelegationQuestionID == "" {
		reports, err := s.delegationResults(ctx, finished)
		if err != nil || len(reports) != len(children) {
			return empty, err
		}
		return DelegationResume{Ticket: parent, Reports: reports}, nil
	}
	if pending.ConsumesDelegationReturns {
		return empty, nil
	}
	question, err := s.delegationQuestion(ctx, parent, children, pending.DelegationQuestionID)
	if err != nil || question.ID == "" {
		return empty, err
	}
	return DelegationResume{Ticket: parent, QuestionID: question.ID, Reports: []models.WorkTicketReport{question}}, nil
}

func (s *Service) delegationResults(ctx context.Context, children map[string]models.WorkTicket) ([]models.WorkTicketReport, error) {
	executions := make([]string, 0, len(children))
	var results []models.WorkTicketReport
	for executionID, child := range children {
		if child.Status == constants.WorkTicketStatusCompleted {
			executions = append(executions, executionID)
			continue
		}
		report, err := s.store.Report(ctx, reportIdentity(executionID, "\x00delegation-terminal"))
		if err != nil {
			return nil, err
		}
		if report.ID == "" || report.MessageID != report.ID || report.TicketID != child.ID || report.ExecutionID != executionID ||
			report.Status != constants.WorkTicketStatusBlocked {
			return nil, nil
		}
		results = append(results, report)
	}
	if len(executions) != 0 {
		settled, err := s.store.DelegationTurnReports(ctx, executions, []constants.WorkTicketStatus{constants.WorkTicketStatusCompleted}, "", len(executions)+1)
		if err != nil || len(settled) != len(executions) {
			return nil, err
		}
		seen := make(map[string]bool, len(settled))
		for _, report := range settled {
			child, ok := children[report.ExecutionID]
			if !ok || seen[report.ExecutionID] || child.ID != report.TicketID || child.Status != constants.WorkTicketStatusCompleted {
				return nil, nil
			}
			seen[report.ExecutionID] = true
			results = append(results, report)
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
	return results, nil
}
