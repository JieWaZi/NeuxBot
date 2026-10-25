package plugins

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/feishu"
	"github.com/gin-gonic/gin"
)

type removalAuthFixture struct {
	// auth 是使用真实飞书驱动与替换进程边界的共享身份。
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

func removalAuth(t *testing.T, logoutError error) *removalAuthFixture {
	t.Helper()
	f := &removalAuthFixture{cancelled: make(chan struct{}), allowExit: make(chan struct{}), logoutError: logoutError}
	f.available.Store(true)
	profile, binary := t.TempDir(), filepath.Join(t.TempDir(), "lark-cli")
	for name, content := range map[string]string{filepath.Join(profile, "config.json"): "{}", binary: "fixture"} {
		if err := os.WriteFile(name, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	f.auth = feishu.New(feishu.Config{ProfileDirectory: profile, Binary: binary, Run: func(ctx context.Context, args []string, _ string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch {
		case joined == "auth logout":
			if f.logoutError != nil {
				return nil, f.logoutError
			}
			f.logouts.Add(1)
			f.available.Store(false)
			return []byte(`{}`), nil
		case strings.Contains(joined, "--no-wait"):
			return []byte(`{"verification_url":"https://example.com/device","device_code":"private","expires_in":300}`), nil
		case strings.Contains(joined, "--device-code"):
			<-ctx.Done()
			close(f.cancelled)
			<-f.allowExit
			return nil, ctx.Err()
		default:
			return []byte(fmt.Sprintf(`{"appId":"fixture-app","identities":{"user":{"available":%t,"verified":true,"openId":"fixture-user","scope":"offline_access"}}}`, f.available.Load())), nil
		}
	}})
	f.auth.Open(context.Background())
	previous := global.PluginAuthorizations
	global.PluginAuthorizations = map[string]*authorization.Authorizer{"feishu": f.auth}
	t.Cleanup(func() {
		f.release.Do(func() { close(f.allowExit) })
		f.auth.Close()
		global.PluginAuthorizations = previous
	})
	return f
}

func removalPlugin(t *testing.T, ctx *gin.Context, name string) PluginDetailResponse {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"`+name+`","version":"1.0.0","extensions":{"com.ally":{"provider":"feishu"}}}`), 0o600); err != nil {
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
	detail := removalPlugin(t, ctx, "office")
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
	first, other := removalPlugin(t, ctx, "office-a"), removalPlugin(t, ctx, "office-b")
	installRemovalPlugin(t, ctx, first, "w1", "w2")
	installRemovalPlugin(t, ctx, other, "w2")
	if err := global.PluginControllerManager.SetEnabled(ctx, "w2", other.Plugin.ID, false); err != nil {
		t.Fatal(err)
	}
	release, err := f.auth.Acquire(authorization.Usage{WakerID: "w2", ExecutionID: "running"})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := (&UninstallWakerPlugin{WakerID: "w1", PluginID: first.Plugin.ID}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := (&UninstallWakerPlugin{WakerID: "w2", PluginID: first.Plugin.ID}).Output(ctx); err != nil {
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
	detail := removalPlugin(t, ctx, "orphan-office")
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
	detail := removalPlugin(t, ctx, "retry-office")
	installRemovalPlugin(t, ctx, detail, "w1")
	if _, err := (&UninstallWakerPlugin{WakerID: "w1", PluginID: detail.Plugin.ID}).Output(ctx); !errors.Is(err, status_error.PluginStorageError) {
		t.Fatalf("cleanup failure hidden: %v", err)
	}
	relations, err := global.PluginControllerManager.Installations(ctx, detail.Plugin.ID, "w1")
	if err != nil || len(relations) != 1 || !f.available.Load() {
		t.Fatalf("failed cleanup deleted installation: %+v %v", relations, err)
	}
}

func TestDeleteProviderResourcePreservesOtherPartnersAndChecksConfirmationBeforeLogout(t *testing.T) {
	ctx := pluginAPIContext(t)
	f := removalAuth(t, nil)
	first, other := removalPlugin(t, ctx, "delete-office-a"), removalPlugin(t, ctx, "delete-office-b")
	installRemovalPlugin(t, ctx, first, "w1")
	installRemovalPlugin(t, ctx, other, "w2")
	if _, err := (&DeletePlugin{PluginID: first.Plugin.ID, Body: DeletePluginRequest{Confirmed: true}}).Output(ctx); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatalf("stale confirmation accepted: %v", err)
	}
	if f.logouts.Load() != 0 {
		t.Fatal("stale confirmation cleared authorization")
	}
	release, err := f.auth.Acquire(authorization.Usage{WakerID: "w2", ExecutionID: "other-running"})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	firstBody := DeletePluginRequest{Confirmed: true, Installations: []VersionSwitch{{WakerID: "w1", ExpectedVersionID: first.ResolvedVersionID}}}
	if _, err := (&DeletePlugin{PluginID: first.Plugin.ID, Body: firstBody}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	if !f.available.Load() || f.logouts.Load() != 0 {
		t.Fatal("other partner lost authorization after resource deletion")
	}
	otherBody := DeletePluginRequest{Confirmed: true, Installations: []VersionSwitch{{WakerID: "w2", ExpectedVersionID: other.ResolvedVersionID}}}
	if _, err := (&DeletePlugin{PluginID: other.Plugin.ID, Body: otherBody}).Output(ctx); !errors.Is(err, status_error.PluginBusy) {
		t.Fatalf("active work did not protect resource deletion: %v", err)
	}
	if _, err := global.PluginControllerManager.Get(ctx, other.Plugin.ID); err != nil || f.logouts.Load() != 0 {
		t.Fatalf("busy deletion changed facts: %v", err)
	}
	release()
	if _, err := (&DeletePlugin{PluginID: other.Plugin.ID, Body: otherBody}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	if f.available.Load() || f.logouts.Load() != 1 {
		t.Fatal("last resource deletion retained shared authorization")
	}
}
