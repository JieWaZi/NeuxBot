package skills

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestScanLibrarySkillImportReturnsCandidatesWithoutInstalling(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "scan-skill.db"), Migrate: true,
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
	waker := models.Waker{ID: "waker-scan-000000000000000000000", Name: "扫描伙伴"}
	if err := global.WakerControllerManager.Create(ctx, waker); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "skills.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for path, name := range map[string]string{
		"review/SKILL.md": "review",
		"test/SKILL.md":   "test-runner",
	} {
		entry, err := writer.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("---\nname: " + name + "\ndescription: Candidate\n---\n")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := (&ScanLibrarySkillImport{
		Body: ScanSkillImportRequest{Kind: "zip", Location: archivePath},
	}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	batch, ok := result.(SkillImportBatch)
	if !ok {
		t.Fatalf("scan result type = %T", result)
	}
	if batch.Token == "" || len(batch.Candidates) != 2 {
		t.Fatalf("scan batch = %+v", batch)
	}
	visible, err := global.SkillControllerManager.ListInstalled(ctx, waker.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 0 {
		t.Fatalf("visible skills after scan = %d, want 0", len(visible))
	}
}

func TestImportLibrarySkillsInstallsOnlySelectedCandidates(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "install-selected-skill.db"), Migrate: true,
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
	previousTransactions := global.TransactionControllerManager
	previousDataDirectory := global.Config.DataDirectory
	t.Cleanup(func() {
		global.WakerControllerManager = previousWakers
		global.SkillControllerManager = previousSkills
		global.TransactionControllerManager = previousTransactions
		global.Config.DataDirectory = previousDataDirectory
	})
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.SkillControllerManager = controllers.NewSkillController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	global.Config.DataDirectory = t.TempDir()
	ctx, _ := gin.CreateTestContext(nil)
	waker := models.Waker{ID: "waker-install-0000000000000000000", Name: "安装伙伴"}
	if err := global.WakerControllerManager.Create(ctx, waker); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "skills.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for path, name := range map[string]string{
		"review/SKILL.md": "review",
		"test/SKILL.md":   "test-runner",
	} {
		entry, err := writer.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("---\nname: " + name + "\ndescription: Candidate\n---\n")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := (&ScanLibrarySkillImport{
		Body: ScanSkillImportRequest{Kind: "zip", Location: archivePath},
	}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	batch := result.(SkillImportBatch)
	var selectedID string
	for _, candidate := range batch.Candidates {
		if candidate.Name == "review" {
			selectedID = candidate.ID
		}
	}
	if selectedID == "" {
		t.Fatal("review candidate not found")
	}

	created, err := (&ImportLibrarySkills{
		Body: ImportSkillRequest{Token: batch.Token, Candidates: []InstallSkillCandidateRequest{{ID: selectedID, Version: "1.0.0"}}},
	}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resources := created.([]models.SkillPackage)
	if len(resources) != 1 || resources[0].Name != "review" {
		t.Fatalf("imported resources = %+v, want only review", resources)
	}
	if err := global.SkillControllerManager.UpdatePackageMetadata(ctx, resources[0].ID, "Renamed review", nil); err != nil {
		t.Fatal(err)
	}
	rescanned, err := (&ScanLibrarySkillImport{Body: ScanSkillImportRequest{Kind: "zip", Location: archivePath}}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range rescanned.(SkillImportBatch).Candidates {
		if candidate.Name == "review" {
			if !candidate.Installed || candidate.InstalledSkillID != resources[0].ID {
				t.Fatalf("existing library content was not linked after rename: %+v", candidate)
			}
		} else if candidate.Installed || candidate.InstalledSkillID != "" {
			t.Fatalf("new content was marked as already imported: %+v", candidate)
		}
	}
	installed, err := global.SkillControllerManager.ListInstalled(ctx, waker.ID, "")
	if err != nil || len(installed) != 0 {
		t.Fatalf("import implicitly installed resource: %+v %v", installed, err)
	}
	if _, err := (&InstallLibrarySkill{SkillID: resources[0].ID, Body: InstallLibrarySkillRequest{WakerIDs: []string{"missing-partner"}}}).Output(ctx); err == nil {
		t.Fatal("install to missing partner succeeded")
	}
	if _, err := global.SkillControllerManager.GetPackage(ctx, resources[0].ID); err != nil {
		t.Fatalf("failed partner installation removed imported resource: %v", err)
	}
	if _, err := (&InstallLibrarySkill{SkillID: resources[0].ID, Body: InstallLibrarySkillRequest{WakerIDs: []string{waker.ID}}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	installed, err = global.SkillControllerManager.ListInstalled(ctx, waker.ID, "")
	if err != nil || len(installed) != 1 || installed[0].SkillPackage.ID != resources[0].ID {
		t.Fatalf("explicit installation absent: %+v %v", installed, err)
	}
}

func TestImportLibrarySkillsValidatesEverySelectionBeforeWritingVersions(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "validate-selection.db"), Migrate: true,
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
	previousTransactions := global.TransactionControllerManager
	previousDataDirectory := global.Config.DataDirectory
	t.Cleanup(func() {
		global.WakerControllerManager = previousWakers
		global.SkillControllerManager = previousSkills
		global.TransactionControllerManager = previousTransactions
		global.Config.DataDirectory = previousDataDirectory
	})
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.SkillControllerManager = controllers.NewSkillController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	global.Config.DataDirectory = t.TempDir()
	ctx, _ := gin.CreateTestContext(nil)
	waker := models.Waker{ID: "waker-validate-000000000000000000", Name: "校验伙伴"}
	if err := global.WakerControllerManager.Create(ctx, waker); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "skill.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("---\nname: validate-all\ndescription: Validate all\n---\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := (&ScanLibrarySkillImport{
		Body: ScanSkillImportRequest{Kind: "zip", Location: archivePath},
	}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	batch := result.(SkillImportBatch)
	versionsRoot := filepath.Join(global.Config.DataDirectory, "skills", "versions")
	if err := os.WriteFile(versionsRoot, []byte("blocks version writes"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = (&ImportLibrarySkills{
		Body: ImportSkillRequest{
			Token: batch.Token,
			Candidates: []InstallSkillCandidateRequest{
				{ID: batch.Candidates[0].ID, Version: "1.0.0"},
				{ID: "missing-candidate", Version: "1.0.0"},
			},
		},
	}).Output(ctx)
	if !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("ImportLibrarySkills.Output() error = %v, want BadRequest", err)
	}
	visible, err := global.SkillControllerManager.ListInstalled(ctx, waker.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 0 {
		t.Fatalf("visible skills = %d, want no partial installation", len(visible))
	}
}

func TestCandidateGitSubpathKeepsRepositoryRelativeCandidatePath(t *testing.T) {
	got := candidateGitSubpath("skills/engineering/wizard")
	if got != "skills/engineering/wizard" {
		t.Fatalf("candidateGitSubpath() = %q, want repository-relative candidate path", got)
	}
}
