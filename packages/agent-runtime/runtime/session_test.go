package runtime_test

import (
	"context"
	r "github.com/JieWaZi/NeuxBot/agent-runtime/runtime"
	"sync/atomic"
	"testing"

	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
)

// TestNativeCheckpointRestoresAcrossRuntimeInstances 防止冷恢复误用新会话或丢失技术身份与修订。
func TestNativeCheckpointRestoresAcrossRuntimeInstances(t *testing.T) {
	capabilities := caps()
	capabilities.ColdResume = true
	var opened []c.OpenRequest
	d := &fakeDriver{caps: capabilities, open: func(_ context.Context, request c.OpenRequest) (c.DriverSession, error) {
		opened = append(opened, request)
		return &fakeSession{prompt: func(ctx context.Context, request c.TurnRequest, sink c.DriverSink) c.DriverResult {
			result := answer(ctx, request, sink)
			result.NativeSession = c.NativeSessionInfo{Handle: "native-history", Resumable: true}
			return result
		}}, nil
	}}
	firstRuntime := started(t, d, nil)
	first := firstRuntime.Run(context.Background(), spec(), c.RunOptions{})
	if first.Checkpoint == nil || first.Checkpoint.NativeHandle != "native-history" || first.Checkpoint.SessionID != first.Invocation.SessionID {
		t.Fatalf("missing native checkpoint: %#v", first)
	}
	if err := firstRuntime.CloseSession(context.Background(), first.Invocation.SessionID); err != nil {
		t.Fatal(err)
	}
	secondRuntime := started(t, &fakeDriver{caps: capabilities, open: d.open}, nil)
	next := spec()
	next.Input.Text = "second"
	next.Session = c.SessionDirective{Mode: c.SessionResume, Checkpoint: first.Checkpoint}
	second := secondRuntime.Run(context.Background(), next, c.RunOptions{})
	if second.Status != c.StatusCompleted || second.Output.Text != "second" || second.Invocation.SessionID != first.Invocation.SessionID || second.Invocation.Revision != 2 {
		t.Fatalf("cold restore: %#v", second)
	}
	if len(opened) != 2 || opened[1].Session.Checkpoint == nil || opened[1].Session.Checkpoint.NativeHandle != "native-history" || len(opened[1].Session.Seed) != 0 {
		t.Fatalf("native open requests: %#v", opened)
	}
	if second.Checkpoint == nil || second.Checkpoint.EngineID == first.Checkpoint.EngineID || second.Checkpoint.Revision != 2 {
		t.Fatalf("checkpoint did not advance to new owner: %#v", second.Checkpoint)
	}
}

// TestCheckpointRequiresActualNativeResumeEvidence 防止驱动能力声明或不透明句柄被误报为可恢复。
func TestCheckpointRequiresActualNativeResumeEvidence(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "unverified", true: "verified"}[native], func(t *testing.T) {
			capabilities := caps()
			capabilities.ColdResume = true
			d := &fakeDriver{caps: capabilities, open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
				return &fakeSession{prompt: func(ctx context.Context, request c.TurnRequest, sink c.DriverSink) c.DriverResult {
					result := answer(ctx, request, sink)
					result.NativeSession = c.NativeSessionInfo{Handle: "native-history", Resumable: native}
					return result
				}}, nil
			}}
			result := started(t, d, nil).Run(context.Background(), spec(), c.RunOptions{})
			if (result.Checkpoint != nil) != native {
				t.Fatalf("checkpoint without negotiated evidence: %#v", result)
			}
		})
	}
}

// TestHostNativeCheckpointDefersOpeningUntilColdRun 防止宿主接入真实原生句柄时提前启动使用者或偷偷改成新会话。
func TestHostNativeCheckpointDefersOpeningUntilColdRun(t *testing.T) {
	capabilities := caps()
	capabilities.ColdResume = true
	d := &fakeDriver{caps: capabilities, open: func(_ context.Context, request c.OpenRequest) (c.DriverSession, error) {
		if request.Session.Mode != c.SessionResume || request.Session.Checkpoint == nil || request.Session.Checkpoint.NativeHandle != "forked-native" || len(request.Session.Seed) != 0 {
			t.Fatalf("native restore request: %#v", request.Session)
		}
		return &fakeSession{prompt: answer}, nil
	}}
	rt := started(t, d, nil)
	input := spec()
	input.Session.Seed = []c.SeedMessage{{Role: "user", Text: "must not replay"}}
	checkpoint, err := rt.NativeCheckpoint(input, "forked-native")
	if err != nil {
		t.Fatal(err)
	}
	if d.opens.Load() != 0 || checkpoint.SessionID == "" || checkpoint.Revision != 0 {
		t.Fatalf("checkpoint preparation: %#v native opens=%d", checkpoint, d.opens.Load())
	}
	input.Session = c.SessionDirective{Mode: c.SessionResume, Checkpoint: checkpoint}
	result := rt.Run(context.Background(), input, c.RunOptions{})
	if result.Status != c.StatusCompleted || result.Invocation.SessionID != checkpoint.SessionID || result.Invocation.Revision != 1 || d.opens.Load() != 1 {
		t.Fatalf("external native restore: %#v opens=%d", result, d.opens.Load())
	}
}

// TestColdCheckpointCannotDuplicateLoadedSession 防止热会话与同一检查点同时取得技术会话所有权。
func TestColdCheckpointCannotDuplicateLoadedSession(t *testing.T) {
	capabilities := caps()
	capabilities.ColdResume = true
	d := &fakeDriver{caps: capabilities, open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: func(ctx context.Context, request c.TurnRequest, sink c.DriverSink) c.DriverResult {
			result := answer(ctx, request, sink)
			result.NativeSession = c.NativeSessionInfo{Handle: "native", Resumable: true}
			return result
		}}, nil
	}}
	rt := started(t, d, nil)
	first := rt.Run(context.Background(), spec(), c.RunOptions{})
	next := spec()
	next.Session = c.SessionDirective{Mode: c.SessionResume, Checkpoint: first.Checkpoint}
	duplicate := rt.Run(context.Background(), next, c.RunOptions{})
	requireCode(t, duplicate.Fault, c.FaultSessionBusy)
	if d.opens.Load() != 1 {
		t.Fatal("duplicate owner opened native resources")
	}
	if err := rt.CloseSession(context.Background(), first.Invocation.SessionID); err != nil {
		t.Fatal(err)
	}
	restored := rt.Run(context.Background(), next, c.RunOptions{})
	if restored.Status != c.StatusCompleted || restored.Invocation.Revision != 2 || d.opens.Load() != 2 {
		t.Fatalf("restore after close: %#v", restored)
	}
}

// TestConfigurationChangeRequiresExplicitNewSession 防止模型、推理或安全配置变化时复用旧上下文并意外再次取得凭据。
func TestConfigurationChangeRequiresExplicitNewSession(t *testing.T) {
	var acquired atomic.Int32
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: answer}, nil
	}}
	rt := started(t, d, func(config *r.Config) {
		config.Resources = funcResourceProvider{func(context.Context, c.ResourceRequest) (c.ResourceLease, error) {
			acquired.Add(1)
			return funcResourceLease{func(context.Context) error { return nil }}, nil
		}}
	})
	first := rt.Run(context.Background(), spec(), c.RunOptions{})
	for _, change := range []func(*c.ExecutionSpec){
		func(input *c.ExecutionSpec) { input.Profile.Model = "new-model" },
		func(input *c.ExecutionSpec) { input.Profile.Reasoning = "high" },
		func(input *c.ExecutionSpec) { input.Profile.CredentialGeneration = "rotated" },
		func(input *c.ExecutionSpec) {
			input.Contributions.Instructions = []c.Instruction{{Name: "profile", Text: "new profile"}}
		},
	} {
		input := spec()
		input.Session = c.SessionDirective{Mode: c.SessionResume, Ref: first.Invocation.SessionID}
		change(&input)
		result := rt.Run(context.Background(), input, c.RunOptions{})
		requireCode(t, result.Fault, c.FaultSessionMismatch)
	}
	if acquired.Load() != 1 || d.opens.Load() != 1 {
		t.Fatalf("mismatch prepared resources: acquire=%d open=%d", acquired.Load(), d.opens.Load())
	}
	input := spec()
	input.Profile.Model = "new-model"
	input.Session = c.SessionDirective{Mode: c.SessionNew, Seed: []c.SeedMessage{{Role: "user", Text: "prior"}, {Role: "assistant", Text: "answer"}}}
	result := rt.Run(context.Background(), input, c.RunOptions{})
	if result.Status != c.StatusCompleted || result.Invocation.SessionID == first.Invocation.SessionID || acquired.Load() != 2 {
		t.Fatalf("explicit new session: %#v", result)
	}
}
