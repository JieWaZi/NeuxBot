package contract

import "strings"

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
		return object("workflow", "annotations", "topics", "guideIds", "content", "resources", "operations")
	case "im.targets.list", "team.tasks.list", "workflow.annotations.list", "workflow.definitions.list":
		return object("items", "total", "pageNum", "pageSize", "nextCursor")
	case "im.deliveries.create", "im.deliveries.get":
		return object("deliveryId", "state", "errorCode", "partsSent", "createdAt")
	case "knowledge.jobs.start":
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
	case "knowledge.pages.list", "knowledge.revisions.list", "knowledge.jobs.list":
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
		return object("ref", "job")
	case "knowledge.audit.get":
		return object("ref", "audit")
	case "knowledge.activity.list":
		return object("ref", "items", "nextCursor")
	case "knowledge.export.get":
		return object("ref", "export")
	case "knowledge.pages.write", "knowledge.pages.replace", "knowledge.pages.rename", "knowledge.pages.delete", "knowledge.revisions.restore", "knowledge.themes.write", "knowledge.sources.import", "knowledge.jobs.retry":
		return object("ref", "receipt")
	case "workflow.source.get":
		return object("workflowId", "revisionId", "currentRevisionId", "source", "sourceDigest", "sdkVersion")
	case "workflow.source.validate":
		return object("valid", "diagnostics", "summary", "manifest", "error")
	case "workflow.source.save":
		return object("saved", "definitionId", "revisionId", "ordinal", "sourceDigest", "unchanged", "replayed")
	case "workflow.sdk.get":
		return object("version", "topic", "content", "source", "guidance")
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
