package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestAgentWorkspaceCopyKeepsBusinessFilesAndRegeneratesOwnership(t *testing.T) {
	agents, source, target := workspaceCopyFixture(t)
	writeCopyFixtureFile(t, filepath.Join(source, "AGENTS.md"), "profile")
	writeCopyFixtureFile(t, filepath.Join(source, "projects", "notes", "draft.txt"), "personal project")
	writeCopyFixtureFile(t, filepath.Join(source, ".neuxbot", "private.json"), "generated")
	projection := filepath.Join(source, "projects", "chat-one")
	writeCopyFixtureFile(t, filepath.Join(projection, constants.ChatContextOwnerFileName), `{"kind":"`+constants.ChatProjectEntriesOwnerKind+`","chatId":"chat-one"}`)
	writeCopyFixtureFile(t, filepath.Join(projection, "excluded.txt"), "generated project")
	writeCopyFixtureFile(t, filepath.Join(filepath.Dir(agents), "Conversations", "chat-one", "log.jsonl"), "chat history")

	if err := PrepareAgentWorkspaceCopy(context.Background(), source, target, "source", "target"); err != nil {
		t.Fatal(err)
	}
	if err := SettleAgentWorkspaceCopy(target, "target", true); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAgentWorkspaceDeletion(target, "target"); err != nil {
		t.Fatal(err)
	}
	assertCopyFile(t, filepath.Join(target, "AGENTS.md"), "profile")
	assertCopyFile(t, filepath.Join(target, "projects", "notes", "draft.txt"), "personal project")
	for _, path := range []string{".neuxbot", filepath.Join("projects", "chat-one"), "Conversations"} {
		assertCopyPathMissing(t, filepath.Join(target, path))
	}
	writeCopyFixtureFile(t, filepath.Join(target, "projects", "notes", "draft.txt"), "copy edit")
	assertCopyFile(t, filepath.Join(source, "projects", "notes", "draft.txt"), "personal project")
}

func TestAgentWorkspaceCopyExcludesGitMetadataAtAnyDepth(t *testing.T) {
	_, source, target := workspaceCopyFixture(t)
	rootMetadata := filepath.Join(".git", "HEAD")
	worktreeMetadata := filepath.Join("projects", "linked", ".git")
	workspaceFile := filepath.Join("projects", "linked", "notes.md")
	writeCopyFixtureFile(t, filepath.Join(source, rootMetadata), "ref: refs/heads/main\n")
	writeCopyFixtureFile(t, filepath.Join(source, worktreeMetadata), "gitdir: ../../.git/worktrees/linked\n")
	writeCopyFixtureFile(t, filepath.Join(source, workspaceFile), "worktree content")
	writeCopyFixtureFile(t, filepath.Join(source, ".gitignore"), "local.log\n")

	if err := PrepareAgentWorkspaceCopy(t.Context(), source, target, "source", "target"); err != nil {
		t.Fatal(err)
	}
	assertCopyPathMissing(t, filepath.Join(target, ".git"))
	assertCopyPathMissing(t, filepath.Join(target, worktreeMetadata))
	assertCopyFile(t, filepath.Join(target, workspaceFile), "worktree content")
	assertCopyFile(t, filepath.Join(target, ".gitignore"), "local.log\n")
	assertCopyFile(t, filepath.Join(source, rootMetadata), "ref: refs/heads/main\n")
	assertCopyFile(t, filepath.Join(source, worktreeMetadata), "gitdir: ../../.git/worktrees/linked\n")
}

func TestAgentWorkspaceCopyKeepsOnlyUnmanagedNativeSkillDirectories(t *testing.T) {
	_, source, target := workspaceCopyFixture(t)
	const userSkill = ".claude/skills/user-skill/SKILL.md"
	const userReference = ".claude/skills/user-skill/references/note.md"
	const secondUserSkill = ".agents/skills/second-user-skill/SKILL.md"
	for path, body := range map[string]string{
		userSkill:                            "user skill",
		userReference:                        "user resource",
		secondUserSkill:                      "another user skill",
		".claude/skills/managed/SKILL.md":    "physical materialization",
		".agents/skills/bot/SKILL.md":        "reserved system skill",
		".claude/settings.json":              "runtime configuration",
		".claude/skills/loose.txt":           "outside any skill directory",
		".neuxbot/skills/generated/SKILL.md": "projection root",
		".neuxbot-skills.json":               `{"version":1,"targets":{".claude/skills":{"managed":"` + strings.Repeat("a", 64) + `"}}}`,
	} {
		writeCopyFixtureFile(t, filepath.Join(source, filepath.FromSlash(path)), body)
	}
	if err := PrepareAgentWorkspaceCopy(context.Background(), source, target, "source", "target"); err != nil {
		t.Fatal(err)
	}
	assertCopyFile(t, filepath.Join(target, filepath.FromSlash(userSkill)), "user skill")
	assertCopyFile(t, filepath.Join(target, filepath.FromSlash(userReference)), "user resource")
	assertCopyFile(t, filepath.Join(target, filepath.FromSlash(secondUserSkill)), "another user skill")
	for _, name := range []string{".claude/skills/managed", ".agents/skills/bot", ".claude/settings.json", ".claude/skills/loose.txt", ".neuxbot", ".neuxbot-skills.json"} {
		assertCopyPathMissing(t, filepath.Join(target, filepath.FromSlash(name)))
	}
	writeCopyFixtureFile(t, filepath.Join(target, filepath.FromSlash(userSkill)), "copy edit")
	assertCopyFile(t, filepath.Join(source, filepath.FromSlash(userSkill)), "user skill")
}

func TestAgentWorkspaceCopyKeepsLocalSkillsWithNoProjectionState(t *testing.T) {
	_, source, target := workspaceCopyFixture(t)
	path := filepath.Join(".qoder", "skills", "local", "SKILL.md")
	writeCopyFixtureFile(t, filepath.Join(source, path), "local skill")
	writeCopyFixtureFile(t, filepath.Join(source, ".neuxbot", "plugin-views", "internal"), "plugin projection")
	if err := PrepareAgentWorkspaceCopy(context.Background(), source, target, "source", "target"); err != nil {
		t.Fatal(err)
	}
	assertCopyFile(t, filepath.Join(target, path), "local skill")
	assertCopyPathMissing(t, filepath.Join(target, ".neuxbot"))
}

func TestAgentWorkspaceCopySkipsNativeSkillsWhenOwnershipIsAmbiguous(t *testing.T) {
	for _, path := range []string{".neuxbot-skills.json", ".neuxbot/skills/orphan/SKILL.md", ".neuxbot-skills-stage-0001/manifest"} {
		t.Run(path, func(t *testing.T) {
			_, source, target := workspaceCopyFixture(t)
			writeCopyFixtureFile(t, filepath.Join(source, filepath.FromSlash(path)), "unreconciled materialization state")
			writeCopyFixtureFile(t, filepath.Join(source, ".claude", "skills", "unknown", "SKILL.md"), "ownership unknown")
			writeCopyFixtureFile(t, filepath.Join(source, "business.txt"), "user file")
			if err := PrepareAgentWorkspaceCopy(context.Background(), source, target, "source", "target"); err != nil {
				t.Fatal(err)
			}
			assertCopyPathMissing(t, filepath.Join(target, ".claude"))
			assertCopyPathMissing(t, filepath.Join(target, filepath.FromSlash(path)))
			assertCopyFile(t, filepath.Join(target, "business.txt"), "user file")
		})
	}
}

func TestAgentWorkspaceCopyRebasesInternalLinksWithoutCopyingExternalReferences(t *testing.T) {
	agents, source, target := workspaceCopyFixture(t)
	note := filepath.Join(source, "notes", "report.md")
	writeCopyFixtureFile(t, note, "original")
	writeCopyFixtureFile(t, filepath.Join(source, ".neuxbot", "hidden"), "internal")
	external := filepath.Join(filepath.Dir(agents), "private.txt")
	writeCopyFixtureFile(t, external, "external")
	links := map[string]string{
		"absolute": note,
		"chain":    "absolute",
		"relative": filepath.Join("notes", "report.md"),
		"private":  filepath.Join(".neuxbot", "hidden"),
		"external": external,
		"escape":   filepath.Join("..", "..", "private.txt"),
		"missing":  "missing-target",
		"owner":    constants.ChatContextOwnerFileName,
	}
	for name, location := range links {
		makeCopyFixtureLink(t, location, filepath.Join(source, name))
	}

	if err := PrepareAgentWorkspaceCopy(context.Background(), source, target, "source", "target"); err != nil {
		t.Fatal(err)
	}
	writeCopyFixtureFile(t, filepath.Join(target, "notes", "report.md"), "copy edit")
	for _, name := range []string{"absolute", "chain", "relative"} {
		path := filepath.Join(target, name)
		if location, err := os.Readlink(path); err != nil || filepath.IsAbs(location) {
			t.Fatalf("%s should be a rebased link: %q, %v", name, location, err)
		}
		assertCopyFile(t, path, "copy edit")
	}
	for _, name := range []string{"private", "external", "escape", "missing", "owner"} {
		assertCopyPathMissing(t, filepath.Join(target, name))
	}
	assertCopyFile(t, note, "original")
	assertCopyFile(t, external, "external")
}

func TestAgentWorkspaceCopyCreatesAnEmptyOwnedWorkspaceForUnusedPartner(t *testing.T) {
	agents := filepath.Join(t.TempDir(), "Agents")
	target := filepath.Join(agents, "target")
	if err := PrepareAgentWorkspaceCopy(context.Background(), filepath.Join(agents, "source"), target, "source", "target"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 1 || entries[0].Name() != constants.ChatContextOwnerFileName {
		t.Fatalf("empty owned workspace entries = %v, %v", entries, err)
	}
	if err := ValidateAgentWorkspaceDeletion(target, "target"); err != nil {
		t.Fatal(err)
	}
}

func TestAgentWorkspaceCopyRejectsForeignSourceAndExistingTarget(t *testing.T) {
	_, source, target := workspaceCopyFixture(t)
	writeCopyFixtureFile(t, filepath.Join(source, constants.ChatContextOwnerFileName), `{"kind":"`+constants.AgentExecutionWorkspaceOwnerKind+`","wakerId":"somebody-else"}`)
	if err := PrepareAgentWorkspaceCopy(context.Background(), source, target, "source", "target"); !errors.Is(err, status_error.ChatDirectoryOwnershipInvalid) {
		t.Fatalf("foreign source error = %v", err)
	}
	assertCopyPathMissing(t, target)
	writeCopyFixtureFile(t, filepath.Join(source, constants.ChatContextOwnerFileName), `{"kind":"`+constants.AgentExecutionWorkspaceOwnerKind+`","wakerId":"source"}`)
	writeCopyFixtureFile(t, filepath.Join(target, "existing.txt"), "keep me")
	if err := PrepareAgentWorkspaceCopy(context.Background(), source, target, "source", "target"); !errors.Is(err, status_error.WorkspaceResourceConflict) {
		t.Fatalf("target conflict error = %v", err)
	}
	assertCopyFile(t, filepath.Join(target, "existing.txt"), "keep me")
}

func TestAgentWorkspaceCopyRecoveryUsesCommittedPartnerPresence(t *testing.T) {
	agents, source, uncommitted := workspaceCopyFixture(t)
	committed := filepath.Join(agents, "committed")
	writeCopyFixtureFile(t, filepath.Join(source, "notes.txt"), "durable copy")
	for id, target := range map[string]string{"target": uncommitted, "committed": committed} {
		if err := PrepareAgentWorkspaceCopy(context.Background(), source, target, "source", id); err != nil {
			t.Fatal(err)
		}
	}
	exists := func(_ context.Context, id string) (bool, error) { return id == "committed", nil }
	if err := RecoverAgentWorkspaceCopies(context.Background(), agents, exists); err != nil {
		t.Fatal(err)
	}
	assertCopyPathMissing(t, uncommitted)
	assertCopyFile(t, filepath.Join(committed, "notes.txt"), "durable copy")
	assertCopyFile(t, filepath.Join(source, "notes.txt"), "durable copy")
	if err := RecoverAgentWorkspaceCopies(context.Background(), agents, func(context.Context, string) (bool, error) {
		t.Fatal("already settled copy was reconciled again")
		return false, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAgentWorkspaceCopyRecoveryDiscardsPrepublicationStaging(t *testing.T) {
	agents, _, target := workspaceCopyFixture(t)
	staging := filepath.Join(agents, agentCopyStaging, "target")
	writeCopyFixtureFile(t, filepath.Join(staging, "28c5ef20e05f474289107d43f5cb31c8", agentCopyPayload, "partial.txt"), "unfinished copy")
	writeCopyFixtureFile(t, filepath.Join(target, "existing.txt"), "unrelated data")
	if err := RecoverAgentWorkspaceCopies(context.Background(), agents, func(context.Context, string) (bool, error) {
		return false, nil
	}); err != nil {
		t.Fatal(err)
	}
	assertCopyPathMissing(t, staging)
	assertCopyFile(t, filepath.Join(target, "existing.txt"), "unrelated data")
}

func TestAgentWorkspaceCopyRecoveryPreservesAnUnrelatedWithdrawnDirectory(t *testing.T) {
	agents, _, target := workspaceCopyFixture(t)
	attempt := filepath.Join(agents, agentCopyStaging, "target", "28c5ef20e05f474289107d43f5cb31c8")
	writeCopyFixtureFile(t, filepath.Join(attempt, "withdrawn", "existing.txt"), "unrelated data")
	if err := RecoverAgentWorkspaceCopies(context.Background(), agents, func(context.Context, string) (bool, error) {
		return false, nil
	}); !errors.Is(err, status_error.ChatDirectoryOwnershipInvalid) {
		t.Fatalf("withdrawn ownership error = %v", err)
	}
	assertCopyFile(t, filepath.Join(target, "existing.txt"), "unrelated data")
}

func TestAgentWorkspaceCopyRollbackPreservesAReplacementWithTheSameOwner(t *testing.T) {
	_, source, target := workspaceCopyFixture(t)
	if err := PrepareAgentWorkspaceCopy(context.Background(), source, target, "source", "target"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAgentWorkspace(target, "target"); err != nil {
		t.Fatal(err)
	}
	writeCopyFixtureFile(t, filepath.Join(target, "replacement.txt"), "unrelated data")
	if err := SettleAgentWorkspaceCopy(target, "target", false); !errors.Is(err, status_error.ChatDirectoryOwnershipInvalid) {
		t.Fatalf("replacement ownership error = %v", err)
	}
	assertCopyFile(t, filepath.Join(target, "replacement.txt"), "unrelated data")
}

func TestAgentWorkspaceCopyRejectsCanceledRequestBeforeCreatingTarget(t *testing.T) {
	_, source, target := workspaceCopyFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := PrepareAgentWorkspaceCopy(ctx, source, target, "source", "target"); !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("canceled copy error = %v", err)
	}
	assertCopyPathMissing(t, target)
}

func workspaceCopyFixture(t *testing.T) (agents, source, target string) {
	t.Helper()
	agents = filepath.Join(t.TempDir(), "Agents")
	source, target = filepath.Join(agents, "source"), filepath.Join(agents, "target")
	if err := EnsureAgentWorkspace(source, "source"); err != nil {
		t.Fatal(err)
	}
	return agents, source, target
}

func writeCopyFixtureFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func makeCopyFixtureLink(t *testing.T, value, path string) {
	t.Helper()
	if err := os.Symlink(value, path); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink creation unavailable: %v", err)
		}
		t.Fatal(err)
	}
}

func assertCopyFile(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil || string(content) != want {
		t.Fatalf("read %s = %q, %v, want %q", path, content, err, want)
	}
}

func assertCopyPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path %s should be absent, stat error = %v", path, err)
	}
}
