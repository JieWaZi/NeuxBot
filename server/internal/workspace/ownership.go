package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// ownerMarker 是受管目录删除和替换前必须核对的归属事实。
type ownerMarker struct {
	// Kind 区分受管目录种类，例如对话执行工作区或消息附件目录。
	Kind string `json:"kind"`
	// ChatID 是受管目录所属 Chat 标识。
	ChatID string `json:"chatId"`
	// ThreadID 是 thread 级共享执行工作区归属标记中的团队对话标识。
	ThreadID string `json:"threadId,omitempty"`
	// WakerID 保留工作区创建者伙伴标识。
	WakerID string `json:"wakerId,omitempty"`
	// MessageID 是附件目录归属标记中的消息标识。
	MessageID string `json:"messageId,omitempty"`
}

func writeJSON(path string, value interface{}, failure status_error.StatusError) error {
	content, err := json.Marshal(value)
	if err != nil {
		return failure
	}
	content = append(content, '\n')
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return failure
	}
	return nil
}

func removeValidatedDirectory(path string) error {
	exists, err := directoryExists(path)
	if err != nil || !exists {
		return err
	}
	if err := os.RemoveAll(path); err != nil {
		return status_error.MessageAttachmentStorageError
	}
	return nil
}

func validateOwner(directory, kind, chatID, messageID string) error {
	content, err := os.ReadFile(filepath.Join(directory, constants.ChatContextOwnerFileName))
	if err != nil {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	var marker ownerMarker
	if err := json.Unmarshal(content, &marker); err != nil {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	if marker.Kind != kind || marker.ChatID != chatID || marker.MessageID != messageID {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	return nil
}

func directoryExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, status_error.MessageAttachmentStorageError
	}
	if !info.IsDir() {
		return false, status_error.MessageAttachmentStorageError
	}
	return true, nil
}

func ensureManagedRoot(workingDirectory string, components ...string) (string, error) {
	root, err := filepath.EvalSymlinks(workingDirectory)
	if err != nil {
		return "", status_error.ManagedWorkspaceError
	}
	for _, component := range components {
		if component == "" || filepath.Base(component) != component {
			return "", status_error.ManagedWorkspaceError
		}
		next := filepath.Join(root, component)
		info, statErr := os.Lstat(next)
		if os.IsNotExist(statErr) {
			if err := os.Mkdir(next, 0o700); err != nil && !os.IsExist(err) {
				return "", status_error.MessageAttachmentStorageError
			}
			info, statErr = os.Lstat(next)
		}
		if statErr != nil {
			return "", status_error.MessageAttachmentStorageError
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", status_error.MessageAttachmentStorageError
		}
		root = next
	}
	return root, nil
}
