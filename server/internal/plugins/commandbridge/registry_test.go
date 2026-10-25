package commandbridge

import (
	"context"
	"testing"
)

func TestRegistryLeasePinsDriverUntilRelease(t *testing.T) {
	registry := NewRegistry(context.Background(), nil)
	defer registry.Close()
	config := Config{DataDirectory: t.TempDir(), PluginID: "plugin"}
	auth, first, release, err := registry.Acquire("plugin", "v1", config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, _, err := registry.Get("plugin", "v2", config, nil); err == nil {
		t.Fatal("active pinned driver replaced")
	}
	sameAuth, sameDriver, err := registry.Get("plugin", "v1", config, nil)
	if err != nil || sameAuth != auth || sameDriver != first {
		t.Fatal("lease lost selected driver")
	}
	release()
	nextAuth, nextDriver, err := registry.Get("plugin", "v2", config, nil)
	if err != nil || nextAuth != auth || nextDriver == first {
		t.Fatal("idle version did not replace driver while preserving identity")
	}
}

func TestRegistrySummaryNeverCreatesOrSwitchesBridge(t *testing.T) {
	registry := NewRegistry(context.Background(), nil)
	defer registry.Close()
	if got := registry.CachedAccount("plugin", "missing"); got.CheckedAt != 0 {
		t.Fatal("missing bridge fabricated checked summary")
	}
	driver := testDriver(t, "ready")
	auth, _, err := registry.Get("plugin", "v1", driver.config, []string{"documents:read"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = auth.Status(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	release, err := auth.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if got := registry.CachedAccount("plugin", "v2"); got.CheckedAt != 0 || got.Available {
		t.Fatal("different version inherited old summary", got)
	}
	if got := registry.CachedAccount("plugin", "v1"); got.ID != "public-user" || !got.Available || got.CheckedAt == 0 {
		t.Fatal("reading another version changed active bridge", got)
	}
}
