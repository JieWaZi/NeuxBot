package controllers

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestRuntimeEnvironmentControllerPersistsConfigurationAndDetection(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "runtime-environment-controller.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get test sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	controller := NewRuntimeEnvironmentController(database.DB)
	ctx := context.Background()
	initial := models.RuntimeEnvironment{
		ID:            "0123456789abcdef0123456789abcdef",
		RuntimeID:     runtimes.RuntimeIDCodex,
		BaseRuntimeID: runtimes.RuntimeIDCodex,
		Name:          "Codex CLI",
		Enabled:       true,
		Binary:        "codex",
		PrefixArgs:    []string{},
	}
	if err := controller.Ensure(ctx, initial); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	configuration := models.RuntimeEnvironment{
		RuntimeID:              runtimes.RuntimeIDCodex,
		Enabled:                false,
		Binary:                 "/opt/tools/codex",
		PrefixArgs:             []string{"--profile", "ally"},
		ConfigHome:             "/tmp/codex-config",
		CustomModels:           []string{"gpt-team-custom"},
		DefaultModel:           "gpt-5.6",
		DefaultReasoning:       "high",
		DefaultPermissionLevel: runtimes.PermissionLevelDefault,
	}
	if err := controller.UpdateConfiguration(ctx, configuration); err != nil {
		t.Fatalf("UpdateConfiguration() error = %v", err)
	}
	detection := runtimes.DetectionResult{
		Runtime: runtimes.RuntimeSummary{
			ID:                            runtimes.RuntimeIDCodex,
			BaseRuntimeID:                 runtimes.RuntimeIDCodex,
			Name:                          "Codex CLI",
			ConfigHomeEnvironmentVariable: "CODEX_HOME",
		},
		State:          runtimes.DetectionStateReady,
		Version:        "1.2.3",
		ResolvedBinary: "/opt/tools/codex",
		Models:         []runtimes.ModelOption{{ID: "gpt-5.6", Name: "GPT-5.6"}},
	}
	if err := controller.UpdateDetection(ctx, runtimes.RuntimeIDCodex, detection, 1234); err != nil {
		t.Fatalf("UpdateDetection() error = %v", err)
	}

	stored, err := controller.Get(ctx, runtimes.RuntimeIDCodex)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.Enabled || stored.Binary != configuration.Binary ||
		stored.ConfigHome != configuration.ConfigHome ||
		stored.DefaultPermissionLevel != runtimes.PermissionLevelDefault {
		t.Fatalf("stored configuration = %#v, want disabled custom configuration", stored)
	}
	if !reflect.DeepEqual(stored.PrefixArgs, configuration.PrefixArgs) {
		t.Fatalf("PrefixArgs = %#v, want %#v", stored.PrefixArgs, configuration.PrefixArgs)
	}
	if !reflect.DeepEqual(stored.CustomModels, configuration.CustomModels) {
		t.Fatalf("CustomModels = %#v, want %#v", stored.CustomModels, configuration.CustomModels)
	}
	if !reflect.DeepEqual(stored.Detection, detection) || stored.DetectedAt != 1234 {
		t.Fatalf("stored detection = %#v at %d, want %#v at 1234", stored.Detection, stored.DetectedAt, detection)
	}
}
