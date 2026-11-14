package catalog

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestCatalogStartupPublishesWithoutDownloadingDetails(t *testing.T) {
	f := newDistributionFixture(t, false)
	f.failDetails.Store(true)
	location := f.server.URL + "/catalog.json"
	changed := make(chan struct{}, 1)
	stop := Start(context.Background(), location, t.TempDir(), func() { changed <- struct{}{} })
	defer stop()
	awaitCatalogSignal(t, changed)
	entries, state := Snapshot(location)
	if len(entries) != 2 || state.State != "available" || f.detailRequests.Load() != 0 {
		t.Fatalf("lightweight catalog depended on details: entries=%d state=%+v requests=%d", len(entries), state, f.detailRequests.Load())
	}
}

func TestCatalogRefreshDoesNotWaitForArchiveDownloads(t *testing.T) {
	f := newDistributionFixture(t, true)
	location := f.server.URL + "/catalog.json"
	stop := Start(context.Background(), location, t.TempDir(), nil)
	defer stop()
	awaitCatalogSignal(t, f.entered)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := Refresh(ctx, location); err != nil {
		t.Fatalf("catalog refresh waited for a blocked archive: %v", err)
	}
	entries, state := Snapshot(location)
	if len(entries) != 2 || state.State != "available" {
		t.Fatalf("archive blocked the usable catalog: entries=%d state=%+v", len(entries), state)
	}
}

func TestColdCatalogDoesNotInventBundledEntries(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	source := constants.PluginCatalogHTTPSource
	remoteIndexes.Delete(source)
	t.Cleanup(func() { remoteIndexes.Delete(source) })
	entries, state := Snapshot(source)
	if len(entries) != 0 || state.State != "unavailable" {
		t.Fatalf("cold catalog invented plugin metadata: entries=%d state=%+v", len(entries), state)
	}
}

func TestReadDetailsCancelsTheRequestedDownload(t *testing.T) {
	entered := make(chan struct{})
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
	}))
	defer source.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entry := Entry{Key: "documented", DetailsURL: source.URL, DetailsSHA256: strings.Repeat("a", 64), DetailsSize: 10, cacheRoot: t.TempDir()}
	completed := make(chan error, 1)
	go func() { _, err := ReadDetails(ctx, entry); completed <- err }()
	awaitCatalogSignal(t, entered)
	cancel()
	select {
	case err := <-completed:
		if err == nil {
			t.Fatal("cancelled details download reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("details download ignored its request cancellation")
	}
}

func TestReadDetailsCancellationDoesNotPoisonImmediateRetry(t *testing.T) {
	for _, stage := range []string{"headers", "body"} {
		t.Run(stage, func(t *testing.T) {
			entered := make(chan struct{})
			var requests atomic.Int32
			f := &distributionFixture{seed: t.TempDir()}
			f.version.Store(1)
			f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, body, _ := f.artifacts("first")
				if requests.Add(1) == 1 {
					if stage == "body" {
						w.Header().Set("Content-Length", fmt.Sprint(len(body)))
						_, _ = w.Write(body[:1])
						w.(http.Flusher).Flush()
					}
					close(entered)
					<-r.Context().Done()
					return
				}
				_, _ = w.Write(body)
			}))
			defer f.server.Close()
			item, _, _ := f.artifacts("first")
			entries, err := parseIndex([]byte(`{"schemaVersion":1,"entries":[` + item + `]}`))
			if err != nil {
				t.Fatal(err)
			}
			entry := entries[0]
			entry.cacheRoot = t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			completed := make(chan error, 1)
			go func() { _, err := ReadDetails(ctx, entry); completed <- err }()
			awaitCatalogSignal(t, entered)
			cancel()
			if err := <-completed; err == nil {
				t.Fatal("cancelled details download reported success")
			}
			details, err := ReadDetails(context.Background(), entry)
			if err != nil || details.Readmes["en"] != "public documentation" {
				t.Fatalf("cancelled %s request poisoned the next valid download: %v", stage, err)
			}
		})
	}
}

func TestCurrentResolutionCancellationDoesNotPoisonRecovery(t *testing.T) {
	entered := make(chan struct{})
	var indexes atomic.Int32
	f := &distributionFixture{seed: t.TempDir()}
	f.version.Store(1)
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/catalog.json" {
			count := indexes.Add(1)
			if count == 2 {
				close(entered)
				<-r.Context().Done()
				return
			}
			if count > 2 {
				f.version.Store(2)
			}
			entry, _, _ := f.artifacts("first")
			fmt.Fprintf(w, `{"schemaVersion":1,"entries":[%s]}`, entry)
			return
		}
		if f.version.Load() == 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _, body := f.artifacts("first")
		_, _ = w.Write(body)
	}))
	defer f.server.Close()
	source := f.server.URL + "/catalog.json"
	if _, err := List(source); err != nil {
		t.Fatal(err)
	}
	before := State(source)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := make(chan error, 1)
	go func() { _, _, err := ResolveCurrent(ctx, source, "first"); completed <- err }()
	awaitCatalogSignal(t, entered)
	cancel()
	if err := <-completed; err == nil {
		t.Fatal("cancelled recovery reported success")
	}
	if after := State(source); after != before {
		t.Errorf("request cancellation changed the shared source state: before=%+v after=%+v", before, after)
	}
	entry, archive, err := ResolveCurrent(context.Background(), source, "first")
	if err != nil || entry.Package.Manifest.Version != "2" {
		t.Fatalf("cancelled recovery poisoned immediate refresh: version=%s error=%v", entry.Package.Manifest.Version, err)
	}
	_, _, want := f.artifacts("first")
	if body, err := os.ReadFile(archive); err != nil || string(body) != string(want) {
		t.Fatal("recovered archive does not contain the new fixed content", err)
	}
}

func TestCurrentResolutionCanCancelWhileStartupIndexWaits(t *testing.T) {
	startupEntered, archiveEntered := make(chan struct{}), make(chan struct{}, 1)
	releaseStartup := make(chan struct{})
	f := &distributionFixture{seed: t.TempDir()}
	f.version.Store(1)
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/catalog.json" {
			close(startupEntered)
			select {
			case <-releaseStartup:
			case <-r.Context().Done():
				return
			}
			entry, _, _ := f.artifacts("first")
			fmt.Fprintf(w, `{"schemaVersion":1,"entries":[%s]}`, entry)
			return
		}
		archiveEntered <- struct{}{}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer f.server.Close()
	source, directory := f.server.URL+"/catalog.json", t.TempDir()
	item, _, _ := f.artifacts("first")
	filename := cacheFilename(directory, source)
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(fmt.Sprintf(`{"content":{"schemaVersion":1,"entries":[%s]},"fetchedAt":1}`, item)), 0600); err != nil {
		t.Fatal(err)
	}
	stop := Start(context.Background(), source, directory, nil)
	defer stop()
	awaitCatalogSignal(t, startupEntered)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := make(chan error, 1)
	go func() { _, _, err := ResolveCurrent(ctx, source, "first"); completed <- err }()
	awaitCatalogSignal(t, archiveEntered)
	index := remoteSource(source)
	deadline := time.Now().Add(time.Second)
	for len(index.recoveryGate) == 0 {
		if time.Now().After(deadline) {
			close(releaseStartup)
			cancel()
			<-completed
			t.Fatal("resolution did not reach the startup recovery wait")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-completed:
		close(releaseStartup)
		if err == nil {
			t.Fatal("cancelled startup wait reported success")
		}
	case <-time.After(300 * time.Millisecond):
		close(releaseStartup)
		<-completed
		t.Fatal("cancelled recovery retained the gate while waiting for startup")
	}
	select {
	case index.recoveryGate <- struct{}{}:
		<-index.recoveryGate
	default:
		t.Fatal("cancelled recovery retained its source gate")
	}
}

func TestDistributionCachePrunesOldAssetsAndKeepsLeasedFiles(t *testing.T) {
	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archives")
	if err := os.MkdirAll(archiveRoot, 0700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	write := func(digit string) string {
		file := filepath.Join(archiveRoot, strings.Repeat(digit, 64)+".zip")
		if err := os.WriteFile(file, []byte("archive"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, now.Add(-8*24*time.Hour), now.Add(-8*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
		return file
	}
	current, leased, old := write("a"), write("b"), write("c")
	release := artifactStates.pin(leased)
	defer release()
	if err := pruneArtifacts(root, []Entry{{ArchiveSHA256: strings.Repeat("a", 64)}}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("expired archive remained: %v", err)
	}
	for _, file := range []string{current, leased} {
		if _, err := os.Stat(file); err != nil {
			t.Fatalf("protected archive was removed: %v", err)
		}
	}
}

func TestWithCurrentArchiveRejectsUnconfirmedDigestWithoutDownload(t *testing.T) {
	f := newDistributionFixture(t, false)
	source := f.server.URL + "/catalog.json"
	ctx := t.Context()
	if err := Refresh(ctx, source); err != nil {
		t.Fatal(err)
	}
	err := WithCurrentArchive(ctx, source, "first", strings.Repeat("f", 64), func(Entry, string) error {
		t.Error("an unconfirmed archive was passed to the caller")
		return nil
	})
	if err != status_error.PluginVersionConflict || f.archiveRequests.Load() != 0 {
		t.Fatalf("unconfirmed archive contacted distribution: requests=%d err=%v", f.archiveRequests.Load(), err)
	}
}
