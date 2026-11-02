package files

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

func TestParseWorkspaceValidatesFrozenRunWithoutCreatingDirectories(t *testing.T) {
	workflowID, runID := util.New(), util.New()
	root := filepath.Join(t.TempDir(), "workflow-workspaces", workflowID)
	value := map[string]any{"rootDirectory": root, "scriptsDirectory": filepath.Join(root, "scripts"), "stateDirectory": filepath.Join(root, "state"), "cacheDirectory": filepath.Join(root, "cache"), "temporaryDirectory": filepath.Join(root, "tmp"), "runsDirectory": filepath.Join(root, "runs"), "runDirectory": filepath.Join(root, "runs", runID)}
	workspace, err := ParseWorkspace(value, workflowID, runID)
	if err != nil || workspace.RootDirectory != root || workspace.RunDirectory != filepath.Join(root, "runs", runID) {
		t.Fatalf("valid frozen paths could not be read: %+v %v", workspace, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("pure parsing created or accessed a missing workspace: %v", err)
	}
	for _, field := range []string{"rootDirectory", "scriptsDirectory", "stateDirectory", "cacheDirectory", "temporaryDirectory", "runsDirectory", "runDirectory"} {
		original := value[field]
		delete(value, field)
		if _, err := ParseWorkspace(value, workflowID, runID); err == nil {
			t.Fatalf("incomplete workspace accepted without %s", field)
		}
		value[field] = filepath.Join(t.TempDir(), "outside")
		if _, err := ParseWorkspace(value, workflowID, runID); err == nil {
			t.Fatalf("workspace accepted unrelated %s", field)
		}
		value[field] = original
	}
	for _, malformed := range []any{nil, "not-workspace", map[string]any{"rootDirectory": 1}} {
		_, err := ParseWorkspace(malformed, workflowID, runID)
		var failure interface{ Key() string }
		if !errors.As(err, &failure) || failure.Key() != status_error.WorkflowStateInvalid.Key() {
			t.Fatalf("malformed workspace error lost its cause: %v", err)
		}
	}
	_, err = ParseWorkspace(value, workflowID, "")
	var failure interface{ Key() string }
	if !errors.As(err, &failure) || failure.Key() != status_error.WorkflowStateInvalid.Key() {
		t.Fatalf("missing run identity accepted an authoring workspace: %v", err)
	}
	_, err = ParseWorkspace(value, util.New(), runID)
	if !errors.As(err, &failure) || failure.Key() != status_error.WorkflowStateConflict.Key() {
		t.Fatalf("another workflow reused a frozen layout: %v", err)
	}
}

func TestUploadOwnershipAndIndependentRunFiles(t *testing.T) {
	ctx := context.Background()
	data, scope := t.TempDir(), t.TempDir()
	scope, err := filepath.EvalSymlinks(scope)
	if err != nil {
		t.Fatal(err)
	}
	definition, task := util.New(), util.New()
	content := []byte{0, 1, 0xff, 0x50, 0x4b}
	upload, err := Save(ctx, data, definition, task, "预算.xlsx", bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReference(data, definition, util.New(), upload.Reference, false); err == nil {
		t.Fatal("cross-task upload accepted")
	}
	if err := ValidateReference(data, util.New(), task, upload.Reference, false); err == nil {
		t.Fatal("cross-workflow upload accepted")
	}
	for _, name := range []string{"../secret", "..", "a\\b", "a\n.txt"} {
		if _, err := Save(ctx, data, definition, task, name, bytes.NewReader(content)); err == nil {
			t.Fatalf("invalid filename: %q", name)
		}
	}
	first, err := Materialize(ctx, data, definition, task, util.New(), scope, upload.Reference)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first, []byte("modified"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := Materialize(ctx, data, definition, task, util.New(), scope, upload.Reference)
	if err != nil || first == second {
		t.Fatalf("independent copies: %s %v", second, err)
	}
	file, err := OpenScopeFile(scope, second)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(file)
	file.Close()
	if err != nil || !bytes.Equal(actual, content) {
		t.Fatal("original bytes changed")
	}
	outside := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(scope, "escape.xlsx")); err == nil {
		if file, err := OpenScopeFile(scope, "escape.xlsx"); err == nil {
			file.Close()
			t.Fatal("symlink escaped")
		}
	}
	id, _, _ := referenceParts(upload.Reference)
	if err := os.WriteFile(filepath.Join(data, "workflow-files", definition, id, "content"), bytes.Repeat([]byte{2}, len(content)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Materialize(ctx, data, definition, task, util.New(), scope, upload.Reference); err == nil {
		t.Fatal("tampered upload materialized")
	}
}

func TestIntegrationAutomaticTaskAPIFileOwnerIsolation(t *testing.T) {
	ctx := context.Background()
	directory, scope := t.TempDir(), t.TempDir()
	definition, task := util.New(), util.New()
	upload, err := Save(ctx, directory, definition, task, "input.md", bytes.NewReader([]byte("# input")), "key-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReference(directory, definition, task, upload.Reference, false, "key-a"); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"key-b", "task-token"} {
		if err := ValidateReference(directory, definition, task, upload.Reference, false, owner); err == nil {
			t.Fatal("cross owner reference accepted", owner)
		}
	}
	if err := ValidateReference(directory, definition, task, upload.Reference, false); err == nil {
		t.Fatal("taskToken task token adopted integration upload")
	}
	if _, err := Materialize(ctx, directory, definition, task, util.New(), scope, upload.Reference, "key-b"); err == nil {
		t.Fatal("cross owner file copied into workflow")
	}
	if _, err := Materialize(ctx, directory, definition, task, util.New(), scope, upload.Reference, "key-a"); err != nil {
		t.Fatal(err)
	}
	taskToken, err := Save(ctx, directory, definition, task, "task-token.md", bytes.NewReader([]byte("task-token")))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReference(directory, definition, task, taskToken.Reference, false, "key-a"); err == nil {
		t.Fatal("integration key adopted task token upload")
	}
}

func TestManagedWorkspaceCreatesOwnedSubdirectories(t *testing.T) {
	managed, workflowID := t.TempDir(), util.New()
	root, err := PrepareManagedWorkspace(managed, workflowID)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"scripts", "state", "cache", "tmp", "runs"} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("managed %s directory missing or not private: %v %v", name, info, err)
		}
	}
}

func TestManagedWorkspaceRejectsDirectorySymlinks(t *testing.T) {
	for _, link := range []string{"workflow-workspaces", "workflow-workspaces/WORKFLOW", "workflow-workspaces/WORKFLOW/cache"} {
		t.Run(link, func(t *testing.T) {
			managed, outside, workflowID := t.TempDir(), t.TempDir(), util.New()
			link = strings.ReplaceAll(link, "WORKFLOW", workflowID)
			target := filepath.Join(managed, link)
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, target); err != nil {
				t.Fatal(err)
			}
			if _, err := PrepareManagedWorkspace(managed, workflowID); err == nil {
				t.Fatal("managed directory followed a symlink")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("outside directory changed: %v %v", entries, err)
			}
		})
	}
}

func TestWorkspaceSeparatesRunsAndKeepsSharedState(t *testing.T) {
	managed, workflowID := t.TempDir(), util.New()
	first, err := PrepareWorkspace(managed, workflowID, util.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first.StateDirectory, "state.json"), []byte("state"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := PrepareWorkspace(managed, workflowID, util.New())
	if err != nil {
		t.Fatal(err)
	}
	if first.RootDirectory != second.RootDirectory || first.RunDirectory == second.RunDirectory {
		t.Fatalf("run directories not separated: %+v %+v", first, second)
	}
	content, err := os.ReadFile(filepath.Join(second.StateDirectory, "state.json"))
	if err != nil || string(content) != "state" {
		t.Fatalf("shared state was lost: %q %v", content, err)
	}
	for _, directory := range []string{first.RunDirectory, second.RunDirectory} {
		info, err := os.Stat(directory)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("run directory is not private: %v %v", info, err)
		}
	}
	for _, invalid := range []string{"../outside", "", "not-a-workflow"} {
		if _, err := PrepareWorkspace(managed, invalid, ""); err == nil {
			t.Fatalf("invalid workflow identity accepted: %q", invalid)
		}
	}
	if _, err := PrepareWorkspace(managed, workflowID, "../outside"); err == nil {
		t.Fatal("invalid run identity accepted")
	}
}

func TestUploadRequiresPersistedOwner(t *testing.T) {
	ctx := context.Background()
	directory, definition, task := t.TempDir(), util.New(), util.New()
	upload, err := Save(ctx, directory, definition, task, "input.md", strings.NewReader("input"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := storePath(directory, definition)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(root, "*", "metadata.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("missing stored metadata: %v %v", paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var metadata storedUpload
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	metadata.OwnerKeyID = ""
	data, err = json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[0], data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateReference(directory, definition, task, upload.Reference, false); err == nil {
		t.Fatal("upload with missing ownership was accepted")
	}
}
