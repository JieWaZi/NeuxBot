package catalog

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestClearCachedArtifactsPreservesLeaseAndLightIndex(t *testing.T) {
	f := newDistributionFixture(t, false)
	root := t.TempDir()
	source := f.server.URL + "/catalog.json"
	stop := Start(t.Context(), source, root, nil)
	defer stop()
	awaitCatalogSignal(t, f.entered)
	awaitArchivePrefetch(t, source)

	entry, first, release, err := AcquireCurrent(t.Context(), source, "first")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := ReadDetails(t.Context(), entry); err != nil {
		t.Fatal(err)
	}
	second, err := Resolve(t.Context(), source, "second")
	if err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(root, "archives", "unowned.zip")
	if err := os.WriteFile(unknown, []byte("unowned"), 0o600); err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewCachedArtifacts(t.Context(), source)
	if err != nil || preview.Files != 2 || preview.Skipped != 1 || preview.Bytes == 0 {
		t.Fatalf("preview=%+v error=%v", preview, err)
	}

	result, err := ClearCachedArtifacts(t.Context(), source)
	if err != nil || result != preview {
		t.Fatalf("clear=%+v preview=%+v error=%v", result, preview, err)
	}
	for _, path := range []string{first, unknown, indexCache(source)} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("protected file %s: %v", path, err)
		}
	}
	for _, path := range []string{second, filepath.Join(root, "details", entry.DetailsSHA256+".json")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("cached artifact %s remains: %v", path, err)
		}
	}
	if entries, _ := Snapshot(source); len(entries) != 2 {
		t.Fatalf("light index unavailable: %v", entries)
	}
	if f.archiveRequests.Load() != 2 {
		t.Fatalf("manual clear refilled archives: %d", f.archiveRequests.Load())
	}

	release()
	release()
	result, err = ClearCachedArtifacts(t.Context(), source)
	if err != nil || result.Files != 1 || result.Skipped != 0 {
		t.Fatalf("released archive remained protected: %+v %v", result, err)
	}
	if err := Refresh(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	awaitArchivePrefetch(t, source)
	if f.archiveRequests.Load() != 4 {
		t.Fatalf("explicit refresh did not refill archives: %d", f.archiveRequests.Load())
	}
}

func TestClearCachedArtifactsCancelsPrefetch(t *testing.T) {
	f := newDistributionFixture(t, true)
	root := t.TempDir()
	source := f.server.URL + "/catalog.json"
	stop := Start(t.Context(), source, root, nil)
	defer stop()
	awaitCatalogSignal(t, f.entered)
	awaitCatalogSignal(t, f.entered)

	finished := make(chan error, 1)
	go func() {
		_, err := ClearCachedArtifacts(t.Context(), source)
		finished <- err
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("clear did not cancel active background downloads")
	}
	awaitArchivePrefetch(t, source)
	files, err := os.ReadDir(filepath.Join(root, "archives"))
	if err != nil || len(files) != 0 || f.archiveRequests.Load() != 2 {
		t.Fatalf("canceled prefetch published files=%v requests=%d error=%v", files, f.archiveRequests.Load(), err)
	}
}

func TestPruneUnreferencedArtifactsPreservesCurrentDownloads(t *testing.T) {
	f := newDistributionFixture(t, false)
	root := t.TempDir()
	source := f.server.URL + "/catalog.json"
	stop := Start(t.Context(), source, root, nil)
	defer stop()
	awaitCatalogSignal(t, f.entered)
	awaitArchivePrefetch(t, source)
	_, oldPath, release, err := AcquireCurrent(t.Context(), source, "first")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	f.version.Store(2)
	if err := Refresh(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	awaitArchivePrefetch(t, source)
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("refresh pruned a leased historical archive: %v", err)
	}
	release()
	newPath, err := Resolve(t.Context(), source, "first")
	if err != nil {
		t.Fatal(err)
	}
	if err := PruneUnreferencedArtifacts(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("historical archive remains: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("current archive removed: %v", err)
	}
}

func TestClearCachedArtifactsRejectsLinkedFoldersAndSpoofFiles(t *testing.T) {
	f := newDistributionFixture(t, false)
	root := t.TempDir()
	source := f.server.URL + "/catalog.json"
	stop := Start(t.Context(), source, root, nil)
	defer stop()
	awaitCatalogSignal(t, f.entered)
	awaitArchivePrefetch(t, source)

	_, _, archive := f.artifacts("first")
	digest := fmt.Sprintf("%x", sha256.Sum256(archive))
	spoof := filepath.Join(root, "archives", digest+".zip")
	if err := os.WriteFile(spoof, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ClearCachedArtifacts(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(spoof); err != nil {
		t.Fatalf("unverified replacement removed: %v", err)
	}

	external := filepath.Join(t.TempDir(), digest+".json")
	if err := os.WriteFile(external, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(external), filepath.Join(root, "details")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := ClearCachedArtifacts(t.Context(), source); err != status_error.StorageRootInvalid {
		t.Fatalf("linked cache directory error=%v", err)
	}
	if _, err := os.Stat(external); err != nil {
		t.Fatalf("outside link followed: %v", err)
	}
}
