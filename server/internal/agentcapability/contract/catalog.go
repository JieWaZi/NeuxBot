// Package contract 定义 CLI 与 Core 共同使用的封闭命令契约。
package contract

import (
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowcatalog "github.com/JieWaZi/NeuxBot/server/internal/workflow/catalog"
	"github.com/google/jsonschema-go/jsonschema"
)

// Operation 是命令解析、发现与服务端校验共用的唯一操作定义。
type Operation struct {
	// ID 是稳定的领域、资源和动作身份。
	ID string `json:"id"`
	// Summary 说明该操作的实际用途。
	Summary string `json:"summary"`
	// InputSchema 限定调用者可以提供的全部参数。
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	// OutputSchema 约束结果边界；复杂领域值由原领域契约继续解释。
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	// ScopeKind 说明必须由哪个服务端业务场景签发能力。
	ScopeKind string `json:"scopeKind"`
	// DisplayKey 是产品动作的稳定展示身份，不携带模型内容或凭据。
	DisplayKey string `json:"displayKey"`
	// Effect 区分读取、内部写入和外部效果接纳。
	Effect string `json:"effectKind"`
	// Completion 表达同步完成、耐久接纳或候选提交。
	Completion string `json:"completionKind"`
	// Guides 是与本操作相关的按需方法指南。
	Guides []string `json:"guides"`
	// Discovery 提供合法主题、资源类别和原生调用映射，能力摘要也保留该目录。
	Discovery map[string]any `json:"discovery,omitempty"`
}

// Field 将一个参数的类型、必填性和简单命令行映射集中定义。
type Field struct {
	// Name 是规范 JSON 参数名。
	Name string
	// Schema 是字段的闭合 JSON Schema。
	Schema map[string]any
	// Required 表示必须显式提交该字段。
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
	case "knowledge.pages.get":
		op.InputSchema["oneOf"] = []any{map[string]any{"required": []string{"pageId"}}, map[string]any{"required": []string{"slug"}}}
	case "workflow.sdk.get":
		op.Discovery = map[string]any{
			"topics": workflowcatalog.TopicNames(), "examples": workflowcatalog.Examples(),
			"primitiveTopics": workflowcatalog.PrimitiveTopics(),
		}
		// kind 缺省为 sdk；示例与 SDK 的 topic 必须分别校验。
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
	case "memory.entries.search", "memory.entries.create":
		for _, field := range fields {
			if field.Name == "scope" {
				op.Discovery = map[string]any{"scopes": field.Schema["enum"]}
				if value, ok := field.Schema["default"]; ok {
					op.Discovery["defaultScope"] = value
				}
			}
		}
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
	memorySearchScope := choice("scope", []string{constants.MemoryScopePartner.String(), constants.MemoryScopeProject.String(), constants.MemoryScopeAll.String()}, false)
	memorySearchScope.Schema["default"] = constants.MemoryScopeAll.String()
	memorySearchScope.Schema["description"] = "Omit to search all visible partner and project memory. Project scope requires a bound project."
	memoryWriteScope := choice("scope", []string{constants.MemoryScopePartner.String(), constants.MemoryScopeProject.String()}, true)
	memoryWriteScope.Schema["description"] = "Choose ownership for this entry; project scope requires a bound project."
	result := []Operation{
		operation("knowledge.jobs.start", "Start the knowledge organization requested in chat using a visible local library and selected current-message attachments; generation finishes separately. Preauthorized maintenance may omit selection fields.", mutation, accepted, "knowledge", text("libraryId", false), text("backend", false), text("connectionId", false), Field{Name: "attachmentIndexes", Schema: map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 0}, "uniqueItems": true, "maxItems": 100}}, list("sourceVersionIds"), text("message", false)),

		operation("knowledge.libraries.list", "List knowledge resources frozen for this execution.", read, immediate, "knowledge"),
		operation("knowledge.pages.search", "Search published Wiki pages within a frozen resource. The first page also returns up to five original-document chunk hits using the library index strategy; each hit preserves its child locator and full parent context.", read, immediate, "knowledge", text("libraryId", true), text("backend", false), text("connectionId", false), text("query", false), scalar("offset", "integer", false)),
		operation("knowledge.pages.get", "Read one page by either pageId or slug within a frozen knowledge resource.", read, immediate, "knowledge", text("libraryId", true), text("backend", false), text("connectionId", false), text("pageId", false), text("slug", false)),
		operation("knowledge.sources.get", "Read one immutable source within a frozen resource.", read, immediate, "knowledge", text("libraryId", true), text("backend", false), text("connectionId", false), text("sourceVersionId", true)),

		operation("im.targets.list", "Discover authorized message recipients.", read, immediate, "im.delivery", page...),
		operation("im.deliveries.create", "Register a durable delivery; acceptance is not platform confirmation.", external, accepted, "im.delivery", text("targetId", true), text("content", false), list("files")),
		operation("im.deliveries.get", "Read the actual state of an authorized delivery.", read, immediate, "im.delivery", text("deliveryId", true)),
		operation("workflow.source.get", "Read source for the bound workflow.", read, immediate, "workflow.authoring", text("revisionId", false)),
		operation("workflow.authoring.prepare", "Prepare minimal authoring guidance, current source, pending annotations, needed resource pages and save contracts in one read. Select actual DSL primitives; no separate SDK discovery is needed.", read, immediate, "workflow.authoring",
			choices("primitives", workflowcatalog.Primitives(), true),
			choices("features", workflowcatalog.Features(), false),
			choice("language", []string{"javascript", "python"}, false)),
		operation("workflow.source.validate", "Optional preflight; returns diagnostics and a compact summary. Request includeManifest only when the full projection is needed.", read, immediate, "workflow.authoring", text("source", true), scalar("includeManifest", "boolean", false)),
		operation("workflow.source.save", "Compile, validate and CAS-save complete source. A successful immediate receipt with saved, revisionId and sourceDigest is final; no read-back is required.", mutation, immediate, "workflow.authoring", text("source", true), text("expectedRevisionId", true), text("sdkVersion", false), list("resolvedAnnotationIds")),
		operation("workflow.sdk.get", "Read one authorized SDK topic or example; discovery lists exact names and primitive mappings.", read, immediate, "workflow.authoring",
			choice("topic", append(workflowcatalog.TopicNames(), workflowcatalog.Examples()...), true),
			Field{Name: "kind", Schema: map[string]any{
				"type": "string", "enum": []string{"sdk", "example"}, "default": "sdk",
			}}),
		operation("workflow.resources.list", "Discover one bounded page of authoring resources.", read, immediate, "workflow.authoring", choice("kind", workflowcatalog.ResourceKinds(), true), text("cursor", false)),
		operation("workflow.annotations.list", "Read authoring annotations for the bound workflow.", read, immediate, "workflow.authoring", append([]Field{text("status", false), text("baseRevisionId", false)}, page...)...),
		operation("workflow.definitions.list", "Discover workflows authorized for this execution.", read, immediate, "workflow.execution", append([]Field{text("search", false)}, page...)...),
		operation("workflow.definitions.describe", "Read one authorized workflow's input and output contract.", read, immediate, "workflow.execution", text("definitionId", true)),
		operation("workflow.runs.start", "Accept an authorized workflow run with frozen inputs.", external, accepted, "workflow.execution", text("definitionId", true), text("revisionId", true), Field{Name: "args", Schema: map[string]any{}, Required: true}),
		operation("workflow.runs.get", "Read an authorized workflow run.", read, immediate, "workflow.execution", text("runId", true)),
		operation("workflow.runs.cancel", "Request cancellation without undoing completed external effects.", mutation, accepted, "workflow.execution", text("runId", true)),
		operation("workflow.evidence.list", "Read original execution and external-effect evidence.", read, immediate, "workflow.authoring", text("runId", true), text("cursor", false)),
		operation("workflow.results.schema", "Read the current worker attempt's output schema.", read, immediate, "workflow.results"),
		operation("workflow.results.submit", "Submit a candidate result; execution success is confirmed separately.", mutation, candidate, "workflow.results", Field{Name: "data", Schema: map[string]any{}, Required: true}),
		operation("team.members.list", "Read the current thread's member identities and expertise.", read, immediate, "team.lead"),
		operation("team.tasks.list", "Read the current thread's task board.", read, immediate, "team.lead", append([]Field{text("query", false)}, page...)...),
		operation("team.tasks.get", "Read one task in the current thread.", read, immediate, "team.lead", text("taskId", true)),
		operation("team.tasks.create", "Assign a formal task to a non-lead member.", mutation, accepted, "team.lead", text("assigneeWakerId", true), text("subject", true), text("description", true), scalar("priority", "integer", false), list("dependencyTaskIds"), scalar("requiresHumanReview", "boolean", false), scalar("directDelivery", "boolean", false)),
		operation("team.tasks.update", "Update fields of a pending task.", mutation, immediate, "team.lead", text("taskId", true), text("assigneeWakerId", false), text("subject", false), text("description", false), scalar("priority", "integer", false)),
		operation("team.tasks.follow-up", "Append instructions to the original active task.", mutation, accepted, "team.lead", text("taskId", true), text("text", true)),
		operation("team.tasks.retry", "Explicitly retry a failed task within its retry allowance.", mutation, accepted, "team.lead", text("taskId", true), text("assigneeWakerId", false)),
		operation("team.tasks.cancel", "Cancel a task when its actual state permits it.", mutation, accepted, "team.lead", text("taskId", true)),
		operation("team.reports.submit", "Submit the current member attempt's factual work report.", mutation, candidate, "team.member", Field{Name: "text", Schema: map[string]any{"type": "string", "minLength": 1, "maxLength": 4000}, Required: true}, scalar("readyForUser", "boolean", false)),
		operation("memory.entries.search", "Search visible partner and project memory; scope defaults to all.", read, immediate, "memory", text("query", true), memorySearchScope, Field{Name: "topK", Schema: map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}),
		operation("memory.entries.get", "Read one visible memory entry.", read, immediate, "memory", text("memoryId", true)),
		operation("memory.entries.create", "Persist one stable fact in partner or project memory.", mutation, immediate, "memory", memoryWriteScope, text("content", true)),
		operation("memory.entries.update", "Replace the content of one visible memory.", mutation, immediate, "memory", text("memoryId", true), text("content", true)),
		operation("memory.entries.forget", "Forget one visible memory while retaining its change history.", mutation, immediate, "memory", text("memoryId", true)),
		operation("interaction.questions.create", "Ask the user a consequential structured question.", mutation, accepted, "interaction.questions", text("message", true), Field{Name: "requestedSchema", Schema: questionSchema(), Required: true}, scalar("allowSkip", "boolean", false)),
		operation("interaction.questions.get", "Read the actual answer state of the current execution's question.", read, immediate, "interaction.questions", text("questionId", true)),
	}
	result = append(result, knowledgeMaintenanceOperations()...)
	for _, resource := range []struct {
		// operation 是有界观察命令的稳定身份。
		operation string
		// id 是所属资源的参数名。
		id string
		// guide 是当前领域的方法指南。
		guide string
	}{{"im.deliveries.wait", "deliveryId", "im.delivery"}, {"workflow.runs.wait", "runId", "workflow.execution"}, {"interaction.questions.wait", "questionId", "interaction.questions"}} {
		result = append(result, operation(resource.operation, "Observe current state for a bounded interval without cancelling the resource.", read, immediate, resource.guide, text(resource.id, true), Field{Name: "timeoutMs", Schema: map[string]any{"type": "integer", "minimum": 1, "maximum": 60000}}))
	}
	return result
}

// Operations 返回调用方可独立使用的完整静态契约。
func Operations() []Operation { return catalog() }

// Find 查找唯一稳定操作；未授权与不存在均不能绕过在线 Grant 校验。
func Find(id string) (Operation, bool) {
	for _, item := range catalog() {
		if item.ID == id {
			return item, true
		}
	}
	return Operation{}, false
}

// Validate 使用命令树共用的 Schema 拒绝跨动作和未知参数。
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

// Command 返回唯一 CLI 命令路径。
func (o Operation) Command() []string { return strings.Split(o.ID, ".") }
