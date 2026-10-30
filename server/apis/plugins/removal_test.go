package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/gin-gonic/gin"
)

type removalAuthFixture struct {
	// auth 是使用真实授权管理器与替换协议边界的共享身份。
	auth *authorization.Authorizer
	// available 模拟官方本机授权事实。
	available atomic.Bool
	// logouts 记录成功的官方退出次数。
	logouts atomic.Int64
	// cancelled 在设备进程收到真实取消后关闭。
	cancelled chan struct{}
	// allowExit 控制已取消设备进程的实际退出。
	allowExit chan struct{}
	// release 保证设备退出门闩只关闭一次。
	release sync.Once
	// logoutError 是退出边界的固定失败场景。
	logoutError error
}

// removalDriver 只替换外部协议，授权管理器、版本绑定和引用清理使用真实实现。
type removalDriver struct {
	// fixture 是受控的外部账号状态。
	fixture *removalAuthFixture
}

func (d removalDriver) Status(context.Context, bool) (authorization.Account, error) {
	return authorization.Account{Configured: true, Available: d.fixture.available.Load(), ID: "fixture-user", Scopes: []string{"read"}}, nil
}
func (d removalDriver) Logout(context.Context) error {
	if d.fixture.logoutError != nil {
		return d.fixture.logoutError
	}
	d.fixture.logouts.Add(1)
	d.fixture.available.Store(false)
	return nil
}
func (d removalDriver) Login(ctx context.Context, _ []string, report func(authorization.Device) error) error {
	if err := report(authorization.Device{VerificationURL: "https://example.com/device", ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}); err != nil {
		return err
	}
	<-ctx.Done()
	close(d.fixture.cancelled)
	<-d.fixture.allowExit
	return ctx.Err()
}
func removalAuth(t *testing.T, logoutError error) *removalAuthFixture {
	t.Helper()
	f := &removalAuthFixture{cancelled: make(chan struct{}), allowExit: make(chan struct{}), logoutError: logoutError}
	f.available.Store(true)
	previous := global.PluginBridges
	global.PluginBridges = commandbridge.NewRegistry(context.Background(), nil)
	t.Cleanup(func() {
		f.release.Do(func() { close(f.allowExit) })
		global.PluginBridges.Close()
		global.PluginBridges = previous
	})
	return f
}

func removalPlugin(t *testing.T, ctx *gin.Context, name string, fixture *removalAuthFixture) PluginDetailResponse {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"`+name+`","version":"1.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".neuxally"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".neuxally/integration.json"), []byte(`{"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"./auth","scopes":["read"]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "auth"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	stage, err := packageplugin.Stage(stagedRoot(), root)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := prepareCatalogFixture(t, ctx, stage, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	version, err := global.PluginControllerManager.Version(ctx, detail.ResolvedVersionID)
	if err != nil {
		t.Fatal(err)
	}
	fixture.auth, _, err = installationService().Bridge(version)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.auth.ReplaceDriver(removalDriver{fixture: fixture}); err != nil {
		t.Fatal(err)
	}
	return detail
}

func installRemovalPlugin(t *testing.T, ctx *gin.Context, detail PluginDetailResponse, partners ...string) {
	t.Helper()
	value, err := (&InstallPlugin{PluginID: detail.Plugin.ID, Body: InstallPluginRequest{VersionID: detail.ResolvedVersionID, WakerIDs: partners}}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range value.(InstallPluginResponse).Results {
		if !result.Installed {
			t.Fatalf("fixture installation failed: %+v", result)
		}
	}
}

func TestLastPluginUninstallClearsAuthorizationAndRequiresLoginAgain(t *testing.T) {
	ctx := pluginAPIContext(t)
	f := removalAuth(t, nil)
	detail := removalPlugin(t, ctx, "office", f)
	installRemovalPlugin(t, ctx, detail, "w1")
	if _, err := (&UninstallWakerPlugin{WakerID: "w1", PluginID: detail.Plugin.ID}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	if f.available.Load() || f.logouts.Load() != 1 {
		t.Fatal("last uninstall retained official authorization")
	}
	if _, err := (&InstallPlugin{PluginID: detail.Plugin.ID, Body: InstallPluginRequest{VersionID: detail.ResolvedVersionID, WakerIDs: []string{"w1"}}}).Output(ctx); !errors.Is(err, status_error.PluginNotReady) {
		t.Fatalf("reinstallation skipped authorization: %v", err)
	}
}

func TestProviderAuthorizationSurvivesOtherPartnersAndActiveWorkBlocksLastUninstall(t *testing.T) {
	ctx := pluginAPIContext(t)
	f := removalAuth(t, nil)
	other := removalPlugin(t, ctx, "office", f)
	installRemovalPlugin(t, ctx, other, "w1", "w2")
	if err := global.PluginControllerManager.SetEnabled(ctx, "w2", other.Plugin.ID, false); err != nil {
		t.Fatal(err)
	}
	release, err := f.auth.Acquire(authorization.Usage{WakerID: "w2", ExecutionID: "running"})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := (&UninstallWakerPlugin{WakerID: "w1", PluginID: other.Plugin.ID}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	if f.logouts.Load() != 0 || !f.available.Load() {
		t.Fatal("another plugin or disabled installation lost shared authorization")
	}
	if _, err := (&UninstallWakerPlugin{WakerID: "w2", PluginID: other.Plugin.ID}).Output(ctx); !errors.Is(err, status_error.PluginBusy) {
		t.Fatalf("active work did not protect last authorization: %v", err)
	}
	relations, err := global.PluginControllerManager.Installations(ctx, other.Plugin.ID, "w2")
	if err != nil || len(relations) != 1 || f.logouts.Load() != 0 {
		t.Fatalf("busy uninstall changed facts: %+v %v", relations, err)
	}
	release()
	if _, err := (&UninstallWakerPlugin{WakerID: "w2", PluginID: other.Plugin.ID}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	if f.logouts.Load() != 1 {
		t.Fatal("last provider installation was not cleared")
	}
}

func TestDeletingOrphanProviderCancelsAndWaitsForDeviceBeforeClearing(t *testing.T) {
	ctx := pluginAPIContext(t)
	f := removalAuth(t, nil)
	detail := removalPlugin(t, ctx, "orphan-office", f)
	if _, err := f.auth.Login(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := (&DeletePlugin{PluginID: detail.Plugin.ID, Body: DeletePluginRequest{Confirmed: true}}).Output(ctx)
		done <- err
	}()
	select {
	case <-f.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("deletion did not cancel pending device authorization")
	}
	if release, err := f.auth.Acquire(); !errors.Is(err, status_error.PluginBusy) {
		if release != nil {
			release()
		}
		t.Fatalf("new execution entered during cleanup: %v", err)
	}
	if _, err := f.auth.Login(context.Background()); !errors.Is(err, status_error.PluginBusy) {
		t.Fatalf("new login entered during cleanup: %v", err)
	}
	if f.logouts.Load() != 0 {
		t.Fatal("official identity cleared before cancelled process exited")
	}
	if _, err := global.PluginControllerManager.Get(ctx, detail.Plugin.ID); err != nil {
		t.Fatal("resource removed before authorization cleanup", err)
	}
	f.release.Do(func() { close(f.allowExit) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("deletion did not finish after device exit")
	}
	if f.available.Load() || f.logouts.Load() != 1 || f.auth.Snapshot().VerificationURL != "" {
		t.Fatal("orphan authorization was not cleared")
	}
	if _, err := global.PluginControllerManager.Get(ctx, detail.Plugin.ID); !errors.Is(err, status_error.PluginNotFound) {
		t.Fatalf("resource retained after cleanup: %v", err)
	}
}

func TestFailedAuthorizationCleanupRetainsInstallation(t *testing.T) {
	ctx := pluginAPIContext(t)
	f := removalAuth(t, status_error.PluginStorageError)
	detail := removalPlugin(t, ctx, "retry-office", f)
	installRemovalPlugin(t, ctx, detail, "w1")
	if _, err := (&UninstallWakerPlugin{WakerID: "w1", PluginID: detail.Plugin.ID}).Output(ctx); !errors.Is(err, status_error.PluginStorageError) {
		t.Fatalf("cleanup failure hidden: %v", err)
	}
	relations, err := global.PluginControllerManager.Installations(ctx, detail.Plugin.ID, "w1")
	if err != nil || len(relations) != 1 || !f.available.Load() {
		t.Fatalf("failed cleanup deleted installation: %+v %v", relations, err)
	}
}

func TestDeletePluginChecksConfirmationAndActiveLeaseBeforeLogout(t *testing.T) {
	ctx := pluginAPIContext(t)
	f := removalAuth(t, nil)
	detail := removalPlugin(t, ctx, "delete-office", f)
	installRemovalPlugin(t, ctx, detail, "w1")
	// 无效历史版本排在最新位置时，清理仍应使用实际可用桥接，不能被旧声明阻塞。
	if _, err := global.PluginControllerManager.PutVersion(ctx, models.PluginVersion{ID: "unsupported-history", PluginID: detail.Plugin.ID, Version: "2", Digest: strings.Repeat("b", 64), CreatedAt: time.Now().Add(time.Minute).UnixMilli(), Package: packageplugin.Package{Manifest: packageplugin.Manifest{Extensions: map[string]json.RawMessage{constants.PluginExtensionNamespace: json.RawMessage(`{"provider":"github"}`)}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := (&DeletePlugin{PluginID: detail.Plugin.ID, Body: DeletePluginRequest{Confirmed: true}}).Output(ctx); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatalf("stale confirmation accepted: %v", err)
	}
	if f.logouts.Load() != 0 {
		t.Fatal("stale confirmation cleared identity")
	}
	release, err := f.auth.Acquire(authorization.Usage{WakerID: "w1", ExecutionID: "running"})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	body := DeletePluginRequest{Confirmed: true, Installations: []VersionSwitch{{WakerID: "w1", ExpectedVersionID: detail.ResolvedVersionID}}}
	if _, err := (&DeletePlugin{PluginID: detail.Plugin.ID, Body: body}).Output(ctx); !errors.Is(err, status_error.PluginBusy) {
		t.Fatalf("active work did not protect deletion: %v", err)
	}
	if _, err := global.PluginControllerManager.Get(ctx, detail.Plugin.ID); err != nil || f.logouts.Load() != 0 {
		t.Fatalf("busy deletion changed facts: %v", err)
	}
	release()
	if _, err := (&DeletePlugin{PluginID: detail.Plugin.ID, Body: body}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	if f.available.Load() || f.logouts.Load() != 1 {
		t.Fatal("deletion retained shared identity")
	}
}

// 已持久化的旧声明仍可移除，删除不启动已经不受支持的授权入口。
func TestLegacyProviderResourcesCanBeUninstalledAndDeleted(t *testing.T) {
	for _, withBridge := range []bool{false, true} {
		t.Run(fmt.Sprint(withBridge), func(t *testing.T) {
			ctx := pluginAPIContext(t)
			store := global.PluginControllerManager
			if err := store.Create(ctx, models.Plugin{ID: "legacy", Name: "legacy", SourceKey: "legacy"}); err != nil {
				t.Fatal(err)
			}
			pkg := packageplugin.Package{Manifest: packageplugin.Manifest{Extensions: map[string]json.RawMessage{constants.PluginExtensionNamespace: json.RawMessage(`{"provider":"github"}`)}}}
			if withBridge {
				pkg.Integration.Authentication = &packageplugin.Authentication{Strategy: constants.PluginAuthCommandStrategy, Protocol: constants.PluginAuthProtocol, Command: "./unsupported-entry"}
			}
			if _, err := store.PutVersion(ctx, models.PluginVersion{ID: "legacy-v1", PluginID: "legacy", Version: "1", Digest: strings.Repeat("a", 64), Package: pkg, TrustedAt: 1}); err != nil {
				t.Fatal(err)
			}
			if err := store.Install(ctx, models.WakerPlugin{PluginID: "legacy", VersionID: "legacy-v1", WakerID: "w1", Enabled: true}); err != nil {
				t.Fatal(err)
			}
			listed, err := listPluginResources(ctx, "", "", 1, 50, false)
			if err != nil || len(listed.Resources) != 1 || listed.Resources[0].Ready || !slices.Contains(listed.Resources[0].Blockers, status_error.PluginManifestInvalid.Key()) {
				t.Fatalf("legacy resource cannot be inspected for removal: %+v %v", listed, err)
			}
			if _, err := (&GetPluginAuthorization{PluginID: "legacy"}).Output(ctx); !errors.Is(err, status_error.PluginManifestInvalid) {
				t.Fatalf("legacy auth was accepted: %v", err)
			}
			if _, err := (&UninstallWakerPlugin{PluginID: "legacy", WakerID: "w1"}).Output(ctx); err != nil {
				t.Fatalf("legacy resource cannot be uninstalled: %v", err)
			}
			if _, err := (&DeletePlugin{PluginID: "legacy", Body: DeletePluginRequest{Confirmed: true}}).Output(ctx); err != nil {
				t.Fatalf("legacy resource cannot be deleted: %v", err)
			}
			if _, err := store.Get(ctx, "legacy"); !errors.Is(err, status_error.PluginNotFound) {
				t.Fatalf("legacy resource retained: %v", err)
			}
		})
	}
}

func TestNonLastUninstallDoesNotPrepareBridgeForColdIdentityConsumer(t *testing.T) {
	ctx := pluginAPIContext(t)
	fixture := removalAuth(t, nil)
	detail := removalPlugin(t, ctx, "cold-consumer", fixture)
	installRemovalPlugin(t, ctx, detail, "w1", "w2")
	global.PluginBridges.Close()
	global.PluginBridges = commandbridge.NewRegistry(context.Background(), nil)
	_, release, err := global.PluginBridges.AcquireIdentity(detail.Plugin.ID, authorization.Usage{WakerID: "w2", ExecutionID: "active-cold-round"})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := (&UninstallWakerPlugin{WakerID: "w1", PluginID: detail.Plugin.ID}).Output(ctx); err != nil {
		t.Fatal("non-last uninstall attempted to replace the running identity driver", err)
	}
	retained, err := global.PluginControllerManager.Installations(ctx, detail.Plugin.ID, "w2")
	if err != nil || len(retained) != 1 {
		t.Fatal("remaining consumer lost its installation", err)
	}
}

func TestUnrelatedInstallationDoesNotBypassLastPluginCleanup(t *testing.T) {
	for _, operation := range []string{"uninstall", "delete"} {
		for _, active := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/active=%t", operation, active), func(t *testing.T) {
				ctx := pluginAPIContext(t)
				fixture := removalAuth(t, nil)
				target := removalPlugin(t, ctx, "cleanup-target", fixture)
				installRemovalPlugin(t, ctx, target, "w1")
				unrelatedFixture := &removalAuthFixture{}
				unrelatedFixture.available.Store(true)
				unrelated := removalPlugin(t, ctx, "unrelated-plugin", unrelatedFixture)
				installRemovalPlugin(t, ctx, unrelated, "w2")
				release := func() {}
				if active {
					var err error
					release, err = fixture.auth.Acquire(authorization.Usage{WakerID: "w1", ExecutionID: "target-running"})
					if err != nil {
						t.Fatal(err)
					}
				}
				defer release()
				var err error
				if operation == "uninstall" {
					_, err = (&UninstallWakerPlugin{WakerID: "w1", PluginID: target.Plugin.ID}).Output(ctx)
				} else {
					_, err = (&DeletePlugin{PluginID: target.Plugin.ID, Body: DeletePluginRequest{Confirmed: true, Installations: []VersionSwitch{{WakerID: "w1", ExpectedVersionID: target.ResolvedVersionID}}}}).Output(ctx)
				}
				if active {
					if !errors.Is(err, status_error.PluginBusy) {
						t.Fatal("unrelated installation bypassed the target's active identity", err)
					}
					retained, readErr := global.PluginControllerManager.Installations(ctx, target.Plugin.ID, "w1")
					if readErr != nil || len(retained) != 1 || fixture.logouts.Load() != 0 {
						t.Fatal("busy cleanup changed target installation", readErr)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					if fixture.logouts.Load() != 1 || fixture.available.Load() {
						t.Fatal("unrelated installation bypassed the target's final logout")
					}
				}
				retained, readErr := global.PluginControllerManager.Installations(ctx, unrelated.Plugin.ID, "w2")
				if readErr != nil || len(retained) != 1 || unrelatedFixture.logouts.Load() != 0 || !unrelatedFixture.available.Load() {
					t.Fatal("target cleanup changed the unrelated plugin", readErr)
				}
			})
		}
	}
}
