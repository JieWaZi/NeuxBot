package execution

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
)

// WorkTicketInput 保留用户明确的工单动作与入口已经核对的公开身份。
type WorkTicketInput struct {
	// Start 表示以本条输入接纳一张新工单。
	Start bool
	// DelegationID 是由宿主工作调度读取的私有子单，用户输入从不设置。
	DelegationID string
	// ResumeTicket 是宿主接受直接回件或原用户转交后待续办的工单。
	ResumeTicket models.WorkTicket
	// DelegationReplyID 标记由宿主读取的原直接子单问题答复。
	DelegationReplyID string
	// DelegationQuestionID 标记由宿主核对并转给本父单的直接子单等待报告。
	DelegationQuestionID string
	// Address 是发起的公开会话地址，普通聊天由实际接纳的 Chat 决定。
	Address workticket.Address
	// OwnerKind 表示担责主体类型，取伙伴或团队。
	OwnerKind string
	// OwnerID 定位担责主体。
	OwnerID string
	// PublicMessageID 是此输入在原主会话中的消息身份。
	PublicMessageID string
	// PublicReplyID 是用户明确引用或经专属工作来源核对的主会话消息。
	PublicReplyID string
	// OriginReplyDigest 核对转交后由原普通会话提交的用户输入。
	OriginReplyDigest string
	// OriginReply 是接纳后需要投递到原普通会话的独立消息。
	OriginReply *eventlog.MessageCreatedData
	// MainConversation 将报告交给原主会话的消息服务。
	MainConversation MainConversationMessages
	// EventSubscription 由自动规则匹配事件后为首次派工签发。
	EventSubscription *models.WorkTicketEventSubscription
	// EventDelivery 是原工单随后接纳的来源动态。
	EventDelivery *models.WorkTicketEventDelivery
}

// workTicketTurn 限制当前执行只能以原接纳绑定推进这张工单。
type workTicketTurn struct {
	// tickets 负责工单接纳和结算校验。
	tickets *workticket.Service
	// binding 在报告授权激活前写入一次，之后仅供读取。
	binding workticket.Binding
	// main 保存主会话来源的报告发布端口，普通聊天为空。
	main MainConversationMessages
	// journal 将普通报告追加到原聊天本轮答复。
	journal chatservice.Journal
	// storageRoot 在跨来源回件期间阻止删除原普通会话。
	storageRoot string
	// resolveTarget 固定明确委派的目标和独立工作来源。
	resolveTarget DelegationTargetResolver
	// wake 在原委派或结论已经持久化后通知宿主队列。
	wake func()
	// initialEvent 表示首条公开结果由自动任务运行按原身份交付。
	initialEvent bool
	// event 保留后续事件的原始引用与确认身份。
	event *models.WorkTicketEventDelivery
	// canWaitForEvent 表示本单有等待 CI 成功事件的规则授权。
	canWaitForEvent bool
	// resolveEventRule 验证模型引用的是当前主体自己的有效事件配置。
	resolveEventRule WorkTicketEventRuleResolver
}

func (m *service) confirmTicketReplay(ctx context.Context, request pendingMessage, execution models.Execution) error {
	if m.workTickets == nil || !(request.ticket.Start || request.ticket.DelegationID != "" || request.ticket.ResumeTicket.ID != "" || request.ticket.PublicReplyID != "" || request.replyToMessageID != "") {
		return nil
	}
	if request.ticket.Address.Kind != constants.WorkTicketOriginMain {
		if err := reconcileOrdinaryWorkReports(ctx, m.journal, m.workTickets, execution.ChatID, execution.ID); err != nil {
			return err
		}
	}
	return m.workTickets.ConfirmExecution(ctx, execution.ID, execution.Status)
}

func (m *service) resolveWorkTicketReference(ctx context.Context, request *pendingMessage) error {
	quoteID := ticketQuoteID(*request)
	if request.ticket.Start && quoteID != "" {
		return status_error.BadRequest
	}
	if quoteID == "" || m.workTickets == nil {
		return nil
	}
	address := request.ticket.Address
	if address.ID == "" && request.entryPoint == WorkbenchEntryPoint {
		address = workticket.Address{Kind: constants.WorkTicketOriginChat, ID: request.chatID}
	}
	if address.ID == "" {
		return nil
	}
	var ticket models.WorkTicket
	var found bool
	var err error
	sourceContinuation := request.entryPoint == MainConversationSourceEntryPoint
	if sourceContinuation && request.ticket.PublicReplyID == "" {
		ticket, found, err = m.workTickets.ForSourceMessage(ctx, address, request.chatID, quoteID)
	} else {
		if address.Kind == constants.WorkTicketOriginChat {
			// 先对账已发布报告，再读取消息关联，覆盖原执行同时结算回执的情况。
			pending, err := m.workTickets.HasPendingOrdinaryReport(ctx, address, quoteID)
			if err != nil {
				return err
			}
			if pending {
				if err := reconcileOrdinaryWorkReports(ctx, m.journal, m.workTickets, address.ID, quoteID); err != nil {
					return err
				}
			}
		}
		ticket, found, err = m.workTickets.ForMessage(ctx, address, quoteID)
	}
	if err != nil {
		return err
	}
	if found {
		request.ticketReference = ticket
	} else if sourceContinuation && request.ticket.PublicReplyID != "" {
		return status_error.WorkTicketAttemptUnavailable
	}
	return nil
}

func ticketQuoteID(request pendingMessage) string {
	if request.ticket.PublicReplyID != "" {
		return request.ticket.PublicReplyID
	}
	return request.replyToMessageID
}

func (m *service) bindWorkTicket(ctx context.Context, state messageChat, request *pendingMessage, row models.Execution) error {
	input, original := request.ticket, request.ticketReference
	if input.ResumeTicket.ID != "" {
		if original.ID != "" || input.Start || input.DelegationID != "" || request.replyToMessageID != "" {
			return status_error.BadRequest
		}
		original = input.ResumeTicket
	}
	if !input.Start && input.DelegationID == "" && original.ID == "" {
		return nil
	}
	if m.workTickets == nil {
		return status_error.AgentOperationForbidden
	}
	address := input.Address
	if address.ID == "" && !state.chat.HiddenFromWakerHistory && state.threadWorkspaceID == "" && request.entryPoint == WorkbenchEntryPoint {
		address = workticket.Address{Kind: constants.WorkTicketOriginChat, ID: state.chat.ID}
		input.OwnerKind, input.OwnerID = "waker", state.chat.WakerID
	}
	internalTicket := input.DelegationID != "" || input.ResumeTicket.ID != ""
	initialEvent := input.EventSubscription != nil && input.Start && request.automaticRunID != "" && request.message.Actor == constants.AutomaticTaskActor
	if address.ID == "" || row.SubmissionID == "" ||
		(request.message.Actor != "user" && !(internalTicket && request.message.Actor == constants.WorkTicketHostActor) && !initialEvent) ||
		request.message.SteerExecutionID != "" || request.message.Workflow != nil ||
		request.workflowCreationSessionID != "" || (request.automaticRunID != "" && !initialEvent) || request.imReplyLinkID != "" ||
		request.parentExecutionID != "" || state.profileOptimization || state.threadWorkspaceID != "" {
		return status_error.BadRequest
	}
	if original.ID != "" && (original.WorkChatID != state.chat.ID || original.OwnerWakerID != state.chat.WakerID || original.Status == constants.WorkTicketStatusStopped) {
		return status_error.WorkTicketCannotContinue
	}
	if address.Kind == constants.WorkTicketOriginMain && input.MainConversation == nil {
		return status_error.AgentOperationForbidden
	}
	if address.Kind != constants.WorkTicketOriginMain {
		input.MainConversation = nil
	}
	projectScope, err := m.workTicketProjectScope(ctx, state)
	if err != nil {
		return err
	}
	messageID := input.PublicMessageID
	if messageID == "" {
		messageID = row.UserMessageID
	}
	admission := workticket.Admission{
		Address: address, TicketID: original.ID, MessageID: messageID, SourceMessageID: row.UserMessageID,
		WorkChatID: state.chat.ID, ExecutionID: row.ID, SubmissionID: row.SubmissionID, SubmissionHash: row.SubmissionHash,
		OwnerKind: input.OwnerKind, OwnerID: input.OwnerID, OwnerWakerID: state.chat.WakerID,
		ProjectID: state.chat.ProjectID, ProjectScope: projectScope, WorkingDirectory: state.workspaceDirectory,
		AllowedDirectories: slices.Clone(state.chat.AdditionalWorkingDirectories), PermissionLevel: row.PermissionLevel,
		OriginReplyDigest: input.OriginReplyDigest, OriginReply: input.OriginReply,
		EventSubscription: input.EventSubscription,
		VerifyScope: func(ctx context.Context) error {
			current, err := m.workTicketProjectScope(ctx, state)
			if err != nil {
				return err
			}
			if current != projectScope {
				return status_error.WorkTicketCannotContinue
			}
			return nil
		},
	}
	transferring := input.ResumeTicket.ID != "" && original.Transferred && original.CurrentExecutionID == "" && original.PendingExecutionID == row.ID
	if input.DelegationID != "" {
		if original.ID != "" || input.Start || request.entryPoint != MainConversationSourceEntryPoint || row.ID != input.DelegationID ||
			row.SubmissionID != input.DelegationID || messageID != input.DelegationID || address.Kind != constants.WorkTicketOriginMain {
			return status_error.BadRequest
		}
		admission.TicketID = input.DelegationID
	}
	if input.EventDelivery != nil {
		admission.EventDeliveryID = input.EventDelivery.ID
		admission.EventPreviousExecutionID = original.CurrentExecutionID
	} else if input.ResumeTicket.ID != "" && !transferring {
		admission.ResumeAfterExecutionID = original.CurrentExecutionID
		admission.DelegationReplyID = input.DelegationReplyID
		admission.DelegationQuestionID = input.DelegationQuestionID
	}
	if request.entryPoint == MainConversationSourceEntryPoint {
		admission.SourceReplyMessageID = request.replyToMessageID
	}
	if original.ID != "" && !transferring {
		if original.ProjectScope != projectScope || original.ProjectID != admission.ProjectID || original.WorkingDirectory != admission.WorkingDirectory ||
			original.PermissionLevel != row.PermissionLevel || !slices.Equal(original.AllowedDirectories, admission.AllowedDirectories) {
			return status_error.WorkTicketCannotContinue
		}
		if err := m.reconcileWorkTicketTurn(ctx, original); err != nil {
			return err
		}
		quote := ticketQuoteID(*request)
		if input.ResumeTicket.ID != "" {
			quote = original.ID
		}
		// 移交后，模型沿用新来源自己的历史。调用方只转交用户明确引用的公开正文。
		if !original.Transferred {
			if err := m.addTicketOriginContext(ctx, state, request, original, quote); err != nil {
				return err
			}
		}
	}
	canWaitForEvent := initialEvent && input.EventSubscription.ChecksAllowed
	if original.ID != "" {
		canWaitForEvent, err = m.workTickets.EventWaitAvailable(ctx, original.ID)
		if err != nil {
			return err
		}
	}
	turn := &workTicketTurn{tickets: m.workTickets, main: input.MainConversation, journal: m.journal,
		resolveTarget: m.resolveDelegationTarget, wake: m.wakeDelegations, initialEvent: initialEvent,
		event: input.EventDelivery, canWaitForEvent: canWaitForEvent, resolveEventRule: m.resolveEventRule}
	request.watchdogTicket = turn
	if m.storage != nil {
		turn.storageRoot = m.storage.Root()
	}
	if request.admission != constants.ExecutionAdmissionCommitAtQueuedNativeGate {
		request.admission = constants.ExecutionAdmissionCommitBeforeDispatch
	}
	previousBoundary := request.admissionBoundary
	request.admissionBoundary = func(ctx context.Context, commit func(context.Context) error) error {
		return previousBoundary.Commit(ctx, func(ctx context.Context) error {
			return turn.admit(ctx, admission, commit)
		})
	}
	request.toolCapabilities = ticketTurnCapabilities(request.toolCapabilities, turn, input.DelegationID == "" && !original.Transferred)
	return nil
}

func (m *service) reconcileWorkTicketTurn(ctx context.Context, ticket models.WorkTicket) error {
	// Core 的 Chat 恢复可以补齐原生终态，只有用户明确续办时才对账这张单的前一轮。
	previous, err := m.journal.ReadExecution(ctx, ticket.WorkChatID, ticket.CurrentExecutionID)
	if err != nil {
		return err
	}
	if previous.ID != ticket.CurrentExecutionID || previous.ChatID != ticket.WorkChatID {
		return status_error.WorkTicketAttemptUnavailable
	}
	switch previous.Status {
	case constants.ExecutionStatusCompleted, constants.ExecutionStatusFailed, constants.ExecutionStatusCanceled:
		if ticket.OriginKind == constants.WorkTicketOriginChat {
			if err := reconcileOrdinaryWorkReports(ctx, m.journal, m.workTickets, previous.ChatID, previous.ID); err != nil {
				return err
			}
		}
		return m.workTickets.ConfirmExecution(ctx, previous.ID, previous.Status)
	default:
		return status_error.WorkTicketCannotContinue
	}
}

func (m *service) workTicketProjectScope(ctx context.Context, state messageChat) (string, error) {
	if !slices.Equal(state.additionalDirectories, state.chat.AdditionalWorkingDirectories) {
		return "", status_error.ProjectSourceUnavailable
	}
	if state.chat.ProjectID == "" {
		return "", nil
	}
	scope, err := readProjectScope(ctx, m.projectController, state.chat.WakerID, state.chat.ProjectID)
	if err != nil {
		return "", err
	}
	if len(scope.directories) != len(state.projectSources) || len(state.additionalDirectories) == 0 {
		return "", status_error.WorkTicketCannotContinue
	}
	for index, dir := range scope.directories {
		frozen := state.projectSources[index]
		if dir.Source.Path != frozen.SourcePath || dir.LoadInstructions != frozen.LoadInstructions ||
			dir.IsDefault != (dir.Source.Path == state.additionalDirectories[0]) {
			return "", status_error.WorkTicketCannotContinue
		}
	}
	return scope.ScopeKey(), nil
}

func (m *service) addTicketOriginContext(ctx context.Context, state messageChat, request *pendingMessage, ticket models.WorkTicket, quoteID string) error {
	if quoteID == ticket.ID {
		return nil
	}
	sourceMessageID, err := m.workTickets.OriginalSourceMessage(ctx, ticket)
	if err != nil {
		return err
	}
	if sourceMessageID == "" || quoteID == sourceMessageID {
		return nil
	}
	message, err := m.journal.ReadWorkTicketReplyTarget(ctx, state.chat.ID, sourceMessageID, state.chat.HiddenFromWakerHistory)
	if util.HasStatusCode(err, status_error.ChatReplyTargetUnavailable) {
		// 附件输入可以作为交办依据，原生会话仍可读取原附件引用。
		return nil
	}
	if err != nil {
		return err
	}
	request.contextInstructions = append(request.contextInstructions, agent.Instruction{Name: "Original work ticket request", Content: message.Text})
	return nil
}

func (turn *workTicketTurn) admit(ctx context.Context, admission workticket.Admission, commit func(context.Context) error) error {
	defer turn.tickets.LockAdmission(admission)()
	binding, err := turn.tickets.PrepareAdmission(ctx, admission)
	if err != nil {
		return err
	}
	turn.binding = binding
	if err := commit(ctx); err != nil {
		// 文件日志为权威接纳事实。只在明确缺失时清理预留，避免投影错误撤回实际接纳。
		settle := context.WithoutCancel(ctx)
		receipt, readErr := turn.journal.ReadSubmission(settle, admission.WorkChatID, admission.SubmissionID)
		if (readErr == nil && receipt == nil) || util.HasStatusCode(readErr, status_error.ChatNotFound) {
			return errors.Join(err, turn.tickets.AbortAdmission(settle, binding))
		}
		return err
	}
	if err := turn.tickets.ConfirmAdmission(context.WithoutCancel(ctx), binding); err != nil {
		return err
	}
	return nil
}

func ticketTurnCapabilities(existing []ToolCapability, turn *workTicketTurn, canPost bool) []ToolCapability {
	existing = slices.Clone(existing)
	for i := range existing {
		if existing[i].ID != constants.ConversationMessagesGuide {
			continue
		}
		bindings := slices.Clone(existing[i].Bindings)
		if canPost && turn.main != nil {
			for j := range bindings {
				if bindings[j].Operation != constants.ConversationMessagePostOperation {
					continue
				}
				post := bindings[j].Call
				bindings[j].Call = func(ctx context.Context, req capability.Request) (capability.Result, error) {
					return turn.callMainPost(ctx, req, post)
				}
			}
		} else {
			bindings = slices.DeleteFunc(bindings, func(binding capability.Binding) bool {
				return binding.Operation == constants.ConversationMessagePostOperation
			})
		}
		existing[i].Bindings = bindings
		existing[i].publishFinal = nil
	}
	tool := ToolCapability{
		ID: constants.WorkTicketCapability, Name: "工单报告", Bindings: append([]capability.Binding{turn.reportBinding()}, turn.delegationBindings()...),
		finishTurn: func(ctx context.Context, status constants.ExecutionStatus) error {
			if turn.main == nil {
				if err := reconcileOrdinaryWorkReports(ctx, turn.journal, turn.tickets, turn.binding.Ticket.WorkChatID, turn.binding.ExecutionID); err != nil {
					return err
				}
			}
			err := turn.tickets.FinishTurn(ctx, turn.binding, status)
			if err == nil && turn.wake != nil {
				turn.wake()
			}
			return err
		},
	}
	if turn.initialEvent {
		tool.Bindings = nil
	}
	if turn.canWaitForEvent {
		tool.Bindings = append(tool.Bindings, turn.eventWaitBinding())
	}
	if !turn.initialEvent && turn.resolveEventRule != nil {
		tool.Bindings = append(tool.Bindings, turn.eventSubscriptionBinding())
	}
	if !turn.initialEvent && (turn.main != nil || !canPost) {
		tool.publishFinal = func(ctx context.Context, answer func(context.Context) (string, error)) error {
			if turn.main != nil {
				return turn.tickets.ReportNativeFinal(ctx, turn.binding, answer, turn.publishMainReport)
			}
			return turn.tickets.ReportNativeFinal(ctx, turn.binding, answer, turn.deliverOrdinary)
		}
	}
	return append(existing, tool)
}

func (turn *workTicketTurn) callMainPost(ctx context.Context, req capability.Request, post func(context.Context, capability.Request) (capability.Result, error)) (result capability.Result, err error) {
	err = turn.tickets.PublishMainMessage(ctx, turn.binding, func(tx context.Context) error {
		var postErr error
		result, postErr = post(tx, req)
		return postErr
	})
	binding := turn.binding
	if !errors.Is(err, status_error.WorkTicketAttemptUnavailable) || binding.ExecutionID == "" ||
		binding.Ticket.OriginKind != constants.WorkTicketOriginMain || binding.Ticket.ParentTicketID != "" {
		return result, err
	}
	// 本轮失去写入资格后仍可确认已经提交的原发言。
	msg, found, readErr := turn.main.Published(ctx, binding.Ticket.WorkChatID, binding.ExecutionID, req.RequestID)
	if readErr != nil {
		return result, readErr
	}
	if !found {
		return result, err
	}
	content, _ := req.Params["content"].(string)
	quote, _ := req.Params["quoteReply"].(bool)
	if msg.Content != content || msg.RequestedQuote() != quote {
		return result, status_error.AgentCommandRequestConflict
	}
	return publicMessageReceipt(msg.ID, true), nil
}

func (turn *workTicketTurn) reportBinding() capability.Binding {
	return capability.Binding{
		Operation: constants.WorkTicketReportOperation,
		Call: func(ctx context.Context, req capability.Request) (capability.Result, error) {
			kind, _ := req.Params["kind"].(string)
			content, _ := req.Params["content"].(string)
			content = strings.TrimSpace(content)
			report, err := turn.tickets.Report(ctx, turn.binding, workticket.ReportInput{RequestID: req.RequestID, Kind: kind, Content: content},
				func(ctx context.Context, ticket models.WorkTicket, receiptID string) (string, error) {
					if turn.main != nil {
						return turn.publishMainReport(ctx, ticket, receiptID, content)
					}
					return turn.deliverOrdinary(ctx, ticket, receiptID, content)
				})
			if util.HasStatusCode(err, status_error.CreateRequestConflict) {
				return capability.Result{}, status_error.AgentCommandRequestConflict
			}
			if err != nil {
				return capability.Result{}, err
			}
			if turn.wake != nil {
				turn.wake()
			}
			return workReportCapabilityReceipt(report), nil
		},
		Receipt: func(ctx context.Context, id string) (capability.Result, error) {
			report, found, err := turn.tickets.ReportReceipt(ctx, turn.binding, id)
			if err != nil {
				return capability.Result{}, err
			}
			if !found {
				return capability.Result{}, status_error.AgentReceiptNotFound
			}
			report.Replayed = true
			return workReportCapabilityReceipt(report), nil
		},
	}
}

func (turn *workTicketTurn) publishMainReport(ctx context.Context, ticket models.WorkTicket, receiptID, content string) (string, error) {
	if ticket.Transferred {
		publisher, ok := turn.main.(transferredWorkPublisher)
		if !ok {
			return "", status_error.AgentOperationForbidden
		}
		message, _, err := publisher.PublishTransferredWorkReport(ctx, ticket, turn.binding.ExecutionID, receiptID, content)
		return message.ID, err
	}
	if turn.event != nil {
		source, err := url.Parse(turn.event.Event.URL)
		if err == nil && source.Scheme == "https" && source.Host == "github.com" && source.User == nil {
			content += "\n\n[GitHub](" + source.String() + ")"
		}
	}
	message, _, err := turn.main.PublishReportWithReceipt(ctx, ticket.WorkChatID, turn.binding.ExecutionID, receiptID, content, ticket.ID)
	if turn.event != nil && (errors.Is(err, status_error.ChatMessageNotFound) || errors.Is(err, status_error.ChatReplyTargetUnavailable)) {
		message, _, err = turn.main.PublishReportWithReceipt(ctx, ticket.WorkChatID, turn.binding.ExecutionID, receiptID, content, "")
	}
	return message.ID, err
}

func workReportCapabilityReceipt(report workticket.ReportReceipt) capability.Result {
	return capability.Result{CommandID: report.ID, Replayed: report.Replayed, Data: map[string]any{"messageId": report.MessageID}}
}

func workTicketReportInstruction(mcp, canPost bool) agent.Instruction {
	command := "botcli work tickets report --request-id <stable-id> --kind <kind> --content <report>"
	if mcp {
		command = "work_tickets_report"
	}
	result := "Report only meaningful progress. For completed, put the full user-facing result and relevant evidence or deliverable links in the report itself. "
	if canPost {
		result = "Use the conversation posting tool for acknowledgments and independent findings. Use report for a change in the ticket's status. " +
			"Send completed once. Include any new conclusion or use the exact text of an already posted conclusion to close the ticket on that message. "
	}
	return agent.Instruction{
		Name: "Work ticket reporting",
		Content: "Handle the explicitly assigned work and its quoted follow-ups. Keep inputs for other work in this chat separate. " +
			"Report through " + command + " with kind progress, waiting, blocked or completed. " +
			result +
			"Use waiting when a user input is needed and blocked when an obstacle prevents progress. " +
			"Explain the next action in your report. Use a new request ID per report and reuse the same ID and content when retrying. " +
			"The report is delivered to the original conversation. After delivering the full result, end without a separate acknowledgment.",
	}
}

func (turn *workTicketTurn) deliverOrdinary(ctx context.Context, ticket models.WorkTicket, receiptID, content string) (string, error) {
	target, err := turn.tickets.OrdinaryReportTarget(ctx, turn.binding.ExecutionID)
	if err != nil {
		return "", err
	}
	if target.ExecutionID != turn.binding.ExecutionID {
		release, err := chatstorage.AcquireReferences(turn.storageRoot, target.WorkChatID)
		if err != nil {
			return "", err
		}
		defer release()
		if err := turn.tickets.PublishOriginReplyForExecution(ctx, turn.binding.ExecutionID, turn.journal.AppendExecutionFacts); err != nil {
			return "", err
		}
	}
	return deliverOrdinaryWorkReport(ctx, turn.journal, target, ticket, turn.binding.ExecutionID, receiptID, content)
}

func deliverOrdinaryWorkReport(ctx context.Context, journal chatservice.Journal, target models.WorkTicketInput, ticket models.WorkTicket, sourceExecutionID, receiptID, content string) (string, error) {
	messageID := target.ExecutionID
	delivery := eventlog.ExecutionDeliveryData{SourceID: receiptID, Summary: content, WorkTicketReport: true}
	if sourceExecutionID != target.ExecutionID {
		messageID = receiptID
		delivery.WorkTicketExecutionID = sourceExecutionID
		delivery.SourceChatID, delivery.SourceWakerID = ticket.WorkChatID, ticket.OwnerWakerID
	}
	reports, err := readOrdinaryWorkReports(journal, target.WorkChatID, target.ExecutionID)
	if err != nil {
		return "", err
	}
	for _, report := range reports {
		if report.ID == receiptID {
			if !report.Matches(target.ExecutionID, delivery.WorkTicketExecutionID, delivery.SourceChatID, delivery.SourceWakerID, content) {
				return "", status_error.AgentCommandRequestConflict
			}
			return report.MessageID, nil
		}
	}
	_, err = journal.AppendExecutionFacts(ctx, target.WorkChatID, []eventlog.AppendInput{{
		Type: constants.ChatEventTypeExecutionDelivery, ExecutionID: target.ExecutionID, MessageID: messageID,
		Data: marshalData(delivery),
	}})
	return messageID, err
}

func reconcileOrdinaryWorkReports(ctx context.Context, journal chatservice.Journal, tickets *workticket.Service, chatID, executionID string) error {
	reports, err := readOrdinaryWorkReports(journal, chatID, executionID)
	if err != nil || len(reports) == 0 {
		return err
	}
	return tickets.ReconcilePublishedReports(ctx, executionID, reports)
}

func readOrdinaryWorkReports(journal chatservice.Journal, chatID, executionID string) ([]workticket.PublishedReport, error) {
	records, err := journal.ReadExecutionRecords(chatID, executionID)
	if err != nil {
		return nil, err
	}
	return workticket.OrdinaryWorkReportReceipts(records, executionID)
}
