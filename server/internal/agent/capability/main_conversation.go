package capability

import "github.com/JieWaZi/NeuxBot/server/constants"

// mainConversationOperations defines the two commands available to a worker with a public addressee.
func mainConversationOperations() []Operation {
	list := operation(constants.ConversationMessagesListOperation,
		"Read recent public messages in the bound conversation, oldest first within each page. Use nextBeforeSequence to read earlier messages.",
		constants.AgentCLIEffectRead, constants.AgentCLICompletionImmediate, constants.ConversationMessagesGuide,
		Field{Name: "beforeSequence", Schema: map[string]any{"type": "integer", "minimum": 0}},
		Field{Name: "limit", Schema: map[string]any{"type": "integer", "minimum": 1, "maximum": 100}})
	message := map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"id", "role", "content", "createdAt"},
		"properties": map[string]any{
			"id": map[string]any{"type": "string"}, "role": map[string]any{"type": "string", "enum": []string{"user", "assistant"}},
			"content": map[string]any{"type": "string"}, "createdAt": map[string]any{"type": "integer"},
			"replyToMessageId": map[string]any{"type": "string"},
		},
	}
	list.OutputSchema = map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"messages", "hasMore"},
		"properties": map[string]any{
			"messages": map[string]any{"type": "array", "items": message},
			"hasMore":  map[string]any{"type": "boolean"}, "nextBeforeSequence": map[string]any{"type": "integer"},
		},
	}

	post := operation(constants.ConversationMessagePostOperation,
		"Publish one complete message to the public conversation bound to this work.",
		constants.AgentCLIEffectMutation, constants.AgentCLICompletionImmediate, constants.ConversationMessagesGuide,
		Field{Name: "content", Required: true, Schema: map[string]any{"type": "string", "minLength": 1, "pattern": `\S`}})
	post.OutputSchema = map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"messageId"},
		"properties": map[string]any{"messageId": map[string]any{"type": "string"}},
	}
	return []Operation{list, post}
}

var mainConversationLabels = map[string]string{
	constants.ConversationMessagesListOperation: "读取主会话消息",
	constants.ConversationMessagePostOperation:  "向主会话发言",
}

// RequiresExecutionID pins public messages and external effects to the turn which originated them.
func (o Operation) RequiresExecutionID() bool {
	return o.Effect == constants.AgentCLIEffectExternal || o.ID == constants.ConversationMessagePostOperation
}
