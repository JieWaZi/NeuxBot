package main

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestDeferredCleanupWaitsForAsynchronousHook(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var calls atomic.Int32
	closeCore := onceCleanup(func() { calls.Add(1); close(started); <-release })
	go closeCore()
	<-started
	go func() { defer close(done); closeCore() }()
	select {
	case <-done:
		t.Fatal("deferred cleanup returned before the asynchronous hook")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not finish")
	}
	closeCore()
	if calls.Load() != 1 {
		t.Fatal("cleanup ran more than once")
	}
}
