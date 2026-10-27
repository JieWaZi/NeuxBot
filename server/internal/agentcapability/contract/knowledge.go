package contract

import "github.com/JieWaZi/NeuxBot/server/constants"

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
