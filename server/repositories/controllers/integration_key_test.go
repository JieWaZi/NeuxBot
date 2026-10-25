package controllers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	keyrules "github.com/JieWaZi/NeuxBot/server/internal/integration"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestIntegrationKeyDigestStorageAndRevision(t *testing.T) {
	c := NewIntegrationKeyController(newTestDB(t))
	ctx := context.Background()
	m, fresh, err := c.ClaimMutation(ctx, models.IntegrationKeyMutation{RequestID: "create", Digest: "input", KeyID: "key-a", Action: "create"})
	if err != nil || !fresh {
		t.Fatal(m, fresh, err)
	}
	grants := []models.IntegrationGrant{{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceIDs: []string{"a"}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve}}}
	created, err := c.CompleteMutation(ctx, "create", "input", models.IntegrationKey{ID: "key-a", Name: "集成", KeyHash: keyrules.HashKey("fixture-only"), Grants: grants, Enabled: true}, 0)
	if err != nil || created.Revision != 1 || created.GrantVersion != 1 || created.CredentialGeneration != 1 {
		t.Fatal(created, err)
	}
	_, fresh, err = c.ClaimMutation(ctx, models.IntegrationKeyMutation{RequestID: "create", Digest: "input", KeyID: "ignored-new-id", Action: "create"})
	if err != nil || fresh {
		t.Fatal("issuance replay was newly claimed", fresh, err)
	}
	if _, _, err := c.ClaimMutation(ctx, models.IntegrationKeyMutation{RequestID: "create", Digest: "different", KeyID: "key-a", Action: "create"}); !errors.Is(err, status_error.IntegrationConflict) {
		t.Fatal("different input accepted", err)
	}
	found, err := c.FindByDigest(ctx, keyrules.HashKey("fixture-only"))
	if err != nil || found.ID != "key-a" {
		t.Fatal(found, err)
	}
	created.Name = "改名"
	renamed, err := c.UpdateAuthorization(ctx, created, 1)
	if err != nil || renamed.GrantVersion != 1 || renamed.Revision != 2 {
		t.Fatal(renamed, err)
	}
	if _, err := c.UpdateAuthorization(ctx, created, 1); !errors.Is(err, status_error.IntegrationConflict) {
		t.Fatal("stale revision accepted", err)
	}
	renamed.Grants[0].ResourceIDs = []string{"a", "b"}
	widened, err := c.UpdateAuthorization(ctx, renamed, 2)
	if err != nil || widened.GrantVersion != 2 {
		t.Fatal(widened, err)
	}
	if err := c.TouchLastUsed(ctx, widened.ID, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	current, err := c.Get(ctx, widened.ID)
	if err != nil || current.GrantVersion != 2 || current.Revision != 3 {
		t.Fatal("last used changed authorization", current, err)
	}
}

func TestDeletedResourcesRemoveOnlyMatchingAPIKeyGrants(t *testing.T) {
	for _, action := range []string{"knowledge", "task", "waker"} {
		t.Run(action, func(t *testing.T) {
			db := newTestDB(t)
			ctx := context.Background()
			keys := NewIntegrationKeyController(db)
			key := models.IntegrationKey{ID: "key", Name: "集成", KeyHash: keyrules.HashKey("secret"), Enabled: true, Revision: 1, GrantVersion: 1, CredentialGeneration: 1, Grants: []models.IntegrationGrant{
				{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceIDs: []string{"target", "keep"}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve}},
				{ResourceKind: constants.IntegrationResourceKindAutomaticTask, ResourceIDs: []string{"target"}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityTaskInvoke}},
			}}
			if err := db.Create(&key).Error; err != nil {
				t.Fatal(err)
			}
			var err error
			if action == "knowledge" {
				err = NewKnowledgeController(db).DeleteLibraryReferences(ctx, "local", "local", "target")
			} else {
				tasks := NewAutomaticTaskController(db)
				if err = tasks.Create(ctx, models.AutomaticTask{ID: "target", WakerID: "owner"}); err != nil {
					t.Fatal(err)
				}
				if action == "task" {
					err = tasks.Delete(ctx, "target")
				} else {
					err = tasks.DeleteByWaker(ctx, "owner")
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := keys.Get(ctx, key.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Revision != 2 || got.GrantVersion != 2 || got.CredentialGeneration != 1 || got.KeyHash != key.KeyHash {
				t.Fatalf("清理未更新授权版本或修改了凭据: revision=%d grantVersion=%d credentialGeneration=%d", got.Revision, got.GrantVersion, got.CredentialGeneration)
			}
			if action == "knowledge" {
				if len(got.Grants) != 2 || len(got.Grants[0].ResourceIDs) != 1 || got.Grants[0].ResourceIDs[0] != "keep" || got.Grants[1].ResourceIDs[0] != "target" {
					t.Fatalf("删除知识库影响了其他授权: %+v", got.Grants)
				}
			} else if len(got.Grants) != 1 || len(got.Grants[0].ResourceIDs) != 2 {
				t.Fatalf("任务权限组未清空或影响知识权限: %+v", got.Grants)
			}
			if _, err := keys.UpdateAuthorization(ctx, key, 1); !errors.Is(err, status_error.IntegrationConflict) {
				t.Fatal("旧表单没有发生版本冲突", err)
			}
		})
	}
}

func TestResourceGrantCleanupEmptyRetryAndRollback(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	keys := NewIntegrationKeyController(db)
	tasks := NewAutomaticTaskController(db)
	key := models.IntegrationKey{ID: "empty-key", Name: "集成", KeyHash: keyrules.HashKey("fixture"), Enabled: true, Revision: 1, GrantVersion: 1, CredentialGeneration: 1, Grants: []models.IntegrationGrant{{ResourceKind: constants.IntegrationResourceKindAutomaticTask, ResourceIDs: []string{"target", "second"}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityTaskInvoke}}}}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"target", "second"} {
		if err := tasks.Create(ctx, models.AutomaticTask{ID: id, WakerID: "owner"}); err != nil {
			t.Fatal(err)
		}
	}
	// 任务实际删除失败时，授权撤销必须一起回滚。
	if err := db.Exec("CREATE TRIGGER reject_task_delete BEFORE DELETE ON automatic_tasks BEGIN SELECT RAISE(ABORT, 'fixture'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := tasks.Delete(ctx, "target"); err == nil {
		t.Fatal("删除失败被忽略")
	}
	current, err := keys.Get(ctx, key.ID)
	if err != nil || current.Revision != 1 || len(current.Grants[0].ResourceIDs) != 2 {
		t.Fatal("失败删除改变了授权", err)
	}
	if err := db.Exec("DROP TRIGGER reject_task_delete").Error; err != nil {
		t.Fatal(err)
	}
	if err := tasks.DeleteByWaker(ctx, "owner"); err != nil {
		t.Fatal(err)
	}
	current, err = keys.Get(ctx, key.ID)
	if err != nil || current.Revision != 2 || current.GrantVersion != 2 || len(current.Grants) != 0 || current.KeyHash != key.KeyHash {
		t.Fatal("清空授权未保留Key", err)
	}
	if keyrules.Allows(current.Grants, constants.IntegrationResourceKindAutomaticTask, "other", constants.IntegrationCapabilityTaskInvoke) {
		t.Fatal("空授权意外放行")
	}
	if err := tasks.DeleteByWaker(ctx, "owner"); err != nil {
		t.Fatal(err)
	}
	retry, err := keys.Get(ctx, key.ID)
	if err != nil || retry.Revision != current.Revision {
		t.Fatal("删除重试重复修改授权", err)
	}
	current.Grants = key.Grants
	if _, err := keys.UpdateAuthorization(ctx, current, current.Revision); err != nil {
		t.Fatal("无授权Key不能重新授权", err)
	}
	if _, err := keys.RemoveResourceGrants(ctx, key.ID, 1, map[constants.IntegrationResourceKind][]string{constants.IntegrationResourceKindAutomaticTask: {"target"}}); !errors.Is(err, status_error.IntegrationConflict) {
		t.Fatal("旧版本撤权没有CAS冲突", err)
	}
}
