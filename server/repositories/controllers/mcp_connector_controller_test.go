package controllers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

func TestMCPExecutionDeclarationsExcludeDisabledSecretsAndDeferDecryption(t *testing.T) {
	db := newTestDB(t)
	controller := NewMCPConnectorController(db)
	ctx := context.Background()
	for _, id := range []string{"enabled1", "disabled1"} {
		connector := connectorFixture(id, id, constants.MCPConnectorScopeBuiltIn, 1)
		connector.Environment = []models.MCPSecretDefinition{{ID: "token", Name: "TOKEN", Value: id + "-secret", Configured: true}}
		if err := controller.Create(ctx, connector); err != nil {
			t.Fatal(err)
		}
	}
	if err := controller.UpsertWakerConnector(ctx, models.WakerMCPConnector{ID: "disabled-relation", WakerID: "w1", MCPConnectorID: "disabled1", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.MCPConnector{}).Where("id = ?", "disabled1").Update("environment", `[{"id":"token","name":"TOKEN","configured":true,"ciphertext":"damaged"}]`).Error; err != nil {
		t.Fatal(err)
	}
	keyReads := 0
	if err := db.Callback().Query().After("gorm:query").Register("mcp-execution-key-reads", func(query *gorm.DB) {
		if query.Statement.Table == (models.MCPStorageKey{}).TableName() {
			keyReads++
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove("mcp-execution-key-reads")
	declarations, err := controller.ExecutionDeclarations(ctx, "w1")
	if err != nil || len(declarations) != 1 || declarations[0].ID != "enabled1" {
		t.Fatalf("enabled declaration: %+v %v", declarations, err)
	}
	if declarations[0].Environment[0].Value != "" || declarations[0].Environment[0].Ciphertext == "" {
		t.Fatalf("declaration decrypted secret: %+v", declarations[0].Environment)
	}
	if keyReads != 0 {
		t.Fatalf("hot declaration read secret key %d times", keyReads)
	}
	if err := db.Model(&models.MCPConnector{}).Where("id = ?", "enabled1").Update("name", "New display label").Error; err != nil {
		t.Fatal(err)
	}
	actual, err := controller.AcquireEnabled(ctx, "w1", declarations)
	if err != nil || len(actual) != 1 || actual[0].Name != "New display label" || actual[0].Environment[0].Value != "enabled1-secret" {
		t.Fatalf("cold resources: %+v %v", actual, err)
	}
	if keyReads != 1 {
		t.Fatalf("cold acquisition key reads = %d, want one actual connector", keyReads)
	}
	if _, err := controller.ExecutionDeclarations(ctx, "w1"); err != nil {
		t.Fatal(err)
	}
	if keyReads != 1 {
		t.Fatalf("second hot declaration read secret key: %d", keyReads)
	}
	if err := db.Model(&models.MCPConnector{}).Where("id = ?", "enabled1").Update("command", "changed").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AcquireEnabled(ctx, "w1", declarations); err != status_error.Conflict {
		t.Fatalf("changed declaration acquired: %v", err)
	}
}

func TestMCPConnectorLibraryFindsDisabledPartnerOwnedResource(t *testing.T) {
	ctx := t.Context()
	store := NewMCPConnectorController(newTestDB(t))
	owned := connectorFixture("owned-mcp", "Postman", constants.MCPConnectorScopePrivate, 2)
	owned.OwnerWakerID = "owner"
	owned.Environment = []models.MCPSecretDefinition{{ID: "token", Name: "TOKEN", Value: "test-value", Configured: true}}
	if err := store.Create(ctx, owned); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertWakerConnector(ctx, models.WakerMCPConnector{ID: "relation", WakerID: "owner", MCPConnectorID: owned.ID}); err != nil {
		t.Fatal(err)
	}
	items, total, err := store.ListLibrary(ctx, "postman", 0, 1)
	if err != nil || total != 1 || len(items) != 1 || items[0].Connector.OwnerWakerID != "owner" {
		t.Fatalf("owned connector missing from library: total=%d items=%d error=%v", total, len(items), err)
	}
	if secret := items[0].Connector.Environment[0]; !secret.Configured || secret.Value != "" {
		t.Fatal("library listing should report configured state without decrypting credentials")
	}
	if item, err := store.GetForWaker(ctx, "owner", owned.ID); err != nil || item.Enabled {
		t.Fatalf("owner cannot read the disabled connector: enabled=%t error=%v", item.Enabled, err)
	}
	if _, err := store.GetForWaker(ctx, "other", owned.ID); err == nil {
		t.Fatal("library listing expanded another partner's connector access")
	}
}

func TestMCPConnectorControllerKeepsExplicitConfigurationRevision(t *testing.T) {
	const createdAt, updatedAt = int64(1000), int64(2000)
	db := newTestDB(t).Session(&gorm.Session{NowFunc: func() time.Time { return time.UnixMilli(3000) }})
	store := NewMCPConnectorController(db)
	ctx := t.Context()
	connector := connectorFixture("revision-test", "Original", constants.MCPConnectorScopePrivate, createdAt)
	if err := store.Create(ctx, connector); err != nil {
		t.Fatal(err)
	}
	current, err := store.Get(ctx, connector.ID)
	if err != nil || current.UpdatedAt != createdAt {
		t.Fatalf("created revision: %d %v", current.UpdatedAt, err)
	}
	current.Name, current.UpdatedAt = "Edited", updatedAt
	if err := store.Update(ctx, current); err != nil {
		t.Fatal(err)
	}
	current, err = store.Get(ctx, connector.ID)
	if err != nil || current.UpdatedAt != updatedAt {
		t.Fatalf("updated revision: %d %v", current.UpdatedAt, err)
	}
	current.TestStatus = constants.MCPTestStatusSucceeded
	if err := store.UpdateTestSnapshot(ctx, current, updatedAt); err != nil {
		t.Fatal(err)
	}
	current, err = store.Get(ctx, connector.ID)
	if err != nil || current.UpdatedAt != updatedAt || current.TestStatus != constants.MCPTestStatusSucceeded {
		t.Fatalf("test snapshot altered configuration revision: %+v %v", current, err)
	}
}

func TestMCPConnectorControllerUsesScopeDefaultsAndExplicitState(t *testing.T) {
	controller := NewMCPConnectorController(newTestDB(t))
	ctx := context.Background()
	now := time.Now().UnixMilli()
	builtIn := connectorFixture(
		"11111111111111111111111111111111",
		"内置连接器",
		constants.MCPConnectorScopeBuiltIn,
		now,
	)
	public := connectorFixture(
		"22222222222222222222222222222222",
		"公开连接器",
		constants.MCPConnectorScopePublic,
		now,
	)
	if err := controller.Create(ctx, builtIn); err != nil {
		t.Fatalf("create built-in connector: %v", err)
	}
	if err := controller.Create(ctx, public); err != nil {
		t.Fatalf("create public connector: %v", err)
	}

	items, err := controller.ListForWaker(ctx, "waker-1", "")
	if err != nil {
		t.Fatalf("list connectors: %v", err)
	}
	if len(items) != 1 || !items[0].Enabled {
		t.Fatalf("unexpected default states: %+v", items)
	}
	if err := controller.UpsertWakerConnector(ctx, models.WakerMCPConnector{
		ID:             "33333333333333333333333333333333",
		WakerID:        "waker-1",
		MCPConnectorID: builtIn.ID,
		Enabled:        false,
		CreatedAt:      now,
		UpdatedAt:      now,
	}); err != nil {
		t.Fatalf("disable built-in connector: %v", err)
	}
	if err := controller.UpsertWakerConnector(ctx, models.WakerMCPConnector{
		ID:             "44444444444444444444444444444444",
		WakerID:        "waker-1",
		MCPConnectorID: public.ID,
		Enabled:        true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}); err != nil {
		t.Fatalf("enable public connector: %v", err)
	}

	enabled, err := controller.ListEnabled(ctx, "waker-1")
	if err != nil {
		t.Fatalf("list enabled connectors: %v", err)
	}
	if len(enabled) != 1 || enabled[0].ID != public.ID {
		t.Fatalf("enabled connectors = %+v, want public connector", enabled)
	}
}

func TestMCPConnectorControllerAllowsDuplicateDisplayNames(t *testing.T) {
	controller := NewMCPConnectorController(newTestDB(t))
	ctx := context.Background()
	now := time.Now().UnixMilli()
	first := connectorFixture(
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"重复名称",
		constants.MCPConnectorScopePublic,
		now,
	)
	second := connectorFixture(
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		first.Name,
		constants.MCPConnectorScopePublic,
		now,
	)
	if err := controller.Create(ctx, first); err != nil {
		t.Fatalf("create first connector: %v", err)
	}
	if err := controller.Create(ctx, second); err != nil {
		t.Fatalf("duplicate display name rejected: %v", err)
	}
	if got, err := controller.Get(ctx, second.ID); err != nil || got.Name != first.Name {
		t.Fatalf("distinct resource not preserved: %+v %v", got, err)
	}
}

func TestMCPConnectorControllerEncryptsCredentialsInsideDatabase(t *testing.T) {
	db := newTestDB(t)
	controller := NewMCPConnectorController(db)
	ctx := context.Background()
	connector := connectorFixture(
		"cccccccccccccccccccccccccccccccc",
		"加密连接器",
		constants.MCPConnectorScopePublic,
		time.Now().UnixMilli(),
	)
	connector.Environment = []models.MCPSecretDefinition{{
		ID:         "token-entry",
		Name:       "API_TOKEN",
		Configured: true,
		Value:      "secret-value",
	}}
	connector.Headers = []models.MCPSecretDefinition{{
		ID:         "authorization-entry",
		Name:       "Authorization",
		Configured: true,
		Value:      "Bearer header-secret",
	}}
	if err := controller.Create(ctx, connector); err != nil {
		t.Fatalf("create encrypted connector: %v", err)
	}

	var stored models.MCPConnector
	if err := db.Where("id = ?", connector.ID).First(&stored).Error; err != nil {
		t.Fatalf("read stored connector: %v", err)
	}
	encoded, err := json.Marshal(stored.Environment)
	if err != nil {
		t.Fatalf("encode stored environment: %v", err)
	}
	if strings.Contains(string(encoded), "secret-value") ||
		stored.Environment[0].Ciphertext == "" {
		t.Fatalf("stored environment contains plaintext or no ciphertext: %s", encoded)
	}
	encodedHeaders, err := json.Marshal(stored.Headers)
	if err != nil {
		t.Fatalf("encode stored headers: %v", err)
	}
	if strings.Contains(string(encodedHeaders), "header-secret") || stored.Headers[0].Ciphertext == "" {
		t.Fatalf("stored headers contain plaintext or no ciphertext: %s", encodedHeaders)
	}
	var keyCount int64
	if err := db.Model(&models.MCPStorageKey{}).Count(&keyCount).Error; err != nil {
		t.Fatalf("count MCP storage keys: %v", err)
	}
	if keyCount != 1 {
		t.Fatalf("MCP storage key count = %d, want 1", keyCount)
	}

	decrypted, err := controller.Get(ctx, connector.ID)
	if err != nil {
		t.Fatalf("get decrypted connector: %v", err)
	}
	if decrypted.Environment[0].Value != "secret-value" ||
		decrypted.Environment[0].Ciphertext != "" {
		t.Fatalf("decrypted environment = %+v", decrypted.Environment)
	}
	if decrypted.Headers[0].Value != "Bearer header-secret" || decrypted.Headers[0].Ciphertext != "" {
		t.Fatalf("decrypted headers = %+v", decrypted.Headers)
	}
}

func connectorFixture(
	id string,
	name string,
	scope constants.MCPConnectorScope,
	now int64,
) models.MCPConnector {
	return models.MCPConnector{
		ID:          id,
		Name:        name,
		RuntimeKey:  "bot_fixture_" + id[:8],
		Scope:       scope,
		Transport:   constants.MCPTransportStdio,
		Command:     "connector",
		Arguments:   []string{},
		Environment: []models.MCPSecretDefinition{},
		Headers:     []models.MCPSecretDefinition{},
		TimeoutMS:   10_000,
		TestStatus:  constants.MCPTestStatusUntested,
		TestTools:   []models.MCPToolSummary{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}
