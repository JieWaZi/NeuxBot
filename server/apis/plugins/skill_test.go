package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
)

func TestPluginSkillReadsOnlyRequestedImmutableVersionAndInstallation(t *testing.T) {
	ctx := pluginAPIContext(t)
	source := t.TempDir()
	for filename, content := range map[string]string{
		"plugin.json":                        `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"reader","version":"1.0.0"}`,
		"skills/reports/SKILL.md":            "---\nname: reports\ndescription: Write reports\n---\n# Reports\nRead references/rules.md\n",
		"skills/reports/references/rules.md": "A real resource",
		"README.md":                          "Unrelated plugin overview",
	} {
		path := filepath.Join(source, filename)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stage, err := packageplugin.Stage(stagedRoot(), source)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := prepareCatalogFixture(t, ctx, stage, "", false)
	if err != nil {
		t.Fatal(err)
	}
	version := detail.Versions[0]
	if _, err := (&InstallPlugin{PluginID: detail.Plugin.ID, Body: InstallPluginRequest{VersionID: version.ID, WakerIDs: []string{"w1"}}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	request := GetPluginSkill{PluginID: detail.Plugin.ID, VersionID: version.ID, WakerID: "w1", Key: "skills/reports"}
	value, err := request.Output(ctx)
	if err != nil || !strings.Contains(value.(PluginSkillResponse).Content, "Read references/rules.md") {
		t.Fatalf("skill content not readable: %+v %v", value, err)
	}
	response := value.(PluginSkillResponse)
	if len(response.Resources) != 3 || response.File != nil || response.Version.ID != version.ID || len(response.Versions) != 1 || response.Versions[0].Skill.Name != "reports" {
		t.Fatalf("skill detail facts incomplete: %+v", response)
	}
	request.FilePath = "references/rules.md"
	value, err = request.Output(ctx)
	if err != nil || value.(PluginSkillResponse).File == nil || value.(PluginSkillResponse).File.Content != "A real resource" {
		t.Fatalf("skill resource not readable: %+v %v", value, err)
	}
	request.FilePath = "../../README.md"
	if _, err := request.Output(ctx); !errors.Is(err, status_error.PluginContentInvalid) {
		t.Fatalf("plugin file escaped skill boundary: %v", err)
	}
	request.FilePath = ""
	request.WakerID = "w2"
	if _, err := request.Output(ctx); !errors.Is(err, status_error.PluginForbidden) {
		t.Fatalf("skill exposed through an uninstalled partner: %v", err)
	}
	request.WakerID, request.Key = "w1", "../plugin.json"
	if _, err := request.Output(ctx); !errors.Is(err, status_error.PluginContentInvalid) {
		t.Fatalf("arbitrary file requested as a skill: %v", err)
	}
	request.Key = "skills/reports"
	if err := os.WriteFile(filepath.Join(global.Config.DataDirectory, version.ContentPath, "skills/reports/SKILL.md"), []byte("modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := request.Output(ctx); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatalf("mutable source displayed as the installed version: %v", err)
	}
}

func TestPluginSkillHistoryOnlyIncludesSameComponentKey(t *testing.T) {
	ctx := pluginAPIContext(t)
	prepare := func(target, version, key string) PluginDetailResponse {
		source := t.TempDir()
		for name, content := range map[string]string{
			"plugin.json":                 `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"history-reader","version":"` + version + `"}`,
			"skills/" + key + "/SKILL.md": "---\nname: " + key + "\ndescription: Version " + version + "\n---\n# " + version + "\n",
		} {
			path := filepath.Join(source, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		stage, err := packageplugin.Stage(stagedRoot(), source)
		if err != nil {
			t.Fatal(err)
		}
		value, err := prepareCatalogFixture(t, ctx, stage, target, false)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	first := prepare("", "1.0.0", "reports")
	prepare(first.Plugin.ID, "2.0.0", "unrelated")
	last := prepare(first.Plugin.ID, "3.0.0", "reports")
	value, err := (&GetPluginSkill{PluginID: first.Plugin.ID, VersionID: last.ResolvedVersionID, Key: "skills/reports"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	response := value.(PluginSkillResponse)
	if len(response.Versions) != 2 || response.Version.Version != "3.0.0" {
		t.Fatalf("wrong skill history: %+v", response)
	}
	for _, version := range response.Versions {
		if version.Skill.Key != "skills/reports" || version.Version == "2.0.0" || version.ID == "" || version.Skill.Description != "Version "+version.Version {
			t.Fatalf("unrelated history entry: %+v", version)
		}
	}
	value, err = (&GetPluginSkill{PluginID: first.Plugin.ID, VersionID: first.ResolvedVersionID, Key: "skills/reports"}).Output(ctx)
	if err != nil || value.(PluginSkillResponse).Version.Version != "1.0.0" || !strings.Contains(value.(PluginSkillResponse).Content, "# 1.0.0") {
		t.Fatalf("historical skill not readable: %+v %v", value, err)
	}
}

func TestCatalogSkillUsesVerifiedDetailsWithoutArchiveOrPersistentSnapshot(t *testing.T) {
	f := newDetailsFixture(t, "")
	request := GetCatalogPluginSkill{Key: "recovery", SkillKey: "skills/demo", SourceCommit: f.commit, ArchiveSHA256: f.archiveHash}
	value, err := request.Output(f.ctx)
	if err != nil || value.(PluginSkillResponse).Content != f.skill {
		t.Fatalf("catalog skill unavailable: %+v %v", value, err)
	}
	response := value.(PluginSkillResponse)
	if len(response.Resources) != 1 || response.Version.ID != "" {
		t.Fatalf("public skill metadata incomplete: %+v", response)
	}
	request.FilePath = "SKILL.md"
	value, err = request.Output(f.ctx)
	if err != nil || value.(PluginSkillResponse).File.Content != f.skill {
		t.Fatalf("public skill raw file unavailable: %+v %v", value, err)
	}
	for _, path := range []string{"references/rules.md", "../plugin.json"} {
		request.FilePath = path
		if _, err := request.Output(f.ctx); !errors.Is(err, status_error.PluginContentInvalid) {
			t.Fatalf("undeclared details resource accepted: %s %v", path, err)
		}
	}
	if f.archiveRequests.Load() != 0 {
		t.Fatal("public skill downloaded archive")
	}
	entries, err := os.ReadDir(stagedRoot())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("public skill left persistent snapshot")
	}
}

func TestPluginSkillRejectsUnsupportedManifestEntry(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, ".codex-plugin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".codex-plugin/plugin.json"), []byte(`{"name":"unsupported","version":"1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := packageplugin.Stage(t.TempDir(), source); !errors.Is(err, status_error.PluginManifestInvalid) {
		t.Fatal("unsupported manifest entry accepted", err)
	}
}

func TestCanonicalSkillKeepsIdenticalPublishedAndInstalledContent(t *testing.T) {
	ctx := pluginAPIContext(t)
	guide := "---\nname: review\ndescription: Review changes\n---\n" + strings.Repeat("Complete business instructions.\n", 300)
	source := t.TempDir()
	for name, body := range map[string]string{"plugin.json": `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"canonical","version":"1.0.0"}`, "skills/review/SKILL.md": guide, "skills/review/references/rules.md": "Canonical reference"} {
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	stage, err := packageplugin.Stage(stagedRoot(), source)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := prepareCatalogFixture(t, ctx, stage, "", false)
	if err != nil {
		t.Fatal(err)
	}
	before := &GetCatalogPluginSkill{Key: "canonical", SkillKey: "skills/review"}
	output, err := before.Output(ctx)
	if err != nil || output.(PluginSkillResponse).Content != guide {
		t.Fatal("published skill changed canonical content", err)
	}
	if _, err := (&InstallPlugin{PluginID: detail.Plugin.ID, Body: InstallPluginRequest{VersionID: detail.ResolvedVersionID, WakerIDs: []string{"w1"}}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	after := &GetPluginSkill{PluginID: detail.Plugin.ID, VersionID: detail.ResolvedVersionID, WakerID: "w1", Key: "skills/review"}
	output, err = after.Output(ctx)
	if err != nil || output.(PluginSkillResponse).Content != guide {
		t.Fatal("installed skill changed canonical content", err)
	}
	after.FilePath = "references/rules.md"
	output, err = after.Output(ctx)
	if err != nil || output.(PluginSkillResponse).File.Content != "Canonical reference" {
		t.Fatal("installed resource root lost", err)
	}
	before.SourceCommit = strings.Repeat("d", 40)
	if _, err := before.Output(ctx); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatal("public skill accepted another source", err)
	}
}
