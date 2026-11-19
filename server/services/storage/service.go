// Package storage 装配存储快照、预览与手动和定期维护。
package storage

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	filesystem "github.com/JieWaZi/NeuxBot/server/internal/storage"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// RetentionCandidates 提供已核验的 Chat 活动投影。
type RetentionCandidates interface {
	// ListRetentionCandidates 读取当前可证明闲置的会话页。
	ListRetentionCandidates(context.Context, int64, int64, string, int) ([]controllers.ChatRetentionCandidate, error)
	// CountRetentionCandidates 统计同一筛选口径下的候选。
	CountRetentionCandidates(context.Context, int64) (int64, error)
	// ListRetentionCandidatesByIDs 重新核验用户选中的会话身份。
	ListRetentionCandidatesByIDs(context.Context, int64, []string) ([]controllers.ChatRetentionCandidate, error)
}

// DeleteExpiredChat 在 Chat 排他操作内重验并删除过期会话。
type DeleteExpiredChat func(context.Context, controllers.ChatRetentionCandidate, int64, func(context.Context, string) (bool, error)) (bool, error)

// CleanupOwner 持有某一种资源清理的权限和并发生命周期。
type CleanupOwner struct {
	// Preview 只读取当前可移除的小计。
	Preview func(context.Context) (int64, error)
	// Remove 重新核验并回收当前可移除的内容。
	Remove func(context.Context) (ResourceCleanup, error)
}

// ResourceCleanup 记录一次所有者回收中确实移除的大小和保护项目。
type ResourceCleanup struct {
	// Bytes 是已移除文件的逻辑字节。
	Bytes int64
	// Skipped 是由于归属或引用保护而保留的项目数。
	Skipped int
}

// Config 装配两个受管根及业务所有者。
type Config struct {
	// Roots 是本 Core 实际管理的两个目录。
	Roots filesystem.Roots
	// PolicyDirectory 是策略所在的应用数据目录。
	PolicyDirectory string
	// Candidates 是 Chat 候选查询投影。
	Candidates RetentionCandidates
	// LookupChats 补齐同一快照的有限会话归属与标题。
	LookupChats func(context.Context, []string) ([]models.Chat, error)
	// DeleteExpired 是 Chat 拥有的条件删除。
	DeleteExpired DeleteExpiredChat
	// Cleanup 是有权限执行的固定类别。
	Cleanup map[constants.StorageCleanupKind]CleanupOwner
	// CheckFileIdle 核对原执行所有者的占用。调用者先持有伙伴和会话操作门。
	CheckFileIdle func(context.Context, string, string) error
	// InheritedAttachments 为旧分叉提供权威附件归属证明。
	InheritedAttachments filesystem.InheritedAttachments
	// Events 是唯一 Core 事件发布方。
	Events events.Publisher
	// Now 读取 Core 当前时间。
	Now func() time.Time
	// Measure 是有界磁盘测量入口。
	Measure func(context.Context, filesystem.Roots, filesystem.Limits) (filesystem.Measurement, error)
}

// Service 在一个 Core 生命周期内拥有存储维护。
type Service struct {
	// config 持有不可变装配。
	config Config
	// ctx 管理 Core 所有的在途操作。
	ctx context.Context
	// cancel 通知后台任务进入结束边界。
	cancel context.CancelFunc
	// mu 保护策略、确认身份、运行和扫描发布。
	mu sync.Mutex
	// bg 只登记接受过的后台操作。
	bg sync.WaitGroup
	// closed 拒绝接受新的后台任务。
	closed bool
	// scheduled 表示已经启动单个调度循环。
	scheduled bool
	// wake 通知调度循环重新读取策略或运行席位。
	wake chan struct{}
	// policyPath 是策略与调度的唯一文件。
	policyPath string
	// retryAfter 避免维护结果无法落盘时立即重复执行到期检查。
	retryAfter time.Time
	// state 是当前已持久化的策略和最近结果。
	state policyFile
	// valid 表示当前策略文件已核验。
	valid bool
	// previews 保存本进程内有界的确认身份。
	previews map[string]preview
	// current 是当前维护，最多一项。
	current *Run
	// currentPreviewID 允许正在运行时重放同一个已接纳确认。
	currentPreviewID string
	// revision 是当前进程的维护事件顺序。
	revision uint64
	// generation 在清理和重新测量后淘汰旧读取。
	generation uint64
	// snapshot 是最近一次可共享的测量。
	snapshot *Snapshot
	// scan 是当前代次的单个在途测量。
	scan *scan
}

// New 读取本机策略，等待领域恢复完成后由装配方启动调度。
func New(ctx context.Context, config Config) *Service {
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Measure == nil {
		config.Measure = filesystem.Measure
	}
	if config.PolicyDirectory == "" {
		config.PolicyDirectory = config.Roots.Data
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Service{config: config, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1),
		policyPath: filepath.Join(config.PolicyDirectory, constants.StoragePolicyFileName), previews: make(map[string]preview)}
	s.state, s.valid = readPolicy(s.policyPath)
	return s
}

// Start 启动已恢复 Core 的保留调度。
func (s *Service) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.scheduled {
		return
	}
	s.scheduled = true
	s.bg.Go(s.schedule)
}

// Close 停止新工作，等待已接纳的维护和扫描退出。
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.bg.Wait()
}

func (s *Service) changed(invalidate bool) {
	if invalidate {
		s.generation++
		s.snapshot = nil
		if s.scan != nil {
			s.scan.cancel()
			s.scan = nil
		}
	}
	s.revision++
	_ = events.Emit(s.config.Events, "storage", "storage.changed", events.Resource{Kind: "global"}, Change{
		Revision: s.revision, Generation: s.generation,
	})
	s.Wake()
}

// Wake 让已运行的调度器按墙钟重算下一次检查。
func (s *Service) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Change 通知消费者读取当前维护和快照。
type Change struct {
	// Revision 是本进程内的维护顺序。
	Revision uint64 `json:"revision"`
	// Generation 标识统计快照当前代次。
	Generation uint64 `json:"generation"`
}
