package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
)

func TestCatalogDetailAndRawReadmeAreReadOnlyAndSourceBound(t *testing.T) {
	for _, size := range []int{300 * 1024, 1024*1024 + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			ctx := pluginAPIContext(t)
			readme := strings.Repeat("Readme正文\n", size/13+1)
			var archive bytes.Buffer
			writer := zip.NewWriter(&archive)
			for name, content := range map[string]string{"neuxally-plugin/demo/.codex-plugin/plugin.json": `{"name":"demo","version":"2"}`, "neuxally-plugin/demo/README.md": readme, "neuxally-plugin/demo/README.zh-CN.md": "目录中文说明"} {
				file, err := writer.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := file.Write([]byte(content)); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(archive.Bytes())
			var source *httptest.Server
			source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/catalog.json" {
					fmt.Fprintf(w, `{"schemaVersion":2,"entries":[{"key":"demo","sourceCommit":"1111111111111111111111111111111111111111","archiveURL":%q,"archiveSHA256":"%x","packagePath":"neuxally-plugin/demo","name":"demo","version":"2","format":"codex"}]}`, source.URL+"/demo.zip", sum)
					return
				}
				_, _ = w.Write(archive.Bytes())
			}))
			defer source.Close()
			location := source.URL + "/catalog.json"
			stopCatalog := catalog.Start(context.Background(), location, t.TempDir(), nil)
			defer stopCatalog()
			t.Setenv("ALLY_PLUGIN_CATALOG_URL", location)
			output, err := (&GetCatalogPlugin{Key: "demo"}).Output(ctx)
			if err != nil {
				t.Fatal(err)
			}
			entry := output.(catalog.Entry)
			if size == 300*1024 && (entry.Package.ReadmeStatus != "available" || entry.Package.Readme != readme) {
				t.Fatal("300 KiB readme was truncated")
			}
			if size > 1024*1024 && entry.Package.ReadmeStatus != "too-large" {
				t.Fatalf("oversize not explicit: %s", entry.Package.ReadmeStatus)
			}
			raw, err := (&GetCatalogPluginReadme{Key: "demo", SourceCommit: entry.SourceCommit, ArchiveSHA256: entry.ArchiveSHA256}).Output(ctx)
			if err != nil || raw.(PluginReadmeResponse).Content != readme {
				t.Fatalf("raw content changed: %v", err)
			}
			localizedOutput, err := (&GetCatalogPlugin{Key: "demo", Locale: "zh-CN"}).Output(ctx)
			if err != nil || localizedOutput.(catalog.Entry).Package.Readme != "目录中文说明" {
				t.Fatalf("catalog locale not selected: %v", err)
			}
			localizedRaw, err := (&GetCatalogPluginReadme{Key: "demo", Locale: "zh-CN", SourceCommit: entry.SourceCommit, ArchiveSHA256: entry.ArchiveSHA256}).Output(ctx)
			if err != nil || localizedRaw.(PluginReadmeResponse).Content != "目录中文说明" {
				t.Fatalf("catalog raw locale diverged: %v", err)
			}
			_, err = (&GetCatalogPluginReadme{Key: "demo", SourceCommit: "2222222222222222222222222222222222222222"}).Output(ctx)
			if !errors.Is(err, status_error.PluginVersionConflict) {
				t.Fatalf("accepted mismatched displayed version: %v", err)
			}
			_, total, err := global.PluginControllerManager.List(ctx, "", "", "", 0, 100, false)
			if err != nil || total != 0 {
				t.Fatalf("read-only preview created system resources: %d %v", total, err)
			}
		})
	}
}

func TestCatalogAndInstalledReadmeLocaleKeepsPinnedSource(t *testing.T) {
	ctx := pluginAPIContext(t)
	root := t.TempDir()
	for name, content := range map[string]string{".codex-plugin/plugin.json": `{"name":"office","version":"0.1.0"}`, "README.md": "Work with documents", "README.zh-CN.md": "处理办公资料"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	stage, err := packageplugin.Stage(stagedRoot(), root)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := prepareCatalogFixture(t, ctx, stage, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	request := &GetPlugin{PluginID: detail.Plugin.ID, VersionID: detail.ResolvedVersionID}
	_ = json.Unmarshal([]byte(`{"Locale":"zh-CN"}`), request)
	value, err := request.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	localized := value.(PluginDetailResponse)
	if localized.Versions[0].Package.Readme != "处理办公资料" {
		t.Fatal("installed locale returned another language")
	}
	raw := &GetPluginReadme{PluginID: detail.Plugin.ID, VersionID: detail.ResolvedVersionID}
	_ = json.Unmarshal([]byte(`{"Locale":"zh-CN"}`), raw)
	value, err = raw.Output(ctx)
	if err != nil || value.(PluginReadmeResponse).Content != "处理办公资料" {
		t.Fatalf("raw locale diverged: %+v %v", value, err)
	}
}
