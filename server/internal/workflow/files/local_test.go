package files

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

func TestRecoverStagedInputsRemovesOwnedAttemptsAndKeepsOtherFiles(t *testing.T) {
	managed, definition, runID := t.TempDir(), util.New(), util.New()
	workspace, err := PrepareWorkspace(managed, definition, runID)
	if err != nil {
		t.Fatal(err)
	}
	stage, err := StageInputs(workspace, runID)
	if err != nil {
		t.Fatal(err)
	}
	stageRoot := filepath.Join(workspace.RootDirectory, constants.WorkflowInputStagingDirectoryName)
	if filepath.Dir(stage.Directory) != stageRoot {
		t.Fatalf("stage %s outside dedicated root %s", stage.Directory, stageRoot)
	}
	if err := os.WriteFile(filepath.Join(stage.Directory, "partial"), []byte("retired"), 0o600); err != nil {
		t.Fatal(err)
	}
	unowned := filepath.Join(workspace.TemporaryDirectory, "workflow-input-staging-1234")
	accepted := filepath.Join(workspace.RootDirectory, constants.WorkflowInputFileDirectoryName, runID)
	orphanID := util.New()
	orphan, err := StageInputs(workspace, orphanID)
	if err != nil {
		t.Fatal(err)
	}
	orphanPath := filepath.Join(constants.WorkflowInputFileDirectoryName, orphanID)
	if err := os.MkdirAll(filepath.Join(orphan.Directory, orphanPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := orphan.Publish(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{unowned, accepted} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := RecoverStagedInputs(t.Context(), managed, func(_ context.Context, id string, _ []string) ([]string, error) {
			if id != definition {
				t.Fatalf("checked the wrong workflow: %s", id)
			}
			return []string{runID}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(workspace.RootDirectory, orphanPath)); !os.IsNotExist(err) {
		t.Fatalf("uncommitted published input remains: %v", err)
	}
	if _, err := os.Stat(stageRoot); !os.IsNotExist(err) {
		t.Fatalf("retired stage root remains: %v", err)
	}
	for _, path := range []string{unowned, accepted} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unrelated input was removed at %s: %v", path, err)
		}
	}
	fresh, err := StageInputs(workspace, runID)
	if err != nil {
		t.Fatal(err)
	}
	fresh.Close()
	if _, err := os.Stat(fresh.Directory); !os.IsNotExist(err) {
		t.Fatalf("closed stage remains: %v", err)
	}
}

func TestCLIInputFreezesOnlyApprovedFilesForTheRun(t *testing.T) {
	approved, workspace := t.TempDir(), t.TempDir()
	approved, _ = filepath.EvalSymlinks(approved)
	workspace, _ = filepath.EvalSymlinks(workspace)
	path := filepath.Join(approved, "input.txt")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	frozen, err := FreezeLocalInput(context.Background(), []string{approved}, nil, path, workspace, "run", "/file", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(frozen.Path); err != nil || string(data) != "original" {
		t.Fatalf("run input was mutable through its source: %q %v", data, err)
	}
	outside := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(outside, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := FreezeLocalInput(context.Background(), []string{approved}, nil, outside, workspace, "run", "/outside", ""); err == nil {
		t.Fatal("CLI widened its approved file roots")
	}
}

func TestReplayFrozenUploadRequiresItsRecordedDigest(t *testing.T) {
	ctx := t.Context()
	directory := t.TempDir()
	definition, sourceID, destinationID := util.New(), util.New(), util.New()
	source, err := PrepareWorkspace(directory, definition, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := PrepareWorkspace(directory, definition, destinationID)
	if err != nil {
		t.Fatal(err)
	}
	upload, err := Save(ctx, directory, definition, "", "report.txt", strings.NewReader("approved"))
	if err != nil {
		t.Fatal(err)
	}
	copy, err := Materialize(ctx, directory, definition, "", sourceID, source.RootDirectory, upload.Reference)
	if err != nil {
		t.Fatal(err)
	}
	id, _, _ := referenceParts(upload.Reference)
	store, err := storePath(directory, definition)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(store, id, "content")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReplayFrozenInput(ctx, source, sourceID, destination, destinationID, "/report", copy.Path, ""); !util.HasStatusCode(err, status_error.WorkflowFileInvalid) {
		t.Fatalf("replayed without a recorded digest: %v", err)
	}
	verified, err := ReplayFrozenInput(ctx, source, sourceID, destination, destinationID, "/report", copy.Path, copy.Digest)
	if err != nil || verified.Digest != upload.Digest || verified.Path == copy.Path {
		t.Fatalf("recorded digest did not verify the run's frozen copy: %+v %v", verified, err)
	}
	if err := os.WriteFile(copy.Path, []byte("replaced"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReplayFrozenInput(ctx, source, sourceID, destination, destinationID, "/report", copy.Path, copy.Digest); !util.HasStatusCode(err, status_error.WorkflowFileInvalid) {
		t.Fatalf("a changed frozen copy was accepted: %v", err)
	}
}
