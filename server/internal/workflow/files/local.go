package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

// LocalInput 是同一次复制得到的冻结路径和真实字节摘要。
type LocalInput struct {
	// Path 指向已写入受管目录的独立原件。
	Path string
	// Digest 是复制到该原件的实际字节摘要。
	Digest string
}

// StagedInputs 持有一次接纳尝试复制的原件，获胜后由运行接管。
type StagedInputs struct {
	// Directory 是临时复制根，和最终运行目录位于同一个受管工作区。
	Directory string
	// Files 按输入字段保存本次复制的摘要及接纳后的最终路径。
	Files map[string]LocalInput
	// workspaceRoot 是存放已接纳原件的工作区根。
	workspaceRoot string
	// runID 是接管本批文件的稳定运行身份。
	runID string
}

// StageInputs 为每次接纳尝试准备独立私有目录，不占用其他请求的目标路径。
func StageInputs(workspace Workspace, runID string) (*StagedInputs, error) {
	root, err := os.OpenRoot(workspace.RootDirectory)
	if err != nil {
		return nil, storageError("stage_inputs", err)
	}
	defer root.Close()
	directoryName := constants.WorkflowInputStagingDirectoryName
	if err := root.Mkdir(directoryName, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, storageError("stage_inputs", err)
	}
	stageRoot, err := openRealInputDirectory(root, directoryName)
	if err != nil {
		return nil, storageError("stage_inputs", err)
	}
	if stageRoot == nil {
		return nil, status_error.WorkflowFileInvalid
	}
	defer stageRoot.Close()
	name := runID + "-" + util.New()
	if err := stageRoot.Mkdir(name, 0o700); err != nil {
		return nil, storageError("stage_inputs", err)
	}
	directory := filepath.Join(workspace.RootDirectory, directoryName, name)
	return &StagedInputs{Directory: directory, Files: make(map[string]LocalInput), workspaceRoot: workspace.RootDirectory, runID: runID}, nil
}

// Record 保存一次复制所得的真实摘要以及接纳后的路径。
func (s *StagedInputs) Record(pointer string, input LocalInput) error {
	relative, err := filepath.Rel(s.Directory, input.Path)
	runDirectory := filepath.Join(constants.WorkflowInputFileDirectoryName, s.runID)
	if err != nil || filepath.Dir(filepath.Dir(relative)) != runDirectory {
		return status_error.WorkflowFileInvalid.WithField("field", pointer)
	}
	input.Path = filepath.Join(s.workspaceRoot, relative)
	s.Files[pointer] = input
	return nil
}

// Publish 在确认无现有接纳回执后，通过同一文件系统的目录重命名移交原件。
func (s *StagedInputs) Publish() error {
	root, err := os.OpenRoot(s.workspaceRoot)
	if err != nil {
		return storageError("publish_input_workspace", err)
	}
	defer root.Close()
	if err := root.Mkdir(constants.WorkflowInputFileDirectoryName, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return storageError("publish_input_directory", err)
	}
	info, err := root.Lstat(constants.WorkflowInputFileDirectoryName)
	if err != nil {
		return storageError("inspect_input_directory", err)
	}
	if !info.IsDir() {
		return status_error.WorkflowFileInvalid
	}
	stage, _ := filepath.Rel(s.workspaceRoot, s.Directory)
	target := filepath.Join(constants.WorkflowInputFileDirectoryName, s.runID)
	// 没有接纳回执的目的地只可能来自进程中断的旧尝试。
	if err := root.Rename(target, filepath.Join(stage, "abandoned")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return storageError("replace_unclaimed_inputs", err)
	}
	if err := root.Rename(filepath.Join(stage, target), target); err != nil {
		return storageError("publish_inputs", err)
	}
	return nil
}

// Rollback 在提交失败时收回刚发布的文件。耗时的实际清理由 Close 执行。
func (s *StagedInputs) Rollback() {
	root, err := os.OpenRoot(s.workspaceRoot)
	if err != nil {
		_ = storageError("rollback_input_workspace", err)
		return
	}
	defer root.Close()
	stage, _ := filepath.Rel(s.workspaceRoot, s.Directory)
	if err := root.Rename(filepath.Join(constants.WorkflowInputFileDirectoryName, s.runID), filepath.Join(stage, "rollback")); err != nil {
		_ = storageError("rollback_inputs", err)
	}
}

// Close 释放当前尝试未被运行接管的原件。
func (s *StagedInputs) Close() {
	root, err := os.OpenRoot(s.workspaceRoot)
	if err != nil {
		_ = storageError("cleanup_staged_inputs", err)
		return
	}
	defer root.Close()
	stageRoot, err := openRealInputDirectory(root, constants.WorkflowInputStagingDirectoryName)
	if err != nil {
		_ = storageError("cleanup_staged_inputs", err)
		return
	}
	if stageRoot == nil {
		return
	}
	defer stageRoot.Close()
	if err := stageRoot.RemoveAll(filepath.Base(s.Directory)); err != nil {
		_ = storageError("cleanup_staged_inputs", err)
	}
}

// RecoverStagedInputs 清理退出前未释放的接纳暂存。调用者已独占受管根且尚未接纳工作。
func RecoverStagedInputs(ctx context.Context, managedDirectory string, ownedRuns func(context.Context, string, []string) ([]string, error)) error {
	managed, err := os.OpenRoot(managedDirectory)
	if err != nil {
		return storageError("recover_staged_inputs", err)
	}
	defer managed.Close()
	workspaces, err := openRealInputDirectory(managed, constants.WorkflowWorkspaceDirectoryName)
	if err != nil {
		return storageError("recover_staged_inputs", err)
	}
	if workspaces == nil {
		return nil
	}
	defer workspaces.Close()
	entries, err := fs.ReadDir(workspaces.FS(), ".")
	if err != nil {
		return storageError("recover_staged_inputs", err)
	}
	var recoveryErr error
	for _, entry := range entries {
		if ctx.Err() != nil {
			return status_error.OperationCanceled
		}
		if !entry.IsDir() || !identity(entry.Name()) {
			continue
		}
		if err := recoverWorkspaceInputStages(ctx, workspaces, entry.Name(), ownedRuns); err != nil {
			recoveryErr = storageError("recover_staged_inputs", err)
		}
	}
	return recoveryErr
}

func recoverWorkspaceInputStages(ctx context.Context, workspaces *os.Root, definition string, ownedRuns func(context.Context, string, []string) ([]string, error)) error {
	workspace, err := openRealInputDirectory(workspaces, definition)
	if err != nil || workspace == nil {
		return err
	}
	defer workspace.Close()
	name := constants.WorkflowInputStagingDirectoryName
	staged, err := openRealInputDirectory(workspace, name)
	if err != nil || staged == nil {
		return err
	}
	entries, err := fs.ReadDir(staged.FS(), ".")
	staged.Close()
	if err != nil {
		return err
	}
	var runIDs []string
	for _, entry := range entries {
		runID, attemptID, _ := strings.Cut(entry.Name(), "-")
		if entry.IsDir() && identity(runID) && identity(attemptID) {
			runIDs = append(runIDs, runID)
		}
	}
	if len(runIDs) > 0 {
		owned, err := ownedRuns(ctx, definition, runIDs)
		if err != nil {
			return err
		}
		retained := make(map[string]bool, len(owned))
		for _, runID := range owned {
			retained[runID] = true
		}
		for _, runID := range runIDs {
			if !retained[runID] {
				if err := workspace.RemoveAll(filepath.Join(constants.WorkflowInputFileDirectoryName, runID)); err != nil {
					return err
				}
			}
		}
	}
	return workspace.RemoveAll(name)
}

func openRealInputDirectory(root *os.Root, name string) (*os.Root, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) || err == nil && !info.IsDir() {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	directory, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	actual, err := directory.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		directory.Close()
		return nil, err
	}
	return directory, nil
}

// FreezeLocalInput 只从认证层冻结的目录或精确文件读取，并将字节复制为本次 Run 的输入。
func FreezeLocalInput(ctx context.Context, roots, files []string, reference, workingDirectory, runID, pointer, managedSpace string) (LocalInput, error) {
	return freezeLocalInput(ctx, roots, files, reference, workingDirectory, runID, pointer, managedSpace, "")
}

// ReplayFrozenInput 核验来源 Run 的副本身份和实际字节，再发布拥有同一摘要的新副本。
func ReplayFrozenInput(ctx context.Context, source Workspace, sourceRunID string, destination Workspace, runID, pointer, reference, expected string) (LocalInput, error) {
	if decoded, err := hex.DecodeString(expected); err != nil || len(decoded) != sha256.Size {
		return LocalInput{}, status_error.WorkflowFileInvalid
	}
	root := filepath.Join(source.RootDirectory, constants.WorkflowInputFileDirectoryName, sourceRunID)
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil || root != canonical {
		return LocalInput{}, status_error.WorkflowFileInvalid
	}
	reference = source.ResolveRunReference(reference)
	withinRun, err := filepath.Rel(root, reference)
	parts := strings.Split(filepath.ToSlash(withinRun), "/")
	if err != nil || len(parts) != 2 || !identity(parts[0]) || !fileName(parts[1]) {
		return LocalInput{}, status_error.WorkflowFileInvalid
	}
	return freezeLocalInput(ctx, []string{root}, nil, reference, destination.RootDirectory, runID, pointer, "", expected)
}

func inputFieldKey(pointer string) string {
	field := sha256.Sum256([]byte(pointer))
	return hex.EncodeToString(field[:16])
}

func freezeLocalInput(ctx context.Context, roots, files []string, reference, workingDirectory, runID, pointer, managedSpace, expected string) (LocalInput, error) {
	var source *os.File
	for _, root := range roots {
		file, err := openAuthorizedScopeFile(root, reference, managedSpace)
		if err == nil {
			source = file
			break
		}
	}
	if source == nil && filepath.IsAbs(reference) {
		actual, err := filepath.EvalSymlinks(reference)
		if err == nil {
			for _, selected := range files {
				if !filepath.IsAbs(selected) || actual != selected {
					continue
				}
				current, err := filepath.EvalSymlinks(selected)
				if err != nil || current != selected {
					continue
				}
				file, err := openAuthorizedScopeFile(filepath.Dir(selected), reference, managedSpace)
				if err != nil {
					continue
				}
				info, statErr := os.Lstat(selected)
				opened, openErr := file.Stat()
				if statErr != nil || openErr != nil || !info.Mode().IsRegular() || !os.SameFile(info, opened) {
					file.Close()
					continue
				}
				source = file
				break
			}
		}
	}
	if source == nil {
		return LocalInput{}, status_error.WorkflowFileInvalid
	}
	defer source.Close()
	return FreezeOpenFile(ctx, source, reference, workingDirectory, runID, pointer, expected)
}

// FreezeOpenFile 复制调用者已按运行范围打开的普通文件，保留该句柄对应的原件身份。
func FreezeOpenFile(ctx context.Context, source *os.File, reference, workingDirectory, runID, pointer, expected string) (LocalInput, error) {
	return freezeOpenFile(ctx, source, filepath.Ext(reference), workingDirectory, runID, pointer, expected, "")
}

// ForwardFrozenFile 使用原收据的名称与摘要，把子运行成果移交给父运行。
func ForwardFrozenFile(ctx context.Context, source *os.File, name, workingDirectory, runID, pointer, digest string) (LocalInput, error) {
	if !fileName(name) {
		return LocalInput{}, status_error.WorkflowFileInvalid
	}
	return freezeOpenFile(ctx, source, "", workingDirectory, runID, pointer+":"+digest, digest, name)
}

func freezeOpenFile(ctx context.Context, source *os.File, extension, workingDirectory, runID, pointer, expected, name string) (LocalInput, error) {
	root, err := os.OpenRoot(workingDirectory)
	if err != nil {
		return LocalInput{}, storageError("cli_input_workspace", err)
	}
	defer root.Close()
	directory := filepath.Join(constants.WorkflowInputFileDirectoryName, runID, inputFieldKey(pointer))
	if err := root.MkdirAll(directory, 0o700); err != nil {
		return LocalInput{}, storageError("cli_input_directory", err)
	}
	temporaryName := filepath.Join(directory, util.New()+".tmp")
	file, err := root.OpenFile(temporaryName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return LocalInput{}, storageError("cli_input_create", err)
	}
	defer root.Remove(temporaryName)
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(source, constants.WorkflowFileMaximumBytes+1))
	syncErr, closeErr := file.Sync(), file.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil || size > constants.WorkflowFileMaximumBytes {
		return LocalInput{}, status_error.WorkflowFileInvalid
	}
	if ctx.Err() != nil {
		return LocalInput{}, status_error.WorkflowCancelled
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if expected != "" && digest != expected {
		return LocalInput{}, status_error.WorkflowFileInvalid
	}
	if name == "" {
		name = digest + extension
	}
	target := filepath.Join(directory, name)
	if existing, err := root.Open(target); err == nil {
		actual := sha256.New()
		count, readErr := io.Copy(actual, io.LimitReader(existing, constants.WorkflowFileMaximumBytes+1))
		existing.Close()
		if readErr != nil || count != size || hex.EncodeToString(actual.Sum(nil)) != digest {
			return LocalInput{}, status_error.WorkflowFileInvalid
		}
		return LocalInput{Path: filepath.Join(workingDirectory, target), Digest: digest}, nil
	}
	if err := root.Rename(temporaryName, target); err != nil {
		return LocalInput{}, storageError("cli_input_commit", err)
	}
	return LocalInput{Path: filepath.Join(workingDirectory, target), Digest: digest}, nil
}
