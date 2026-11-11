package chatstorage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestForkAttachmentCopiesOnlyPrivateAttachmentFiles(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	for _, relative := range []string{"attachments/message/file.txt", "attachments/replaced/file.txt", "log/chat.jsonl", "file-results/private.json"} {
		path := filepath.Join(source, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(relative), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := CopyForkAttachment(source, target, "attachments/message/file.txt"); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"log/chat.jsonl", "file-results/private.json"} {
		if err := CopyForkAttachment(source, target, relative); err == nil {
			t.Fatalf("copied private nonattachment: %s", relative)
		}
	}
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "attachments"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(target, "attachments", "replaced")); err != nil {
		t.Fatal(err)
	}
	if err := CopyForkAttachment(source, target, "attachments/replaced/file.txt"); err == nil {
		t.Fatal("accepted replaced target directory")
	}
}
