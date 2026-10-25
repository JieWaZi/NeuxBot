package knowledge

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/neux-knowledge/core"
	"os/user"
	"path/filepath"
)

// Open 使用专属 SQLite 和 raw 目录；损坏或迁移失败不回退空库。
func Open(ctx context.Context, dataDirectory, managedDirectory string) (*core.Engine, error) {
	engine, err := core.Open(ctx, core.Options{DBPath: filepath.Join(dataDirectory, "knowledge", "knowledge.db"), RawDir: filepath.Join(managedDirectory, "knowledge", "raw")})
	return engine, MapError(err)
}

// MapError 在独立 SDK 首次进入 Ally 时翻译稳定错误并保留原始诊断日志。
func MapError(err error) error {
	if err == nil {
		return nil
	}
	var stable interface {
		error
		Code() int64
	}
	if errors.As(err, &stable) {
		return err
	}
	var e *core.Error
	if errors.As(err, &e) {
		switch e.Code {
		case core.ErrInvalid:
			// SDK 暂以操作和原因区分非空目录，首次转换时保留可操作的业务提示。
			if e.Operation == "theme.delete" {
				switch e.Detail {
				case "theme has children":
					return status_error.KnowledgeFolderHasChildren
				case "theme contains pages":
					return status_error.KnowledgeFolderHasPages
				}
			}
			return status_error.KnowledgeInvalid
		case core.ErrNotFound:
			return status_error.KnowledgeNotFound
		case core.ErrVersionConflict, core.ErrIdempotencyConflict, core.ErrManualProtected, core.ErrSourceUnavailable:
			return status_error.KnowledgeConflict
		case core.ErrScopeViolation:
			return status_error.KnowledgeForbidden
		case core.ErrCancelled, core.ErrStaleAttempt:
			return status_error.KnowledgeCancelled
		case core.ErrExecutionNotSuccessful, core.ErrCandidateInvalid:
			return status_error.KnowledgeCandidateInvalid
		case core.ErrDatabaseBusy:
			return status_error.DatabaseBusy
		}
	}
	logx.Errorf("knowledge SDK failed: %v", err)
	return status_error.KnowledgeStorageError
}

// LocalUserActivity 只在已认证的本机用户API入口注入OS真实身份，不从请求正文接受操作者。
func LocalUserActivity(ctx context.Context) context.Context {
	actor := core.ActivityActor{Trigger: "user"}
	if current, err := user.Current(); err == nil && current.Uid != "" {
		actor.UserID = "local:" + current.Uid
	}
	return core.WithActivityActor(ctx, actor)
}
