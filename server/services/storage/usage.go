package storage

import (
	"context"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	filesystem "github.com/JieWaZi/NeuxBot/server/internal/storage"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

const scanFreshness = time.Minute

// Snapshot 记录同一代目录测量，供总量、排序和预览共用。
type Snapshot struct {
	// Generation 是测量的稳定代次。
	Generation uint64
	// MeasuredAt 是统计完成时间。
	MeasuredAt time.Time
	// Measurement 是可包含已知小计的实际结果。
	Measurement filesystem.Measurement
	// Chats 按 Chat 身份汇总私有文件。
	Chats map[string]PrivateUsage
	// ChatMetadata 记录同一测量代次的展示身份。
	ChatMetadata []models.Chat
	// ChatMetadataError 描述目录测量成功但实体归属不可读。
	ChatMetadataError error
}

// PrivateUsage 汇总一个 Chat 的私有文件字节。
type PrivateUsage struct {
	// Bytes 是可归属到该 Chat 的逻辑字节。
	Bytes int64
	// FileCount 是可归属的常规文件数量。
	FileCount int
	// Folders 按绝对目录汇总已测量字节。
	Folders map[string]int64
}

type scan struct {
	// ctx 使旧代扫描在所有权变化时停止。
	ctx context.Context
	// cancel 释放该扫描拥有的路径和查询工作。
	cancel context.CancelFunc
	// done 关闭之后允许消费者读取同一个结果。
	done chan struct{}
	// generation 确保写入中断后原扫描不能回填。
	generation uint64
	// snapshot 是此次扫描结果。
	snapshot Snapshot
	// err 是测量无法完成时的错误。
	err error
}

// Usage 共享同范围扫描，等待者自行取消并保留其他消费者。
func (s *Service) Usage(ctx context.Context, force bool) (Snapshot, error) {
	for {
		s.mu.Lock()
		if s.closed || s.ctx.Err() != nil || ctx.Err() != nil {
			s.mu.Unlock()
			return Snapshot{}, status_error.OperationCanceled
		}
		now := s.config.Now()
		if !force && s.snapshot != nil && now.Sub(s.snapshot.MeasuredAt) < scanFreshness {
			snapshot := *s.snapshot
			s.mu.Unlock()
			return snapshot, nil
		}
		if s.scan == nil {
			s.generation++
			scanContext, cancel := context.WithCancel(s.ctx)
			work := &scan{ctx: scanContext, cancel: cancel, done: make(chan struct{}), generation: s.generation}
			s.scan = work
			s.bg.Go(func() { s.measure(work) })
		}
		work := s.scan
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return Snapshot{}, status_error.OperationCanceled
		case <-s.ctx.Done():
			return Snapshot{}, status_error.OperationCanceled
		case <-work.done:
		}
		s.mu.Lock()
		current := work.generation == s.generation
		s.mu.Unlock()
		if current {
			return work.snapshot, work.err
		}
		force = false
	}
}

func (s *Service) measure(work *scan) {
	defer work.cancel()
	measured, err := s.config.Measure(work.ctx, s.config.Roots, filesystem.Limits{
		MaxEntries: constants.StorageScanMaximumEntries,
		MaxDepth:   constants.StorageScanMaximumDepth,
		Timeout:    constants.StorageScanMaximumSeconds * time.Second,
	})
	chats := make(map[string]PrivateUsage)
	for _, entry := range measured.Entries {
		if entry.ChatID == "" {
			continue
		}
		used := chats[entry.ChatID]
		if used.Folders == nil {
			used.Folders = make(map[string]int64)
		}
		used.Bytes += entry.Bytes
		used.FileCount++
		if entry.Folder != "" {
			used.Folders[entry.Folder] += entry.Bytes
		}
		chats[entry.ChatID] = used
	}
	work.snapshot = Snapshot{Generation: work.generation, MeasuredAt: s.config.Now(), Measurement: measured, Chats: chats}
	if err == nil && len(chats) > 0 && s.config.LookupChats != nil {
		ids := make([]string, 0, len(chats))
		for id := range chats {
			ids = append(ids, id)
		}
		work.snapshot.ChatMetadata, work.snapshot.ChatMetadataError = s.config.LookupChats(work.ctx, ids)
	}
	work.err = err
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scan == work {
		if err == nil {
			s.snapshot = &work.snapshot
		}
		s.scan = nil
	}
	close(work.done)
}

// CachedUsage 返回已经测量的当前快照，用于不触发目录扫描的预览。
func (s *Service) CachedUsage() (Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshot == nil {
		return Snapshot{}, false
	}
	return *s.snapshot, true
}

// Invalidate 使所有者的删除等变更淘汰已有快照。
func (s *Service) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.changed(true)
	}
}
