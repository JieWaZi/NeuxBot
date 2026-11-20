// Package cliinstall installs the bundled botcli for interactive user terminals.
package cliinstall

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/gofrs/flock"
)

const botCLIBuildPath = "github.com/JieWaZi/NeuxBot/server/cmd/botcli"

// Install makes the supplied NeuxBot botcli executable available to new terminals.
func Install(ctx context.Context, executable string) (string, error) {
	if ctx.Err() != nil {
		return "", status_error.OperationCanceled
	}
	if !isBotCLI(executable) {
		logx.Errorf("install botcli: source %q is not a readable NeuxBot botcli executable", executable)
		return "", status_error.BotCLIInstallationFailed
	}
	target, err := userExecutablePath()
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(target)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", installFailure("create install directory", directory, err)
	}
	lock := flock.New(filepath.Join(directory, ".neuxbot-botcli.lock"))
	lockCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	locked, err := lock.TryLockContext(lockCtx, 100*time.Millisecond)
	cancel()
	if !locked || err != nil {
		if ctx.Err() != nil {
			return "", status_error.OperationCanceled
		}
		return "", installFailure("lock install directory", directory, err)
	}
	defer lock.Close()
	if err := installExecutable(ctx, executable, target); err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", status_error.OperationCanceled
	}
	if err := configureUserPath(directory); err != nil {
		return "", err
	}
	return target, nil
}

func isBotCLI(path string) bool {
	info, err := buildinfo.ReadFile(path)
	return err == nil && info.Path == botCLIBuildPath
}

func installExecutable(ctx context.Context, source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return installFailure("open bundled executable", source, err)
	}
	defer input.Close()
	sourceInfo, err := input.Stat()
	if err != nil {
		return installFailure("inspect bundled executable", source, err)
	}
	existing, err := os.Lstat(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return installFailure("inspect installed executable", target, err)
	}
	if existing != nil {
		if !existing.Mode().IsRegular() || !isBotCLI(target) {
			logx.Warnf("install botcli: preserving existing file at %q", target)
			return status_error.BotCLIInstallationConflict
		}
		if runtime.GOOS == "windows" || existing.Mode().Perm()&0o100 != 0 {
			if os.SameFile(sourceInfo, existing) {
				return nil
			}
			if sourceInfo.Size() == existing.Size() {
				same, err := sameExecutable(input, target)
				if err != nil {
					return installFailure("read executable contents", target, err)
				}
				if same {
					return nil
				}
			}
		}
	}
	staged, err := os.CreateTemp(filepath.Dir(target), ".neuxbot-botcli-*")
	if err != nil {
		return installFailure("stage executable", target, err)
	}
	defer os.Remove(staged.Name())
	copyErr := stageExecutable(staged, input)
	closeErr := staged.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return installFailure("stage executable", target, err)
	}
	if ctx.Err() != nil {
		return status_error.OperationCanceled
	}
	if existing != nil {
		current, err := os.Lstat(target)
		if err != nil || !os.SameFile(existing, current) {
			logx.Warnf("install botcli: existing file at %q changed during update: %v", target, err)
			return status_error.BotCLIInstallationConflict
		}
	}
	err = publishExecutable(staged.Name(), target, existing != nil)
	if errors.Is(err, os.ErrExist) {
		return status_error.BotCLIInstallationConflict
	}
	if err != nil {
		return installFailure("publish executable", target, err)
	}
	return nil
}

func sameExecutable(source *os.File, target string) (bool, error) {
	existing, err := os.Open(target)
	if err != nil {
		return false, err
	}
	defer existing.Close()
	sourceHash, targetHash := sha256.New(), sha256.New()
	if _, err := io.Copy(sourceHash, source); err != nil {
		return false, err
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	if _, err := io.Copy(targetHash, existing); err != nil {
		return false, err
	}
	return bytes.Equal(sourceHash.Sum(nil), targetHash.Sum(nil)), nil
}

func stageExecutable(target, source *os.File) error {
	if _, err := io.Copy(target, source); err != nil {
		return err
	}
	if err := target.Chmod(0o755); err != nil {
		return err
	}
	return target.Sync()
}

func installFailure(action, path string, err error) error {
	logx.Errorf("install botcli: %s %q: %v", action, path, err)
	return status_error.BotCLIInstallationFailed
}
