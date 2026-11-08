package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPresentationLoadsBoundedBilingualMetadataWithoutChangingIdentity(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "feishu-office", "description": "Office tools", "extensions": map[string]any{"com.openai": map[string]any{"interface": map[string]any{"displayName": "Feishu Office", "defaultPrompt": []string{"Read documents"}, "capabilities": []string{"Read"}, "supportURL": "https://example.com/support", "brandColor": "#123456", "brandColorDark": "#abcdef"}}, "com.neuxbot": map[string]any{"schemaVersion": 1, "presentation": ".neuxbot/presentation.json"}}})
	fixtureFile(t, root, ".neuxbot/presentation.json", `{"displayNames":{"en":"Feishu Office","zh-CN":"飞书办公"},"localizedDescriptions":{"en":"Work with documents","zh-CN":"处理文档与办公资料"},"defaultPromptLocalizations":{"zh-CN":["读取文档"]},"setup":{"instructions":{"zh-CN":"连接前请授权"},"helpURL":"https://example.com/help"}}`)
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
	fixtureFile(t, root, ".neuxbot/presentation.json", `{"displayNames":{"zh-CN":"`+strings.Repeat("x", 4097)+`"}}`)
	if _, err = Load(root); err == nil {
		t.Fatal("unbounded localization accepted")
	}
	fixtureFile(t, root, ".neuxbot/presentation.json", `{"defaultPromptLocalizations":{"zh-CN":["one","two"]}}`)
	if _, err = Load(root); err == nil {
		t.Fatal("misaligned prompt translation accepted")
	}

}

func TestReadmeLanguageFallbackDoesNotHideInvalidOrOversizedChinese(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "office"})
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

func TestStandardThemeIconsAndPromptNormalization(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"extensions": map[string]any{"com.openai": map[string]any{"interface": map[string]any{"defaultPrompt": "Read reports", "logo": "assets/default.svg", "logoDark": "assets/dark.svg"}}}})
	fixtureFile(t, root, "assets/default.svg", `<svg xmlns="http://www.w3.org/2000/svg"><path fill="#123456" d="M0 0h1"/></svg>`)
	fixtureFile(t, root, "assets/dark.svg", `<svg xmlns="http://www.w3.org/2000/svg"><path fill="#abcdef" d="M0 0h1"/></svg>`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Presentation.DefaultPrompt) != 1 || pkg.Presentation.DefaultPrompt[0] != "Read reports" || pkg.Icon != pkg.Presentation.Icons.Default || pkg.Presentation.Icons.Default == pkg.Presentation.Icons.Dark || pkg.Presentation.Icons.Dark == "" {
		t.Fatalf("theme or prompt collapsed: %+v", pkg.Presentation)
	}
	if err := os.Remove(filepath.Join(root, "assets/dark.svg")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("missing declared dark icon silently fell back")
	}
}

func TestStandardInterfaceRejectsThemeObjectInputs(t *testing.T) {
	for _, key := range []string{"logo", "logoDark", "composerIcon", "composerIconDark", "brandColor", "brandColorDark"} {
		t.Run(key, func(t *testing.T) {
			root := t.TempDir()
			standardInput(t, root, map[string]any{"extensions": map[string]any{"com.openai": map[string]any{"interface": map[string]any{key: map[string]string{"default": "./assets/icon.svg", "dark": "./assets/dark.svg"}}}}})
			if _, err := Load(root); err == nil {
				t.Fatal("nonstandard theme object accepted")
			}
		})
	}
}
