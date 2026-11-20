package cliinstall

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallExecutablePreservesForeignFile(t *testing.T) {
	dir := t.TempDir()
	source, target := filepath.Join(dir, "bundled"), filepath.Join(dir, "botcli")
	writeFixture(t, source, "bundled executable")
	writeFixture(t, target, "user executable")
	if err := installExecutable(t.Context(), source, target); err == nil {
		t.Fatal("installation overwrote an unrelated executable")
	}
	if got := readFixture(t, target); got != "user executable" {
		t.Fatalf("unrelated executable changed: %q", got)
	}
}

func TestInstallExecutableCopiesSourceIntoAbsentTarget(t *testing.T) {
	dir := t.TempDir()
	source, target := filepath.Join(dir, "bundled"), filepath.Join(dir, "botcli")
	writeFixture(t, source, "bundled executable")
	if err := installExecutable(t.Context(), source, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if got := readFixture(t, target); got != "bundled executable" {
		t.Fatalf("installed executable depends on the bundle: %q", got)
	}
}

func writeFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFixture(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
