package runtime_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	r "github.com/JieWaZi/NeuxBot/server/internal/agent/core/runtime"
)

// preparedLease 记录实际释放，失败时仍保留同一资源。
type preparedLease struct {
	successes atomic.Int32 // successes 记录已确认成功释放，不能重复成功。
	closes    atomic.Int32 // closes 统计释放尝试。
	fail      atomic.Bool  // fail 模拟仍不能释放的资源。
}

func (l *preparedLease) Resources() []c.ResourceBinding { return nil }
func (l *preparedLease) Close(context.Context) error {
	l.closes.Add(1)
	if l.fail.Load() {
		return errors.New("lease still in use")
	}
	l.successes.Add(1)
	return nil
}
func prepareRuntime(t *testing.T) *r.Runtime {
	t.Helper()
	rt, err := r.New(r.Config{})
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
	return rt
}
func bindPrepared(t *testing.T, prepared c.PreparedRun, d c.Driver, lease c.ResourceLease) {
	t.Helper()
	if err := prepared.Bind(context.Background(), c.SessionResources{Driver: d, Resources: lease}); err != nil {
		t.Fatal(err)
	}
}
func TestPrepareRetainsLeaseAcrossHotTurnsAndAbandonsOnlyColdSession(t *testing.T) {
	rt := prepareRuntime(t)
	lease := &preparedLease{}
	d := &fakeDriver{caps: caps(), open: func(context.Context, c.OpenRequest) (c.DriverSession, error) {
		return &fakeSession{prompt: answer}, nil
	}}
	request := stagedRequest()
	request.Plan.Tags = []string{"account:a"}
	first, err := rt.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	bindPrepared(t, first, d, lease)
	if d.opens.Load() != 0 {
		t.Fatal("bind opened native connection")
	}
	if err = rt.CloseIdle(context.Background(), "account:a"); err == nil {
		t.Fatal("prepared account was treated as idle")
	}
	if result := first.Run(context.Background(), c.TurnInput{Input: c.Input{Text: "one"}}); result.Status != c.StatusCompleted {
		t.Fatalf("first=%+v", result)
	}
	request.Session.Mode = c.SessionResume
	hot, err := rt.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if hot.NeedsResources() {
		t.Fatal("hot preparation reacquired resources")
	}
	if err = hot.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if lease.closes.Load() != 0 {
		t.Fatal("abandoned hot preparation closed existing consumer")
	}
	hot, err = rt.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	result := hot.Run(context.Background(), c.TurnInput{Input: c.Input{Text: "two"}})
	if result.Status != c.StatusCompleted || d.opens.Load() != 1 {
		t.Fatalf("hot=%+v opens=%d", result, d.opens.Load())
	}
	if err = rt.CloseIdle(context.Background(), "account:a"); err != nil {
		t.Fatal(err)
	}
	if lease.closes.Load() != 1 {
		t.Fatalf("closes=%d", lease.closes.Load())
	}
	cold, err := rt.Prepare(context.Background(), stagedRequest())
	if err != nil {
		t.Fatal(err)
	}
	fresh := &preparedLease{}
	bindPrepared(t, cold, d, fresh)
	if err = cold.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fresh.closes.Load() != 1 || d.opens.Load() != 1 {
		t.Fatal("abandoned cold preparation opened or retained resources")
	}
}
func TestPrepareRuntimeCloseWaitsForLatePartialBind(t *testing.T) {
	rt := prepareRuntime(t)
	prepared, err := rt.Prepare(context.Background(), stagedRequest())
	if err != nil {
		t.Fatal(err)
	}
	closeResult := make(chan error, 1)
	go func() { closeResult <- rt.Close(context.Background()) }()
	select {
	case <-prepared.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("runtime close did not cancel acquisition")
	}
	select {
	case err := <-closeResult:
		t.Fatalf("runtime discarded external acquisition: %v", err)
	default:
	}
	lease := &preparedLease{}
	if err = prepared.Bind(context.Background(), c.SessionResources{Resources: lease}); err == nil {
		t.Fatal("late partial bind ignored runtime cancellation")
	}
	if err = prepared.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not finish after acquisition acknowledgement")
	}
	if lease.closes.Load() != 1 {
		t.Fatalf("partial lease closes=%d", lease.closes.Load())
	}
}
func TestPrepareReadOnlyHandoffRetainsNormalConfigurationAndClosesConsumer(t *testing.T) {
	rt := prepareRuntime(t)
	request := stagedRequest()
	var opens atomic.Int32
	d := &fakeDriver{caps: c.DriverCapabilities{Version: "test", ExplicitConfig: true, DenyAllTools: true, ToolAllowList: true, HotResume: true, ColdResume: true}, open: func(_ context.Context, open c.OpenRequest) (c.DriverSession, error) {
		opens.Add(1)
		return &fakeSession{prompt: func(ctx context.Context, turn c.TurnRequest, sink c.DriverSink) c.DriverResult {
			result := answer(ctx, turn, sink)
			result.NativeSession = c.NativeSessionInfo{Handle: "native", Resumable: true}
			return result
		}}, nil
	}}
	first, err := rt.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	lease := &preparedLease{}
	bindPrepared(t, first, d, lease)
	firstResult := first.Run(context.Background(), c.TurnInput{Input: c.Input{Text: "normal"}})
	if firstResult.Status != c.StatusCompleted {
		t.Fatalf("first=%+v", firstResult)
	}
	request.Session.Mode = c.SessionResume
	request.Session.NativeHandle = "native"
	request.Mode = c.PreparationReadOnlyHandoff
	request.Plan.Workspace.ReadOnly = true
	handoff, err := rt.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	handoffLease := &preparedLease{}
	bindPrepared(t, handoff, d, handoffLease)
	if handoff.Session().Mode != c.SessionResume {
		t.Fatal("handoff lost the native context")
	}
	result := handoff.Run(context.Background(), c.TurnInput{Input: c.Input{Text: "summary"}})
	if result.Status != c.StatusCompleted || handoffLease.closes.Load() != 1 {
		t.Fatalf("handoff=%+v closes=%d", result, handoffLease.closes.Load())
	}
	request.Mode = c.PreparationContinue
	request.Plan.Workspace.ReadOnly = false
	normal, err := rt.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !normal.NeedsResources() || normal.Session().Mode != c.SessionResume {
		t.Fatal("handoff destroyed ordinary configuration compatibility")
	}
	bindPrepared(t, normal, d, &preparedLease{})
	if err = normal.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestPrepareReadOnlyHandoffRejectsWritableActualPlan(t *testing.T) {
	rt := prepareRuntime(t)
	request := stagedRequest()
	request.Session.Mode = c.SessionResume
	request.Session.NativeHandle = "native"
	request.Mode = c.PreparationReadOnlyHandoff
	if _, err := rt.Prepare(context.Background(), request); err == nil {
		t.Fatal("writable handoff declaration accepted")
	}
	request.Plan.Workspace.ReadOnly = true
	prepared, err := rt.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	actual := request.Plan
	actual.Workspace.ReadOnly = false
	lease := &preparedLease{}
	d := &fakeDriver{caps: caps()}
	if err = prepared.Bind(context.Background(), c.SessionResources{ActualPlan: &actual, Driver: d, Resources: lease}); err == nil {
		t.Fatal("writable handoff resource accepted")
	}
	if err = prepared.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if lease.closes.Load() != 1 {
		t.Fatal("invalid handoff resources lost ownership")
	}
}
