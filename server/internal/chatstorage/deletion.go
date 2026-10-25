package chatstorage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

const (
	deletionDirectoryName        = "deletions"
	deletionIntentDirectoryName  = "intents"
	deletionStagingDirectoryName = "staging"
	deletionIntentVersion        = 1
)

// AggregateKind 区分普通 Chat 与 Team 整体聚合目录。
type AggregateKind string

const (
	// AggregateKindChat 表示 `chat/<chatID>` 聚合目录。
	AggregateKindChat AggregateKind = "chat"
	// AggregateKindTeam 表示 `team/<threadID>` 聚合目录。
	AggregateKindTeam AggregateKind = "team"
)

// DeletionTarget 是经过业务层归属预检的一个完整聚合目录。
type DeletionTarget struct {
	// Kind 是普通 Chat 或 Team 聚合类型。
	Kind AggregateKind
	// AggregateID 是 Chat ID 或 Thread ID。
	AggregateID string
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
	// Kind 是普通 Chat 或 Team 聚合类型。
	Kind AggregateKind `json:"kind"`
	// AggregateID 是 Chat ID 或 Thread ID。
	AggregateID string `json:"aggregateId"`
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
	if c == nil || !safeSegment(intentID) || len(targets) == 0 || len(chatIDs) == 0 {
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
		originalPath, pathErr := c.aggregatePath(target.Kind, target.AggregateID)
		if pathErr != nil {
			return nil, pathErr
		}
		key := string(target.Kind) + "\x00" + target.AggregateID
		if _, duplicated := seen[key]; duplicated {
			continue
		}
		seen[key] = struct{}{}
		intent.Targets = append(intent.Targets, deletionIntentTarget{
			Kind:         target.Kind,
			AggregateID:  target.AggregateID,
			OriginalPath: originalPath,
			StagedPath:   filepath.Join(intentStagingRoot, string(target.Kind)+"-"+target.AggregateID),
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
		if _, statErr := os.Stat(target.OriginalPath); statErr != nil {
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
		committed, decideErr := decideCommitted(intent.ID, append([]string(nil), intent.ChatIDs...))
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

func (c *DeletionCoordinator) aggregatePath(kind AggregateKind, aggregateID string) (string, error) {
	if !safeSegment(aggregateID) {
		return "", status_error.BadRequest
	}
	switch kind {
	case AggregateKindChat:
		return c.layout.ChatRoot(aggregateID)
	case AggregateKindTeam:
		return c.layout.TeamRoot(aggregateID)
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
		len(intent.ChatIDs) == 0 || len(intent.Targets) == 0 {
		return status_error.ManagedWorkspaceError
	}
	if intent.State != "prepared" && intent.State != "staged" && intent.State != "committed" {
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
		expected, err := c.aggregatePath(target.Kind, target.AggregateID)
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
