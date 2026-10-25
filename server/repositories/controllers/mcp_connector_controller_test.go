package controllers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

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
		RuntimeKey:  "ally_fixture_" + id[:8],
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
