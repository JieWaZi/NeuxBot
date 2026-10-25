package workspacefiles

import (
	"context"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/google/uuid"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const removedManifest = "attachments/removed-chat-files.json"

// ChatFiles 是会话文件列表及用户隐藏记录。
type ChatFiles struct {
	// Generated 是受管生成附件。
	Generated []FileMatch `json:"generated"`
	// RemovedPaths 是应在会话中隐藏的相对路径。
	RemovedPaths []string `json:"removedPaths"`
}

func (m *Manager) chatRoot(subject, id, root string) (string, error) {
	scope, err := m.scope(subject, id)
	if err != nil {
		return "", err
	}
	if root != "" {
		actual, e := canonicalDirectory(root)
		if e != nil {
			return "", e
		}
		if actual != scope.Root {
			return "", status_error.WorkspacePathInvalid
		}
	}
	return scope.Root, nil
}
func removedPaths(h *os.Root) ([]string, error) {
	for _, name := range []string{"attachments", removedManifest} {
		info, err := h.Lstat(name)
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		if err != nil {
			return nil, fileError("stat-chat-manifest", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, status_error.WorkspacePathInvalid
		}
	}
	f, err := h.Open(removedManifest)
	if err != nil {
		return nil, fileError("open-chat-manifest", err)
	}
	defer f.Close()
	bytes, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil {
		return nil, fileError("read-chat-manifest", err)
	}
	if len(bytes) > 1024*1024 {
		return nil, status_error.WorkspaceResourceTooLarge
	}
	var paths []string
	if err = json.Unmarshal(bytes, &paths); err != nil {
		return nil, fileError("decode-chat-manifest", err)
	}
	if paths == nil {
		paths = []string{}
	}
	return paths, nil
}

// ListChatFiles 保留用户隐藏记录；附件被重新生成后自动恢复展示。
func (m *Manager) ListChatFiles(ctx context.Context, subject, id, root string) (ChatFiles, error) {
	actual, err := m.chatRoot(subject, id, root)
	if err != nil {
		return ChatFiles{}, err
	}
	h, err := os.OpenRoot(actual)
	if err != nil {
		return ChatFiles{}, fileError("open-chat-root", err)
	}
	defer h.Close()
	removed, err := removedPaths(h)
	if err != nil {
		return ChatFiles{}, err
	}
	generated, err := m.Search(ctx, subject, id, actual, "", "attachments/generated", 200)
	if err == status_error.WorkspaceResourceNotFound {
		generated = []FileMatch{}
		err = nil
	}
	if err != nil {
		return ChatFiles{}, err
	}
	filtered := []string{}
	for _, p := range removed {
		if !strings.HasPrefix(p, "attachments/") {
			filtered = append(filtered, p)
			continue
		}
		if _, _, err := m.resolve(subject, id, actual, p, false); err != nil {
			filtered = append(filtered, p)
		}
	}
	return ChatFiles{Generated: generated, RemovedPaths: filtered}, nil
}

// DeleteChatFile 只物理删除会话拥有的附件，项目文件仅写入隐藏清单。
func (m *Manager) DeleteChatFile(subject, id, root, relative string) error {
	m.mutations.Lock()
	defer m.mutations.Unlock()
	actual, err := m.chatRoot(subject, id, root)
	if err != nil {
		return err
	}
	allowed, target, err := m.resolve(subject, id, actual, relative, false)
	if err != nil {
		return err
	}
	file, err := openFile(allowed, target)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	file.Close()
	if err != nil {
		return fileError("stat-chat-file", err)
	}
	if !info.Mode().IsRegular() {
		return status_error.WorkspacePathInvalid
	}
	h, err := os.OpenRoot(actual)
	if err != nil {
		return fileError("open-chat-root", err)
	}
	defer h.Close()
	owned := strings.HasPrefix(relative, "attachments/")
	if owned {
		if !within(filepath.Join(actual, "attachments"), target) {
			return status_error.WorkspacePathInvalid
		}
		info, err := h.Lstat(relative)
		if err != nil {
			return fileError("stat-chat-attachment", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return status_error.WorkspacePathInvalid
		}
	}
	removed, err := removedPaths(h)
	if err != nil {
		return err
	}
	found := false
	for _, p := range removed {
		if p == relative {
			found = true
		}
	}
	if !found {
		removed = append(removed, relative)
	}
	if err = h.MkdirAll("attachments", 0700); err != nil {
		return fileError("create-chat-attachments", err)
	}
	bytes, err := json.Marshal(removed)
	if err != nil {
		return fileError("encode-chat-manifest", err)
	}
	if len(bytes) > 1024*1024 {
		return status_error.WorkspaceResourceTooLarge
	}
	temporary := removedManifest + "." + uuid.NewString() + ".tmp"
	f, err := h.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fileError("create-chat-manifest", err)
	}
	defer h.Remove(temporary)
	_, err = f.Write(bytes)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fileError("write-chat-manifest", err)
	}
	if err = h.Rename(temporary, removedManifest); err != nil {
		return fileError("replace-chat-manifest", err)
	}
	if owned {
		if err = h.Remove(relative); err != nil {
			return fileError("delete-chat-attachment", err)
		}
	}
	return nil
}
