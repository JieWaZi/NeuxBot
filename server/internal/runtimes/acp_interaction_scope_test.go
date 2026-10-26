package runtimes

import (
	"context"
	"testing"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// scopedElicitationHandler 观察无需启动屏障即可到达的真实 Bridge 调用身份。
type scopedElicitationHandler struct {
	// invocation 保存 Host 收到的调用身份。
	invocation string
}

func (h *scopedElicitationHandler) Request(_ context.Context, request ElicitationRequest) (ElicitationDecision, error) {
	h.invocation = request.InvocationID
	return ElicitationDecision{Declined: true}, nil
}

func TestACPInteractionScopesComeFromBridgeBeforeActivation(t *testing.T) {
	bridge := newACPBridge("session", "originating-invocation", nil, Workspace{Path: "/workspace"}, ExecutionPolicy{PermissionLevel: PermissionLevelDefault}, standardACPRuntimeBinding(), 0)
	var approvalInvocation string
	bridge.approvalHandler = approvalHandlerFunc(func(_ context.Context, request ApprovalRequest) (ApprovalDecision, error) {
		approvalInvocation = request.InvocationID
		return ApprovalDecision{OptionID: "allow"}, nil
	})
	title := "Read file"
	bridge.requestPermission(context.Background(), acp.RequestPermissionRequest{SessionId: "session", ToolCall: acp.ToolCallUpdate{ToolCallId: "tool", Title: &title}, Options: []acp.PermissionOption{{OptionId: "allow", Name: "Allow", Kind: acp.PermissionOptionKindAllowOnce}}})
	if approvalInvocation != "originating-invocation" {
		t.Errorf("pre-activation approval scope=%q", approvalInvocation)
	}
	handler := &scopedElicitationHandler{}
	bridge.elicitationHandler = handler
	request := acp.NewUnstableCreateElicitationRequestForm(acp.UnstableElicitationSchema{Type: acp.UnstableElicitationSchemaTypeObject, Properties: map[string]any{}})
	request.Form.Message = "Choose"
	bridge.createElicitation(context.Background(), request)
	if handler.invocation != "originating-invocation" {
		t.Fatalf("pre-activation elicitation scope=%q", handler.invocation)
	}
}
