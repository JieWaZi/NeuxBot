package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestMCPConnectorAPIMaintainsSharedConfigurationAndEncryptedCredentials(
	t *testing.T,
) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "mcp-connector-api.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get SQL database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	previousWakers := global.WakerControllerManager
	previousConnectors := global.MCPConnectorControllerManager
	previousTransactions := global.TransactionControllerManager
	previousTest := runMCPConnectionTest
	t.Cleanup(func() {
		global.WakerControllerManager = previousWakers
		global.MCPConnectorControllerManager = previousConnectors
		global.TransactionControllerManager = previousTransactions
		runMCPConnectionTest = previousTest
	})
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.MCPConnectorControllerManager = controllers.NewMCPConnectorController(
		database.DB,
	)
	global.TransactionControllerManager = controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)

	ctx, _ := gin.CreateTestContext(nil)
	for _, waker := range []models.Waker{
		{ID: "waker-1", Name: "伙伴一"},
		{ID: "waker-2", Name: "伙伴二"},
	} {
		if err := global.WakerControllerManager.Create(ctx, waker); err != nil {
			t.Fatalf("create waker: %v", err)
		}
	}

	createdValue, err := (&CreateWakerConnector{
		WakerID: "waker-1",
		Body: ConnectorInput{
			Name:           "代码搜索",
			Description:    "连接代码服务",
			Transport:      constants.MCPTransportStdio,
			Command:        "search-server",
			Arguments:      []string{"--stdio"},
			Environment:    []SecretInput{{Name: "TOKEN", Value: "secret-value"}},
			Headers:        []SecretInput{},
			TimeoutMS:      5_000,
			TrustConfirmed: true,
		},
	}).Output(ctx)
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}
	created := createdValue.(ConnectorResponse)
	if !created.Enabled || !created.Connector.Environment[0].Configured {
		t.Fatalf("created connector = %+v", created)
	}
	if created.Connector.ActivationPolicy != constants.MCPActivationPolicyPerExecution {
		t.Fatalf("created connector activation policy = %q", created.Connector.ActivationPolicy)
	}
	encodedResponse, err := json.Marshal(created)
	if err != nil {
		t.Fatalf("encode created response: %v", err)
	}
	if strings.Contains(string(encodedResponse), "secret-value") ||
		strings.Contains(string(encodedResponse), "ciphertext") {
		t.Fatalf("connector response leaked credential data: %s", encodedResponse)
	}
	createdStored, err := global.MCPConnectorControllerManager.Get(
		ctx,
		created.Connector.ID,
	)
	if err != nil {
		t.Fatalf("read created connector: %v", err)
	}
	if !strings.HasPrefix(createdStored.RuntimeKey, "ally_dai_ma_sou_suo_") {
		t.Fatalf("runtime key = %q, want readable pinyin key", createdStored.RuntimeKey)
	}

	listedValue, err := (&ListWakerConnectors{WakerID: "waker-2"}).Output(ctx)
	if err != nil {
		t.Fatalf("list connector for other waker: %v", err)
	}
	listed := listedValue.([]ConnectorResponse)
	if len(listed) != 0 {
		t.Fatalf("other waker connectors = %+v", listed)
	}

	environmentID := created.Connector.Environment[0].ID
	updatedValue, err := (&UpdateWakerConnector{
		WakerID:     "waker-1",
		ConnectorID: created.Connector.ID,
		Body: ConnectorInput{
			Name:        "代码检索",
			Description: "更新说明但保留凭据",
			Transport:   constants.MCPTransportStdio,
			Command:     "search-server",
			Arguments:   []string{"--stdio"},
			Environment: []SecretInput{{ID: environmentID, Name: "TOKEN"}},
			Headers:     []SecretInput{},
			TimeoutMS:   5_000,
		},
	}).Output(ctx)
	if err != nil {
		t.Fatalf("update connector: %v", err)
	}
	updated := updatedValue.(ConnectorResponse)
	if !updated.Connector.Environment[0].Configured {
		t.Fatal("blank credential input did not retain encrypted value")
	}
	decrypted, err := global.MCPConnectorControllerManager.Get(
		ctx,
		created.Connector.ID,
	)
	if err != nil || decrypted.Environment[0].Value != "secret-value" {
		t.Fatalf("decrypted credential = %+v, error = %v", decrypted.Environment, err)
	}
	if decrypted.RuntimeKey != createdStored.RuntimeKey {
		t.Fatalf(
			"runtime key changed from %q to %q after rename",
			createdStored.RuntimeKey,
			decrypted.RuntimeKey,
		)
	}

	_, err = (&UpdateWakerConnector{
		WakerID:     "waker-1",
		ConnectorID: created.Connector.ID,
		Body: ConnectorInput{
			Name:        updated.Connector.Name,
			Description: created.Connector.Description,
			Transport:   constants.MCPTransportStdio,
			Command:     "changed-server",
			Arguments:   []string{"--stdio"},
			Environment: []SecretInput{{ID: environmentID, Name: "TOKEN"}},
			Headers:     []SecretInput{},
			TimeoutMS:   5_000,
		},
	}).Output(ctx)
	if !errors.Is(err, status_error.MCPConnectorTrustRequired) {
		t.Fatalf("changed command error = %v, want trust required", err)
	}

	runMCPConnectionTest = func(
		_ context.Context,
		connector models.MCPConnector,
	) connectionTestResult {
		if connector.Environment[0].Value != "secret-value" {
			t.Fatalf("test received credential = %+v", connector.Environment)
		}
		return connectionTestResult{
			Status:     constants.MCPTestStatusSucceeded,
			DurationMS: 12,
			Tools: []models.MCPToolSummary{{
				Name:        "search",
				Description: "搜索代码",
			}},
		}
	}
	testedValue, err := (&TestWakerConnector{
		WakerID:     "waker-1",
		ConnectorID: created.Connector.ID,
	}).Output(ctx)
	if err != nil {
		t.Fatalf("test connector: %v", err)
	}
	tested := testedValue.(ConnectorResponse)
	if tested.Connector.TestStatus != constants.MCPTestStatusSucceeded ||
		len(tested.Connector.TestTools) != 1 {
		t.Fatalf("tested connector = %+v", tested)
	}

	builtin := models.MCPConnector{ID: "builtin-protected", Name: "protected", RuntimeKey: "builtin_protected", Scope: constants.MCPConnectorScopeBuiltIn}
	if err := global.MCPConnectorControllerManager.Create(ctx, builtin); err != nil {
		t.Fatal(err)
	}
	if _, err := (&DeleteWakerConnector{WakerID: "waker-1", ConnectorID: builtin.ID}).Output(ctx); !errors.Is(err, status_error.BuiltInMCPConnectorCannotDelete) {
		t.Fatalf("built-in deletion: %v", err)
	}
	second := models.MCPConnector{ID: "public-second", Name: "second", RuntimeKey: "public_second", Scope: constants.MCPConnectorScopePublic}
	if err := global.MCPConnectorControllerManager.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{second.ID} {
		if err := global.MCPConnectorControllerManager.UpsertWakerConnector(ctx, models.WakerMCPConnector{ID: "relation-" + id, WakerID: "waker-2", MCPConnectorID: id, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err := global.MCPConnectorControllerManager.UpsertWakerConnector(ctx, models.WakerMCPConnector{ID: "second-w1", WakerID: "waker-1", MCPConnectorID: second.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := (&DeleteWakerConnector{WakerID: "waker-1", ConnectorID: second.ID}).Output(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := (&DeleteWakerConnector{
		WakerID:     "waker-1",
		ConnectorID: created.Connector.ID,
	}).Output(ctx); err != nil {
		t.Fatalf("delete connector: %v", err)
	}
	if _, err := global.MCPConnectorControllerManager.Get(
		ctx,
		created.Connector.ID,
	); err == nil {
		t.Fatal("deleted connector lookup succeeded")
	}
	for _, id := range []string{created.Connector.ID, second.ID} {
		var count int64
		want := int64(0)
		if id == second.ID {
			want = 1
		}
		if err := database.DB.Model(&models.WakerMCPConnector{}).Where("mcp_connector_id = ?", id).Count(&count).Error; err != nil || count != want {
			t.Fatalf("deleted connector relations = %d, %v", count, err)
		}
	}
	if _, err := global.MCPConnectorControllerManager.Get(ctx, builtin.ID); err != nil {
		t.Fatal("batch deletion changed built-in connector", err)
	}
}
