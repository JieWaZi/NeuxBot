package plugininstallation

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"os"
	"path/filepath"
	"testing"
)

func TestBridgeEntrypointsRequireTrustedVersionBeforeProcessResolution(t *testing.T) {
	service := New(Config{})
	if _, _, err := service.Bridge(models.PluginVersion{}); !errors.Is(err, status_error.MCPConnectorTrustRequired) {
		t.Fatalf("direct bridge bypassed trust: %v", err)
	}
	if _, _, _, err := service.BridgeLease(models.PluginVersion{}); !errors.Is(err, status_error.MCPConnectorTrustRequired) {
		t.Fatalf("leased bridge bypassed trust: %v", err)
	}
}

func TestBridgeLeaseDistinguishesNativeEntrypointsInsideSameSnapshot(t *testing.T) {
	data, source := t.TempDir(), t.TempDir()
	for _, target := range []string{"one", "two"} {
		root := filepath.Join(source, target)
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "auth"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	digest, err := plugins.ContentDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	root, err := plugins.CopyContent(filepath.Join(data, "plugins/content"), source, digest)
	if err != nil {
		t.Fatal(err)
	}
	relative, _ := filepath.Rel(data, root)
	registry := commandbridge.NewRegistry(context.Background(), nil)
	defer registry.Close()
	service := New(Config{DataDirectory: data, Bridges: registry})
	version := models.PluginVersion{PluginID: "p", Digest: digest, ContentPath: relative, TrustedAt: 1, Package: plugins.Package{SourcePath: "one", Integration: plugins.Integration{Authentication: &plugins.Authentication{Strategy: constants.PluginAuthCommandStrategy, Protocol: constants.PluginAuthProtocol, Command: "./auth"}}}}
	_, first, release, err := service.BridgeLease(version)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	version.Package.SourcePath = "two"
	if _, _, _, err := service.BridgeLease(version); err == nil {
		t.Fatal("native target changed beneath active lease")
	}
	release()
	_, second, nextRelease, err := service.BridgeLease(version)
	if err != nil {
		t.Fatal(err)
	}
	defer nextRelease()
	if first == second {
		t.Fatal("same snapshot reused another target entrypoint")
	}
}

func TestBridgeRejectsLegacyProviderBeforeCreatingAuthentication(t *testing.T) {
	data, source := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "auth"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	digest, err := plugins.ContentDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	root, err := plugins.CopyContent(filepath.Join(data, "plugins/content"), source, digest)
	if err != nil {
		t.Fatal(err)
	}
	relative, _ := filepath.Rel(data, root)
	registry := commandbridge.NewRegistry(context.Background(), nil)
	defer registry.Close()
	service := New(Config{DataDirectory: data, Bridges: registry})
	version := models.PluginVersion{PluginID: "legacy", Digest: digest, ContentPath: relative, TrustedAt: 1, Package: plugins.Package{Manifest: plugins.Manifest{Extensions: map[string]json.RawMessage{constants.PluginExtensionNamespace: json.RawMessage(`{"provider":"github"}`)}}, Integration: plugins.Integration{Authentication: &plugins.Authentication{Strategy: constants.PluginAuthCommandStrategy, Protocol: constants.PluginAuthProtocol, Command: "./auth"}}}}
	if _, _, err := service.Bridge(version); !errors.Is(err, status_error.PluginManifestInvalid) {
		t.Fatalf("bridge accepted legacy provider: %v", err)
	}
	if _, _, _, err := service.BridgeLease(version); !errors.Is(err, status_error.PluginManifestInvalid) {
		t.Fatalf("lease accepted legacy provider: %v", err)
	}
}
