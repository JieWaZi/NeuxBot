// Package files 管理上传文件的不可变原件、归属校验与运行输入副本。
package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
)

// Upload 是上传后可直接放入文件参数的引用及真实元数据。
type Upload struct {
	// Reference 是不可猜测的上传身份，不是调用方提供的本地路径。
	Reference string `json:"reference"`
	// Name 是用于展示和下载的原始文件名。
	Name string `json:"name"`
	// Size 是原件字节数。
	Size int64 `json:"size"`
	// Digest 是原件的 SHA-256 摘要。
	Digest string `json:"digest"`
}

// storedUpload 将不可变原件绑定到工作流及可选任务，防止跨任务引用。
type storedUpload struct {
	// Upload 保存可公开的引用与元数据。
	Upload
	// OwnerKeyID 固定集成主体；legacy 和 managed 分别表示旧任务 Token 与本机输入。
	OwnerKeyID string `json:"ownerKeyId"`
	// TaskID 为空表示由 Core 管理入口上传，否则只允许该任务的 API 使用。
	TaskID string `json:"taskId"`
}

func storePath(dataDirectory, definitionID string) (string, error) {
	if dataDirectory == "" || !identity(definitionID) {
		return "", status_error.WorkflowFileInvalid
	}
	return filepath.Join(dataDirectory, constants.WorkflowFileDirectoryName, definitionID), nil
}

func identity(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func referenceParts(reference string) (string, string, error) {
	if !strings.HasPrefix(reference, constants.WorkflowFileReferencePrefix) {
		return "", "", status_error.WorkflowFileInvalid
	}
	parts := strings.SplitN(strings.TrimPrefix(reference, constants.WorkflowFileReferencePrefix), "/", 2)
	if len(parts) != 2 || !identity(parts[0]) || !fileName(parts[1]) {
		return "", "", status_error.WorkflowFileInvalid
	}
	return parts[0], parts[1], nil
}

func fileName(value string) bool {
	return value != "" && value != "." && value != ".." && len(value) <= 255 && !strings.ContainsAny(value, "/\\\x00\r\n")
}

func storageError(operation string, err error) error {
	logx.Errorf("workflow file storage failed: operation=%s error=%v", operation, err)
	return status_error.WorkflowFileStorageError
}

// Save 在事务外有界写入原件，只有完整内容和归属元数据都落盘后才签发引用。
func Save(ctx context.Context, dataDirectory, definitionID, taskID, name string, input io.Reader, ownerKeyIDs ...string) (Upload, error) {
	if !fileName(name) || (taskID != "" && !identity(taskID)) {
		return Upload{}, status_error.WorkflowFileInvalid
	}
	directory, err := storePath(dataDirectory, definitionID)
	if err != nil {
		return Upload{}, err
	}
	id := util.New()
	directory = filepath.Join(directory, id)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return Upload{}, storageError("mkdir", err)
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(directory)
		}
	}()
	file, err := os.OpenFile(filepath.Join(directory, "content"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Upload{}, storageError("create", err)
	}
	digest := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, digest), io.LimitReader(input, constants.WorkflowFileMaximumBytes+1))
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		return Upload{}, storageError("upload", copyErr)
	}
	if ctx.Err() != nil {
		return Upload{}, status_error.WorkflowCancelled
	}
	if size > constants.WorkflowFileMaximumBytes {
		return Upload{}, status_error.WorkflowBudgetExceeded.WithField("budget", "file_bytes")
	}
	if syncErr != nil {
		return Upload{}, storageError("sync", syncErr)
	}
	if closeErr != nil {
		return Upload{}, storageError("close", closeErr)
	}
	upload := Upload{Reference: constants.WorkflowFileReferencePrefix + id + "/" + name, Name: name, Size: size, Digest: hex.EncodeToString(digest.Sum(nil))}
	metadata, err := json.Marshal(storedUpload{Upload: upload, TaskID: taskID, OwnerKeyID: uploadOwner(taskID, ownerKeyIDs)})
	if err != nil {
		return Upload{}, storageError("encode", err)
	}
	// 元数据不与用户文件共名。
	if err := os.WriteFile(filepath.Join(directory, "metadata.json"), metadata, 0600); err != nil {
		return Upload{}, storageError("metadata", err)
	}
	success = true
	return upload, nil
}

func open(dataDirectory, definitionID, taskID, reference string, allowManaged bool, ownerKeyIDs ...string) (*os.File, storedUpload, error) {
	id, _, err := referenceParts(reference)
	if err != nil {
		return nil, storedUpload{}, err
	}
	directory, err := storePath(dataDirectory, definitionID)
	if err != nil {
		return nil, storedUpload{}, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, storedUpload{}, status_error.WorkflowFileNotFound
	}
	defer root.Close()
	metadata, err := root.ReadFile(filepath.Join(id, "metadata.json"))
	if err != nil {
		return nil, storedUpload{}, status_error.WorkflowFileNotFound
	}
	var upload storedUpload
	if json.Unmarshal(metadata, &upload) != nil || upload.Reference != reference || (upload.TaskID != taskID && !(allowManaged && upload.TaskID == "")) {
		return nil, storedUpload{}, status_error.WorkflowFileInvalid
	}
	owner := upload.OwnerKeyID
	if owner == "" {
		owner = uploadOwner(upload.TaskID, nil)
	}
	if owner != uploadOwner(taskID, ownerKeyIDs) && !(allowManaged && upload.TaskID == "" && owner == "managed") {
		return nil, storedUpload{}, status_error.WorkflowFileInvalid
	}
	file, err := root.Open(filepath.Join(id, "content"))
	if err != nil {
		return nil, storedUpload{}, status_error.WorkflowFileNotFound
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != upload.Size {
		file.Close()
		return nil, storedUpload{}, status_error.WorkflowFileInvalid
	}
	return file, upload, nil
}

// ValidateReference 校验上传原件的归属，不允许 API 引用本地路径或其他任务的文件。
func ValidateReference(dataDirectory, definitionID, taskID, reference string, allowManaged bool, ownerKeyIDs ...string) error {
	file, _, err := open(dataDirectory, definitionID, taskID, reference, allowManaged, ownerKeyIDs...)
	if err == nil {
		_ = file.Close()
	}
	return err
}

// Materialize 将原件复制到本次冻结范围，真实路径供全部工作流节点读取。
func Materialize(ctx context.Context, dataDirectory, definitionID, taskID, runID, workingDirectory, reference string, ownerKeyIDs ...string) (string, error) {
	file, upload, err := open(dataDirectory, definitionID, taskID, reference, true, ownerKeyIDs...)
	if err != nil {
		return "", err
	}
	defer file.Close()
	id, name, _ := referenceParts(reference)
	root, err := os.OpenRoot(workingDirectory)
	if err != nil {
		return "", storageError("workspace", err)
	}
	defer root.Close()
	directory := filepath.Join(constants.WorkflowInputFileDirectoryName, runID, id)
	if err := root.MkdirAll(directory, 0700); err != nil {
		return "", storageError("input_directory", err)
	}
	tempName := filepath.Join(directory, util.New()+".tmp")
	temporary, err := root.OpenFile(tempName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", storageError("input_create", err)
	}
	defer root.Remove(tempName)
	digest := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(temporary, digest), io.LimitReader(file, constants.WorkflowFileMaximumBytes+1))
	closeErr := temporary.Close()
	if copyErr != nil {
		return "", storageError("input_copy", copyErr)
	}
	if closeErr != nil {
		return "", storageError("input_close", closeErr)
	}
	if ctx.Err() != nil {
		return "", status_error.WorkflowCancelled
	}
	if size != upload.Size || hex.EncodeToString(digest.Sum(nil)) != upload.Digest {
		return "", status_error.WorkflowFileInvalid
	}
	target := filepath.Join(directory, name)
	if err := root.Rename(tempName, target); err != nil {
		return "", storageError("input_commit", err)
	}
	return filepath.Join(workingDirectory, target), nil
}

// EnsureScopeDirectory 解析工作范围的真实绝对目录并拒绝不可用位置。
func EnsureScopeDirectory(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if !filepath.IsAbs(path) {
		return "", status_error.WorkflowResourceInvalid.WithField("field", "scope.workingDirectory")
	}
	value, err := filepath.EvalSymlinks(path)
	if err != nil {
		logx.Errorf("resolve workflow scope failed: error=%v", err)
		return "", status_error.WorkflowResourceInvalid
	}
	info, err := os.Stat(value)
	if err != nil {
		logx.Errorf("inspect workflow scope failed: error=%v", err)
		return "", status_error.WorkflowResourceInvalid
	}
	if !info.IsDir() {
		return "", status_error.WorkflowResourceInvalid
	}
	return value, nil
}

// PrepareManagedWorkspace 在事务外为稳定身份准备独立目录，并使用真实路径。
func PrepareManagedWorkspace(dataDirectory, id string) (string, error) {
	if dataDirectory == "" {
		return "", status_error.WorkflowResourceInvalid.WithField("field", "dataDirectory")
	}
	directory := filepath.Join(dataDirectory, constants.WorkflowWorkspaceDirectoryName, id)
	if err := os.MkdirAll(directory, 0700); err != nil {
		logx.Errorf("create workflow workspace failed: workspace_id=%s error=%v", id, err)
		return "", status_error.WorkflowResourceInvalid
	}
	return EnsureScopeDirectory(directory)
}

// ValidateAPIInputFiles 将调用参数中的文件引用限制在本任务的受管上传中。
func ValidateAPIInputFiles(dataDirectory, definitionID, taskID string, schema map[string]any, args any, ownerKeyIDs ...string) error {
	for pointer, reference := range workflowdsl.FileValues(schema, args) {
		if err := ValidateReference(dataDirectory, definitionID, taskID, reference, false, ownerKeyIDs...); err != nil {
			return err
		}
		if reference == "" {
			return status_error.WorkflowFileInvalid.WithField("jsonPointer", pointer)
		}
	}
	return nil
}

// uploadOwner 保持存量元数据的明确 legacy 语义，新 Key 不继承空归属文件。
func uploadOwner(taskID string, owners []string) string {
	if len(owners) > 0 && owners[0] != "" {
		return owners[0]
	}
	if taskID == "" {
		return "managed"
	}
	return "legacy"
}
