package conversation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"gorm.io/gorm"
)

const nativeDirectory = "native-conversations"

func adapterRowID(session agent.NativeSessionKey, adapterID string) []byte {
	return nativeRowID(agent.NativeResultKey{Session: session, InvocationID: adapterID})
}

// ReadVersion returns the durable Chat cursor without reading history rows.
func (s *Store) ReadVersion(ctx context.Context, chatID string) (int64, error) {
	slot := s.chatSlot(chatID)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	path, err := s.LogPath(chatID)
	if err != nil {
		return 0, err
	}
	idx, err := slot.load(ctx, path, true)
	if err != nil {
		return 0, err
	}
	return idx.version(), nil
}

func (s *Store) ensureReverseCoverage(ctx context.Context) error {
	s.reverseMu.Lock()
	defer s.reverseMu.Unlock()
	if err := s.indexes.with(ctx, func(*gorm.DB) error { return nil }); err != nil {
		return err
	}
	epoch, dirty := s.indexes.reverseState()
	if !s.reverseReady || s.reverseEpoch != epoch {
		if err := s.catalog.Refresh(); err != nil {
			return err
		}
		for _, chatID := range s.catalog.ListChatIDs() {
			if _, err := s.ReadVersion(ctx, chatID); err != nil {
				return err
			}
		}
		for shard := range streamShards {
			path := filepath.Join(s.root, nativeDirectory, fmt.Sprintf("%02x.jsonl", shard))
			if err := s.loadNativeShard(ctx, shard, path); err != nil {
				return err
			}
		}
		var after uint64
		after, dirty = s.indexes.reverseState()
		if after != epoch {
			s.reverseReady = false
			return status_error.ChatLogIndexCorrupted
		}
		s.reverseEpoch = after
		s.reverseReady = true
	}
	for _, path := range dirty {
		if !filepath.IsLocal(path) {
			return status_error.ChatLogIndexCorrupted
		}
		if filepath.Dir(path) == nativeDirectory {
			base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
			shard, err := strconv.ParseUint(base, 16, 8)
			if err != nil || shard >= streamShards {
				return status_error.ChatLogIndexCorrupted
			}
			if err := s.loadNativeShard(ctx, int(shard), filepath.Join(s.root, path)); err != nil {
				return err
			}
			continue
		}
		if _, err := s.ReadVersion(ctx, strings.TrimSuffix(filepath.Base(path), ".jsonl")); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) prepareNativeCoverage(ctx context.Context) error {
	entries, err := os.ReadDir(filepath.Join(s.root, nativeDirectory))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return status_error.ChatLogError
	}
	for _, entry := range entries {
		base := entry.Name()
		if entry.IsDir() || filepath.Ext(base) != ".jsonl" {
			continue
		}
		shard, err := strconv.ParseUint(strings.TrimSuffix(base, ".jsonl"), 16, 8)
		if err != nil || shard >= streamShards {
			continue
		}
		if err := s.loadNativeShard(ctx, int(shard), filepath.Join(s.root, nativeBucketPath(int(shard)))); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) loadNativeShard(ctx context.Context, shard int, path string) error {
	slot := &s.natives[shard]
	slot.mu.Lock()
	defer slot.mu.Unlock()
	_, err := slot.load(ctx, path, false)
	return err
}

// ReadNativeInputs resolves provider-consumed adapter IDs to durable admissions in input order.
func (s *Store) ReadNativeInputs(ctx context.Context, session agent.NativeSessionKey, adapterIDs []string) ([]agent.DurableHostInput, error) {
	if !validNativeSession(session) {
		return nil, status_error.ChatLogRecordInvalid
	}
	keys := make([][]byte, 0, len(adapterIDs))
	wanted := make(map[string]string, len(adapterIDs))
	for _, id := range adapterIDs {
		if !validID(id) {
			return nil, status_error.ChatLogRecordInvalid
		}
		key := adapterRowID(session, id)
		if wanted[string(key)] != "" {
			return nil, status_error.ChatLogRecordInvalid
		}
		wanted[string(key)] = id
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, nil
	}
	if err := s.ensureReverseCoverage(ctx); err != nil {
		return nil, err
	}
	rows, paths, err := s.indexes.adapterPositions(ctx, keys)
	if err != nil {
		return nil, err
	}
	if len(rows) < len(keys) {
		return nil, status_error.ChatLogNativeOutcomePending
	}
	groups := make(map[int64][]rowIndex)
	for _, row := range rows {
		groups[row.Stream] = append(groups[row.Stream], row)
	}
	resolved := make(map[string]agent.DurableHostInput, len(keys))
	for stream, group := range groups {
		if err := s.readAdmissionRows(ctx, session, paths[stream], group, wanted, resolved); err != nil {
			return nil, err
		}
	}
	out := make([]agent.DurableHostInput, 0, len(keys))
	for _, id := range adapterIDs {
		if resolved[id].Input.AdapterInputID == "" {
			return nil, status_error.ChatLogNativeOutcomePending
		}
		out = append(out, resolved[id])
	}
	return out, nil
}

func (s *Store) readAdmissionRows(ctx context.Context, session agent.NativeSessionKey, relative string, rows []rowIndex, wanted map[string]string, out map[string]agent.DurableHostInput) error {
	if !filepath.IsLocal(relative) {
		return status_error.ChatLogIndexCorrupted
	}
	path := filepath.Join(s.root, relative)
	var chatID string
	slot, expectedPath := s.nativeStream(agent.NativeResultKey{Session: session})
	if path != expectedPath {
		chatID = strings.TrimSuffix(filepath.Base(path), ".jsonl")
		slot = s.chatSlot(chatID)
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if chatID != "" {
		address, err := s.LogPath(chatID)
		if err != nil || address != path {
			return status_error.ChatLogIndexCorrupted
		}
	}
	file, err := os.Open(path)
	if err != nil {
		return status_error.ChatLogNativeOutcomePending
	}
	defer file.Close()
	for _, pos := range rows {
		if err := contextError(ctx); err != nil {
			return err
		}
		row, err := readDiskRow(file, pos.Where)
		if err != nil {
			return err
		}
		input := row.DetachedInput
		if chatID != "" && row.Chat != nil {
			input = row.Chat.Input
		}
		id := wanted[string(pos.Adapter)]
		if input == nil || input.Session != session || input.AdapterInputID != id {
			return status_error.ChatLogCorrupted
		}
		if out[id].Input.AdapterInputID != "" {
			return status_error.ChatLogIdempotencyConflict
		}
		out[id] = agent.DurableHostInput{Ref: agent.HostInputRef{ChatID: chatID, HostInvocationID: input.HostInvocationID}, Input: *input}
	}
	return nil
}

// prepareInputClaims acquires admission locks in bucket order, before any stream lock.
// SQLite checks ownership only after the caller has handled an existing command receipt.
func (s *Store) prepareInputClaims(ctx context.Context, inputs []agent.HostInput) (func() error, func(), error) {
	noop := func() {}
	if len(inputs) == 0 {
		return func() error { return nil }, noop, nil
	}
	var locked [streamShards]bool
	keys := make([][]byte, 0, len(inputs))
	seen := make(map[string]bool, len(inputs))
	for _, input := range inputs {
		locked[nativeShard(input.Session)] = true
		key := adapterRowID(input.Session, input.AdapterInputID)
		if seen[string(key)] {
			return nil, noop, status_error.ChatLogIdempotencyConflict
		}
		seen[string(key)] = true
		keys = append(keys, key)
	}
	for shard, hold := range locked {
		if hold {
			s.admissions[shard].Lock()
		}
	}
	release := func() {
		for shard := len(locked) - 1; shard >= 0; shard-- {
			if locked[shard] {
				s.admissions[shard].Unlock()
			}
		}
	}
	if err := s.ensureReverseCoverage(ctx); err != nil {
		release()
		return nil, noop, err
	}
	epoch, _ := s.indexes.reverseState()
	check := func() error {
		var row rowIndex
		var changed bool
		err := s.indexes.with(ctx, func(db *gorm.DB) error {
			changed = epoch != s.indexes.epoch
			if changed {
				return nil
			}
			return db.Select("cursor").Where("adapter IS NOT NULL AND adapter IN ?", keys).Limit(1).Find(&row).Error
		})
		if err != nil {
			return err
		}
		if changed {
			return status_error.ChatLogIndexCorrupted
		}
		if row.Cursor != 0 {
			return status_error.ChatLogIdempotencyConflict
		}
		return nil
	}
	return check, release, nil
}
