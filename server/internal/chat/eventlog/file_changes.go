package eventlog

import (
	"crypto/sha256"
	"os"
	"sort"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// ReadFileChangesReference 只读取 Chat 范围与指定消息的最后成果引用，不加载工具正文。
func (s *Store) ReadFileChangesReference(chatID, messageID string) ([]Record, error) {
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
		created, err := readIndexEntryAt(file, 1)
		if err != nil {
			return nil, err
		}
		records, err := s.readRangeLocked(chatID, created.offset, created.endOffset(), 1)
		if err != nil {
			return nil, err
		}
		hashes := map[[32]byte]bool{sha256.Sum256([]byte(messageID)): true}
		return s.appendFileChangesLocked(chatID, file, state.lastSequence, hashes, records, true)
	})
}

// appendFileChangesLocked 补齐晚到成果；索引只选中最后引用，读取量不随工具正文增长。
func (s *Store) appendFileChangesLocked(chatID string, file *os.File, latest int64, hashes map[[32]byte]bool, records []Record, inherited bool) ([]Record, error) {
	last := make(map[[32]byte]indexEntry)
	err := scanIndexEntries(file, 1, latest, func(entry indexEntry) bool {
		if hashes[entry.messageHash] && (entry.flags&indexFlagFileChanges != 0 || inherited && entry.flags&indexFlagInheritedMessage != 0) {
			last[entry.messageHash] = entry
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	seen := make(map[int64]bool, len(records))
	for _, record := range records {
		seen[record.Sequence] = true
	}
	for _, entry := range last {
		if seen[entry.sequence] {
			continue
		}
		selected, err := s.readRangeLocked(chatID, entry.offset, entry.endOffset(), entry.sequence)
		if err != nil {
			return nil, err
		}
		records = append(records, selected...)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Sequence < records[j].Sequence })
	return records, nil
}
