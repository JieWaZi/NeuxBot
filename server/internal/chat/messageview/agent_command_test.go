package messageview

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

func TestBusinessCaptionRequiresSameExecutionCoreFactAndDoesNotAddRows(t *testing.T) {
	for _, source := range []string{"missing", "other-execution", "current-execution", "retired-operation", "old-completion"} {
		t.Run(source, func(t *testing.T) {
			builder := NewMessageViewBuilder()
			for _, record := range []eventlog.Record{chatCreatedRecord(1, "/workspace"), executionCreatedRecord(2, "execution")} {
				if _, err := builder.Apply(record); err != nil {
					t.Fatal(err)
				}
			}
			operation, completion := "workflow.source.save", constants.AgentCLICompletionImmediate
			switch source {
			case "retired-operation":
				operation = "routine.rules.sources"
			case "old-completion":
				completion = constants.AgentCLICompletionAccepted
			}
			if source != "missing" {
				owner := "execution"
				if source == "other-execution" {
					owner = "other"
				}
				data, _ := json.Marshal(eventlog.AgentCommandData{CommandID: "receipt", Operation: operation, Completion: completion})
				if _, err := builder.Apply(eventlog.Record{Sequence: 3, ExecutionID: owner, Type: constants.ChatEventTypeAgentCommandCommitted, Data: data}); err != nil {
					t.Fatal(err)
				}
			}
			output, _ := json.Marshal(map[string]any{
				"protocolVersion": constants.AgentCLIProtocolVersion, "ok": true, "commandId": "receipt", "operation": operation,
				"data": map[string]string{"private": "must not appear in caption"},
			})
			for index, tool := range []string{"first", "replay"} {
				if _, err := builder.Apply(runtimeRecord(int64(4+index*2), "execution", "execution", "invoke", constants.RuntimeEventTypeToolUse, map[string]any{"id": tool, "name": "bash", "input": map[string]any{"command": "botcli workflow source save --input source.json"}})); err != nil {
					t.Fatal(err)
				}
				if _, err := builder.Apply(runtimeRecord(int64(5+index*2), "execution", "execution", "invoke", constants.RuntimeEventTypeToolResult, map[string]any{"tool_use_id": tool, "output": string(output), "is_error": false})); err != nil {
					t.Fatal(err)
				}
			}
			messages := builder.Messages()
			if len(messages) != 1 || len(messages[0].DisplayParts) != 2 {
				t.Fatalf("command facts added duplicate rows: %+v", messages)
			}
			count := 0
			for _, part := range messages[0].DisplayParts {
				if part.ToolLabel == "保存工作流" {
					count++
				}
			}
			wanted := 0
			if source == "current-execution" {
				wanted = 1
			}
			if count != wanted {
				t.Fatalf("untrusted or duplicate business caption: %d want %d: %+v", count, wanted, messages[0].DisplayParts)
			}
		})
	}
}

func TestAgentCommandReplayRejectsInvalidReceipt(t *testing.T) {
	for _, data := range []string{
		`{"commandId":"receipt","operation":"","completion":"immediate"}`,
		`{"commandId":"receipt","operation":"routine.rules.sources","completion":""}`,
	} {
		builder := NewMessageViewBuilder()
		_, err := builder.Apply(eventlog.Record{
			Sequence: 1, ExecutionID: "execution", Type: constants.ChatEventTypeAgentCommandCommitted, Data: json.RawMessage(data),
		})
		if !errors.Is(err, status_error.ChatLogCorrupted) {
			t.Errorf("invalid command receipt %s error = %v", data, err)
		}
	}
}
