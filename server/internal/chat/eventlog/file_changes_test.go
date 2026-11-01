package eventlog

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

// TestLateFileChangesPreserveScopeAndOlderWindows 覆盖晚到成果与下一轮交错、旧窗口补齐及重启索引恢复。
func TestLateFileChangesPreserveScopeAndOlderWindows(t *testing.T) {
	directory := t.TempDir()
	store, err := NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	id := "12121212121212121212121212121212"
	inputs := []AppendInput{
		{Type: constants.ChatEventTypeCreated, Data: marshalTestData(t, ChatCreatedData{})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "old"},
		{Type: constants.ChatEventTypeAssistantMessage, InvocationID: "old-invocation", Data: json.RawMessage(`{"content":[],"messageRevision":3}`)},
		{Type: constants.ChatEventTypeExecutionCompleted, Data: marshalTestData(t, ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "current"},
		{Type: constants.ChatEventTypeExecutionStarted, InvocationID: "current-invocation"},
		{Type: constants.ChatEventTypeFileChanges, MessageID: "old", ExecutionID: "old", Data: marshalTestData(t, FileChangesData{RecordID: "record", Version: 2, State: "ready", Complete: true})},
		{Type: constants.ChatEventTypeAssistantMessage, Data: json.RawMessage(`{"content":[],"messageRevision":8}`)},
	}
	if _, err = store.Create(id, inputs); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	records, err := restarted.ReadAll(id)
	if err != nil {
		t.Fatal(err)
	}
	last := records[len(records)-1]
	if last.ExecutionID != "current" || last.MessageID != "current" || last.InvocationID != "current-invocation" {
		t.Fatalf("late result overwrote scope: %+v", last)
	}
	window, err := restarted.ReadMessageWindow(id, 5, 1)
	if err != nil {
		t.Fatal(err)
	}
	if window.Records[len(window.Records)-1].Type != constants.ChatEventTypeFileChanges {
		t.Fatalf("old window lost late result: %+v", window)
	}
	reference, err := restarted.ReadFileChangesReference(id, "old")
	if err != nil {
		t.Fatal(err)
	}
	if len(reference) != 2 || reference[1].Type != constants.ChatEventTypeFileChanges {
		t.Fatalf("reference loaded bodies: %+v", reference)
	}
	for _, record := range reference {
		if strings.Contains(string(record.Data), "messageRevision") {
			t.Fatal("reference loaded tool/body")
		}
	}
	if _, err := restarted.Append(id, []AppendInput{{Type: constants.ChatEventTypeFileChanges, MessageID: "old", ExecutionID: "old", InvocationID: "must-not-change-scope", Data: marshalTestData(t, FileChangesData{RecordID: "record", Version: 3})}}); err == nil {
		t.Fatal("result accepted an invocation identity outside its scope")
	}
	added, err := restarted.Append(id, []AppendInput{{Type: constants.ChatEventTypeInvocationFinished}})
	if err != nil {
		t.Fatal(err)
	}
	if added[0].ExecutionID != "current" || added[0].InvocationID != "current-invocation" {
		t.Fatalf("writer scope corrupted: %+v", added)
	}
}
