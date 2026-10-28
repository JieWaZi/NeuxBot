package runtimes

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// validateWorkspace 规范化 Runtime 工作目录与额外授权目录。
func validateWorkspace(workspace Workspace) (Workspace, error) {
	if strings.TrimSpace(workspace.Path) == "" {
		return Workspace{}, status_error.BadRequest
	}
	absolute, err := validateDirectoryPath(workspace.Path, "workspace")
	if err != nil {
		return Workspace{}, err
	}
	workspace.Path = absolute
	directories := make([]string, 0, len(workspace.AdditionalWritableDirectories))
	seen := make(map[string]struct{}, len(workspace.AdditionalWritableDirectories))
	for index, directory := range workspace.AdditionalWritableDirectories {
		validated, err := validateDirectoryPath(
			directory,
			fmt.Sprintf("additional writable directory %d", index),
		)
		if err != nil {
			return Workspace{}, err
		}
		key := normalizedPathForComparison(validated)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		directories = append(directories, validated)
	}
	workspace.AdditionalWritableDirectories = directories
	projectDirectories, err := validateWorkspaceProjectDirectories(
		workspace.Path,
		workspace.ProjectDirectories,
	)
	if err != nil {
		return Workspace{}, err
	}
	workspace.ProjectDirectories = projectDirectories
	return workspace, nil
}

// validateWorkspaceProjectDirectories 校验项目入口位于受管工作区 projects 命名空间且只有一个默认目录。
func validateWorkspaceProjectDirectories(
	workspacePath string,
	directories []WorkspaceProjectDirectory,
) ([]WorkspaceProjectDirectory, error) {
	validated := make([]WorkspaceProjectDirectory, 0, len(directories))
	seen := make(map[string]struct{}, len(directories))
	defaultCount := 0
	for _, directory := range directories {
		rawPath := strings.TrimSpace(directory.Path)
		cleanPath := filepath.Clean(rawPath)
		pathParts := strings.Split(filepath.ToSlash(cleanPath), "/")
		if filepath.IsAbs(rawPath) || len(pathParts) != 2 ||
			pathParts[0] != constants.ExecutionWorkspaceProjectsDirectory ||
			pathParts[1] == "" {
			return nil, status_error.BadRequest
		}
		absolutePath := filepath.Join(workspacePath, cleanPath)
		info, err := os.Stat(absolutePath)
		if err != nil {
			return nil, status_error.RuntimeUnavailable
		}
		if !info.IsDir() {
			return nil, status_error.BadRequest
		}
		key := normalizedPathForComparison(absolutePath)
		if _, exists := seen[key]; exists {
			return nil, status_error.BadRequest
		}
		seen[key] = struct{}{}
		if directory.Default {
			defaultCount++
		}
		validated = append(validated, WorkspaceProjectDirectory{
			Path:    filepath.ToSlash(cleanPath),
			Default: directory.Default,
		})
	}
	if len(validated) > 0 && defaultCount != 1 {
		return nil, status_error.BadRequest
	}
	return validated, nil
}

// validateDirectoryPath 返回一个已经存在的绝对目录路径。
func validateDirectoryPath(path string, name string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", status_error.BadRequest
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", status_error.RuntimeUnavailable
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", status_error.RuntimeUnavailable
	}
	if !info.IsDir() {
		return "", status_error.BadRequest
	}
	return absolute, nil
}

// validateExecutionPolicy 要求调用方冻结三档产品权限；ACP Mode 由 Binding 投影。
func validateExecutionPolicy(policy ExecutionPolicy) error {
	if !constants.SelectablePermissionLevel(policy.PermissionLevel) {
		return status_error.BadRequest
	}
	return nil
}
