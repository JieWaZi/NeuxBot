package capability

import (
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowcatalog "github.com/JieWaZi/NeuxBot/server/internal/workflow/catalog"
	"github.com/google/jsonschema-go/jsonschema"
)

// Operation is the sole operation definition shared by command parsing, discovery, and server validation.
type Operation struct {
	// ID is the stable domain, resource, and action identity.
	ID string `json:"id"`
	// Summary describes the operation's actual purpose.
	Summary string `json:"summary"`
	// InputSchema bounds all parameters the caller may supply.
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	// OutputSchema constrains the result boundary; complex values retain their domain contracts.
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	// ScopeKind identifies the server-side business context that must issue the capability.
	ScopeKind string `json:"scopeKind"`
	// DisplayKey is the stable product action identity without model content or credentials.
	DisplayKey string `json:"displayKey"`
	// Effect distinguishes reads, internal writes, and admission of external effects.
	Effect string `json:"effectKind"`
	// Completion expresses synchronous completion, durable admission, or candidate submission.
	Completion string `json:"completionKind"`
	// Guides lists on-demand method guides related to this operation.
	Guides []string `json:"guides"`
	// Discovery provides valid topics, resource kinds, and native invocation mappings, also retained in capability summaries.
	Discovery map[string]any `json:"discovery,omitempty"`
}

// Field centralizes a parameter's type, required status, and simple CLI mapping.
type Field struct {
	// Name is the canonical JSON parameter name.
	Name string
	// Schema is the field's closed JSON Schema.
	Schema map[string]any
	// Required reports whether the field must be explicitly submitted.
	Required bool
}

func scalar(name, kind string, required bool) Field {
	return Field{Name: name, Schema: map[string]any{"type": kind}, Required: required}
}

func operation(id, summary, effect, completion, guide string, fields ...Field) Operation {
	properties := map[string]any{}
	required := []string{}
	for _, field := range fields {
		properties[field.Name] = field.Schema
		if field.Required {
			required = append(required, field.Name)
		}
	}
	guides := []string{}
	if guide != "" {
		guides = append(guides, guide)
	}
	op := Operation{
		ID: id, Summary: summary, Effect: effect, Completion: completion, Guides: guides,
		ScopeKind: guide, DisplayKey: "agentOperations." + id, OutputSchema: outputSchema(id),
		InputSchema: map[string]any{
			"type": "object", "properties": properties, "required": required, "additionalProperties": false,
		},
	}
	switch id {
	case constants.AutomaticTaskUpdateOperation:
		op.InputSchema["anyOf"] = []any{
			map[string]any{"required": []string{"name"}},
			map[string]any{"required": []string{"instruction"}},
			map[string]any{"required": []string{"schedule"}},
		}
	case constants.WakerProfileOptimizeOperation:
		op.InputSchema["anyOf"] = []any{
			map[string]any{"required": []string{"content"}},
			map[string]any{"required": []string{"expertise"}},
			map[string]any{"required": []string{"workStyle"}},
			map[string]any{"required": []string{"workMethods"}},
		}
	case "workflow.source.save":
		op.InputSchema["oneOf"] = []any{map[string]any{"required": []string{"source"}, "not": map[string]any{"anyOf": []any{map[string]any{"required": []string{"patches"}}, map[string]any{"required": []string{"baseSourceDigest"}}}}}, map[string]any{"required": []string{"patches", "baseSourceDigest"}, "not": map[string]any{"required": []string{"source"}}}}
	case "knowledge.pages.get":
		op.InputSchema["oneOf"] = []any{map[string]any{"required": []string{"pageId"}}, map[string]any{"required": []string{"slug"}}}
	case "workflow.sdk.get":
		op.Discovery = map[string]any{
			"topics": workflowcatalog.TopicNames(), "examples": workflowcatalog.Examples(),
			"primitiveTopics": workflowcatalog.PrimitiveTopics(),
		}
		// kind defaults to sdk; example and SDK topics must be validated separately.
		op.InputSchema["allOf"] = []any{map[string]any{
			"if":   map[string]any{"required": []string{"kind"}, "properties": map[string]any{"kind": map[string]any{"const": "example"}}},
			"then": map[string]any{"properties": map[string]any{"topic": map[string]any{"enum": workflowcatalog.Examples()}}},
			"else": map[string]any{"properties": map[string]any{"topic": map[string]any{"enum": workflowcatalog.TopicNames()}}},
		}}
	case "workflow.resources.list":
		op.Discovery = map[string]any{"resourceKinds": workflowcatalog.ResourceKinds()}
	case "workflow.authoring.prepare":
		op.Discovery = map[string]any{
			"primitives": workflowcatalog.Primitives(), "features": workflowcatalog.Features(),
			"primitiveTopics": workflowcatalog.PrimitiveTopics(),
		}
		op.Guides = append(op.Guides, "workflow.output-delivery", "workflow.api-retrieval")
	}
	return op
}

func catalog() []Operation {
	read, mutation, external := constants.AgentCLIEffectRead, constants.AgentCLIEffectMutation, constants.AgentCLIEffectExternal
	immediate, accepted, candidate := constants.AgentCLICompletionImmediate, constants.AgentCLICompletionAccepted, constants.AgentCLICompletionCandidate
	text := func(name string, required bool) Field { return scalar(name, "string", required) }
	choice := func(name string, values []string, required bool) Field {
		return Field{Name: name, Required: required, Schema: map[string]any{"type": "string", "enum": values}}
	}
	choices := func(name string, values []string, required bool) Field {
		schema := map[string]any{
			"type": "array", "items": map[string]any{"type": "string", "enum": values},
			"uniqueItems": true,
		}
		if required {
			schema["minItems"] = 1
		}
		return Field{Name: name, Required: required, Schema: schema}
	}
	list := func(name string) Field {
		return Field{Name: name, Schema: map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}
	}
	page := []Field{{Name: "pageNum", Schema: map[string]any{"type": "integer", "minimum": 1}}, {Name: "pageSize", Schema: map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}, text("cursor", false)}
	profileObject := func(name string, properties map[string]any, required ...string) Field {
		return Field{Name: name, Schema: map[string]any{"type": "array", "maxItems": 32, "items": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}}}
	}
	profileText := map[string]any{"type": "string", "maxLength": 1000}
	profileOverview := []Field{
		profileObject("expertise", map[string]any{"title": profileText, "description": profileText}, "title", "description"),
		{Name: "workStyle", Schema: map[string]any{"type": "array", "maxItems": 32, "items": profileText}},
		profileObject("workMethods", map[string]any{"name": profileText, "steps": map[string]any{"type": "array", "maxItems": 32, "items": profileText}}, "name", "steps"),
	}
	profileSetup := []Field{
		{Name: "roleName", Required: true, Schema: map[string]any{"type": "string", "minLength": 1, "maxLength": 80, "description": "Short role title in the user's language."}},
		{Name: "bio", Required: true, Schema: map[string]any{"type": "string", "minLength": 1, "maxLength": 1000, "description": "Brief partner introduction in the user's language."}},
		{Name: "content", Required: true, Schema: map[string]any{"type": "string", "minLength": 1, "maxLength": constants.WakerProfileContentMaximumBytes, "description": "Concise English AGENTS.md, at most 1,300 tokens using cl100k_base."}},
	}
	for _, field := range profileOverview {
		field.Required = true
		profileSetup = append(profileSetup, field)
	}
	profileEdit := append([]Field{
		{Name: "revision", Required: true, Schema: map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$"}},
		{Name: "content", Schema: map[string]any{"type": "string", "maxLength": constants.WakerProfileContentMaximumBytes, "description": "Updated AGENTS.md, at most 1,300 tokens using cl100k_base."}},
	}, profileOverview...)
	taskFields := func(required bool) []Field {
		return []Field{
			{Name: "name", Required: required, Schema: map[string]any{"type": "string", "minLength": 1, "maxLength": constants.AutomaticTaskNameMaximumRunes}},
			{Name: "instruction", Required: required, Schema: map[string]any{"type": "string", "minLength": 1, "maxLength": constants.AutomaticTaskInstructionMaximumRunes}},
			{Name: "schedule", Required: required, Schema: automaticTaskScheduleSchema()},
		}
	}
	taskID := map[string]any{"type": "string", "minLength": 1, "maxLength": 32}
	result := []Operation{
		operation(constants.AutomaticTaskCreateOperation, "Create and enable a new scheduled task for the current partner. Use automatic.tasks.update to edit an existing task. Supply a self-contained instruction and a schedule with an IANA time zone.", mutation, immediate, "automatic.tasks", taskFields(true)...),
		operation(constants.AutomaticTaskListOperation, "Find scheduled tasks created in this chat. Provide taskId to read one task's full saved instruction, schedule and current revision. Without taskId, return summaries; use nextOffset to continue.", read, immediate, "automatic.tasks",
			Field{Name: "taskId", Schema: taskID}, Field{Name: "offset", Schema: map[string]any{"type": "integer", "minimum": 0, "maximum": 2147483647}}),
		operation(constants.AutomaticTaskUpdateOperation, "Edit a scheduled task created in this chat. Read the current task first. Supply its ID, revision and only the fields the user wants changed. If changing the schedule, use a complete rule with only the fields for its kind.", mutation, immediate, "automatic.tasks",
			append([]Field{
				{Name: "taskId", Required: true, Schema: taskID},
				{Name: "revision", Required: true, Schema: map[string]any{"type": "integer", "minimum": 1, "maximum": 9007199254740991}},
			}, taskFields(false)...)...),
		operation("knowledge.jobs.start", "Start document ingestion requested in chat: persist selected current-message attachments as originals, then generate summaries and knowledge according to library indexing settings. Message adds extraction requirements; the host preserves the original request and inherits this execution's model configuration. For existing-Wiki maintenance use knowledge.wiki.maintain. Preauthorized ingestion may omit selection fields.", mutation, accepted, "knowledge", text("libraryId", false), text("backend", false), text("connectionId", false), Field{Name: "attachmentIndexes", Schema: map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 0}, "uniqueItems": true, "maxItems": 100}}, list("sourceVersionIds"), text("message", false)),

		operation("knowledge.libraries.list", "List knowledge resources frozen for this execution.", read, immediate, "knowledge"),
		operation("knowledge.pages.search", "Search published Wiki pages within a frozen resource. The first page also returns up to five original-document chunk hits using the library index strategy; each hit preserves its child locator and full parent context.", read, immediate, "knowledge", text("libraryId", true), text("backend", false), text("connectionId", false), text("query", false), scalar("offset", "integer", false)),
		operation("knowledge.pages.get", "Read one page by either pageId or slug within a frozen knowledge resource.", read, immediate, "knowledge", text("libraryId", true), text("backend", false), text("connectionId", false), text("pageId", false), text("slug", false)),
		operation("knowledge.sources.get", "Read one immutable source within a frozen resource.", read, immediate, "knowledge", text("libraryId", true), text("backend", false), text("connectionId", false), text("sourceVersionId", true)),

		operation("im.targets.list", "Discover authorized message recipients.", read, immediate, "im.delivery", page...),
		operation("im.deliveries.create", "Register a durable delivery. Omit targetId for files replying to the current IM chat. Acceptance is not platform confirmation.", external, accepted, "im.delivery", text("targetId", false), text("content", false), list("files")),
		operation("im.deliveries.get", "Read the actual state of an authorized delivery.", read, immediate, "im.delivery", text("deliveryId", true)),
		operation("workflow.source.get", "Read workflow source at the specified revision or current head. Use the conversation's default workflow or supply workflowId.", read, immediate, "workflow.authoring", text("revisionId", false), text("workflowId", false)),
		operation("workflow.authoring.prepare", "Prepare minimal authoring guidance, current source, pending annotations, needed resource pages, execution defaults, retry compatibility and save contracts in one read. Select actual DSL primitives; no separate SDK discovery is needed.", read, immediate, "workflow.authoring",
			choices("primitives", workflowcatalog.Primitives(), true),
			choices("features", workflowcatalog.Features(), false),
			text("workflowId", false),
			choice("language", []string{"javascript", "python"}, false)),
		operation("workflow.source.validate", "Optional preflight of source and current authoring retry configuration; returns node diagnostics and a compact summary. Request includeManifest only when the full projection is needed.", read, immediate, "workflow.authoring", text("source", true), text("workflowId", false), scalar("includeManifest", "boolean", false)),
		operation("workflow.source.save", "Compile, validate and CAS-save source or exact patches as a new revision. Same requestId replays its receipt. A successful saved receipt is final; no separate commit is needed.", mutation, immediate, "workflow.authoring", text("source", false), text("expectedRevisionId", true), text("sdk", false), text("workflowId", false), text("baseSourceDigest", false), Field{Name: "patches", Schema: map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": map[string]any{"type": "object", "properties": map[string]any{"search": map[string]any{"type": "string", "minLength": 1}, "replace": map[string]any{"type": "string"}}, "required": []string{"search", "replace"}, "additionalProperties": false}}}, list("resolvedAnnotationIds")),
		operation("workflow.sdk.get", "Read one authorized SDK topic or example; discovery lists exact names and primitive mappings.", read, immediate, "workflow.authoring",
			choice("topic", append(workflowcatalog.TopicNames(), workflowcatalog.Examples()...), true),
			Field{Name: "kind", Schema: map[string]any{
				"type": "string", "enum": []string{"sdk", "example"}, "default": "sdk",
			}}),
		operation("workflow.resources.list", "Discover one bounded page of authoring resources.", read, immediate, "workflow.authoring", choice("kind", workflowcatalog.ResourceKinds(), true), text("cursor", false), text("workflowId", false)),
		operation("workflow.annotations.list", "Read authoring annotations for the target workflow.", read, immediate, "workflow.authoring", append([]Field{text("status", false), text("baseRevisionId", false), text("workflowId", false)}, page...)...),
		operation("workflow.definitions.list", "Discover workflows authorized for this execution.", read, immediate, "workflow.execution", append([]Field{text("search", false)}, page...)...),
		operation("workflow.definitions.describe", "Read the authorized immutable workflow's input and output contract.", read, immediate, "workflow.execution", text("definitionId", true), text("revisionId", false)),
		operation("workflow.runs.start", "Accept an authorized workflow run with frozen inputs.", external, accepted, "workflow.execution", text("definitionId", true), text("revisionId", true), Field{Name: "args", Schema: map[string]any{}, Required: true}),
		operation("workflow.runs.list", "List workflow runs launched in the current conversation.", read, immediate, "workflow.execution", page...),
		operation("workflow.runs.repeat", "Run a uniquely identified previous run again at the user's request, preserving the original inputs and revision.", external, accepted, "workflow.execution", text("runId", true)),
		operation("workflow.runs.get", "Read an authorized workflow run.", read, immediate, "workflow.execution", text("runId", true)),
		operation("workflow.runs.cancel", "Request cancellation without undoing completed external effects.", mutation, accepted, "workflow.execution", text("runId", true)),
		operation("workflow.evidence.list", "Read original execution and external-effect evidence.", read, immediate, "workflow.authoring", text("runId", true), text("cursor", false)),
		operation("workflow.results.schema", "Read the current worker attempt's output schema.", read, immediate, "workflow.results"),
		operation("workflow.results.submit", "Submit a candidate result; execution success is confirmed separately.", mutation, candidate, "workflow.results", Field{Name: "data", Schema: map[string]any{}, Required: true}),
		operation("team.members.list", "Read the current thread's member identities and roles.", read, immediate, "team.lead"),
		operation("team.tasks.list", "Read the current thread's task board.", read, immediate, "team.lead", append([]Field{text("query", false)}, page...)...),
		operation("team.tasks.get", "Read one task in the current thread.", read, immediate, "team.lead", text("taskId", true)),
		operation("team.tasks.create", "Assign a formal task to a non-lead member.", mutation, accepted, "team.lead", text("assigneeWakerId", true), text("subject", true), text("description", true), scalar("priority", "integer", false), list("dependencyTaskIds"), scalar("requiresHumanReview", "boolean", false), scalar("directDelivery", "boolean", false)),
		operation("team.tasks.update", "Update fields of a pending task.", mutation, immediate, "team.lead", text("taskId", true), text("assigneeWakerId", false), text("subject", false), text("description", false), scalar("priority", "integer", false)),
		operation("team.tasks.follow-up", "Append instructions to the original active task.", mutation, accepted, "team.lead", text("taskId", true), text("text", true)),
		operation("team.tasks.retry", "Explicitly retry a failed task within its retry allowance.", mutation, accepted, "team.lead", text("taskId", true), text("assigneeWakerId", false)),
		operation("team.tasks.cancel", "Cancel a task when its actual state permits it.", mutation, accepted, "team.lead", text("taskId", true)),
		operation("team.reports.submit", "Submit the current member attempt's factual work report.", mutation, candidate, "team.member", Field{Name: "text", Schema: map[string]any{"type": "string", "minLength": 1, "maxLength": 4000}, Required: true}, scalar("readyForUser", "boolean", false)),
		operation("memory.entries.search", "Search the authorized partner's memories.", read, immediate, "memory", text("query", true), Field{Name: "topK", Schema: map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}),
		operation("memory.entries.get", "Read one visible memory entry.", read, immediate, "memory", text("memoryId", true)),
		operation("memory.entries.create", "Save one stable fact as a regular memory for the authorized partner.", mutation, immediate, "memory", text("content", true)),
		operation("memory.entries.update", "Replace the content of one visible memory.", mutation, immediate, "memory", text("memoryId", true), text("content", true)),
		operation("memory.entries.forget", "Forget one visible memory while retaining its change history.", mutation, immediate, "memory", text("memoryId", true)),
		operation(constants.WakerProfileInterviewOperation, "Ask two or three tailored multiple-choice partner setup questions. Each answer can combine several choices and optional Other text.", mutation, accepted, constants.WakerProfileOnboardingGuide, text("message", true), Field{Name: "questions", Required: true, Schema: wakerInterviewSchema()}),
		operation(constants.WakerProfileSaveOperation, "Save the overview and English AGENTS.md after the user answers the interview. Reuse requestId to recover the committed receipt.", mutation, immediate, constants.WakerProfileOnboardingGuide, profileSetup...),
		operation(constants.WakerProfileGetOperation, "Read this chat partner's current profile and its revision before making changes.", read, immediate, constants.WakerProfileOptimizationGuide),
		operation(constants.WakerProfileOptimizeOperation, "Save the requested profile changes for this chat partner. Omitted fields retain their current values.", mutation, immediate, constants.WakerProfileOptimizationGuide, profileEdit...),
		operation("interaction.questions.create", "Ask the user a consequential structured question.", mutation, accepted, "interaction.questions", text("message", true), Field{Name: "requestedSchema", Schema: questionSchema(), Required: true}, scalar("allowSkip", "boolean", false)),
		operation("interaction.questions.get", "Read the actual answer state of the current execution's question.", read, immediate, "interaction.questions", text("questionId", true)),
	}
	result = append(result, resourceManagementOperations()...)
	result = append(result, knowledgeMaintenanceOperations()...)
	for _, resource := range []struct {
		// operation is the stable identity of the bounded observation command.
		operation string
		// id is the parameter name for the owning resource.
		id string
		// guide is the current domain's method guide.
		guide string
	}{{"im.deliveries.wait", "deliveryId", "im.delivery"}, {"workflow.runs.wait", "runId", "workflow.execution"}, {"interaction.questions.wait", "questionId", "interaction.questions"}} {
		result = append(result, operation(resource.operation, "Observe current state for a bounded interval without cancelling the resource.", read, immediate, resource.guide, text(resource.id, true), Field{Name: "timeoutMs", Schema: map[string]any{"type": "integer", "minimum": 1, "maximum": 60000}}))
	}
	return result
}

// resourceManagementOperations gives both transports the same closed inputs and scope.
func resourceManagementOperations() []Operation {
	read, write := constants.AgentCLIEffectRead, constants.AgentCLIEffectMutation
	immediate := constants.AgentCLICompletionImmediate
	text := func(name string, maximum int, required bool) Field {
		return Field{Name: name, Required: required, Schema: map[string]any{"type": "string", "maxLength": maximum}}
	}
	skillID := text("skillId", 128, true)
	skillVersion := text("expectedVersionId", 128, true)
	optionalSkillVersion := skillVersion
	optionalSkillVersion.Required = false
	content := text("content", 64<<10, true)
	connectorID := text("connectorId", 128, true)
	revision := text("expectedRevision", 128, true)
	optionalRevision := revision
	optionalRevision.Required = false
	connectorFields := []Field{
		text("name", 120, false), text("description", 2000, false),
		{Name: "transport", Schema: map[string]any{"type": "string", "enum": []string{string(constants.MCPTransportStdio), string(constants.MCPTransportStreamableHTTP), string(constants.MCPTransportSSE)}}},
		text("url", 4096, false), text("command", 2048, false),
		{Name: "arguments", Schema: map[string]any{"type": "array", "maxItems": 64, "items": map[string]any{"type": "string", "maxLength": 4096}}},
		{Name: "connectionTimeoutMs", Schema: map[string]any{"type": "integer", "minimum": constants.MCPConnectorMinimumTimeoutMS, "maximum": constants.MCPConnectorMaximumTimeoutMS}},
	}
	createFields := append([]Field(nil), connectorFields...)
	createFields[0].Required, createFields[2].Required = true, true
	return []Operation{
		operation("skills.packages.list", "Find skills installed for the current chat partner.", read, immediate, "skills.management", text("query", 120, false)),
		operation("skills.packages.get", "Read an installed skill and its current published SKILL.md before revising it.", read, immediate, "skills.management", skillID),
		operation("skills.packages.create", "On the user's request, publish a new skill and install it for this partner. Supply the full SKILL.md; report success only after the returned receipt.", write, immediate, "skills.management", text("name", 120, true), text("description", 1000, true), content),
		operation("skills.packages.revise", "On the user's request, publish the edited SKILL.md as a new revision. Read the current version first and supply expectedVersionId. The host may require approval for shared changes.", write, immediate, "skills.management", skillID, skillVersion, content),
		operation("skills.packages.uninstall", "On request, remove this partner's installation. Published versions remain in the skill library.", write, immediate, "skills.management", skillID, optionalSkillVersion),
		operation("skills.packages.delete", "On an explicit request to delete from the library, remove the skill, its versions and installations after host approval.", write, immediate, "skills.management", skillID, skillVersion),
		operation("mcp.connectors.list", "Find MCP connectors visible to this partner without exposing credentials.", read, immediate, "mcp.management", text("query", 120, false)),
		operation("mcp.connectors.get", "Read the connector's redacted configuration, scope and revision before editing.", read, immediate, "mcp.management", connectorID),
		operation("mcp.connectors.create", "On the user's request, test and save an MCP connector in the NeuxBot library. Install it for this partner and enable it when the test succeeds. Report the enabled state from the receipt.", write, immediate, "mcp.management", createFields...),
		operation("mcp.connectors.update", "On the user's request, update a connector by ID and expected revision. Omitted fields retain their values. The host handles approval and credentials.", write, immediate, "mcp.management", append([]Field{connectorID, revision}, connectorFields...)...),
		operation("mcp.connectors.test", "Test a saved MCP connector after host approval and report the live connection result.", read, immediate, "mcp.management", connectorID),
		operation("mcp.connectors.detach", "On request, remove the link between a shared connector and this partner.", write, immediate, "mcp.management", connectorID, optionalRevision),
		operation("mcp.connectors.delete", "On request, delete a connector after host approval of the affected scope.", write, immediate, "mcp.management", connectorID, optionalRevision),
	}
}

// Operations returns the complete static contract for independent caller use.
func Operations() []Operation { return catalog() }

// Find locates a unique stable operation; neither missing nor unauthorized operations bypass live grant validation.
func Find(id string) (Operation, bool) {
	for _, item := range catalog() {
		if item.ID == id {
			return item, true
		}
	}
	return Operation{}, false
}

// Validate uses the command tree's shared schema to reject cross-action and unknown parameters.
func (o Operation) Validate(params map[string]any) error {
	encoded, err := json.Marshal(o.InputSchema)
	if err != nil {
		return status_error.AgentInputInvalid
	}
	var schema jsonschema.Schema
	if json.Unmarshal(encoded, &schema) != nil {
		return status_error.AgentInputInvalid
	}
	resolved, err := schema.Resolve(nil)
	if err != nil || resolved.Validate(params) != nil {
		return status_error.AgentInputInvalid
	}
	return nil
}

// Command returns the sole CLI command path.
func (o Operation) Command() []string { return strings.Split(o.ID, ".") }

// knowledgeMaintenanceOperations reuses the shared command catalog and schemas for actual knowledge reads and maintenance.
func knowledgeMaintenanceOperations() []Operation {
	text := func(name string, required bool) Field { return scalar(name, "string", required) }
	number := func(name string, minimum int, required bool) Field {
		return Field{Name: name, Required: required, Schema: map[string]any{"type": "integer", "minimum": minimum}}
	}
	list := func(name string) Field {
		return Field{Name: name, Schema: map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 1000}}
	}
	paging := []Field{number("offset", 0, false), {Name: "limit", Schema: map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}}
	pagePaging := []Field{number("offset", 0, false), {Name: "limit", Schema: map[string]any{"type": "integer", "minimum": 1, "maximum": constants.KnowledgeCLIPageListLimit, "default": 10}}}
	read := func(id, summary string, fields ...Field) Operation {
		base := []Field{text("libraryId", true), text("backend", false), text("connectionId", false)}
		return operation(id, summary, constants.AgentCLIEffectRead, constants.AgentCLICompletionImmediate, "knowledge", append(base, fields...)...)
	}
	write := func(id, summary string, fields ...Field) Operation {
		base := []Field{text("libraryId", true), text("backend", false), text("connectionId", false)}
		op := operation(id, summary, constants.AgentCLIEffectMutation, constants.AgentCLICompletionImmediate, "knowledge", append(base, fields...)...)
		op.Guides = append(op.Guides, "knowledge.maintenance")
		return op
	}
	page := []Field{text("pageId", true), number("expectedVersion", 1, true), number("structureVersion", 1, true)}
	consolidation := []Field{text("expectedUpdatedAt", true), text("canonicalPageId", true), number("canonicalExpectedVersion", 1, true), text("canonicalExpectedUpdatedAt", true)}
	archive := append(append([]Field{}, page...), text("expectedUpdatedAt", false), text("canonicalPageId", false), number("canonicalExpectedVersion", 1, false), text("canonicalExpectedUpdatedAt", false))
	return []Operation{
		operation("knowledge.wiki.maintain", "Start an independent existing-Wiki maintenance task using the same scoped tools and injected workflow as manual maintenance. Message adds findings and requirements; the host preserves the original user request and inherits this execution's model configuration. Do not use document ingestion for Wiki maintenance.", constants.AgentCLIEffectMutation, constants.AgentCLICompletionAccepted, "knowledge", text("libraryId", true), text("backend", false), text("connectionId", false), text("message", false)),
		read("knowledge.libraries.get", "Read current library configuration and version guards."),
		read("knowledge.sources.list", "List persistent document sources in the frozen library.", append(paging, text("query", false), text("state", false), text("sort", false))...),
		read("knowledge.sources.read", "Read saved source chunks with evidence locators; offset/limit paginate chunks and truncated marks the bounded text response. Does not run parsing or a model.", append(paging, text("sourceVersionId", true), text("query", false), text("chunkId", false))...),
		read("knowledge.pages.list", "List Wiki directory metadata without bodies or evidence. Follow nextOffset while hasMore; read needed bodies with knowledge.pages.get.", append(pagePaging, text("query", false), text("pageType", false), text("themeId", false))...),
		read("knowledge.themes.list", "Read the actual Wiki directory structure."),
		read("knowledge.index.get", "Read the persisted Wiki introduction and grouped directory preview (default 5 per group); continue with group cursors or pages.list.", list("pageTypes"), Field{Name: "limit", Schema: map[string]any{"type": "integer", "minimum": 1, "maximum": constants.KnowledgeCLIIndexGroupLimit, "default": 5}}, text("cursor", false)),
		read("knowledge.revisions.list", "List saved revisions for one page.", append(paging, text("pageId", true))...),
		read("knowledge.revisions.get", "Read the complete saved revision, including provenance.", text("pageId", true), number("version", 1, true)),
		read("knowledge.jobs.list", "List actual organization jobs; acceptance is not publication.", paging...),
		read("knowledge.jobs.get", "Read the actual knowledge job and terminal outcome.", text("jobId", true)),
		read("knowledge.jobs.wait", "Wait for the existing local knowledge task and its saved report. completed means terminal, including failure or cancellation; inspect resource.job.state. When completed is false, continue waiting on the same job until terminal before giving the final chat summary. Timeout only ends this observation window.", text("jobId", true), Field{Name: "timeoutMs", Schema: map[string]any{"type": "integer", "minimum": 1, "maximum": 60000}}),
		read("knowledge.audit.get", "Run existing read-only orphan, link, content and source checks."),
		write("knowledge.wiki.auto-fix", "Apply upstream deterministic Wiki repairs within the explicitly authorized library, preserving source evidence and protected content. Inspect remaining issues before further revisions.", number("structureVersion", 1, true)),
		read("knowledge.activity.list", "Read actual library activities using a stable cursor.", text("afterId", false), number("limit", 1, false), text("action", false), text("outcome", false), text("actorUserId", false)),
		read("knowledge.export.get", "Read a consistent Markdown export; exported copies do not mutate the library.", list("pageIds")),
		write("knowledge.pages.write", "Create or revise a complete Wiki page as agent. For existing pages echo updated_at from the page read as expectedUpdatedAt; never obtain a fresh timestamp for an old draft. Preserve valid content and citations; use replace for deliberate removals. In authorized library maintenance, folderId selects an existing folder; omission preserves placement and an empty string selects root.", text("pageId", false), number("expectedVersion", 0, true), text("expectedUpdatedAt", false), number("structureVersion", 1, true), text("slug", true), text("title", true), text("summary", true), text("content", true), text("pageType", true), list("aliases"), list("sourceVersionIds"), text("folderId", false)),
		write("knowledge.pages.replace", "Replace all exact occurrences from the current page; oldText must match. Records an agent revision and preserves other content.", append(page, text("oldText", true), text("newText", true))...),
		write("knowledge.pages.rename", "Rename a page slug and update incoming links atomically, preserving page identity and history.", append(page, text("newSlug", true))...),
		write("knowledge.pages.delete", "Archive a page within the requested Wiki cleanup or explicit deletion. For local duplicate consolidation, supply canonicalPageId and both pages' original read version/timestamp guards: incoming references are redirected atomically to that canonical page. Without a canonical destination remaining incoming links are removed. This step retains history; use knowledge.pages.purge to finish permanent duplicate cleanup. Preserve original documents and explicit preservation requests.", archive...),
		write("knowledge.pages.purge", "Permanently delete an already archived duplicate page and its revision history in a local library after verified consolidation. Read the target guards after archiving; retain the canonical guards from the content-verifying read. Remaining incoming links are redirected atomically. Original documents, canonical content, operation receipts and the maintenance report are retained. Honor the user's scope and preservation requests; never use for a live page or an unverified merge.", append(append([]Field{}, page...), consolidation...)...),
		write("knowledge.revisions.restore", "Restore a saved revision as a new version, preserving current placement.", append(page, number("revisionVersion", 1, true))...),
		write("knowledge.themes.write", "Create or update a Wiki directory, or delete an explicitly requested empty directory.", text("themeId", false), text("name", true), text("parentId", false), number("structureVersion", 1, true), scalar("delete", "boolean", false)),
		write("knowledge.sources.import", "Import explicit source bytes into the selected library without starting organization; bytes is base64 in JSON.", text("filename", true), text("mime", false), text("bytes", true), text("sourceId", false), number("expectedVersion", 0, false)),
		operation("knowledge.jobs.retry", "Explicitly retry a failed knowledge job using its existing frozen sources and configuration.", constants.AgentCLIEffectMutation, constants.AgentCLICompletionAccepted, "knowledge", text("libraryId", true), text("backend", false), text("connectionId", false), text("jobId", true), number("expectedVersion", 1, true)),
	}
}

// questionSchema admits only strings and string multiselect forms supported by the shared question component.
func questionSchema() map[string]any {
	text := map[string]any{"type": "string"}
	stringsSchema := map[string]any{"type": "array", "items": map[string]any{"type": "string", "minLength": 1}, "uniqueItems": true}
	choiceStrings := map[string]any{"type": "array", "items": map[string]any{"type": "string", "minLength": 1}, "uniqueItems": true, "minItems": 1, "maxItems": 20}
	option := map[string]any{"type": "object", "properties": map[string]any{"const": map[string]any{"type": "string", "minLength": 1}, "title": text, "description": text}, "required": []string{"const"}, "additionalProperties": false}
	options := map[string]any{"type": "array", "items": option, "minItems": 1, "maxItems": 20}
	field := map[string]any{"type": "object", "properties": map[string]any{
		"type": map[string]any{"enum": []string{"string", "array"}}, "title": text, "description": text,
		"enum": choiceStrings, "oneOf": options, "minLength": map[string]any{"type": "integer", "minimum": 0}, "maxLength": map[string]any{"type": "integer", "minimum": 1},
		"items": map[string]any{"type": "object", "properties": map[string]any{"type": map[string]any{"const": "string"}, "enum": choiceStrings, "anyOf": options}, "additionalProperties": false},
	}, "required": []string{"type"}, "additionalProperties": false}
	return map[string]any{"type": "object", "properties": map[string]any{
		"type": map[string]any{"const": "object"}, "title": text, "description": text,
		"properties": map[string]any{"type": "object", "minProperties": 1, "maxProperties": 8, "propertyNames": map[string]any{"type": "string", "minLength": 1, "not": map[string]any{"pattern": "(_custom|__other)$"}}, "additionalProperties": field},
		"required":   stringsSchema, "additionalProperties": map[string]any{"const": false},
	}, "required": []string{"type", "properties"}, "additionalProperties": false}
}

// wakerInterviewSchema receives model-generated choices, while Core defines the form's Other behavior.
func wakerInterviewSchema() map[string]any {
	id := map[string]any{"type": "string", "pattern": "^[A-Za-z][A-Za-z0-9]{0,31}$"}
	label := map[string]any{"type": "string", "minLength": 1, "maxLength": 240}
	option := map[string]any{"type": "object", "required": []string{"id", "label"}, "additionalProperties": false,
		"properties": map[string]any{"id": id, "label": label, "description": map[string]any{"type": "string", "maxLength": 500}},
	}
	question := map[string]any{"type": "object", "required": []string{"id", "title", "options"}, "additionalProperties": false,
		"properties": map[string]any{"id": id, "title": label, "options": map[string]any{"type": "array", "minItems": 2, "maxItems": 5, "items": option}},
	}
	return map[string]any{"type": "array", "minItems": 2, "maxItems": 3, "items": question}
}

func automaticTaskScheduleSchema() map[string]any {
	integer := func(minimum, maximum int) map[string]any {
		return map[string]any{"type": "integer", "minimum": minimum, "maximum": maximum}
	}
	fields := map[string]map[string]any{
		"onceAt":     {"type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}$", "description": "Local time in YYYY-MM-DDTHH:mm."},
		"minute":     integer(0, 59),
		"hour":       integer(0, 23),
		"weekday":    integer(0, 6),
		"dayOfMonth": integer(1, 31),
		"cron":       {"type": "string", "minLength": 1, "maxLength": 120, "description": "Five-field cron expression without a timezone prefix."},
	}
	shape := func(kind constants.AutomaticScheduleKind, names ...string) map[string]any {
		properties := map[string]any{
			"kind":     map[string]any{"type": "string", "const": string(kind)},
			"timezone": map[string]any{"type": "string", "minLength": 1, "maxLength": 120, "description": "IANA time zone such as Asia/Shanghai."},
		}
		for _, name := range names {
			properties[name] = fields[name]
		}
		return map[string]any{"type": "object", "additionalProperties": false,
			"required": append([]string{"kind", "timezone"}, names...), "properties": properties}
	}
	return map[string]any{"type": "object", "oneOf": []any{
		shape(constants.AutomaticScheduleKindOnce, "onceAt"),
		shape(constants.AutomaticScheduleKindHourly, "minute"),
		shape(constants.AutomaticScheduleKindDaily, "hour", "minute"),
		shape(constants.AutomaticScheduleKindWeekly, "weekday", "hour", "minute"),
		shape(constants.AutomaticScheduleKindMonthly, "dayOfMonth", "hour", "minute"),
		shape(constants.AutomaticScheduleKindCron, "cron"),
	}}
}

// outputSchema maintains only CLI result boundaries without duplicating source manifests, execution evidence, or dynamic user schemas.
func outputSchema(id string) map[string]any {
	object := func(names ...string) map[string]any {
		properties := map[string]any{}
		for _, name := range names {
			properties[name] = map[string]any{}
		}
		return map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	}
	if strings.HasSuffix(id, ".wait") {
		return object("completed", "resource")
	}
	switch id {
	case constants.AutomaticTaskCreateOperation:
		return object("taskId", "name", "wakerName", "instructionPreview", "schedule", "firstRunAt")
	case constants.AutomaticTaskListOperation:
		return object("items", "nextOffset")
	case constants.AutomaticTaskUpdateOperation:
		return object("taskId", "name", "schedule", "revision", "nextRunAt")
	case "workflow.authoring.prepare":
		schema := object("workflow", "annotations", "topics", "guideIds", "content", "resources", "operations", "execution")
		schema["properties"].(map[string]any)["workflow"] = outputSchema("workflow.source.get")
		schema["required"] = []string{"workflow"}
		return schema
	case "im.targets.list", "team.tasks.list", "workflow.annotations.list", "workflow.definitions.list", "workflow.runs.list":
		return object("items", "total", "pageNum", "pageSize", "nextCursor")
	case "im.deliveries.create", "im.deliveries.get":
		return object("deliveryId", "state", "errorCode", "partsSent", "createdAt")
	case "knowledge.jobs.start", "knowledge.wiki.maintain":
		return object("receipt", "noop")
	case "knowledge.libraries.list":
		return object("items", "total")
	case "knowledge.pages.search":
		return object("ref", "items", "total", "chunks", "retrievalMode")
	case "knowledge.pages.get":
		return object("ref", "page")
	case "knowledge.sources.get":
		return object("ref", "source")
	case "knowledge.libraries.get":
		return object("ref", "library")
	case "knowledge.sources.list":
		return object("ref", "items", "total", "versioning")
	case "knowledge.pages.list":
		return object("ref", "items", "total", "offset", "nextOffset", "hasMore")
	case "knowledge.revisions.list", "knowledge.jobs.list":
		return object("ref", "items", "total")
	case "knowledge.sources.read":
		return object("ref", "source", "items", "total", "offset", "nextOffset", "hasMore", "truncated")
	case "knowledge.themes.list":
		return object("ref", "items")
	case "knowledge.index.get":
		return object("ref", "index")
	case "knowledge.revisions.get":
		return object("ref", "revision")
	case "knowledge.jobs.get":
		return object("ref", "job", "report")
	case "knowledge.audit.get":
		return object("ref", "audit")
	case "knowledge.wiki.auto-fix":
		return object("ref", "receipt", "fixed", "audit")
	case "knowledge.activity.list":
		return object("ref", "items", "nextCursor")
	case "knowledge.export.get":
		return object("ref", "export")
	case "knowledge.pages.write", "knowledge.pages.replace", "knowledge.pages.rename", "knowledge.pages.delete", "knowledge.pages.purge", "knowledge.revisions.restore", "knowledge.themes.write", "knowledge.sources.import", "knowledge.jobs.retry":
		return object("ref", "receipt")
	case "workflow.source.get":
		schema := object("workflowId", "revisionId", "currentRevisionId", "source", "sourceDigest", "sdk", "workspace")
		properties := schema["properties"].(map[string]any)
		workspace := object("rootDirectory", "scriptsDirectory", "stateDirectory", "cacheDirectory", "temporaryDirectory", "runsDirectory")
		fields := workspace["properties"].(map[string]any)
		for name := range fields {
			fields[name] = map[string]any{"type": "string", "minLength": 1}
		}
		workspace["required"] = []string{"rootDirectory", "scriptsDirectory", "stateDirectory", "cacheDirectory", "temporaryDirectory", "runsDirectory"}
		properties["workspace"] = workspace
		schema["required"] = []string{"workspace"}
		return schema
	case "workflow.source.validate":
		return object("valid", "diagnostics", "summary", "manifest", "error", "errorCode")
	case "workflow.source.save":
		return object("saved", "definitionId", "revisionId", "ordinal", "sourceDigest", "manifestDigest", "replayed")
	case "workflow.sdk.get":
		return object("sdk", "topic", "content", "source", "guidance")
	case "workflow.resources.list":
		return object("kind", "items", "nextCursor", "toolPolicyCapabilities")
	case "workflow.definitions.describe":
		return object("id", "name", "description", "whenToUse", "revisionId", "runnable", "inputSchema", "outputSchema")
	case "workflow.runs.start", "workflow.runs.repeat":
		return object("runId", "definitionId", "revisionId")
	case "workflow.runs.cancel":
		return object("runId", "definitionId", "cancellationRequested")
	case "workflow.runs.get":
		return object("run", "humanRequests", "operationRequests", "outputFiles")
	case "workflow.evidence.list":
		return object("run", "instances", "actionCalls", "humanRequests", "operations", "totalInstances", "nextCursor")
	case "workflow.results.schema":
		return object("schema", "nodeId", "guidance")
	case "workflow.results.submit":
		return object("submitted", "executionId", "attemptGeneration")
	case "team.members.list":
		return object("items")
	case "team.tasks.get":
		return object("id", "threadId", "senderWakerId", "assigneeWakerId", "subject", "description", "status", "priority", "requiresHumanReview", "directDelivery", "currentFence", "result", "errorMessage", "changedPaths", "createdAt", "updatedAt", "startedAt", "finishedAt", "cancelRequestedAt")
	case "team.tasks.create":
		return object("taskId", "accepted")
	case "team.tasks.update":
		return object("taskId", "updated")
	case "team.tasks.follow-up":
		return object("taskId", "followUpId", "accepted")
	case "team.tasks.retry":
		return object("taskId", "retryQueued")
	case "team.tasks.cancel":
		return object("taskId", "cancellationRequested")
	case "team.reports.submit":
		return object("reported", "taskId", "attemptId", "nextStep")
	case "memory.entries.search":
		return object("items", "mode", "indexConflict")
	case "memory.entries.get":
		return object("memoryId", "scope", "content", "createdAt", "updatedAt")
	case "memory.entries.create", "memory.entries.update", "memory.entries.forget":
		return object("memoryId", "duplicate")
	case "skills.packages.list", "mcp.connectors.list":
		return object("items", "truncated")
	case "skills.packages.get":
		return object("skillId", "versionId", "name", "version", "description", "enabled", "revisable", "content")
	case "skills.packages.create", "skills.packages.revise", "skills.packages.uninstall", "skills.packages.delete":
		return object("skillId", "versionId", "name", "version")
	case "mcp.connectors.get", "mcp.connectors.create", "mcp.connectors.update":
		return object("connectorId", "name", "revision", "description", "transport", "scope", "enabled", "origin", "command", "connectionTimeoutMs", "testStatus", "hasCredentials")
	case "mcp.connectors.detach", "mcp.connectors.delete":
		return object("connectorId", "name", "detached", "deleted")
	case "mcp.connectors.test":
		return object("connectorId", "name", "status", "toolCount", "durationMs")
	case constants.WakerProfileInterviewOperation:
		return object("questionId", "status")
	case constants.WakerProfileGetOperation:
		return object("revision", "name", "roleName", "bio", "expertise", "workStyle", "workMethods", "content")
	case constants.WakerProfileSaveOperation, constants.WakerProfileOptimizeOperation:
		return object("wakerId", "saved")
	case "interaction.questions.create", "interaction.questions.get":
		return object("questionId", "status", "message", "requestedSchema", "allowSkip", "content")
	}
	return nil
}

// DisplayLabel returns a fixed product action without model data; text does not determine completion semantics.
func (o Operation) DisplayLabel() string {
	labels := map[string]string{
		constants.AutomaticTaskCreateOperation: "创建自动任务",
		constants.AutomaticTaskListOperation:   "查看对话创建的任务",
		constants.AutomaticTaskUpdateOperation: "编辑自动任务",
		"knowledge.pages.purge":                "删除已归并的重复页面",
		"knowledge.libraries.get":              "读取知识库设置", "knowledge.sources.list": "查看知识来源", "knowledge.sources.read": "核查原文证据", "knowledge.pages.list": "查看知识页面", "knowledge.themes.list": "查看知识目录", "knowledge.index.get": "读取知识索引", "knowledge.revisions.list": "查看知识历史", "knowledge.revisions.get": "读取历史版本", "knowledge.jobs.list": "查看整理任务", "knowledge.jobs.get": "查看整理结果", "knowledge.audit.get": "核查知识质量", "knowledge.activity.list": "查看知识活动", "knowledge.export.get": "导出知识正文",
		"knowledge.pages.write": "修订知识页面", "knowledge.pages.replace": "精确修订知识", "knowledge.pages.rename": "重命名知识页面", "knowledge.pages.delete": "归档知识页面", "knowledge.revisions.restore": "恢复知识版本", "knowledge.themes.write": "调整知识目录", "knowledge.sources.import": "导入知识来源", "knowledge.jobs.retry": "重试知识整理",
		"knowledge.jobs.start":     "接纳授权知识整理",
		"knowledge.wiki.maintain":  "提交 Wiki 维护",
		"knowledge.jobs.wait":      "等待知识整理结果",
		"knowledge.libraries.list": "查看可用知识库", "knowledge.pages.search": "搜索知识", "knowledge.pages.get": "读取知识页面", "knowledge.sources.get": "查看原件版本", "workflow.authoring.prepare": "准备工作流创作",
		"im.targets.list": "查看可发送聊天", "im.deliveries.create": "提交消息交付", "im.deliveries.get": "查看交付状态", "im.deliveries.wait": "等待消息交付",
		"workflow.source.get": "读取工作流", "workflow.source.validate": "检查工作流", "workflow.source.save": "保存工作流", "workflow.sdk.get": "了解工作流能力", "workflow.resources.list": "查看可用伙伴与资源", "workflow.annotations.list": "查看修改要求", "workflow.evidence.list": "查看执行记录",
		"workflow.definitions.list": "查看可用工作流", "workflow.definitions.describe": "查看工作流参数", "workflow.runs.start": "提交工作流运行", "workflow.runs.list": "查看对话中的运行", "workflow.runs.repeat": "再次执行原运行", "workflow.runs.get": "查看执行进度", "workflow.runs.cancel": "请求停止工作流", "workflow.runs.wait": "等待工作流运行", "workflow.results.schema": "查看步骤结果格式", "workflow.results.submit": "提交步骤候选结果",
		"team.members.list": "查看团队成员", "team.tasks.list": "查看团队任务", "team.tasks.get": "查看任务详情", "team.tasks.create": "分配团队任务", "team.tasks.update": "更新团队任务", "team.tasks.follow-up": "补充团队任务", "team.tasks.retry": "请求重试任务", "team.tasks.cancel": "请求停止任务", "team.reports.submit": "提交工作报告",
		"memory.entries.search": "搜索记忆", "memory.entries.get": "查看记忆", "memory.entries.create": "保存记忆", "memory.entries.update": "更新记忆", "memory.entries.forget": "遗忘记忆",
		"skills.packages.list": "查看技能列表", "skills.packages.get": "读取技能", "skills.packages.create": "新建技能", "skills.packages.revise": "更新技能", "skills.packages.uninstall": "卸载技能", "skills.packages.delete": "删除技能",
		"mcp.connectors.list": "查看 MCP 列表", "mcp.connectors.get": "读取 MCP 配置", "mcp.connectors.create": "添加 MCP", "mcp.connectors.update": "更新 MCP", "mcp.connectors.test": "测试 MCP", "mcp.connectors.detach": "解绑 MCP", "mcp.connectors.delete": "删除 MCP",
		constants.WakerProfileInterviewOperation: "提出伙伴建档问题",
		constants.WakerProfileSaveOperation:      "保存伙伴档案",
		constants.WakerProfileGetOperation:       "查看伙伴档案",
		constants.WakerProfileOptimizeOperation:  "更新伙伴档案",
		"interaction.questions.create":           "请求用户补充", "interaction.questions.get": "查看用户回复", "interaction.questions.wait": "等待用户回复",
	}
	return labels[o.ID]
}
