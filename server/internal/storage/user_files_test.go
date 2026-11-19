package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
)

func TestManagedUserFilesExcludeSystemDataAndCheckVersionBeforeRemoval(t *testing.T) {
	managed := t.TempDir()
	if err := workspace.EnsureAgentWorkspace(filepath.Join(managed, "Agents/partner"), "partner"); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		"Agents/partner/report.md",
		"Agents/partner/.git/config",
		"Agents/partner/AGENTS.md",
		"Conversations/chat/attachments/message/image.png",
		"Conversations/chat/log/chat.jsonl",
		"Conversations/chat/file-results/git/object",
	}
	for _, path := range paths {
		target := filepath.Join(managed, path)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("sample"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	marker := "Conversations/chat/attachments/message/workspace-owner.json"
	if err := os.WriteFile(filepath.Join(managed, marker), []byte(`{"kind":"message-attachments","chatId":"chat","messageId":"message"}`), 0600); err != nil {
		t.Fatal(err)
	}
	measurement, err := Measure(t.Context(), Roots{Managed: managed, Data: t.TempDir()}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	managed = measurement.Roots.Managed
	userFiles := map[string]FileEntry{}
	for _, entry := range measurement.Entries {
		if id, _, _, ok := entry.UserFile(managed); ok {
			userFiles[id] = entry
		}
	}
	if len(userFiles) != 2 {
		t.Fatalf("userFiles=%+v", userFiles)
	}
	work := userFiles[paths[0]]
	if err := os.WriteFile(work.Path, []byte("new version"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := work.RemoveUserFile(managed, nil, nil); !errors.Is(err, status_error.WorkspaceResourceConflict) {
		t.Fatalf("changed file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(managed, marker), []byte(`{"kind":"message-attachments","chatId":"other","messageId":"message"}`), 0600); err != nil {
		t.Fatal(err)
	}
	upload := userFiles[paths[3]]
	if err := upload.RemoveUserFile(managed, nil, nil); !errors.Is(err, status_error.ChatDirectoryOwnershipInvalid) {
		t.Fatalf("wrong owner: %v", err)
	}
	if err := os.WriteFile(filepath.Join(managed, marker), []byte(`{"kind":"message-attachments","chatId":"chat","messageId":"message"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := upload.RemoveUserFile(managed, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(upload.Path); !os.IsNotExist(err) {
		t.Fatalf("upload still exists: %v", err)
	}
	for _, path := range []string{paths[0], paths[1], paths[2], paths[4], paths[5], marker} {
		if _, err := os.Stat(filepath.Join(managed, path)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestManagedFileAliasesKeepBothPathsAndCountBytesOnce(t *testing.T) {
	managed := t.TempDir()
	for _, partner := range []string{"first", "second"} {
		if err := os.MkdirAll(filepath.Join(managed, "Agents", partner), 0700); err != nil {
			t.Fatal(err)
		}
	}
	first := filepath.Join(managed, "Agents/first/report.md")
	second := filepath.Join(managed, "Agents/second/report.md")
	if err := os.WriteFile(first, []byte("bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(first, second); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	measurement, err := Measure(t.Context(), Roots{Managed: managed, Data: t.TempDir()}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Bytes != 5 || len(measurement.Entries) != 1 || len(measurement.FileAliases) != 1 {
		t.Fatalf("measurement=%+v", measurement)
	}
	owner := measurement.Entries[0].WakerID
	alias := measurement.FileAliases[0].WakerID
	if owner == alias || owner == "" || alias == "" {
		t.Fatalf("hard link owners: %q and %q", owner, alias)
	}
}

func TestManagedAttachmentRemovalRollsBackRecordedReferenceOnConflict(t *testing.T) {
	managed := t.TempDir()
	directory := filepath.Join(managed, "Conversations/chat/attachments/message")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "upload.txt")
	if err := os.WriteFile(file, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "workspace-owner.json"), []byte(`{"kind":"message-attachments","chatId":"chat","messageId":"message"}`), 0600); err != nil {
		t.Fatal(err)
	}
	measurement, err := Measure(t.Context(), Roots{Data: t.TempDir(), Managed: managed}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range measurement.Entries {
		if filepath.Base(entry.Path) != "upload.txt" {
			continue
		}
		const marker = "removed-test.json"
		removeErr := entry.RemoveUserFile(measurement.Roots.Managed, nil, func(owner *os.Root, _ string) (func() error, error) {
			if err := owner.WriteFile(marker, []byte(`true`), 0600); err != nil {
				return nil, err
			}
			if err := os.WriteFile(file, []byte("new content"), 0600); err != nil {
				return nil, err
			}
			return func() error { return owner.Remove(marker) }, nil
		})
		if !errors.Is(removeErr, status_error.WorkspaceResourceConflict) {
			t.Fatalf("remove changed file: %v", removeErr)
		}
		if content, err := os.ReadFile(file); err != nil || string(content) != "new content" {
			t.Fatalf("changed file was lost: %q %v", content, err)
		}
		if _, err := os.Stat(filepath.Join(managed, "Conversations/chat", marker)); !os.IsNotExist(err) {
			t.Fatalf("failed delete left a false removed record: %v", err)
		}
		return
	}
	t.Fatal("attachment was not measured")
}
