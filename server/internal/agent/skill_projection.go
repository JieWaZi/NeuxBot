package agent

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"syscall"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	managedskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
)

const windowsPrivilegeNotHeld = syscall.Errno(1314)

type skillWorkspace struct {
	// root confines manifest and projection changes to the canonical working directory.
	root *os.Root
	// installed lists the last committed native payload digest for each managed entry.
	installed managedskills.MaterializationManifest
	// verified avoids rehashing a shared snapshot within the same prepare transaction.
	verified map[string]bool
	// missingSources records only skipped source documents that may reappear.
	missingSources []string
}

type installedSkill struct {
	// digest is the recorded hash of the directory payload, including execution bits.
	digest string
	// linked identifies a managed link to the workspace's content-addressed snapshot.
	linked bool
}

// prepareSkills publishes one reserved target under ProviderRuntime's file mutex.
func prepareSkills(selection *SkillSelection, missingSources *[]string) error {
	if selection == nil {
		return nil
	}
	workspace, err := openSkillWorkspace(selection.cwd, selection.directory, selection.optional)
	if err != nil {
		return err
	}
	defer workspace.root.Close()
	err = workspace.prepareTarget(selection.directory, selection.entries, selection.optional)
	if missingSources != nil {
		*missingSources = workspace.missingSources
	}
	return err
}

func openSkillWorkspace(cwd, directory string, optional bool) (*skillWorkspace, error) {
	observed, err := os.Lstat(cwd)
	if err != nil || !realSkillDirectory(observed) {
		return nil, status_error.SkillContentInvalid
	}
	root, err := os.OpenRoot(cwd)
	if err != nil {
		return nil, skillStorageError(err)
	}
	actual, err := root.Lstat(".")
	if err != nil || !os.SameFile(observed, actual) {
		root.Close()
		return nil, status_error.SkillContentInvalid
	}
	workspace := &skillWorkspace{root: root, verified: map[string]bool{}}
	err = workspace.readManifest()
	if optional && errors.Is(err, status_error.SkillContentInvalid) {
		// Ownership from a malformed manifest cannot be used to touch individual
		// entries. Isolate the discovery directory as a unit, preserving its files.
		if err = workspace.archiveSkillDiscovery(directory); err == nil {
			err = workspace.archiveEditedPath(managedskills.MaterializationManifestName)
		}
		if err == nil {
			err = workspace.readManifest()
		}
	}
	if err != nil {
		root.Close()
		return nil, err
	}
	return workspace, nil
}

func (w *skillWorkspace) readManifest() error {
	name := managedskills.MaterializationManifestName
	info, err := w.root.Lstat(name)
	if os.IsNotExist(err) {
		w.installed = managedskills.MaterializationManifest{
			Version: managedskills.MaterializationManifestVersion,
			Targets: map[string]map[string]string{},
		}
		return nil
	}
	if err != nil {
		return skillStorageError(err)
	}
	if !info.Mode().IsRegular() || info.Size() > managedskills.MaterializationManifestMaximumBytes {
		return status_error.SkillContentInvalid
	}
	file, err := w.root.Open(name)
	if err != nil {
		return skillStorageError(err)
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return status_error.SkillContentInvalid
	}
	data, err := io.ReadAll(io.LimitReader(file, managedskills.MaterializationManifestMaximumBytes+1))
	if err != nil {
		return skillStorageError(err)
	}
	w.installed, err = managedskills.ParseMaterializationManifest(data)
	return err
}

// prepareTarget updates the selected discovery directory and its manifest entries.
func (w *skillWorkspace) prepareTarget(directory string, entries []skillEntry, optional bool) error {
	if !safeSkillEntryName(directory) {
		return status_error.SkillContentInvalid
	}
	key := directory + "/" + managedskills.MaterializationDiscoveryDirectory
	old := w.installed.Targets[key]
	if len(old) == 0 && len(entries) == 0 {
		return nil
	}
	parents := []string{directory, managedskills.MaterializationDiscoveryDirectory}
	if err := ensureSkillParents(w.root, parents, len(old) == 0 || optional); err != nil {
		if !optional || !errors.Is(err, status_error.SkillContentInvalid) {
			return err
		}
		if err := w.archiveSkillDiscovery(directory); err != nil {
			return err
		}
		if err := ensureSkillParents(w.root, parents, true); err != nil {
			return err
		}
	}
	previous := map[string]installedSkill{}
	next := map[string]string{}
	for name, digest := range old {
		target := filepath.Join(key, name)
		if optional {
			if _, err := w.root.Lstat(target); os.IsNotExist(err) {
				continue
			}
		}
		current, err := w.verifyInstalledSkill(target, digest, optional)
		if optional && errors.Is(err, status_error.SkillContentInvalid) {
			// Rename only the manifest-owned entry, never follow a substituted link.
			if err = w.archiveEditedPath(target); err == nil {
				continue
			}
		}
		if err != nil {
			return err
		}
		previous[name] = current
	}
	candidates := make([]skillEntry, 0, len(entries))
	for _, entry := range entries {
		if _, owned := previous[entry.name]; !owned {
			_, err := w.root.Lstat(filepath.Join(key, entry.name))
			if err == nil && optional {
				if entry.name != constants.AgentCLISkillName {
					continue
				}
				// The reserved host skill name cannot refer to unverified content.
				if err := w.archiveEditedPath(filepath.Join(key, entry.name)); err != nil {
					return err
				}
				err = fs.ErrNotExist
			}
			if !os.IsNotExist(err) {
				return status_error.SkillContentInvalid
			}
		}
		candidates = append(candidates, entry)
	}

	stagePath, err := os.MkdirTemp(w.root.Name(), ".neuxbot-skills-stage-")
	if err != nil {
		return skillStorageError(err)
	}
	stage := filepath.Base(stagePath)
	keepStage := false
	committed := false
	created := map[string]bool{}
	defer func() {
		if !keepStage {
			if err := w.root.RemoveAll(stage); err != nil {
				logx.Errorf("clean managed skill staging: error=%v", err)
			}
			if !committed {
				for digest := range created {
					w.removeUnusedSnapshot(digest)
				}
			}
		}
	}()
	if err := w.root.Mkdir(filepath.Join(stage, "new"), 0700); err != nil {
		return skillStorageError(err)
	}
	if err := w.root.Mkdir(filepath.Join(stage, "old"), 0700); err != nil {
		return skillStorageError(err)
	}
	changes := map[string]bool{}
	for _, entry := range candidates {
		digest, err := w.publishSnapshot(stage, entry, created)
		if err != nil {
			if optional {
				path := filepath.Join(entry.path, "SKILL.md")
				if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
					w.missingSources = append(w.missingSources, path)
				}
				logx.Warnf("skip unavailable managed skill: name=%s error=%v", entry.name, err)
				continue
			}
			return err
		}
		next[entry.name] = digest
		prior := previous[entry.name]
		if prior.linked && prior.digest == digest {
			continue
		}
		newEntry := filepath.Join(stage, "new", entry.name)
		// Stage and destination have the same depth, so the link resolves to
		// the shared payload before and after installation (including on Windows).
		err = os.Symlink(skillSnapshotLink(digest), filepath.Join(w.root.Name(), newEntry))
		if err != nil {
			if runtime.GOOS != "windows" || !(errors.Is(err, fs.ErrPermission) || errors.Is(err, windowsPrivilegeNotHeld) || errors.Is(err, errors.ErrUnsupported)) {
				return skillStorageError(err)
			}
			if prior.digest == digest {
				continue
			}
			if err := managedskills.CopyDirectory(filepath.Join(w.root.Name(), skillSnapshotPath(digest)), filepath.Join(w.root.Name(), newEntry)); err != nil {
				return err
			}
			if err := verifySkillDirectory(filepath.Join(w.root.Name(), newEntry), digest); err != nil {
				return err
			}
		}
		changes[entry.name] = true
	}
	for name := range previous {
		if _, stays := next[name]; !stays {
			changes[name] = false
		}
	}
	if len(changes) == 0 && maps.Equal(old, next) {
		committed = true
		return nil
	}
	manifest := w.installed
	manifest.Targets = maps.Clone(manifest.Targets)
	manifest.Targets[key] = next
	data, err := json.Marshal(manifest)
	if err != nil || len(data) > managedskills.MaterializationManifestMaximumBytes {
		return status_error.SkillContentInvalid
	}
	if err := w.root.WriteFile(filepath.Join(stage, "manifest"), data, 0600); err != nil {
		return skillStorageError(err)
	}
	if err := w.replaceTargetEntries(stage, key, changes, previous, &keepStage); err != nil {
		return err
	}
	w.installed = manifest
	committed = true
	for name, digest := range old {
		if digest != next[name] {
			w.removeUnusedSnapshot(digest)
		}
	}
	return nil
}

func (w *skillWorkspace) replaceTargetEntries(stage, key string, changes map[string]bool, previous map[string]installedSkill, keepStage *bool) error {
	// Keep backups until the new manifest commits. An incomplete rollback leaves
	// the backups in place for recovery.
	changed := []string{}
	rollback := func() {
		for _, name := range slices.Backward(changed) {
			target := filepath.Join(key, name)
			if err := w.root.RemoveAll(target); err != nil {
				*keepStage = true
				logx.Errorf("rollback managed skill install: error=%v", err)
				continue
			}
			if _, existed := previous[name]; existed {
				if err := w.root.Rename(filepath.Join(stage, "old", name), target); err != nil {
					*keepStage = true
					logx.Errorf("restore managed skill backup: error=%v", err)
				}
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(changes)) {
		target := filepath.Join(key, name)
		if _, existed := previous[name]; existed {
			if err := w.root.Rename(target, filepath.Join(stage, "old", name)); err != nil {
				rollback()
				return skillStorageError(err)
			}
		}
		changed = append(changed, name)
		if changes[name] {
			if err := w.root.Rename(filepath.Join(stage, "new", name), target); err != nil {
				rollback()
				return skillStorageError(err)
			}
		}
	}
	if err := w.root.Rename(filepath.Join(stage, "manifest"), managedskills.MaterializationManifestName); err != nil {
		rollback()
		return skillStorageError(err)
	}
	return nil
}

func (w *skillWorkspace) verifyInstalledSkill(target, digest string, optional bool) (installedSkill, error) {
	info, err := w.root.Lstat(target)
	if err != nil {
		return installedSkill{}, status_error.SkillContentInvalid
	}
	installation := installedSkill{digest: digest}
	if realSkillDirectory(info) {
		// Native discovery can use a verified directory copy when links are unavailable.
		return installation, verifySkillDirectory(filepath.Join(w.root.Name(), target), digest)
	}
	link, err := w.root.Readlink(target)
	if err != nil || filepath.ToSlash(link) != filepath.ToSlash(skillSnapshotLink(digest)) {
		return installedSkill{}, status_error.SkillContentInvalid
	}
	installation.linked = true
	if optional {
		if err := ensureSkillParents(w.root, []string{managedskills.MaterializationWorkspaceDirectory, managedskills.MaterializationDiscoveryDirectory}, true); err != nil {
			return installedSkill{}, err
		}
		if _, err := w.root.Lstat(skillSnapshotPath(digest)); os.IsNotExist(err) {
			// The exact owned link can be reused once a replacement snapshot is verified.
			return installation, nil
		}
	}
	err = w.verifySnapshot(digest)
	if optional && filepath.Base(target) != constants.AgentCLISkillName && errors.Is(err, status_error.SkillContentInvalid) {
		// Another discovery target may still have a live lease on the same payload.
		// Leave its bytes in place and let this target quarantine only its own link.
		for other, entries := range w.installed.Targets {
			if filepath.FromSlash(other) == filepath.Dir(target) {
				continue
			}
			for _, referenced := range entries {
				if referenced == digest {
					return installedSkill{}, err
				}
			}
		}
		snapshot := skillSnapshotPath(digest)
		if info, statErr := w.root.Lstat(snapshot); statErr == nil && realSkillDirectory(info) {
			// Keep local edits outside native discovery. The owned link can then be
			// restored from verified library content or detached if its source is gone.
			err = w.archiveEditedPath(snapshot)
		}
	}
	return installation, err
}

func (w *skillWorkspace) archiveSkillDiscovery(directory string) error {
	if !safeSkillEntryName(directory) {
		return status_error.SkillContentInvalid
	}
	parent, err := w.root.Lstat(directory)
	if err == nil && !realSkillDirectory(parent) {
		// The discovery parent itself was replaced. Preserve it without following it.
		if err := w.archiveEditedPath(directory); err != nil {
			return err
		}
	} else if err == nil {
		target := filepath.Join(directory, managedskills.MaterializationDiscoveryDirectory)
		if _, err := w.root.Lstat(target); !os.IsNotExist(err) {
			if err != nil {
				return skillStorageError(err)
			}
			if err := w.archiveEditedPath(target); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return skillStorageError(err)
	}
	return nil
}

func (w *skillWorkspace) archiveEditedPath(path string) error {
	parts := []string{managedskills.MaterializationWorkspaceDirectory, managedskills.MaterializationDiscoveryDirectory}
	if err := ensureSkillParents(w.root, parts, true); err != nil {
		return err
	}
	parent := filepath.Join(parts...)
	backup, err := os.MkdirTemp(filepath.Join(w.root.Name(), parent), ".edited-")
	if err != nil {
		return skillStorageError(err)
	}
	backup = filepath.Join(parent, filepath.Base(backup))
	archivedSnapshot := filepath.Join(backup, filepath.Base(path))
	if err := w.root.Rename(path, archivedSnapshot); err != nil {
		if cleanupErr := w.root.Remove(backup); cleanupErr != nil {
			logx.Errorf("clean empty managed skill backup: error=%v", cleanupErr)
		}
		return skillStorageError(err)
	}
	logx.Warnf("preserved an edited managed skill snapshot: backup=%s", archivedSnapshot)
	return nil
}

func (w *skillWorkspace) publishSnapshot(stage string, entry skillEntry, created map[string]bool) (string, error) {
	digest := entry.digest
	var include func(string) bool
	if entry.name == constants.AgentCLISkillName {
		include = managedskills.IsSystemSkillDocument
		full, projected, err := managedskills.SelectedDigests(entry.path, include)
		if err != nil || full != entry.digest {
			return "", status_error.SkillContentInvalid
		}
		digest = projected
	}
	if err := ensureSkillParents(w.root, []string{managedskills.MaterializationWorkspaceDirectory, managedskills.MaterializationDiscoveryDirectory}, true); err != nil {
		return "", err
	}
	target := skillSnapshotPath(digest)
	if _, err := w.root.Lstat(target); !os.IsNotExist(err) {
		return digest, w.verifySnapshot(digest)
	}
	staged := filepath.Join(stage, "snapshot-"+digest)
	if err := w.root.Mkdir(staged, 0700); err != nil {
		return "", skillStorageError(err)
	}
	defer func() {
		if err := w.root.RemoveAll(staged); err != nil {
			logx.Errorf("clean managed skill candidate: error=%v", err)
		}
	}()
	stagedPath := filepath.Join(w.root.Name(), staged)
	if err := managedskills.CopySelectedDirectory(entry.path, stagedPath, include); err != nil {
		return "", err
	}
	if err := verifySkillDirectory(stagedPath, digest); err != nil {
		return "", err
	}
	if err := validateSnapshotLinks(stagedPath); err != nil {
		return "", err
	}
	if err := w.root.Rename(staged, target); err != nil {
		return "", skillStorageError(err)
	}
	created[digest] = true
	w.verified[digest] = true
	return digest, nil
}

func (w *skillWorkspace) verifySnapshot(digest string) error {
	if w.verified[digest] {
		return nil
	}
	if err := ensureSkillParents(w.root, []string{managedskills.MaterializationWorkspaceDirectory, managedskills.MaterializationDiscoveryDirectory}, false); err != nil {
		return err
	}
	path := filepath.Join(w.root.Name(), skillSnapshotPath(digest))
	if err := verifySkillDirectory(path, digest); err != nil {
		return err
	}
	if err := validateSnapshotLinks(path); err != nil {
		return err
	}
	w.verified[digest] = true
	return nil
}

func (w *skillWorkspace) removeUnusedSnapshot(digest string) {
	for _, entries := range w.installed.Targets {
		for _, referenced := range entries {
			if referenced == digest {
				return
			}
		}
	}
	if err := ensureSkillParents(w.root, []string{managedskills.MaterializationWorkspaceDirectory, managedskills.MaterializationDiscoveryDirectory}, false); err != nil {
		return
	}
	if err := verifySkillDirectory(filepath.Join(w.root.Name(), skillSnapshotPath(digest)), digest); err != nil {
		return
	}
	if err := w.root.RemoveAll(skillSnapshotPath(digest)); err != nil {
		logx.Errorf("remove unused managed skill snapshot: digest=%s error=%v", digest, err)
		return
	}
	delete(w.verified, digest)
}

func skillSnapshotPath(digest string) string {
	return filepath.FromSlash(managedskills.MaterializationSnapshotPath(digest))
}

func skillSnapshotLink(digest string) string {
	return filepath.Join("..", "..", skillSnapshotPath(digest))
}

func verifySkillDirectory(path, digest string) error {
	info, err := os.Lstat(path)
	if err != nil || !realSkillDirectory(info) {
		return status_error.SkillContentInvalid
	}
	// The archive digest ignores a root .git directory, so its presence
	// prevents an existing target from being claimed as unchanged.
	if info, err := os.Lstat(filepath.Join(path, ".git")); (err != nil && !os.IsNotExist(err)) || (err == nil && info.IsDir()) {
		return status_error.SkillContentInvalid
	}
	actual, err := managedskills.Digest(path)
	if err != nil || actual != digest {
		return status_error.SkillContentInvalid
	}
	return nil
}

func validateSnapshotLinks(path string) error {
	root, err := os.OpenRoot(path)
	if err != nil {
		return skillStorageError(err)
	}
	defer root.Close()
	info, err := root.Stat("SKILL.md")
	if err != nil || !info.Mode().IsRegular() {
		return status_error.SkillContentInvalid
	}
	return fs.WalkDir(root.FS(), ".", func(relative string, entry fs.DirEntry, err error) error {
		if err != nil {
			return skillStorageError(err)
		}
		if entry.Type()&os.ModeSymlink == 0 {
			return nil
		}
		link, err := root.Readlink(filepath.FromSlash(relative))
		if err != nil || filepath.IsAbs(link) || filepath.VolumeName(link) != "" ||
			!filepath.IsLocal(filepath.Join(filepath.Dir(relative), link)) {
			return status_error.SkillContentInvalid
		}
		// Root checks the entire chain without allowing a target outside this
		// snapshot. A missing internal target can still be used as a relative link.
		if _, err := root.Stat(filepath.FromSlash(relative)); err != nil && !os.IsNotExist(err) {
			return status_error.SkillContentInvalid
		}
		return nil
	})
}

// ensureSkillParents keeps every parent of a managed native entry and snapshot a real directory.
func ensureSkillParents(root *os.Root, parts []string, create bool) error {
	var name string
	for _, part := range parts {
		name = filepath.Join(name, part)
		info, err := root.Lstat(name)
		if os.IsNotExist(err) && create {
			if err := root.Mkdir(name, 0700); err != nil {
				return skillStorageError(err)
			}
			continue
		}
		if err != nil || !realSkillDirectory(info) {
			return status_error.SkillContentInvalid
		}
	}
	return nil
}

func realSkillDirectory(info os.FileInfo) bool {
	return info.IsDir() && info.Mode()&os.ModeSymlink == 0
}
