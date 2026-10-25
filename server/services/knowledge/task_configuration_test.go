package knowledge

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"testing"
)

// modelFreezeGate 只控制独立模型解析时序，不实现通用执行端口。
type modelFreezeGate struct {
	entered chan struct{} // 通知首次模型选择已解析。
	resume  chan struct{} // 放行竞争请求。
}

func TestConcurrentOrganizeKeepsFirstPersistedConfiguration(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	_, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "raw-concurrency", LibraryID: lib.ID, Filename: "source.md", Bytes: []byte("# Evidence\nA source document.")})
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := s.config.References.UpdateLibraryExecutionDefaults(ctx, "pref1", 0, models.LibraryExecutionDefaults{LibraryID: lib.ID, RuntimeID: "codex", Model: "first"})
	if err != nil {
		t.Fatal(err)
	}
	gate := &modelFreezeGate{entered: make(chan struct{}), resume: make(chan struct{})}
	s.config.ResolveModel = func(ctx context.Context, value models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
		if value.Model == "first" {
			close(gate.entered)
			<-gate.resume
		}
		return value, nil
	}
	input := OrganizeCommand{RequestID: "same-organize", LibraryID: lib.ID, Configuration: models.KnowledgeTaskConfiguration{}}
	done := make(chan error, 1)
	go func() { _, _, e := s.Organize(ctx, input); done <- e }()
	<-gate.entered
	defaults.Model = "second"
	if _, err = s.config.References.UpdateLibraryExecutionDefaults(ctx, "pref2", defaults.Version, defaults); err != nil {
		t.Fatal(err)
	}
	receipt, accepted, err := s.Organize(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Model != "second" {
		t.Fatalf("test setup did not accept second config: %+v", accepted)
	}
	close(gate.resume)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	persisted, err := s.config.References.GetTaskConfiguration(ctx, input.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Model != accepted.Model {
		t.Fatalf("accepted job %s configured as %s; delayed same-request overwrote persisted model to %s", receipt.ResourceID, accepted.Model, persisted.Model)
	}
}

func TestOrganizeKeepsFrozenSelectionAfterCoreAcceptanceFailure(t *testing.T) {
	s, library := newServiceFixture(t)
	ctx := context.Background()
	defaults, err := s.config.References.UpdateLibraryExecutionDefaults(ctx, "before-accept", 0, models.LibraryExecutionDefaults{LibraryID: library.ID, RuntimeID: "codex", Model: "original", Reasoning: "high"})
	if err != nil {
		t.Fatal(err)
	}
	input := OrganizeCommand{RequestID: "accept-recovery", LibraryID: library.ID, SourceVersionIDs: []string{"missing-source"}, ExplicitSources: true, Configuration: models.KnowledgeTaskConfiguration{}}
	_, first, err := s.Organize(ctx, input)
	if err == nil || first.Model != "original" || first.Reasoning != "high" {
		t.Fatalf("expected core rejection with original selection: %+v %v", first, err)
	}
	defaults.Model = "changed-later"
	defaults.Reasoning = "low"
	if _, err = s.config.References.UpdateLibraryExecutionDefaults(ctx, "after-accept", defaults.Version, defaults); err != nil {
		t.Fatal(err)
	}
	_, retry, err := s.Organize(ctx, input)
	if err == nil || retry.Model != "original" || retry.Reasoning != "high" {
		t.Fatalf("partial acceptance recovery changed selection: %+v %v", retry, err)
	}
}
