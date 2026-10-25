package plugins

import (
	"encoding/json"
	"testing"
)

func TestPresentationSeparatesPackageAndDeclaredUpstreamVersion(t *testing.T) {
	for source, expected := range map[string]string{
		`{"upstream":{"tag":"v1.0.98"}}`:    "v1.0.98",
		`{"upstream":{"version":"1.0.98"}}`: "1.0.98",
		`{"cli":{"version":"2.102.0"}}`:     "",
		`{"upstream":{"tag":98}}`:           "",
		`{}`:                                "",
	} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"office","version":"0.1.0","author":{"name":"NeuxBot contributors"}}`)
			fixtureFile(t, root, "provenance.json", source)
			pkg, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(pkg)
			var presentation map[string]any
			if err := json.Unmarshal(data, &presentation); err != nil {
				t.Fatal(err)
			}
			upstream, _ := presentation["upstream"].(map[string]any)
			actual, _ := upstream["tag"].(string)
			if actual == "" {
				actual, _ = upstream["version"].(string)
			}
			if actual != expected || pkg.Manifest.Version != "0.1.0" {
				t.Fatalf("package/upstream version changed: %s %q want %q", pkg.Manifest.Version, actual, expected)
			}
		})
	}
}

func TestPackageUsesDeclaredUpstreamIdentityInsteadOfWrapperBrand(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"office","version":"0.1.0","author":{"name":"NeuxBot contributors"},"interface":{"developerName":"飞书"}}`)
	fixtureFile(t, root, "provenance.json", `{"upstream":{"author":"Lark Suite CLI contributors","tag":"v1.0.98","repository":"https://github.com/larksuite/cli","license":"MIT"}}`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Upstream == nil || pkg.Upstream.Author != "Lark Suite CLI contributors" || pkg.Upstream.Tag != "v1.0.98" || pkg.Upstream.Repository != "https://github.com/larksuite/cli" || pkg.Upstream.License != "MIT" {
		t.Fatalf("upstream declaration changed: %+v", pkg.Upstream)
	}
	if pkg.Manifest.Author.Name != "NeuxBot contributors" || pkg.Manifest.Version != "0.1.0" {
		t.Fatal("upstream display replaced the actual package identity")
	}
}
