package builtin

import (
	"bytes"
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability/contract"
)

func TestSystemSkillVersionsCLIAndRejectsTampering(t *testing.T) {
	data := t.TempDir()
	binary := filepath.Join(t.TempDir(), "source-cli")
	if err := os.WriteFile(binary, []byte("first CLI"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := SystemDirectory(data, binary)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(CLIPath(first))
	if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0) {
		t.Fatalf("CLI is not a standalone executable: %v %v", info, err)
	}
	guide, err := os.ReadFile(filepath.Join(first, "references", "knowledge.md"))
	if err != nil {
		t.Fatal(err)
	}
	wantGuide, err := Guide("knowledge")
	if err != nil || strings.TrimSpace(string(guide)) != wantGuide {
		t.Fatalf("skill reference differs from live guide: %v", err)
	}
	if err := os.WriteFile(binary, []byte("second CLI"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := SystemDirectory(data, binary)
	if err != nil || second == first {
		t.Fatalf("CLI update reused previous skill: %v", err)
	}
	for directory, want := range map[string]string{first: "first CLI", second: "second CLI"} {
		content, err := os.ReadFile(CLIPath(directory))
		if err != nil || !bytes.Equal(content, []byte(want)) {
			t.Fatalf("CLI version was overwritten: %q %v", content, err)
		}
	}
	if err := os.WriteFile(CLIPath(second), []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := SystemDirectory(data, binary); !errors.Is(err, status_error.SkillContentInvalid) {
		t.Fatalf("modified CLI accepted: %v", err)
	}
}

func TestSystemGuidesAndRelativeLinksResolveBundledResources(t *testing.T) {
	item, err := systemDefinition()
	if err != nil {
		t.Fatal(err)
	}
	resources := make(map[string]bool, len(item.Files))
	for _, resource := range item.Files {
		resources[resource.Path] = true
	}
	links := regexp.MustCompile(`\]\(([^)]+)\)`)
	for _, resource := range item.Files {
		for _, match := range links.FindAllSubmatch(resource.Data, -1) {
			target := strings.SplitN(string(match[1]), "#", 2)[0]
			if target == "" || strings.Contains(target, "://") {
				continue
			}
			if !resources[path.Clean(path.Join(path.Dir(resource.Path), target))] {
				t.Errorf("broken bundled link: %s -> %s", resource.Path, target)
			}
		}
	}
	for _, operation := range contract.Operations() {
		for _, id := range operation.Guides {
			if content, err := Guide(id); err != nil || content == "" {
				t.Errorf("operation guide unavailable: %s -> %s: %v", operation.ID, id, err)
			}
		}
	}
}

func TestSystemSkillRejectsModifiedResourcesAndRedirectedDirectory(t *testing.T) {
	data := t.TempDir()
	binary := filepath.Join(t.TempDir(), "allycli")
	if err := os.WriteFile(binary, []byte("cli fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	directory, err := SystemDirectory(data, binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SystemDirectory(data, binary); !errors.Is(err, status_error.SkillContentInvalid) {
		t.Fatalf("modified system skill accepted: %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	data = t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(data, "system-skills")); err != nil {
		t.Fatal(err)
	}
	if _, err := SystemDirectory(data, binary); !errors.Is(err, status_error.SkillContentInvalid) {
		t.Fatalf("redirected system skill directory accepted: %v", err)
	}
}
