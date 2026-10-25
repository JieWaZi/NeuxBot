package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContentStoreDefersDeletedResourceUntilExecutionReleases(t *testing.T) {
	root := t.TempDir()
	content := filepath.Join(root, "plugins", "content", "snapshot")
	data := filepath.Join(root, "plugins", "data", "resource")
	for _, directory := range []string{content, data} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	live := []string{content, data}
	store := NewContentStore(root, func() ([]string, error) { return live, nil })
	release, err := store.Acquire(content, data)
	if err != nil {
		t.Fatal(err)
	}
	live = nil
	if err := store.Collect(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(content); err != nil {
		t.Fatal("active content deleted", err)
	}
	release()
	release()
	for _, directory := range []string{content, data} {
		if _, err := os.Stat(directory); !os.IsNotExist(err) {
			t.Fatal("unreferenced content retained", err)
		}
	}
	if _, err := store.Acquire(content); err == nil {
		t.Fatal("admitted already collected content")
	}
}

func TestContentStoreKeepsSharedVersionAndRejectsForeignRoots(t *testing.T) {
	root := t.TempDir()
	content := filepath.Join(root, "plugins", "content", "same-content")
	if err := os.MkdirAll(content, 0700); err != nil {
		t.Fatal(err)
	}
	store := NewContentStore(root, func() ([]string, error) { return []string{content}, nil })
	if err := store.Collect(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(content); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire(t.TempDir()); err == nil {
		t.Fatal("accepted foreign directory")
	}
}

func TestContentStoreNeverCollectsThroughForeignManagedDirectoryLink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "plugins"), 0700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "keep")
	if err := os.MkdirAll(victim, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "plugins", "content")); err != nil {
		t.Skip(err)
	}
	store := NewContentStore(root, func() ([]string, error) { return nil, nil })
	if err := store.Collect(); err == nil {
		t.Error("managed root escape accepted")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("foreign directory was deleted", err)
	}
}
