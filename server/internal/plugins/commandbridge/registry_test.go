package commandbridge

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
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

func TestIdentityLeaseTracksCurrentRoundWithoutPreparingDriver(t *testing.T) {
	registry := NewRegistry(context.Background(), nil)
	defer registry.Close()
	update, release, err := registry.AcquireIdentity("plugin", authorization.Usage{ChatID: "chat", ExecutionID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	entry := registry.entries["plugin"]
	if entry.driver != nil || entry.digest != "" {
		t.Fatal("chat identity lease prepared an authorization driver")
	}
	update(authorization.Usage{ChatID: "chat", ExecutionID: "second"})
	if uses := entry.auth.Usages(); len(uses) != 1 || uses[0].ExecutionID != "second" {
		t.Fatal("consumer retained the first execution usage", uses)
	}
	update()
	if len(entry.auth.Usages()) != 0 {
		t.Fatal("idle consumer remained an active execution usage")
	}
	if _, _, err := registry.Get("plugin", "version", Config{DataDirectory: t.TempDir()}, nil); err == nil {
		t.Fatal("management replaced an identity before the native consumer exited")
	}
	release()
	if _, _, err := registry.Get("plugin", "version", Config{DataDirectory: t.TempDir()}, nil); err != nil {
		t.Fatal("released identity blocked management", err)
	}
	update(authorization.Usage{ExecutionID: "after-close"})
	if len(entry.auth.Usages()) != 0 {
		t.Fatal("closed consumer restored an active usage")
	}
}

func TestRegistryClassifiesUsageAndAuthorizationChanges(t *testing.T) {
	for _, identityOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "configured_bridge", true: "identity_only"}[identityOnly], func(t *testing.T) {
			changes := []bool{}
			registry := NewRegistry(context.Background(), func(id string, usageOnly bool) {
				if id != "plugin" {
					t.Fatalf("change targeted %q", id)
				}
				changes = append(changes, usageOnly)
			})
			defer registry.Close()
			if !identityOnly {
				if _, _, err := registry.Get("plugin", "v1", Config{DataDirectory: t.TempDir()}, nil); err != nil {
					t.Fatal(err)
				}
			}
			update, release, err := registry.AcquireIdentity("plugin", authorization.Usage{ChatID: "chat", ExecutionID: "first"})
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			update(authorization.Usage{ChatID: "chat", ExecutionID: "second"})
			update()
			release()
			if len(changes) != 4 {
				t.Fatalf("usage change count = %d, want 4", len(changes))
			}
			for _, usageOnly := range changes {
				if !usageOnly {
					t.Fatal("usage invalidated plugin authorization resources")
				}
			}
			auth := registry.entries["plugin"].auth
			if err := auth.Cancel(auth.Snapshot().ID); err != nil {
				t.Fatal(err)
			}
			if len(changes) != 5 || changes[4] {
				t.Fatal("authorization session change was classified as usage", changes)
			}
		})
	}
}
