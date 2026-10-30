package runtime_test

import (
	"context"
	"errors"
	"testing"
	"time"

	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	r "github.com/JieWaZi/NeuxBot/server/internal/agent/core/runtime"
)

// stagedRequest 固定无宿主回调的静态会话声明。
func stagedRequest() c.PrepareRequest {
	return c.PrepareRequest{Plan: c.SessionPlan{Profile: c.Profile{DriverID: "fake"}}}
}

func TestStagedClosedPreparationNoLongerNeedsResources(t *testing.T) {
	rt := prepareRuntime(t)
	prepared, err := rt.Prepare(context.Background(), stagedRequest())
	if err != nil {
		t.Fatal(err)
	}
	if err = prepared.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if prepared.NeedsResources() {
		t.Fatal("closed preparation requested another resource owner")
	}
}

func TestStagedColdResumeKeepsNativeDirectiveThroughBind(t *testing.T) {
	rt := prepareRuntime(t)
	request := stagedRequest()
	request.Session.Mode = c.SessionResume
	request.Session.NativeHandle = "persisted-native"
	prepared, err := rt.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close(context.Background())
	before := prepared.Session()
	capabilities := caps()
	capabilities.ColdResume = true
	d := &fakeDriver{caps: capabilities, open: func(_ context.Context, open c.OpenRequest) (c.DriverSession, error) {
		if open.Session.Mode != c.SessionResume || open.Session.NativeHandle != "persisted-native" || len(open.Seed) != 0 {
			t.Fatalf("cold restore request=%+v seed=%+v", open.Session, open.Seed)
		}
		return &fakeSession{prompt: func(ctx context.Context, turn c.TurnRequest, sink c.DriverSink) c.DriverResult {
			if turn.Input.Text != "new input" {
				t.Fatalf("cold restore replayed input: %+v", turn.Input)
			}
			result := answer(ctx, turn, sink)
			result.NativeSession = c.NativeSessionInfo{Handle: "persisted-native", Resumable: true, ForkMode: "native", ForkPosition: "turn-7"}
			return result
		}}, nil
	}}
	bindPrepared(t, prepared, d, &preparedLease{})
	if after := prepared.Session(); before != after {
		t.Fatalf("binding rewrote the native resume directive: before=%+v after=%+v", before, after)
	}
	result := prepared.Run(context.Background(), c.TurnInput{Input: c.Input{Text: "new input"}, Seed: []c.SeedMessage{{Role: "invalid", Text: "must not replay"}}})
	if result.Status != c.StatusCompleted || result.Output.Text != "new input" || result.NativeSession != (c.NativeSessionInfo{Handle: "persisted-native", Resumable: true, ForkMode: "native", ForkPosition: "turn-7"}) {
		t.Fatalf("cold restore result=%+v", result)
	}
}

func TestStagedHotOnlyDriverResumesOwnedNativeHandle(t *testing.T) {
	rt := prepareRuntime(t)
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: func(ctx context.Context, turn c.TurnRequest, sink c.DriverSink) c.DriverResult {
			result := answer(ctx, turn, sink)
			result.NativeSession.Handle = "hot-native"
			return result
		}}, nil
	}}
	request := stagedRequest()
	first, err := rt.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	bindPrepared(t, first, d, &preparedLease{})
	if result := first.Run(context.Background(), c.TurnInput{Input: c.Input{Text: "first"}}); result.Status != c.StatusCompleted {
		t.Fatal(result)
	}
	request.Session = c.SessionDirective{Mode: c.SessionResume, NativeHandle: "hot-native"}
	second, err := rt.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(context.Background())
	if second.NeedsResources() {
		t.Fatal("owned hot native handle requested cold resources")
	}
	result := second.Run(context.Background(), c.TurnInput{Input: c.Input{Text: "second"}})
	if result.Status != c.StatusCompleted || result.Output.Text != "second" || d.opens.Load() != 1 {
		t.Fatalf("hot restore=%+v opens=%d", result, d.opens.Load())
	}
}

func TestStagedColdResumeRequiresNativeRestoreCapability(t *testing.T) {
	rt := prepareRuntime(t)
	request := stagedRequest()
	request.Session = c.SessionDirective{Mode: c.SessionResume, NativeHandle: "persisted-native"}
	prepared, err := rt.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close(context.Background())
	d := &fakeDriver{caps: caps()}
	lease := &preparedLease{}
	err = prepared.Bind(context.Background(), c.SessionResources{Driver: d, Resources: lease})
	var technical *c.Fault
	if !errors.As(err, &technical) || technical.Code != c.FaultUnsupported || d.opens.Load() != 0 || lease.closes.Load() != 0 {
		t.Fatalf("unverified native restore: error=%v opens=%d closes=%d", err, d.opens.Load(), lease.closes.Load())
	}
}

func TestStagedStaticConfigurationChangesOpenFreshContext(t *testing.T) {
	for name, change := range map[string]func(*c.SessionPlan){
		"model":        func(plan *c.SessionPlan) { plan.Profile.Model = "another" },
		"thinking":     func(plan *c.SessionPlan) { off := false; plan.Profile.Thinking = &off },
		"native mode":  func(plan *c.SessionPlan) { plan.Profile.NativeMode = "read-only" },
		"resource key": func(plan *c.SessionPlan) { plan.ResourceKey = "another" },
		"instructions": func(plan *c.SessionPlan) {
			plan.Contributions.Instructions = []c.Instruction{{Name: "profile", Text: "another"}}
		},
		"permissions":   func(plan *c.SessionPlan) { plan.Grants.Tools = []string{"read"} },
		"native config": func(plan *c.SessionPlan) { plan.Configuration.Options = map[string]string{"model": "another"} },
	} {
		t.Run(name, func(t *testing.T) {
			rt := prepareRuntime(t)
			d := &fakeDriver{caps: caps(), open: func(_ context.Context, request c.OpenRequest) (c.DriverSession, error) {
				if request.Session.Mode != c.SessionNew || request.Session.NativeHandle != "" {
					t.Fatalf("changed configuration restored old context: %+v", request.Session)
				}
				return &fakeSession{prompt: func(ctx context.Context, turn c.TurnRequest, sink c.DriverSink) c.DriverResult {
					result := answer(ctx, turn, sink)
					result.NativeSession.Handle = "native"
					return result
				}}, nil
			}}
			request := stagedRequest()
			first, err := rt.Prepare(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			bindPrepared(t, first, d, &preparedLease{})
			if result := first.Run(context.Background(), c.TurnInput{Input: c.Input{Text: "first"}}); result.Status != c.StatusCompleted {
				t.Fatal(result)
			}
			request.Session = c.SessionDirective{Mode: c.SessionResume, NativeHandle: "native"}
			change(&request.Plan)
			second, err := rt.Prepare(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			defer second.Close(context.Background())
			if !second.NeedsResources() {
				t.Fatal("changed static configuration reused the old consumer")
			}
			bindPrepared(t, second, d, &preparedLease{})
			result := second.Run(context.Background(), c.TurnInput{Input: c.Input{Text: "second"}})
			if result.Status != c.StatusCompleted || result.Output.Text != "second" || d.opens.Load() != 2 {
				t.Fatalf("configuration replacement=%+v opens=%d", result, d.opens.Load())
			}
		})
	}
}

func TestStagedCancelRetainsOwnerUntilLatePartialBind(t *testing.T) {
	rt, err := r.New(r.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	acquireContext, cancelAcquire := context.WithCancel(context.Background())
	defer cancelAcquire()
	prepared, err := rt.Prepare(acquireContext, stagedRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.NeedsResources() {
		t.Fatal("cold session did not request resources")
	}
	cancelAcquire()
	select {
	case <-prepared.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("acquisition context survived cancellation")
	}
	wait, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err = rt.Close(wait); err == nil {
		t.Fatal("runtime discarded unbound acquisition owner")
	}
	lease := &preparedLease{}
	lease.fail.Store(true)
	if err = prepared.Bind(context.Background(), c.SessionResources{Resources: lease}); err == nil {
		t.Fatal("cancelled partial bind was accepted")
	}
	if lease.closes.Load() != 0 {
		t.Fatal("bind released before caller acknowledged acquisition completion")
	}
	if err = prepared.Close(context.Background()); err == nil {
		t.Fatal("partial cleanup fault was lost")
	}
	lease.fail.Store(false)
	if err = rt.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if lease.closes.Load() < 2 || lease.successes.Load() != 1 {
		t.Fatalf("partial lease ownership lost: closes=%d", lease.closes.Load())
	}
}

func TestStagedHotTurnIgnoresSeedAndTurnPoliciesDoNotReacquire(t *testing.T) {
	rt := prepareRuntime(t)
	d := &fakeDriver{caps: caps(), open: func(_ context.Context, request c.OpenRequest) (c.DriverSession, error) {
		if len(request.Seed) != 1 || request.Seed[0].Text != "history" {
			t.Fatalf("new session seed=%+v", request.Seed)
		}
		return &fakeSession{prompt: answer}, nil
	}}
	request := stagedRequest()
	first, err := rt.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err = first.Bind(context.Background(), c.SessionResources{Driver: d}); err != nil {
		t.Fatal(err)
	}
	result := first.Run(context.Background(), c.TurnInput{Input: c.Input{Text: "one"}, Seed: []c.SeedMessage{{Role: "user", Text: "history"}}})
	if result.Status != c.StatusCompleted {
		t.Fatalf("first=%+v", result)
	}
	request.Session = c.SessionDirective{Mode: c.SessionResume}
	second, err := rt.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if second.NeedsResources() {
		t.Fatal("turn-only policy change reacquired resources")
	}
	result = second.Run(context.Background(), c.TurnInput{Input: c.Input{Text: "two"}, Seed: []c.SeedMessage{{Role: "invalid", Text: "must not replay"}}, Interaction: c.InteractionInteractive, Output: c.OutputBestEffort})
	if result.Status != c.StatusCompleted || d.opens.Load() != 1 {
		t.Fatalf("hot=%+v opens=%d", result, d.opens.Load())
	}
}

func TestStagedBindErrorRetainsDriverBeforeDependentResources(t *testing.T) {
	rt := prepareRuntime(t)
	lease := &preparedLease{}
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) { return nil, errors.New("must not open") }}
	prepared, err := rt.Prepare(context.Background(), stagedRequest())
	if err != nil {
		t.Fatal(err)
	}
	actual := c.SessionPlan{Profile: c.Profile{DriverID: "another-driver"}}
	if err = prepared.Bind(context.Background(), c.SessionResources{ActualPlan: &actual, Driver: d, Resources: lease}); err == nil {
		t.Fatal("mismatched actual driver accepted")
	}
	if d.opens.Load() != 0 || d.closes.Load() != 0 || lease.closes.Load() != 0 {
		t.Fatal("bind opened or discarded a resource")
	}
	if err = prepared.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d.closes.Load() != 1 || lease.closes.Load() != 1 {
		t.Fatal("failed bind did not retain resource ownership")
	}
}

func TestStagedBindTransfersCancellationToRunContext(t *testing.T) {
	rt := prepareRuntime(t)
	declaration, cancelDeclaration := context.WithCancel(context.Background())
	prepared, err := rt.Prepare(declaration, stagedRequest())
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: func(ctx context.Context, turn c.TurnRequest, sink c.DriverSink) c.DriverResult {
			close(entered)
			<-ctx.Done()
			return c.DriverResult{Status: c.StatusCancelled, StopReason: c.StopCancelled}
		}}, nil
	}}
	bindPrepared(t, prepared, d, &preparedLease{})
	cancelDeclaration()
	runContext, cancelRun := context.WithCancel(context.Background())
	done := make(chan c.Result, 1)
	go func() { done <- prepared.Run(runContext, c.TurnInput{Input: c.Input{Text: "accepted turn"}}) }()
	select {
	case <-entered:
	case result := <-done:
		t.Fatalf("declaration context cancelled accepted work: %+v", result)
	case <-time.After(time.Second):
		t.Fatal("accepted run did not start")
	}
	cancelRun()
	select {
	case result := <-done:
		if result.Status != c.StatusCancelled {
			t.Fatalf("run cancellation=%+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("run context did not cancel native consumer")
	}
}

func TestStagedActualPlanCannotExpandDeclaredGrants(t *testing.T) {
	rt := prepareRuntime(t)
	request := stagedRequest()
	request.Plan.Grants = c.Grants{Tools: []string{"read"}}
	request.Plan.Policies.Tools = c.ToolPolicy{Mode: c.ToolsAllowAll}
	prepared, err := rt.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	actual := request.Plan
	actual.Grants = c.Grants{AllowAllTools: true}
	d := &fakeDriver{caps: caps(), open: func(_ context.Context, open c.OpenRequest) (c.DriverSession, error) {
		if open.Policies.Tools.Mode != c.ToolsAllowList || len(open.Policies.Tools.Allow) != 1 || open.Policies.Tools.Allow[0] != "read" {
			t.Fatalf("resources expanded declared grant: %+v", open.Policies.Tools)
		}
		return &fakeSession{prompt: answer}, nil
	}}
	if err = prepared.Bind(context.Background(), c.SessionResources{ActualPlan: &actual, Driver: d}); err != nil {
		t.Fatal(err)
	}
	result := prepared.Run(context.Background(), c.TurnInput{Input: c.Input{Text: "authorized turn"}})
	if result.Status != c.StatusCompleted {
		t.Fatalf("result=%+v", result)
	}
}

func TestStagedTemporaryHotTurnClosesExistingConsumer(t *testing.T) {
	rt := prepareRuntime(t)
	request := stagedRequest()
	lease := &preparedLease{}
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: answer}, nil
	}}
	first, err := rt.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	bindPrepared(t, first, d, lease)
	if result := first.Run(context.Background(), c.TurnInput{Input: c.Input{Text: "one"}}); result.Status != c.StatusCompleted {
		t.Fatal(result)
	}
	request.Session.Mode = c.SessionResume
	request.Lifetime = c.SessionTemporary
	second, err := rt.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	result := second.Run(context.Background(), c.TurnInput{Input: c.Input{Text: "last"}})
	if result.Status != c.StatusCompleted || lease.closes.Load() != 1 {
		t.Fatalf("temporary result=%+v closes=%d", result, lease.closes.Load())
	}
}
