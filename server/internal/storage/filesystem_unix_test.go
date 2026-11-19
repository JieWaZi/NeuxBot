//go:build darwin || linux

package storage

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestMeasureSkipsSpecialFiles(t *testing.T) {
	root := t.TempDir()
	writeMeasuredFile(t, root, "known", 4)
	if err := unix.Mkfifo(filepath.Join(root, "named-pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Measure(t.Context(), Roots{Data: root, Managed: root}, Limits{})
	if err != nil || !got.Complete || got.Bytes != 4 || got.Files != 1 {
		t.Fatalf("measurement=%+v, error=%v", got, err)
	}
}
