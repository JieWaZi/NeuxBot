package workspace

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	catalog "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/catalog"

	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"

	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func TestPrepareRuntimeFilesLayersVirtualTeamInstructionsIntoRuntimeFile(t *testing.T) {
	workspacePath := t.TempDir()
	registry, err := catalog.New(catalog.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sdk := New(registry.Runtimes())
	var codex runtimeconfig.RuntimeSummary
	for _, runtime := range registry.Runtimes() {
		if runtime.ID == runtimeconfig.RuntimeIDCodex {
			codex = runtime
			break
		}
	}
	teamContent := "协作优先级\n\n# Team: 测试团队"
	capabilityContent := constants.ChannelTasksMCPCapabilityInstructions
	err = sdk.prepareRuntimeFiles(
		runtimeconfig.DetectionResult{Runtime: codex},
		runtimecontract.Workspace{Path: workspacePath, Managed: true},
		[]runtimecontract.Instruction{
			{Name: "IDENTITY.md", Content: "伙伴身份"},
			{Name: "PERSONA.md", Content: "工作风格"},
			{Name: "PLAYBOOK.md", Content: "工作方式"},
			{Name: constants.TeamInstructionFileName, Content: teamContent},
			{Name: constants.ChannelTasksMCPTitle, Content: capabilityContent},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("sdk.prepareRuntimeFiles() error = %v", err)
	}
	agentsPath := filepath.Join(workspacePath, codex.InstructionFileName)
	agentsText, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	agents := string(agentsText)
	for _, expected := range []string{
		"temporary or unrelated to any project must be created under `attachments/generated/`",
		"伙伴身份",
		"工作风格",
		"工作方式",
		"## TEAM.md",
		"协作优先级",
		"# Team: 测试团队",
		"## NeuxBot Team Tasks",
		capabilityContent,
	} {
		if !strings.Contains(agents, expected) {
			t.Fatalf("AGENTS.md = %q, want %q", agents, expected)
		}
	}
	for _, forbidden := range []string{
		"## IDENTITY.md",
		"## PERSONA.md",
		"## PLAYBOOK.md",
	} {
		if strings.Contains(agents, forbidden) {
			t.Fatalf("AGENTS.md = %q, must not contain %q", agents, forbidden)
		}
	}
	teamPath := filepath.Join(workspacePath, constants.TeamInstructionFileName)
	if _, err := os.Stat(teamPath); !os.IsNotExist(err) {
		t.Fatalf("virtual TEAM.md must not be materialized: %v", err)
	}
}

func TestPrepareRuntimeFilesRemovesStaleTeamMD(t *testing.T) {
	workspacePath := t.TempDir()
	teamPath := filepath.Join(workspacePath, constants.TeamInstructionFileName)
	if err := os.WriteFile(teamPath, []byte("stale team"), 0o600); err != nil {
		t.Fatalf("seed TEAM.md: %v", err)
	}
	registry, err := catalog.New(catalog.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sdk := New(registry.Runtimes())
	var codex runtimeconfig.RuntimeSummary
	for _, runtime := range registry.Runtimes() {
		if runtime.ID == runtimeconfig.RuntimeIDCodex {
			codex = runtime
			break
		}
	}
	err = sdk.prepareRuntimeFiles(
		runtimeconfig.DetectionResult{Runtime: codex},
		runtimecontract.Workspace{Path: workspacePath, Managed: true},
		[]runtimecontract.Instruction{{Name: "IDENTITY.md", Content: "solo chat"}},
		nil,
	)
	if err != nil {
		t.Fatalf("sdk.prepareRuntimeFiles() error = %v", err)
	}
	if _, err := os.Stat(teamPath); !os.IsNotExist(err) {
		t.Fatalf("stale TEAM.md should be removed: %v", err)
	}
}

// TestPrepareRuntimeInstructionsStayStableAcrossTurns 验证相同结构化输入不会生成每轮变化的模型指令。
func TestPrepareRuntimeInstructionsStayStableAcrossTurns(t *testing.T) {
	registry, err := catalog.New(catalog.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sdk := New(registry.Runtimes())
	for _, runtime := range registry.Runtimes() {
		runtime := runtime
		t.Run(runtime.ID, func(t *testing.T) {
			workspacePath := t.TempDir()
			workspace := runtimecontract.Workspace{Path: workspacePath, Managed: true}
			instructions := []runtimecontract.Instruction{{Name: "IDENTITY.md", Content: "稳定伙伴档案"}}
			var first []byte
			for turn := range 2 {
				if prepareErr := sdk.prepareRuntimeFiles(
					runtimeconfig.DetectionResult{Runtime: runtime},
					workspace,
					instructions,
					nil,
				); prepareErr != nil {
					t.Fatalf("prepare turn %d: %v", turn+1, prepareErr)
				}
				content, readErr := os.ReadFile(filepath.Join(workspacePath, runtime.InstructionFileName))
				if readErr != nil {
					t.Fatalf("read turn %d instructions: %v", turn+1, readErr)
				}
				if turn == 0 {
					first = append([]byte(nil), content...)
					continue
				}
				if string(content) != string(first) {
					t.Fatalf("instructions changed across turns:\nfirst=%q\nsecond=%q", first, content)
				}
			}
		})
	}
}

func TestPrepareRuntimeFilesLinksCurrentSkills(t *testing.T) {
	workspacePath := t.TempDir()
	skillPath := filepath.Join(t.TempDir(), "review")
	if err := os.MkdirAll(skillPath, 0o700); err != nil {
		t.Fatalf("create skill directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte("# Review"), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	registry, err := catalog.New(catalog.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sdk := New(registry.Runtimes())
	var codex runtimeconfig.RuntimeSummary
	for _, runtime := range registry.Runtimes() {
		if runtime.ID == runtimeconfig.RuntimeIDCodex {
			codex = runtime
		}
	}
	err = sdk.prepareRuntimeFiles(
		runtimeconfig.DetectionResult{
			Runtime:      codex,
			Capabilities: runtimeconfig.CapabilitySet{runtimeconfig.CapabilitySkillDirectory: true},
		},
		runtimecontract.Workspace{Path: workspacePath, Managed: true},
		nil,
		[]runtimecontract.Skill{{Name: "review", Path: skillPath}},
	)
	if err != nil {
		t.Fatalf("sdk.prepareRuntimeFiles() error = %v", err)
	}
	target := filepath.Join(runtimeSkillRoot(runtimeconfig.DetectionResult{Runtime: codex}, workspacePath), "review")
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("lstat linked skill: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("skill target mode = %v, want symbolic link", info.Mode())
	}
	linked, err := filepath.EvalSymlinks(target)
	resolvedSource, sourceErr := filepath.EvalSymlinks(skillPath)
	if err != nil || sourceErr != nil || linked != resolvedSource {
		t.Fatalf("linked skill = %q, %v", linked, err)
	}
	content, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil || string(content) != "# Review" {
		t.Fatalf("materialized skill = %q, %v", content, err)
	}

}

func TestPrepareRuntimeFilesReplacesPreviousSkillVersionOnNextCall(t *testing.T) {
	workspacePath, versions := t.TempDir(), t.TempDir()
	registry, err := catalog.New(catalog.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sdk := New(registry.Runtimes())
	var runtime runtimeconfig.RuntimeSummary
	for _, candidate := range registry.Runtimes() {
		if candidate.ID == runtimeconfig.RuntimeIDCodex {
			runtime = candidate
			break
		}
	}
	for _, version := range []string{"1.0.0", "1.0.1"} {
		source := filepath.Join(versions, version)
		if err := os.MkdirAll(source, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(version), 0o600); err != nil {
			t.Fatal(err)
		}
		err = sdk.prepareRuntimeFiles(
			runtimeconfig.DetectionResult{Runtime: runtime, Capabilities: runtimeconfig.CapabilitySet{runtimeconfig.CapabilitySkillDirectory: true}},
			runtimecontract.Workspace{Path: workspacePath, Managed: true}, nil,
			[]runtimecontract.Skill{{Name: "review", Path: source}},
		)
		if err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(filepath.Join(runtimeSkillRoot(runtimeconfig.DetectionResult{Runtime: runtime}, workspacePath), "review", "SKILL.md"))
		if err != nil || string(content) != version {
			t.Fatalf("Runtime kept stale skill: got %q want %q error=%v", content, version, err)
		}
	}
}

// TestPrepareRuntimeFilesWritesProjectRoutingForCodexAndClaude 验证所有 Runtime 使用同一份项目文件路由规则。
func TestPrepareRuntimeFilesWritesProjectRoutingForCodexAndClaude(t *testing.T) {
	workspacePath := t.TempDir()
	for _, relativePath := range []string{"projects/app", "projects/docs"} {
		if err := os.MkdirAll(filepath.Join(workspacePath, relativePath), 0o700); err != nil {
			t.Fatalf("create project directory %q: %v", relativePath, err)
		}
	}
	registry, err := catalog.New(catalog.Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sdk := New(registry.Runtimes())

	for _, runtimeID := range []string{runtimeconfig.RuntimeIDCodex, runtimeconfig.RuntimeIDClaude} {
		t.Run(runtimeID, func(t *testing.T) {
			var summary runtimeconfig.RuntimeSummary
			for _, candidate := range registry.Runtimes() {
				if candidate.ID == runtimeID {
					summary = candidate
					break
				}
			}
			err := sdk.prepareRuntimeFiles(
				runtimeconfig.DetectionResult{Runtime: summary},
				runtimecontract.Workspace{
					Path:    workspacePath,
					Managed: true,
					ProjectDirectories: []runtimecontract.WorkspaceProjectDirectory{
						{Path: "projects/app", Default: true},
						{Path: "projects/docs"},
					},
				},
				nil,
				nil,
			)
			if err != nil {
				t.Fatalf("sdk.prepareRuntimeFiles() error = %v", err)
			}
			content, err := os.ReadFile(filepath.Join(workspacePath, summary.InstructionFileName))
			if err != nil {
				t.Fatalf("read Runtime instructions: %v", err)
			}
			text := string(content)
			for _, expected := range []string{
				"`projects/app` (default project directory)",
				"`projects/docs`",
				"New or modified files that clearly belong to a project must be written under that project's directory",
				"temporary or unrelated to any project must be created under `attachments/generated/`",
				"Other paths under `attachments/` contain user-provided files; do not overwrite them",
				"Do not create user deliverables in the NeuxBot Chat workspace root",
			} {
				if !strings.Contains(text, expected) {
					t.Fatalf("Runtime instructions = %q, want %q", text, expected)
				}
			}
			for _, forbidden := range []string{"projects/share", "select the share directory"} {
				if strings.Contains(text, forbidden) {
					t.Fatalf("Runtime instructions = %q, must not contain %q", text, forbidden)
				}
			}
		})
	}
}

func TestLinkOrCopyRuntimeSkillFallsBackForExistingTarget(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	target := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("# Review"), 0o600); err != nil {
		t.Fatalf("write source skill: %v", err)
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("create conflicting target: %v", err)
	}
	if err := linkOrCopyRuntimeSkill(source, target); err != nil {
		t.Fatalf("linkOrCopyRuntimeSkill() error = %v", err)
	}
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("lstat copied skill: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("fallback target mode = %v, want copied directory", info.Mode())
	}
	content, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatalf("read copied skill: %v", err)
	}
	if string(content) != "# Review" {
		t.Fatalf("copied skill = %q", content)
	}
}

func TestRuntimeSkillPreservesExecutableAndKnowledgeReference(t *testing.T) {
	source := t.TempDir()
	resources := map[string]string{
		"SKILL.md":                 "# Skill\nRead references/wiki-guide.md and run scripts/allycli.",
		"references/wiki-guide.md": "Knowledge reference fixture",
		"scripts/allycli":          "CLI fixture",
	}
	for name, content := range resources {
		path := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, copyFallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("copy=%t", copyFallback), func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "ally")
			if copyFallback {
				if err := os.MkdirAll(target, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := linkOrCopyRuntimeSkill(source, target); err != nil {
				t.Fatal(err)
			}
			for name, want := range resources {
				path := filepath.Join(target, filepath.FromSlash(name))
				content, err := os.ReadFile(path)
				if err != nil || string(content) != want {
					t.Fatalf("skill resource unavailable: %s %v", name, err)
				}
			}
			info, err := os.Stat(filepath.Join(target, "scripts", "allycli"))
			if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0) {
				t.Fatalf("skill CLI lost execution permission: %v", err)
			}
		})
	}
}
