package skilllearning

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/skills"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

const originalBody = "\n# Existing steps\nKeep the owner's checklist.\n步骤结果应与模板一致。\n"
const originalMarkdown = "---\nname: checklist\ndescription: Existing instructions\nmetadata:\n  license: custom\n---\n" + originalBody

func learnedChecklistDraft() skills.LearnedDraft {
	const anchor = "Keep the owner's checklist."
	after := anchor + "\nRun a dry check before submitting."
	return skills.LearnedDraft{Revision: &skills.LearnedRevision{
		Description: "Improved instructions",
		Edits:       []skills.LearnedEdit{{Before: anchor, After: &after}},
	}}
}

func TestPublishCreatesAnEnabledLocalSkillForTheSourcePartner(t *testing.T) {
	svc, db := testLibrary(t)
	ctx := t.Context()
	input := Publication{SuggestionID: "suggestion", WakerID: "owner", Name: "checklist", Draft: skills.LearnedDraft{Markdown: originalMarkdown}}
	result, err := svc.Publish(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.SuggestionID != input.SuggestionID || result.WakerID != "owner" || result.SkillID == "" || result.VersionID == "" || result.SkillName != "checklist" {
		t.Fatalf("unexpected receipt: %+v", result)
	}
	if retry, err := svc.Publish(ctx, Publication{SuggestionID: "suggestion", WakerID: "owner"}); err != nil || retry != result {
		t.Fatalf("retry = %+v, %v", retry, err)
	}
	if _, found, err := svc.GetReceipt(ctx, "other", "suggestion"); err != nil || found {
		t.Fatalf("another partner read the receipt: %v, %v", found, err)
	}
	installed, err := svc.skills.ListEnabled(ctx, "owner")
	if err != nil || len(installed) != 1 || installed[0].SkillPackage.ID != result.SkillID || installed[0].CurrentVersion.Version != "1.0.0" {
		t.Fatalf("owner installation = %+v, %v", installed, err)
	}
	others, err := svc.skills.ListInstalled(ctx, "other", "")
	if err != nil || len(others) != 0 {
		t.Fatalf("other partner installations = %+v, %v", others, err)
	}
	items, total, err := svc.skills.ListLibrary(ctx, "", 0, 20)
	if err != nil || total != 1 || len(items) != 1 || items[0].SkillPackage.Source != constants.SkillSource("learned") {
		t.Fatalf("learned skill not visible in library: %+v, %d, %v", items, total, err)
	}
	var receipts int64
	if err := db.DB.Model(&models.SkillLearningReceipt{}).Count(&receipts).Error; err != nil || receipts != 1 {
		t.Fatalf("receipt rows = %d, %v", receipts, err)
	}
	candidates, err := svc.ListCandidates(ctx, "owner")
	if err != nil || !slices.ContainsFunc(candidates, func(item Candidate) bool {
		return item.SkillID == result.SkillID && item.VersionID == result.VersionID && item.Revisable && item.Name == "checklist"
	}) {
		t.Fatalf("learned skill missing from subsequent learning input: %+v, %v", candidates, err)
	}
}

func TestPublishRejectsInvalidOrConflictingNativeSkillNames(t *testing.T) {
	ctx := t.Context()
	svc, db := testLibrary(t)
	if err := db.DB.Create(&models.Plugin{ID: "plugin", Name: "Plugin", SourceKey: "local:plugin"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&models.PluginVersion{ID: "plugin-v1", PluginID: "plugin", Version: "1.0.0", Package: plugins.Package{
		Skills: []plugins.Skill{{Name: "plugin-check", Description: "Run plugin checks"}},
	}}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&models.WakerPlugin{ID: "plugin-rel", PluginID: "plugin", VersionID: "plugin-v1", WakerID: "owner", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	seedSkill(t, svc, "imported", "already-used", constants.SkillSourceZip)
	seedSkill(t, svc, "built-in", "built-in-check", constants.SkillSourceBuiltin)
	for _, test := range []struct {
		// 寻址使用的建议名称。
		name string
		// 模型返回的稳定技能内容。
		markdown string
		// 发布必须报告的稳定错误。
		want error
	}{
		{"missing-frontmatter", "A description without frontmatter", status_error.SkillContentInvalid},
		{"renamed", originalMarkdown, status_error.SkillContentInvalid},
		{"bot", strings.ReplaceAll(originalMarkdown, "checklist", "bot"), status_error.SkillNameConflict},
		{"plugin-check", strings.ReplaceAll(originalMarkdown, "checklist", "plugin-check"), status_error.SkillNameConflict},
		{"already-used", strings.ReplaceAll(originalMarkdown, "checklist", "already-used"), status_error.SkillNameConflict},
		{"built-in-check", strings.ReplaceAll(originalMarkdown, "checklist", "built-in-check"), status_error.SkillNameConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := svc.Publish(ctx, Publication{SuggestionID: test.name, WakerID: "owner", Name: test.name, Draft: skills.LearnedDraft{Markdown: test.markdown}}); !errors.Is(err, test.want) {
				t.Fatalf("publish error = %v, want %v", err, test.want)
			}
			if _, found, err := svc.GetReceipt(ctx, "owner", test.name); err != nil || found {
				t.Fatalf("failed publication recorded a receipt: %v, %v", found, err)
			}
		})
	}
}

func TestReviseChecksConfirmedSharedScopeAndPreservesOtherResources(t *testing.T) {
	ctx := t.Context()
	svc, _ := testLibrary(t)
	old := seedSkill(t, svc, "imported", "checklist", constants.SkillSourceZip)
	input := Publication{SuggestionID: "revision", WakerID: "owner", SkillID: "imported", ExpectedVersionID: old.ID, Name: "checklist", Draft: learnedChecklistDraft()}
	if err := svc.skills.EnsureWakerSkill(ctx, models.WakerSkill{ID: "share-other", WakerID: "other", SkillPackageID: input.SkillID}); err != nil {
		t.Fatal(err)
	}
	target, err := svc.ReadTarget(ctx, "owner", input.SkillID, old.ID)
	if err != nil || target.Markdown != originalMarkdown || len(target.AffectedPartners) != 2 {
		t.Fatalf("unexpected revision target: %+v, %v", target, err)
	}
	if _, err := svc.Publish(ctx, input); !errors.Is(err, status_error.Conflict) {
		t.Fatalf("auto published a shared revision: %v", err)
	}
	input.ConfirmedPartnerIDs = []string{"other", "owner"}
	if err := svc.skills.EnsureWakerSkill(ctx, models.WakerSkill{ID: "share-third", WakerID: "third", SkillPackageID: input.SkillID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(ctx, input); !errors.Is(err, status_error.Conflict) {
		t.Fatalf("outdated scope published: %v", err)
	}
	if got, err := svc.skills.ListVersions(ctx, input.SkillID); err != nil || len(got) != 1 {
		t.Fatalf("failed revision modified history: %+v, %v", got, err)
	}
	input.ConfirmedPartnerIDs = append(input.ConfirmedPartnerIDs, "third")
	published, err := svc.Publish(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := svc.skills.GetVersion(ctx, published.VersionID)
	if err != nil || revision.ID == old.ID || revision.Version != "1.0.1" || revision.ContentPath == old.ContentPath {
		t.Fatalf("invalid immutable revision: %+v, %v", revision, err)
	}
	for _, item := range []models.SkillVersion{old, revision} {
		root := filepath.Join(svc.dataDirectory, item.ContentPath)
		content, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if item == old && string(content) != originalMarkdown {
			t.Fatalf("old version changed: %s", content)
		}
		wantBody := strings.Replace(originalBody, "Keep the owner's checklist.", "Keep the owner's checklist.\nRun a dry check before submitting.", 1)
		if item == revision && (!strings.Contains(string(content), "license: custom") || !strings.HasSuffix(string(content), "---\n"+wantBody) || !strings.Contains(string(content), "Improved instructions")) {
			t.Fatalf("new version discarded revision or protected metadata: %s", content)
		}
		asset, err := os.ReadFile(filepath.Join(root, "assets", "sample.bin"))
		if err != nil || !slices.Equal(asset, []byte{0, 12, 255}) {
			t.Fatalf("attachment changed in %s: %v, %v", item.ID, asset, err)
		}
	}
	input.SuggestionID = "stale-version"
	if _, err := svc.Publish(ctx, input); !errors.Is(err, status_error.Conflict) {
		t.Fatalf("stale version published: %v", err)
	}
}

func TestReviseRejectsReplacingTheExistingBody(t *testing.T) {
	ctx := t.Context()
	svc, _ := testLibrary(t)
	old := seedSkill(t, svc, "imported", "checklist", constants.SkillSourceZip)
	after := "Verify."
	for id, draft := range map[string]skills.LearnedDraft{
		"full-markdown": {Markdown: "---\nname: checklist\ndescription: Improved instructions\n---\nVerify."},
		"large-edit": {Revision: &skills.LearnedRevision{Edits: []skills.LearnedEdit{{
			Before: "Keep the owner's checklist.\n步骤结果应与模板一致。", After: &after,
		}}}},
	} {
		_, err := svc.Publish(ctx, Publication{
			SuggestionID: id, WakerID: "owner", SkillID: "imported", ExpectedVersionID: old.ID, Name: "checklist", Draft: draft,
		})
		if !errors.Is(err, status_error.SkillContentInvalid) {
			t.Fatalf("%s published: %v", id, err)
		}
	}
	if got, err := svc.skills.ListVersions(ctx, "imported"); err != nil || len(got) != 1 {
		t.Fatalf("invalid revision modified history: %+v, %v", got, err)
	}
}

func TestPublishRacingRetriesReturnTheSameRevision(t *testing.T) {
	ctx := t.Context()
	svc, _ := testLibrary(t)
	created, err := svc.Publish(ctx, Publication{SuggestionID: "create", WakerID: "owner", Name: "checklist", Draft: skills.LearnedDraft{Markdown: originalMarkdown}})
	if err != nil {
		t.Fatal(err)
	}
	input := Publication{SuggestionID: "revision", WakerID: "owner", SkillID: created.SkillID, ExpectedVersionID: created.VersionID, Name: "checklist", Draft: learnedChecklistDraft()}
	type outcome struct {
		// receipt 标识本次调用所确认的同一发布。
		receipt models.SkillLearningReceipt
		// err 保存发布失败原因。
		err error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			<-start
			result, err := svc.Publish(ctx, input)
			results <- outcome{result, err}
		}()
	}
	close(start)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.receipt != second.receipt {
		t.Fatalf("racing retry returned different outcomes: %+v, %+v", first, second)
	}
	versions, err := svc.skills.ListVersions(ctx, created.SkillID)
	if err != nil || len(versions) != 2 {
		t.Fatalf("duplicate revision was published: %+v, %v", versions, err)
	}
	entries, err := os.ReadDir(filepath.Join(svc.dataDirectory, "skills", "versions", created.SkillID))
	if err != nil || len(entries) != 2 {
		t.Fatalf("attempt residue remains: %d entries, %v", len(entries), err)
	}
}

func TestPublishRollsBackWhenAnotherPartnerInstallsAfterPreparation(t *testing.T) {
	ctx := t.Context()
	svc, _ := testLibrary(t)
	old := seedSkill(t, svc, "imported", "checklist", constants.SkillSourceZip)
	transactions := 0
	svc.transactions = transactionHook{
		TransactionController: svc.transactions,
		before: func() {
			transactions++
			if transactions == 2 {
				if err := svc.skills.EnsureWakerSkill(ctx, models.WakerSkill{
					ID: "new-share", WakerID: "other", SkillPackageID: "imported", Enabled: true,
				}); err != nil {
					t.Fatal(err)
				}
			}
		},
	}
	_, err := svc.Publish(ctx, Publication{
		SuggestionID: "revision", WakerID: "owner", Name: "checklist", Draft: learnedChecklistDraft(),
		SkillID: "imported", ExpectedVersionID: old.ID,
	})
	if !errors.Is(err, status_error.Conflict) {
		t.Fatalf("published after installation scope changed: %v", err)
	}
	if _, found, err := svc.GetReceipt(ctx, "owner", "revision"); err != nil || found {
		t.Fatalf("unexpected receipt after shared-scope conflict: %v, %v", found, err)
	}
	target, err := svc.ReadTarget(ctx, "owner", "imported", old.ID)
	if err != nil || len(target.AffectedPartners) != 2 || target.Markdown != originalMarkdown {
		t.Fatalf("prior skill or new share changed: %+v, %v", target, err)
	}
	entries, err := os.ReadDir(filepath.Join(svc.dataDirectory, "skills", "versions", "imported"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("unpublished files remain: %v, %v", entries, err)
	}
}

func TestReadTargetRequiresLocalSourceAndPartnerInstallation(t *testing.T) {
	ctx := t.Context()
	svc, _ := testLibrary(t)
	zip := seedSkill(t, svc, "zip", "checklist", constants.SkillSourceZip)
	git := seedSkill(t, svc, "git", "git-skill", constants.SkillSourceGit)
	if _, err := svc.ReadTarget(ctx, "other", "zip", zip.ID); !errors.Is(err, status_error.SkillNotFound) {
		t.Fatalf("read target installed by another partner: %v", err)
	}
	if _, err := svc.ReadTarget(ctx, "owner", "git", git.ID); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("read Git target for learning revision: %v", err)
	}
}

func TestPublishGuardRunsAfterStagingAndReleasesAfterCommit(t *testing.T) {
	ctx := t.Context()
	svc, _ := testLibrary(t)
	old := seedSkill(t, svc, "imported", "checklist", constants.SkillSourceZip)
	released := false
	input := Publication{
		SuggestionID: "revision", WakerID: "owner", Name: "checklist", Draft: learnedChecklistDraft(),
		SkillID: "imported", ExpectedVersionID: old.ID,
	}
	input.AcquirePublication = func() (func(), error) {
		directory := filepath.Join(svc.dataDirectory, "skills", "versions", "imported")
		if entries, err := os.ReadDir(directory); err != nil || len(entries) != 2 {
			t.Fatalf("guard acquired before the new version was staged: %v, %v", entries, err)
		}
		if _, found, err := svc.GetReceipt(ctx, "owner", "revision"); err != nil || found {
			t.Fatalf("receipt already committed before acquiring guard: %v, %v", found, err)
		}
		return func() {
			released = true
			if _, found, err := svc.GetReceipt(ctx, "owner", "revision"); err != nil || !found {
				t.Fatalf("guard released before commit: %v, %v", found, err)
			}
		}, nil
	}
	if _, err := svc.Publish(ctx, input); err != nil || !released {
		t.Fatalf("publish did not succeed within the guard: released=%v, %v", released, err)
	}
}

func TestPublishGuardFailureRemovesPreparedFilesAndLeavesNoReceipt(t *testing.T) {
	ctx := t.Context()
	svc, _ := testLibrary(t)
	_, err := svc.Publish(ctx, Publication{
		SuggestionID: "cancelled", WakerID: "owner", Name: "checklist", Draft: skills.LearnedDraft{Markdown: originalMarkdown},
		AcquirePublication: func() (func(), error) {
			return nil, status_error.Conflict
		},
	})
	if !errors.Is(err, status_error.Conflict) {
		t.Fatalf("publish ignored guard failure: %v", err)
	}
	if _, found, err := svc.GetReceipt(ctx, "owner", "cancelled"); err != nil || found {
		t.Fatalf("failed guard wrote the receipt: %v, %v", found, err)
	}
	versions, err := os.ReadDir(filepath.Join(svc.dataDirectory, "skills", "versions"))
	if err != nil || len(versions) != 0 {
		t.Fatalf("guard failure left prepared files: %v, %v", versions, err)
	}
}

type transactionHook struct {
	// 保留真实数据库事务边界。
	controllers.TransactionController
	// before 在调用事务边界前执行竞争写入。
	before func()
}

func (h transactionHook) Transaction(ctx context.Context, fn func(context.Context) error) error {
	h.before()
	return h.TransactionController.Transaction(ctx, fn)
}

func seedSkill(t *testing.T, svc *Service, id, name string, source constants.SkillSource) models.SkillVersion {
	t.Helper()
	ctx := t.Context()
	stage := t.TempDir()
	if err := os.WriteFile(filepath.Join(stage, "SKILL.md"), []byte(strings.ReplaceAll(originalMarkdown, "checklist", name)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(stage, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "assets", "sample.bin"), []byte{0, 12, 255}, 0o600); err != nil {
		t.Fatal(err)
	}
	relative, digest, err := skills.CopyVersion(svc.dataDirectory, id, "1.0.0", stage)
	if err != nil {
		t.Fatal(err)
	}
	version := models.SkillVersion{ID: id + "-version", SkillPackageID: id, Version: "1.0.0", Name: name, Description: "Existing instructions", ContentPath: relative, ContentDigest: digest}
	if err := svc.skills.CreatePackage(ctx, models.SkillPackage{ID: id, Name: "Display name for " + id, Source: source, BuiltIn: source == constants.SkillSourceBuiltin}); err != nil {
		t.Fatal(err)
	}
	if err := svc.skills.CreateVersion(ctx, version, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.skills.EnsureWakerSkill(ctx, models.WakerSkill{ID: id + "-installed", SkillPackageID: id, WakerID: "owner", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	return version
}

func testLibrary(t *testing.T) (*Service, *dbhelper.DB) {
	t.Helper()
	root := t.TempDir()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(root, "test.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	partners := []models.Waker{{ID: "owner", Name: "Owner"}, {ID: "other", Name: "Other"}, {ID: "third", Name: "Third"}}
	if err := db.DB.Create(&partners).Error; err != nil {
		t.Fatal(err)
	}
	return New(Config{
		DataDirectory: root,
		Skills:        controllers.NewSkillController(db.DB),
		Wakers:        controllers.NewWakerController(db.DB),
		Plugins:       controllers.NewPluginController(db.DB),
		Transactions:  controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)),
	}), db
}
