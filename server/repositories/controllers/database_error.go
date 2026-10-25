package controllers

import (
	"context"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func controllerDB(ctx context.Context, fallback *gorm.DB) *gorm.DB {
	registrationError := installDatabaseRetry(fallback)
	database := dbhelper.GetCtxDB(ctx, fallback)
	database = database.Session(&gorm.Session{
		Context: ctx,
		Logger:  database.Logger.LogMode(logger.Silent),
	})
	if registrationError != nil {
		database.AddError(registrationError)
	}
	if _, transactional := database.Statement.ConnPool.(gorm.TxCommitter); !transactional && database.Dialector.Name() == "sqlite" {
		// 独立 SQL 的完整事务由重试回调拥有，禁用包在其外层的 ORM 隐式事务。
		database.Config.SkipDefaultTransaction = true
	}
	return database
}

func databaseError(operation string, resource string, err error) error {
	if isDatabaseBusy(err) {
		return status_error.DatabaseBusy
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, status_error.DatabaseOperationCancelled) {
		return status_error.DatabaseOperationCancelled
	}
	logx.Errorf(
		"database operation failed: operation=%s resource=%s error=%v",
		operation,
		resource,
		err,
	)
	return status_error.DatabaseError
}
