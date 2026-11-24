package memory

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestCoreMemoryBudgetUsesSerializedUTF8(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	mc := Context{PartnerID: "partner"}
	firstText := strings.Repeat("中", 1500)
	secondText := strings.Repeat("\"", 1788) + "a"
	first, err := service.Write(t.Context(), mc, firstText)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Write(t.Context(), mc, secondText)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetCore(t.Context(), mc.PartnerID, first.Memory.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := service.SetCore(t.Context(), mc.PartnerID, second.Memory.ID, true); err != nil {
		t.Fatal(err)
	}
	before, err := service.CoreSnapshot(t.Context(), mc.PartnerID)
	if err != nil || len(before.JSON) != 8192 {
		t.Fatalf("snapshot bytes = %d, error = %v", len(before.JSON), err)
	}
	if err := service.Update(t.Context(), mc, second.Memory.ID, secondText+"c"); !errors.Is(err, status_error.MemoryCoreBudgetExceeded) {
		t.Fatalf("over-budget update = %v", err)
	}
	after, err := service.CoreSnapshot(t.Context(), mc.PartnerID)
	if err != nil || before.Revision != after.Revision || string(before.JSON) != string(after.JSON) {
		t.Fatalf("rejected update changed durable snapshot, error = %v", err)
	}
}

func TestConcurrentCoreSelectionHonorsBudget(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	mc := Context{PartnerID: "partner"}
	var ids []string
	for _, text := range []string{strings.Repeat("中", 1500), strings.Repeat("文", 1500)} {
		result, err := service.Write(t.Context(), mc, text)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, result.Memory.ID)
	}
	results := make(chan error, len(ids))
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Go(func() { results <- service.SetCore(t.Context(), mc.PartnerID, id, true) })
	}
	wg.Wait()
	close(results)
	var successful, refused int
	for err := range results {
		switch {
		case err == nil:
			successful++
		case errors.Is(err, status_error.MemoryCoreBudgetExceeded):
			refused++
		default:
			t.Fatal(err)
		}
	}
	if successful != 1 || refused != 1 {
		t.Fatalf("successful = %d, refused = %d", successful, refused)
	}
	snapshot, err := service.CoreSnapshot(t.Context(), mc.PartnerID)
	if err != nil || snapshot.Revision != 1 || len(snapshot.JSON) >= 8192 {
		t.Fatalf("revision = %d, bytes = %d, error = %v", snapshot.Revision, len(snapshot.JSON), err)
	}
}

func TestCoreRevisionAdvancesOnlyForSnapshotChanges(t *testing.T) {
	service := newTestService(t, EmbeddingConfig{}, nil)
	mc := Context{PartnerID: "partner"}
	result, err := service.Write(t.Context(), mc, "retained fact")
	if err != nil {
		t.Fatal(err)
	}
	id := result.Memory.ID
	snapshot := func(want uint64) CoreSnapshot {
		t.Helper()
		actual, err := service.CoreSnapshot(t.Context(), mc.PartnerID)
		if err != nil || actual.Revision != want {
			t.Fatalf("revision = %d, want %d, error = %v", actual.Revision, want, err)
		}
		return actual
	}
	if got := string(snapshot(0).JSON); got != `{"items":[]}` {
		t.Fatalf("initial snapshot = %s", got)
	}
	if err := service.SetCore(t.Context(), "another-partner", id, true); !errors.Is(err, status_error.MemoryNotFound) {
		t.Fatalf("cross-partner classification = %v", err)
	}
	if err := service.SetCore(t.Context(), mc.PartnerID, id, true); err != nil {
		t.Fatal(err)
	}
	snapshot(1)
	if err := service.SetCore(t.Context(), mc.PartnerID, id, true); err != nil {
		t.Fatal(err)
	}
	if err := service.Update(t.Context(), mc, id, "retained fact"); err != nil {
		t.Fatal(err)
	}
	snapshot(1)
	if err := service.Update(t.Context(), mc, id, "revised fact"); err != nil {
		t.Fatal(err)
	}
	snapshot(2)
	if err := service.Forget(t.Context(), mc, id); err != nil {
		t.Fatal(err)
	}
	if got := string(snapshot(3).JSON); got != `{"items":[]}` {
		t.Fatalf("clear snapshot = %s", got)
	}
}
