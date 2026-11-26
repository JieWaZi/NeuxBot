package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	managedskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
)

func writeMeasuredFile(t *testing.T, root, relative string, size int) string {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", size)), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMeasureClassifiesSystemSkillsOnlyInOwnedExecutionWorkspaces(t *testing.T) {
	managed := t.TempDir()
	layout, err := chatstorage.NewLayout(managed)
	if err != nil {
		t.Fatal(err)
	}
	threadID, chatID, wakerID := util.New(), util.New(), util.New()
	member, err := layout.TeamMemberWorkspace(threadID, wakerID, false)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := layout.TeamSharedDirectory(threadID)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(shared, "backup")
	item, err := managedskills.SystemSkillDefinition()
	if err != nil {
		t.Fatal(err)
	}
	var document []byte
	for _, file := range item.Files {
		if file.Path == "SKILL.md" {
			document = file.Data
		}
	}
	if len(document) == 0 {
		t.Fatal("missing system skill document")
	}
	digest := strings.Repeat("f", 64)
	snapshot := managedskills.MaterializationSnapshotPath(digest) + "/SKILL.md"
	manifest := fmt.Sprintf(`{"version":1,"targets":{".cursor/skills":{"bot":%q}}}`, digest)
	for _, directory := range []string{member, backup} {
		if err := workspace.EnsureThreadMemberExecutionWorkspace(directory, threadID, chatID, wakerID); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, managedskills.MaterializationManifestName), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		filename := writeMeasuredFile(t, directory, snapshot, 0)
		if err := os.WriteFile(filename, document, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Measure(t.Context(), Roots{Data: t.TempDir(), Managed: managed}, Limits{})
	if err != nil || !got.Complete {
		t.Fatalf("measurement=%+v, error=%v", got, err)
	}
	categories := make(map[string]constants.StorageCategory)
	for _, file := range got.Entries {
		categories[file.Path] = file.Category
	}
	if category := categories[filepath.Join(member, snapshot)]; category != constants.StorageCategoryPrograms {
		t.Fatalf("owned member snapshot: %s", category)
	}
	if category := categories[filepath.Join(backup, snapshot)]; category != constants.StorageCategoryWorkFiles {
		t.Fatalf("shared backup counted as %s", category)
	}
}

func TestMeasureClassifiesPartnerCopyStagingAsRecovery(t *testing.T) {
	managed := t.TempDir()
	writeMeasuredFile(t, managed, filepath.Join("Agents", constants.WakerCopyStagingDirectoryName, "partner", "partial.txt"), 3)
	result, err := Measure(t.Context(), Roots{Data: t.TempDir(), Managed: managed}, Limits{})
	if err != nil || len(result.Entries) != 1 {
		t.Fatalf("measurement=%+v, error=%v", result, err)
	}
	file := result.Entries[0]
	if file.Category != constants.StorageCategoryRecovery || file.WakerID != "" {
		t.Fatalf("staged partner file classified as %+v", file)
	}
}

func TestMeasureKeepsPublishedPartnerCopyOutOfRankingsUntilSettled(t *testing.T) {
	roots := Roots{Data: t.TempDir(), Managed: t.TempDir()}
	layout, err := chatstorage.NewLayout(roots.Managed)
	if err != nil {
		t.Fatal(err)
	}
	source, err := layout.AgentWorkspace("source")
	if err != nil {
		t.Fatal(err)
	}
	target, err := layout.AgentWorkspace("target")
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.EnsureAgentWorkspace(source, "source"); err != nil {
		t.Fatal(err)
	}
	writeMeasuredFile(t, source, "notes.txt", 3)
	if err := workspace.PrepareAgentWorkspaceCopy(t.Context(), source, target, "source", "target"); err != nil {
		t.Fatal(err)
	}
	measureFile := func(path string) FileEntry {
		t.Helper()
		measurement, err := Measure(t.Context(), roots, Limits{})
		if err != nil || !measurement.Complete {
			t.Fatalf("measurement=%+v, error=%v", measurement, err)
		}
		for _, file := range measurement.Entries {
			if file.Path == path {
				return file
			}
		}
		t.Fatalf("missing measured file %s", path)
		return FileEntry{}
	}
	path := filepath.Join(target, "notes.txt")
	if file := measureFile(path); file.Category != constants.StorageCategoryRecovery || file.WakerID != "" {
		t.Fatalf("unsettled workspace file classified as %+v", file)
	}
	if err := workspace.SettleAgentWorkspaceCopy(target, "target", true); err != nil {
		t.Fatal(err)
	}
	if file := measureFile(path); file.Category != constants.StorageCategoryWorkFiles || file.WakerID != "target" {
		t.Fatalf("settled workspace file classified as %+v", file)
	}
}

func TestMeasureDeduplicatesNestedRoots(t *testing.T) {
	data := t.TempDir()
	managed := filepath.Join(data, "nested")
	writeMeasuredFile(t, data, "bot.db", 3)
	writeMeasuredFile(t, managed, "Conversations/chat-a/log/item", 5)

	got, err := Measure(t.Context(), Roots{Data: data, Managed: managed}, Limits{})
	if err != nil || !got.Complete || got.Bytes != 8 || got.Files != 2 || len(got.Entries) != 2 {
		t.Fatalf("measurement=%+v, error=%v", got, err)
	}
	for _, file := range got.Entries {
		if file.ChatID == "chat-a" && file.OwnerRoot == got.Roots.Managed &&
			file.Folder == filepath.Join(got.Roots.Managed, "Conversations", "chat-a", "log") &&
			file.Category == "conversations" {
			return
		}
	}
	t.Fatalf("chat file did not keep its more specific root and folder: %+v", got.Entries)
}

func TestMeasureClassifiesIdenticalRoots(t *testing.T) {
	root := t.TempDir()
	writeMeasuredFile(t, root, "memory.db-wal", 1)
	writeMeasuredFile(t, root, "plugins/targets/cache", 2)
	writeMeasuredFile(t, root, "Plugins/archives/archive", 3)
	writeMeasuredFile(t, root, "electron/Cache/Cache_Data/entry", 4)
	writeMeasuredFile(t, root, "electron/IndexedDB/value", 5)
	writeMeasuredFile(t, root, "Conversations/chat-a/attachments/upload", 6)
	writeMeasuredFile(t, root, "NativeConversation/0a.jsonl", 7)
	writeMeasuredFile(t, root, "Agents/waker-a/.cursor/notes", 8)
	writeMeasuredFile(t, root, "unrecognized/file", 9)

	got, err := Measure(t.Context(), Roots{Data: root, Managed: root}, Limits{})
	if err != nil || !got.Complete || got.Bytes != 45 || got.Files != 9 {
		t.Fatalf("measurement=%+v, error=%v", got, err)
	}
	want := map[int64]string{
		1: "knowledge", 2: "cache", 3: "cache", 4: "cache", 5: "app_data",
		6: "conversations", 7: "shared_native", 8: "work_files", 9: "other",
	}
	for _, file := range got.Entries {
		if string(file.Category) != want[file.Bytes] {
			t.Errorf("%s category=%s, want %s", file.Path, file.Category, want[file.Bytes])
		}
		if file.Bytes == 7 && file.ChatID != "" {
			t.Errorf("shared shard received a chat identity: %+v", file)
		}
		if file.Bytes == 8 && file.WakerID != "waker-a" {
			t.Errorf("partner work file lost identity: %+v", file)
		}
	}
}

func TestMeasureSkipsInternalSymlinks(t *testing.T) {
	data := t.TempDir()
	external := t.TempDir()
	writeMeasuredFile(t, data, "known", 3)
	writeMeasuredFile(t, external, "secret", 200)
	for name, target := range map[string]string{"external": external, "cycle": data, "missing": filepath.Join(data, "absent")} {
		if err := os.Symlink(target, filepath.Join(data, name)); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
	}

	got, err := Measure(t.Context(), Roots{Data: data, Managed: data}, Limits{})
	if err != nil || !got.Complete || got.Bytes != 3 || got.Files != 1 {
		t.Fatalf("measurement=%+v, error=%v", got, err)
	}
}

func TestMeasureDeduplicatesHardLinksAcrossRoots(t *testing.T) {
	data, managed := t.TempDir(), t.TempDir()
	first := writeMeasuredFile(t, data, "bot.db", 9)
	if err := os.Link(first, filepath.Join(managed, "same-inode")); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}

	got, err := Measure(t.Context(), Roots{Data: data, Managed: managed}, Limits{})
	if err != nil || !got.Complete || got.Bytes != 9 || got.Files != 1 {
		t.Fatalf("measurement=%+v, error=%v", got, err)
	}
}

func TestMeasureUsesSharedEntryBudget(t *testing.T) {
	data, managed := t.TempDir(), t.TempDir()
	writeMeasuredFile(t, data, "a", 1)
	writeMeasuredFile(t, managed, "b", 1)
	writeMeasuredFile(t, managed, "c", 1)

	got, err := Measure(t.Context(), Roots{Data: data, Managed: managed}, Limits{MaxEntries: 2})
	if err != nil || got.Complete || got.Bytes != 2 || got.Files != 2 || len(got.Problems) != 1 ||
		got.Problems[0].Reason != "entry_limit" {
		t.Fatalf("measurement=%+v, error=%v", got, err)
	}
	full, err := Measure(t.Context(), Roots{Data: data, Managed: managed}, Limits{MaxEntries: 3})
	if err != nil || !full.Complete || full.Bytes != 3 || full.Files != 3 {
		t.Fatalf("exact-limit measurement=%+v, error=%v", full, err)
	}
}

func TestMeasurePreservesKnownFilesAtDepthLimit(t *testing.T) {
	root := t.TempDir()
	writeMeasuredFile(t, root, "a/file", 4)
	writeMeasuredFile(t, root, "a/b/ignored", 5)
	writeMeasuredFile(t, root, "known", 2)

	got, err := Measure(t.Context(), Roots{Data: root, Managed: root}, Limits{MaxDepth: 1})
	if err != nil || got.Complete || got.Bytes != 6 || got.Files != 2 || len(got.Problems) != 1 ||
		got.Problems[0].Reason != "depth_limit" || got.Problems[0].Path != "a/b" {
		t.Fatalf("measurement=%+v, error=%v", got, err)
	}
}

func TestMeasureCountsMissingRootAsUnused(t *testing.T) {
	data := t.TempDir()
	managed := filepath.Join(data, "currently", "unused")
	writeMeasuredFile(t, data, "bot.db", 5)

	got, err := Measure(t.Context(), Roots{Data: data, Managed: managed}, Limits{})
	if err != nil || !got.Complete || got.Bytes != 5 || filepath.Base(got.Roots.Managed) != "unused" {
		t.Fatalf("measurement=%+v, error=%v", got, err)
	}
}

func TestMeasureReportsUnreadableDirectory(t *testing.T) {
	root := t.TempDir()
	file := writeMeasuredFile(t, root, "private/file", 4)
	folder := filepath.Dir(file)
	if err := os.Chmod(folder, 0); err != nil {
		t.Skipf("permission change unavailable: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(folder, 0o700) })
	if _, err := os.ReadDir(folder); !os.IsPermission(err) {
		t.Skipf("directory permissions do not restrict this process: %v", err)
	}
	writeMeasuredFile(t, root, "measured", 3)

	got, err := Measure(t.Context(), Roots{Data: root, Managed: root}, Limits{})
	if err != nil || got.Complete || got.Bytes != 3 || len(got.Problems) != 1 ||
		got.Problems[0].Reason != "permission_denied" || got.Problems[0].Path != "private" {
		t.Fatalf("measurement=%+v, error=%v", got, err)
	}
}

func TestMeasureReportsDeadlineAndCancellation(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := Measure(ctx, Roots{Data: root, Managed: root}, Limits{})
	if err != status_error.OperationCanceled || got.Complete || len(got.Problems) != 1 || got.Problems[0].Reason != "canceled" {
		t.Fatalf("canceled measurement=%+v, error=%v", got, err)
	}

	got, err = Measure(t.Context(), Roots{Data: root, Managed: root}, Limits{Timeout: time.Nanosecond})
	if err != nil || got.Complete || len(got.Problems) != 1 || got.Problems[0].Reason != "timeout" {
		t.Fatalf("timed out measurement=%+v, error=%v", got, err)
	}
}

func TestDiskReportsRealVolumesAndGroupsRoots(t *testing.T) {
	data, managed := t.TempDir(), t.TempDir()
	got, err := Measure(t.Context(), Roots{Data: data, Managed: managed}, Limits{})
	if err != nil || !got.Complete || len(got.Disks) != 1 {
		t.Fatalf("measurement=%+v, error=%v", got, err)
	}
	disk := got.Disks[0]
	if disk.ID == "" || disk.TotalBytes == 0 || disk.AvailableBytes > disk.TotalBytes ||
		len(disk.Roots) != 2 || disk.Roots[0] != got.Roots.Data || disk.Roots[1] != got.Roots.Managed {
		t.Fatalf("invalid volume: %+v", disk)
	}
}

func TestMeasureDetectsDirectoryDisappearance(t *testing.T) {
	data := t.TempDir()
	relative := "scanned"
	observed := filepath.Join(data, relative)
	if err := os.Mkdir(observed, 0o700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(observed)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := os.OpenRoot(data)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if err := os.Remove(observed); err != nil {
		t.Fatal(err)
	}

	s := scanner{result: Measurement{Complete: true}}
	s.checkDirectory(&scanRoot{path: data}, relative, info, opened.Lstat, relative)
	if s.result.Complete || len(s.result.Problems) != 1 || s.result.Problems[0].Reason != "disappeared" {
		t.Fatalf("measurement=%+v", s.result)
	}
}

func TestMeasureDetectsDirectoryReplacement(t *testing.T) {
	data := t.TempDir()
	relative := "scanned"
	observed := filepath.Join(data, relative)
	if err := os.Mkdir(observed, 0o700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(observed)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := os.OpenRoot(data)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if err := os.Rename(observed, filepath.Join(data, "previous")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(observed, 0o700); err != nil {
		t.Fatal(err)
	}

	s := scanner{result: Measurement{Complete: true}}
	s.checkDirectory(&scanRoot{path: data}, relative, info, opened.Lstat, relative)
	if s.result.Complete || len(s.result.Problems) != 1 || s.result.Problems[0].Reason != "changed" {
		t.Fatalf("measurement=%+v", s.result)
	}
}

func TestMeasureClassifiesConfiguredDatabaseAndCompanions(t *testing.T) {
	data, managed := t.TempDir(), t.TempDir()
	db := writeMeasuredFile(t, data, "configured.sqlite", 1)
	writeMeasuredFile(t, data, "configured.sqlite-wal", 2)
	writeMeasuredFile(t, data, "user.sqlite", 4)

	got, err := Measure(t.Context(), Roots{Data: data, Managed: managed, Database: db}, Limits{})
	if err != nil || !got.Complete || got.Bytes != 7 {
		t.Fatalf("measurement=%+v, error=%v", got, err)
	}
	var appBytes int64
	for _, file := range got.Entries {
		if file.Category == "app_data" {
			appBytes += file.Bytes
		}
	}
	if appBytes != 3 {
		t.Fatalf("configured database app data bytes=%d, want 3", appBytes)
	}
}
