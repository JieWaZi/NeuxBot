package execution

import (
	"context"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestExecutionRegistryCloseWaitsForReservedExecution(t *testing.T) {
	registry := newExecutionRegistry()
	registry.start(context.Background())
	executionContext, err := registry.reserve("chat-1", "execution-1")
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

func TestExecutionRegistryCloseWaitsForLaunchedExecutionWithoutDoubleRelease(
	t *testing.T,
) {
	registry := newExecutionRegistry()
	registry.start(context.Background())
	executionContext, err := registry.reserve("chat-1", "execution-1")
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

	if _, err := registry.reserve("chat-complete", "execution-complete"); err != nil {
		t.Fatalf("reserve(completion) error = %v", err)
	}
	go func() {
		time.Sleep(10 * time.Millisecond)
		registry.release("chat-complete", "execution-complete")
	}()
	if err := registry.cancelAndWait(context.Background(), "chat-complete"); err != nil {
		t.Fatalf("cancelAndWait(completion) error = %v", err)
	}

	if _, err := registry.reserve("chat-timeout", "execution-timeout"); err != nil {
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

func TestCancelExecutionAndWaitCannotCancelNextChatExecution(t *testing.T) {
	registry := newExecutionRegistry()
	registry.start(context.Background())
	defer registry.close()
	if _, err := registry.reserve("shared-chat", "old-worker"); err != nil {
		t.Fatal(err)
	}
	registry.release("shared-chat", "old-worker")
	next, err := registry.reserve("shared-chat", "new-input")
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
	if _, err := registry.reserve("chat", "old"); err != nil {
		t.Fatal(err)
	}
	defer registry.release("chat", "old")
	registry.complete("chat", "old", nil)
	if _, err := registry.reserve("chat", "next"); err != nil {
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
	ctx, err := registry.reserve("chat", "execution")
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
