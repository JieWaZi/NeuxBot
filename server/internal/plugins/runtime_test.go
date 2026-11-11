package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func TestResolveServerKeepsPortableLiteralsAndUsesExplicitBindings(t *testing.T) {
	root := t.TempDir()
	data := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	data, _ = filepath.EvalSymlinks(data)
	pkg := Package{ContractVersion: constants.PluginTargetContractVersion, Format: constants.PluginFormatPortable, Manifest: Manifest{Schema: constants.PluginManifestSchema, Extensions: map[string]json.RawMessage{"com.neuxbot": json.RawMessage(`{"schemaVersion":1,"configuration":{"fields":[{"key":"TOKEN","secret":true,"required":true}],"bindings":[{"server":"local","target":"env","name":"TOKEN","value":"${TOKEN}"}]}}`)}}, Servers: []Server{{Key: "local", Type: "stdio", Command: "node", Args: []string{"${PLUGIN_ROOT}/index.js", "${UNKNOWN}"}, Env: map[string]string{"DATA": "${PLUGIN_DATA}"}}}}
	resolved, err := ResolveServer(pkg, pkg.Servers[0], root, data, map[string]string{"TOKEN": "credential", "UNKNOWN": "must-not-expand"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.WorkingDirectory != root || resolved.Server.Args[0] != root+"/index.js" || resolved.Server.Args[1] != "${UNKNOWN}" || resolved.Server.Env["TOKEN"] != "credential" || resolved.Server.Env["PLUGIN_DATA"] != data {
		t.Fatalf("wrong native server config: %+v", resolved)
	}
	id := ComponentID("plugin-a", "local")
	if len(id) != 32 || ComponentID("plugin-a", "local") != id || ComponentID("plugin-b", "local") == id {
		t.Fatal("component identity is not stable and scoped")
	}
}
func TestResolveServerRejectsDataDirectorySymlinkEscape(t *testing.T) {
	root := t.TempDir()
	data := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	data, _ = filepath.EvalSymlinks(data)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(data, "escaped")); err != nil {
		t.Fatal(err)
	}
	server := Server{Key: "local", Type: "stdio", Command: "node", Cwd: "${PLUGIN_DATA}/escaped"}
	if _, err := ResolveServer(standardTestPackage(), server, root, data, nil); err == nil {
		t.Fatal("escaped persistent data root")
	}
}

func TestResolveServerValidatesRemoteAfterSharedValueExpansion(t *testing.T) {
	for _, format := range []string{constants.PluginFormatPortable} {
		t.Run(format, func(t *testing.T) {
			pkg := Package{ContractVersion: constants.PluginTargetContractVersion, Format: format, Manifest: Manifest{Schema: constants.PluginManifestSchema, Extensions: map[string]json.RawMessage{"com.neuxbot": json.RawMessage(`{"schemaVersion":1,"configuration":{"fields":[{"key":"TOKEN","secret":true,"required":true}],"bindings":[{"server":"remote","target":"header","name":"Authorization","value":"${TOKEN}"}]}}`)}}}
			server := Server{Key: "remote", Type: "streamable-http", URL: "https://example.com/mcp"}
			pkg.Servers = []Server{server}
			if _, err := ResolveServer(pkg, server, t.TempDir(), t.TempDir(), map[string]string{"TOKEN": "valid"}); err != nil {
				t.Fatal("valid fixture rejected", err)
			}
			if _, err := ResolveServer(pkg, server, t.TempDir(), t.TempDir(), map[string]string{"TOKEN": "token\r\nInjected: value"}); err == nil {
				t.Fatal("expanded header contains CRLF but was admitted")
			}
		})
	}
}

// TestPreviewServerDoesNotCreatePersistentDirectories 防止技能预选创建插件数据或服务工作目录。
func TestPreviewServerDoesNotCreatePersistentDirectories(t *testing.T) {
	root, base := t.TempDir(), t.TempDir()
	base, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(base, "data", "plugin")
	pkg := standardTestPackage()
	input := Server{Type: "stdio", Command: "tool", Cwd: "${PLUGIN_DATA}/nested"}
	resolved, err := PreviewServer(pkg, input, root, data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.WorkingDirectory != filepath.Join(data, "nested") {
		t.Fatalf("planned cwd = %q", resolved.WorkingDirectory)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatalf("preview created data: %v", err)
	}
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	actual, err := ResolveServer(pkg, input, root, data, nil)
	if err != nil || actual.WorkingDirectory != resolved.WorkingDirectory {
		t.Fatalf("preview/acquisition mismatch: %+v %v", actual, err)
	}
}

// TestPreviewServerRejectsPersistentEscape 验证预选和实际取得均拒绝符号链接及父目录越界。
func TestPreviewServerRejectsPersistentEscape(t *testing.T) {
	root, data, outside := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(data, "escape")); err != nil {
		t.Skip(err)
	}
	for _, cwd := range []string{"${PLUGIN_DATA}/escape/new", "${PLUGIN_DATA}/../outside"} {
		for _, resolve := range []func(Package, Server, string, string, map[string]string) (ResolvedServer, error){PreviewServer, ResolveServer} {
			if _, err := resolve(standardTestPackage(), Server{Type: "stdio", Command: "tool", Cwd: cwd}, root, data, nil); err == nil {
				t.Fatalf("unsafe cwd accepted: %q", cwd)
			}
		}
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("escape wrote outside data: %v %v", entries, err)
	}
}
