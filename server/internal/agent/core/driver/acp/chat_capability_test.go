package acp_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	driver "github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// TestChatInputKeepsSnapshotsAndNegotiatesImages 验证图片能力协商与不可变代码引用实际进入公开 Prompt。
func TestChatInputKeepsSnapshotsAndNegotiatesImages(t *testing.T) {
	for _, images := range []bool{false, true} {
		t.Run(map[bool]string{false: "text_fallback", true: "native_image"}[images], func(t *testing.T) {
			agent := newFixtureAgent()
			agent.initialize = func(_ context.Context, request sdk.InitializeRequest) (sdk.InitializeResponse, error) {
				return sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber, AgentCapabilities: sdk.AgentCapabilities{PromptCapabilities: sdk.PromptCapabilities{Image: images}}}, nil
			}
			image := filepath.Join(t.TempDir(), "image.png")
			if err := os.WriteFile(image, []byte("image-bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			agent.prompt = func(_ context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
				want := 1
				if images {
					want = 2
				}
				if len(request.Prompt) != want {
					t.Fatalf("prompt blocks=%d want=%d", len(request.Prompt), want)
				}
				text := request.Prompt[0].Text.Text
				for _, part := range []string{"current task", "attachments/image.png", "sample.go:3-4", "fixed source snapshot", "selected skills: review", "selected MCP connections: docs"} {
					if !strings.Contains(text, part) {
						t.Errorf("missing %q in %q", part, text)
					}
				}
				if images && (request.Prompt[1].Image == nil || request.Prompt[1].Image.Data != base64.StdEncoding.EncodeToString([]byte("image-bytes"))) {
					t.Fatalf("image payload lost: %+v", request.Prompt[1])
				}
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
			}
			driver := newDriver(t, agent)
			session, err := driver.Open(context.Background(), openRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "current task", Attachments: []contract.Attachment{{Name: "image.png", Path: image, DisplayPath: "attachments/image.png", MIMEType: "image/png"}}, CodeReferences: []contract.CodeReference{{Path: "sample.go", StartLine: 3, EndLine: 4, Content: "fixed source snapshot"}}, SelectedSkills: []string{"review"}, SelectedMCP: []string{"docs"}}}, &recordingSink{})
			if result.Status != contract.StatusCompleted {
				t.Fatalf("input failed: %+v", result)
			}
		})
	}
}

// TestChatMCPUsesLeasedTypedConnections 验证三种传输、稳定秘密配置及原生能力拒绝。
func TestChatMCPUsesLeasedTypedConnections(t *testing.T) {
	agent := newFixtureAgent()
	agent.initialize = func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
		return sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber, AgentCapabilities: sdk.AgentCapabilities{McpCapabilities: sdk.McpCapabilities{Http: true, Sse: true}}}, nil
	}
	agent.newSession = func(_ context.Context, request sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
		if len(request.McpServers) != 3 || request.McpServers[0].Stdio == nil || request.McpServers[1].Http == nil || request.McpServers[2].Sse == nil {
			t.Fatalf("MCP transport lost: %+v", request.McpServers)
		}
		if request.McpServers[0].Stdio.Env[0].Name != "A" || request.McpServers[1].Http.Headers[0].Value != "secret" {
			t.Fatalf("connection config lost: %+v", request.McpServers)
		}
		return sdk.NewSessionResponse{SessionId: "native-session"}, nil
	}
	request := openRequest(t)
	request.Contributions.MCP = []contract.MCP{{Name: "local", ResourceID: "stdio"}, {Name: "remote", ResourceID: "http"}, {Name: "events", ResourceID: "sse"}}
	request.Resources = []contract.ResourceBinding{{ID: "stdio", MCP: &contract.MCPConnection{Transport: contract.MCPStdio, Command: "node", Arguments: []string{"server.js"}, Environment: map[string]string{"Z": "last", "A": "first"}}}, {ID: "http", MCP: &contract.MCPConnection{Transport: contract.MCPHTTP, URL: "https://example.test/mcp", Headers: map[string]string{"Authorization": "secret"}}}, {ID: "sse", MCP: &contract.MCPConnection{Transport: contract.MCPSSE, URL: "https://example.test/events"}}}
	driver := newDriver(t, agent)
	if _, err := driver.Open(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	unsupported := newDriver(t, newFixtureAgent())
	if _, err := unsupported.Open(context.Background(), request); err == nil {
		t.Fatal("unadvertised HTTP/SSE transport accepted")
	}
}

// TestChatApprovalKeepsExactOptions 验证未知审批类别仍通过原始身份回传，不压缩成布尔授权。
func TestChatApprovalKeepsExactOptions(t *testing.T) {
	agent := newFixtureAgent()
	sink := &recordingSink{interact: func(_ context.Context, request contract.InteractionRequest) (contract.InteractionResponse, error) {
		if request.Approval == nil || request.Approval.ToolCallID != "tool" || request.Approval.Command != "git status" || len(request.Approval.Options) != 2 || request.Approval.Options[1].Kind != "vendor_restricted" {
			t.Fatalf("approval facts lost: %+v", request)
		}
		return contract.InteractionResponse{Invocation: request.Invocation, ID: request.ID, Action: contract.InteractionAccept, OptionID: "restricted"}, nil
	}}
	agent.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
		response, err := agent.connection.Load().RequestPermission(ctx, sdk.RequestPermissionRequest{SessionId: request.SessionId, ToolCall: sdk.ToolCallUpdate{ToolCallId: "tool", Title: sdk.Ptr("Run status"), RawInput: map[string]any{"command": "git status"}}, Options: []sdk.PermissionOption{{OptionId: "always", Name: "Always", Kind: sdk.PermissionOptionKindAllowAlways}, {OptionId: "restricted", Name: "Restricted", Kind: "vendor_restricted"}}})
		if err != nil {
			return sdk.PromptResponse{}, err
		}
		if response.Outcome.Selected == nil || response.Outcome.Selected.OptionId != "restricted" {
			t.Fatalf("exact approval option lost: %+v", response)
		}
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
	}
	driver := newDriver(t, agent)
	session, err := driver.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "work"}}, sink)
	if result.Status != contract.StatusCompleted {
		t.Fatalf("approval failed: %+v", result)
	}
}

// TestChatFormAndURLElicitationRoundTrip 验证公开问题模式与提交、拒绝、取消三种决定。
func TestChatFormAndURLElicitationRoundTrip(t *testing.T) {
	for _, action := range []contract.InteractionAction{contract.InteractionAccept, contract.InteractionDecline, contract.InteractionCancel} {
		t.Run(string(action), func(t *testing.T) {
			agent := newFixtureAgent()
			var interactions int
			sink := &recordingSink{interact: func(_ context.Context, request contract.InteractionRequest) (contract.InteractionResponse, error) {
				interactions++
				if request.Input == nil || request.Prompt != "Choose" {
					t.Fatalf("input missing: %+v", request)
				}
				if interactions == 1 && request.Input.RequestedSchema["properties"] == nil || interactions == 2 && request.Input.URL != "https://example.test/authorize" {
					t.Fatalf("schema/URL lost: %+v", request.Input)
				}
				return contract.InteractionResponse{Invocation: request.Invocation, ID: request.ID, Action: action, Content: map[string]any{"choice": "yes"}}, nil
			}}
			agent.prompt = func(ctx context.Context, prompt sdk.PromptRequest) (sdk.PromptResponse, error) {
				form := sdk.NewUnstableCreateElicitationRequestForm(sdk.UnstableElicitationSchema{Properties: map[string]any{"choice": map[string]any{"type": "string"}}})
				form.Form.Message = "Choose"
				url := sdk.NewUnstableCreateElicitationRequestUrl("url-request", "https://example.test/authorize")
				url.Url.Message = "Choose"
				for _, request := range []sdk.UnstableCreateElicitationRequest{form, url} {
					response, err := agent.connection.Load().UnstableCreateElicitation(ctx, request)
					if err != nil {
						return sdk.PromptResponse{}, err
					}
					if action == contract.InteractionAccept && (response.Accept == nil || response.Accept.Content["choice"] != "yes") || action == contract.InteractionDecline && response.Decline == nil || action == contract.InteractionCancel && response.Cancel == nil {
						t.Fatalf("decision lost: %+v", response)
					}
				}
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
			}
			driver := newDriver(t, agent)
			session, err := driver.Open(context.Background(), openRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "work"}}, sink)
			if result.Status != contract.StatusCompleted || interactions != 2 {
				t.Fatalf("elicitation failed: %+v interactions=%d", result, interactions)
			}
		})
	}
}

// TestChatCanonicalToolOutputKeepsRichFacts 验证参数、输出、位置与差异通过同一规范事件到达宿主。
func TestChatCanonicalToolOutputKeepsRichFacts(t *testing.T) {
	agent := newFixtureAgent()
	sink := &recordingSink{}
	agent.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
		original := "before"
		call := sdk.SessionUpdateToolCall{ToolCallId: "edit", Title: "Edit file", Kind: sdk.ToolKindEdit, Status: sdk.ToolCallStatusCompleted, RawInput: map[string]any{"path": "main.go"}, RawOutput: map[string]any{"ok": true}, Locations: []sdk.ToolCallLocation{{Path: "main.go", Line: sdk.Ptr(4)}}, Content: []sdk.ToolCallContent{{Diff: &sdk.ToolCallContentDiff{Path: "main.go", OldText: &original, NewText: "after"}}, {Content: &sdk.ToolCallContentContent{Content: sdk.TextBlock("updated")}}}}
		err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: sdk.SessionUpdate{ToolCall: &call}})
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, err
	}
	driver := newDriver(t, agent)
	session, err := driver.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "edit"}}, sink)
	if result.Status != contract.StatusCompleted {
		t.Fatal(result)
	}
	var tool *contract.ToolEvent
	for _, event := range sink.snapshot() {
		if event.Tool != nil {
			tool = event.Tool
		}
	}
	if tool == nil || tool.Title != "Edit file" || tool.Kind != "edit" || tool.Text != "updated" || len(tool.Content) != 2 || tool.Content[0].Diff == nil || tool.Content[0].Diff.OldText != "before" || len(tool.Locations) != 1 || *tool.Locations[0].Line != 4 {
		t.Fatalf("rich tool facts lost: %+v", tool)
	}
	var input, output map[string]any
	if json.Unmarshal(tool.Input, &input) != nil || json.Unmarshal(tool.Output, &output) != nil || input["path"] != "main.go" || output["ok"] != true {
		t.Fatalf("tool JSON lost: %+v", tool)
	}
}

// TestChatRichAssistantContentPreservesPublicResources 捕获图片地址、内嵌资源与音频在规范转换时丢失的错误。
func TestChatRichAssistantContentPreservesPublicResources(t *testing.T) {
	agent := newFixtureAgent()
	image := sdk.ImageBlock("aW1hZ2U=", "image/png")
	image.Image.Uri = sdk.Ptr("https://example.test/image.png")
	blocks := []sdk.ContentBlock{
		image,
		{Audio: &sdk.ContentBlockAudio{Type: "audio", Data: "YXVkaW8=", MimeType: "audio/wav"}},
		{ResourceLink: &sdk.ContentBlockResourceLink{Type: "resource_link", Uri: "file:///workspace/report.txt", Name: "report", MimeType: sdk.Ptr("text/plain")}},
		{Resource: &sdk.ContentBlockResource{Type: "resource", Resource: sdk.EmbeddedResourceResource{TextResourceContents: &sdk.TextResourceContents{Uri: "file:///workspace/note.txt", Text: "saved snapshot", MimeType: sdk.Ptr("text/plain")}}}},
		{Resource: &sdk.ContentBlockResource{Type: "resource", Resource: sdk.EmbeddedResourceResource{BlobResourceContents: &sdk.BlobResourceContents{Uri: "file:///workspace/archive.bin", Blob: "Ynl0ZXM=", MimeType: sdk.Ptr("application/octet-stream")}}}},
	}
	agent.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
		for _, block := range blocks {
			if err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: sdk.Ptr("answer"), Content: block}}}); err != nil {
				return sdk.PromptResponse{}, err
			}
		}
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
	}
	driver := newDriver(t, agent)
	session, err := driver.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "show resources"}}, sink)
	if result.Status != contract.StatusCompleted {
		t.Fatal(result)
	}
	var content []contract.ContentBlock
	for _, event := range sink.snapshot() {
		if event.Message != nil {
			content = append(content, event.Message.Content...)
		}
	}
	if len(content) != 5 || content[0].Type != "image" || content[0].URI != "https://example.test/image.png" || content[0].Data != "aW1hZ2U=" || content[1].Type != "audio" || content[1].MIMEType != "audio/wav" || content[2].Name != "report" || content[3].Text != "saved snapshot" || content[4].Data != "Ynl0ZXM=" {
		t.Fatalf("rich public resources lost: %+v", content)
	}
}

// TestChatPlanKeepsProgressAndRemoval 捕获标准计划未投射或完成后重复终态的错误。
func TestChatPlanKeepsProgressAndRemoval(t *testing.T) {
	agent := newFixtureAgent()
	agent.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
		updates := []sdk.SessionUpdate{
			{PlanRemoved: &sdk.SessionUpdatePlanRemoved{Id: "unknown"}},
			{Plan: &sdk.SessionUpdatePlan{Entries: []sdk.PlanEntry{{Content: "inspect source", Priority: sdk.PlanEntryPriorityHigh, Status: sdk.PlanEntryStatusInProgress, Meta: map[string]any{"private": "must-not-cross"}}}}},
			{Plan: &sdk.SessionUpdatePlan{Entries: []sdk.PlanEntry{{Content: "inspect source", Priority: sdk.PlanEntryPriorityHigh, Status: sdk.PlanEntryStatusCompleted}}}},
			{PlanRemoved: &sdk.SessionUpdatePlanRemoved{Id: "done"}},
			{Plan: &sdk.SessionUpdatePlan{Entries: []sdk.PlanEntry{{Content: "verify", Priority: sdk.PlanEntryPriorityMedium, Status: sdk.PlanEntryStatusPending}}}},
			{PlanRemoved: &sdk.SessionUpdatePlanRemoved{Id: "active"}},
		}
		for _, update := range updates {
			if err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: update}); err != nil {
				return sdk.PromptResponse{}, err
			}
		}
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
	}
	driver := newDriver(t, agent)
	session, err := driver.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "plan"}}, sink)
	if result.Status != contract.StatusCompleted {
		t.Fatal(result)
	}
	var tools []contract.ToolEvent
	for _, event := range sink.snapshot() {
		if event.Tool != nil {
			tools = append(tools, *event.Tool)
		}
	}
	if len(tools) != 4 || tools[0].ID != "acp-plan" || tools[0].Name != "update_plan" || tools[0].Update || tools[0].Status != "in_progress" || !tools[1].Update || tools[1].Status != "completed" || tools[2].Status != "in_progress" || tools[3].Status != "completed" {
		t.Fatalf("plan lifecycle lost: %+v", tools)
	}
	var input struct {
		Plan []struct {
			Content  string // Content 是计划条目正文。
			Priority string // Priority 是公开优先级。
			Status   string // Status 是公开执行状态。
		} // Plan 是公开计划条目的完整快照。
	}
	if err := json.Unmarshal(tools[0].Input, &input); err != nil {
		t.Fatal(err)
	}
	if len(input.Plan) != 1 || input.Plan[0].Content != "inspect source" || input.Plan[0].Priority != "high" || input.Plan[0].Status != "in_progress" || strings.Contains(string(tools[0].Input), "private") {
		t.Fatalf("plan content lost or metadata copied: %s", tools[0].Input)
	}
}

// TestChatContextUsagePreservesWindow 捕获上下文已用量到达宿主时丢失总窗口的错误。
func TestChatContextUsagePreservesWindow(t *testing.T) {
	agent := newFixtureAgent()
	agent.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
		err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: sdk.SessionUpdate{UsageUpdate: &sdk.SessionUsageUpdate{Used: 900, Size: 64000}}})
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, err
	}
	driver := newDriver(t, agent)
	session, err := driver.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "work"}}, sink)
	if result.Status != contract.StatusCompleted || result.Usage.ContextWindowTokens != 64000 || result.Usage.ContextTokens != 900 {
		t.Fatalf("result context window lost: %+v", result)
	}
	var eventUsage *contract.Usage
	for _, event := range sink.snapshot() {
		if event.Usage != nil {
			eventUsage = event.Usage
		}
	}
	if eventUsage == nil || !eventUsage.ContextKnown || eventUsage.ContextWindowTokens != 64000 || eventUsage.ContextTokens != 900 {
		t.Fatalf("live context window lost: %+v", eventUsage)
	}
}

// TestChatApprovalRoundTripsEveryPublicChoice 捕获始终允许、始终拒绝及未知选项被压缩为布尔决定的错误。
func TestChatApprovalRoundTripsEveryPublicChoice(t *testing.T) {
	for _, kind := range []sdk.PermissionOptionKind{sdk.PermissionOptionKindAllowOnce, sdk.PermissionOptionKindAllowAlways, sdk.PermissionOptionKindRejectOnce, sdk.PermissionOptionKindRejectAlways, "vendor_restricted"} {
		t.Run(string(kind), func(t *testing.T) {
			agent := newFixtureAgent()
			sink := &recordingSink{interact: func(_ context.Context, request contract.InteractionRequest) (contract.InteractionResponse, error) {
				if request.Approval == nil || len(request.Approval.Options) != 1 || request.Approval.Options[0].Kind != string(kind) {
					t.Error("public choice lost")
				}
				return contract.InteractionResponse{Invocation: request.Invocation, ID: request.ID, Action: contract.InteractionAccept, OptionID: "native-choice"}, nil
			}}
			agent.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
				response, err := agent.connection.Load().RequestPermission(ctx, sdk.RequestPermissionRequest{SessionId: request.SessionId, ToolCall: sdk.ToolCallUpdate{ToolCallId: "tool"}, Options: []sdk.PermissionOption{{OptionId: "native-choice", Name: "Choice", Kind: kind}}})
				if err != nil {
					return sdk.PromptResponse{}, err
				}
				if response.Outcome.Selected == nil || response.Outcome.Selected.OptionId != "native-choice" {
					t.Errorf("public choice not returned: %+v", response)
				}
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
			}
			driver := newDriver(t, agent)
			session, err := driver.Open(context.Background(), openRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "work"}}, sink)
			if result.Status != contract.StatusCompleted {
				t.Fatal(result)
			}
		})
	}
}

// TestChatApprovalRejectsUnofferedChoice 捕获宿主返回不存在选项后被当作原生授权的错误。
func TestChatApprovalRejectsUnofferedChoice(t *testing.T) {
	agent := newFixtureAgent()
	agent.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
		response, err := agent.connection.Load().RequestPermission(ctx, sdk.RequestPermissionRequest{SessionId: request.SessionId, ToolCall: sdk.ToolCallUpdate{ToolCallId: "tool"}, Options: []sdk.PermissionOption{{OptionId: "offered", Name: "Once", Kind: sdk.PermissionOptionKindAllowOnce}}})
		if err != nil {
			return sdk.PromptResponse{}, err
		}
		if response.Outcome.Cancelled == nil || response.Outcome.Selected != nil {
			t.Errorf("unoffered choice accepted: %+v", response)
		}
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
	}
	sink := &recordingSink{interact: func(_ context.Context, request contract.InteractionRequest) (contract.InteractionResponse, error) {
		return contract.InteractionResponse{Invocation: request.Invocation, ID: request.ID, Action: contract.InteractionAccept, OptionID: "not-offered"}, nil
	}}
	driver := newDriver(t, agent)
	session, err := driver.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "work"}}, sink); result.Status != contract.StatusCompleted {
		t.Fatal(result)
	}
}

// TestChatMCPRequiresMaterializedConnection 捕获必需MCP丢失及可选MCP缺失阻断准备的错误。
func TestChatMCPRequiresMaterializedConnection(t *testing.T) {
	for _, optional := range []bool{false, true} {
		t.Run(map[bool]string{false: "required", true: "optional"}[optional], func(t *testing.T) {
			agent := newFixtureAgent()
			var created bool
			agent.newSession = func(_ context.Context, request sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
				created = true
				if len(request.McpServers) != 0 {
					t.Errorf("missing resource materialized: %+v", request.McpServers)
				}
				return sdk.NewSessionResponse{SessionId: "native-session"}, nil
			}
			driver := newDriver(t, agent)
			request := openRequest(t)
			request.Contributions.MCP = []contract.MCP{{Name: "docs", ResourceID: "docs", Optional: optional}}
			_, err := driver.Open(context.Background(), request)
			if optional {
				if err != nil || !created {
					t.Fatalf("optional resource blocked native session: created=%v err=%v", created, err)
				}
			} else {
				var failure *contract.Fault
				if !errors.As(err, &failure) || failure.Code != contract.FaultPreparation || created {
					t.Fatalf("required resource ignored: created=%v err=%v", created, err)
				}
			}
		})
	}
}

// TestChatImageInputBudgetAndFallback 捕获原生图片读取不受限或文本回退仍读取附件的错误。
func TestChatImageInputBudgetAndFallback(t *testing.T) {
	for _, images := range []bool{false, true} {
		t.Run(map[bool]string{false: "fallback", true: "native image budget"}[images], func(t *testing.T) {
			agent := newFixtureAgent()
			agent.initialize = func(context.Context, sdk.InitializeRequest) (sdk.InitializeResponse, error) {
				return sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber, AgentCapabilities: sdk.AgentCapabilities{PromptCapabilities: sdk.PromptCapabilities{Image: images}}}, nil
			}
			image := filepath.Join(t.TempDir(), "large.png")
			if images {
				file, err := os.Create(image)
				if err != nil {
					t.Fatal(err)
				}
				if err := file.Truncate((20 << 20) + 1); err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			}
			var prompted bool
			agent.prompt = func(_ context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
				prompted = true
				if len(request.Prompt) != 1 || !strings.Contains(request.Prompt[0].Text.Text, "large.png") {
					t.Errorf("fallback file reference lost: %+v", request.Prompt)
				}
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
			}
			driver := newDriver(t, agent)
			session, err := driver.Open(context.Background(), openRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "inspect", Attachments: []contract.Attachment{{Name: "large.png", Path: image, MIMEType: "image/png"}}}}, &recordingSink{})
			if images {
				if result.Fault == nil || result.Fault.Code != contract.FaultLimit || prompted {
					t.Fatalf("oversized image reached native prompt: %+v prompted=%v", result, prompted)
				}
			} else if result.Status != contract.StatusCompleted || !prompted {
				t.Fatalf("text fallback read missing native file: %+v", result)
			}
		})
	}
}

// TestChatPromptUsageKeepsThoughtAndCache 捕获公开推理与缓存用量在Chat边界被丢失的错误。
func TestChatPromptUsageKeepsThoughtAndCache(t *testing.T) {
	agent := newFixtureAgent()
	agent.prompt = func(context.Context, sdk.PromptRequest) (sdk.PromptResponse, error) {
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn, Usage: &sdk.Usage{InputTokens: 13, OutputTokens: 7, CachedReadTokens: sdk.Ptr(5), CachedWriteTokens: sdk.Ptr(3), ThoughtTokens: sdk.Ptr(2)}}, nil
	}
	driver := newDriver(t, agent)
	session, err := driver.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "work"}}, &recordingSink{})
	if result.Status != contract.StatusCompleted || !result.Usage.Known || result.Usage.InputTokens != 13 || result.Usage.OutputTokens != 7 || result.Usage.CachedReadTokens != 5 || result.Usage.CachedWriteTokens != 3 || result.Usage.ThoughtTokens != 2 {
		t.Fatalf("public usage lost: %+v", result)
	}
}

// TestChatRejectsNegativeThoughtUsage 捕获非法推理token作为成功用量交付的错误。
func TestChatRejectsNegativeThoughtUsage(t *testing.T) {
	agent := newFixtureAgent()
	agent.prompt = func(context.Context, sdk.PromptRequest) (sdk.PromptResponse, error) {
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn, Usage: &sdk.Usage{ThoughtTokens: sdk.Ptr(-1)}}, nil
	}
	driver := newDriver(t, agent)
	session, err := driver.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "work"}}, &recordingSink{})
	if result.Fault == nil || result.Fault.Code != contract.FaultProtocol || result.Status != contract.StatusFailed {
		t.Fatalf("negative thought usage accepted: %+v", result)
	}
}

// TestChatToolUpdatesKeepEmptyReplacements 捕获显式清空与省略字段被合并、旧工具内容无法清除的错误。
func TestChatToolUpdatesKeepEmptyReplacements(t *testing.T) {
	agent := newFixtureAgent()
	agent.prompt = func(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
		updates := []sdk.SessionUpdate{
			sdk.StartToolCall("tool", "Read file", sdk.WithStartContent([]sdk.ToolCallContent{sdk.ToolContent(sdk.TextBlock("previous"))}), sdk.WithStartLocations([]sdk.ToolCallLocation{{Path: "previous.go"}})),
			sdk.UpdateToolCall("tool", sdk.WithUpdateContent([]sdk.ToolCallContent{}), sdk.WithUpdateLocations([]sdk.ToolCallLocation{})),
			sdk.UpdateToolCall("tool", sdk.WithUpdateStatus(sdk.ToolCallStatusCompleted)),
		}
		for _, update := range updates {
			if err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: update}); err != nil {
				return sdk.PromptResponse{}, err
			}
		}
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
	}
	driver := newDriver(t, agent)
	session, err := driver.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "work"}}, sink)
	if result.Status != contract.StatusCompleted {
		t.Fatal(result)
	}
	var tools []contract.ToolEvent
	for _, event := range sink.snapshot() {
		if event.Tool != nil {
			tools = append(tools, *event.Tool)
		}
	}
	if len(tools) != 3 {
		t.Fatalf("tool lifecycle lost: %+v", tools)
	}
	if tools[0].Update || len(tools[0].Content) != 1 || len(tools[0].Locations) != 1 {
		t.Fatalf("initial contents lost: %+v", tools[0])
	}
	if !tools[1].Update || tools[1].Content == nil || len(tools[1].Content) != 0 || tools[1].Locations == nil || len(tools[1].Locations) != 0 {
		t.Fatalf("explicit replacements lost: %+v", tools[1])
	}
	if !tools[2].Update || tools[2].Content != nil || tools[2].Locations != nil || tools[2].Status != "completed" {
		t.Fatalf("omitted replacements changed: %+v", tools[2])
	}
}

// TestNativeIdentityKeepsSkillSyntaxAndMaterializedInstructions 验证用户Profile仍使用基础CLI语法，原生系统指令不重复注入正文。
func TestNativeIdentityKeepsSkillSyntaxAndMaterializedInstructions(t *testing.T) {
	for base, prefix := range map[string]string{"codex": "$", "kimi": "/skill:", "pi": "/skill:", "claude": ""} {
		t.Run(base, func(t *testing.T) {
			agent := newFixtureAgent()
			prompts := 0
			agent.prompt = func(_ context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
				text := request.Prompt[0].Text.Text
				if !strings.Contains(text, "selected skills: "+prefix+"review") || strings.Contains(text, "MATERIALIZED_SYSTEM") {
					t.Errorf("native syntax or duplicate system instructions: %q", text)
				}
				if (prompts == 0) != strings.Contains(text, "MAINTENANCE_INSTRUCTION") {
					t.Errorf("static prefix replay: prompt=%d text=%q", prompts, text)
				}
				prompts++
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
			}
			nativeDriver, err := driver.New(driver.Config{ID: "custom-profile", NativeID: base, MaterializedInstructions: []string{"system-0"}, Factory: func(context.Context, contract.OpenRequest) (sdk.Agent, error) { return agent, nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer nativeDriver.Close(context.Background())
			request := openRequest(t)
			request.Contributions.Instructions = []contract.Instruction{{Name: "system-0", Text: "MATERIALIZED_SYSTEM"}, {Name: "maintenance", Text: "MAINTENANCE_INSTRUCTION"}}
			session, err := nativeDriver.Open(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				result := session.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "current task", SelectedSkills: []string{"review"}}}, &recordingSink{})
				if result.Status != contract.StatusCompleted {
					t.Fatalf("prompt=%+v", result)
				}
			}
		})
	}
}
