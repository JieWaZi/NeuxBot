package messageview

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

func TestTicketReportsRemainOrderedAndReplyableAfterNativeReplacement(t *testing.T) {
	builder := NewPublicMessageViewBuilder()
	for _, row := range []eventlog.Record{
		executionCreatedRecord(1, "execution"),
		executionCreatedRecord(2, "other"),
		ticketReportRecord(3, "execution", "progress-1", "Work started"),
		ticketReportRecord(4, "execution", "progress-1", "Work started"),
		ticketReportRecord(5, "execution", "result", "Result is ready"),
		{Sequence: 6, Type: constants.ChatEventTypeExecutionDelivery, MessageID: "execution", ExecutionID: "execution",
			Data: json.RawMessage(`{"sourceId":"workflow","summary":"Workflow delivery"}`)},
	} {
		if _, err := builder.Apply(row); err != nil {
			t.Fatal(err)
		}
	}
	if err := builder.ReplaceStoredContent(agent.StoredMessage{ID: "execution"}, 7, 7); err != nil {
		t.Fatal(err)
	}
	message := builder.PublicMessage("execution")
	if got := message.AssistantResponseText(true); got != "Work started\n\nResult is ready" {
		t.Fatalf("replyable report text = %q", got)
	}
	var partIDs []string
	for _, part := range message.DisplayParts {
		partIDs = append(partIDs, part.ID)
	}
	if want := []string{"execution:delivery", "progress-1", "result"}; !reflect.DeepEqual(partIDs, want) {
		t.Fatalf("parts = %v, want %v", partIDs, want)
	}
	other := builder.PublicMessage("other")
	if other.Content != "" || len(other.DisplayParts) != 0 {
		t.Fatalf("report leaked to another reply: %+v", other)
	}
}

func TestTicketReportProvidesBodyWithoutNativeAssistantText(t *testing.T) {
	messages, err := ReplayPublicMessages([]eventlog.Record{
		executionCreatedRecord(1, "execution"),
		ticketReportRecord(2, "execution", "result", "Result is ready"),
	})
	if err != nil || len(messages) != 1 {
		t.Fatalf("could not replay result: %+v, %v", messages, err)
	}
	if msg := messages[0]; !msg.Readable || msg.Content != "Result is ready" || msg.AssistantResponseText(true) != "Result is ready" {
		t.Fatalf("report-only reply lost its public body: %+v", msg)
	}
}

func ticketReportRecord(sequence int64, execution, receipt, content string) eventlog.Record {
	payload, _ := json.Marshal(map[string]any{"sourceId": receipt, "summary": content, "workTicketReport": true})
	return eventlog.Record{Sequence: sequence, Type: constants.ChatEventTypeExecutionDelivery, MessageID: execution, ExecutionID: execution, Data: payload}
}
