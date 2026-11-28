package controllers

import (
	"context"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

// WorkTicketStore 保存工单、尝试、输入和原会话报告，写入加入用例事务。
type WorkTicketStore interface {
	// ActivityCandidates 读取主体自己的根工单、其任务树和伙伴接受的子工单。
	ActivityCandidates(ctx context.Context, conversationID, subjectType, subjectID string) ([]models.WorkTicket, error)
	// AuthorizesTeamDelegate 核查新团队工单实际接单人的在任资格。
	AuthorizesTeamDelegate(ctx context.Context, teamID, wakerID string) (bool, error)
	EventSubscription(context.Context, string) (models.WorkTicketEventSubscription, error)
	EventTopicSubscriptions(context.Context, string, string) ([]models.WorkTicketEventSubscription, error)
	CreateEventSubscription(context.Context, models.WorkTicketEventSubscription) error
	EventDelivery(context.Context, string) (models.WorkTicketEventDelivery, error)
	CreateEventDelivery(context.Context, models.WorkTicketEventDelivery) error
	SettleEventDelivery(context.Context, string, string, string) error
	RecoverEventDelivery(context.Context, models.WorkTicketEventDelivery, bool, string) error
	MarkEventInputRead(context.Context, string, int64) error
	EventWait(context.Context, string) (models.WorkTicketEventWait, error)
	ActiveEventWait(context.Context, models.WorkTicket) (models.WorkTicketEventWait, error)
	CreateEventWait(context.Context, models.WorkTicketEventWait) error
	WakeEventWait(context.Context, string, string) error
	// TransferAttempt 读取原工单的同键移交回执。
	TransferAttempt(ctx context.Context, ticketID, requestID string) (models.WorkTicketAttempt, error)
	// PendingTransfers 按原身份读取仍待启动的移交。
	PendingTransfers(ctx context.Context, afterID string, limit int) ([]models.WorkTicket, error)
	// PendingTransferAttempts 批量读取候选工单当前代次的待启动尝试。
	PendingTransferAttempts(ctx context.Context, tickets []models.WorkTicket) ([]models.WorkTicketAttempt, error)
	// TransferOwner 在精确旧尝试上持久更换当前负责人及执行范围。
	TransferOwner(ctx context.Context, previous, next models.WorkTicket) error
	// FreezeTransferDirectory 为已预留的接手执行确认目标工作区。
	FreezeTransferDirectory(ctx context.Context, ticket models.WorkTicket) error
	// Delegations 读指定尝试的直接子单。
	Delegations(context.Context, string, int64, int) ([]models.WorkTicket, error)
	// LiveDelegationOwners 按父单代次批量读取仍需维持授权的子单负责人。
	LiveDelegationOwners(ctx context.Context, parents []models.WorkTicket) ([]models.WorkTicket, error)
	// TransferDelegations 将同一批直接子单的回件授权切换到交接后的父单代次。
	TransferDelegations(ctx context.Context, parentID string, previous, next int64) error
	// PendingDelegations 列未接纳执行的子单，按稳定标识分页。
	PendingDelegations(context.Context, string, int) ([]models.WorkTicket, error)
	// WaitingDelegationParents 列仍有未消费的直接子单结果的父单。
	WaitingDelegationParents(context.Context, string, int) ([]models.WorkTicket, error)
	// DelegationReports 读取已经落盘到直接父单的报告。
	DelegationReports(context.Context, []string, int) ([]models.WorkTicketReport, error)
	// DelegationTurnReports 读取这些实际 Worker 回合的特定私有回件类型。
	DelegationTurnReports(context.Context, []string, []constants.WorkTicketStatus, string, int) ([]models.WorkTicketReport, error)
	// DelegationReplyForQuestion 查找原问题已有的唯一父单答复。
	DelegationReplyForQuestion(context.Context, string) (models.WorkTicketReport, error)
	// PendingDelegationReplies 列尚未确认进入子单的直接问题答复。
	PendingDelegationReplies(context.Context, string, int) ([]models.WorkTicketReport, error)
	// ReserveDelegationExecution 首次接纳时固定目标自己的实际工作目录。
	ReserveDelegationExecution(context.Context, models.WorkTicket, models.WorkTicket) error
	// MarkDelegationFailed 保存可核对的问答子单失败状态。
	MarkDelegationFailed(context.Context, models.WorkTicket) error
	// Ticket 按稳定工单身份读取当前状态。
	Ticket(ctx context.Context, id string) (models.WorkTicket, error)
	// TicketsForMessages 批量读取原会话中的已确认消息关联。
	TicketsForMessages(ctx context.Context, kind constants.WorkTicketOrigin, originID string, ids []string) ([]models.WorkTicketMessageView, error)
	// PublicInput 读取原会话中已确认的用户输入。
	PublicInput(ctx context.Context, kind constants.WorkTicketOrigin, originID, messageID string) (models.WorkTicketInput, error)
	// SourceInput 仅在原工作 Chat 中识别已确认的输入或该轮答复。
	SourceInput(ctx context.Context, workChatID, messageID string) (models.WorkTicketInput, error)
	// Submission 查询原会话对同一用户提交的处理回执。
	Submission(ctx context.Context, kind constants.WorkTicketOrigin, originID, id string) (models.WorkTicketInput, error)
	// Input 读执行对应的接纳和终态身份。
	Input(ctx context.Context, executionID string) (models.WorkTicketInput, error)
	// UnfinishedInputs 仅列需要核对权威终态的已接纳工单输入。
	UnfinishedInputs(ctx context.Context, afterID string, limit int) ([]models.WorkTicketInput, error)
	// PendingOriginReplies 读取已接纳执行的待投普通会话输入。
	PendingOriginReplies(ctx context.Context, afterID string, limit int) ([]models.WorkTicketInput, error)
	// ConfirmOriginReply 清除已经公开或因原会话删除而撤销的待投副本。
	ConfirmOriginReply(ctx context.Context, executionID string) error
	// Attempt 读取已经分配的业务尝试。
	Attempt(ctx context.Context, ticketID string, generation int64) (models.WorkTicketAttempt, error)
	// Report 读取 Worker 原调用的持久报告。
	Report(ctx context.Context, id string) (models.WorkTicketReport, error)
	// PendingReports 分页读取尚未确认原目标收件的持久报告。
	PendingReports(ctx context.Context, afterID string, limit int) ([]models.WorkTicketReport, error)
	// HasPendingReport 判断原执行是否已有报告预留，仍缺少交付回执。
	HasPendingReport(ctx context.Context, ticketID, executionID string) (bool, error)
	// HasPublishedReport 核对本轮已交付的报告，正文为空时查询任意回件。
	HasPublishedReport(ctx context.Context, ticketID, executionID, content string) (bool, error)
	// HasPublishedMainMessage 判断该主会话是否已收到本轮独立发言。
	HasPublishedMainMessage(ctx context.Context, conversationID, chatID, executionID string) (bool, error)
	// Authorizes 核对原伙伴、会话绑定与项目仍有效。
	Authorizes(ctx context.Context, ticket models.WorkTicket, allowMissingChat bool) (bool, error)
	// PendingConclusionsForChats 查询将删除的普通会话中未确认收件的结案报告。
	PendingConclusionsForChats(ctx context.Context, chatIDs []string) ([]models.WorkTicketReport, error)
	// MarkOrdinaryTicketOriginDeleted 为仍可能回件的转交工单存储已核验的原址删除。
	MarkOrdinaryTicketOriginDeleted(ctx context.Context, origin models.Chat) error
	// PreserveDeletedOriginReceipt 保留核实后的收件地址与报告回执。
	PreserveDeletedOriginReceipt(ctx context.Context, report models.WorkTicketReport) error
	// CreateTicket 新建具有冻结范围的工单。
	CreateTicket(ctx context.Context, ticket *models.WorkTicket) error
	// CreateAttempt 在确定的工单代次下持久预留一次尝试。
	CreateAttempt(ctx context.Context, attempt models.WorkTicketAttempt) error
	// CreateInput 登记一次执行绑定和原输入提交。
	CreateInput(ctx context.Context, input models.WorkTicketInput) error
	// CreateReport 保留本次原报告作为耐久结算收据。
	CreateReport(ctx context.Context, report models.WorkTicketReport) error
	// ChangeTicket 核对预期状态后保存本次状态迁移。
	ChangeTicket(ctx context.Context, previous, next models.WorkTicket) error
	// ChangeAttempt 保存尝试的业务状态。
	ChangeAttempt(ctx context.Context, attempt models.WorkTicketAttempt, status constants.WorkTicketAttemptStatus) error
	// ChangeInput 保存接纳或终态的时间。
	ChangeInput(ctx context.Context, input models.WorkTicketInput) error
	// ConfirmReport 关联已经提交至原会话的报告回执。
	ConfirmReport(ctx context.Context, report models.WorkTicketReport) error
	// AbortAdmission 释放尚未确认、且权威日志无接纳回执的输入和预留。
	AbortAdmission(ctx context.Context, ticket models.WorkTicket, input models.WorkTicketInput) error
}

// workTicketController 使用 GORM 持久化工单生命周期。
type workTicketController struct {
	// db 为事务之外提供 Core SQLite 连接。
	db *gorm.DB
}

// NewWorkTicketController 创建工单仓储。
func NewWorkTicketController(db *gorm.DB) WorkTicketStore {
	return &workTicketController{db: db}
}

func (c *workTicketController) Ticket(ctx context.Context, id string) (models.WorkTicket, error) {
	return workTicketRow[models.WorkTicket](controllerDB(ctx, c.db).Where("id = ?", id), "read_work_ticket", id)
}

func (c *workTicketController) Submission(ctx context.Context, kind constants.WorkTicketOrigin, originID, id string) (models.WorkTicketInput, error) {
	return workTicketRow[models.WorkTicketInput](controllerDB(ctx, c.db).Where("origin_kind = ? AND origin_id = ? AND submission_id = ?", kind, originID, id), "read_work_ticket_submission", originID)
}

func (c *workTicketController) Input(ctx context.Context, executionID string) (models.WorkTicketInput, error) {
	return workTicketRow[models.WorkTicketInput](controllerDB(ctx, c.db).Where("execution_id = ?", executionID), "read_work_ticket_input", executionID)
}

func (c *workTicketController) UnfinishedInputs(ctx context.Context, afterID string, limit int) ([]models.WorkTicketInput, error) {
	var rows []models.WorkTicketInput
	err := controllerDB(ctx, c.db).Table("work_ticket_inputs INDEXED BY idx_work_ticket_unfinished").
		Where("confirmed_at IS NOT NULL AND finished_at IS NULL AND execution_id > ?", afterID).
		Order("execution_id").Limit(limit).Find(&rows).Error
	if err != nil {
		err = databaseError("read_unfinished_work_ticket_inputs", "", err)
	}
	return rows, err
}

func (c *workTicketController) PublicInput(ctx context.Context, kind constants.WorkTicketOrigin, originID, messageID string) (models.WorkTicketInput, error) {
	return workTicketRow[models.WorkTicketInput](controllerDB(ctx, c.db).
		Where("origin_kind = ? AND origin_id = ? AND message_id = ? AND confirmed_at IS NOT NULL", kind, originID, messageID),
		"read_work_ticket_public_input", originID)
}

func (c *workTicketController) SourceInput(ctx context.Context, workChatID, messageID string) (models.WorkTicketInput, error) {
	db := controllerDB(ctx, c.db)
	row, err := workTicketRow[models.WorkTicketInput](db.Where("work_chat_id = ? AND source_message_id = ? AND confirmed_at IS NOT NULL", workChatID, messageID), "read_work_ticket_source_input", workChatID)
	if err != nil || row.ExecutionID != "" {
		return row, err
	}
	return workTicketRow[models.WorkTicketInput](db.Where("execution_id = ? AND work_chat_id = ? AND confirmed_at IS NOT NULL", messageID, workChatID), "read_work_ticket_source_result", workChatID)
}

func (c *workTicketController) Attempt(ctx context.Context, ticketID string, generation int64) (models.WorkTicketAttempt, error) {
	return workTicketRow[models.WorkTicketAttempt](controllerDB(ctx, c.db).
		Where("ticket_id = ? AND generation = ?", ticketID, generation), "read_work_ticket_attempt", ticketID)
}

func (c *workTicketController) Report(ctx context.Context, id string) (models.WorkTicketReport, error) {
	return workTicketRow[models.WorkTicketReport](controllerDB(ctx, c.db).Where("id = ?", id), "read_work_ticket_report", id)
}

func (c *workTicketController) PendingReports(ctx context.Context, afterID string, limit int) ([]models.WorkTicketReport, error) {
	var rows []models.WorkTicketReport
	err := controllerDB(ctx, c.db).Table("work_ticket_reports INDEXED BY idx_work_ticket_undelivered").
		Where("message_id = '' AND id > ?", afterID).Order("id").Limit(limit).Find(&rows).Error
	if err != nil {
		err = databaseError("read_pending_work_ticket_reports", "", err)
	}
	return rows, err
}

func (c *workTicketController) HasPendingReport(ctx context.Context, ticketID, executionID string) (bool, error) {
	row, err := workTicketRow[models.WorkTicketReport](controllerDB(ctx, c.db).Select("id").
		Where("execution_id = ? AND ticket_id = ? AND message_id = ''", executionID, ticketID), "read_work_ticket_pending_report", executionID)
	return row.ID != "", err
}

func (c *workTicketController) HasPublishedReport(ctx context.Context, ticketID, executionID, content string) (bool, error) {
	query := controllerDB(ctx, c.db).Select("id").Where("execution_id = ? AND ticket_id = ? AND message_id <> ''", executionID, ticketID)
	if content != "" {
		query = query.Where("content = ?", content)
	}
	row, err := workTicketRow[models.WorkTicketReport](query, "read_work_ticket_published_report", executionID)
	return row.ID != "", err
}

func (c *workTicketController) HasPublishedMainMessage(ctx context.Context, conversationID, chatID, executionID string) (bool, error) {
	query := mainPublicationQuery(controllerDB(ctx, c.db), conversationID, chatID, executionID).
		Where("id <> ?", models.MainConversationDispatchAcknowledgmentID(conversationID, chatID, executionID)).Select("id")
	row, err := workTicketRow[models.MainConversationMessage](query, "read_work_ticket_main_delivery", executionID)
	return row.ID != "", err
}

func workTicketRow[T any](query *gorm.DB, operation, resource string) (row T, err error) {
	if err := query.Limit(1).Find(&row).Error; err != nil {
		return row, databaseError(operation, resource, err)
	}
	return row, nil
}

func (c *workTicketController) TicketsForMessages(ctx context.Context, kind constants.WorkTicketOrigin, originID string, ids []string) ([]models.WorkTicketMessageView, error) {
	views := make([]models.WorkTicketMessageView, 0)
	if len(ids) == 0 {
		return views, nil
	}
	db := controllerDB(ctx, c.db)
	var inputs []models.WorkTicketInput
	existing := db.Where("origin_kind = ? AND origin_id = ? AND confirmed_at IS NOT NULL", kind, originID)
	if err := existing.Session(&gorm.Session{}).Select("ticket_id", "message_id").Where("message_id IN ?", ids).Find(&inputs).Error; err != nil {
		return nil, databaseError("read_work_ticket_public_inputs", originID, err)
	}
	byMessage := make(map[string]string, len(inputs))
	for _, input := range inputs {
		byMessage[input.MessageID] = input.TicketID
	}
	// 报告回执直接携带原会话消息身份。同一普通答复可承载多份报告。
	var reports []models.WorkTicketReport
	if err := db.Select("ticket_id", "message_id").Where("message_id IN ?", ids).Group("ticket_id, message_id").Find(&reports).Error; err != nil {
		return nil, databaseError("read_work_ticket_public_reports", originID, err)
	}
	for _, report := range reports {
		byMessage[report.MessageID] = report.TicketID
	}
	if kind == constants.WorkTicketOriginMain {
		if err := associateMainPublications(db, originID, ids, byMessage); err != nil {
			return nil, err
		}
	}
	if len(byMessage) == 0 {
		return views, nil
	}
	ticketIDs := make([]string, 0, len(byMessage))
	for _, ticketID := range byMessage {
		ticketIDs = append(ticketIDs, ticketID)
	}
	var tickets []models.WorkTicket
	if err := db.Where("origin_kind = ? AND origin_id = ? AND id IN ? AND parent_ticket_id = ''", kind, originID, ticketIDs).Find(&tickets).Error; err != nil {
		return nil, databaseError("read_work_tickets_by_message", originID, err)
	}
	byID := make(map[string]models.WorkTicket, len(tickets))
	for _, ticket := range tickets {
		byID[ticket.ID] = ticket
	}
	for _, id := range ids {
		if ticket, ok := byID[byMessage[id]]; ok {
			views = append(views, models.WorkTicketMessageView{MessageID: id, Ticket: ticket})
			delete(byMessage, id)
		}
	}
	return views, nil
}

func associateMainPublications(db *gorm.DB, originID string, ids []string, byMessage map[string]string) error {
	var messages []models.MainConversationMessage
	err := db.Select("id", "role", "source_chat_id", "source_execution_id", "removed_at", "admission_pending").
		Where("conversation_id = ? AND id IN ?", originID, ids).Find(&messages).Error
	if err != nil {
		return databaseError("read_work_ticket_main_messages", originID, err)
	}
	var executionIDs []string
	for _, message := range messages {
		if message.RemovedAt != 0 || message.AdmissionPending {
			delete(byMessage, message.ID)
			continue
		}
		if byMessage[message.ID] == "" && message.Role == string(constants.MessageRoleAssistant) && message.SourceExecutionID != "" {
			executionIDs = append(executionIDs, message.SourceExecutionID)
		}
	}
	if len(executionIDs) == 0 {
		return nil
	}
	var inputs []models.WorkTicketInput
	err = db.Select("execution_id", "ticket_id", "work_chat_id").
		Where("origin_kind = ? AND origin_id = ? AND execution_id IN ? AND confirmed_at IS NOT NULL",
			constants.WorkTicketOriginMain, originID, executionIDs).Find(&inputs).Error
	if err != nil {
		return databaseError("read_work_ticket_inputs_by_execution", originID, err)
	}
	byExecution := make(map[string]models.WorkTicketInput, len(inputs))
	for _, input := range inputs {
		byExecution[input.ExecutionID] = input
	}
	for _, message := range messages {
		if message.RemovedAt != 0 || message.AdmissionPending || byMessage[message.ID] != "" || message.Role != string(constants.MessageRoleAssistant) {
			continue
		}
		if input, ok := byExecution[message.SourceExecutionID]; ok && input.WorkChatID == message.SourceChatID {
			byMessage[message.ID] = input.TicketID
		}
	}
	return nil
}

func (c *workTicketController) Authorizes(ctx context.Context, ticket models.WorkTicket, allowMissingChat bool) (bool, error) {
	db := controllerDB(ctx, c.db)
	waker, err := workTicketRow[models.Waker](db.Select("id").Where("id = ?", ticket.OwnerWakerID), "authorize_work_ticket_actor", ticket.ID)
	if err != nil || waker.ID == "" {
		return false, err
	}
	chat, err := workTicketRow[models.Chat](db.Select("id", "waker_id", "project_id", "workspace_directory", "additional_working_directories", "hidden_from_waker_history").Where("id = ?", ticket.WorkChatID), "authorize_work_ticket_chat", ticket.ID)
	if err != nil {
		return false, err
	}
	directoriesMatch := slices.Equal(chat.AdditionalWorkingDirectories, ticket.AllowedDirectories) ||
		(ticket.ParentTicketID != "" && ticket.Generation == 0 && len(ticket.AllowedDirectories) == 0 && chat.ProjectID == "" &&
			len(chat.AdditionalWorkingDirectories) == 1 && chat.AdditionalWorkingDirectories[0] == chat.WorkspaceDirectory)
	if chat.ID == "" {
		if !allowMissingChat {
			return false, nil
		}
	} else if chat.WakerID != ticket.OwnerWakerID ||
		chat.ProjectID != ticket.ProjectID ||
		!directoriesMatch ||
		chat.HiddenFromWakerHistory != (ticket.Transferred || ticket.OriginKind == constants.WorkTicketOriginMain) {
		return false, nil
	}
	if ticket.ProjectID != "" {
		project, err := workTicketRow[models.Project](db.Select("id").Where("id = ? AND waker_id = ?", ticket.ProjectID, ticket.OwnerWakerID), "authorize_work_ticket_project", ticket.ID)
		if err != nil || project.ID == "" {
			return false, err
		}
	}
	if !ticket.Transferred && ticket.OriginKind == constants.WorkTicketOriginChat {
		return ticket.WorkChatID == ticket.OriginID && ticket.OwnerKind == "waker" && ticket.OwnerID == ticket.OwnerWakerID, nil
	}
	sourceQuery := db.Where("work_chat_id = ? AND waker_id = ?", ticket.WorkChatID, ticket.OwnerWakerID)
	if !ticket.Transferred {
		sourceQuery = sourceQuery.Where("conversation_id = ?", ticket.OriginID)
	}
	source, err := workTicketRow[models.MainConversationWork](sourceQuery, "authorize_work_ticket_source", ticket.ID)
	if err != nil || source.WorkChatID == "" {
		return false, err
	}
	targets, err := mainWorkRequestTargets(db, []models.MainConversationWork{source})
	if err != nil {
		return false, err
	}
	target := targets[source.WorkChatID]
	return target.Title != nil && target.Owner.SubjectID == ticket.OwnerID && target.Owner.SubjectType == ticket.OwnerKind, nil
}

func (c *workTicketController) CreateTicket(ctx context.Context, ticket *models.WorkTicket) error {
	if err := c.create(ctx, ticket.ID, ticket); err != nil {
		return err
	}
	c.publishActivityChange(ctx, *ticket)
	return nil
}

func (c *workTicketController) CreateAttempt(ctx context.Context, attempt models.WorkTicketAttempt) error {
	return c.create(ctx, attempt.TicketID, &attempt)
}

func (c *workTicketController) CreateInput(ctx context.Context, input models.WorkTicketInput) error {
	return c.create(ctx, input.TicketID, &input)
}

func (c *workTicketController) CreateReport(ctx context.Context, report models.WorkTicketReport) error {
	return c.create(ctx, report.TicketID, &report)
}

func (c *workTicketController) create(ctx context.Context, id string, value any) error {
	if err := controllerDB(ctx, c.db).Create(value).Error; err != nil {
		if dbhelper.IsDuplicateError(err) {
			return status_error.CreateRequestConflict
		}
		return databaseError("create_work_ticket_fact", id, err)
	}
	return nil
}

func (c *workTicketController) ChangeTicket(ctx context.Context, previous, next models.WorkTicket) error {
	result := controllerDB(ctx, c.db).Model(&models.WorkTicket{}).
		Where("id = ? AND generation = ? AND current_execution_id = ? AND pending_execution_id = ? AND status = ? AND updated_at = ?", previous.ID, previous.Generation, previous.CurrentExecutionID, previous.PendingExecutionID, previous.Status, previous.UpdatedAt).
		UpdateColumns(map[string]any{
			"status": next.Status, "summary": next.Summary, "generation": next.Generation,
			"current_execution_id": next.CurrentExecutionID, "pending_execution_id": next.PendingExecutionID,
			"updated_at": next.UpdatedAt, "awaiting_delegation_returns": next.AwaitingDelegationReturns,
		})
	if err := workTicketChanged(result, "update_work_ticket", previous.ID); err != nil {
		return err
	}
	c.publishActivityChange(ctx, next)
	return nil
}

func (c *workTicketController) ChangeAttempt(ctx context.Context, attempt models.WorkTicketAttempt, status constants.WorkTicketAttemptStatus) error {
	result := controllerDB(ctx, c.db).Model(&models.WorkTicketAttempt{}).
		Where("ticket_id = ? AND generation = ? AND status = ?", attempt.TicketID, attempt.Generation, attempt.Status).
		UpdateColumn("status", status)
	return workTicketChanged(result, "update_work_ticket_attempt", attempt.TicketID)
}

func (c *workTicketController) ChangeInput(ctx context.Context, input models.WorkTicketInput) error {
	query := controllerDB(ctx, c.db).Model(&models.WorkTicketInput{}).Where("execution_id = ? AND finished_at IS NULL", input.ExecutionID)
	var result *gorm.DB
	if input.FinishedAt == nil {
		result = query.Where("confirmed_at IS NULL").UpdateColumn("confirmed_at", input.ConfirmedAt)
	} else {
		result = query.Where("confirmed_at IS NOT NULL").UpdateColumn("finished_at", input.FinishedAt)
	}
	return workTicketChanged(result, "update_work_ticket_input", input.TicketID)
}

func (c *workTicketController) ConfirmReport(ctx context.Context, report models.WorkTicketReport) error {
	result := controllerDB(ctx, c.db).Model(&models.WorkTicketReport{}).
		Where("id = ? AND message_id = ''", report.ID).
		UpdateColumns(map[string]any{"message_id": report.MessageID, "delivery_conversation_id": report.DeliveryConversationID})
	return workTicketChanged(result, "confirm_work_ticket_report", report.TicketID)
}

func workTicketChanged(result *gorm.DB, operation, id string) error {
	if result.Error != nil {
		return databaseError(operation, id, result.Error)
	}
	if result.RowsAffected == 0 {
		return status_error.WorkTicketAttemptUnavailable
	}
	return nil
}

func (c *workTicketController) AbortAdmission(ctx context.Context, ticket models.WorkTicket, input models.WorkTicketInput) error {
	db := controllerDB(ctx, c.db)
	result := db.Where("execution_id = ? AND confirmed_at IS NULL", input.ExecutionID).Delete(&models.WorkTicketInput{})
	if err := workTicketChanged(result, "abort_work_ticket_input", ticket.ID); err != nil {
		return err
	}
	if input.Generation > ticket.Generation {
		result := db.Where("ticket_id = ? AND generation = ? AND status = ?", ticket.ID, input.Generation, constants.WorkTicketAttemptStatusPending).Delete(&models.WorkTicketAttempt{})
		if err := workTicketChanged(result, "abort_work_ticket_attempt", ticket.ID); err != nil {
			return err
		}
	}
	if ticket.Generation == 0 && ticket.ParentTicketID == "" {
		result := db.Where("id = ? AND generation = 0 AND pending_execution_id = ?", ticket.ID, input.ExecutionID).Delete(&models.WorkTicket{})
		return workTicketChanged(result, "abort_work_ticket", ticket.ID)
	}
	if ticket.Transferred && ticket.CurrentExecutionID == "" && ticket.PendingExecutionID == input.ExecutionID && ticket.Generation == input.Generation {
		return nil // 已提交的移交仍待派发，执行接纳可沿同一身份重试。
	}
	next := ticket
	next.PendingExecutionID = ""
	return c.ChangeTicket(ctx, ticket, next)
}

// deleteWorkTickets 清理属于正在删除的来源或工作记录的全部工单事实。
func deleteWorkTickets(db *gorm.DB, scope models.WorkTicket, resourceID string) error {
	ids := db.Model(&models.WorkTicket{}).Select("id").Where(&scope)
	if scope.OriginKind == constants.WorkTicketOriginChat && scope.OwnerID != "" {
		// 按伙伴清理普通会话时，当前接手身份不代表拥有原交办会话。
		ids = ids.Where("work_chat_id = origin_id")
	}
	for _, row := range []any{&models.WorkTicketReport{}, &models.WorkTicketInput{}, &models.WorkTicketAttempt{}} {
		if err := db.Where("ticket_id IN (?)", ids).Delete(row).Error; err != nil {
			return databaseError("delete_work_ticket_facts", resourceID, err)
		}
	}
	if err := db.Where("id IN (?)", ids).Delete(&models.WorkTicket{}).Error; err != nil {
		return databaseError("delete_work_tickets", resourceID, err)
	}
	return nil
}
