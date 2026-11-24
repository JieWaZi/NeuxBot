package capability

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func TestBotCLIReadmeMatchesRegisteredOperations(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "cmd", "botcli", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(content), "<!-- botcli-operations:start -->")
	if !ok {
		t.Fatal("README is missing the operation inventory")
	}
	section, _, ok = strings.Cut(section, "<!-- botcli-operations:end -->")
	if !ok {
		t.Fatal("README operation inventory is not closed")
	}
	documented := map[string]bool{}
	for _, line := range strings.Split(section, "\n") {
		cells := strings.Split(strings.TrimSpace(line), "|")
		if len(cells) != 6 || !strings.HasPrefix(strings.TrimSpace(cells[1]), "`") {
			continue
		}
		id := strings.Trim(strings.TrimSpace(cells[1]), "`")
		op, exists := Find(id)
		if !exists || documented[id] {
			t.Fatalf("README lists an unknown or repeated operation: %s", id)
		}
		if strings.TrimSpace(cells[2]) == "" ||
			strings.Trim(strings.TrimSpace(cells[3]), "`") != op.Effect ||
			strings.Trim(strings.TrimSpace(cells[4]), "`") != op.Completion {
			t.Fatalf("README purpose/effect/completion differs from the contract: %s", id)
		}
		documented[id] = true
	}
	for _, op := range Operations() {
		if !documented[op.ID] {
			t.Errorf("README is missing operation: %s", op.ID)
		}
	}
}

func TestKnowledgeStartAcceptsChatSelectionAndRejectsInvalidAttachmentIndexes(t *testing.T) {
	op, _ := Find("knowledge.jobs.start")
	if err := op.Validate(map[string]any{"libraryId": "library", "backend": "local", "connectionId": "local", "attachmentIndexes": []int{1}, "message": "Organize by topic"}); err != nil {
		t.Fatalf("natural chat request rejected: %v", err)
	}
	if err := op.Validate(map[string]any{}); err != nil {
		t.Fatalf("preauthorized maintenance request rejected: %v", err)
	}
	for _, indexes := range []any{[]int{-1}, []int{0, 0}, []float64{0.5}, []string{"file.md"}} {
		if op.Validate(map[string]any{"libraryId": "library", "attachmentIndexes": indexes}) == nil {
			t.Fatalf("invalid indexes accepted: %v", indexes)
		}
	}
}

func TestWikiAutoFixRequiresCurrentLibraryStructure(t *testing.T) {

	op, ok := Find("knowledge.wiki.auto-fix")
	if !ok {
		t.Fatal("whole-library maintenance cannot discover the upstream auto-fix operation")
	}
	if err := op.Validate(map[string]any{"libraryId": "kb", "structureVersion": 3}); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []map[string]any{
		{"libraryId": "kb"},
		{"libraryId": "kb", "structureVersion": 0},
		{"libraryId": "kb", "structureVersion": 3, "pageId": "another-page"},
	} {
		if op.Validate(invalid) == nil {
			t.Fatalf("invalid maintenance target accepted: %v", invalid)
		}
	}
}

func TestKnowledgeJobWaitRequiresScopedJobAndBoundedTimeout(t *testing.T) {
	op, ok := Find("knowledge.jobs.wait")
	if !ok {
		t.Fatal("missing knowledge task observation")
	}
	if op.Effect != constants.AgentCLIEffectRead || op.Completion != constants.AgentCLICompletionImmediate {
		t.Fatal("waiting must only observe the existing task")
	}
	if err := op.Validate(map[string]any{"libraryId": "kb", "jobId": "job", "timeoutMs": 60000}); err != nil {
		t.Fatal(err)
	}
	for _, params := range []map[string]any{
		{"libraryId": "kb"}, {"jobId": "job"},
		{"libraryId": "kb", "jobId": "job", "timeoutMs": 0},
		{"libraryId": "kb", "jobId": "job", "timeoutMs": 60001},
		{"libraryId": "kb", "jobId": "job", "model": "override"},
	} {
		if op.Validate(params) == nil {
			t.Fatalf("invalid wait accepted: %v", params)
		}
	}
}

func TestWikiPurgeRequiresCanonicalAndExactReadGuards(t *testing.T) {
	op, ok := Find("knowledge.pages.purge")
	if !ok {
		t.Fatal("missing permanent deletion for archived duplicate pages")
	}
	valid := map[string]any{"libraryId": "kb", "pageId": "duplicate", "expectedVersion": 2, "expectedUpdatedAt": "2026-01-01T00:00:00Z", "structureVersion": 3, "canonicalPageId": "canonical", "canonicalExpectedVersion": 4, "canonicalExpectedUpdatedAt": "2026-01-01T00:01:00Z"}
	if err := op.Validate(valid); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"libraryId", "pageId", "expectedVersion", "expectedUpdatedAt", "structureVersion", "canonicalPageId", "canonicalExpectedVersion", "canonicalExpectedUpdatedAt"} {
		input := make(map[string]any, len(valid))
		for key, value := range valid {
			if key != field {
				input[key] = value
			}
		}
		if op.Validate(input) == nil {
			t.Fatalf("purge accepted missing guard: %s", field)
		}
	}
}

func TestWikiMaintainAcceptsTaskRequirementsWithoutSourceOrModelOverrides(t *testing.T) {
	op, ok := Find("knowledge.wiki.maintain")
	if !ok {
		t.Fatal("missing Wiki maintenance task command")
	}
	if op.Completion != constants.AgentCLICompletionAccepted {
		t.Fatal("maintenance must report task admission")
	}
	if err := op.Validate(map[string]any{"libraryId": "kb", "message": "Merge duplicates, preserve source citations"}); err != nil {
		t.Fatal(err)
	}
	for _, extra := range []string{"attachmentIndexes", "sourceVersionIds", "runtimeId", "accountId", "model", "reasoning"} {
		if op.Validate(map[string]any{"libraryId": "kb", extra: "override"}) == nil {
			t.Fatalf("accepted forbidden override: %s", extra)
		}
	}
	if op.Validate(map[string]any{}) == nil {
		t.Fatal("accepted missing library")
	}
}

func TestResourceManagementCommandSchemas(t *testing.T) {
	for _, tc := range []struct {
		operation string
		valid     map[string]any
		invalid   map[string]any
	}{
		{"skills.packages.revise", map[string]any{"skillId": "skill", "expectedVersionId": "version", "content": "# Body"}, map[string]any{"skillId": "skill", "content": "# Body"}},
		{"skills.packages.delete", map[string]any{"skillId": "skill", "expectedVersionId": "version"}, map[string]any{"skillId": "skill", "expectedVersionId": "version", "wakerId": "other"}},
		{"mcp.connectors.create", map[string]any{"name": "Demo", "transport": "streamable_http", "url": "http://127.0.0.1:3000/mcp"}, map[string]any{"name": "Demo", "transport": "streamable_http", "trustConfirmed": true}},
		{"mcp.connectors.update", map[string]any{"connectorId": "connector", "expectedRevision": "revision", "name": "New name"}, map[string]any{"connectorId": "connector", "expectedRevision": "revision", "headers": []any{}}},
	} {
		op, ok := Find(tc.operation)
		if !ok || op.Validate(tc.valid) != nil || op.Validate(tc.invalid) == nil {
			t.Errorf("invalid resource operation contract: %s", tc.operation)
		}
	}
}

func TestMemoryCommandsRejectOwnershipOverrides(t *testing.T) {
	search, _ := Find("memory.entries.search")
	create, _ := Find("memory.entries.create")
	for _, tc := range []struct {
		// op 是应由当前执行授权确定归属的操作。
		op Operation
		// field 是固定业务入参。
		field string
	}{{search, "query"}, {create, "content"}} {
		if err := tc.op.Validate(map[string]any{tc.field: "preferences"}); err != nil {
			t.Fatal(err)
		}
		for _, override := range []string{"scope", "scopeId", "partnerId", "projectId"} {
			if err := tc.op.Validate(map[string]any{tc.field: "preferences", override: "another"}); err == nil {
				t.Fatalf("%s accepted ownership override %s", tc.op.ID, override)
			}
		}
	}
}

func TestWorkflowSDKTopicsAreDiscoveredAndCheckedByKind(t *testing.T) {
	op, _ := Find("workflow.sdk.get")
	for _, params := range []map[string]any{
		{"topic": "function"}, {"topic": "overview", "kind": "sdk"},
		{"topic": "f0", "kind": "example"},
		{"topic": "review", "kind": "example"},
	} {
		if err := op.Validate(params); err != nil {
			t.Fatalf("advertised SDK input rejected: %v: %v", params, err)
		}
	}
	for _, params := range []map[string]any{
		{"topic": "functionTask"}, {"topic": "index"}, {"topic": "phase"},
		{"topic": "f0"}, {"topic": "function", "kind": "example"},
		{"topic": "weekly-report", "kind": "example"},
		{"topic": "environment-review", "kind": "example"},
		{"topic": "environment-release", "kind": "example"},
	} {
		if op.Validate(params) == nil {
			t.Fatalf("invalid SDK query accepted: %v", params)
		}
	}
	mapping := op.Discovery["primitiveTopics"].(map[string]string)
	if mapping["functionTask"] != "function" || mapping["phase"] != "overview" {
		t.Fatalf("missing primitive-to-topic guidance: %v", mapping)
	}
	resources, _ := Find("workflow.resources.list")
	if resources.Validate(map[string]any{"kind": "wakers"}) != nil ||
		resources.Validate(map[string]any{"kind": "builtinActions"}) != nil ||
		resources.Validate(map[string]any{"kind": "partners"}) == nil {
		t.Fatal("resource discovery differs from accepted kinds")
	}
	prepare, _ := Find("workflow.authoring.prepare")
	if prepare.Validate(map[string]any{"primitives": []string{}}) == nil ||
		prepare.Validate(map[string]any{"primitives": []string{"functionTask"}, "features": []string{"unknown"}}) == nil {
		t.Fatal("preparation accepted missing primitives or an unknown feature")
	}
}

func TestKnowledgeCommandsMatchCurrentChatBoundary(t *testing.T) {
	ids := []string{}
	for _, operation := range Operations() {
		if strings.HasPrefix(operation.ID, "knowledge.") {
			ids = append(ids, operation.ID)
		}
	}
	for _, id := range []string{"knowledge.jobs.start", "knowledge.libraries.list", "knowledge.pages.search", "knowledge.pages.get", "knowledge.sources.get"} {
		if !slices.Contains(ids, id) {
			t.Fatalf("knowledge operation missing: %s", id)
		}
	}
}

func TestKnowledgeMaintenanceRequiresVersionsAndRejectsUnknownFields(t *testing.T) {
	op, ok := Find("knowledge.pages.replace")
	if !ok {
		t.Fatal("precise Wiki replacement is not registered")
	}
	valid := map[string]any{"libraryId": "kb", "pageId": "page", "expectedVersion": 2, "structureVersion": 1, "oldText": "old", "newText": "new"}
	if op.Validate(valid) != nil {
		t.Fatal("complete versioned replacement rejected")
	}
	delete(valid, "expectedVersion")
	if op.Validate(valid) == nil {
		t.Fatal("unversioned replacement accepted")
	}
	valid["expectedVersion"] = 2
	valid["backendOverride"] = "another-server"
	if op.Validate(valid) == nil {
		t.Fatal("unknown override accepted")
	}
	read, _ := Find("knowledge.pages.get")
	if read.Validate(map[string]any{"libraryId": "kb", "slug": "concept/topic"}) != nil ||
		read.Validate(map[string]any{"libraryId": "kb", "pageId": "page", "slug": "concept/topic"}) == nil {
		t.Fatal("page ID and slug must be alternative locators")
	}
}

func TestSystemPluginOperationsCannotBeDiscoveredOrGranted(t *testing.T) {
	for _, id := range []string{"plugins.catalog.search", "plugins.catalog.get", "plugins.installations.install", "plugins.installations.get"} {
		if _, ok := Find(id); ok {
			t.Errorf("removed system plugin operation remains discoverable: %s", id)
		}
	}
}
