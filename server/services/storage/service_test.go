package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	filesystem "github.com/JieWaZi/NeuxBot/server/internal/storage"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
)

func TestStoragePolicyRequiresPreviewAndKeepsFirstDeadlineAcrossRestart(t *testing.T) {
	directory := t.TempDir()
	now := time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)
	config := Config{PolicyDirectory: directory, Now: func() time.Time { return now }, Candidates: &emptyRetentionCandidates{}}
	service := New(t.Context(), config)
	t.Cleanup(service.Close)
	initial := service.Policy()
	if initial.Enabled || initial.RetentionDays != 15 || !initial.Valid {
		t.Fatalf("new policy = %+v", initial)
	}
	if err := service.UpdatePolicy(true, 30, initial.Revision, ""); !errors.Is(err, status_error.StoragePreviewExpired) {
		t.Fatalf("accepted enable without impact preview: %v", err)
	}
	preview, err := service.PreviewExpiration(t.Context(), 30, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.UpdatePolicy(true, 31, initial.Revision, preview.ID); !errors.Is(err, status_error.StoragePreviewExpired) {
		t.Fatalf("accepted a preview of different days: %v", err)
	}
	if err := service.UpdatePolicy(true, 30, initial.Revision, preview.ID); err != nil {
		t.Fatal(err)
	}
	stored := service.Policy()
	if want := now.Add(24 * time.Hour).UnixMilli(); !stored.Enabled || stored.NextCheckAt != want {
		t.Fatalf("enabled policy = %+v, deadline %d", stored, want)
	}
	service.Close()
	now = now.Add(time.Minute)
	restarted := New(t.Context(), config)
	t.Cleanup(restarted.Close)
	if got := restarted.Policy(); got != stored {
		t.Fatalf("restarted policy = %+v, want %+v", got, stored)
	}
}

func TestStorageCorruptPolicyCannotAuthorizeWorkOrOverwriteFile(t *testing.T) {
	directory := t.TempDir()
	policyFile := filepath.Join(directory, "storage-policy.json")
	if err := os.WriteFile(policyFile, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	service := New(t.Context(), Config{PolicyDirectory: directory})
	t.Cleanup(service.Close)
	if got := service.Policy(); got.Valid || got.Enabled {
		t.Fatalf("invalid policy = %+v", got)
	}
	if err := service.UpdatePolicy(false, 15, 0, ""); !errors.Is(err, status_error.StoragePolicyInvalid) {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(policyFile); err != nil || string(got) != "{broken" {
		t.Fatalf("corrupt policy overwritten: %q, %v", got, err)
	}
}

type emptyRetentionCandidates struct{}

func (*emptyRetentionCandidates) ListRetentionCandidates(context.Context, int64, int64, string, int) ([]controllers.ChatRetentionCandidate, error) {
	return nil, nil
}

func (*emptyRetentionCandidates) CountRetentionCandidates(context.Context, int64) (int64, error) {
	return 0, nil
}

func (*emptyRetentionCandidates) ListRetentionCandidatesByIDs(context.Context, int64, []string) ([]controllers.ChatRetentionCandidate, error) {
	return nil, nil
}

func TestStorageUsageDropsScanStartedBeforeInvalidation(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	service := New(t.Context(), Config{PolicyDirectory: t.TempDir(), Measure: func(context.Context, filesystem.Roots, filesystem.Limits) (filesystem.Measurement, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
			return filesystem.Measurement{Bytes: 7}, nil
		}
		return filesystem.Measurement{Bytes: 19}, nil
	}})
	t.Cleanup(service.Close)
	first := make(chan int64, 1)
	go func() { snapshot, _ := service.Usage(t.Context(), false); first <- snapshot.Measurement.Bytes }()
	<-entered
	service.Invalidate()
	next, err := service.Usage(t.Context(), false)
	close(release)
	if err != nil || next.Measurement.Bytes != 19 {
		t.Fatalf("new scan = %+v, %v", next, err)
	}
	if got := <-first; got != 19 {
		t.Fatalf("stale bytes were returned: %d", got)
	}
	cached, err := service.Usage(t.Context(), false)
	if err != nil || cached.Measurement.Bytes != 19 || calls.Load() != 2 {
		t.Fatalf("cache = %+v, calls = %d, error = %v", cached, calls.Load(), err)
	}
}

func TestStorageCleanupReportsCompletedAndFailedOwners(t *testing.T) {
	failed := make(chan struct{})
	kinds := []constants.StorageCleanupKind{constants.StorageCleanupKindCaches, constants.StorageCleanupKindRuntimes, constants.StorageCleanupKindLogs}
	usage := func(context.Context) (int64, error) { return 28, nil }
	service := New(t.Context(), Config{PolicyDirectory: t.TempDir(), Cleanup: map[constants.StorageCleanupKind]CleanupOwner{
		kinds[0]: {Preview: usage, Remove: func(context.Context) (ResourceCleanup, error) { return ResourceCleanup{Bytes: 25}, nil }},
		kinds[1]: {Preview: usage, Remove: func(context.Context) (ResourceCleanup, error) {
			close(failed)
			return ResourceCleanup{Bytes: 3}, status_error.StorageCleanupFailed
		}},
	}})
	t.Cleanup(service.Close)
	preview, err := service.PreviewCleanup(t.Context(), kinds)
	if err != nil || len(preview.Items) != 3 || preview.Items[2].Available {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	id, err := service.StartCleanup(preview.ID)
	if id == "" || err != nil {
		t.Fatalf("run = %q, %v", id, err)
	}
	<-failed
	service.Close()
	last := service.Maintenance().Last
	if last == nil || last.ID != id || last.RemovedBytes != 28 || last.Failed != 1 || len(last.Items) != 2 {
		t.Fatalf("result = %+v", last)
	}
	if !last.Items[1].Failed {
		t.Fatalf("runtime failure was lost: %+v", last.Items)
	}
}

func TestStorageCleanupPreviewAndAdmissionKeepEachRequestDistinct(t *testing.T) {
	caches := constants.StorageCleanupKindCaches
	programs := constants.StorageCleanupKindRuntimes
	started := make(chan struct{})
	finish := make(chan struct{})
	service := New(t.Context(), Config{PolicyDirectory: t.TempDir(), Cleanup: map[constants.StorageCleanupKind]CleanupOwner{
		caches: {
			Preview: func(context.Context) (int64, error) { return 12, nil },
			Remove: func(context.Context) (ResourceCleanup, error) {
				close(started)
				<-finish
				return ResourceCleanup{Bytes: 12}, nil
			},
		},
		programs: {
			Preview: func(context.Context) (int64, error) { return 0, nil },
			Remove:  func(context.Context) (ResourceCleanup, error) { return ResourceCleanup{Bytes: 2}, nil },
		},
	}})
	t.Cleanup(service.Close)
	defer close(finish)
	preview, err := service.PreviewCleanup(t.Context(), []constants.StorageCleanupKind{caches, programs})
	if err != nil {
		t.Fatal(err)
	}
	if saved := service.previews[preview.ID]; len(saved.resources) != 1 || saved.resources[0] != caches {
		t.Errorf("preview granted permission for a category with zero measured bytes: %+v", saved)
	}
	other, err := service.PreviewCleanup(t.Context(), []constants.StorageCleanupKind{caches})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartCleanup(preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if id, err := service.StartCleanup("missing"); id != "" || !errors.Is(err, status_error.StoragePreviewExpired) {
		t.Errorf("invalid preview returned existing run %q with error %v", id, err)
	}
	if id, err := service.StartCleanup(other.ID); id != "" || !errors.Is(err, status_error.StatusError(40900009705)) {
		t.Errorf("distinct preview joined existing run %q with error %v", id, err)
	}
	if id, err := service.StartCleanup(preview.ID); id != run || err != nil {
		t.Errorf("identical preview replay returned %q (%v), want %q", id, err, run)
	}
}

type scheduledRetentionCandidates struct {
	// emptyRetentionCandidates 提供测试不使用的读取方式。
	emptyRetentionCandidates
	// rows 是以活动游标排序的实际删除候选。
	rows []controllers.ChatRetentionCandidate
}

func (c *scheduledRetentionCandidates) ListRetentionCandidates(context.Context, int64, int64, string, int) ([]controllers.ChatRetentionCandidate, error) {
	return c.rows, nil
}

func TestAutomaticRetentionStopsAfterPolicyChangesInsideBatch(t *testing.T) {
	now := time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)
	directory := t.TempDir()
	err := savePolicy(filepath.Join(directory, constants.StoragePolicyFileName), policyFile{Policy: Policy{
		Enabled: true, RetentionDays: 15, Revision: 1, NextCheckAt: now.UnixMilli(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	first := make(chan struct{})
	resume := make(chan struct{})
	var count atomic.Int32
	service := New(t.Context(), Config{PolicyDirectory: directory, Now: func() time.Time { return now },
		Candidates: &scheduledRetentionCandidates{rows: []controllers.ChatRetentionCandidate{{ChatID: "first"}, {ChatID: "second"}}},
		DeleteExpired: func(ctx context.Context, row controllers.ChatRetentionCandidate, _ int64, allow func(context.Context, string) (bool, error)) (bool, error) {
			count.Add(1)
			ok, err := allow(ctx, row.ChatID)
			if row.ChatID == "first" {
				close(first)
				<-resume
			}
			return ok, err
		}})
	t.Cleanup(service.Close)
	service.Start()
	select {
	case <-first:
	case <-time.After(2 * time.Second):
		close(resume)
		t.Fatal("due automatic check did not run")
	}
	err = service.UpdatePolicy(false, 15, 1, "")
	close(resume)
	if err != nil {
		t.Fatal(err)
	}
	service.Close()
	state := service.Maintenance()
	if count.Load() != 1 || state.Last == nil || state.Last.Deleted != 1 || state.Last.Skipped != 1 {
		t.Fatalf("wrong final batch: %+v, attempts %d", state.Last, count.Load())
	}
}

func TestStorageReportsFailureToPersistCleanupResult(t *testing.T) {
	directory := t.TempDir()
	service := New(t.Context(), Config{PolicyDirectory: directory})
	t.Cleanup(service.Close)
	if err := os.Mkdir(filepath.Join(directory, constants.StoragePolicyFileName), 0700); err != nil {
		t.Fatal(err)
	}
	service.finishRun(Run{ID: "manual", Kind: "resources"}, false, 0)
	if last := service.Maintenance().Last; last == nil || last.Failed != 1 {
		t.Fatalf("state hid persistence error: %+v", last)
	}
}
