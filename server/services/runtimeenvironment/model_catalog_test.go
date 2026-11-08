package runtimeenvironment

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// TestModelCatalogRejectsAccountFromAnotherRuntime 验证凭据检查与缓存读取前校验账号归属。
func TestModelCatalogRejectsAccountFromAnotherRuntime(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "accounts.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := database.DB.DB()
	t.Cleanup(func() { _ = pool.Close() })
	accounts := controllers.NewAIAccountController(database.DB)
	account := models.AIAccount{ID: "0123456789abcdef0123456789abcdef", RuntimeID: "codex-profile", Name: "other profile", AuthMode: constants.AccountAuthModeSubscription,
		Endpoints: []models.AIAccountEndpoint{{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL}}}
	if err := accounts.Create(t.Context(), account, ""); err != nil {
		t.Fatal(err)
	}
	svc := &service{accounts: aiaccount.New(accounts)}
	_, err = svc.readModelCatalog(t.Context(), models.RuntimeEnvironment{RuntimeID: "codex", BaseRuntimeID: constants.RuntimeIDCodex}, account.ID, false)
	if !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
		t.Fatalf("catalog accepted another profile's signed-out account: %v", err)
	}
}

func TestModelCatalogReadUsesPersistedOptionsAndRefreshProbes(t *testing.T) {
	root := t.TempDir()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(root, "catalog.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := controllers.NewRuntimeModelCatalogController(database.DB)
	environment := models.RuntimeEnvironment{RuntimeID: "codex", BaseRuntimeID: "codex", Binary: "codex"}
	ctx := t.Context()
	err = store.Store(ctx, models.RuntimeModelCatalog{
		RuntimeID: "codex", AccountID: agent.SystemAccountID,
		Fingerprint: persistedModelCatalogFingerprint(environment, 0, ""), ProbeStartedAt: 1,
		Snapshot: agent.ModelCatalogSnapshot{
			State: agent.CatalogFresh, DetectedAt: 1,
			AgentModelCatalog: agent.AgentModelCatalog{Models: []agent.AgentChoice{{
				ID: "persisted-model", Config: &agent.AgentModelConfig{Options: []agent.AgentSessionOption{{
					ID: "reasoning_effort", Choices: []agent.AgentChoice{{ID: "high"}},
				}}},
			}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	environment.Detection.Version = "new-startup-version"
	probes := 0
	svc := &service{
		modelCatalog: agent.NewModelCatalog(nil), modelCatalogs: store,
		runtimeCatalog: &countingRuntimeCatalog{}, accountWorkspaceDirectory: root,
		resolveAccount: func(context.Context, agent.AccountRequest, string) (agent.AccountRequest, error) {
			probes++
			return agent.AccountRequest{}, status_error.RuntimeAuthRequired
		},
	}
	cached, err := svc.readModelCatalog(ctx, environment, "", false)
	if err != nil || probes != 0 || cached.State != agent.CatalogFresh || len(cached.Models) != 1 ||
		cached.Models[0].ID != "persisted-model" || cached.Models[0].Config.Options[0].Choices[0].ID != "high" {
		t.Fatalf("passive read did not reuse the durable catalog: %+v, probes=%d, err=%v", cached, probes, err)
	}
	other := environment
	other.ConfigHome = filepath.Join(root, "another-profile")
	missing, err := svc.readModelCatalog(ctx, other, "", false)
	if err != nil || missing.State != agent.CatalogSignedOut || probes != 1 {
		t.Fatalf("first read did not probe the new profile: %+v, probes=%d, err=%v", missing, probes, err)
	}
	repeat, err := svc.readModelCatalog(ctx, other, "", false)
	if err != nil || repeat.State != agent.CatalogSignedOut || probes != 1 {
		t.Fatalf("first probe outcome was not persisted: %+v, probes=%d, err=%v", repeat, probes, err)
	}
	fresh, err := svc.readModelCatalog(ctx, environment, "", true)
	if err != nil || fresh.State != agent.CatalogSignedOut || probes != 2 {
		t.Fatalf("explicit refresh did not check native credentials: %+v, probes=%d, err=%v", fresh, probes, err)
	}
	err = store.Store(ctx, models.RuntimeModelCatalog{
		RuntimeID: "codex", AccountID: agent.SystemAccountID,
		Fingerprint: persistedModelCatalogFingerprint(environment, 0, ""), ProbeStartedAt: 2,
		Snapshot: cached.ModelCatalogSnapshot,
	})
	if err != nil {
		t.Fatal(err)
	}
	restarted := &service{modelCatalogs: controllers.NewRuntimeModelCatalogController(database.DB)}
	reloaded, err := restarted.readModelCatalog(ctx, environment, "", false)
	if err != nil || reloaded.State != agent.CatalogSignedOut || len(reloaded.Models) != 0 {
		t.Fatalf("late probe overrode the durable signed-out result: %+v, err=%v", reloaded, err)
	}
}

func TestNativeACPPermissionsRequireAdvertisedModes(t *testing.T) {
	for _, provider := range []string{"workbuddy", "qoder", "qoder-cn"} {
		if levels := NativePermissionLevels(provider, agent.AgentChoice{}); len(levels) != 0 {
			t.Fatalf("%s invented permission menu: %v", provider, levels)
		}
		limited := NativePermissionLevels(provider, agent.AgentChoice{PermissionModes: []string{"default"}})
		if len(limited) != 1 || limited[0] != constants.PermissionLevelDefault {
			t.Fatalf("limited: %v", limited)
		}
		full := NativePermissionLevels(provider, agent.AgentChoice{PermissionModes: []string{"default", "auto", "bypassPermissions"}})
		if len(full) != 3 || full[2] != constants.PermissionLevelFullAccess {
			t.Fatalf("native alias ignored: %v", full)
		}
	}
}

func TestManagedCatalogExcludesOtherProviderModelsWithMatchingIDs(t *testing.T) {
	for _, provider := range []string{"opencode", "pi"} {
		native := accountconfig.NativeModelID(provider, "chosen")
		original := agent.ModelCatalogSnapshot{AgentModelCatalog: agent.AgentModelCatalog{DefaultID: "other/chosen", Models: []agent.AgentChoice{
			{ID: "chosen"}, {ID: "other/chosen"}, {ID: native}, {ID: accountconfig.NativeModelID(provider, "unselected")},
		}}}
		selected := selectManagedCatalog(provider, original, []string{"chosen"})
		if len(selected.Models) != 1 || selected.Models[0].ID != "chosen" || selected.DefaultID != "" {
			t.Fatalf("%s mixed managed and native credentials: %+v", provider, selected)
		}
		if len(original.Models) != 4 || original.Models[2].ID != native {
			t.Fatal("account projection changed the cached native catalog")
		}
	}
}

func TestNativeFamilyPermissionMenusFollowRealPolicies(t *testing.T) {
	for _, provider := range []string{"cursor", "gemini", "opencode", "grok", "kimi", "pi"} {
		if slices.Contains(NativePermissionLevels(provider, agent.AgentChoice{}), constants.PermissionLevelAuto) {
			t.Fatalf("%s invented automatic approval", provider)
		}
	}
	if levels := NativePermissionLevels("kimi", agent.AgentChoice{PermissionModes: []string{"default", "auto", "yolo"}}); len(levels) != 3 {
		t.Fatalf("Kimi advertised permission modes lost: %v", levels)
	}
	if levels := NativePermissionLevels("grok", agent.AgentChoice{AutoMode: true}); slices.Contains(levels, constants.PermissionLevelAuto) {
		t.Fatalf("Claude model metadata was used to enable Grok automatic approval: %v", levels)
	}
	if levels := NativePermissionLevels("pi", agent.AgentChoice{PermissionModes: []string{"auto"}}); len(levels) != 3 {
		t.Fatalf("Pi tool bridge permission policies lost: %v", levels)
	}
	if levels := NativePermissionLevels("claude", agent.AgentChoice{AutoMode: true}); slices.Contains(levels, constants.PermissionLevelFullAccess) {
		t.Fatalf("Claude bypass was shown without native capability: %v", levels)
	}
	if levels := NativePermissionLevels("claude", agent.AgentChoice{AutoMode: true, PermissionModes: []string{"bypassPermissions"}}); len(levels) != 3 {
		t.Fatalf("Claude model and process permission evidence was not combined: %v", levels)
	}
}
