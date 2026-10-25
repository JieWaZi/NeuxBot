package runtimes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// cursorAcceptanceAnswers 把真实 Cursor 问题交给明确的测试用户答案，不由模型替答。
type cursorAcceptanceAnswers func(context.Context, ElicitationRequest) (ElicitationDecision, error)

// Request 使用同一 NeuxBot 宿主问答端口回填测试答案。
func (f cursorAcceptanceAnswers) Request(ctx context.Context, request ElicitationRequest) (ElicitationDecision, error) {
	return f(ctx, request)
}

// cursorAcceptanceEcho 保存真实 MCP 服务收到的输入。
type cursorAcceptanceEcho struct {
	// Text 是本次受控工具调用需要原样返回的标记。
	Text string `json:"text"`
}

// TestCursorRealAcceptance 通过 NeuxBot SDK 验证真实文件门禁、恢复、问答、MCP、技能和三档权限。
func TestCursorRealAcceptance(t *testing.T) {
	if os.Getenv("ALLY_CURSOR_REAL_PROBE") != "1" {
		t.Skip("requires explicit real Cursor probe opt-in")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".cursor", "cli-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	config := t.TempDir()
	if err = os.WriteFile(filepath.Join(config, "cli-config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	binary := os.Getenv("ALLY_CURSOR_REAL_BINARY")
	if binary == "" {
		binary = "cursor-agent"
	}
	profile := ProfileSpec{ID: "cursor-acceptance", BaseRuntimeID: RuntimeIDCursor, BinaryCandidates: []string{binary}, Environment: map[string]string{"CURSOR_CONFIG_DIR": config, "TMPDIR": t.TempDir(), "NO_OPEN_BROWSER": "1"}}
	sdk, err := New(Config{Profiles: []ProfileSpec{profile}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sdk.Close(context.Background()) }()
	discovery, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	detection := sdk.Detect(discovery, DetectRequest{RuntimeID: profile.ID, BaseEnvironment: os.Environ()})
	cancel()
	if detection.State != DetectionStateReady || detection.Version == "" {
		t.Fatalf("discovery: %+v", detection)
	}
	t.Logf("Cursor version=%s binary=%s", detection.Version, detection.ResolvedBinary)
	workspace := t.TempDir()
	skill := t.TempDir()
	if err = os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: acceptance-marker\ndescription: Report the acceptance verification marker.\n---\nThe acceptance secret marker is SKILL_READ_7319. When requested, report it.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := mcp.NewServer(&mcp.Implementation{Name: "ally_acceptance_echo", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "echo", Description: "Return the provided acceptance marker exactly."}, func(_ context.Context, _ *mcp.CallToolRequest, input cursorAcceptanceEcho) (*mcp.CallToolResult, any, error) {
		calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: input.Text}}}, nil, nil
	})
	endpoint := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true}))
	defer endpoint.Close()
	var approvals atomic.Int32
	var rejectedWrite atomic.Bool
	var allow atomic.Bool
	request := RunRequest{Target: RuntimeTarget{RuntimeID: profile.ID}, Detection: detection, Workspace: Workspace{Path: workspace, Managed: true}, Session: SessionDirective{Mode: SessionModeNew}, Policy: RunPolicy{Model: "composer-2.5", Reasoning: "default", Execution: ExecutionPolicy{PermissionLevel: PermissionLevelDefault}}, Skills: []Skill{{Name: "acceptance-marker", Path: skill, Selected: true}}, Connectors: []Connector{{ID: "acceptance", Name: "Acceptance echo", RuntimeKey: "ally_acceptance_echo", Transport: ConnectorTransportStreamableHTTP, URL: endpoint.URL}}}
	request.ApprovalHandler = approvalHandlerFunc(func(_ context.Context, r ApprovalRequest) (ApprovalDecision, error) {
		approvals.Add(1)
		if !allow.Load() && strings.Contains(r.Command, "denied.txt") {
			rejectedWrite.Store(true)
		}
		kind := "reject_once"
		if allow.Load() {
			kind = "allow_once"
		}
		for _, o := range r.Options {
			if o.Kind == kind {
				return ApprovalDecision{OptionID: o.ID}, nil
			}
		}
		return ApprovalDecision{Cancelled: true}, nil
	})
	run := func(label, prompt string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
		defer cancel()
		request.Message = Message{Text: prompt}
		sink := &recordingSink{}
		started := time.Now()
		result := sdk.Run(ctx, request, sink)
		if result.Failure != nil {
			t.Fatalf("%s failed: %+v output=%s", label, result.Failure, sink.textOutput())
		}
		if result.Session.OutputID == "" {
			t.Fatalf("%s lost session", label)
		}
		request.Session = SessionDirective{Mode: SessionModeResume, ID: result.Session.OutputID}
		var usage *UsageData
		for _, e := range sink.events {
			if e.Type == EventTypeUsage {
				var v UsageData
				if json.Unmarshal(e.Data, &v) == nil && v.Turn != nil {
					usage = &v
				}
			}
		}
		if usage == nil || usage.Turn.OutputTokens <= 0 || usage.Context == nil ||
			usage.Context.UsedTokens <= 0 || usage.Context.WindowTokens <= 0 {
			t.Fatalf("%s missing real usage: %+v", label, usage)
		}
		t.Logf("%s elapsed=%s session=%s approvals=%d usage=%+v", label, time.Since(started), result.Session.OutputID, approvals.Load(), usage.Turn)
		return sink.textOutput()
	}
	run("reject", "Use Shell to run exactly: printf rejected > denied.txt . If permission is rejected, do not retry or use another tool. Report rejection.")
	if approvals.Load() == 0 || !rejectedWrite.Load() {
		t.Fatal("no real approval requested")
	}
	if _, err = os.Stat(filepath.Join(workspace, "denied.txt")); !os.IsNotExist(err) {
		t.Fatalf("rejected operation executed: %v", err)
	}
	allow.Store(true)
	run("allow", "Use Shell to run exactly: printf approved > approved.txt . Then report success.")
	if data, err = os.ReadFile(filepath.Join(workspace, "approved.txt")); err != nil || string(data) != "approved" {
		t.Fatalf("approved effect missing: %q %v", data, err)
	}
	if err = sdk.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	sdk, err = New(Config{Profiles: []ProfileSpec{profile}})
	if err != nil {
		t.Fatal(err)
	}
	answer := run("cold-resume", "Without tools or reading files, name the file whose write was rejected and the file whose write was approved in this chat.")
	if !strings.Contains(answer, "denied.txt") || !strings.Contains(answer, "approved.txt") {
		t.Fatalf("history lost: %s", answer)
	}
	var questions atomic.Int32
	request.ElicitationHandler = cursorAcceptanceAnswers(func(_ context.Context, r ElicitationRequest) (ElicitationDecision, error) {
		questions.Add(1)
		return ElicitationDecision{Accepted: true, Content: map[string]any{"question_0": []string{"Red", "Blue"}}}, nil
	})
	answer = run("multi-question", "Use native AskQuestion to ask ONE multiple-selection question allowing more than one answer. Question: Choose colors. Options exactly Red and Blue. Wait for my response, then repeat both selections.")
	if questions.Load() != 1 || !strings.Contains(answer, "Red") || !strings.Contains(answer, "Blue") {
		t.Fatalf("multi answer not delivered: questions=%d output=%s", questions.Load(), answer)
	}
	request.ElicitationHandler = cursorAcceptanceAnswers(func(_ context.Context, r ElicitationRequest) (ElicitationDecision, error) {
		questions.Add(1)
		return ElicitationDecision{Accepted: true, Content: map[string]any{"question_0_custom": "橙色"}}, nil
	})
	answer = run("free-question", "Use native AskQuestion to ask ONE single-choice question. Question: Pick a color. Options Red and Blue. Allow Other. Wait for my answer, then repeat it exactly.")
	if questions.Load() != 2 || !strings.Contains(answer, "橙色") {
		t.Fatalf("free answer not delivered: %s", answer)
	}
	before := calls.Load()
	answer = run("mcp-and-skill", "Call the ally_acceptance_echo MCP echo tool exactly once with text MCP_REAL_7319; do not simulate it. Also read the selected acceptance-marker skill and report its secret marker. Reply with both markers.")
	if calls.Load() != before+1 || !strings.Contains(answer, "MCP_REAL_7319") || !strings.Contains(answer, "SKILL_READ_7319") {
		t.Fatalf("MCP/skill evidence missing: calls=%d output=%s", calls.Load()-before, answer)
	}
	for _, level := range []PermissionLevel{PermissionLevelAuto, PermissionLevelFullAccess} {
		request.Policy.Execution.PermissionLevel = level
		before := approvals.Load()
		name := "permission-" + string(level) + ".txt"
		run(string(level), "Use Shell to run exactly: printf verified > "+name+" . Then reply done.")
		if _, err = os.Stat(filepath.Join(workspace, name)); err != nil {
			t.Fatalf("%s side effect missing: %v", level, err)
		}
		if level == PermissionLevelFullAccess && approvals.Load() != before {
			t.Fatal("full access unexpectedly asked host approval")
		}
	}
	// 在真实审批等待处取消，确认取消后不会执行旧命令，并可以继续同一会话。
	request.Policy.Execution.PermissionLevel = PermissionLevelDefault
	originalHandler := request.ApprovalHandler
	cancelContext, stopTurn := context.WithTimeout(context.Background(), 100*time.Second)
	defer stopTurn()
	var cancelledAtApproval atomic.Bool
	request.ApprovalHandler = approvalHandlerFunc(func(_ context.Context, _ ApprovalRequest) (ApprovalDecision, error) {
		cancelledAtApproval.Store(true)
		stopTurn()
		return ApprovalDecision{Cancelled: true}, nil
	})
	request.Message = Message{Text: "Use Shell to run exactly: printf should-not-run > cancelled.txt . Do not retry if cancelled."}
	cancelled := sdk.Run(cancelContext, request, &recordingSink{})
	if !cancelledAtApproval.Load() || cancelled.Failure == nil || cancelled.Failure.Code != FailureCodeContextCanceled {
		t.Fatalf("approval cancellation failed: %+v", cancelled.Failure)
	}
	if _, err = os.Stat(filepath.Join(workspace, "cancelled.txt")); !os.IsNotExist(err) {
		t.Fatalf("cancelled operation executed: %v", err)
	}
	request.ApprovalHandler = originalHandler
	answer = run("after-cancel", "Do not execute the cancelled command or any tools. Reply exactly CANCEL_RECOVERY_OK.")
	if !strings.Contains(answer, "CANCEL_RECOVERY_OK") {
		t.Fatalf("cancel recovery lost new message: %s", answer)
	}
	if _, err = os.Stat(filepath.Join(workspace, "cancelled.txt")); !os.IsNotExist(err) {
		t.Fatalf("old cancelled command replayed: %v", err)
	}

}
