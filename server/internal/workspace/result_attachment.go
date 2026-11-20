package workspace

import (
	"context"
	"errors"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// StagedResultAttachment 保存待发布的会话私有附件及其稳定目标身份。
type StagedResultAttachment struct {
	// chatID 是附件归属的会话标识。
	chatID string
	// fileID 是正式产出在目标会话中的稳定附件标识。
	fileID string
	// target 是准备提交的附件目录。
	target string
	// staging 是本次创建的临时副本。已有正式副本时为空。
	staging string
	// reference 是提交后可写入 Chat 日志的文件引用。
	reference AttachmentReference
}

// ResultAttachmentPath 返回已验证的正式附件相对路径，用于比较已提交引用。
func ResultAttachmentPath(fileID, name string) (string, error) {
	if !validAttachmentSegment(fileID) || !validAttachmentSegment(name) {
		return "", status_error.MessageAttachmentInvalid
	}
	return filepath.ToSlash(filepath.Join(constants.ExecutionWorkspaceAttachmentsDirectory, fileID, name)), nil
}

// StageResultAttachment 复制已获准打开的原件，正式引用由调用方在提交时发布。
func StageResultAttachment(ctx context.Context, directory, chatID, fileID, name string, open func() (io.ReadCloser, error)) (*StagedResultAttachment, error) {
	path, err := ResultAttachmentPath(fileID, name)
	if err != nil {
		return nil, err
	}
	root, err := ensureManagedRoot(directory, constants.ExecutionWorkspaceAttachmentsDirectory)
	if err != nil {
		return nil, err
	}
	attachment := &StagedResultAttachment{chatID: chatID, fileID: fileID, target: filepath.Join(root, fileID),
		reference: AttachmentReference{Name: name, Path: path, MediaType: mime.TypeByExtension(filepath.Ext(name)), Managed: true}}
	exists, err := attachment.targetExists()
	if err != nil {
		return nil, err
	}
	if exists {
		return attachment, nil
	}
	source, err := open()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := source.Close(); err != nil {
			attachment.logFailure("close source", attachment.target, err)
		}
	}()
	staging, err := os.MkdirTemp(root, "attachment-staging-"+fileID+"-")
	if err != nil {
		attachment.logFailure("create staging directory", root, err)
		return nil, status_error.MessageAttachmentStorageError
	}
	attachment.staging = staging
	ready := false
	defer func() {
		if !ready {
			attachment.Discard()
		}
	}()
	if err := writeJSON(filepath.Join(staging, constants.ChatContextOwnerFileName), ownerMarker{Kind: constants.ChatMessageAttachmentsOwnerKind, ChatID: chatID, MessageID: fileID}, status_error.MessageAttachmentStorageError); err != nil {
		return nil, err
	}
	stagedPath := filepath.Join(staging, name)
	destination, err := os.OpenFile(stagedPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		attachment.logFailure("open staging file", stagedPath, err)
		return nil, status_error.MessageAttachmentStorageError
	}
	size, copyErr := io.Copy(destination, io.LimitReader(source, constants.WorkflowFileMaximumBytes+1))
	syncErr, closeErr := destination.Sync(), destination.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		attachment.logFailure("write staging file", stagedPath, err)
		return nil, status_error.MessageAttachmentStorageError
	}
	if size > constants.WorkflowFileMaximumBytes {
		return nil, status_error.MessageAttachmentStorageError
	}
	if ctx.Err() != nil {
		return nil, status_error.OperationCanceled
	}
	ready = true
	return attachment, nil
}

// Publish 原子发布已复制的附件，保留并行重试留下的同一份正式文件。
func (a *StagedResultAttachment) Publish() (AttachmentReference, error) {
	exists, err := a.targetExists()
	if err != nil {
		return AttachmentReference{}, err
	}
	if exists {
		return a.reference, nil
	}
	if a.staging == "" {
		return AttachmentReference{}, status_error.MessageAttachmentStorageError
	}
	if err := os.Rename(a.staging, a.target); err != nil {
		a.logFailure("publish staged attachment", a.target, err)
		return AttachmentReference{}, status_error.MessageAttachmentStorageError
	}
	a.staging = ""
	return a.reference, nil
}

// Discard 清理本次未使用的临时副本。
func (a *StagedResultAttachment) Discard() {
	if a.staging == "" {
		return
	}
	if err := os.RemoveAll(a.staging); err != nil {
		a.logFailure("discard staged attachment", a.staging, err)
	}
}

func (a *StagedResultAttachment) targetExists() (bool, error) {
	exists, err := directoryExists(a.target)
	if err != nil || !exists {
		return false, err
	}
	info, err := os.Lstat(a.target)
	if err != nil {
		a.logFailure("inspect attachment directory", a.target, err)
		return false, status_error.ChatDirectoryOwnershipInvalid
	}
	if !info.IsDir() {
		return false, status_error.ChatDirectoryOwnershipInvalid
	}
	if err := validateOwner(a.target, constants.ChatMessageAttachmentsOwnerKind, a.chatID, a.fileID); err != nil {
		return false, err
	}
	path := filepath.Join(a.target, a.reference.Name)
	info, err = os.Lstat(path)
	if err != nil {
		a.logFailure("inspect attachment file", path, err)
		return false, status_error.MessageAttachmentStorageError
	}
	if !info.Mode().IsRegular() {
		return false, status_error.MessageAttachmentStorageError
	}
	return true, nil
}

func (a *StagedResultAttachment) logFailure(operation, path string, err error) {
	logx.Errorf("result attachment storage failed: operation=%q chat_id=%s file_id=%s path=%q error=%v", operation, a.chatID, a.fileID, path, err)
}

func validAttachmentSegment(value string) bool {
	return value != "" && value != "." && value != ".." && filepath.VolumeName(value) == "" && !strings.ContainsAny(value, "/\\\x00")
}
