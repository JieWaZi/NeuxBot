package controllers

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/remote"
	"testing"
)

func TestKnowledgeOperationLinkIsIdempotentAndScopeBound(t *testing.T) {
	c := NewKnowledgeController(newTestDB(t))
	ctx := context.Background()
	in := models.KnowledgeOperationLink{RequestID: "request", Operation: "library.create", Digest: "same", Backend: "local", ConnectionID: "local"}
	if err := c.BeginOperation(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := c.BeginOperation(ctx, in); err != nil {
		t.Fatal(err)
	}
	other := in
	other.Digest = "changed"
	if err := c.BeginOperation(ctx, other); !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatalf("changed request: %v", err)
	}
	if err := c.CompleteOperation(ctx, "request", "library", "resource", "job"); err != nil {
		t.Fatal(err)
	}
	got, err := c.GetOperation(ctx, "request")
	if err != nil || got.KnowledgeBaseID != "library" || got.ResourceID != "resource" {
		t.Fatalf("link=%+v err=%v", got, err)
	}
}

func TestKnowledgeHostDiagnosticDoesNotReplaceCoreReceipt(t *testing.T) {
	c := NewKnowledgeController(newTestDB(t))
	ctx := context.Background()
	if err := c.BeginOperation(ctx, models.KnowledgeOperationLink{RequestID: "job", Operation: "job.accept", Digest: "same", Backend: "local", ConnectionID: "local", ResourceID: "core-job"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetHostDiagnostic(ctx, "job", "blocked", "40400000005", "模型配置不可用"); err != nil {
		t.Fatal(err)
	}
	got, err := c.GetOperation(ctx, "job")
	if err != nil || got.HostState != "blocked" || got.ResourceID != "core-job" || !got.ReceiptConfirmed {
		t.Fatalf("diagnostic=%+v err=%v", got, err)
	}
	if err := c.SetHostDiagnostic(ctx, "job", "active", "", ""); err != nil {
		t.Fatal(err)
	}
	got, _ = c.GetOperation(ctx, "job")
	if got.HostDiagnostic != "" || got.HostErrorCode != "" {
		t.Fatalf("stale diagnostic=%+v", got)
	}
}

func TestRemoteOperationClaimPersistsAndNeverReclaimsUnknownWrite(t *testing.T) {
	db := newTestDB(t)
	c := NewKnowledgeController(db)
	ctx := context.Background()
	in := models.KnowledgeOperationLink{RequestID: "remote-request", Operation: "source.import", Digest: "content-hash-only", Backend: "custom", ConnectionID: "connection", KnowledgeBaseID: "kb", ConnectionRevision: 3}
	fresh, err := c.ClaimRemoteOperation(ctx, in)
	if err != nil || !fresh {
		t.Fatal(fresh, err)
	}
	restarted := NewKnowledgeController(db)
	fresh, err = restarted.ClaimRemoteOperation(ctx, in)
	if err != nil || fresh {
		t.Fatal("unknown write reclaimed after restart", fresh, err)
	}
	rows, total, err := restarted.ListRemoteOperations(ctx, "custom", "connection", "kb", 0, 50)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].Digest != "content-hash-only" {
		t.Fatal(rows, total, err)
	}
	other := in
	other.KnowledgeBaseID = "another-library"
	if _, err = restarted.ClaimRemoteOperation(ctx, other); !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatal("resource changed", err)
	}
	if err = restarted.CompleteOperation(ctx, in.RequestID, "kb", "immutable-source", ""); err != nil {
		t.Fatal(err)
	}
	_, total, err = restarted.ListRemoteOperations(ctx, "custom", "connection", "kb", 0, 50)
	if err != nil || total != 0 {
		t.Fatal("confirmed receipt still unresolved", total, err)
	}
}
func TestLocalRecoveryExcludesRemoteOperationAnchors(t *testing.T) {
	c := NewKnowledgeController(newTestDB(t))
	ctx := context.Background()
	for _, in := range []models.KnowledgeOperationLink{{RequestID: "local", Operation: "source.import", Digest: "a", Backend: "local", ConnectionID: "local", KnowledgeBaseID: "kb"}, {RequestID: "remote", Operation: "source.import", Digest: "b", Backend: "custom", ConnectionID: "connection", KnowledgeBaseID: "kb"}} {
		if err := c.BeginOperation(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := c.ListPendingOperations(ctx)
	if err != nil || len(rows) != 1 || rows[0].Backend != "local" {
		t.Fatal("local engine would consume remote request", rows, err)
	}
}

// TestKnowledgeJobLinkPersistsIndependentConfiguration 验证任务引用持久保存独立模型配置。
func TestKnowledgeJobLinkPersistsIndependentConfiguration(t *testing.T) {
	db := newTestDB(t)
	c := NewKnowledgeController(db)
	ctx := context.Background()
	in := models.KnowledgeOperationLink{RequestID: "organize", Operation: "job.accept", Digest: "fixed", Backend: "local", ConnectionID: "local", KnowledgeBaseID: "library", JobID: "job", ConfigurationID: "model-config"}
	if err := c.BeginOperation(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := NewKnowledgeController(db).GetOperation(ctx, in.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ConfigurationID != "model-config" || got.JobID != "job" || got.ChatID != "" || got.ExecutionID != "" {
		t.Fatalf("独立任务引用丢失: %+v", got)
	}
}

func TestIntegrationClaimRequiresCurrentConnectionAndExactMount(t *testing.T) {
	for _, change := range []string{"revised", "disabled", "unmounted", "wrong-library"} {
		t.Run(change, func(t *testing.T) {
			db := newTestDB(t)
			ctx := context.Background()
			mounts := NewKnowledgeRemoteMountController(db)
			_, err := mounts.Add(ctx, AddKnowledgeRemoteMountInput{RequestID: "mount", ConnectionID: "connection", MountID: "mount", Credential: "secret", Detection: remote.Detection{BaseURL: "http://fixture.test/api/v1", ServiceKind: "neuxally", UploadMode: "separate", Library: remote.Library{ID: "kb", Name: "知识"}}})
			if err != nil {
				t.Fatal(err)
			}
			c := NewKnowledgeController(db)
			link := models.KnowledgeOperationLink{RequestID: "original", Operation: "source.import", Digest: "content", Backend: "integration", ConnectionID: "connection", ConnectionRevision: 1, KnowledgeBaseID: "kb"}
			if fresh, e := c.ClaimRemoteOperation(ctx, link); e != nil || !fresh {
				t.Fatal(fresh, e)
			}
			if change == "unmounted" {
				err = mounts.Remove(ctx, "mount", 1)
			} else if change != "wrong-library" {
				connections := NewKnowledgeConnectionController(db)
				connection, e := connections.Get(ctx, "connection")
				if e != nil {
					t.Fatal(e)
				}
				connection.Enabled = change != "disabled"
				_, err = connections.Save(ctx, SaveKnowledgeConnectionInput{RequestID: "change", ExpectedRevision: 1, Connection: connection})
			}
			if err != nil {
				t.Fatal(err)
			}
			if fresh, e := c.ClaimRemoteOperation(ctx, link); e != nil || fresh {
				t.Fatalf("original send fact changed: fresh=%v err=%v", fresh, e)
			}
			link.RequestID = "fresh"
			if change == "wrong-library" {
				link.KnowledgeBaseID = "other"
			}
			if fresh, e := c.ClaimRemoteOperation(ctx, link); e == nil || fresh {
				t.Fatalf("stale or unmounted write accepted: fresh=%v err=%v", fresh, e)
			}
			if _, e := c.GetOperation(ctx, "fresh"); !errors.Is(e, status_error.KnowledgeNotFound) {
				t.Fatalf("failed claim persisted: %v", e)
			}
		})
	}
}
