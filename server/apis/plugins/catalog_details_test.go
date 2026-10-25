package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"github.com/gin-gonic/gin"
)

// detailsFixture 固定轻量公开资料与独立安装包的测试分发边界。
type detailsFixture struct {
	// ctx 是临时数据库上下文。
	ctx *gin.Context
	// source 是测试分发服务。
	source *httptest.Server
	// archiveRequests 记录完整安装包读取次数。
	archiveRequests atomic.Int32
	// detailsRequests 记录公开资料读取次数。
	detailsRequests atomic.Int32
	// commit 是固定来源提交。
	commit string
	// archiveHash 是完整安装包摘要。
	archiveHash string
	// skill 是固定公开技能正文。
	skill string
}

func newDetailsFixture(t *testing.T, invalid string) *detailsFixture {
	t.Helper()
	f := &detailsFixture{ctx: pluginAPIContext(t), commit: strings.Repeat("d", 40), skill: "---\nname: demo\ndescription: Public demo\n---\nComplete public skill\n"}
	t.Setenv("TMPDIR", t.TempDir())
	archive := recoveryArchive(t, "2.0.0", "English public README")
	f.archiveHash = fmt.Sprintf("%x", sha256.Sum256(archive))
	auth := map[string]any{"strategy": "plugin-command", "protocol": "neuxally-auth/1", "preferredAction": "open-url", "scopes": []string{"read"}}
	for _, field := range []string{"command", "args", "env", "headers", "secret"} {
		if invalid == field || (invalid == "execution-field" && field == "command") {
			auth[field] = "secret-program"
		}
	}
	doc := map[string]any{"schemaVersion": 1, "sourceCommit": f.commit, "archiveSHA256": f.archiveHash, "packagePath": "entry", "package": map[string]any{"manifest": map[string]any{"name": "recovery", "version": "2.0.0", "description": "Public docs"}, "format": "codex", "presentation": map[string]any{"displayName": "Recovery"}, "skills": []map[string]any{{"key": "skills/demo", "name": "demo", "description": "Public demo", "path": "skills/demo"}}, "servers": []map[string]any{{"key": "remote", "type": "streamable-http", "url": "https://example.invalid/mcp"}}, "integration": map[string]any{"authentication": auth}, "diagnostics": []any{}}, "readmes": map[string]string{"en": "English public README", "zh-CN": "中文公开说明"}, "skillContents": map[string]string{"skills/demo": f.skill}}
	if invalid == "binding" {
		doc["sourceCommit"] = strings.Repeat("e", 40)
	}
	if invalid == "schema" {
		doc["schemaVersion"] = 2
	}
	if invalid == "missing-skill" {
		doc["skillContents"] = map[string]string{}
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(body))
	if invalid == "hash" {
		hash = strings.Repeat("f", 64)
	}
	size := len(body)
	if invalid == "size" {
		size--
	}
	f.source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/catalog.json":
			_, _ = fmt.Fprintf(w, `{"schemaVersion":2,"entries":[{"key":"recovery","name":"recovery","version":"2.0.0","format":"codex","sourceCommit":%q,"archiveURL":%q,"archiveSHA256":%q,"packagePath":"entry","detailsURL":%q,"detailsSHA256":%q,"detailsSize":%d}]}`, f.commit, f.source.URL+"/owned.zip", f.archiveHash, f.source.URL+"/recovery.details.json", hash, size)
		case "/recovery.details.json":
			f.detailsRequests.Add(1)
			if invalid == "network" {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write(body)
		case "/owned.zip":
			f.archiveRequests.Add(1)
			_, _ = w.Write(archive)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.source.Close)
	location := f.source.URL + "/catalog.json"
	stop := catalog.Start(context.Background(), location, t.TempDir(), nil)
	t.Cleanup(stop)
	t.Setenv("ALLY_PLUGIN_CATALOG_URL", location)
	return f
}

func TestCatalogDetailsReadmeAndSkillNeedNoInstallationOrArchive(t *testing.T) {
	f := newDetailsFixture(t, "")
	var readers sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			_, err := (&GetCatalogPlugin{Key: "recovery", Locale: "zh-CN"}).Output(f.ctx)
			failures <- err
		}()
	}
	readers.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	value, err := (&GetCatalogPlugin{Key: "recovery", Locale: "zh-CN"}).Output(f.ctx)
	if err != nil || value.(catalog.Entry).Package.Readme != "中文公开说明" {
		t.Fatalf("public localized details failed: %+v %v", value, err)
	}
	publicJSON, err := json.Marshal(value.(catalog.Entry).Package)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"command", "args", "env", "headers", "secret", "extensions", "digest"} {
		if strings.Contains(string(publicJSON), `"`+field+`":`) {
			t.Fatalf("public response leaked executable/private field %s: %s", field, publicJSON)
		}
	}
	value, err = (&GetCatalogPluginReadme{Key: "recovery", SourceCommit: f.commit, ArchiveSHA256: f.archiveHash}).Output(f.ctx)
	if err != nil || value.(PluginReadmeResponse).Content != "English public README" {
		t.Fatalf("public README failed: %+v %v", value, err)
	}
	value, err = (&GetCatalogPluginSkill{Key: "recovery", SkillKey: "skills/demo", SourceCommit: f.commit, ArchiveSHA256: f.archiveHash}).Output(f.ctx)
	if err != nil || value.(PluginSkillResponse).Content != f.skill {
		t.Fatalf("public SKILL failed: %+v %v", value, err)
	}
	if f.archiveRequests.Load() != 0 || f.detailsRequests.Load() != 1 {
		t.Fatalf("public reads downloaded full archive or duplicated metadata: zip=%d details=%d", f.archiveRequests.Load(), f.detailsRequests.Load())
	}
	_, total, err := global.PluginControllerManager.List(f.ctx, "", "", "", 0, 100, false)
	if err != nil || total != 0 {
		t.Fatalf("public read registered resources: %d %v", total, err)
	}
	if _, err := (&GetCatalogPluginReadme{Key: "recovery", SourceCommit: strings.Repeat("a", 40), ArchiveSHA256: f.archiveHash}).Output(f.ctx); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatalf("bound read accepted another commit: %v", err)
	}
	value, err = (&PrepareCatalogPlugin{Key: "recovery"}).Output(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	prepared := value.(PluginDetailResponse)
	if f.archiveRequests.Load() != 1 || prepared.Ready || len(prepared.Fields) != 1 || prepared.Fields[0].Key != "TOKEN" {
		t.Fatalf("prepare used public metadata as executable truth: zip=%d %+v", f.archiveRequests.Load(), prepared)
	}
}

func TestDeclaredDetailsFailuresNeverFallBackToArchive(t *testing.T) {
	for _, invalid := range []string{"hash", "binding", "schema", "execution-field", "missing-skill", "network", "args", "env", "headers", "secret", "size"} {
		t.Run(invalid, func(t *testing.T) {
			f := newDetailsFixture(t, invalid)
			_, err := (&GetCatalogPlugin{Key: "recovery"}).Output(f.ctx)
			if err == nil {
				t.Fatal("invalid declared sidecar was accepted")
			}
			for range 3 {
				_, _ = (&GetCatalogPluginReadme{Key: "recovery", SourceCommit: f.commit, ArchiveSHA256: f.archiveHash}).Output(f.ctx)
			}
			if f.detailsRequests.Load() != 1 {
				t.Fatalf("invalid metadata caused repeated downloads: %d", f.detailsRequests.Load())
			}
			if f.archiveRequests.Load() != 0 {
				t.Fatalf("invalid declared sidecar fell back to ZIP: %d", f.archiveRequests.Load())
			}
			if invalid == "network" && !errors.Is(err, status_error.PluginDistributionUnavailable) {
				t.Fatalf("sidecar network failure reported as readiness: %v", err)
			}
		})
	}
}

func TestCurrentDetailsRecoversMetadataButPinnedReadsStayBound(t *testing.T) {
	for _, failure := range []string{"removed-url", "replaced-bytes"} {
		t.Run(failure, func(t *testing.T) {
			ctx := pluginAPIContext(t)
			t.Setenv("TMPDIR", t.TempDir())
			archiveHash := strings.Repeat("c", 64)
			commits := []string{strings.Repeat("a", 40), strings.Repeat("b", 40)}
			bodies := make([][]byte, 2)
			for revision := range 2 {
				bodies[revision] = []byte(fmt.Sprintf(`{"schemaVersion":1,"sourceCommit":%q,"archiveSHA256":%q,"packagePath":"entry","package":{"manifest":{"name":"recovery","version":"%d.0.0"},"format":"codex","skills":[],"servers":[],"diagnostics":[]},"readmes":{"en":"revision-%d"},"skillContents":{}}`, commits[revision], archiveHash, revision+1, revision))
			}
			var mode, indexRequests, archiveRequests, detailsRequests atomic.Int32
			var source *httptest.Server
			source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				revision := int(mode.Load())
				switch r.URL.Path {
				case "/catalog.json":
					indexRequests.Add(1)
					path := "/details.json"
					if failure == "removed-url" {
						path = []string{"/old.details.json", "/new.details.json"}[revision]
					}
					_, _ = fmt.Fprintf(w, `{"schemaVersion":2,"entries":[{"key":"recovery","name":"recovery","version":"%d.0.0","format":"codex","sourceCommit":%q,"archiveURL":%q,"archiveSHA256":%q,"packagePath":"entry","detailsURL":%q,"detailsSHA256":"%x","detailsSize":%d}]}`, revision+1, commits[revision], source.URL+"/owned.zip", archiveHash, source.URL+path, sha256.Sum256(bodies[revision]), len(bodies[revision]))
				case "/owned.zip":
					archiveRequests.Add(1)
					w.WriteHeader(http.StatusNotFound)
				default:
					detailsRequests.Add(1)
					if failure == "removed-url" && revision == 1 && r.URL.Path == "/old.details.json" {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					_, _ = w.Write(bodies[revision])
				}
			}))
			defer source.Close()
			location := source.URL + "/catalog.json"
			stop := catalog.Start(context.Background(), location, t.TempDir(), nil)
			defer stop()
			t.Setenv("ALLY_PLUGIN_CATALOG_URL", location)
			if _, err := catalog.Find(location, "recovery"); err != nil {
				t.Fatal(err)
			}
			mode.Store(1)
			if _, err := (&GetCatalogPluginReadme{Key: "recovery", SourceCommit: commits[0], ArchiveSHA256: archiveHash}).Output(ctx); err == nil {
				t.Fatal("pinned raw request silently refreshed metadata")
			}
			if indexRequests.Load() != 1 {
				t.Fatal("pinned raw request refreshed catalog")
			}
			value, err := (&GetCatalogPlugin{Key: "recovery"}).Output(ctx)
			if err != nil || value.(catalog.Entry).Package.Readme != "revision-1" {
				t.Fatalf("current metadata did not recover: %+v %v", value, err)
			}
			if indexRequests.Load() != 2 || detailsRequests.Load() != 2 || archiveRequests.Load() != 0 {
				t.Fatalf("recovery exceeded bounded requests: index=%d details=%d ZIP=%d", indexRequests.Load(), detailsRequests.Load(), archiveRequests.Load())
			}
			if _, err := (&GetCatalogPluginReadme{Key: "recovery", SourceCommit: commits[0], ArchiveSHA256: archiveHash}).Output(ctx); !errors.Is(err, status_error.PluginVersionConflict) {
				t.Fatalf("old metadata binding changed silently: %v", err)
			}
		})
	}
}
