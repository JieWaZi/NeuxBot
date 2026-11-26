package global

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/memory/reviewstate"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestUnavailableMemoryDatabaseDoesNotFailCoreInitialization(t *testing.T) {
	previous := MemoryService
	t.Cleanup(func() { MemoryService = previous })
	MemoryService = nil
	data := t.TempDir()
	// A directory cannot be opened as the independent SQLite database.
	if err := os.Mkdir(filepath.Join(data, "memory.db"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := loadMemory(t.Context(), &Configuration{DataDirectory: data}); err != nil {
		t.Fatalf("optional memory failed startup: %v", err)
	}
	if MemoryService != nil {
		t.Fatal("startup exposed an unavailable memory service")
	}
}

func TestReviewFollowsFirstEnabledRuntimeWhenDefaultIsDisabled(t *testing.T) {
	values := []models.RuntimeEnvironment{
		{RuntimeID: "previous", IsDefault: true},
		{RuntimeID: "unregistered", IsDefault: true, Enabled: true},
		{RuntimeID: "claude", Enabled: true},
		{RuntimeID: "codex", Enabled: true},
	}
	if got := defaultReviewRuntime(values); got != "claude" {
		t.Fatalf("default with disabled predecessor = %q", got)
	}
	values[3].IsDefault = true
	if got := defaultReviewRuntime(values); got != "codex" {
		t.Fatalf("enabled explicit default = %q", got)
	}
}

func TestReviewIdentityChangesWithLocalSystemSubject(t *testing.T) {
	a := agent.AccountRequest{Subject: "account-a", ConfigFingerprint: "same-provider-configuration"}
	b := a
	b.Subject = "account-b"
	if systemReviewIdentity(a) == systemReviewIdentity(b) {
		t.Fatal("switching native login kept review authorization unchanged")
	}
}

func TestReviewInspectsEnabledCodexConfiguration(t *testing.T) {
	ctx := t.Context()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "review.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	previous := RuntimeEnvironmentControllerManager
	RuntimeEnvironmentControllerManager = controllers.NewRuntimeEnvironmentController(database.DB)
	t.Cleanup(func() { RuntimeEnvironmentControllerManager = previous })
	environment := models.RuntimeEnvironment{
		ID: "codex-review", RuntimeID: "codex-profile", BaseRuntimeID: constants.RuntimeIDCodex,
		Enabled: true, DefaultModel: "configured-model", PrefixArgs: []string{"--profile", "review"},
		Detection: agent.DetectionResult{State: agent.DetectionStateReady, ResolvedBinary: "/bin/codex"},
	}
	if err := RuntimeEnvironmentControllerManager.Ensure(ctx, environment); err != nil {
		t.Fatal(err)
	}
	settings := reviewstate.Settings{Source: reviewstate.SourceGlobal, RuntimeID: environment.RuntimeID}
	if err := ValidateReviewSelection(ctx, settings); err != nil {
		t.Fatal(err)
	}
	selection, err := reviewSelection(ctx, "", settings, "")
	if err != nil || selection.Model != environment.DefaultModel || selection.AccountID != constants.AiAccountSystemID {
		t.Fatalf("复盘模型=%q，账号=%q，错误=%v", selection.Model, selection.AccountID, err)
	}
	detection := environment.Detection
	detection.ResolvedBinary = "/bin/new-codex"
	if err := RuntimeEnvironmentControllerManager.UpdateDetection(ctx, environment.RuntimeID, detection, 1); err != nil {
		t.Fatal(err)
	}
	next, err := reviewSelection(ctx, "", settings, "")
	if err != nil || selection.Connection.Launch.Executable != "/bin/codex" || next.Connection.Launch.Executable != detection.ResolvedBinary {
		t.Fatalf("复盘启动程序未按执行边界选择: 当前=%q 后续=%q, %v", selection.Connection.Launch.Executable, next.Connection.Launch.Executable, err)
	}
}

func TestReviewResolvesWorkBuddySystemAccount(t *testing.T) {
	ctx := t.Context()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEBUDDY_CONFIG_DIR", home)
	t.Setenv("CODEBUDDY_AUTH_TOKEN", "fixture-token")
	t.Setenv("CODEBUDDY_INTERNET_ENVIRONMENT", "external")
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "review.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	previousRuntime, previousAccount := RuntimeEnvironmentControllerManager, AIAccountService
	RuntimeEnvironmentControllerManager = controllers.NewRuntimeEnvironmentController(database.DB)
	AIAccountService = aiaccount.New(controllers.NewAIAccountController(database.DB))
	t.Cleanup(func() { RuntimeEnvironmentControllerManager, AIAccountService = previousRuntime, previousAccount })
	environment := models.RuntimeEnvironment{
		ID: "workbuddy-review", RuntimeID: constants.RuntimeIDWorkBuddy, BaseRuntimeID: constants.RuntimeIDWorkBuddy,
		Enabled: true, Detection: agent.DetectionResult{State: agent.DetectionStateReady, ResolvedBinary: filepath.Join(home, "codebuddy")},
	}
	if err := RuntimeEnvironmentControllerManager.Ensure(ctx, environment); err != nil {
		t.Fatal(err)
	}
	settings := reviewstate.Settings{Source: reviewstate.SourceGlobal, RuntimeID: environment.RuntimeID, Model: "hy4std"}
	scratch := t.TempDir()
	selection, err := reviewSelection(ctx, "", settings, scratch)
	if err != nil {
		t.Fatalf("内置账号在复盘检查时不可用: %v", err)
	}
	workdir, err := os.MkdirTemp(scratch, "review-")
	if err != nil {
		t.Fatal(err)
	}
	request := agent.AccountRequest{Provider: environment.RuntimeID, AccountID: selection.AccountID, CWD: workdir, Model: selection.Model}
	identity, err := AIAccountService.ResolveAccount(ctx, request, "")
	if err != nil || selection.SystemIdentity == "" || selection.SystemIdentity != systemReviewIdentity(identity) {
		t.Fatalf("复盘检查与实际调用的账号身份不一致: %v", err)
	}
}
