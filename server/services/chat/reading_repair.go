package chat

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

const readingRepairLimit = 256

// readingRepair 记录原会话投影尚未覆盖的会话。
type readingRepair struct {
	// mu 保护待修复及隔离登记。
	mu sync.Mutex
	// pending 保存最多 256 个会话的当前修复登记。
	pending map[string]readingRepairAttempt
	// isolated 保存需显式修复的损坏日志身份。
	isolated map[string]bool
	// next 隔离早先修复与后来登记的故障。
	next uint64
	// overflow 表示需要下一轮原日志目录核对。
	overflow bool
	// scanning 表示当前正在核对原日志目录。
	scanning bool
	// wake 通知唯一修复循环而不阻塞业务写入。
	wake chan struct{}
	// cancel 在 ConversationStore 关闭前取消修复循环。
	cancel context.CancelFunc
	// done 在修复循环退出时关闭。
	done chan struct{}
}

type readingRepairAttempt struct {
	// generation 隔离后来提交的权威写入。
	generation uint64
	// failed 表示实际投影失败或已进入隔离修复。
	failed bool
}

func newReadingRepair() *readingRepair {
	return &readingRepair{pending: map[string]readingRepairAttempt{}, isolated: map[string]bool{}, wake: make(chan struct{}, 1)}
}

func (m *service) startReadingRepair(ctx context.Context) {
	r := m.readingRepair
	if r == nil || r.cancel != nil {
		return
	}
	ctx, r.cancel = context.WithCancel(ctx)
	r.done = make(chan struct{})
	go func() {
		defer close(r.done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-r.wake:
			}
			backoff := time.Second
			timer := time.NewTimer(backoff)
			for {
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				pending, failed := m.repairReadingPass(ctx)
				if !pending {
					break
				}
				if failed {
					backoff = min(30*time.Second, 2*backoff)
				} else {
					backoff = time.Second
				}
				timer.Reset(backoff)
			}
		}
	}()
}

func (m *service) registerReadingFailure(chatID string, _ error) {
	m.enqueueReadingRepair(chatID, true)
}

func (m *service) enqueueReadingRepair(chatID string, invalidate bool) {
	r := m.readingRepair
	if r == nil {
		return
	}
	r.mu.Lock()
	previous, exists := r.pending[chatID]
	notify := invalidate && !previous.failed
	resource := chatID
	if exists || len(r.pending) < readingRepairLimit {
		r.next++
		r.pending[chatID] = readingRepairAttempt{generation: r.next, failed: invalidate || previous.failed}
	} else {
		resource = ""
		notify = !r.overflow && !r.scanning
		r.overflow = true
	}
	r.mu.Unlock()
	if notify {
		m.queryIndexes.reading.ProjectionUnavailable(context.Background(), resource)
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (m *service) repairReadingPass(ctx context.Context) (bool, bool) {
	r := m.readingRepair
	r.mu.Lock()
	attempts := make(map[string]readingRepairAttempt, len(r.pending))
	for id, generation := range r.pending {
		attempts[id] = generation
	}
	scan := r.overflow
	if scan {
		r.overflow = false
		r.scanning = true
	}
	r.mu.Unlock()
	failed := false
	for id, attempt := range attempts {
		if ctx.Err() != nil {
			return false, false
		}
		err := m.catchUpReadingProjection(ctx, id)
		if !r.recordOutcome(m, ctx, id, attempt.generation, err) {
			failed = true
			m.registerReadingFailure(id, err)
		}
	}
	if scan {
		ids, err := m.logStore.ListChatIDs()
		if err == nil {
			for start := 0; start < len(ids) && ctx.Err() == nil; start += 128 {
				for _, id := range ids[start:min(start+128, len(ids))] {
					r.mu.Lock()
					generation := r.pending[id].generation
					isolated := r.isolated[id]
					r.mu.Unlock()
					if isolated {
						continue
					}
					err = m.catchUpReadingProjection(ctx, id)
					if !r.recordOutcome(m, ctx, id, generation, err) {
						failed = true
						m.registerReadingFailure(id, err)
					}
				}
			}
		}
		r.mu.Lock()
		r.scanning = false
		if err != nil && len(ids) == 0 {
			r.overflow = true
			failed = true
		}
		settled := !r.overflow
		for _, attempt := range r.pending {
			if attempt.failed {
				settled = false
				break
			}
		}
		r.mu.Unlock()
		if settled {
			m.queryIndexes.reading.ProjectionRecovered(context.WithoutCancel(ctx), "")
		}

	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pending) > 0 || r.overflow, failed
}

func (r *readingRepair) recordOutcome(m *service, ctx context.Context, id string, generation uint64, err error) bool {
	isolated := hasStatusErrorCode(err, status_error.ChatLogCorrupted.Code()) || hasStatusErrorCode(err, status_error.ChatLogIndexCorrupted.Code())
	deleted := hasStatusErrorCode(err, status_error.ChatNotFound.Code())
	if err != nil && !isolated && !deleted {
		return false
	}
	r.mu.Lock()
	if r.pending[id].generation != generation {
		r.mu.Unlock()
		return true
	}
	previous := r.pending[id]
	delete(r.pending, id)
	changed := previous.failed || r.isolated[id] != isolated
	if isolated {
		r.isolated[id] = true
	} else {
		delete(r.isolated, id)
	}
	r.mu.Unlock()
	if changed {
		m.queryIndexes.reading.ProjectionRecovered(context.WithoutCancel(ctx), id)
	}
	return true
}

func (m *service) catchUpReadingProjection(ctx context.Context, chatID string) error {
	err := m.catchUpLiveProjection(ctx, chatID)
	if err == nil && m.queryIndexes.reading != nil {
		isolated := false
		if m.readingRepair != nil {
			m.readingRepair.mu.Lock()
			isolated = m.readingRepair.isolated[chatID]
			m.readingRepair.mu.Unlock()
		}
		hasCursor, readErr := m.queryIndexes.reading.HasCursor(ctx, constants.LocalReaderID, chatID)
		if readErr != nil {
			return readErr
		}
		if hasCursor && !isolated {
			return nil
		}
		return m.repairExistingReading(ctx, chatID, hasCursor)
	}
	if !hasStatusErrorCode(err, status_error.ChatNotFound.Code()) {
		return err
	}
	release, err := chatstorage.AcquireOperation(m.storage.Root(), chatID)
	if err != nil {
		return err
	}
	defer release()
	latest, err := m.logStore.ReadLatestSequence(chatID)
	if err != nil {
		if hasStatusErrorCode(err, status_error.ChatNotFound.Code()) {
			// 删除持有同一操作锁。仍存在的投影表示权威日志丢失。
			_, missing, inspectErr := m.queryIndexes.inspectChat(ctx, chatID)
			if inspectErr != nil {
				return inspectErr
			}
			if !missing {
				return status_error.ChatLogCorrupted
			}
		}
		return err
	}
	records, err := m.logStore.ReadAfter(chatID, 0, int(latest))
	if err != nil {
		return err
	}
	state, err := replayChatState(records)
	if err != nil {
		return err
	}
	err = m.queryIndexes.createChatsIfMissing(ctx, []models.Chat{state.chat}, true)
	if hasStatusErrorCode(err, status_error.Conflict.Code()) {
		return m.catchUpLiveProjection(ctx, chatID)
	}
	return err
}

// repairExistingReading validates an isolated log and atomically creates any missing baseline.
func (m *service) repairExistingReading(ctx context.Context, chatID string, hasCursor bool) error {
	indexed, err := m.queryIndexes.chatController.Get(ctx, chatID)
	if err != nil {
		return err
	}
	latest, err := m.logStore.ReadLatestSequence(chatID)
	if err != nil {
		return err
	}
	if indexed.LatestSequence > latest {
		return status_error.ChatLogCorrupted
	}
	if indexed.LatestSequence != latest {
		return status_error.ReadingProjectionIncomplete
	}
	if valid, err := m.recovery.staticProjectionMatches(chatID, indexed); err != nil {
		return err
	} else if !valid {
		return status_error.ChatLogIndexCorrupted
	}
	if hasCursor {
		return nil
	}
	rows, err := m.logStore.ReadAfter(chatID, 0, int(latest))
	if err != nil {
		return err
	}
	changes, err := m.queryIndexes.prepareReading(ctx, chatID, rows, latest)
	if err != nil {
		return err
	}
	return m.queryIndexes.transactionController.Transaction(ctx, func(tx context.Context) error {
		exists, err := m.queryIndexes.reading.HasCursor(tx, constants.LocalReaderID, chatID)
		if err != nil || exists {
			return err
		}
		initial := int64(0)
		latestReadable, err := m.queryIndexes.projectReadableMessages(tx, chatID, changes, true, &initial)
		if err != nil {
			return err
		}
		if err := m.queryIndexes.chatController.AdvanceProjection(tx, chatID, latest, map[string]any{"latest_readable_sequence": latestReadable, "updated_at": indexed.UpdatedAt}); err != nil {
			return err
		}
		m.queryIndexes.reading.ProjectionChanged(tx, chatID)
		return nil
	})
}

// ReadingIncompleteReason 返回指定会话范围的投影故障原因。
// nil 表示整个工作区。
func (m *service) ReadingIncompleteReason(chatIDs []string) string {
	r := m.readingRepair
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.overflow || r.scanning {
		return "recovering"
	}
	if chatIDs == nil {
		for _, p := range r.pending {
			if p.failed {
				return "recovering"
			}
		}
		if len(r.isolated) > 0 {
			return "isolated"
		}
		return ""
	}
	reason := ""
	for _, id := range chatIDs {
		if r.pending[id].failed {
			return "recovering"
		}
		if r.isolated[id] {
			reason = "isolated"
		}
	}
	return reason
}

// IsolatedReadingIDs 返回暂不能核验原日志的会话身份。
func (m *service) IsolatedReadingIDs() []string {
	r := m.readingRepair
	if r == nil {
		return nil
	}
	r.mu.Lock()
	ids := make([]string, 0, len(r.isolated))
	for id := range r.isolated {
		ids = append(ids, id)
	}
	r.mu.Unlock()
	slices.Sort(ids)
	return ids
}

// InitializeReadingBaseline 在写入新中断结果之前建立历史基线。
func (m *service) InitializeReadingBaseline(ctx context.Context) error {
	ids, err := m.logStore.ListChatIDs()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		latest, err := m.logStore.ReadLatestSequence(id)
		if err == nil {
			_, err = m.recovery.rebuildProjection(ctx, id, RecoveryDiagnostic{ChatID: id, LogLatestSequence: latest}, false, true)
		}
		if err != nil {
			if infrastructureReadingError(err) {
				return err
			}
			m.readingRepair.mu.Lock()
			m.readingRepair.isolated[id] = true
			m.readingRepair.mu.Unlock()
		}
	}
	return nil
}

func infrastructureReadingError(err error) bool {
	return hasStatusErrorCode(err, status_error.DatabaseError.Code()) || hasStatusErrorCode(err, status_error.DatabaseBusy.Code()) || hasStatusErrorCode(err, status_error.DatabaseOperationCancelled.Code())
}

// observeReadingCheckpoint advances a durable native checkpoint without changing Runtime admission.
func (m *service) observeReadingCheckpoint(chatID string) {
	if m.readingRepair == nil {
		return
	}
	// Coalesce checkpoints on the projection owner's one timer; the native delivery lock never loads growing bodies.
	m.enqueueReadingRepair(chatID, false)
}

// readingIsolated identifies an original startup diagnostic that must stay visible to readers.
func readingIsolated(diagnostic RecoveryDiagnostic) bool {
	return diagnostic.State == constants.ChatRecoveryStateIsolated
}

// forgetDeletedReading retires failures only after an actual aggregate deletion commits.
func (m *service) forgetDeletedReading(id string) {
	r := m.readingRepair
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.pending, id)
	delete(r.isolated, id)
	r.mu.Unlock()
}
