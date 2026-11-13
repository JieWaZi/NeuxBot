package execution

import (
	"context"
	"maps"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/google/jsonschema-go/jsonschema"
)

// wakerInterviewRequest 是伙伴根据概要生成的问题与备选项。
type wakerInterviewRequest struct {
	// Message 是卡片引导语。
	Message string `json:"message"`
	// Questions 是用户需要回答的两到三个多选题。
	Questions []wakerInterviewQuestion `json:"questions"`
}

type wakerInterviewQuestion struct {
	// ID 是该卡片内稳定的答复字段名。
	ID string `json:"id"`
	// Title 是界面显示的问题。
	Title string `json:"title"`
	// Options 是模型生成的可选答案。
	Options []wakerInterviewOption `json:"options"`
}

type wakerInterviewOption struct {
	// ID 是对应机器可读的答复选项。
	ID string `json:"id"`
	// Label 是用户看到的选择。
	Label string `json:"label"`
	// Description 是可选的简短说明。
	Description string `json:"description,omitempty"`
}

const wakerInterviewOtherSuffix = "__other"

func (m *service) wakerInterviewBinding(chat models.Chat, executionID string) capability.Binding {
	questionID := func(requestID string) string {
		return cliQuestionID(executionID, "waker-profile-interview:"+requestID)
	}
	return capability.Binding{Operation: constants.WakerProfileInterviewOperation, Call: func(ctx context.Context, request capability.Request) (capability.Result, error) {
		var input wakerInterviewRequest
		if err := capability.DecodeParams(request.Params, &input); err != nil {
			return capability.Result{}, err
		}
		params, err := wakerInterviewForm(input)
		if err != nil {
			return capability.Result{}, err
		}
		id := questionID(request.RequestID)
		m.onboardingMu.Lock()
		defer m.onboardingMu.Unlock()
		waker, err := m.pendingOnboardingWaker(ctx, chat)
		if err != nil {
			return capability.Result{}, err
		}
		answer, err := m.onboardingInterviewAnswer(waker)
		if err != nil {
			return capability.Result{}, err
		}
		if interview := waker.OnboardingInterview; interview != nil && interview.ID != id {
			if acceptedWakerInterview(answer) || interview.ExecutionID == executionID && answer["status"] == "pending" {
				return capability.Result{}, status_error.WakerOnboardingInterviewPending
			}
		}
		// 先登记位置，只有原日志确认有效回答后才允许提交档案草稿。
		if err := m.wakerController.SetOnboardingInterview(ctx, waker.ID, chat.ID, models.WakerOnboardingInterview{
			ID: id, ExecutionID: executionID,
		}); err != nil {
			return capability.Result{}, err
		}
		answer, replayed, err := m.elicitations.createQuestionWithValidation(ctx, chat.ID, executionID, id,
			capability.Request{RequestID: request.RequestID, Params: params}, validateWakerInterviewForm)
		return wakerInterviewResult(id, answer, replayed), err
	}, Receipt: func(_ context.Context, requestID string) (capability.Result, error) {
		id := questionID(requestID)
		answer, err := m.elicitations.questionSnapshot(chat.ID, executionID, id)
		return wakerInterviewResult(id, answer, true), err
	}}
}

func wakerInterviewResult(id string, answer map[string]any, replayed bool) capability.Result {
	return capability.Result{CommandID: id, Replayed: replayed, Data: map[string]any{"questionId": id, "status": answer["status"]}}
}

func wakerInterviewForm(input wakerInterviewRequest) (map[string]any, error) {
	if len(input.Questions) < 2 || len(input.Questions) > 3 {
		return nil, status_error.AgentInputInvalid
	}
	fields := make(map[string]any, 2*len(input.Questions))
	requiredAnswers := make([]map[string]any, 0, len(input.Questions))
	for _, question := range input.Questions {
		title := strings.TrimSpace(question.Title)
		otherID := question.ID + wakerInterviewOtherSuffix
		if question.ID == "" || strings.HasSuffix(question.ID, wakerInterviewOtherSuffix) || strings.HasSuffix(question.ID, "_custom") ||
			title == "" || fields[question.ID] != nil || fields[otherID] != nil || len(question.Options) < 2 || len(question.Options) > 5 {
			return nil, status_error.AgentInputInvalid
		}
		options := make([]map[string]any, 0, len(question.Options))
		labels, values := map[string]bool{}, map[string]bool{}
		for _, option := range question.Options {
			label := strings.TrimSpace(option.Label)
			if label == "" || labels[label] || option.ID == "" || values[option.ID] ||
				strings.EqualFold(option.ID, "other") || strings.EqualFold(label, "other") || label == "其他" || label == "其它" {
				return nil, status_error.AgentInputInvalid
			}
			labels[label], values[option.ID] = true, true
			options = append(options, map[string]any{"const": option.ID, "title": label, "description": option.Description})
		}
		fields[question.ID] = map[string]any{
			"type": "array", "title": title, "minItems": 1, "maxItems": len(options),
			"uniqueItems": true, "items": map[string]any{"type": "string", "anyOf": options},
		}
		fields[otherID] = map[string]any{"type": "string", "minLength": 1, "maxLength": 1000, "pattern": `\S`}
		requiredAnswers = append(requiredAnswers, map[string]any{"anyOf": []map[string]any{
			{"required": []string{question.ID}}, {"required": []string{otherID}},
		}})
	}
	return map[string]any{
		"message": input.Message, "allowSkip": false,
		"requestedSchema": map[string]any{"type": "object", "additionalProperties": false, "properties": fields, "allOf": requiredAnswers},
	}, nil
}

func validateWakerInterviewForm(schema *jsonschema.Schema) error {
	// Other 的受信字段由 Core 生成，主问题仍沿用普通交互字段的呈现约束。
	main := *schema
	main.Properties = maps.Clone(schema.Properties)
	for field := range main.Properties {
		if strings.HasSuffix(field, wakerInterviewOtherSuffix) {
			delete(main.Properties, field)
		}
	}
	return validateQuestionForm(&main)
}

func (m *service) onboardingInterviewAnswer(waker models.Waker) (map[string]any, error) {
	interview := waker.OnboardingInterview
	if interview == nil {
		return nil, nil
	}
	answer, err := m.elicitations.questionSnapshot(waker.OnboardingChatID, interview.ExecutionID, interview.ID)
	if util.HasStatusCode(err, status_error.AgentReceiptNotFound) || util.HasStatusCode(err, status_error.ExecutionNotFound) || util.HasStatusCode(err, status_error.ChatNotFound) {
		return nil, nil
	}
	return answer, err
}

func acceptedWakerInterview(answer map[string]any) bool {
	content, _ := answer["content"].(map[string]any)
	// 提交时已按原请求冻结的 Schema 校验，接受旧单选和新多选结果。
	return answer["status"] == string(constants.ElicitationResultAccepted) && len(content) > 0
}

func (m *service) checkWakerOnboardingProgress(ctx context.Context, chat models.Chat, finished bool) error {
	waker, err := m.wakerController.Get(ctx, chat.WakerID)
	if err != nil || waker.OnboardingStatus != constants.WakerOnboardingStatusPending {
		return err
	}
	answer, err := m.onboardingInterviewAnswer(waker)
	if err != nil {
		return err
	}
	if !acceptedWakerInterview(answer) {
		return status_error.WakerOnboardingInterviewRequired
	}
	if finished {
		return status_error.WakerOnboardingIncomplete
	}
	return nil
}
