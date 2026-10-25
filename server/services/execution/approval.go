package execution

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// pendingApproval 保存一次尚未向 Runtime 返回的审批交互。
type pendingApproval struct {
	// resolutionMutex 串行化用户选择与执行取消，保证先获得处理权的一方形成唯一结果。
	resolutionMutex sync.Mutex
	// chatID 是审批所属 Chat 标识。
	chatID string
	// executionID 是审批所属活跃执行标识。
	executionID string
	// requested 是已经安全归一化并进入权威日志的审批内容。
	requested eventlog.ApprovalRequestedData
	// requestedAt 是请求进入权威日志的 Unix 毫秒时间。
	requestedAt int64
	// response 把最终选择交回正在等待的 Runtime 回调。
	response chan runtimes.ApprovalDecision
}

// approvalCoordinator 在进程内连接 ACP 回调、Chat 权威事实和用户响应。
type approvalCoordinator struct {
	// commitMutex 串行化审批内存状态、权威事件和查询投影的提交顺序。
	commitMutex sync.Mutex
	// mutex 保护 byID 和 orderByChat。
	mutex sync.Mutex
	// byID 保存仍未完成的审批。
	byID map[string]*pendingApproval
	// orderByChat 保存每个 Chat 的审批到达顺序。
	orderByChat map[string][]string
	// journal 先持久化审批事实，再更新可重建投影并发布安全事件。
	journal chatservice.Journal
}

// executionApprovalHandler 把固定 Chat/Execution 身份绑定到审批协调器。
type executionApprovalHandler struct {
	// coordinator 是进程级审批协调器。
	coordinator *approvalCoordinator
	// chatID 是当前 Runtime 调用所属 Chat 标识。
	chatID string
	// executionID 是当前 Runtime 调用所属执行标识。
	executionID string
}

// newApprovalCoordinator 创建空的进程内审批协调器。
func newApprovalCoordinator(journal chatservice.Journal) *approvalCoordinator {
	return &approvalCoordinator{
		byID:        make(map[string]*pendingApproval),
		orderByChat: make(map[string][]string),
		journal:     journal,
	}
}

// handler 返回只允许当前执行发起请求的 Runtime 审批端口。
func (c *approvalCoordinator) handler(
	chatID string,
	executionID string,
) runtimes.ApprovalHandler {
	return &executionApprovalHandler{
		coordinator: c,
		chatID:      chatID,
		executionID: executionID,
	}
}

// Request 把 Runtime 请求交给绑定执行的审批协调器。
func (h *executionApprovalHandler) Request(
	ctx context.Context,
	request runtimes.ApprovalRequest,
) (runtimes.ApprovalDecision, error) {
	if h == nil || h.coordinator == nil {
		return runtimes.ApprovalDecision{Cancelled: true}, nil
	}
	return h.coordinator.request(ctx, h.chatID, h.executionID, request)
}

// ResolveApproval 处理前端提交的一次审批选择。
func (m *service) ResolveApproval(
	ctx context.Context,
	chatID string,
	approvalID string,
	optionID string,
) error {
	if m == nil || m.approvals == nil {
		return status_error.ChatServiceNotStarted
	}
	chatID = strings.TrimSpace(chatID)
	approvalID = strings.TrimSpace(approvalID)
	optionID = strings.TrimSpace(optionID)
	if chatID == "" || approvalID == "" || optionID == "" {
		return status_error.BadRequest
	}
	return m.approvals.resolveSelected(ctx, chatID, approvalID, optionID)
}

func (c *approvalCoordinator) request(
	ctx context.Context,
	chatID string,
	executionID string,
	request runtimes.ApprovalRequest,
) (runtimes.ApprovalDecision, error) {
	if ctx == nil || strings.TrimSpace(chatID) == "" ||
		strings.TrimSpace(executionID) == "" {
		return runtimes.ApprovalDecision{Cancelled: true}, status_error.BadRequest
	}
	requested := approvalEventData(util.New(), request)
	pending := &pendingApproval{
		chatID:      chatID,
		executionID: executionID,
		requested:   requested,
		response:    make(chan runtimes.ApprovalDecision, 1),
	}

	c.commitMutex.Lock()
	record, err := c.journal.AppendApprovalFact(chatID, eventlog.AppendInput{
		Type:        constants.ChatEventTypeApprovalRequested,
		ExecutionID: executionID,
		Data:        marshalData(requested),
	})
	if err != nil {
		c.commitMutex.Unlock()
		return runtimes.ApprovalDecision{Cancelled: true}, err
	}
	c.mutex.Lock()
	pending.requestedAt = record.RecordedAt
	c.byID[requested.ApprovalID] = pending
	c.orderByChat[chatID] = append(
		c.orderByChat[chatID],
		requested.ApprovalID,
	)
	c.mutex.Unlock()
	c.journal.PublishApprovalFact(ctx, record, c.pendingProjection(chatID))
	c.commitMutex.Unlock()

	select {
	case decision := <-pending.response:
		return decision, nil
	case <-ctx.Done():
		err := c.resolveWithoutOption(
			ctx,
			chatID,
			requested.ApprovalID,
			constants.ApprovalResultInvalidated,
		)
		if err != nil && !errors.Is(err, status_error.ApprovalAlreadyResolved) {
			return runtimes.ApprovalDecision{Cancelled: true}, approvalContextStatus(ctx.Err())
		}
		select {
		case decision := <-pending.response:
			return decision, nil
		default:
			return runtimes.ApprovalDecision{Cancelled: true}, approvalContextStatus(ctx.Err())
		}
	}
}

// approvalContextStatus 把审批等待的 Context 终态收敛为稳定状态错误。
func approvalContextStatus(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return status_error.OperationTimeout
	}
	return status_error.OperationCanceled
}

func approvalEventData(
	approvalID string,
	request runtimes.ApprovalRequest,
) eventlog.ApprovalRequestedData {
	locations := make([]eventlog.ApprovalLocationData, 0, len(request.Locations))
	for _, location := range request.Locations {
		var line *int
		if location.Line != nil {
			lineValue := *location.Line
			line = &lineValue
		}
		locations = append(locations, eventlog.ApprovalLocationData{
			Path: location.Path,
			Line: line,
		})
	}
	options := make([]eventlog.ApprovalOptionData, 0, len(request.Options))
	for _, option := range request.Options {
		options = append(options, eventlog.ApprovalOptionData{
			ID: option.ID, Name: option.Name, Kind: option.Kind,
		})
	}
	return eventlog.ApprovalRequestedData{
		ApprovalID: approvalID,
		ToolCallID: request.ToolCallID,
		Title:      request.Title,
		Kind:       request.Kind,
		Summary:    request.Summary,
		Command:    request.Command,
		Locations:  locations,
		Options:    options,
	}
}

func (c *approvalCoordinator) resolveSelected(
	ctx context.Context,
	chatID string,
	approvalID string,
	optionID string,
) error {
	pending, err := c.lockPendingResolution(chatID, approvalID)
	if err != nil {
		return err
	}
	defer pending.resolutionMutex.Unlock()
	pending, option, err := c.beginResolution(chatID, approvalID, optionID, true)
	if err != nil {
		return err
	}
	return c.commitResolution(ctx, pending, eventlog.ApprovalResolvedData{
		ApprovalID: pending.requested.ApprovalID,
		ToolCallID: pending.requested.ToolCallID,
		OptionID:   option.ID,
		OptionName: option.Name,
		OptionKind: option.Kind,
		Command:    pending.requested.Command,
		Result:     constants.ApprovalResultSelected,
	}, runtimes.ApprovalDecision{OptionID: option.ID})
}

func (c *approvalCoordinator) resolveWithoutOption(
	ctx context.Context,
	chatID string,
	approvalID string,
	result constants.ApprovalResult,
) error {
	pending, err := c.lockPendingResolution(chatID, approvalID)
	if err != nil {
		return err
	}
	defer pending.resolutionMutex.Unlock()
	pending, _, err = c.beginResolution(chatID, approvalID, "", false)
	if err != nil {
		return err
	}
	return c.commitResolution(ctx, pending, eventlog.ApprovalResolvedData{
		ApprovalID: pending.requested.ApprovalID,
		ToolCallID: pending.requested.ToolCallID,
		Command:    pending.requested.Command,
		Result:     result,
	}, runtimes.ApprovalDecision{Cancelled: true})
}

func (c *approvalCoordinator) lockPendingResolution(
	chatID string,
	approvalID string,
) (*pendingApproval, error) {
	c.mutex.Lock()
	pending := c.byID[approvalID]
	if pending == nil || pending.chatID != chatID {
		c.mutex.Unlock()
		return nil, status_error.ApprovalAlreadyResolved
	}
	c.mutex.Unlock()
	pending.resolutionMutex.Lock()
	return pending, nil
}

func (c *approvalCoordinator) beginResolution(
	chatID string,
	approvalID string,
	optionID string,
	enforceArrivalOrder bool,
) (*pendingApproval, eventlog.ApprovalOptionData, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	pending := c.byID[approvalID]
	if pending == nil || pending.chatID != chatID {
		return nil, eventlog.ApprovalOptionData{}, status_error.ApprovalAlreadyResolved
	}
	order := c.orderByChat[chatID]
	if enforceArrivalOrder && (len(order) == 0 || order[0] != approvalID) {
		return nil, eventlog.ApprovalOptionData{}, status_error.BadRequest
	}
	var selected eventlog.ApprovalOptionData
	if optionID != "" {
		for _, option := range pending.requested.Options {
			if option.ID == optionID {
				selected = option
				break
			}
		}
		if selected.ID == "" {
			return nil, eventlog.ApprovalOptionData{}, status_error.BadRequest
		}
	}
	return pending, selected, nil
}

func (c *approvalCoordinator) commitResolution(
	ctx context.Context,
	pending *pendingApproval,
	resolved eventlog.ApprovalResolvedData,
	decision runtimes.ApprovalDecision,
) error {
	c.commitMutex.Lock()
	defer c.commitMutex.Unlock()
	record, err := c.journal.AppendApprovalFact(
		pending.chatID,
		eventlog.AppendInput{
			Type:        constants.ChatEventTypeApprovalResolved,
			ExecutionID: pending.executionID,
			Data:        marshalData(resolved),
		},
	)
	if err != nil {
		return err
	}

	c.mutex.Lock()
	delete(c.byID, pending.requested.ApprovalID)
	order := c.orderByChat[pending.chatID]
	remaining := order[:0]
	for _, approvalID := range order {
		if approvalID != pending.requested.ApprovalID {
			remaining = append(remaining, approvalID)
		}
	}
	if len(remaining) == 0 {
		delete(c.orderByChat, pending.chatID)
	} else {
		c.orderByChat[pending.chatID] = remaining
	}
	c.mutex.Unlock()

	c.journal.PublishApprovalFact(ctx, record, c.pendingProjection(pending.chatID))
	pending.response <- decision
	return nil
}

func (c *approvalCoordinator) pendingProjection(
	chatID string,
) []models.PendingApproval {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	order := c.orderByChat[chatID]
	result := make([]models.PendingApproval, 0, len(order))
	for _, approvalID := range order {
		pending := c.byID[approvalID]
		if pending == nil {
			continue
		}
		result = append(result, pendingApprovalProjection(
			pending.requested,
			pending.requestedAt,
		))
	}
	return result
}

func pendingApprovalProjection(
	requested eventlog.ApprovalRequestedData,
	requestedAt int64,
) models.PendingApproval {
	locations := make([]models.PendingApprovalLocation, 0, len(requested.Locations))
	for _, location := range requested.Locations {
		var line *int
		if location.Line != nil {
			lineValue := *location.Line
			line = &lineValue
		}
		locations = append(locations, models.PendingApprovalLocation{
			Path: location.Path,
			Line: line,
		})
	}
	options := make([]models.PendingApprovalOption, 0, len(requested.Options))
	for _, option := range requested.Options {
		options = append(options, models.PendingApprovalOption{
			ID: option.ID, Name: option.Name, Kind: option.Kind,
		})
	}
	return models.PendingApproval{
		ApprovalID:  requested.ApprovalID,
		ToolCallID:  requested.ToolCallID,
		Title:       requested.Title,
		Kind:        requested.Kind,
		Summary:     requested.Summary,
		Command:     requested.Command,
		Locations:   locations,
		Options:     options,
		RequestedAt: requestedAt,
	}
}
