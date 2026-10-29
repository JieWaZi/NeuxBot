package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
	"github.com/google/jsonschema-go/jsonschema"
)

// executionInputState 将真实未决问题连接到 Runtime 无活动策略。
type executionInputState struct {
	// coordinator 拥有当前未决输入事实。
	coordinator *elicitationCoordinator
	// executionID 是唯一允许观察的执行。
	executionID string
	// changes 只为该执行合并状态唤醒。
	changes <-chan struct{}
}

func (s executionInputState) Pending() bool {
	s.coordinator.mutex.Lock()
	defer s.coordinator.mutex.Unlock()
	for _, question := range s.coordinator.byID {
		if question.executionID == s.executionID {
			return true
		}
	}
	return false
}

func (s executionInputState) Changed() <-chan struct{} { return s.changes }

func (c *elicitationCoordinator) inputState(executionID string) runtimecontract.UserInputState {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.changes == nil {
		c.changes = make(map[string]chan struct{})
	}
	if c.changes[executionID] == nil {
		c.changes[executionID] = make(chan struct{}, 1)
	}
	return executionInputState{coordinator: c, executionID: executionID, changes: c.changes[executionID]}
}

func (c *elicitationCoordinator) signalInputLocked(executionID string) {
	if channel := c.changes[executionID]; channel != nil {
		select {
		case channel <- struct{}{}:
		default:
		}
	}
}

func (c *elicitationCoordinator) finishQuestions(chatID, executionID string) {
	c.commitMutex.Lock()
	c.mutex.Lock()
	ids := []string{}
	for id, pending := range c.byID {
		if pending.chatID == chatID && pending.executionID == executionID && pending.requested.CommandRequestID != "" {
			ids = append(ids, id)
		}
	}
	c.mutex.Unlock()
	c.commitMutex.Unlock()
	for _, id := range ids {
		if err := c.resolveWithoutContent(context.Background(), chatID, id, constants.ElicitationResultInvalidated); err != nil {
			logx.Errorf("invalidate CLI question failed: chat_id=%s execution_id=%s question_id=%s error=%v", chatID, executionID, id, err)
		}
	}
	c.mutex.Lock()
	delete(c.changes, executionID)
	c.mutex.Unlock()
}

func (m *service) questionBindings(chatID, executionID string, facts ...*executionMessageFacts) []agentcapability.Binding {
	var messageFacts *executionMessageFacts
	if len(facts) > 0 {
		messageFacts = facts[0]
	}
	questionID := func(requestID string) string { return agentIdentity(executionID + ":question:" + requestID)[:32] }
	read := func(id string) (map[string]any, error) {
		return m.elicitations.questionSnapshot(chatID, executionID, id)
	}
	bindings := []agentcapability.Binding{{Operation: "interaction.questions.create", Call: func(ctx context.Context, request agentcapability.Request) (agentcapability.Result, error) {
		id := questionID(request.RequestID)
		result, replayed, err := m.elicitations.createQuestion(ctx, chatID, executionID, id, request, messageFacts)
		return agentcapability.Result{CommandID: id, Replayed: replayed, Data: result}, err
	}, Receipt: func(_ context.Context, requestID string) (agentcapability.Result, error) {
		id := questionID(requestID)
		result, err := read(id)
		return agentcapability.Result{CommandID: id, Replayed: true, Data: result}, err
	}}, {Operation: "interaction.questions.get", Call: func(_ context.Context, request agentcapability.Request) (agentcapability.Result, error) {
		id, _ := request.Params["questionId"].(string)
		result, err := read(id)
		return agentcapability.Result{Data: result}, err
	}}}
	if m.events != nil {
		bindings = append(bindings, agentcapability.Binding{Operation: "interaction.questions.wait", Call: func(ctx context.Context, request agentcapability.Request) (agentcapability.Result, error) {
			id, _ := request.Params["questionId"].(string)
			return agentcapability.Observe(ctx, request.Params, func() events.Subscription {
				return m.events.Subscribe("interaction", events.Resource{Kind: "execution", ID: executionID})
			}, func(context.Context) (any, bool, error) {
				value, err := read(id)
				return value, value["status"] != "pending", err
			})
		}})
	}
	return bindings
}

func (c *elicitationCoordinator) questionSnapshot(chatID, executionID, questionID string) (map[string]any, error) {
	records, err := c.journal.ReadRecords(chatID)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	for _, record := range records {
		if record.ExecutionID != executionID {
			continue
		}
		switch record.Type {
		case constants.ChatEventTypeElicitationRequested:
			var value eventlog.ElicitationRequestedData
			if json.Unmarshal(record.Data, &value) == nil && value.ElicitationID == questionID && value.CommandRequestID != "" {
				result = map[string]any{"questionId": questionID, "status": "pending", "message": value.Message, "requestedSchema": value.RequestedSchema, "allowSkip": value.AllowSkip}
			}
		case constants.ChatEventTypeElicitationResolved:
			var value eventlog.ElicitationResolvedData
			if json.Unmarshal(record.Data, &value) == nil && value.ElicitationID == questionID && result != nil {
				result["status"], result["content"] = string(value.Result), value.Content
			}
		}
	}
	if result == nil {
		return nil, status_error.AgentReceiptNotFound
	}
	return result, nil
}

func (c *elicitationCoordinator) createQuestion(ctx context.Context, chatID, executionID, id string, request agentcapability.Request, facts ...*executionMessageFacts) (map[string]any, bool, error) {
	var messageFacts *executionMessageFacts
	if len(facts) > 0 {
		messageFacts = facts[0]
	}
	var input struct {
		// Message 是用户可以理解的决定说明。
		Message string `json:"message"`
		// RequestedSchema 固定问题的输入结构。
		RequestedSchema map[string]any `json:"requestedSchema"`
		// AllowSkip 明确用户是否可以跳过该问题。
		AllowSkip bool `json:"allowSkip"`
	}
	if err := agentcapability.DecodeParams(request.Params, &input); err != nil {
		return nil, false, err
	}
	encoded, err := json.Marshal(input.RequestedSchema)
	if err != nil || len(encoded) > 65536 || len(input.Message) > 4096 || strings.TrimSpace(input.Message) == "" || input.RequestedSchema["type"] != "object" {
		return nil, false, status_error.AgentInputInvalid
	}
	var schema jsonschema.Schema
	if json.Unmarshal(encoded, &schema) != nil {
		return nil, false, status_error.AgentInputInvalid
	}
	if _, err := schema.Resolve(nil); err != nil {
		return nil, false, status_error.AgentInputInvalid
	}
	if err := validateQuestionForm(&schema); err != nil {
		return nil, false, err
	}
	body, _ := json.Marshal(request.Params)
	digest := sha256.Sum256(body)
	hash := hex.EncodeToString(digest[:])
	c.commitMutex.Lock()
	defer c.commitMutex.Unlock()
	if ctx.Err() != nil {
		return nil, false, status_error.AgentContextUnauthorized
	}
	records, err := c.journal.ReadRecords(chatID)
	if err != nil {
		return nil, false, err
	}
	for _, record := range records {
		if record.Type != constants.ChatEventTypeElicitationRequested || record.ExecutionID != executionID {
			continue
		}
		var value eventlog.ElicitationRequestedData
		if json.Unmarshal(record.Data, &value) == nil && value.CommandRequestID == request.RequestID {
			if value.CommandDigest != hash {
				return nil, false, status_error.AgentCommandRequestConflict
			}
			result, err := c.questionSnapshot(chatID, executionID, value.ElicitationID)
			return result, true, err
		}
	}
	c.mutex.Lock()
	count := 0
	for _, pending := range c.byID {
		if pending.executionID == executionID {
			count++
		}
	}
	c.mutex.Unlock()
	if count >= 8 {
		return nil, false, status_error.AgentInputInvalid
	}
	requested := eventlog.ElicitationRequestedData{ElicitationID: id, CommandRequestID: request.RequestID, CommandDigest: hash, AllowSkip: &input.AllowSkip, Mode: "form", Message: input.Message, RequestedSchema: encoded}
	record, err := c.journal.AppendElicitationFact(chatID, eventlog.AppendInput{Type: constants.ChatEventTypeElicitationRequested, ExecutionID: executionID, InvocationID: messageFacts.invocation(), Data: marshalData(requested)})
	if err != nil {
		return nil, false, err
	}
	messageFacts.push(record)
	c.mutex.Lock()
	c.byID[id] = &pendingElicitation{chatID: chatID, executionID: executionID, messageFacts: messageFacts, invocationID: record.InvocationID, requested: requested, requestedAt: record.RecordedAt, response: make(chan runtimecontract.ElicitationDecision, 1)}
	c.orderByChat[chatID] = append(c.orderByChat[chatID], id)
	c.signalInputLocked(executionID)
	c.mutex.Unlock()
	c.journal.PublishElicitationFact(ctx, record, c.pendingProjection(chatID))
	if c.events != nil {
		_ = events.Emit(c.events, "interaction", "interaction.changed", events.Resource{Kind: "execution", ID: executionID}, map[string]any{"questionId": id})
	}
	result, err := c.questionSnapshot(chatID, executionID, id)
	return result, false, err
}

// validateQuestionForm 拒绝共享问答组件无法呈现或完成的表单。
func validateQuestionForm(schema *jsonschema.Schema) error {
	for _, key := range schema.Required {
		if schema.Properties[key] == nil {
			return status_error.AgentInputInvalid
		}
	}
	for key, field := range schema.Properties {
		if field == nil || strings.TrimSpace(key) == "" || strings.HasSuffix(key, "_custom") || strings.HasSuffix(key, "__other") {
			return status_error.AgentInputInvalid
		}
		choices := field
		switch field.Type {
		case "array":
			if field.Items == nil || len(field.Enum) > 0 || len(field.OneOf) > 0 {
				return status_error.AgentInputInvalid
			}
			choices = field.Items
			if len(choices.Enum) == 0 && len(choices.AnyOf) == 0 {
				return status_error.AgentInputInvalid
			}
		case "string":
			if field.Items != nil {
				return status_error.AgentInputInvalid
			}
		default:
			return status_error.AgentInputInvalid
		}
		if choices.Enum != nil && len(choices.Enum) == 0 || choices.MinLength != nil && choices.MaxLength != nil && *choices.MinLength > *choices.MaxLength {
			return status_error.AgentInputInvalid
		}
		options := append(append([]*jsonschema.Schema(nil), choices.OneOf...), choices.AnyOf...)
		if len(choices.Enum) > 0 && len(options) > 0 {
			return status_error.AgentInputInvalid
		}
		seen := make(map[string]bool)
		values := append([]any(nil), choices.Enum...)
		resolved, err := choices.Resolve(nil)
		if err != nil {
			return status_error.AgentInputInvalid
		}
		for _, option := range options {
			if option == nil || option.Const == nil {
				return status_error.AgentInputInvalid
			}
			values = append(values, *option.Const)
		}
		for _, value := range values {
			text, ok := value.(string)
			if !ok || strings.TrimSpace(text) == "" || seen[text] || resolved.Validate(value) != nil {
				return status_error.AgentInputInvalid
			}
			seen[text] = true
		}
	}
	return nil
}
