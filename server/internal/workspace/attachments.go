package workspace

import (
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

const attachmentStagingPrefix = "attachment-staging-"

// AttachmentInput 是用户选择的一份本地文件。
type AttachmentInput struct {
	// SourcePath 是桌面文件选择器返回的本地绝对或相对路径。
	SourcePath string
}

// AttachmentReference 是可以安全写入 Chat JSONL 的文件引用。
type AttachmentReference struct {
	// Partial 标记失败或停止时仍然有效的部分成果。
	Partial bool `json:"partial,omitempty"`
	// Name 是向用户和 Agent 展示的原始文件名。
	Name string `json:"name"`
	// Path 是相对 Conversation 私有文件目录的稳定路径。
	Path string `json:"path"`
	// MediaType 是根据扩展名推断的 MIME 类型。
	MediaType string `json:"mediaType,omitempty"`
	// Managed 表示文件是否复制到 NeuxBot 归属目录。
	Managed bool `json:"managed"`
}

// PreparedAttachments 是附件校验、复制与回滚所需的结果。
type PreparedAttachments struct {
	// References 是保持用户选择顺序的 JSONL 安全引用。
	References []AttachmentReference
	// RuntimePaths 是 Runtime 可以读取的已解析绝对路径。
	RuntimePaths []string
	// Rollback 删除本次新建且带合法归属标记的附件目录。
	Rollback func() error
}

// resolvedAttachment 保存路径穿越校验后的单个附件事实。
type resolvedAttachment struct {
	// sourcePath 是 Runtime 可以读取的已解析绝对路径。
	sourcePath string
	// relativePath 是工作目录内附件已有的相对路径。
	relativePath string
	// name 是向用户展示的原始文件名。
	name string
	// mediaType 是根据扩展名推断的 MIME 类型。
	mediaType string
	// managed 表示附件需要复制到 NeuxBot 归属目录。
	managed bool
}

// PrepareChatAttachments 把上传原件复制到 Conversation 的私有 attachments/。
func PrepareChatAttachments(
	workingDirectory string,
	chatID string,
	messageID string,
	inputs []AttachmentInput,
) (PreparedAttachments, error) {
	return prepareChatAttachments(workingDirectory, chatID, messageID, inputs, false)
}

// StageQueuedAttachments 持久保存稍后派发的上传原件，保留每个原始文件名。
func StageQueuedAttachments(directory, chatID, inputID string, inputs []AttachmentInput) (PreparedAttachments, error) {
	return prepareChatAttachments(directory, chatID, inputID, inputs, true)
}

func prepareChatAttachments(
	workingDirectory string,
	chatID string,
	messageID string,
	inputs []AttachmentInput,
	staged bool,
) (PreparedAttachments, error) {
	if len(inputs) == 0 {
		return PreparedAttachments{Rollback: func() error { return nil }}, nil
	}
	workspace, err := filepath.EvalSymlinks(workingDirectory)
	if err != nil {
		return PreparedAttachments{}, status_error.ManagedWorkspaceError
	}
	resolvedAttachments := make([]resolvedAttachment, 0, len(inputs))
	externalCount := 0
	for _, input := range inputs {
		absolute, err := filepath.Abs(strings.TrimSpace(input.SourcePath))
		if err != nil {
			return PreparedAttachments{}, status_error.MessageAttachmentInvalid
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return PreparedAttachments{}, status_error.MessageAttachmentInvalid
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() {
			return PreparedAttachments{}, status_error.MessageAttachmentInvalid
		}
		file, err := os.Open(resolved)
		if err != nil {
			return PreparedAttachments{}, status_error.MessageAttachmentInvalid
		}
		if err := file.Close(); err != nil {
			return PreparedAttachments{}, status_error.MessageAttachmentInvalid
		}
		relative, err := filepath.Rel(workspace, resolved)
		inside := err == nil && relative != ".." &&
			!strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
			!filepath.IsAbs(relative)
		if inside && !staged {
			resolvedAttachments = append(resolvedAttachments, resolvedAttachment{
				sourcePath:   resolved,
				relativePath: relative,
				name:         filepath.Base(resolved),
				mediaType:    mime.TypeByExtension(filepath.Ext(resolved)),
			})
			continue
		}
		externalCount++
		resolvedAttachments = append(resolvedAttachments, resolvedAttachment{
			sourcePath: resolved,
			name:       filepath.Base(resolved),
			mediaType:  mime.TypeByExtension(filepath.Ext(resolved)),
			managed:    true,
		})
	}

	attachmentDirectory := constants.ExecutionWorkspaceAttachmentsDirectory
	if staged {
		attachmentDirectory = constants.ChannelLeadInputAttachmentsDirectory
	}
	target := filepath.Join(workspace, attachmentDirectory, messageID)
	if externalCount > 0 {
		attachmentsRoot, rootErr := ensureManagedRoot(
			workspace,
			attachmentDirectory,
		)
		if rootErr != nil {
			return PreparedAttachments{}, rootErr
		}
		target = filepath.Join(attachmentsRoot, messageID)
	}
	references, runtimePaths, err := materializeAttachmentReferences(
		resolvedAttachments,
		target,
		chatID,
		messageID,
		staged,
	)
	if err != nil {
		return PreparedAttachments{}, err
	}
	return PreparedAttachments{
		References:   references,
		RuntimePaths: runtimePaths,
		Rollback: func() error {
			if externalCount == 0 {
				return nil
			}
			if err := validateOwner(
				target,
				constants.ChatMessageAttachmentsOwnerKind,
				chatID,
				messageID,
			); err != nil {
				return err
			}
			return removeValidatedDirectory(target)
		},
	}, nil
}

// RemoveQueuedAttachments 回收指定负责人输入所拥有的附件副本。
func RemoveQueuedAttachments(directory, chatID, inputID string) error {
	if !validAttachmentSegment(inputID) {
		return status_error.MessageAttachmentInvalid
	}
	root, err := openQueuedAttachmentRoot(directory)
	if err != nil || root == nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat(inputID)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.IsDir() {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	owned, err := root.OpenRoot(inputID)
	if err != nil {
		return status_error.MessageAttachmentStorageError
	}
	owner, err := ReadWorkspaceOwnerAt(owned)
	_ = owned.Close()
	if err != nil || owner.Kind != constants.ChatMessageAttachmentsOwnerKind || owner.ChatID != chatID || owner.MessageID != inputID {
		return status_error.ChatDirectoryOwnershipInvalid
	}
	if err := root.RemoveAll(inputID); err != nil {
		return status_error.MessageAttachmentStorageError
	}
	return nil
}

func openQueuedAttachmentRoot(directory string) (*os.Root, error) {
	path := filepath.Join(directory, constants.ChannelLeadInputAttachmentsDirectory)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil || !info.IsDir() {
		return nil, status_error.ChatDirectoryOwnershipInvalid
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, status_error.MessageAttachmentStorageError
	}
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		_ = root.Close()
		return nil, status_error.ChatDirectoryOwnershipInvalid
	}
	return root, nil
}

// RecoverQueuedAttachmentIDs 清理未完成的临时目录，并返回等待与队列收据对账的标识。
func RecoverQueuedAttachmentIDs(directory string) ([]string, error) {
	root, err := openQueuedAttachmentRoot(directory)
	if err != nil || root == nil {
		return nil, err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, status_error.MessageAttachmentStorageError
	}
	var ids []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		if value, temporary := strings.CutPrefix(id, attachmentStagingPrefix); temporary {
			var suffix string
			id, suffix, _ = strings.Cut(value, "-")
			if _, err := strconv.ParseUint(suffix, 10, 32); err != nil {
				continue
			}
		}
		if len(id) != 32 {
			continue
		}
		if _, err := hex.DecodeString(id); err != nil {
			continue
		}
		if id == entry.Name() {
			ids = append(ids, id)
		} else if err := root.RemoveAll(entry.Name()); err != nil {
			return nil, status_error.MessageAttachmentStorageError
		}
	}
	return ids, nil
}

func materializeAttachmentReferences(
	attachments []resolvedAttachment,
	target string,
	chatID string,
	messageID string,
	staged bool,
) ([]AttachmentReference, []string, error) {
	references := make([]AttachmentReference, 0, len(attachments))
	runtimePaths := make([]string, 0, len(attachments))
	externalCount := 0
	for _, attachment := range attachments {
		if attachment.managed {
			externalCount++
		}
	}
	if externalCount == 0 {
		for _, attachment := range attachments {
			references = append(references, AttachmentReference{
				Name:      attachment.name,
				Path:      filepath.ToSlash(attachment.relativePath),
				MediaType: attachment.mediaType,
			})
			runtimePaths = append(runtimePaths, attachment.sourcePath)
		}
		return references, runtimePaths, nil
	}
	root := filepath.Dir(target)
	if exists, err := directoryExists(target); err != nil {
		return nil, nil, err
	} else if exists {
		return nil, nil, status_error.MessageAttachmentStorageError
	}
	staging, err := os.MkdirTemp(root, attachmentStagingPrefix+messageID+"-")
	if err != nil {
		return nil, nil, status_error.MessageAttachmentStorageError
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := writeJSON(
		filepath.Join(staging, constants.ChatContextOwnerFileName),
		ownerMarker{
			Kind:      constants.ChatMessageAttachmentsOwnerKind,
			ChatID:    chatID,
			MessageID: messageID,
		},
		status_error.MessageAttachmentStorageError,
	); err != nil {
		return nil, nil, err
	}
	usedNames := make(map[string]struct{}, externalCount)
	for index, attachment := range attachments {
		if !attachment.managed {
			references = append(references, AttachmentReference{
				Name:      attachment.name,
				Path:      filepath.ToSlash(attachment.relativePath),
				MediaType: attachment.mediaType,
			})
			runtimePaths = append(runtimePaths, attachment.sourcePath)
			continue
		}
		name := attachment.name
		if staged {
			slot := strconv.Itoa(index)
			if err := os.Mkdir(filepath.Join(staging, slot), 0o700); err != nil {
				return nil, nil, status_error.MessageAttachmentStorageError
			}
			name = filepath.Join(slot, name)
		} else {
			name = uniqueAttachmentName(name, index, usedNames)
		}
		destination := filepath.Join(staging, name)
		if err := copyFile(attachment.sourcePath, destination, staged); err != nil {
			return nil, nil, err
		}
		relative := filepath.Join(
			filepath.Base(root),
			messageID,
			name,
		)
		references = append(references, AttachmentReference{
			Name:      attachment.name,
			Path:      filepath.ToSlash(relative),
			MediaType: attachment.mediaType,
			Managed:   true,
		})
		runtimePaths = append(runtimePaths, filepath.Join(target, name))
	}
	if err := os.Rename(staging, target); err != nil {
		return nil, nil, status_error.MessageAttachmentStorageError
	}
	cleanup = false
	return references, runtimePaths, nil
}

func uniqueAttachmentName(
	baseName string,
	index int,
	used map[string]struct{},
) string {
	name := baseName
	if _, exists := used[name]; exists {
		name = fmt.Sprintf("%d-%s", index+1, baseName)
	}
	used[name] = struct{}{}
	return name
}

func copyFile(sourcePath, destinationPath string, durable bool) error {
	sourceInfo, err := os.Lstat(sourcePath)
	if err != nil || !sourceInfo.Mode().IsRegular() {
		return status_error.MessageAttachmentInvalid
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return status_error.MessageAttachmentInvalid
	}
	defer source.Close()
	openedInfo, err := source.Stat()
	if err != nil || !os.SameFile(sourceInfo, openedInfo) {
		return status_error.MessageAttachmentInvalid
	}
	destination, err := os.OpenFile(
		destinationPath,
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		0o600,
	)
	if err != nil {
		return status_error.MessageAttachmentStorageError
	}
	if _, err := io.Copy(destination, source); err != nil {
		_ = destination.Close()
		return status_error.MessageAttachmentStorageError
	}
	if durable {
		if err := destination.Sync(); err != nil {
			_ = destination.Close()
			return status_error.MessageAttachmentStorageError
		}
	}
	if err := destination.Close(); err != nil {
		return status_error.MessageAttachmentStorageError
	}
	return nil
}
