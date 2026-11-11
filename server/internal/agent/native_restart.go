package agent

import (
	"context"
	"errors"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// RestartProvider retires this provider's idle native owners under an admission fence.
// Preparing or active work and independent directory consumers reject restart before any owner closes.
// The caller subsequently re-probes the installation; durable conversation bindings remain authoritative.
func (l *ThreadLifecycle) RestartProvider(ctx context.Context, provider string) error {
	release, err := l.BeginProviderMaintenance(ctx, provider)
	if err != nil {
		return err
	}
	defer release()
	return nil
}

// BeginProviderMaintenance drains idle native owners and retains the admission fence
// until the caller finishes replacing or removing this provider's program files.
// Failed cleanup keeps the original owner indexed and refuses filesystem mutation.
func (l *ThreadLifecycle) BeginProviderMaintenance(ctx context.Context, provider string) (func(), error) {
	l.mu.Lock()
	if l.closing || l.restarting[provider] {
		l.mu.Unlock()
		return nil, status_error.RuntimeRestartBusy
	}
	allowed := make(map[*providerConnection]bool)
	uses := func(state *threadState) bool {
		return state.connection != nil && state.connection.account.provider == provider || state.active != nil && state.admittedAccount.provider == provider
	}
	for _, state := range l.threads {
		if !uses(state) {
			continue
		}
		if state.active != nil {
			l.mu.Unlock()
			return nil, status_error.RuntimeRestartBusy
		}
		allowed[state.connection] = true
	}
	l.provider.mu.Lock()
	if l.provider.closing || l.provider.restarting[provider] || l.provider.systemAuthentication[provider] {
		l.provider.mu.Unlock()
		l.mu.Unlock()
		return nil, status_error.RuntimeRestartBusy
	}
	var failedOwners []*providerConnection
	for _, connection := range l.provider.connections {
		if connection.account.provider == provider && !allowed[connection] {
			connection.mu.Lock()
			failed := false
			if attempt := connection.closingAttempt; connection.closing && attempt != nil {
				select {
				case <-attempt.done:
					failed = attempt.err != nil
				default:
				}
			}
			connection.mu.Unlock()
			if !failed {
				l.provider.mu.Unlock()
				l.mu.Unlock()
				return nil, status_error.RuntimeRestartBusy
			}
			failedOwners = append(failedOwners, connection)
		}
	}
	if l.restarting == nil {
		l.restarting = make(map[string]bool)
	}
	if l.provider.restarting == nil {
		l.provider.restarting = make(map[string]bool)
	}
	l.restarting[provider], l.provider.restarting[provider] = true, true
	l.provider.mu.Unlock()
	l.mu.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			l.provider.mu.Lock()
			delete(l.provider.restarting, provider)
			l.provider.mu.Unlock()
			l.mu.Lock()
			delete(l.restarting, provider)
			l.mu.Unlock()
		})
	}
	err := l.closeIdleConsumers(ctx, status_error.RuntimeRestartBusy, uses)
	// A completed directory probe may have retained a failed cleanup owner without a Thread.
	// Retry that exact owner; never cancel an independently active directory consumer.
	for _, owner := range failedOwners {
		err = errors.Join(err, owner.Close(ctx))
	}
	if err != nil {
		release()
		return nil, err
	}
	return release, nil
}
