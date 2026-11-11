package files

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
