package runtime_test

import (
	"context"
	"errors"
	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	r "github.com/JieWaZi/NeuxBot/agent-runtime/runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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
func spec() c.ExecutionSpec {
	return c.ExecutionSpec{Profile: c.Profile{DriverID: "fake"}, Input: c.Input{Text: "first"}}
}
func answer(ctx context.Context, req c.TurnRequest, sink c.DriverSink) c.DriverResult {
	if err := sink.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted}); err != nil {
		return c.DriverResult{Status: c.StatusCancelled, StopReason: c.StopCancelled}
	}
	m := c.AssistantMessage{MessageID: "answer", Phase: c.PhaseFinal, Delta: req.Input.Text, Complete: true}
	_ = sink.Accept(ctx, c.DriverEvent{Kind: c.EventAssistantMessage, Message: &m})
	return c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn}
}
func started(t *testing.T, d *fakeDriver, configure func(*r.Config)) *r.Runtime {
	t.Helper()
	cfg := r.Config{Drivers: []c.Driver{d}}
	if configure != nil {
		configure(&cfg)
	}
	rt, err := r.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if d.opens.Load() != 0 {
		t.Fatal("constructor started driver")
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
	return rt
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
	one := rt.Invoke(context.Background(), spec())
	if one.Status != c.StatusCompleted || one.Output.Text != "first" || closes.Load() != 1 {
		t.Fatalf("invoke result %#v closes %d", one, closes.Load())
	}
	a := rt.Run(context.Background(), spec(), c.RunOptions{})
	bSpec := spec()
	bSpec.Input.Text = "second"
	bSpec.Session = c.SessionDirective{Mode: c.SessionResume, Ref: a.Invocation.SessionID}
	b := rt.Run(context.Background(), bSpec, c.RunOptions{})
	if b.Status != c.StatusCompleted || b.Output.Text != "second" || b.Invocation.Revision != 2 || d.opens.Load() != 2 {
		t.Fatalf("resume %#v opens %d", b, d.opens.Load())
	}
	inputsMu.Lock()
	defer inputsMu.Unlock()
	if len(inputs) != 3 || inputs[2] != "second" {
		t.Fatalf("inputs %#v", inputs)
	}
	bSpec.Session.Seed = []c.SeedMessage{{Role: "user", Text: "old"}}
	bad := rt.Run(context.Background(), bSpec, c.RunOptions{})
	requireCode(t, bad.Fault, c.FaultInvalidSpec)
}

func TestSameSessionBusyDifferentSessionsParallel(t *testing.T) {
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
	a := rt.Run(context.Background(), spec(), c.RunOptions{})
	block.Store(true)
	resume := spec()
	resume.Session = c.SessionDirective{Mode: c.SessionResume, Ref: a.Invocation.SessionID}
	done := make(chan c.Result, 2)
	go func() { done <- rt.Run(context.Background(), resume, c.RunOptions{}) }()
	<-entered
	busy := rt.Run(context.Background(), resume, c.RunOptions{})
	requireCode(t, busy.Fault, c.FaultSessionBusy)
	go func() { done <- rt.Run(context.Background(), spec(), c.RunOptions{}) }()
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
	rt := started(t, d, func(cfg *r.Config) { cfg.Grants = c.Grants{Tools: []string{"read"}} })
	in := spec()
	in.Policies.Tools = c.ToolPolicy{Mode: c.ToolsAllowList, Allow: []string{"read"}}
	boom := errors.New("observer offline")
	result := rt.Run(context.Background(), in, c.RunOptions{Observer: c.ObserverFunc(func(_ context.Context, e c.Event) error {
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

func (p credentialProvider) Acquire(context.Context, c.CredentialRequest) (c.CredentialLease, error) {
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
	rt := started(t, d, func(cfg *r.Config) {
		cfg.Credentials = credentialProvider{&log, &mu}
		cfg.Resources = resourceProvider{&log, &mu}
	})
	in := spec()
	in.Profile.CredentialRef = "account"
	got := rt.Invoke(context.Background(), in)
	requireCode(t, got.Fault, c.FaultPreparation)
	if len(log) != 2 || log[0] != "resource" || log[1] != "credential" {
		t.Fatalf("release order %#v", log)
	}
}

func TestCancelCloseAndUniquePreallocatedIdentities(t *testing.T) {
	entered := make(chan string, 2)
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: func(ctx context.Context, req c.TurnRequest, s c.DriverSink) c.DriverResult {
			_ = s.Accept(ctx, c.DriverEvent{Kind: c.EventPromptAccepted})
			entered <- req.Invocation.ID
			<-ctx.Done()
			return c.DriverResult{Status: c.StatusCancelled, StopReason: c.StopCancelled}
		}}, nil
	}}
	rt := started(t, d, nil)
	id1, err := rt.NewInvocationID()
	if err != nil {
		t.Fatal(err)
	}
	id2, err := rt.NewInvocationID()
	if err != nil {
		t.Fatal(err)
	}
	if !rt.Cancel(id1) {
		t.Fatal("pending cancellation failed")
	}
	done := make(chan c.Result, 1)
	in := spec()
	in.InvocationID = id2
	go func() { done <- rt.Invoke(context.Background(), in) }()
	<-entered
	if rt.Cancel(id1) {
		t.Fatal("old cancel affected other call")
	}
	if !rt.Cancel(id2) {
		t.Fatal("active cancel missing")
	}
	got := <-done
	if got.Status != c.StatusCancelled {
		t.Fatalf("%#v", got)
	}
	for _, id := range []string{id1, id2} {
		in.InvocationID = id
		requireCode(t, rt.Invoke(context.Background(), in).Fault, c.FaultDuplicateInvocation)
	}
	in.InvocationID = ""
	go func() { done <- rt.Invoke(context.Background(), in) }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rt.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if result := <-done; result.Status != c.StatusCancelled {
		t.Fatalf("%#v", result)
	}
	requireCode(t, rt.Invoke(context.Background(), spec()).Fault, c.FaultClosed)
	if d.closes.Load() != 1 {
		t.Fatalf("driver closes %d", d.closes.Load())
	}
}

func TestNonHotRunReleasesAndDoesNotPretendCheckpoint(t *testing.T) {
	var closed atomic.Int32
	cp := caps()
	cp.HotResume = false
	d := &fakeDriver{caps: cp, open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: answer, close: func(context.Context) error { closed.Add(1); return nil }}, nil
	}}
	rt := started(t, d, nil)
	got := rt.Run(context.Background(), spec(), c.RunOptions{})
	if got.Status != c.StatusCompleted || got.Checkpoint != nil || closed.Load() != 1 {
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
			rt := started(t, d, func(cfg *r.Config) { cfg.Grants = c.Grants{Tools: []string{"read"}} })
			in := spec()
			in.Policies.Tools = c.ToolPolicy{Mode: c.ToolsAllowList, Allow: []string{"read"}}
			in.Limits.MaxEventBytes = 100
			if which == "acceptance" {
				in.Limits.MaxEventBytes = 1
			}
			got := rt.Invoke(context.Background(), in)
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
	rt := started(t, d, func(cfg *r.Config) { cfg.Grants = c.Grants{Tools: []string{"read"}} })
	in := spec()
	in.Policies.Tools = c.ToolPolicy{Mode: c.ToolsAllowList, Allow: []string{"read"}}
	got := rt.Invoke(context.Background(), in)
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
	got := rt.Invoke(context.Background(), spec())
	requireCode(t, got.Fault, c.FaultProtocol)
}

func TestPreallocatedIDsConsumeOutOfOrderAndNeverReenter(t *testing.T) {
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: answer}, nil
	}}
	rt := started(t, d, nil)
	a, err := rt.NewInvocationID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := rt.NewInvocationID()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{b, a} {
		in := spec()
		in.InvocationID = id
		if got := rt.Invoke(context.Background(), in); got.Status != c.StatusCompleted {
			t.Fatalf("out of order pending %#v", got)
		}
		if rt.Cancel(id) {
			t.Fatal("completed identity was cancellable")
		}
		requireCode(t, rt.Invoke(context.Background(), in).Fault, c.FaultDuplicateInvocation)
	}
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
	rt := started(t, d, func(cfg *r.Config) {
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
	go func() { done <- rt.Invoke(context.Background(), spec()) }()
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
	go func() { done <- rt.Invoke(context.Background(), spec()) }()
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
	got := rt.Invoke(context.Background(), spec())
	if got.Status != c.StatusCompleted || got.Output.Status != c.OutputAvailable || got.Output.Text != "first" || got.CleanupFault == nil || !errors.Is(got.CleanupFault, boom) {
		t.Fatalf("%#v", got)
	}
}

func TestHotSessionRetainsLeaseUntilExplicitClose(t *testing.T) {
	var closes atomic.Int32
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: answer}, nil
	}}
	rt := started(t, d, func(cfg *r.Config) {
		cfg.Resources = funcResourceProvider{func(context.Context, c.ResourceRequest) (c.ResourceLease, error) {
			return funcResourceLease{func(context.Context) error { closes.Add(1); return nil }}, nil
		}}
	})
	got := rt.Run(context.Background(), spec(), c.RunOptions{})
	if closes.Load() != 0 {
		t.Fatal("hot session lost lease while driver retained native state")
	}
	if err := rt.CloseSession(context.Background(), got.Invocation.SessionID); err != nil {
		t.Fatal(err)
	}
	if closes.Load() != 1 {
		t.Fatalf("lease close count %d", closes.Load())
	}
}

func TestCloseSessionRespectsCallerWaitDeadline(t *testing.T) {
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
	got := rt.Run(context.Background(), spec(), c.RunOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	returned := make(chan error, 1)
	go func() { returned <- rt.CloseSession(ctx, got.Invocation.SessionID) }()
	<-entered
	select {
	case err := <-returned:
		if err == nil {
			t.Error("close session ignored caller deadline")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("CloseSession did not bound caller wait")
	}
	close(release)
}

func TestSessionCloseFailureDuringActiveRunIsReturned(t *testing.T) {
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
	go func() { done <- rt.Run(context.Background(), spec(), c.RunOptions{}) }()
	ref := <-entered
	err := rt.CloseSession(context.Background(), ref.SessionID)
	if !errors.Is(err, boom) {
		t.Fatalf("CloseSession lost release failure: %v", err)
	}
	got := <-done
	if got.CleanupFault == nil {
		t.Fatal("Run lost cleanup failure")
	}
}

func TestUnchangedInputCanResumeButAccountChangeCannot(t *testing.T) {
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: answer}, nil
	}}
	rt := started(t, d, nil)
	first := rt.Run(context.Background(), spec(), c.RunOptions{})
	in := spec()
	in.Session = c.SessionDirective{Mode: c.SessionResume, Ref: first.Invocation.SessionID}
	in.Profile.Subject = "another"
	got := rt.Run(context.Background(), in, c.RunOptions{})
	requireCode(t, got.Fault, c.FaultSessionMismatch)
	in.Profile.Subject = ""
	got = rt.Run(context.Background(), in, c.RunOptions{})
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
	got := rt.Run(context.Background(), in, c.RunOptions{Observer: c.ObserverFunc(func(ctx context.Context, _ c.Event) error { <-ctx.Done(); return ctx.Err() })})
	requireCode(t, got.DeliveryFault, c.FaultDelivery)
	got = rt.Invoke(context.Background(), in)
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
	rt := started(t, d, func(cfg *r.Config) {
		cfg.MaxOpenSessions = 1
		cfg.Resources = funcResourceProvider{func(context.Context, c.ResourceRequest) (c.ResourceLease, error) {
			return funcResourceLease{func(context.Context) error { leaseCloses.Add(1); return nil }}, nil
		}}
	})
	got := rt.Invoke(context.Background(), spec())
	if got.Status != c.StatusCompleted || !errors.Is(got.CleanupFault, boom) {
		t.Fatalf("%#v", got)
	}
	if leaseCloses.Load() != 0 {
		t.Fatal("lease released while native consumer may still be running")
	}
	requireCode(t, rt.Invoke(context.Background(), spec()).Fault, c.FaultCapacity)
	if err := rt.CloseSession(context.Background(), got.Invocation.SessionID); err != nil {
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
	rt := started(t, d, func(cfg *r.Config) {
		cfg.Resources = funcResourceProvider{func(context.Context, c.ResourceRequest) (c.ResourceLease, error) {
			return funcResourceLease{func(context.Context) error {
				if leaseCloses.Add(1) == 1 {
					return boom
				}
				return nil
			}}, nil
		}}
	})
	got := rt.Invoke(context.Background(), spec())
	if got.CleanupFault == nil {
		t.Fatal("expected retained failed lease")
	}
	if err := rt.CloseSession(context.Background(), got.Invocation.SessionID); err != nil {
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
	got := rt.Run(context.Background(), in, c.RunOptions{InteractionHandler: handler})
	if got.Status != c.StatusCompleted {
		t.Fatalf("observer deadline incorrectly limited user input: %#v", got)
	}
	in.Limits.InteractionTimeout = time.Millisecond
	got = rt.Run(context.Background(), in, c.RunOptions{InteractionHandler: handler})
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
			rt := started(t, d, func(cfg *r.Config) {
				cfg.Grants = c.Grants{Tools: []string{"read"}, AllowAllTools: mode == c.ToolsAllowAll}
			})
			in := spec()
			in.Policies.Tools = c.ToolPolicy{Mode: mode, Allow: []string{"read"}}
			got := rt.Invoke(context.Background(), in)
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
	rt := started(t, d, func(cfg *r.Config) { cfg.Grants = c.Grants{Tools: []string{"read"}} })
	in := spec()
	in.Policies.Tools = c.ToolPolicy{Mode: c.ToolsAllowList, Allow: []string{"read"}}
	got := rt.Invoke(context.Background(), in)
	requireCode(t, got.Fault, c.FaultProtocol)
}

func TestRuntimeCloseCanRetryWithoutRepeatingSuccessfulRelease(t *testing.T) {
	var resourceCloses, sessionCloses atomic.Int32
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: answer, close: func(context.Context) error { sessionCloses.Add(1); return nil }}, nil
	}}
	rt := started(t, d, func(cfg *r.Config) {
		cfg.Resources = funcResourceProvider{func(context.Context, c.ResourceRequest) (c.ResourceLease, error) {
			return funcResourceLease{func(context.Context) error {
				if resourceCloses.Add(1) == 1 {
					return errors.New("resource still active")
				}
				return nil
			}}, nil
		}}
	})
	got := rt.Run(context.Background(), spec(), c.RunOptions{})
	if got.Status != c.StatusCompleted {
		t.Fatalf("%#v", got)
	}
	if err := rt.Close(context.Background()); err == nil {
		t.Fatal("first close should preserve unresolved lease")
	}
	if d.closes.Load() != 0 {
		t.Fatal("driver released before retained session dependencies")
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
	got := rt.Run(context.Background(), in, c.RunOptions{InteractionHandler: interactionFunc(func(ctx context.Context, req c.InteractionRequest) (c.InteractionResponse, error) {
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
	got := rt.Run(context.Background(), in, c.RunOptions{InteractionHandler: interactionFunc(func(_ context.Context, req c.InteractionRequest) (c.InteractionResponse, error) {
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
		in.Session.Seed = seed
		got := rt.Run(context.Background(), in, c.RunOptions{})
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
			got := rt.Run(ctx, spec(), c.RunOptions{Observer: c.ObserverFunc(func(context.Context, c.Event) error { observerCalls.Add(1); return nil })})
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
	got := rt.Run(context.Background(), spec(), c.RunOptions{Observer: c.ObserverFunc(func(context.Context, c.Event) error { return boom })})
	requireCode(t, got.DeliveryFault, c.FaultDelivery)
	if !got.Effects.PromptAccepted || !got.Effects.ToolActivity || !got.Effects.OutcomeUnknown {
		t.Fatalf("first fault suppressed drained evidence: %#v", got.Effects)
	}
	if !errors.Is(got.Fault, boom) {
		t.Fatalf("first fault replaced: %#v", got.Fault)
	}
}
