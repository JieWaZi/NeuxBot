package controllers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

func TestSkillInstalledPartnerPreviewsStayBoundedAndUseRealProfiles(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	partners := []models.Waker{}
	for i := 0; i < 7; i++ {
		partners = append(partners, models.Waker{ID: fmt.Sprintf("partner-%d", i), Name: fmt.Sprintf("伙伴%d", i), Avatar: fmt.Sprintf("actual-avatar-%d", i)})
	}
	if err := db.Create(&partners).Error; err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	relations := []models.WakerSkill{}
	for i := 0; i < 101; i++ {
		id := fmt.Sprintf("skill-%03d", i)
		ids = append(ids, id)
		for j := 0; j < 6; j++ {
			relations = append(relations, models.WakerSkill{ID: fmt.Sprintf("relation-%d-%d", i, j), SkillPackageID: id, WakerID: partners[j].ID, Enabled: j%2 == 0})
		}
	}
	if err := db.CreateInBatches(&relations, 100).Error; err != nil {
		t.Fatal(err)
	}
	var queries, maxRows int64
	if err := db.Callback().Query().After("gorm:query").Register("test:skill_partner_reads", func(query *gorm.DB) {
		if !query.DryRun {
			queries++
			maxRows = max(maxRows, query.RowsAffected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	store := NewSkillController(db)
	for _, size := range []int{1, 101} {
		queries, maxRows = 0, 0
		previews, err := store.InstalledPartnerPreviews(ctx, ids[:size])
		if err != nil {
			t.Fatal(err)
		}
		if len(previews) != size || queries != int64(2*((size+99)/100)) || maxRows > 400 {
			t.Fatalf("unbounded preview: skills=%d queries=%d rows=%d", len(previews), queries, maxRows)
		}
		for _, id := range ids[:size] {
			if len(previews[id]) != constants.SkillPartnerPreviewLimit {
				t.Fatalf("preview for %s = %+v", id, previews[id])
			}
			for i, partner := range previews[id] {
				if partner.ID != partners[i].ID || partner.Avatar != partners[i].Avatar || partner.Name != partners[i].Name {
					t.Fatalf("incorrect partner: %+v", partner)
				}
			}
		}
	}
	counts, err := store.InstalledCounts(ctx, ids[:1])
	if err != nil || counts[ids[0]] != 6 {
		t.Fatalf("preview changed total usage: %v %v", counts, err)
	}
	var plan []struct {
		// Detail 是 SQLite 返回的实际查询计划说明。
		Detail string
	}
	if err := db.Raw("EXPLAIN QUERY PLAN SELECT skill_package_id, waker_id, ROW_NUMBER() OVER (PARTITION BY skill_package_id ORDER BY waker_id) FROM waker_skills WHERE skill_package_id IN ?", ids).Find(&plan).Error; err != nil {
		t.Fatal(err)
	}
	indexed := false
	for _, step := range plan {
		indexed = indexed || strings.Contains(step.Detail, "idx_skill_installed_partners")
	}
	if !indexed {
		t.Fatalf("preview query lost its covering index: %+v", plan)
	}
}

func TestSkillInstalledPartnersReadCompleteListWithBatchedPublicProfiles(t *testing.T) {
	db := newTestDB(t)
	partners := make([]models.Waker, 0, 106)
	relations := make([]models.WakerSkill, 0, 105)
	for index := 0; index < 106; index++ {
		id := fmt.Sprintf("partner-%03d", index)
		partners = append(partners, models.Waker{ID: id, Name: id, Avatar: "avatar-" + id, Bio: "private profile text"})
		if index < 105 {
			relations = append(relations, models.WakerSkill{ID: "relation-" + id, WakerID: id, SkillPackageID: "complete-list", Enabled: index%2 == 0})
		}
	}
	if err := db.CreateInBatches(&partners, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.CreateInBatches(&relations, 100).Error; err != nil {
		t.Fatal(err)
	}
	queryCount := 0
	if err := db.Callback().Query().After("gorm:query").Register("test:complete_skill_partner_reads", func(query *gorm.DB) {
		if !query.DryRun {
			queryCount++
		}
	}); err != nil {
		t.Fatal(err)
	}
	installed, err := NewSkillController(db).InstalledPartners(context.Background(), "complete-list")
	if err != nil || len(installed) != 105 || queryCount != 3 {
		t.Fatalf("complete partner read = %d partners, %d queries, error = %v", len(installed), queryCount, err)
	}
	for index, partner := range installed {
		if partner.ID != partners[index].ID || partner.Name != partners[index].Name || partner.Avatar != partners[index].Avatar || partner.Bio != "" {
			t.Fatalf("public profile read changed order, lost disabled relation, or leaked fields: %+v", partner)
		}
		if partner.Enabled != (index%2 == 0) {
			t.Fatalf("unexpected installation state for %s: %v", partner.ID, partner.Enabled)
		}
	}
}

func TestSkillControllerUsesOnlyActualInstallationAndEnabledState(t *testing.T) {
	controller := NewSkillController(newTestDB(t))
	ctx := context.Background()
	skill := models.SkillPackage{ID: "installed-skill", Name: "审查", Source: constants.SkillSourceGit, CurrentVersionID: "installed-version"}
	if err := controller.CreatePackage(ctx, skill); err != nil {
		t.Fatal(err)
	}
	if err := controller.CreateVersion(ctx, models.SkillVersion{ID: skill.CurrentVersionID, SkillPackageID: skill.ID, Version: "1.0.0", ContentPath: "skills/versions/installed/1", ContentDigest: "digest"}, true, skill.CurrentVersionID); err != nil {
		t.Fatal(err)
	}
	items, err := controller.ListInstalled(ctx, "partner", "")
	if err != nil || len(items) != 0 {
		t.Fatalf("library resource implicitly installed: %+v %v", items, err)
	}
	if err := controller.EnsureWakerSkill(ctx, models.WakerSkill{ID: "relation", WakerID: "partner", SkillPackageID: skill.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	items, err = controller.ListInstalled(ctx, "partner", "")
	if err != nil || len(items) != 1 || items[0].CurrentVersion.ID != skill.CurrentVersionID || !items[0].Enabled {
		t.Fatalf("explicit installation absent: %+v %v", items, err)
	}
	if err := controller.UpsertWakerSkill(ctx, models.WakerSkill{WakerID: "partner", SkillPackageID: skill.ID, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := controller.EnsureWakerSkill(ctx, models.WakerSkill{ID: "reinstall", WakerID: "partner", SkillPackageID: skill.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	enabled, err := controller.ListEnabled(ctx, "partner")
	if err != nil || len(enabled) != 0 {
		t.Fatalf("reinstall overwrote disabled preference: %+v %v", enabled, err)
	}
	if err := controller.DeleteByWaker(ctx, "partner"); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.GetPackage(ctx, skill.ID); err != nil {
		t.Fatalf("deleting partner removed library skill: %v", err)
	}
	if _, err := controller.GetVersion(ctx, skill.CurrentVersionID); err != nil {
		t.Fatalf("deleting partner removed library version: %v", err)
	}
	items, err = controller.ListInstalled(ctx, "partner", "")
	if err != nil || len(items) != 0 {
		t.Fatalf("deleting partner retained installation: %+v %v", items, err)
	}
}

func TestSkillControllerCurrentVersionSynchronizesVersionedMetadata(t *testing.T) {
	controller := NewSkillController(newTestDB(t))
	ctx := context.Background()
	skill := models.SkillPackage{
		ID:          "skill-metadata-0000000000000000000000",
		Name:        "old-name",
		Description: "旧说明",
		Source:      constants.SkillSourceZip,
	}
	if err := controller.CreatePackage(ctx, skill); err != nil {
		t.Fatal(err)
	}
	version := models.SkillVersion{
		ID:             "version-metadata-000000000000000000",
		SkillPackageID: skill.ID,
		Version:        "1.1.0",
		Name:           "new-name",
		Description:    "新说明",
		ContentPath:    "skills/versions/metadata/1",
		ContentDigest:  "digest",
	}
	if err := controller.CreateVersion(ctx, version, true, ""); err != nil {
		t.Fatal(err)
	}
	updated, err := controller.GetPackage(ctx, skill.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.CurrentVersionID != version.ID || updated.Name != skill.Name || updated.Description != version.Description {
		t.Fatalf("updated skill = %+v", updated)
	}
}

func TestSkillControllerRejectsStaleCurrentVersionUpdate(t *testing.T) {
	controller := NewSkillController(newTestDB(t))
	ctx := context.Background()
	skill := models.SkillPackage{
		ID:               "skill-concurrency-000000000000000000",
		Name:             "review",
		Source:           constants.SkillSourceZip,
		CurrentVersionID: "version-1-00000000000000000000000",
	}
	if err := controller.CreatePackage(ctx, skill); err != nil {
		t.Fatal(err)
	}
	first := models.SkillVersion{
		ID: skill.CurrentVersionID, SkillPackageID: skill.ID, Version: "1.0.0",
		Name: skill.Name, ContentPath: "skills/versions/concurrency/1.0.0", ContentDigest: "one",
	}
	if err := controller.CreateVersion(ctx, first, true, first.ID); err != nil {
		t.Fatal(err)
	}
	second := models.SkillVersion{
		ID: "version-2-00000000000000000000000", SkillPackageID: skill.ID, Version: "2.0.0",
		Name: skill.Name, ContentPath: "skills/versions/concurrency/2.0.0", ContentDigest: "two",
	}
	if err := controller.CreateVersion(ctx, second, true, first.ID); err != nil {
		t.Fatal(err)
	}
	stale := models.SkillVersion{
		ID: "version-stale-00000000000000000000", SkillPackageID: skill.ID, Version: "1.5.0",
		Name: skill.Name, ContentPath: "skills/versions/concurrency/1.5.0", ContentDigest: "stale",
	}
	if err := controller.CreateVersion(ctx, stale, true, first.ID); err == nil {
		t.Fatal("stale current version update succeeded")
	}
	updated, err := controller.GetPackage(ctx, skill.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.CurrentVersionID != second.ID {
		t.Fatalf("current version = %q, want %q", updated.CurrentVersionID, second.ID)
	}
	if _, err := controller.GetVersion(ctx, stale.ID); err == nil {
		t.Fatal("stale version record was not rolled back")
	}
}

func TestExistingLibraryContentUsesCurrentImportedVersions(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	for _, skill := range []models.SkillPackage{
		{ID: "git", Name: "Renamed", Source: constants.SkillSourceGit, CurrentVersionID: "git-current"},
		{ID: "zip", Name: "Same name", Source: constants.SkillSourceZip, CurrentVersionID: "zip-current"},
		{ID: "builtin", Name: "Same name", Source: constants.SkillSourceBuiltin, BuiltIn: true, CurrentVersionID: "builtin-current"},
	} {
		if err := db.Create(&skill).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, version := range []models.SkillVersion{
		{ID: "git-current", SkillPackageID: "git", Version: "2.0.0", ContentDigest: "current-git"},
		{ID: "git-old", SkillPackageID: "git", Version: "1.0.0", ContentDigest: "historical"},
		{ID: "zip-current", SkillPackageID: "zip", Version: "1.0.0", ContentDigest: "current-zip"},
		{ID: "builtin-current", SkillPackageID: "builtin", Version: "1.0.0", ContentDigest: "internal-only"},
	} {
		if err := db.Create(&version).Error; err != nil {
			t.Fatal(err)
		}
	}
	matches, err := NewSkillController(db).ExistingLibraryContent(ctx, []string{"current-git", "current-zip", "historical", "internal-only", "same-name-new-content"})
	if err != nil || len(matches) != 2 || matches["current-git"] != "git" || matches["current-zip"] != "zip" {
		t.Fatalf("current library content matches = %v, error=%v", matches, err)
	}
	var plan []struct {
		// Detail 是 SQLite 返回的真实查询计划。
		Detail string
	}
	if err := db.Raw("EXPLAIN QUERY PLAN SELECT content_digest, MIN(skill_package_id) FROM skill_versions WHERE content_digest IN ? AND id IN (SELECT current_version_id FROM skill_packages WHERE built_in = ? AND source IN ?) GROUP BY content_digest", []string{"current-git"}, false, []constants.SkillSource{constants.SkillSourceGit, constants.SkillSourceZip}).Find(&plan).Error; err != nil {
		t.Fatal(err)
	}
	indexed := false
	for _, step := range plan {
		indexed = indexed || strings.Contains(step.Detail, "idx_skill_versions_content_digest")
	}
	if !indexed {
		t.Fatalf("content query lost its digest index: %+v", plan)
	}
}
