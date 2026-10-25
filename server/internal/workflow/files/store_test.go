package files

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

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
	for _, owner := range []string{"key-b", "legacy"} {
		if err := ValidateReference(directory, definition, task, upload.Reference, false, owner); err == nil {
			t.Fatal("cross owner reference accepted", owner)
		}
	}
	if err := ValidateReference(directory, definition, task, upload.Reference, false); err == nil {
		t.Fatal("legacy task token adopted integration upload")
	}
	if _, err := Materialize(ctx, directory, definition, task, util.New(), scope, upload.Reference, "key-b"); err == nil {
		t.Fatal("cross owner file copied into workflow")
	}
	if _, err := Materialize(ctx, directory, definition, task, util.New(), scope, upload.Reference, "key-a"); err != nil {
		t.Fatal(err)
	}
	legacy, err := Save(ctx, directory, definition, task, "legacy.md", bytes.NewReader([]byte("legacy")))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReference(directory, definition, task, legacy.Reference, false, "key-a"); err == nil {
		t.Fatal("integration key adopted old token upload")
	}
}
