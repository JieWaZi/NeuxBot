package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
)

func TestPluginSkillReadsOnlyRequestedImmutableVersionAndVisibleOwner(t *testing.T) {
	ctx := pluginAPIContext(t)
	source := t.TempDir()
	for filename, content := range map[string]string{
		".codex-plugin/plugin.json":          `{"name":"reader","version":"1.0.0","skills":"./skills/"}`,
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
	detail, err := prepareCatalogFixture(t, ctx, stage, "w1", "", false)
	if err != nil {
		t.Fatal(err)
	}
	version := detail.Versions[0]
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
		t.Fatalf("private skill leaked to another owner: %v", err)
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
			".codex-plugin/plugin.json":   `{"name":"history-reader","version":"` + version + `"}`,
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
		value, err := prepareCatalogFixture(t, ctx, stage, "", target, false)
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

func TestCatalogSkillVerifiesArchiveAndReleasesTemporarySnapshot(t *testing.T) {
	ctx := pluginAPIContext(t)
	root := t.TempDir()
	t.Setenv("ALLY_PLUGIN_CATALOG_URL", root)
	if err := os.MkdirAll(filepath.Join(root, "catalog"), 0o700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "catalog", "reader.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range map[string]string{".claude-plugin/plugin.json": `{"name":"reader","version":"1.2.3"}`, "skills/reports/SKILL.md": "---\nname: reports\ndescription: Reports\n---\n# Catalog skill\n", "skills/reports/references/rules.md": "Catalog resource"} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(bytes)
	index, _ := json.Marshal(map[string]any{"schemaVersion": 1, "entries": []map[string]any{{"key": "reader", "directory": "catalog/reader.zip", "sourceCommit": strings.Repeat("a", 40), "archiveSHA256": hex.EncodeToString(digest[:]), "package": map[string]any{"manifest": map[string]any{"name": "reader"}}}}})
	if err := os.WriteFile(filepath.Join(root, "catalog.json"), index, 0o600); err != nil {
		t.Fatal(err)
	}
	request := GetCatalogPluginSkill{Key: "reader", SkillKey: "skills/reports"}
	value, err := request.Output(ctx)
	if err != nil || !strings.Contains(value.(PluginSkillResponse).Content, "# Catalog skill") {
		t.Fatalf("catalog skill not readable: %+v %v", value, err)
	}
	response := value.(PluginSkillResponse)
	if len(response.Resources) != 3 || response.File != nil || len(response.Versions) != 1 || response.Version.Version != "1.2.3" || response.Version.ID != "" {
		t.Fatalf("catalog skill details incomplete: %+v", response)
	}
	request.FilePath = "references/rules.md"
	value, err = request.Output(ctx)
	if err != nil || value.(PluginSkillResponse).File == nil || value.(PluginSkillResponse).File.Content != "Catalog resource" {
		t.Fatalf("catalog resource unavailable: %+v %v", value, err)
	}
	request.FilePath = "../plugin.json"
	if _, err := request.Output(ctx); !errors.Is(err, status_error.PluginContentInvalid) {
		t.Fatalf("catalog resource traversed skill root: %v", err)
	}
	request.FilePath = ""
	entries, err := os.ReadDir(stagedRoot())
	if err != nil || len(entries) != 0 {
		t.Fatalf("skill preview left a persistent snapshot: %+v %v", entries, err)
	}
	if err := os.WriteFile(archive, []byte("mutated archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := request.Output(ctx); !errors.Is(err, status_error.PluginContentInvalid) {
		t.Fatalf("catalog preview accepted an unverified archive: %v", err)
	}
}

func TestUpstreamSkillReadsFullPinnedZIPBeforeAndAfterInstallation(t *testing.T) {
	ctx := pluginAPIContext(t)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	files := map[string]string{
		"entry/.codex-plugin/plugin.json":  `{"name":"upstream-reader","version":"0.1.0"}`,
		"entry/.neuxally/integration.json": `{"nativePlugins":{"codex":{"path":"upstream/codex"},"claude":{"path":"upstream/claude"}}}`,
	}
	for _, target := range []string{"codex", "claude"} {
		files["upstream/"+target+"/."+target+"-plugin/plugin.json"] = `{"name":"upstream"}`
		files["upstream/"+target+"/skills/review/SKILL.md"] = "---\nname: review\ndescription: Review changes\n---\n# " + target + " complete guide\n" + strings.Repeat("Full upstream body.\n", 600)
		files["upstream/"+target+"/skills/review/references/rules.md"] = "Contained " + target + " resource"
	}
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive.Bytes()) }))
	defer server.Close()
	root := t.TempDir()
	sum := sha256.Sum256(archive.Bytes())
	commit := strings.Repeat("a", 40)
	digest := fmt.Sprintf("%x", sum)
	index := fmt.Sprintf(`{"schemaVersion":2,"entries":[{"key":"reader","name":"upstream-reader","version":"0.1.0","format":"codex","sourceCommit":%q,"archiveSHA256":%q,"archiveURL":%q,"packagePath":"entry"}]}`, commit, digest, server.URL+"/reader.zip")
	if err := os.WriteFile(filepath.Join(root, "catalog.json"), []byte(index), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ALLY_PLUGIN_CATALOG_URL", root)
	preview, err := (&GetCatalogPlugin{Key: "reader"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	entry := preview.(catalog.Entry)
	archivePath := filepath.Join(root, "reader.zip")
	if err := os.WriteFile(archivePath, archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	stage, err := packageplugin.StageSource(stagedRoot(), archivePath, "entry")
	if err != nil {
		t.Fatal(err)
	}
	for target, summary := range stage.Package.NativeImplementations {
		summary.Skills = nil
		summary.Servers = nil
		stage.Package.NativeImplementations[target] = summary
	}
	detail, err := persistPluginFixture(t, ctx, stage, "", entry.SourceKey, map[string]any{"sourceCommit": entry.SourceCommit, "archiveSHA256": entry.ArchiveSHA256, "archiveURL": entry.ArchiveURL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = (&InstallPlugin{PluginID: detail.Plugin.ID, Body: InstallPluginRequest{VersionID: detail.ResolvedVersionID, WakerIDs: []string{"w1"}}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	visible, err := (&GetPlugin{PluginID: detail.Plugin.ID, VersionID: detail.ResolvedVersionID}).Output(ctx)
	if err != nil || len(visible.(PluginDetailResponse).Versions[0].Package.NativeImplementations["codex"].Skills) != 0 {
		t.Fatalf("ordinary detail must preserve saved component snapshot: %v", err)
	}
	stored, err := global.PluginControllerManager.Version(ctx, detail.ResolvedVersionID)
	if err != nil || len(stored.Package.NativeImplementations["codex"].Skills) != 0 {
		t.Fatal("ordinary detail rewrote persistent runtime package")
	}
	for _, target := range []string{"codex", "claude"} {
		before := &GetCatalogPluginSkill{Key: "reader", SkillKey: "skills/review"}
		_ = json.Unmarshal([]byte(fmt.Sprintf(`{"Target":%q,"SourceCommit":%q,"ArchiveSHA256":%q}`, target, entry.SourceCommit, entry.ArchiveSHA256)), before)
		output, err := before.Output(ctx)
		if err != nil {
			t.Fatalf("before installation %s: %v", target, err)
		}
		expected := files["upstream/"+target+"/skills/review/SKILL.md"]
		if output.(PluginSkillResponse).Content != expected {
			t.Fatal("catalog upstream skill was truncated or selected another target")
		}
		after := &GetPluginSkill{PluginID: detail.Plugin.ID, VersionID: detail.ResolvedVersionID, WakerID: "w1", Key: "skills/review"}
		_ = json.Unmarshal([]byte(fmt.Sprintf(`{"Target":%q}`, target)), after)
		output, err = after.Output(ctx)
		if err != nil || output.(PluginSkillResponse).Content != expected {
			t.Fatalf("installed upstream full content differs: %s %v", target, err)
		}
		after.FilePath = "references/rules.md"
		output, err = after.Output(ctx)
		if err != nil || output.(PluginSkillResponse).File.Content != "Contained "+target+" resource" {
			t.Fatalf("upstream resource root differs: %v", err)
		}
		after.FilePath = "../../../../entry/.codex-plugin/plugin.json"
		if _, err = after.Output(ctx); err == nil {
			t.Fatal("target skill escaped contained directory")
		}
		_ = json.Unmarshal([]byte(`{"SourceCommit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`), before)
		if _, err = before.Output(ctx); !errors.Is(err, status_error.PluginVersionConflict) {
			t.Fatalf("catalog skill accepted a different displayed source: %v", err)
		}
	}
}

func TestCanonicalSkillReadsFullPinnedZIPBeforeAndAfterInstallation(t *testing.T) {
	ctx := pluginAPIContext(t)
	previousBridges := global.PluginBridges
	global.PluginBridges = commandbridge.NewRegistry(context.Background(), nil)
	t.Cleanup(func() {
		global.PluginBridges.Close()
		global.PluginBridges = previousBridges
	})
	guide := "---\nname: review\ndescription: Review changes\n---\n# Canonical complete guide\n" + strings.Repeat("Complete business instructions.\n", 300)
	files := map[string]string{
		"plugins/github/.codex-plugin/plugin.json":         `{"name":"github","version":"0.1.0"}`,
		"plugins/github/.mcp.json":                         `{"mcpServers":{"github":{"type":"http","url":"https://api.githubcopilot.com/mcp/"}}}`,
		"plugins/github/.neuxally/integration.json":        `{"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"node","args":["bridge.mjs"],"bindings":[{"runtime":"codex","server":"github","url":"https://api.githubcopilot.com/mcp/","header":"Authorization"},{"runtime":"claude","server":"github","url":"https://api.githubcopilot.com/mcp/","header":"Authorization"}]}}`,
		"plugins/github/bridge.mjs":                        `import readline from 'node:readline';for await(const line of readline.createInterface({input:process.stdin})){const r=JSON.parse(line);console.log(JSON.stringify({jsonrpc:'2.0',id:r.id,result:{state:'authorized',account:{configured:true,available:true,id:'test-user',scopes:[]}}}));}`,
		"plugins/github/skills/review/SKILL.md":            guide,
		"plugins/github/skills/review/references/rules.md": "Canonical reference",
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, body := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive.Bytes()) }))
	defer server.Close()
	root := t.TempDir()
	sum := sha256.Sum256(archive.Bytes())
	commit, digest := strings.Repeat("c", 40), fmt.Sprintf("%x", sum)
	index := fmt.Sprintf(`{"schemaVersion":2,"entries":[{"key":"github","name":"github","version":"0.1.0","format":"codex","sourceCommit":%q,"archiveSHA256":%q,"archiveURL":%q,"packagePath":"plugins/github"}]}`, commit, digest, server.URL+"/github.zip")
	if err := os.WriteFile(filepath.Join(root, "catalog.json"), []byte(index), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ALLY_PLUGIN_CATALOG_URL", root)
	before := &GetCatalogPluginSkill{Key: "github", SkillKey: "skills/review", SourceCommit: commit, ArchiveSHA256: digest}
	output, err := before.Output(ctx)
	if err != nil || output.(PluginSkillResponse).Content != guide {
		t.Fatal("canonical catalog skill did not use the full fixed package", err)
	}
	prepared, err := (&PrepareCatalogPlugin{Key: "github", Body: PrepareCatalogPluginRequest{TrustConfirmed: true}}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	detail := prepared.(PluginDetailResponse)
	if _, err := (&InstallPlugin{PluginID: detail.Plugin.ID, Body: InstallPluginRequest{VersionID: detail.ResolvedVersionID, WakerIDs: []string{"w1"}}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	after := &GetPluginSkill{PluginID: detail.Plugin.ID, VersionID: detail.ResolvedVersionID, WakerID: "w1", Key: "skills/review"}
	output, err = after.Output(ctx)
	if err != nil || output.(PluginSkillResponse).Content != guide {
		t.Fatal("installed canonical skill changed its fixed source", err)
	}
	after.FilePath = "references/rules.md"
	output, err = after.Output(ctx)
	if err != nil || output.(PluginSkillResponse).File.Content != "Canonical reference" {
		t.Fatal("canonical resource root was lost", err)
	}
	before.SourceCommit = strings.Repeat("d", 40)
	if _, err := before.Output(ctx); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatal("canonical skill accepted a different source", err)
	}
}
