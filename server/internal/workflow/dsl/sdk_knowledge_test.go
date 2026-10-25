package dsl

import (
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/internal/workflow/catalog"
)

func TestSDKQueriesReturnFocusedGeneratedGuidanceAndCompilableExamples(t *testing.T) {
	for _, topic := range SDKTopics() {
		text, err := SDKTopic(topic)
		if err != nil || !strings.HasPrefix(text, "# Workflow SDK: "+topic) {
			t.Fatalf("generated topic %q missing: %q %v", topic, text, err)
		}
		if len(text) >= len(SDKArtifact("creation-knowledge.md")) {
			t.Fatalf("topic %q returned the complete knowledge document", topic)
		}
	}
	worker, _ := SDKTopic("worker")
	if !strings.Contains(worker, "declare function worker") || strings.Contains(worker, "declare function askUser") || strings.Contains(worker, "interface WorkflowMeta") {
		t.Fatalf("worker topic has incorrect declarations: %s", worker)
	}
	for _, topic := range SDKExampleTopics() {
		source, err := SDKExample(topic)
		if err != nil || strings.Contains(source, "workflow_result") {
			t.Fatalf("invalid example %q: %v", topic, err)
		}
		if _, diagnostics, err := CompileForAuthoring(source); err != nil {
			t.Fatalf("uncompilable queried example %q: %v %+v", topic, err, diagnostics)
		}
	}
	if _, err := SDKTopic("../creation-knowledge"); err == nil {
		t.Fatal("unknown SDK topic fell back to full knowledge")
	}
	if _, err := SDKExample("unknown"); err == nil {
		t.Fatal("unknown example silently substituted")
	}
}

func TestAuthoringBundleSelectsActualPrimitivesAndDeduplicatesSharedRules(t *testing.T) {
	mapping := catalog.PrimitiveTopics()
	for _, primitive := range Registry() {
		if mapping[primitive.Name] == "" {
			t.Fatalf("primitive is missing from discovery: %s", primitive.Name)
		}
	}
	topics := catalog.AuthoringTopics([]string{"functionTask"}, []string{"files"})
	content, err := SDKBundle(append(topics, "function", "overview"))
	if err != nil || strings.Count(content, "declare function functionTask") != 1 ||
		strings.Count(content, "This is a restricted DSL") != 1 {
		t.Fatalf("shared SDK rules were duplicated: %v\n%s", err, content)
	}
	if strings.Contains(content, "declare function worker") ||
		strings.Contains(content, "# Workflow SDK: presentation") ||
		strings.Contains(content, "x-ui-control") {
		t.Fatal("simple file/function authoring loaded worker, page Catalog or advanced form controls")
	}
	content, err = SDKBundle(catalog.AuthoringTopics([]string{"worker"}, []string{"presentation"}))
	if err != nil || !strings.Contains(content, "# Workflow SDK: presentation") ||
		!strings.Contains(content, "declare function worker") {
		t.Fatal("page authoring did not include the actual presentation Catalog")
	}
	content, err = SDKBundle(catalog.AuthoringTopics([]string{"action"}, []string{"im"}))
	if err != nil || !strings.Contains(content, "imTargetIds") || !strings.Contains(content, "im_delivery") {
		t.Fatal("IM authoring lost its target authorization contract")
	}
}
