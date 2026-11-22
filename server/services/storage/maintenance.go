package storage

import (
	"context"
	"slices"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
)

const (
	previewLifetime = 5 * time.Minute
	maximumPreviews = 128
)

type previewKind string

const (
	previewPolicy    previewKind = "policy"
	previewChats     previewKind = "chats"
	previewResources previewKind = "resources"
)

type preview struct {
	// kind 区分确认的真实用途。
	kind previewKind
	// expiresAt 限制确认可重放时间。
	expiresAt time.Time
	// days 是用户实际预览的保留天数。
	days int
	// cutoff 是确认后仍维持的匹配截止点。
	cutoff int64
	// revision 指向签发时的用户策略。
	revision uint64
	// chats 是已确认的候选身份和日志序号。
	chats []controllers.ChatRetentionCandidate
	// resources 只保存固定可用类别。
	resources []constants.StorageCleanupKind
}

// ExpirationPreview 是一次策略或手动清理的影响确认。
type ExpirationPreview struct {
	// ID 预览标识
	ID string `json:"id"`
	// Total 候选总数
	Total int64 `json:"total"`
	// KnownBytes 已知大小
	KnownBytes int64 `json:"knownBytes"`
	// Complete 大小完整
	Complete bool `json:"complete"`
	// ExpiresAt 失效时间
	ExpiresAt int64 `json:"expiresAt"`
}

// CleanupItem 是一个所属资源的预览和可操作性。
type CleanupItem struct {
	// Category 清理类型
	Category constants.StorageCleanupKind `json:"category"`
	// Bytes 已知大小
	Bytes int64 `json:"bytes"`
	// Available 可以清理
	Available bool `json:"available"`
	// Complete 大小完整
	Complete bool `json:"complete"`
}

// CleanupPreview 是固定类型的清理确认。
type CleanupPreview struct {
	// ID 预览标识
	ID string `json:"id"`
	// Items 清理项目
	Items []CleanupItem `json:"items"`
	// ExpiresAt 失效时间
	ExpiresAt int64 `json:"expiresAt"`
}

type activeRun struct {
	// id 标识本次运行及其接纳回执。
	id string
	// kind 标识确认所属的清理类别。
	kind previewKind
}

// ResourceCleanupReceipt 保存本进程内最近一次资源清理的分项回执。
type ResourceCleanupReceipt struct {
	// RunID 对应已接纳的清理任务。
	RunID string `json:"runId"`
	// Items 分项清理结果。
	Items []CleanupResult `json:"items"`
}

// ExpiredChatCleanupReceipt 对账手动提交的过期会话清理。
type ExpiredChatCleanupReceipt struct {
	// RunID 对应已接纳的清理任务。
	RunID string `json:"runId"`
	// Deleted 是已删除的会话数。
	Deleted int `json:"deleted"`
	// Skipped 是未满足删除条件的会话数。
	Skipped int `json:"skipped"`
	// Failed 是删除失败的会话数。
	Failed int `json:"failed"`
	// Pending 是会话已删除但文件仍待回收的数量。
	Pending int `json:"pending"`
}

// CleanupResult 返回一种受管资源的实际清理结果。
type CleanupResult struct {
	// Category 清理类型
	Category constants.StorageCleanupKind `json:"category"`
	// RemovedBytes 移除大小
	RemovedBytes int64 `json:"removedBytes"`
	// Failed 执行失败
	Failed bool `json:"failed"`
}

// MaintenanceState 提供清理互斥状态与资源清理对账回执。
type MaintenanceState struct {
	// CurrentRunID 当前维护任务。
	CurrentRunID string `json:"currentRunId,omitempty"`
	// Cleanup 资源清理回执。
	Cleanup *ResourceCleanupReceipt `json:"cleanup,omitempty"`
	// ExpiredChats 手动过期会话清理回执。
	ExpiredChats *ExpiredChatCleanupReceipt `json:"expiredChats,omitempty"`
}

// Maintenance 返回正在运行的维护和用于核对资源清理的回执。
func (s *Service) Maintenance() MaintenanceState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := MaintenanceState{Cleanup: s.completedCleanup, ExpiredChats: s.completedExpiredChats}
	if s.current != nil {
		state.CurrentRunID = s.current.id
	}
	return state
}

func (s *Service) lookupPreview(id string, kind previewKind) (preview, bool) {
	saved, exists := s.previews[id]
	return saved, exists && saved.kind == kind && s.config.Now().Before(saved.expiresAt)
}

func (s *Service) storePreview(value preview) (string, int64) {
	now := s.config.Now()
	var oldestID string
	var oldest time.Time
	for id, saved := range s.previews {
		if !now.Before(saved.expiresAt) {
			delete(s.previews, id)
			continue
		}
		if oldestID == "" || saved.expiresAt.Before(oldest) {
			oldestID, oldest = id, saved.expiresAt
		}
	}
	if len(s.previews) == maximumPreviews {
		delete(s.previews, oldestID)
	}
	id := util.New()
	value.expiresAt = now.Add(previewLifetime)
	s.previews[id] = value
	return id, value.expiresAt.UnixMilli()
}

// PreviewExpiration 核对拟保存策略或选定会话，生成一次确认。
func (s *Service) PreviewExpiration(ctx context.Context, days int, ids []string) (ExpirationPreview, error) {
	base := s.Policy()
	if !base.Valid {
		return ExpirationPreview{}, status_error.StoragePolicyInvalid
	}
	if !validDays(days) || len(ids) > constants.StorageCleanupBatchSize {
		return ExpirationPreview{}, status_error.BadRequest
	}
	value := preview{kind: previewPolicy, days: days, cutoff: expirationCutoff(s.config.Now(), days), revision: base.Revision}
	var rows []controllers.ChatRetentionCandidate
	var total int64
	var err error
	if len(ids) != 0 {
		if days != base.RetentionDays {
			return ExpirationPreview{}, status_error.StoragePolicyConflict
		}
		value.kind = previewChats
		rows, err = s.config.Candidates.ListRetentionCandidatesByIDs(ctx, value.cutoff, ids)
		value.chats = rows
		total = int64(len(rows))
		if err == nil && len(rows) != len(ids) {
			return ExpirationPreview{}, status_error.StoragePreviewExpired
		}
	} else {
		rows, total, _, err = s.ListExpired(ctx, days, value.cutoff, 0, "", constants.StorageCleanupBatchSize)
	}
	if err != nil {
		return ExpirationPreview{}, err
	}
	result := ExpirationPreview{Total: total, Complete: total == 0}
	if snapshot, exists := s.CachedUsage(); exists {
		result.Complete = snapshot.Measurement.Complete && int64(len(rows)) == total
		for _, row := range rows {
			private, measured := snapshot.Chats[row.ChatID]
			result.KnownBytes += private.Bytes
			result.Complete = result.Complete && measured
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ExpirationPreview{}, status_error.OperationCanceled
	}
	if s.state.Policy.Revision != base.Revision {
		return ExpirationPreview{}, status_error.StoragePolicyConflict
	}
	result.ID, result.ExpiresAt = s.storePreview(value)
	return result, nil
}

// StartExpiredCleanup 接纳已确认的过期会话身份。
func (s *Service) StartExpiredCleanup(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", status_error.OperationCanceled
	}
	if !s.valid {
		return "", status_error.StoragePolicyInvalid
	}
	if s.current != nil && s.current.kind == previewChats && id != "" && s.currentPreviewID == id {
		return s.current.id, nil
	}
	saved, valid := s.lookupPreview(id, previewChats)
	if !valid || saved.revision != s.state.Policy.Revision {
		return "", status_error.StoragePreviewExpired
	}
	if s.current != nil {
		return "", status_error.StorageMaintenanceBusy
	}
	delete(s.previews, id)
	s.currentPreviewID = id
	return s.beginRetention(saved, false), nil
}

// PreviewCleanup 核对所有者支持与当前可回收范围。
func (s *Service) PreviewCleanup(ctx context.Context, kinds []constants.StorageCleanupKind) (CleanupPreview, error) {
	if len(kinds) == 0 {
		return CleanupPreview{}, status_error.BadRequest
	}
	result := CleanupPreview{Items: make([]CleanupItem, 0, len(kinds))}
	value := preview{kind: previewResources}
	seen := make([]constants.StorageCleanupKind, 0, len(kinds))
	for _, kind := range kinds {
		if kind.String() == "" || slices.Contains(seen, kind) {
			return CleanupPreview{}, status_error.BadRequest
		}
		seen = append(seen, kind)
		item := CleanupItem{Category: kind, Complete: true}
		if owner, ok := s.config.Cleanup[kind]; ok {
			bytes, err := owner.Preview(ctx)
			if ctx.Err() != nil {
				return CleanupPreview{}, status_error.OperationCanceled
			}
			item.Bytes, item.Available, item.Complete = bytes, err == nil || bytes > 0, err == nil
			if bytes > 0 {
				value.resources = append(value.resources, kind)
			}
		}
		result.Items = append(result.Items, item)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return CleanupPreview{}, status_error.OperationCanceled
	}
	if len(value.resources) > 0 {
		result.ID, result.ExpiresAt = s.storePreview(value)
	}
	return result, nil
}

// StartCleanup 接纳固定类别的确认并交给实际资源所有者。
func (s *Service) StartCleanup(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", status_error.OperationCanceled
	}
	if s.current != nil && s.current.kind == previewResources && s.currentPreviewID == id {
		return s.current.id, nil
	}
	value, valid := s.lookupPreview(id, previewResources)
	if !valid {
		return "", status_error.StoragePreviewExpired
	}
	if s.current != nil {
		return "", status_error.StorageMaintenanceBusy
	}
	delete(s.previews, id)
	s.currentPreviewID = id
	runID := s.beginRun(previewResources)
	s.bg.Go(func() {
		receipt := &ResourceCleanupReceipt{RunID: runID, Items: make([]CleanupResult, 0, len(value.resources))}
		for _, kind := range value.resources {
			if s.ctx.Err() != nil {
				break
			}
			bytes, err := s.config.Cleanup[kind].Remove(s.ctx)
			receipt.Items = append(receipt.Items, CleanupResult{Category: kind, RemovedBytes: bytes, Failed: err != nil})
		}
		s.finishRun(receipt, nil, false, 0)
	})
	return runID, nil
}

func (s *Service) beginRun(kind previewKind) string {
	id := util.New()
	s.current = &activeRun{id: id, kind: kind}
	s.changed(false)
	return id
}

func (s *Service) finishRun(receipt *ResourceCleanupReceipt, expiredChats *ExpiredChatCleanupReceipt, automatic bool, policyRevision uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = nil
	s.currentPreviewID = ""
	if receipt != nil {
		s.completedCleanup = receipt
	}
	if expiredChats != nil {
		s.completedExpiredChats = expiredChats
	}
	next := s.state
	if automatic && s.valid && next.Policy.Enabled && next.Policy.Revision == policyRevision && s.ctx.Err() == nil {
		next.Policy.NextCheckAt = s.config.Now().Add(retentionInterval).UnixMilli()
		if err := savePolicy(s.policyPath, next); err != nil {
			s.retryAfter = s.config.Now().Add(time.Hour)
		} else {
			s.state = next
			s.retryAfter = time.Time{}
		}
	}
	s.changed(true)
}
