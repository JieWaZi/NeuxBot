package integration

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"path/filepath"
	"testing"
)

func TestIntegrationAutomaticTaskKnowledgeScopeAndRevocation(t *testing.T) {
	s, keys, key := integrationFixture(t)
	ctx := context.Background()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "tasks.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB.DB()
	defer pool.Close()
	tasks := controllers.NewAutomaticTaskController(db.DB)
	s.config.Tasks = tasks
	key.Grants = []models.IntegrationGrant{{ResourceKind: constants.IntegrationResourceKindAutomaticTask, ResourceIDs: []string{"task"}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityTaskInvoke}}}
	key, err = keys.UpdateAuthorization(ctx, key, key.Revision)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := s.Authenticate(ctx, "fixture-only")
	if err != nil {
		t.Fatal(err)
	}
	frozen := &models.KnowledgeMaintenanceAuthorization{Backend: "local", ConnectionID: "local", KnowledgeBaseID: "library", Operation: "organize", SourceVersionIDs: []string{"one"}}
	task := models.AutomaticTask{ID: "task", WakerID: "waker", Revision: 1, ConfigurationVersion: 1, AutomaticTaskConfiguration: models.AutomaticTaskConfiguration{Name: "原名称", Enabled: true, Trigger: constants.AutomaticTaskTriggerAPI, Knowledge: frozen}}
	if err = tasks.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	run := models.AutomaticRun{ID: "run", TaskID: task.ID, WakerID: task.WakerID, OccurrenceKey: "key-a", Source: constants.AutomaticRunSourceAPI, Configuration: task.AutomaticTaskConfiguration, ConfigurationRevision: 1, IntegrationInvocation: &models.IntegrationInvocation{Identity: identity, TaskID: task.ID, TaskConfigurationVersion: 1}}
	if err = tasks.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	check := func(allowed bool) {
		t.Helper()
		release, err := s.AcquireInvocationGuard(ctx, run.ID)
		if allowed && err != nil {
			t.Fatal(err)
		}
		if !allowed && err == nil {
			release()
			t.Fatal("invalid original task authority allowed")
		}
		if err == nil {
			release()
		}
	}
	check(true)
	// 不需要直接知识权限，任务原范围才是整理授权。
	if _, err = s.Authorize(ctx, identity, constants.IntegrationResourceKindKnowledge, "library", []constants.IntegrationCapability{constants.IntegrationCapabilityOrganize}); err == nil {
		t.Fatal("fixture unexpectedly has direct knowledge permission")
	}
	change := func(next models.AutomaticTask) {
		t.Helper()
		if err = s.WithAuthorizationWrite(ctx, func(ctx context.Context) error { return tasks.UpdateConfiguration(ctx, next, task.Revision) }); err != nil {
			t.Fatal(err)
		}
		task = next
	}
	next := task
	next.Name = "新名称"
	next.Model = "future-model"
	next.ConfigurationVersion++
	next.Revision++
	change(next)
	check(true)
	next = task
	expanded := *frozen
	expanded.SourceVersionIDs = []string{"one", "two"}
	next.Knowledge = &expanded
	next.ConfigurationVersion++
	next.Revision++
	change(next)
	check(true)
	next = task
	shrunk := *frozen
	shrunk.SourceVersionIDs = []string{"two"}
	next.Knowledge = &shrunk
	next.ConfigurationVersion++
	next.Revision++
	change(next)
	check(false)
	next = task
	next.Knowledge = frozen
	next.ConfigurationVersion++
	next.Revision++
	change(next)
	check(true)
	key.Enabled = false
	if _, err = keys.UpdateAuthorization(ctx, key, key.Revision); err != nil {
		t.Fatal(err)
	}
	check(false)
	restarted := New(Config{Keys: keys, Tasks: tasks})
	if release, err := restarted.AcquireInvocationGuard(ctx, run.ID); err == nil {
		release()
		t.Fatal("restart restored revoked caller")
	}
}
