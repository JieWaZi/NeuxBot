package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// removeUnusedChatPartialFiles 在成功附件交付后只释放本 Run 未使用的部分产物副本。
func (s *Service) removeUnusedChatPartialFiles(ctx context.Context, run models.WorkflowRun) error {
	if run.Status != constants.WorkflowRunStatusSucceeded || len(run.ChatFiles) == 0 {
		return nil
	}
	retained := map[string]bool{}
	for _, file := range run.ChatFiles {
		if !file.Partial && file.Path != "" {
			retained[file.Path] = true
		}
	}
	unneeded := []string{}
	for key, file := range run.ChatFiles {
		if !file.Partial || file.Error != "" || retained[file.Path] {
			continue
		}
		digest, err := hex.DecodeString(file.Digest)
		if err != nil || len(digest) != sha256.Size {
			continue
		}
		path := filepath.Join(constants.WorkflowInputFileDirectoryName, run.ID, outputFileID(key), file.Digest+filepath.Ext(file.Name))
		if filepath.IsLocal(path) && file.Path == path {
			unneeded = append(unneeded, path)
		}
	}
	if len(unneeded) == 0 {
		return nil
	}
	base, err := filepath.EvalSymlinks(s.dataDirectory)
	if err != nil {
		return status_error.WorkflowFileStorageError
	}
	relative, err := filepath.Rel(s.dataDirectory, s.chatOutputDirectory(run))
	if err != nil || !filepath.IsLocal(relative) {
		return status_error.WorkflowFileInvalid
	}
	directory := filepath.Join(base, relative)
	actual, err := filepath.EvalSymlinks(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || actual != directory {
		return status_error.WorkflowFileInvalid
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() {
		return status_error.WorkflowFileInvalid
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return status_error.WorkflowFileStorageError
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return status_error.WorkflowFileInvalid
	}
	for _, path := range unneeded {
		if err := ctx.Err(); err != nil {
			return err
		}
		full := filepath.Join(directory, path)
		actual, err := filepath.EvalSymlinks(full)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || actual != full {
			return status_error.WorkflowFileInvalid
		}
		fileInfo, err := root.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !fileInfo.Mode().IsRegular() {
			return status_error.WorkflowFileInvalid
		}
		if err := root.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return status_error.WorkflowFileStorageError
		}
	}
	return nil
}
