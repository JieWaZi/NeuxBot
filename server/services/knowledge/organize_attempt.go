package knowledge

import (
	"context"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
)

// organizeAttempt 只拥有当前尝试的取消上下文，不添加上游不存在的整批时间限制。
type organizeAttempt struct {
	attempt int                // 核心持久尝试号。
	ctx     context.Context    // 本次同步模型管线的上下文。
	cancel  context.CancelFunc // 服务关闭或尝试收束时释放真实模型调用。
	done    chan struct{}      // 标记当前驱动已经收束。
	finish  sync.Once          // 同一尝试只结束一次。
}

func (s *Service) attemptContext(job *core.Job, _ models.KnowledgeTaskConfiguration) (*organizeAttempt, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.closing || s.ctx == nil || s.ctx.Err() != nil {
		return nil, status_error.KnowledgeCancelled
	}
	if previous := s.attempts[job.ID]; previous != nil {
		if previous.attempt == job.Attempt {
			return previous, nil
		}
		if previous.attempt > job.Attempt {
			return nil, status_error.KnowledgeCancelled
		}
		previous.finish.Do(func() { close(previous.done); previous.cancel() })
	}
	// 在取消句柄登记锁内重读持久屏障，拒绝在删除取消后才启动的旧驱动。
	current, err := s.config.Engine.GetJob(s.ctx, job.ID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	if current.Attempt != job.Attempt || (current.CancelRequested && !job.CancelRequested) {
		return nil, status_error.KnowledgeCancelled
	}
	ctx, cancel := context.WithCancel(s.ctx)
	attempt := &organizeAttempt{attempt: job.Attempt, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	if s.attempts == nil {
		s.attempts = map[string]*organizeAttempt{}
	}
	s.attempts[job.ID] = attempt
	return attempt, nil
}

func (s *Service) finishAttempt(job string, attempt *organizeAttempt) {
	attempt.finish.Do(func() { close(attempt.done); attempt.cancel() })
	s.mutex.Lock()
	if s.attempts[job] == attempt {
		delete(s.attempts, job)
	}
	s.mutex.Unlock()
}

// StopOrganizeAttempt 收束宿主拥有的模型管线；调用者必须先保存核心取消屏障，等待期间不得持有发布锁。
func (s *Service) StopOrganizeAttempt(ctx context.Context, jobID string) error {
	s.mutex.Lock()
	attempt := s.attempts[jobID]
	if attempt != nil {
		attempt.cancel()
	}
	delete(s.restart, jobID)
	s.mutex.Unlock()
	if attempt == nil {
		return nil
	}
	select {
	case <-attempt.done:
		return nil
	case <-ctx.Done():
		return status_error.KnowledgeCancelled
	}
}
