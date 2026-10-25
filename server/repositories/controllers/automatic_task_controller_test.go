package controllers

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func automaticTaskTestRepository(t *testing.T) (AutomaticTaskController, TransactionController) {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "tasks.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	})
	return NewAutomaticTaskController(database.DB), NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
}

func automaticTaskFixture(wakerID string) models.AutomaticTask {
	return models.AutomaticTask{ID: util.New(), WakerID: wakerID, Revision: 1, ConfigurationVersion: 1, TriggerVersion: 1,
		TokenHash: []byte("only-a-digest"), AutomaticTaskConfiguration: models.AutomaticTaskConfiguration{
			Name: "日报整理", Instruction: "整理日报", Trigger: constants.AutomaticTaskTriggerSchedule, Enabled: true,
			Schedule:  models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindDaily, Timezone: "Asia/Shanghai", Hour: 9},
			RuntimeID: "codex", PermissionLevel: constants.PermissionLevelDefault,
		},
	}
}

func TestAutomaticTaskRepositoryCRUDAndScopes(t *testing.T) {
	repository, transactions := automaticTaskTestRepository(t)
	ctx := context.Background()
	wakerID := util.New()
	task := automaticTaskFixture(wakerID)
	other := automaticTaskFixture(util.New())
	if err := repository.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := repository.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	found, err := repository.Get(ctx, task.ID)
	if err != nil || found.Schedule.Hour != 9 || found.Schedule.Timezone != "Asia/Shanghai" {
		t.Fatalf("read = %+v, %v", found, err)
	}
	found.Name = "夜间归档"
	found.Enabled = false
	found.Schedule = models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindCron, Timezone: "UTC", Cron: "* * * * *"}
	found.Revision++
	found.ConfigurationVersion++
	if err := repository.UpdateConfiguration(ctx, found, 1); err != nil {
		t.Fatal(err)
	}
	if err := repository.UpdateConfiguration(ctx, found, 1); err != status_error.AutomaticTaskRevisionConflict {
		t.Fatalf("stale update = %v", err)
	}
	updated, err := repository.Get(ctx, task.ID)
	if err != nil || updated.Enabled || updated.Schedule.Cron != "* * * * *" || updated.Schedule.Hour != 0 {
		t.Fatalf("updated = %+v, %v", updated, err)
	}
	listed, count, err := repository.List(ctx, AutomaticTaskFilter{WakerID: wakerID, Query: "归档", Limit: 50})
	if err != nil || count != 1 || len(listed) != 1 || listed[0].ID != task.ID {
		t.Fatalf("list = %+v, %d, %v", listed, count, err)
	}
	_, count, err = repository.List(ctx, AutomaticTaskFilter{WakerID: wakerID, Query: "%"})
	if err != nil || count != 0 {
		t.Fatalf("literal search count = %d, %v", count, err)
	}
	counts, err := repository.Counts(ctx, wakerID)
	if err != nil || counts.Total != 1 || counts.Enabled != 0 || counts.Scheduled != 1 || counts.API != 0 {
		t.Fatalf("summary = %+v, %v", counts, err)
	}
	encoded, err := json.Marshal(updated)
	if err != nil || strings.Contains(string(encoded), "only-a-digest") || strings.Contains(string(encoded), "tokenHash") {
		t.Fatalf("secret exposed: %s, %v", encoded, err)
	}
	run := models.AutomaticRun{ID: util.New(), TaskID: task.ID, WakerID: wakerID, OccurrenceKey: "manual:request", Source: constants.AutomaticRunSourceManual, Admission: constants.AutomaticRunAdmissionPending, Configuration: updated.AutomaticTaskConfiguration}
	if err := repository.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := transactions.Transaction(ctx, func(tx context.Context) error { return repository.Delete(tx, task.ID) }); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Get(ctx, task.ID); err != status_error.AutomaticTaskNotFound {
		t.Fatalf("deleted task = %v", err)
	}
	if _, err := repository.GetRun(ctx, run.ID); err != status_error.AutomaticRunNotFound {
		t.Fatalf("deleted run = %v", err)
	}
	if _, err := repository.Get(ctx, other.ID); err != nil {
		t.Fatalf("other waker affected: %v", err)
	}
}

func TestAutomaticTaskClaimOnlyOnceAndRollback(t *testing.T) {
	repository, transactions := automaticTaskTestRepository(t)
	ctx := context.Background()
	task := automaticTaskFixture(util.New())
	if err := repository.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	results := make(chan bool, 2)
	failures := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			claimed, err := repository.ClaimRun(ctx, task.ID, 1, util.New(), nil)
			results <- claimed
			failures <- err
		}()
	}
	workers.Wait()
	close(results)
	close(failures)
	winners := 0
	for claimed := range results {
		if claimed {
			winners++
		}
	}
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("claim winners = %d", winners)
	}
	current, err := repository.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ReleaseRun(ctx, task.ID, "different-run"); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := repository.Get(ctx, task.ID)
	if unchanged.ActiveRunID != current.ActiveRunID {
		t.Fatal("unrelated release cleared the slot")
	}
	if err := repository.ReleaseRun(ctx, task.ID, current.ActiveRunID); err != nil {
		t.Fatal(err)
	}
	current, _ = repository.Get(ctx, task.ID)
	err = transactions.Transaction(ctx, func(tx context.Context) error {
		claimed, err := repository.ClaimRun(tx, task.ID, current.Revision, "rolled-back-run", nil)
		if err != nil {
			return err
		}
		if !claimed {
			t.Fatal("claim unexpectedly failed")
		}
		return status_error.AutomaticTaskRevisionConflict
	})
	if err != status_error.AutomaticTaskRevisionConflict {
		t.Fatalf("rollback = %v", err)
	}
	current, _ = repository.Get(ctx, task.ID)
	if current.ActiveRunID != "" {
		t.Fatal("failed transaction left the run slot occupied")
	}
}

func TestAutomaticRunUniqueOpportunityAndFrozenConfiguration(t *testing.T) {
	repository, _ := automaticTaskTestRepository(t)
	ctx := context.Background()
	task := automaticTaskFixture(util.New())
	if err := repository.Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	run := models.AutomaticRun{ID: util.New(), TaskID: task.ID, WakerID: task.WakerID, OccurrenceKey: "api:1:stable-key", RequestHash: "digest", Input: "本次附加要求", Source: constants.AutomaticRunSourceAPI, Configuration: task.AutomaticTaskConfiguration, ConfigurationRevision: 1, Admission: constants.AutomaticRunAdmissionPending}
	if err := repository.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	duplicate := run
	duplicate.ID = util.New()
	if err := repository.CreateRun(ctx, duplicate); err != status_error.AutomaticTaskRequestConflict {
		t.Fatalf("duplicate = %v", err)
	}
	found, exists, err := repository.FindRun(ctx, task.ID, run.OccurrenceKey)
	if err != nil || !exists || found.ID != run.ID || found.Configuration.Name != task.Name || found.Input != run.Input {
		t.Fatalf("replayed = %+v, %v, %v", found, exists, err)
	}
	found.ChatID = util.New()
	found.ExecutionID = util.New()
	found.Admission = constants.AutomaticRunAdmissionAccepted
	if err := repository.UpdateRunAdmission(ctx, found); err != nil {
		t.Fatal(err)
	}
	count, err := repository.CountAcceptedRuns(ctx, task.ID)
	if err != nil || count != 1 {
		t.Fatalf("accepted count = %d, %v", count, err)
	}
	page, count, err := repository.ListRuns(ctx, task.ID, 50, 0)
	if err != nil || count != 1 || len(page) != 1 || page[0].ChatID != found.ChatID {
		t.Fatalf("history = %+v, %d, %v", page, count, err)
	}
}
