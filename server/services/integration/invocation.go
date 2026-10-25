package integration

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"slices"
	"sync"
)

// AcquireInvocationGuard 只按原 AutomaticRun 恢复调用主体和任务知识范围，不要求直接知识授权。
func (s *Service) AcquireInvocationGuard(ctx context.Context, runID string) (func(), error) {
	if s == nil || s.config.Tasks == nil || runID == "" {
		return nil, status_error.IntegrationForbidden
	}
	s.policy.RLock()
	return s.acquireInvocationGuardLocked(ctx, runID)
}

// acquireInvocationGuardLocked 在调用方持有唯一授权读锁时验证自动任务。
func (s *Service) acquireInvocationGuardLocked(ctx context.Context, runID string) (func(), error) {
	fail := func() (func(), error) { s.policy.RUnlock(); return nil, status_error.IntegrationForbidden }
	run, err := s.config.Tasks.GetRun(ctx, runID)
	if err != nil {
		return fail()
	}
	if run.IntegrationInvocation == nil {
		// 定时与本机手动工作没有合成的集成 Key 依赖。
		var once sync.Once
		return func() { once.Do(s.policy.RUnlock) }, nil
	}
	invocation := run.IntegrationInvocation
	if invocation.TaskID != run.TaskID || run.Source != constants.AutomaticRunSourceAPI {
		return fail()
	}
	if _, err = s.authorize(ctx, invocation.Identity, constants.IntegrationResourceKindAutomaticTask, run.TaskID, []constants.IntegrationCapability{constants.IntegrationCapabilityTaskInvoke}); err != nil {
		return fail()
	}
	task, err := s.config.Tasks.Get(ctx, run.TaskID)
	if err != nil || !task.Enabled || task.Deleting || task.Trigger != constants.AutomaticTaskTriggerAPI || !coversKnowledge(task.Knowledge, run.Configuration.Knowledge) {
		return fail()
	}
	var once sync.Once
	return func() { once.Do(s.policy.RUnlock) }, nil
}

// coversKnowledge 只允许当前任务继续覆盖原冻结授权；改名或未来模型选择不构成撤权。
func coversKnowledge(current, frozen *models.KnowledgeMaintenanceAuthorization) bool {
	if current == nil || frozen == nil || frozen.Backend != "local" || frozen.ConnectionID != "local" || frozen.KnowledgeBaseID == "" || frozen.Operation != "organize" || current.Backend != frozen.Backend || current.ConnectionID != frozen.ConnectionID || current.KnowledgeBaseID != frozen.KnowledgeBaseID || current.Operation != frozen.Operation {
		return false
	}
	// 空来源原本表示在接纳时冻结 pending；当前收窄到显式来源时不能重解释该原授权。
	if len(current.SourceVersionIDs) > 0 {
		if len(frozen.SourceVersionIDs) == 0 {
			return false
		}
		for _, id := range frozen.SourceVersionIDs {
			if !slices.Contains(current.SourceVersionIDs, id) {
				return false
			}
		}
	}
	for _, index := range frozen.AttachmentIndexes {
		if !slices.Contains(current.AttachmentIndexes, index) {
			return false
		}
	}
	return true
}
