package capability

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
	case "workflow.source.save":
		op.InputSchema["oneOf"] = []any{map[string]any{"required": []string{"source"}, "not": map[string]any{"anyOf": []any{map[string]any{"required": []string{"patches"}}, map[string]any{"required": []string{"baseSourceDigest"}}}}}, map[string]any{"required": []string{"patches", "baseSourceDigest"}, "not": map[string]any{"required": []string{"source"}}}}
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
		operation("knowledge.jobs.start", "Start document ingestion requested in chat: persist selected current-message attachments as originals, then generate summaries and knowledge according to library indexing settings. Message adds extraction requirements; the host preserves the original request and inherits this execution's model configuration. For existing-Wiki maintenance use knowledge.wiki.maintain. Preauthorized ingestion may omit selection fields.", mutation, accepted, "knowledge", text("libraryId", false), text("backend", false), text("connectionId", false), Field{Name: "attachmentIndexes", Schema: map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 0}, "uniqueItems": true, "maxItems": 100}}, list("sourceVersionIds"), text("message", false)),

		operation("knowledge.libraries.list", "List knowledge resources frozen for this execution.", read, immediate, "knowledge"),
		operation("knowledge.pages.search", "Search published Wiki pages within a frozen resource. The first page also returns up to five original-document chunk hits using the library index strategy; each hit preserves its child locator and full parent context.", read, immediate, "knowledge", text("libraryId", true), text("backend", false), text("connectionId", false), text("query", false), scalar("offset", "integer", false)),
		operation("knowledge.pages.get", "Read one page by either pageId or slug within a frozen knowledge resource.", read, immediate, "knowledge", text("libraryId", true), text("backend", false), text("connectionId", false), text("pageId", false), text("slug", false)),
		operation("knowledge.sources.get", "Read one immutable source within a frozen resource.", read, immediate, "knowledge", text("libraryId", true), text("backend", false), text("connectionId", false), text("sourceVersionId", true)),

		operation("im.targets.list", "Discover authorized message recipients.", read, immediate, "im.delivery", page...),
		operation("im.deliveries.create", "Register a durable delivery; acceptance is not platform confirmation.", external, accepted, "im.delivery", text("targetId", true), text("content", false), list("files")),
		operation("im.deliveries.get", "Read the actual state of an authorized delivery.", read, immediate, "im.delivery", text("deliveryId", true)),
		operation("workflow.source.get", "Read source for the bound workflow.", read, immediate, "workflow.authoring", text("revisionId", false), text("workflowId", false)),
		operation("workflow.authoring.prepare", "Prepare minimal authoring guidance, current source, pending annotations, needed resource pages, execution defaults, retry compatibility and save contracts in one read. Select actual DSL primitives; no separate SDK discovery is needed.", read, immediate, "workflow.authoring",
			choices("primitives", workflowcatalog.Primitives(), true),
			choices("features", workflowcatalog.Features(), false),
			choice("language", []string{"javascript", "python"}, false)),
		operation("workflow.source.validate", "Optional preflight of source and current authoring retry configuration; returns node diagnostics and a compact summary. Request includeManifest only when the full projection is needed.", read, immediate, "workflow.authoring", text("source", true), scalar("includeManifest", "boolean", false)),
		operation("workflow.source.save", "Compile, validate and CAS-save source or exact patches as a new revision. Same requestId replays its receipt. A successful saved receipt is final; no separate commit is needed.", mutation, immediate, "workflow.authoring", text("source", false), text("expectedRevisionId", true), text("sdk", false), text("workflowId", false), text("baseSourceDigest", false), Field{Name: "patches", Schema: map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": map[string]any{"type": "object", "properties": map[string]any{"search": map[string]any{"type": "string", "minLength": 1}, "replace": map[string]any{"type": "string"}}, "required": []string{"search", "replace"}, "additionalProperties": false}}}, list("resolvedAnnotationIds")),
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

// knowledgeMaintenanceOperations 复用统一命令目录与 Schema，开放实际知识读取与维护操作。
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

// questionSchema 只接纳当前共享问答组件能实际填写的字符串与字符串多选表单。
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

// outputSchema 只维护 CLI 的结果边界，不复制源码 Manifest、运行证据或动态用户 Schema。
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
	case "workflow.authoring.prepare":
		schema := object("workflow", "annotations", "topics", "guideIds", "content", "resources", "operations", "execution")
		schema["properties"].(map[string]any)["workflow"] = outputSchema("workflow.source.get")
		schema["required"] = []string{"workflow"}
		return schema
	case "im.targets.list", "team.tasks.list", "workflow.annotations.list", "workflow.definitions.list":
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
	case "workflow.runs.start":
		return object("runId", "definitionId", "revisionId")
	case "workflow.runs.cancel":
		return object("runId", "definitionId", "cancellationRequested")
	case "workflow.runs.get":
		return object("run", "humanRequests", "operationRequests")
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
	case "interaction.questions.create", "interaction.questions.get":
		return object("questionId", "status", "message", "requestedSchema", "allowSkip", "content")
	}
	return nil
}

// DisplayLabel 返回不含模型数据的固定产品动作；完成语义不由文字推断。
func (o Operation) DisplayLabel() string {
	labels := map[string]string{
		"knowledge.pages.purge":   "删除已归并的重复页面",
		"knowledge.libraries.get": "读取知识库设置", "knowledge.sources.list": "查看知识来源", "knowledge.sources.read": "核查原文证据", "knowledge.pages.list": "查看知识页面", "knowledge.themes.list": "查看知识目录", "knowledge.index.get": "读取知识索引", "knowledge.revisions.list": "查看知识历史", "knowledge.revisions.get": "读取历史版本", "knowledge.jobs.list": "查看整理任务", "knowledge.jobs.get": "查看整理结果", "knowledge.audit.get": "核查知识质量", "knowledge.activity.list": "查看知识活动", "knowledge.export.get": "导出知识正文",
		"knowledge.pages.write": "修订知识页面", "knowledge.pages.replace": "精确修订知识", "knowledge.pages.rename": "重命名知识页面", "knowledge.pages.delete": "归档知识页面", "knowledge.revisions.restore": "恢复知识版本", "knowledge.themes.write": "调整知识目录", "knowledge.sources.import": "导入知识来源", "knowledge.jobs.retry": "重试知识整理",
		"knowledge.jobs.start":     "接纳授权知识整理",
		"knowledge.wiki.maintain":  "提交 Wiki 维护",
		"knowledge.jobs.wait":      "等待知识整理结果",
		"knowledge.libraries.list": "查看可用知识库", "knowledge.pages.search": "搜索知识", "knowledge.pages.get": "读取知识页面", "knowledge.sources.get": "查看原件版本", "workflow.authoring.prepare": "准备工作流创作",
		"im.targets.list": "查看可发送聊天", "im.deliveries.create": "提交消息交付", "im.deliveries.get": "查看交付状态", "im.deliveries.wait": "等待消息交付",
		"workflow.source.get": "读取工作流", "workflow.source.validate": "检查工作流", "workflow.source.save": "保存工作流", "workflow.sdk.get": "了解工作流能力", "workflow.resources.list": "查看可用伙伴与资源", "workflow.annotations.list": "查看修改要求", "workflow.evidence.list": "查看执行记录",
		"workflow.definitions.list": "查看可用工作流", "workflow.definitions.describe": "查看工作流参数", "workflow.runs.start": "提交工作流运行", "workflow.runs.get": "查看执行进度", "workflow.runs.cancel": "请求停止工作流", "workflow.runs.wait": "等待工作流运行", "workflow.results.schema": "查看步骤结果格式", "workflow.results.submit": "提交步骤候选结果",
		"team.members.list": "查看团队成员", "team.tasks.list": "查看团队任务", "team.tasks.get": "查看任务详情", "team.tasks.create": "分配团队任务", "team.tasks.update": "更新团队任务", "team.tasks.follow-up": "补充团队任务", "team.tasks.retry": "请求重试任务", "team.tasks.cancel": "请求停止任务", "team.reports.submit": "提交工作报告",
		"memory.entries.search": "搜索记忆", "memory.entries.get": "查看记忆", "memory.entries.create": "保存记忆", "memory.entries.update": "更新记忆", "memory.entries.forget": "遗忘记忆",
		"interaction.questions.create": "请求用户补充", "interaction.questions.get": "查看用户回复", "interaction.questions.wait": "等待用户回复",
	}
	return labels[o.ID]
}
