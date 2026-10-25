package messageview

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

func TestReplayMessagesPreservesSelectedCapabilitySnapshots(t *testing.T) {
	t.Parallel()

	messages, err := ReplayMessages([]eventlog.Record{
		messageRecord(1, constants.ChatEventTypeUserMessageCreated, "user-1", "", eventlog.MessageCreatedData{
			Role:    constants.MessageRoleUser,
			Content: "检查改动",
			Skills: []eventlog.MessageSkillReference{{
				ID:      "skill-review",
				Name:    "代码审查",
				Version: "1.4.0",
			}},
			Connectors: []eventlog.MessageConnectorReference{{
				ID:   "connector-browser",
				Name: "浏览器自动化",
			}},
		}),
	})
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	if len(messages) != 1 || len(messages[0].Skills) != 1 ||
		messages[0].Skills[0].ID != "skill-review" ||
		messages[0].Skills[0].Name != "代码审查" ||
		messages[0].Skills[0].Version != "1.4.0" ||
		len(messages[0].Connectors) != 1 ||
		messages[0].Connectors[0].ID != "connector-browser" ||
		messages[0].Connectors[0].Name != "浏览器自动化" {
		t.Fatalf("messages = %+v", messages)
	}
}

func TestReplayMessagesRestoresRecordedInnermostFailure(t *testing.T) {
	t.Parallel()

	invocationFinished := json.RawMessage(
		`{"failureMessage":"{\"code\":-32603,\"message\":\"Internal error\",\"data\":{\"error\":\"可执行文件 codex 不存在\"}}"}`,
	)
	executionFinished, _ := eventlog.MarshalData(eventlog.ExecutionFinishedData{
		ErrorCode: "RUNTIME_UNAVAILABLE",
		Status:    constants.ExecutionStatusFailed,
	})
	messages, err := ReplayMessages([]eventlog.Record{
		chatCreatedRecord(1, "/workspace/project"),
		executionCreatedRecord(2, "execution-1"),
		{
			ChatID:      "chat-1",
			Sequence:    3,
			RecordedAt:  300,
			Type:        constants.ChatEventTypeInvocationFinished,
			ExecutionID: "execution-1",
			Data:        invocationFinished,
		},
		{
			ChatID:      "chat-1",
			Sequence:    4,
			RecordedAt:  400,
			Type:        constants.ChatEventTypeExecutionFailed,
			ExecutionID: "execution-1",
			Data:        executionFinished,
		},
	})
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	if len(messages) != 1 || messages[0].ErrorMessage != "可执行文件 codex 不存在" {
		t.Fatalf("messages = %#v", messages)
	}
}

func TestReplayMessagesKeepsWholeFileCodeReferencesSeparateFromAttachments(t *testing.T) {
	t.Parallel()

	messages, err := ReplayMessages([]eventlog.Record{
		messageRecord(1, constants.ChatEventTypeUserMessageCreated, "user-1", "", eventlog.MessageCreatedData{
			Role:    constants.MessageRoleUser,
			Content: "检查文件",
			CodeReferences: []eventlog.CodeReference{{
				Path: "projects/ally/server/service.go",
			}},
		}),
	})
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	if len(messages) != 1 || len(messages[0].CodeReferences) != 1 ||
		messages[0].CodeReferences[0].Path != "projects/ally/server/service.go" ||
		len(messages[0].Attachments) != 0 {
		t.Fatalf("messages = %+v", messages)
	}
}

func TestReplayMessagesKeepsOrderedWorkProcessAndFinalText(t *testing.T) {
	t.Parallel()

	records := []eventlog.Record{
		chatCreatedRecord(1, "/workspace/project"),
		executionCreatedRecord(2, "execution-1"),
		runtimeRecord(4, "execution-1", "execution-1", "invocation-1", "text_delta", map[string]any{"text": "我先看一下。"}),
		runtimeRecord(5, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"id":   "tool-1",
			"type": "read",
			"name": "Read",
			"input": map[string]any{
				"file_path": "/workspace/project/README.md",
			},
		}),
		runtimeRecord(6, "execution-1", "execution-1", "invocation-1", "tool_result", map[string]any{
			"tool_use_id": "tool-1",
			"content":     "README secret body",
		}),
		runtimeRecord(7, "execution-1", "execution-1", "invocation-1", "thinking_delta", map[string]any{"text": "确认入口。"}),
		runtimeRecord(8, "execution-1", "execution-1", "invocation-1", "text_delta", map[string]any{"text": "结论正文。"}),
	}

	messages, err := ReplayMessages(records)
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	parts := messages[0].DisplayParts
	if len(parts) != 4 {
		t.Fatalf("len(DisplayParts) = %d, want 4: %#v", len(parts), parts)
	}
	wantTypes := []constants.MessageDisplayPartType{
		constants.MessageDisplayPartProcessCommentary,
		constants.MessageDisplayPartTool,
		constants.MessageDisplayPartReasoning,
		constants.MessageDisplayPartText,
	}
	for index, want := range wantTypes {
		if parts[index].Type != want {
			t.Fatalf("part[%d].Type = %q, want %q; parts=%#v", index, parts[index].Type, want, parts)
		}
	}
	if parts[0].Text != "我先看一下。" || parts[2].Text != "确认入口。" || parts[3].Text != "结论正文。" {
		t.Fatalf("unexpected message view text parts: %#v", parts)
	}
}

func TestReplayMessagesKeepsApprovalResultOnToolActivity(t *testing.T) {
	t.Parallel()

	records := []eventlog.Record{
		chatCreatedRecord(1, "/workspace/project"),
		executionCreatedRecord(2, "execution-1"),
		runtimeRecord(3, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"id":   "tool-1",
			"name": "Bash",
		}),
		approvalResolvedRecord(4, "execution-1", eventlog.ApprovalResolvedData{
			ApprovalID: "approval-1",
			ToolCallID: "tool-1",
			OptionID:   "allow-once",
			OptionName: "Allow once",
			OptionKind: "allow_once",
			Command:    "curl https://chatgpt.com/",
			Result:     constants.ApprovalResultSelected,
		}),
	}

	messages, err := ReplayMessages(records)
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	result := messages[0].DisplayParts[0].ToolApprovalResult
	if result == nil || result.Result != constants.ApprovalResultSelected ||
		result.OptionKind != "allow_once" || result.OptionName != "Allow once" ||
		result.Command != "curl https://chatgpt.com/" {
		t.Fatalf("ToolApprovalResult = %#v", result)
	}
}

func TestReplayMessagesAppliesApprovalResultThatArrivesBeforeToolActivity(t *testing.T) {
	t.Parallel()

	records := []eventlog.Record{
		chatCreatedRecord(1, "/workspace/project"),
		executionCreatedRecord(2, "execution-1"),
		approvalResolvedRecord(3, "execution-1", eventlog.ApprovalResolvedData{
			ApprovalID: "approval-1",
			ToolCallID: "tool-1",
			Result:     constants.ApprovalResultInvalidated,
		}),
		runtimeRecord(4, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"id":   "tool-1",
			"name": "Bash",
		}),
	}

	messages, err := ReplayMessages(records)
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	result := messages[0].DisplayParts[0].ToolApprovalResult
	if result == nil || result.Result != constants.ApprovalResultInvalidated {
		t.Fatalf("ToolApprovalResult = %#v", result)
	}
}

func TestReplayMessagesBuildsSafeToolPhrasesWithoutResults(t *testing.T) {
	t.Parallel()

	records := []eventlog.Record{
		chatCreatedRecord(1, "/workspace/project"),
		executionCreatedRecord(2, "execution-1"),
		runtimeRecord(4, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"id":   "tool-read",
			"name": "Read",
			"input": map[string]any{
				"file_path": "/workspace/project/src/main.go",
				"query":     "secret search",
			},
		}),
		runtimeRecord(5, "execution-1", "execution-1", "invocation-1", "tool_result", map[string]any{
			"tool_use_id": "tool-read",
			"content":     "file body must stay private",
		}),
		runtimeRecord(6, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"id":      "tool-command",
			"name":    "Bash",
			"command": "cat /workspace/project/secret.txt",
		}),
		runtimeRecord(7, "execution-1", "execution-1", "invocation-1", "tool_result", map[string]any{
			"tool_use_id": "tool-command",
			"exit_code":   0,
			"output":      "command output must stay private",
		}),
		runtimeRecord(8, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"id":   "tool-web",
			"name": "WebFetch",
			"input": map[string]any{
				"url": "https://user:pass@example.com/private?q=secret#frag",
			},
		}),
	}

	messages, err := ReplayMessages(records)
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	encoded, err := json.Marshal(messages[0].DisplayParts)
	if err != nil {
		t.Fatalf("Marshal(DisplayParts) error = %v", err)
	}
	jsonText := string(encoded)
	for _, forbidden := range []string{
		"file body must stay private",
		"command output must stay private",
	} {
		if strings.Contains(jsonText, forbidden) {
			t.Fatalf("DisplayParts leaked %q in %s", forbidden, jsonText)
		}
	}
	for _, required := range []string{
		"\"toolSafeSummary\"",
		"\"path\":\"src/main.go\"",
		"\"domain\":\"example.com\"",
		"\"toolLabel\":\"读取 main.go\"",
		"\"toolLabel\":\"读取文件 · cat /workspace/project/secret.txt\"",
		"\"toolLabel\":\"抓取 https://user:pass@example.com/private?q=secret#frag\"",
	} {
		if !strings.Contains(jsonText, required) {
			t.Fatalf("DisplayParts missing %q in %s", required, jsonText)
		}
	}
	for _, forbidden := range []string{"\"exitCode\"", "\"resultCount\""} {
		if strings.Contains(jsonText, forbidden) {
			t.Fatalf("DisplayParts exposed technical summary %q in %s", forbidden, jsonText)
		}
	}
}

func TestReplayMessagesUsesOtherToolForOrphanResult(t *testing.T) {
	t.Parallel()

	records := []eventlog.Record{
		executionCreatedRecord(1, "execution-1"),
		runtimeRecord(2, "execution-1", "execution-1", "invocation-1", "tool_result", map[string]any{
			"tool_use_id": "missing-tool",
			"status":      "error",
			"content":     "raw result must not leak",
		}),
	}

	messages, err := ReplayMessages(records)
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	parts := messages[0].DisplayParts
	if len(parts) != 1 {
		t.Fatalf("len(DisplayParts) = %d, want 1: %#v", len(parts), parts)
	}
	part := parts[0]
	if part.Type != constants.MessageDisplayPartTool ||
		part.ToolCategory != constants.MessageToolCategoryOther ||
		part.ToolStatus != constants.MessageToolStatusFailed {
		t.Fatalf("orphan tool result built as %#v", part)
	}
	encoded, _ := json.Marshal(part)
	if strings.Contains(string(encoded), "raw result must not leak") {
		t.Fatalf("orphan tool result leaked raw content: %s", encoded)
	}
}

func TestMessageViewBuilderKeepsTrailingTextProvisionalWhileToolIsRunning(t *testing.T) {
	t.Parallel()

	builder := NewMessageViewBuilder()
	records := []eventlog.Record{
		executionCreatedRecord(1, "execution-1"),
		runtimeRecord(2, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{"id": "tool-1", "name": "Read"}),
		runtimeRecord(3, "execution-1", "execution-1", "invocation-1", "text_delta", map[string]any{"text": "暂定文字"}),
	}
	for _, record := range records {
		if _, err := builder.Apply(record); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
	}
	message := builder.Messages()[0]
	if message.DisplayParts[1].Type != constants.MessageDisplayPartProcessCommentary {
		t.Fatalf("running tool trailing part = %#v, want process commentary", message.DisplayParts[1])
	}

	if _, err := builder.Apply(runtimeRecord(4, "execution-1", "execution-1", "invocation-1", "tool_result", map[string]any{"tool_use_id": "tool-1"})); err != nil {
		t.Fatalf("Apply(tool result) error = %v", err)
	}
	message = builder.Messages()[0]
	if message.DisplayParts[1].Type != constants.MessageDisplayPartText {
		t.Fatalf("completed tool trailing part = %#v, want final text", message.DisplayParts[1])
	}

	if _, err := builder.Apply(runtimeRecord(5, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{"id": "tool-2", "name": "Grep"})); err != nil {
		t.Fatalf("Apply(next tool) error = %v", err)
	}
	message = builder.Messages()[0]
	if message.DisplayParts[1].Type != constants.MessageDisplayPartProcessCommentary {
		t.Fatalf("text before later tool = %#v, want process commentary", message.DisplayParts[1])
	}
}

func TestMessageViewBuilderEnrichesStreamingToolAndBuildsRunMetrics(t *testing.T) {
	t.Parallel()

	finished, _ := eventlog.MarshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})
	records := []eventlog.Record{
		chatCreatedRecord(1, "/workspace/project"),
		executionCreatedRecord(2, "execution-1"),
		{
			ChatID:      "chat-1",
			Sequence:    4,
			RecordedAt:  300,
			Type:        constants.ChatEventTypeExecutionStarted,
			MessageID:   "execution-1",
			ExecutionID: "execution-1",
		},
		runtimeRecord(5, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"id": "tool-1", "name": "Read", "input": map[string]any{},
		}),
		runtimeRecord(6, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"id": "tool-1", "name": "Read", "input": map[string]any{"file_path": "/workspace/project/README.md"},
		}),
		runtimeRecord(7, "execution-1", "execution-1", "invocation-1", "usage", map[string]any{
			"turn": map[string]any{
				"input_tokens": 60, "cache_read_tokens": 40, "output_tokens": 20, "reasoning_output_tokens": 8,
			},
			"context": map[string]any{"used_tokens": 17400, "window_tokens": 1000000},
		}),
		{
			ChatID:      "chat-1",
			Sequence:    8,
			RecordedAt:  700,
			Type:        constants.ChatEventTypeExecutionCompleted,
			MessageID:   "execution-1",
			ExecutionID: "execution-1",
			Data:        finished,
		},
	}

	messages, err := ReplayMessages(records)
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	message := messages[0]
	if len(message.DisplayParts) != 1 {
		t.Fatalf("len(DisplayParts) = %d, want one enriched tool: %#v", len(message.DisplayParts), message.DisplayParts)
	}
	tool := message.DisplayParts[0]
	if tool.ToolAction != constants.MessageToolActionReadFile || tool.ToolSafeSummary == nil || tool.ToolSafeSummary.Path != "README.md" {
		t.Fatalf("tool view = %#v, want read README.md", tool)
	}
	if message.RunMetrics == nil {
		t.Fatal("RunMetrics = nil")
	}
	if message.RunMetrics.StartedAt != 300 || message.RunMetrics.DurationMs != 400 ||
		message.RunMetrics.InputTokens != 60 || message.RunMetrics.CacheReadTokens != 40 ||
		message.RunMetrics.OutputTokens != 20 || message.RunMetrics.ReasoningOutputTokens != 8 ||
		message.RunMetrics.ContextUsedTokens != 17400 || message.RunMetrics.ContextWindowTokens != 1000000 {
		t.Fatalf("RunMetrics = %#v", message.RunMetrics)
	}
}

func TestReplayMessagesBuildsExecutionFileChangesFromFinalToolDiffs(t *testing.T) {
	t.Parallel()

	finished, _ := eventlog.MarshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})
	records := []eventlog.Record{
		chatCreatedRecord(1, "/workspace/project"),
		executionCreatedRecord(2, "execution-1"),
		runtimeRecord(4, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"name": "file_modify", "status": "in_progress", "toolCallId": "edit-1",
			"content": []any{map[string]any{
				"type": "diff", "path": "/workspace/project/main.go",
				"oldText": "a\nb\n", "newText": "a\nc\n",
			}},
		}),
		runtimeRecord(5, "execution-1", "execution-1", "invocation-1", "tool_result", map[string]any{
			"name": "file_modify", "status": "completed", "toolCallId": "edit-1",
			"content": []any{map[string]any{
				"type": "diff", "path": "/workspace/project/main.go",
				"oldText": "a\nb\n", "newText": "a\nc\nd\n",
			}},
		}),
		runtimeRecord(6, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"name": "file_modify", "status": "completed", "toolCallId": "edit-2",
			"content": []any{map[string]any{
				"type": "diff", "path": "/workspace/project/main.go",
				"oldText": "a\nc\nd\n", "newText": "a\nc\ne\n",
			}},
		}),
		runtimeRecord(7, "execution-1", "execution-1", "invocation-1", "tool_result", map[string]any{
			"name": "file_modify", "status": "failed", "toolCallId": "edit-failed",
			"content": []any{map[string]any{
				"type": "diff", "path": "/workspace/project/failed.go", "newText": "failed\n",
			}},
		}),
		{
			ChatID:      "chat-1",
			Sequence:    8,
			RecordedAt:  800,
			Type:        constants.ChatEventTypeExecutionCompleted,
			MessageID:   "execution-1",
			ExecutionID: "execution-1",
			Data:        finished,
		},
	}

	messages, err := ReplayMessages(records)
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	if len(messages) != 1 || len(messages[0].FileChanges) != 1 {
		t.Fatalf("FileChanges = %#v, want one successful file", messages)
	}
	change := messages[0].FileChanges[0]
	if change.Path != "main.go" || change.AddedLines != 3 || change.DeletedLines != 2 {
		t.Fatalf("FileChanges[0] = %#v, want main.go +3 -2", change)
	}
	if len(messages[0].FileChangeDiffs) != 1 {
		t.Fatalf("FileChangeDiffs = %#v, want one execution diff", messages[0].FileChangeDiffs)
	}
	diff := messages[0].FileChangeDiffs[0]
	if diff.Path != "main.go" || diff.OldContent != "a\nb\n" ||
		diff.NewContent != "a\nc\ne\n" {
		t.Fatalf("FileChangeDiffs[0] = %#v, want first old content and final new content", diff)
	}
	firstTool := messages[0].DisplayParts[0]
	if firstTool.ToolSafeSummary == nil || len(firstTool.ToolSafeSummary.Files) != 1 ||
		firstTool.ToolSafeSummary.Files[0].AddedLines != 2 ||
		firstTool.ToolSafeSummary.Files[0].DeletedLines != 1 {
		t.Fatalf("first tool summary = %#v, want final refinement only", firstTool.ToolSafeSummary)
	}
}

// TestMessageSnapshotsKeepDiffRefinementsAndIsolation 覆盖延迟投影的中间快照、失败回退及快照隔离。
func TestMessageSnapshotsKeepDiffRefinementsAndIsolation(t *testing.T) {
	t.Parallel()
	builder := NewMessageViewBuilder()
	apply := func(record eventlog.Record) {
		t.Helper()
		if _, err := builder.Apply(record); err != nil {
			t.Fatal(err)
		}
	}
	apply(chatCreatedRecord(1, "/workspace"))
	apply(executionCreatedRecord(2, "execution-1"))
	edit := func(sequence int64, toolID, oldText, newText string) eventlog.Record {
		return runtimeRecord(sequence, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"name": "file_modify", "toolCallId": toolID,
			"content": []any{map[string]any{
				"type": "diff", "path": "/workspace/main.go", "oldText": oldText, "newText": newText,
			}},
		})
	}
	apply(edit(3, "edit-1", "a\n", "b\n"))
	first := builder.Messages()[0]
	if want := []MessageFileChangeDiff{{Path: "main.go", OldContent: "a\n", NewContent: "b\n"}}; !reflect.DeepEqual(first.FileChangeDiffs, want) {
		t.Fatalf("initial diffs = %+v, want %+v", first.FileChangeDiffs, want)
	}
	apply(edit(4, "edit-1", "a\n", "c\n"))
	// 不含 diff 的后续工具更新不能清除该工具最后一次完整差异。
	apply(runtimeRecord(5, "execution-1", "execution-1", "invocation-1", "tool_result", map[string]any{
		"toolCallId": "edit-1", "status": "completed",
	}))
	apply(edit(6, "edit-2", "c\n", "d\n"))
	current := builder.Messages()[0]
	if want := []MessageFileChangeDiff{{Path: "main.go", OldContent: "a\n", NewContent: "d\n"}}; !reflect.DeepEqual(current.FileChangeDiffs, want) {
		t.Fatalf("refined diffs = %+v, want %+v", current.FileChangeDiffs, want)
	}
	if first.FileChangeDiffs[0].NewContent != "b\n" {
		t.Fatalf("earlier snapshot mutated: %+v", first.FileChangeDiffs)
	}
	apply(runtimeRecord(7, "execution-1", "execution-1", "invocation-1", "tool_result", map[string]any{
		"toolCallId": "edit-2", "status": "failed",
	}))
	current = builder.Messages()[0]
	if want := []MessageFileChangeDiff{{Path: "main.go", OldContent: "a\n", NewContent: "c\n"}}; !reflect.DeepEqual(current.FileChangeDiffs, want) {
		t.Fatalf("diffs after tool failure = %+v, want %+v", current.FileChangeDiffs, want)
	}
	if want := []ToolFileChangeSummary{{Path: "main.go", AddedLines: 1, DeletedLines: 1}}; !reflect.DeepEqual(current.FileChanges, want) {
		t.Fatalf("summaries after tool failure = %+v, want %+v", current.FileChanges, want)
	}
	public := builder.PublicMessage("execution-1")
	privateJSON, _ := json.Marshal(current)
	publicJSON, _ := json.Marshal(public)
	if string(privateJSON) != string(publicJSON) || len(public.RuntimeEvents) != 0 || len(public.FileChangeDiffs) != 0 {
		t.Fatal("public snapshot must keep the same display without private runtime events or diffs")
	}
	current.FileChangeDiffs[0].NewContent = "mutated"
	current.FileChanges[0].Path = "mutated"
	if next := builder.Messages()[0]; next.FileChangeDiffs[0].NewContent != "c\n" || next.FileChanges[0].Path != "main.go" {
		t.Fatalf("caller mutated builder state: %+v", next)
	}
}

// TestReplayPublicMessagesPreservesInheritedFiles 验证公开历史与分叉内部历史使用相同展示事实。
func TestReplayPublicMessagesPreservesInheritedFiles(t *testing.T) {
	t.Parallel()
	inherited, _ := eventlog.MarshalData(InheritedMessage{
		SourceMessageID: "source-message",
		Message: Message{
			Inherited: true, Role: constants.MessageRoleAssistant, Content: "已更新文件",
			FileChanges: []ToolFileChangeSummary{{Path: "main.go", AddedLines: 1}},
		},
		Diffs: []MessageFileChangeDiff{{Path: "main.go", NewContent: "package main\n"}},
	})
	records := []eventlog.Record{
		chatCreatedRecord(1, "/workspace"),
		{Sequence: 2, MessageID: "inherited-1", Type: constants.ChatEventTypeMessageInherited, Data: inherited},
		executionCreatedRecord(3, "execution-2"),
		runtimeRecord(4, "execution-2", "execution-2", "invocation-2", "text_delta", map[string]any{"text": "后续答复"}),
	}
	private, err := ReplayMessages(records)
	if err != nil {
		t.Fatal(err)
	}
	public, err := ReplayPublicMessages(records)
	if err != nil {
		t.Fatal(err)
	}
	privateJSON, _ := json.Marshal(private)
	publicJSON, _ := json.Marshal(public)
	if string(privateJSON) != string(publicJSON) || len(public) != 2 || public[1].Content != "后续答复" {
		t.Fatalf("public/private display mismatch: %s / %s", publicJSON, privateJSON)
	}
	if len(private[0].FileChangeDiffs) != 1 || private[0].FileChangeDiffs[0].NewContent != "package main\n" || len(private[1].RuntimeEvents) != 1 {
		t.Fatal("internal replay lost inherited diffs or runtime events")
	}
	for _, message := range public {
		if len(message.FileChangeDiffs) != 0 || len(message.RuntimeEvents) != 0 {
			t.Fatal("public history retained private data")
		}
	}
}

// TestReplayMessagesMapsRealProjectDiffPathToWorkspaceEntry 验证消息路径可以由桌面端通过工作区入口读取。
func TestReplayMessagesMapsRealProjectDiffPathToWorkspaceEntry(t *testing.T) {
	t.Parallel()

	workspaceDirectory := filepath.Join(t.TempDir(), "chat")
	projectDirectory := t.TempDir()
	created, _ := eventlog.MarshalData(eventlog.ChatCreatedData{
		WakerID:                      "waker-1",
		WorkspaceDirectory:           workspaceDirectory,
		AdditionalWorkingDirectories: []string{projectDirectory},
		Title:                        "测试对话",
		ProjectSources: []eventlog.ProjectSourceSnapshot{{
			EntryName:  "app",
			SourcePath: projectDirectory,
		}},
	})
	records := []eventlog.Record{
		{
			ChatID:     "chat-1",
			Sequence:   1,
			RecordedAt: 100,
			Type:       constants.ChatEventTypeCreated,
			Data:       created,
		},
		executionCreatedRecord(2, "execution-1"),
		runtimeRecord(4, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"name": "file_modify", "status": "completed", "toolCallId": "edit-1",
			"content": []any{map[string]any{
				"type": "diff", "path": filepath.Join(projectDirectory, "main.go"),
				"newText": "package main\n",
			}},
		}),
	}

	messages, err := ReplayMessages(records)
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	if len(messages) != 1 || len(messages[0].FileChanges) != 1 {
		t.Fatalf("FileChanges = %#v, want one project file", messages)
	}
	if path := messages[0].FileChanges[0].Path; path != "projects/app/main.go" {
		t.Fatalf("FileChanges[0].Path = %q, want projects/app/main.go", path)
	}
}

// TestBuildToolSafeSummaryIgnoresLegacyRuntimeChanges 验证文件修改只消费标准 ACP diff。
func TestBuildToolSafeSummaryIgnoresLegacyRuntimeChanges(t *testing.T) {
	t.Parallel()

	summary := buildToolSafeSummary(map[string]interface{}{
		"changes": []interface{}{map[string]interface{}{
			"path": "/workspace/project/legacy.go",
			"diff": "--- a/legacy.go\n+++ b/legacy.go\n-old\n+new\n",
		}},
	}, workspacePathScope{workspaceDirectory: "/workspace/project"})
	if summary != nil {
		t.Fatalf("buildToolSafeSummary() = %#v, want legacy Runtime changes ignored", summary)
	}
}

func TestReplayMessagesUsesStructuredCodexToolSemantics(t *testing.T) {
	t.Parallel()

	records := []eventlog.Record{
		chatCreatedRecord(1, "/workspace/project"),
		executionCreatedRecord(2, "execution-1"),
		runtimeRecord(4, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"id":   "tool-search",
			"name": "command",
			"type": "commandExecution",
			"commandActions": []any{map[string]any{
				"type": "search", "path": "/workspace/project/server",
			}},
			"command": "rg secret /workspace/project/server",
		}),
	}

	messages, err := ReplayMessages(records)
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	part := messages[0].DisplayParts[0]
	if part.ToolAction != constants.MessageToolActionSearchFiles ||
		part.ToolCategory != constants.MessageToolCategoryFileSearch ||
		part.ToolSafeSummary == nil || part.ToolSafeSummary.Path != "server" {
		t.Fatalf("Codex tool view = %#v", part)
	}
	encoded, _ := json.Marshal(part)
	if strings.Contains(string(encoded), "secret") {
		t.Fatalf("Codex tool view leaked command: %s", encoded)
	}
}

func TestBuildToolPhraseUsesBoundedSemanticRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		// name 表示当前工具短语测试场景名称。
		name string
		// data 表示用于构建展示短语的归一化工具事件数据。
		data map[string]interface{}
		// want 表示期望生成的完成态工具短语。
		want string
	}{
		{
			name: "grep pattern and glob",
			data: map[string]interface{}{
				"name":  "Grep",
				"input": map[string]interface{}{"pattern": "adapter", "glob": "**/*.go"},
			},
			want: "搜索内容 /adapter/ in **/*.go",
		},
		{
			name: "managed user input with bounded question",
			data: map[string]interface{}{
				"name": "mcp__acp_go_user_input__AskUserQuestion",
				"input": map[string]interface{}{
					"questions": []interface{}{map[string]interface{}{
						"question": "  你希望\n如何展示？  ",
						"options":  []interface{}{"private option"},
					}},
					"token": "private credential",
				},
			},
			want: "向你提问 · 你希望 如何展示？",
		},
		{
			name: "standard ACP managed question without input",
			data: map[string]interface{}{
				"name": "mcp", "server": "acp_go_user_input", "tool": "AskUserQuestion",
			},
			want: "向你提问",
		},
		{
			name: "cursor managed question",
			data: map[string]interface{}{
				"name": "acp_go_user_input: AskUserQuestion",
				"rawInput": map[string]interface{}{
					"providerIdentifier": "acp_go_user_input", "toolName": "AskUserQuestion",
					"args": map[string]interface{}{
						"questions": []interface{}{map[string]interface{}{"question": "选择展示方式"}},
					},
				},
			},
			want: "向你提问 · 选择展示方式",
		},
		{
			name: "pi flattened managed question",
			data: map[string]interface{}{
				"name": "mcp",
				"rawInput": map[string]interface{}{
					"tool": "acp_go_user_input_AskUserQuestion",
					"args": map[string]interface{}{
						"questions": []interface{}{map[string]interface{}{"question": "继续吗？"}},
					},
				},
			},
			want: "向你提问 · 继续吗？",
		},
		{
			name: "direct managed question name",
			data: map[string]interface{}{"name": "acp_go_user_input_AskUserQuestion"},
			want: "向你提问",
		},
		{
			name: "native question ignores malformed entries",
			data: map[string]interface{}{
				"name": "AskUserQuestion",
				"input": map[string]interface{}{
					"questions": []interface{}{nil, "invalid", map[string]interface{}{"question": " "},
						map[string]interface{}{"question": strings.Repeat("问", 100)}},
				},
			},
			want: "向你提问 · " + strings.Repeat("问", 80) + "…",
		},
		{
			name: "managed question without question text does not expose metadata",
			data: map[string]interface{}{
				"name": "mcp__acp_go_user_input__AskUserQuestion",
				"input": map[string]interface{}{
					"questions": "invalid", "description": "private metadata",
				},
			},
			want: "向你提问",
		},
		{
			name: "external question tool retains external identity",
			data: map[string]interface{}{"name": "mcp__external__AskUserQuestion"},
			want: "EXTERNAL / AskUserQuestion mcp__external__AskUserQuestion",
		},
		{
			name: "other managed tool retains its identity",
			data: map[string]interface{}{"name": "mcp__acp_go_user_input__different_tool"},
			want: "ACP_GO_USER_INPUT / different_tool mcp__acp_go_user_input__different_tool",
		},
		{
			name: "mcp tool and meaningful argument",
			data: map[string]interface{}{
				"name":  "mcp__github__search_code",
				"input": map[string]interface{}{"query": "adapter"},
			},
			want: "GITHUB / search_code adapter",
		},
		{
			name: "standard ACP builtin memory search",
			data: map[string]interface{}{
				"name":      "mcp",
				"server":    "ally_memory_builtin",
				"tool":      "memory_search",
				"arguments": map[string]interface{}{"query": "用户偏好"},
			},
			want: "搜索记忆",
		},
		{
			name: "cursor builtin memory search",
			data: map[string]interface{}{
				"name": "ally_memory_builtin: memory_search",
				"rawInput": map[string]interface{}{
					"providerIdentifier": "ally_memory_builtin",
					"toolName":           "memory_search",
					"args":               map[string]interface{}{"query": "用户偏好"},
				},
			},
			want: "搜索记忆",
		},
		{
			name: "pi builtin memory search",
			data: map[string]interface{}{
				"name": "mcp",
				"rawInput": map[string]interface{}{
					"tool": "ally_memory_builtin_memory_search",
					"args": map[string]interface{}{"query": "用户偏好"},
				},
			},
			want: "搜索记忆",
		},
		{
			name: "kimi direct builtin memory search",
			data: map[string]interface{}{
				"name":  "ally_memory_builtin_memory_search",
				"input": map[string]interface{}{"query": "用户偏好"},
			},
			want: "搜索记忆",
		},
		{
			name: "builtin memory write",
			data: map[string]interface{}{
				"name":  "mcp__ally_memory_builtin__memory_write",
				"input": map[string]interface{}{"content": "回答先给结论"},
			},
			want: "保存记忆",
		},
		{
			name: "builtin memory update",
			data: map[string]interface{}{
				"name":   "mcp",
				"server": "ally_memory_builtin",
				"tool":   "memory_update",
			},
			want: "更新记忆",
		},
		{
			name: "builtin memory forget",
			data: map[string]interface{}{
				"name":   "mcp",
				"server": "ally_memory_builtin",
				"tool":   "memory_forget",
			},
			want: "遗忘记忆",
		},
		{
			name: "workflow source save from Codex",
			data: map[string]interface{}{"name": "mcp__ally_workflow__workflow", "input": map[string]interface{}{"action": "source.upsert", "params": map[string]interface{}{"source": "private source"}}},
			want: "保存工作流",
		},
		{
			name: "workflow resources from ACP",
			data: map[string]interface{}{"name": "mcp", "server": "ally_workflow", "tool": "workflow", "arguments": map[string]interface{}{"action": constants.WorkflowActionQuery, "type": constants.WorkflowQueryResources}},
			want: "查看可用伙伴与资源",
		},
		{
			name: "workflow validation from Pi",
			data: map[string]interface{}{"name": "mcp", "rawInput": map[string]interface{}{"tool": "ally_workflow_workflow", "args": map[string]interface{}{"action": "source.validate"}}},
			want: "检查工作流",
		},
		{
			name: "workflow result submission",
			data: map[string]interface{}{"name": "mcp__ally_workflow__workflow", "input": map[string]interface{}{"action": constants.WorkflowActionResultSubmit, "params": map[string]interface{}{"data": "private output"}}},
			want: "提交步骤结果",
		},
		{
			name: "workflow authorized directory query",
			data: map[string]interface{}{"name": "mcp__ally_workflow__workflow", "input": map[string]interface{}{"action": "query", "type": "workflows"}},
			want: "查看可用工作流",
		},
		{
			name: "workflow result schema query",
			data: map[string]interface{}{"name": "mcp__ally_workflow__workflow", "input": map[string]interface{}{"action": "query", "type": "result_schema"}},
			want: "查看步骤结果格式",
		},
		{
			name: "saved legacy workflow result remains readable",
			data: map[string]interface{}{"name": "ally_workflow_result_workflow_result", "input": map[string]interface{}{"action": "submit"}},
			want: "提交步骤结果",
		},
		{
			name: "saved legacy workflow directory remains readable",
			data: map[string]interface{}{"name": "mcp__ally_workflow_use__workflow_use", "input": map[string]interface{}{"action": "workflows.list"}},
			want: "查看可用工作流",
		},
		{
			name: "builtin channel task create",
			data: map[string]interface{}{
				"name":   "mcp",
				"server": constants.BuiltinChannelTasksMCPConnectorRuntimeKey,
				"tool":   "channel_tasks",
				"arguments": map[string]interface{}{
					"action": "create",
				},
			},
			want: "分配团队任务",
		},
		{
			name: "builtin channel task list from tool name",
			data: map[string]interface{}{
				"name": "mcp__ally_channel_tasks_builtin__channel_tasks",
				"input": map[string]interface{}{
					"action": "list",
				},
			},
			want: "查看团队任务",
		},
		{
			name: "task description",
			data: map[string]interface{}{
				"name":  "Task",
				"input": map[string]interface{}{"description": "修复流式展示"},
			},
			want: "子任务 修复流式展示",
		},
		{
			name: "claude bash with intent description",
			data: map[string]interface{}{
				"type": "tool_use",
				"name": "Bash",
				"input": map[string]interface{}{
					"command":     "pnpm --dir desktop check",
					"description": "运行前端完整检查",
				},
			},
			want: "运行前端完整检查",
		},
		{
			name: "claude bash nested command",
			data: map[string]interface{}{
				"type": "tool_use",
				"name": "Bash",
				"input": map[string]interface{}{
					"command": "ls -la desktop",
				},
			},
			want: "执行 ls -la desktop",
		},
		{
			name: "claude web search nested query",
			data: map[string]interface{}{
				"type": "tool_use",
				"name": "WebSearch",
				"input": map[string]interface{}{
					"query": "今日科技新闻",
				},
			},
			want: "搜索网页 \"今日科技新闻\"",
		},
		{
			name: "codex web search queries array",
			data: map[string]interface{}{
				"name": "web_search",
				"type": "webSearch",
				"action": map[string]interface{}{
					"type":    "search",
					"queries": []interface{}{"今日科技新闻"},
				},
			},
			want: "搜索网页 \"今日科技新闻\"",
		},
		{
			name: "codex web search open page",
			data: map[string]interface{}{
				"name": "web_search",
				"type": "webSearch",
				"action": map[string]interface{}{
					"type": "openPage",
					"url":  "https://www.example.com/news",
				},
			},
			want: "浏览页面 www.example.com",
		},
		{
			name: "codex web search find in page",
			data: map[string]interface{}{
				"name": "web_search",
				"type": "webSearch",
				"action": map[string]interface{}{
					"type":    "findInPage",
					"url":     "https://example.com",
					"pattern": "发布",
				},
			},
			want: "页面内查找 \"发布\"",
		},
		{
			name: "web search fallback",
			data: map[string]interface{}{
				"name": "web_search",
				"type": "webSearch",
			},
			want: "搜索网页",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label, loadingLabel := buildToolPhrase(
				tt.data,
				workspacePathScope{workspaceDirectory: "/workspace/project"},
			)
			if label != tt.want || loadingLabel != "正在"+tt.want+"…" {
				t.Fatalf("buildToolPhrase() = %q, %q, want %q", label, loadingLabel, tt.want)
			}
		})
	}
}

func TestReplayMessagesUsesClaudeNestedToolInputAndLateResultQuery(t *testing.T) {
	t.Parallel()

	records := []eventlog.Record{
		chatCreatedRecord(1, "/workspace/project"),
		executionCreatedRecord(2, "execution-1"),
		runtimeRecord(4, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"type": "tool_use",
			"id":   "tool-bash",
			"name": "Bash",
			"input": map[string]any{
				"command":     "pnpm check",
				"description": "运行前端完整检查",
			},
		}),
		runtimeRecord(5, "execution-1", "execution-1", "invocation-1", "tool_result", map[string]any{
			"type":        "tool_result",
			"tool_use_id": "tool-bash",
			"content":     "result body must stay private",
		}),
		runtimeRecord(6, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"type": "webSearch",
			"id":   "tool-web",
			"name": "web_search",
		}),
		runtimeRecord(7, "execution-1", "execution-1", "invocation-1", "tool_result", map[string]any{
			"type":  "webSearch",
			"id":    "tool-web",
			"name":  "web_search",
			"query": "今日科技新闻",
		}),
	}

	messages, err := ReplayMessages(records)
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	parts := messages[0].DisplayParts
	if len(parts) != 2 {
		t.Fatalf("len(DisplayParts) = %d, want 2: %#v", len(parts), parts)
	}
	bash := parts[0]
	if bash.ToolAction != constants.MessageToolActionCheckProject ||
		bash.ToolCategory != constants.MessageToolCategoryCommand ||
		bash.ToolLabel != "运行前端完整检查" ||
		bash.ToolStatus != constants.MessageToolStatusCompleted {
		t.Fatalf("claude bash view = %#v", bash)
	}
	web := parts[1]
	if web.ToolAction != constants.MessageToolActionSearchWeb ||
		web.ToolCategory != constants.MessageToolCategoryNetwork ||
		web.ToolLabel != "搜索网页 \"今日科技新闻\"" ||
		web.ToolStatus != constants.MessageToolStatusCompleted {
		t.Fatalf("web search view = %#v", web)
	}
	encoded, _ := json.Marshal(parts)
	if strings.Contains(string(encoded), "result body must stay private") {
		t.Fatalf("DisplayParts leaked tool result body: %s", encoded)
	}
}

// TestReplayMessagesRecognizesStandardACPMemoryTool 验证标准 ACP MCP 载荷仍展示产品语义。
func TestReplayMessagesRecognizesStandardACPMemoryTool(t *testing.T) {
	t.Parallel()

	rawInput := map[string]any{
		"server": "ally_memory_builtin",
		"tool":   "memory_search",
		"arguments": map[string]any{
			"query": "用户最近保存的记忆条目",
			"scope": "all",
			"top_k": float64(20),
		},
	}
	records := []eventlog.Record{
		chatCreatedRecord(1, "/workspace/project"),
		executionCreatedRecord(2, "execution-1"),
		runtimeRecord(4, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"_meta":      map[string]any{"is_mcp_tool_call": true},
			"kind":       "execute",
			"name":       "command",
			"rawInput":   rawInput,
			"status":     "in_progress",
			"title":      "mcp.ally_memory_builtin.memory_search",
			"toolCallId": "mcp-memory-1",
		}),
		runtimeRecord(5, "execution-1", "execution-1", "invocation-1", "tool_result", map[string]any{
			"name":       "unknown",
			"rawInput":   rawInput,
			"rawOutput":  map[string]any{"result": map[string]any{"content": []any{}}},
			"status":     "completed",
			"toolCallId": "mcp-memory-1",
		}),
	}

	messages, err := ReplayMessages(records)
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	parts := messages[0].DisplayParts
	if len(parts) != 1 {
		t.Fatalf("len(DisplayParts) = %d, want 1: %#v", len(parts), parts)
	}
	part := parts[0]
	if part.ToolCategory != constants.MessageToolCategoryMCP ||
		part.ToolAction != constants.MessageToolActionCallService ||
		part.ToolLabel != "搜索记忆" ||
		part.ToolStatus != constants.MessageToolStatusCompleted {
		t.Fatalf("ACP memory tool view = %#v", part)
	}
}

// TestMessageViewBuilderKeepsUserQuestionAcrossUpdates 验证提问摘要在实时更新及历史重放中保持一致。
func TestMessageViewBuilderKeepsUserQuestionAcrossUpdates(t *testing.T) {
	t.Parallel()

	records := []eventlog.Record{
		executionCreatedRecord(1, "execution-1"),
		runtimeRecord(2, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"id": "question-1", "name": "mcp__acp_go_user_input__AskUserQuestion",
		}),
		runtimeRecord(3, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"id": "question-1", "name": "mcp__acp_go_user_input__AskUserQuestion",
			"rawInput": map[string]any{
				"server": "acp_go_user_input", "tool": "AskUserQuestion",
				"arguments": map[string]any{
					"questions": []any{map[string]any{"question": "选择展示方式"}},
				},
			},
		}),
		runtimeRecord(4, "execution-1", "execution-1", "invocation-1", "tool_result", map[string]any{
			"tool_use_id": "question-1",
			"rawOutput":   map[string]any{"answers": "private answer"},
		}),
	}
	builder := NewMessageViewBuilder()
	for index, record := range records {
		if _, err := builder.Apply(record); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if index == 0 {
			continue
		}
		parts := builder.Messages()[0].DisplayParts
		want := "向你提问"
		if index >= 2 {
			want += " · 选择展示方式"
		}
		if len(parts) != 1 || parts[0].ToolLabel != want ||
			parts[0].ToolLoadingLabel != "正在"+want+"…" {
			t.Fatalf("question view at update %d = %#v", index, parts)
		}
		if index == 3 && parts[0].ToolStatus != constants.MessageToolStatusCompleted {
			t.Fatalf("completed question status = %q", parts[0].ToolStatus)
		}
	}
	replayed, err := ReplayMessages(records)
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	liveJSON, _ := json.Marshal(builder.Messages()[0].DisplayParts)
	replayedJSON, _ := json.Marshal(replayed[0].DisplayParts)
	if string(liveJSON) != string(replayedJSON) || strings.Contains(string(liveJSON), "private answer") {
		t.Fatalf("question projection differs or leaks answer: live=%s replay=%s", liveJSON, replayedJSON)
	}
}

// TestReplayMessagesNormalizesBuiltinMemoryAcrossRuntimeShapes 验证不同 CLI 的 MCP 事件展示一致。
func TestReplayMessagesNormalizesBuiltinMemoryAcrossRuntimeShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		// name 是 CLI 及其原生事件形态。
		name string
		// updates 是同一次内置记忆调用产生的工具事件数据。
		updates []map[string]any
	}{
		{
			name: "cursor provider fields after generic start",
			updates: []map[string]any{
				{
					"name": "MCP: tool", "rawInput": map[string]any{},
					"status": "pending", "toolCallId": "memory-1",
				},
				{
					"name": "ally_memory_builtin: memory_search",
					"rawInput": map[string]any{
						"providerIdentifier": "ally_memory_builtin",
						"toolName":           "memory_search",
						"args":               map[string]any{"query": "用户偏好"},
					},
					"status": "in_progress", "toolCallId": "memory-1",
				},
				{"status": "completed", "toolCallId": "memory-1"},
			},
		},
		{
			name: "pi gateway flattened tool",
			updates: []map[string]any{
				{
					"name": "mcp",
					"rawInput": map[string]any{
						"tool": "ally_memory_builtin_memory_search",
						"args": map[string]any{"query": "用户偏好"},
					},
					"status": "pending", "toolCallId": "memory-1",
				},
				{"status": "in_progress", "toolCallId": "memory-1"},
				{"status": "completed", "toolCallId": "memory-1"},
			},
		},
		{
			name: "kimi standard ACP tool",
			updates: []map[string]any{
				{
					"name": "mcp__ally_memory_builtin__memory_search",
					"rawInput": map[string]any{
						"server": "ally_memory_builtin", "tool": "memory_search",
						"arguments": map[string]any{"query": "用户偏好"},
					},
					"status": "in_progress", "toolCallId": "memory-1",
				},
				{"status": "completed", "toolCallId": "memory-1"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			records := []eventlog.Record{
				chatCreatedRecord(1, "/workspace/project"),
				executionCreatedRecord(2, "execution-1"),
			}
			for index, update := range test.updates {
				eventType := constants.RuntimeEventTypeToolUse
				if update["status"] == "completed" {
					eventType = constants.RuntimeEventTypeToolResult
				}
				records = append(records, runtimeRecord(
					int64(index+4),
					"execution-1",
					"execution-1",
					"invocation-1",
					eventType,
					update,
				))
			}

			messages, err := ReplayMessages(records)
			if err != nil {
				t.Fatalf("ReplayMessages() error = %v", err)
			}
			parts := messages[0].DisplayParts
			if len(parts) != 1 || parts[0].ToolLabel != "搜索记忆" ||
				parts[0].ToolStatus != constants.MessageToolStatusCompleted {
				t.Fatalf("memory tool view = %#v", parts)
			}
		})
	}
}

// TestReplayMessagesNormalizesBuiltinIMAcrossRuntimeShapes 验证 IM 交付只展示产品动作且完成更新不降级短语。
func TestReplayMessagesNormalizesBuiltinIMAcrossRuntimeShapes(t *testing.T) {
	t.Parallel()

	actions := []struct {
		// action 是 IM 工具实际接收的操作标识。
		action string
		// label 是消息中应展示的产品动作。
		label string
	}{
		{"targets.list", "查看可发送聊天"},
		{"deliveries.create", "提交消息交付"},
		{"deliveries.get", "查看交付状态"},
		{"", "管理消息交付"},
		{"unknown", "管理消息交付"},
	}
	for _, action := range actions {
		t.Run(action.action, func(t *testing.T) {
			t.Parallel()
			input := map[string]any{
				"action": action.action, "targetId": "private-target", "requestId": "private-request",
				"content": "private-message", "files": []any{"/workspace/project/private-file.txt"},
			}
			shapes := map[string]map[string]any{
				"standard ACP": {"name": "mcp", "server": "ally_im", "tool": "im_delivery", "arguments": input},
				"Codex":        {"name": "mcp__ally_im__im_delivery", "input": input},
				"Cursor": {"name": "ally_im: im_delivery", "rawInput": map[string]any{
					"providerIdentifier": "ally_im", "toolName": "im_delivery", "args": input,
				}},
				"Pi": {"name": "mcp", "rawInput": map[string]any{
					"tool": "ally_im_im_delivery", "args": input,
				}},
				"flattened direct": {"name": "ally_im_im_delivery", "input": input},
			}
			for name, data := range shapes {
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					data["toolCallId"] = "im-1"
					data["status"] = "in_progress"
					records := []eventlog.Record{
						chatCreatedRecord(1, "/workspace/project"),
						executionCreatedRecord(2, "execution-1"),
						runtimeRecord(3, "execution-1", "execution-1", "invocation-1", constants.RuntimeEventTypeToolUse, data),
					}
					messages, err := ReplayMessages(records)
					if err != nil {
						t.Fatal(err)
					}
					parts := messages[0].DisplayParts
					if len(parts) != 1 || parts[0].ToolLabel != action.label ||
						parts[0].ToolLoadingLabel != "正在"+action.label+"…" ||
						parts[0].ToolCategory != constants.MessageToolCategoryMCP {
						t.Fatalf("IM tool view = %#v", parts)
					}
					records = append(records, runtimeRecord(4, "execution-1", "execution-1", "invocation-1", constants.RuntimeEventTypeToolResult, map[string]any{
						"toolCallId": "im-1", "status": "completed", "output": "private-result",
					}))
					messages, err = ReplayMessages(records)
					if err != nil {
						t.Fatal(err)
					}
					parts = messages[0].DisplayParts
					if len(parts) != 1 || parts[0].ToolLabel != action.label ||
						parts[0].ToolStatus != constants.MessageToolStatusCompleted {
						t.Fatalf("completed IM tool view = %#v", parts)
					}
					encoded, err := json.Marshal(messages)
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(string(encoded), "private-") || strings.Contains(string(encoded), "ally_im") || strings.Contains(string(encoded), "im_delivery") {
						t.Fatalf("IM display exposes protocol or private input: %s", encoded)
					}
				})
			}
		})
	}
}

// TestReplayMessagesPreservesRichToolLabelOnBareACPUpdate 验证 bare tool update 不会降级 richer 标签。
func TestReplayMessagesPreservesRichToolLabelOnBareACPUpdate(t *testing.T) {
	t.Parallel()

	rawInput := map[string]any{
		"server": "ally_memory_builtin",
		"tool":   "memory_search",
		"arguments": map[string]any{
			"query": "用户偏好",
		},
	}
	records := []eventlog.Record{
		chatCreatedRecord(1, "/workspace/project"),
		executionCreatedRecord(2, "execution-1"),
		runtimeRecord(4, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"_meta":      map[string]any{"is_mcp_tool_call": true},
			"kind":       "execute",
			"name":       "command",
			"rawInput":   rawInput,
			"status":     "in_progress",
			"title":      "mcp.ally_memory_builtin.memory_search",
			"toolCallId": "mcp-memory-1",
		}),
		runtimeRecord(5, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"toolCallId": "mcp-memory-1",
			"status":     "in_progress",
			"rawOutput":  map[string]any{"elapsedTimeSeconds": 1.5},
		}),
		runtimeRecord(6, "execution-1", "execution-1", "invocation-1", "tool_result", map[string]any{
			"status":     "completed",
			"toolCallId": "mcp-memory-1",
		}),
	}

	messages, err := ReplayMessages(records)
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	part := messages[0].DisplayParts[0]
	if part.ToolLabel != "搜索记忆" {
		t.Fatalf("ToolLabel = %q, want 搜索记忆", part.ToolLabel)
	}
}

// TestMergeToolPhrasePreservesRicherLabelOnBareUpdate 验证 mergeToolPhrase 的单调合并规则。
func TestMergeToolPhrasePreservesRicherLabelOnBareUpdate(t *testing.T) {
	t.Parallel()

	label, loading := mergeToolPhrase("搜索记忆", "正在搜索记忆…", "执行操作", "正在执行操作…")
	if label != "搜索记忆" || loading != "正在搜索记忆…" {
		t.Fatalf("mergeToolPhrase() = (%q, %q), want richer existing label", label, loading)
	}
	label, loading = mergeToolPhrase("", "", "读取 main.go", "正在读取 main.go…")
	if label != "读取 main.go" {
		t.Fatalf("empty existing label = %q, want incoming label", label)
	}
	label, _ = mergeToolPhrase("执行 ls", "正在执行 ls…", "运行前端完整检查", "正在运行前端完整检查…")
	if label != "运行前端完整检查" {
		t.Fatalf("longer incoming label = %q", label)
	}
	label, loading = mergeToolPhrase("调用外部服务", "正在调用外部服务…", "搜索记忆", "正在搜索记忆…")
	if label != "搜索记忆" || loading != "正在搜索记忆…" {
		t.Fatalf("generic MCP label = (%q, %q), want richer builtin label", label, loading)
	}
}

// TestReplayMessagesDisplaysNormalizedACPWebImageAndSkill 验证 ACP 标准事实覆盖网页、图片和技能展示。
func TestReplayMessagesDisplaysNormalizedACPWebImageAndSkill(t *testing.T) {
	t.Parallel()

	records := []eventlog.Record{
		chatCreatedRecord(1, "/workspace/project"),
		executionCreatedRecord(2, "execution-1"),
		runtimeRecord(4, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"kind": "search", "name": "web_search", "type": "webSearch",
			"rawInput": map[string]any{
				"type":   "webSearch",
				"action": map[string]any{"type": "search", "query": "ACP protocol"},
			},
			"status": "in_progress", "toolCallId": "web-1",
		}),
		runtimeRecord(5, "execution-1", "execution-1", "invocation-1", "tool_result", map[string]any{
			"status": "completed", "toolCallId": "web-1",
		}),
		runtimeRecord(6, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"kind": "read", "name": "view_image", "type": "imageView",
			"rawInput": map[string]any{"path": "/workspace/project/chart.png"},
			"status":   "completed", "title": "View Image /workspace/project/chart.png",
			"toolCallId": "image-1",
		}),
		runtimeRecord(7, "execution-1", "execution-1", "invocation-1", "tool_result", map[string]any{
			"status": "completed", "toolCallId": "image-1",
		}),
		runtimeRecord(8, "execution-1", "execution-1", "invocation-1", "tool_use", map[string]any{
			"kind":     "other",
			"rawInput": map[string]any{"skill": "diagnosing-bugs"},
			"status":   "in_progress", "title": "Load skill: diagnosing-bugs",
			"toolCallId": "skill-1",
		}),
		runtimeRecord(9, "execution-1", "execution-1", "invocation-1", "tool_result", map[string]any{
			"status": "completed", "toolCallId": "skill-1",
		}),
	}

	messages, err := ReplayMessages(records)
	if err != nil {
		t.Fatalf("ReplayMessages() error = %v", err)
	}
	parts := messages[0].DisplayParts
	if len(parts) != 3 {
		t.Fatalf("len(DisplayParts) = %d, want 3: %#v", len(parts), parts)
	}
	if parts[0].ToolAction != constants.MessageToolActionSearchWeb ||
		parts[0].ToolLabel != "搜索网页 \"ACP protocol\"" ||
		parts[0].ToolStatus != constants.MessageToolStatusCompleted {
		t.Fatalf("web tool view = %#v", parts[0])
	}
	if parts[1].ToolAction != constants.MessageToolActionViewImage ||
		parts[1].ToolLabel != "查看图片 chart.png" ||
		parts[1].ToolStatus != constants.MessageToolStatusCompleted {
		t.Fatalf("image tool view = %#v", parts[1])
	}
	if parts[2].ToolLabel != "使用技能 diagnosing-bugs" ||
		parts[2].ToolCategory != constants.MessageToolCategorySkill ||
		parts[2].ToolAction != constants.MessageToolActionUseSkill ||
		parts[2].ToolStatus != constants.MessageToolStatusCompleted {
		t.Fatalf("skill tool view = %#v", parts[2])
	}
}

// TestReplayMessagesDisplaysMCPStartupFailure 验证连接失败保留失败事实并使用可读连接动作。
func TestReplayMessagesDisplaysMCPStartupFailure(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"name": "mcp__calendar__startup", "kind": "other", "status": "failed",
		"toolCallId": "mcp_startup.calendar", "title": "mcp__calendar__startup",
	}
	messages, err := ReplayMessages([]eventlog.Record{
		chatCreatedRecord(1, "/workspace"),
		executionCreatedRecord(2, "execution"),
		runtimeRecord(3, "execution", "execution", "invocation", "tool_use", data),
		runtimeRecord(4, "execution", "execution", "invocation", "tool_result", data),
	})
	if err != nil {
		t.Fatal(err)
	}
	parts := messages[0].DisplayParts
	if len(parts) != 1 || parts[0].ToolStatus != constants.MessageToolStatusFailed ||
		parts[0].ToolLabel != "连接 MCP 服务 calendar" {
		t.Fatalf("startup failure view = %+v", parts)
	}
}

func messageRecord(sequence int64, eventType constants.ChatEventType, messageID string, executionID string, data eventlog.MessageCreatedData) eventlog.Record {
	encoded, _ := eventlog.MarshalData(data)
	return eventlog.Record{
		ChatID:      "chat-1",
		Sequence:    sequence,
		RecordedAt:  sequence * 100,
		Type:        eventType,
		MessageID:   messageID,
		ExecutionID: executionID,
		Data:        encoded,
	}
}

func chatCreatedRecord(sequence int64, workspaceDirectory string) eventlog.Record {
	encoded, _ := eventlog.MarshalData(eventlog.ChatCreatedData{
		WakerID:            "waker-1",
		WorkspaceDirectory: workspaceDirectory,
		Title:              "测试对话",
	})
	return eventlog.Record{
		ChatID:     "chat-1",
		Sequence:   sequence,
		RecordedAt: sequence * 100,
		Type:       constants.ChatEventTypeCreated,
		Data:       encoded,
	}
}

func executionCreatedRecord(sequence int64, executionID string) eventlog.Record {
	encoded, _ := eventlog.MarshalData(eventlog.ExecutionCreatedData{
		UserMessageID: "user-1",
		RuntimeID:     "codex",
		Model:         "gpt-5",
	})
	return eventlog.Record{
		ChatID:      "chat-1",
		Sequence:    sequence,
		RecordedAt:  sequence * 100,
		Type:        constants.ChatEventTypeExecutionCreated,
		ExecutionID: executionID,
		Data:        encoded,
	}
}

func runtimeRecord(sequence int64, messageID string, executionID string, invocationID string, runtimeType constants.RuntimeEventType, data map[string]any) eventlog.Record {
	encodedData, _ := json.Marshal(data)
	encoded, _ := eventlog.MarshalData(eventlog.RuntimeEventData{
		Type:            runtimeType,
		Data:            encodedData,
		MessageRevision: sequence,
	})
	return eventlog.Record{
		ChatID:       "chat-1",
		Sequence:     sequence,
		RecordedAt:   sequence * 100,
		Type:         constants.ChatEventTypeRuntimeEvent,
		MessageID:    messageID,
		ExecutionID:  executionID,
		InvocationID: invocationID,
		Data:         encoded,
	}
}

func approvalResolvedRecord(
	sequence int64,
	executionID string,
	data eventlog.ApprovalResolvedData,
) eventlog.Record {
	encoded, _ := eventlog.MarshalData(data)
	return eventlog.Record{
		ChatID:      "chat-1",
		Sequence:    sequence,
		RecordedAt:  sequence * 100,
		Type:        constants.ChatEventTypeApprovalResolved,
		ExecutionID: executionID,
		Data:        encoded,
	}
}

func TestCommandFailureUsesNativeExitMetadata(t *testing.T) {
	for _, data := range []map[string]interface{}{
		{"status": "completed", "rawOutput": map[string]interface{}{"metadata": map[string]interface{}{"exit": float64(70)}}},
		{"status": "completed", "rawOutput": map[string]interface{}{"exit_code": float64(2)}},
		{"status": "completed", "_meta": map[string]interface{}{"terminal_exit": map[string]interface{}{"exit_code": float64(1)}}},
	} {
		if displayToolResultStatus(data) != constants.MessageToolStatusFailed {
			t.Fatalf("失败命令被标为成功: %+v", data)
		}
	}
	if displayToolResultStatus(map[string]interface{}{"status": "completed", "rawOutput": map[string]interface{}{"metadata": map[string]interface{}{"exit": float64(0)}}}) != constants.MessageToolStatusCompleted {
		t.Fatal("成功命令被标为失败")
	}
}
