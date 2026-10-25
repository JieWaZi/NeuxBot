package eventlog

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func TestStoreWritesScopedJSONLAndRestoresRecordContext(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "0123456789abcdef0123456789abcdef"
	executionID := "execution-1"
	invocationID := "invocation-1"
	userMessageID := "user-1"
	records, err := store.Create(chatID, []AppendInput{
		{Type: constants.ChatEventTypeCreated},
		{
			Type:      constants.ChatEventTypeUserMessageCreated,
			MessageID: userMessageID,
			Data: marshalTestData(t, MessageCreatedData{
				Role: constants.MessageRoleUser,
			}),
		},
		{
			Type:        constants.ChatEventTypeExecutionCreated,
			ExecutionID: executionID,
			Data:        marshalTestData(t, ExecutionCreatedData{UserMessageID: userMessageID}),
		},
		{
			Type:         constants.ChatEventTypeExecutionStarted,
			ExecutionID:  executionID,
			InvocationID: invocationID,
		},
		{
			Type:         constants.ChatEventType("assistant.message"),
			MessageID:    executionID,
			ExecutionID:  executionID,
			InvocationID: invocationID,
			Data:         json.RawMessage(`{"content":[]}`),
		},
		{
			Type:         constants.ChatEventTypeInvocationFinished,
			ExecutionID:  executionID,
			InvocationID: invocationID,
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(records) != 6 {
		t.Fatalf("Create() records = %d, want 6", len(records))
	}

	logPath, err := store.LogPath(chatID)
	if err != nil {
		t.Fatalf("LogPath() error = %v", err)
	}
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	for _, forbidden := range [][]byte{
		[]byte(`"chatId"`),
		[]byte(`"sequence"`),
	} {
		if bytes.Contains(content, forbidden) {
			t.Fatalf("scoped JSONL contains %s: %s", forbidden, content)
		}
	}
	if count := bytes.Count(content, []byte(`"executionId"`)); count != 1 {
		t.Fatalf("executionId occurrences = %d, want 1: %s", count, content)
	}
	if count := bytes.Count(content, []byte(`"invocationId"`)); count != 1 {
		t.Fatalf("invocationId occurrences = %d, want 1: %s", count, content)
	}
	if count := bytes.Count(content, []byte(`"messageId"`)); count != 1 {
		t.Fatalf("messageId occurrences = %d, want 1: %s", count, content)
	}

	replayed, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	for index, record := range replayed {
		if record.ChatID != chatID || record.Sequence != int64(index+1) {
			t.Fatalf("ReadAll() record %d = %+v", index, record)
		}
	}
	message := replayed[4]
	if message.ExecutionID != executionID ||
		message.InvocationID != invocationID ||
		message.MessageID != executionID {
		t.Fatalf("scoped assistant message = %+v", message)
	}
}

func TestStoreRejectsTransientRuntimeEvent(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "11223344556677889900aabbccddeeff"
	if _, err := store.Create(chatID, []AppendInput{{
		Type: constants.ChatEventTypeRuntimeEvent,
	}}); err == nil {
		t.Fatal("Create() error = nil, want transient event rejected")
	}
}

func TestStoreRepairsOnlyIncompleteFinalLine(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "0123456789abcdef0123456789abcdef"
	created, err := store.Create(chatID, []AppendInput{{
		Type: constants.ChatEventTypeCreated,
		Data: marshalTestData(t, ChatCreatedData{
			WakerID:            "waker",
			WorkspaceDirectory: "/tmp/project",
			Title:              "title",
		}),
	}})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(created) != 1 || created[0].Sequence != 1 {
		t.Fatalf("Create() records = %#v", created)
	}
	path, err := store.LogPath(chatID)
	if err != nil {
		t.Fatalf("LogPath() error = %v", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	if _, err := file.WriteString(`{"schemaVersion":1`); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("ReadAll() record count = %d, want 1", len(records))
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(content) == 0 || content[len(content)-1] != '\n' {
		t.Fatalf("repaired log does not end with newline: %q", content)
	}
	repair, err := store.RepairInfo(chatID)
	if err != nil {
		t.Fatalf("RepairInfo() error = %v", err)
	}
	if repair.TailBytesRemoved != int64(len(`{"schemaVersion":1`)) ||
		repair.RepairedAt == 0 {
		t.Fatalf("RepairInfo() = %+v", repair)
	}
}

func TestStoreRejectsCorruptionBeforeFinalLine(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "fedcba9876543210fedcba9876543210"
	path, err := store.LogPath(chatID)
	if err != nil {
		t.Fatalf("LogPath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := `{"schemaVersion":1,"chatId":"fedcba9876543210fedcba9876543210","sequence":1,"recordedAt":1,"type":"chat.created","data":{}}
not-json
{"schemaVersion":1,"chatId":"fedcba9876543210fedcba9876543210","sequence":2,"recordedAt":2,"type":"message.user_created","data":{}}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := store.ReadAll(chatID); err == nil {
		t.Fatal("ReadAll() error = nil, want corruption error")
	}
}

func TestStoreReadAfterReplaysOnlyNewerSequences(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "00112233445566778899aabbccddeeff"
	if _, err := store.Create(chatID, []AppendInput{
		{Type: constants.ChatEventTypeCreated},
		{Type: constants.ChatEventTypeUserMessageCreated},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "execution-1"},
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	records, err := store.ReadAfter(chatID, 1, 10)
	if err != nil {
		t.Fatalf("ReadAfter() error = %v", err)
	}
	if len(records) != 2 || records[0].Sequence != 2 || records[1].Sequence != 3 {
		t.Fatalf("ReadAfter() = %#v", records)
	}
}

func TestStoreReadAfterWithoutLimitReturnsCompleteBacklog(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "0123456789abcdef0123456789abcdef"
	inputs := make([]AppendInput, 1005)
	for index := range inputs {
		inputs[index] = AppendInput{Type: constants.ChatEventTypeTitleUpdated}
	}
	if _, err := store.Create(chatID, inputs); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	records, err := store.ReadAfter(chatID, 0, 0)
	if err != nil {
		t.Fatalf("ReadAfter() error = %v", err)
	}
	if len(records) != len(inputs) {
		t.Fatalf("ReadAfter() records = %d, want %d", len(records), len(inputs))
	}
	if records[len(records)-1].Sequence != int64(len(inputs)) {
		t.Fatalf("last sequence = %d, want %d", records[len(records)-1].Sequence, len(inputs))
	}
}

func TestStoreReadAfterRepairsIncompleteTailWhileKeepingBoundedResult(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "102030405060708090a0b0c0d0e0f000"
	if _, err := store.Create(chatID, []AppendInput{
		{Type: constants.ChatEventTypeCreated},
		{Type: constants.ChatEventTypeTitleUpdated},
		{Type: constants.ChatEventTypeTitleUpdated},
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	path, err := store.LogPath(chatID)
	if err != nil {
		t.Fatalf("LogPath() error = %v", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	if _, err := file.WriteString(`{"schemaVersion":1`); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	records, err := store.ReadAfter(chatID, 0, 1)
	if err != nil {
		t.Fatalf("ReadAfter() error = %v", err)
	}
	if len(records) != 1 || records[0].Sequence != 1 {
		t.Fatalf("ReadAfter() = %+v, want only sequence 1", records)
	}
	repair, err := store.RepairInfo(chatID)
	if err != nil {
		t.Fatalf("RepairInfo() error = %v", err)
	}
	if repair.TailBytesRemoved != int64(len(`{"schemaVersion":1`)) {
		t.Fatalf("RepairInfo() = %+v", repair)
	}
	appended, err := store.Append(chatID, []AppendInput{{
		Type: constants.ChatEventTypeTitleUpdated,
	}})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if len(appended) != 1 || appended[0].Sequence != 4 {
		t.Fatalf("Append() = %+v, want sequence 4", appended)
	}
}

func TestReplayInvocationsReturnsPersistedAttemptFacts(t *testing.T) {
	t.Parallel()

	records := []Record{{
		ChatID:       "chat-1",
		Sequence:     12,
		RecordedAt:   1200,
		Type:         constants.ChatEventType("invocation.finished"),
		ExecutionID:  "execution-1",
		InvocationID: "invocation-2",
		Data: json.RawMessage(`{
            "attempt":2,
            "recoveryStrategy":"transcript_reseed",
            "triggerReason":"session_missing",
            "runtimeId":"codex",
            "baseRuntimeId":"codex",
            "binary":"/usr/local/bin/codex",
            "version":"1.2.3",
            "model":"gpt-5",
            "reasoning":"high",
            "permissionLevel":"auto",
            "capabilities":["resume_session","structured_output"],
            "sessionMode":"new",
            "sessionOutputId":"session-2",
            "sessionState":"created",
            "exitCode":0,
            "protocolHealthy":true
        }`),
	}}

	invocations, err := ReplayInvocations(records)
	if err != nil {
		t.Fatalf("ReplayInvocations() error = %v", err)
	}
	if len(invocations) != 1 {
		t.Fatalf("len(ReplayInvocations()) = %d, want 1", len(invocations))
	}
	invocation := invocations[0]
	if invocation.ID != "invocation-2" ||
		invocation.ExecutionID != "execution-1" ||
		invocation.Facts.Attempt != 2 ||
		invocation.Facts.RecoveryStrategy != "transcript_reseed" ||
		invocation.Facts.SessionOutputID != "session-2" ||
		!invocation.Facts.ProtocolHealthy {
		t.Fatalf("ReplayInvocations() = %+v, want persisted invocation facts", invocation)
	}
}

func marshalTestData(t *testing.T, value interface{}) []byte {
	t.Helper()
	data, err := MarshalData(value)
	if err != nil {
		t.Fatalf("MarshalData() error = %v", err)
	}
	return data
}

func TestInvocationTerminationSurvivesStoreReopenAndLegacyStaysUnknown(t *testing.T) {
	directory := t.TempDir()
	store, err := NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	chatID := "78787878787878787878787878787878"
	_, err = store.Create(chatID, []AppendInput{
		{Type: constants.ChatEventTypeCreated},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "execution-1"},
		{Type: constants.ChatEventTypeInvocationFinished, ExecutionID: "execution-1", InvocationID: "old", Data: json.RawMessage(`{"exitCode":0,"protocolHealthy":true}`)},
		{Type: constants.ChatEventTypeInvocationFinished, ExecutionID: "execution-1", InvocationID: "new", Data: json.RawMessage(`{"stopReason":"max_tokens","terminationReason":"truncated","failureMessage":"delivery failed"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	records, err := reopened.ReadAll(chatID)
	if err != nil {
		t.Fatal(err)
	}
	invocations, err := ReplayInvocations(records)
	if err != nil {
		t.Fatal(err)
	}
	if len(invocations) != 2 || string(invocations[0].Facts.TerminationReason) != "unknown" || invocations[0].Facts.StopReason != "" {
		t.Fatalf("legacy completion inferred: %+v", invocations)
	}
	if string(invocations[1].Facts.TerminationReason) != "truncated" || invocations[1].Facts.StopReason != "max_tokens" || invocations[1].Facts.FailureMessage != "delivery failed" {
		t.Fatalf("termination facts lost: %+v", invocations[1])
	}
}
