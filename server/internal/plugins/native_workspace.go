package plugins

import (
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"os"
	"path/filepath"
)

// PrepareCodexWorkspace 发布本轮独占marketplace，来源必须位于marketplace自身根内。
func PrepareCodexWorkspace(workspace string, targets []NativeTarget, marketplace ...string) error {
	directory := filepath.Join(workspace, ".agents")
	if info, err := os.Lstat(directory); err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return status_error.PluginContentInvalid
	} else if err != nil && !os.IsNotExist(err) {
		return status_error.PluginStorageError
	}
	if os.MkdirAll(directory, 0700) != nil {
		return status_error.PluginStorageError
	}
	stage, err := os.MkdirTemp(directory, "native-plugins-")
	if err != nil {
		return status_error.PluginStorageError
	}
	defer os.RemoveAll(stage)
	entries := []map[string]any{}
	seen := map[string]bool{}
	for _, target := range targets {
		if !pluginNamePattern.MatchString(target.Name) || seen[target.Name] {
			return status_error.PluginVersionConflict
		}
		seen[target.Name] = true
		destination := filepath.Join(stage, "plugins", target.Name)
		if os.MkdirAll(destination, 0700) != nil {
			return status_error.PluginStorageError
		}
		if err := pluginArchive.CopyDirectory(target.Root, destination); err != nil {
			return err
		}
		if err := VerifyContent(destination, target.Digest); err != nil {
			return err
		}
		entries = append(entries, map[string]any{"name": target.Name, "source": map[string]string{"source": "local", "path": "./.agents/plugins/plugins/" + target.Name}, "policy": map[string]string{"installation": "AVAILABLE", "authentication": "ON_USE"}})
	}
	name := constants.PluginNativeMarketplace
	if len(marketplace) > 0 && marketplace[0] != "" {
		name = marketplace[0]
	}
	data, _ := json.Marshal(map[string]any{"name": name, "plugins": entries})
	if os.WriteFile(filepath.Join(stage, "marketplace.json"), data, 0600) != nil {
		return status_error.PluginStorageError
	}
	destination := filepath.Join(directory, "plugins")
	if os.RemoveAll(destination) != nil {
		return status_error.PluginStorageError
	}
	if os.Rename(stage, destination) != nil {
		return status_error.PluginStorageError
	}
	return nil
}
