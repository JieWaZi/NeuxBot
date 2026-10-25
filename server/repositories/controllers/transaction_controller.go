// Package controllers 定义持久化接口及其 GORM 实现。
package controllers

import (
	"context"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// TransactionController 统一执行数据库事务并收敛事务基础设施错误。
type TransactionController interface {
	// Transaction 原子执行数据库回调；SQLite busy 时回滚后按 1、2、4 秒重试完整事务。
	// 回调可多次执行，只能包含数据库操作，不能依赖前一次调用的临时结果。
	Transaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// transactionController 使用 ginx 事务管理器实现数据库事务边界。
type transactionController struct {
	// manager 执行 GORM 事务并把事务连接注入 Context。
	manager dbhelper.TransactionManager
}

// NewTransactionController 创建统一数据库事务 Controller。
func NewTransactionController(manager dbhelper.TransactionManager) TransactionController {
	return &transactionController{manager: manager}
}

// Transaction 在同一数据库事务中执行回调。
func (c *transactionController) Transaction(
	ctx context.Context,
	fn func(ctx context.Context) error,
) error {
	// 嵌套事务不能独立刷新外层快照，将 busy 交给最外层事务重试。
	if ctx.Value(databaseTransactionKey{}) != nil {
		return events.CommitScope(ctx, func(buffered context.Context) error {
			return c.manager.Transaction(buffered, fn)
		})
	}
	err := retryDatabase(ctx, func() error {
		return events.CommitScope(ctx, func(buffered context.Context) error {
			return c.manager.Transaction(context.WithValue(buffered, databaseTransactionKey{}, true), fn)
		})
	}, waitDatabaseRetry)
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status_error.DatabaseOperationCancelled
	}
	var statusErr statuserror.CommonError
	if errors.As(err, &statusErr) {
		return err
	}
	logx.Errorf("database transaction failed: error=%v", err)
	return status_error.DatabaseError
}

// databaseTransactionKey 标记统一事务的最外层重试边界。
type databaseTransactionKey struct{}
