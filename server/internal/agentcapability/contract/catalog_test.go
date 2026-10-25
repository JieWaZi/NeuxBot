package contract

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func TestAllyCLIReadmeMatchesRegisteredOperations(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "cmd", "allycli", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(content), "<!-- allycli-operations:start -->")
	if !ok {
		t.Fatal("README is missing the operation inventory")
	}
	section, _, ok = strings.Cut(section, "<!-- allycli-operations:end -->")
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

func TestMemoryScopeContractsDistinguishSearchAndOwnership(t *testing.T) {
	search, _ := Find("memory.entries.search")
	create, _ := Find("memory.entries.create")
	for _, scope := range []constants.MemoryScope{constants.MemoryScopePartner, constants.MemoryScopeProject, constants.MemoryScopeAll} {
		if err := search.Validate(map[string]any{"query": "preferences", "scope": scope.String()}); err != nil {
			t.Fatalf("visible search scope rejected: %s: %v", scope, err)
		}
	}
	if search.Validate(map[string]any{"query": "preferences"}) != nil ||
		search.Discovery["defaultScope"] != constants.MemoryScopeAll.String() {
		t.Fatal("omitted search scope lost its all-visible default")
	}
	for _, scope := range []constants.MemoryScope{constants.MemoryScopePartner, constants.MemoryScopeProject} {
		if create.Validate(map[string]any{"scope": scope.String(), "content": "Stable fact"}) != nil {
			t.Fatalf("memory ownership scope rejected: %s", scope)
		}
	}
	for _, scope := range []string{"all", "", "global"} {
		if create.Validate(map[string]any{"scope": scope, "content": "Stable fact"}) == nil {
			t.Fatalf("invalid memory ownership accepted: %q", scope)
		}
	}
	if create.Validate(map[string]any{"content": "Stable fact"}) == nil ||
		search.Validate(map[string]any{"query": "preferences", "scope": "global"}) == nil {
		t.Fatal("missing ownership or unknown search scope accepted")
	}
	for _, op := range []Operation{search, create} {
		properties := op.InputSchema["properties"].(map[string]any)
		scope := properties["scope"].(map[string]any)
		if !reflect.DeepEqual(op.Discovery["scopes"], scope["enum"]) {
			t.Fatalf("capabilities scope choices differ from validation: %s", op.ID)
		}
	}
}

func TestWorkflowSDKTopicsAreDiscoveredAndCheckedByKind(t *testing.T) {
	op, _ := Find("workflow.sdk.get")
	for _, params := range []map[string]any{
		{"topic": "function"}, {"topic": "overview", "kind": "sdk"},
		{"topic": "f0", "kind": "example"},
	} {
		if err := op.Validate(params); err != nil {
			t.Fatalf("advertised SDK input rejected: %v: %v", params, err)
		}
	}
	for _, params := range []map[string]any{
		{"topic": "functionTask"}, {"topic": "index"}, {"topic": "phase"},
		{"topic": "f0"}, {"topic": "function", "kind": "example"},
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
