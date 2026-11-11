package controllers

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"context"
	"path/filepath"
	"reflect"
	"testing"

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
		RuntimeID:     agentapi.RuntimeIDCodex,
		BaseRuntimeID: agentapi.RuntimeIDCodex,
		Name:          "Codex CLI",
		Enabled:       true,
		Binary:        "codex",
		PrefixArgs:    []string{},
	}
	if err := controller.Ensure(ctx, initial); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	configuration := models.RuntimeEnvironment{
		RuntimeID:              agentapi.RuntimeIDCodex,
		Enabled:                false,
		Binary:                 "/opt/tools/codex",
		PrefixArgs:             []string{"--profile", "ally"},
		ConfigHome:             "/tmp/codex-config",
		DefaultModel:           "gpt-5.6",
		DefaultReasoning:       "high",
		DefaultPermissionLevel: agentapi.PermissionLevelDefault,
	}
	if err := controller.UpdateConfiguration(ctx, configuration); err != nil {
		t.Fatalf("UpdateConfiguration() error = %v", err)
	}
	detection := agentapi.DetectionResult{
		Runtime: agentapi.RuntimeSummary{
			ID:                            agentapi.RuntimeIDCodex,
			BaseRuntimeID:                 agentapi.RuntimeIDCodex,
			Name:                          "Codex CLI",
			PrefixArgs:                    []string{"--native"},
			ConfigHomeEnvironmentVariable: "CODEX_HOME",
			ConfigHome:                    "/catalog/profile",
		},
		State:          agentapi.DetectionStateReady,
		Version:        "1.2.3",
		ResolvedBinary: "/opt/tools/codex",
		Models:         []agentapi.ModelOption{{ID: "gpt-5.6", Name: "GPT-5.6"}},
	}
	if err := controller.UpdateDetection(ctx, agentapi.RuntimeIDCodex, detection, 1234); err != nil {
		t.Fatalf("UpdateDetection() error = %v", err)
	}

	stored, err := controller.Get(ctx, agentapi.RuntimeIDCodex)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.Enabled || stored.Binary != detection.ResolvedBinary || stored.ConfigHome != detection.Runtime.ConfigHome || stored.DefaultPermissionLevel != agentapi.PermissionLevelDefault {
		t.Fatalf("stored runtime must use readonly detection instead of submitted launch fields: %+v", stored)
	}
	if !reflect.DeepEqual(stored.PrefixArgs, detection.Runtime.PrefixArgs) {
		t.Fatalf("native launch arguments not restored: %v", stored.PrefixArgs)
	}
	for _, field := range []string{"binary", "prefix_args", "config_home", "custom_models"} {
		if database.DB.Migrator().HasColumn(&models.RuntimeEnvironment{}, field) {
			t.Fatalf("removed user setting still persisted: %s", field)
		}
	}
	if !reflect.DeepEqual(stored.Detection, detection) || stored.DetectedAt != 1234 {
		t.Fatalf("stored detection = %#v at %d, want %#v at 1234", stored.Detection, stored.DetectedAt, detection)
	}
}
