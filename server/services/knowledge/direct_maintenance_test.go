package knowledge

import (
	"context"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestWikiMaintenanceRunsACPWithoutPartnerChatOrSources(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	called := make(chan MaintenanceInvocation, 1)
	s.config.RunMaintenance = func(_ context.Context, _ models.LibraryExecutionDefaults, input MaintenanceInvocation) (string, error) {
		called <- input
		return "已核查现有 Wiki；无须改动。", nil
	}
	r, _, err := s.Organize(ctx, OrganizeCommand{RequestID: "independent-wiki-maintenance", LibraryID: lib.ID, Maintenance: true, Configuration: models.KnowledgeTaskConfiguration{RuntimeID: "codex"}})
	if err != nil || r == nil || r.State != "accepted" {
		t.Fatalf("existing Wiki maintenance was not admitted independently: %+v %v", r, err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	select {
	case invocation := <-called:
		if invocation.JobID != r.JobID || invocation.Instructions == "" || len(invocation.Bindings) == 0 {
			t.Fatalf("missing frozen independent scope: %+v", invocation)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("independent ACP maintenance was not driven")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, e := s.config.Engine.GetJob(ctx, r.JobID)
		if e != nil {
			t.Fatal(e)
		}
		if job.State == "completed" {
			if len(job.SourceVersionIDs) != 0 {
				t.Fatal("maintenance acquired original sources as ingestion work")
			}
			stages, e := s.config.Engine.ListStages(ctx, job.ID)
			if e != nil || len(stages) != 1 || stages[0].ResultText != "已核查现有 Wiki；无须改动。" {
				t.Fatalf("missing actual ACP result: %+v %v", stages, e)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("ACP result did not complete the knowledge task")
}
