package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// chatOutputBatch 在真实事务之间保留本次确认的文件，复制不持有命令锁或 SQLite 写事务。
type chatOutputBatch struct {
	// run 提供当前来源运行的冻结读取范围。
	run models.WorkflowRun
	// pending 记录还需要从本次范围取得的原路径。
	pending map[string]string
	// captured 仅复用这次提交中同一来源路径已经取得的字节。
	captured map[string]models.WorkflowChatFile
}

func (b *chatOutputBatch) attach(run *models.WorkflowRun, effectID string, paths map[string]string) error {
	for field, path := range paths {
		key := field
		if effectID != "" {
			key = effectID + ":" + field
		}
		if _, exists := run.ChatFiles[key]; exists {
			continue
		}
		file, ok := b.captured[path]
		if !ok {
			b.run = *run
			if b.pending == nil {
				b.pending = make(map[string]string)
			}
			b.pending[path] = key
			continue
		}
		if run.ChatFiles == nil {
			run.ChatFiles = make(map[string]models.WorkflowChatFile)
		}
		file.Partial = effectID != ""
		run.ChatFiles[key] = file
	}
	if len(b.pending) != 0 {
		return status_error.WorkflowStateConflict
	}
	return nil
}

// withChatOutputSnapshots 在发现声明文件时回滚本次检查点，复制完成后重新取得租约并重验状态。
func (s *Service) withChatOutputSnapshots(ctx context.Context, batch *chatOutputBatch, commit func(*chatOutputBatch) error) error {
	for {
		if len(batch.pending) != 0 && batch.captured == nil {
			batch.captured = make(map[string]models.WorkflowChatFile)
		}
		for path, key := range batch.pending {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			file := models.WorkflowChatFile{Name: filepath.Base(path)}
			if err := s.freezeChatFile(ctx, batch.run, path, key, &file); err != nil {
				file.Error = util.ErrorCode(err, status_error.WorkflowFileStorageError.Key())
			}
			batch.captured[path] = file
		}
		batch.pending = nil
		err := commit(batch)
		if len(batch.pending) == 0 || !errors.Is(err, status_error.WorkflowStateConflict) {
			return err
		}
	}
}

func (s *Service) chatOutputDirectory(run models.WorkflowRun) string {
	return filepath.Join(s.dataDirectory, constants.WorkflowFileDirectoryName, run.DefinitionID, "chat-results", run.ID)
}

func (s *Service) freezeChatFile(ctx context.Context, run models.WorkflowRun, path, key string, output *models.WorkflowChatFile) error {
	original, err := openRunFile(run, path)
	if err != nil {
		return err
	}
	defer original.Close()
	before, err := original.Stat()
	if err != nil {
		return status_error.WorkflowFileInvalid
	}
	dir := s.chatOutputDirectory(run)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return status_error.WorkflowFileStorageError
	}
	file, err := workflowfiles.FreezeOpenFile(ctx, original, path, dir, run.ID, key, "")
	if err != nil {
		return err
	}
	// 复制沿用同一文件句柄。原件被原地改写时不能将混合字节记为本次结果。
	after, err := original.Stat()
	if err != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return status_error.WorkflowFileInvalid
	}
	relative, err := filepath.Rel(dir, file.Path)
	if err != nil || !filepath.IsLocal(relative) {
		return status_error.WorkflowFileInvalid
	}
	output.Path, output.Digest = relative, file.Digest
	return nil
}

func (s *Service) openChatFile(run models.WorkflowRun, file models.WorkflowChatFile) (*os.File, error) {
	content, _, err := s.openChatSnapshot(run, file)
	if err != nil {
		return nil, err
	}
	digest := sha256.New()
	count, err := io.Copy(digest, io.LimitReader(content, constants.WorkflowFileMaximumBytes+1))
	if err != nil {
		logChatSnapshotFailure(run, file, "hash frozen file", err)
		content.Close()
		return nil, status_error.WorkflowFileStorageError
	}
	if count > constants.WorkflowFileMaximumBytes || file.Digest != hex.EncodeToString(digest.Sum(nil)) {
		content.Close()
		return nil, status_error.WorkflowFileInvalid
	}
	if _, err := content.Seek(0, io.SeekStart); err != nil {
		logChatSnapshotFailure(run, file, "rewind frozen file", err)
		content.Close()
		return nil, status_error.WorkflowFileStorageError
	}
	return content, nil
}

// openChatSnapshot 校验受管快照归属与文件元数据，读取正文由实际消费方负责。
func (s *Service) openChatSnapshot(run models.WorkflowRun, file models.WorkflowChatFile) (*os.File, int64, error) {
	if file.Error != "" || !filepath.IsLocal(file.Path) || len(file.Digest) != sha256.Size*2 {
		return nil, 0, status_error.WorkflowFileInvalid
	}
	root, err := os.OpenRoot(s.chatOutputDirectory(run))
	if err != nil {
		logChatSnapshotFailure(run, file, "open output directory", err)
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, status_error.WorkflowFileNotFound
		}
		return nil, 0, status_error.WorkflowFileStorageError
	}
	defer root.Close()
	content, err := root.Open(file.Path)
	if err != nil {
		logChatSnapshotFailure(run, file, "open frozen file", err)
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, status_error.WorkflowFileNotFound
		}
		return nil, 0, status_error.WorkflowFileStorageError
	}
	info, err := content.Stat()
	if err != nil {
		logChatSnapshotFailure(run, file, "inspect frozen file", err)
		content.Close()
		return nil, 0, status_error.WorkflowFileStorageError
	}
	if !info.Mode().IsRegular() || info.Size() > constants.WorkflowFileMaximumBytes {
		content.Close()
		return nil, 0, status_error.WorkflowFileInvalid
	}
	return content, info.Size(), nil
}

func logChatSnapshotFailure(run models.WorkflowRun, file models.WorkflowChatFile, operation string, err error) {
	if err != nil {
		logx.Errorf("read workflow chat output failed: operation=%q run_id=%s chat_id=%s path=%q error=%v", operation, run.ID, run.SourceChatID, file.Path, err)
	}
}

// commitChatOutputEffect 随成功节点的收据冻结显式文件字段，后续节点可覆盖业务目录的同名文件。
func (s *Service) commitChatOutputEffect(ctx context.Context, effect models.WorkflowEffect, schema map[string]any, mutate func(context.Context, *controllers.WorkflowRunState, *models.WorkflowEffect) error) error {
	var invocationSchema map[string]any
	if effect.Boundary.Primitive == "action" {
		invocationSchema, _ = workflowstate.BoundaryOptions(effect.Boundary)["schema"].(map[string]any)
	}
	if len(schema) == 0 && len(invocationSchema) == 0 {
		return s.state.MutateEffect(ctx, effect.RunID, effect.ID, mutate)
	}
	return s.withChatOutputSnapshots(ctx, &chatOutputBatch{}, func(batch *chatOutputBatch) error {
		return s.state.MutateEffect(ctx, effect.RunID, effect.ID, func(ctx context.Context, state *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
			previous := current.Status
			if err := mutate(ctx, state, current); err != nil {
				return err
			}
			if state.Run.SourceChatID == "" || state.Run.SourceWorkbench || previous == current.Status || current.Status != constants.WorkflowStepStatusSucceeded || current.Result == nil {
				return nil
			}
			value, err := current.Result.JSON()
			if err != nil {
				return err
			}
			envelope, _ := value.(map[string]any)
			switch current.Boundary.Primitive {
			case "action":
				value = envelope["data"]
			case "workflow", "joinWorkflow":
				value = envelope["result"]
			}
			files := workflowdsl.FileValues(schema, value)
			if len(invocationSchema) != 0 {
				maps.Copy(files, workflowdsl.FileValues(invocationSchema, value))
			}
			return batch.attach(&state.Run, current.ID, files)
		})
	})
}

func (l *runnerLifecycle) copying(id string) bool {
	current := l.copyingRun.Load()
	return current != nil && *current == id
}

func (l *runnerLifecycle) copyChatOutput(id string, copyOutput func()) {
	if !l.copyingRun.CompareAndSwap(nil, &id) {
		return
	}
	l.workers.Add(1)
	go func() {
		defer l.workers.Done()
		defer l.copyingRun.Store(nil)
		copyOutput()
	}()
}

func (s *Service) notifyTerminalChat(l *runnerLifecycle, run models.WorkflowRun) {
	if run.SourceChatID == "" || run.SourceWorkbench {
		return
	}
	s.chatTurns.active.Delete(chatRunOrigin{chatID: run.SourceChatID, executionID: run.SourceExecutionID})
	select {
	case l.deliveryWake <- struct{}{}:
	default:
	}
}

// deferChatOutput 仅将产生正式文件的最终检查点交给独立工作协程。
func (s *Service) deferChatOutput(lifecycle *runnerLifecycle, id string, batch *chatOutputBatch, err error) bool {
	if len(batch.pending) == 0 || !errors.Is(err, status_error.WorkflowStateConflict) {
		return false
	}
	lifecycle.copyChatOutput(id, func() {
		err := s.withChatOutputSnapshots(lifecycle.ctx, batch, func(ready *chatOutputBatch) error {
			_, err := s.tickCheckpoint(lifecycle.ctx, id, ready)
			return err
		})
		if err != nil {
			if lifecycle.ctx.Err() == nil {
				logx.Warnf("commit workflow file output failed: run_id=%s error=%v", id, err)
			}
			return
		}
		current, err := s.runs.GetRun(lifecycle.ctx, id)
		if err == nil && workflowstate.TerminalRun(current.Status) {
			s.notifyTerminalChat(lifecycle, current)
		}
	})
	return true
}
