package catalog

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPQueriesReturnCachedIndexWhileNetworkWaits(t *testing.T) {
	gate := make(chan struct{})
	defer close(gate)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-gate
		fmt.Fprint(w, `{"schemaVersion":2,"entries":[]}`)
	}))
	// 先释放请求再等待 HTTP 服务退出，失败路径同样有界回收。
	t.Cleanup(source.Close)
	location := source.URL + "/catalog.json"
	cache := indexCache(location)
	if err := os.MkdirAll(filepath.Dir(cache), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, []byte(`{"schemaVersion":2,"entries":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(cache) })
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(cache, old, old); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := List(location); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cached catalog query waited for network instead of returning the previous snapshot")
	}
}

func TestHTTPQueriesDoNotRefreshAfterFiveMinutes(t *testing.T) {
	var requests atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"schemaVersion":2,"entries":[]}`)
	}))
	defer source.Close()
	location := source.URL + "/catalog.json"
	if _, err := List(location); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(indexCache(location), old, old); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if _, err := List(location); err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("page/search queries refetched remote index %d times", requests.Load())
	}
}

func TestHTTPMetadataAndDigestDownload(t *testing.T) {
	archive := []byte("fixed ZIP placeholder")
	sum := sha256.Sum256(archive)
	downloads := 0
	var source *httptest.Server
	source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/catalog.json" {
			fmt.Fprintf(w, `{"schemaVersion":2,"entries":[{"key":"github","sourceCommit":"1111111111111111111111111111111111111111","archiveURL":%q,"archiveSHA256":"%x","packagePath":"neuxally-plugin/github","name":"github","version":"2","description":"GitHub tools","format":"codex"}]}`, source.URL+"/github.zip", sum)
			return
		}
		downloads++
		w.Write(archive)
	}))
	defer source.Close()
	entries, err := List(source.URL + "/catalog.json")
	if err != nil || len(entries) != 1 || entries[0].Package.Manifest.Version != "2" || downloads != 0 {
		t.Fatalf("metadata fetch downloaded package: %+v %v", entries, err)
	}
	location, err := Resolve(source.URL+"/catalog.json", "github")
	if err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(location)
	if string(content) != string(archive) {
		t.Fatal("wrong download")
	}
	archive = []byte("tampered")
	os.Remove(location)
	if _, err := Resolve(source.URL+"/catalog.json", "github"); err == nil {
		t.Fatal("accepted digest mismatch")
	}
}

func TestHTTPDirectoryFailurePreservesStaleMetadata(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"schemaVersion":2,"entries":[]}`) }))
	location := source.URL + "/catalog.json"
	if entries, err := List(location); err != nil || len(entries) != 0 {
		t.Fatalf("initial catalog: %v", err)
	}
	source.Close()
	updated := make(chan struct{}, 1)
	stop := Start(context.Background(), location, filepath.Dir(indexCache(location)), func() { updated <- struct{}{} })
	<-updated
	stop()
	if entries, err := List(location); err != nil || len(entries) != 0 || State(location).State != "stale" {
		t.Fatalf("stale cache disappeared: %v %+v", err, State(location))
	}
	fresh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer fresh.Close()
	if _, err := List(fresh.URL + "/catalog.json"); err == nil {
		t.Fatal("first sync failure disguised as empty catalog")
	}
}

func TestDifferentCatalogSourcesDoNotMergeSameName(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"schemaVersion":2,"entries":[{"key":"same-name","sourceCommit":"1111111111111111111111111111111111111111","archiveURL":"https://example.invalid/fixed.zip","archiveSHA256":"1111111111111111111111111111111111111111111111111111111111111111","packagePath":"neuxally-plugin/same-name","name":"same-name","version":"1","format":"codex"}]}`)
	})
	first := httptest.NewServer(handler)
	defer first.Close()
	second := httptest.NewServer(handler)
	defer second.Close()
	one, err := List(first.URL + "/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	two, err := List(second.URL + "/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	if one[0].SourceKey == two[0].SourceKey {
		t.Fatal("merged unrelated sources by display name")
	}
}

func startupCatalog(version string) string {
	return fmt.Sprintf(`{"schemaVersion":2,"entries":[{"key":"same-name","sourceCommit":"1111111111111111111111111111111111111111","archiveURL":"https://example.invalid/fixed.zip","archiveSHA256":"1111111111111111111111111111111111111111111111111111111111111111","packagePath":"neuxally-plugin/same-name","name":"same-name","version":%q,"format":"codex"}]}`, version)
}

func awaitCatalogSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("catalog startup operation did not finish")
	}
}

func TestStartupCachePersistsAndRevalidatesOnce(t *testing.T) {
	var requests atomic.Int32
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	validators := make(chan string, 3)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requests.Add(1)
		validators <- r.Header.Get("If-None-Match") + "|" + r.Header.Get("If-Modified-Since")
		if count == 2 {
			entered <- struct{}{}
			<-gate
		}
		w.Header().Set("ETag", `"v2"`)
		w.Header().Set("Last-Modified", "Mon, 19 Oct 2026 01:00:00 GMT")
		if count == 3 {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if count == 1 {
			w.Header().Set("ETag", `"v1"`)
			fmt.Fprint(w, startupCatalog("0.1.0"))
			return
		}
		fmt.Fprint(w, startupCatalog("0.1.1"))
	}))
	defer source.Close()
	directory := t.TempDir()
	location := source.URL + "/catalog.json"
	changed := make(chan struct{}, 3)
	stop := Start(context.Background(), location, directory, func() { changed <- struct{}{} })
	awaitCatalogSignal(t, changed)
	stop()
	if filepath.Dir(indexCache(location)) != directory {
		t.Fatal("catalog was not stored in application data directory")
	}
	if _, err := os.Stat(indexCache(location)); err != nil {
		t.Fatal(err)
	}

	// 重启后已知内容立即可读；更新等待期间的查询和搜索不会再次拉取远端。
	stop = Start(context.Background(), location, directory, func() { changed <- struct{}{} })
	awaitCatalogSignal(t, entered)
	done := make(chan error, 1)
	go func() {
		entries, err := List(location)
		if err == nil && entries[0].Package.Manifest.Version != "0.1.0" {
			err = fmt.Errorf("previous version lost")
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("persisted catalog waited for startup refresh")
	}
	for range 10 {
		if _, err := List(location); err != nil {
			t.Error(err)
		}
	}
	close(gate)
	awaitCatalogSignal(t, changed)
	stop()
	entries, err := List(location)
	if err != nil || entries[0].Package.Manifest.Version != "0.1.1" {
		t.Fatalf("new snapshot not published: %+v %v", entries, err)
	}

	stop = Start(context.Background(), location, directory, func() { changed <- struct{}{} })
	awaitCatalogSignal(t, changed)
	stop()
	entries, err = List(location)
	if err != nil || entries[0].Package.Manifest.Version != "0.1.1" || requests.Load() != 3 {
		t.Fatalf("304 lost content or duplicate startup fetch: %v requests=%d", err, requests.Load())
	}
	if <-validators != "|" || <-validators != `"v1"|Mon, 19 Oct 2026 01:00:00 GMT` || <-validators != `"v2"|Mon, 19 Oct 2026 01:00:00 GMT` {
		t.Fatal("conditional request validators were not restored")
	}
	if State(location).State != "available" {
		t.Fatal("304 failed to mark snapshot checked")
	}
}

func TestColdStartupConcurrentQueriesShareOneRequest(t *testing.T) {
	var requests atomic.Int32
	entered := make(chan struct{}, 1)
	gate := make(chan struct{})
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		entered <- struct{}{}
		<-gate
		fmt.Fprint(w, startupCatalog("0.1.0"))
	}))
	defer source.Close()
	location := source.URL + "/catalog.json"
	stop := Start(context.Background(), location, t.TempDir(), nil)
	awaitCatalogSignal(t, entered)
	var clients sync.WaitGroup
	failures := make(chan error, 20)
	for range 20 {
		clients.Add(1)
		go func() {
			defer clients.Done()
			entries, err := List(location)
			if err == nil && len(entries) != 1 {
				err = fmt.Errorf("cold catalog returned a fake empty result")
			}
			failures <- err
		}()
	}
	close(gate)
	clients.Wait()
	stop()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Error(err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("concurrent cold queries made %d requests", requests.Load())
	}
}

func TestStartupRefreshFailureNeverReplacesGoodSnapshot(t *testing.T) {
	var mode atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode.Load() {
		case 1:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 2:
			fmt.Fprint(w, `{"schemaVersion":2,"entries":[`)
		default:
			fmt.Fprint(w, startupCatalog("0.1.0"))
		}
	}))
	defer source.Close()
	location := source.URL + "/catalog.json"
	directory := t.TempDir()
	changed := make(chan struct{}, 1)
	stop := Start(context.Background(), location, directory, func() { changed <- struct{}{} })
	awaitCatalogSignal(t, changed)
	stop()
	before, err := os.ReadFile(indexCache(location))
	if err != nil {
		t.Fatal(err)
	}
	for _, failureMode := range []int32{1, 2} {
		mode.Store(failureMode)
		stop = Start(context.Background(), location, directory, func() { changed <- struct{}{} })
		awaitCatalogSignal(t, changed)
		stop()
		entries, err := List(location)
		if err != nil || len(entries) != 1 || entries[0].Package.Manifest.Version != "0.1.0" || State(location).State != "stale" {
			t.Fatalf("failure lost known data: %v %+v", err, State(location))
		}
		after, err := os.ReadFile(indexCache(location))
		if err != nil || string(before) != string(after) {
			t.Fatal("failed refresh overwrote persistent cache")
		}
	}
}

func TestStartupCloseCancelsRefreshAndDoesNotRetryOnQuery(t *testing.T) {
	var requests atomic.Int32
	entered := make(chan struct{}, 1)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		entered <- struct{}{}
		<-r.Context().Done()
	}))
	defer source.Close()
	location := source.URL + "/catalog.json"
	stop := Start(context.Background(), location, t.TempDir(), nil)
	awaitCatalogSignal(t, entered)
	closed := make(chan struct{})
	go func() { stop(); close(closed) }()
	awaitCatalogSignal(t, closed)
	if _, err := List(location); err == nil {
		t.Fatal("cancelled first sync presented a successful empty catalog")
	}
	if requests.Load() != 1 {
		t.Fatal("query retried a stopped startup operation")
	}
}

func TestArchiveNetworkFailureDoesNotRequirePluginAuthorization(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer source.Close()
	_, err := ResolveEntry("", Entry{ArchiveURL: source.URL + "/gone.zip", ArchiveSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(t.TempDir())))})
	if !errors.Is(err, status_error.PluginDistributionUnavailable) {
		t.Fatalf("archive HTTP failure was reported as plugin readiness: %v", err)
	}
}

func TestCurrentResolutionCoalescesRecoveryAndDoesNotRetryUnchangedArchive(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			archives := [][]byte{[]byte("old " + t.TempDir()), []byte("new " + t.TempDir())}
			var mode, indexRequests, oldRequests, newRequests atomic.Int32
			var source *httptest.Server
			source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/catalog.json" {
					indexRequests.Add(1)
					selected := 0
					if changed && mode.Load() == 1 {
						selected = 1
					}
					sum := sha256.Sum256(archives[selected])
					_, _ = fmt.Fprintf(w, `{"schemaVersion":2,"entries":[{"key":"current","name":"current","version":"%d","format":"codex","sourceCommit":"1111111111111111111111111111111111111111","archiveURL":%q,"archiveSHA256":"%x","packagePath":"entry"}]}`, selected+1, source.URL+[]string{"/old.zip", "/new.zip"}[selected], sum)
					return
				}
				if r.URL.Path == "/old.zip" {
					oldRequests.Add(1)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				newRequests.Add(1)
				_, _ = w.Write(archives[1])
			}))
			defer source.Close()
			location := source.URL + "/catalog.json"
			stop := Start(context.Background(), location, t.TempDir(), nil)
			defer stop()
			if _, err := Find(location, "current"); err != nil {
				t.Fatal(err)
			}
			mode.Store(1)
			failures := make(chan error, 12)
			var clients sync.WaitGroup
			for range 12 {
				clients.Add(1)
				go func() {
					defer clients.Done()
					entry, archive, err := ResolveCurrent(location, "current")
					if changed {
						if err == nil {
							content, readErr := os.ReadFile(archive)
							err = readErr
							if err == nil && (entry.Package.Manifest.Version != "2" || string(content) != string(archives[1])) {
								err = status_error.PluginContentInvalid
							}
						}
					} else if errors.Is(err, status_error.PluginDistributionUnavailable) {
						err = nil
					} else if err == nil {
						err = status_error.PluginContentInvalid
					}
					failures <- err
				}()
			}
			clients.Wait()
			close(failures)
			for err := range failures {
				if err != nil {
					t.Error(err)
				}
			}
			if indexRequests.Load() != 2 {
				t.Fatalf("concurrent recovery made %d index requests", indexRequests.Load())
			}
			if oldRequests.Load() != 1 {
				t.Fatalf("same failed archive was requested %d times during one recovery", oldRequests.Load())
			}
			if changed && newRequests.Load() != 1 {
				t.Fatalf("replacement archive was downloaded %d times", newRequests.Load())
			}
			if _, err := List(location); err != nil {
				t.Fatal(err)
			}
			if indexRequests.Load() != 2 {
				t.Fatal("normal query retried distribution recovery")
			}
		})
	}
}

func TestSlowArchiveDoesNotBlockCurrentDocumentation(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprint(cached), func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			archive := []byte("slow ZIP " + t.TempDir())
			archiveHash := fmt.Sprintf("%x", sha256.Sum256(archive))
			commit := "1111111111111111111111111111111111111111"
			body := []byte(fmt.Sprintf(`{"schemaVersion":1,"sourceCommit":%q,"archiveSHA256":%q,"packagePath":"entry","package":{"manifest":{"name":"current","version":"1"},"format":"codex","skills":[],"servers":[],"diagnostics":[]},"readmes":{"en":"public documentation"},"skillContents":{}}`, commit, archiveHash))
			gate, entered := make(chan struct{}), make(chan struct{})
			var once sync.Once
			var source *httptest.Server
			source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/catalog.json":
					_, _ = fmt.Fprintf(w, `{"schemaVersion":2,"entries":[{"key":"current","name":"current","version":"1","format":"codex","sourceCommit":%q,"archiveURL":%q,"archiveSHA256":%q,"packagePath":"entry","detailsURL":%q,"detailsSHA256":"%x","detailsSize":%d}]}`, commit, source.URL+"/owned.zip", archiveHash, source.URL+"/details.json", sha256.Sum256(body), len(body))
				case "/owned.zip":
					once.Do(func() { close(entered) })
					<-gate
					_, _ = w.Write(archive)
				case "/details.json":
					_, _ = w.Write(body)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer source.Close()
			// 失败路径先释放下载，再关闭服务，避免回归超时后等待慢连接。
			var releaseGate sync.Once
			defer releaseGate.Do(func() { close(gate) })
			location := source.URL + "/catalog.json"
			stop := Start(context.Background(), location, t.TempDir(), nil)
			defer stop()
			if cached {
				if _, _, err := ResolveDocumentation(location, "current"); err != nil {
					t.Fatal(err)
				}
			}
			archiveDone := make(chan error, 1)
			go func() { _, _, err := ResolveCurrent(location, "current"); archiveDone <- err; close(archiveDone) }()
			defer func() { releaseGate.Do(func() { close(gate) }); <-archiveDone }()
			awaitCatalogSignal(t, entered)
			documentationDone := make(chan error, 1)
			go func() { _, _, err := ResolveDocumentation(location, "current"); documentationDone <- err }()
			select {
			case err := <-documentationDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("current documentation waited for unrelated full archive download")
			}
			select {
			case err := <-archiveDone:
				t.Fatalf("archive gate was not exercised: %v", err)
			default:
			}
		})
	}
}

func TestExplicitRefreshCoalescesAndPreservesConditionalStaleSnapshot(t *testing.T) {
	var requests atomic.Int32
	var unavailable atomic.Bool
	gate, entered := make(chan struct{}), make(chan struct{})
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requests.Add(1)
		if count == 1 {
			w.Header().Set("ETag", "fixed-v1")
			fmt.Fprint(w, `{"schemaVersion":2,"entries":[]}`)
			return
		}
		if unavailable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.Header.Get("If-None-Match") != "fixed-v1" {
			t.Error("refresh lost entity validator")
		}
		if count == 2 {
			close(entered)
		}
		<-gate
		w.WriteHeader(http.StatusNotModified)
	}))
	defer source.Close()
	location := source.URL + "/catalog.json"
	if _, err := List(location); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 8)
	for range 8 {
		go func() { done <- Refresh(context.Background(), location) }()
	}
	awaitCatalogSignal(t, entered)
	// 等待其余并发调用进入同一在途同步，不引入生产冷却或常驻计时器。
	time.Sleep(30 * time.Millisecond)
	close(gate)
	for range 8 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 2 {
		t.Fatalf("concurrent refresh duplicated network: %d", requests.Load())
	}
	unavailable.Store(true)
	if err := Refresh(context.Background(), location); !errors.Is(err, status_error.PluginDistributionUnavailable) {
		t.Fatalf("refresh hid failure: %v", err)
	}
	entries, state := Snapshot(location)
	if len(entries) != 0 || state.State != "stale" || state.ErrorCode == "" || state.FetchedAt == 0 {
		t.Fatalf("lost old snapshot or source failure: %+v", state)
	}
	if _, err := List(location); err != nil {
		t.Fatal("failed refresh discarded readable snapshot", err)
	}
}

func TestSnapshotDoesNotSynchronizeColdRemote(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	var requests atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer source.Close()
	entries, state := Snapshot(source.URL + "/catalog.json")
	if len(entries) != 0 || state.State != "unavailable" || requests.Load() != 0 {
		t.Fatalf("snapshot contacted upstream: %+v requests=%d", state, requests.Load())
	}
}

func TestCloseWaitsForExplicitRefreshAfterStartup(t *testing.T) {
	var requests atomic.Int32
	entered := make(chan struct{})
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			fmt.Fprint(w, `{"schemaVersion":2,"entries":[]}`)
			return
		}
		close(entered)
		<-r.Context().Done()
	}))
	defer source.Close()
	location := source.URL + "/catalog.json"
	stop := Start(context.Background(), location, t.TempDir(), nil)
	if _, err := List(location); err != nil {
		t.Fatal(err)
	}
	index := remoteSource(location)
	<-index.done
	refreshed := make(chan error, 1)
	go func() { refreshed <- Refresh(context.Background(), location) }()
	awaitCatalogSignal(t, entered)
	// 阻止被取消的请求发布最终状态，验证关闭确实等待本次核验退出。
	index.mu.Lock()
	closed := make(chan struct{})
	go func() { stop(); close(closed) }()
	select {
	case <-closed:
		index.mu.Unlock()
		t.Fatal("close returned while explicit refresh still owned state publication")
	case <-time.After(40 * time.Millisecond):
	}
	index.mu.Unlock()
	awaitCatalogSignal(t, closed)
	if err := <-refreshed; !errors.Is(err, status_error.PluginDistributionUnavailable) {
		t.Fatalf("close did not cancel explicit refresh: %v", err)
	}
	if err := Refresh(context.Background(), location); !errors.Is(err, status_error.PluginDistributionUnavailable) {
		t.Fatalf("closed source accepted new refresh: %v", err)
	}
	if requests.Load() != 2 {
		t.Fatalf("closed source started another request: %d", requests.Load())
	}
}
