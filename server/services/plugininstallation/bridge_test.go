package plugininstallation

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"os"
	"path/filepath"
	"testing"
)

func TestBridgeEntrypointsRequireTrustedVersionBeforeProcessResolution(t *testing.T) {
	registry := commandbridge.NewRegistry(context.Background(), nil)
	t.Cleanup(registry.Close)
	service := New(Config{Bridges: registry})
	version := models.PluginVersion{Package: plugins.Package{ContractVersion: constants.PluginTargetContractVersion, Format: constants.PluginFormatPortable, Manifest: plugins.Manifest{Schema: constants.PluginManifestSchema}, Integration: plugins.Integration{Authentication: &plugins.Authentication{Strategy: constants.PluginAuthCommandStrategy, Protocol: constants.PluginAuthProtocol, Command: "./auth"}}}}
	if _, _, err := service.Bridge(context.Background(), version); !errors.Is(err, status_error.MCPConnectorTrustRequired) {
		t.Fatalf("direct bridge bypassed trust: %v", err)
	}
	if _, _, _, err := service.BridgeLease(context.Background(), version); !errors.Is(err, status_error.MCPConnectorTrustRequired) {
		t.Fatalf("leased bridge bypassed trust: %v", err)
	}
}

func TestBridgeLeaseDistinguishesPackageRootsInsideSameSnapshot(t *testing.T) {
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
	version := models.PluginVersion{PluginID: "p", Digest: digest, ContentPath: relative, TrustedAt: 1, ID: "v", Package: plugins.Package{ContractVersion: constants.PluginTargetContractVersion, Format: constants.PluginFormatPortable, Manifest: plugins.Manifest{Schema: constants.PluginManifestSchema}, SourcePath: "one", Integration: plugins.Integration{Authentication: &plugins.Authentication{Strategy: constants.PluginAuthCommandStrategy, Protocol: constants.PluginAuthProtocol, Command: "./auth"}}}}
	_, first, release, err := service.BridgeLease(context.Background(), version)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	version.Package.SourcePath = "two"
	if _, _, _, err := service.BridgeLease(context.Background(), version); err == nil {
		t.Fatal("native target changed beneath active lease")
	}
	release()
	_, second, nextRelease, err := service.BridgeLease(context.Background(), version)
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
	if _, _, err := service.Bridge(context.Background(), version); !errors.Is(err, status_error.PluginPackageUnsupported) {
		t.Fatalf("bridge accepted legacy provider: %v", err)
	}
	if _, _, _, err := service.BridgeLease(context.Background(), version); !errors.Is(err, status_error.PluginPackageUnsupported) {
		t.Fatalf("lease accepted legacy provider: %v", err)
	}
}

// cancellingBridgeStore 在两次并发事实读取之间模拟请求取消。
type cancellingBridgeStore struct {
	// PluginController 提供本用例不调用的其他持久化端口。
	controllers.PluginController
	// cancel 结束发起桥接准备的请求。
	cancel context.CancelFunc
}

// ConfigurationRevision 返回原修订并取消发起请求。
func (s *cancellingBridgeStore) ConfigurationRevision(context.Context, string) (int64, error) {
	s.cancel()
	return 0, nil
}

// AccountGeneration 仅在原请求仍有效时提供账号代次。
func (*cancellingBridgeStore) AccountGeneration(ctx context.Context, _ string) (int64, error) {
	if ctx.Err() != nil {
		return 0, status_error.OperationCanceled
	}
	return 0, nil
}

func TestBridgePreparationPropagatesRequestCancellationToAccountReads(t *testing.T) {
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
	relative, err := filepath.Rel(data, root)
	if err != nil {
		t.Fatal(err)
	}
	registry := commandbridge.NewRegistry(context.Background(), nil)
	t.Cleanup(registry.Close)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := New(Config{DataDirectory: data, Bridges: registry, Store: &cancellingBridgeStore{cancel: cancel}})
	version := models.PluginVersion{ID: "version", PluginID: "plugin", Digest: digest, ContentPath: relative, TrustedAt: 1, Package: plugins.Package{ContractVersion: constants.PluginTargetContractVersion, Format: constants.PluginFormatPortable, Manifest: plugins.Manifest{Schema: constants.PluginManifestSchema}, Integration: plugins.Integration{Authentication: &plugins.Authentication{Strategy: constants.PluginAuthCommandStrategy, Protocol: constants.PluginAuthProtocol, Command: "./auth"}}}}
	if _, _, err := service.Bridge(ctx, version); !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("cancelled request continued account preparation: %v", err)
	}
}
