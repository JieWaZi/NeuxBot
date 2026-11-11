package chatstorage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

const (
	deletionDirectoryName        = "deletions"
	deletionIntentDirectoryName  = "intents"
	deletionStagingDirectoryName = "staging"
	deletionIntentVersion        = 1
)

// AggregateKind 区分 Chat、团队、伙伴及 Chat 项目入口的受管删除目标。
type AggregateKind string

const (
	// AggregateKindChat 表示 Conversations/<chatID> 私有目录。
	AggregateKindChat AggregateKind = "chat"
	// AggregateKindChatProjectEntries 表示 Chat 自己拥有的项目入口命名空间。
	AggregateKindChatProjectEntries AggregateKind = "chat-project-entries"
	// AggregateKindWaker 表示由伙伴删除用例单独拥有的 Agents/<wakerID> 目录。
	AggregateKindWaker AggregateKind = "waker"
	// AggregateKindTeam 表示 Workspaces/<threadID> 团队目录。
	AggregateKindTeam AggregateKind = "team"
)

// DeletionTarget 是经过业务层归属预检的一个完整聚合目录。
type DeletionTarget struct {
	// Kind 是已完成归属预检的受管目标类型。
	Kind AggregateKind
	// AggregateID 是该目标的 Chat、Thread 或 Waker 身份。
	AggregateID string
	// WakerID 仅在项目入口目标中保存原 ChatCreated 的伙伴身份。
	WakerID string
	// WorkspaceDirectory 仅保存该删除操作冻结的真实 cwd，不形成长期映射。
	WorkspaceDirectory string
}

// StagedDeletion 是一次可提交或回滚的持久删除意图。
type StagedDeletion struct {
	// coordinator 拥有 manifest 与 staging 的路径规则。
	coordinator *DeletionCoordinator
	// intent 是磁盘中已持久化的删除协议状态。
	intent deletionIntent
}

// DeletionCoordinator 管理整聚合 rename、崩溃恢复和最终清理。
type DeletionCoordinator struct {
	// layout 提供原始聚合目录的唯一规范路径。
	layout *Layout
	// root 保存删除意图和同文件系统 staging 目录。
	root string
	// intentRoot 保存小型 JSON 删除意图。
	intentRoot string
	// stagingRoot 保存已从正式目录原子移出的聚合目录。
	stagingRoot string
}

// deletionIntent 是崩溃后判断恢复或继续清理所需的全部持久事实。
type deletionIntent struct {
	// Version 是删除协议版本。
	Version int `json:"version"`
	// ID 是本次删除意图的随机标识。
	ID string `json:"id"`
	// State 是 prepared、staged 或 committed。
	State string `json:"state"`
	// ChatIDs 是同一数据库事务删除的全部 Chat 索引。
	ChatIDs []string `json:"chatIds"`
	// Targets 是需要共同提交或回滚的聚合目录。
	Targets []deletionIntentTarget `json:"targets"`
}

// deletionIntentTarget 保存一个聚合目录 rename 的两端地址。
type deletionIntentTarget struct {
	// Kind 是已完成归属预检的受管目标类型。
	Kind AggregateKind `json:"kind"`
	// AggregateID 是该目标的 Chat、Thread 或 Waker 身份。
	AggregateID string `json:"aggregateId"`
	// WakerID 是项目入口所属 Chat 的权威伙伴身份快照。
	WakerID string `json:"wakerId,omitempty"`
	// WorkspaceDirectory 是已完成归属预检的 cwd 操作快照。
	WorkspaceDirectory string `json:"workspaceDirectory,omitempty"`
	// OriginalPath 是正式聚合目录。
	OriginalPath string `json:"originalPath"`
	// StagedPath 是同文件系统临时目录。
	StagedPath string `json:"stagedPath"`
}

// NewDeletionCoordinator 创建聚合删除协议；目录只在首次删除时延迟物化。
func NewDeletionCoordinator(layout *Layout) (*DeletionCoordinator, error) {
	if layout == nil || layout.Root() == "" {
		return nil, status_error.BadRequest
	}
	root := filepath.Join(layout.Root(), deletionDirectoryName)
	coordinator := &DeletionCoordinator{
		layout:      layout,
		root:        root,
		intentRoot:  filepath.Join(root, deletionIntentDirectoryName),
		stagingRoot: filepath.Join(root, deletionStagingDirectoryName),
	}
	return coordinator, nil
}

// Stage 持久化意图后把全部聚合目录原子移入受管 staging。
func (c *DeletionCoordinator) Stage(
	intentID string,
	targets []DeletionTarget,
	chatIDs []string,
) (*StagedDeletion, error) {
	if c == nil || !safeSegment(intentID) || len(targets) == 0 || (len(chatIDs) == 0 && !wakerDeletionTargets(targets)) {
		return nil, status_error.BadRequest
	}
	intent := deletionIntent{
		Version: deletionIntentVersion,
		ID:      intentID,
		State:   "prepared",
		ChatIDs: uniqueSorted(chatIDs),
		Targets: make([]deletionIntentTarget, 0, len(targets)),
	}
	intentStagingRoot := filepath.Join(c.stagingRoot, intentID)
	if err := os.MkdirAll(intentStagingRoot, 0o700); err != nil {
		return nil, status_error.ManagedWorkspaceError
	}
	if err := syncDirectory(filepath.Dir(c.root)); err != nil {
		return nil, status_error.ManagedWorkspaceError
	}
	if err := syncDirectory(c.root); err != nil {
		return nil, status_error.ManagedWorkspaceError
	}
	if err := syncDirectory(c.stagingRoot); err != nil {
		return nil, status_error.ManagedWorkspaceError
	}
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		originalPath, pathErr := c.aggregatePath(target.Kind, target.AggregateID, target.WakerID, target.WorkspaceDirectory)
		if pathErr != nil {
			return nil, pathErr
		}
		key := string(target.Kind) + "\x00" + target.AggregateID
		if _, duplicated := seen[key]; duplicated {
			continue
		}
		seen[key] = struct{}{}
		intent.Targets = append(intent.Targets, deletionIntentTarget{
			Kind:               target.Kind,
			AggregateID:        target.AggregateID,
			WakerID:            target.WakerID,
			WorkspaceDirectory: target.WorkspaceDirectory,
			OriginalPath:       originalPath,
			StagedPath:         filepath.Join(intentStagingRoot, string(target.Kind)+"-"+target.AggregateID),
		})
	}
	if len(intent.Targets) == 0 {
		return nil, status_error.BadRequest
	}
	staged := &StagedDeletion{coordinator: c, intent: intent}
	if err := c.writeIntent(intent); err != nil {
		return nil, err
	}
	for _, target := range intent.Targets {
		info, statErr := os.Lstat(target.OriginalPath)
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(target.OriginalPath))
		if statErr != nil || parentErr != nil || info == nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || filepath.Clean(parent) != filepath.Clean(filepath.Dir(target.OriginalPath)) {
			_ = staged.Rollback()
			logx.Errorf("stage deletion aggregate missing: intent_id=%s path=%s error=%v", intent.ID, target.OriginalPath, statErr)
			return nil, status_error.ManagedWorkspaceError
		}
		if renameErr := os.Rename(target.OriginalPath, target.StagedPath); renameErr != nil {
			_ = staged.Rollback()
			logx.Errorf("stage deletion aggregate failed: intent_id=%s source=%s target=%s error=%v", intent.ID, target.OriginalPath, target.StagedPath, renameErr)
			return nil, status_error.ManagedWorkspaceError
		}
		if syncErr := syncRename(target.OriginalPath, target.StagedPath); syncErr != nil {
			_ = staged.Rollback()
			logx.Errorf("sync staged deletion failed: intent_id=%s source=%s target=%s error=%v", intent.ID, target.OriginalPath, target.StagedPath, syncErr)
			return nil, status_error.ManagedWorkspaceError
		}
	}
	staged.intent.State = "staged"
	if err := c.writeIntent(staged.intent); err != nil {
		_ = staged.Rollback()
		return nil, err
	}
	return staged, nil
}

// Rollback 把已 staging 的聚合目录恢复到原位并删除意图。
func (s *StagedDeletion) Rollback() error {
	if s == nil || s.coordinator == nil {
		return nil
	}
	for index := len(s.intent.Targets) - 1; index >= 0; index-- {
		target := s.intent.Targets[index]
		stagedExists, stagedErr := pathExists(target.StagedPath)
		originalExists, originalErr := pathExists(target.OriginalPath)
		if stagedErr != nil || originalErr != nil {
			logx.Errorf("inspect staged deletion rollback failed: intent_id=%s staged_error=%v original_error=%v", s.intent.ID, stagedErr, originalErr)
			return status_error.ManagedWorkspaceError
		}
		if stagedExists && originalExists {
			return status_error.ManagedWorkspaceError
		}
		if stagedExists {
			if err := os.MkdirAll(filepath.Dir(target.OriginalPath), 0o700); err != nil {
				return status_error.ManagedWorkspaceError
			}
			if err := os.Rename(target.StagedPath, target.OriginalPath); err != nil {
				logx.Errorf("rollback staged deletion failed: intent_id=%s source=%s target=%s error=%v", s.intent.ID, target.StagedPath, target.OriginalPath, err)
				return status_error.ManagedWorkspaceError
			}
			if err := syncRename(target.StagedPath, target.OriginalPath); err != nil {
				return status_error.ManagedWorkspaceError
			}
		}
	}
	return s.coordinator.removeIntent(s.intent)
}

// Commit 标记数据库事务已提交并最终清理 staging；重复调用保持幂等。
func (s *StagedDeletion) Commit() error {
	if s == nil || s.coordinator == nil {
		return nil
	}
	s.intent.State = "committed"
	if err := s.coordinator.writeIntent(s.intent); err != nil {
		return err
	}
	for _, target := range s.intent.Targets {
		if err := os.RemoveAll(target.StagedPath); err != nil {
			logx.Errorf("purge staged deletion failed: intent_id=%s path=%s error=%v", s.intent.ID, target.StagedPath, err)
			return status_error.ManagedWorkspaceError
		}
	}
	return s.coordinator.removeIntent(s.intent)
}

// Resume 恢复全部残留意图；decideCommitted 必须只依据数据库 tombstone。
func (c *DeletionCoordinator) Resume(
	decideCommitted func(string, []string) (bool, error),
	completed func(string) error,
) error {
	if decideCommitted == nil {
		return status_error.BadRequest
	}
	return c.resume(func(intent deletionIntent) (bool, error) {
		return decideCommitted(intent.ID, append([]string(nil), intent.ChatIDs...))
	}, completed, false)
}

// StageWaker 将伙伴自身目录纳入同一耐久 rename 协议，不扩大 Chat 级联。
func (c *DeletionCoordinator) StageWaker(intentID, wakerID string) (*StagedDeletion, error) {
	return c.Stage(intentID, []DeletionTarget{{Kind: AggregateKindWaker, AggregateID: wakerID}}, nil)
}

// ResumeWakers 按伙伴是否已经从权威领域删除恢复伙伴目录，Chat 恢复不会处理这些目标。
func (c *DeletionCoordinator) ResumeWakers(decideCommitted func(string) (bool, error)) error {
	if decideCommitted == nil {
		return status_error.BadRequest
	}
	return c.resume(func(intent deletionIntent) (bool, error) { return decideCommitted(intent.Targets[0].AggregateID) }, nil, true)
}

func (c *DeletionCoordinator) resume(decideCommitted func(deletionIntent) (bool, error), completed func(string) error, wakers bool) error {
	entries, err := os.ReadDir(c.intentRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return status_error.ManagedWorkspaceError
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(c.intentRoot, entry.Name()))
		if readErr != nil {
			return status_error.ManagedWorkspaceError
		}
		var intent deletionIntent
		jsonErr := json.Unmarshal(content, &intent)
		validationErr := c.validateIntent(intent)
		if jsonErr != nil || validationErr != nil || entry.Name() != intent.ID+".json" {
			logx.Errorf("decode deletion intent failed: file=%s decode_error=%v validation_error=%v", entry.Name(), jsonErr, validationErr)
			return status_error.ManagedWorkspaceError
		}
		if (intent.Targets[0].Kind == AggregateKindWaker) != wakers {
			continue
		}
		committed, decideErr := decideCommitted(intent)
		if decideErr != nil {
			return decideErr
		}
		staged := &StagedDeletion{coordinator: c, intent: intent}
		if committed {
			if commitErr := staged.Commit(); commitErr != nil {
				return commitErr
			}
		} else if rollbackErr := staged.Rollback(); rollbackErr != nil {
			return rollbackErr
		}
		if completed != nil {
			if completeErr := completed(intent.ID); completeErr != nil {
				return completeErr
			}
		}
	}
	return nil
}

func (c *DeletionCoordinator) aggregatePath(kind AggregateKind, aggregateID, wakerID, workspaceDirectory string) (string, error) {
	if !safeSegment(aggregateID) {
		return "", status_error.BadRequest
	}
	switch kind {
	case AggregateKindChat:
		return c.layout.ChatRoot(aggregateID)
	case AggregateKindChatProjectEntries:
		if !safeSegment(wakerID) || !filepath.IsAbs(workspaceDirectory) {
			return "", status_error.BadRequest
		}
		agentRoot, err := c.layout.AgentWorkspace(wakerID)
		if err != nil {
			return "", err
		}
		if filepath.Clean(workspaceDirectory) != agentRoot {
			// 隔离 worker 目录继续归原工作流；这里只允许删除它的 Chat 入口子树。
			workflowRoot := filepath.Join(c.layout.Root(), constants.WorkflowWorkspaceDirectoryName)
			relative, err := filepath.Rel(workflowRoot, workspaceDirectory)
			parts := strings.Split(filepath.ToSlash(relative), "/")
			if err != nil || len(parts) != 4 || !safeSegment(parts[0]) || parts[1] != constants.WorkflowTemporaryDirectoryName || parts[2] != constants.WorkflowWorkerDirectoryName || !safeSegment(parts[3]) {
				return "", status_error.BadRequest
			}
		}
		return filepath.Join(workspaceDirectory, constants.ProjectEntriesDirectoryName, aggregateID), nil
	case AggregateKindTeam:
		return c.layout.TeamRoot(aggregateID)
	case AggregateKindWaker:
		return c.layout.AgentWorkspace(aggregateID)
	default:
		return "", status_error.BadRequest
	}
}

func (c *DeletionCoordinator) writeIntent(intent deletionIntent) error {
	if err := c.validateIntent(intent); err != nil {
		return err
	}
	data, err := json.Marshal(intent)
	if err != nil {
		return status_error.ManagedWorkspaceError
	}
	if err := os.MkdirAll(c.intentRoot, 0o700); err != nil {
		return status_error.ManagedWorkspaceError
	}
	path := filepath.Join(c.intentRoot, intent.ID+".json")
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return status_error.ManagedWorkspaceError
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temporary, path)
	}
	if err == nil {
		err = syncDirectory(c.intentRoot)
	}
	if err != nil {
		_ = os.Remove(temporary)
		logx.Errorf("persist deletion intent failed: intent_id=%s error=%v", intent.ID, err)
		return status_error.ManagedWorkspaceError
	}
	return nil
}

func (c *DeletionCoordinator) removeIntent(intent deletionIntent) error {
	if err := os.Remove(filepath.Join(c.intentRoot, intent.ID+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return status_error.ManagedWorkspaceError
	}
	if err := syncDirectoryIfExists(c.intentRoot); err != nil {
		return status_error.ManagedWorkspaceError
	}
	if err := os.RemoveAll(filepath.Join(c.stagingRoot, intent.ID)); err != nil {
		return status_error.ManagedWorkspaceError
	}
	if err := syncDirectoryIfExists(c.stagingRoot); err != nil {
		return status_error.ManagedWorkspaceError
	}
	return nil
}

func (c *DeletionCoordinator) validateIntent(intent deletionIntent) error {
	if intent.Version != deletionIntentVersion || !safeSegment(intent.ID) ||
		len(intent.Targets) == 0 {
		return status_error.ManagedWorkspaceError
	}
	if intent.State != "prepared" && intent.State != "staged" && intent.State != "committed" {
		return status_error.ManagedWorkspaceError
	}
	if intent.Targets[0].Kind == AggregateKindWaker {
		if len(intent.Targets) != 1 || len(intent.ChatIDs) != 0 {
			return status_error.ManagedWorkspaceError
		}
	} else if len(intent.ChatIDs) == 0 {
		return status_error.ManagedWorkspaceError
	}
	previousChatID := ""
	for _, chatID := range intent.ChatIDs {
		if !safeSegment(chatID) || (previousChatID != "" && chatID <= previousChatID) {
			return status_error.ManagedWorkspaceError
		}
		previousChatID = chatID
	}
	seenTargets := make(map[string]struct{}, len(intent.Targets))
	for _, target := range intent.Targets {
		expected, err := c.aggregatePath(target.Kind, target.AggregateID, target.WakerID, target.WorkspaceDirectory)
		if err != nil || filepath.Clean(expected) != filepath.Clean(target.OriginalPath) {
			return status_error.ManagedWorkspaceError
		}
		key := string(target.Kind) + "\x00" + target.AggregateID
		if _, duplicated := seenTargets[key]; duplicated {
			return status_error.ManagedWorkspaceError
		}
		seenTargets[key] = struct{}{}
		expectedStaged := filepath.Join(
			c.stagingRoot,
			intent.ID,
			string(target.Kind)+"-"+target.AggregateID,
		)
		if filepath.Clean(expectedStaged) != filepath.Clean(target.StagedPath) {
			return status_error.ManagedWorkspaceError
		}
	}
	return nil
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !safeSegment(value) {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func syncRename(sourcePath string, targetPath string) error {
	if err := syncDirectory(filepath.Dir(sourcePath)); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(targetPath))
}

func syncDirectoryIfExists(path string) error {
	exists, err := pathExists(path)
	if err != nil || !exists {
		return err
	}
	return syncDirectory(path)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func wakerDeletionTargets(targets []DeletionTarget) bool {
	return len(targets) == 1 && targets[0].Kind == AggregateKindWaker && safeSegment(targets[0].AggregateID)
}
