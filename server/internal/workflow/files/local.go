package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"

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

// FreezeLocalInput 只从认证层冻结的目录读取，并将字节复制为本次 Run 的输入。
func FreezeLocalInput(ctx context.Context, roots []string, reference, workingDirectory, runID, pointer string) (LocalInput, error) {
	var source *os.File
	for _, root := range roots {
		file, err := OpenScopeFile(root, reference)
		if err == nil {
			source = file
			break
		}
	}
	if source == nil {
		return LocalInput{}, status_error.WorkflowFileInvalid
	}
	defer source.Close()
	root, err := os.OpenRoot(workingDirectory)
	if err != nil {
		return LocalInput{}, storageError("cli_input_workspace", err)
	}
	defer root.Close()
	field := sha256.Sum256([]byte(pointer))
	directory := filepath.Join(constants.WorkflowInputFileDirectoryName, runID, hex.EncodeToString(field[:16]))
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
	name := hex.EncodeToString(hash.Sum(nil)) + filepath.Ext(reference)
	target := filepath.Join(directory, name)
	if existing, err := root.Open(target); err == nil {
		actual := sha256.New()
		count, readErr := io.Copy(actual, io.LimitReader(existing, constants.WorkflowFileMaximumBytes+1))
		existing.Close()
		if readErr != nil || count != size || hex.EncodeToString(actual.Sum(nil)) != hex.EncodeToString(hash.Sum(nil)) {
			return LocalInput{}, status_error.WorkflowFileInvalid
		}
		return LocalInput{Path: filepath.Join(workingDirectory, target), Digest: hex.EncodeToString(hash.Sum(nil))}, nil
	}
	if err := root.Rename(temporaryName, target); err != nil {
		return LocalInput{}, storageError("cli_input_commit", err)
	}
	return LocalInput{Path: filepath.Join(workingDirectory, target), Digest: hex.EncodeToString(hash.Sum(nil))}, nil
}
