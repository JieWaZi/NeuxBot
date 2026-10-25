package catalog

import (
	"os"
	"path/filepath"
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
	if len(entries) != 2 {
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
