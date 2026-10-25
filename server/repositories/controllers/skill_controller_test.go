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

func TestSkillControllerVisibleStateAndEnabledSkills(t *testing.T) {
	controller := NewSkillController(newTestDB(t))
	ctx := context.Background()
	public := models.SkillPackage{ID: "skill-public-000000000000000000000000", Name: "审查", Scope: constants.SkillScopePublic, Source: constants.SkillSourceGit, BuiltIn: true, CurrentVersionID: "version-public-0000000000000000"}
	private := models.SkillPackage{ID: "skill-private-000000000000000000000000", Name: "私有", Scope: constants.SkillScopePrivate, Source: constants.SkillSourceZip, OwnerWakerID: "waker-0000000000000000000000000000", CurrentVersionID: "version-private-000000000000000"}
	if err := controller.CreatePackage(ctx, public); err != nil {
		t.Fatal(err)
	}
	if err := controller.CreatePackage(ctx, private); err != nil {
		t.Fatal(err)
	}
	if err := controller.CreateVersion(ctx, models.SkillVersion{ID: public.CurrentVersionID, SkillPackageID: public.ID, Version: "1.0.0", ContentPath: "skills/versions/public/1", ContentDigest: "digest"}, true, public.CurrentVersionID); err != nil {
		t.Fatal(err)
	}
	if err := controller.CreateVersion(ctx, models.SkillVersion{ID: private.CurrentVersionID, SkillPackageID: private.ID, Version: "1.0.0", ContentPath: "skills/versions/private/1", ContentDigest: "digest"}, true, private.CurrentVersionID); err != nil {
		t.Fatal(err)
	}
	items, err := controller.ListVisible(ctx, private.OwnerWakerID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Enabled {
		t.Fatalf("visible skill state = %+v", items)
	}
	if err := controller.EnsureWakerSkill(ctx, models.WakerSkill{
		ID: "default-relation", WakerID: private.OwnerWakerID, SkillPackageID: public.ID, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	defaults, err := controller.ListEnabled(ctx, private.OwnerWakerID)
	if err != nil || len(defaults) != 1 || defaults[0].SkillPackage.ID != public.ID {
		t.Fatalf("explicit default skills = %+v, error=%v", defaults, err)
	}
	if err := controller.UpsertWakerSkill(ctx, models.WakerSkill{ID: "relation-000000000000000000000000000", WakerID: private.OwnerWakerID, SkillPackageID: public.ID, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	enabled, err := controller.ListEnabled(ctx, private.OwnerWakerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(enabled) != 0 {
		t.Fatalf("enabled skills = %d, want 0", len(enabled))
	}
	if err := controller.EnsureWakerSkill(ctx, models.WakerSkill{
		ID: "another-default", WakerID: private.OwnerWakerID, SkillPackageID: public.ID, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	enabled, err = controller.ListEnabled(ctx, private.OwnerWakerID)
	if err != nil || len(enabled) != 0 {
		t.Fatalf("bootstrap overwrote disabled preference: %+v %v", enabled, err)
	}
}

func TestSkillControllerCurrentVersionSynchronizesVersionedMetadata(t *testing.T) {
	controller := NewSkillController(newTestDB(t))
	ctx := context.Background()
	skill := models.SkillPackage{
		ID:          "skill-metadata-0000000000000000000000",
		Name:        "old-name",
		Description: "旧说明",
		Scope:       constants.SkillScopePublic,
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
		Scope:            constants.SkillScopePublic,
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
