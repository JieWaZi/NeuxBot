package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
)

// NativePlugin identifies one complete, immutable plugin attached to a native process.
type NativePlugin struct {
	// Provider identifies the adapter that understands this package.
	Provider string
	// Name is the verified native plugin identifier.
	Name string
	// Version is the package version used by native caches.
	Version string
	// Digest identifies all bytes in the plugin root.
	Digest string
	// Directory is the physical plugin root owned by the workspace view.
	Directory string
	// Marketplace identifies the Codex cache namespace for this exact plugin set.
	Marketplace string
	// MarketplaceRoot locates the secret-free Codex marketplace index.
	MarketplaceRoot string
}

// Physical plugin views can be shared by several sessions until the last native consumer exits.
var pluginViewFiles = struct {
	// Mutex protects only directory gate registration and lease counts.
	sync.Mutex
	// owners counts process-local leases on exact physical directories.
	owners map[string]int
	// gates isolates publication and removal to the exact physical directory.
	gates map[string]*pluginDirectoryGate
}{owners: map[string]int{}, gates: map[string]*pluginDirectoryGate{}}

type pluginDirectoryGate struct {
	// ready serializes filesystem operations for one physical directory.
	ready chan struct{}
	// users includes the holder and all callers waiting for this gate.
	users int
}

func pluginSegment(value string) bool {
	return value != "" && value != "." && filepath.IsLocal(value) && filepath.Base(value) == value && !strings.ContainsAny(value, "/\\:")
}

// AcquirePluginViews publishes an immutable set under its actual execution workspace.
func AcquirePluginViews(ctx context.Context, workspace Workspace, targets []plugins.NativeTarget) ([]NativePlugin, func(context.Context) error, error) {
	if len(targets) == 0 {
		return nil, nil, nil
	}
	if !workspace.Managed || !filepath.IsAbs(workspace.Path) {
		return nil, nil, status_error.PluginForbidden
	}
	cli := targets[0].Runtime
	if !pluginSegment(cli) {
		return nil, nil, status_error.PluginContentInvalid
	}
	identity := [][]string{}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, target := range targets {
		if target.Runtime != cli || !pluginSegment(target.ID) || !pluginSegment(target.Name) || !pluginSegment(target.Version) ||
			ids[strings.ToLower(target.ID)] || names[strings.ToLower(target.Name)] {
			return nil, nil, status_error.PluginVersionConflict
		}
		ids[strings.ToLower(target.ID)], names[strings.ToLower(target.Name)] = true, true
		identity = append(identity, []string{target.ID, target.Name, target.Version, target.SourceDigest, target.Digest, target.Profile})
	}
	body, _ := json.Marshal(identity)
	sum := sha256.Sum256(body)
	set := hex.EncodeToString(sum[:])
	market := constants.PluginNativeMarketplace + "-" + set[:32]
	relative := filepath.Join(".neuxbot", "plugin-views", set, cli)
	dirs := map[string]plugins.NativeTarget{}
	entries := []map[string]any{}
	for _, target := range targets {
		dirs[target.ID] = target
		entries = append(entries, map[string]any{"name": target.Name, "source": map[string]string{"source": "local", "path": "./" + target.ID}, "policy": map[string]string{"installation": "AVAILABLE", "authentication": "ON_USE"}})
	}
	files := map[string][]byte{}
	if cli == constants.RuntimeIDCodex {
		index, _ := json.Marshal(map[string]any{"name": market, "plugins": entries})
		files[filepath.Join(".agents", "plugins", "marketplace.json")] = index
	}
	dir, release, err := acquirePluginDirectory(ctx, workspace.Path, relative, dirs, files)
	if err != nil {
		return nil, nil, err
	}
	result := make([]NativePlugin, 0, len(targets))
	for _, target := range targets {
		item := NativePlugin{Provider: cli, Directory: filepath.Join(dir, target.ID), Name: target.Name, Version: target.Version, Digest: target.Digest}
		if cli == constants.RuntimeIDCodex {
			item.Marketplace = market
			item.MarketplaceRoot = dir
		}
		result = append(result, item)
	}
	return result, release, nil
}

// prepareCodexPluginCache attaches a private cache to a confirmed managed account without editing its settings.
func prepareCodexPluginCache(ctx context.Context, account AccountRequest, lease AccountLease, targets []NativePlugin) (map[string]json.RawMessage, func(context.Context) error, error) {
	if len(targets) == 0 {
		return nil, nil, nil
	}
	if account.AccountID == "" || account.AccountID == SystemAccountID || !filepath.IsAbs(lease.ConfigHome) || lease.ConfigHome != account.ConfigHome {
		return nil, nil, status_error.AIAccountConfigurationInvalid
	}
	market, source := targets[0].Marketplace, targets[0].MarketplaceRoot
	if !strings.HasPrefix(market, constants.PluginNativeMarketplace+"-") || !pluginSegment(market) || !filepath.IsAbs(source) {
		return nil, nil, status_error.PluginContentInvalid
	}
	enabled := map[string]map[string]bool{}
	dirs := map[string]plugins.NativeTarget{}
	for _, target := range targets {
		if target.Provider != constants.RuntimeIDCodex || target.Marketplace != market || target.MarketplaceRoot != source || !pluginSegment(target.Name) || !pluginSegment(target.Version) {
			return nil, nil, status_error.PluginContentInvalid
		}
		dirs[filepath.Join(target.Name, target.Version)] = plugins.NativeTarget{Root: target.Directory, Digest: target.Digest}
		enabled[target.Name+"@"+market] = map[string]bool{"enabled": true}
	}
	_, release, err := acquirePluginDirectory(ctx, lease.ConfigHome, filepath.Join("plugins", "cache", market), dirs, nil)
	if err != nil {
		return nil, nil, err
	}
	pluginConfig, _ := json.Marshal(enabled)
	sourceConfig, _ := json.Marshal(source)
	return map[string]json.RawMessage{"features.plugins": json.RawMessage("true"), "plugins": pluginConfig, "marketplaces." + market + ".source_type": json.RawMessage(`"local"`), "marketplaces." + market + ".source": sourceConfig}, release, nil
}

// acquirePluginDirectory uses an anchored private staging directory and never replaces an active generation.
func acquirePluginDirectory(ctx context.Context, parent, relative string, dirs map[string]plugins.NativeTarget, files map[string][]byte) (string, func(context.Context) error, error) {
	canonical, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", nil, status_error.PluginStorageError
	}
	owner, err := os.OpenRoot(canonical)
	if err != nil {
		return "", nil, status_error.PluginStorageError
	}
	defer owner.Close()
	unlock, err := lockPluginDirectory(ctx, filepath.Join(canonical, relative))
	if err != nil {
		return "", nil, err
	}
	defer unlock()
	parentRel := filepath.Dir(relative)
	current := ""
	for _, part := range strings.Split(parentRel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		if err := owner.Mkdir(current, 0700); err != nil && !os.IsExist(err) {
			return "", nil, status_error.PluginStorageError
		}
		info, err := owner.Lstat(current)
		if err != nil || !info.IsDir() {
			return "", nil, status_error.PluginContentInvalid
		}
	}
	parentRoot, err := owner.OpenRoot(parentRel)
	if err != nil {
		return "", nil, status_error.PluginStorageError
	}
	defer parentRoot.Close()
	name := filepath.Base(relative)
	destination := filepath.Join(canonical, relative)
	ownershipKey := normalizedPathForComparison(destination)
	verify := func() error {
		info, err := parentRoot.Lstat(name)
		if err != nil || !info.IsDir() {
			return status_error.PluginContentInvalid
		}
		for sub, target := range dirs {
			path := filepath.Join(destination, sub)
			actual, err := filepath.EvalSymlinks(path)
			if err != nil || actual != path {
				return status_error.PluginContentInvalid
			}
			if err := plugins.VerifyContent(actual, target.Digest); err != nil {
				return err
			}
		}
		for file, expected := range files {
			actual, err := parentRoot.ReadFile(filepath.Join(name, file))
			if err != nil || string(actual) != string(expected) {
				return status_error.PluginVersionConflict
			}
		}
		return nil
	}
	if _, err := parentRoot.Lstat(name); os.IsNotExist(err) {
		stage := ".staging-" + rand.Text()
		if err := parentRoot.Mkdir(stage, 0700); err != nil {
			return "", nil, status_error.PluginStorageError
		}
		defer parentRoot.RemoveAll(stage)
		temp, err := parentRoot.OpenRoot(stage)
		if err != nil {
			return "", nil, status_error.PluginStorageError
		}
		defer temp.Close()
		for sub, target := range dirs {
			if err := copyPluginRoot(temp, sub, target.Root); err != nil {
				return "", nil, err
			}
			if err := plugins.VerifyContent(filepath.Join(temp.Name(), sub), target.Digest); err != nil {
				return "", nil, err
			}
		}
		for file, data := range files {
			if err := temp.MkdirAll(filepath.Dir(file), 0700); err != nil {
				return "", nil, status_error.PluginStorageError
			}
			if err := temp.WriteFile(file, data, 0600); err != nil {
				return "", nil, status_error.PluginStorageError
			}
		}
		if err := parentRoot.Rename(stage, name); err != nil {
			return "", nil, status_error.PluginStorageError
		}
	}
	if err := verify(); err != nil {
		return "", nil, err
	}
	// Keep a directory handle for cleanup: a renamed workspace path must not redirect removal.
	published, err := parentRoot.Lstat(name)
	if err != nil {
		return "", nil, status_error.PluginStorageError
	}
	held, err := parentRoot.OpenRoot(".")
	if err != nil {
		return "", nil, status_error.PluginStorageError
	}
	pluginViewFiles.Lock()
	pluginViewFiles.owners[ownershipKey]++
	pluginViewFiles.Unlock()
	released := false
	return destination, func(ctx context.Context) error {
		unlock, err := lockPluginDirectory(ctx, destination)
		if err != nil {
			return err
		}
		defer unlock()
		if released {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		pluginViewFiles.Lock()
		owners := pluginViewFiles.owners[ownershipKey]
		pluginViewFiles.Unlock()
		if owners == 1 {
			current, err := held.Lstat(name)
			if err != nil && !os.IsNotExist(err) {
				return status_error.PluginStorageError
			}
			if current != nil && os.SameFile(current, published) {
				if err := held.RemoveAll(name); err != nil {
					return status_error.PluginStorageError
				}
			}
		}
		pluginViewFiles.Lock()
		if owners == 1 {
			delete(pluginViewFiles.owners, ownershipKey)
		} else {
			pluginViewFiles.owners[ownershipKey]--
		}
		pluginViewFiles.Unlock()
		released = true
		return held.Close()
	}, nil
}

func lockPluginDirectory(ctx context.Context, path string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path = normalizedPathForComparison(path)
	pluginViewFiles.Lock()
	gate := pluginViewFiles.gates[path]
	if gate == nil {
		gate = &pluginDirectoryGate{ready: make(chan struct{}, 1)}
		pluginViewFiles.gates[path] = gate
	}
	gate.users++
	pluginViewFiles.Unlock()
	forget := func() {
		pluginViewFiles.Lock()
		gate.users--
		if gate.users == 0 {
			delete(pluginViewFiles.gates, path)
		}
		pluginViewFiles.Unlock()
	}
	select {
	case gate.ready <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-gate.ready
			forget()
			return nil, err
		}
		return func() { <-gate.ready; forget() }, nil
	case <-ctx.Done():
		forget()
		return nil, ctx.Err()
	}
}

// copyPluginRoot preserves the admitted tree, including executable files and internal links.
func copyPluginRoot(stage *os.Root, sub, source string) error {
	root, err := os.OpenRoot(source)
	if err != nil {
		return status_error.PluginStorageError
	}
	defer root.Close()
	return fs.WalkDir(root.FS(), ".", func(name string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return status_error.PluginStorageError
		}
		if name == ".git" && item.IsDir() {
			return fs.SkipDir
		}
		dest := filepath.Join(sub, filepath.FromSlash(name))
		switch {
		case item.IsDir():
			err = stage.MkdirAll(dest, 0700)
		case item.Type()&os.ModeSymlink != 0:
			var target string
			target, err = root.Readlink(name)
			if err == nil {
				err = stage.Symlink(target, dest)
			}
		case item.Type().IsRegular():
			info, infoErr := item.Info()
			if infoErr != nil {
				return status_error.PluginStorageError
			}
			data, readErr := root.ReadFile(name)
			if readErr != nil {
				return status_error.PluginStorageError
			}
			err = stage.WriteFile(dest, data, 0600|(info.Mode().Perm()&0111))
		default:
			return status_error.PluginContentInvalid
		}
		if err != nil {
			return status_error.PluginStorageError
		}
		return nil
	})
}
