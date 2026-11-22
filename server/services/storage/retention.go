package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
)

const retentionInterval = 24 * time.Hour

// Policy 是当前闲置会话保留策略。
type Policy struct {
	// Enabled 自动清理
	Enabled bool `json:"enabled"`
	// RetentionDays 保留天数
	RetentionDays int `json:"retentionDays"`
	// Revision 策略版本
	Revision uint64 `json:"revision"`
	// NextCheckAt 下次检查
	NextCheckAt int64 `json:"nextCheckAt,omitempty"`
	// Valid 策略可用
	Valid bool `json:"valid"`
}

type policyFile struct {
	// Policy 是用户设置以及跨重启的下次检查时间。
	Policy Policy `json:"policy"`
}

func readPolicy(path string) (policyFile, bool) {
	state := policyFile{Policy: Policy{RetentionDays: constants.StorageDefaultRetentionDays}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, true
	}
	if err == nil {
		var stored policyFile
		err = json.Unmarshal(data, &stored)
		if err == nil && validDays(stored.Policy.RetentionDays) &&
			((!stored.Policy.Enabled && stored.Policy.NextCheckAt == 0) || (stored.Policy.Enabled && stored.Policy.NextCheckAt > 0)) {
			return stored, true
		}
	}
	logx.Warnf("storage policy is unreadable or invalid: %v", err)
	return state, false
}

func savePolicy(path string, state policyFile) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".storage-policy-")
	if err == nil {
		defer os.Remove(tmp.Name())
		err = json.NewEncoder(tmp).Encode(state)
		if err == nil {
			err = tmp.Sync()
		}
		if closeErr := tmp.Close(); err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(tmp.Name(), path)
		}
	}
	if err != nil {
		logx.Errorf("persist storage policy: %v", err)
		return status_error.StoragePolicyInvalid
	}
	return nil
}

func validDays(days int) bool {
	return days >= 1 && days <= constants.StorageMaximumRetentionDays
}

// Policy 返回当前策略及其可用性。
func (s *Service) Policy() Policy {
	s.mu.Lock()
	defer s.mu.Unlock()
	policy := s.state.Policy
	policy.Valid = s.valid
	return policy
}

// UpdatePolicy 保存策略，影响范围扩大时消费匹配的确认。
func (s *Service) UpdatePolicy(enabled bool, days int, expected uint64, previewID string) error {
	if !validDays(days) {
		return status_error.BadRequest
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.valid {
		return status_error.StoragePolicyInvalid
	}
	if s.closed {
		return status_error.OperationCanceled
	}
	policy := s.state.Policy
	if policy.Revision != expected {
		return status_error.StoragePolicyConflict
	}
	if policy.Enabled == enabled && policy.RetentionDays == days {
		return nil
	}
	if enabled && (!policy.Enabled || days < policy.RetentionDays) {
		record, ok := s.lookupPreview(previewID, previewPolicy)
		if !ok || record.days != days || record.revision != expected {
			return status_error.StoragePreviewExpired
		}
	}

	next := s.state
	if !enabled {
		policy.NextCheckAt = 0
	} else if !policy.Enabled {
		policy.NextCheckAt = s.config.Now().Add(retentionInterval).UnixMilli()
	}
	policy.Enabled, policy.RetentionDays = enabled, days
	policy.Revision++
	next.Policy = policy
	if err := savePolicy(s.policyPath, next); err != nil {
		return err
	}
	s.state = next
	s.retryAfter = time.Time{}
	delete(s.previews, previewID)
	s.changed(true)
	return nil
}

func expirationCutoff(now time.Time, days int) int64 {
	return now.UnixMilli() - int64(days)*retentionInterval.Milliseconds()
}

// ListExpired 返回当前期限下的候选计数及稳定游标页。
func (s *Service) ListExpired(ctx context.Context, days int, cutoff, afterActivity int64, afterID string, limit int) ([]controllers.ChatRetentionCandidate, int64, int64, error) {
	if days == 0 {
		days = s.Policy().RetentionDays
	}
	if !validDays(days) || limit < 1 || limit > constants.StorageCleanupBatchSize {
		return nil, 0, 0, status_error.BadRequest
	}
	if cutoff == 0 {
		cutoff = expirationCutoff(s.config.Now(), days)
	}
	if cutoff > expirationCutoff(s.config.Now(), days) {
		return nil, 0, 0, status_error.BadRequest
	}
	total, err := s.config.Candidates.CountRetentionCandidates(ctx, cutoff)
	if err != nil {
		return nil, 0, 0, err
	}
	rows, err := s.config.Candidates.ListRetentionCandidates(ctx, cutoff, afterActivity, afterID, limit)
	return rows, total, cutoff, err
}

func (s *Service) schedule() {
	for {
		s.mu.Lock()
		policy, valid, idle := s.state.Policy, s.valid, s.current == nil
		var timer *time.Timer
		var tick <-chan time.Time
		if valid && policy.Enabled && idle && s.ctx.Err() == nil {
			deadline := time.UnixMilli(policy.NextCheckAt)
			if s.retryAfter.After(deadline) {
				deadline = s.retryAfter
			}
			delay := deadline.Sub(s.config.Now())
			if delay <= 0 {
				cutoff := expirationCutoff(s.config.Now(), policy.RetentionDays)
				s.beginRetention(preview{revision: policy.Revision, cutoff: cutoff}, true)
			} else {
				// 定期重算墙钟，保证没有桌面唤醒事件的 Web Core 也能推进已到期检查。
				timer = time.NewTimer(min(delay, time.Minute))
				tick = timer.C
			}
		}
		s.mu.Unlock()
		select {
		case <-s.ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-s.wake:
		case <-tick:
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

func (s *Service) beginRetention(request preview, automatic bool) string {
	runID := s.beginRun(previewChats)
	s.bg.Go(func() { s.runRetention(runID, request, automatic) })
	return runID
}

func (s *Service) runRetention(runID string, request preview, automatic bool) {
	var receipt *ExpiredChatCleanupReceipt
	if automatic {
		var lastActivity int64
		var lastID string
		for s.retentionAllowed(request.revision, true) {
			page, err := s.config.Candidates.ListRetentionCandidates(s.ctx, request.cutoff, lastActivity, lastID, constants.StorageCleanupBatchSize)
			if err != nil {
				if s.ctx.Err() == nil {
					logx.Warnf("read expired chats for storage cleanup: %v", err)
				}
				break
			}
			if len(page) == 0 {
				break
			}
			s.removeCandidates(request, page, nil)
			last := page[len(page)-1]
			lastActivity, lastID = last.LastActivityAt, last.ChatID
			if len(page) < constants.StorageCleanupBatchSize {
				break
			}
		}
	} else {
		receipt = &ExpiredChatCleanupReceipt{RunID: runID}
		s.removeCandidates(request, request.chats, receipt)
	}
	s.finishRun(nil, receipt, automatic, request.revision)
}

func (s *Service) retentionAllowed(revision uint64, automatic bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && s.ctx.Err() == nil && s.valid && s.state.Policy.Revision == revision && (!automatic || s.state.Policy.Enabled)
}

func (s *Service) removeCandidates(request preview, candidates []controllers.ChatRetentionCandidate, receipt *ExpiredChatCleanupReceipt) {
	automatic := receipt == nil
	for i, candidate := range candidates {
		if !s.retentionAllowed(request.revision, automatic) {
			if receipt != nil {
				receipt.Skipped += len(candidates) - i
			}
			return
		}
		admitted := func(context.Context, string) (bool, error) {
			return s.retentionAllowed(request.revision, automatic), nil
		}
		deleted, err := s.config.DeleteExpired(context.WithoutCancel(s.ctx), candidate, request.cutoff, admitted)
		if err != nil {
			logx.Warnf("clean up expired chat %s: %v", candidate.ChatID, err)
		}
		if receipt == nil {
			continue
		}
		switch {
		case deleted:
			receipt.Deleted++
			if err != nil {
				receipt.Pending++
			}
		case err != nil:
			receipt.Failed++
		default:
			receipt.Skipped++
		}
	}
}
