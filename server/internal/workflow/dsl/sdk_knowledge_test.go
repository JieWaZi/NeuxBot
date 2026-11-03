package dsl

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/internal/workflow/catalog"
)

func TestSDKReviewExampleUsesTheSkillAuthorityAndRejectsLegacyFixtures(t *testing.T) {
	source, err := SDKExample("review")
	if err != nil {
		t.Fatalf("current review example is not available through SDK: %v", err)
	}
	authority, err := os.ReadFile("../../../../skills/ally/references/workflow-review-example.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	if source != string(authority) {
		t.Fatal("SDK example diverged from the authoritative skill source; regenerate the SDK")
	}
	if _, diagnostics, err := Compile(source); err != nil {
		t.Fatalf("current SDK example cannot compile: %v %+v", err, diagnostics)
	}
	for _, retired := range []string{"weekly-report", "environment-review", "environment-release"} {
		if _, err := SDKExample(retired); err == nil {
			t.Fatalf("legacy integration fixture is still advertised as authoring guidance: %s", retired)
		}
	}
}

func TestSDKScriptActionPythonSnippetActuallyWritesOneJSONValue(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip(err)
	}
	content, err := SDKBundle([]string{"action"})
	if err != nil {
		t.Fatal(err)
	}
	_, after, found := strings.Cut(content, "```python\n")
	if !found {
		t.Fatal("script action guidance has no complete Python stdin/stdout example")
	}
	code, _, found := strings.Cut(after, "```")
	if !found {
		t.Fatal("Python example is incomplete")
	}
	for _, input := range []any{nil, false, float64(0), "", map[string]any{"text": "虚构输入"}} {
		encoded, _ := json.Marshal(input)
		command := exec.Command(python, "-c", code)
		command.Stdin = bytes.NewReader(encoded)
		output, err := command.Output()
		if err != nil {
			t.Fatalf("SDK script failed: %v", err)
		}
		var got any
		if json.Unmarshal(output, &got) != nil || !reflect.DeepEqual(got, input) {
			t.Fatalf("SDK action did not echo exactly one JSON value: input=%#v output=%q", input, output)
		}
	}
}

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
		if err != nil {
			t.Fatalf("invalid example %q: %v", topic, err)
		}
		if _, diagnostics, err := Compile(source); err != nil {
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
	if _, err := SDKTopic("presentation"); err == nil {
		t.Fatal("removed presentation topic is still discoverable")
	}
	content, err = SDKBundle(catalog.AuthoringTopics([]string{"action"}, []string{"im"}))
	if err != nil || !strings.Contains(content, "imTargetIds") || !strings.Contains(content, "im_delivery") {
		t.Fatal("IM authoring lost its target authorization contract")
	}
}
