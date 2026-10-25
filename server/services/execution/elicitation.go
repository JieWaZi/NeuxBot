package execution

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/google/jsonschema-go/jsonschema"
)

// pendingElicitation 保存一次尚未向 Runtime 返回的结构化用户输入交互。
type pendingElicitation struct {
	// resolutionMutex 串行化用户提交与执行取消，保证先获得处理权的一方形成唯一结果。
	resolutionMutex sync.Mutex
	// chatID 是结构化输入所属 Chat 标识。
	chatID string
	// executionID 是结构化输入所属活跃执行标识。
	executionID string
	// requested 是已经安全归一化并进入权威日志的输入内容。
	requested eventlog.ElicitationRequestedData
	// requestedAt 是请求进入权威日志的 Unix 毫秒时间。
	requestedAt int64
	// response 把最终选择交回正在等待的 Runtime 回调。
	response chan runtimes.ElicitationDecision
}

// elicitationCoordinator 在进程内连接 ACP 回调、Chat 权威事实和用户响应。
type elicitationCoordinator struct {
	// events 发布已提交问题状态的内部更新，不创建连接或第二份事实。
	events *events.Hub
	// commitMutex 串行化结构化输入内存状态、权威事件和查询投影的提交顺序。
	commitMutex sync.Mutex
	// mutex 保护 byID 和 orderByChat。
	mutex sync.Mutex
	// byID 保存仍未完成的结构化输入。
	byID map[string]*pendingElicitation
	// orderByChat 保存每个 Chat 的结构化输入到达顺序。
	orderByChat map[string][]string
	// changes 保存当前执行的单消费者等待状态唤醒信号。
	changes map[string]chan struct{}
	// journal 先持久化结构化输入事实，再更新可重建投影并发布安全事件。
	journal chatservice.Journal
}

// executionElicitationHandler 把固定 Chat/Execution 身份绑定到结构化输入协调器。
type executionElicitationHandler struct {
	// coordinator 是进程级结构化输入协调器。
	coordinator *elicitationCoordinator
	// chatID 是当前 Runtime 调用所属 Chat 标识。
	chatID string
	// executionID 是当前 Runtime 调用所属执行标识。
	executionID string
}

// newElicitationCoordinator 创建空的进程内结构化输入协调器。
func newElicitationCoordinator(journal chatservice.Journal, hub ...*events.Hub) *elicitationCoordinator {
	var publisher *events.Hub
	if len(hub) > 0 {
		publisher = hub[0]
	}
	return &elicitationCoordinator{
		byID:        make(map[string]*pendingElicitation),
		orderByChat: make(map[string][]string),
		journal:     journal, events: publisher,
		changes: make(map[string]chan struct{}),
	}
}

// handler 返回只允许当前执行发起请求的 Runtime 结构化输入端口。
func (c *elicitationCoordinator) handler(
	chatID string,
	executionID string,
) runtimes.ElicitationHandler {
	return &executionElicitationHandler{
		coordinator: c,
		chatID:      chatID,
		executionID: executionID,
	}
}

// Request 把 Runtime 请求交给绑定执行的结构化输入协调器。
func (h *executionElicitationHandler) Request(
	ctx context.Context,
	request runtimes.ElicitationRequest,
) (runtimes.ElicitationDecision, error) {
	if h == nil || h.coordinator == nil {
		return runtimes.ElicitationDecision{Cancelled: true}, nil
	}
	return h.coordinator.request(ctx, h.chatID, h.executionID, request)
}

// ResolveElicitation 处理前端提交的一次结构化用户输入。
func (m *service) ResolveElicitation(
	ctx context.Context,
	chatID string,
	elicitationID string,
	content map[string]any,
	declined bool,
) error {
	if m == nil || m.elicitations == nil {
		return status_error.ChatServiceNotStarted
	}
	chatID = strings.TrimSpace(chatID)
	elicitationID = strings.TrimSpace(elicitationID)
	if chatID == "" || elicitationID == "" {
		return status_error.BadRequest
	}
	if declined {
		return m.elicitations.resolveDeclined(ctx, chatID, elicitationID)
	}
	return m.elicitations.resolveAccepted(ctx, chatID, elicitationID, content)
}

func (c *elicitationCoordinator) request(
	ctx context.Context,
	chatID string,
	executionID string,
	request runtimes.ElicitationRequest,
) (runtimes.ElicitationDecision, error) {
	if ctx == nil || strings.TrimSpace(chatID) == "" ||
		strings.TrimSpace(executionID) == "" {
		return runtimes.ElicitationDecision{Cancelled: true}, status_error.BadRequest
	}
	requested := elicitationEventData(util.New(), request)
	pending := &pendingElicitation{
		chatID:      chatID,
		executionID: executionID,
		requested:   requested,
		response:    make(chan runtimes.ElicitationDecision, 1),
	}

	c.commitMutex.Lock()
	record, err := c.journal.AppendElicitationFact(chatID, eventlog.AppendInput{
		Type:        constants.ChatEventTypeElicitationRequested,
		ExecutionID: executionID,
		Data:        marshalData(requested),
	})
	if err != nil {
		c.commitMutex.Unlock()
		return runtimes.ElicitationDecision{Cancelled: true}, err
	}
	c.mutex.Lock()
	pending.requestedAt = record.RecordedAt
	c.byID[requested.ElicitationID] = pending
	c.signalInputLocked(executionID)
	c.orderByChat[chatID] = append(
		c.orderByChat[chatID],
		requested.ElicitationID,
	)
	c.mutex.Unlock()
	c.journal.PublishElicitationFact(ctx, record, c.pendingProjection(chatID))
	c.commitMutex.Unlock()

	select {
	case decision := <-pending.response:
		return decision, nil
	case <-ctx.Done():
		err := c.resolveWithoutContent(
			ctx,
			chatID,
			requested.ElicitationID,
			constants.ElicitationResultInvalidated,
		)
		if err != nil && !errors.Is(err, status_error.ElicitationAlreadyResolved) {
			return runtimes.ElicitationDecision{Cancelled: true}, elicitationContextStatus(ctx.Err())
		}
		select {
		case decision := <-pending.response:
			return decision, nil
		default:
			return runtimes.ElicitationDecision{Cancelled: true}, elicitationContextStatus(ctx.Err())
		}
	}
}

func elicitationContextStatus(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return status_error.OperationTimeout
	}
	return status_error.OperationCanceled
}

func elicitationEventData(
	elicitationID string,
	request runtimes.ElicitationRequest,
) eventlog.ElicitationRequestedData {
	result := eventlog.ElicitationRequestedData{
		ElicitationID: elicitationID,
		Mode:          request.Mode,
		Message:       request.Message,
		URL:           request.URL,
	}
	if len(request.RequestedSchema) > 0 {
		schema, err := json.Marshal(request.RequestedSchema)
		if err == nil {
			result.RequestedSchema = schema
		}
	}
	return result
}

func (c *elicitationCoordinator) resolveAccepted(
	ctx context.Context,
	chatID string,
	elicitationID string,
	content map[string]any,
) error {
	pending, err := c.lockPendingResolution(chatID, elicitationID)
	if err != nil {
		return err
	}
	defer pending.resolutionMutex.Unlock()
	pending, err = c.beginResolution(chatID, elicitationID, true)
	if err != nil {
		return err
	}
	if content == nil {
		content = map[string]any{}
	}
	if err := validateElicitationContent(pending.requested, content); err != nil {
		return err
	}
	return c.commitResolution(ctx, pending, eventlog.ElicitationResolvedData{
		ElicitationID: pending.requested.ElicitationID,
		ToolCallID:    pending.requested.ToolCallID,
		Content:       content,
		Result:        constants.ElicitationResultAccepted,
	}, runtimes.ElicitationDecision{Accepted: true, Content: content})
}

// validateElicitationContent 按请求时冻结的 JSON Schema 校验 form 响应。
func validateElicitationContent(
	requested eventlog.ElicitationRequestedData,
	content map[string]any,
) error {
	if requested.Mode != "form" {
		return nil
	}
	if len(requested.RequestedSchema) == 0 {
		return status_error.BadRequest
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(requested.RequestedSchema, &schema); err != nil {
		return status_error.BadRequest
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return status_error.BadRequest
	}
	if err := resolved.Validate(content); err != nil {
		return status_error.BadRequest
	}
	return nil
}

func (c *elicitationCoordinator) resolveDeclined(
	ctx context.Context,
	chatID string,
	elicitationID string,
) error {
	pending, err := c.lockPendingResolution(chatID, elicitationID)
	if err != nil {
		return err
	}
	defer pending.resolutionMutex.Unlock()
	pending, err = c.beginResolution(chatID, elicitationID, true)
	if err != nil {
		return err
	}
	if pending.requested.AllowSkip != nil && !*pending.requested.AllowSkip {
		return status_error.BadRequest
	}
	return c.commitResolution(ctx, pending, eventlog.ElicitationResolvedData{
		ElicitationID: pending.requested.ElicitationID,
		ToolCallID:    pending.requested.ToolCallID,
		Result:        constants.ElicitationResultDeclined,
	}, runtimes.ElicitationDecision{Declined: true})
}

func (c *elicitationCoordinator) resolveWithoutContent(
	ctx context.Context,
	chatID string,
	elicitationID string,
	result constants.ElicitationResult,
) error {
	pending, err := c.lockPendingResolution(chatID, elicitationID)
	if err != nil {
		return err
	}
	defer pending.resolutionMutex.Unlock()
	pending, err = c.beginResolution(chatID, elicitationID, false)
	if err != nil {
		return err
	}
	return c.commitResolution(ctx, pending, eventlog.ElicitationResolvedData{
		ElicitationID: pending.requested.ElicitationID,
		ToolCallID:    pending.requested.ToolCallID,
		Result:        result,
	}, runtimes.ElicitationDecision{Cancelled: true})
}

func (c *elicitationCoordinator) lockPendingResolution(
	chatID string,
	elicitationID string,
) (*pendingElicitation, error) {
	c.mutex.Lock()
	pending := c.byID[elicitationID]
	if pending == nil || pending.chatID != chatID {
		c.mutex.Unlock()
		return nil, status_error.ElicitationAlreadyResolved
	}
	c.mutex.Unlock()
	pending.resolutionMutex.Lock()
	return pending, nil
}

func (c *elicitationCoordinator) beginResolution(
	chatID string,
	elicitationID string,
	enforceArrivalOrder bool,
) (*pendingElicitation, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	pending := c.byID[elicitationID]
	if pending == nil || pending.chatID != chatID {
		return nil, status_error.ElicitationAlreadyResolved
	}
	order := c.orderByChat[chatID]
	if enforceArrivalOrder && (len(order) == 0 || order[0] != elicitationID) {
		return nil, status_error.BadRequest
	}
	return pending, nil
}

func (c *elicitationCoordinator) commitResolution(
	ctx context.Context,
	pending *pendingElicitation,
	resolved eventlog.ElicitationResolvedData,
	decision runtimes.ElicitationDecision,
) error {
	c.commitMutex.Lock()
	defer c.commitMutex.Unlock()
	record, err := c.journal.AppendElicitationFact(
		pending.chatID,
		eventlog.AppendInput{
			Type:        constants.ChatEventTypeElicitationResolved,
			ExecutionID: pending.executionID,
			Data:        marshalData(resolved),
		},
	)
	if err != nil {
		return err
	}

	c.mutex.Lock()
	delete(c.byID, pending.requested.ElicitationID)
	c.signalInputLocked(pending.executionID)
	order := c.orderByChat[pending.chatID]
	remaining := order[:0]
	for _, currentID := range order {
		if currentID != pending.requested.ElicitationID {
			remaining = append(remaining, currentID)
		}
	}
	if len(remaining) == 0 {
		delete(c.orderByChat, pending.chatID)
	} else {
		c.orderByChat[pending.chatID] = remaining
	}
	c.mutex.Unlock()

	c.journal.PublishElicitationFact(ctx, record, c.pendingProjection(pending.chatID))
	if c.events != nil {
		_ = events.Emit(c.events, "interaction", "interaction.changed", events.Resource{Kind: "execution", ID: pending.executionID}, map[string]any{"questionId": pending.requested.ElicitationID})
	}
	pending.response <- decision
	return nil
}

func (c *elicitationCoordinator) pendingProjection(
	chatID string,
) []models.PendingElicitation {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	order := c.orderByChat[chatID]
	result := make([]models.PendingElicitation, 0, len(order))
	for _, elicitationID := range order {
		pending := c.byID[elicitationID]
		if pending == nil {
			continue
		}
		result = append(result, pendingElicitationProjection(
			pending.requested,
			pending.requestedAt,
		))
	}
	return result
}

func pendingElicitationProjection(
	requested eventlog.ElicitationRequestedData,
	requestedAt int64,
) models.PendingElicitation {
	return models.PendingElicitation{
		AllowSkip:       requested.AllowSkip,
		ElicitationID:   requested.ElicitationID,
		ToolCallID:      requested.ToolCallID,
		Mode:            requested.Mode,
		Message:         requested.Message,
		RequestedSchema: append(json.RawMessage(nil), requested.RequestedSchema...),
		URL:             requested.URL,
		RequestedAt:     requestedAt,
	}
}
