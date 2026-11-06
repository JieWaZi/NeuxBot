package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// ProjectLink 保存真实项目来源及其独立展示标签，是否投射入口由原生目录能力决定。
type ProjectLink struct {
	// EntryName 是来源的稳定展示标签。
	EntryName string
	// SourcePath 是已授权的真实项目根目录。
	SourcePath string
}

// EnsureAgentWorkspace 创建或校验伙伴拥有的固定 cwd，不以 Chat 作为目录归属。
func EnsureAgentWorkspace(root, wakerID string) error {
	root = strings.TrimSpace(root)
	if root == "" || wakerID == "" {
		return status_error.ManagedWorkspaceError
	}
	if info, err := os.Lstat(root); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return status_error.ChatDirectoryOwnershipInvalid
	} else if err != nil && !os.IsNotExist(err) {
		return status_error.ManagedWorkspaceError
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return status_error.ManagedWorkspaceError
	}
	markerPath := filepath.Join(root, constants.ChatContextOwnerFileName)
	if content, err := os.ReadFile(markerPath); err == nil {
		var marker ownerMarker
		if json.Unmarshal(content, &marker) != nil ||
			marker.Kind != constants.AgentExecutionWorkspaceOwnerKind ||
			marker.WakerID != wakerID {
			return status_error.ChatDirectoryOwnershipInvalid
		}
	} else if !os.IsNotExist(err) {
		return status_error.ManagedWorkspaceError
	} else if err := writeJSON(
		markerPath,
		ownerMarker{
			Kind:    constants.AgentExecutionWorkspaceOwnerKind,
			WakerID: wakerID,
		},
		status_error.ManagedWorkspaceError,
	); err != nil {
		return err
	}
	return ensureExecutionWorkspaceFileDirectories(root)
}

// EnsureThreadMemberExecutionWorkspace 创建或校验团队成员独占的 Runtime cwd。
func EnsureThreadMemberExecutionWorkspace(
	root string,
	threadID string,
	chatID string,
	wakerID string,
) error {
	root = strings.TrimSpace(root)
	threadID = strings.TrimSpace(threadID)
	chatID = strings.TrimSpace(chatID)
	wakerID = strings.TrimSpace(wakerID)
	if root == "" || threadID == "" || chatID == "" || wakerID == "" {
		return status_error.ManagedWorkspaceError
	}
	if info, err := os.Lstat(root); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return status_error.ChatDirectoryOwnershipInvalid
	} else if err != nil && !os.IsNotExist(err) {
		return status_error.ManagedWorkspaceError
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return status_error.ManagedWorkspaceError
	}
	markerPath := filepath.Join(root, constants.ChatContextOwnerFileName)
	if content, err := os.ReadFile(markerPath); err == nil {
		var marker ownerMarker
		if json.Unmarshal(content, &marker) != nil ||
			marker.Kind != constants.ThreadMemberExecutionWorkspaceOwnerKind ||
			marker.ThreadID != threadID ||
			marker.ChatID != chatID ||
			marker.WakerID != wakerID {
			return status_error.ChatDirectoryOwnershipInvalid
		}
	} else if !os.IsNotExist(err) {
		return status_error.ManagedWorkspaceError
	} else if err := writeJSON(
		markerPath,
		ownerMarker{
			Kind:     constants.ThreadMemberExecutionWorkspaceOwnerKind,
			ThreadID: threadID,
			ChatID:   chatID,
			WakerID:  wakerID,
		},
		status_error.ManagedWorkspaceError,
	); err != nil {
		return err
	}
	if err := ensureExecutionWorkspaceFileDirectories(root); err != nil {
		return err
	}
	return nil
}

// EnsureThreadExecutionWorkspace 创建或校验团队对话 thread 级共享执行工作区。
func EnsureThreadExecutionWorkspace(
	root, threadID, leadWakerID string,
) error {
	root = strings.TrimSpace(root)
	threadID = strings.TrimSpace(threadID)
	leadWakerID = strings.TrimSpace(leadWakerID)
	if root == "" || threadID == "" || leadWakerID == "" {
		return status_error.ManagedWorkspaceError
	}
	if info, err := os.Lstat(root); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return status_error.ChatDirectoryOwnershipInvalid
	} else if err != nil && !os.IsNotExist(err) {
		return status_error.ManagedWorkspaceError
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return status_error.ManagedWorkspaceError
	}
	markerPath := filepath.Join(root, constants.ChatContextOwnerFileName)
	if content, err := os.ReadFile(markerPath); err == nil {
		var marker ownerMarker
		if json.Unmarshal(content, &marker) != nil ||
			marker.Kind != constants.ThreadExecutionWorkspaceOwnerKind ||
			marker.ThreadID != threadID {
			return status_error.ChatDirectoryOwnershipInvalid
		}
	} else if !os.IsNotExist(err) {
		return status_error.ManagedWorkspaceError
	} else if err := writeJSON(
		markerPath,
		ownerMarker{
			Kind:     constants.ThreadExecutionWorkspaceOwnerKind,
			ThreadID: threadID,
			WakerID:  leadWakerID,
		},
		status_error.ManagedWorkspaceError,
	); err != nil {
		return err
	}
	_, err := ensureManagedRoot(root, constants.TeamSharedDirectoryName)
	return err
}

func ensureExecutionWorkspaceFileDirectories(root string) error {
	for _, name := range []string{constants.TaskOutputsDirectoryName, "tmp"} {
		if _, err := ensureManagedRoot(root, name); err != nil {
			return err
		}
	}
	return nil
}

func isSafeDirectoryName(value string) bool {
	return isSafeFileName(value)
}

func isSafeFileName(value string) bool {
	return value != "" && filepath.Base(value) == value && value != "." && value != ".." && !strings.ContainsAny(value, "/\\\x00\r\n")
}

// ReadProjectRootInstructions 读取项目根目录中与当前 Runtime 匹配的指令文件，不递归子目录。
func ReadProjectRootInstructions(root string, fileNames []string) (map[string]string, error) {
	contents := make(map[string]string, len(fileNames))
	for _, name := range fileNames {
		content, err := os.ReadFile(filepath.Join(root, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, status_error.ProjectSourceUnavailable
		}
		contents[name] = string(content)
	}
	return contents, nil
}

// ExecutionWorkspaceOwner 保存受管执行工作区 marker 解析出的归属信息。
type ExecutionWorkspaceOwner struct {
	// Kind 是受管目录归属种类。
	Kind string
	// ChatID 是对话执行工作区所属 Chat 标识。
	ChatID string
	// ThreadID 是 thread 级共享执行工作区所属团队对话标识。
	ThreadID string
	// WakerID 是成员工作区所属伙伴标识。
	WakerID string
}

// ReadExecutionWorkspaceOwner 读取并解析受管执行工作区归属标记。
func ReadExecutionWorkspaceOwner(root string) (ExecutionWorkspaceOwner, error) {
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return ExecutionWorkspaceOwner{}, nil
	}
	if err != nil {
		return ExecutionWorkspaceOwner{}, status_error.ManagedWorkspaceError
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ExecutionWorkspaceOwner{}, status_error.ChatDirectoryOwnershipInvalid
	}
	markerPath := filepath.Join(root, constants.ChatContextOwnerFileName)
	content, err := os.ReadFile(markerPath)
	if err != nil {
		if os.IsNotExist(err) {
			if _, statErr := os.Stat(root); os.IsNotExist(statErr) {
				return ExecutionWorkspaceOwner{}, nil
			}
			return ExecutionWorkspaceOwner{}, status_error.ChatDirectoryOwnershipInvalid
		}
		return ExecutionWorkspaceOwner{}, status_error.ManagedWorkspaceError
	}
	var marker ownerMarker
	if err := json.Unmarshal(content, &marker); err != nil {
		return ExecutionWorkspaceOwner{}, status_error.ChatDirectoryOwnershipInvalid
	}
	return ExecutionWorkspaceOwner{
		Kind:     marker.Kind,
		ChatID:   marker.ChatID,
		ThreadID: marker.ThreadID,
		WakerID:  marker.WakerID,
	}, nil
}

// ValidateThreadExecutionWorkspaceDeletion 只校验 thread 共享工作区归属，不产生文件副作用。
func ValidateThreadExecutionWorkspaceDeletion(root, threadID string) error {
	owner, err := ReadExecutionWorkspaceOwner(root)
	if err != nil {
		return err
	}
	if owner.Kind == "" {
		return nil
	}
	if owner.Kind != constants.ThreadExecutionWorkspaceOwnerKind || owner.ThreadID != threadID {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	return nil
}

// ValidateThreadMemberExecutionWorkspace 只校验团队成员 cwd 归属，不产生文件副作用。
func ValidateThreadMemberExecutionWorkspace(root string, threadID string, chatID string, wakerID string) error {
	owner, err := ReadExecutionWorkspaceOwner(root)
	if err != nil {
		return err
	}
	if owner.Kind != constants.ThreadMemberExecutionWorkspaceOwnerKind ||
		owner.ThreadID != threadID ||
		owner.ChatID != chatID ||
		owner.WakerID != wakerID {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	return nil
}

// EnsureExistingThreadMemberExecutionWorkspace 校验成员 cwd 并恢复可再生的会话文件目录。
func EnsureExistingThreadMemberExecutionWorkspace(root string, threadID string, chatID string, wakerID string) error {
	if err := ValidateThreadMemberExecutionWorkspace(root, threadID, chatID, wakerID); err != nil {
		return err
	}
	return ensureExecutionWorkspaceFileDirectories(root)
}

// TaskDirectories 保存已分配 Execution 的持久成果与临时输入目录。
type TaskDirectories struct {
	// Outputs 是本轮新建持久成果的真实目录。
	Outputs string
	// Temporary 是本轮中间文件及原生输入副本的真实目录。
	Temporary string
}

// EnsureTaskDirectories 在既有 cwd 中为同一个 Execution 幂等准备目录，不分配新身份。
func EnsureTaskDirectories(root, executionID string) (TaskDirectories, error) {
	if !isSafeDirectoryName(executionID) {
		return TaskDirectories{}, status_error.BadRequest
	}
	outputs, err := ensureManagedRoot(root, constants.TaskOutputsDirectoryName, executionID)
	if err != nil {
		return TaskDirectories{}, err
	}
	temporary, err := ensureManagedRoot(root, "tmp", executionID)
	if err != nil {
		return TaskDirectories{}, err
	}
	return TaskDirectories{Outputs: outputs, Temporary: temporary}, nil
}

// EnsureIsolatedExecutionWorkspace 校验宿主领域拥有的独立 Chat 执行目录，不复用普通伙伴 cwd。
func EnsureIsolatedExecutionWorkspace(root, chatID, wakerID string) error {
	if root == "" || chatID == "" || wakerID == "" {
		return status_error.ManagedWorkspaceError
	}
	if info, err := os.Lstat(root); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return status_error.ChatDirectoryOwnershipInvalid
	} else if err != nil && !os.IsNotExist(err) {
		return status_error.ManagedWorkspaceError
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return status_error.ManagedWorkspaceError
	}
	marker := filepath.Join(root, constants.ChatContextOwnerFileName)
	if content, err := os.ReadFile(marker); err == nil {
		var owner ownerMarker
		if json.Unmarshal(content, &owner) != nil || owner.Kind != constants.IsolatedExecutionWorkspaceOwnerKind || owner.ChatID != chatID || owner.WakerID != wakerID {
			return status_error.ChatDirectoryOwnershipInvalid
		}
	} else if !os.IsNotExist(err) {
		return status_error.ManagedWorkspaceError
	} else if err := writeJSON(marker, ownerMarker{Kind: constants.IsolatedExecutionWorkspaceOwnerKind, ChatID: chatID, WakerID: wakerID}, status_error.ManagedWorkspaceError); err != nil {
		return err
	}
	return ensureExecutionWorkspaceFileDirectories(root)
}

// ValidateAgentWorkspaceDeletion 仅接受伙伴自身归属的固定 cwd，供名称确认后的伙伴删除用例使用。
func ValidateAgentWorkspaceDeletion(root, wakerID string) error {
	owner, err := ReadExecutionWorkspaceOwner(root)
	if err != nil {
		return err
	}
	if owner.Kind == "" {
		return nil
	}
	if owner.Kind != constants.AgentExecutionWorkspaceOwnerKind || owner.WakerID != wakerID {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	return nil
}

// ProjectEntry 保存一个原生工具入口及其未改变的真实授权来源。
type ProjectEntry struct {
	// SourcePath 是已冻结并规范化的真实项目根。
	SourcePath string `json:"sourcePath"`
	// Path 是相对实际 cwd 的 Chat 独有入口，不改变真实文件身份。
	Path string `json:"path"`
}

// PrepareProjectEntries 仅物化当前 Chat 的项目入口，保留其他 Chat 与用户管理的文件。
func PrepareProjectEntries(root, chatID string, projects []ProjectLink) ([]ProjectEntry, func() error, error) {
	noop := func() error { return nil }
	if len(projects) == 0 {
		return nil, noop, nil
	}
	if !isSafeDirectoryName(chatID) {
		return nil, noop, status_error.BadRequest
	}
	projectsRoot, err := ensureManagedRoot(root, constants.ProjectEntriesDirectoryName)
	if err != nil {
		return nil, noop, err
	}
	directory := filepath.Join(projectsRoot, chatID)
	created := false
	if err := os.Mkdir(directory, 0700); err == nil {
		created = true
	} else if !os.IsExist(err) {
		return nil, noop, status_error.ManagedWorkspaceError
	}
	rollback := func() error {
		if !created {
			return nil
		}
		if err := validateOwner(directory, constants.ChatProjectEntriesOwnerKind, chatID, ""); err != nil {
			return err
		}
		return removeValidatedDirectory(directory)
	}
	if created {
		if err := writeJSON(filepath.Join(directory, constants.ChatContextOwnerFileName), ownerMarker{Kind: constants.ChatProjectEntriesOwnerKind, ChatID: chatID}, status_error.ManagedWorkspaceError); err != nil {
			_ = os.Remove(directory)
			return nil, noop, err
		}
	} else {
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, noop, status_error.ChatDirectoryOwnershipInvalid
		}
		if err := validateOwner(directory, constants.ChatProjectEntriesOwnerKind, chatID, ""); err != nil {
			return nil, noop, err
		}
	}
	entries := make([]ProjectEntry, 0, len(projects))
	for _, project := range projects {
		if !isSafeDirectoryName(project.EntryName) || !filepath.IsAbs(project.SourcePath) {
			_ = rollback()
			return nil, noop, status_error.ProjectSourceUnavailable
		}
		source, err := filepath.EvalSymlinks(project.SourcePath)
		if err != nil {
			_ = rollback()
			return nil, noop, status_error.ProjectSourceUnavailable
		}
		info, err := os.Stat(source)
		if err != nil || !info.IsDir() {
			_ = rollback()
			return nil, noop, status_error.ProjectSourceUnavailable
		}
		path := filepath.Join(directory, project.EntryName)
		if info, err := os.Lstat(path); err == nil {
			target, readErr := os.Readlink(path)
			if info.Mode()&os.ModeSymlink == 0 || readErr != nil || target != source {
				_ = rollback()
				return nil, noop, status_error.ProjectSourceUnavailable
			}
		} else if !os.IsNotExist(err) {
			_ = rollback()
			return nil, noop, status_error.ProjectSourceUnavailable
		} else if err := os.Symlink(source, path); err != nil {
			_ = rollback()
			return nil, noop, status_error.ProjectSourceUnavailable
		}
		entries = append(entries, ProjectEntry{SourcePath: source, Path: filepath.ToSlash(filepath.Join(constants.ProjectEntriesDirectoryName, chatID, project.EntryName))})
	}
	return entries, rollback, nil
}

// ValidateProjectEntriesDeletion 只接受当前 Chat 拥有的真实入口目录，不跟随替换链接。
func ValidateProjectEntriesDeletion(directory, chatID string) error {
	owner, err := ReadExecutionWorkspaceOwner(directory)
	if err != nil {
		return err
	}
	if owner.Kind != constants.ChatProjectEntriesOwnerKind || owner.ChatID != chatID {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	return nil
}
