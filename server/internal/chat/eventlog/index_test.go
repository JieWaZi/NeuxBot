package eventlog

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

// TestWarmIndexReadDetectsExternalActiveLogCorruption 保留热读对外部修改的活动执行正文校验。
func TestWarmIndexReadDetectsExternalActiveLogCorruption(t *testing.T) {
	store, chatID, latest := activeExecutionStore(t, 8)
	records, err := store.ReadAll(chatID)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := store.LogPath(chatID)
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte{'['}, records[4].Offset); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadAfter(chatID, latest, 1); err == nil {
		t.Fatal("hot read accepted corrupted active execution after external log change")
	}
}

// TestWarmIndexReadRestoresExternallyChangedScope 验证文件版本变化后重新恢复作用域，后续追加不沿用旧调用身份。
func TestWarmIndexReadRestoresExternallyChangedScope(t *testing.T) {
	store, chatID, latest := activeExecutionStore(t, 8)
	path, _ := store.LogPath(chatID)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = bytes.ReplaceAll(content, []byte(`"invocation"`), []byte(`"externally"`))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadAfter(chatID, latest, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(chatID, []AppendInput{{Type: constants.ChatEventTypeAssistantMessage, ExecutionID: "active", InvocationID: "externally", Data: marshalTestData(t, RuntimeMessageData{MessageRevision: latest + 1})}}); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(content), []byte{'\n'})
	if bytes.Contains(lines[len(lines)-1], []byte("invocation")) {
		t.Fatalf("append rewrote invocation identity instead of using recovered scope: %s", lines[len(lines)-1])
	}
}

func TestStoreMaintainsIndexAndPagesMessagesWithoutSQLite(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "abababababababababababababababab"
	records, err := store.Create(chatID, twoTurnIndexInputs(t))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	indexPath, err := store.IndexPath(chatID)
	if err != nil {
		t.Fatalf("IndexPath() error = %v", err)
	}
	indexInfo, err := os.Stat(indexPath)
	if err != nil {
		t.Fatalf("Stat(index) error = %v", err)
	}
	wantIndexSize := indexHeaderSize + int64(len(records))*indexEntrySize
	if indexInfo.Size() != wantIndexSize {
		t.Fatalf("index size = %d, want %d", indexInfo.Size(), wantIndexSize)
	}

	latest, err := store.ReadMessageWindow(chatID, 0, 2)
	if err != nil {
		t.Fatalf("ReadMessageWindow(latest) error = %v", err)
	}
	if !latest.HasMore || latest.NextBeforeSequence != 7 ||
		latest.LatestSequence != int64(len(records)) {
		t.Fatalf("latest window = %+v", latest)
	}
	if starts := messageStartSequences(latest.Records); len(starts) != 2 ||
		starts[0] != 7 || starts[1] != 8 {
		t.Fatalf("latest message starts = %v, want [7 8]", starts)
	}

	older, err := store.ReadMessageWindow(chatID, latest.NextBeforeSequence, 2)
	if err != nil {
		t.Fatalf("ReadMessageWindow(older) error = %v", err)
	}
	if older.HasMore {
		t.Fatalf("older.HasMore = true, want false")
	}
	if starts := messageStartSequences(older.Records); len(starts) != 2 ||
		starts[0] != 2 || starts[1] != 3 {
		t.Fatalf("older message starts = %v, want [2 3]", starts)
	}
}

func TestReadProjectionAfterUsesOnlyCurrentExecutionPrefix(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
	records, err := store.Create(chatID, twoTurnIndexInputs(t))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	window, err := store.ReadProjectionAfter(chatID, 9, 0)
	if err != nil {
		t.Fatalf("ReadProjectionAfter() error = %v", err)
	}
	sequences := recordSequences(window.Records)
	if len(sequences) != 5 || sequences[0] != 1 || sequences[1] != 8 ||
		sequences[2] != 9 || sequences[3] != 10 || sequences[4] != 11 {
		t.Fatalf("projection sequences = %v, want [1 8 9 10 11]", sequences)
	}
	if window.LatestSequence != records[len(records)-1].Sequence {
		t.Fatalf("LatestSequence = %d, want %d", window.LatestSequence, records[len(records)-1].Sequence)
	}

	finished, err := store.ReadProjectionAfter(chatID, records[len(records)-1].Sequence, 0)
	if err != nil {
		t.Fatalf("ReadProjectionAfter(finished) error = %v", err)
	}
	if sequences := recordSequences(finished.Records); len(sequences) != 1 || sequences[0] != 1 {
		t.Fatalf("finished projection sequences = %v, want only Chat scope", sequences)
	}
}

func TestRecoverIndexRebuildsMissingAndLaggingDerivedFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "efefefefefefefefefefefefefefefef"
	records, err := store.Create(chatID, twoTurnIndexInputs(t))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	indexPath, err := store.IndexPath(chatID)
	if err != nil {
		t.Fatalf("IndexPath() error = %v", err)
	}
	if err := os.Remove(indexPath); err != nil {
		t.Fatalf("Remove(index) error = %v", err)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore(reopen) error = %v", err)
	}
	recovery, err := reopened.RecoverIndex(chatID)
	if err != nil {
		t.Fatalf("RecoverIndex(missing) error = %v", err)
	}
	if !recovery.Rebuilt || recovery.LatestSequence != int64(len(records)) {
		t.Fatalf("missing index recovery = %+v", recovery)
	}
	info, err := os.Stat(indexPath)
	if err != nil {
		t.Fatalf("Stat(rebuilt index) error = %v", err)
	}
	if err := os.Truncate(indexPath, info.Size()-indexEntrySize); err != nil {
		t.Fatalf("Truncate(index) error = %v", err)
	}

	reopenedAgain, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore(second reopen) error = %v", err)
	}
	recovery, err = reopenedAgain.RecoverIndex(chatID)
	if err != nil {
		t.Fatalf("RecoverIndex(lagging) error = %v", err)
	}
	if !recovery.Rebuilt || recovery.LatestSequence != int64(len(records)) {
		t.Fatalf("lagging index recovery = %+v", recovery)
	}
}

func TestIndexedReadRebuildsCorruptMiddleEntry(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "34343434343434343434343434343434"
	if _, err := store.Create(chatID, twoTurnIndexInputs(t)); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	indexPath, err := store.IndexPath(chatID)
	if err != nil {
		t.Fatalf("IndexPath() error = %v", err)
	}
	file, err := os.OpenFile(indexPath, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("OpenFile(index) error = %v", err)
	}
	corruptOffset := indexEntryOffset(7) + 48
	byteAtOffset := []byte{0}
	if _, err := file.ReadAt(byteAtOffset, corruptOffset); err != nil {
		_ = file.Close()
		t.Fatalf("ReadAt(index) error = %v", err)
	}
	byteAtOffset[0] ^= 0xff
	if _, err := file.WriteAt(byteAtOffset, corruptOffset); err != nil {
		_ = file.Close()
		t.Fatalf("WriteAt(index) error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close(index) error = %v", err)
	}

	window, err := store.ReadMessageWindow(chatID, 7, 2)
	if err != nil {
		t.Fatalf("ReadMessageWindow() error = %v", err)
	}
	if starts := messageStartSequences(window.Records); len(starts) != 2 ||
		starts[0] != 2 || starts[1] != 3 {
		t.Fatalf("rebuilt window starts = %v, want [2 3]", starts)
	}
}

func TestIndexSupportsMessageLookupAndExactActivityTimes(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "12121212121212121212121212121212"
	records, err := store.Create(chatID, twoTurnIndexInputs(t))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	message, err := store.ReadMessage(chatID, "execution-2")
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}
	if sequences := recordSequences(message); len(sequences) != 5 ||
		sequences[0] != 1 || sequences[1] != 8 || sequences[4] != 11 {
		t.Fatalf("message sequences = %v, want Chat scope plus second assistant", sequences)
	}
	start := records[0].RecordedAt - time.Second.Milliseconds()
	end := records[len(records)-1].RecordedAt + time.Second.Milliseconds()
	times, err := store.MessageRecordedTimes(chatID, start, end)
	if err != nil {
		t.Fatalf("MessageRecordedTimes() error = %v", err)
	}
	if len(times) != 4 {
		t.Fatalf("message activity count = %d, want 4", len(times))
	}
}

func twoTurnIndexInputs(t *testing.T) []AppendInput {
	t.Helper()
	return []AppendInput{
		{Type: constants.ChatEventTypeCreated},
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "user-1"},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "execution-1"},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "execution-1"},
		{Type: constants.ChatEventTypeAssistantMessage, ExecutionID: "execution-1", Data: marshalTestData(t, RuntimeMessageData{MessageRevision: 5})},
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "execution-1", Data: marshalTestData(t, ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "user-2"},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "execution-2"},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "execution-2"},
		{Type: constants.ChatEventTypeAssistantMessage, ExecutionID: "execution-2", Data: marshalTestData(t, RuntimeMessageData{MessageRevision: 10})},
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "execution-2", Data: marshalTestData(t, ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
	}
}

func messageStartSequences(records []Record) []int64 {
	sequences := make([]int64, 0)
	for _, record := range records {
		if record.Type == constants.ChatEventTypeUserMessageCreated ||
			record.Type == constants.ChatEventTypeExecutionCreated {
			sequences = append(sequences, record.Sequence)
		}
	}
	return sequences
}

func recordSequences(records []Record) []int64 {
	sequences := make([]int64, 0, len(records))
	for _, record := range records {
		sequences = append(sequences, record.Sequence)
	}
	return sequences
}

func TestCountUserMessagesExcludesReportsAndRecoversIndex(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatID := "acacacacacacacacacacacacacacacac"
	inputs := twoTurnIndexInputs(t)
	for i := range inputs {
		if inputs[i].Type == constants.ChatEventTypeUserMessageCreated {
			inputs[i].Data = marshalTestData(t, MessageCreatedData{Role: constants.MessageRoleUser})
		}
	}
	inputs = append(inputs,
		AppendInput{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "report", Data: marshalTestData(t, MessageCreatedData{Role: constants.MessageRoleUser, SourceWakerID: "member", Actor: "member_report"})},
		AppendInput{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "coordination", Data: marshalTestData(t, MessageCreatedData{Role: constants.MessageRoleUser, Actor: "system"})},
		AppendInput{Type: constants.ChatEventTypeMessageInherited, MessageID: "inherited", Data: marshalTestData(t, MessageCreatedData{Role: constants.MessageRoleUser})},
	)
	if _, err := store.Create(chatID, inputs); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		count, err := store.CountUserMessages(chatID)
		if err != nil || count != 2 {
			t.Fatalf("count = %d, err = %v; want 2", count, err)
		}
		indexPath, err := store.IndexPath(chatID)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(indexPath); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Append(chatID, []AppendInput{{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "user-3", Data: marshalTestData(t, MessageCreatedData{Role: constants.MessageRoleUser, Actor: "user"})}}); err != nil {
		t.Fatal(err)
	}
	count, err := store.CountUserMessages(chatID)
	if err != nil || count != 3 {
		t.Fatalf("count after append = %d, err = %v; want 3", count, err)
	}
}
