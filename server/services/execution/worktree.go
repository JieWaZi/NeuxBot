package execution

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workspacefiles"
)

// WithIdleWorktreeChange 验证共享工作树没有进行中的执行或文件冻结，并保护分支切换。
func (m *service) WithIdleWorktreeChange(ctx context.Context, repository string, change func() error) error {
	m.executions.worktreeChange.Lock()
	defer m.executions.worktreeChange.Unlock()
	if ctx.Err() != nil {
		return status_error.OperationCanceled
	}
	for _, id := range m.OccupiedChatIDs() {
		chat, err := m.journal.LoadExecutionChat(ctx, id)
		if util.HasStatusCode(err, status_error.ChatNotFound) {
			// 首发在建立 Chat 权威事实前已经持有执行占位。
			return status_error.ChatHasActiveExecution
		}
		if err != nil {
			return err
		}
		if workspacefiles.RepositoryIntersectsWorkspace(repository, chat.WorkspaceDirectory) ||
			workspacefiles.RepositoryIntersectsWorkspace(repository, chat.AdditionalWorkingDirectories...) {
			return status_error.ChatHasActiveExecution
		}
	}
	return change()
}
