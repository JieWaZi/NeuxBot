package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	managedskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func newInstaller(t *testing.T) Installer {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{
		Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "skills.db"), Migrate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return Installer{
		DataDirectory: t.TempDir(), Skills: controllers.NewSkillController(database.DB),
		Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB)),
	}
}

func ordinaryCatalogFixture(t *testing.T) {
	t.Helper()
	original := bundle
	fixture, err := json.Marshal(map[string]any{"skills": []definition{refreshFixture()}})
	if err != nil {
		t.Fatal(err)
	}
	bundle = fixture
	t.Cleanup(func() { bundle = original })
}

func TestInstallPreservesCompleteResourcesAndUserChoices(t *testing.T) {
	ordinaryCatalogFixture(t)
	installer := newInstaller(t)
	ctx := context.Background()
	if err := installer.EnsureCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	items, err := definitions()
	if err != nil || len(items) == 0 {
		t.Fatalf("missing published skills: %v", err)
	}
	for _, item := range items {
		stored, err := installer.Skills.GetPackage(ctx, item.ID)
		if err != nil || !stored.BuiltIn || stored.Source != constants.SkillSourceBuiltin {
			t.Fatalf("builtin package: %+v %v", stored, err)
		}
		version, err := installer.Skills.GetVersion(ctx, stored.CurrentVersionID)
		if err != nil {
			t.Fatal(err)
		}
		for _, resource := range item.Files {
			file := filepath.Join(installer.DataDirectory, version.ContentPath, resource.Path)
			actual, err := os.ReadFile(file)
			if err != nil || !bytes.Equal(actual, resource.Data) {
				t.Fatalf("bundled resource did not survive installation: %s %v", resource.Path, err)
			}
		}
		metadata, err := managedskills.ReadMetadata(filepath.Join(installer.DataDirectory, version.ContentPath))
		if err != nil || metadata.Name != item.Directory {
			t.Fatalf("runtime metadata: %+v %v", metadata, err)
		}
		if err := EnableForWaker(ctx, installer.Skills, "ally", []string{item.Directory}); err != nil {
			t.Fatal(err)
		}
		enabled, err := installer.Skills.ListEnabled(ctx, "ally")
		if err != nil || len(enabled) == 0 {
			t.Fatalf("default relation missing: %+v %v", enabled, err)
		}
		if err := installer.Skills.UpsertWakerSkill(ctx, models.WakerSkill{
			ID: util.New(), WakerID: "ally", SkillPackageID: item.ID, Enabled: false,
		}); err != nil {
			t.Fatal(err)
		}
		// 用真正的新版本模拟用户修改，重启安装不得推进或覆盖当前版本。
		customSource := t.TempDir()
		customContent := []byte("---\nname: " + item.Directory + "\ndescription: User customized instructions\n---\nCustom behavior.\n")
		if err := os.WriteFile(filepath.Join(customSource, "SKILL.md"), customContent, 0o600); err != nil {
			t.Fatal(err)
		}
		contentPath, digest, err := managedskills.CopyVersion(installer.DataDirectory, item.ID, "9.0.0", customSource)
		if err != nil {
			t.Fatal(err)
		}
		custom := models.SkillVersion{
			ID: util.New(), SkillPackageID: item.ID, Version: "9.0.0", Name: item.Directory,
			ContentPath: contentPath, ContentDigest: digest, Operation: "edit",
		}
		if err := installer.Skills.CreateVersion(ctx, custom, true, stored.CurrentVersionID); err != nil {
			t.Fatal(err)
		}
		if err := installer.EnsureCatalog(ctx); err != nil {
			t.Fatal(err)
		}
		if err := EnableForWaker(ctx, installer.Skills, "ally", []string{item.Directory}); err != nil {
			t.Fatal(err)
		}
		updated, err := installer.Skills.GetPackage(ctx, item.ID)
		if err != nil || updated.CurrentVersionID != custom.ID {
			t.Fatalf("bootstrap replaced customized version: %+v %v", updated, err)
		}
		versions, err := installer.Skills.ListVersions(ctx, item.ID)
		if err != nil || len(versions) != 2 {
			t.Fatalf("bootstrap duplicated versions: %+v %v", versions, err)
		}
	}
	for _, wakerID := range []string{"ally", "unrelated-partner"} {
		enabled, err := installer.Skills.ListEnabled(ctx, wakerID)
		if err != nil || len(enabled) != 0 {
			t.Fatalf("unexpected default enabled skills for %s: %+v %v", wakerID, enabled, err)
		}
	}
}

// rejectingVersionCatalog 在初始版本提交处制造故障，验证包元数据和文件的补偿。
type rejectingVersionCatalog struct {
	// SkillController 复用真实数据库操作，仅替换版本提交边界。
	controllers.SkillController
}

func (rejectingVersionCatalog) CreateVersion(context.Context, models.SkillVersion, bool, string) error {
	return status_error.Conflict
}

func TestFailedInstallationRollsBackAndCanRetry(t *testing.T) {
	ordinaryCatalogFixture(t)
	installer := newInstaller(t)
	actual := installer.Skills
	installer.Skills = rejectingVersionCatalog{SkillController: actual}
	ctx := context.Background()
	if err := installer.EnsureCatalog(ctx); !errors.Is(err, status_error.Conflict) {
		t.Fatalf("expected simulated commit failure: %v", err)
	}
	visible, err := actual.ListVisible(ctx, "ally", "")
	if err != nil || len(visible) != 0 {
		t.Fatalf("partial package committed: %+v %v", visible, err)
	}
	installer.Skills = actual
	if err := installer.EnsureCatalog(ctx); err != nil {
		t.Fatalf("failed installation left files preventing retry: %v", err)
	}
}

func TestPublishedSkillRefreshesChangedResourcesWithoutChangingCatalogVersion(t *testing.T) {
	for _, changedPath := range []string{"SKILL.md", "references/guide.md", "scripts/check.sh"} {
		t.Run(changedPath, func(t *testing.T) {
			installer := newInstaller(t)
			ctx := context.Background()
			item := refreshFixture()
			if err := installer.ensureDefinition(ctx, item); err != nil {
				t.Fatal(err)
			}
			before, _ := installer.Skills.GetPackage(ctx, item.ID)
			old, _ := installer.Skills.GetVersion(ctx, before.CurrentVersionID)
			if err := installer.Skills.UpsertWakerSkill(ctx, models.WakerSkill{
				ID: util.New(), WakerID: "ally", SkillPackageID: item.ID, Enabled: false,
			}); err != nil {
				t.Fatal(err)
			}
			for index := range item.Files {
				if item.Files[index].Path == changedPath {
					if changedPath == "scripts/check.sh" {
						item.Files[index].Executable = true
					} else {
						item.Files[index].Data = append(item.Files[index].Data, []byte("\nUpdated instructions.\n")...)
					}
				}
			}
			for attempt := 0; attempt < 2; attempt++ {
				if err := installer.ensureDefinition(ctx, item); err != nil {
					t.Fatal(err)
				}
			}
			after, _ := installer.Skills.GetPackage(ctx, item.ID)
			current, _ := installer.Skills.GetVersion(ctx, after.CurrentVersionID)
			if current.ID == old.ID || current.Version != "1.0.1" || current.ContentDigest == old.ContentDigest {
				t.Fatalf("published content did not refresh: old=%+v current=%+v", old, current)
			}
			versions, _ := installer.Skills.ListVersions(ctx, item.ID)
			if len(versions) != 2 {
				t.Fatalf("restart duplicated version history: %+v", versions)
			}
			original, err := os.ReadFile(filepath.Join(installer.DataDirectory, old.ContentPath, changedPath))
			if err != nil {
				t.Fatal(err)
			}
			if changedPath != "scripts/check.sh" && bytes.Contains(original, []byte("Updated instructions")) {
				t.Fatal("refresh overwrote immutable history")
			}
			for _, file := range item.Files {
				path := filepath.Join(installer.DataDirectory, current.ContentPath, file.Path)
				content, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(content, file.Data) {
					t.Fatalf("updated resource missing: %s %v", file.Path, err)
				}
				info, err := os.Stat(path)
				if err != nil || (info.Mode().Perm()&0o111 != 0) != file.Executable {
					t.Fatalf("executable permission changed: %s %v", file.Path, err)
				}
			}
			enabled, err := installer.Skills.ListEnabled(ctx, "ally")
			if err != nil || len(enabled) != 0 {
				t.Fatalf("refresh changed disabled preference: %+v %v", enabled, err)
			}
		})
	}
}

func TestFailedRefreshKeepsCurrentVersionAndCanRetry(t *testing.T) {
	installer := newInstaller(t)
	ctx := context.Background()
	item := refreshFixture()
	if err := installer.ensureDefinition(ctx, item); err != nil {
		t.Fatal(err)
	}
	before, _ := installer.Skills.GetPackage(ctx, item.ID)
	item.Files[1].Data = []byte("Updated reference.\n")
	actual := installer.Skills
	installer.Skills = rejectingVersionCatalog{SkillController: actual}
	if err := installer.ensureDefinition(ctx, item); !errors.Is(err, status_error.Conflict) {
		t.Fatalf("refresh did not reach version commit: %v", err)
	}
	after, _ := actual.GetPackage(ctx, item.ID)
	versions, _ := actual.ListVersions(ctx, item.ID)
	if after.CurrentVersionID != before.CurrentVersionID || len(versions) != 1 {
		t.Fatal("failed refresh changed authoritative version")
	}
	if _, err := os.Stat(filepath.Join(installer.DataDirectory, "skills", "versions", item.ID, "1.0.1")); !os.IsNotExist(err) {
		t.Fatalf("uncommitted refresh directory was not removed: %v", err)
	}
	installer.Skills = actual
	if err := installer.ensureDefinition(ctx, item); err != nil {
		t.Fatalf("refresh retry failed: %v", err)
	}
}

func refreshFixture() definition {
	return definition{
		ID: "00000000000000000000000000000099", Directory: "refresh-test", Version: "1.0.0",
		Files: []resource{
			{Path: "SKILL.md", Data: []byte("---\nname: refresh-test\ndescription: Published instructions\n---\nOriginal instructions.\n")},
			{Path: "references/guide.md", Data: []byte("Original reference.\n")},
			{Path: "scripts/check.sh", Data: []byte("#!/bin/sh\nexit 0\n")},
		},
	}
}

func TestRefreshPreservesCustomizedCurrentVersion(t *testing.T) {
	installer := newInstaller(t)
	ctx := context.Background()
	item := refreshFixture()
	if err := installer.ensureDefinition(ctx, item); err != nil {
		t.Fatal(err)
	}
	stored, _ := installer.Skills.GetPackage(ctx, item.ID)
	customSource := t.TempDir()
	content := []byte("---\nname: refresh-test\ndescription: User instructions\n---\nKeep my changes.\n")
	if err := os.WriteFile(filepath.Join(customSource, "SKILL.md"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	path, digest, err := managedskills.CopyVersion(installer.DataDirectory, item.ID, "2.0.0", customSource)
	if err != nil {
		t.Fatal(err)
	}
	custom := models.SkillVersion{ID: util.New(), SkillPackageID: item.ID, Version: "2.0.0", Name: item.Directory, Operation: "edit", ContentPath: path, ContentDigest: digest}
	if err := installer.Skills.CreateVersion(ctx, custom, true, stored.CurrentVersionID); err != nil {
		t.Fatal(err)
	}
	item.Files[1].Data = []byte("New bundled reference.\n")
	if err := installer.ensureDefinition(ctx, item); err != nil {
		t.Fatal(err)
	}
	after, _ := installer.Skills.GetPackage(ctx, item.ID)
	versions, _ := installer.Skills.ListVersions(ctx, item.ID)
	actual, err := os.ReadFile(filepath.Join(installer.DataDirectory, path, "SKILL.md"))
	if err != nil || !bytes.Equal(actual, content) || after.CurrentVersionID != custom.ID || len(versions) != 2 {
		t.Fatalf("published update changed user version: %+v %v", after, err)
	}
}

func TestBuiltinRefreshUsesUnoccupiedIncreasingVersion(t *testing.T) {
	for _, tc := range []struct {
		// name 标识本次版本排序情形。
		name string
		// published 是随应用声明的初始版本。
		published string
		// history 是已经使用的不可变版本号。
		history []string
		// want 是本次更新应该使用的新版本号。
		want string
	}{
		{name: "same release", published: "1.0.0", history: []string{"1.0.1", "1.0.0"}, want: "1.0.2"},
		{name: "higher release", published: "2.0.0", history: []string{"1.0.9"}, want: "2.0.0"},
		{name: "numeric ordering", published: "1.0.0", history: []string{"1.0.9", "1.0.10"}, want: "1.0.11"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			versions := make([]models.SkillVersion, len(tc.history))
			for index, number := range tc.history {
				versions[index].Version = number
			}
			actual, err := nextBuiltinVersion(tc.published, versions)
			if err != nil || actual != tc.want {
				t.Fatalf("version=%s want=%s error=%v", actual, tc.want, err)
			}
		})
	}
}
