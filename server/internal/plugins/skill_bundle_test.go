package plugins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/skills"
)

func skillBundleFixture(t *testing.T, files map[string]string, names map[string]string) skills.StagedBatch {
	t.Helper()
	root := t.TempDir()
	content := filepath.Join(root, "content")
	for path, body := range files {
		path = filepath.Join(content, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	batch := skills.StagedBatch{Root: root, Source: skills.Source{Kind: constants.SkillSourceZip, Location: filepath.Join(t.TempDir(), "private-random-upload.zip")}}
	for path, name := range names {
		directory := filepath.Join(content, path)
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: Real skill\n---\nUse the bundled resources.\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		batch.Packages = append(batch.Packages, skills.Package{Name: name, Description: "Real skill", RelativePath: path, Directory: directory})
	}
	return batch
}

func TestBuildSkillBundlePreservesSelectedResourcesAndLicenses(t *testing.T) {
	batch := skillBundleFixture(t, map[string]string{
		"LICENSE": "Root license", "catalog/NOTICE": "Ancestor notice", "LICENSES/MIT.txt": "License directory",
		"catalog/review/resources/data.bin": "\x00\x01\x02", "catalog/review/bin/tool": "#!/bin/sh\ntrue\n", "catalog/review/LICENSE": "Skill-specific license",
		"catalog/test/scripts/test.sh": "#!/bin/sh\ntrue\n", "unused/bin/other": "unused executable", "mcp.json": "unselected MCP", ".neuxbot/auth.mjs": "unselected authentication",
	}, map[string]string{"catalog/review": "review", "catalog/test": "test-runner", "unused": "unused"})
	for _, path := range []string{"catalog/review/bin/tool", "unused/bin/other"} {
		if err := os.Chmod(filepath.Join(batch.Root, "content", path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	parent := t.TempDir()
	stage, provenance, err := BuildSkillBundle(parent, batch, []string{"catalog/test", "catalog/review"}, "testbundle", "审阅与测试", "选择的实际技能")
	if err != nil {
		t.Fatal(err)
	}
	if stage.Package.Manifest.Name == "审阅与测试" || stage.Package.Presentation.DisplayName != "审阅与测试" || stage.Package.Manifest.Schema != constants.PluginManifestSchema || len(stage.Package.Skills) != 2 || len(stage.Package.Servers) != 0 || stage.Package.RequiresTrust() || stage.Package.Manifest.License != "" {
		t.Fatalf("not a standard skills-only package: %+v", stage.Package)
	}
	for path, expected := range map[string]string{"skills/review/resources/data.bin": "\x00\x01\x02", "skills/review/LICENSE": "Skill-specific license", "docs/source-licenses/LICENSE": "Root license", "docs/source-licenses/catalog/NOTICE": "Ancestor notice", "docs/source-licenses/LICENSES/MIT.txt": "License directory"} {
		body, err := os.ReadFile(filepath.Join(stage.Root, path))
		if err != nil || string(body) != expected {
			t.Fatalf("lost resource %s: %v", path, err)
		}
	}
	information, err := os.Stat(filepath.Join(stage.Root, "skills/review/bin/tool"))
	if err != nil || information.Mode().Perm()&0o111 == 0 {
		t.Fatal("selected executable mode was lost", err)
	}
	for _, path := range []string{"skills/unused", "mcp.json", ".neuxbot/auth.mjs", "provenance.json"} {
		if _, err := os.Stat(filepath.Join(stage.Root, path)); !os.IsNotExist(err) {
			t.Fatalf("unselected or invented source retained: %s", path)
		}
	}
	encoded, _ := json.Marshal(provenance)
	if strings.Contains(string(encoded), batch.Root) || strings.Contains(string(encoded), "private-random-upload") || strings.Contains(string(encoded), "sourceCommit") {
		t.Fatal("private or invented provenance escaped")
	}

}

func TestBuildSkillBundleRejectsSelectionAndResourceBoundaryViolations(t *testing.T) {
	for _, mode := range []string{"duplicate-id", "unknown-id", "duplicate-name", "nested", "hidden-nested", "cross-link", "internal-link", "ancestor-link", "license-skill"} {
		t.Run(mode, func(t *testing.T) {
			names := map[string]string{"one": "one", "two": "two", "unused": "unused"}
			files := map[string]string{"one/resource.txt": "resource", "two/resource.txt": "other"}
			if mode == "duplicate-name" {
				names["two"] = "one"
			}
			if mode == "nested" {
				names["one/child"] = "child"
			}
			if mode == "hidden-nested" {
				files["one/references/deep/SKILL.md"] = "nested entry"
			}
			if mode == "license-skill" {
				files["LICENSES/SKILL.md"] = "not a license"
			}
			batch := skillBundleFixture(t, files, names)
			ids := []string{"one", "two"}
			if mode == "duplicate-id" {
				ids = []string{"one", "one"}
			}
			if mode == "unknown-id" {
				ids = []string{"one", "missing"}
			}
			if mode == "cross-link" || mode == "internal-link" {
				link := "../two/resource.txt"
				if mode == "internal-link" {
					link = "resource.txt"
				}
				if err := os.Symlink(link, filepath.Join(batch.Root, "content/one/linked.txt")); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "ancestor-link" {
				if err := os.Symlink("one", filepath.Join(batch.Root, "content/alias")); err != nil {
					t.Fatal(err)
				}
				batch.Packages = append(batch.Packages, skills.Package{Name: "alias", Description: "Real skill", RelativePath: "alias", Directory: filepath.Join(batch.Root, "content/alias")})
				ids = []string{"alias", "two"}
			}
			_, _, err := BuildSkillBundle(t.TempDir(), batch, ids, "testbundle", "Bundle", "")
			if mode == "internal-link" {
				if err != nil {
					t.Fatal("contained resource link was rejected", err)
				}
				return
			}
			want := status_error.PluginSkillSelectionConflict
			switch mode {
			case "duplicate-id", "unknown-id":
				want = status_error.BadRequest
			case "ancestor-link":
				want = status_error.SkillImportIntegrityInvalid
			case "cross-link":
				want = status_error.SkillContentInvalid
			}
			if !errors.Is(err, want) {
				t.Fatalf("got %v, want %v", err, want)
			}
		})
	}
}
