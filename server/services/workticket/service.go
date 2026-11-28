// Package workticket 持有原交办的授权、尝试和业务结论。
package workticket

import (
	"context"
	"crypto/sha256"
	"slices"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Address 是工单唯一的原始会话和回报地址。
type Address struct {
	// Kind 区分会话类型。
	Kind constants.WorkTicketOrigin
	// ID 是会话的稳定标识。
	ID string
}

func (a Address) valid() bool {
	return a.ID != "" && (a.Kind == constants.WorkTicketOriginChat || a.Kind == constants.WorkTicketOriginMain)
}

func ticketAddress(ticket models.WorkTicket) Address {
	return Address{Kind: ticket.OriginKind, ID: ticket.OriginID}
}

// Service 通过同一工单状态机处理普通与主会话的交办。
type Service struct {
	// store 保存工单及已确认的消息关联。
	store controllers.WorkTicketStore
	// tx 原子提交与工单推进有关的 SQLite 写入。
	tx controllers.TransactionController
	// changed 在工单的可见视图提交后通知原会话。
	changed func(context.Context, Address)
	// publishDeletedOrigin 将确认无法回到原址的报告交还原伙伴主会话。
	publishDeletedOrigin DeletedOriginPublisher
	// settlements 串行同一工单的回报、执行结束和人为停止，包括普通聊天的日志写入。
	settlements [64]sync.Mutex
}

// New 将工单状态机连接到持久化与提交后通知端口。
func New(store controllers.WorkTicketStore, tx controllers.TransactionController, changed func(context.Context, Address), publishDeletedOrigin DeletedOriginPublisher) *Service {
	return &Service{store: store, tx: tx, changed: changed, publishDeletedOrigin: publishDeletedOrigin}
}

func (s *Service) lockSettlement(ticketID string) func() {
	key := sha256.Sum256([]byte(ticketID))
	lock := &s.settlements[int(key[0])%len(s.settlements)]
	lock.Lock()
	return lock.Unlock
}

func (s *Service) notify(ctx context.Context, address Address) {
	if s.changed != nil {
		events.AfterCommit(ctx, func() { s.changed(ctx, address) })
	}
}

func ticketTime(ticket models.WorkTicket) int64 {
	return max(time.Now().UnixMilli(), ticket.UpdatedAt+1)
}

// Lookup 批量读取当前原会话消息所关联的工单视图。
func (s *Service) Lookup(ctx context.Context, address Address, messageIDs []string) (rows []models.WorkTicketMessageView, err error) {
	if !address.valid() || len(messageIDs) > 100 || slices.Contains(messageIDs, "") {
		return nil, status_error.BadRequest
	}
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		rows, err = s.store.TicketsForMessages(tx, address.Kind, address.ID, messageIDs)
		return err
	})
	return rows, err
}

// ForMessage 将原会话中的一个明确引用解析到已接纳的工单。
func (s *Service) ForMessage(ctx context.Context, address Address, messageID string) (models.WorkTicket, bool, error) {
	if messageID == "" {
		return models.WorkTicket{}, false, nil
	}
	rows, err := s.Lookup(ctx, address, []string{messageID})
	if err != nil || len(rows) == 0 {
		return models.WorkTicket{}, false, err
	}
	return rows[0].Ticket, true, nil
}

// HasDeletedOrdinaryOrigin 核对原址删除的持久凭据，避免阻挡接手执行和后续报告回投。
func (s *Service) HasDeletedOrdinaryOrigin(ctx context.Context, address Address, ticketID string) (bool, error) {
	ticket, err := s.store.Ticket(ctx, ticketID)
	return ticketAddress(ticket) == address && ticket.DeletedOrigin != nil, err
}

// ForSourceMessage 仅认可授权工作 Chat 中真实绑定到原会话的输入和答复。
func (s *Service) ForSourceMessage(ctx context.Context, address Address, workChatID, messageID string) (row models.WorkTicket, found bool, err error) {
	if !address.valid() || workChatID == "" || messageID == "" {
		return row, false, nil
	}
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		input, err := s.store.SourceInput(tx, workChatID, messageID)
		if err != nil || input.ExecutionID == "" || input.OriginKind != address.Kind || input.OriginID != address.ID {
			return err
		}
		row, err = s.store.Ticket(tx, input.TicketID)
		found = row.ID != "" && ticketAddress(row) == address && row.WorkChatID == workChatID
		return err
	})
	return row, found, err
}

// OriginalSourceMessage 从原交办回执读取给 Worker 的第一条实际用户输入。
func (s *Service) OriginalSourceMessage(ctx context.Context, ticket models.WorkTicket) (string, error) {
	if ticket.OriginKind == constants.WorkTicketOriginChat {
		return ticket.ID, nil
	}
	input, err := s.store.PublicInput(ctx, ticket.OriginKind, ticket.OriginID, ticket.ID)
	if err != nil {
		return "", err
	}
	if input.TicketID != ticket.ID || input.WorkChatID != ticket.WorkChatID {
		return "", status_error.WorkTicketAttemptUnavailable
	}
	return input.SourceMessageID, nil
}

// Stop 先结束工单并撤销原授权，再返回可供宿主取消的精确执行身份。
func (s *Service) Stop(ctx context.Context, address Address, ticketID string) (row models.WorkTicket, err error) {
	if !address.valid() || ticketID == "" {
		return row, status_error.BadRequest
	}
	defer s.lockSettlement(ticketID)()
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		ticket, err := s.store.Ticket(tx, ticketID)
		if err != nil {
			return err
		}
		if ticket.ID == "" || ticketAddress(ticket) != address || (!ticket.Transferred && ticket.CurrentExecutionID == "") {
			return status_error.WorkTicketNotFound
		}
		if ticket.Status == constants.WorkTicketStatusStopped {
			row = ticket
			return nil
		}
		if ticket.Status == constants.WorkTicketStatusCompleted {
			return status_error.WorkTicketCannotContinue
		}
		generation := ticket.Generation
		if ticket.CurrentExecutionID != "" {
			input, err := s.store.Input(tx, ticket.CurrentExecutionID)
			if err != nil {
				return err
			}
			generation = input.Generation
		}
		if err := s.endAttempt(tx, ticket.ID, generation); err != nil {
			return err
		}
		now := ticketTime(ticket)
		row = ticket
		row.Status, row.UpdatedAt = constants.WorkTicketStatusStopped, now
		row.AwaitingDelegationReturns = false
		if pendingTransferAdmission(ticket, ticket.PendingExecutionID) {
			row.PendingExecutionID = ""
		}
		if err := s.store.ChangeTicket(tx, ticket, row); err != nil {
			return err
		}
		s.notify(tx, address)
		return nil
	})
	return row, err
}

func (s *Service) endAttempt(ctx context.Context, ticketID string, generation int64) error {
	attempt, err := s.store.Attempt(ctx, ticketID, generation)
	if err != nil {
		return err
	}
	if attempt.TicketID == "" {
		return status_error.WorkTicketAttemptUnavailable
	}
	if attempt.Status == constants.WorkTicketAttemptStatusEnded {
		return nil
	}
	return s.store.ChangeAttempt(ctx, attempt, constants.WorkTicketAttemptStatusEnded)
}
