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
	pkg := Package{Format: constants.PluginFormatPortable, Manifest: Manifest{Extensions: map[string]json.RawMessage{"com.ally": json.RawMessage(`{"bindings":[{"server":"local","target":"env","name":"TOKEN","value":"${TOKEN}"}]}`)}}, Servers: []Server{{Key: "local", Type: "stdio", Command: "node", Args: []string{"${PLUGIN_ROOT}/index.js", "${UNKNOWN}"}, Env: map[string]string{"DATA": "${PLUGIN_DATA}"}}}}
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
	if _, err := ResolveServer(Package{Format: constants.PluginFormatPortable}, server, root, data, nil); err == nil {
		t.Fatal("escaped persistent data root")
	}
}

func TestResolveServerValidatesRemoteAfterSharedValueExpansion(t *testing.T) {
	for _, format := range []string{constants.PluginFormatPortable, constants.PluginFormatClaude} {
		t.Run(format, func(t *testing.T) {
			pkg := Package{Format: format, Manifest: Manifest{Extensions: map[string]json.RawMessage{"com.ally": json.RawMessage(`{"bindings":[{"server":"remote","target":"header","name":"Authorization","value":"${TOKEN}"}]}`)}}}
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
