package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	managedskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
)

func TestManagedSkillsRemainDiscoverableAfterProbeFailure(t *testing.T) {
	cwd, source := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("published skill"), 0600); err != nil {
		t.Fatal(err)
	}
	detection := DetectionResult{Runtime: RuntimeSummary{SkillDirectoryName: ".agents"}, State: DetectionStateUnavailable}
	if err := prepareTestSkills(detection, Workspace{Path: cwd, Managed: true}, []Skill{{Name: "demo", Path: source}}); err != nil {
		t.Fatalf("failed diagnostic probe blocked skill publication: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(cwd, ".agents", "skills", "demo", "SKILL.md"))
	if err != nil || string(body) != "published skill" {
		t.Fatalf("registered discovery directory lost skill: %q %v", body, err)
	}
}

func TestPublishedSkillDeclarationNeedsNoSourceIOAndColdPublishChecksContent(t *testing.T) {
	cwd, source := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("published skill"), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := managedskills.Digest(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(source, source+"-offline"); err != nil {
		t.Fatal(err)
	}
	detection := DetectionResult{Runtime: RuntimeSummary{SkillDirectoryName: ".agents"}, Capabilities: CapabilitySet{CapabilitySkillDirectory: true}}
	selection, err := FreezeSkills(detection, Workspace{Path: cwd, Managed: true}, []Skill{{Name: "demo", Path: source, Digest: digest}}, nil)
	if err != nil {
		t.Fatalf("published declaration read source content: %v", err)
	}
	if err := os.Rename(source+"-offline", source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := prepareSkills(selection, nil); !errors.Is(err, status_error.SkillContentInvalid) {
		t.Fatalf("cold publication accepted changed immutable content: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".agents", "skills", "demo")); !os.IsNotExist(err) {
		t.Fatalf("tampered skill became visible: %v", err)
	}
}

func TestManagedSkillsPreserveWorkspaceFilesAndIsolateSources(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", "TEAM.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("user instructions"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	user := filepath.Join(root, ".claude", "skills", "user-skill")
	if err := os.MkdirAll(user, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("version one"), 0600); err != nil {
		t.Fatal(err)
	}
	request := struct {
		// Workspace is the authorized managed directory.
		Workspace Workspace
		// Detection supplies the native skill delivery capabilities.
		Detection DetectionResult
		// Skills names the source content to publish.
		Skills []Skill
	}{Workspace: Workspace{Path: root, Managed: true}, Detection: DetectionResult{Runtime: RuntimeSummary{BaseRuntimeID: "claude", InstructionFileName: "CLAUDE.md", SkillDirectoryName: ".claude"}, Capabilities: CapabilitySet{CapabilitySkillDirectory: true}}, Skills: []Skill{{Name: "managed", Path: source}}}
	if err := prepareTestSkills(request.Detection, request.Workspace, request.Skills); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", "TEAM.md"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(data) != "user instructions" {
			t.Fatalf("user instructions changed: %s %q %v", name, data, err)
		}
	}
	if _, err := os.Stat(user); err != nil {
		t.Fatalf("unmanaged skill removed: %v", err)
	}
	target := filepath.Join(root, ".claude", "skills", "managed")
	info, err := os.Lstat(target)
	if err != nil || (runtime.GOOS != "windows" && info.Mode()&os.ModeSymlink == 0) {
		t.Fatalf("skill projection is missing: %v %v", info, err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("version two"), 0600); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if string(data) != "version one" {
		t.Fatalf("source mutated installed skill: %q", data)
	}
	if err := prepareTestSkills(request.Detection, request.Workspace, request.Skills); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(target, "SKILL.md"))
	if string(data) != "version two" {
		t.Fatalf("skill update missing: %q", data)
	}
	request.Skills = nil
	if err := prepareTestSkills(request.Detection, request.Workspace, request.Skills); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("disabled managed skill remains: %v", err)
	}
	if _, err := os.Stat(user); err != nil {
		t.Fatalf("disable removed user skill: %v", err)
	}
}

func TestManagedSkillsRejectCollisionsAndModifiedInstalls(t *testing.T) {
	for _, modified := range []bool{false, true} {
		t.Run(map[bool]string{false: "unmanaged collision", true: "edited installation"}[modified], func(t *testing.T) {
			root, source := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("source"), 0600); err != nil {
				t.Fatal(err)
			}
			detection := DetectionResult{Runtime: RuntimeSummary{SkillDirectoryName: ".agents"}, Capabilities: CapabilitySet{CapabilitySkillDirectory: true}}
			workspace := Workspace{Path: root, Managed: true}
			skills := []Skill{{Name: "demo", Path: source}}
			target := filepath.Join(root, ".agents", "skills", "demo")
			if modified {
				if err := prepareTestSkills(detection, workspace, skills); err != nil {
					t.Fatal(err)
				}
			} else if err := os.MkdirAll(target, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("user content"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := prepareTestSkills(detection, workspace, skills); err == nil {
				t.Fatal("user content was claimed as managed")
			}
			data, _ := os.ReadFile(filepath.Join(target, "SKILL.md"))
			if string(data) != "user content" {
				t.Fatalf("user content changed: %q", data)
			}
		})
	}
}

func TestManagedSkillsSwitchRuntimeAndRejectLinkedRoots(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	detection := DetectionResult{Runtime: RuntimeSummary{SkillDirectoryName: ".claude"}, Capabilities: CapabilitySet{CapabilitySkillDirectory: true}}
	workspace := Workspace{Path: root, Managed: true}
	skills := []Skill{{Name: "demo", Path: source}}
	if err := prepareTestSkills(detection, workspace, skills); err != nil {
		t.Fatal(err)
	}
	detection.Runtime.SkillDirectoryName = ".agents"
	if err := prepareTestSkills(detection, workspace, skills); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "demo")); err != nil {
		t.Fatalf("independent target was removed: %v", err)
	}
	if runtime.GOOS != "windows" {
		claude, err := filepath.EvalSymlinks(filepath.Join(root, ".claude", "skills", "demo"))
		if err != nil {
			t.Fatal(err)
		}
		agents, err := filepath.EvalSymlinks(filepath.Join(root, ".agents", "skills", "demo"))
		if err != nil {
			t.Fatal(err)
		}
		canonicalRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(canonicalRoot, ".neuxbot", "skills", skills[0].Digest)
		if claude != want || agents != want {
			t.Fatalf("native targets must reuse workspace snapshot %q, got %q and %q", want, claude, agents)
		}
	}
	if err := os.RemoveAll(filepath.Join(root, ".claude")); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".claude")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	detection.Runtime.SkillDirectoryName = ".claude"
	if err := prepareTestSkills(detection, workspace, skills); err == nil {
		t.Fatal("linked native discovery directory accepted")
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "skills", "demo", "SKILL.md")); err != nil {
		t.Fatalf("failed prepare removed prior installation: %v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("prepare wrote outside workspace: %v %v", entries, err)
	}
}

func TestManagedSystemSkillProjectsReferencesWithoutExecutable(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	for name, value := range map[string]string{
		"SKILL.md":                       "system skill",
		"references/guide.md":            "system reference",
		"references/example.workflow.js": "system example",
		"scripts/botcli":                 "canonical executable bytes",
		"scripts/botcli.exe":             "canonical Windows executable bytes",
	} {
		path := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	detection := DetectionResult{Runtime: RuntimeSummary{SkillDirectoryName: ".agents"}}
	workspace := Workspace{Path: root, Managed: true}
	if err := prepareTestSkills(detection, workspace, []Skill{{Name: "bot", Path: source}}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, ".agents", "skills", "bot")
	for name, want := range map[string]string{"SKILL.md": "system skill", "references/guide.md": "system reference", "references/example.workflow.js": "system example"} {
		body, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(name)))
		if err != nil || string(body) != want {
			t.Fatalf("system document %s = %q, %v", name, body, err)
		}
	}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr == nil && (entry.Name() == "botcli" || entry.Name() == "botcli.exe") {
			t.Errorf("workspace contains a system executable at %s", path)
		}
		return walkErr
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "scripts")); !os.IsNotExist(err) {
		t.Fatalf("native system skill exposes scripts: %v", err)
	}
	if err := prepareTestSkills(detection, workspace, nil); err != nil {
		t.Fatalf("manifest did not recognize the docs-only installation: %v", err)
	}
}

func TestManagedSkillsMigrateLegacyCopiesInPreparedTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows may retain a copy when directory links are unavailable")
	}
	root, source := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("version one"), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := managedskills.Digest(source)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, ".agents", "skills", "demo")
	if err := managedskills.CopyDirectory(source, target); err != nil {
		t.Fatal(err)
	}
	manifest := `{"version":1,"targets":{".agents/skills":{"demo":"` + digest + `"}}}`
	if err := os.WriteFile(filepath.Join(root, managedskills.MaterializationManifestName), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	if err := prepareTestSkills(DetectionResult{Runtime: RuntimeSummary{SkillDirectoryName: ".agents"}}, Workspace{Path: root, Managed: true}, []Skill{{Name: "demo", Path: source}}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(target); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("verified legacy copy was not replaced with a link: %v %v", info, err)
	}
}

func TestManagedSkillSnapshotsRetainOtherRuntimeVersion(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	workspace := Workspace{Path: root, Managed: true}
	put := func(body, discovery string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		skills := []Skill{{Name: "demo", Path: source}}
		if err := prepareTestSkills(DetectionResult{Runtime: RuntimeSummary{SkillDirectoryName: discovery}}, workspace, skills); err != nil {
			t.Fatal(err)
		}
		return skills[0].Digest
	}
	first := put("version one", ".agents")
	put("version one", ".claude")
	snapshot := filepath.Join(root, skillSnapshotPath(first))
	copyTarget := filepath.Join(root, ".claude", "skills", "demo")
	info, err := os.Lstat(copyTarget)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the directory-copy projection used when native links are unavailable.
	if info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(copyTarget); err != nil {
			t.Fatal(err)
		}
		if err := managedskills.CopyDirectory(snapshot, copyTarget); err != nil {
			t.Fatal(err)
		}
	}
	put("version two", ".agents")
	skillTestBytes(t, root, ".agents", "version two")
	skillTestBytes(t, root, ".claude", "version one")
	if _, err := os.Stat(snapshot); err != nil {
		t.Fatalf("snapshot still referenced by a copy was removed: %v", err)
	}
	if err := prepareTestSkills(DetectionResult{Runtime: RuntimeSummary{SkillDirectoryName: ".claude"}}, workspace, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(snapshot); !os.IsNotExist(err) {
		t.Fatalf("unreferenced snapshot still present: %v", err)
	}
	skillTestBytes(t, root, ".agents", "version two")
}

func TestManagedSkillsFailedSourcePreservesPreviousVersion(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("version one"), 0600); err != nil {
		t.Fatal(err)
	}
	detection := DetectionResult{Runtime: RuntimeSummary{SkillDirectoryName: ".agents"}, Capabilities: CapabilitySet{CapabilitySkillDirectory: true}}
	workspace := Workspace{Path: root, Managed: true}
	skills := []Skill{{Name: "demo", Path: source}}
	if err := prepareTestSkills(detection, workspace, skills); err != nil {
		t.Fatal(err)
	}
	// A broken replacement source cannot remove the last committed version.
	if err := os.Remove(filepath.Join(source, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := prepareTestSkills(detection, workspace, skills); err == nil {
		t.Fatal("invalid replacement accepted")
	}
	data, _ := os.ReadFile(filepath.Join(root, ".agents", "skills", "demo", "SKILL.md"))
	if string(data) != "version one" {
		t.Fatalf("previous version lost: %q", data)
	}
}

func prepareTestSkills(detection DetectionResult, workspace Workspace, skills []Skill) error {
	for index := range skills {
		digest, err := managedskills.Digest(skills[index].Path)
		if err != nil {
			return err
		}
		skills[index].Digest = digest
	}
	selection, err := FreezeSkills(detection, workspace, skills, nil)
	if err != nil {
		return err
	}
	return prepareSkills(selection, nil)
}

func TestManagedSkillsPreserveUnownedLinks(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "demo")
	if err := os.Symlink(source, target); err != nil {
		t.Skip(err)
	}
	detection := DetectionResult{Runtime: RuntimeSummary{SkillDirectoryName: ".agents"}, Capabilities: CapabilitySet{CapabilitySkillDirectory: true}}
	if err := prepareTestSkills(detection, Workspace{Path: root, Managed: true}, []Skill{{Name: "demo", Path: source}}); err == nil {
		t.Fatal("unowned source link was adopted")
	}
	if actual, err := os.Readlink(target); err != nil || actual != source {
		t.Fatalf("unowned link changed: %q %v", actual, err)
	}
}

func TestManagedSkillSnapshotRejectsEscapingResourceLink(t *testing.T) {
	root, source, outside := t.TempDir(), t.TempDir(), t.TempDir()
	for _, directory := range []string{source, outside} {
		if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("notes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(source, "reference.md")
	if err := os.Symlink(filepath.Join(outside, "SKILL.md"), link); err != nil {
		t.Skip(err)
	}
	detection := DetectionResult{Runtime: RuntimeSummary{SkillDirectoryName: ".agents"}}
	workspace := Workspace{Path: root, Managed: true}
	skills := []Skill{{Name: "demo", Path: source}}
	if err := prepareTestSkills(detection, workspace, skills); !errors.Is(err, status_error.SkillContentInvalid) {
		t.Fatalf("resource link outside the snapshot accepted: %v", err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("SKILL.md", link); err != nil {
		t.Fatal(err)
	}
	if err := prepareTestSkills(detection, workspace, skills); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, ".agents", "skills", "demo", "reference.md"))
	if err != nil || string(body) != "notes" {
		t.Fatalf("internal resource link no longer resolves: %q, %v", body, err)
	}
}

// skillTestClient leaves model and protocol operations outside these resource tests.
type skillTestClient struct {
	// AgentClient supplies operations these lifecycle tests never submit.
	AgentClient
	// closeFailures injects deterministic native exit failures.
	closeFailures atomic.Int32
	// closes records native close attempts.
	closes atomic.Int32
}

func (*skillTestClient) Initialize(context.Context) error { return nil }
func (c *skillTestClient) Close(context.Context) error {
	c.closes.Add(1)
	if c.closeFailures.Add(-1) >= 0 {
		return status_error.RuntimeShutdownFailed
	}
	return nil
}
func (*skillTestClient) Capabilities() AgentCapabilities { return AgentCapabilities{} }
func (*skillTestClient) NewSession(context.Context, AgentSessionOptions) (AgentSession, error) {
	return AgentSession{ID: "fixture-native"}, nil
}

// skillTestStore models an empty conversation without replacing resource ownership.
type skillTestStore struct {
	// ConversationStore supplies storage operations unused before prompt submission.
	ConversationStore
}

func (skillTestStore) ReadBinding(context.Context, string) (*NativeBinding, error) { return nil, nil }
func (skillTestStore) ReadVersion(context.Context, string) (int64, error)          { return 0, nil }

func skillTestSelection(t *testing.T, cwd, directory, content string) *SkillSelection {
	t.Helper()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := managedskills.Digest(source)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := FreezeSkills(DetectionResult{Runtime: RuntimeSummary{SkillDirectoryName: directory}, Capabilities: CapabilitySet{CapabilitySkillDirectory: true}}, Workspace{Path: cwd, Managed: true}, []Skill{{Name: "demo", Path: source, Digest: digest}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return selection
}
func skillTestRuntime(t *testing.T, client func() *skillTestClient) *ProviderRuntime {
	t.Helper()
	runtime := NewProviderRuntime(ProviderRuntimeOptions{
		AcquireAccount: func(context.Context, AccountRequest) (AccountLease, error) { return AccountLease{}, nil },
		NewClient: func(context.Context, AccountRequest, ProviderLaunch, AccountLease, LaunchAuthLease, ResourceLease, AgentClientCallbacks) (ProviderClient, error) {
			return client(), nil
		},
	})
	t.Cleanup(func() {
		if err := runtime.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return runtime
}
func skillTestRequest(cwd, provider string, selection *SkillSelection) ProviderConnectionRequest {
	return ProviderConnectionRequest{Account: AccountRequest{Provider: provider, AccountID: "test-account", Subject: "test-subject", CWD: cwd}, Resources: ResourceSelection{Skills: selection}}
}
func skillTestPorts() ProviderConnectionPorts {
	return ProviderConnectionPorts{AcquireResources: func(_ context.Context, s ResourceSelection) (ResourceLease, error) {
		return ResourceLease{Skills: s.Skills}, nil
	}}
}
func skillTestBytes(t *testing.T, cwd, directory, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cwd, directory, "skills", "demo", "SKILL.md"))
	if err != nil || string(data) != want {
		t.Fatalf("installed skill = %q, %v; want %q", data, err, want)
	}
}
func TestSharedSkillsActiveCrossRuntimeConflictAndSharing(t *testing.T) {
	cwd := t.TempDir()
	runtime := skillTestRuntime(t, func() *skillTestClient { return &skillTestClient{} })
	first := skillTestSelection(t, cwd, ".agents", "first")
	a, err := runtime.Open(t.Context(), skillTestRequest(cwd, "codex", first), skillTestPorts())
	if err != nil {
		t.Fatal(err)
	}
	second := skillTestSelection(t, cwd, ".agents", "second")
	if c, err := runtime.Open(t.Context(), skillTestRequest(cwd, "kimi", second), skillTestPorts()); c != nil || !errors.Is(err, status_error.RuntimeUnavailable) {
		t.Fatalf("incompatible active target admitted: %v %v", c != nil, err)
	}
	shared := skillTestSelection(t, cwd, ".agents", "first")
	b, err := runtime.Open(t.Context(), skillTestRequest(cwd, "kimi", shared), skillTestPorts())
	if err != nil {
		t.Fatal(err)
	}
	skillTestBytes(t, cwd, ".agents", "first")
	if err := a.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if c, err := runtime.Open(t.Context(), skillTestRequest(cwd, "codex", second), skillTestPorts()); c != nil || !errors.Is(err, status_error.RuntimeUnavailable) {
		t.Fatalf("remaining shared owner was bypassed: %v %v", c != nil, err)
	}
	if err := b.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Open(t.Context(), skillTestRequest(cwd, "codex", second), skillTestPorts()); err != nil {
		t.Fatal(err)
	}
	skillTestBytes(t, cwd, ".agents", "second")
}
func TestSharedSkillsPreparingConsumerFencesOtherStartup(t *testing.T) {
	cwd := t.TempDir()
	runtime := skillTestRuntime(t, func() *skillTestClient { return &skillTestClient{} })
	first, second := skillTestSelection(t, cwd, ".agents", "first"), skillTestSelection(t, cwd, ".agents", "second")
	entered, proceed, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := runtime.Open(t.Context(), skillTestRequest(cwd, "codex", first), ProviderConnectionPorts{AcquireResources: func(_ context.Context, s ResourceSelection) (ResourceLease, error) {
			close(entered)
			<-proceed
			return ResourceLease{Skills: s.Skills}, nil
		}})
		done <- err
	}()
	<-entered
	c, err := runtime.Open(t.Context(), skillTestRequest(cwd, "kimi", second), skillTestPorts())
	close(proceed)
	if openErr := <-done; openErr != nil {
		t.Fatal(openErr)
	}
	if c != nil || !errors.Is(err, status_error.RuntimeUnavailable) {
		t.Fatalf("preparing reservation bypassed: %v %v", c != nil, err)
	}
	skillTestBytes(t, cwd, ".agents", "first")
}
func TestSharedSkillsCloseFailureRetainsTargetUntilRetry(t *testing.T) {
	cwd := t.TempDir()
	client := &skillTestClient{}
	client.closeFailures.Store(1)
	runtime := skillTestRuntime(t, func() *skillTestClient { return client })
	first, second := skillTestSelection(t, cwd, ".agents", "first"), skillTestSelection(t, cwd, ".agents", "second")
	c, err := runtime.Open(t.Context(), skillTestRequest(cwd, "codex", first), skillTestPorts())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(t.Context()); !errors.Is(err, status_error.RuntimeShutdownFailed) {
		t.Fatalf("Close = %v", err)
	}
	if next, err := runtime.Open(t.Context(), skillTestRequest(cwd, "kimi", second), skillTestPorts()); next != nil || !errors.Is(err, status_error.RuntimeUnavailable) {
		t.Fatalf("failed exit lost ownership: %v %v", next != nil, err)
	}
	skillTestBytes(t, cwd, ".agents", "first")
	if err := c.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Open(t.Context(), skillTestRequest(cwd, "kimi", second), skillTestPorts()); err != nil {
		t.Fatal(err)
	}
	skillTestBytes(t, cwd, ".agents", "second")
}
func TestSharedSkillsPartialAcquisitionReleasesReservation(t *testing.T) {
	cwd := t.TempDir()
	runtime := skillTestRuntime(t, func() *skillTestClient { return &skillTestClient{} })
	first, second := skillTestSelection(t, cwd, ".agents", "first"), skillTestSelection(t, cwd, ".agents", "second")
	var released atomic.Bool
	c, err := runtime.Open(t.Context(), skillTestRequest(cwd, "codex", first), ProviderConnectionPorts{AcquireResources: func(context.Context, ResourceSelection) (ResourceLease, error) {
		return ResourceLease{Close: func(context.Context) error { released.Store(true); return nil }}, status_error.PluginNotReady
	}})
	if c == nil || !errors.Is(err, status_error.PluginNotReady) {
		t.Fatalf("partial acquisition = %v %v", c != nil, err)
	}
	if err := c.WaitOpenCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !released.Load() {
		t.Fatal("partial resource lease was leaked")
	}
	if _, err := runtime.Open(t.Context(), skillTestRequest(cwd, "kimi", second), skillTestPorts()); err != nil {
		t.Fatal(err)
	}
	skillTestBytes(t, cwd, ".agents", "second")
}
func TestSharedSkillsConcurrentIndependentTargetsPreserveManifest(t *testing.T) {
	cwd := t.TempDir()
	runtime := skillTestRuntime(t, func() *skillTestClient { return &skillTestClient{} })
	var wg sync.WaitGroup
	for _, directory := range []string{".agents", ".claude"} {
		selection := skillTestSelection(t, cwd, directory, directory)
		wg.Go(func() {
			if _, err := runtime.Open(t.Context(), skillTestRequest(cwd, "codex", selection), skillTestPorts()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	skillTestBytes(t, cwd, ".agents", ".agents")
	skillTestBytes(t, cwd, ".claude", ".claude")
	data, err := os.ReadFile(filepath.Join(cwd, managedskills.MaterializationManifestName))
	if err != nil {
		t.Fatal(err)
	}
	var manifest managedskills.MaterializationManifest
	if err := json.Unmarshal(data, &manifest); err != nil || len(manifest.Targets) != 2 {
		t.Fatalf("independent manifest lost: %s %v", data, err)
	}
}
func TestSharedSkillsThreadAdmissionAndIdleRetirement(t *testing.T) {
	cwd := t.TempDir()
	var clientsMu sync.Mutex
	clients := []*skillTestClient{}
	runtime := skillTestRuntime(t, func() *skillTestClient {
		c := &skillTestClient{}
		clientsMu.Lock()
		clients = append(clients, c)
		clientsMu.Unlock()
		return c
	})
	lifecycle := NewThreadLifecycle(runtime, skillTestStore{})
	t.Cleanup(func() { _ = lifecycle.Close(context.Background()) })
	prepare := func(chat, provider, body string) (*Turn, error) {
		selection := skillTestSelection(t, cwd, ".agents", body)
		return lifecycle.Prepare(t.Context(), ThreadRequest{ChatID: chat, Connection: skillTestRequest(cwd, provider, selection)}, TurnRequest{Text: "input", HostInvocationID: chat, ExecutionID: chat, AssistantMessageID: chat}, TurnPorts{Admit: func(context.Context, TurnAdmission) (ConversationBatch, error) { return ConversationBatch{}, nil }, AcquireResources: skillTestPorts().AcquireResources})
	}
	first, err := prepare("first", "codex", "one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepare("blocked", "kimi", "two"); !errors.Is(err, ErrTurnAdmissionBusy) || !errors.Is(err, status_error.RuntimeUnavailable) {
		t.Fatalf("cross-chat active conflict did not preserve a retryable admission error: %v", err)
	}
	// Settlement without process release models a successfully completed public turn.
	first.mu.Lock()
	first.phase = turnFinishing
	first.mu.Unlock()
	if err := first.settle(false); err != nil {
		t.Fatal(err)
	}
	same, err := prepare("same", "kimi", "one")
	if err != nil {
		t.Fatal(err)
	}
	if clients[0].closes.Load() != 0 {
		t.Fatal("same content evicted idle owner")
	}
	same.mu.Lock()
	same.phase = turnFinishing
	same.mu.Unlock()
	if err := same.settle(false); err != nil {
		t.Fatal(err)
	}
	replacement, err := prepare("replacement", "codex", "two")
	if err != nil {
		t.Fatal(err)
	}
	if clients[0].closes.Load() != 1 || clients[1].closes.Load() != 1 {
		t.Fatal("idle incompatible consumers were not retired")
	}
	skillTestBytes(t, cwd, ".agents", "two")
	if err := replacement.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestSharedSkillsContentChangesSessionCompatibility(t *testing.T) {
	cwd := t.TempDir()
	first := ThreadRequest{Connection: skillTestRequest(cwd, "codex", skillTestSelection(t, cwd, ".agents", "one"))}
	second := ThreadRequest{Connection: skillTestRequest(cwd, "codex", skillTestSelection(t, cwd, ".agents", "two"))}
	a, err := freezeThreadSettings(&first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := freezeThreadSettings(&second)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) == string(b) {
		t.Fatal("changed skill content reused the old native session configuration")
	}
}

func TestSharedSkillsFrozenSourceMutationPreservesInstalledContent(t *testing.T) {
	cwd := t.TempDir()
	runtime := skillTestRuntime(t, func() *skillTestClient { return &skillTestClient{} })
	first := skillTestSelection(t, cwd, ".agents", "first")
	c, err := runtime.Open(t.Context(), skillTestRequest(cwd, "codex", first), skillTestPorts())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	frozen := skillTestSelection(t, cwd, ".agents", "frozen")
	if err := os.WriteFile(filepath.Join(frozen.entries[0].path, "SKILL.md"), []byte("changed after freeze"), 0600); err != nil {
		t.Fatal(err)
	}
	failed, err := runtime.Open(t.Context(), skillTestRequest(cwd, "codex", frozen), skillTestPorts())
	if failed == nil || !errors.Is(err, status_error.SkillContentInvalid) {
		t.Fatalf("changed source accepted: %v %v", failed != nil, err)
	}
	if err := failed.WaitOpenCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	skillTestBytes(t, cwd, ".agents", "first")
}

func TestSharedSkillsIdleCloseFailureRetainsFilesAndRetries(t *testing.T) {
	cwd := t.TempDir()
	firstClient := &skillTestClient{}
	firstClient.closeFailures.Store(1)
	var builds atomic.Int32
	runtime := skillTestRuntime(t, func() *skillTestClient {
		if builds.Add(1) == 1 {
			return firstClient
		}
		return &skillTestClient{}
	})
	lifecycle := NewThreadLifecycle(runtime, skillTestStore{})
	t.Cleanup(func() { _ = lifecycle.Close(context.Background()) })
	prepare := func(chat, body string) (*Turn, error) {
		return lifecycle.Prepare(t.Context(), ThreadRequest{ChatID: chat, Connection: skillTestRequest(cwd, "codex", skillTestSelection(t, cwd, ".agents", body))}, TurnRequest{Text: "input", HostInvocationID: chat, ExecutionID: chat, AssistantMessageID: chat}, TurnPorts{Admit: func(context.Context, TurnAdmission) (ConversationBatch, error) { return ConversationBatch{}, nil }, AcquireResources: skillTestPorts().AcquireResources})
	}
	first, err := prepare("first", "one")
	if err != nil {
		t.Fatal(err)
	}
	first.mu.Lock()
	first.phase = turnFinishing
	first.mu.Unlock()
	if err := first.settle(false); err != nil {
		t.Fatal(err)
	}
	if _, err := prepare("replace", "two"); !errors.Is(err, status_error.RuntimeShutdownFailed) {
		t.Fatalf("idle close failure = %v", err)
	}
	skillTestBytes(t, cwd, ".agents", "one")
	next, err := prepare("retry", "two")
	if err != nil {
		t.Fatal(err)
	}
	if firstClient.closes.Load() != 2 {
		t.Fatal("retained idle process was not retried")
	}
	skillTestBytes(t, cwd, ".agents", "two")
	if err := next.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestSharedSkillsPluginVersionsParticipateInTargetReservation(t *testing.T) {
	cwd := t.TempDir()
	runtime := skillTestRuntime(t, func() *skillTestClient { return &skillTestClient{} })
	template := skillTestSelection(t, cwd, ".agents", "same bytes")
	freeze := func(version string) ProviderConnectionRequest {
		versions := map[string]string{"plugin": version}
		selection, err := FreezeSkills(DetectionResult{Runtime: RuntimeSummary{SkillDirectoryName: ".agents"}, Capabilities: CapabilitySet{CapabilitySkillDirectory: true}}, Workspace{Path: cwd, Managed: true}, []Skill{{Name: "demo", Path: template.entries[0].path, Digest: template.entries[0].digest}}, versions)
		if err != nil {
			t.Fatal(err)
		}
		request := skillTestRequest(cwd, "codex", selection)
		request.Resources.Plugins = versions
		return request
	}
	if _, err := runtime.Open(t.Context(), freeze("fixed-v1"), skillTestPorts()); err != nil {
		t.Fatal(err)
	}
	if next, err := runtime.Open(t.Context(), freeze("fixed-v2"), skillTestPorts()); next != nil || !errors.Is(err, status_error.RuntimeUnavailable) {
		t.Fatalf("different fixed version shared active target: %v %v", next != nil, err)
	}
	skillTestBytes(t, cwd, ".agents", "same bytes")
}

func TestOptionalSkillProjectionSkipsMissingSources(t *testing.T) {
	cwd, present := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(present, "SKILL.md"), []byte("available skill"), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := managedskills.Digest(present)
	if err != nil {
		t.Fatal(err)
	}
	target, err := FreezeOptionalSkills(
		DetectionResult{Runtime: RuntimeSummary{SkillDirectoryName: ".agents"}},
		Workspace{Path: cwd, Managed: true},
		[]Skill{
			{Name: "bad-reference", Path: filepath.Join(cwd, "gone"), Digest: digest},
			{Name: "valid-learned", Path: present, Digest: digest},
		}, nil, "test-partner", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareSkills(target, nil); err != nil {
		t.Fatalf("optional skill made the chat unable to prepare: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".agents", "skills", "valid-learned", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(cwd, ".agents", "skills", "bad-reference")); !os.IsNotExist(err) {
		t.Fatalf("missing source incorrectly published: %v", err)
	}
}

func TestOptionalSkillProjectionRecoversEditedSnapshot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a native discovery symlink")
	}
	for _, sourceAvailable := range []bool{true, false} {
		name := "missing source"
		if sourceAvailable {
			name = "published source"
		}
		t.Run(name, func(t *testing.T) {
			cwd := t.TempDir()
			selection := skillTestSelection(t, cwd, ".agents", "published content")
			selection.optional = true
			if err := prepareSkills(selection, nil); err != nil {
				t.Fatal(err)
			}
			projected := filepath.Join(cwd, ".agents", "skills", "demo", "SKILL.md")
			if err := os.WriteFile(projected, []byte("edited content"), 0600); err != nil {
				t.Fatal(err)
			}
			if !sourceAvailable {
				if err := os.Remove(filepath.Join(selection.entries[0].path, "SKILL.md")); err != nil {
					t.Fatal(err)
				}
			}
			if err := prepareSkills(selection, nil); err != nil {
				t.Fatalf("edited optional skill blocked the next chat: %v", err)
			}
			if sourceAvailable {
				skillTestBytes(t, cwd, ".agents", "published content")
			} else if _, err := os.Lstat(filepath.Dir(projected)); !os.IsNotExist(err) {
				t.Fatalf("unavailable edited projection is still discoverable: %v", err)
			}
			backups, err := filepath.Glob(filepath.Join(cwd, ".neuxbot", "skills", ".edited-*", selection.entries[0].digest, "SKILL.md"))
			if err != nil || len(backups) != 1 {
				t.Fatalf("expected one backup of the assistant's edits, got %v: %v", backups, err)
			}
			data, err := os.ReadFile(backups[0])
			if err != nil || string(data) != "edited content" {
				t.Fatalf("assistant's edits were lost: %q %v", data, err)
			}
		})
	}
}

func TestOptionalSkillChangeDoesNotFenceSamePartnerChat(t *testing.T) {
	cwd := t.TempDir()
	pool := skillTestRuntime(t, func() *skillTestClient { return &skillTestClient{} })
	lifecycle := NewThreadLifecycle(pool, skillTestStore{})
	t.Cleanup(func() { _ = lifecycle.Close(context.Background()) })
	prepare := func(id, owner, body string) (*Turn, error) {
		skills := skillTestSelection(t, cwd, ".agents", body)
		skills.owner, skills.optional = owner, true
		return lifecycle.Prepare(t.Context(), ThreadRequest{ChatID: id, Connection: skillTestRequest(cwd, "codex", skills)},
			TurnRequest{Text: "user input", HostInvocationID: id, ExecutionID: id, AssistantMessageID: id},
			TurnPorts{Admit: func(context.Context, TurnAdmission) (ConversationBatch, error) { return ConversationBatch{}, nil }, AcquireResources: skillTestPorts().AcquireResources})
	}
	first, err := prepare("first", "same-partner", "known skill")
	if err != nil {
		t.Fatal(err)
	}
	second, err := prepare("second", "same-partner", "recently learned change")
	if err != nil {
		t.Fatalf("skill change blocked another chat: %v", err)
	}
	skillTestBytes(t, cwd, ".agents", "known skill")
	if _, err := prepare("cross-partner", "somebody-else", "different skill"); !errors.Is(err, ErrTurnAdmissionBusy) {
		t.Fatalf("shared projection relaxed the partner boundary: %v", err)
	}
	for _, turn := range []*Turn{second, first} {
		if err := turn.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	third, err := prepare("third", "same-partner", "recently learned change")
	if err != nil {
		t.Fatal(err)
	}
	skillTestBytes(t, cwd, ".agents", "recently learned change")
	if err := third.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestCorruptOptionalSkillProjectionDoesNotFenceChat(t *testing.T) {
	cwd := t.TempDir()
	pool := skillTestRuntime(t, func() *skillTestClient { return &skillTestClient{} })
	lifecycle := NewThreadLifecycle(pool, skillTestStore{})
	t.Cleanup(func() { _ = lifecycle.Close(context.Background()) })
	selection := skillTestSelection(t, cwd, ".agents", "published content")
	selection.optional, selection.owner = true, "partner"
	if err := prepareSkills(selection, nil); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(cwd, ".agents", "skills", "demo")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("accidentally replaced skill link"), 0600); err != nil {
		t.Fatal(err)
	}
	turn, err := lifecycle.Prepare(t.Context(), ThreadRequest{ChatID: "chat", Connection: skillTestRequest(cwd, "codex", selection)},
		TurnRequest{Text: "ordinary text", HostInvocationID: "chat", ExecutionID: "chat", AssistantMessageID: "chat"},
		TurnPorts{Admit: func(context.Context, TurnAdmission) (ConversationBatch, error) { return ConversationBatch{}, nil }, AcquireResources: skillTestPorts().AcquireResources})
	if err != nil {
		t.Fatalf("optional skill prevented ordinary cold chat admission: %v", err)
	}
	_ = turn.Close(t.Context())
}

func TestOptionalSkillProjectionPreservesCorruptManifestEvidence(t *testing.T) {
	cwd := t.TempDir()
	selection := skillTestSelection(t, cwd, ".agents", "published content")
	selection.optional = true
	if err := prepareSkills(selection, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, managedskills.MaterializationManifestName), []byte("broken manifest"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := prepareSkills(selection, nil); err != nil {
		t.Fatalf("invalid optional manifest prevented publishing verified skills: %v", err)
	}
	skillTestBytes(t, cwd, ".agents", "published content")
	backups, err := filepath.Glob(filepath.Join(cwd, ".neuxbot", "skills", ".edited-*", managedskills.MaterializationManifestName))
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected archived manifest, got %v: %v", backups, err)
	}
	if body, err := os.ReadFile(backups[0]); err != nil || string(body) != "broken manifest" {
		t.Fatalf("archived manifest = %q, %v", body, err)
	}
}

func TestSkillRepairKeepsOtherActiveDiscoveryTargetReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires native discovery symlinks")
	}
	cwd := t.TempDir()
	pool := skillTestRuntime(t, func() *skillTestClient { return &skillTestClient{} })
	first := skillTestSelection(t, cwd, ".agents", "common version")
	first.optional, first.owner = true, "same-partner"
	second := skillTestSelection(t, cwd, ".claude", "common version")
	second.optional, second.owner = true, "same-partner"
	if _, err := pool.Open(t.Context(), skillTestRequest(cwd, "codex", first), skillTestPorts()); err != nil {
		t.Fatal(err)
	}
	replaced, err := pool.Open(t.Context(), skillTestRequest(cwd, "claude", second), skillTestPorts())
	if err != nil {
		t.Fatal(err)
	}
	if err := replaced.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	firstTarget := filepath.Join(cwd, ".agents", "skills", "demo", "SKILL.md")
	if err := os.WriteFile(firstTarget, []byte("agent drafted changes"), 0600); err != nil {
		t.Fatal(err)
	}
	next, err := FreezeOptionalSkills(DetectionResult{Runtime: RuntimeSummary{SkillDirectoryName: ".claude"}},
		Workspace{Path: cwd, Managed: true}, nil, nil, "same-partner", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Open(t.Context(), skillTestRequest(cwd, "claude", next), skillTestPorts()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(firstTarget); err != nil {
		t.Fatalf("another native discovery target lost the snapshot leased to its active connection: %v", err)
	}
}
