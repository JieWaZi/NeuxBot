// Package chatstorage 统一规划普通 Chat 与团队成员 Chat 的权威日志和受管工作区路径。
package chatstorage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
)

const (
	chatDirectoryName         = "Conversations"
	teamDirectoryName         = "Workspaces"
	logDirectoryName          = "log"
	teamLeaderDirectoryName   = "leader"
	teamMembersDirectory      = "members"
	teamSharedDirectoryName   = "shared"
	provisioningDirectoryName = "provisioning"
)

// Layout 封装受管文件根目录中 Chat/Team 聚合路径的全部拼接规则。
type Layout struct {
	// root 是 Chat 与 Team 聚合目录共同使用的受管文件根目录。
	root string
}

// TeamProvisioningLayout 返回一次 provisioning 独占的临时布局根，不产生文件副作用。
func (l *Layout) TeamProvisioningLayout(provisioningID string) (*Layout, error) {
	if l == nil || !safeSegment(provisioningID) {
		return nil, status_error.BadRequest
	}
	return &Layout{root: filepath.Join(l.root, provisioningDirectoryName, provisioningID)}, nil
}

// PublishTeamProvisioning 校验 staging 归属后把完整 Team 聚合目录原子发布到正式位置。
func (l *Layout) PublishTeamProvisioning(provisioningID string, threadID string) error {
	staging, err := l.TeamProvisioningLayout(provisioningID)
	if err != nil {
		return err
	}
	source, err := staging.TeamRoot(threadID)
	if err != nil {
		return err
	}
	target, err := l.TeamRoot(threadID)
	if err != nil {
		return err
	}
	workspaceRoot, err := staging.TeamWorkspace(threadID)
	if err != nil {
		return err
	}
	if err := validateProvisioningWorkspace(workspaceRoot, threadID); err != nil {
		return err
	}
	if _, err := os.Stat(target); err == nil || !errors.Is(err, os.ErrNotExist) {
		return status_error.ManagedWorkspaceError
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return status_error.ManagedWorkspaceError
	}
	if err := os.Rename(source, target); err != nil {
		logx.Errorf(
			"publish team provisioning failed: provisioning_id=%s thread_id=%s source=%s target=%s error=%v",
			provisioningID,
			threadID,
			source,
			target,
			err,
		)
		return status_error.ManagedWorkspaceError
	}
	if err := syncRename(source, target); err != nil {
		return status_error.ManagedWorkspaceError
	}
	return l.removeEmptyProvisioningParents(provisioningID)
}

// RollbackTeamProvisioning 删除尚未发布的精确 staging 聚合目录。
func (l *Layout) RollbackTeamProvisioning(provisioningID string, threadID string) error {
	staging, err := l.TeamProvisioningLayout(provisioningID)
	if err != nil {
		return err
	}
	root, err := staging.TeamRoot(threadID)
	if err != nil {
		return err
	}
	exists, err := pathExists(root)
	if err != nil {
		return status_error.ManagedWorkspaceError
	}
	if !exists {
		return l.removeEmptyProvisioningParents(provisioningID)
	}
	workspaceRoot, err := staging.TeamWorkspace(threadID)
	if err != nil {
		return err
	}
	if err := validateProvisioningWorkspace(workspaceRoot, threadID); err != nil &&
		!safeIncompleteProvisioningWorkspace(root, workspaceRoot) {
		return err
	}
	if err := os.RemoveAll(root); err != nil {
		return status_error.ManagedWorkspaceError
	}
	return l.removeEmptyProvisioningParents(provisioningID)
}

// safeIncompleteProvisioningWorkspace 仅识别共享工作区归属标记落盘前的最小半成品树。
func safeIncompleteProvisioningWorkspace(root string, workspaceRoot string) bool {
	if filepath.Clean(root) != filepath.Clean(workspaceRoot) {
		return false
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) > 1 {
		return false
	}
	if len(entries) == 0 {
		return true
	}
	entry := entries[0]
	info, err = entry.Info()
	return err == nil && entry.Name() == constants.ChatContextOwnerFileName && info.Mode().IsRegular()
}

// validateProvisioningWorkspace 只接受带精确 thread 归属标记的 staging 工作区。
func validateProvisioningWorkspace(workspaceRoot string, threadID string) error {
	owner, err := workspace.ReadExecutionWorkspaceOwner(workspaceRoot)
	if err != nil {
		return err
	}
	if owner.Kind != constants.ThreadExecutionWorkspaceOwnerKind || owner.ThreadID != threadID {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	return nil
}

// removeEmptyProvisioningParents 清理一次 provisioning 发布或回滚后的空父目录。
func (l *Layout) removeEmptyProvisioningParents(provisioningID string) error {
	root := filepath.Join(l.root, provisioningDirectoryName, provisioningID)
	for _, path := range []string{
		filepath.Join(root, teamDirectoryName),
		root,
		filepath.Join(l.root, provisioningDirectoryName),
	} {
		if err := os.Remove(path); err != nil &&
			!errors.Is(err, os.ErrNotExist) &&
			!isDirectoryNotEmpty(err) {
			return status_error.ManagedWorkspaceError
		}
	}
	return nil
}

// NewLayout 创建只负责确定路径、不产生文件副作用的存储布局。
func NewLayout(root string) (*Layout, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, status_error.BadRequest
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		logx.Errorf("resolve chat storage root failed: root=%q error=%v", root, err)
		return nil, status_error.ManagedWorkspaceError
	}
	// 根尚未存在时规范化最长已存在父路径，再拼回待创建部分，不产生文件副作用。
	current := absoluteRoot
	missing := []string{}
	for {
		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr == nil {
			absoluteRoot = resolved
			for index := len(missing) - 1; index >= 0; index-- {
				absoluteRoot = filepath.Join(absoluteRoot, missing[index])
			}
			break
		}
		if !os.IsNotExist(resolveErr) || filepath.Dir(current) == current {
			logx.Errorf("resolve chat storage parent failed: root=%q parent=%q error=%v", root, current, resolveErr)
			return nil, status_error.ManagedWorkspaceError
		}
		missing = append(missing, filepath.Base(current))
		current = filepath.Dir(current)
	}
	return &Layout{root: absoluteRoot}, nil
}

// Root 返回 Chat 与 Team 聚合目录共同使用的受管文件根目录。
func (l *Layout) Root() string {
	if l == nil {
		return ""
	}
	return l.root
}

// ChatRoot 返回普通 Chat 的聚合目录。
func (l *Layout) ChatRoot(chatID string) (string, error) {
	if !safeSegment(chatID) {
		return "", status_error.ChatIDInvalid
	}
	return filepath.Join(l.root, chatDirectoryName, chatID), nil
}

// ChatLogDirectory 返回普通 Chat 的权威日志目录。
func (l *Layout) ChatLogDirectory(chatID string) (string, error) {
	root, err := l.ChatRoot(chatID)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, logDirectoryName), nil
}

// AgentWorkspace 返回伙伴拥有、在普通 Chat 之间共享的真实 Runtime 工作目录。
func (l *Layout) AgentWorkspace(wakerID string) (string, error) {
	if !safeSegment(wakerID) {
		return "", status_error.BadRequest
	}
	return filepath.Join(l.root, "Agents", wakerID), nil
}

// TeamRoot 返回团队对话的聚合目录。
func (l *Layout) TeamRoot(threadID string) (string, error) {
	if !safeSegment(threadID) {
		return "", status_error.BadRequest
	}
	return filepath.Join(l.root, teamDirectoryName, threadID), nil
}

// TeamWorkspace 返回团队对话拥有的共享范围，独立于全部成员的 Conversation。
func (l *Layout) TeamWorkspace(threadID string) (string, error) { return l.TeamRoot(threadID) }

// TeamSharedDirectory 返回团队成员共同使用的真实共享目录。
func (l *Layout) TeamSharedDirectory(threadID string) (string, error) {
	root, err := l.TeamWorkspace(threadID)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, teamSharedDirectoryName), nil
}

// TeamMemberWorkspace 返回指定成员独占的 Runtime cwd。
func (l *Layout) TeamMemberWorkspace(threadID string, wakerID string, lead bool) (string, error) {
	root, err := l.TeamWorkspace(threadID)
	if err != nil {
		return "", err
	}
	if lead {
		return filepath.Join(root, teamLeaderDirectoryName), nil
	}
	if !safeSegment(wakerID) {
		return "", status_error.BadRequest
	}
	return filepath.Join(root, teamMembersDirectory, wakerID), nil
}

// RemoveEmptyChatAggregate 清理回滚或删除后已经为空的普通 Chat 聚合目录。
func (l *Layout) RemoveEmptyChatAggregate(chatID string) error {
	root, err := l.ChatRoot(chatID)
	if err != nil {
		return err
	}
	for _, path := range []string{
		filepath.Join(root, constants.ExecutionWorkspaceAttachmentsDirectory),
		filepath.Join(root, "file-results"),
		filepath.Join(root, logDirectoryName),
		root,
		filepath.Join(l.root, chatDirectoryName),
	} {
		if err := os.Remove(path); err != nil &&
			!errors.Is(err, os.ErrNotExist) &&
			!isDirectoryNotEmpty(err) {
			logx.Errorf("remove empty chat aggregate failed: path=%s error=%v", path, err)
			return status_error.ManagedWorkspaceError
		}
	}
	return nil
}

// RemoveEmptyTeamAggregate 清理全部成员日志和工作区删除后的团队聚合目录。
func (l *Layout) RemoveEmptyTeamAggregate(threadID string) error {
	root, err := l.TeamRoot(threadID)
	if err != nil {
		return err
	}
	for _, path := range []string{
		root,
		filepath.Join(l.root, teamDirectoryName),
	} {
		if err := os.Remove(path); err != nil &&
			!errors.Is(err, os.ErrNotExist) &&
			!isDirectoryNotEmpty(err) {
			logx.Errorf("remove empty team aggregate failed: path=%s error=%v", path, err)
			return status_error.ManagedWorkspaceError
		}
	}
	return nil
}

func isDirectoryNotEmpty(err error) bool {
	return errors.Is(err, os.ErrExist) || strings.Contains(strings.ToLower(err.Error()), "not empty")
}

func safeSegment(value string) bool {
	return value != "" &&
		filepath.Base(value) == value &&
		value != "." &&
		value != ".." &&
		!strings.ContainsAny(value, "/\\\x00\r\n")
}
