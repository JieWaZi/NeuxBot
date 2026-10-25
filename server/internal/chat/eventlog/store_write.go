package eventlog

import (
	"io"
	"os"
	"path/filepath"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// Create 使用独占文件创建一份新 Chat 日志。
func (s *Store) Create(chatID string, inputs []AppendInput) ([]Record, error) {
	if len(inputs) == 0 {
		return nil, status_error.ChatLogRecordInvalid
	}
	state, err := s.state(chatID)
	if err != nil {
		return nil, err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()

	path := s.path(chatID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, status_error.ChatLogError
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, status_error.ChatLogError
	}
	records, encoded, scope, err := encodeRecords(
		chatID,
		0,
		0,
		recordScope{},
		inputs,
	)
	if err == nil {
		err = writeSyncAndClose(file, encoded)
	} else {
		_ = file.Close()
	}
	if err != nil {
		_ = os.Remove(path)
		return nil, status_error.ChatLogError
	}
	if err := s.replaceIndexLocked(chatID, records); err != nil {
		state.indexDirty = true
	}
	state.initialized = true
	state.lastSequence = records[len(records)-1].Sequence
	state.scope = scope
	state.scopeLogInfo, _ = os.Stat(path)
	return records, nil
}

// Append 串行追加一批事件并在返回前同步到磁盘。
func (s *Store) Append(chatID string, inputs []AppendInput) ([]Record, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	state, err := s.state(chatID)
	if err != nil {
		return nil, err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()

	if !state.initialized {
		if _, readErr := s.recoverIndexLocked(chatID, state); readErr != nil {
			return nil, readErr
		}
	}
	path := s.path(chatID)
	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, status_error.ChatLogError
	}
	records, encoded, scope, err := encodeRecords(
		chatID,
		state.lastSequence,
		fileInfo.Size(),
		state.scope,
		inputs,
	)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, status_error.ChatLogError
	}
	if err := writeSyncAndClose(file, encoded); err != nil {
		state.initialized = false
		return nil, status_error.ChatLogError
	}
	state.lastSequence = records[len(records)-1].Sequence
	state.scope = scope
	state.scopeLogInfo, _ = os.Stat(path)
	if err := s.appendIndexLocked(chatID, records); err != nil {
		state.indexDirty = true
	}
	return records, nil
}

// writeSyncAndClose 完整写入、同步并关闭文件，返回遇到的第一个错误。
func writeSyncAndClose(file *os.File, content []byte) error {
	written, err := file.Write(content)
	if err == nil && written != len(content) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	return err
}
