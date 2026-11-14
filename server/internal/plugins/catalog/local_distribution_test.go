package catalog

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartupPrefetchesPackagesAndInstallationReusesPersistentCache(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	var archives, details atomic.Int32
	body := []byte("complete package " + t.TempDir())
	hash := fmt.Sprintf("%x", sha256.Sum256(body))
	public := []byte(fmt.Sprintf(`{"schemaVersion":1,"sourceCommit":"%s","archiveSHA256":"%s","packagePath":"entry","package":{"contractVersion":"neuxbot-plugin/1","manifest":{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"current","version":"1"},"format":"agent-plugins","skills":[],"servers":[],"diagnostics":[]},"readmes":{"en":"public documentation"},"skillContents":{}}`, strings.Repeat("1", 40), hash))
	var source *httptest.Server
	source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/catalog.json":
			fmt.Fprintf(w, `{"schemaVersion":1,"entries":[{"key":"current","name":"current","version":"1","format":"agent-plugins","sourceCommit":"%s","archiveURL":%q,"archiveSHA256":%q,"packagePath":"entry","detailsURL":%q,"detailsSHA256":"%x","detailsSize":%d}]}`, strings.Repeat("1", 40), source.URL+"/package.zip", hash, source.URL+"/details.json", sha256.Sum256(public), len(public))
		case "/details.json":
			details.Add(1)
			w.Write(public)
		case "/package.zip":
			archives.Add(1)
			w.Write(body)
		}
	}))
	root := t.TempDir()
	done := make(chan struct{}, 2)
	location := source.URL + "/catalog.json"
	stop := Start(context.Background(), location, root, func() { done <- struct{}{} })
	defer stop()
	awaitCatalogSignal(t, done)
	awaitArchivePrefetch(t, location)
	if details.Load() != 0 || archives.Load() != 1 {
		t.Fatalf("startup did not keep details lazy: details=%d archives=%d", details.Load(), archives.Load())
	}
	entry, _, err := ResolveDocumentation(context.Background(), location, "current")
	if err != nil {
		t.Fatal(err)
	}
	source.Close()
	archive, err := ResolveEntry(context.Background(), entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(archive, filepath.Join(root, "archives")) {
		t.Fatalf("archive is not in persistent Plugins root: %s", archive)
	}
	if raw, err := os.ReadFile(archive); err != nil || string(raw) != string(body) {
		t.Fatalf("cached archive: %v", err)
	}
	if _, err := ReadDetails(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	if details.Load() != 1 || archives.Load() != 1 {
		t.Fatal("installation refetched preloaded assets")
	}
}

func TestArtifactLockWaitCanBeCancelled(t *testing.T) {
	cache := artifactCache{states: make(map[string]*artifactLoadState)}
	_, release, err := cache.acquire(t.Context(), "busy")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	completed := make(chan error, 1)
	go func() {
		_, _, err := cache.acquire(ctx, "busy")
		completed <- err
	}()
	select {
	case err := <-completed:
		if err == nil {
			t.Fatal("cancelled wait acquired lock")
		}
	case <-time.After(time.Second):
		t.Fatal("lock wait ignored cancellation")
	}
}

// distributionFixture 提供真实目录、公开资料与两个不同归档，故障开关只影响外部 HTTP 边界。
type distributionFixture struct {
	// keys 是本次来源声明的完整插件集合。
	keys []string
	// server 提供受测试控制的本机分发源。
	server *httptest.Server
	// seed 避免各用例共享摘要缓存。
	seed string
	// version 控制新目录与旧目录的不同固定内容。
	version atomic.Int32
	// archiveRequests 统计全部完整包下载。
	archiveRequests atomic.Int32
	// detailRequests 统计全部公开资料下载。
	detailRequests atomic.Int32
	// failArchives 模拟后台下载暂时失败。
	failArchives atomic.Bool
	// failDetails 模拟新公开资料暂时不可用。
	failDetails atomic.Bool
	// entered 通知归档请求已到达。
	entered chan struct{}
	// gate 在需要验证取消和并发时阻塞归档响应。
	gate chan struct{}
}

func newDistributionFixture(t *testing.T, block bool) *distributionFixture {
	t.Helper()
	f := &distributionFixture{keys: []string{"first", "second"}, seed: t.TempDir(), entered: make(chan struct{}, 16)}
	f.version.Store(1)
	if block {
		f.gate = make(chan struct{})
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/catalog.json" {
			var items []string
			for _, key := range f.keys {
				entry, _, _ := f.artifacts(key)
				items = append(items, entry)
			}
			fmt.Fprintf(w, `{"schemaVersion":1,"entries":[%s]}`, strings.Join(items, ","))
			return
		}
		key := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".details.json")
		if strings.HasSuffix(r.URL.Path, ".details.json") {
			f.detailRequests.Add(1)
			if f.failDetails.Load() {
				w.WriteHeader(503)
				return
			}
			_, body, _ := f.artifacts(key)
			w.Write(body)
			return
		}
		key = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".zip")
		f.archiveRequests.Add(1)
		if f.failArchives.Load() {
			w.WriteHeader(503)
			return
		}
		f.entered <- struct{}{}
		if f.gate != nil {
			select {
			case <-f.gate:
			case <-r.Context().Done():
				return
			}
		}
		_, _, body := f.artifacts(key)
		w.Write(body)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *distributionFixture) artifacts(key string) (string, []byte, []byte) {
	version := fmt.Sprint(f.version.Load())
	archive := []byte(f.seed + key + version)
	archiveHash := fmt.Sprintf("%x", sha256.Sum256(archive))
	public := []byte(fmt.Sprintf(`{"schemaVersion":1,"sourceCommit":"%s","archiveSHA256":%q,"packagePath":"entry","package":{"contractVersion":"neuxbot-plugin/1","manifest":{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":%q,"version":%q},"format":"agent-plugins","skills":[],"servers":[],"diagnostics":[]},"readmes":{"en":"public documentation"},"skillContents":{}}`, strings.Repeat("1", 40), archiveHash, key, version))
	entry := fmt.Sprintf(`{"key":%q,"name":%q,"version":%q,"format":"agent-plugins","sourceCommit":"%s","archiveURL":%q,"archiveSHA256":%q,"packagePath":"entry","detailsURL":%q,"detailsSHA256":"%x","detailsSize":%d}`, key, key, version, strings.Repeat("1", 40), f.server.URL+"/"+key+".zip", archiveHash, f.server.URL+"/"+key+".details.json", sha256.Sum256(public), len(public))
	return entry, public, archive
}

func TestAllPackagesPrefetchedAndPersistentAcrossRestart(t *testing.T) {
	f := newDistributionFixture(t, false)
	root := t.TempDir()
	location := f.server.URL + "/catalog.json"
	done := make(chan struct{}, 2)
	stop := Start(context.Background(), location, root, func() { done <- struct{}{} })
	awaitCatalogSignal(t, done)
	awaitArchivePrefetch(t, location)
	stop()
	if f.archiveRequests.Load() != 2 || f.detailRequests.Load() != 0 {
		t.Fatalf("not all plugins prefetched: archives=%d details=%d", f.archiveRequests.Load(), f.detailRequests.Load())
	}
	f.server.Close()
	stop = Start(context.Background(), location, root, func() { done <- struct{}{} })
	defer stop()
	awaitCatalogSignal(t, done)
	for _, key := range []string{"first", "second"} {
		entry, err := Find(location, key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveEntry(context.Background(), entry); err != nil {
			t.Fatal(err)
		}
	}
	if f.archiveRequests.Load() != 2 || f.detailRequests.Load() != 0 {
		t.Fatal("restart did not reuse persistent content")
	}
}

func TestFailedBackgroundPackageAllowsInstallationNetworkFallback(t *testing.T) {
	f := newDistributionFixture(t, false)
	f.failArchives.Store(true)
	location := f.server.URL + "/catalog.json"
	done := make(chan struct{}, 1)
	stop := Start(context.Background(), location, t.TempDir(), func() { done <- struct{}{} })
	defer stop()
	awaitCatalogSignal(t, done)
	awaitArchivePrefetch(t, location)
	if State(location).State != "available" {
		t.Fatal("failed archive prefetch invalidated the lightweight catalog")
	}
	f.failArchives.Store(false)
	entry, err := Find(location, "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveEntry(context.Background(), entry); err != nil {
		t.Fatalf("installation did not retry failed prefetch: %v", err)
	}
	if f.archiveRequests.Load() != 3 {
		t.Fatalf("unexpected fallback request count: %d", f.archiveRequests.Load())
	}
}

func TestNewCatalogPublishesBeforeItsDetailsAreAvailable(t *testing.T) {
	f := newDistributionFixture(t, false)
	location := f.server.URL + "/catalog.json"
	done := make(chan struct{}, 2)
	stop := Start(context.Background(), location, t.TempDir(), func() { done <- struct{}{} })
	defer stop()
	awaitCatalogSignal(t, done)
	awaitArchivePrefetch(t, location)
	f.version.Store(2)
	f.failDetails.Store(true)
	if err := Refresh(context.Background(), location); err != nil {
		t.Fatalf("detail availability blocked catalog update: %v", err)
	}
	entry, err := Find(location, "first")
	if err != nil || entry.Package.Manifest.Version != "2" || f.detailRequests.Load() != 0 {
		t.Fatalf("new lightweight catalog was not published: %v %+v", err, entry)
	}
	if _, err := ReadDetails(context.Background(), entry); err == nil {
		t.Fatal("unavailable detail was reported as readable")
	}
	if State(location).State != "available" {
		t.Fatal("detail failure invalidated a usable catalog")
	}
}

func TestCloseCancelsBackgroundPackagesAndDoesNotPublishHalfArchive(t *testing.T) {
	f := newDistributionFixture(t, true)
	root := t.TempDir()
	location := f.server.URL + "/catalog.json"
	stop := Start(context.Background(), location, root, nil)
	awaitCatalogSignal(t, f.entered)
	closed := make(chan struct{})
	go func() { stop(); close(closed) }()
	awaitCatalogSignal(t, closed)
	files, err := filepath.Glob(filepath.Join(root, "archives", "*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("cancelled prefetch left partial archive: %v %v", files, err)
	}
}

func TestInstallationSharesInflightBackgroundDownload(t *testing.T) {
	f := newDistributionFixture(t, true)
	location := f.server.URL + "/catalog.json"
	done := make(chan struct{}, 1)
	stop := Start(context.Background(), location, t.TempDir(), func() { done <- struct{}{} })
	defer stop()
	awaitCatalogSignal(t, f.entered)
	awaitCatalogSignal(t, f.entered)
	entry, err := Find(location, "first")
	if err != nil {
		t.Fatal(err)
	}
	installed := make(chan error, 1)
	go func() { _, err := ResolveEntry(context.Background(), entry); installed <- err }()
	close(f.gate)
	awaitCatalogSignal(t, done)
	if err := <-installed; err != nil {
		t.Fatal(err)
	}
	if f.archiveRequests.Load() != 2 {
		t.Fatalf("installation duplicated background download: %d", f.archiveRequests.Load())
	}
}

func TestInstallationAndPrefetchShareTwoTransferSlots(t *testing.T) {
	f := newDistributionFixture(t, true)
	f.keys = []string{"first", "second", "third"}
	location := f.server.URL + "/catalog.json"
	done := make(chan struct{}, 1)
	stop := Start(context.Background(), location, t.TempDir(), func() { done <- struct{}{} })
	defer stop()
	defer func() {
		select {
		case <-f.gate:
		default:
			close(f.gate)
		}
	}()
	awaitCatalogSignal(t, f.entered)
	awaitCatalogSignal(t, f.entered)
	entry, err := Find(location, "third")
	if err != nil {
		t.Fatal(err)
	}
	installed := make(chan error, 1)
	go func() { _, err := ResolveEntry(context.Background(), entry); installed <- err }()
	select {
	case <-f.entered:
		t.Fatal("installation exceeded the shared two-transfer limit")
	case <-time.After(100 * time.Millisecond):
	}
	close(f.gate)
	awaitCatalogSignal(t, done)
	if err := <-installed; err != nil {
		t.Fatal(err)
	}
	if f.archiveRequests.Load() != 3 {
		t.Fatalf("cache failed to share third package: %d", f.archiveRequests.Load())
	}
}

func TestExplicitBackgroundRefreshSharesStartupAndCoreCancellation(t *testing.T) {
	f := newDistributionFixture(t, true)
	location := f.server.URL + "/catalog.json"
	var changes atomic.Int32
	stop := Start(context.Background(), location, t.TempDir(), func() { changes.Add(1) })
	defer stop()
	awaitCatalogSignal(t, f.entered)
	awaitCatalogSignal(t, f.entered)
	for range 20 {
		if err := RefreshInBackground(location); err != nil {
			t.Fatal(err)
		}
	}
	if f.archiveRequests.Load() != 2 {
		t.Fatal("explicit refresh duplicated startup downloads")
	}
	closed := make(chan struct{})
	go func() { stop(); close(closed) }()
	awaitCatalogSignal(t, closed)
	if changes.Load() == 0 {
		t.Fatal("lightweight catalog was never published before archive cancellation")
	}
	if err := RefreshInBackground(location); err == nil {
		t.Fatal("closed owner accepted new work")
	}
}

func TestExplicitBackgroundRefreshPublishesIndexAndQueuesArchives(t *testing.T) {
	f := newDistributionFixture(t, false)
	location := f.server.URL + "/catalog.json"
	done := make(chan struct{}, 3)
	stop := Start(context.Background(), location, t.TempDir(), func() { done <- struct{}{} })
	defer stop()
	awaitCatalogSignal(t, done)
	awaitArchivePrefetch(t, location)
	f.version.Store(2)
	if err := RefreshInBackground(location); err != nil {
		t.Fatal(err)
	}
	awaitCatalogSignal(t, done)
	awaitArchivePrefetch(t, location)
	if f.archiveRequests.Load() != 4 || f.detailRequests.Load() != 0 {
		t.Fatal("explicit refresh omitted full package preparation")
	}
	f.failDetails.Store(true)
	f.version.Store(3)
	if err := RefreshInBackground(location); err != nil {
		t.Fatal(err)
	}
	awaitCatalogSignal(t, done)
	awaitArchivePrefetch(t, location)
	entries, state := Snapshot(location)
	if len(entries) != 2 || entries[0].Package.Manifest.Version != "3" || state.State != "available" || state.ErrorCode != "" {
		t.Fatal("detail failure prevented the new lightweight catalog", entries, state)
	}
	select {
	case <-done:
		t.Fatal("one failed synchronization emitted multiple changes")
	default:
	}
}

func TestColdStartConcurrentReadsWaitForOneBackgroundSnapshot(t *testing.T) {
	entered, gate := make(chan struct{}), make(chan struct{})
	var requests atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-gate:
			fmt.Fprint(w, `{"schemaVersion":1,"entries":[]}`)
		case <-r.Context().Done():
		}
	}))
	defer source.Close()
	location := source.URL + "/catalog.json"
	stop := Start(context.Background(), location, t.TempDir(), nil)
	defer stop()
	awaitCatalogSignal(t, entered)
	snapshot := make(chan struct{})
	go func() { _, _ = Snapshot(location); close(snapshot) }()
	awaitCatalogSignal(t, snapshot)
	read := make(chan error, 10)
	for range 10 {
		go func() { _, err := List(location); read <- err }()
	}
	select {
	case err := <-read:
		t.Fatal("cold catalog read returned before the startup snapshot", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(gate)
	for range 10 {
		select {
		case err := <-read:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("cold catalog read did not receive the shared snapshot")
		}
	}
	if requests.Load() != 1 {
		t.Fatal("cold readers duplicated startup synchronization")
	}
}

func TestBackgroundRefreshRequiresACoreOwnedCatalog(t *testing.T) {
	source := "https://example.invalid/" + filepath.Base(t.TempDir()) + "/catalog.json"
	remoteSource(source)
	if err := RefreshInBackground(source); err == nil {
		t.Fatal("temporary read index accepted unowned background downloads")
	}
}

// awaitArchivePrefetch 只在需要验证归档结果的用例等待独立后台边界。
func awaitArchivePrefetch(t *testing.T, source string) {
	t.Helper()
	index := remoteSource(source)
	index.mu.RLock()
	flight := index.archiveFlight
	index.mu.RUnlock()
	if flight != nil {
		awaitCatalogSignal(t, flight.done)
	}
}
