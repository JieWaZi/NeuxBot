package installation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	filesystem "github.com/JieWaZi/NeuxBot/server/internal/storage"
)

const retiredStageMarker = ".cleanup-ready"

var abandonedStage = regexp.MustCompile(`^\.staging-[0-9]+$`)

// ReclaimVersions measures or removes unused, verified private versions while holding the installer gate.
// The caller holds the provider and account maintenance fence before enabling removal.
func (i *Installer) ReclaimVersions(ctx context.Context, id string, protected []string, remove bool) (int64, error) {
	return i.reclaimPrivateFiles(ctx, id, protected, remove, true)
}

// RecoverRetiredStages resumes only terminal staging cleanup when an installer is opened under the Core root lock.
func (i *Installer) RecoverRetiredStages(ctx context.Context) error {
	entries, err := os.ReadDir(i.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return failure("read managed installation root", err)
	}
	var result error
	for _, entry := range entries {
		if ctx.Err() != nil {
			return errors.Join(result, canceled(ctx))
		}
		if !entry.IsDir() || !i.Supported(entry.Name()) {
			continue
		}
		_, err := i.reclaimPrivateFiles(ctx, entry.Name(), nil, true, false)
		result = errors.Join(result, err)
	}
	return result
}

func (i *Installer) reclaimPrivateFiles(ctx context.Context, id string, protected []string, remove, versions bool) (int64, error) {
	if !i.Supported(id) {
		return 0, nil
	}
	release, err := i.beginMaintenance(id)
	if err != nil {
		return 0, err
	}
	defer release()
	if err := i.validateStore(id); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	if _, err := i.readUninstallJournal(id); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return 0, err
		}
		return 0, status_error.RuntimeInstallationBusy
	}
	parent := filepath.Join(i.root, id)
	root, err := os.OpenRoot(parent)
	if err != nil {
		return 0, failure("open managed program store", err)
	}
	defer root.Close()
	entries, err := os.ReadDir(parent)
	if err != nil {
		return 0, failure("read managed program store", err)
	}
	owned := make([]managedVersion, 0, len(entries))
	candidates := make([]string, 0)
	for _, entry := range entries {
		if ctx.Err() != nil {
			return 0, canceled(ctx)
		}
		if !entry.IsDir() {
			continue
		}
		if abandonedStage.MatchString(entry.Name()) {
			marker := filepath.Join(entry.Name(), retiredStageMarker)
			info, err := root.Lstat(marker)
			if err == nil && info.Mode().IsRegular() && info.Size() == int64(len(stageReceipt(id, entry.Name()))) {
				if recorded, err := root.ReadFile(marker); err == nil && string(recorded) == stageReceipt(id, entry.Name()) {
					candidates = append(candidates, entry.Name())
				}
			}
		} else if versions && strings.HasPrefix(entry.Name(), "version-") {
			if version, err := i.readManagedVersion(id, filepath.Join(parent, entry.Name())); err == nil {
				owned = append(owned, version)
			}
		}
	}
	slices.SortFunc(owned, func(a, b managedVersion) int { return strings.Compare(b.root, a.root) })
	for index, version := range owned {
		if index == 0 {
			continue
		}
		used := slices.ContainsFunc(protected, func(binary string) bool {
			if binary == "" {
				return false
			}
			if inside(version.root, binary) {
				return true
			}
			real, err := filepath.EvalSymlinks(binary)
			return err == nil && inside(version.root, real)
		})
		if !used {
			candidates = append(candidates, filepath.Base(version.root))
		}
	}

	var total int64
	var failed error
	for _, name := range candidates {
		if ctx.Err() != nil {
			return total, errors.Join(failed, canceled(ctx))
		}
		folder := filepath.Join(parent, name)
		measured, err := filesystem.Measure(ctx, filesystem.Roots{Data: folder, Managed: folder}, filesystem.Limits{})
		if err != nil || !measured.Complete {
			failed = errors.Join(failed, status_error.StorageScanIncomplete)
			continue
		}
		if remove {
			if err := root.RemoveAll(name); err != nil {
				failed = errors.Join(failed, failure("remove obsolete managed version", err))
				continue
			}
		}
		total += measured.Bytes
	}
	return total, failed
}

// A retirement receipt is only published after the staging owner and every subprocess have returned.
func stageReceipt(id, name string) string {
	return "neuxbot-managed-installation-retired:" + id + ":" + name
}

func markRetiredStage(staging, id string) error {
	marker, err := os.OpenFile(filepath.Join(staging, retiredStageMarker), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = marker.WriteString(stageReceipt(id, filepath.Base(staging)))
	if err == nil {
		err = marker.Sync()
	}
	return errors.Join(err, marker.Close())
}
