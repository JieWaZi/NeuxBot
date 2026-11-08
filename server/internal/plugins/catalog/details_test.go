package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltCatalogDetailsArtifacts(t *testing.T) {
	directory := os.Getenv("NEUXALLY_PLUGIN_DETAILS_ARTIFACT_DIR")
	if directory == "" {
		t.Skip("显式固定插件分发资料验收")
	}
	raw, err := os.ReadFile(filepath.Join(directory, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := parseIndex(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("unexpected real catalog entry count: %d", len(entries))
	}
	for _, entry := range entries {
		t.Run(entry.Key, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(directory, entry.Key+".details.json"))
			if err != nil {
				t.Fatal(err)
			}
			details, err := parseDetails(raw, entry)
			if err != nil {
				t.Fatalf("real published metadata rejected: %v", err)
			}
			for _, locale := range []string{"en", "zh-CN"} {
				content, _, err := details.Readme(locale)
				if err != nil || content == "" || details.ViewPackage(locale).Readme != content {
					t.Fatalf("real localized README unavailable: %s %v", locale, err)
				}
			}
			for _, skill := range details.Package.Skills {
				_, content, err := details.Skill(skill.Key)
				if err != nil || content == "" {
					t.Fatalf("real SKILL unavailable: %s %v", skill.Key, err)
				}
			}
			t.Logf("%s: verified %d byte public metadata, %d complete SKILL bodies, no archive download", entry.Key, len(raw), len(details.SkillContents))
		})
	}
}

func TestStandardDetailsRejectsLegacyAndPrivateFields(t *testing.T) {
	body := []byte(`{"schemaVersion":1,"sourceCommit":"1111111111111111111111111111111111111111","archiveSHA256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","packagePath":"entry","package":{"contractVersion":"neuxbot-plugin/1","manifest":{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"standard","version":"1"},"format":"agent-plugins","presentation":{"defaultPrompt":["Read reports"],"defaultPromptLocalizations":{"zh-CN":["读取报告"]},"capabilities":["Read"],"supportURL":"https://example.com/support","brandColor":{"default":"#123456","dark":"#abcdef"},"icons":{},"setup":{"instructions":{"en":"Connect your account"},"helpURL":"https://example.com/help"}},"skills":[],"servers":[],"diagnostics":[]},"readmes":{"en":"Public README"},"skillContents":{}}`)
	check := func(raw []byte) error {
		sum := sha256.Sum256(raw)
		entry := Entry{SourceCommit: strings.Repeat("1", 40), ArchiveSHA256: strings.Repeat("b", 64), PackagePath: "entry", DetailsSHA256: hex.EncodeToString(sum[:]), DetailsSize: int64(len(raw)), Package: plugins.Package{Manifest: plugins.Manifest{Name: "standard", Version: "1"}}}
		_, err := parseDetails(raw, entry)
		return err
	}
	if err := check(body); err != nil {
		t.Fatal(err)
	}
	for _, changed := range []string{strings.Replace(string(body), `"schemaVersion":1`, `"schemaVersion":2`, 1), strings.Replace(string(body), `"contractVersion":"neuxbot-plugin/1",`, "", 1), strings.Replace(string(body), `"capabilities":["Read"]`, `"capabilities":["Read"],"command":"auth.mjs"`, 1), strings.Replace(string(body), `"setup":{"instructions"`, `"setup":{"command":"auth.mjs","instructions"`, 1), strings.Replace(string(body), `"brandColor":{"default":"#123456"`, `"brandColor":{"default":"url(https://bad.example)"`, 1), strings.Replace(string(body), `"zh-CN":["读取报告"]`, `"zh-CN":["读取报告","错误索引"]`, 1)} {
		if err := check([]byte(changed)); err == nil {
			t.Fatal("unsafe/old details accepted")
		}
	}
}
