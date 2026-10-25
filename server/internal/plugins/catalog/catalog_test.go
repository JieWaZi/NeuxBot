package catalog

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogLoadsRealBundledFeishu(t *testing.T) {
	source := filepath.Join("..", "..", "..", "..", "..", "desktop", "resources", "plugins")
	if _, err := os.Stat(filepath.Join(source, "catalog.json")); os.IsNotExist(err) {
		t.Skip("运行 pnpm plugins:prepare 后验证真实目录")
	}
	entries, err := List(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Key != "feishu-office" || len(entries[0].Package.Skills) < 5 {
		t.Fatalf("real catalog incomplete: %+v", entries)
	}
	if _, err := Resolve(source, "../outside"); err == nil {
		t.Fatal("accepted traversal key")
	}
	if _, err := Resolve(source, "feishu-office"); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogRejectsChangedZIP(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "catalog")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	archive := []byte("fixed archive bytes")
	sum := sha256.Sum256(archive)
	index := fmt.Sprintf(`{"schemaVersion":1,"entries":[{"key":"example","directory":"catalog/example.zip","sourceCommit":"1111111111111111111111111111111111111111","archiveSHA256":"%x","package":{"manifest":{"name":"example"}}}]}`, sum)
	if err := os.WriteFile(filepath.Join(root, "catalog.json"), []byte(index), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "example.zip"), archive, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(root, "example"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "example.zip"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(root, "example"); err == nil {
		t.Fatal("accepted modified ZIP")
	}
}

func TestCatalogCarriesSanitizedIconAndBilingualPresentationWithoutPackageDownload(t *testing.T) {
	icon := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>`))
	data := []byte(`{"schemaVersion":2,"entries":[{"key":"office","name":"office","version":"0.1.0","description":"Office tools","format":"codex","sourceCommit":"` + strings.Repeat("a", 40) + `","archiveSHA256":"` + strings.Repeat("b", 64) + `","archiveURL":"https://example.invalid/office.zip","packagePath":"neuxally-plugin/office","icon":"` + icon + `","displayNames":{"en":"Office","zh-CN":"办公"},"localizedDescriptions":{"zh-CN":"处理办公资料"}}]}`)
	entries, err := parseIndex(data)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Package.Icon != icon {
		t.Fatal("catalog lost safe icon before installation")
	}
	encoded, _ := json.Marshal(entries[0].Package.Presentation)
	if !strings.Contains(string(encoded), "办公") {
		t.Fatalf("localized catalog metadata missing: %s", encoded)
	}
	unsafe := strings.Replace(string(data), icon, "https://example.invalid/track.svg", 1)
	entries, err = parseIndex([]byte(unsafe))
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Package.Icon != "" {
		t.Fatal("remote catalog icon accepted")
	}
}

func TestBuiltPresentationArtifacts(t *testing.T) {
	directory := os.Getenv("NEUXALLY_PLUGIN_PRESENTATION_ARTIFACT_DIR")
	if directory == "" {
		t.Skip("explicit immutable plugin artifact acceptance only")
	}
	data, err := os.ReadFile(filepath.Join(directory, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := parseIndex(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("unexpected plugin catalog count: %d", len(entries))
	}
	for _, entry := range entries {
		t.Run(entry.Key, func(t *testing.T) {
			if entry.Package.Icon == "" || len(entry.Package.Presentation.DisplayNames) != 2 || len(entry.Package.Presentation.LocalizedDescriptions) != 2 {
				t.Fatal("pre-install presentation incomplete")
			}
			archiveBytes, err := os.ReadFile(filepath.Join(directory, entry.Key+".zip"))
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprintf("%x", sha256.Sum256(archiveBytes)) != entry.ArchiveSHA256 {
				t.Fatal("catalog archive hash differs from artifact")
			}
			stage, err := plugins.StageSource(t.TempDir(), filepath.Join(directory, entry.Key+".zip"), entry.PackagePath)
			if err != nil {
				t.Fatal(err)
			}
			if stage.Package.Icon != entry.Package.Icon {
				t.Fatal("catalog and ZIP icon diverged")
			}
			if stage.Package.Manifest.Version != "0.1.0" || stage.Package.Presentation.DeveloperName != "NeuxBot" {
				t.Fatal("logical plugin identity or developer differs")
			}
			for _, locale := range []string{"en", "zh-CN"} {
				localized, err := plugins.LocalizeReadme(stage.Root, stage.Package, locale)
				if err != nil {
					t.Fatal(err)
				}
				if localized.ReadmeStatus != "available" || localized.Readme == "" {
					t.Fatalf("missing localized business README: %s", locale)
				}
				raw, err := plugins.ReadRawReadme(stage.Root, stage.Package, locale)
				if err != nil || raw != localized.Readme {
					t.Fatalf("raw/preview diverged: %s %v", locale, err)
				}
				if entry.Package.Presentation.DisplayNames[locale] != stage.Package.Presentation.DisplayNames[locale] || entry.Package.Presentation.LocalizedDescriptions[locale] != stage.Package.Presentation.LocalizedDescriptions[locale] {
					t.Fatalf("metadata and archive locale diverged: %s", locale)
				}
			}
			t.Logf("%s 0.1.0: metadata icon %d bytes, bilingual names/descriptions/README match complete ZIP", entry.Key, len(entry.Package.Icon))
		})
	}
}

func TestCatalogRejectsIncompleteOrInvalidDetailsMetadata(t *testing.T) {
	for _, fields := range []string{
		`"detailsURL":"https://example.invalid/details.json"`,
		`"detailsSize":0`,
		`"detailsURL":null,"detailsSHA256":null,"detailsSize":null`,
		`"detailsURL":"file:///tmp/details.json","detailsSHA256":"` + strings.Repeat("a", 64) + `","detailsSize":100`,
		`"detailsURL":"https://example.invalid/details.json","detailsSHA256":"bad","detailsSize":100`,
		`"detailsURL":"https://example.invalid/details.json","detailsSHA256":"` + strings.Repeat("a", 64) + `","detailsSize":0`,
		`"detailsURL":"https://example.invalid/details.json","detailsSHA256":"` + strings.Repeat("a", 64) + `","detailsSize":4194305`,
	} {
		raw := fmt.Sprintf(`{"schemaVersion":2,"entries":[{"key":"details","name":"details","sourceCommit":"%s","archiveURL":"https://example.invalid/owned.zip","archiveSHA256":"%s","packagePath":"entry",%s}]}`, strings.Repeat("a", 40), strings.Repeat("b", 64), fields)
		if _, err := parseIndex([]byte(raw)); err == nil {
			t.Fatalf("invalid sidecar metadata accepted: %s", fields)
		}
	}
}
