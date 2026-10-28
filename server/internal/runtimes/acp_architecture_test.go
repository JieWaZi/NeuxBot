package runtimes

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRuntimePackageOwnsNoPrivateExecutionProtocolOrProcessLauncher(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve Runtime package directory")
	}
	directory := filepath.Dir(currentFile)
	for _, forbidden := range []string{
		"invocation.go",
		"run.go",
		"connection.go",
		"acp_run.go",
		"acp_writer.go",
		"acp_permission.go",
		"acp_elicitation.go",
		"process.go",
		"protocol.go",
		"event_queue.go",
		"parser_claude.go",
		"parser_codex.go",
		"parser_codex_appserver.go",
	} {
		if _, err := os.Stat(filepath.Join(directory, forbidden)); !os.IsNotExist(err) {
			t.Fatalf("legacy Runtime implementation %q still exists: %v", forbidden, err)
		}
	}

	files := token.NewFileSet()
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(files, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", entry.Name(), err)
		}
		for _, imported := range parsed.Imports {
			importPath := strings.Trim(imported.Path.Value, `"`)
			if importPath == "os/exec" ||
				strings.HasSuffix(importPath, "/pkg/codex/protocol") ||
				strings.HasSuffix(importPath, "/pkg/claude/protocol") {
				return fmt.Errorf(
					"production Runtime file %s owns private execution concern %q",
					entry.Name(),
					importPath,
				)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "acp_") ||
			strings.HasPrefix(name, "acp_binding") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		lowered := strings.ToLower(string(source))
		if strings.Contains(lowered, "codex") || strings.Contains(lowered, "claude") ||
			strings.Contains(lowered, "cursor") || strings.Contains(lowered, "kimi") ||
			strings.Contains(string(source), "RuntimeIDPi") || strings.Contains(lowered, "acppi") {
			t.Fatalf("shared ACP module %s contains provider-specific behavior", name)
		}
	}
}
