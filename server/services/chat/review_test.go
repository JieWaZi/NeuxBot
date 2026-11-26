package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversationfixture"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/memory/reviewstate"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestReviewEvidenceKeepsOnlyFinalResponse(t *testing.T) {
	journal := newTestConversationJournal(t, t.TempDir())
	const chatID = "12121212121212121212121212121212"
	const executionID = "execution"
	changes := conversationfixture.Facts([]eventlog.AppendInput{
		{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: "partner"})},
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "user", Data: marshalData(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Content: "检查报告"})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: executionID, Data: json.RawMessage(`{"entryPoint":"workbench","userMessageId":"user","review":{"configuration":"frozen","skill":1}}`)},
	})
	changes = append(changes, conversationfixture.NativeMessage(t, journal.Store, chatID, executionID,
		agent.MessageChange{Kind: agent.MessageAppend, ContentType: constants.RuntimeEventTypeTextDelta, Text: "开始查找文件"},
		agent.MessageChange{Kind: agent.MessageEvent, ContentType: constants.RuntimeEventTypeToolResult, ToolCallID: "check",
			Data: json.RawMessage(`{"toolName":"Bash","status":"completed"}`)},
		agent.MessageChange{Kind: agent.MessageAppend, ContentType: constants.RuntimeEventTypeTextDelta, Text: "报告核对完成"},
	))
	changes = append(changes, conversationfixture.Facts([]eventlog.AppendInput{
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: executionID, Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
	})...)
	if _, err := conversationfixture.Commit(t.Context(), journal.Store, chatID, changes); err != nil {
		t.Fatal(err)
	}
	chat := &service{logStore: journal}
	turns, err := chat.ReadReviewTurns(t.Context(), chatID, 0, 0, reviewstate.Admission{Skill: 1}, 1)
	if err != nil || len(turns) != 1 || turns[0].Assistant != "报告核对完成" || len(turns[0].Steps) != 1 {
		t.Fatalf("复盘证据 = %+v，错误 = %v", turns, err)
	}
}

func TestManagedResourceChangesPreserveDurableTimelinePositionOnRetry(t *testing.T) {
	t.Parallel()
	database, chat := newRecoveryTestService(t, t.TempDir())
	commands := controllers.NewAgentCommandController(database)
	chat.managedCommands = commands
	const chatID = "12121212121212121212121212121212"
	workspace := t.TempDir()
	if _, err := chat.CreateDormantChats(t.Context(), []DormantChat{{Chat: models.Chat{
		ID: chatID, WakerID: "waker", WorkspaceDirectory: workspace, AdditionalWorkingDirectories: []string{workspace},
	}}}); err != nil {
		t.Fatal(err)
	}
	records, err := chat.AppendExecutionFacts(t.Context(), chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "request", Data: marshalData(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Content: "Revise a skill."})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "execution", Data: marshalData(eventlog.ExecutionCreatedData{UserMessageID: "request"})},
	})
	if err != nil {
		t.Fatal(err)
	}
	committed := func(operation string, data map[string]any, live bool) capability.Result {
		t.Helper()
		ctx := t.Context()
		request := capability.Request{Operation: operation, RequestID: "original"}
		call := func(ctx context.Context, request capability.Request) (capability.Result, error) {
			return capability.Execute(ctx, commands, "execution", request, func(ctx context.Context) error {
				return chat.queryIndexes.transactionController.Transaction(ctx, func(ctx context.Context) error {
					return capability.Commit(ctx, data)
				})
			})
		}
		if live {
			binding := capability.Binding{Operation: operation, Call: call, Receipt: func(ctx context.Context, id string) (capability.Result, error) {
				return capability.Lookup(ctx, commands, "execution", operation, id)
			}}
			call = chat.BindManagedResourceChanges(models.Chat{ID: chatID, WakerID: "waker"}, "execution", []capability.Binding{binding})[0].Call
		} else {
			// The domain commit can survive before the process appends the notice.
			ctx = capability.WithReceiptOrigin(ctx, capability.ReceiptOrigin{ChatID: chatID, ExecutionID: "execution", WakerID: "waker", Operation: operation}, nil)
		}
		result, err := call(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	for range 2 {
		committed("skills.packages.revise", map[string]any{"skillId": "skill", "versionId": "version", "name": "Release notes"}, true)
	}
	pending, err := commands.ListPendingManagedChanges(t.Context(), chatID, "", capability.Receipt{}, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("live command did not finish projection: %+v %v", pending, err)
	}
	changes, err := chat.ReadManagedResourceChanges(t.Context(), chatID, 0)
	if err != nil || len(changes) != 1 || changes[0].Sequence != records[len(records)-1].Sequence+1 ||
		changes[0].OccurredAt == 0 || changes[0].Action != "revise" || changes[0].SkillID != "skill" ||
		changes[0].VersionID != "version" || changes[0].Name != "Release notes" {
		t.Fatalf("unexpected durable skill changes: %+v %v", changes, err)
	}
	events, err := chat.ListEvents(t.Context(), chatID, records[len(records)-1].Sequence, 1)
	if err != nil || len(events) != 1 || events[0].Type != constants.ChatEventTypeManagedSkillChanged ||
		events[0].Sequence != changes[0].Sequence || events[0].Message != nil {
		t.Fatalf("unexpected skill change subscription event: %+v %v", events, err)
	}
	committed("mcp.connectors.create", map[string]any{"connectorId": "connector", "scope": "private", "name": "Test MCP"}, false)
	pending, err = commands.ListPendingManagedChanges(t.Context(), chatID, "", capability.Receipt{}, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("the projection gap was not stored on the source command: %+v %v", pending, err)
	}
	changes, err = chat.ReadManagedResourceChanges(t.Context(), chatID, 0)
	if err != nil || len(changes) != 2 || changes[1].Sequence != changes[0].Sequence+1 ||
		changes[1].ConnectorID != "connector" || changes[1].OwnerWakerID != "waker" || changes[1].Name != "Test MCP" {
		t.Fatalf("unexpected durable connector changes: %+v %v", changes, err)
	}
	window, err := chat.ReadManagedResourceChanges(t.Context(), chatID, changes[1].Sequence)
	if err != nil || len(window) != 1 || window[0].ConnectorID != "connector" {
		t.Fatalf("unexpected resource changes in message window: %+v %v", window, err)
	}
	events, err = chat.ListEvents(t.Context(), chatID, changes[0].Sequence, 1)
	if err != nil || len(events) != 1 || events[0].Type != constants.ChatEventTypeManagedConnectorChanged || events[0].Sequence != changes[1].Sequence {
		t.Fatalf("unexpected connector change subscription event: %+v %v", events, err)
	}
	result := committed("skills.packages.uninstall", map[string]any{"skillId": "skill", "versionId": "version", "name": "Release notes"}, false)
	receipt, found, err := commands.Find(t.Context(), result.CommandID)
	if err != nil || !found {
		t.Fatalf("missing uninstall receipt: %+v %v", receipt, err)
	}
	kind, data, err := managedResourceChangeOf(receipt)
	if err != nil {
		t.Fatal(err)
	}
	// The log append may also survive before the database receives its acknowledgement.
	if err := chat.recordManagedResourceChange(t.Context(), chatID, "execution", kind, "managed-resource-command:"+receipt.ID, data); err != nil {
		t.Fatal(err)
	}
	if err := chat.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	pending, err = commands.ListPendingManagedChanges(t.Context(), chatID, "", capability.Receipt{}, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("startup did not recover the command receipt: %+v %v", pending, err)
	}
	changes, err = chat.ReadManagedResourceChanges(t.Context(), chatID, 0)
	if err != nil || len(changes) != 3 || changes[2].Action != "uninstall" {
		t.Fatalf("startup lost the resource change: %+v %v", changes, err)
	}
}

func TestReviewToolEvidenceUsesOnlyStructuredDiagnostics(t *testing.T) {
	msg := messageview.Message{DisplayParts: []messageview.MessageDisplayPart{
		{ID: "failure", ToolCallID: "native-1", ToolCategory: "command", ToolAction: "execute_command", ToolStatus: constants.MessageToolStatusFailed},
		{ID: "recovery", ToolCallID: "native-2", ToolCategory: "command", ToolAction: "run_tests", ToolStatus: constants.MessageToolStatusCompleted},
	}}
	parts := []agent.MessagePart{
		{ToolCallID: "native-1", Data: json.RawMessage(`{"toolName":"Bash","exitCode":127,"output":"private output","input":{"command":"private command"}}`)},
		{ToolCallID: "native-2", Data: json.RawMessage(`{"toolName":"RunTests"}`)},
	}
	steps := reviewToolSteps(msg, parts)
	if len(steps) != 2 || !steps[0].Failed || !strings.Contains(steps[0].Name, "Bash") || steps[0].Summary != "Exit code 127" || steps[1].Failed {
		t.Fatalf("unexpected safe tool evidence: %+v", steps)
	}
	body, err := json.Marshal(steps)
	if err != nil || strings.Contains(string(body), "private") {
		t.Fatalf("unsafe tool evidence: %s, %v", body, err)
	}
}

func TestAutomaticTaskCreationReceiptRestoresOneCard(t *testing.T) {
	for _, recovery := range []string{"startup", "receipt lookup", "command retry", "timeline refresh"} {
		t.Run(recovery, func(t *testing.T) { assertAutomaticTaskCreationRecovery(t, recovery) })
	}
}

func assertAutomaticTaskCreationRecovery(t *testing.T, recovery string) {
	ctx := t.Context()
	database, service := newRecoveryTestService(t, t.TempDir())
	commands := controllers.NewAgentCommandController(database)
	service.managedCommands = commands
	const chatID = "12121212121212121212121212121212"
	const executionID = "execution"
	workspace := t.TempDir()
	if _, err := service.CreateDormantChats(ctx, []DormantChat{{Chat: models.Chat{
		ID: chatID, WakerID: "partner", WorkspaceDirectory: workspace, AdditionalWorkingDirectories: []string{workspace},
	}}}); err != nil {
		t.Fatal(err)
	}
	rows, err := service.AppendExecutionFacts(ctx, chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "request", Data: marshalData(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Content: "Schedule a report."})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: executionID, Data: marshalData(eventlog.ExecutionCreatedData{UserMessageID: "request"})},
	})
	if err != nil {
		t.Fatal(err)
	}
	const operation = "automatic.tasks.create"
	request := capability.Request{Operation: operation, RequestID: "report"}
	origin := capability.ReceiptOrigin{ChatID: chatID, ExecutionID: executionID, WakerID: "partner", Operation: operation}
	binding := capability.Binding{
		Operation: operation,
		Call: func(ctx context.Context, request capability.Request) (capability.Result, error) {
			return capability.Execute(ctx, commands, executionID, request, func(ctx context.Context) error {
				return capability.Commit(ctx, map[string]any{
					"taskId": "task", "name": "Weekly report", "wakerName": "Analyst", "instructionPreview": "Summarize release notes",
					"schedule":   map[string]any{"kind": "weekly", "weekday": 5, "hour": 9, "minute": 0, "timezone": "Asia/Shanghai"},
					"firstRunAt": int64(1795741200000),
				})
			})
		},
		Receipt: func(ctx context.Context, id string) (capability.Result, error) {
			return capability.Lookup(ctx, commands, executionID, operation, id)
		},
	}
	if _, err := binding.Call(capability.WithReceiptOrigin(ctx, origin, nil), request); err != nil {
		t.Fatal(err)
	}
	if recovery == "timeline refresh" {
		if err := commands.Create(ctx, capability.Receipt{
			ID: "unrelated-skill", Origin: capability.ReceiptOrigin{ChatID: chatID, ExecutionID: executionID, WakerID: origin.WakerID, Operation: "skills.packages.revise"},
			Data: map[string]any{"name": "Skill", "skillId": "skill", "versionId": "version"},
		}); err != nil {
			t.Fatal(err)
		}
		page, err := service.ReadMessageTimeline(ctx, chatID, 0, 0, 10)
		if err != nil || len(page.Resources) != 2 || len(page.Resources[1].AutomaticTaskCreations) != 0 {
			t.Fatalf("shared timeline read should not restore command receipts: %+v, %v", page, err)
		}
	}
	bound := service.BindManagedResourceChanges(models.Chat{ID: chatID, WakerID: "partner"}, executionID, []capability.Binding{binding})[0]
	for range 2 {
		switch recovery {
		case "startup":
			service.recoverManagedResourceChanges(ctx, chatID)
		case "receipt lookup":
			if _, err := bound.Receipt(ctx, request.RequestID); err != nil {
				t.Fatal(err)
			}
		case "command retry":
			if _, err := bound.Call(ctx, request); err != nil {
				t.Fatal(err)
			}
		case "timeline refresh":
			service.RestoreAutomaticTaskCreationCards(ctx, chatID)
			page, err := service.ReadMessageTimeline(ctx, chatID, 0, 0, 10)
			if err != nil || len(page.Resources) != 2 || len(page.Resources[1].AutomaticTaskCreations) != 1 {
				t.Fatalf("refresh should return the recovered creation card: %+v, %v", page, err)
			}
		}
	}
	events, err := service.ListEvents(ctx, chatID, rows[len(rows)-1].Sequence, 10)
	if err != nil || len(events) != 1 || events[0].Type != constants.ChatEventTypeExecutionDelivery || events[0].Message == nil {
		t.Fatalf("expected one committed card event: %+v, %v", events, err)
	}
	page, err := service.ReadMessageHistory(ctx, chatID, 0, 10)
	if err != nil || len(page.Resources) != 2 {
		t.Fatalf("invalid history: %+v, %v", page, err)
	}
	message, err := json.Marshal(page.Resources[1])
	if err != nil || !strings.Contains(string(message), `"automaticTaskCreations":[{"taskId":"task"`) ||
		!strings.Contains(string(message), `"instructionPreview":"Summarize release notes"`) {
		t.Fatalf("creation card absent from history: %s, %v", message, err)
	}
}
