package runtimeenvironment

import (
	"context"
	"encoding/json"
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

func TestModelCatalogReadVerifiesSystemSubjectBeforePersistedCache(t *testing.T) {
	root := t.TempDir()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(root, "catalog.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := database.DB.DB()
	defer pool.Close()
	store := controllers.NewRuntimeModelCatalogController(database.DB)
	environment := models.RuntimeEnvironment{RuntimeID: "codex", BaseRuntimeID: "codex", Binary: "codex"}
	first := agent.AccountRequest{Provider: "codex", AccountID: agent.SystemAccountID, Subject: "first", AuthGeneration: 1, ConfigHome: root}
	encoded, _ := json.Marshal([]any{first.Subject, first.ConfigHome, first.DataHome, first.ConfigFingerprint})
	fingerprint := persistedModelCatalogFingerprint(environment, first.AuthGeneration, string(encoded))
	if err := store.Store(t.Context(), models.RuntimeModelCatalog{RuntimeID: "codex", AccountID: agent.SystemAccountID, Fingerprint: fingerprint, AuthGeneration: 1, ProbeStartedAt: 1, Snapshot: agent.ModelCatalogSnapshot{State: agent.CatalogFresh, AgentModelCatalog: agent.AgentModelCatalog{Models: []agent.AgentChoice{{ID: "first-subject-model"}}}}}); err != nil {
		t.Fatal(err)
	}
	probes := 0
	current := first
	signedOut := false
	svc := &service{modelCatalogs: store, runtimeCatalog: &countingRuntimeCatalog{}, accountWorkspaceDirectory: root, resolveAccount: func(context.Context, agent.AccountRequest, string) (agent.AccountRequest, error) {
		probes++
		if signedOut {
			return agent.AccountRequest{}, status_error.RuntimeAuthRequired
		}
		return current, nil
	}}
	cached, err := svc.readModelCatalog(t.Context(), environment, "", false)
	if err != nil || cached.State != agent.CatalogFresh || len(cached.Models) != 1 || probes != 1 {
		t.Fatalf("same subject cache unavailable: %+v probes=%d err=%v", cached, probes, err)
	}
	current.Subject = "second"
	changed, err := svc.readModelCatalog(t.Context(), environment, "", false)
	if err != nil || len(changed.Models) != 0 || probes != 2 {
		t.Fatalf("new subject reused old catalog: %+v probes=%d err=%v", changed, probes, err)
	}
	signedOut = true
	loggedOut, err := svc.readModelCatalog(t.Context(), environment, "", false)
	if err != nil || loggedOut.State != agent.CatalogSignedOut || len(loggedOut.Models) != 0 || probes != 3 {
		t.Fatalf("logout reused old catalog: %+v probes=%d err=%v", loggedOut, probes, err)
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
		Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: constants.AIAccountOpenAIURL}}
	if err := accounts.Create(t.Context(), account, ""); err != nil {
		t.Fatal(err)
	}
	svc := &service{accounts: aiaccount.New(accounts)}
	_, err = svc.readModelCatalog(t.Context(), models.RuntimeEnvironment{RuntimeID: "codex", BaseRuntimeID: constants.RuntimeIDCodex}, account.ID, false)
	if !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
		t.Fatalf("catalog accepted another profile's signed-out account: %v", err)
	}
}
