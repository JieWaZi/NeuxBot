package runtimeenvironment

import (
	"context"
	"errors"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/installation"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

type installationBoundary struct {
	// maintenance 可选地限制 Fake 的可维护性。
	maintenance *installation.Maintenance
	// mutations 统计程序变更次数。
	mutations int
	// inspectedArgs 记录归属识别收到的程序入口参数。
	inspectedArgs []string
	// lastBinary 保存维护接纳时选中的真实程序。
	lastBinary string
	// mutex 保护已发布程序与下一次任务的测试行为。
	mutex sync.Mutex
	// entered 通知测试下载已由后台任务接管。
	entered chan struct{}
	// release 在测试确认快照后允许原子发布。
	release chan struct{}
	// published 是模拟存储中已完成验证的程序。
	published installation.Result
	// fail 控制下一次安装是否失败。
	fail bool
	// supported 控制当前平台支持事实。
	supported bool
	// readError 模拟已发布程序校验失败，允许真实重装修复。
	readError error
	// result 覆盖维护后适配器确认的真实程序入口。
	result *installation.Result
	// resultVersion 覆盖维护后的实际原生版本回执。
	resultVersion string
	// latest 是模拟官方元数据的版本，不触发程序下载。
	latest string
	// checkError 模拟官方元数据不可用。
	checkError error
	// checks 统计显式元数据检查次数。
	checks int
}

// Supported 返回测试固定的平台支持结果。
func (f *installationBoundary) Supported(string) bool { return f.supported }

// Inspect 返回无网络的程序归属。
func (f *installationBoundary) Inspect(_ context.Context, _ string, binary string, args []string) (installation.Maintenance, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.inspectedArgs = append([]string(nil), args...)
	if f.maintenance != nil {
		return *f.maintenance, nil
	}
	return installation.Maintenance{Installed: binary != "" || f.published.Binary != "", Method: "managed", Path: binary, CanInstall: f.supported, CanUpdate: f.supported, CanUninstall: f.supported}, nil
}

// CheckVersion 只返回 Fake 官方元数据，不触发 Install。
func (f *installationBoundary) CheckVersion(context.Context, string) (installation.Release, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.checks++
	version := f.latest
	if version == "" {
		version = "2.0.0"
	}
	return installation.Release{Version: version}, f.checkError
}

// Installed 读取模拟发布存储及校验结果。
func (f *installationBoundary) Installed(string) (installation.Result, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if f.readError != nil {
		return installation.Result{}, f.readError
	}
	if f.published.Binary == "" {
		return installation.Result{}, os.ErrNotExist
	}
	return f.published, nil
}

// Install 通过屏障模拟取消、失败及完成验证后的发布。
func (f *installationBoundary) InstallAt(ctx context.Context, _ string, binary string, _ []string, progress func(installation.Progress)) (installation.Result, error) {
	f.mutex.Lock()
	f.mutations++
	f.lastBinary = binary
	f.mutex.Unlock()
	progress(installation.Progress{Phase: "downloading", ReceivedBytes: 25, TotalBytes: 100})
	f.entered <- struct{}{}
	select {
	case <-ctx.Done():
		return installation.Result{}, status_error.OperationCanceled
	case <-f.release:
	}
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if f.fail {
		return installation.Result{}, status_error.RuntimeInstallationFailed
	}
	f.published = installation.Result{Binary: "/managed/node", PrefixArgs: []string{"/managed/codex/main.js"}, Version: "2.0.0"}
	if f.result != nil {
		f.published = *f.result
		f.published.PrefixArgs = append([]string(nil), f.result.PrefixArgs...)
	}
	if f.resultVersion != "" {
		f.published.Version = f.resultVersion
	}
	if f.maintenance != nil && f.maintenance.Reason == "repairable" {
		f.maintenance.Reason = ""
	}
	f.readError = nil
	return f.published, nil
}

// Uninstall 使用相同屏障验证取消与卸载生命周期。
func (f *installationBoundary) Uninstall(ctx context.Context, _ string, binary string, _ []string) error {
	f.mutex.Lock()
	f.mutations++
	f.lastBinary = binary
	f.mutex.Unlock()
	f.entered <- struct{}{}
	select {
	case <-ctx.Done():
		return status_error.OperationCanceled
	case <-f.release:
	}
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if f.fail {
		return status_error.RuntimeInstallationFailed
	}
	f.published = installation.Result{}
	return nil
}

func installationLifecycleFixture(t *testing.T) (*service, *installationBoundary, *countingRuntimeCatalog) {
	t.Helper()
	catalog, repo, api := newRuntimeEnvironmentTestService(t)
	svc := api.(*service)
	svc.beforeRuntimeMaintenance = func(context.Context, string) (func(), error) { return func() {}, nil }
	if _, err := svc.EnsureInitialized(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := repo.Ensure(t.Context(), models.RuntimeEnvironment{ID: "profile", RuntimeID: "codex-profile", BaseRuntimeID: constants.RuntimeIDCodex, Name: "profile"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateDetection(t.Context(), constants.RuntimeIDCodex, agent.DetectionResult{Runtime: catalog.Runtimes()[1], ResolvedBinary: "/old/system/codex", Version: "1.0.0"}, 1); err != nil {
		t.Fatal(err)
	}
	// 静态配置及基础智能体共用同一程序，分别保留探测身份。
	catalog.summaries = append(catalog.Runtimes(), catalog.Runtimes()[1])
	catalog.summaries[2].ID = "codex-profile"
	catalog.summaries[2].PrefixArgs = []string{"--profile", "native"}
	svc.lifecycleContext, svc.cancelLifecycle = context.WithCancel(t.Context())
	fake := &installationBoundary{entered: make(chan struct{}, 4), release: make(chan struct{}), supported: true}
	svc.installer = fake
	t.Cleanup(svc.Close)
	return svc, fake, catalog
}

func waitInstallationState(t *testing.T, svc *service, state constants.RuntimeInstallationState) RuntimeInstallationSnapshot {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		snapshot, err := svc.InstallationSnapshot(t.Context(), constants.RuntimeIDCodex)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.State == state {
			return snapshot
		}
		select {
		case <-deadline:
			t.Fatalf("installation state never became %s: %+v", state, snapshot)
		case <-time.After(time.Millisecond):
		}
	}
}

// TestInstallationLifecyclePublishesOnlyVerifiedProgram 验证请求取消不取消任务、配置去重和完成后真实程序重检。
func TestInstallationLifecyclePublishesOnlyVerifiedProgram(t *testing.T) {
	svc, fake, catalog := installationLifecycleFixture(t)
	detectRelease := make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(detectRelease) })
	controlled := &controlledRuntimeCatalog{countingRuntimeCatalog: catalog, started: make(chan string, 2), blockedRuntime: constants.RuntimeIDCodex, release: detectRelease}
	svc.runtimeCatalog, svc.probeProvider = controlled, controlled.probe
	hub := events.New()
	defer hub.Close()
	subscription := hub.Subscribe("runtimes", events.Resource{Kind: "global"})
	defer subscription.Close()
	svc.eventPublisher = hub
	request, cancel := context.WithCancel(t.Context())
	if err := svc.InstallRuntime(request, constants.RuntimeIDCodex); err != nil {
		t.Fatal(err)
	}
	cancel()
	<-fake.entered
	if err := svc.InstallRuntime(t.Context(), "codex-profile"); !errors.Is(err, status_error.RuntimeInstallationBusy) {
		t.Fatalf("profile started a duplicate program task: %v", err)
	}
	snapshot := waitInstallationState(t, svc, constants.RuntimeInstallationStateInstalling)
	if !snapshot.Cancelable || snapshot.Source == constants.RuntimeInstallationSourceManaged {
		t.Fatalf("unfinished download claimed an installation: %+v", snapshot)
	}
	// 分隔下载与发布的实际时刻，确保对外快照能观察到检测阶段的新时间。
	time.Sleep(2 * time.Millisecond)
	close(fake.release)
	select {
	case <-controlled.started:
	case <-time.After(5 * time.Second):
		t.Fatal("published program did not reach detection")
	}
	detecting, err := svc.InstallationSnapshot(t.Context(), constants.RuntimeIDCodex)
	if err != nil || detecting.Phase != "detecting" || detecting.Revision <= snapshot.Revision || detecting.UpdatedAt <= snapshot.UpdatedAt || detecting.Cancelable || !detecting.Installed || detecting.Version != "2.0.0" {
		t.Fatalf("detection transition did not advance the published snapshot: previous=%+v current=%+v error=%v", snapshot, detecting, err)
	}
	unblock.Do(func() { close(detectRelease) })
	snapshot = waitInstallationState(t, svc, constants.RuntimeInstallationStateInstalled)
	if snapshot.Version != "2.0.0" || snapshot.Progress != 100 || !snapshot.Installed || snapshot.Cancelable || snapshot.Revision <= detecting.Revision {
		t.Fatalf("verified publication lost its receipt: %+v", snapshot)
	}
	requestSnapshot := catalog.lastRequest(constants.RuntimeIDCodex)
	if requestSnapshot.LaunchOverride.Binary != "/managed/node" || len(requestSnapshot.LaunchOverride.PrefixArgs) != 1 {
		t.Fatalf("runtime did not select the published executable: %+v", requestSnapshot.LaunchOverride)
	}
	profileSnapshot := catalog.lastRequest("codex-profile")
	if len(profileSnapshot.LaunchOverride.PrefixArgs) != 3 || profileSnapshot.LaunchOverride.PrefixArgs[1] != "--profile" {
		t.Fatalf("managed interpreter replaced static profile arguments: %+v", profileSnapshot.LaunchOverride)
	}
	seenProgress, seenTerminal := false, false
	for !seenTerminal {
		select {
		case event := <-subscription.Events:
			seenProgress = seenProgress || event.Type == "runtime.installation.changed"
			seenTerminal = event.Type == "runtimecontract.changed"
		case <-time.After(time.Second):
			t.Fatal("installation terminal event missing")
		}
	}
	if !seenProgress {
		t.Fatal("installation progress event missing")
	}
}

// TestInstallationCancelRetryAndShutdown 验证取消回收后可重试，失败更新保留旧程序，退出拒绝新任务。
func TestInstallationCancelRetryAndShutdown(t *testing.T) {
	svc, fake, _ := installationLifecycleFixture(t)
	if err := svc.InstallRuntime(t.Context(), constants.RuntimeIDCodex); err != nil {
		t.Fatal(err)
	}
	<-fake.entered
	if err := svc.CancelRuntimeInstallation(t.Context(), "codex-profile"); err != nil {
		t.Fatal(err)
	}
	waitInstallationState(t, svc, constants.RuntimeInstallationStateCancelled)
	fake.mutex.Lock()
	fake.fail = true
	fake.published = installation.Result{Binary: "/managed/old", Version: "1.0.0"}
	fake.mutex.Unlock()
	close(fake.release)
	if err := svc.InstallRuntime(t.Context(), constants.RuntimeIDCodex); err != nil {
		t.Fatal(err)
	}
	<-fake.entered
	snapshot := waitInstallationState(t, svc, constants.RuntimeInstallationStateFailed)
	// 旧版本已发布且可继续运行；一次失败更新不能将其抹掉。
	stored, err := fake.Installed(constants.RuntimeIDCodex)
	if err != nil || stored.Version != "1.0.0" || snapshot.ErrorCode == "" || !snapshot.Installed || snapshot.Version != "1.0.0" {
		t.Fatalf("failed update discarded previous publication: %+v %v", stored, err)
	}
	svc.Close()
	if err := svc.InstallRuntime(t.Context(), constants.RuntimeIDCodex); !errors.Is(err, status_error.RuntimeEnvironmentServiceUnavailable) {
		t.Fatalf("shutdown accepted an orphan task: %v", err)
	}
}

// TestInstallationSnapshotSupportsRepair 验证不支持的平台返回能力快照，已损坏程序仍可通过真实重装修复。
func TestInstallationSnapshotSupportsRepair(t *testing.T) {
	svc, fake, catalog := installationLifecycleFixture(t)
	fake.supported = false
	snapshot, err := svc.InstallationSnapshot(t.Context(), constants.RuntimeIDCodex)
	if err != nil || snapshot.Supported {
		t.Fatalf("unsupported platform did not return a capability snapshot: %+v %v", snapshot, err)
	}
	fake.supported, fake.readError = true, status_error.RuntimeInstallationFailed
	snapshot, err = svc.InstallationSnapshot(t.Context(), constants.RuntimeIDCodex)
	if err != nil || snapshot.State != constants.RuntimeInstallationStateFailed || snapshot.ErrorCode == "" {
		t.Fatalf("damaged program did not expose a repairable failure: %+v %v", snapshot, err)
	}
	detected := svc.detect(t.Context(), catalog.Runtimes()[1], models.RuntimeEnvironment{RuntimeID: constants.RuntimeIDCodex, BaseRuntimeID: constants.RuntimeIDCodex})
	if detected.ResolvedBinary != "" || detected.State != agent.DetectionStateUnavailable || catalog.detectionCount() != 0 {
		t.Fatalf("damaged managed program silently fell back to another executable: %+v", detected)
	}
	if err := svc.InstallRuntime(t.Context(), constants.RuntimeIDCodex); err != nil {
		t.Fatalf("damaged program cannot be reinstalled: %v", err)
	}
	<-fake.entered
	close(fake.release)
	waitInstallationState(t, svc, constants.RuntimeInstallationStateInstalled)
}

// TestInstallationShutdownWaitsForActiveTask 验证 Core 退出取消当前下载并等到任务回收完成。
func TestInstallationShutdownWaitsForActiveTask(t *testing.T) {
	svc, fake, _ := installationLifecycleFixture(t)
	if err := svc.InstallRuntime(t.Context(), constants.RuntimeIDCodex); err != nil {
		t.Fatal(err)
	}
	<-fake.entered
	closed := make(chan struct{})
	go func() {
		svc.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Core shutdown left an installation worker running")
	}
	snapshot, err := svc.InstallationSnapshot(t.Context(), constants.RuntimeIDCodex)
	if err != nil || snapshot.State != constants.RuntimeInstallationStateCancelled || snapshot.Cancelable {
		t.Fatalf("shutdown did not settle the installation owner: %+v %v", snapshot, err)
	}
}

// TestProgramMaintenanceRefusesMutationAfterFailedOwnerCleanup 验证清理失败时程序完全不变。
func TestProgramMaintenanceRefusesMutationAfterFailedOwnerCleanup(t *testing.T) {
	for _, uninstall := range []bool{false, true} {
		t.Run(map[bool]string{false: "update", true: "uninstall"}[uninstall], func(t *testing.T) {
			svc, fake, _ := installationLifecycleFixture(t)
			svc.beforeRuntimeMaintenance = func(context.Context, string) (func(), error) {
				return nil, status_error.RuntimeShutdownFailed
			}
			var err error
			if uninstall {
				err = svc.UninstallRuntime(t.Context(), constants.RuntimeIDCodex)
			} else {
				err = svc.InstallRuntime(t.Context(), constants.RuntimeIDCodex)
			}
			if err != nil {
				t.Fatal(err)
			}
			snapshot := waitInstallationState(t, svc, constants.RuntimeInstallationStateFailed)
			fake.mutex.Lock()
			mutations := fake.mutations
			fake.mutex.Unlock()
			if mutations != 0 || snapshot.ErrorCode != status_error.RuntimeShutdownFailed.Key() {
				t.Fatalf("failed cleanup allowed a file mutation: mutations=%d snapshot=%+v", mutations, snapshot)
			}
		})
	}
}

// TestProgramUninstallIsScopedAndRetainsExecutionSettings 验证程序卸载影响所有同源配置但不删除账号默认值。
func TestProgramUninstallIsScopedAndRetainsExecutionSettings(t *testing.T) {
	svc, fake, _ := installationLifecycleFixture(t)
	for _, id := range []string{constants.RuntimeIDCodex, "codex-profile"} {
		if err := svc.environments.UpdateFields(t.Context(), id, map[string]any{"default_account_id": "keep-account", "default_model": "keep-model", "default_reasoning": "keep-reasoning"}); err != nil {
			t.Fatal(err)
		}
	}
	var held bool
	svc.beforeRuntimeMaintenance = func(_ context.Context, base string) (func(), error) {
		if base != constants.RuntimeIDCodex {
			t.Errorf("maintenance targeted profile rather than base: %s", base)
		}
		held = true
		return func() { held = false }, nil
	}
	svc.probeProvider = func(_ context.Context, summary agent.RuntimeSummary, environment models.RuntimeEnvironment, request agent.DetectRequest) agent.DetectionResult {
		if held {
			t.Error("post-uninstall probe ran inside the native admission fence")
		}
		if environment.Detection.ResolvedBinary != "" || request.LaunchOverride.Binary == "/old/system/codex" {
			t.Error("uninstall reused a removed program selection")
		}
		return agent.DetectionResult{Runtime: summary, State: agent.DetectionStateUnavailable}
	}
	if err := svc.UninstallRuntime(t.Context(), constants.RuntimeIDCodex); err != nil {
		t.Fatal(err)
	}
	<-fake.entered
	if err := svc.InstallRuntime(t.Context(), "codex-profile"); !errors.Is(err, status_error.RuntimeInstallationBusy) {
		t.Fatalf("profile bypassed uninstall ownership: %v", err)
	}
	close(fake.release)
	snapshot := waitInstallationState(t, svc, constants.RuntimeInstallationStateIdle)
	if snapshot.Operation != "uninstall" || snapshot.Installed || snapshot.Cancelable {
		t.Fatalf("uninstall did not settle with actual absence: %+v", snapshot)
	}
	for _, id := range []string{constants.RuntimeIDCodex, "codex-profile"} {
		environment, err := svc.environments.Get(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if environment.DefaultAccountID != "keep-account" || environment.DefaultModel != "keep-model" || environment.DefaultReasoning != "keep-reasoning" || environment.Detection.ResolvedBinary != "" {
			t.Fatalf("uninstall removed execution settings or retained stale program for %s: %+v", id, environment)
		}
	}
}

func TestProgramMaintenanceRefusesUnprovenOwnership(t *testing.T) {
	svc, fake, _ := installationLifecycleFixture(t)
	fake.maintenance = &installation.Maintenance{Method: "unknown", Reason: "ownership_unknown"}
	for _, mutate := range []func(context.Context, string) error{svc.InstallRuntime, svc.UninstallRuntime} {
		if err := mutate(t.Context(), constants.RuntimeIDCodex); !errors.Is(err, status_error.RuntimeMaintenanceUnsupported) {
			t.Fatalf("unproven owner admitted mutation: %v", err)
		}
	}
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	if fake.mutations != 0 {
		t.Fatal("unproven owner reached filesystem boundary")
	}
}

// TestProgramMaintenanceKeepsSelectedSystemInstallation 验证已有私有副本不会接管用户选择的系统程序。
func TestProgramMaintenanceKeepsSelectedSystemInstallation(t *testing.T) {
	svc, fake, catalog := installationLifecycleFixture(t)
	fake.maintenance = &installation.Maintenance{Installed: true, Method: "npm", Path: "/old/system/codex", CanUpdate: true, CanUninstall: true}
	fake.published = installation.Result{Binary: "/private/unselected/codex", Version: "99.0.0"}
	snapshot, err := svc.InstallationSnapshot(t.Context(), constants.RuntimeIDCodex)
	if err != nil || snapshot.Source != constants.RuntimeInstallationSourceSystem || snapshot.Version != "1.0.0" || snapshot.InstallationMethod != "npm" {
		t.Fatalf("private copy replaced the selected system snapshot: %+v err=%v", snapshot, err)
	}
	environment, err := svc.environments.Get(t.Context(), constants.RuntimeIDCodex)
	if err != nil {
		t.Fatal(err)
	}
	svc.detect(t.Context(), catalog.Runtimes()[1], environment)
	if request := catalog.lastRequest(constants.RuntimeIDCodex); request.LaunchOverride.Binary != "/old/system/codex" {
		t.Fatalf("private copy replaced the selected system probe: %+v", request.LaunchOverride)
	}
	if err := svc.InstallRuntime(t.Context(), constants.RuntimeIDCodex); err != nil {
		t.Fatal(err)
	}
	<-fake.entered
	fake.mutex.Lock()
	target := fake.lastBinary
	fake.mutex.Unlock()
	if target != "/old/system/codex" {
		t.Fatalf("update targeted a parallel copy: %s", target)
	}
	close(fake.release)
	waitInstallationState(t, svc, constants.RuntimeInstallationStateInstalled)
}

// TestProgramOwnershipExcludesStaticProfileArguments 验证受管脚本归属不被配置参数破坏。
func TestProgramOwnershipExcludesStaticProfileArguments(t *testing.T) {
	svc, fake, catalog := installationLifecycleFixture(t)
	fake.published = installation.Result{Binary: "/trusted/node", PrefixArgs: []string{"/owned/entry.js"}, Version: "2.0.0"}
	environment := models.RuntimeEnvironment{RuntimeID: "codex-profile", BaseRuntimeID: constants.RuntimeIDCodex, PrefixArgs: []string{"/owned/entry.js", "--profile", "native"}, Detection: agent.DetectionResult{ResolvedBinary: "/trusted/node"}}
	svc.detect(t.Context(), catalog.Runtimes()[2], environment)
	fake.mutex.Lock()
	arguments := append([]string(nil), fake.inspectedArgs...)
	fake.mutex.Unlock()
	if len(arguments) != 1 || arguments[0] != "/owned/entry.js" {
		t.Fatalf("ownership used profile flags or lost managed script: %v", arguments)
	}
	environment.PrefixArgs = []string{"--profile", "native"}
	if args := svc.programSelectionArguments(environment); args != nil {
		t.Fatalf("native program ownership received a nonnil empty profile suffix: %#v", args)
	}
}

func TestProgramSnapshotDoesNotRetainRemovedSelection(t *testing.T) {
	svc, fake, _ := installationLifecycleFixture(t)
	fake.maintenance = &installation.Maintenance{Method: "managed", CanInstall: true}
	snapshot, err := svc.InstallationSnapshot(t.Context(), constants.RuntimeIDCodex)
	if err != nil || snapshot.Installed || snapshot.Version != "" || snapshot.State != constants.RuntimeInstallationStateIdle || snapshot.Source != constants.RuntimeInstallationSourceUnknown || !snapshot.CanInstall {
		t.Fatalf("removed selected program remained installed from old detection: %+v err=%v", snapshot, err)
	}
}

func TestProgramMaintenanceDoesNotReplaceActiveVersionProbe(t *testing.T) {
	svc, fake, _ := installationLifecycleFixture(t)
	svc.restartProvider = func(context.Context, string) error {
		t.Fatal("restart entered while native version process was active")
		return nil
	}
	svc.installationJobs = map[string]*runtimeInstallationJob{constants.RuntimeIDCodex: {snapshot: RuntimeInstallationSnapshot{CheckState: constants.RuntimeUpdateCheckStateChecking}}}
	for _, mutate := range []func(context.Context, string) error{svc.UninstallRuntime, svc.RestartRuntime} {
		if err := mutate(t.Context(), constants.RuntimeIDCodex); !errors.Is(err, status_error.RuntimeUpdateCheckBusy) {
			t.Fatalf("maintenance replaced an active version probe owner: %v", err)
		}
	}
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	if fake.mutations != 0 {
		t.Fatal("active version probe allowed program mutation")
	}
}

func TestProgramUpdateRequiresActualPublisherVersion(t *testing.T) {
	svc, fake, _ := installationLifecycleFixture(t)
	fake.resultVersion = "1.0.0"
	if err := svc.InstallRuntime(t.Context(), constants.RuntimeIDCodex); err != nil {
		t.Fatal(err)
	}
	<-fake.entered
	close(fake.release)
	snapshot := waitInstallationState(t, svc, constants.RuntimeInstallationStateFailed)
	if snapshot.ErrorReason != "version" || snapshot.ErrorOperation != "verifying" {
		t.Fatalf("unchanged program was reported as a successful update: %+v", snapshot)
	}
}

func TestProgramSnapshotRecoversPendingUninstallWithoutMemoryJob(t *testing.T) {
	svc, fake, _ := installationLifecycleFixture(t)
	fake.maintenance = &installation.Maintenance{Method: "managed", Reason: "cleanup_pending", CanUninstall: true}
	fake.published = installation.Result{Binary: "/owned/old-program", Version: "1.0.0"}
	snapshot, err := svc.InstallationSnapshot(t.Context(), constants.RuntimeIDCodex)
	if err != nil || snapshot.Installed || !snapshot.CanUninstall || snapshot.CanInstall || snapshot.CanUpdate || snapshot.Operation != "uninstall" || snapshot.State != constants.RuntimeInstallationStateFailed {
		t.Fatalf("pending disk cleanup lost its uninstall owner after memory reset: %+v err=%v", snapshot, err)
	}
	if err := svc.InstallRuntime(t.Context(), constants.RuntimeIDCodex); !errors.Is(err, status_error.RuntimeMaintenanceUnsupported) {
		t.Fatalf("pending deletion admitted a replacement installation: %v", err)
	}
	if err := svc.UninstallRuntime(t.Context(), constants.RuntimeIDCodex); err != nil {
		t.Fatal(err)
	}
	<-fake.entered
}

// TestProgramRepairReinstallsCurrentVersionWithoutRunningDamagedTarget 验证损坏选择不可被同版本 no-op 吞掉。
func TestProgramRepairReinstallsCurrentVersionWithoutRunningDamagedTarget(t *testing.T) {
	svc, fake, _ := installationLifecycleFixture(t)
	// 归属适配器确认当前选择需要修复；另一个已发布的同版本不能替代这份修复事实。
	fake.maintenance = &installation.Maintenance{Method: "managed", Installed: true, CanUpdate: true, CanUninstall: true, Reason: "repairable"}
	fake.published = installation.Result{Binary: "/owned/current-version", Version: "2.0.0"}
	fake.latest = "2.0.0"
	if err := svc.InstallRuntime(t.Context(), constants.RuntimeIDCodex); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fake.entered:
	case <-time.After(time.Second):
		t.Fatal("same-version repair was swallowed by an up-to-date no-op")
	}
	fake.mutex.Lock()
	checks, mutations := fake.checks, fake.mutations
	fake.mutex.Unlock()
	if checks != 0 || mutations != 1 {
		t.Fatalf("repair checked or executed the known-damaged local target: checks=%d mutations=%d", checks, mutations)
	}
	close(fake.release)
	waitInstallationState(t, svc, constants.RuntimeInstallationStateInstalled)
}

// TestProgramMaintenanceCommitsVerifiedLaunchBeforeReprobe 验证包管理器换入口后仍启动所维护的同一程序。
func TestProgramMaintenanceCommitsVerifiedLaunchBeforeReprobe(t *testing.T) {
	for method, result := range map[string]installation.Result{
		"npm":      {Binary: "/tools/node.exe", PrefixArgs: []string{"/global/npm/node_modules/@openai/codex/bin/codex.js"}, Version: "2.0.0"},
		"homebrew": {Binary: "/brew/bin/codex", Version: "2.0.0"},
	} {
		t.Run(method, func(t *testing.T) {
			svc, fake, catalog := installationLifecycleFixture(t)
			oldBinary := "/global/npm/codex.cmd"
			if method == "homebrew" {
				oldBinary = "/brew/Cellar/codex/1.0.0/bin/codex"
			}
			for _, summary := range catalog.Runtimes() {
				if summary.BaseRuntimeID != constants.RuntimeIDCodex {
					continue
				}
				if err := svc.environments.UpdateDetection(t.Context(), summary.ID, agent.DetectionResult{Runtime: summary, ResolvedBinary: oldBinary, Version: "1.0.0"}, 1); err != nil {
					t.Fatal(err)
				}
			}
			fake.result = &result
			fake.maintenance = &installation.Maintenance{Method: method, Installed: true, CanUpdate: true, CanUninstall: true}
			expectArgs := func(id string) []string {
				arguments := append([]string(nil), result.PrefixArgs...)
				if id == "codex-profile" {
					arguments = append(arguments, "--profile", "native")
				}
				return arguments
			}
			var releaseOnce sync.Once
			held := false
			svc.beforeRuntimeMaintenance = func(context.Context, string) (func(), error) {
				held = true
				return func() {
					releaseOnce.Do(func() {
						for _, id := range []string{constants.RuntimeIDCodex, "codex-profile"} {
							environment, err := svc.environments.Get(t.Context(), id)
							if err != nil || environment.Detection.ResolvedBinary != result.Binary || !slices.Equal(environment.PrefixArgs, expectArgs(id)) {
								t.Errorf("maintenance fence released before verified launch committed: id=%s environment=%+v err=%v", id, environment, err)
							}
						}
						held = false
					})
				}, nil
			}
			svc.probeProvider = func(_ context.Context, summary agent.RuntimeSummary, environment models.RuntimeEnvironment, request agent.DetectRequest) agent.DetectionResult {
				if held || request.LaunchOverride.Binary != result.Binary || !slices.Equal(request.LaunchOverride.PrefixArgs, expectArgs(environment.RuntimeID)) {
					t.Errorf("reprobe used old or duplicated launch: held=%v id=%s launch=%+v", held, environment.RuntimeID, request.LaunchOverride)
				}
				return agent.DetectionResult{Runtime: summary, State: agent.DetectionStateReady, ResolvedBinary: request.LaunchOverride.Binary, Version: result.Version}
			}
			if err := svc.InstallRuntime(t.Context(), constants.RuntimeIDCodex); err != nil {
				t.Fatal(err)
			}
			<-fake.entered
			close(fake.release)
			waitInstallationState(t, svc, constants.RuntimeInstallationStateInstalled)
			for _, id := range []string{constants.RuntimeIDCodex, "codex-profile"} {
				environment, err := svc.environments.Get(t.Context(), id)
				if err != nil || environment.Detection.ResolvedBinary != result.Binary || !slices.Equal(environment.PrefixArgs, expectArgs(id)) {
					t.Fatalf("verified launch did not survive ordinary detection: id=%s environment=%+v err=%v", id, environment, err)
				}
			}
		})
	}
}
