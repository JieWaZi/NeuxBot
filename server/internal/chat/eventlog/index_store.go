package eventlog

import (
	"errors"
	"hash/crc32"
	"io"
	"os"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// queryWithRecoveredIndexLocked 使用已校验索引查询，发现中间索引损坏时从 JSONL 重建并重试一次。
func queryWithRecoveredIndexLocked[T any](
	s *Store,
	chatID string,
	state *fileState,
	query func() (T, error),
) (T, error) {
	var zero T
	if _, err := s.recoverIndexLocked(chatID, state); err != nil {
		return zero, err
	}
	result, err := query()
	if err == nil || errors.Is(err, status_error.ChatMessageCursorInvalid) {
		return result, err
	}
	state.indexDirty = true
	if _, rebuildErr := s.recoverIndexLocked(chatID, state); rebuildErr != nil {
		return zero, rebuildErr
	}
	return query()
}

// IndexRecovery 保存一次 Chat 派生索引恢复结果。
type IndexRecovery struct {
	// LatestSequence 是权威 JSONL 当前最大连续序号。
	LatestSequence int64
	// LogSize 是权威 JSONL 当前合法字节长度。
	LogSize int64
	// Rebuilt 表示索引缺失、损坏或落后后已从 JSONL 重建。
	Rebuilt bool
	// TailBytesRemoved 是本次恢复删除的不完整 JSONL 尾部字节数。
	TailBytesRemoved int64
	// TailRepairedAt 是本次尾部修复完成的 Unix 毫秒时间。
	TailRepairedAt int64
}

// RecoverIndex 校验 JSONL 尾部与派生索引，异常时从权威日志安全重建。
func (s *Store) RecoverIndex(chatID string) (IndexRecovery, error) {
	state, err := s.state(chatID)
	if err != nil {
		return IndexRecovery{}, err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return s.recoverIndexLocked(chatID, state)
}

// recoverIndexLocked 在单 Chat 文件锁内恢复日志尾部、索引和追加作用域。
func (s *Store) recoverIndexLocked(chatID string, state *fileState) (IndexRecovery, error) {
	info, err := os.Stat(s.path(chatID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return IndexRecovery{}, status_error.ChatNotFound.WithParams(
				map[string]interface{}{"ChatID": chatID},
			)
		}
		return IndexRecovery{}, status_error.ChatLogError
	}
	if info.Size() == 0 {
		return IndexRecovery{}, status_error.ChatLogCorrupted
	}
	if err := s.loadValidIndexLocked(chatID, info, state); err == nil && !state.indexDirty {
		return IndexRecovery{
			LatestSequence:   state.lastSequence,
			LogSize:          info.Size(),
			TailBytesRemoved: state.tailBytesRemoved,
			TailRepairedAt:   state.tailRepairedAt,
		}, nil
	}

	records, err := s.readAndRepairLocked(chatID, state)
	if err != nil {
		return IndexRecovery{}, err
	}
	if err := s.replaceIndexLocked(chatID, records); err != nil {
		state.indexDirty = true
		return IndexRecovery{}, err
	}
	state.initialized = true
	state.indexDirty = false
	state.lastSequence = records[len(records)-1].Sequence
	logSize := records[len(records)-1].Offset + records[len(records)-1].Length
	return IndexRecovery{
		LatestSequence:   state.lastSequence,
		LogSize:          logSize,
		Rebuilt:          true,
		TailBytesRemoved: state.tailBytesRemoved,
		TailRepairedAt:   state.tailRepairedAt,
	}, nil
}

// loadValidIndexLocked 校验索引锚点；仅在日志版本变化时恢复下一次追加所需作用域。
func (s *Store) loadValidIndexLocked(
	chatID string,
	logInfo os.FileInfo,
	state *fileState,
) error {
	logSize := logInfo.Size()
	file, err := os.Open(s.indexPath(chatID))
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() < indexHeaderSize ||
		(info.Size()-indexHeaderSize)%indexEntrySize != 0 {
		return status_error.ChatLogIndexCorrupted
	}
	header := make([]byte, indexHeaderSize)
	if _, err := io.ReadFull(file, header); err != nil {
		return err
	}
	if err := validateIndexHeader(header, chatID); err != nil {
		return err
	}
	count := (info.Size() - indexHeaderSize) / indexEntrySize
	if count < 1 {
		return status_error.ChatLogIndexCorrupted
	}
	first, err := readIndexEntryAt(file, 1)
	if err != nil || first.sequence != 1 || first.offset != 0 ||
		first.flags&indexFlagChatCreated == 0 {
		return status_error.ChatLogIndexCorrupted
	}
	last, err := readIndexEntryAt(file, count)
	if err != nil || last.sequence != count || last.endOffset() != logSize {
		return status_error.ChatLogIndexCorrupted
	}
	if err := s.verifyIndexedLine(chatID, first); err != nil {
		return err
	}
	if count > 1 {
		if err := s.verifyIndexedLine(chatID, last); err != nil {
			return err
		}
	}
	// 即使作用域可复用，仍校验索引头尾和权威日志锚点，保留外部损坏的恢复入口。
	if state.initialized && !state.indexDirty && state.lastSequence == count &&
		state.scopeLogInfo != nil && os.SameFile(state.scopeLogInfo, logInfo) &&
		state.scopeLogInfo.Size() == logInfo.Size() && state.scopeLogInfo.ModTime().Equal(logInfo.ModTime()) {
		return nil
	}
	state.initialized = true
	state.lastSequence = count
	state.scope = recordScope{}
	if last.executionStartSequence > 0 && last.flags&indexFlagExecutionTerminal == 0 {
		start, readErr := readIndexEntryAt(file, last.executionStartSequence)
		if readErr != nil {
			return readErr
		}
		records, readErr := s.readRangeLocked(
			chatID,
			start.offset,
			logSize,
			start.sequence,
		)
		if readErr != nil {
			return readErr
		}
		scope := recordScope{}
		for _, record := range records {
			if record.Type == constants.ChatEventTypeExecutionCreated {
				scope.executionID = record.ExecutionID
				scope.invocationID = ""
			}
			if record.InvocationID != "" {
				scope.invocationID = record.InvocationID
			}
			if isExecutionTerminalRecord(record.Type) {
				scope = recordScope{}
			}
		}
		state.scope = scope
	}
	state.scopeLogInfo = logInfo
	return nil
}

// replaceIndexLocked 使用已经完整校验的记录原子替换派生索引。
func (s *Store) replaceIndexLocked(chatID string, records []Record) error {
	if len(records) == 0 {
		return status_error.ChatLogCorrupted
	}
	header, err := encodeIndexHeader(chatID)
	if err != nil {
		return err
	}
	path := s.indexPath(chatID)
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			_ = os.Remove(temporary)
			file, err = os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		}
		if err != nil {
			return status_error.ChatLogError
		}
	}
	failed := true
	defer func() {
		_ = file.Close()
		if failed {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := file.Write(header); err != nil {
		return status_error.ChatLogError
	}
	logFile, err := os.Open(s.path(chatID))
	if err != nil {
		return status_error.ChatLogError
	}
	defer logFile.Close()
	executionStart := int64(0)
	for _, record := range records {
		if record.Type == constants.ChatEventTypeExecutionCreated {
			executionStart = record.Sequence
		}
		line := make([]byte, record.Length)
		if _, err := logFile.ReadAt(line, record.Offset); err != nil {
			return status_error.ChatLogError
		}
		entry := indexEntryForRecord(record, executionStart, line)
		if _, err := file.Write(encodeIndexEntry(entry)); err != nil {
			return status_error.ChatLogError
		}
		if isExecutionTerminalRecord(record.Type) {
			executionStart = 0
		}
	}
	if err := file.Sync(); err != nil {
		return status_error.ChatLogError
	}
	if err := file.Close(); err != nil {
		return status_error.ChatLogError
	}
	if err := os.Rename(temporary, path); err != nil {
		return status_error.ChatLogError
	}
	failed = false
	return nil
}

// appendIndexLocked 在 JSONL 已经 fsync 后尽力追加对应派生索引记录。
func (s *Store) appendIndexLocked(chatID string, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	file, err := os.OpenFile(s.indexPath(chatID), os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	expectedSequence := (info.Size()-indexHeaderSize)/indexEntrySize + 1
	if info.Size() < indexHeaderSize ||
		(info.Size()-indexHeaderSize)%indexEntrySize != 0 ||
		records[0].Sequence != expectedSequence {
		return status_error.ChatLogIndexCorrupted
	}
	logFile, err := os.Open(s.path(chatID))
	if err != nil {
		return err
	}
	defer logFile.Close()
	executionStart := int64(0)
	if records[0].Sequence > 1 {
		previous, readErr := readIndexEntryAt(file, records[0].Sequence-1)
		if readErr != nil {
			return readErr
		}
		if previous.flags&indexFlagExecutionTerminal == 0 {
			executionStart = previous.executionStartSequence
		}
	}
	for _, record := range records {
		if record.Type == constants.ChatEventTypeExecutionCreated {
			executionStart = record.Sequence
		}
		line := make([]byte, record.Length)
		if _, err := logFile.ReadAt(line, record.Offset); err != nil {
			return err
		}
		entry := indexEntryForRecord(record, executionStart, line)
		if _, err := file.Write(encodeIndexEntry(entry)); err != nil {
			return err
		}
		if isExecutionTerminalRecord(record.Type) {
			executionStart = 0
		}
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return nil
}

// readIndexEntryAt 随机读取指定一基序号的索引记录。
func readIndexEntryAt(file *os.File, sequence int64) (indexEntry, error) {
	if sequence < 1 {
		return indexEntry{}, status_error.ChatLogIndexCorrupted
	}
	buffer := make([]byte, indexEntrySize)
	if _, err := file.ReadAt(buffer, indexEntryOffset(sequence)); err != nil {
		return indexEntry{}, status_error.ChatLogError
	}
	entry, err := decodeIndexEntry(buffer)
	if err != nil {
		return indexEntry{}, err
	}
	if entry.sequence != sequence {
		return indexEntry{}, status_error.ChatLogIndexCorrupted
	}
	return entry, nil
}

// verifyIndexedLine 对索引锚点对应的权威 JSONL 行执行校验。
func (s *Store) verifyIndexedLine(chatID string, entry indexEntry) error {
	file, err := os.Open(s.path(chatID))
	if err != nil {
		return status_error.ChatLogError
	}
	defer file.Close()
	line := make([]byte, entry.length)
	if _, err := file.ReadAt(line, entry.offset); err != nil {
		return status_error.ChatLogError
	}
	if len(line) == 0 || line[len(line)-1] != '\n' ||
		crc32.ChecksumIEEE(line) != entry.checksum {
		return status_error.ChatLogIndexCorrupted
	}
	return nil
}

// readRangeLocked 在调用方已经持有 Chat 文件锁时读取字节区间。
func (s *Store) readRangeLocked(
	chatID string,
	startOffset int64,
	endOffset int64,
	firstSequence int64,
) ([]Record, error) {
	file, err := os.Open(s.path(chatID))
	if err != nil {
		return nil, status_error.ChatLogError
	}
	defer file.Close()
	content := make([]byte, endOffset-startOffset)
	if len(content) > 0 {
		if _, err := file.ReadAt(content, startOffset); err != nil {
			return nil, status_error.ChatLogError
		}
	}
	return decodeRecordRange(chatID, startOffset, firstSequence, content)
}
