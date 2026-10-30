package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/internal/plan"
	r "github.com/JieWaZi/NeuxBot/server/internal/agent/core/runtime"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStageDiagnosticsDistinguishColdInitializationFromHotAcceptance(t *testing.T) {
	var logs bytes.Buffer
	driver := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: func(ctx context.Context, request c.TurnRequest, sink c.DriverSink) c.DriverResult {
			result := answer(ctx, request, sink)
			result.NativeSession.Handle = "native-session"
			return result
		}}, nil
	}}
	rt := started(t, driver, func(config *testConfig) {
		config.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	})
	input := spec()
	input.Input.Text = "private-input-never-log"
	for turn := 0; turn < 2; turn++ {
		result := runTurn(rt, context.Background(), input, c.RunOptions{})
		if result.Status != c.StatusCompleted {
			t.Fatalf("turn %d: %+v", turn, result)
		}
		input.Session = c.SessionDirective{Mode: c.SessionResume, NativeHandle: result.NativeSession.Handle}
	}
	if driver.opens.Load() != 1 {
		t.Fatalf("native opens = %d", driver.opens.Load())
	}
	var stages []string
	var reused []bool
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		stage, _ := entry["stage"].(string)
		stages = append(stages, stage)
		if stage == "cli_initialization" {
			value, _ := entry["reused"].(bool)
			reused = append(reused, value)
		}
		if entry["invocation_id"] == "" || entry["elapsed_ms"] == nil || entry["success"] != true {
			t.Fatalf("incomplete diagnostic: %s", line)
		}
	}
	if strings.Join(stages, ",") != "cli_initialization,input_acceptance,cli_initialization,input_acceptance" || len(reused) != 2 || reused[0] || !reused[1] {
		t.Fatalf("stages = %v, reused = %v", stages, reused)
	}
	if strings.Contains(logs.String(), input.Input.Text) {
		t.Fatal("diagnostic leaked input")
	}
}

func TestStageDiagnosticsRecordFailedInitializationWithoutInputOrRawError(t *testing.T) {
	var logs bytes.Buffer
	driver := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return nil, errors.New("private-native-error")
	}}
	rt := started(t, driver, func(config *testConfig) {
		config.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	})
	input := spec()
	input.Input.Text = "private-failed-input"
	result := runTurn(rt, context.Background(), input, c.RunOptions{})
	if result.Status != c.StatusFailed {
		t.Fatalf("result = %+v", result)
	}
	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["stage"] != "cli_initialization" || entry["success"] != false || entry["reused"] != false {
		t.Fatalf("diagnostic = %s", logs.String())
	}
	if strings.Contains(logs.String(), input.Input.Text) || strings.Contains(logs.String(), "private-native-error") {
		t.Fatal("diagnostic leaked input or raw error")
	}
}

type fakeDriver struct {
	caps   c.DriverCapabilities                                          // caps 是明确支持的能力。
	open   func(context.Context, c.OpenRequest) (c.DriverSession, error) // open 控制外部准备边界。
	opens  atomic.Int32                                                  // opens 统计真实准备次数。
	closes atomic.Int32                                                  // closes 统计驱动关闭次数。
}

func (d *fakeDriver) ID() string                         { return "fake" }
func (d *fakeDriver) Capabilities() c.DriverCapabilities { return d.caps }
func (d *fakeDriver) Open(ctx context.Context, req c.OpenRequest) (c.DriverSession, error) {
	d.opens.Add(1)
	return d.open(ctx, req)
}
func (d *fakeDriver) Close(context.Context) error { d.closes.Add(1); return nil }

type fakeSession struct {
	prompt func(context.Context, c.TurnRequest, c.DriverSink) c.DriverResult // prompt 模拟原生回合。
	close  func(context.Context) error                                       // close 记录真实资源释放。
}

func (s *fakeSession) Prompt(ctx context.Context, req c.TurnRequest, sink c.DriverSink) c.DriverResult {
	return s.prompt(ctx, req, sink)
}
func (s *fakeSession) Close(ctx context.Context) error {
	if s.close != nil {
		return s.close(ctx)
	}
	return nil
}
func caps() c.DriverCapabilities {
	return c.DriverCapabilities{Version: "test", ExplicitConfig: true, DenyAllTools: true, ToolAllowList: true, HotResume: true}
}
func spec() plan.Spec {
	return plan.Spec{Profile: c.Profile{DriverID: "fake"}, Input: c.Input{Text: "first"}}
}
func answer(ctx context.Context, req c.TurnRequest, sink c.DriverSink) c.DriverResult {
	if err := sink.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted}); err != nil {
		return c.DriverResult{Status: c.StatusCancelled, StopReason: c.StopCancelled}
	}
	m := c.AssistantMessage{MessageID: "answer", Phase: c.PhaseFinal, Delta: req.Input.Text, Complete: true}
	_ = sink.Accept(ctx, c.DriverEvent{Kind: c.EventAssistantMessage, Message: &m})
	return c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn}
}

// testCredentialRequest 是调用方取得测试账号资源的本地输入。
type testCredentialRequest struct{}

// testCredentialLease 是测试调用方取得的可关闭账号资源。
type testCredentialLease interface {
	Credential() c.Credential
	Close(context.Context) error
}

// testCredentialProvider 只用于模拟 Core 之外的顺序账号准备。
type testCredentialProvider interface {
	Acquire(context.Context, testCredentialRequest) (testCredentialLease, error)
}

// testConfig 只在测试中组装顺序取得的外部资源，不扩展生产 Runtime 契约。
type testConfig struct {
	r.Config                           // Config 是待验证的引擎预算。
	Resources   c.ResourceProvider     // Resources 模拟调用方取得领域资源。
	Credentials testCredentialProvider // Credentials 模拟调用方取得账号租约。
	Grants      c.Grants               // Grants 是测试请求的授权上界。
}

// testRuntime 把测试输入按真实 staged 顺序提交，技术所有权只在嵌入的 Runtime。
type testRuntime struct {
	*r.Runtime            // Runtime 是实际受测的唯一内核。
	config     testConfig // config 是调用方冻结的测试依赖。
	driver     c.Driver   // driver 是本次冷路径交接的驱动。
}

func started(t *testing.T, d *fakeDriver, configure func(*testConfig)) *testRuntime {
	t.Helper()
	cfg := testConfig{}
	if configure != nil {
		configure(&cfg)
	}
	rt, err := r.New(cfg.Config)
	if err != nil {
		t.Fatal(err)
	}

	if err = rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := rt.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return &testRuntime{Runtime: rt, config: cfg, driver: d}
}

// testLease 以取得顺序保存账号及领域资源，释放失败保留栈顶和依赖。
type testLease struct {
	resources c.ResourceLease               // resources 提供真实测试资源视图。
	closers   []func(context.Context) error // closers 保护已经取得的租约。
}

func (l *testLease) Resources() []c.ResourceBinding {
	if l.resources != nil {
		return l.resources.Resources()
	}
	return nil
}
func (l *testLease) Close(ctx context.Context) error {
	for len(l.closers) > 0 {
		last := len(l.closers) - 1
		if err := l.closers[last](ctx); err != nil {
			return err
		}
		l.closers = l.closers[:last]
	}
	return nil
}
func runTurn(rt *testRuntime, ctx context.Context, spec plan.Spec, options c.RunOptions) c.Result {
	return submitTurn(rt, ctx, spec, options, c.SessionPersistent)
}
func invokeTurn(rt *testRuntime, ctx context.Context, spec plan.Spec) c.Result {
	return submitTurn(rt, ctx, spec, c.RunOptions{}, c.SessionTemporary)
}
func submitTurn(rt *testRuntime, ctx context.Context, spec plan.Spec, options c.RunOptions, lifetime c.SessionLifetime) c.Result {
	declaration := c.SessionPlan{Profile: spec.Profile, Workspace: spec.Workspace, Contributions: spec.Contributions, Policies: c.SessionPolicies{Tools: spec.Policies.Tools, NativeConfig: spec.Policies.NativeConfig}, Grants: rt.config.Grants}
	directive := spec.Session
	request := c.PrepareRequest{Plan: declaration, Session: directive, Lifetime: lifetime}
	prepared, err := rt.Prepare(ctx, request)
	result := c.Result{Status: c.StatusFailed, StopReason: c.StopFailed, Output: c.Output{Status: c.OutputUnavailable}}
	if err != nil {
		var f *c.Fault
		if !errors.As(err, &f) {
			f = &c.Fault{Code: c.FaultPreparation, Cause: err}
		}
		result.Fault = f
		return result
	}
	result.Invocation = prepared.Invocation()
	defer prepared.Close(context.Background())
	if prepared.NeedsResources() {
		lease := &testLease{}
		actual := c.SessionResources{Driver: rt.driver, Resources: lease}
		var acquireErr error
		if rt.config.Credentials != nil {
			var credential testCredentialLease
			credential, acquireErr = rt.config.Credentials.Acquire(prepared.Context(), testCredentialRequest{})
			if credential != nil {
				lease.closers = append(lease.closers, credential.Close)
				actual.Credential = credential.Credential()
			}
		}
		if acquireErr == nil && rt.config.Resources != nil {
			lease.resources, acquireErr = rt.config.Resources.Acquire(prepared.Context(), c.ResourceRequest{Invocation: prepared.Invocation(), Workspace: declaration.Workspace, Contributions: declaration.Contributions})
			if lease.resources != nil {
				lease.closers = append(lease.closers, lease.resources.Close)
			}
		}
		bindErr := prepared.Bind(context.Background(), actual)
		if acquireErr != nil {
			result.Fault = &c.Fault{Code: c.FaultPreparation, Cause: acquireErr}
			if cleanup := prepared.Close(context.Background()); cleanup != nil {
				errors.As(cleanup, &result.CleanupFault)
			}
			return result
		}
		if bindErr != nil {
			errors.As(bindErr, &result.Fault)
			if result.Fault.Code == c.FaultCancelled {
				result.Status = c.StatusCancelled
				result.StopReason = c.StopCancelled
			}
			return result
		}
	}
	return prepared.Run(ctx, c.TurnInput{Input: spec.Input, Seed: spec.Seed, Limits: spec.Limits, Interaction: spec.Policies.Interaction, Output: spec.Policies.Output, Options: options})
}
func requireCode(t *testing.T, got *c.Fault, code c.FaultCode) {
	t.Helper()
	if got == nil || got.Code != code {
		t.Fatalf("fault=%#v want %s", got, code)
	}
}

func TestInvokeIsTemporaryAndHotRunResumesOnlyIncrement(t *testing.T) {
	var closes atomic.Int32
	var inputsMu sync.Mutex
	var inputs []string
	d := &fakeDriver{caps: caps(), open: func(_ context.Context, req c.OpenRequest) (c.DriverSession, error) {
		if req.Session.Mode != c.SessionNew {
			t.Error("open must create only once")
		}
		return &fakeSession{prompt: func(ctx context.Context, req c.TurnRequest, s c.DriverSink) c.DriverResult {
			inputsMu.Lock()
			inputs = append(inputs, req.Input.Text)
			inputsMu.Unlock()
			return answer(ctx, req, s)
		}, close: func(context.Context) error { closes.Add(1); return nil }}, nil
	}}
	rt := started(t, d, nil)
	one := invokeTurn(rt, context.Background(), spec())
	if one.Status != c.StatusCompleted || one.Output.Text != "first" || closes.Load() != 1 {
		t.Fatalf("invoke result %#v closes %d", one, closes.Load())
	}
	a := runTurn(rt, context.Background(), spec(), c.RunOptions{})
	bSpec := spec()
	bSpec.Input.Text = "second"
	bSpec.Session = c.SessionDirective{Mode: c.SessionResume, NativeHandle: a.NativeSession.Handle}
	b := runTurn(rt, context.Background(), bSpec, c.RunOptions{})
	if b.Status != c.StatusCompleted || b.Output.Text != "second" || b.Invocation.Revision != 2 || d.opens.Load() != 2 {
		t.Fatalf("resume %#v opens %d", b, d.opens.Load())
	}
	inputsMu.Lock()
	if len(inputs) != 3 || inputs[2] != "second" {
		t.Fatalf("inputs %#v", inputs)
	}
	inputsMu.Unlock()
	bSpec.Seed = []c.SeedMessage{{Role: "user", Text: "old"}}
	continued := runTurn(rt, context.Background(), bSpec, c.RunOptions{})
	if continued.Status != c.StatusCompleted || d.opens.Load() != 2 {
		t.Fatalf("hot seed replay=%+v opens=%d", continued, d.opens.Load())
	}
}

func TestSameRuntimeBusyIndependentRuntimesParallel(t *testing.T) {
	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	var block atomic.Bool
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: func(ctx context.Context, req c.TurnRequest, s c.DriverSink) c.DriverResult {
			if block.Load() {
				entered <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			return answer(ctx, req, s)
		}}, nil
	}}
	rt := started(t, d, nil)
	a := runTurn(rt, context.Background(), spec(), c.RunOptions{})
	block.Store(true)
	resume := spec()
	resume.Session = c.SessionDirective{Mode: c.SessionResume, NativeHandle: a.NativeSession.Handle}
	done := make(chan c.Result, 2)
	go func() { done <- runTurn(rt, context.Background(), resume, c.RunOptions{}) }()
	<-entered
	busy := runTurn(rt, context.Background(), resume, c.RunOptions{})
	requireCode(t, busy.Fault, c.FaultSessionBusy)
	other := started(t, d, nil)
	go func() { done <- runTurn(other, context.Background(), spec(), c.RunOptions{}) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("different session was serialized")
	}
	close(release)
	for range 2 {
		if result := <-done; result.Status != c.StatusCompleted {
			t.Fatalf("%#v", result)
		}
	}
}

func TestObserverFailurePreservesEffectsAndNeverRetries(t *testing.T) {
	var prompts atomic.Int32
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: func(ctx context.Context, req c.TurnRequest, s c.DriverSink) c.DriverResult {
			prompts.Add(1)
			_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted})
			_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventTool, Tool: &c.ToolEvent{ID: "1", Name: "read"}})
			return c.DriverResult{Status: c.StatusCancelled, StopReason: c.StopCancelled}
		}}, nil
	}}
	rt := started(t, d, func(cfg *testConfig) { cfg.Grants = c.Grants{Tools: []string{"read"}} })
	in := spec()
	in.Policies.Tools = c.ToolPolicy{Mode: c.ToolsAllowList, Allow: []string{"read"}}
	boom := errors.New("observer offline")
	result := runTurn(rt, context.Background(), in, c.RunOptions{Observer: c.ObserverFunc(func(_ context.Context, e c.Event) error {
		if e.DriverEvent.Kind == c.EventTool {
			return boom
		}
		return nil
	})})
	requireCode(t, result.DeliveryFault, c.FaultDelivery)
	if !result.Effects.PromptAccepted || !result.Effects.ToolActivity || !result.Effects.OutcomeUnknown || prompts.Load() != 1 {
		t.Fatalf("effects %#v calls %d", result, prompts.Load())
	}
}

type credentialProvider struct {
	log *[]string   // log 是释放次序记录。
	mu  *sync.Mutex // mu 保护次序记录。
}

func (p credentialProvider) Acquire(context.Context, testCredentialRequest) (testCredentialLease, error) {
	return credentialLease(p), nil
}

type credentialLease credentialProvider

func (p credentialLease) Credential() c.Credential {
	return c.Credential{Environment: map[string]string{"TOKEN": "secret"}}
}
func (p credentialLease) Close(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	*p.log = append(*p.log, "credential")
	return nil
}

type resourceProvider struct {
	log *[]string   // log 是释放次序记录。
	mu  *sync.Mutex // mu 保护次序记录。
}

func (p resourceProvider) Acquire(context.Context, c.ResourceRequest) (c.ResourceLease, error) {
	return resourceLease(p), errors.New("materialization failed")
}

type resourceLease resourceProvider

func (p resourceLease) Resources() []c.ResourceBinding { return nil }
func (p resourceLease) Close(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	*p.log = append(*p.log, "resource")
	return nil
}
func TestPreparationFailureReleasesReverseEvenReturnedLease(t *testing.T) {
	var log []string
	var mu sync.Mutex
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		t.Fatal("open after preparation failure")
		return nil, nil
	}}
	rt := started(t, d, func(cfg *testConfig) {
		cfg.Credentials = credentialProvider{&log, &mu}
		cfg.Resources = resourceProvider{&log, &mu}
	})
	in := spec()
	got := invokeTurn(rt, context.Background(), in)
	requireCode(t, got.Fault, c.FaultPreparation)
	if len(log) != 2 || log[0] != "resource" || log[1] != "credential" {
		t.Fatalf("release order %#v", log)
	}
}

func TestRunContextCancellationAndRuntimeCloseStopCurrentConsumer(t *testing.T) {
	entered := make(chan struct{}, 2)
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: func(ctx context.Context, turn c.TurnRequest, sink c.DriverSink) c.DriverResult {
			_ = sink.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted})
			entered <- struct{}{}
			<-ctx.Done()
			return c.DriverResult{Status: c.StatusCancelled, StopReason: c.StopCancelled}
		}}, nil
	}}
	rt := started(t, d, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan c.Result, 1)
	go func() { done <- invokeTurn(rt, ctx, spec()) }()
	<-entered
	cancel()
	if result := <-done; result.Status != c.StatusCancelled {
		t.Fatalf("context cancellation=%+v", result)
	}
	go func() { done <- invokeTurn(rt, context.Background(), spec()) }()
	<-entered
	if err := rt.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if result := <-done; result.Status != c.StatusCancelled {
		t.Fatalf("runtime cancellation=%+v", result)
	}
	requireCode(t, invokeTurn(rt, context.Background(), spec()).Fault, c.FaultClosed)
}

func TestNonHotRunReleasesConsumer(t *testing.T) {
	var closed atomic.Int32
	cp := caps()
	cp.HotResume = false
	d := &fakeDriver{caps: cp, open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: answer, close: func(context.Context) error { closed.Add(1); return nil }}, nil
	}}
	rt := started(t, d, nil)
	got := runTurn(rt, context.Background(), spec(), c.RunOptions{})
	if got.Status != c.StatusCompleted || closed.Load() != 1 {
		t.Fatalf("result %#v close %d", got, closed.Load())
	}
}

func TestBudgetFailureStillPreservesRealDeliveryFacts(t *testing.T) {
	for _, which := range []string{"acceptance", "tool"} {
		t.Run(which, func(t *testing.T) {
			d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
				return &fakeSession{prompt: func(ctx context.Context, _ c.TurnRequest, s c.DriverSink) c.DriverResult {
					_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted})
					if which == "tool" {
						_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventTool, Tool: &c.ToolEvent{ID: "1", Name: "read", Text: strings.Repeat("x", 1000)}})
					}
					return c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn}
				}}, nil
			}}
			rt := started(t, d, func(cfg *testConfig) { cfg.Grants = c.Grants{Tools: []string{"read"}} })
			in := spec()
			in.Policies.Tools = c.ToolPolicy{Mode: c.ToolsAllowList, Allow: []string{"read"}}
			in.Limits.MaxEventBytes = 100
			if which == "acceptance" {
				in.Limits.MaxEventBytes = 1
			}
			got := invokeTurn(rt, context.Background(), in)
			requireCode(t, got.Fault, c.FaultLimit)
			if !got.Effects.PromptAccepted || !got.Effects.OutcomeUnknown || (which == "tool" && !got.Effects.ToolActivity) {
				t.Fatalf("real facts lost %#v", got.Effects)
			}
		})
	}
}

func TestDriverCannotMutateEffectiveToolAuthorization(t *testing.T) {
	d := &fakeDriver{caps: caps(), open: func(_ context.Context, req c.OpenRequest) (c.DriverSession, error) {
		req.Policies.Tools.Allow[0] = "delete"
		return &fakeSession{prompt: func(ctx context.Context, _ c.TurnRequest, s c.DriverSink) c.DriverResult {
			_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted})
			_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventTool, Tool: &c.ToolEvent{ID: "1", Name: "delete"}})
			return c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn}
		}}, nil
	}}
	rt := started(t, d, func(cfg *testConfig) { cfg.Grants = c.Grants{Tools: []string{"read"}} })
	in := spec()
	in.Policies.Tools = c.ToolPolicy{Mode: c.ToolsAllowList, Allow: []string{"read"}}
	got := invokeTurn(rt, context.Background(), in)
	requireCode(t, got.Fault, c.FaultProtocol)
	if got.Status == c.StatusCompleted {
		t.Fatal("driver rewrote runtime authorization")
	}
}

func TestDriverCannotClaimCompletionWithoutDelivery(t *testing.T) {
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: func(context.Context, c.TurnRequest, c.DriverSink) c.DriverResult {
			return c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn}
		}}, nil
	}}
	rt := started(t, d, nil)
	got := invokeTurn(rt, context.Background(), spec())
	requireCode(t, got.Fault, c.FaultProtocol)
}

type funcResourceProvider struct {
	acquire func(context.Context, c.ResourceRequest) (c.ResourceLease, error) // acquire 是测试中的物化外部边界。
}

func (p funcResourceProvider) Acquire(ctx context.Context, req c.ResourceRequest) (c.ResourceLease, error) {
	return p.acquire(ctx, req)
}

type funcResourceLease struct {
	close func(context.Context) error // close 是测试中的真实释放边界。
}

func (p funcResourceLease) Resources() []c.ResourceBinding  { return nil }
func (p funcResourceLease) Close(ctx context.Context) error { return p.close(ctx) }

func TestCloseWhilePreparingReleasesLeaseExactlyOnce(t *testing.T) {
	entered := make(chan struct{})
	var closes atomic.Int32
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		t.Error("open after cancelled preparation")
		return &fakeSession{prompt: answer}, nil
	}}
	rt := started(t, d, func(cfg *testConfig) {
		cfg.Resources = funcResourceProvider{func(ctx context.Context, _ c.ResourceRequest) (c.ResourceLease, error) {
			close(entered)
			<-ctx.Done()
			return funcResourceLease{func(cleanup context.Context) error {
				if cleanup.Err() != nil {
					t.Error("cleanup reused cancelled invocation context")
				}
				closes.Add(1)
				return nil
			}}, nil
		}}
	})
	done := make(chan c.Result, 1)
	go func() { done <- invokeTurn(rt, context.Background(), spec()) }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rt.Close(ctx); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.Status != c.StatusCancelled || closes.Load() != 1 {
		t.Fatalf("result %#v closes %d", got, closes.Load())
	}
}

func TestCloseTimeoutCanBeRetriedWithoutDuplicateRelease(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var closes atomic.Int32
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: func(ctx context.Context, _ c.TurnRequest, s c.DriverSink) c.DriverResult {
			_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted})
			close(entered)
			<-release
			return c.DriverResult{Status: c.StatusCancelled, StopReason: c.StopCancelled}
		}, close: func(context.Context) error { closes.Add(1); return nil }}, nil
	}}
	rt := started(t, d, nil)
	done := make(chan c.Result, 1)
	go func() { done <- invokeTurn(rt, context.Background(), spec()) }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if err := rt.Close(ctx); err == nil {
		t.Fatal("close did not respect caller deadline")
	}
	close(release)
	<-done
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := rt.Close(ctx2); err != nil {
		t.Fatal(err)
	}
	if closes.Load() != 1 || d.closes.Load() != 1 {
		t.Fatalf("session closes %d driver closes %d", closes.Load(), d.closes.Load())
	}
}

func TestCleanupFailureDoesNotEraseCompletedAnswer(t *testing.T) {
	boom := errors.New("close failed")
	var attempts atomic.Int32
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: answer, close: func(context.Context) error {
			if attempts.Add(1) == 1 {
				return boom
			}
			return nil
		}}, nil
	}}
	rt := started(t, d, nil)
	got := invokeTurn(rt, context.Background(), spec())
	if got.Status != c.StatusCompleted || got.Output.Status != c.OutputAvailable || got.Output.Text != "first" || got.CleanupFault == nil || !errors.Is(got.CleanupFault, boom) {
		t.Fatalf("%#v", got)
	}
}

func TestHotSessionRetainsLeaseUntilExplicitClose(t *testing.T) {
	var closes atomic.Int32
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: answer}, nil
	}}
	rt := started(t, d, func(cfg *testConfig) {
		cfg.Resources = funcResourceProvider{func(context.Context, c.ResourceRequest) (c.ResourceLease, error) {
			return funcResourceLease{func(context.Context) error { closes.Add(1); return nil }}, nil
		}}
	})
	_ = runTurn(rt, context.Background(), spec(), c.RunOptions{})
	if closes.Load() != 0 {
		t.Fatal("hot session lost lease while driver retained native state")
	}
	if err := rt.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if closes.Load() != 1 {
		t.Fatalf("lease close count %d", closes.Load())
	}
}

func TestRuntimeCloseRespectsCallerWaitDeadline(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: answer, close: func(ctx context.Context) error {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return nil
		}}, nil
	}}
	rt := started(t, d, nil)
	_ = runTurn(rt, context.Background(), spec(), c.RunOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	returned := make(chan error, 1)
	go func() { returned <- rt.Close(ctx) }()
	<-entered
	select {
	case err := <-returned:
		if err == nil {
			t.Error("close session ignored caller deadline")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Runtime.Close did not bound caller wait")
	}
	close(release)
}

func TestRuntimeCloseRetriesActiveRunCleanupWithoutErasingFault(t *testing.T) {
	entered := make(chan c.InvocationRef, 1)
	boom := errors.New("session close failed")
	var attempts atomic.Int32
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: func(ctx context.Context, req c.TurnRequest, s c.DriverSink) c.DriverResult {
			_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted})
			entered <- req.Invocation
			<-ctx.Done()
			return c.DriverResult{Status: c.StatusCancelled, StopReason: c.StopCancelled}
		}, close: func(context.Context) error {
			if attempts.Add(1) == 1 {
				return boom
			}
			return nil
		}}, nil
	}}
	rt := started(t, d, nil)
	done := make(chan c.Result, 1)
	go func() { done <- runTurn(rt, context.Background(), spec(), c.RunOptions{}) }()
	<-entered
	if err := rt.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if !errors.Is(got.CleanupFault, boom) || attempts.Load() != 2 {
		t.Fatalf("run cleanup=%v attempts=%d", got.CleanupFault, attempts.Load())
	}
}

func TestUnchangedInputCanResumeButAccountChangeCannot(t *testing.T) {
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: answer}, nil
	}}
	rt := started(t, d, nil)
	first := runTurn(rt, context.Background(), spec(), c.RunOptions{})
	in := spec()
	in.Session = c.SessionDirective{Mode: c.SessionResume, NativeHandle: first.NativeSession.Handle}
	in.Profile.Subject = "another"
	got := runTurn(rt, context.Background(), in, c.RunOptions{})
	if got.Status != c.StatusCompleted || d.opens.Load() != 2 {
		t.Fatalf("changed account did not create a fresh session: %+v", got)
	}
	in.Profile.Subject = ""
	got = runTurn(rt, context.Background(), in, c.RunOptions{})
	if got.Status != c.StatusCompleted {
		t.Fatalf("unchanged input resume %#v", got)
	}
}

func TestObserverDeadlineAndInteractionRequiredAreDistinct(t *testing.T) {
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: func(ctx context.Context, req c.TurnRequest, s c.DriverSink) c.DriverResult {
			if err := s.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted}); err == nil {
				_, _ = s.Interact(ctx, c.InteractionRequest{ID: "question", Kind: c.InteractionInput, Prompt: "which one?"})
			}
			return c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn}
		}}, nil
	}}
	rt := started(t, d, nil)
	in := spec()
	in.Limits.DeliveryTimeout = 5 * time.Millisecond
	got := runTurn(rt, context.Background(), in, c.RunOptions{Observer: c.ObserverFunc(func(ctx context.Context, _ c.Event) error { <-ctx.Done(); return ctx.Err() })})
	requireCode(t, got.DeliveryFault, c.FaultDelivery)
	got = invokeTurn(rt, context.Background(), in)
	requireCode(t, got.Fault, c.FaultInteractionRequired)
	if got.StopReason != c.StopInteractionRequired {
		t.Fatalf("%#v", got)
	}
}

func TestNativeCloseFailureRetainsDependentLeaseAndCapacityUntilRetry(t *testing.T) {
	var nativeCloses, leaseCloses atomic.Int32
	boom := errors.New("native still running")
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: answer, close: func(context.Context) error {
			if nativeCloses.Add(1) == 1 {
				return boom
			}
			return nil
		}}, nil
	}}
	rt := started(t, d, func(cfg *testConfig) {
		cfg.Resources = funcResourceProvider{func(context.Context, c.ResourceRequest) (c.ResourceLease, error) {
			return funcResourceLease{func(context.Context) error { leaseCloses.Add(1); return nil }}, nil
		}}
	})
	got := invokeTurn(rt, context.Background(), spec())
	if got.Status != c.StatusCompleted || !errors.Is(got.CleanupFault, boom) {
		t.Fatalf("%#v", got)
	}
	if leaseCloses.Load() != 0 {
		t.Fatal("lease released while native consumer may still be running")
	}
	// 显式关闭重试保留的同一消费者，未开放第二条取得资源路径。
	if err := rt.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if leaseCloses.Load() != 1 || nativeCloses.Load() != 2 {
		t.Fatalf("native %d lease %d", nativeCloses.Load(), leaseCloses.Load())
	}
}

func TestCleanupRetryDoesNotRepeatAlreadySuccessfulNativeClose(t *testing.T) {
	var nativeCloses, leaseCloses atomic.Int32
	boom := errors.New("lease close failed")
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: answer, close: func(context.Context) error { nativeCloses.Add(1); return nil }}, nil
	}}
	rt := started(t, d, func(cfg *testConfig) {
		cfg.Resources = funcResourceProvider{func(context.Context, c.ResourceRequest) (c.ResourceLease, error) {
			return funcResourceLease{func(context.Context) error {
				if leaseCloses.Add(1) == 1 {
					return boom
				}
				return nil
			}}, nil
		}}
	})
	got := invokeTurn(rt, context.Background(), spec())
	if got.CleanupFault == nil {
		t.Fatal("expected retained failed lease")
	}
	if err := rt.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if nativeCloses.Load() != 1 || leaseCloses.Load() != 2 {
		t.Fatalf("successful native close repeated: native %d lease %d", nativeCloses.Load(), leaseCloses.Load())
	}
}

type interactionFunc func(context.Context, c.InteractionRequest) (c.InteractionResponse, error)

func (f interactionFunc) Handle(ctx context.Context, req c.InteractionRequest) (c.InteractionResponse, error) {
	return f(ctx, req)
}
func TestInteractionUsesIndependentDeadline(t *testing.T) {
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: func(ctx context.Context, req c.TurnRequest, s c.DriverSink) c.DriverResult {
			_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted})
			_, err := s.Interact(ctx, c.InteractionRequest{ID: "q", Kind: c.InteractionInput, Prompt: "choose"})
			if err != nil {
				return c.DriverResult{Status: c.StatusCancelled, StopReason: c.StopCancelled}
			}
			m := c.AssistantMessage{MessageID: "a", Phase: c.PhaseFinal, Delta: "done"}
			_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventAssistantMessage, Message: &m})
			return c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn}
		}}, nil
	}}
	rt := started(t, d, nil)
	in := spec()
	in.Policies.Interaction = c.InteractionInteractive
	in.Limits.DeliveryTimeout = time.Millisecond
	in.Limits.InteractionTimeout = 100 * time.Millisecond
	handler := interactionFunc(func(ctx context.Context, req c.InteractionRequest) (c.InteractionResponse, error) {
		select {
		case <-time.After(10 * time.Millisecond):
			return c.InteractionResponse{Invocation: req.Invocation, ID: req.ID, Action: c.InteractionAccept, Content: map[string]any{"answer": "yes"}}, nil
		case <-ctx.Done():
			return c.InteractionResponse{}, ctx.Err()
		}
	})
	got := runTurn(rt, context.Background(), in, c.RunOptions{InteractionHandler: handler})
	if got.Status != c.StatusCompleted {
		t.Fatalf("observer deadline incorrectly limited user input: %#v", got)
	}
	in.Limits.InteractionTimeout = time.Millisecond
	got = runTurn(rt, context.Background(), in, c.RunOptions{InteractionHandler: handler})
	requireCode(t, got.Fault, c.FaultInteractionRequired)
}

func TestToolStatusUpdateCanOmitPreviouslyAuthorizedName(t *testing.T) {
	for _, mode := range []c.ToolMode{c.ToolsAllowList, c.ToolsAllowAll} {
		t.Run(string(mode), func(t *testing.T) {
			d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
				return &fakeSession{prompt: func(ctx context.Context, req c.TurnRequest, s c.DriverSink) c.DriverResult {
					_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted})
					_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventTool, Tool: &c.ToolEvent{ID: "call", Name: "read", Status: "started"}})
					_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventTool, Tool: &c.ToolEvent{ID: "call", Status: "completed"}})
					m := c.AssistantMessage{MessageID: "a", Phase: c.PhaseFinal, Delta: "done"}
					_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventAssistantMessage, Message: &m})
					return c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn}
				}}, nil
			}}
			rt := started(t, d, func(cfg *testConfig) {
				cfg.Grants = c.Grants{Tools: []string{"read"}, AllowAllTools: mode == c.ToolsAllowAll}
			})
			in := spec()
			in.Policies.Tools = c.ToolPolicy{Mode: mode, Allow: []string{"read"}}
			got := invokeTurn(rt, context.Background(), in)
			if got.Status != c.StatusCompleted || got.Output.Text != "done" {
				t.Fatalf("legal tool completion rejected: %#v", got)
			}
		})
	}
}

func TestUnknownToolIDCannotBorrowOmittedNameInAllowList(t *testing.T) {
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: func(ctx context.Context, _ c.TurnRequest, s c.DriverSink) c.DriverResult {
			_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted})
			_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventTool, Tool: &c.ToolEvent{ID: "unknown", Status: "completed"}})
			return c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn}
		}}, nil
	}}
	rt := started(t, d, func(cfg *testConfig) { cfg.Grants = c.Grants{Tools: []string{"read"}} })
	in := spec()
	in.Policies.Tools = c.ToolPolicy{Mode: c.ToolsAllowList, Allow: []string{"read"}}
	got := invokeTurn(rt, context.Background(), in)
	requireCode(t, got.Fault, c.FaultProtocol)
}

func TestRuntimeCloseCanRetryWithoutRepeatingSuccessfulRelease(t *testing.T) {
	var resourceCloses, sessionCloses atomic.Int32
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: answer, close: func(context.Context) error { sessionCloses.Add(1); return nil }}, nil
	}}
	rt := started(t, d, func(cfg *testConfig) {
		cfg.Resources = funcResourceProvider{func(context.Context, c.ResourceRequest) (c.ResourceLease, error) {
			return funcResourceLease{func(context.Context) error {
				if resourceCloses.Add(1) == 1 {
					return errors.New("resource still active")
				}
				return nil
			}}, nil
		}}
	})
	got := runTurn(rt, context.Background(), spec(), c.RunOptions{})
	if got.Status != c.StatusCompleted {
		t.Fatalf("%#v", got)
	}
	if err := rt.Close(context.Background()); err == nil {
		t.Fatal("first close should preserve unresolved lease")
	}
	if d.closes.Load() != 1 {
		t.Fatal("driver was not released after its native consumer stopped")
	}
	if err := rt.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if resourceCloses.Load() != 2 || sessionCloses.Load() != 1 || d.closes.Load() != 1 {
		t.Fatalf("resource %d session %d driver %d", resourceCloses.Load(), sessionCloses.Load(), d.closes.Load())
	}
}

func TestCancelledInteractionDoesNotCancelHealthyTurnOrAcceptLateReply(t *testing.T) {
	entered := make(chan struct{})
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: func(ctx context.Context, _ c.TurnRequest, s c.DriverSink) c.DriverResult {
			_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted})
			requestCtx, cancel := context.WithCancel(ctx)
			go func() { <-entered; cancel() }()
			response, err := s.Interact(requestCtx, c.InteractionRequest{ID: "q", Kind: c.InteractionInput})
			if !errors.Is(err, context.Canceled) || response.Action == c.InteractionAccept {
				t.Errorf("revoked interaction accepted or wrongly classified: %#v %v", response, err)
			}
			if ctx.Err() != nil {
				t.Error("single revoked question cancelled healthy invocation")
			}
			m := c.AssistantMessage{MessageID: "a", Phase: c.PhaseFinal, Delta: "continued"}
			_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventAssistantMessage, Message: &m})
			return c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn}
		}}, nil
	}}
	rt := started(t, d, nil)
	in := spec()
	in.Policies.Interaction = c.InteractionInteractive
	in.Limits.InteractionTimeout = 20 * time.Millisecond
	got := runTurn(rt, context.Background(), in, c.RunOptions{InteractionHandler: interactionFunc(func(ctx context.Context, req c.InteractionRequest) (c.InteractionResponse, error) {
		close(entered)
		<-ctx.Done()
		return c.InteractionResponse{Invocation: req.Invocation, ID: req.ID, Action: c.InteractionAccept, Content: map[string]any{"answer": "late"}}, nil
	})})
	if got.Status != c.StatusCompleted || got.Output.Text != "continued" {
		t.Fatalf("healthy turn was cancelled: %#v", got)
	}
}

func TestInteractionHandlerDoesNotBlockOrdinaryEvents(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: func(ctx context.Context, _ c.TurnRequest, s c.DriverSink) c.DriverResult {
			_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted})
			question := make(chan struct{})
			go func() {
				defer close(question)
				_, _ = s.Interact(ctx, c.InteractionRequest{ID: "q", Kind: c.InteractionInput})
			}()
			<-entered
			event := make(chan error, 1)
			go func() { event <- s.Accept(ctx, c.DriverEvent{Kind: c.EventThinking, Text: "progress"}) }()
			select {
			case err := <-event:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(20 * time.Millisecond):
				t.Error("ordinary event blocked behind external interaction handler")
			}
			close(release)
			<-question
			m := c.AssistantMessage{MessageID: "a", Phase: c.PhaseFinal, Delta: "done"}
			_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventAssistantMessage, Message: &m})
			return c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn}
		}}, nil
	}}
	rt := started(t, d, nil)
	in := spec()
	in.Policies.Interaction = c.InteractionInteractive
	got := runTurn(rt, context.Background(), in, c.RunOptions{InteractionHandler: interactionFunc(func(_ context.Context, req c.InteractionRequest) (c.InteractionResponse, error) {
		close(entered)
		<-release
		return c.InteractionResponse{Invocation: req.Invocation, ID: req.ID, Action: c.InteractionAccept, Content: map[string]any{"answer": "yes"}}, nil
	})})
	if got.Status != c.StatusCompleted {
		t.Fatalf("%#v", got)
	}
}

func TestOversizedSeedRejectedBeforeNativePreparation(t *testing.T) {
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		t.Error("oversized seed reached native preparation")
		return &fakeSession{prompt: answer}, nil
	}}
	rt := started(t, d, nil)
	for _, seed := range [][]c.SeedMessage{make([]c.SeedMessage, 41), {{Role: "user", Text: strings.Repeat("中", 21846)}}} {
		for i := range seed {
			if seed[i].Role == "" {
				seed[i].Role = "user"
			}
		}
		in := spec()
		in.Seed = seed
		got := runTurn(rt, context.Background(), in, c.RunOptions{})
		requireCode(t, got.Fault, c.FaultInvalidSpec)
	}
	if d.opens.Load() != 0 {
		t.Fatalf("opens %d", d.opens.Load())
	}
}

func TestCancelledTurnPreservesDrainedDeliveryAndToolFacts(t *testing.T) {
	for _, which := range []string{"prompt accepted during drain", "tool during drain", "tool evidence without accepted event"} {
		t.Run(which, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var observerCalls atomic.Int32
			d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
				return &fakeSession{prompt: func(turnCtx context.Context, _ c.TurnRequest, s c.DriverSink) c.DriverResult {
					if which == "tool during drain" {
						_ = s.Accept(turnCtx, c.DriverEvent{Kind: c.EventPromptAccepted})
					}
					cancel()
					if which == "prompt accepted during drain" {
						_ = s.Accept(turnCtx, c.DriverEvent{Kind: c.EventPromptAccepted})
					} else {
						_ = s.Accept(turnCtx, c.DriverEvent{Kind: c.EventTool, Tool: &c.ToolEvent{ID: "t", Name: "read"}})
					}
					return c.DriverResult{Status: c.StatusCancelled, StopReason: c.StopCancelled}
				}}, nil
			}}
			rt := started(t, d, nil)
			got := runTurn(rt, ctx, spec(), c.RunOptions{Observer: c.ObserverFunc(func(context.Context, c.Event) error { observerCalls.Add(1); return nil })})
			if got.Status != c.StatusCancelled || !got.Effects.OutcomeUnknown {
				t.Fatalf("cancelled drain lost effect uncertainty: %#v", got)
			}
			if which == "prompt accepted during drain" && !got.Effects.PromptAccepted {
				t.Fatal("drained prompt delivery lost")
			}
			if which != "prompt accepted during drain" && !got.Effects.ToolActivity {
				t.Fatal("drained tool activity lost")
			}
			wantCalls := int32(0)
			if which == "tool during drain" {
				wantCalls = 1
			}
			if observerCalls.Load() != wantCalls {
				t.Fatalf("cancelled events delivered to observer: %d", observerCalls.Load())
			}
		})
	}
}

func TestObserverFailureStillPreservesSubsequentlyDrainedToolFacts(t *testing.T) {
	boom := errors.New("journal unavailable")
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: func(ctx context.Context, _ c.TurnRequest, s c.DriverSink) c.DriverResult {
			_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted})
			_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventTool, Tool: &c.ToolEvent{ID: "t", Name: "read"}})
			return c.DriverResult{Status: c.StatusCancelled, StopReason: c.StopCancelled}
		}}, nil
	}}
	rt := started(t, d, nil)
	got := runTurn(rt, context.Background(), spec(), c.RunOptions{Observer: c.ObserverFunc(func(context.Context, c.Event) error { return boom })})
	requireCode(t, got.DeliveryFault, c.FaultDelivery)
	if !got.Effects.PromptAccepted || !got.Effects.ToolActivity || !got.Effects.OutcomeUnknown {
		t.Fatalf("first fault suppressed drained evidence: %#v", got.Effects)
	}
	if !errors.Is(got.Fault, boom) {
		t.Fatalf("first fault replaced: %#v", got.Fault)
	}
}
