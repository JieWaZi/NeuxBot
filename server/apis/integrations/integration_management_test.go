package integrations

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestIntegrationIssuanceReplayAndGrantCAS(t *testing.T) {
	root := t.TempDir()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(root, "app.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB.DB()
	defer pool.Close()
	engine, err := core.Open(context.Background(), core.Options{DBPath: filepath.Join(root, "knowledge.db"), RawDir: filepath.Join(root, "raw")})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	receipt, err := engine.CreateLibrary(context.Background(), core.CreateLibraryInput{RequestID: "library", Name: "本机", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	oldKeys, oldService, oldEngine := global.IntegrationKeyControllerManager, global.IntegrationService, global.KnowledgeEngine
	defer func() {
		global.IntegrationKeyControllerManager, global.IntegrationService, global.KnowledgeEngine = oldKeys, oldService, oldEngine
	}()
	global.IntegrationKeyControllerManager = controllers.NewIntegrationKeyController(db.DB)
	global.IntegrationService = integrationservice.New(integrationservice.Config{Keys: global.IntegrationKeyControllerManager})
	global.KnowledgeEngine = engine
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/", nil)
	input := CreateAPIKeyInput{RequestID: "create-key", Name: "集成", Grants: []models.IntegrationGrant{{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceIDs: []string{receipt.ResourceID}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve}}}}
	result, err := (&CreateAPIKey{Body: input}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first := result.(APIKeyIssuance)
	if !strings.HasPrefix(first.Secret, "sk-") || first.Key == nil {
		t.Fatal("missing initial secret", first.IssuanceState)
	}
	result, err = (&CreateAPIKey{Body: input}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	replay := result.(APIKeyIssuance)
	if replay.KeyID != first.KeyID || replay.Secret != "" || replay.IssuanceState != "secret_unavailable" {
		t.Fatal("replay issued another credential", replay.IssuanceState)
	}
	updated := UpdateAPIKeyInput{Name: "改名", ExpectedRevision: first.Key.Revision, Grants: input.Grants}
	if _, err := (&UpdateAPIKey{KeyID: first.KeyID, Body: updated}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := (&UpdateAPIKey{KeyID: first.KeyID, Body: updated}).Output(ctx); !errors.Is(err, status_error.IntegrationConflict) {
		t.Fatal("stale authorization overwrote newer record", err)
	}
	input.RequestID = "remote-id"
	input.Grants[0].ResourceIDs = []string{"remote-only-id"}
	if _, err := (&CreateAPIKey{Body: input}).Output(ctx); err == nil {
		t.Fatal("published a nonlocal resource")
	}
	identity, err := global.IntegrationService.Authenticate(ctx, first.Secret)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := global.IntegrationService.Authorize(ctx, identity, constants.IntegrationResourceKindKnowledge, receipt.ResourceID, []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&DeleteAPIKey{KeyID: first.KeyID, Body: APIKeyRevisionInput{ExpectedRevision: 1}}).Output(ctx); !errors.Is(err, status_error.IntegrationConflict) {
		t.Fatal("stale delete removed a changed key", err)
	}
	deletion := &DeleteAPIKey{KeyID: first.KeyID, Body: APIKeyRevisionInput{ExpectedRevision: 2}}
	if _, err := deletion.Output(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := global.IntegrationService.Authenticate(ctx, first.Secret); !errors.Is(err, status_error.IntegrationUnauthorized) {
		t.Fatal("deleted key still authenticates", err)
	}
	if release, err := global.IntegrationService.AcquireGuard(ctx, auth); err == nil {
		release()
		t.Fatal("deleted key still permits delayed publication")
	}
	if _, err := deletion.Output(ctx); err != nil {
		t.Fatal("delete retry should be idempotent", err)
	}
	rows, total, err := global.IntegrationKeyControllerManager.List(ctx, 10, 0)
	if err != nil || total != 0 || len(rows) != 0 {
		t.Fatal("deleted key remains in management list", rows, total, err)
	}

}

func TestAPIKeyListReconcilesOnlyMissingResources(t *testing.T) {
	root := t.TempDir()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(root, "app.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB.DB()
	defer pool.Close()
	engine, err := core.Open(context.Background(), core.Options{DBPath: filepath.Join(root, "knowledge.db"), RawDir: filepath.Join(root, "raw")})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	receipt, err := engine.CreateLibrary(context.Background(), core.CreateLibraryInput{RequestID: "library", Name: "本机", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	oldKeys, oldService, oldEngine := global.IntegrationKeyControllerManager, global.IntegrationService, global.KnowledgeEngine
	defer func() {
		global.IntegrationKeyControllerManager, global.IntegrationService, global.KnowledgeEngine = oldKeys, oldService, oldEngine
	}()
	global.IntegrationKeyControllerManager = controllers.NewIntegrationKeyController(db.DB)
	global.IntegrationService = integrationservice.New(integrationservice.Config{Keys: global.IntegrationKeyControllerManager})
	global.KnowledgeEngine = engine
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/", nil)

	input := CreateAPIKeyInput{RequestID: "key", Name: "集成", Grants: []models.IntegrationGrant{{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceIDs: []string{receipt.ResourceID}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve}}}}
	result, err := (&CreateAPIKey{Body: input}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key := *result.(APIKeyIssuance).Key
	// 模拟旧版本留下的失效引用，不能在创建入口绕过资源验证。
	key.Grants[0].ResourceIDs = append(key.Grants[0].ResourceIDs, "deleted")
	key, err = global.IntegrationKeyControllerManager.UpdateAuthorization(ctx, key, key.Revision)
	if err != nil {
		t.Fatal(err)
	}
	result, err = (&ListAPIKeys{}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := result.(APIKeyList).Resources[0]
	if got.Revision != key.Revision+1 || got.GrantVersion != key.GrantVersion+1 || len(got.Grants[0].ResourceIDs) != 1 || got.Grants[0].ResourceIDs[0] != receipt.ResourceID {
		t.Fatal("历史失效引用未清理", got.Grants, got.Revision)
	}
	stale := UpdateAPIKeyInput{Name: key.Name, Grants: key.Grants, ExpectedRevision: key.Revision}
	if _, err := (&UpdateAPIKey{KeyID: key.ID, Body: stale}).Output(ctx); !errors.Is(err, status_error.IntegrationConflict) {
		t.Fatal("失效旧表单应优先返回版本冲突", err)
	}
	// 已撤销历史记录也应清理引用，但不能借清理恢复凭据或授权。
	revokedAt := int64(1)
	revoked := models.IntegrationKey{ID: "revoked", Name: "已撤销", KeyHash: strings.Repeat("a", 64), Revision: 1, GrantVersion: 1, CredentialGeneration: 1, RevokedAt: &revokedAt, Grants: []models.IntegrationGrant{{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceIDs: []string{"gone"}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve}}}}
	if err := db.DB.Create(&revoked).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := (&ListAPIKeys{}).Output(ctx); err != nil {
		t.Fatal("已撤销历史Key阻断管理页", err)
	}
	cleaned, err := global.IntegrationKeyControllerManager.Get(ctx, revoked.ID)
	if err != nil || cleaned.Revision != 2 || cleaned.GrantVersion != 2 || len(cleaned.Grants) != 0 || cleaned.RevokedAt == nil || cleaned.KeyHash != revoked.KeyHash {
		t.Fatal("已撤销Key清理改变了凭据或撤销状态", err)
	}
	if _, err := global.IntegrationKeyControllerManager.UpdateAuthorization(ctx, cleaned, cleaned.Revision); !errors.Is(err, status_error.IntegrationConflict) {
		t.Fatal("普通编辑放宽了撤销校验", err)
	}
	// 核心暂时不可用时不得把有效引用当成不存在删除。
	global.KnowledgeEngine = nil
	if _, err := (&ListAPIKeys{}).Output(ctx); !errors.Is(err, status_error.KnowledgeUnsupported) {
		t.Fatal("资源不可用被忽略", err)
	}
	current, err := global.IntegrationKeyControllerManager.Get(ctx, key.ID)
	if err != nil || current.Revision != got.Revision {
		t.Fatal("不可用时修改了授权", err)
	}
}
