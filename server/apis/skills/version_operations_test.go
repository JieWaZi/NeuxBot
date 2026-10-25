package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	internalSkills "github.com/JieWaZi/NeuxBot/server/internal/skills"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// skillVersionAPIFixture 保存版本操作 API 的真实持久化测试环境。
type skillVersionAPIFixture struct {
	// context 是 Operator 和 Repository 共用的 Gin 测试上下文。
	context *gin.Context
	// waker 是拥有私有技能的测试伙伴。
	waker models.Waker
	// skill 是等待执行版本操作的测试技能包。
	skill models.SkillPackage
	// historical 是恢复操作选择的历史版本。
	historical models.SkillVersion
	// current 是操作开始时的当前版本。
	current models.SkillVersion
}

func TestRestoreSkillVersionUsesServerCalculatedNextPatch(t *testing.T) {
	fixture := newSkillVersionAPIFixture(t)
	operator := &RestoreSkillVersion{
		WakerID:   fixture.waker.ID,
		SkillID:   fixture.skill.ID,
		VersionID: fixture.historical.ID,
	}
	if err := operator.Validate(fixture.context); err != nil {
		t.Fatalf("Validate() error = %v, want server-calculated version", err)
	}
	output, err := operator.Output(fixture.context)
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}
	version, ok := output.(models.SkillVersion)
	if !ok {
		t.Fatalf("Output() type = %T, want models.SkillVersion", output)
	}
	if version.Version != "1.4.10" {
		t.Fatalf("restored version = %q, want 1.4.10", version.Version)
	}
}

func TestCompareSemVerSupportsSegmentsLargerThanInt64(t *testing.T) {
	if comparison := compareSemVer("9223372036854775808.0.0", "9223372036854775807.999.999"); comparison <= 0 {
		t.Fatalf("compareSemVer() = %d, want positive", comparison)
	}
	if comparison := compareSemVer("1.18446744073709551616.0", "1.18446744073709551615.999"); comparison <= 0 {
		t.Fatalf("compareSemVer() = %d, want positive", comparison)
	}
}

func TestCreateSkillVersionSyncsMetadataFromSkillMarkdown(t *testing.T) {
	fixture := newSkillVersionAPIFixture(t)
	updatedContent := "---\nname: review-workflow\ndescription: 检查关键改动\n---\nupdated body\n"
	output, err := (&CreateSkillVersion{
		WakerID: fixture.waker.ID,
		SkillID: fixture.skill.ID,
		Body: CreateSkillVersionRequest{
			Version: "1.5.0",
			Content: updatedContent,
		},
	}).Output(fixture.context)
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}
	version, ok := output.(models.SkillVersion)
	if !ok {
		t.Fatalf("Output() type = %T, want models.SkillVersion", output)
	}
	if version.Name != "review-workflow" || version.Description != "检查关键改动" {
		t.Fatalf("version metadata = %+v, want review-workflow / 检查关键改动", version)
	}
}

func TestPullSkillUpdateFollowsSavedSubpathWhenMetadataNameChanges(t *testing.T) {
	fixture := newSkillVersionAPIFixture(t)
	importRoot := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(importRoot, "SKILL.md"),
		[]byte("---\nname: renamed-review\ndescription: Renamed review skill\n---\nupdated\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	previousImport := importSkillSource
	t.Cleanup(func() { importSkillSource = previousImport })
	importSkillSource = func(
		context.Context,
		string,
		internalSkills.Source,
	) ([]internalSkills.Package, string, error) {
		return []internalSkills.Package{{
			Name: "renamed-review", Description: "Renamed review skill",
			Directory: importRoot, RelativePath: "skills/review",
		}}, importRoot, nil
	}

	output, err := (&PullSkillUpdate{
		WakerID: fixture.waker.ID,
		SkillID: fixture.skill.ID,
		Body:    PullSkillUpdateRequest{Version: "1.5.0"},
	}).Output(fixture.context)
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}
	version, ok := output.(models.SkillVersion)
	if !ok {
		t.Fatalf("Output() type = %T, want models.SkillVersion", output)
	}
	if version.Version != "1.5.0" || version.Name != "renamed-review" {
		t.Fatalf("pulled version = %+v, want renamed-review v1.5.0", version)
	}
	updated, err := global.SkillControllerManager.GetPackage(fixture.context, fixture.skill.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "review" {
		t.Fatalf("updated skill name = %q, want review", updated.Name)
	}
}

func newSkillVersionAPIFixture(t *testing.T) skillVersionAPIFixture {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{
		Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "skill-version-api.db"), Migrate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	previousWakers := global.WakerControllerManager
	previousSkills := global.SkillControllerManager
	previousDataDirectory := global.Config.DataDirectory
	t.Cleanup(func() {
		global.WakerControllerManager = previousWakers
		global.SkillControllerManager = previousSkills
		global.Config.DataDirectory = previousDataDirectory
	})
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.SkillControllerManager = controllers.NewSkillController(database.DB)
	global.Config.DataDirectory = t.TempDir()
	ctx, _ := gin.CreateTestContext(nil)

	waker := models.Waker{ID: "waker-version-operations", Name: "版本测试伙伴"}
	if err := global.WakerControllerManager.Create(ctx, waker); err != nil {
		t.Fatal(err)
	}
	skill := models.SkillPackage{
		ID: "skill-version-operations", Name: "review", Scope: constants.SkillScopePrivate,
		Source: constants.SkillSourceGit, OwnerWakerID: waker.ID,
		GitURL: "https://example.com/review.git", GitSubpath: "skills/review",
	}
	if err := global.SkillControllerManager.CreatePackage(ctx, skill); err != nil {
		t.Fatal(err)
	}

	createVersion := func(id string, version string, body string, makeCurrent bool) models.SkillVersion {
		t.Helper()
		source := t.TempDir()
		content := "---\nname: review\ndescription: Review changes\n---\n" + body
		if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		contentPath, digest, err := internalSkills.CopyVersion(
			global.Config.DataDirectory,
			skill.ID,
			version,
			source,
		)
		if err != nil {
			t.Fatal(err)
		}
		created := models.SkillVersion{
			ID: id, SkillPackageID: skill.ID, Version: version, Name: skill.Name,
			Description: "Review changes", ContentPath: contentPath, ContentDigest: digest,
		}
		if err := global.SkillControllerManager.CreateVersion(ctx, created, makeCurrent, ""); err != nil {
			t.Fatal(err)
		}
		return created
	}
	historical := createVersion("version-historical", "1.2.0", "historical\n", false)
	current := createVersion("version-current", "1.4.9", "current\n", true)
	skill.CurrentVersionID = current.ID
	return skillVersionAPIFixture{
		context: ctx, waker: waker, skill: skill, historical: historical, current: current,
	}
}
