package runtimeenvironment

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// authenticationBoundary 不启动 CLI、浏览器或真实凭据读取。
type authenticationBoundary struct {
	// result 固定原生认证返回的证据语义。
	result constants.RuntimeAuthenticationResult
	// initializeEntered 在只读原生握手真正开始后通知测试。
	initializeEntered chan struct{}
	// initializeRelease 将只读握手停在确定性屏障以观察对外任务状态。
	initializeRelease chan struct{}
	// initializeError 模拟原生握手失败，与配置能力缺失分别断言。
	initializeError error
	// mutex 保护并发原生操作记录。
	mutex sync.Mutex
	// capabilities 固定真实握手的模拟回执。
	capabilities agent.NativeAuthenticationCapabilities
	// initialized 记录初始化次数。
	initialized int
	// methods 记录实际认证方法。
	methods []string
	// entered 标记认证已开始。
	entered chan struct{}
	// finish 控制原生认证完成。
	finish chan struct{}
	// closeFailures 指定同一客户端的关闭失败次数。
	closeFailures int
	// closes 记录关闭尝试次数。
	closes int
}

func (f *authenticationBoundary) Initialize(ctx context.Context) (agent.NativeAuthenticationCapabilities, error) {
	f.mutex.Lock()
	f.initialized++
	capabilities, err := f.capabilities, f.initializeError
	f.mutex.Unlock()
	if f.initializeEntered != nil {
		f.initializeEntered <- struct{}{}
	}
	if f.initializeRelease != nil {
		select {
		case <-f.initializeRelease:
		case <-ctx.Done():
			return agent.NativeAuthenticationCapabilities{}, ctx.Err()
		}
	}
	return capabilities, err
}
func (f *authenticationBoundary) Authenticate(ctx context.Context, method string) (constants.RuntimeAuthenticationResult, error) {
	f.mutex.Lock()
	f.methods = append(f.methods, method)
	f.mutex.Unlock()
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.finish != nil {
		select {
		case <-f.finish:
		case <-ctx.Done():
			return constants.RuntimeAuthenticationResultUnspecified, ctx.Err()
		}
	}
	if f.result != constants.RuntimeAuthenticationResultUnspecified {
		return f.result, nil
	}
	return constants.RuntimeAuthenticationResultAuthenticationCompleted, nil
}
func (f *authenticationBoundary) Close(context.Context) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.closes++
	if f.closes <= f.closeFailures {
		return status_error.RuntimeShutdownFailed
	}
	return nil
}
func authenticationFixture(t *testing.T, fake *authenticationBoundary) *service {
	t.Helper()
	catalog, repo, value := newRuntimeEnvironmentTestService(t)
	svc := value.(*service)
	catalog.summaries = []agent.RuntimeSummary{{ID: constants.RuntimeIDGrok, BaseRuntimeID: constants.RuntimeIDGrok, Name: "Grok Build"}}
	if _, err := svc.EnsureInitialized(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateDetection(t.Context(), constants.RuntimeIDGrok, agent.DetectionResult{ResolvedBinary: "/fixture/grok", Auth: agent.AuthStatusMissing}, 1); err != nil {
		t.Fatal(err)
	}
	svc.lifecycleContext, svc.cancelLifecycle = context.WithCancel(t.Context())
	svc.authentication = newRuntimeAuthenticationManager(svc, func(context.Context, string) (func(), error) { return func() {}, nil })
	svc.authentication.factory = func(_ context.Context, account agent.AccountRequest, launch agent.ProviderLaunch) (agent.NativeAuthenticationClient, error) {
		if account.AccountID != agent.SystemAccountID || account.Provider != constants.RuntimeIDGrok || account.Subject != "" || launch.Executable != "/fixture/grok" {
			t.Errorf("crossed system/program scope: account=%s provider=%s executable=%s", account.AccountID, account.Provider, launch.Executable)
		}
		return fake, nil
	}
	t.Cleanup(svc.Close)
	return svc
}
func waitAuthenticationState(t *testing.T, svc *service, state constants.RuntimeAuthenticationState) RuntimeAuthenticationSnapshot {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		view, err := svc.AuthenticationSnapshot(t.Context(), constants.RuntimeIDGrok, false)
		if err != nil {
			t.Fatal(err)
		}
		if view.State == state {
			return view
		}
		select {
		case <-deadline:
			t.Fatalf("never became %s: %+v", state, view)
		case <-time.After(time.Millisecond):
		}
	}
}
func TestRuntimeAuthenticationSystemGrokUsesRealAdvertisedMethod(t *testing.T) {
	fake := &authenticationBoundary{capabilities: agent.NativeAuthenticationCapabilities{Methods: []agent.NativeAuthenticationMethod{{ID: "cached_token", Kind: "agent"}, {ID: "xai.api_key", Kind: "agent"}, {ID: "grok.com/oidc", Kind: "agent"}}}, entered: make(chan struct{}, 1), finish: make(chan struct{})}
	svc := authenticationFixture(t, fake)
	if _, err := svc.AuthenticationSnapshot(t.Context(), constants.RuntimeIDGrok, false); err != nil {
		t.Fatal(err)
	}
	view := waitAuthenticationState(t, svc, constants.RuntimeAuthenticationStateIdle)
	if !view.CanAuthenticate || !view.APIKeySupported || len(view.Methods) != 1 || view.Methods[0].ID != "grok.com/oidc" || view.Scope != "system" {
		t.Fatalf("capabilities=%+v", view)
	}
	fake.mutex.Lock()
	if len(fake.methods) != 0 || fake.initialized != 1 || fake.closes != 1 {
		t.Error("GET logged in or failed to close")
	}
	fake.mutex.Unlock()
	request, cancel := context.WithCancel(t.Context())
	started, err := svc.AuthenticateRuntime(request, constants.RuntimeIDGrok, "")
	if err != nil || started.JobID == "" || started.JobID == view.JobID {
		t.Fatalf("start=%+v %v", started, err)
	}
	cancel()
	<-fake.entered
	current := waitAuthenticationState(t, svc, constants.RuntimeAuthenticationStateAuthenticating)
	if !current.Cancelable || current.VerificationURL != "" || current.UserCode != "" {
		t.Fatalf("invented device data: %+v", current)
	}
	close(fake.finish)
	completed := waitAuthenticationState(t, svc, constants.RuntimeAuthenticationStateSucceeded)
	if completed.Cancelable || completed.JobID != started.JobID || completed.Revision <= started.Revision {
		t.Fatalf("terminal=%+v", completed)
	}
}
func TestRuntimeAuthenticationCancelFencesOldJob(t *testing.T) {
	fake := &authenticationBoundary{capabilities: agent.NativeAuthenticationCapabilities{Methods: []agent.NativeAuthenticationMethod{{ID: "grok.com/oidc", Kind: "agent"}}}, entered: make(chan struct{}, 2), finish: make(chan struct{})}
	svc := authenticationFixture(t, fake)
	first, err := svc.AuthenticateRuntime(t.Context(), constants.RuntimeIDGrok, "grok.com/oidc")
	if err != nil {
		t.Fatal(err)
	}
	<-fake.entered
	if _, err := svc.AuthenticateRuntime(t.Context(), constants.RuntimeIDGrok, "grok.com/oidc"); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("duplicate=%v", err)
	}
	_ = svc.CancelRuntimeAuthentication(t.Context(), constants.RuntimeIDGrok, first.JobID)
	waitAuthenticationState(t, svc, constants.RuntimeAuthenticationStateCancelled)
	second, err := svc.AuthenticateRuntime(t.Context(), constants.RuntimeIDGrok, "grok.com/oidc")
	if err != nil {
		t.Fatal(err)
	}
	<-fake.entered
	_ = svc.CancelRuntimeAuthentication(t.Context(), constants.RuntimeIDGrok, first.JobID)
	current := waitAuthenticationState(t, svc, constants.RuntimeAuthenticationStateAuthenticating)
	if current.JobID != second.JobID {
		t.Fatalf("old cancel changed new task: %+v", current)
	}
	close(fake.finish)
	waitAuthenticationState(t, svc, constants.RuntimeAuthenticationStateSucceeded)
}
func TestRuntimeAuthenticationRetainsSameOwnerAfterCloseFailure(t *testing.T) {
	fake := &authenticationBoundary{capabilities: agent.NativeAuthenticationCapabilities{Methods: []agent.NativeAuthenticationMethod{{ID: "grok.com/oidc", Kind: "agent"}}}, closeFailures: 2}
	svc := authenticationFixture(t, fake)
	var mutex sync.Mutex
	releases, factories := 0, 0
	svc.authentication.before = func(context.Context, string) (func(), error) {
		return func() { mutex.Lock(); releases++; mutex.Unlock() }, nil
	}
	svc.authentication.factory = func(context.Context, agent.AccountRequest, agent.ProviderLaunch) (agent.NativeAuthenticationClient, error) {
		mutex.Lock()
		factories++
		mutex.Unlock()
		return fake, nil
	}
	if _, err := svc.AuthenticateRuntime(t.Context(), constants.RuntimeIDGrok, "grok.com/oidc"); err != nil {
		t.Fatal(err)
	}
	view := waitAuthenticationState(t, svc, constants.RuntimeAuthenticationStateFailed)
	if view.ErrorCode != status_error.RuntimeShutdownFailed.Key() {
		t.Fatalf("close failure=%+v", view)
	}
	mutex.Lock()
	if releases != 0 {
		t.Error("close failure released admission")
	}
	mutex.Unlock()
	if _, err := svc.AuthenticateRuntime(t.Context(), constants.RuntimeIDGrok, "grok.com/oidc"); !errors.Is(err, status_error.RuntimeShutdownFailed) {
		t.Fatalf("replaced unclosed owner: %v", err)
	}
	mutex.Lock()
	if factories != 1 {
		t.Error("factory called before original close succeeded")
	}
	mutex.Unlock()
	if _, err := svc.AuthenticateRuntime(t.Context(), constants.RuntimeIDGrok, "grok.com/oidc"); err != nil {
		t.Fatal(err)
	}
	waitAuthenticationState(t, svc, constants.RuntimeAuthenticationStateSucceeded)
	mutex.Lock()
	defer mutex.Unlock()
	if releases != 2 || factories != 2 {
		t.Fatalf("ownership releases=%d factories=%d", releases, factories)
	}
}
func TestRuntimeAuthenticationCapabilityMatrix(t *testing.T) {
	for _, tc := range []struct {
		// provider 固定真实 CLI 家族。
		provider string
		// id 固定原生方法。
		id string
		// kind 固定原生交互种类。
		kind string
		// want 表示真正的交互登录。
		want bool
		// terminal 表示需要终端或现有 API 配置入口。
		terminal bool
	}{
		{"codex", "chat-gpt", "agent", true, false}, {"codex", "api-key", "agent", false, false}, {"claude", "claudeai", "agent", true, false}, {"cursor", "cursor_login", "agent", true, false}, {"workbuddy", "login", "agent", true, false}, {"qoder", "login", "agent", true, false}, {"qoder-cn", "login", "agent", true, false}, {"gemini", "oauth-personal", "agent", true, false}, {"gemini", "gemini-api-key", "agent", false, false}, {"grok", "grok.com/oidc", "agent", true, false}, {"kimi", "login", "agent", false, true}, {"opencode", "login", "agent", false, true}, {"pi", "pi_terminal_login", "terminal", false, true},
	} {
		t.Run(tc.provider+tc.id, func(t *testing.T) {
			methods, terminal := actionableNativeMethods(tc.provider, agent.NativeAuthenticationCapabilities{Methods: []agent.NativeAuthenticationMethod{{ID: tc.id, Kind: tc.kind}}})
			if (len(methods) == 1) != tc.want || terminal != tc.terminal {
				t.Fatalf("methods=%+v terminal=%v", methods, terminal)
			}
		})
	}
	methods, _ := actionableNativeMethods(constants.RuntimeIDGrok, agent.NativeAuthenticationCapabilities{APIKeyMode: true, Methods: []agent.NativeAuthenticationMethod{{ID: "grok.com/oidc", Kind: "agent"}}})
	if len(methods) != 0 {
		t.Fatal("API mode offered browser login")
	}
}
func TestRuntimeAuthenticationRejectsNonInteractiveMethod(t *testing.T) {
	fake := &authenticationBoundary{capabilities: agent.NativeAuthenticationCapabilities{Methods: []agent.NativeAuthenticationMethod{{ID: "xai.api_key", Kind: "agent"}, {ID: "grok.com/oidc", Kind: "agent"}}}}
	svc := authenticationFixture(t, fake)
	if _, err := svc.AuthenticateRuntime(t.Context(), constants.RuntimeIDGrok, "xai.api_key"); err != nil {
		t.Fatal(err)
	}
	view := waitAuthenticationState(t, svc, constants.RuntimeAuthenticationStateFailed)
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	if len(fake.methods) != 0 || view.ErrorCode != status_error.RuntimePolicyUnsupported.Key() {
		t.Fatalf("API method executed: %+v %v", view, fake.methods)
	}
}
func TestRuntimeAuthenticationStaticProfilePreservesScope(t *testing.T) {
	svc := authenticationFixture(t, &authenticationBoundary{})
	catalog := svc.runtimeCatalog.(*countingRuntimeCatalog)
	profile := agent.RuntimeSummary{ID: "grok-profile", BaseRuntimeID: constants.RuntimeIDGrok, ConfigHome: t.TempDir(), Profile: true}
	profile.PrefixArgs = []string{"--native-profile"}
	catalog.summaries = append(catalog.summaries, profile)
	if err := svc.environments.Ensure(t.Context(), models.RuntimeEnvironment{ID: "profile", RuntimeID: profile.ID, BaseRuntimeID: profile.BaseRuntimeID, PrefixArgs: []string{"--native-profile"}, Detection: agent.DetectionResult{ResolvedBinary: "/fixture/profile"}}); err != nil {
		t.Fatal(err)
	}
	account, launch, _, err := svc.authentication.launch(t.Context(), profile.ID)
	if err != nil || account.NativeConfigHome != profile.ConfigHome || account.AccountID != agent.SystemAccountID || launch.Executable != "/fixture/profile" || len(launch.PrefixArgs) != 1 {
		t.Fatalf("profile=%+v %+v %v", account, launch, err)
	}
}

func TestRuntimeAuthenticationReportsEnvironmentConfigurationReason(t *testing.T) {
	fake := &authenticationBoundary{capabilities: agent.NativeAuthenticationCapabilities{EnvironmentCredential: true, Methods: []agent.NativeAuthenticationMethod{{ID: "grok.com/oidc", Kind: "agent"}}}}
	svc := authenticationFixture(t, fake)
	if _, err := svc.AuthenticationSnapshot(t.Context(), constants.RuntimeIDGrok, false); err != nil {
		t.Fatal(err)
	}
	view := waitAuthenticationState(t, svc, constants.RuntimeAuthenticationStateIdle)
	if view.CanAuthenticate || !view.APIKeySupported || view.ConfigurationReason != constants.RuntimeAuthenticationConfigurationReasonEnvironmentCredentials || view.ErrorCode != "" {
		t.Fatalf("environment was confused with capability failure: %+v", view)
	}
}

func TestRuntimeAuthenticationSharesInstallationAdmission(t *testing.T) {
	svc := authenticationFixture(t, &authenticationBoundary{})
	svc.installationJobs = map[string]*runtimeInstallationJob{constants.RuntimeIDGrok: {snapshot: RuntimeInstallationSnapshot{State: constants.RuntimeInstallationStateInstalling}}}
	if _, err := svc.AuthenticateRuntime(t.Context(), constants.RuntimeIDGrok, "grok.com/oidc"); !errors.Is(err, status_error.RuntimeInstallationBusy) {
		t.Fatalf("installation overlap=%v", err)
	}
	if _, err := svc.AuthenticationSnapshot(t.Context(), constants.RuntimeIDGrok, false); !errors.Is(err, status_error.RuntimeInstallationBusy) {
		t.Fatalf("read-only management process overlapped installation: %v", err)
	}
	svc.installationJobs = nil
	svc.restarting = map[string]bool{constants.RuntimeIDGrok: true}
	if _, err := svc.AuthenticateRuntime(t.Context(), constants.RuntimeIDGrok, "grok.com/oidc"); !errors.Is(err, status_error.RuntimeRestartBusy) {
		t.Fatalf("restart overlap=%v", err)
	}
	if _, err := svc.AuthenticationSnapshot(t.Context(), constants.RuntimeIDGrok, false); !errors.Is(err, status_error.RuntimeRestartBusy) {
		t.Fatalf("read-only management process overlapped restart: %v", err)
	}
}

func TestRuntimeAuthenticationCapabilityFailureRequiresExplicitRefresh(t *testing.T) {
	fake := &authenticationBoundary{initializeError: status_error.RuntimeUnavailable}
	svc := authenticationFixture(t, fake)
	if _, err := svc.AuthenticationSnapshot(t.Context(), constants.RuntimeIDGrok, false); err != nil {
		t.Fatal(err)
	}
	failed := waitAuthenticationState(t, svc, constants.RuntimeAuthenticationStateFailed)
	if failed.JobID != "" || failed.ConfigurationReason != "" || failed.Cancelable || failed.ErrorCode != status_error.RuntimeUnavailable.Key() {
		t.Fatalf("capability failure confused with login/configuration: %+v", failed)
	}
	fake.mutex.Lock()
	fake.initializeError = nil
	fake.capabilities = agent.NativeAuthenticationCapabilities{Methods: []agent.NativeAuthenticationMethod{{ID: "grok.com/oidc", Kind: "agent"}}}
	fake.mutex.Unlock()
	cached, err := svc.AuthenticationSnapshot(t.Context(), constants.RuntimeIDGrok, false)
	if err != nil || cached.State != constants.RuntimeAuthenticationStateFailed {
		t.Fatalf("normal event read started retry loop: %+v %v", cached, err)
	}
	if _, err := svc.AuthenticationSnapshot(t.Context(), constants.RuntimeIDGrok, true); err != nil {
		t.Fatal(err)
	}
	recovered := waitAuthenticationState(t, svc, constants.RuntimeAuthenticationStateIdle)
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	if !recovered.CanAuthenticate || recovered.JobID != "" || fake.initialized != 2 {
		t.Fatalf("explicit retry did not recover: %+v count=%d", recovered, fake.initialized)
	}
}

func TestRuntimeAuthenticationProfilesShareSystemMutationFence(t *testing.T) {
	fake := &authenticationBoundary{capabilities: agent.NativeAuthenticationCapabilities{Methods: []agent.NativeAuthenticationMethod{{ID: "grok.com/oidc", Kind: "agent"}}}, entered: make(chan struct{}, 1), finish: make(chan struct{})}
	svc := authenticationFixture(t, fake)
	profile := agent.RuntimeSummary{ID: "grok-profile", BaseRuntimeID: constants.RuntimeIDGrok}
	catalog := svc.runtimeCatalog.(*countingRuntimeCatalog)
	catalog.summaries = append(catalog.summaries, profile)
	if err := svc.environments.Ensure(t.Context(), models.RuntimeEnvironment{ID: "profile", RuntimeID: profile.ID, BaseRuntimeID: profile.BaseRuntimeID, Detection: agent.DetectionResult{ResolvedBinary: "/fixture/profile"}}); err != nil {
		t.Fatal(err)
	}
	job, err := svc.AuthenticateRuntime(t.Context(), constants.RuntimeIDGrok, "grok.com/oidc")
	if err != nil {
		t.Fatal(err)
	}
	<-fake.entered
	if _, err := svc.AuthenticateRuntime(t.Context(), profile.ID, "grok.com/oidc"); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("profile bypassed mutation fence: %v", err)
	}
	if _, err := svc.AuthenticationSnapshot(t.Context(), profile.ID, false); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("new native capability consumer bypassed mutation fence: %v", err)
	}
	_ = svc.CancelRuntimeAuthentication(t.Context(), constants.RuntimeIDGrok, job.JobID)
	waitAuthenticationState(t, svc, constants.RuntimeAuthenticationStateCancelled)
}

func TestRuntimeAuthenticationReadOnlyHandshakeNeverClaimsMutationJob(t *testing.T) {
	fake := &authenticationBoundary{initializeEntered: make(chan struct{}, 1), initializeRelease: make(chan struct{})}
	var release sync.Once
	defer release.Do(func() { close(fake.initializeRelease) })
	svc := authenticationFixture(t, fake)
	svc.authentication.before = func(context.Context, string) (func(), error) {
		t.Error("GET acquired an authentication mutation fence")
		return func() {}, nil
	}
	view, err := svc.AuthenticationSnapshot(t.Context(), constants.RuntimeIDGrok, false)
	if err != nil {
		t.Fatal(err)
	}
	<-fake.initializeEntered
	if view.State != constants.RuntimeAuthenticationStateChecking || view.JobID != "" || view.Cancelable || svc.authentication.IsAuthenticating(constants.RuntimeIDGrok) || !svc.authentication.HasNativeOwner(constants.RuntimeIDGrok) {
		t.Fatalf("read-only capability lookup locked execution settings: %+v", view)
	}
	current, err := svc.AuthenticationSnapshot(t.Context(), constants.RuntimeIDGrok, false)
	if err != nil || current.JobID != "" || current.Cancelable {
		t.Fatalf("recovered read-only snapshot claimed a mutation: %+v %v", current, err)
	}
	release.Do(func() { close(fake.initializeRelease) })
	waitAuthenticationState(t, svc, constants.RuntimeAuthenticationStateIdle)
	if svc.authentication.HasNativeOwner(constants.RuntimeIDGrok) {
		t.Fatal("completed read-only handshake kept the program maintenance fence")
	}
}

// TestRuntimeAuthenticationPublishesExplicitConnectionResult 验证连接确认不会变成无区别的新授权成功。
func TestRuntimeAuthenticationPublishesExplicitConnectionResult(t *testing.T) {
	for _, result := range []constants.RuntimeAuthenticationResult{constants.RuntimeAuthenticationResultExistingLoginVerified, constants.RuntimeAuthenticationResultConnectionVerified, constants.RuntimeAuthenticationResultAuthenticationCompleted} {
		t.Run(string(result), func(t *testing.T) {
			fake := &authenticationBoundary{result: result, capabilities: agent.NativeAuthenticationCapabilities{Methods: []agent.NativeAuthenticationMethod{{ID: "grok.com/oidc", Kind: "agent"}}}}
			svc := authenticationFixture(t, fake)
			if _, err := svc.AuthenticateRuntime(t.Context(), constants.RuntimeIDGrok, "grok.com/oidc"); err != nil {
				t.Fatal(err)
			}
			completed := waitAuthenticationState(t, svc, constants.RuntimeAuthenticationStateSucceeded)
			if completed.Result != result {
				t.Fatalf("native result was lost: got=%s want=%s", completed.Result, result)
			}
		})
	}
}
