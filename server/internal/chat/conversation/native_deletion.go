package conversation

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"gorm.io/gorm"
)

// PurgeDeletedChatNativeRecords 在删除暂存日志仍可验证时清理独占的原生会话记录。
// 损坏导致归属无法确认时保留共享分片；返回值也包含上一次尝试已清理的会话。
func (s *Store) PurgeDeletedChatNativeRecords(ctx context.Context, stagedLogs map[string]string) (bool, error) {
	sessions := make(map[agent.NativeSessionKey]struct{})
	for chatID, path := range stagedLogs {
		if err := scanDeletionLog(ctx, path, true, func(_ batchHeader, _ []byte, rows [][]byte) error {
			for _, raw := range rows {
				var row diskRow
				if json.Unmarshal(raw, &row) != nil || !validDiskRow(row, true) {
					return status_error.ChatLogCorrupted
				}
				if binding := row.Chat.Binding; binding != nil {
					sessions[binding.Session] = struct{}{}
					if binding.Handoff != nil && binding.Handoff.Summary != nil {
						sessions[binding.Handoff.Summary.Session] = struct{}{}
					}
				}
			}
			return nil
		}); err != nil {
			if corruptDeletionLog(err) {
				logx.Warnf("skip native deletion cleanup: staged_chat_id=%s error=%v", chatID, err)
				return false, nil
			}
			return false, err
		}
	}
	if len(sessions) == 0 {
		return false, nil
	}
	if err := s.catalog.Refresh(); err != nil {
		return false, err
	}
	for _, chatID := range s.catalog.ListChatIDs() {
		if len(sessions) == 0 {
			return false, nil
		}
		if _, deleting := stagedLogs[chatID]; deleting {
			continue
		}
		if err := s.retainReferencedNativeSessions(ctx, chatID, sessions); err != nil {
			// 目录仍有此 Chat 身份，空日志也无法证明未引用候选会话。
			if corruptDeletionLog(err) || util.HasStatusCode(err, status_error.ChatNotFound) {
				logx.Warnf("skip native deletion cleanup: surviving_chat_id=%s error=%v", chatID, err)
				return false, nil
			}
			return false, err
		}
	}
	var shards [streamShards]map[agent.NativeSessionKey]struct{}
	for session := range sessions {
		shard := nativeShard(session)
		if shards[shard] == nil {
			shards[shard] = make(map[agent.NativeSessionKey]struct{})
		}
		shards[shard][session] = struct{}{}
	}
	purged := false
	for shard, selected := range shards {
		if len(selected) == 0 {
			continue
		}
		if err := s.purgeNativeShard(ctx, shard, selected); err != nil {
			if corruptDeletionLog(err) {
				logx.Warnf("skip native deletion cleanup: shard=%d error=%v", shard, err)
				continue
			}
			return false, err
		}
		purged = true
	}
	return purged, nil
}

func corruptDeletionLog(err error) bool {
	return util.HasStatusCode(err, status_error.ChatLogCorrupted) || util.HasStatusCode(err, status_error.ChatLogIndexCorrupted)
}

func (s *Store) retainReferencedNativeSessions(ctx context.Context, chatID string, selected map[agent.NativeSessionKey]struct{}) error {
	path, err := s.LogPath(chatID)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(s.root, path)
	if err != nil {
		logx.Errorf("resolve surviving chat log for native deletion: chat_id=%s error=%v", chatID, err)
		return status_error.ChatLogError
	}
	index := streamIndex{db: &s.indexes, ctx: ctx, coverage: streamCoverage{Path: relative}}
	if err := index.loadCoverage(); err != nil {
		return err
	}
	// 在原定位重建前检查已核验的字节边界。完整旧帧仍能通过日志解析，不能据此认定没有后续绑定。
	if info, statErr := os.Stat(path); statErr == nil && info.Size() < index.coverage.Size {
		return status_error.ChatLogCorrupted
	}
	r, err := s.openChat(ctx, chatID)
	if err != nil {
		return err
	}
	defer r.close()
	var after int64
	for {
		var bindings []rowIndex
		if err := r.index.query(func(db *gorm.DB) error {
			return db.Where("stream = ? AND kind = ? AND cursor > ?", r.index.coverage.Stream, indexBinding, after).
				Order("cursor ASC").Limit(indexReadBatch).Find(&bindings).Error
		}); err != nil {
			return err
		}
		for _, pos := range bindings {
			row, err := readDiskRow(r.file, pos.Where)
			if err != nil {
				return err
			}
			if row.Chat == nil || row.Chat.Binding == nil {
				return status_error.ChatLogCorrupted
			}
			binding := row.Chat.Binding
			delete(selected, binding.Session)
			if binding.Handoff != nil && binding.Handoff.Summary != nil {
				delete(selected, binding.Handoff.Summary.Session)
			}
			after = pos.Cursor
		}
		if len(bindings) < indexReadBatch || len(selected) == 0 {
			return nil
		}
	}
}

func (s *Store) purgeNativeShard(ctx context.Context, shard int, selected map[agent.NativeSessionKey]struct{}) error {
	slot := &s.natives[shard]
	slot.mu.Lock()
	defer slot.mu.Unlock()
	path := filepath.Join(s.root, nativeBucketPath(shard))
	idx, err := slot.load(ctx, path, false)
	if err != nil || idx.version() == 0 {
		return err
	}
	// A retry replaces this temporary file. The live shard is published only
	// after its replacement is complete and synced on the same filesystem.
	temporary := path + ".purging"
	if err := os.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
		return status_error.ChatLogError
	}
	out, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return status_error.ChatLogError
	}
	defer os.Remove(temporary)
	defer out.Close()
	changed := false
	err = scanDeletionLog(ctx, path, false, func(header batchHeader, frame []byte, rows [][]byte) error {
		purged := 0
		for i, raw := range rows {
			var row diskRow
			if json.Unmarshal(raw, &row) != nil || !validDiskRow(row, false) {
				return status_error.ChatLogCorrupted
			}
			var session agent.NativeSessionKey
			switch {
			case row.Fragment != nil:
				session = row.Fragment.Key.Session
			case row.Terminal != nil:
				session = row.Terminal.Result.Key.Session
			case row.DetachedInput != nil:
				session = row.DetachedInput.Session
			}
			if _, ok := selected[session]; ok {
				rows[i] = []byte(`{"x":true}`)
				purged++
			}
		}
		if purged > 0 {
			changed = true
			if purged == len(rows) {
				header.Key = "purged-native:" + strconv.FormatInt(header.Receipt.First, 10)
				header.Request = [32]byte{}
			}
			var err error
			frame, _, err = encodeBatch(header, rows, 0)
			if err != nil {
				return err
			}
		}
		if _, err := out.Write(frame); err != nil {
			return status_error.ChatLogError
		}
		return nil
	})
	if err != nil || !changed {
		return err
	}
	if out.Sync() != nil || out.Close() != nil || os.Rename(temporary, path) != nil || syncDirectory(filepath.Dir(path)) != nil {
		return status_error.ChatLogError
	}
	s.indexes.markDirty(idx.coverage.Path, true)
	_, err = slot.load(ctx, path, false)
	return err
}

func scanDeletionLog(ctx context.Context, path string, public bool, visit func(batchHeader, []byte, [][]byte) error) error {
	file, err := os.Open(path)
	if public && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return status_error.ChatLogError
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64<<10)
	var version int64
	for {
		if err := contextError(ctx); err != nil {
			return err
		}
		frame, err := readLine(reader)
		if errors.Is(err, io.EOF) {
			if len(frame) != 0 {
				return status_error.ChatLogCorrupted
			}
			return nil
		}
		if err != nil {
			return err
		}
		rows := make([][]byte, 0, maxBatchRows)
		header, err := decodeBatch(frame, 0, version, func(raw []byte, _ location) error {
			rows = append(rows, raw)
			return nil
		})
		if err != nil {
			return err
		}
		if err := visit(header, frame, rows); err != nil {
			return err
		}
		version = header.Receipt.Version
	}
}
