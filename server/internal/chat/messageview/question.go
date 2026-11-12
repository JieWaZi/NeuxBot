package messageview

import (
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

// ProjectQuestionMessage 按问题创建事件定位时间线卡片，并以同一请求的结果或执行终态冻结它。
func ProjectQuestionMessage(request, outcome eventlog.Record) (Message, error) {
	var requested eventlog.ElicitationRequestedData
	if err := json.Unmarshal(request.Data, &requested); err != nil || requested.ElicitationID == "" {
		return Message{}, status_error.ChatLogCorrupted
	}
	question := &MessageQuestion{
		ElicitationID: requested.ElicitationID, Mode: requested.Mode, Message: requested.Message,
		URL: requested.URL, AllowSkip: requested.AllowSkip,
	}
	if requested.Mode == "form" && len(requested.RequestedSchema) > 0 {
		var schema map[string]any
		if json.Unmarshal(requested.RequestedSchema, &schema) == nil {
			question.RequestedSchema = schema
		}
	}
	message := Message{
		ID: requested.ElicitationID, Role: constants.MessageRoleAssistant, ExecutionID: request.ExecutionID,
		Question: question, CreatedSequence: request.Sequence, Revision: request.Sequence,
		CreatedAt: request.RecordedAt, UpdatedAt: request.RecordedAt,
	}
	switch outcome.Type {
	case constants.ChatEventTypeElicitationResolved:
		var resolved eventlog.ElicitationResolvedData
		if json.Unmarshal(outcome.Data, &resolved) != nil || resolved.ElicitationID != requested.ElicitationID {
			return Message{}, status_error.ChatLogCorrupted
		}
		question.Result = resolved.Result
		if resolved.Result == constants.ElicitationResultAccepted && requested.Mode == "form" {
			question.Answers = publicQuestionAnswers(requested.RequestedSchema, resolved.Content)
		}
	case constants.ChatEventTypeExecutionCompleted, constants.ChatEventTypeExecutionFailed, constants.ChatEventTypeExecutionCanceled:
		question.Result = constants.ElicitationResultInvalidated
	}
	if question.Result != "" {
		question.URL = ""
		message.Revision, message.UpdatedAt = outcome.Sequence, outcome.RecordedAt
	}
	return message, nil
}

// questionAnswerField 声明可公开值及提供方对保密字段的标记。
type questionAnswerField struct {
	// Format 保存 JSON Schema 的特殊格式。
	Format string `json:"format"`
	// WriteOnly 标记仅供提交的值。
	WriteOnly bool `json:"writeOnly"`
	// Meta 保存原生适配器提供的保密和自定义选项元数据。
	Meta struct {
		// Codex 描述原生保密字段或自定义输入字段。
		Codex struct {
			// IsSecret 标记 Codex 用户输入保密。
			IsSecret bool `json:"isSecret"`
			// IsOtherAnswer 标记该字段为原问题的自定义答案。
			IsOtherAnswer bool `json:"isOtherAnswer"`
			// QuestionID 标识自定义答案所属的问题。
			QuestionID string `json:"questionId"`
		} `json:"codex"`
		// Custom 标识 AskUserQuestion 的自定义答案所属的问题。
		Custom struct {
			// QuestionID 标识自定义答案所属的问题。
			QuestionID string `json:"questionId"`
		} `json:"_askUserQuestionCustomAnswer"`
	} `json:"_meta"`
	// Items 是多选值的项目定义。
	Items *questionAnswerField `json:"items"`
	// OneOf 是字段可能匹配的定义。
	OneOf []questionAnswerField `json:"oneOf"`
	// AnyOf 是字段可选的定义。
	AnyOf []questionAnswerField `json:"anyOf"`
	// AllOf 是字段额外应用的定义。
	AllOf []questionAnswerField `json:"allOf"`
	// Ref 是当前投影无法安全展开的引用。
	Ref string `json:"$ref"`
}

func (field questionAnswerField) secret() bool {
	if field.WriteOnly || field.Meta.Codex.IsSecret || field.Ref != "" ||
		strings.EqualFold(field.Format, "password") || strings.EqualFold(field.Format, "secret") {
		return true
	}
	if field.Items != nil && field.Items.secret() {
		return true
	}
	for _, alternatives := range [][]questionAnswerField{field.OneOf, field.AnyOf, field.AllOf} {
		for _, alternative := range alternatives {
			if alternative.secret() {
				return true
			}
		}
	}
	return false
}

func publicQuestionAnswers(schema json.RawMessage, submitted map[string]any) map[string]any {
	if len(submitted) == 0 {
		return nil
	}
	var form struct {
		// Properties 是表单明确请求的字段。
		Properties map[string]*questionAnswerField `json:"properties"`
	}
	if json.Unmarshal(schema, &form) != nil {
		return nil
	}
	answers := make(map[string]any)
	for key, answer := range submitted {
		field := form.Properties[key]
		if field == nil || field.secret() {
			continue
		}
		original := strings.TrimSuffix(strings.TrimSuffix(key, "_custom"), "__other")
		switch {
		case field.Meta.Codex.IsOtherAnswer:
			original = field.Meta.Codex.QuestionID
		case field.Meta.Custom.QuestionID != "":
			original = field.Meta.Custom.QuestionID
		}
		if original != key {
			if base := form.Properties[original]; base == nil || base.secret() {
				continue
			}
		}
		switch value := answer.(type) {
		case []any:
			safe := true
			for _, item := range value {
				if !isPublicQuestionScalar(item) {
					safe = false
					break
				}
			}
			if safe {
				answers[key] = value
			}
		default:
			if isPublicQuestionScalar(value) {
				answers[key] = value
			}
		}
	}
	return answers
}

func isPublicQuestionScalar(value any) bool {
	switch value.(type) {
	case string, bool, float64:
		return true
	default:
		return false
	}
}
