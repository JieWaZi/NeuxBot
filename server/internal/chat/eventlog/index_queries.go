package eventlog

import (
	"crypto/sha256"
	"encoding/json"
	"io"
	"os"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

const indexScanChunkEntries = int64(512)

// ProjectionWindow 保存公开事件适配器所需的最小前缀与游标后事实。
type ProjectionWindow struct {
	// Records 包含 Chat 创建记录、必要的当前执行前缀和游标后事实。
	Records []Record
	// LatestSequence 是读取时权威 JSONL 的最大连续序号。
	LatestSequence int64
}

// MessageWindow 保存一页消息对应的连续权威事实范围。
type MessageWindow struct {
	// Records 包含 Chat 创建记录和选中消息的完整连续事实。
	Records []Record
	// NextBeforeSequence 是继续读取更早消息时使用的创建序号。
	NextBeforeSequence int64
	// LatestSequence 是读取时权威 JSONL 的最大连续序号。
	LatestSequence int64
	// HasMore 表示当前窗口之前仍有更早消息。
	HasMore bool
}

// ReadProjectionAfter 读取公开事件转换所需的最小状态前缀和游标后记录。
func (s *Store) ReadProjectionAfter(
	chatID string,
	after int64,
	limit int,
) (ProjectionWindow, error) {
	if after < 0 || limit < 0 {
		return ProjectionWindow{}, status_error.ChatEventCursorInvalid
	}
	state, err := s.state(chatID)
	if err != nil {
		return ProjectionWindow{}, err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return queryWithRecoveredIndexLocked(s, chatID, state, func() (ProjectionWindow, error) {
		return s.readProjectionAfterIndexedLocked(chatID, after, limit, state.lastSequence)
	})
}

// readProjectionAfterIndexedLocked 读取调用方已锁定且已恢复的派生索引窗口。
func (s *Store) readProjectionAfterIndexedLocked(
	chatID string,
	after int64,
	limit int,
	latest int64,
) (ProjectionWindow, error) {
	indexFile, err := os.Open(s.indexPath(chatID))
	if err != nil {
		return ProjectionWindow{}, status_error.ChatLogError
	}
	defer indexFile.Close()
	createdEntry, err := readIndexEntryAt(indexFile, 1)
	if err != nil {
		return ProjectionWindow{}, err
	}
	created, err := s.readRangeLocked(chatID, createdEntry.offset, createdEntry.endOffset(), 1)
	if err != nil {
		return ProjectionWindow{}, err
	}
	if after == 0 {
		last := latest
		if limit > 0 && int64(limit) < last {
			last = int64(limit)
		}
		endEntry, readErr := readIndexEntryAt(indexFile, last)
		if readErr != nil {
			return ProjectionWindow{}, readErr
		}
		records, readErr := s.readRangeLocked(chatID, 0, endEntry.endOffset(), 1)
		return ProjectionWindow{Records: records, LatestSequence: latest}, readErr
	}

	anchorSequence := min(after, latest)
	anchor, err := readIndexEntryAt(indexFile, anchorSequence)
	if err != nil {
		return ProjectionWindow{}, err
	}
	prefixStart := int64(0)
	if anchor.executionStartSequence > 0 && anchor.flags&indexFlagExecutionTerminal == 0 {
		prefixStart = anchor.executionStartSequence
	}
	firstTail := after + 1
	lastTail := latest
	if limit > 0 && firstTail+int64(limit)-1 < lastTail {
		lastTail = firstTail + int64(limit) - 1
	}
	if firstTail > latest {
		if prefixStart == 0 {
			return ProjectionWindow{Records: created, LatestSequence: latest}, nil
		}
		startEntry, readErr := readIndexEntryAt(indexFile, prefixStart)
		if readErr != nil {
			return ProjectionWindow{}, readErr
		}
		prefix, readErr := s.readRangeLocked(
			chatID,
			startEntry.offset,
			anchor.endOffset(),
			prefixStart,
		)
		if readErr != nil {
			return ProjectionWindow{}, readErr
		}
		return ProjectionWindow{
			Records:        append(created, prefix...),
			LatestSequence: latest,
		}, nil
	}
	decodeStart := firstTail
	if prefixStart > 0 {
		decodeStart = prefixStart
	}
	startEntry, err := readIndexEntryAt(indexFile, decodeStart)
	if err != nil {
		return ProjectionWindow{}, err
	}
	endEntry, err := readIndexEntryAt(indexFile, lastTail)
	if err != nil {
		return ProjectionWindow{}, err
	}
	rangeRecords, err := s.readRangeLocked(
		chatID,
		startEntry.offset,
		endEntry.endOffset(),
		decodeStart,
	)
	if err != nil {
		return ProjectionWindow{}, err
	}
	if decodeStart == 1 {
		return ProjectionWindow{Records: rangeRecords, LatestSequence: latest}, nil
	}
	return ProjectionWindow{
		Records:        append(created, rangeRecords...),
		LatestSequence: latest,
	}, nil
}

// ReadMessageWindow 通过每 Chat 索引读取一页完整消息事实，不访问 SQLite。
func (s *Store) ReadMessageWindow(
	chatID string,
	beforeSequence int64,
	limit int,
) (MessageWindow, error) {
	if beforeSequence < 0 || limit < 1 {
		return MessageWindow{}, status_error.ChatMessageCursorInvalid
	}
	state, err := s.state(chatID)
	if err != nil {
		return MessageWindow{}, err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return queryWithRecoveredIndexLocked(s, chatID, state, func() (MessageWindow, error) {
		return s.readMessageWindowIndexedLocked(
			chatID,
			beforeSequence,
			limit,
			state.lastSequence,
		)
	})
}

// readMessageWindowIndexedLocked 通过调用方已锁定且已恢复的索引读取消息窗口。
func (s *Store) readMessageWindowIndexedLocked(
	chatID string,
	beforeSequence int64,
	limit int,
	latestSequence int64,
) (MessageWindow, error) {
	indexFile, err := os.Open(s.indexPath(chatID))
	if err != nil {
		return MessageWindow{}, status_error.ChatLogError
	}
	defer indexFile.Close()
	upperSequence := latestSequence + 1
	endOffset := int64(0)
	if beforeSequence > 0 {
		boundary, readErr := readIndexEntryAt(indexFile, beforeSequence)
		if readErr != nil {
			return MessageWindow{}, readErr
		}
		if !boundary.isMessageStart() {
			return MessageWindow{}, status_error.ChatMessageCursorInvalid
		}
		upperSequence = beforeSequence
		endOffset = boundary.offset
	}
	entries, err := scanMessageEntriesBackward(indexFile, upperSequence-1, limit+1)
	if err != nil {
		return MessageWindow{}, err
	}
	hasMore := len(entries) > limit
	if hasMore {
		entries = entries[:limit]
	}
	if len(entries) == 0 {
		return MessageWindow{Records: []Record{}, LatestSequence: latestSequence}, nil
	}
	for left, right := 0, len(entries)-1; left < right; left, right = left+1, right-1 {
		entries[left], entries[right] = entries[right], entries[left]
	}
	if endOffset == 0 {
		last, readErr := readIndexEntryAt(indexFile, latestSequence)
		if readErr != nil {
			return MessageWindow{}, readErr
		}
		endOffset = last.endOffset()
	}
	start := entries[0]
	records, err := s.readRangeLocked(
		chatID,
		start.offset,
		endOffset,
		start.sequence,
	)
	if err != nil {
		return MessageWindow{}, err
	}
	if start.sequence > 1 {
		createdEntry, readErr := readIndexEntryAt(indexFile, 1)
		if readErr != nil {
			return MessageWindow{}, readErr
		}
		created, readErr := s.readRangeLocked(chatID, 0, createdEntry.endOffset(), 1)
		if readErr != nil {
			return MessageWindow{}, readErr
		}
		records = append(created, records...)
	}
	window := MessageWindow{
		Records:        records,
		LatestSequence: latestSequence,
		HasMore:        hasMore,
	}
	if hasMore {
		window.NextBeforeSequence = entries[0].sequence
	}
	return window, nil
}

// ReadMessage 通过消息标识摘要定位并读取该消息的完整事实范围。
func (s *Store) ReadMessage(chatID string, messageID string) ([]Record, error) {
	state, err := s.state(chatID)
	if err != nil {
		return nil, err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return queryWithRecoveredIndexLocked(s, chatID, state, func() ([]Record, error) {
		return s.readMessageIndexedLocked(chatID, messageID, state.lastSequence)
	})
}

// readMessageIndexedLocked 通过调用方已锁定且已恢复的索引读取一条消息事实。
func (s *Store) readMessageIndexedLocked(
	chatID string,
	messageID string,
	latestSequence int64,
) ([]Record, error) {
	indexFile, err := os.Open(s.indexPath(chatID))
	if err != nil {
		return nil, status_error.ChatLogError
	}
	defer indexFile.Close()
	targetHash := sha256.Sum256([]byte(messageID))
	var target indexEntry
	err = scanIndexEntries(indexFile, 1, latestSequence, func(entry indexEntry) bool {
		if entry.isMessageStart() && entry.messageHash == targetHash {
			target = entry
			return false
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	if target.sequence == 0 {
		return []Record{}, nil
	}
	return s.readMessageAtIndexedLocked(chatID, target.sequence, latestSequence, indexFile)
}

// ReadMessageAt 按原日志中的已知消息入口读取完整事实，不扫描历史前缀。
func (s *Store) ReadMessageAt(chatID string, sequence int64) ([]Record, error) {
	state, err := s.state(chatID)
	if err != nil {
		return nil, err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return queryWithRecoveredIndexLocked(s, chatID, state, func() ([]Record, error) {
		file, err := os.Open(s.indexPath(chatID))
		if err != nil {
			return nil, status_error.ChatLogError
		}
		defer file.Close()
		return s.readMessageAtIndexedLocked(chatID, sequence, state.lastSequence, file)
	})
}

// readMessageAtIndexedLocked 仅扫描已定位消息自身的范围，正文仍从原 JSONL 解码。
func (s *Store) readMessageAtIndexedLocked(chatID string, sequence, latest int64, file *os.File) ([]Record, error) {
	target, err := readIndexEntryAt(file, sequence)
	if err != nil {
		return nil, err
	}
	if !target.isMessageStart() {
		return nil, status_error.ChatLogIndexCorrupted
	}
	endOffset := int64(0)
	err = scanIndexEntries(file, sequence+1, latest, func(entry indexEntry) bool {
		if entry.isMessageStart() {
			endOffset = entry.offset
			return false
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	if endOffset == 0 {
		last, err := readIndexEntryAt(file, latest)
		if err != nil {
			return nil, err
		}
		endOffset = last.endOffset()
	}
	records, err := s.readRangeLocked(chatID, target.offset, endOffset, sequence)
	if err != nil {
		return nil, err
	}
	createdEntry, err := readIndexEntryAt(file, 1)
	if err != nil {
		return nil, err
	}
	created, err := s.readRangeLocked(chatID, 0, createdEntry.endOffset(), 1)
	if err != nil {
		return nil, err
	}
	return append(created, records...), nil
}

// MessageRecordedTimes 返回时间窗内全部消息创建时间，用于精确时区分组。
func (s *Store) MessageRecordedTimes(
	chatID string,
	startMs int64,
	endMs int64,
) ([]int64, error) {
	state, err := s.state(chatID)
	if err != nil {
		return nil, err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return queryWithRecoveredIndexLocked(s, chatID, state, func() ([]int64, error) {
		return s.messageRecordedTimesIndexedLocked(
			chatID,
			startMs,
			endMs,
			state.lastSequence,
		)
	})
}

// messageRecordedTimesIndexedLocked 扫描调用方已锁定且已恢复的固定索引记录。
func (s *Store) messageRecordedTimesIndexedLocked(
	chatID string,
	startMs int64,
	endMs int64,
	latestSequence int64,
) ([]int64, error) {
	file, err := os.Open(s.indexPath(chatID))
	if err != nil {
		return nil, status_error.ChatLogError
	}
	defer file.Close()
	times := make([]int64, 0)
	err = scanIndexEntries(file, 1, latestSequence, func(entry indexEntry) bool {
		if entry.isMessageStart() && entry.flags&indexFlagInheritedMessage == 0 && entry.recordedAt >= startMs && entry.recordedAt <= endMs {
			times = append(times, entry.recordedAt)
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return times, nil
}

// scanIndexEntries 分块顺序读取固定记录，并允许调用方提前结束扫描。
func scanIndexEntries(
	file *os.File,
	fromSequence int64,
	toSequence int64,
	visit func(indexEntry) bool,
) error {
	for blockStart := fromSequence; blockStart <= toSequence; {
		blockEnd := min(toSequence, blockStart+indexScanChunkEntries-1)
		count := blockEnd - blockStart + 1
		buffer := make([]byte, count*indexEntrySize)
		if _, err := file.ReadAt(buffer, indexEntryOffset(blockStart)); err != nil && err != io.EOF {
			return status_error.ChatLogError
		}
		for sequence := blockStart; sequence <= blockEnd; sequence++ {
			position := (sequence - blockStart) * indexEntrySize
			entry, err := decodeIndexEntry(buffer[position : position+indexEntrySize])
			if err != nil || entry.sequence != sequence {
				return status_error.ChatLogIndexCorrupted
			}
			if !visit(entry) {
				return nil
			}
		}
		blockStart = blockEnd + 1
	}
	return nil
}

// scanMessageEntriesBackward 分块倒序扫描消息入口，避免索引较大时整文件载入内存。
func scanMessageEntriesBackward(
	file *os.File,
	fromSequence int64,
	limit int,
) ([]indexEntry, error) {
	entries := make([]indexEntry, 0, limit)
	for blockEnd := fromSequence; blockEnd >= 1 && len(entries) < limit; {
		blockStart := max(int64(1), blockEnd-indexScanChunkEntries+1)
		count := blockEnd - blockStart + 1
		buffer := make([]byte, count*indexEntrySize)
		if _, err := file.ReadAt(buffer, indexEntryOffset(blockStart)); err != nil && err != io.EOF {
			return nil, status_error.ChatLogError
		}
		for sequence := blockEnd; sequence >= blockStart && len(entries) < limit; sequence-- {
			position := (sequence - blockStart) * indexEntrySize
			entry, err := decodeIndexEntry(buffer[position : position+indexEntrySize])
			if err != nil || entry.sequence != sequence {
				return nil, status_error.ChatLogIndexCorrupted
			}
			if entry.isMessageStart() {
				entries = append(entries, entry)
			}
		}
		blockEnd = blockStart - 1
	}
	return entries, nil
}

// CountUserMessages 从可恢复的定位索引统计真实用户消息，只读取候选消息正文。
func (s *Store) CountUserMessages(chatID string) (int64, error) {
	state, err := s.state(chatID)
	if err != nil {
		return 0, err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return queryWithRecoveredIndexLocked(s, chatID, state, func() (int64, error) {
		file, err := os.Open(s.indexPath(chatID))
		if err != nil {
			return 0, status_error.ChatLogError
		}
		defer file.Close()
		seen := make(map[[32]byte]bool)
		var readErr error
		err = scanIndexEntries(file, 1, state.lastSequence, func(entry indexEntry) bool {
			if entry.flags&indexFlagUserMessage == 0 || seen[entry.messageHash] {
				return true
			}
			var records []Record
			records, readErr = s.readRangeLocked(chatID, entry.offset, entry.endOffset(), entry.sequence)
			if readErr != nil {
				return false
			}
			var data MessageCreatedData
			if len(records) != 1 || json.Unmarshal(records[0].Data, &data) != nil {
				readErr = status_error.ChatLogError
				return false
			}
			if data.Role == constants.MessageRoleUser && data.SourceWakerID == "" &&
				(data.Actor == "" || data.Actor == string(constants.ChannelMessageActorUser)) {
				seen[entry.messageHash] = true
			}
			return true
		})
		if err != nil {
			return 0, err
		}
		if readErr != nil {
			return 0, readErr
		}
		return int64(len(seen)), nil
	})
}
