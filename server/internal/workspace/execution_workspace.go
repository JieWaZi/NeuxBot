package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// ProjectLink 保存一个进入 Chat projects/ 命名空间的项目来源。
type ProjectLink struct {
	// EntryName 是工作区 projects/ 下的稳定入口名。
	EntryName string
	// SourcePath 是需要被只读引用的真实项目根目录。
	SourcePath string
}

// EnsureExecutionWorkspace 创建 Chat 工作区及其稳定目录和项目软链接。
func EnsureExecutionWorkspace(root, chatID, wakerID string, projects []ProjectLink) error {
	root = strings.TrimSpace(root)
	if root == "" || chatID == "" || wakerID == "" {
		return status_error.ManagedWorkspaceError
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return status_error.ManagedWorkspaceError
	}
	markerPath := filepath.Join(root, constants.ChatContextOwnerFileName)
	if content, err := os.ReadFile(markerPath); err == nil {
		var marker ownerMarker
		if json.Unmarshal(content, &marker) != nil ||
			marker.Kind != constants.ChatExecutionWorkspaceOwnerKind ||
			marker.ChatID != chatID ||
			marker.WakerID != wakerID {
			return status_error.ChatDirectoryOwnershipInvalid
		}
	} else if !os.IsNotExist(err) {
		return status_error.ManagedWorkspaceError
	} else if err := writeJSON(
		markerPath,
		ownerMarker{
			Kind:    constants.ChatExecutionWorkspaceOwnerKind,
			ChatID:  chatID,
			WakerID: wakerID,
		},
		status_error.ManagedWorkspaceError,
	); err != nil {
		return err
	}
	return ensureExecutionWorkspaceLayout(root, projects)
}

// EnsureThreadMemberExecutionWorkspace 创建或校验团队成员独占的 Runtime cwd。
func EnsureThreadMemberExecutionWorkspace(
	root string,
	threadID string,
	chatID string,
	wakerID string,
	sharedProjectsRoot string,
) error {
	root = strings.TrimSpace(root)
	threadID = strings.TrimSpace(threadID)
	chatID = strings.TrimSpace(chatID)
	wakerID = strings.TrimSpace(wakerID)
	sharedProjectsRoot = strings.TrimSpace(sharedProjectsRoot)
	if root == "" || threadID == "" || chatID == "" || wakerID == "" || sharedProjectsRoot == "" {
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
	return ensureRelativeDirectoryLink(
		filepath.Join(root, constants.ExecutionWorkspaceProjectsDirectory),
		root,
		sharedProjectsRoot,
	)
}

// EnsureThreadExecutionWorkspace 创建或校验团队对话 thread 级共享执行工作区。
func EnsureThreadExecutionWorkspace(
	root, threadID, leadWakerID string,
	projects []ProjectLink,
) error {
	root = strings.TrimSpace(root)
	threadID = strings.TrimSpace(threadID)
	leadWakerID = strings.TrimSpace(leadWakerID)
	if root == "" || threadID == "" || leadWakerID == "" {
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
	return ensureThreadExecutionWorkspaceLayout(root, projects)
}

func ensureExecutionWorkspaceLayout(root string, projects []ProjectLink) error {
	if err := os.MkdirAll(filepath.Join(root, constants.ExecutionWorkspaceProjectsDirectory), 0o700); err != nil {
		return status_error.ManagedWorkspaceError
	}
	if err := ensureExecutionWorkspaceFileDirectories(root); err != nil {
		return err
	}
	return ensureProjectLinks(filepath.Join(root, constants.ExecutionWorkspaceProjectsDirectory), projects)
}

func ensureExecutionWorkspaceFileDirectories(root string) error {
	generatedFiles := filepath.Join(
		root,
		constants.ExecutionWorkspaceAttachmentsDirectory,
		constants.ExecutionWorkspaceGeneratedFilesDirectory,
	)
	if err := os.MkdirAll(generatedFiles, 0o700); err != nil {
		return status_error.ManagedWorkspaceError
	}
	return nil
}

func ensureThreadExecutionWorkspaceLayout(root string, projects []ProjectLink) error {
	projectsRoot := filepath.Join(root, constants.ExecutionWorkspaceProjectsDirectory)
	if err := os.MkdirAll(projectsRoot, 0o700); err != nil {
		return status_error.ManagedWorkspaceError
	}
	return ensureProjectLinks(projectsRoot, projects)
}

func ensureProjectLinks(projectsRoot string, projects []ProjectLink) error {
	for _, project := range projects {
		if filepath.Base(project.EntryName) != project.EntryName ||
			project.EntryName == "." ||
			project.EntryName == ".." ||
			project.SourcePath == "" {
			return status_error.ManagedWorkspaceError
		}
		entry := filepath.Join(projectsRoot, project.EntryName)
		info, err := os.Lstat(entry)
		if err == nil {
			if info.Mode()&os.ModeSymlink == 0 {
				return status_error.ProjectSourceUnavailable
			}
			linked, readErr := os.Readlink(entry)
			if readErr != nil || linked != project.SourcePath {
				return status_error.ProjectSourceUnavailable
			}
			continue
		}
		if !os.IsNotExist(err) {
			return status_error.ProjectSourceUnavailable
		}
		if err := os.Symlink(project.SourcePath, entry); err != nil {
			return status_error.ProjectSourceUnavailable
		}
	}
	return nil
}

func ensureRelativeDirectoryLink(linkPath string, linkRoot string, targetPath string) error {
	relativeTarget, err := filepath.Rel(linkRoot, targetPath)
	if err != nil || relativeTarget == "." {
		return status_error.ManagedWorkspaceError
	}
	info, err := os.Lstat(linkPath)
	if err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return status_error.ChatDirectoryOwnershipInvalid
		}
		linked, readErr := os.Readlink(linkPath)
		if readErr != nil || filepath.Clean(linked) != filepath.Clean(relativeTarget) {
			return status_error.ChatDirectoryOwnershipInvalid
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return status_error.ManagedWorkspaceError
	}
	if err := os.Symlink(relativeTarget, linkPath); err != nil {
		return status_error.ManagedWorkspaceError
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

// DeleteExecutionWorkspace 删除经过归属校验的整个 Chat 执行工作区。
func DeleteExecutionWorkspace(root, chatID string) error {
	if err := ValidateExecutionWorkspaceDeletion(root, chatID); err != nil {
		return err
	}
	if err := os.RemoveAll(root); err != nil {
		return status_error.ManagedWorkspaceError
	}
	return nil
}

// DeleteThreadExecutionWorkspace 删除经过归属校验的整个 thread 共享执行工作区。
func DeleteThreadExecutionWorkspace(root, threadID string) error {
	if err := ValidateThreadExecutionWorkspaceDeletion(root, threadID); err != nil {
		return err
	}
	if err := os.RemoveAll(root); err != nil {
		return status_error.ManagedWorkspaceError
	}
	return nil
}

// ValidateExecutionWorkspaceDeletion 只校验 Chat 工作区归属，不产生文件副作用。
func ValidateExecutionWorkspaceDeletion(root, chatID string) error {
	owner, err := ReadExecutionWorkspaceOwner(root)
	if err != nil {
		return err
	}
	if owner.Kind == "" {
		return nil
	}
	if owner.Kind != constants.ChatExecutionWorkspaceOwnerKind || owner.ChatID != chatID {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	return nil
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
	projects := filepath.Join(root, constants.ExecutionWorkspaceProjectsDirectory)
	info, err := os.Lstat(projects)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	resolvedProjects, err := filepath.EvalSymlinks(projects)
	if err != nil {
		return status_error.ProjectSourceUnavailable
	}
	projectInfo, err := os.Stat(resolvedProjects)
	if err != nil || !projectInfo.IsDir() {
		return status_error.ProjectSourceUnavailable
	}
	return ensureExecutionWorkspaceFileDirectories(root)
}
