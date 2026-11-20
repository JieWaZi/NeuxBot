package builtin

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	managedskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
	"github.com/JieWaZi/NeuxBot/server/internal/storage"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
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
	guide, err := os.ReadFile(filepath.Join(first, "references", "wiki-guide.md"))
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
	item, err := managedskills.SystemSkillDefinition()
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
	for _, operation := range capability.Operations() {
		for _, id := range operation.Guides {
			if content, err := Guide(id); err != nil || content == "" {
				t.Errorf("operation guide unavailable: %s -> %s: %v", operation.ID, id, err)
			}
		}
	}
}

func TestSystemSkillRejectsModifiedResourcesAndRedirectedDirectory(t *testing.T) {
	data := t.TempDir()
	binary := filepath.Join(t.TempDir(), "botcli")
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

func TestSystemSkillCleanupKeepsPublishedAndUnknownDirectories(t *testing.T) {
	data := t.TempDir()
	binary := filepath.Join(data, "botcli")
	if err := os.WriteFile(binary, []byte("release one"), 0600); err != nil {
		t.Fatal(err)
	}
	older, err := SystemDirectory(data, binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("release two"), 0600); err != nil {
		t.Fatal(err)
	}
	current, err := SystemDirectory(data, binary)
	if err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(filepath.Dir(current), strings.Repeat("a", 64))
	if err := os.Mkdir(unknown, 0700); err != nil {
		t.Fatal(err)
	}
	unknownStage := filepath.Join(filepath.Dir(current), "system-skill-staging-100")
	if err := os.Mkdir(unknownStage, 0700); err != nil {
		t.Fatal(err)
	}
	ownedStage := filepath.Join(filepath.Dir(current), "system-skill-staging-101")
	if err := os.Mkdir(ownedStage, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ownedStage, systemStagingOwner), []byte("system-skill-staging-101"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CleanupObsoleteSystemDirectories(current); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(older); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("valid old version remained", err)
	}
	if _, err := os.Stat(ownedStage); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("verified stage remained", err)
	}
	for _, path := range []string{current, unknown, unknownStage} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("protected path disappeared", err)
		}
	}
}

func TestMeasureVerifiesMaterializedSystemSkillFiles(t *testing.T) {
	data, managed := t.TempDir(), t.TempDir()
	binary := filepath.Join(t.TempDir(), "botcli")
	if err := os.WriteFile(binary, []byte("CLI test fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	published, err := SystemDirectory(data, binary)
	if err != nil {
		t.Fatal(err)
	}
	_, digest, err := managedskills.SelectedDigests(published, managedskills.IsSystemSkillDocument)
	if err != nil {
		t.Fatal(err)
	}

	partner := filepath.Join(managed, "Agents", "waker-a")
	snapshot := ".neuxbot/skills/" + digest
	for _, target := range []string{snapshot, ".cursor/skills/bot", ".agents/skills/bot", ".cursor/skills/another", ".neuxbot/skills/" + filepath.Base(published)} {
		if err := managedskills.CopySelectedDirectory(published, filepath.Join(partner, target), managedskills.IsSystemSkillDocument); err != nil {
			t.Fatal(err)
		}
	}
	const reference = "references/workflow-authoring.md"
	tampered := filepath.Join(partner, ".cursor/skills/bot", reference)
	document, err := os.ReadFile(tampered)
	if err != nil {
		t.Fatal(err)
	}
	document[0] ^= 1
	if err := os.WriteFile(tampered, document, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partner, ".cursor", "skills", "bot", "references", "user-notes.md"), []byte("user notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(partner, snapshot, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partner, snapshot, "scripts", "botcli"), []byte("user file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(partner, ".claude", "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Exercise native discovery links when the platform permits creating them.
	if err := os.Symlink(filepath.Join("..", "..", filepath.FromSlash(snapshot)), filepath.Join(partner, ".claude", "skills", "bot")); err != nil && runtime.GOOS != "windows" {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"version":1,"targets":{".cursor/skills":{"bot":%q},".claude/skills":{"bot":%q}}}`, digest, digest)
	member := filepath.Join(managed, "Workspaces", "thread-a", "members", "waker-a")
	if err := workspace.EnsureThreadMemberExecutionWorkspace(member, "thread-a", "chat-a", "waker-a"); err != nil {
		t.Fatal(err)
	}
	unmanaged := filepath.Join(partner, "project")
	for _, directory := range []string{member, unmanaged} {
		if err := managedskills.CopySelectedDirectory(published, filepath.Join(directory, snapshot), managedskills.IsSystemSkillDocument); err != nil {
			t.Fatal(err)
		}
	}
	for _, directory := range []string{partner, member, unmanaged} {
		if err := os.WriteFile(filepath.Join(directory, managedskills.MaterializationManifestName), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := storage.Measure(t.Context(), storage.Roots{Data: data, Managed: managed}, storage.Limits{})
	if err != nil || !got.Complete {
		t.Fatalf("measurement=%+v, error=%v", got, err)
	}
	categories := map[string]string{}
	for _, file := range got.Entries {
		if file.OwnerRoot != got.Roots.Managed {
			continue
		}
		relative, err := filepath.Rel(got.Roots.Managed, file.Path)
		if err != nil {
			t.Fatal(err)
		}
		categories[filepath.ToSlash(relative)] = string(file.Category)
	}
	const partnerPrefix = "Agents/waker-a/"
	for path, want := range map[string]string{
		partnerPrefix + snapshot + "/SKILL.md":                                      "programs",
		partnerPrefix + snapshot + "/" + reference:                                  "programs",
		partnerPrefix + ".cursor/skills/bot/SKILL.md":                               "programs",
		partnerPrefix + ".cursor/skills/bot/" + reference:                           "work_files",
		partnerPrefix + ".cursor/skills/bot/references/user-notes.md":               "work_files",
		partnerPrefix + ".cursor/skills/another/SKILL.md":                           "work_files",
		partnerPrefix + ".agents/skills/bot/SKILL.md":                               "work_files",
		partnerPrefix + ".claude/skills/bot/SKILL.md":                               "",
		partnerPrefix + ".neuxbot/skills/" + filepath.Base(published) + "/SKILL.md": "work_files",
		partnerPrefix + snapshot + "/scripts/botcli":                                "work_files",
		partnerPrefix + "project/" + snapshot + "/SKILL.md":                         "work_files",
		"Workspaces/thread-a/members/waker-a/" + snapshot + "/SKILL.md":             "programs",
	} {
		if got := categories[path]; got != want {
			t.Errorf("%s category=%q, want %q", path, got, want)
		}
	}
}
