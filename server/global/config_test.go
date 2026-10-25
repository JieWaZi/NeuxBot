package global

import (
	"context"
	"errors"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestLoadSystemSkillStaysOutsideEditablePartnerCatalog(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "builtin-skills.db"), Migrate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	previousSkills, previousTransactions := SkillControllerManager, TransactionControllerManager
	t.Cleanup(func() {
		SkillControllerManager, TransactionControllerManager = previousSkills, previousTransactions
	})
	SkillControllerManager = controllers.NewSkillController(database.DB)
	TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	configuration := NewConfiguration()
	configuration.DataDirectory = t.TempDir()
	ctx := context.Background()
	if err := loadBuiltinSkills(ctx, configuration); err != nil {
		t.Fatal(err)
	}
	enabled, err := SkillControllerManager.ListEnabled(ctx, builtinrolepreset.PersonalAssistantWakerID)
	if err != nil || len(enabled) != 0 {
		t.Fatalf("system skill leaked into editable partner state: %+v %v", enabled, err)
	}
	binary := filepath.Join(t.TempDir(), "allycli")
	if err := os.WriteFile(binary, []byte("cli fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	directory, err := builtinskills.SystemDirectory(configuration.DataDirectory, binary)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(filepath.Join(directory, "SKILL.md")); err != nil {
		t.Fatal(err)
	}

}

func TestPrepareKeepsSQLiteDefaultsAndCreatesToken(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("ALLY_DATA_DIR", dataDir)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ALLY_MANAGED_FILES_DIR", "")
	configuration := NewConfiguration()
	if err := Prepare(configuration); err != nil {
		t.Fatalf("prepare configuration: %v", err)
	}
	if configuration.DB.Type != conf.Sqlite {
		t.Fatalf("database type = %q, want sqlite", configuration.DB.Type)
	}
	if configuration.DB.DBName != filepath.Join(dataDir, "ally.db") {
		t.Fatalf("database path = %q", configuration.DB.DBName)
	}
	if configuration.DataDirectory != dataDir || configuration.ManagedFilesDirectory != filepath.Join(home, constants.ManagedFilesDirectoryName) {
		t.Fatalf("storage roots were not separated: data=%q managed=%q", configuration.DataDirectory, configuration.ManagedFilesDirectory)
	}
	info, err := os.Stat(configuration.ManagedFilesDirectory)
	if err != nil || !info.IsDir() {
		t.Fatalf("managed root was not prepared: %v", err)
	}
	if configuration.DB.MaxOpenConns != 0 || configuration.DB.MaxIdleConns != 0 {
		t.Fatalf(
			"connection pool should remain unset: open=%d idle=%d",
			configuration.DB.MaxOpenConns,
			configuration.DB.MaxIdleConns,
		)
	}
	if configuration.Security.LocalToken == "" {
		t.Fatal("local token was not generated")
	}
}

func TestPrepareManagedFilesOverrideKeepsDatabaseLocation(t *testing.T) {
	dataDir, managedDir := t.TempDir(), filepath.Join(t.TempDir(), "work-files")
	t.Setenv("ALLY_DATA_DIR", dataDir)
	t.Setenv("ALLY_MANAGED_FILES_DIR", managedDir)
	configuration := NewConfiguration()
	if err := Prepare(configuration); err != nil {
		t.Fatal(err)
	}
	if configuration.ManagedFilesDirectory != managedDir || configuration.DataDirectory != dataDir || configuration.DB.DBName != filepath.Join(dataDir, "ally.db") {
		t.Fatalf("managed override changed database root: %+v", configuration)
	}
	if err := os.WriteFile(filepath.Join(managedDir, "chat"), []byte("occupied"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ALLY_MANAGED_FILES_DIR", filepath.Join(managedDir, "chat", "nested"))
	if err := Prepare(NewConfiguration()); err == nil {
		t.Fatal("unusable managed root was accepted")
	}
}

func TestPrepareRejectsNonLoopbackHost(t *testing.T) {
	configuration := NewConfiguration()
	configuration.Server.Host = "0.0.0.0"
	if err := Prepare(configuration); err == nil {
		t.Fatal("expected non-loopback host to be rejected")
	}
}

func TestRunLoadSequenceRollsBackOnceAfterFailure(t *testing.T) {
	wantFailure := errors.New("load failed")
	executed := []string{}
	rollbackCount := 0
	err := runLoadSequence(
		func() { rollbackCount++ },
		func() error {
			executed = append(executed, "database")
			return nil
		},
		func() error {
			executed = append(executed, "memory")
			return wantFailure
		},
		func() error {
			executed = append(executed, "runtime")
			return nil
		},
	)
	if !errors.Is(err, wantFailure) {
		t.Fatalf("load error = %v, want %v", err, wantFailure)
	}
	if rollbackCount != 1 {
		t.Fatalf("rollback count = %d, want 1", rollbackCount)
	}
	if len(executed) != 2 || executed[0] != "database" || executed[1] != "memory" {
		t.Fatalf("executed steps = %v", executed)
	}
}

func TestRunLoadSequenceDoesNotRollBackAfterSuccess(t *testing.T) {
	rollbackCount := 0
	err := runLoadSequence(
		func() { rollbackCount++ },
		func() error { return nil },
		func() error { return nil },
	)
	if err != nil {
		t.Fatalf("run load sequence: %v", err)
	}
	if rollbackCount != 0 {
		t.Fatalf("rollback count = %d, want 0", rollbackCount)
	}
}
