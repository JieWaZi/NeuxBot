package runtimes

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

const (
	defaultEventDeliveryTimeout = 30 * time.Second
	defaultInactivityTimeout    = 10 * time.Minute
	maxInactivityTimeout        = 24 * time.Hour
)

// newEvent 把稳定产品事件载荷编码为 Runtime Event。
func newEvent(eventType EventType, payload any) (Event, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return Event{}, status_error.RuntimeEventEncodingFailed
	}
	return Event{Type: eventType, Data: data}, nil
}

// normalizeDefaults 填充并校验 ACP Client 的资源保护默认值。
func normalizeDefaults(source Defaults) (Defaults, error) {
	defaults := source
	if defaults.Inactivity == 0 {
		defaults.Inactivity = defaultInactivityTimeout
	}
	if defaults.EventDelivery == 0 {
		defaults.EventDelivery = defaultEventDeliveryTimeout
	}
	if defaults.EventDelivery <= 0 {
		return Defaults{}, status_error.BadRequest
	}
	if defaults.Inactivity < 0 || defaults.Inactivity > maxInactivityTimeout {
		return Defaults{}, status_error.BadRequest
	}
	return defaults, nil
}

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
	seen := make(map[string]struct{}, len(policy.TrustedControlTools))
	for _, tool := range policy.TrustedControlTools {
		server := strings.TrimSpace(tool.Server)
		name := strings.TrimSpace(tool.Tool)
		if server == "" || name == "" || len(tool.Actions) == 0 {
			return status_error.BadRequest
		}
		key := server + "\x00" + name
		if _, exists := seen[key]; exists {
			return status_error.BadRequest
		}
		seen[key] = struct{}{}
		actions := make(map[string]struct{}, len(tool.Actions))
		for _, action := range tool.Actions {
			action = strings.TrimSpace(action)
			if action == "" {
				return status_error.BadRequest
			}
			if _, exists := actions[action]; exists {
				return status_error.BadRequest
			}
			actions[action] = struct{}{}
		}
	}
	return nil
}

// newFailure 创建带稳定瞬时性分类的 Runtime 失败。
func newFailure(code FailureCode, stage string, message string) *Failure {
	return &Failure{
		Code:       code,
		Stage:      stage,
		Message:    message,
		Transience: failureTransience(code),
	}
}

// newFailureFromError 保留完整诊断，并在错误链仍可访问时提取展示原因。
func newFailureFromError(code FailureCode, stage string, err error) *Failure {
	failure := newFailure(code, stage, err.Error())
	failure.DisplayMessage = FailureDisplayMessage(err)
	return failure
}

// finalSessionState 根据标准 ACP Session 输入、输出与失败确定终态。
func finalSessionState(session SessionResult, failure *Failure) SessionState {
	if failure != nil && failure.Code == FailureCodeSessionNotFound {
		return SessionStateMissing
	}
	if session.OutputID == "" {
		return SessionStateUnknown
	}
	if session.Mode == SessionModeResume {
		return SessionStateResumed
	}
	return SessionStateCreated
}
