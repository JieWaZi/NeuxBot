package installation

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// Maintenance exposes only local, proven program ownership; it never contacts a publisher.
type Maintenance struct {
	// Method identifies the selected program's owner, independently of other installed copies.
	Method string
	// Path is the selected executable or its managed script.
	Path string
	// Installed reports whether the selected program files actually exist.
	Installed bool
	// CanInstall permits creating a private program when no selected installation exists.
	CanInstall bool
	// CanUpdate permits updating the selected program through its proven owner.
	CanUpdate bool
	// CanUninstall permits removing only the selected program's ownership scope.
	CanUninstall bool
	// Reason is a controlled explanation for unavailable maintenance.
	Reason string
	// OfficialURL is the reviewed publisher maintenance documentation.
	OfficialURL string
}

type managedVersion struct {
	// root is one immutable version within this runtime's private program directory.
	root string
	// record binds the runtime, entry paths, and verified digests.
	record installRecord
}

type maintenancePlan struct {
	// view contains locally observed capabilities.
	view Maintenance
	// managed identifies an exact selected private receipt.
	managed *managedVersion
	// manager binds a selected global package to its locally proven owner.
	manager *packageOwner
	// pending retains a validated uninstall owner after interrupted file removal.
	pending *uninstallJournal
	// native binds a self-contained external file to a verified publisher digest.
	native *nativeOwner
}

type uninstallEntry struct {
	// Original is a direct version directory basename within the runtime store.
	Original string
	// Staged is the corresponding hidden uninstall directory basename.
	Staged string
	// Record retains ownership after partial deletion removes the on-disk receipt.
	Record installRecord
}

type uninstallJournal struct {
	// RuntimeID binds this deletion owner to one base runtime.
	RuntimeID string
	// Entries lists only prevalidated program versions, never account paths.
	Entries []uninstallEntry
}

func samePath(a, b string) bool {
	return a != "" && b != "" && filepath.Clean(a) == filepath.Clean(b)
}

func inside(root, name string) bool {
	if root == "" || !filepath.IsAbs(name) {
		return false
	}
	relative, err := filepath.Rel(root, name)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

// Inspect reports maintenance for this exact selection, never silently choosing another copy.
func (i *Installer) Inspect(ctx context.Context, id, binary string, args []string) (Maintenance, error) {
	plan, err := i.inspect(ctx, id, binary, args)
	return plan.view, err
}

func (i *Installer) inspect(ctx context.Context, id, binary string, args []string) (maintenancePlan, error) {
	plan := maintenancePlan{view: Maintenance{Method: "unknown", Path: binary, Reason: "unmanaged", OfficialURL: sources[id].URL}}
	if ctx.Err() != nil {
		return plan, canceled(ctx)
	}
	if _, exists := sources[id]; !exists {
		plan.view.Reason = "unsupported"
		return plan, nil
	}
	selectedEntry := binary
	if len(args) > 0 && filepath.IsAbs(args[0]) {
		selectedEntry = args[0]
	}
	if binary == "" || inside(filepath.Join(i.root, id), selectedEntry) {
		if journal, err := i.readUninstallJournal(id); err == nil {
			plan.pending = &journal
			plan.view.Method, plan.view.Reason, plan.view.CanUninstall = "managed", "cleanup_pending", true
			return plan, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			plan.view.Method, plan.view.Reason = "managed", "ownership-mismatch"
			return plan, nil
		}
	}
	if binary == "" {
		current, err := i.localManagedResult(id)
		if err == nil {
			binary, args = current.Binary, current.PrefixArgs
		} else if !errors.Is(err, os.ErrNotExist) && i.Supported(id) {
			plan.view.Method, plan.view.Reason = "managed", "ownership-mismatch"
			return plan, nil
		} else {
			plan.view.Method, plan.view.Reason, plan.view.CanInstall = "managed", "", i.Supported(id)
			if !i.Supported(id) {
				plan.view.Reason = "platform_unsupported"
			}
			return plan, nil
		}
	}
	plan.view.Path = binary
	entry := binary
	if len(args) > 0 && filepath.IsAbs(args[0]) {
		entry = args[0]
	}
	plan.view.Path = entry
	if _, err := os.Stat(entry); err == nil {
		plan.view.Installed = true
	} else if errors.Is(err, os.ErrNotExist) && !inside(filepath.Join(i.root, id), entry) {
		plan.view.Method, plan.view.Reason, plan.view.CanInstall = "managed", "missing", i.Supported(id)
		return plan, nil
	}
	if inside(filepath.Join(i.root, id), entry) {
		version, err := i.selectedManaged(id, binary, args)
		plan.view.Method = "managed"
		if err != nil {
			if manifest, manifestErr := i.selectedManagedRecord(id, binary, args, false); manifestErr == nil && repairableManagedPayload(manifest) {
				plan.managed = &manifest
				plan.view.CanUpdate, plan.view.CanInstall, plan.view.Reason = i.Supported(id), !plan.view.Installed && i.Supported(id), "repairable"
				plan.view.CanUninstall = true
				if !i.Supported(id) {
					plan.view.Reason = "platform_unsupported"
				}
				return plan, nil
			}
			plan.view.Reason = "ownership-mismatch"
			return plan, nil
		}
		plan.managed = &version
		plan.view.CanUpdate, plan.view.CanUninstall, plan.view.Reason = i.Supported(id), true, ""
		if !i.Supported(id) {
			plan.view.Reason = "platform_unsupported"
		}
		return plan, nil
	}
	if owner := i.packageOwnership(id, binary, args); owner != nil {
		plan.manager = owner
		plan.view.Method, plan.view.Reason = owner.method, ""
		plan.view.CanUpdate, plan.view.CanUninstall = i.Supported(id) && owner.executable != "", owner.executable != ""
		if owner.executable == "" {
			plan.view.Reason = "package-manager-unavailable"
		} else if !i.Supported(id) {
			plan.view.Reason = "platform_unsupported"
		}
		return plan, nil
	}
	if owner := i.nativeOwnership(id, binary, args); owner != nil {
		plan.native = owner
		plan.view.Method, plan.view.Path, plan.view.Reason = "native", owner.Path, ""
		plan.view.CanUpdate, plan.view.CanUninstall = i.Supported(id), true
		if !i.Supported(id) {
			plan.view.Reason = "platform_unsupported"
		}
	}
	return plan, nil
}

func (i *Installer) localManagedResult(id string) (Result, error) {
	if err := i.validateStore(id); err != nil {
		return Result{}, err
	}
	entries, err := os.ReadDir(filepath.Join(i.root, id))
	if err != nil {
		return Result{}, err
	}
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "version-") {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return Result{}, os.ErrNotExist
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	version, err := i.readManagedManifest(id, filepath.Join(i.root, id, names[0]))
	if err != nil {
		return Result{}, err
	}
	payload := filepath.Join(version.root, "payload")
	result := Result{Binary: filepath.Join(payload, filepath.FromSlash(version.record.Binary)), Version: version.record.Version, SourceURL: version.record.SourceURL}
	for _, relative := range version.record.Args {
		result.PrefixArgs = append(result.PrefixArgs, filepath.Join(payload, filepath.FromSlash(relative)))
	}
	if version.record.Node {
		node, err := i.lookPath("node")
		if err != nil {
			node = "node"
		}
		result.PrefixArgs, result.Binary = append([]string{result.Binary}, result.PrefixArgs...), node
	}
	return result, nil
}

func (i *Installer) selectedManaged(id, binary string, args []string) (managedVersion, error) {
	return i.selectedManagedRecord(id, binary, args, true)
}

func (i *Installer) selectedManagedRecord(id, binary string, args []string, verifyPayload bool) (managedVersion, error) {
	entry := binary
	if len(args) > 0 && inside(filepath.Join(i.root, id), args[0]) {
		entry = args[0]
	}
	parent := filepath.Join(i.root, id)
	for root := filepath.Dir(entry); inside(parent, root); root = filepath.Dir(root) {
		if !strings.HasPrefix(filepath.Base(root), "version-") || filepath.Dir(root) != parent {
			continue
		}
		version, err := i.readManagedManifest(id, root)
		if verifyPayload {
			version, err = i.readManagedVersion(id, root)
		}
		if err != nil {
			return version, err
		}
		payload := filepath.Join(root, "payload")
		expected := filepath.Join(payload, filepath.FromSlash(version.record.Binary))
		var expectedArgs []string
		for _, relative := range version.record.Args {
			expectedArgs = append(expectedArgs, filepath.Join(payload, filepath.FromSlash(relative)))
		}
		if version.record.Node {
			expectedArgs = append([]string{expected}, expectedArgs...)
			if reflect.DeepEqual(expectedArgs, args) {
				return version, nil
			}
		} else if samePath(expected, binary) && reflect.DeepEqual(expectedArgs, args) {
			return version, nil
		}
	}
	return managedVersion{}, failure("selected program receipt missing", nil)
}

func (i *Installer) readManagedVersion(id, root string) (managedVersion, error) {
	version, err := i.readManagedManifest(id, root)
	if err != nil {
		return version, err
	}
	for _, relative := range append([]string{version.record.Binary}, version.record.Args...) {
		file, err := confinedFile(filepath.Join(root, "payload"), relative)
		if err != nil {
			return version, err
		}
		digest, err := digestFile(file)
		if err != nil || digest != version.record.Digests[relative] {
			return version, failure("managed program ownership digest", err)
		}
	}
	return version, nil
}

func (i *Installer) readManagedManifest(id, root string) (managedVersion, error) {
	version := managedVersion{root: root}
	if err := i.validateStore(id); err != nil {
		return version, err
	}
	parent := filepath.Join(i.root, id)
	if !inside(parent, root) || filepath.Dir(root) != parent || !strings.HasPrefix(filepath.Base(root), "version-") {
		return version, failure("managed version ownership", nil)
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return version, failure("managed directory ownership", err)
	}
	name := filepath.Join(root, "installation.json")
	info, err = os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > metadataLimit {
		return version, failure("managed receipt ownership", err)
	}
	data, err := os.ReadFile(name)
	if err != nil || json.Unmarshal(data, &version.record) != nil || version.record.RuntimeID != id || !versionPattern.MatchString(version.record.Version) {
		return version, failure("managed receipt identity", err)
	}
	parsed, parseErr := url.Parse(version.record.SourceURL)
	if parseErr != nil || !officialURL(parsed) || version.record.PublishedAt <= 0 || version.record.Node && id != "gemini" {
		return version, failure("managed publisher receipt identity", parseErr)
	}
	for _, relative := range append([]string{version.record.Binary}, version.record.Args...) {
		if !safeRelative(relative) || !sha256Pattern.MatchString(version.record.Digests[relative]) {
			return version, failure("managed receipt path identity", nil)
		}
	}
	return version, nil
}

func repairableManagedPayload(version managedVersion) bool {
	payload := filepath.Join(version.root, "payload")
	info, err := os.Lstat(payload)
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	for _, relative := range append([]string{version.record.Binary}, version.record.Args...) {
		current := payload
		for _, part := range strings.Split(relative, "/") {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if errors.Is(err, os.ErrNotExist) {
				break
			}
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				return false
			}
		}
	}
	return true
}

func (i *Installer) validateStore(id string) error {
	parent := filepath.Join(i.root, id)
	for _, directory := range []string{i.root, parent} {
		info, err := os.Lstat(directory)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return failure("managed store directory ownership", nil)
		}
	}
	realRoot, err := filepath.EvalSymlinks(i.root)
	if err != nil {
		return failure("resolve managed store ownership", err)
	}
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil || !samePath(realParent, filepath.Join(realRoot, id)) {
		return failure("managed store escapes ownership", err)
	}
	return nil
}

func (i *Installer) beginMaintenance(id string) (func(), error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.active[id] {
		return nil, status_error.RuntimeInstallationBusy
	}
	i.active[id] = true
	return func() { i.mu.Lock(); delete(i.active, id); i.mu.Unlock() }, nil
}

// InstallAt updates this exact selected installation; new installations retain the private store.
func (i *Installer) InstallAt(ctx context.Context, id, binary string, args []string, notify func(Progress)) (Result, error) {
	done, err := i.beginMaintenance(id)
	if err != nil {
		return Result{}, err
	}
	defer done()
	plan, err := i.inspect(ctx, id, binary, args)
	if err != nil {
		return Result{}, err
	}
	if !plan.view.CanInstall && !plan.view.CanUpdate {
		return Result{}, status_error.RuntimeInstallationUnsupported
	}
	if plan.manager != nil {
		return i.maintainPackage(ctx, id, *plan.manager, false, notify)
	}
	if plan.native != nil {
		return i.maintainNative(ctx, id, *plan.native, notify)
	}
	// A repair must verify and publish fresh bytes for this damaged selection,
	// even when another private installation already matches the release.
	return i.installManaged(ctx, id, notify, plan.view.Reason == "repairable")
}

// Uninstall validates every owned version before removal, preserving all account data.
func (i *Installer) Uninstall(ctx context.Context, id, binary string, args []string) error {
	done, err := i.beginMaintenance(id)
	if err != nil {
		return err
	}
	defer done()
	plan, err := i.inspect(ctx, id, binary, args)
	if err != nil {
		return err
	}
	if !plan.view.CanUninstall {
		return status_error.RuntimeInstallationUnsupported
	}
	if plan.manager != nil {
		_, err := i.maintainPackage(ctx, id, *plan.manager, true, nil)
		return err
	}
	if plan.native != nil {
		return i.uninstallNative(ctx, id, *plan.native)
	}
	if plan.pending != nil {
		return i.finishUninstall(ctx, id, *plan.pending)
	}
	if plan.managed == nil {
		return status_error.RuntimeInstallationUnsupported
	}
	parent := filepath.Join(i.root, id)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return failure("read managed uninstall versions", err)
	}
	journal := uninstallJournal{RuntimeID: id}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "version-") {
			continue
		}
		root := filepath.Join(parent, entry.Name())
		version, err := i.readManagedManifest(id, root)
		if err != nil {
			return err
		}
		if !repairableManagedPayload(version) {
			return failure("managed uninstall payload ownership", nil)
		}
		aside, err := os.MkdirTemp(parent, ".uninstall-")
		if err != nil {
			return failure("create uninstall staging", err)
		}
		if err := os.Remove(aside); err != nil {
			return failure("prepare uninstall staging", err)
		}
		journal.Entries = append(journal.Entries, uninstallEntry{Original: entry.Name(), Staged: filepath.Base(aside), Record: version.record})
	}
	data, err := json.Marshal(journal)
	if err != nil {
		return failure("encode uninstall ownership", err)
	}
	file, err := os.OpenFile(filepath.Join(parent, ".uninstall.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return failure("create uninstall ownership", err)
	}
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		_ = os.Remove(filepath.Join(parent, ".uninstall.json"))
		return failure("persist uninstall ownership", err)
	}
	return i.finishUninstall(ctx, id, journal)
}

func (i *Installer) readUninstallJournal(id string) (uninstallJournal, error) {
	var journal uninstallJournal
	if err := i.validateStore(id); err != nil {
		return journal, err
	}
	name := filepath.Join(i.root, id, ".uninstall.json")
	info, err := os.Lstat(name)
	if err != nil {
		return journal, err
	}
	if !info.Mode().IsRegular() || info.Size() > metadataLimit {
		return journal, failure("uninstall journal ownership", nil)
	}
	data, err := os.ReadFile(name)
	if err != nil || json.Unmarshal(data, &journal) != nil || journal.RuntimeID != id || len(journal.Entries) == 0 {
		return journal, failure("uninstall journal identity", err)
	}
	seen := make(map[string]bool)
	for _, entry := range journal.Entries {
		if !safeRelative(entry.Original) || !safeRelative(entry.Staged) || filepath.Base(entry.Original) != entry.Original || filepath.Base(entry.Staged) != entry.Staged || !strings.HasPrefix(entry.Original, "version-") || !strings.HasPrefix(entry.Staged, ".uninstall-") || seen[entry.Original] || seen[entry.Staged] || entry.Record.RuntimeID != id {
			return journal, failure("uninstall journal path ownership", nil)
		}
		seen[entry.Original], seen[entry.Staged] = true, true
	}
	return journal, nil
}

func (i *Installer) finishUninstall(ctx context.Context, id string, journal uninstallJournal) error {
	parent := filepath.Join(i.root, id)
	var renamed [][2]string
	hadPending := false
	rollback := func() {
		var rollbackErr error
		for index := len(renamed) - 1; index >= 0; index-- {
			rollbackErr = errors.Join(rollbackErr, os.Rename(renamed[index][1], renamed[index][0]))
		}
		if rollbackErr == nil && !hadPending {
			_ = os.Remove(filepath.Join(parent, ".uninstall.json"))
		}
	}
	for _, entry := range journal.Entries {
		if err := i.validateStore(id); err != nil {
			rollback()
			return err
		}
		if ctx.Err() != nil {
			rollback()
			return canceled(ctx)
		}
		original, staged := filepath.Join(parent, entry.Original), filepath.Join(parent, entry.Staged)
		if info, err := os.Lstat(staged); err == nil {
			hadPending = true
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return failure("uninstall staging ownership", nil)
			}
			if _, err := os.Lstat(original); !errors.Is(err, os.ErrNotExist) {
				return failure("uninstall original path was replaced", err)
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return failure("read uninstall staging", err)
		}
		if _, err := os.Lstat(original); errors.Is(err, os.ErrNotExist) {
			continue
		}
		version, err := i.readManagedManifest(id, original)
		if err != nil || !reflect.DeepEqual(version.record, entry.Record) || !repairableManagedPayload(version) {
			rollback()
			return failure("uninstall version changed", err)
		}
		if err := os.Rename(original, staged); err != nil {
			rollback()
			return failure("stage managed uninstall", err)
		}
		renamed = append(renamed, [2]string{original, staged})
	}
	for _, entry := range journal.Entries {
		if ctx.Err() != nil {
			return canceled(ctx)
		}
		if err := i.removeProgramDirectory(filepath.Join(parent, entry.Staged)); err != nil {
			return failure("remove managed program version", err)
		}
	}
	if err := os.Remove(filepath.Join(parent, ".uninstall.json")); err != nil {
		return failure("complete uninstall ownership", err)
	}
	return nil
}
