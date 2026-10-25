package execution

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
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
		intent := WorkIntent{ClientRequestID: util.New(), Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}}, Input: WorkInput{Actor: "workflow", Content: "仅测试冻结工具目录"}, Options: WorkOptions{RuntimeID: runtimes.RuntimeIDCodex, PermissionLevel: runtimes.PermissionLevelDefault, SessionPolicy: constants.RuntimeSessionPolicyNew, AllowedConnectorIDs: &allowed}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch}
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
	accepted, err := manager.execution.Start(context.Background(), WorkIntent{ClientRequestID: util.New(), Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}}, Input: WorkInput{Actor: "test-user", Content: "普通聊天"}, Options: WorkOptions{RuntimeID: runtimes.RuntimeIDCodex, PermissionLevel: runtimes.PermissionLevelDefault}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	if len(runner.requests) != 1 || len(runner.requests[0].Connectors) != 1 || runner.requests[0].Connectors[0].ID != "selected" {
		t.Fatalf("contextual connector reached ordinary chat: %+v", runner.requests)
	}
}
