package native

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestTaskSystemInstructionsKeepNativeCodexRoleAndSerializeOtherCLIs(t *testing.T) {
	messages := []Message{{Role: "system", Content: "system instruction"}, {Role: "user", Content: "current task"}}
	codex, err := CompileTaskMessages("codex", messages)
	if err != nil {
		t.Fatal(err)
	}
	if codex.Text != "current task" || len(codex.Instructions) != 1 {
		t.Fatalf("codex=%+v", codex)
	}
	workspace := t.TempDir()
	config, err := ConfigureTaskInstructions(Config{RuntimeID: "codex", WorkingDirectory: workspace, CodexConfiguration: map[string]string{"preserved": "true"}}, codex.Instructions)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "model-instructions.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "system instruction" || config.CodexConfiguration["model_instructions_file"] != strconv.Quote(path) || config.CodexConfiguration["preserved"] != "true" {
		t.Fatalf("native configuration=%+v contents=%s", config.CodexConfiguration, data)
	}
	other, err := CompileTaskMessages("kimi", messages)
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Instructions) != 0 || !strings.Contains(other.Text, "<message role=\"system\">\nsystem instruction") || !strings.Contains(other.Text, "current task") {
		t.Fatalf("other=%+v", other)
	}
}
func TestTaskInstructionConfigurationDoesNotMutateFrozenOverrides(t *testing.T) {
	overrides := map[string]string{"unchanged": "true"}
	_, err := ConfigureTaskInstructions(Config{RuntimeID: "codex", WorkingDirectory: t.TempDir(), CodexConfiguration: overrides}, []string{"one"})
	if err != nil {
		t.Fatal(err)
	}
	if len(overrides) != 1 {
		t.Fatal("native helper mutated frozen host configuration")
	}
}
func TestNativeSelectedSkillSyntaxComesFromDescriptor(t *testing.T) {
	for id, want := range map[string]string{"codex": "$", "kimi": "/skill:", "pi": "/skill:", "claude": ""} {
		descriptor, ok := Lookup(id)
		if !ok || descriptor.SkillPrefix != want {
			t.Fatalf("%s skill syntax=%q want=%q", id, descriptor.SkillPrefix, want)
		}
	}
}
