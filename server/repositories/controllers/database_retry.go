package controllers

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"gorm.io/gorm"
)

// isDatabaseBusy 通过 SQLite 主错误码识别 busy（含 busy snapshot 等扩展码）。
func isDatabaseBusy(err error) bool {
	if errors.Is(err, status_error.DatabaseBusy) {
		return true
	}
	var sqliteError interface{ Code() int }
	return errors.As(err, &sqliteError) && sqliteError.Code()&0xff == 5
}

// retryDatabase 仅重试未提交的 busy 操作，共初次执行和三次退避重试。
func retryDatabase(ctx context.Context, operation func() error, wait func(context.Context, time.Duration) error) error {
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return status_error.DatabaseOperationCancelled
		}
		err := operation()
		if err != nil && ctx.Err() != nil {
			return status_error.DatabaseOperationCancelled
		}
		if !isDatabaseBusy(err) {
			return err
		}
		if attempt == 3 {
			return status_error.DatabaseBusy
		}
		if err := wait(ctx, time.Second<<attempt); err != nil {
			return status_error.DatabaseOperationCancelled
		}
	}
}

// waitDatabaseRetry 的退避等待可被调用方 Context 立即取消。
func waitDatabaseRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// databaseRetryRegistrations 仅协调各连接的回调安装，不串行化数据库操作。
var databaseRetryRegistrations sync.Map

// databaseRetryRegistration 保存同一 GORM 回调注册表的一次性初始化结果。
type databaseRetryRegistration struct {
	// once 保证并发 Controller 只安装一次回调，与业务写入无关。
	once sync.Once
	// err 保存回调注册失败，阻止在不完整配置上继续操作。
	err error
}

// installDatabaseRetry 将 SQL 执行、结果扫描和提交纳入同一个重试范围。
func installDatabaseRetry(database *gorm.DB) error {
	if database.Dialector.Name() != "sqlite" {
		return nil
	}
	// Session 复制 Config 但共享 callbacks，以实际注册表作为安装身份。
	value, _ := databaseRetryRegistrations.LoadOrStore(database.Callback(), &databaseRetryRegistration{})
	registration := value.(*databaseRetryRegistration)
	registration.once.Do(func() {
		callbacks := database.Callback()
		for _, err := range []error{
			callbacks.Query().Replace("gorm:query", retrySQLCallback(callbacks.Query().Get("gorm:query"))),
			callbacks.Create().Replace("gorm:create", retrySQLCallback(callbacks.Create().Get("gorm:create"))),
			callbacks.Update().Replace("gorm:update", retrySQLCallback(callbacks.Update().Get("gorm:update"))),
			callbacks.Delete().Replace("gorm:delete", retrySQLCallback(callbacks.Delete().Get("gorm:delete"))),
		} {
			if err != nil {
				registration.err = databaseError("install_sqlite_retry", "database", err)
				return
			}
		}
	})
	return registration.err
}

// retrySQLCallback 对独立操作开启新事务；已有事务的 busy 留给最外层回调重试。
func retrySQLCallback(operation func(*gorm.DB)) func(*gorm.DB) {
	return func(database *gorm.DB) {
		if database.Error != nil {
			return
		}
		// 子查询通过 DryRun 在原 Statement 上构建 SQL，不执行数据库操作。
		if database.DryRun {
			operation(database)
			return
		}
		if _, transactional := database.Statement.ConnPool.(gorm.TxCommitter); transactional {
			operation(database)
			return
		}
		err := retryDatabase(database.Statement.Context, func() error {
			return database.Transaction(func(transaction *gorm.DB) error {
				// GORM 开启事务时克隆 Statement，但不复制当前处理器的构建顺序。
				transaction.Statement.BuildClauses = database.Statement.BuildClauses
				operation(transaction)
				database.RowsAffected = transaction.RowsAffected
				return transaction.Error
			})
		}, waitDatabaseRetry)
		if err != nil {
			database.AddError(err)
		}
	}
}
