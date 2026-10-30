package eventlog

import (
	"bytes"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"strings"
	"testing"
)

func TestRuntimeStorageCodecKeepsPublicRecordsComplete(t *testing.T) {
	body := strings.Repeat("中文abcdef", 1024)
	inputs := []AppendInput{{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "exec", Data: json.RawMessage(`{}`)}}
	for _, suffix := range []string{"first", "second"} {
		data, _ := json.Marshal(map[string]any{"toolCallId": "tool", "content": body + suffix})
		message, _ := MarshalData(RuntimeMessageData{Content: []RuntimeMessageContent{{Type: constants.RuntimeEventTypeToolUpdate, Data: data}}, MessageRevision: 1})
		inputs = append(inputs, AppendInput{Type: constants.ChatEventTypeToolMessage, InvocationID: "inv", Data: message})
	}
	records, wire, _, err := encodeRecords("chat", 0, 0, recordScope{}, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(wire, []byte(`"patch"`)) {
		t.Fatal("storage did not compact repeated tool body")
	}
	decoded, err := decodeRecordRange("chat", 0, 1, wire)
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index < len(inputs); index++ {
		if !bytes.Equal(records[index].Data, inputs[index].Data) || !bytes.Equal(decoded[index].Data, inputs[index].Data) {
			t.Fatal("public records exposed storage encoding")
		}
	}
}

func TestRuntimeStorageCodecRejectsOverflowingPatch(t *testing.T) {
	base := json.RawMessage(`{"toolCallId":"tool","content":"first"}`)
	data, _ := json.Marshal(storedRuntimeMessage{MessageRevision: 2, Content: []storedRuntimeContent{{Type: constants.RuntimeEventTypeToolUpdate, Patch: &runtimeMessagePatch{ToolCallID: "tool", BaseBytes: len(base), PrefixBytes: int(^uint(0) >> 1), SuffixBytes: int(^uint(0) >> 1)}}}})
	scope := recordScope{toolUpdates: map[string]json.RawMessage{"tool": base}}
	if _, err := scope.decodeRuntimeMessage(data); err == nil {
		t.Fatal("overflowing patch accepted")
	}
}

func TestRuntimeStorageCodecAppendRecoveryAndRange(t *testing.T) {
	directory := t.TempDir()
	store, err := NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	chat := "ababcdcdababcdcdababcdcdababcdcd"
	if _, err = store.Create(chat, []AppendInput{{Type: constants.ChatEventTypeCreated, Data: json.RawMessage(`{}`)}, {Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "exec", Data: json.RawMessage(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	for i := range 4 {
		data, _ := json.Marshal(map[string]any{"toolCallId": "tool", "content": strings.Repeat("内容", 1000+i)})
		message, _ := MarshalData(RuntimeMessageData{MessageRevision: int64(i + 1), Content: []RuntimeMessageContent{{Type: constants.RuntimeEventTypeToolUpdate, Data: data}}})
		if _, err = store.Append(chat, []AppendInput{{Type: constants.ChatEventTypeToolMessage, InvocationID: "inv", Data: message}}); err != nil {
			t.Fatal(err)
		}
		// 新 Store 模拟重启；下一次追加不能引用仅存于旧进程的基准。
		store, err = NewStore(directory)
		if err != nil {
			t.Fatal(err)
		}
	}
	records, err := store.ReadAll(chat)
	if err != nil || len(records) != 6 {
		t.Fatalf("recovered records=%d err=%v", len(records), err)
	}
	window, err := store.ReadAfter(chat, 4, 1)
	if err != nil || len(window) != 1 || !bytes.Equal(window[0].Data, records[4].Data) {
		t.Fatalf("indexed window failed to restore full event: count=%d err=%v", len(window), err)
	}
	for _, record := range records[2:] {
		if bytes.Contains(record.Data, []byte(`"patch"`)) {
			t.Fatal("storage patch escaped recovery boundary")
		}
	}
}
