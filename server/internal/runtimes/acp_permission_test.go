package runtimes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	acp "github.com/JieWaZi/acp-go/sdk"
)

func TestACPRequestPermissionCancelsWhenSessionIsMissing(t *testing.T) {
	t.Parallel()

	client := newACPClient()
	response, err := client.RequestPermission(
		context.Background(),
		acp.RequestPermissionRequest{
			SessionId: "missing-session",
			Options: []acp.PermissionOption{{
				OptionId: "reject-always",
				Name:     "Always reject",
				Kind:     acp.PermissionOptionKindRejectAlways,
			}},
		},
	)
	if err != nil {
		t.Fatalf("RequestPermission() error = %v", err)
	}
	if response.Outcome.Cancelled == nil || response.Outcome.Selected != nil {
		t.Fatalf("RequestPermission outcome = %#v, want cancelled", response.Outcome)
	}
}

func TestNormalizeACPApprovalRequestKeepsUnknownOptionKind(t *testing.T) {
	t.Parallel()

	title := "自定义审批"
	request, valid := normalizeACPApprovalRequest(acp.RequestPermissionRequest{
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: "tool-1",
			Title:      &title,
		},
		Options: []acp.PermissionOption{
			{
				OptionId: "allow-once",
				Name:     "Allow once",
				Kind:     acp.PermissionOptionKindAllowOnce,
			},
			{
				OptionId: "custom-scope",
				Name:     "Allow this repo clone",
				Kind:     acp.PermissionOptionKind("allow_repo"),
			},
		},
	})
	if !valid || len(request.Options) != 2 {
		t.Fatalf("normalizeACPApprovalRequest() = %#v, valid=%v", request, valid)
	}
	unknown := request.Options[1]
	if unknown.ID != "custom-scope" ||
		unknown.Name != "Allow this repo clone" ||
		unknown.Kind != "allow_repo" {
		t.Fatalf("unknown option = %#v", unknown)
	}
}

func TestACPRequestPermissionReturnsUnknownOptionID(t *testing.T) {
	t.Parallel()

	title := "自定义审批"
	bridge := newACPBridge(
		"session",
		"invocation",
		nil,
		Workspace{Path: "/workspace"},
		ExecutionPolicy{PermissionLevel: PermissionLevelAuto},
		codexACPRuntimeBinding(),
		0,
	)
	bridge.approvalHandler = approvalHandlerFunc(func(
		_ context.Context,
		request ApprovalRequest,
	) (ApprovalDecision, error) {
		if len(request.Options) != 1 || request.Options[0].ID != "custom-scope" {
			t.Fatalf("ApprovalRequest options = %#v", request.Options)
		}
		return ApprovalDecision{OptionID: request.Options[0].ID}, nil
	})
	response := bridge.requestPermission(context.Background(), acp.RequestPermissionRequest{
		SessionId: "session",
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: "tool-1",
			Title:      &title,
		},
		Options: []acp.PermissionOption{{
			OptionId: "custom-scope",
			Name:     "Allow this repo clone",
			Kind:     acp.PermissionOptionKind("allow_repo"),
		}},
	})
	if response.Outcome.Selected == nil ||
		response.Outcome.Selected.OptionId != "custom-scope" {
		t.Fatalf("RequestPermission outcome = %#v", response.Outcome)
	}
}

func TestACPRequestPermissionCancelsWorkspaceReadOnlyMutation(t *testing.T) {
	t.Parallel()
	bridge := newACPBridge(
		"session",
		"invocation",
		nil,
		Workspace{Path: "/workspace"},
		ExecutionPolicy{
			PermissionLevel:   PermissionLevelFullAccess,
			WorkspaceReadOnly: true,
		},
		claudeACPRuntimeBinding(),
		0,
	)
	bridge.approvalHandler = approvalHandlerFunc(func(
		context.Context,
		ApprovalRequest,
	) (ApprovalDecision, error) {
		t.Fatal("workspace read-only invocation delegated a mutation approval")
		return ApprovalDecision{}, nil
	})
	title := "修改文件"
	response := bridge.requestPermission(context.Background(), acp.RequestPermissionRequest{
		SessionId: "session",
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: "tool-1",
			Title:      &title,
		},
		Options: []acp.PermissionOption{{
			OptionId: "allow-once",
			Name:     "Allow once",
			Kind:     acp.PermissionOptionKindAllowOnce,
		}},
	})
	if response.Outcome.Cancelled == nil || response.Outcome.Selected != nil {
		t.Fatalf("read-only permission outcome = %#v, want cancelled", response.Outcome)
	}
}

func TestACPRequestPermissionApprovesOnlyExactTrustedControlAction(t *testing.T) {
	t.Parallel()
	for _, readOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("readOnly=%t", readOnly), func(t *testing.T) {
			bridge := newACPBridge(
				"session",
				"invocation",
				nil,
				Workspace{Path: "/workspace"},
				ExecutionPolicy{
					PermissionLevel:   PermissionLevelDefault,
					WorkspaceReadOnly: readOnly,
					TrustedControlTools: []TrustedControlTool{{
						Server:  "ally_channel_tasks",
						Tool:    "channel_tasks",
						Actions: []string{"list", "create"},
					}},
				},
				codexACPRuntimeBinding(),
				0,
			)
			response := bridge.requestPermission(context.Background(), trustedControlPermissionRequest(
				"ally_channel_tasks",
				"channel_tasks",
				json.RawMessage(`{"action":"create"}`),
			))
			if response.Outcome.Selected == nil || response.Outcome.Selected.OptionId != "allow-once" ||
				response.Outcome.Cancelled != nil {
				t.Fatalf("trusted control outcome = %#v, want allow-once", response.Outcome)
			}
		})
	}
}

func TestACPRequestPermissionRejectsUntrustedReadOnlyRequests(t *testing.T) {
	t.Parallel()
	bridge := newACPBridge(
		"session",
		"invocation",
		nil,
		Workspace{Path: "/workspace"},
		ExecutionPolicy{
			PermissionLevel:   PermissionLevelDefault,
			WorkspaceReadOnly: true,
			TrustedControlTools: []TrustedControlTool{{
				Server:  "ally_channel_tasks",
				Tool:    "channel_tasks",
				Actions: []string{"list"},
			}},
		},
		claudeACPRuntimeBinding(),
		0,
	)
	tests := []struct {
		// name 是失败场景名称。
		name string
		// request 是必须安全取消的权限请求。
		request acp.RequestPermissionRequest
	}{
		{name: "memory server", request: trustedControlPermissionRequest("memory", "channel_tasks", map[string]any{"action": "list"})},
		{name: "other tool", request: trustedControlPermissionRequest("ally_channel_tasks", "write_file", map[string]any{"action": "list"})},
		{name: "mutation not granted", request: trustedControlPermissionRequest("ally_channel_tasks", "channel_tasks", map[string]any{"action": "create"})},
		{name: "shell", request: acp.RequestPermissionRequest{
			ToolCall: acp.ToolCallUpdate{ToolCallId: "shell", RawInput: map[string]any{"command": "touch forbidden"}},
			Options:  []acp.PermissionOption{{OptionId: "allow-once", Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce}},
		}},
		{name: "ambiguous allow once", request: acp.RequestPermissionRequest{
			ToolCall: trustedControlPermissionRequest("ally_channel_tasks", "channel_tasks", map[string]any{"action": "list"}).ToolCall,
			Options: []acp.PermissionOption{
				{OptionId: "allow-once-1", Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce},
				{OptionId: "allow-once-2", Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce},
			},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := bridge.requestPermission(context.Background(), test.request)
			if response.Outcome.Cancelled == nil || response.Outcome.Selected != nil {
				t.Fatalf("untrusted read-only outcome = %#v, want cancelled", response.Outcome)
			}
		})
	}
}

func trustedControlPermissionRequest(
	server string,
	tool string,
	arguments any,
) acp.RequestPermissionRequest {
	return acp.RequestPermissionRequest{
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: "control-call",
			RawInput: map[string]any{
				"server":    server,
				"tool":      tool,
				"arguments": arguments,
			},
		},
		Options: []acp.PermissionOption{
			{OptionId: "allow-once", Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce},
			{OptionId: "reject-once", Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce},
		},
	}
}

func TestACPPlanApprovalKeepsLongStandardContent(t *testing.T) {
	title := "Review plan"
	body := strings.Repeat("A complete plan step.\n", 800)
	request := acp.RequestPermissionRequest{ToolCall: acp.ToolCallUpdate{ToolCallId: "plan", Title: &title, Content: []acp.ToolCallContent{acp.ToolContent(acp.TextBlock(body))}}, Options: []acp.PermissionOption{{OptionId: "accept", Name: "Accept", Kind: acp.PermissionOptionKindAllowOnce}}}
	normalized, valid := normalizeACPApprovalRequest(request)
	if !valid || normalized.Summary != body || normalized.Title != title {
		t.Fatal("long plan was rejected or hidden in its title")
	}
	request.ToolCall.Content = []acp.ToolCallContent{acp.ToolContent(acp.TextBlock(strings.Repeat("x", 129*1024)))}
	if _, valid := normalizeACPApprovalRequest(request); valid {
		t.Fatal("approval accepted an undisplayable plan")
	}
}

func TestWritableExecutionPolicyAcceptsSignedInternalToolRange(t *testing.T) {
	policy := ExecutionPolicy{PermissionLevel: PermissionLevelDefault, TrustedControlTools: []TrustedControlTool{{Server: "ally_workflow", Tool: "workflow", Actions: []string{"source.upsert"}}}}
	if err := validateExecutionPolicy(policy); err != nil {
		t.Fatalf("signed internal tool policy rejected: %v", err)
	}
}
