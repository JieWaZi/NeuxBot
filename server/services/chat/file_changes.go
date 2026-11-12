package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/changes"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// FileChangesRecorder 是 Execution 调用的独立快照端口，测试日志可不装配此文件能力。
type FileChangesRecorder interface {
	// BeginFileChanges 在原生输入执行前保存冻结范围的基线。
	BeginFileChanges(context.Context, models.Chat, string, []string, []string) (*FileChangesRecording, error)
}

// FileChangesRecording 关联一次技术采集与原 Chat 日志，不拥有执行状态。
type FileChangesRecording struct {
	// service 提交成果引用并更新原消息版本。
	service *service
	// chatID 是文件成果与日志的真实归属。
	chatID string
	// executionID 是结果关联的原执行身份。
	executionID string
	// capture 是唯一前后快照所有者。
	capture *changes.Capture
	// publishedVersion 防止已明确不可采集的基线在执行结束时重复发布同一版本。
	publishedVersion int
}

var _ FileChangesRecorder = (*service)(nil)

// BeginFileChanges 复用日志归属冻结授权目录；采集失败不改变模型执行结果。
func (m *service) BeginFileChanges(ctx context.Context, chat models.Chat, executionID string, workingDirectories, referencedFiles []string) (*FileChangesRecording, error) {
	directory, err := m.logStore.FileChangesDirectory(chat.ID)
	if err != nil {
		return nil, err
	}
	space := chat.WorkspaceDirectory
	roots := append([]string{space}, workingDirectories...)
	capture, err := changes.Begin(ctx, directory, executionID, roots, changes.Options{FilePaths: referencedFiles, ManagedSpace: space, ExcludePaths: []string{directory}})
	if err != nil {
		return nil, err
	}
	return &FileChangesRecording{service: m, chatID: chat.ID, executionID: executionID, capture: capture}, nil
}

// Freeze 在允许下一轮写盘前保存结束侧，不计算差异或发布成果终态。
func (r *FileChangesRecording) Freeze(ctx context.Context) error {
	return r.capture.Freeze(ctx)
}

// Finish 比较已经冻结的快照并提交轻量引用；文件内容先于引用落盘。
func (r *FileChangesRecording) Finish(ctx context.Context) error {
	defer r.Close()
	summary, err := r.capture.Finish(ctx)
	if err != nil {
		return err
	}
	return r.publish(ctx, *summary)
}

// Close 保留未完成记录用于中断恢复；明确未接纳的输入应调用 Discard。
func (r *FileChangesRecording) Close() error {
	if r == nil || r.capture == nil {
		return nil
	}
	return r.capture.Close()
}

// Discard 丢弃尚未发布的采集；只有真实未提交的新 Chat 才清理其独有结果目录。
func (r *FileChangesRecording) Discard(ctx context.Context, uncommittedNewChat bool) error {
	if r == nil || r.capture == nil {
		return nil
	}
	if r.publishedVersion != 0 {
		return status_error.WorkspaceResourceConflict
	}
	if err := r.capture.Discard(ctx); err != nil {
		return err
	}
	if !uncommittedNewChat {
		return nil
	}
	directory, err := r.service.logStore.FileChangesDirectory(r.chatID)
	if err != nil {
		return err
	}
	surviving := false
	if err := changes.WalkSummaries(directory, func(changes.Summary) error { surviving = true; return nil }); err != nil {
		return err
	}
	if surviving {
		return nil
	}
	path, err := r.service.logStore.LogPath(r.chatID)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		logx.Warnf("inspect uncommitted file result owner failed: chat_id=%s error=%v", r.chatID, err)
		return status_error.ChatLogError
	}
	// 目录来自原日志归属，删除相对路径受 Root 约束；不能跟随被替换的结果目录链接。
	root, err := os.OpenRoot(filepath.Dir(filepath.Dir(path)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		logx.Warnf("open uncommitted file result owner failed: chat_id=%s error=%v", r.chatID, err)
		return status_error.ChatLogError
	}
	defer root.Close()
	relative := "file-results"
	info, err := root.Lstat(relative)
	if err != nil && !os.IsNotExist(err) {
		return status_error.ChatLogError
	}
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return status_error.WorkspaceScopeInvalid
		}
		if err := root.RemoveAll(relative); err != nil {
			logx.Warnf("discard uncommitted file result directory failed: chat_id=%s error=%v", r.chatID, err)
			return status_error.ChatLogError
		}
	}
	return nil
}

// PublishPending 在真实执行已进入原日志后公开基线状态，不阻塞终态等待结束快照。
func (r *FileChangesRecording) PublishPending(ctx context.Context) error {
	return r.publish(ctx, r.capture.Summary())
}

func (r *FileChangesRecording) publish(ctx context.Context, summary changes.Summary) error {
	if summary.Version <= r.publishedVersion {
		return nil
	}
	data := fileChangesReference(summary)
	_, err := r.service.AppendExecutionFacts(ctx, r.chatID, []eventlog.AppendInput{{Type: constants.ChatEventTypeFileChanges, MessageID: r.executionID, ExecutionID: r.executionID, Data: marshalData(data)}})
	if err != nil {
		return err
	}
	r.publishedVersion = summary.Version
	return nil
}

func fileChangesReference(summary changes.Summary) eventlog.FileChangesData {
	return eventlog.FileChangesData{RecordID: summary.ID, Version: summary.Version, State: summary.State, Reason: summary.Reason, TotalFiles: summary.TotalFiles, Complete: summary.Complete, Truncated: summary.Truncated}
}

// GetMessageFileChanges 按索引读取指定消息的成果引用，再返回授权快照摘要。
func (m *service) GetMessageFileChanges(ctx context.Context, chatID, messageID string) (changes.Summary, error) {
	if err := ctx.Err(); err != nil {
		return changes.Summary{}, chatContextStatus(err)
	}
	records, err := m.logStore.ReadFileChangesReference(chatID, messageID)
	if err != nil {
		return changes.Summary{}, err
	}
	var scope eventlog.ChatCreatedData
	var reference *eventlog.FileChangesData
	for _, record := range records {
		switch record.Type {
		case constants.ChatEventTypeCreated:
			if json.Unmarshal(record.Data, &scope) != nil {
				return changes.Summary{}, status_error.ChatLogCorrupted
			}
		case constants.ChatEventTypeFileChanges:
			var value eventlog.FileChangesData
			if record.MessageID == messageID && json.Unmarshal(record.Data, &value) == nil {
				reference = &value
			}
		case constants.ChatEventTypeMessageInherited:
			var inherited messageview.InheritedMessage
			if record.MessageID == messageID && json.Unmarshal(record.Data, &inherited) == nil {
				reference = inherited.Message.FileChangeRecord
				scope.AdditionalWorkingDirectories = append(scope.AdditionalWorkingDirectories, inherited.FileChangeRoots...)
			}
		}
	}
	if reference == nil {
		return changes.Summary{}, fileChangeNotFound(messageID)
	}
	directory, err := m.logStore.FileChangesDirectory(chatID)
	if err != nil {
		return changes.Summary{}, err
	}

	summary, err := changes.ReadSummary(directory, reference.RecordID)
	if err != nil {
		return changes.Summary{}, fileChangeNotFound(messageID)
	}
	if summary.Version < reference.Version {
		return changes.Summary{}, fileChangeNotFound(messageID)
	}
	if summary.Version > reference.Version {
		// 内容落盘与日志提交之间只能公开旧引用证明的状态，不能提前宣布结束侧可用。
		summary.Version, summary.State, summary.Reason = reference.Version, reference.State, reference.Reason
		summary.Files = []changes.File{}
		summary.TotalFiles, summary.Complete, summary.Truncated = reference.TotalFiles, reference.Complete, reference.Truncated
	}
	allowed := append([]string{scope.WorkspaceDirectory}, scope.AdditionalWorkingDirectories...)
	for _, source := range scope.ProjectSources {
		allowed = append(allowed, source.SourcePath)
	}
	for _, root := range summary.Roots {
		if !fileChangeRootAllowed(root.Path, allowed) {
			return changes.Summary{}, status_error.BadRequest
		}
	}
	return *summary, nil
}

func fileChangeRootAllowed(root string, allowed []string) bool {
	for _, candidate := range allowed {
		if !filepath.IsAbs(candidate) {
			continue
		}
		// 两侧均来自已冻结的记录，读取历史成果无需重新解释磁盘上的链接。
		relative, err := filepath.Rel(candidate, root)
		if err == nil && !filepath.IsAbs(relative) && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func fileChangeNotFound(path string) error {
	return status_error.ChatMessageFileChangeNotFound.WithParams(map[string]interface{}{"Path": path})
}

func (m *service) readRecordedFileDiff(ctx context.Context, chatID, messageID, path string) (messageview.MessageFileChangeDiff, error) {
	summary, err := m.GetMessageFileChanges(ctx, chatID, messageID)
	if err != nil {
		return messageview.MessageFileChangeDiff{}, err
	}
	var selected *changes.File
	for index := range summary.Files {
		file := &summary.Files[index]
		if file.AbsolutePath == path {
			selected = file
			break
		}
	}
	if selected == nil {
		return messageview.MessageFileChangeDiff{}, fileChangeNotFound(path)
	}
	directory, err := m.logStore.FileChangesDirectory(chatID)
	if err != nil {
		return messageview.MessageFileChangeDiff{}, err
	}
	diff, err := changes.ReadDiff(ctx, directory, summary.ID, selected.RootID, selected.Path)
	if err != nil {
		logx.Warnf("read file result failed: chat_id=%s message_id=%s error=%v", chatID, messageID, err)
		return messageview.MessageFileChangeDiff{}, fileChangeNotFound(path)
	}
	return messageview.MessageFileChangeDiff{Path: selected.AbsolutePath, Comparison: diff}, nil
}

// recoverFileChanges 仅在宿主启动且没有活动采集时恢复遗留记录，再提交可证明的引用版本。
func (m *service) recoverFileChanges(ctx context.Context) error {
	chatIDs, err := m.logStore.ListChatIDs()
	if err != nil {
		return err
	}
	for _, chatID := range chatIDs {
		if err := ctx.Err(); err != nil {
			return chatContextStatus(err)
		}
		directory, err := m.logStore.FileChangesDirectory(chatID)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(directory, "changes")); os.IsNotExist(err) {
			continue
		} else if err != nil {
			logx.Warnf("inspect file result recovery failed: chat_id=%s error=%v", chatID, err)
			continue
		}
		if err := changes.Recover(directory); err != nil {
			logx.Warnf("recover file results failed: chat_id=%s error=%v", chatID, err)
			continue
		}
		err = changes.WalkSummaries(directory, func(summary changes.Summary) error {
			if err := ctx.Err(); err != nil {
				return chatContextStatus(err)
			}
			if summary.Version <= 1 {
				return nil
			}
			// 只读该执行的轻量引用；正常启动不重放历史工具正文。继承记录没有对应原执行时跳过。
			records, err := m.logStore.ReadFileChangesReference(chatID, summary.ExecutionID)
			if err != nil {
				return err
			}
			var reference *eventlog.FileChangesData
			for _, record := range records {
				if record.Type == constants.ChatEventTypeFileChanges && record.MessageID == summary.ExecutionID {
					var value eventlog.FileChangesData
					if json.Unmarshal(record.Data, &value) != nil {
						return status_error.ChatLogCorrupted
					}
					reference = &value
				}
			}
			if reference == nil || reference.RecordID != summary.ID || summary.Version <= reference.Version {
				return nil
			}
			recording := &FileChangesRecording{service: m, chatID: chatID, executionID: summary.ExecutionID, publishedVersion: reference.Version}
			return recording.publish(ctx, summary)
		})
		if err != nil {
			if ctx.Err() != nil {
				return chatContextStatus(ctx.Err())
			}
			logx.Warnf("publish recovered file results failed: chat_id=%s error=%v", chatID, err)
		}
	}
	return nil
}
