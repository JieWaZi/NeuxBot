package messageview

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

func TestToolIdentityKeepsInvocationsSeparate(t *testing.T) {
	records := []eventlog.Record{
		chatCreatedRecord(1, "/workspace/project"), executionCreatedRecord(2, "execution-1"),
		runtimeRecord(3, "execution-1", "execution-1", "invocation-1", constants.RuntimeEventTypeToolUse, map[string]any{"toolCallId": "call-1", "name": "read", "input": map[string]any{"path": "one.go"}}),
		runtimeRecord(4, "execution-1", "execution-1", "invocation-1", constants.RuntimeEventTypeToolResult, map[string]any{"toolCallId": "call-1", "status": "completed"}),
		runtimeRecord(5, "execution-1", "execution-1", "invocation-2", constants.RuntimeEventTypeToolUse, map[string]any{"toolCallId": "call-1", "name": "read", "input": map[string]any{"path": "two.go"}}),
	}
	messages, err := ReplayPublicMessages(records)
	if err != nil {
		t.Fatal(err)
	}
	parts := messages[0].DisplayParts
	if len(parts) != 2 || parts[1].ToolStatus != constants.MessageToolStatusRunning {
		t.Fatalf("same tool IDs across two invocations collapsed: %+v", parts)
	}
}
func TestExplicitClearedToolContentReplacesDiff(t *testing.T) {
	records := []eventlog.Record{
		chatCreatedRecord(1, "/workspace/project"), executionCreatedRecord(2, "execution-1"),
		runtimeRecord(3, "execution-1", "execution-1", "invocation-1", constants.RuntimeEventTypeToolUse, map[string]any{"toolCallId": "call-1", "name": "file_modify", "kind": "edit", "content": []any{map[string]any{"type": "diff", "path": "one.go", "oldText": "old\n", "newText": "new\n"}}}),
		runtimeRecord(4, "execution-1", "execution-1", "invocation-1", constants.RuntimeEventTypeToolUpdate, map[string]any{"toolCallId": "call-1", "content": []any{}}),
		runtimeRecord(5, "execution-1", "execution-1", "invocation-1", constants.RuntimeEventTypeToolResult, map[string]any{"toolCallId": "call-1", "status": "completed"}),
	}
	messages, err := ReplayPublicMessages(records)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages[0].FileChanges) != 0 {
		t.Fatalf("cleared content retains stale file summary: %+v", messages[0].FileChanges)
	}
}

func TestToolProjectionClearsExplicitInputAndKeepsOmittedFields(t *testing.T) {
	builder := NewPublicMessageViewBuilder()
	records := []eventlog.Record{
		chatCreatedRecord(1, "/workspace/project"), executionCreatedRecord(2, "execution-1"),
		runtimeRecord(3, "execution-1", "execution-1", "invocation-1", constants.RuntimeEventTypeToolUse, map[string]any{"toolCallId": "call-1", "name": "read", "rawInput": map[string]any{"path": "old.go"}, "path": "old.go", "input": map[string]any{"path": "old.go"}}),
		runtimeRecord(4, "execution-1", "execution-1", "invocation-1", constants.RuntimeEventTypeToolInputSnapshot, map[string]any{"toolCallId": "call-1", "rawInput": map[string]any{"path": "new.go"}, "path": "new.go", "input": map[string]any{"path": "new.go"}}),
		runtimeRecord(5, "execution-1", "execution-1", "invocation-1", constants.RuntimeEventTypeToolUpdate, map[string]any{"toolCallId": "call-1", "title": "Reading"}),
	}
	for _, record := range records {
		if _, err := builder.Apply(record); err != nil {
			t.Fatal(err)
		}
	}
	part := builder.PublicMessage("execution-1").DisplayParts[0]
	if part.ToolSafeSummary == nil || part.ToolSafeSummary.Path != "new.go" || part.ToolAction != constants.MessageToolActionReadFile {
		t.Fatalf("latest input or omitted identity lost: %+v", part)
	}
	if _, err := builder.Apply(runtimeRecord(6, "execution-1", "execution-1", "invocation-1", constants.RuntimeEventTypeToolInputSnapshot, map[string]any{"toolCallId": "call-1", "rawInput": nil})); err != nil {
		t.Fatal(err)
	}
	part = builder.PublicMessage("execution-1").DisplayParts[0]
	if part.ToolSafeSummary != nil || part.ToolLabel != "读取文件" {
		t.Fatalf("explicit null input kept stale aliases: %+v", part)
	}
}

func TestToolProjectionKeepsPrivateBodiesOutOfCurrentFacts(t *testing.T) {
	builder := NewPublicMessageViewBuilder()
	for _, record := range []eventlog.Record{
		chatCreatedRecord(1, "/workspace/project"), executionCreatedRecord(2, "execution-1"),
		runtimeRecord(3, "execution-1", "execution-1", "invocation-1", constants.RuntimeEventTypeToolUse, map[string]any{"toolCallId": "write-1", "name": "file_modify", "kind": "edit", "input": map[string]any{"content": "private-body-sentinel", "offset": map[string]any{"private": "private-body-sentinel"}}, "rawOutput": "private-output-sentinel"}),
	} {
		if _, err := builder.Apply(record); err != nil {
			t.Fatal(err)
		}
	}
	for _, state := range builder.toolStates["execution-1"] {
		encoded, err := json.Marshal(state.fields)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "private-body-sentinel") || strings.Contains(string(encoded), "private-output-sentinel") {
			t.Fatalf("private input retained as current display facts: %s", encoded)
		}
	}
}

func TestToolInvocationIdentityPreservesComposedFileDiffs(t *testing.T) {
	records := []eventlog.Record{chatCreatedRecord(1, "/workspace/project"), executionCreatedRecord(2, "execution-1")}
	for i, invocation := range []string{"invocation-1", "invocation-2"} {
		oldText, newText := "a\n", "b\n"
		if i == 1 {
			oldText, newText = "b\n", "c\n"
		}
		records = append(records, runtimeRecord(int64(3+i*2), "execution-1", "execution-1", invocation, constants.RuntimeEventTypeToolUse, map[string]any{"toolCallId": "edit-1", "name": "file_modify", "content": []any{map[string]any{"type": "diff", "path": "main.go", "oldText": oldText, "newText": newText}}}), runtimeRecord(int64(4+i*2), "execution-1", "execution-1", invocation, constants.RuntimeEventTypeToolResult, map[string]any{"toolCallId": "edit-1", "status": "completed"}))
	}
	messages, err := ReplayMessages(records)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages[0].FileChanges) != 1 || messages[0].FileChanges[0].AddedLines != 2 || len(messages[0].FileChangeDiffs) != 1 || messages[0].FileChangeDiffs[0].OldContent != "a\n" || messages[0].FileChangeDiffs[0].NewContent != "c\n" {
		t.Fatalf("invocation file changes collapsed: summary=%+v diffs=%+v", messages[0].FileChanges, messages[0].FileChangeDiffs)
	}
}

func TestToolInvocationIdentityKeepsApprovalsSeparate(t *testing.T) {
	records := []eventlog.Record{chatCreatedRecord(1, "/workspace/project"), executionCreatedRecord(2, "execution-1"),
		runtimeRecord(3, "execution-1", "execution-1", "invocation-1", constants.RuntimeEventTypeToolUse, map[string]any{"toolCallId": "call-1", "name": "read"}),
		approvalResolvedRecord(4, "execution-1", eventlog.ApprovalResolvedData{ToolCallID: "call-1", Result: constants.ApprovalResultSelected, OptionName: "Once"}),
		runtimeRecord(5, "execution-1", "execution-1", "invocation-1", constants.RuntimeEventTypeToolResult, map[string]any{"toolCallId": "call-1", "status": "completed"}),
		runtimeRecord(6, "execution-1", "execution-1", "invocation-2", constants.RuntimeEventTypeToolUse, map[string]any{"toolCallId": "call-1", "name": "read"}),
	}
	messages, err := ReplayPublicMessages(records)
	if err != nil {
		t.Fatal(err)
	}
	parts := messages[0].DisplayParts
	if len(parts) != 2 || parts[0].ToolApprovalResult == nil || parts[1].ToolApprovalResult != nil || parts[0].ID == parts[1].ID || parts[0].ToolCallID != parts[1].ToolCallID {
		t.Fatalf("native identity or approval isolation lost: %+v", parts)
	}
}

func TestToolMetadataAfterResultKeepsCoreCommandCaption(t *testing.T) {
	fact, _ := json.Marshal(eventlog.AgentCommandData{CommandID: "receipt", Operation: "workflow.source.save", Completion: "immediate"})
	records := []eventlog.Record{chatCreatedRecord(1, "/workspace"), executionCreatedRecord(2, "execution"),
		{Sequence: 3, ExecutionID: "execution", Type: constants.ChatEventTypeAgentCommandCommitted, Data: fact},
		runtimeRecord(4, "execution", "execution", "invocation", constants.RuntimeEventTypeToolUse, map[string]any{"id": "call", "name": "bash", "input": map[string]any{"command": "allycli workflow source save --input source.json"}}),
		runtimeRecord(5, "execution", "execution", "invocation", constants.RuntimeEventTypeToolResult, map[string]any{"tool_use_id": "call", "output": `{"protocolVersion":1,"ok":true,"commandId":"receipt","operation":"workflow.source.save"}`}),
		runtimeRecord(6, "execution", "execution", "invocation", constants.RuntimeEventTypeToolUpdate, map[string]any{"id": "call", "_meta": map[string]any{"terminal_exit": map[string]any{"exit_code": 0}}}),
	}
	messages, err := ReplayPublicMessages(records)
	if err != nil {
		t.Fatal(err)
	}
	part := messages[0].DisplayParts[0]
	if part.ToolLabel != "保存工作流" || part.ToolStatus != constants.MessageToolStatusCompleted {
		t.Fatalf("late metadata discarded Core-owned caption: %+v", part)
	}
}

func TestOrphanToolResultKeepsPrecedingTextAsProcess(t *testing.T) {
	messages, err := ReplayPublicMessages([]eventlog.Record{executionCreatedRecord(1, "execution"),
		runtimeRecord(2, "execution", "execution", "invocation", constants.RuntimeEventTypeTextDelta, map[string]any{"text": "正在读取"}),
		runtimeRecord(3, "execution", "execution", "invocation", constants.RuntimeEventTypeToolResult, map[string]any{"toolCallId": "read", "status": "completed"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if messages[0].DisplayParts[0].Type != constants.MessageDisplayPartProcessCommentary {
		t.Fatalf("text before an orphan result became final body: %+v", messages[0].DisplayParts)
	}
}
