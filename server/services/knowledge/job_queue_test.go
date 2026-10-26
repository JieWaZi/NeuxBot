package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
)

func TestQueuedAdmissionPublishesCommittedStateWithoutStartingModels(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	s.config.Events = events.New()
	subscription := s.config.Events.Subscribe("knowledge", events.Resource{})
	defer subscription.Close()
	_, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "raw", LibraryID: lib.ID, Filename: "source.md", Bytes: []byte("source")})
	if err != nil {
		t.Fatal(err)
	}
	receipt, _, err := s.Organize(ctx, OrganizeCommand{RequestID: "queue-admission", LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-subscription.Events:
		job, readErr := s.config.Engine.GetJob(ctx, receipt.JobID)
		if event.Type != "knowledge.changed" || event.Resource.ID != lib.ID || readErr != nil || job.State != "queued" {
			t.Fatal("queue event did not identify committed state", event, job, readErr)
		}
	default:
		t.Fatal("queued admission did not notify other resource readers")
	}
}

func TestDefaultOrganizeOnlyQueuesUnoccupiedSources(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	first, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "first-raw", LibraryID: lib.ID, Filename: "first.md", Bytes: []byte("first")})
	if err != nil {
		t.Fatal(err)
	}
	accepted, _, err := s.Organize(ctx, OrganizeCommand{RequestID: "first-job", LibraryID: lib.ID})
	if err != nil || accepted.JobID == "" {
		t.Fatal(accepted, err)
	}
	second, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "second-raw", LibraryID: lib.ID, Filename: "second.md", Bytes: []byte("second")})
	if err != nil {
		t.Fatal("upload while queued failed", err)
	}
	queued, _, err := s.Organize(ctx, OrganizeCommand{RequestID: "next-job", LibraryID: lib.ID, Message: "第二批要求"})
	if err != nil || queued.JobID == "" {
		t.Fatal("default selection included occupied sources", queued, err)
	}
	job, err := s.config.Engine.GetJob(ctx, queued.JobID)
	if err != nil || len(job.SourceVersionIDs) != 1 || job.SourceVersionIDs[0] != second.ResourceID {
		t.Fatal("wrong queued scope", job, err)
	}
	_, _, err = s.Organize(ctx, OrganizeCommand{RequestID: "duplicate", LibraryID: lib.ID, SourceVersionIDs: []string{first.ResourceID}})
	if !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatal("explicit occupied source accepted", err)
	}
	noop, _, err := s.Organize(ctx, OrganizeCommand{RequestID: "no-other-files", LibraryID: lib.ID})
	if err != nil || noop.State != "no-op" {
		t.Fatal("all sources already queued", noop, err)
	}
}

func TestQueuedServiceRestartFailsInterruptedJobAndRunsNextWithFrozenConfiguration(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	jobs := make([]*core.Receipt, 0, 2)
	for _, id := range []string{"first", "second"} {
		raw, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: id + "-raw", LibraryID: lib.ID, Filename: id + ".md", Bytes: []byte("# 资料\n" + id)})
		if err != nil {
			t.Fatal(err)
		}
		job, _, err := s.Organize(ctx, OrganizeCommand{RequestID: id + "-job", LibraryID: lib.ID, SourceVersionIDs: []string{raw.ResourceID}, Message: id + "要求", Configuration: models.KnowledgeTaskConfiguration{RuntimeID: "codex", Model: id + "-model"}})
		if err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, job)
	}
	for round := 0; round < 2; round++ {
		model := &blockedKnowledgeModel{entered: make(chan core.GenerateRequest, 2), stopped: make(chan struct{})}
		expectedModel := []string{"first-model", "second-model"}[round]
		s.config.Generator = func(_ context.Context, selected models.LibraryExecutionDefaults) (core.Generator, error) {
			if selected.Model != expectedModel {
				t.Errorf("queue selected model %q, want %q", selected.Model, expectedModel)
			}
			return model, nil
		}
		t.Cleanup(s.Close)
		if err := s.Start(ctx); err != nil {
			t.Fatal(err)
		}
		select {
		case request := <-model.entered:
			if request.JobID != jobs[round].JobID {
				t.Fatal("later job overtook running task", request.JobID)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("queue did not recover")
		}
		s.Close()
		first, _ := s.config.Engine.GetJob(ctx, jobs[0].JobID)
		second, _ := s.config.Engine.GetJob(ctx, jobs[1].JobID)
		if round == 0 && (first.State != "processing" || second.State != "queued") {
			t.Fatal("shutdown changed persistent queue", first, second)
		}
		if round == 1 && (first.State != "failed" || second.State != "processing") {
			t.Fatal("restart did not fail interrupted work and advance queue", first, second)
		}
		config, err := s.config.References.GetTaskConfiguration(ctx, "second-job")
		if err != nil || config.Model != "second-model" || config.Message != "second要求" {
			t.Fatal("queued configuration changed", config, err)
		}
		s = New(s.config)
	}
}

func TestDefaultOrganizeExcludesFailedSourceStillOwnedByActiveTask(t *testing.T) {
	s, lib, path, _ := newServiceFixtureWithDB(t)
	ctx := context.Background()
	first, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "first-raw", LibraryID: lib.ID, Filename: "first.md", Bytes: []byte("first")})
	if err != nil {
		t.Fatal(err)
	}
	accepted, _, err := s.Organize(ctx, OrganizeCommand{RequestID: "first-job", LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.config.Engine.EnsureOrganizeSourceWork(ctx, accepted.JobID); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("UPDATE knowledge_organize_source_work SET state='dead',diagnostic='failed in current task' WHERE job_id=? AND source_version_id=?", accepted.JobID, first.ResourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE knowledge_source_versions SET state='failed' WHERE id=?", first.ResourceID); err != nil {
		t.Fatal(err)
	}
	second, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "second-raw", LibraryID: lib.ID, Filename: "second.md", Bytes: []byte("second")})
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := s.Organize(ctx, OrganizeCommand{RequestID: "next-job", LibraryID: lib.ID})
	if err != nil {
		t.Fatal("active failed source blocked other files", err)
	}
	job, err := s.config.Engine.GetJob(ctx, next.JobID)
	if err != nil || len(job.SourceVersionIDs) != 1 || job.SourceVersionIDs[0] != second.ResourceID {
		t.Fatal("wrong unoccupied scope", job, err)
	}
}

func TestRecoveryContinuesQueueAfterEachPreparationFailure(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	var jobs []*core.Receipt
	for _, id := range []string{"first", "second", "third"} {
		raw, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: id + "-raw", LibraryID: lib.ID, Filename: id + ".md", Bytes: []byte(id)})
		if err != nil {
			t.Fatal(err)
		}
		job, _, err := s.Organize(ctx, OrganizeCommand{RequestID: id + "-job", LibraryID: lib.ID, SourceVersionIDs: []string{raw.ResourceID}, Configuration: models.KnowledgeTaskConfiguration{RuntimeID: "codex", Model: id}})
		if err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, job)
	}
	selected := make(chan string, 3)
	s.config.Generator = func(_ context.Context, in models.LibraryExecutionDefaults) (core.Generator, error) {
		selected <- in.Model
		return nil, status_error.RuntimeUnavailable
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, expected := range []string{"first", "second", "third"} {
		select {
		case got := <-selected:
			if got != expected {
				t.Fatalf("frozen queue order: got %s want %s", got, expected)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("recovery did not continue queued tasks", expected)
		}
	}
	s.workers.Wait()
	for _, receipt := range jobs {
		job, err := s.config.Engine.GetJob(ctx, receipt.JobID)
		if err != nil || job.State != "failed" {
			t.Fatal("queued task not settled", job, err)
		}
	}
}

func TestRecoveryContinuesAfterCancellationBarrier(t *testing.T) {
	s, lib, path, _ := newServiceFixtureWithDB(t)
	ctx := context.Background()
	first := regressionJob(t, s, lib)
	raw, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "next-raw", LibraryID: lib.ID, Filename: "next.md", Bytes: []byte("下一份")})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := s.Organize(ctx, OrganizeCommand{RequestID: "next-job", LibraryID: lib.ID, SourceVersionIDs: []string{raw.ResourceID}})
	if err != nil {
		t.Fatal(err)
	}
	// 模拟进程已持久保存取消屏障，但尚未来得及完成取消的崩溃窗口。
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("UPDATE knowledge_jobs SET cancel_requested=1,state='cancel_requested' WHERE id=?", first.ID); err != nil {
		t.Fatal(err)
	}
	selected := make(chan string, 1)
	s.config.Generator = func(_ context.Context, in models.LibraryExecutionDefaults) (core.Generator, error) {
		selected <- in.Model
		return nil, status_error.RuntimeUnavailable
	}
	if err = s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	select {
	case <-selected:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled head did not release next queued task")
	}
	s.workers.Wait()
	cancelled, _ := s.config.Engine.GetJob(ctx, first.ID)
	next, _ := s.config.Engine.GetJob(ctx, second.JobID)
	if cancelled.State != "cancelled" || next.State != "failed" {
		t.Fatal(cancelled, next)
	}
}

func TestRecoverySettlesCancellationWithoutModelConfiguration(t *testing.T) {
	s, lib, path, _ := newServiceFixtureWithDB(t)
	ctx := context.Background()
	job := regressionJob(t, s, lib)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("UPDATE knowledge_jobs SET cancel_requested=1,state='cancel_requested' WHERE id=?", job.ID); err != nil {
		t.Fatal(err)
	}
	// 取消已经持久化，收束它不应再依赖已丢失的模型配置。
	s.config.References = startupConfigurationReferences{KnowledgeController: s.config.References, configurationError: status_error.KnowledgeNotFound}
	defer s.Close()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("saved cancellation was blocked by model configuration: %v", err)
	}
	s.workers.Wait()
	current, err := s.config.Engine.GetJob(ctx, job.ID)
	if err != nil || current.State != "cancelled" {
		t.Fatalf("saved cancellation did not settle: %+v %v", current, err)
	}
}
