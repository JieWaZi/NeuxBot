package execution

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestConnectorAllowlistReachesRuntimeAndPreservesInternalCapability(t *testing.T) {
	for _, allowed := range [][]string{{}, {"selected"}} {
		runner := &fakeRuntime{}
		manager := newMessageTestService(t, t.TempDir(), runner)
		manager.execution.(*service).connectorController = &retryConnectorController{enabled: []models.MCPConnector{
			{ID: "selected", Name: "Selected", RuntimeKey: "ally_selected", Transport: constants.MCPTransportStreamableHTTP, URL: "http://localhost/selected"},
			{ID: "excluded", Name: "Excluded", RuntimeKey: "ally_excluded", Transport: constants.MCPTransportStreamableHTTP, URL: "http://localhost/excluded"},
		}}
		intent := WorkIntent{ClientRequestID: util.New(), Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}}, Input: WorkInput{Actor: "workflow", Content: "仅测试冻结工具目录"}, Options: WorkOptions{RuntimeID: runtimeconfig.RuntimeIDCodex, PermissionLevel: runtimeconfig.PermissionLevelDefault, SessionPolicy: constants.RuntimeSessionPolicyNew, AllowedConnectorIDs: &allowed}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch}
		accepted, err := manager.execution.Start(context.Background(), intent)
		if err != nil {
			t.Fatal(err)
		}
		waitForChatTerminal(t, manager, accepted.Chat.ID)
		if len(runner.requests) != 1 || len(runner.requests[0].Connectors) != len(allowed) {
			t.Fatalf("wrong runtime tool catalog: %+v", runner.requests)
		}
		for _, connector := range runner.requests[0].Connectors {
			if connector.ID == "excluded" {
				t.Fatal("excluded MCP reached runtime")
			}
		}
		changed := []string{"selected", "excluded"}
		intent.Options.AllowedConnectorIDs = &changed
		if _, err := manager.execution.Start(context.Background(), intent); !errors.Is(err, status_error.CreateRequestConflict) {
			t.Fatalf("idempotent admission silently expanded tool scope: %v", err)
		}
	}
}

func TestOrdinaryChatDoesNotMountContextualBuiltinPlaceholders(t *testing.T) {
	runner := &fakeRuntime{}
	manager := newMessageTestService(t, t.TempDir(), runner)
	manager.execution.(*service).connectorController = &retryConnectorController{enabled: []models.MCPConnector{
		{ID: "selected", Name: "Selected", RuntimeKey: "ally_selected", Transport: constants.MCPTransportStreamableHTTP, URL: "http://localhost/selected"},
	}}
	accepted, err := manager.execution.Start(context.Background(), WorkIntent{ClientRequestID: util.New(), Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}}, Input: WorkInput{Actor: "test-user", Content: "普通聊天"}, Options: WorkOptions{RuntimeID: runtimeconfig.RuntimeIDCodex, PermissionLevel: runtimeconfig.PermissionLevelDefault}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	if len(runner.requests) != 1 || len(runner.requests[0].Connectors) != 1 || runner.requests[0].Connectors[0].ID != "selected" {
		t.Fatalf("contextual connector reached ordinary chat: %+v", runner.requests)
	}
}

func TestSessionPlanExcludesConnectorSecretMaterial(t *testing.T) {
	connector := models.MCPConnector{ID: "connector", Transport: constants.MCPTransportStreamableHTTP, URL: "https://service.invalid/mcp", Headers: []models.MCPSecretDefinition{{Name: "Authorization", Value: "private-header", Configured: true}}, Environment: []models.MCPSecretDefinition{{Name: "TOKEN", Value: "private-environment", Configured: true}}}
	plan, _, _ := (&service{}).buildSessionPlan(messageChat{}, pendingMessage{}, &preparedMessageFiles{}, nil, []models.MCPConnector{connector}, models.RuntimeEnvironment{})
	if len(plan.Connectors) != 1 || plan.Connectors[0].URL != connector.URL {
		t.Fatalf("static session plan retained connector secrets: connectors=%d", len(plan.Connectors))
	}
}

func TestSavedConnectorConfigurationChangeReplacesHotResources(t *testing.T) {
	runner := &leasedChatRuntime{}
	runner.result.Session = runtimecontract.SessionResult{OutputID: "native-session", State: runtimecontract.SessionStateCreated}
	manager := newMessageTestService(t, t.TempDir(), &runner.accountRuntime)
	core := manager.execution.(*service)
	core.runtime = runner
	connectors := &retryConnectorController{enabled: []models.MCPConnector{{ID: "connector", Name: "Service", RuntimeKey: "ally_service", Transport: constants.MCPTransportStreamableHTTP, URL: "https://service.invalid/mcp", Headers: []models.MCPSecretDefinition{{Name: "Authorization", Ciphertext: "saved-first", Value: "first-private-value", Configured: true}}}}}
	core.connectorController = connectors
	t.Cleanup(func() { _ = runner.CloseWorkspace(context.Background(), "") })
	intent := WorkIntent{ClientRequestID: "connector-config", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}}, Input: WorkInput{Actor: "user", Content: "first"}, Options: WorkOptions{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}}
	first, err := core.Start(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	firstKey := runner.resourceKey
	connectors.enabled[0].Headers[0].Ciphertext = "saved-second"
	connectors.enabled[0].Headers[0].Value = "second-private-value"
	intent.ClientRequestID, intent.Chat, intent.Input.Content = "", ChatTarget{ChatID: first.Chat.ID}, "second"
	second, err := core.Start(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, second.Chat.ID)
	if len(runner.requests) != 2 || runner.requests[1].Resources.Connectors[0].Headers["Authorization"] != "second-private-value" || runner.resourceKey == firstKey {
		t.Fatal("saved connector configuration change reused the prior credential resource")
	}
	secondKey := runner.resourceKey
	connectors.enabled[0].TestedAt, connectors.enabled[0].TestDurationMS = 12345, 100
	intent.Input.Content = "third"
	third, err := core.Start(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, third.Chat.ID)
	if runner.resourceKey != secondKey {
		t.Fatal("connection test metadata invalidated the hot session")
	}
}
