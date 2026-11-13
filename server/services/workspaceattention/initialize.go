package workspaceattention

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
)

// InitializeBaseline 在原恢复写入新工作事实之前建立一次历史基线。
func InitializeBaseline(ctx context.Context, store *controllers.ReadingInitializationController, baselineChats func(context.Context) error) error {
	state, err := store.Begin(ctx)
	if err != nil || state.BaselineCompletedAt != nil {
		return err
	}
	if err := baselineChats(ctx); err != nil {
		return err
	}
	if err := store.MarkHistoricalRuns(ctx, state.StartedAt); err != nil {
		return err
	}
	return store.MarkBaselineCompleted(ctx)
}

// FinishInitialization 在原恢复屏障之后清理旧跟踪表。
func FinishInitialization(ctx context.Context, store *controllers.ReadingInitializationController, transactions controllers.TransactionController) error {
	state, err := store.Begin(ctx)
	if err != nil || state.CompletedAt != nil {
		return err
	}
	return transactions.Transaction(ctx, store.Finish)
}
