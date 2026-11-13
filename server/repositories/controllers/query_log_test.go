package controllers

import (
	"context"
	"gorm.io/gorm/logger"
	"time"
)

type repositoryQueryLog struct {
	// Interface 复用其余日志方法。
	logger.Interface
	// Statements 保存本次读取的实际 SQL。
	Statements []string
	// Rows 保存对应 SQL 返回到应用的行数，不代表数据库内部扫描次数。
	Rows []int64
}

// LogMode 保留观测器，防止仓储切换 Silent 时丢失查询次数与返回行数。
func (l *repositoryQueryLog) LogMode(logger.LogLevel) logger.Interface { return l }

func (l *repositoryQueryLog) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, rows := fc()
	l.Statements = append(l.Statements, sql)
	l.Rows = append(l.Rows, rows)
}
