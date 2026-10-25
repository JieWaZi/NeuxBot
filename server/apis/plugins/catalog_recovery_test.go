package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
)

func recoveryArchive(t *testing.T, version, readme string) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, body := range map[string]string{"entry/.codex-plugin/plugin.json": `{"name":"recovery","version":"` + version + `","extensions":{"com.ally":{"config":[{"key":"TOKEN","secret":true,"required":true}]}}}`, "entry/README.md": readme, "entry/skills/demo/SKILL.md": "---\nname: demo\ndescription: Demo\n---\n" + readme} {
		item, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := item.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func TestCurrentCatalogRecoversStaleArchiveWithoutRelaxingPinnedReads(t *testing.T) {
	for _, operation := range []string{"detail", "prepare"} {
		for _, failure := range []string{"removed-url", "replaced-bytes"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				ctx := pluginAPIContext(t)
				t.Setenv("TMPDIR", t.TempDir())
				first := recoveryArchive(t, "1.0.0", "old "+t.TempDir())
				second := recoveryArchive(t, "2.0.0", "new "+t.TempDir())
				sums := [][32]byte{sha256.Sum256(first), sha256.Sum256(second)}
				var mode, indexRequests atomic.Int32
				var source *httptest.Server
				source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					selected := int(mode.Load())
					if r.URL.Path == "/catalog.json" {
						indexRequests.Add(1)
						archivePath := "/owned.zip"
						if failure == "removed-url" {
							archivePath = []string{"/old.zip", "/new.zip"}[selected]
						}
						_, _ = fmt.Fprintf(w, `{"schemaVersion":2,"entries":[{"key":"recovery","name":"recovery","version":"%d.0.0","format":"codex","sourceCommit":%q,"archiveURL":%q,"archiveSHA256":"%x","packagePath":"entry"}]}`, selected+1, strings.Repeat([]string{"a", "b"}[selected], 40), source.URL+archivePath, sums[selected])
						return
					}
					if failure == "removed-url" && selected == 1 && r.URL.Path == "/old.zip" {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					_, _ = w.Write([][]byte{first, second}[selected])
				}))
				defer source.Close()
				location := source.URL + "/catalog.json"
				stop := catalog.Start(context.Background(), location, t.TempDir(), nil)
				defer stop()
				t.Setenv("ALLY_PLUGIN_CATALOG_URL", location)
				original, err := catalog.Find(location, "recovery")
				if err != nil {
					t.Fatal(err)
				}
				mode.Store(1)
				if _, err := (&GetCatalogPluginReadme{Key: "recovery", SourceCommit: original.SourceCommit, ArchiveSHA256: original.ArchiveSHA256}).Output(ctx); err == nil {
					t.Fatal("pinned read silently changed archive")
				}
				if _, err := (&GetCatalogPluginSkill{Key: "recovery", SkillKey: "skills/demo", SourceCommit: original.SourceCommit, ArchiveSHA256: original.ArchiveSHA256}).Output(ctx); err == nil {
					t.Fatal("pinned skill silently changed archive")
				}
				if indexRequests.Load() != 1 {
					t.Fatal("pinned reads refreshed the current directory")
				}
				if operation == "detail" {
					output, err := (&GetCatalogPlugin{Key: "recovery"}).Output(ctx)
					if err != nil {
						t.Fatalf("current details did not recover changed distribution: %v", err)
					}
					if got := output.(catalog.Entry); got.Package.Manifest.Version != "2.0.0" || !strings.HasPrefix(got.Package.Readme, "new ") {
						t.Fatalf("wrong recovered package: %+v", got)
					}
				} else {
					output, err := (&PrepareCatalogPlugin{Key: "recovery"}).Output(ctx)
					if err != nil {
						t.Fatalf("current preparation did not recover changed distribution: %v", err)
					}
					detail := output.(PluginDetailResponse)
					if detail.Versions[0].Version != "2.0.0" || detail.Ready || len(detail.Installations) != 0 {
						t.Fatalf("preparation required readiness or installed unexpectedly: %+v", detail)
					}
					stored, err := (&GetPlugin{PluginID: detail.Plugin.ID}).Output(ctx)
					if err != nil || stored.(PluginDetailResponse).Ready || !strings.HasPrefix(stored.(PluginDetailResponse).Versions[0].Package.Readme, "new ") {
						t.Fatalf("unconfigured installed details could not show README: %+v %v", stored, err)
					}
				}
				if indexRequests.Load() != 2 {
					t.Fatalf("recovery fetched directory %d times, want startup plus one", indexRequests.Load())
				}
				if _, err := (&GetCatalogPluginReadme{Key: "recovery", SourceCommit: original.SourceCommit, ArchiveSHA256: original.ArchiveSHA256}).Output(ctx); !errors.Is(err, status_error.PluginVersionConflict) {
					t.Fatalf("pinned request switched versions after recovery: %v", err)
				}
				if _, err := catalog.List(location); err != nil {
					t.Fatal(err)
				}
				if indexRequests.Load() != 2 {
					t.Fatal("ordinary listing refreshed after recovery")
				}
				relations, err := global.PluginControllerManager.Installations(ctx, "", "w1")
				if err != nil || len(relations) != 0 {
					t.Fatalf("current read/preparation changed installations: %+v %v", relations, err)
				}
			})
		}
	}
}
