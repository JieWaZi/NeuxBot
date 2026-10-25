package plugininstallation

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestAllyCLIRejectsRemovedPluginOperationsWithoutInstallation(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(root, "plugins.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := controllers.NewPluginController(database.DB)
	registry := agentcapability.New(nil)
	defer registry.Close()
	grant, activate, revoke, err := registry.Prepare(agentcapability.GrantSpec{ExecutionID: "execution-one", Validate: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer revoke()
	activate()
	endpoint := httptest.NewServer(registry)
	defer endpoint.Close()
	connection, err := agentcapability.WriteConnection(root, "connection.json", agentcapability.Connection{ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: endpoint.URL + constants.AgentCLIPath, CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token})
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "allycli")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/allycli")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %s %v", output, err)
	}
	native := filepath.Join(root, "user-home", ".codex")
	if err := os.MkdirAll(native, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(native, "config.toml")
	if err := os.WriteFile(sentinel, []byte("user-config-unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"plugins.catalog.search", "plugins.catalog.get", "plugins.installations.install", "plugins.installations.get"} {
		t.Run(id, func(t *testing.T) {
			params := map[string]any{"query": "demo"}
			switch id {
			case "plugins.catalog.get":
				params = map[string]any{"key": "demo"}
			case "plugins.installations.install":
				params = map[string]any{"key": "demo", "trustConfirmed": true}
			case "plugins.installations.get":
				params = map[string]any{"pluginId": "demo"}
			}
			input, _ := json.Marshal(params)
			args := append(strings.Split(id, "."), "--input", "-")
			if id == "plugins.installations.install" {
				args = append(args, "--request-id", "stable-install")
			}
			command := exec.Command(binary, args...)
			command.Env = []string{constants.AgentCLIContextEnvironment + "=" + connection, "PATH=" + root, "HOME=" + filepath.Dir(native)}
			command.Stdin = bytes.NewReader(input)
			output, err := command.CombinedOutput()
			var response agentcapability.Response
			if json.Unmarshal(output, &response) != nil || err == nil || response.Error == nil || response.Error.Code != status_error.AgentInputInvalid.Key() {
				t.Fatalf("removed CLI operation was not rejected by closed command syntax: %s %v", output, err)
			}
			body, _ := json.Marshal(agentcapability.Request{Operation: id, Params: params, RequestID: "stable-install"})
			request, err := http.NewRequest(http.MethodPost, endpoint.URL+constants.AgentCLIPath+"/commands", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer "+grant.Token)
			raw, err := endpoint.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer raw.Body.Close()
			if err := json.NewDecoder(raw.Body).Decode(&response); err != nil || response.Error == nil || response.Error.Code != status_error.AgentOperationForbidden.Key() {
				t.Fatalf("removed HTTP operation was not forbidden: %+v %v", response, err)
			}
			_, _, _, err = registry.Prepare(agentcapability.GrantSpec{ExecutionID: "stale-operation", Validate: func(context.Context) error { return nil }, Bindings: []agentcapability.Binding{{Operation: id, Call: func(context.Context, agentcapability.Request) (agentcapability.Result, error) {
				return agentcapability.Result{}, nil
			}, Receipt: func(context.Context, string) (agentcapability.Result, error) { return agentcapability.Result{}, nil }}}})
			if err != status_error.AgentInputInvalid {
				t.Fatalf("removed operation was accepted in a stale binding: %v", err)
			}
		})
	}
	relations, err := store.Installations(context.Background(), "", "current-partner")
	if err != nil || len(relations) != 0 {
		t.Fatalf("rejected operation created installations: %+v %v", relations, err)
	}
	_, total, err := store.List(context.Background(), "", "", "", 0, 100, false)
	if err != nil || total != 0 {
		t.Fatalf("rejected operation prepared system resources: total=%d error=%v", total, err)
	}
	if content, _ := os.ReadFile(sentinel); string(content) != "user-config-unchanged" {
		t.Fatal("modified user global native config")
	}
}
