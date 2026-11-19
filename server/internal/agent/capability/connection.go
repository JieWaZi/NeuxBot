package capability

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/google/uuid"
)

const (
	residentConnectionFilename = "connection.json"
	codexShellDirectory        = "codex-threads"
)

func codexShellConnectionPath(root, threadID string) (string, error) {
	id, err := uuid.Parse(threadID)
	if err != nil || id == uuid.Nil || id.String() != threadID {
		return "", status_error.AgentContextStorageError
	}
	return filepath.Join(root, codexShellDirectory, threadID, residentConnectionFilename), nil
}

// BindCodexShellConnection publishes another name for the admitted resident descriptor.
// Codex supplies its native thread ID after filtering the shell environment.
func (r *Registry) BindCodexShellConnection(path, executionID, threadID string) error {
	epochDirectory := filepath.Dir(filepath.Dir(path))
	root := filepath.Dir(epochDirectory)
	if !filepath.IsAbs(path) || filepath.Base(root) != constants.AgentCLIContextDirectory ||
		filepath.Base(epochDirectory) != r.epoch || filepath.Base(path) != residentConnectionFilename {
		return status_error.AgentContextStorageError
	}
	alias, pathErr := codexShellConnectionPath(root, threadID)
	r.mutex.Lock()
	defer r.mutex.Unlock()
	key := r.connections[path]
	current := r.leases[key]
	if r.closed || current == nil || current.executionID != executionID || current.ctx.Err() != nil {
		return status_error.AgentContextUnauthorized
	}
	if bound := r.codexShellAliases[key]; bound != "" && bound != alias {
		// A replacement native session must not leave the old thread pointing
		// at the resident descriptor that will authorize its next turn.
		if err := r.removeCodexShellConnectionLocked(key); err != nil {
			return status_error.AgentContextUnauthorized
		}
	}
	if pathErr != nil {
		return pathErr
	}
	source, sourceErr := os.Lstat(path)
	existing, lookupErr := os.Lstat(alias)
	if r.codexShellAliases[key] == alias && sourceErr == nil && lookupErr == nil && os.SameFile(source, existing) {
		return nil
	}
	var oldKey string
	if lookupErr == nil {
		previous, err := ReadConnection(alias)
		if err != nil {
			return status_error.AgentContextStorageError
		}
		if previous.CoreEpoch == r.epoch {
			oldKey = tokenKey(previous.Token)
			if r.codexShellAliases[oldKey] != alias || r.leases[oldKey] != nil {
				return status_error.AgentContextUnauthorized
			}
		}
	} else if !os.IsNotExist(lookupErr) {
		return status_error.AgentContextStorageError
	}
	connection, err := ReadConnection(path)
	if err != nil || connection.CoreEpoch != r.epoch || tokenKey(connection.Token) != key {
		return status_error.AgentContextStorageError
	}
	if err := prepareConnectionDirectory(filepath.Dir(alias)); err != nil {
		return err
	}
	if lookupErr == nil {
		// A previous-epoch locator cannot be live under the data-directory lock.
		if err := os.Remove(alias); err != nil {
			logx.Errorf("retire stale Codex shell connection failed: error=%v", err)
			return status_error.AgentContextStorageError
		}
		delete(r.codexShellAliases, oldKey)
	}
	if err := os.Link(path, alias); err != nil {
		logx.Errorf("link resident connection for Codex shell failed: error=%v", err)
		return status_error.AgentContextStorageError
	}
	r.codexShellAliases[key] = alias
	return nil
}

func (r *Registry) removeCodexShellConnectionLocked(key string) error {
	alias := r.codexShellAliases[key]
	if alias == "" {
		return nil
	}
	if err := os.Remove(alias); err != nil && !os.IsNotExist(err) {
		logx.Errorf("remove Codex shell connection failed: error=%v", err)
		return status_error.AgentContextStorageError
	}
	delete(r.codexShellAliases, key)
	directory := filepath.Dir(alias)
	_ = os.Remove(directory)
	_ = os.Remove(filepath.Dir(directory))
	return nil
}

// ReadCodexShellConnection locates a connection inside the bundled executable's
// data root. The native ID selects a file, and the file holds the Core credential.
func ReadCodexShellConnection(executable, threadID string) (Connection, error) {
	executable, err := filepath.EvalSymlinks(executable)
	if err != nil || !filepath.IsAbs(executable) {
		return Connection{}, status_error.AgentContextStorageError
	}
	scripts := filepath.Dir(executable)
	skill := filepath.Dir(scripts)
	bundle := filepath.Dir(skill)
	name := constants.AgentCLIBinaryName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	digest, err := hex.DecodeString(filepath.Base(skill))
	if err != nil || len(digest) != sha256.Size || filepath.Base(executable) != name ||
		filepath.Base(scripts) != constants.AgentCLISkillScriptsDirectory || filepath.Base(bundle) != constants.AgentCLISystemSkillsDirectory {
		return Connection{}, status_error.AgentContextStorageError
	}
	alias, err := codexShellConnectionPath(filepath.Join(filepath.Dir(bundle), constants.AgentCLIContextDirectory), threadID)
	if err != nil {
		return Connection{}, err
	}
	resolved, err := filepath.EvalSymlinks(alias)
	if err != nil || resolved != alias {
		return Connection{}, status_error.AgentContextStorageError
	}
	return ReadConnection(alias)
}

// CleanupRetiredConnections reclaims verified descriptors from earlier Core epochs.
func (r *Registry) CleanupRetiredConnections(directory string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	absolute, err := filepath.EvalSymlinks(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || absolute != filepath.Clean(directory) {
		return status_error.AgentContextStorageError
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return status_error.AgentContextStorageError
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return status_error.AgentContextStorageError
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || name == r.epoch {
			continue
		}
		switch {
		case isHexConnectionName(name, sha256.Size):
			// Older Core versions stored descriptors directly under the connection identity.
			err = r.retireConnectionDirectory(root, name, "")
		case name == codexShellDirectory, isHexConnectionName(name, 16):
			err = r.retireConnectionGroup(root, name)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func isHexConnectionName(name string, bytes int) bool {
	if len(name) != hex.EncodedLen(bytes) {
		return false
	}
	_, err := hex.DecodeString(name)
	return err == nil
}

func (r *Registry) retireConnectionGroup(root *os.Root, group string) error {
	children, err := fs.ReadDir(root.FS(), group)
	if err != nil {
		return status_error.AgentContextStorageError
	}
	epoch := group
	if group == codexShellDirectory {
		epoch = ""
	}
	for _, child := range children {
		if !child.IsDir() {
			continue
		}
		name := child.Name()
		if epoch == "" {
			if _, err := codexShellConnectionPath(root.Name(), name); err != nil {
				continue
			}
		} else if !isHexConnectionName(name, sha256.Size) {
			continue
		}
		if err := r.retireConnectionDirectory(root, path.Join(group, name), epoch); err != nil {
			return err
		}
	}
	return removeEmptyConnectionDirectory(root, group)
}

func (r *Registry) retireConnectionDirectory(root *os.Root, directory, epoch string) error {
	entries, err := fs.ReadDir(root.FS(), directory)
	if err != nil {
		return status_error.AgentContextStorageError
	}
	for _, entry := range entries {
		name := entry.Name()
		if name != residentConnectionFilename && !strings.HasPrefix(name, "connection-staging-") {
			continue
		}
		relative := filepath.FromSlash(path.Join(directory, name))
		descriptor, err := ReadConnection(filepath.Join(root.Name(), relative))
		if err != nil || descriptor.CoreEpoch == r.epoch || (epoch != "" && descriptor.CoreEpoch != epoch) {
			continue
		}
		if err := root.Remove(relative); err != nil && !os.IsNotExist(err) {
			logx.Errorf("remove retired agent context file failed: error=%v", err)
			return status_error.AgentContextStorageError
		}
	}
	return removeEmptyConnectionDirectory(root, directory)
}

func removeEmptyConnectionDirectory(root *os.Root, directory string) error {
	entries, err := fs.ReadDir(root.FS(), directory)
	if err == nil && len(entries) == 0 {
		err = root.Remove(filepath.FromSlash(directory))
	}
	if err != nil && !os.IsNotExist(err) {
		logx.Errorf("remove empty agent context directory failed: error=%v", err)
		return status_error.AgentContextStorageError
	}
	return nil
}
