package runtimes

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
)

// TestGeminiPluginFallbackDiscovery 用真实CLI只读发现生产Skill投射，不发起模型调用。
func TestGeminiPluginFallbackDiscovery(t *testing.T) {
	if os.Getenv("NEUXALLY_GEMINI_PLUGIN_ACCEPTANCE") == "" {
		t.Skip("explicit isolated Gemini skill discovery only")
	}
	binary, err := exec.LookPath("gemini")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	source := filepath.Join(root, "source")
	for name, content := range map[string]string{".codex-plugin/plugin.json": `{"name":"fallback-acceptance","version":"1"}`, "skills/native-probe/SKILL.md": "---\nname: native-probe\ndescription: Verify Skill fallback discovery.\n---\nRead references/rules.md.", "skills/native-probe/references/rules.md": "Complete relative resource preserved."} {
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	stage, err := plugins.Stage(filepath.Join(root, "staging"), source)
	if err != nil {
		t.Fatal(err)
	}
	pkg, pkgRoot, err := plugins.SelectTarget(stage.Root, stage.Package, RuntimeIDGemini)
	if err != nil {
		t.Fatal(err)
	}
	sdk, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close(context.Background())
	var runtime RuntimeSummary
	for _, summary := range sdk.Runtimes() {
		if summary.ID == RuntimeIDGemini {
			runtime = summary
		}
	}
	skills := []Skill{}
	for _, skill := range pkg.Skills {
		skills = append(skills, Skill{Name: skill.Name, Path: filepath.Join(pkgRoot, skill.Path)})
	}
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := sdk.prepareRuntimeFiles(DetectionResult{Runtime: runtime, State: DetectionStateReady, Capabilities: CapabilitySet{CapabilitySkillDirectory: true}}, Workspace{Path: workspace, Managed: true}, nil, skills); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".gemini/skills/native-probe/references/rules.md")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "skills", "list")
	command.WaitDelay = time.Second
	command.Dir = workspace
	environment, err := accountconfig.Prepare(workspace, accountconfig.Spec{RuntimeID: constants.RuntimeIDGemini, ID: "fallback-fixture", Revision: 1, AuthMode: constants.AccountAuthModeAPIKey, Protocol: constants.AccountProtocolGoogleGenerativeAI, BaseURL: "http://127.0.0.1:1/v1beta", APIKey: "local-placeholder-not-a-secret", Model: "gemini-3.8-flash", SelectedModels: []string{"gemini-3.8-flash"}})
	if err != nil {
		t.Fatal(err)
	}
	command.Env = append(accountconfig.Environment(os.Environ()), "GEMINI_CLI_NO_RELAUNCH=1")
	for key, value := range environment {
		command.Env = append(command.Env, key+"="+value)
	}
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "native-probe") {
		t.Fatalf("real fallback discovery: %s %v", output, err)
	}
	version, err := exec.CommandContext(ctx, binary, "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Gemini %s real CLI discovered production Skill projection: %s", strings.TrimSpace(string(version)), strings.TrimSpace(string(output)))
}
