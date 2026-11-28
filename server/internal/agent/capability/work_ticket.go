package capability

import "github.com/JieWaZi/NeuxBot/server/constants"

// workTicketReportOperation describes the result mutation granted to a ticket worker.
func workTicketReportOperation() Operation {
	report := operation(constants.WorkTicketReportOperation,
		"Report progress, a wait, a blocker or a completed result to the original conversation for this work ticket.",
		constants.AgentCLIEffectMutation, constants.AgentCLICompletionImmediate, "",
		Field{Name: "kind", Required: true, Schema: map[string]any{"type": "string", "enum": []string{constants.WorkTicketReportKindProgress, string(constants.WorkTicketStatusWaiting), string(constants.WorkTicketStatusBlocked), string(constants.WorkTicketStatusCompleted)}}},
		Field{Name: "content", Required: true, Schema: map[string]any{"type": "string", "minLength": 1, "maxLength": 16000, "pattern": `\S`}})
	report.ScopeKind = constants.WorkTicketCapability
	report.OutputSchema = map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"messageId"},
		"properties": map[string]any{"messageId": map[string]any{"type": "string"}},
	}
	return report
}

func workTicketCollaborationOperations() []Operation {
	delegate := operation(constants.WorkTicketDelegateOperation,
		"Assign bounded text work to a local partner or team. Its result returns to this ticket.",
		constants.AgentCLIEffectMutation, constants.AgentCLICompletionImmediate, "",
		Field{Name: "subjectType", Required: true, Schema: map[string]any{"type": "string", "enum": []string{"waker", "team"}}},
		Field{Name: "subjectId", Required: true, Schema: map[string]any{"type": "string", "minLength": 1, "maxLength": 32}},
		Field{Name: "content", Required: true, Schema: map[string]any{"type": "string", "minLength": 1, "maxLength": 16000}},
		Field{Name: "context", Schema: map[string]any{"type": "string", "maxLength": 16000}},
		Field{Name: "questionOnly", Schema: map[string]any{"type": "boolean"}})
	delegate.ScopeKind = constants.WorkTicketCapability
	delegate.OutputSchema = map[string]any{"type": "object", "additionalProperties": false, "required": []string{"ticketId"},
		"properties": map[string]any{"ticketId": map[string]any{"type": "string"}}}
	results := operation(constants.WorkTicketResultsOperation,
		"Read results sent only to this work ticket by its direct children.", constants.AgentCLIEffectRead, constants.AgentCLICompletionImmediate, "",
		Field{Name: "reportId", Schema: map[string]any{"type": "string", "minLength": 1, "maxLength": 32}})
	results.ScopeKind = constants.WorkTicketCapability
	results.OutputSchema = map[string]any{"type": "object", "required": []string{"reports"},
		"properties": map[string]any{"reports": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}}}
	reply := operation(constants.WorkTicketReplyOperation, "Send text back to a direct child's waiting work report.",
		constants.AgentCLIEffectMutation, constants.AgentCLICompletionAccepted, "",
		Field{Name: "reportId", Required: true, Schema: map[string]any{"type": "string", "minLength": 1, "maxLength": 32}},
		Field{Name: "content", Required: true, Schema: map[string]any{"type": "string", "minLength": 1, "maxLength": 16000}})
	reply.ScopeKind = constants.WorkTicketCapability
	reply.OutputSchema = map[string]any{"type": "object", "required": []string{"replyId"},
		"properties": map[string]any{"replyId": map[string]any{"type": "string"}}}
	wait := operation(constants.WorkTicketEventWaitOperation,
		"When the user wants to continue only after passing checks on this ticket's authorized GitHub pull request, register the condition and finish the current turn.",
		constants.AgentCLIEffectMutation, constants.AgentCLICompletionAccepted, "",
		Field{Name: "kind", Required: true, Schema: map[string]any{"type": "string", "enum": []string{"checks_passed"}}})
	wait.ScopeKind = constants.WorkTicketCapability
	wait.OutputSchema = map[string]any{"type": "object", "required": []string{"id", "kind"},
		"properties": map[string]any{"id": map[string]any{"type": "string"}, "kind": map[string]any{"type": "string"}}}
	subscribe := operation(constants.WorkTicketEventSubscribeOperation,
		"Add an already observed pull request as context for this work ticket when explicitly requested. This does not claim responsibility for the PR.",
		constants.AgentCLIEffectMutation, constants.AgentCLICompletionAccepted, "",
		Field{Name: "ruleId", Required: true, Schema: map[string]any{"type": "string", "minLength": 1, "maxLength": 32}},
		Field{Name: "observationId", Required: true, Schema: map[string]any{"type": "string", "minLength": 1, "maxLength": 64}})
	subscribe.ScopeKind = constants.WorkTicketCapability
	subscribe.OutputSchema = map[string]any{"type": "object", "required": []string{"topic"},
		"properties": map[string]any{"topic": map[string]any{"type": "string"}}}
	return []Operation{delegate, results, reply, wait, subscribe}
}
