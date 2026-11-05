package conversation

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	"gorm.io/gorm"
)

// UsageShards is the stable number of append-only canonical native streams.
const UsageShards = streamShards

const usageTerminalIndex = "conversation_usage_terminals"

var usageTerminalPredicate = "kind = " + strconv.Itoa(int(indexNativeTerminal))

// usageTerminalQuery selects a sparse cursor range without visiting intervening fragments.
var usageTerminalQuery = "SELECT cursor, at, loc_offset, loc_length, loc_checksum FROM conversation_positions " +
	"INDEXED BY " + usageTerminalIndex + " WHERE stream = ? AND cursor > ? AND cursor <= ? AND " +
	usageTerminalPredicate + " ORDER BY cursor LIMIT ?"

// UsageTerminal exposes one provider terminal fact without resolving its final text.
type UsageTerminal struct {
	// Cursor is the canonical position inside the named native shard.
	Cursor int64
	// RecordedAt is the durable terminal commit time in Unix milliseconds.
	RecordedAt int64
	// Result contains the terminal evidence and its original attribution.
	Result agent.NativeResult
}

// SetUsageChanged installs the startup-owned notification callback.
func (s *Store) SetUsageChanged(notify func()) {
	if notify == nil {
		s.usageChanged.Store(nil)
	} else {
		s.usageChanged.Store(&notify)
	}
}

func (s *Store) nativeUsageAttribution(ctx context.Context, result agent.NativeResult) *agent.UsageAttribution {
	if len(result.Turn.ConsumedInputIDs) == 0 || slices.Contains(result.Unavailable, acpmeta.ReceiptConsumedInputsUnavailable) {
		return nil
	}
	// One native P may consume several Hs. The first proven input owns P;
	// an unavailable original admission must not assign it to a steer.
	// Admission has already published an exact locator on the normal path.
	// Do not make the terminal write wait for a full reverse-index rebuild.
	id := result.Turn.ConsumedInputIDs[0]
	key := adapterRowID(result.Key.Session, id)
	positions, paths, err := s.indexes.adapterPositions(ctx, [][]byte{key})
	if err != nil || len(positions) != 1 {
		return nil
	}
	found := make(map[string]agent.DurableHostInput, 1)
	if err := s.readAdmissionRows(ctx, result.Key.Session, paths[positions[0].Stream], positions,
		map[string]string{string(key): id}, found); err != nil {
		return nil
	}
	admission, ok := found[id]
	if !ok {
		return nil
	}
	owner := agent.UsageAttribution{ChatID: admission.Ref.ChatID}
	if frozen := admission.Input.Attribution; frozen != nil {
		owner = *frozen
		if admission.Ref.ChatID != "" {
			owner.ChatID = admission.Ref.ChatID
		}
	}
	return &owner
}

// UsageHighWater captures the committed prefix of a canonical native shard.
func (s *Store) UsageHighWater(ctx context.Context, shard int) (int64, error) {
	if shard < 0 || shard >= UsageShards {
		return 0, status_error.BadRequest
	}
	slot := &s.natives[shard]
	slot.mu.Lock()
	defer slot.mu.Unlock()
	idx, err := slot.load(ctx, filepath.Join(s.root, nativeBucketPath(shard)), false)
	if err != nil {
		return 0, err
	}
	return idx.version(), nil
}

// ReadUsageTerminals seeks only terminal positions in a bounded canonical prefix.
// Next covers every position examined, independently of provider or usage availability.
func (s *Store) ReadUsageTerminals(ctx context.Context, shard int, after, through int64) ([]UsageTerminal, int64, error) {
	if shard < 0 || shard >= UsageShards || after < 0 || through < after {
		return nil, after, status_error.BadRequest
	}
	slot := &s.natives[shard]
	slot.mu.Lock()
	defer slot.mu.Unlock()
	path := filepath.Join(s.root, nativeBucketPath(shard))
	idx, err := slot.load(ctx, path, false)
	if err != nil {
		return nil, after, err
	}
	if through > idx.version() {
		return nil, after, status_error.ChatLogCorrupted
	}
	if after == through {
		return nil, through, nil
	}
	var positions []rowIndex
	// The literal matches the disposable partial index predicate.
	err = idx.query(func(db *gorm.DB) error {
		return db.Raw(usageTerminalQuery, idx.coverage.Stream, after, through, indexReadBatch).Scan(&positions).Error
	})
	if err != nil {
		return nil, after, err
	}
	if len(positions) == 0 {
		return nil, through, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, after, status_error.ChatLogError
	}
	defer file.Close()
	rows := make([]UsageTerminal, 0, len(positions))
	for _, pos := range positions {
		if err := ctx.Err(); err != nil {
			return nil, after, err
		}
		row, err := readDiskRow(file, pos.Where)
		if err != nil || row.Terminal == nil {
			return nil, after, status_error.ChatLogCorrupted
		}
		result := row.Terminal.Result
		result.FinalMessage = nil
		rows = append(rows, UsageTerminal{Cursor: pos.Cursor, RecordedAt: pos.At, Result: result})
	}
	next := through
	if len(positions) == indexReadBatch {
		next = positions[len(positions)-1].Cursor
	}
	return rows, next, nil
}
