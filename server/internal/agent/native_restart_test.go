package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func restartFixture(closeAccount func(context.Context) error) (*ThreadLifecycle, *ProviderRuntime, *providerConnection) {
	provider := NewProviderRuntime(ProviderRuntimeOptions{CleanupTimeout: time.Second})
	opened := make(chan struct{})
	close(opened)
	connection := &providerConnection{runtime: provider, id: 1, account: providerAccount{provider: "codex", id: SystemAccountID}, opened: opened, cancelOpen: func() {}, accountLease: AccountLease{Close: closeAccount}}
	provider.connections[1] = connection
	lifecycle := NewThreadLifecycle(provider, nil)
	lifecycle.threads["chat"] = &threadState{id: "chat", connection: connection}
	return lifecycle, provider, connection
}

func TestRestartProviderRejectsActiveWorkBeforeClosingIdle(t *testing.T) {
	var closed atomic.Int32
	lifecycle, provider, _ := restartFixture(func(context.Context) error { closed.Add(1); return nil })
	lifecycle.threads["active"] = &threadState{id: "active", active: &Turn{}, admittedAccount: providerAccount{provider: "codex", id: SystemAccountID}}
	if err := lifecycle.RestartProvider(t.Context(), "codex"); !errors.Is(err, status_error.RuntimeRestartBusy) || closed.Load() != 0 {
		t.Fatalf("restart interrupted work or partially drained idle owners: err=%v closed=%d", err, closed.Load())
	}
	delete(lifecycle.threads, "active")
	if err := lifecycle.RestartProvider(t.Context(), "codex"); err != nil || closed.Load() != 1 || len(provider.connections) != 0 {
		t.Fatalf("idle restart did not close the actual owner: err=%v closed=%d", err, closed.Load())
	}
}

func TestRestartProviderFencesAdmissionAndRetainsFailedOwner(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var attempts atomic.Int32
	lifecycle, provider, original := restartFixture(func(context.Context) error {
		if attempts.Add(1) == 1 {
			close(entered)
			<-release
			return status_error.RuntimeShutdownFailed
		}
		return nil
	})
	result := make(chan error, 1)
	go func() { result <- lifecycle.RestartProvider(t.Context(), "codex") }()
	<-entered
	if connection, err := provider.Open(t.Context(), ProviderConnectionRequest{Account: AccountRequest{Provider: "codex", AccountID: "other"}}, ProviderConnectionPorts{}); connection != nil || !errors.Is(err, status_error.RuntimeRestartBusy) {
		t.Fatalf("restart admitted a replacement process before cleanup: %v", err)
	}
	close(release)
	if err := <-result; !errors.Is(err, status_error.RuntimeShutdownFailed) {
		t.Fatalf("close failure was hidden: %v", err)
	}
	if provider.connections[1] != original || lifecycle.threads["chat"].connection != original {
		t.Fatal("restart lost the failed native owner")
	}
	if err := lifecycle.RestartProvider(t.Context(), "codex"); err != nil || attempts.Load() != 2 || len(provider.connections) != 0 {
		t.Fatalf("restart did not retry the same owner: err=%v attempts=%d", err, attempts.Load())
	}
}

func TestRestartProviderDoesNotInterruptIndependentDirectoryConsumer(t *testing.T) {
	provider := NewProviderRuntime(ProviderRuntimeOptions{})
	provider.connections[1] = &providerConnection{account: providerAccount{provider: "codex", id: SystemAccountID}}
	lifecycle := NewThreadLifecycle(provider, nil)
	if err := lifecycle.RestartProvider(t.Context(), "codex"); !errors.Is(err, status_error.RuntimeRestartBusy) || len(provider.connections) != 1 {
		t.Fatalf("restart withdrew a directory consumer: %v", err)
	}
}

func TestRestartProviderRetriesFailedDirectoryOwner(t *testing.T) {
	var attempts atomic.Int32
	lifecycle, provider, original := restartFixture(func(context.Context) error {
		if attempts.Add(1) == 1 {
			return status_error.RuntimeShutdownFailed
		}
		return nil
	})
	delete(lifecycle.threads, "chat")
	if err := original.Close(t.Context()); !errors.Is(err, status_error.RuntimeShutdownFailed) {
		t.Fatal("fixture did not retain the failed directory owner", err)
	}
	if err := lifecycle.RestartProvider(t.Context(), "codex"); err != nil || attempts.Load() != 2 || len(provider.connections) != 0 {
		t.Fatalf("restart did not recover the same failed directory owner: err=%v attempts=%d", err, attempts.Load())
	}
}

func TestProviderMaintenanceRetainsFenceUntilFilesystemCommit(t *testing.T) {
	lifecycle, provider, _ := restartFixture(func(context.Context) error { return nil })
	release, err := lifecycle.BeginProviderMaintenance(t.Context(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if len(provider.connections) != 0 {
		t.Fatal("maintenance did not retire the old native owner")
	}
	if connection, err := provider.Open(t.Context(), ProviderConnectionRequest{Account: AccountRequest{Provider: "codex", AccountID: "other"}}, ProviderConnectionPorts{}); connection != nil || !errors.Is(err, status_error.RuntimeRestartBusy) {
		t.Fatalf("filesystem maintenance lost its admission fence: %v", err)
	}
	if _, err := provider.BeginSystemAuthentication(t.Context(), "codex"); !errors.Is(err, status_error.AIAccountBusy) {
		t.Fatalf("maintenance admitted native authentication: %v", err)
	}
	release()
	release()
	provider.mu.Lock()
	blocked := provider.restarting["codex"]
	provider.mu.Unlock()
	if blocked {
		t.Fatal("successful maintenance left the provider blocked")
	}
}
