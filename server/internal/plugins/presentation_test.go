package plugins

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPresentationLoadsBoundedBilingualMetadataWithoutChangingIdentity(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"feishu-office","description":"Office tools","interface":{"displayName":"Feishu Office"}}`)
	fixtureFile(t, root, ".neuxally/presentation.json", `{"displayNames":{"en":"Feishu Office","zh-CN":"飞书办公"},"localizedDescriptions":{"en":"Work with documents","zh-CN":"处理文档与办公资料"}}`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(pkg.Presentation)
	if !strings.Contains(string(encoded), `"zh-CN":"飞书办公"`) || !strings.Contains(string(encoded), `"zh-CN":"处理文档与办公资料"`) {
		t.Fatalf("localized presentation missing: %s", encoded)
	}
	if pkg.Manifest.Name != "feishu-office" {
		t.Fatal("presentation changed identity")
	}
	fixtureFile(t, root, ".neuxally/presentation.json", `{"displayNames":{"zh-CN":"`+strings.Repeat("x", 4097)+`"}}`)
	pkg, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ = json.Marshal(pkg.Presentation)
	if strings.Contains(string(encoded), strings.Repeat("x", 4097)) {
		t.Fatal("unbounded locale field accepted")
	}
}

func TestReadmeLanguageFallbackDoesNotHideInvalidOrOversizedChinese(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"office"}`)
	fixtureFile(t, root, "README.md", "Work with documents")
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	localized, err := LocalizeReadme(root, pkg, "zh-CN")
	if err != nil || localized.Readme != "Work with documents" {
		t.Fatal("missing translation did not fall back")
	}
	fixtureFile(t, root, "README.zh-CN.md", "处理办公资料")
	localized, err = LocalizeReadme(root, pkg, "zh-CN")
	if err != nil || localized.Readme != "处理办公资料" {
		t.Fatal("Chinese preview missing")
	}
	raw, err := ReadRawReadme(root, pkg, "zh-CN")
	if err != nil || raw != localized.Readme {
		t.Fatal("raw locale differs from preview")
	}
	fixtureFile(t, root, "README.zh-CN.md", strings.Repeat("x", maxReadmeBytes+1))
	localized, err = LocalizeReadme(root, pkg, "zh-CN")
	if err != nil || localized.ReadmeStatus != "too-large" || localized.Readme != "" {
		t.Fatal("oversized translation silently fell back or truncated")
	}
	fixtureFile(t, root, "README.zh-CN.md", "invalid\x00document")
	localized, err = LocalizeReadme(root, pkg, "zh-CN")
	if err != nil || localized.ReadmeStatus != "invalid" || localized.Readme != "" {
		t.Fatal("invalid translation silently fell back")
	}
	if _, err = ReadRawReadme(root, pkg, "zh-CN"); err == nil {
		t.Fatal("invalid raw documentation accepted")
	}
}
