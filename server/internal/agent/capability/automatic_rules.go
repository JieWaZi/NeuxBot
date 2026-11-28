package capability

import "github.com/JieWaZi/NeuxBot/server/constants"

const (
	RuleList   = "routine.rules.list"
	RuleGet    = "routine.rules.get"
	RuleCreate = "routine.rules.create"
	RuleUpdate = "routine.rules.update"
	RuleDelete = "routine.rules.delete"
)

func automaticRuleOperations() []Operation {
	const guide = "routine.rules"
	schedule := map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"kind"},
		"properties": map[string]any{
			"kind":       map[string]any{"type": "string", "enum": []string{"once", "hourly", "daily", "weekly", "monthly", "cron"}},
			"timezone":   map[string]any{"type": "string", "description": "IANA timezone, for example Asia/Shanghai. Leave it implicit to use the host's confirmed timezone."},
			"onceAt":     map[string]any{"type": "string", "description": "Local YYYY-MM-DDTHH:mm for a once rule."},
			"minute":     map[string]any{"type": "integer", "minimum": 0, "maximum": 59},
			"hour":       map[string]any{"type": "integer", "minimum": 0, "maximum": 23},
			"weekday":    map[string]any{"type": "integer", "minimum": 0, "maximum": 6, "description": "Sunday=0, Monday=1."},
			"dayOfMonth": map[string]any{"type": "integer", "minimum": 1, "maximum": 31},
			"cron":       map[string]any{"type": "string", "description": "Five-field expression with no seconds."},
		},
	}
	workflow := map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"id", "revisionId"},
		"properties": map[string]any{
			"id":         map[string]any{"type": "string", "minLength": 1, "maxLength": 32},
			"revisionId": map[string]any{"type": "string", "minLength": 1, "maxLength": 32},
			"args":       map[string]any{"type": "object", "description": "Fixed workflow input values required by the saved revision."},
		},
	}
	id := Field{Name: "taskId", Required: true, Schema: map[string]any{"type": "string", "minLength": 1, "maxLength": 32}}
	fields := func(create bool) []Field {
		return []Field{
			{Name: "name", Required: create, Schema: map[string]any{"type": "string", "minLength": 1, "maxLength": 120}},
			{Name: "instruction", Schema: map[string]any{"type": "string", "minLength": 1, "maxLength": 10000}},
			{Name: "schedule", Required: create, Schema: schedule},
			{Name: "reportDestination", Schema: map[string]any{"type": "string", "enum": []string{"main_conversation", "topic"}}},
			{Name: "reportPolicy", Schema: map[string]any{"type": "string", "enum": []string{"always", "changes_only"}}},
			{Name: "enabled", Schema: map[string]any{"type": "boolean"}},
		}
	}
	read, mutation, immediate := constants.AgentCLIEffectRead, constants.AgentCLIEffectMutation, constants.AgentCLICompletionImmediate
	create := operation(RuleCreate, "Save one requested schedule. Provide either the partner instruction or the saved workflow (with its revision and fixed inputs). Workflow reports go to this subject's main conversation. Saving does not run the task now.", mutation, immediate, guide, append(fields(true), Field{Name: "workflow", Schema: workflow})...)
	create.InputSchema["oneOf"] = []any{map[string]any{"required": []string{"instruction"}}, map[string]any{"required": []string{"workflow"}}}
	return []Operation{
		operation(RuleList, "List the signed-in conversation subject's scheduling rules and latest run states.", read, immediate, guide),
		operation(RuleGet, "Read a rule by its real taskId, including its full schedule, delivery settings, and up to five runs.", read, immediate, guide, id),
		create,
		operation(RuleUpdate, "Change only supplied fields of a rule identified with list/get. Set enabled=false to pause or true to resume. To reschedule, provide the full schedule.", mutation, immediate, guide, append([]Field{id}, fields(false)...)...),
		operation(RuleDelete, "Permanently delete an identified rule and its associated run history only when the user explicitly asked to delete it. To pause, use update.", mutation, immediate, guide, id),
	}
}
