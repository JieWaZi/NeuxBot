package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
)

// UserFile 是一个已通过业务归属检查的文件路径及其测量值。
type UserFile struct {
	// Entry 固定测量时的常规文件实例。
	Entry FileEntry
	// ID 是受管根内文件的稳定身份。
	ID string
	// RelativePath 是所属空间或会话中的相对路径。
	RelativePath string
	// Source 区分空间文件和会话附件。
	Source string
	// MessageID 是受管附件对应的原始上传消息身份。
	MessageID string
	// Legacy 表示文件通过旧分叉的权威路径获得归属。
	Legacy bool
}

// InheritedAttachments 读取旧分叉中没有单独归属标记的确切附件引用。
type InheritedAttachments func(context.Context, string) (map[string]string, error)

// UserFile 返回符合存储文件列表范围的常规文件身份。
func (entry FileEntry) UserFile(managed string) (id, relative, source string, ok bool) {
	if entry.OwnerRoot != managed {
		return "", "", "", false
	}
	path, err := filepath.Rel(managed, entry.Path)
	if err != nil || !filepath.IsLocal(path) {
		return "", "", "", false
	}
	id = filepath.ToSlash(path)
	if strings.Contains(id, "\\") {
		return "", "", "", false
	}
	parts := strings.Split(id, "/")
	switch {
	case entry.WakerID != "" && entry.Category == constants.StorageCategoryWorkFiles &&
		len(parts) >= 3 && parts[0] == "Agents" && parts[1] == entry.WakerID:
		switch strings.ToLower(parts[2]) {
		case "agents.md", "claude.md", "gemini.md", "codebuddy.md":
			return "", "", "", false
		}
		source = "workspace"
	case entry.ChatID != "" && entry.Category == constants.StorageCategoryConversations &&
		len(parts) >= 4 && len(parts) <= 5 && parts[0] == "Conversations" && parts[1] == entry.ChatID &&
		parts[2] == constants.ExecutionWorkspaceAttachmentsDirectory && !strings.HasPrefix(parts[3], "attachment-staging-"):
		source = "attachment"
	default:
		return "", "", "", false
	}

	for _, part := range parts[2:] {
		if strings.HasPrefix(part, ".") || strings.EqualFold(part, constants.ChatContextOwnerFileName) {
			return "", "", "", false
		}
	}
	return id, strings.Join(parts[2:], "/"), source, true
}

// InspectUserFiles 按测量代次核验可管理文件，并共享同一目录和旧分叉的归属读取。
func (m Measurement) InspectUserFiles(ctx context.Context, visible func(string) bool, inherited InheritedAttachments) ([]UserFile, bool) {
	managed := m.Roots.Managed
	root, err := openUserFileRoot(managed)
	if errors.Is(err, os.ErrNotExist) {
		return nil, true
	}
	if err != nil {
		return nil, false
	}
	defer root.Close()
	owners := map[string]fileOwnerObservation{}
	projects := map[string]bool{}
	legacy := map[string]map[string]string{}
	complete := true
	var files []UserFile
	for _, entries := range [][]FileEntry{m.Entries, m.FileAliases} {
		for _, entry := range entries {
			if ctx.Err() != nil {
				return files, false
			}
			if entry.ChatID != "" && !visible(entry.ChatID) {
				continue
			}
			id, relative, source, ok := entry.UserFile(managed)
			if !ok {
				continue
			}
			parts := strings.Split(id, "/")
			ownerDepth, message := 2, ""
			if source == "attachment" {
				ownerDepth = len(parts) - 1
				if len(parts) == 5 {
					message = parts[3]
				}
			} else if len(parts) >= 5 && strings.EqualFold(parts[2], constants.ProjectEntriesDirectoryName) {
				projectKey := strings.Join(parts[:4], "/")
				internal, exists := projects[projectKey]
				if !exists {
					internal = workspace.IsInternalExecutionPath(filepath.Join(managed, "Agents", entry.WakerID), entry.Path)
					projects[projectKey] = internal
				}
				if internal {
					continue
				}
			}
			key := strings.Join(parts[:ownerDepth], "/")
			owner, exists := owners[key]
			if !exists {
				directory, err := openMeasuredDirectory(root, parts[:ownerDepth])
				if err == nil {
					owner = readUserFileOwner(directory, entry.WakerID, entry.ChatID, message)
					directory.Close()
				} else {
					owner.err = err
				}
				owners[key] = owner
			}
			complete = complete && owner.err == nil
			owned := owner.owned
			if !owned && owner.missing && source == "attachment" && inherited != nil {
				paths, known := legacy[entry.ChatID]
				if !known {
					paths, err = inherited(ctx, entry.ChatID)
					complete = complete && err == nil
					if err != nil {
						paths = nil
					}
					legacy[entry.ChatID] = paths
				}
				uploadID, found := paths[relative]
				owned = found
				if message == "" {
					message = uploadID
				}
			}
			if owned {
				files = append(files, UserFile{Entry: entry, ID: id, RelativePath: relative, Source: source, MessageID: message, Legacy: !owner.owned})
			}
		}
	}
	return files, complete
}

type fileOwnerObservation struct {
	// owned 表示目录当前明确归属于申请的伙伴或消息。
	owned bool
	// missing 表示没有标记，允许旧分叉使用单独的权威引用证明。
	missing bool
	// err 表示归属读取由于 I/O 问题无法完成。
	err error
}

func readUserFileOwner(root *os.Root, waker, chat, message string) fileOwnerObservation {
	_, err := root.Lstat(constants.ChatContextOwnerFileName)
	if errors.Is(err, os.ErrNotExist) {
		return fileOwnerObservation{missing: true}
	}
	if err != nil {
		return fileOwnerObservation{err: err}
	}
	marker, err := workspace.ReadWorkspaceOwnerAt(root)
	if err != nil {
		return fileOwnerObservation{err: err}
	}
	if chat == "" {
		return fileOwnerObservation{owned: marker.Kind == constants.AgentExecutionWorkspaceOwnerKind && marker.WakerID == waker}
	}
	return fileOwnerObservation{owned: message != "" && marker.Kind == constants.ChatMessageAttachmentsOwnerKind && marker.ChatID == chat && marker.MessageID == message}
}

func openUserFileRoot(managed string) (*os.Root, error) {
	rootInfo, err := os.Lstat(managed)
	if err != nil {
		return nil, err
	}
	if !rootInfo.IsDir() {
		return nil, status_error.WorkspaceResourceConflict
	}
	return openConfiguredRoot(&scanRoot{path: managed, info: rootInfo})
}

// UserFileVersion 将删除确认绑定到测量时的文件实例和修改状态。
func (entry FileEntry) UserFileVersion() string {
	return fmt.Sprintf("%x:%x:%x:%x", entry.identity.volume, entry.identity.inode, entry.Bytes, entry.ModifiedAt.UnixNano())
}

// RemoveUserFile 通过受管目录句柄删除已测量的常规文件。附件删除前写入会话隐藏记录。
func (entry FileEntry) RemoveUserFile(managed string, inherited map[string]string, remember func(*os.Root, string) (func() error, error)) (result error) {
	id, relative, _, ok := entry.UserFile(managed)
	if !ok {
		return status_error.WorkspacePathInvalid
	}
	root, err := openUserFileRoot(managed)
	if err != nil {
		return status_error.WorkspaceResourceConflict
	}
	defer root.Close()
	parts := strings.Split(id, "/")
	owner, err := openMeasuredDirectory(root, parts[:2])
	if err != nil {
		return status_error.WorkspaceResourceConflict
	}
	defer owner.Close()
	parent := owner
	if len(parts) > 3 {
		parent, err = openMeasuredDirectory(owner, parts[2:len(parts)-1])
		if err != nil {
			return status_error.WorkspaceResourceConflict
		}
		defer parent.Close()
	}
	if entry.ChatID == "" {
		if !readUserFileOwner(owner, entry.WakerID, "", "").owned {
			return status_error.ChatDirectoryOwnershipInvalid
		}
		if workspace.IsInternalExecutionPath(filepath.Join(managed, "Agents", entry.WakerID), entry.Path) {
			return status_error.WorkspacePathInvalid
		}
	} else {
		var message string
		if len(parts) == 5 {
			message = parts[3]
		}
		claim := readUserFileOwner(parent, "", entry.ChatID, message)
		if _, exact := inherited[relative]; !claim.owned && !(claim.missing && exact) {
			return status_error.ChatDirectoryOwnershipInvalid
		}
	}
	name := parts[len(parts)-1]
	info, err := parent.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() != entry.Bytes || !info.ModTime().Equal(entry.ModifiedAt) {
		return status_error.WorkspaceResourceConflict
	}
	identity, err := regularFileIdentity(parent, name, info)
	if err != nil || identity != entry.identity || !matchesFileObservation(parent, name, info) {
		return status_error.WorkspaceResourceConflict
	}
	if entry.ChatID != "" && remember != nil {
		rollback, err := remember(owner, relative)
		if err != nil {
			return err
		}
		defer func() {
			if result != nil && rollback != nil {
				if rollbackErr := rollback(); rollbackErr != nil {
					logx.Warnf("restore measured removal manifest: chat=%q error=%v", entry.ChatID, rollbackErr)
					result = errors.Join(result, rollbackErr)
				}
			}
		}()
		if !matchesFileObservation(parent, name, info) {
			return status_error.WorkspaceResourceConflict
		}
	}
	if err := parent.Remove(name); err != nil {
		if os.IsNotExist(err) {
			return status_error.WorkspaceResourceConflict
		}
		logx.Warnf("remove measured file: waker=%q chat=%q error=%v", entry.WakerID, entry.ChatID, err)
		return status_error.WorkspaceStorageFailed
	}
	return nil
}
