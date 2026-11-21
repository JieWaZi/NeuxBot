package execution

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

type missingSubmissionJournal struct {
	// Journal 提供本测试不调用的其他日志方法。
	chatservice.Journal
}

func (missingSubmissionJournal) ReadSubmission(context.Context, string, string) (*models.Execution, error) {
	return nil, nil
}

func TestExecutionRegistryCloseWaitsForReservedExecution(t *testing.T) {
	registry := newExecutionRegistry()
	registry.start(context.Background())
	executionContext, err := registry.reserve("chat-1", "execution-1", "")
	if err != nil {
		t.Fatalf("reserve() error = %v", err)
	}

	closed := make(chan struct{})
	go func() {
		registry.close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("close() returned before the reserved execution was released")
	case <-time.After(20 * time.Millisecond):
	}
	select {
	case <-executionContext.Done():
	case <-time.After(time.Second):
		t.Fatal("reserved execution context was not canceled by close()")
	}

	registry.release("chat-1", "execution-1")
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close() remained blocked after the reservation was released")
	}
}

func TestCancellationBeforeSubmissionReserveOnlyStopsThatInput(t *testing.T) {
	ctx := t.Context()
	registry := newExecutionRegistry()
	registry.start(ctx)
	t.Cleanup(registry.close)
	core := &service{executions: registry, journal: missingSubmissionJournal{}}

	if err := core.CancelSubmissionAndWait(ctx, "chat", "pending-input"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.reserve("chat", "canceled-execution", "pending-input"); !errors.Is(err, status_error.OperationCanceled) {
		registry.release("chat", "canceled-execution")
		t.Fatalf("stopped submission reached preparation: %v", err)
	}
	if _, err := registry.reserve("chat", "replayed-execution", "pending-input"); !errors.Is(err, status_error.OperationCanceled) {
		registry.release("chat", "replayed-execution")
		t.Fatalf("stopped submission replay reached preparation: %v", err)
	}
	next, err := registry.reserve("chat", "new-execution", "new-input")
	if err != nil {
		t.Fatal(err)
	}
	defer registry.release("chat", "new-execution")
	if err := core.CancelSubmissionAndWait(ctx, "chat", "pending-input"); err != nil || next.Err() != nil {
		t.Fatalf("late stop touched a different input: %v, %v", err, next.Err())
	}
}

func TestStopNativeExecutionsSignalsAllBeforeWaiting(t *testing.T) {
	ctx := t.Context()
	registry := newExecutionRegistry()
	registry.start(ctx)
	first, err := registry.reserve("first-chat", "first-execution", "")
	if err != nil {
		t.Fatal(err)
	}
	last, err := registry.reserve("last-chat", "last-execution", "")
	if err != nil {
		t.Fatal(err)
	}
	core := &service{executions: registry}
	await := core.StopNativeExecutions([]string{"first-chat", "last-chat"})
	if first.Err() == nil || last.Err() == nil {
		t.Fatalf("stop waited on an individual execution before signaling the group")
	}
	registry.release("first-chat", "first-execution")
	registry.release("last-chat", "last-execution")
	if err := await(ctx); err != nil {
		t.Fatal(err)
	}
	registry.close()
}

func TestExecutionRegistryCloseWaitsForLaunchedExecutionWithoutDoubleRelease(
	t *testing.T,
) {
	registry := newExecutionRegistry()
	registry.start(context.Background())
	executionContext, err := registry.reserve("chat-1", "execution-1", "")
	if err != nil {
		t.Fatalf("reserve() error = %v", err)
	}
	runStarted := make(chan struct{})
	runFinished := make(chan struct{})
	registry.launch(
		"chat-1",
		"execution-1",
		executionContext,
		func(ctx context.Context) {
			close(runStarted)
			<-ctx.Done()
			close(runFinished)
		},
	)
	<-runStarted

	closed := make(chan struct{})
	go func() {
		registry.close()
		close(closed)
	}()
	select {
	case <-runFinished:
	case <-time.After(time.Second):
		t.Fatal("launched execution did not observe close cancellation")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close() did not wait for launched execution cleanup")
	}

	// 后台 defer 再次 release 时必须保持幂等，不能重复 Done 或关闭通道。
	registry.release("chat-1", "execution-1")
}

func TestExecutionRegistryCancelAndWaitCoversCompletionAndTimeout(t *testing.T) {
	registry := newExecutionRegistry()
	registry.close()
	registry.start(context.Background())
	registry.start(context.Background())
	if err := registry.cancelAndWait(context.Background(), "missing"); err != nil {
		t.Fatalf("cancelAndWait(missing) error = %v", err)
	}

	if _, err := registry.reserve("chat-complete", "execution-complete", ""); err != nil {
		t.Fatalf("reserve(completion) error = %v", err)
	}
	go func() {
		time.Sleep(10 * time.Millisecond)
		registry.release("chat-complete", "execution-complete")
	}()
	if err := registry.cancelAndWait(context.Background(), "chat-complete"); err != nil {
		t.Fatalf("cancelAndWait(completion) error = %v", err)
	}

	if _, err := registry.reserve("chat-timeout", "execution-timeout", ""); err != nil {
		t.Fatalf("reserve(timeout) error = %v", err)
	}
	timeoutContext, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := registry.cancelAndWait(timeoutContext, "chat-timeout"); err !=
		status_error.ChatHasActiveExecution {
		t.Fatalf("cancelAndWait(timeout) error = %v", err)
	}
	registry.release("chat-timeout", "execution-timeout")
	registry.close()
}

func TestExecutionCancellationWaitsForNativeSettlement(t *testing.T) {
	for _, exact := range []bool{false, true} {
		name := "chat"
		if exact {
			name = "execution"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				registry := newExecutionRegistry()
				registry.start(t.Context())
				defer registry.close()
				active, err := registry.reserve("chat", "execution", "")
				if err != nil {
					t.Fatal(err)
				}
				defer registry.release("chat", "execution")
				done := make(chan error, 1)
				core := &service{executions: registry}
				go func() {
					if exact {
						done <- core.CancelExecutionAndWait(t.Context(), "chat", "execution")
						return
					}
					done <- core.CancelAndWait(t.Context(), "chat")
				}()
				synctest.Wait()
				select {
				case err := <-done:
					t.Fatalf("cancel returned before native settlement: %v", err)
				default:
				}
				if active.Err() != context.Canceled {
					t.Fatalf("native execution did not receive cancellation: %v", active.Err())
				}
				registry.release("chat", "execution")
				if err := <-done; err != nil {
					t.Fatalf("native cancellation failed after settlement: %v", err)
				}
			})
		})
	}
}

func TestCancelExecutionAndWaitCannotCancelNextChatExecution(t *testing.T) {
	registry := newExecutionRegistry()
	registry.start(context.Background())
	defer registry.close()
	if _, err := registry.reserve("shared-chat", "old-worker", ""); err != nil {
		t.Fatal(err)
	}
	registry.release("shared-chat", "old-worker")
	next, err := registry.reserve("shared-chat", "new-input", "")
	if err != nil {
		t.Fatal(err)
	}
	defer registry.release("shared-chat", "new-input")
	if err = registry.cancelExecutionAndWait(context.Background(), "shared-chat", "old-worker"); err != nil {
		t.Fatal(err)
	}
	if next.Err() != nil {
		t.Fatal("stopping old worker cancelled the new ordinary input")
	}
}

func TestExecutionRegistryReleasesAdmissionBeforeFileSettlementButDeletionStillWaits(t *testing.T) {
	registry := newExecutionRegistry()
	registry.start(context.Background())
	defer registry.close()
	if _, err := registry.reserve("chat", "old", ""); err != nil {
		t.Fatal(err)
	}
	defer registry.release("chat", "old")
	registry.complete("chat", "old", nil)
	if _, err := registry.reserve("chat", "next", ""); err != nil {
		t.Fatalf("file settlement retained model admission: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := registry.waitForFileSnapshots(ctx, context.Background(), "chat", "next"); err != context.Canceled {
		t.Fatalf("next input did not wait for the previous after snapshot: %v", err)
	}
	if err := registry.waitForFileSnapshots(context.Background(), ctx, "chat", "next"); err != context.Canceled {
		t.Fatalf("snapshot preparation ignored execution cancellation: %v", err)
	}
	registry.freezeFileSnapshots("chat", "old")
	if err := registry.waitForFileSnapshots(context.Background(), context.Background(), "chat", "next"); err != nil {
		t.Fatalf("next input waited for diff calculation after the snapshot froze: %v", err)
	}
	registry.release("chat", "next")
	if err := registry.cancelAndWait(ctx, "chat"); err != status_error.ChatHasActiveExecution {
		t.Fatalf("deletion skipped unfinished file owner: %v", err)
	}
	registry.release("chat", "old")
	if err := registry.cancelAndWait(context.Background(), "chat"); err != nil {
		t.Fatal(err)
	}
}

func TestExecutionRegistryReleasesContextAfterSettlement(t *testing.T) {
	registry := newExecutionRegistry()
	registry.start(context.Background())
	defer registry.close()
	ctx, err := registry.reserve("chat", "execution", "")
	if err != nil {
		t.Fatal(err)
	}
	defer registry.release("chat", "execution")
	registry.complete("chat", "execution", nil)
	if ctx.Err() != nil {
		t.Fatal("completion canceled the context before file settlement")
	}
	registry.release("chat", "execution")
	if ctx.Err() != context.Canceled {
		t.Fatal("settled execution context is still retained by the registry lifecycle")
	}
}
