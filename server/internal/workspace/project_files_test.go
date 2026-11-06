package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProjectFilesMapsSourceWithoutCopying(t *testing.T) {
	project := t.TempDir()
	source := filepath.Join(project, "server", "main.go")
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("package main"), 0o600); err != nil {
		t.Fatal(err)
	}

	references, err := ResolveProjectFiles(
		[]ProjectLink{{EntryName: "ally", SourcePath: project}},
		[]string{source},
	)
	if err != nil {
		t.Fatalf("ResolveProjectFiles() error = %v", err)
	}
	resolvedSource, err := filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(references) != 1 || references[0].Name != "main.go" || references[0].Path != resolvedSource {
		t.Fatalf("references = %+v", references)
	}
	if content, readErr := os.ReadFile(source); readErr != nil || string(content) != "package main" {
		t.Fatalf("source changed: content=%q error=%v", content, readErr)
	}
}

func TestResolveProjectFilesRejectsOutsideSource(t *testing.T) {
	project := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := ResolveProjectFiles(
		[]ProjectLink{{EntryName: "ally", SourcePath: project}},
		[]string{outside},
	); err == nil {
		t.Fatal("ResolveProjectFiles() error = nil, want outside-source rejection")
	}
}
