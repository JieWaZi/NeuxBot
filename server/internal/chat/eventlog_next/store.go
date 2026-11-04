package eventlog_next

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
)

const streamShards = 64

// Store is the sole writer and reader for a managed root. Callers share this instance.
type Store struct {
	// root is the application-owned managed file directory.
	root string
	// catalog preserves the ordinary and team Chat path layout.
	catalog *chatstorage.Catalog
	// reverseMu serializes first-use recovery for inverse input queries.
	reverseMu sync.Mutex
	// reverseReady marks initial reverse projection validation.
	reverseReady bool
	// reverseEpoch ties reverse readiness to the currently opened database.
	reverseEpoch uint64
	// indexes owns the disposable positioning database.
	indexes indexDB
	// chats serializes public logs by shard.
	chats [streamShards]streamSlot
	// natives serializes the independent native streams.
	natives [streamShards]streamSlot
}

var _ agent.ConversationStore = (*Store)(nil)

func NewStore(root string) (*Store, error) {
	layout, err := chatstorage.NewLayout(root)
	if err != nil {
		return nil, err
	}
	catalog, err := chatstorage.NewCatalog(layout)
	if err != nil {
		return nil, err
	}
	s := &Store{root: layout.Root(), catalog: catalog}
	s.indexes.path = filepath.Join(s.root, indexFilename)
	for i := range s.chats {
		s.chats[i].indexes, s.natives[i].indexes = &s.indexes, &s.indexes
	}
	return s, nil
}

// RegisterTeamChat fixes the log address before the first committed Chat change.
func (s *Store) RegisterTeamChat(threadID, chatID string) error {
	slot := s.chatSlot(chatID)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	_, err := s.catalog.RegisterTeamChat(threadID, chatID)
	return err
}

func (s *Store) LogPath(chatID string) (string, error) {
	address, err := s.catalog.Address(chatID)
	return address.LogPath, err
}

func (s *Store) IndexPath(chatID string) (string, error) {
	if _, err := s.catalog.Address(chatID); err != nil {
		return "", err
	}
	return s.indexes.path, nil
}

func (s *Store) FileChangesDirectory(chatID string) (string, error) {
	path, err := s.LogPath(chatID)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "file-results", chatID), nil
}

func (s *Store) chatSlot(id string) *streamSlot {
	return &s.chats[sha256.Sum256([]byte(id))[0]%streamShards]
}

// Commit persists admission, binding and other Chat changes as one CAS operation.
func (s *Store) Commit(ctx context.Context, batch agent.ConversationBatch) (agent.CommitReceipt, error) {
	if !validID(batch.Key) || batch.ExpectedVersion < 0 || len(batch.Changes) == 0 || len(batch.Changes) > maxBatchRows {
		return agent.CommitReceipt{}, status_error.ChatLogRecordInvalid
	}
	rows := make([]diskRow, 0, len(batch.Changes))
	for i := range batch.Changes {
		row := diskRow{Chat: &batch.Changes[i]}
		if !validDiskRow(row, true) {
			return agent.CommitReceipt{}, status_error.ChatLogRecordInvalid
		}
		rows = append(rows, row)
	}
	raw, hash, err := marshalRows(rows)
	if err != nil {
		return agent.CommitReceipt{}, err
	}
	slot := s.chatSlot(batch.ChatID)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	path, err := s.LogPath(batch.ChatID)
	if err != nil {
		return agent.CommitReceipt{}, err
	}
	idx, err := slot.load(ctx, path, true)
	if err != nil {
		return agent.CommitReceipt{}, err
	}
	old, exists, err := idx.batch(path, batch.Key)
	if err != nil {
		return agent.CommitReceipt{}, err
	}
	if exists {
		if old.Request != hash {
			return agent.CommitReceipt{}, status_error.ChatLogIdempotencyConflict
		}
		return old.Receipt, nil
	}
	if batch.ExpectedVersion != idx.version() {
		return agent.CommitReceipt{}, status_error.ChatLogVersionConflict
	}
	if err := s.validateChatBatch(ctx, path, idx, batch.Changes); err != nil {
		return agent.CommitReceipt{}, err
	}
	header := makeHeader(batch.Key, hash, idx.version(), len(rows))
	if err := slot.append(ctx, path, idx, header, rows, raw); err != nil {
		return agent.CommitReceipt{}, err
	}
	return header.Receipt, nil
}

func makeHeader(key string, hash [32]byte, before int64, rows int) batchHeader {
	return batchHeader{Key: key, Request: hash, Receipt: agent.CommitReceipt{
		First: before + 1, Version: before + int64(rows), RecordedAt: time.Now().UnixMilli(),
	}}
}

func (s *Store) validateChatBatch(ctx context.Context, path string, idx *streamIndex, changes []agent.ConversationChange) error {
	var file *os.File
	if idx.version() > 0 {
		var err error
		file, err = os.Open(path)
		if err != nil {
			return status_error.ChatLogError
		}
		defer file.Close()
	}
	var binding *agent.NativeBinding
	needsBinding := slices.ContainsFunc(changes, func(change agent.ConversationChange) bool {
		return change.Binding != nil || change.Input != nil
	})
	if needsBinding {
		pos, err := idx.bindingPosition()
		if err != nil {
			return err
		}
		if pos.Cursor > 0 {
			row, err := readDiskRow(file, pos.Where)
			if err != nil || row.Chat == nil || row.Chat.Binding == nil {
				return status_error.ChatLogCorrupted
			}
			binding = row.Chat.Binding
		}
	}
	created := idx.version() > 0
	inputs := make(map[string]*agent.HostInput)
	consumed := make(map[string]bool)
	for _, change := range changes {
		if !created && (change.Fact == nil || change.Fact.Type != constants.ChatEventTypeCreated) {
			return status_error.ChatLogRecordInvalid
		}
		switch {
		case change.Fact != nil && change.Fact.Type == constants.ChatEventTypeCreated:
			if created {
				return status_error.ChatLogRecordInvalid
			}
			created = true
		case change.Binding != nil:
			if change.Binding.Cleared && (binding == nil || binding.Cleared || binding.Session != change.Binding.Session) {
				return status_error.ChatLogRecordInvalid
			}
			binding = change.Binding
		case change.Input != nil:
			input := change.Input
			old, err := idx.lookup("host <> '' AND host = ? AND kind = ?", input.HostInvocationID, indexInput)
			if err != nil {
				return err
			}
			if binding == nil || binding.Cleared || binding.Session != input.Session ||
				old.Cursor != 0 || inputs[input.HostInvocationID] != nil {
				return status_error.ChatLogRecordInvalid
			}
			inputs[input.HostInvocationID] = input
		case change.Consumption != nil:
			host := change.Consumption.HostInvocationID
			old, err := idx.lookup("host <> '' AND host = ? AND kind = ?", host, indexConsumption)
			if err != nil {
				return err
			}
			if consumed[host] || old.Cursor != 0 {
				return status_error.ChatLogIdempotencyConflict
			}
			input := inputs[host]
			if input == nil {
				pos, err := idx.lookup("host <> '' AND host = ? AND kind = ?", host, indexInput)
				if err != nil {
					return err
				}
				if pos.Cursor > 0 {
					row, err := readDiskRow(file, pos.Where)
					if err != nil || row.Chat == nil || row.Chat.Input == nil {
						return status_error.ChatLogCorrupted
					}
					input = row.Chat.Input
				}
			}
			result, err := s.readNativeTerminal(ctx, change.Consumption.Result)
			if err != nil {
				return err
			}
			if input == nil || input.Session != result.Result.Key.Session ||
				!slices.Contains(result.Result.Turn.ConsumedInputIDs, input.AdapterInputID) {
				return status_error.ChatLogRecordInvalid
			}
			consumed[host] = true
		case change.Message != nil:
			m := change.Message
			if m.Kind == agent.MessageNativeFinal {
				terminal, err := s.readNativeTerminal(ctx, *m.Result)
				if err != nil {
					return err
				}
				if terminal.Result.FinalMessage == nil && terminal.Body == nil {
					return status_error.ChatLogRecordInvalid
				}
			}
			if m.Kind == agent.MessageNativeFragment {
				if err := s.visitNativeRange(ctx, *m.Fragments, nil); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validDiskRow(row diskRow, chat bool) bool {
	if chat {
		return row.Chat != nil && row.Fragment == nil && row.Terminal == nil && row.DetachedInput == nil && validChatChange(*row.Chat)
	}
	return row.Chat == nil && ((row.Fragment != nil && row.Terminal == nil && row.DetachedInput == nil && validNativeFragment(*row.Fragment)) ||
		(row.Terminal != nil && row.Fragment == nil && row.DetachedInput == nil && validNativeTerminal(*row.Terminal)) ||
		(row.DetachedInput != nil && row.Fragment == nil && row.Terminal == nil && validHostInput(*row.DetachedInput) &&
			row.DetachedInput.ExecutionID == "" && row.DetachedInput.AssistantMessageID == ""))
}

func validChatChange(change agent.ConversationChange) bool {
	count := 0
	if f := change.Fact; f != nil {
		switch f.Type {
		case constants.ChatEventTypeUnspecified, constants.ChatEventTypeRuntimeEvent,
			constants.ChatEventTypeNativeSessionUpdated, constants.ChatEventTypeNativeSessionCleared,
			constants.ChatEventTypeAssistantMessage, constants.ChatEventTypeToolMessage:
			return false
		case constants.ChatEventTypeUserMessageCreated, constants.ChatEventTypeMessageInherited:
			if !validID(f.MessageID) {
				return false
			}
		case constants.ChatEventTypeExecutionCreated:
			if !validID(f.ExecutionID) && !validID(f.MessageID) {
				return false
			}
		case constants.ChatEventTypeInvocationFinished, constants.ChatEventTypeExecutionCompleted,
			constants.ChatEventTypeExecutionFailed, constants.ChatEventTypeExecutionCanceled:
			var fields map[string]json.RawMessage
			if len(f.Data) != 0 && json.Unmarshal(f.Data, &fields) != nil {
				return false
			}
			for _, name := range []string{"usage", "finalMessage", "resultText"} {
				if _, exists := fields[name]; exists {
					return false
				}
			}
		}
		count++
	}
	if m := change.Message; m != nil {
		if !validID(m.MessageID) || m.Revision < 0 || m.ContentType != "" || m.Text != "" || m.ToolCallID != "" || len(m.Data) != 0 {
			return false
		}
		switch m.Kind {
		case agent.MessageComplete:
			if m.Result != nil || m.Fragments != nil {
				return false
			}
		case agent.MessageNativeFinal:
			if m.Result == nil || !validNativeKey(*m.Result) || m.Fragments != nil {
				return false
			}
		case agent.MessageNativeFragment:
			if m.Result != nil || m.Fragments == nil || !validNativeRange(*m.Fragments) {
				return false
			}
		default:
			return false
		}
		count++
	}
	if b := change.Binding; b != nil {
		if !validNativeSession(b.Session) || b.Cleared && len(b.Configuration) > 0 {
			return false
		}
		count++
	}
	if i := change.Input; i != nil {
		if !validHostInput(*i) || !validID(i.ExecutionID) {
			return false
		}
		count++
	}
	if c := change.Consumption; c != nil {
		if !validID(c.HostInvocationID) || !validNativeKey(c.Result) {
			return false
		}
		count++
	}
	return count == 1
}

func describe(row diskRow, where location, at int64) rowIndex {
	r := rowIndex{Where: where, At: at}
	switch {
	case row.Fragment != nil:
		r.Native, r.Message, r.Content = nativeRowID(row.Fragment.Key), row.Fragment.MessageID, row.Fragment.Change.ContentType
		switch row.Fragment.Change.Kind {
		case agent.MessageAppend:
			r.Kind = indexNativeTextAppend
		case agent.MessageReplace:
			r.Kind = indexNativeTextReplace
		case agent.MessageToolSnapshot:
			r.Kind, r.Tool = indexNativeTool, row.Fragment.Change.ToolCallID
			if row.Fragment.Patch != nil {
				r.PatchBase = row.Fragment.Patch.Base
			}
		case agent.MessageEvent:
			r.Kind, r.Tool = indexNativeEvent, row.Fragment.Change.ToolCallID
		}
	case row.DetachedInput != nil:
		r.Kind, r.Native = indexDetachedInput, detachedRowID(row.DetachedInput.Session, row.DetachedInput.HostInvocationID)
		r.Adapter = adapterRowID(row.DetachedInput.Session, row.DetachedInput.AdapterInputID)
	case row.Terminal != nil:
		r.Kind, r.Native = indexNativeTerminal, nativeRowID(row.Terminal.Result.Key)
	case row.Chat.Fact != nil:
		f := row.Chat.Fact
		r.Message, r.Execution = f.MessageID, f.ExecutionID
		switch f.Type {
		case constants.ChatEventTypeUserMessageCreated:
			r.Kind = indexUserMessage
		case constants.ChatEventTypeAssistantMessage, constants.ChatEventTypeToolMessage:
			r.Kind = indexAssistantMessage
		case constants.ChatEventTypeMessageInherited:
			r.Kind = indexInherited
		case constants.ChatEventTypeExecutionCreated:
			r.Kind = indexExecutionCreated
			if r.Execution == "" {
				r.Execution = r.Message
			}
			if r.Message == "" {
				r.Message = r.Execution
			}
		case constants.ChatEventTypeExecutionCompleted:
			r.Kind = indexExecutionCompleted
		case constants.ChatEventTypeExecutionFailed, constants.ChatEventTypeExecutionCanceled:
			r.Kind = indexExecutionEnded
		}
	case row.Chat.Message != nil:
		m := row.Chat.Message
		r.Message, r.Execution = m.MessageID, m.ExecutionID
		switch m.Kind {
		case agent.MessageNativeFragment:
			r.Kind = indexAssistantMessage
		case agent.MessageNativeFinal:
			r.Kind = indexTextReplace
		case agent.MessageComplete:
			r.Kind = indexMessageComplete
		}
	case row.Chat.Binding != nil:
		r.Kind = indexBinding
	case row.Chat.Input != nil:
		r.Kind, r.Host, r.Execution = indexInput, row.Chat.Input.HostInvocationID, row.Chat.Input.ExecutionID
		r.Adapter = adapterRowID(row.Chat.Input.Session, row.Chat.Input.AdapterInputID)
	case row.Chat.Consumption != nil:
		r.Kind, r.Host = indexConsumption, row.Chat.Consumption.HostInvocationID
	}
	return r
}

// Delete removes the public log and independently managed file result directory.
func (s *Store) Delete(chatID string) error {
	slot := s.chatSlot(chatID)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	path, err := s.LogPath(chatID)
	if err != nil {
		return err
	}
	fileChanges, err := s.FileChangesDirectory(chatID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return status_error.ChatLogError
	}
	if err := os.RemoveAll(fileChanges); err != nil {
		return status_error.ChatLogError
	}
	if rel, e := filepath.Rel(s.root, path); e == nil {
		s.indexes.delete(context.Background(), rel)
	}
	if _, err := os.Stat(filepath.Dir(path)); err == nil {
		if err := syncDirectory(filepath.Dir(path)); err != nil {
			return err
		}
	}
	s.catalog.Remove(chatID)
	return nil
}

// Close releases the derived index connection and any private fallback files.
func (s *Store) Close() { s.indexes.close() }
