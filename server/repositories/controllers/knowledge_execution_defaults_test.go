package controllers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestLibraryExecutionDefaultsRetryTransientBusyAtomically(t *testing.T) {
	db := newTestDB(t)
	repository := NewKnowledgeController(db)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	lock := db.WithContext(ctx).Begin()
	if lock.Error != nil {
		t.Fatal(lock.Error)
	}
	defer lock.Rollback()
	if err := lock.Create(&models.Waker{ID: "held-defaults", Name: "held-defaults"}).Error; err != nil {
		t.Fatal(err)
	}
	released := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		released <- lock.Commit().Error
	}()
	in := models.LibraryExecutionDefaults{LibraryID: "busy-library", RuntimeID: "codex", Model: "chosen"}
	saved, err := repository.UpdateLibraryExecutionDefaults(ctx, "busy-save", 0, in)
	if releaseErr := <-released; releaseErr != nil {
		t.Fatal(releaseErr)
	}
	if err != nil {
		t.Fatalf("transient busy must retry the complete defaults transaction: %v", err)
	}
	if saved.Version != 1 || saved.Model != "chosen" {
		t.Fatalf("retry did not commit the original configuration exactly once: %+v", saved)
	}
	replayed, err := repository.UpdateLibraryExecutionDefaults(ctx, "busy-save", 0, in)
	if err != nil || replayed.Version != 1 || replayed.UpdatedAt != saved.UpdatedAt {
		t.Fatalf("retried transaction lost its immutable receipt: %+v %v", replayed, err)
	}
}

func TestLibraryExecutionDefaultsPersistCASAndStableReplay(t *testing.T) {
	db := newTestDB(t)
	repository := NewKnowledgeController(db)
	ctx := context.Background()
	empty, err := repository.LibraryExecutionDefaults(ctx, "library")
	if err != nil || empty.Version != 0 || empty.LibraryID != "library" {
		t.Fatalf("missing defaults=%+v %v", empty, err)
	}
	initial := models.LibraryExecutionDefaults{LibraryID: "library", RuntimeID: "codex", AccountID: "selected-account", Model: "model", Reasoning: "high"}
	first, err := repository.UpdateLibraryExecutionDefaults(ctx, "first", 0, initial)
	if err != nil || first.Version != 1 || first.Reasoning != "high" {
		t.Fatalf("first=%+v %v", first, err)
	}
	initial.Model = "new-model"
	initial.Reasoning = ""
	second, err := repository.UpdateLibraryExecutionDefaults(ctx, "second", 1, initial)
	if err != nil || second.Version != 2 {
		t.Fatalf("second=%+v %v", second, err)
	}
	initial.Model = "model"
	initial.Reasoning = "high"
	replay, err := repository.UpdateLibraryExecutionDefaults(ctx, "first", 0, initial)
	if err != nil || replay.Version != 1 || replay.Model != "model" || replay.Reasoning != "high" {
		t.Fatalf("replay=%+v %v", replay, err)
	}
	_, err = repository.UpdateLibraryExecutionDefaults(ctx, "stale", 1, initial)
	if !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatalf("stale write accepted: %v", err)
	}
	initial.LibraryID = "another"
	_, err = repository.UpdateLibraryExecutionDefaults(ctx, "first", 0, initial)
	if !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatalf("request reused across libraries: %v", err)
	}
	repository = NewKnowledgeController(db)
	saved, err := repository.LibraryExecutionDefaults(ctx, "library")
	if err != nil || saved.Version != 2 || saved.Model != "new-model" || saved.AccountID != "selected-account" || saved.Reasoning != "" {
		t.Fatalf("restart read=%+v %v", saved, err)
	}
	other, err := repository.LibraryExecutionDefaults(ctx, "another")
	if err != nil || other.Version != 0 {
		t.Fatalf("library defaults leaked: %+v %v", other, err)
	}
}
