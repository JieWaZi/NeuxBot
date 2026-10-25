package eventlog

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// ReadAll 返回一份日志中的全部合法事件并修复崩溃尾行。
func (s *Store) ReadAll(chatID string) ([]Record, error) {
	state, err := s.state(chatID)
	if err != nil {
		return nil, err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	records, err := s.readAndRepairLocked(chatID, state)
	if err != nil {
		return nil, err
	}
	state.initialized = true
	state.lastSequence = 0
	if len(records) > 0 {
		state.lastSequence = records[len(records)-1].Sequence
	}
	expectedIndexSize := indexHeaderSize + int64(len(records))*indexEntrySize
	indexInfo, indexErr := os.Stat(s.indexPath(chatID))
	if state.indexDirty || indexErr != nil || indexInfo.Size() != expectedIndexSize {
		if err := s.replaceIndexLocked(chatID, records); err != nil {
			state.indexDirty = true
		} else {
			state.indexDirty = false
		}
	}
	return records, nil
}

// ReadAfter 返回大于游标的有界事件列表。
func (s *Store) ReadAfter(chatID string, after int64, limit int) ([]Record, error) {
	if after < 0 || limit < 0 {
		return nil, status_error.ChatEventCursorInvalid
	}
	state, err := s.state(chatID)
	if err != nil {
		return nil, err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return queryWithRecoveredIndexLocked(s, chatID, state, func() ([]Record, error) {
		return s.readAfterIndexedLocked(chatID, after, limit, state.lastSequence)
	})
}

// readAfterIndexedLocked 通过固定记录索引直接定位游标附近，只重放必要执行前缀。
func (s *Store) readAfterIndexedLocked(
	chatID string,
	after int64,
	limit int,
	latestSequence int64,
) ([]Record, error) {
	if after >= latestSequence {
		return []Record{}, nil
	}
	firstResultSequence := after + 1
	lastResultSequence := latestSequence
	if limit > 0 && firstResultSequence+int64(limit)-1 < lastResultSequence {
		lastResultSequence = firstResultSequence + int64(limit) - 1
	}
	indexFile, err := os.Open(s.indexPath(chatID))
	if err != nil {
		return nil, err
	}
	defer indexFile.Close()
	firstEntry, err := readIndexEntryAt(indexFile, firstResultSequence)
	if err != nil {
		return nil, err
	}
	decodeStartSequence := firstResultSequence
	if firstEntry.executionStartSequence > 0 {
		decodeStartSequence = firstEntry.executionStartSequence
	}
	startEntry, err := readIndexEntryAt(indexFile, decodeStartSequence)
	if err != nil {
		return nil, err
	}
	endEntry, err := readIndexEntryAt(indexFile, lastResultSequence)
	if err != nil {
		return nil, err
	}
	decoded, err := s.readRangeLocked(
		chatID,
		startEntry.offset,
		endEntry.endOffset(),
		decodeStartSequence,
	)
	if err != nil {
		return nil, err
	}
	result := make([]Record, 0, lastResultSequence-firstResultSequence+1)
	for _, record := range decoded {
		if record.Sequence >= firstResultSequence {
			result = append(result, record)
		}
	}
	return result, nil
}

// readAndRepairLocked 全量读取并只截断无法构成完整 JSONL 行的崩溃尾部。
func (s *Store) readAndRepairLocked(
	chatID string,
	state *fileState,
) ([]Record, error) {
	path := s.path(chatID)
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, status_error.ChatNotFound.WithParams(
				map[string]interface{}{"ChatID": chatID},
			)
		}
		return nil, status_error.ChatLogError
	}
	if len(content) == 0 {
		return nil, status_error.ChatLogCorrupted
	}
	if content[len(content)-1] != '\n' {
		lastNewline := bytes.LastIndexByte(content, '\n')
		if lastNewline < 0 {
			return nil, status_error.ChatLogCorrupted
		}
		repairedLength := lastNewline + 1
		removedBytes := int64(len(content) - repairedLength)
		if err := os.Truncate(path, int64(repairedLength)); err != nil {
			return nil, status_error.ChatLogError
		}
		state.tailBytesRemoved = removedBytes
		state.tailRepairedAt = time.Now().UnixMilli()
		content = content[:repairedLength]
	}

	lines := bytes.SplitAfter(content, []byte{'\n'})
	records := make([]Record, 0, len(lines)-1)
	expectedSequence := int64(1)
	offset := int64(0)
	scope := recordScope{}
	for index, line := range lines {
		lineLength := int64(len(line))
		payload := bytes.TrimSuffix(line, []byte{'\n'})
		if len(payload) == 0 {
			offset += lineLength
			continue
		}
		record, err := decodeRecordPayload(
			payload,
			chatID,
			expectedSequence,
			fmt.Sprintf("chat log line %d", index+1),
			&scope,
		)
		if err != nil {
			return nil, err
		}
		record.Offset = offset
		record.Length = lineLength
		records = append(records, record)
		expectedSequence++
		offset += lineLength
	}
	state.scope = scope
	state.scopeLogInfo, _ = os.Stat(path)
	return records, nil
}
