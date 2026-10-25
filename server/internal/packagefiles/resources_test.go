package packagefiles

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestResourcesInspectMetadataAndReadSelectedFiles(t *testing.T) {
	archive := Archive{Invalid: status_error.PluginContentInvalid, Limit: status_error.PluginContentLimitExceeded, IO: status_error.PluginStorageError}
	root := t.TempDir()
	for name, content := range map[string][]byte{
		"SKILL.md": []byte("# Reports"), "references/rules.md": []byte("Use a template"), "assets/icon.bin": {0xff, 0, 0xfe}, "private/credentials": []byte("not a skill resource"),
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	resources, err := archive.Resources(root, []string{"private"})
	if err != nil || len(resources) != 5 {
		t.Fatalf("unexpected resources: %+v %v", resources, err)
	}
	file, err := archive.ReadResource(root, "references/rules.md", resources)
	if err != nil || file.Binary || file.Content != "Use a template" || !file.Executable {
		t.Fatalf("text resource was not preserved: %+v %v", file, err)
	}
	file, err = archive.ReadResource(root, "assets/icon.bin", resources)
	if err != nil || !file.Binary || file.Content != "" || file.ContentBase64 != "/wD+" {
		t.Fatalf("binary resource was not encoded: %+v %v", file, err)
	}
	for _, path := range []string{"../outside", "/etc/passwd", "private/credentials", "references"} {
		if _, err := archive.ReadResource(root, path, resources); !errors.Is(err, status_error.PluginContentInvalid) {
			t.Fatalf("unlisted or escaping file %q accepted: %v", path, err)
		}
	}
	if err := os.Rename(filepath.Join(root, "references"), filepath.Join(root, "saved-references")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "saved-references"), filepath.Join(root, "references")); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.ReadResource(root, "references/rules.md", resources); !errors.Is(err, status_error.PluginContentInvalid) {
		t.Fatalf("resource parent symlink accepted: %v", err)
	}
	if _, err := archive.Resources(root, []string{"private"}); !errors.Is(err, status_error.PluginContentInvalid) {
		t.Fatalf("resource list followed a symlink: %v", err)
	}
}

func TestResourcePreviewIsBoundedIndependentlyFromListing(t *testing.T) {
	archive := Archive{Invalid: status_error.PluginContentInvalid, Limit: status_error.PluginContentLimitExceeded, IO: status_error.PluginStorageError}
	root := t.TempDir()
	file, err := os.Create(filepath.Join(root, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxResourcePreviewBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	resources, err := archive.Resources(root, nil)
	if err != nil || len(resources) != 1 || resources[0].Size != maxResourcePreviewBytes+1 || resources[0].Digest == "" {
		t.Fatalf("large file metadata unavailable: %+v %v", resources, err)
	}
	if _, err := archive.ReadResource(root, "large.bin", resources); !errors.Is(err, status_error.PluginContentLimitExceeded) {
		t.Fatalf("large preview accepted: %v", err)
	}
	if err := os.Truncate(filepath.Join(root, "large.bin"), maxImportedFileBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Resources(root, nil); !errors.Is(err, status_error.PluginContentLimitExceeded) {
		t.Fatalf("oversized resource listing accepted: %v", err)
	}
}
