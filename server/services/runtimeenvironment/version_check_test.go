package runtimeenvironment

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// TestVersionCheckRefreshesLocalReceiptWithoutDownload 验证旧探测快照不会让已手动升级的程序继续显示更新。
func TestVersionCheckRefreshesLocalReceiptWithoutDownload(t *testing.T) {
	svc, installer, catalog := installationLifecycleFixture(t)
	catalog.installationVersion, installer.latest = "2.10.0", "2.10.0"
	before, err := svc.InstallationSnapshot(t.Context(), constants.RuntimeIDCodex)
	if err != nil || before.CheckState != constants.RuntimeUpdateCheckStateUnknown || before.UpdateAvailable != nil || installer.checks != 0 {
		t.Fatalf("GET performed a hidden update check: %+v error=%v", before, err)
	}
	if err := svc.CheckRuntimeUpdates(t.Context(), constants.RuntimeIDCodex); err != nil {
		t.Fatal(err)
	}
	after, err := svc.InstallationSnapshot(t.Context(), constants.RuntimeIDCodex)
	if err != nil || after.Version != "2.10.0" || after.LatestVersion != "2.10.0" || after.CheckedAt == 0 || after.UpdateAvailable == nil || *after.UpdateAvailable || after.Source != constants.RuntimeInstallationSourceSystem {
		t.Fatalf("up-to-date local CLI was incorrectly offered an update: %+v error=%v", after, err)
	}
	if err := svc.InstallRuntime(t.Context(), constants.RuntimeIDCodex); err != nil {
		t.Fatal(err)
	}
	if len(installer.entered) != 0 {
		t.Fatal("up-to-date CLI triggered an installation")
	}
}

// TestVersionCheckDistinguishesUpdateFailureAndUnknown 验证有更新、版本更高与检查失败不会被折成同一个布尔值。
func TestVersionCheckDistinguishesUpdateFailureAndUnknown(t *testing.T) {
	svc, installer, catalog := installationLifecycleFixture(t)
	catalog.installationVersion, installer.latest = "2.1.226 (Claude Code)", "2.1.232"
	if err := svc.CheckRuntimeUpdates(t.Context(), constants.RuntimeIDCodex); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := svc.InstallationSnapshot(t.Context(), constants.RuntimeIDCodex)
	if snapshot.UpdateAvailable == nil || !*snapshot.UpdateAvailable {
		t.Fatalf("newer official release was not offered: %+v", snapshot)
	}
	catalog.installationVersion = "3.0.0"
	if err := svc.CheckRuntimeUpdates(t.Context(), constants.RuntimeIDCodex); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = svc.InstallationSnapshot(t.Context(), constants.RuntimeIDCodex)
	if snapshot.UpdateAvailable == nil || *snapshot.UpdateAvailable {
		t.Fatalf("higher local version was offered a downgrade: %+v", snapshot)
	}
	installer.checkError = status_error.RuntimeInstallationFailed
	if err := svc.CheckRuntimeUpdates(t.Context(), constants.RuntimeIDCodex); !errors.Is(err, status_error.RuntimeInstallationFailed) {
		t.Fatalf("official check failure was hidden: %v", err)
	}
	snapshot, _ = svc.InstallationSnapshot(t.Context(), constants.RuntimeIDCodex)
	if snapshot.CheckState != constants.RuntimeUpdateCheckStateFailed || snapshot.UpdateAvailable != nil || snapshot.CheckErrorCode == "" {
		t.Fatalf("failed check claimed the CLI was up to date: %+v", snapshot)
	}
}

// TestRestartRuntimeUsesNativeOwnerBeforeDetection 验证重启确实调用原生所有者，忙时不会仅重检或终止活动工作。
func TestRestartRuntimeUsesNativeOwnerBeforeDetection(t *testing.T) {
	svc, _, catalog := installationLifecycleFixture(t)
	closed := false
	svc.restartProvider = func(_ context.Context, provider string) error {
		if provider != constants.RuntimeIDCodex {
			t.Fatal("restart used the profile name as native provider")
		}
		return status_error.RuntimeRestartBusy
	}
	if err := svc.RestartRuntime(t.Context(), "codex-profile"); !errors.Is(err, status_error.RuntimeRestartBusy) || catalog.detectionCount() != 0 {
		t.Fatalf("active native work was refreshed instead of rejected: %v", err)
	}
	svc.restartProvider = func(context.Context, string) error { closed = true; return nil }
	if err := svc.RestartRuntime(t.Context(), "codex-profile"); err != nil || !closed || catalog.detectionCount() != 1 {
		t.Fatalf("restart did not close then inspect the actual provider: closed=%v probes=%d error=%v", closed, catalog.detectionCount(), err)
	}
}
