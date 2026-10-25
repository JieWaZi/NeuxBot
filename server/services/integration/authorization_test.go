package integration

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/neux-knowledge/core"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	rules "github.com/JieWaZi/NeuxBot/server/internal/integration"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func integrationFixture(t *testing.T) (*Service, controllers.IntegrationKeyController, models.IntegrationKey) {
	t.Helper()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "integration.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB.DB()
	t.Cleanup(func() { _ = pool.Close() })
	store := controllers.NewIntegrationKeyController(db.DB)
	ctx := context.Background()
	_, _, err = store.ClaimMutation(ctx, models.IntegrationKeyMutation{RequestID: "create", Digest: "fixture", KeyID: "key-a", Action: "create"})
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.CompleteMutation(ctx, "create", "fixture", models.IntegrationKey{ID: "key-a", Name: "集成", KeyHash: rules.HashKey("fixture-only"), Enabled: true, Grants: []models.IntegrationGrant{{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceIDs: []string{"a"}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityOrganize}}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return New(Config{Keys: store}), store, key
}

func TestIntegrationAuthenticationAndAllRequiredCapabilities(t *testing.T) {
	s, _, _ := integrationFixture(t)
	ctx := context.Background()
	id, err := s.Authenticate(ctx, "fixture-only")
	if err != nil || id.KeyID != "key-a" {
		t.Fatal(id, err)
	}
	for _, token := range []string{"", "invalid"} {
		if _, err := s.Authenticate(ctx, token); !errors.Is(err, status_error.IntegrationUnauthorized) {
			t.Fatal("invalid credential accepted", err)
		}
	}
	if _, err := s.Authorize(ctx, id, constants.IntegrationResourceKindKnowledge, "a", []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityOrganize}); err != nil {
		t.Fatal(err)
	}
	for _, resource := range []string{"", "b"} {
		if _, err := s.Authorize(ctx, id, constants.IntegrationResourceKindKnowledge, resource, []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve}); !errors.Is(err, status_error.IntegrationForbidden) {
			t.Fatal("scope bypass", resource, err)
		}
	}
	if _, err := s.Authorize(ctx, id, constants.IntegrationResourceKindKnowledge, "a", []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityIngest}); !errors.Is(err, status_error.IntegrationForbidden) {
		t.Fatal("any-of capability bypass", err)
	}
}

func TestIntegrationGuardSerializesRevocationAndRestore(t *testing.T) {
	s, store, key := integrationFixture(t)
	ctx := context.Background()
	id, _ := s.Authenticate(ctx, "fixture-only")
	auth, _ := s.Authorize(ctx, id, constants.IntegrationResourceKindKnowledge, "a", []constants.IntegrationCapability{constants.IntegrationCapabilityOrganize})
	release, err := s.AcquireGuard(ctx, auth)
	if err != nil {
		t.Fatal(err)
	}
	started, finished := make(chan struct{}), make(chan error, 1)
	go func() {
		close(started)
		finished <- s.WithAuthorizationWrite(ctx, func(ctx context.Context) error {
			key.Enabled = false
			_, err := store.UpdateAuthorization(ctx, key, key.Revision)
			return err
		})
	}()
	<-started
	select {
	case err := <-finished:
		t.Fatal("revocation returned while publication still owns its guard", err)
	case <-time.After(20 * time.Millisecond):
	}
	release()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if release, err := s.AcquireGuard(ctx, auth); err == nil {
		release()
		t.Fatal("late publication allowed after revocation")
	}
	restarted := New(Config{Keys: store})
	if release, err := restarted.AcquireGuard(ctx, auth); err == nil {
		release()
		t.Fatal("restart restored a revoked grant")
	}
}

func TestTaskDeletionWaitsForAuthorizationGuardAndRemovesGrants(t *testing.T) {
	ctx := context.Background()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "delete.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB.DB()
	defer pool.Close()
	keys := controllers.NewIntegrationKeyController(db.DB)
	key := models.IntegrationKey{ID: "key", Name: "集成", KeyHash: rules.HashKey("fixture"), Revision: 1, GrantVersion: 1, CredentialGeneration: 1, Enabled: true, Grants: []models.IntegrationGrant{{ResourceKind: constants.IntegrationResourceKindAutomaticTask, ResourceIDs: []string{"task"}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityTaskInvoke}}}}
	if err := db.DB.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	rawTasks := controllers.NewAutomaticTaskController(db.DB)
	s := New(Config{Keys: keys, Tasks: rawTasks})
	tasks := s.CoordinateTasks(rawTasks)
	task := models.AutomaticTask{ID: "task", AutomaticTaskConfiguration: models.AutomaticTaskConfiguration{Enabled: true, Trigger: constants.AutomaticTaskTriggerAPI}}
	if err := tasks.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	identity, err := s.Authenticate(ctx, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := s.Authorize(ctx, identity, constants.IntegrationResourceKindAutomaticTask, "task", []constants.IntegrationCapability{constants.IntegrationCapabilityTaskInvoke})
	if err != nil {
		t.Fatal(err)
	}
	release, err := s.AcquireGuard(ctx, auth)
	if err != nil {
		t.Fatal(err)
	}
	started, done := make(chan struct{}), make(chan error, 1)
	go func() { close(started); done <- tasks.Delete(ctx, "task") }()
	<-started
	select {
	case err := <-done:
		release()
		t.Fatal("持有提交屏障时删除提前完成", err)
	case <-time.After(20 * time.Millisecond):
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	current, err := keys.Get(ctx, key.ID)
	if err != nil || len(current.Grants) != 0 || current.GrantVersion != 2 {
		t.Fatal("任务删除未撤销授权", err)
	}
	if release, err := s.AcquireGuard(ctx, auth); err == nil {
		release()
		t.Fatal("删除后旧授权仍可提交")
	}
}

func TestKnowledgeBatchGuardRetainsLockForLocalAutomaticRun(t *testing.T) {
	s, _, _ := integrationFixture(t)
	ctx := context.Background()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "tasks.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB.DB()
	defer pool.Close()
	s.config.Tasks = controllers.NewAutomaticTaskController(db.DB)
	if err = s.config.Tasks.CreateRun(ctx, models.AutomaticRun{ID: "local-run", TaskID: "local-task", WakerID: "fixture", OccurrenceKey: "local"}); err != nil {
		t.Fatal(err)
	}
	identity, _ := s.Authenticate(ctx, "fixture-only")
	auth, _ := s.Authorize(ctx, identity, constants.IntegrationResourceKindKnowledge, "a", []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityOrganize})
	raw, _ := json.Marshal(KnowledgeRecovery{Origin: "direct_api", Subject: identity.KeyID, Authorization: auth, Permission: core.PermissionSnapshot{PolicyVersion: 1, Operations: []string{"organize"}}})
	release, err := s.RestoreKnowledgeGuards(ctx, []json.RawMessage{raw, raw}, "local-run")
	if err != nil {
		t.Fatal(err)
	}
	started, done := make(chan struct{}), make(chan error, 1)
	go func() {
		close(started)
		done <- s.WithAuthorizationWrite(ctx, func(context.Context) error { return nil })
	}()
	<-started
	select {
	case err := <-done:
		t.Fatal("local automatic run dropped publication lock", err)
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("batch publication lock did not release")
	}
	if release, err = s.RestoreKnowledgeGuards(ctx, []json.RawMessage{raw}, "missing"); err == nil {
		release()
		t.Fatal("missing run accepted")
	}
}
