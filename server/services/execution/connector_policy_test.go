package execution

import (
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"

	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestSessionPlanExcludesConnectorSecretMaterial(t *testing.T) {
	connector := models.MCPConnector{ID: "connector", Transport: constants.MCPTransportStreamableHTTP, URL: "https://service.invalid/mcp", Headers: []models.MCPSecretDefinition{{Name: "Authorization", Value: "private-header", Configured: true}}, Environment: []models.MCPSecretDefinition{{Name: "TOKEN", Value: "private-environment", Configured: true}}}
	plan, _ := (&service{}).buildSessionPlan(messageChat{}, pendingMessage{}, &preparedMessageFiles{}, nil, []models.MCPConnector{connector}, models.RuntimeEnvironment{})
	if len(plan.Connectors) != 1 || plan.Connectors[0].URL != connector.URL {
		t.Fatalf("static session plan retained connector secrets: connectors=%d", len(plan.Connectors))
	}
}

func TestNativeScopedEntrypointRejectsBeforeOpeningCodex(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	empty := []string{}
	_, err := manager.execution.Start(t.Context(), WorkIntent{ClientRequestID: "scoped-native-input",
		Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}}, Input: WorkInput{Actor: "user", Content: "scoped task"},
		Options: WorkOptions{RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto, AllowedConnectorIDs: &empty}})
	if !errors.Is(err, status_error.RuntimePolicyUnsupported) || len(manager.nativeCalls(t, "initialize")) != 0 {
		t.Fatalf("entrypoint allowed inherited native MCP: %v", err)
	}
}

func TestNativeAgentDomainScopeReachesCodex(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	domains := []string{"memory", "interaction"}
	_, err := manager.execution.Start(t.Context(), WorkIntent{ClientRequestID: "scoped-agent-domain-input",
		Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}}, Input: WorkInput{Actor: "user", Content: "scoped task"},
		Options: WorkOptions{RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelFullAccess, AllowedAgentDomains: &domains}})
	if err != nil {
		t.Fatalf("host capability domain scope rejected: %v", err)
	}
	if len(manager.nativeCalls(t, "initialize")) != 1 {
		t.Fatal("host-scoped request did not reach native client")
	}
}
