package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestIntegrationListenerLifecycle(t *testing.T) {
	root := t.TempDir()
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := reservation.Addr().(*net.TCPAddr).Port
	_ = reservation.Close()
	data, _ := json.Marshal(map[string]any{"ip": "127.0.0.1", "port": port, "enabled": true})
	if err := os.WriteFile(filepath.Join(root, "integration-api.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	listener := NewListener(root, func(string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	})
	if err := listener.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	old := listener.GetAPIStatus()
	if !old.Available || old.Port != port {
		t.Fatal(old)
	}
	blocked, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer blocked.Close()
	if err := listener.SetAPIAddress(context.Background(), "127.0.0.1", blocked.Addr().(*net.TCPAddr).Port); err == nil {
		t.Fatal("port conflict accepted")
	}
	if current := listener.GetAPIStatus(); current != old {
		t.Fatal("failed switch lost working listener", current)
	}
	if err := listener.Configure(context.Background(), old.IP, old.Port, false, ""); err != nil {
		t.Fatal(err)
	}
	if listener.GetAPIStatus().Available || listener.Settings().Enabled {
		t.Fatal("disabled API remains available")
	}
	if err := listener.Configure(context.Background(), old.IP, old.Port, true, ""); err != nil {
		t.Fatal(err)
	}
	if !listener.GetAPIStatus().Available {
		t.Fatal("re-enable failed")
	}
	listener.Close()
	listener.Close()
}

func TestIntegrationDisableClosesListenerBeforeServeStarts(t *testing.T) {
	bound, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer bound.Close()
	listener := NewListener(t.TempDir(), func(string) http.Handler { return http.NotFoundHandler() })
	// 模拟已经登记监听套接字，但 Serve goroutine 尚未被调度的真实窗口。
	listener.ctx, listener.cancel = context.WithCancel(context.Background())
	listener.enabled = true
	listener.api = ListenerStatus{IP: "127.0.0.1", Port: bound.Addr().(*net.TCPAddr).Port, Available: true}
	listener.server = &http.Server{Handler: http.NotFoundHandler()}
	listener.listener = bound
	defer listener.Close()

	if err := listener.Configure(context.Background(), listener.api.IP, listener.api.Port, false, ""); err != nil {
		t.Fatal(err)
	}
	if err := bound.SetDeadline(time.Now()); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("disabled listener retained bound socket before Serve: %v", err)
	}
	if listener.GetAPIStatus().Available || listener.Settings().Enabled {
		t.Fatal("disabled API remains available")
	}
}

func integrationFreePort(t *testing.T) int {
	t.Helper()
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := reservation.Addr().(*net.TCPAddr).Port
	_ = reservation.Close()
	return port
}
func integrationSavedListener(t *testing.T, root string, port int, enabled bool) *Listener {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"ip": "127.0.0.1", "port": port, "enabled": enabled})
	if err := os.WriteFile(filepath.Join(root, "integration-api.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	listener := NewListener(root, func(string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	})
	if err := listener.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(listener.Close)
	return listener
}
func integrationProbeListener(t *testing.T, listener *Listener) {
	t.Helper()
	settings := listener.Settings()
	response, err := http.Get("http://" + net.JoinHostPort(settings.IP, strconv.Itoa(settings.Port)))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatal(response.StatusCode)
	}
}

func TestMCPDefaultsPersistenceAndIndependentListenerLifecycle(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	port := integrationFreePort(t)
	if NewListener(root, nil).Settings().MCPEnabled {
		t.Fatal("new MCP settings enabled by default")
	}
	listener := integrationSavedListener(t, root, port, false)
	if settings := listener.Settings(); settings.MCPEnabled || settings.Available || settings.Enabled {
		t.Fatal("old configuration unexpectedly opened MCP", settings)
	}
	if err := listener.SetMCPEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	if settings := listener.Settings(); !settings.MCPEnabled || !settings.Available || settings.Enabled || listener.GetAPIStatus().Available {
		t.Fatal("MCP-only listener advertised API availability", settings)
	}
	integrationProbeListener(t, listener)
	socket := listener.listener
	if err := listener.Configure(ctx, "127.0.0.1", port, false, "https://proxy.example.test/prefix"); err != nil {
		t.Fatal(err)
	}
	if settings := listener.Settings(); !settings.MCPEnabled || settings.Enabled || settings.PublicBaseURL != "https://proxy.example.test/prefix/api/v1" || listener.listener != socket {
		t.Fatal("API update lost MCP settings or replaced socket", settings)
	}
	if err := listener.Configure(ctx, "127.0.0.1", port, true, listener.Settings().PublicBaseURL); err != nil {
		t.Fatal(err)
	}
	if err := listener.SetMCPEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	if settings := listener.Settings(); settings.MCPEnabled || !settings.Enabled || !settings.Available || !listener.GetAPIStatus().Available || listener.listener != socket {
		t.Fatal("MCP disable disrupted API", settings)
	}
	if err := listener.Configure(ctx, "127.0.0.1", port, false, listener.Settings().PublicBaseURL); err != nil {
		t.Fatal(err)
	}
	if listener.Settings().Available {
		t.Fatal("both-disabled listener still bound")
	}
	if err := listener.SetMCPEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	listener.Close()
	restarted := NewListener(root, func(string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	})
	if err := restarted.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	settings := restarted.Settings()
	if !settings.MCPEnabled || settings.Enabled || !settings.Available || restarted.GetAPIStatus().Available || settings.PublicBaseURL != "https://proxy.example.test/prefix/api/v1" {
		t.Fatal("restart lost independent switches", settings)
	}
	integrationProbeListener(t, restarted)
	nextPort := integrationFreePort(t)
	if err := restarted.SetAPIAddress(ctx, "127.0.0.1", nextPort); err != nil {
		t.Fatal(err)
	}
	if settings = restarted.Settings(); !settings.MCPEnabled || settings.Enabled || settings.Port != nextPort || settings.PublicBaseURL != "https://proxy.example.test/prefix/api/v1" {
		t.Fatal("address update lost MCP or saved API fields", settings)
	}
	integrationProbeListener(t, restarted)
}

func TestMCPListenerEnableAndPersistenceFailureRollBack(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	blocked, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := blocked.Addr().(*net.TCPAddr).Port
	listener := integrationSavedListener(t, root, port, false)
	before := listener.Settings()
	path := filepath.Join(root, "integration-api.json")
	saved, _ := os.ReadFile(path)
	if err = listener.SetMCPEnabled(ctx, true); err == nil {
		t.Fatal("MCP enabled despite occupied port")
	}
	after, _ := os.ReadFile(path)
	if listener.Settings() != before || string(after) != string(saved) {
		t.Fatal("failed MCP enable changed saved or runtime settings", listener.Settings())
	}
	_ = blocked.Close()
	if err = listener.SetMCPEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	before = listener.Settings()
	socket := listener.listener
	conflict, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer conflict.Close()
	if err = listener.SetAPIAddress(ctx, "127.0.0.1", conflict.Addr().(*net.TCPAddr).Port); err == nil {
		t.Fatal("MCP-only address conflict accepted")
	}
	if listener.Settings() != before || listener.listener != socket {
		t.Fatal("address conflict disrupted MCP listener")
	}
	integrationProbeListener(t, listener)
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err = listener.SetMCPEnabled(ctx, false); err == nil {
		t.Fatal("MCP disable ignored settings replacement failure")
	}
	if listener.Settings() != before || listener.listener != socket {
		t.Fatal("failed save closed MCP listener", listener.Settings())
	}
	integrationProbeListener(t, listener)
}

func TestConcurrentAPIAndMCPUpdatesPreserveEachOther(t *testing.T) {
	ctx := context.Background()
	port := integrationFreePort(t)
	listener := integrationSavedListener(t, t.TempDir(), port, false)
	for i := 0; i < 5; i++ {
		if err := listener.SetMCPEnabled(ctx, false); err != nil {
			t.Fatal(err)
		}
		if err := listener.Configure(ctx, "127.0.0.1", port, false, ""); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		done := make(chan error, 2)
		go func() {
			<-start
			done <- listener.Configure(ctx, "127.0.0.1", port, true, "https://proxy.example.test/shared")
		}()
		go func() { <-start; done <- listener.SetMCPEnabled(ctx, true) }()
		close(start)
		for j := 0; j < 2; j++ {
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
		settings := listener.Settings()
		if !settings.Enabled || !settings.MCPEnabled || !settings.Available || settings.PublicBaseURL != "https://proxy.example.test/shared/api/v1" {
			t.Fatal("concurrent update lost independent configuration", settings)
		}
		var saved listenerSaved
		data, err := os.ReadFile(filepath.Join(listener.dataDirectory, "integration-api.json"))
		if err != nil || json.Unmarshal(data, &saved) != nil || !saved.Enabled || !saved.MCPEnabled || saved.PublicBaseURL != settings.PublicBaseURL {
			t.Fatal("persisted configuration lost concurrent update", string(data), err)
		}
	}
}

func TestIntegrationDefaultsDisabledWithoutSavedSettings(t *testing.T) {
	listener := NewListener(t.TempDir(), func(string) http.Handler {
		t.Fatal("default-disabled integrations attempted to start listening")
		return http.NotFoundHandler()
	})
	if settings := listener.Settings(); settings.Enabled || settings.MCPEnabled || settings.Available {
		t.Fatal("new integrations are not disabled", settings)
	}
	if err := listener.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if settings := listener.Settings(); settings.Enabled || settings.MCPEnabled || settings.Available || listener.GetAPIStatus().Available {
		t.Fatal("startup enabled integrations without saved settings", settings)
	}
}
