package agent

import (
	"context"
	"errors"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// TurnRequest holds one immutable host input and its public delivery identity.
type TurnRequest struct {
	// UsageSource identifies the verified host entry point.
	UsageSource string
	// UsageWakerID identifies the partner selected at admission.
	UsageWakerID string
	// HostInvocationID identifies this invocation before native submission.
	HostInvocationID string
	// ExecutionID names the public execution. Detached work leaves it empty.
	ExecutionID string
	// AssistantMessageID names the public assistant message throughout the turn.
	AssistantMessageID string
	// Text is the plain input when Content is empty.
	Text string
	// Content carries ordered structured native input.
	Content []AgentContent
}

// TurnAdmission carries the opened binding and the exact reserved adapter input.
type TurnAdmission struct {
	// Binding identifies the session opened during preparation.
	Binding NativeBinding
	// Input identifies the specific input that BeforeSubmit will authorize.
	Input HostInput
}

// TurnPorts contains the business operations supplied for one turn.
type TurnPorts struct {
	// AcquireResources materializes the selected resources once on the cold path.
	AcquireResources func(context.Context, ResourceSelection) (ResourceLease, error)
	// ResolveHandoffSource returns authorized launch material for the exact prior binding.
	ResolveHandoffSource func(context.Context, NativeBinding) (HandoffSource, error)
	// Admit supplies frozen business facts for the one BeforeSubmit batch. It runs once.
	Admit func(context.Context, TurnAdmission) (ConversationBatch, error)
	// Authorize enables this input after the durable admission, returning its revocation.
	Authorize func(context.Context, TurnAdmission, CommitReceipt) (func(), error)
	// Committed observes a durable Chat version. Implementations must enqueue without blocking.
	Committed func(CommitReceipt)
	// Observe consumes normalized transient output, serially, for host projection.
	Observe func(context.Context, AgentScope, AgentEvent) error
	// Approve handles an interaction for this exact admitted turn.
	Approve func(context.Context, AgentScope, AgentApproval) (string, error)
	// Input handles a structured question for this exact admitted turn.
	Input func(context.Context, AgentScope, AgentInputRequest) (AgentInputResponse, error)
	// CompleteInput handles completion of this turn's URL request.
	CompleteInput func(context.Context, AgentScope, string) error
}

// TurnResult keeps admission, native outcome and local delivery faults separate.
type TurnResult struct {
	// BusinessAdmitted is true once the durable BeforeSubmit batch was written.
	BusinessAdmitted bool
	// Binding is the opened native session, even if the business gate rejected it.
	Binding NativeBinding
	// Input is the exact adapter reservation when available.
	Input AgentPromptSubmission
	// Effective is the native session configuration observed through settlement.
	Effective AgentSession
	// Submission records the transport evidence for this input.
	Submission string
	// Admission records whether the adapter proved provider acceptance.
	Admission string
	// Outcome retains the immutable native completion, even on delivery failure.
	Outcome *AgentNativeOutcome
	// OperationError describes failure to run or wait for the prompt.
	OperationError error
	// DeliveryError describes observation or interaction failure.
	DeliveryError error
	// StorageError describes failure to persist or confirm known evidence.
	StorageError error
	// CleanupError describes failure to release the owner or its resources.
	CleanupError error
}

type turnPhase uint8

const (
	turnPreparing turnPhase = iota
	turnPrepared
	turnRunning
	turnFinishing
)

// Turn owns the reserved thread seat until this exact input settles.
type Turn struct {
	// lifecycle owns the thread seat and shared process and store dependencies.
	lifecycle *ThreadLifecycle
	// thread is the state holding this Turn's seat.
	thread *threadState
	// ctx follows preparation, cancellation and the executing caller.
	ctx context.Context
	// cancel only interrupts work belonging to this Turn.
	cancel context.CancelFunc
	// done closes after cleanup and release of the seat.
	done chan struct{}
	// input freezes the host invocation and public message identity.
	input TurnRequest
	// ports provides business operations bound to this input.
	ports TurnPorts
	// mu protects phase, exact active scope and interaction authorization.
	mu sync.Mutex
	// phase distinguishes preparing, prepared, running and settling.
	phase turnPhase
	// conn references the actual process owner, as set during preparation.
	conn *providerConnection
	// binding is the pending or committed native session for this Turn.
	binding NativeBinding
	// effective is the native configuration reported before prompt submission.
	effective AgentSession
	// opened indicates this Turn opened a connection that must be committed to keep it.
	opened bool
	// admitted records a successful durable BeforeSubmit.
	admitted bool
	// adapterInputID links the foreground flush to independently arriving receipts.
	adapterInputID string
	// nativeInvocationID is the first proven native scope for this input.
	nativeInvocationID string
	// handoffClaim retains this exact delivery attempt through an uncertain commit.
	handoffClaim *HandoffClaim
	// gateStorageErr records durable admission failure separately from native transport.
	gateStorageErr error
	// authorized enables observations and interactions after the business gate.
	authorized bool
	// revoke disables the exact authorization enabled in BeforeSubmit.
	revoke func()
	// controls tracks steering admissions until their exact native operation settles.
	controls sync.WaitGroup
	// steerRevokes holds additional input grants under the same native turn.
	steerRevokes []func()
	// handoffToolAttempt is set from actual native tool or interaction evidence.
	handoffToolAttempt bool
	// summary is a temporary private Turn on the old connection during handoff.
	summary *Turn
	// buffer owns pending native message changes for this invocation.
	buffer DeltaBuffer
	// live owns ordered transient observation and its delivery failure.
	live liveTurnObserver
	// cleanupErr is published before done closes.
	cleanupErr error
}

func newTurn(ctx context.Context, lifecycle *ThreadLifecycle, thread *threadState, input TurnRequest, ports TurnPorts) *Turn {
	ctx, cancel := context.WithCancel(ctx)
	t := &Turn{lifecycle: lifecycle, thread: thread, ctx: ctx, cancel: cancel, done: make(chan struct{}), input: input, ports: ports}
	context.AfterFunc(ctx, t.cancelPrepared)
	return t
}

func (t *Turn) setSession(conn *providerConnection, binding NativeBinding) {
	t.mu.Lock()
	t.conn, t.binding = conn, binding
	t.mu.Unlock()
}

// Run consumes the prepared Turn once, retaining native evidence through local failures.
func (t *Turn) Run(ctx context.Context) TurnResult {
	t.mu.Lock()
	if t.phase != turnPrepared {
		t.mu.Unlock()
		return TurnResult{OperationError: status_error.RuntimeUnavailable}
	}
	t.phase = turnRunning
	life := t.ctx
	runCtx, cancelRun := context.WithCancel(ctx)
	if life.Err() != nil {
		cancelRun()
	}
	t.ctx = runCtx
	t.mu.Unlock()
	stop := context.AfterFunc(life, cancelRun)
	defer stop()
	defer cancelRun()
	result := t.runPrompt()
	result.CleanupError = t.settle(t.thread.chatID == "" || t.opened && !t.admitted)
	return result
}

func (t *Turn) runPrompt() TurnResult {
	res := TurnResult{Binding: cloneBinding(t.binding), Effective: t.observedSession(), Submission: "not_sent"}
	if err := t.ctx.Err(); err != nil {
		res.OperationError = err
		return res
	}
	client, err := t.conn.Client()
	if err != nil {
		res.OperationError = err
		return res
	}
	t.live.start(t.ctx, t.ports.Observe)
	run, err := client.ReservePrompt(t.ctx, AgentPromptRequest{
		SessionID: t.binding.Session.SessionID, HostInvocationID: t.input.HostInvocationID,
		Text: t.input.Text, Content: t.input.Content, BeforeSubmit: t.beforeSubmit,
	})
	if err != nil {
		res.OperationError = err
	} else {
		out := run()
		res.Input, res.Submission, res.Admission = out.Input, out.Submission, out.Admission
		res.OperationError, res.Outcome = out.OperationError, out.Outcome
		if out.Outcome != nil {
			res.OperationError = errors.Join(res.OperationError, out.Outcome.OperationError)
			res.DeliveryError = out.Outcome.DeliveryError
		}
	}
	t.mu.Lock()
	t.authorized = false
	t.phase = turnFinishing
	revoke := t.revoke
	res.BusinessAdmitted = t.admitted
	t.mu.Unlock()
	t.controls.Wait()
	for _, revoke := range t.steerRevokes {
		revoke()
	}
	if revoke != nil {
		revoke()
	}

	// Settling observed evidence uses its own bounded context even after cancellation.
	finalize, cancel := context.WithTimeout(context.WithoutCancel(t.ctx), turnSettlementTimeout)
	defer cancel()
	res.StorageError = errors.Join(t.gateStorageErr, t.buffer.finish(finalize, t.deltaTarget()))
	if t.handoffClaim != nil && (res.Submission == "not_sent" || res.Admission == "not_accepted" && res.Outcome == nil) {
		res.StorageError = errors.Join(res.StorageError, t.restoreHandoff(finalize))
	}
	res.Binding = cloneBinding(t.binding)
	res.Effective = t.observedSession()
	if res.Outcome != nil {
		l := t.lifecycle
		reader := l.sessionReader(t.thread)
		var err error
		if reader != nil && reader.session == t.binding.Session {
			err = l.persistReceipt(finalize, reader, *res.Outcome, t.ports.Committed)
		} else {
			err = l.commitAndAcknowledge(finalize, client, t.binding.Session, *res.Outcome, t.ports.Committed)
		}
		res.StorageError = errors.Join(res.StorageError, err)
	}
	res.DeliveryError = errors.Join(res.DeliveryError, t.live.finish(finalize))
	return res
}

func (t *Turn) beforeSubmit(ctx context.Context, submission AgentPromptSubmission) error {
	admission, receipt, err := t.recordInput(ctx, t.input, t.ports, submission)
	if err != nil {
		return err
	}
	if t.thread.chatID != "" {
		t.publishBinding()
	}
	t.mu.Lock()
	t.admitted = true
	t.buffer.chatVersion = receipt.Version
	t.adapterInputID = submission.InputID
	t.mu.Unlock()
	revoke, err := authorizeInput(ctx, t.ports, admission, receipt)
	if err == nil && ctx.Err() == nil && t.binding.Handoff != nil {
		err = t.claimHandoff(ctx, submission)
		if err != nil {
			t.gateStorageErr = err
		}
	}
	t.mu.Lock()
	t.revoke = revoke
	t.authorized = err == nil && ctx.Err() == nil
	t.mu.Unlock()
	if err != nil {
		return err
	}
	return ctx.Err()
}

func (t *Turn) recordInput(ctx context.Context, input TurnRequest, ports TurnPorts, submission AgentPromptSubmission) (TurnAdmission, CommitReceipt, error) {
	var receipt CommitReceipt
	if err := ctx.Err(); err != nil {
		return TurnAdmission{}, receipt, err
	}
	if submission.HostInvocationID != input.HostInvocationID || submission.SessionID != t.binding.Session.SessionID || submission.InputID == "" {
		return TurnAdmission{}, receipt, status_error.RuntimeProtocolIncompatible
	}
	admission := TurnAdmission{Binding: cloneBinding(t.binding), Input: HostInput{
		Session: t.binding.Session, HostInvocationID: submission.HostInvocationID,
		AdapterInputID: submission.InputID, NativeInputID: stringValue(submission.NativeInputID),
		ExecutionID: input.ExecutionID, AssistantMessageID: input.AssistantMessageID,
	}}
	var err error
	if t.binding.Session.Provider == "codex" {
		admission.Input.Attribution = &UsageAttribution{ChatID: t.thread.chatID, AccountID: t.conn.account.id,
			WakerID: input.UsageWakerID, Source: input.UsageSource}
	}
	if input.ExecutionID == "" {
		err = t.lifecycle.store.CommitDetachedInput(ctx, admission.Input)
	} else {
		batch, prepareErr := ports.Admit(ctx, admission)
		if prepareErr != nil {
			return admission, receipt, prepareErr
		}
		if batch.Key == "" || batch.ChatID != t.thread.chatID {
			return admission, receipt, status_error.ChatLogRecordInvalid
		}
		changes := make([]ConversationChange, 0, len(batch.Changes)+2)
		for _, change := range batch.Changes {
			if change.Fact == nil || change.Binding != nil || change.Input != nil || change.Message != nil || change.Consumption != nil {
				return admission, receipt, status_error.ChatLogRecordInvalid
			}
			changes = append(changes, change)
		}
		batch.Changes = append(changes, ConversationChange{Binding: &t.binding}, ConversationChange{Input: &admission.Input})
		receipt, err = commitConversation(ctx, t.lifecycle.store, batch)
	}
	if err != nil && input.HostInvocationID == t.input.HostInvocationID {
		t.gateStorageErr = err
	}
	return admission, receipt, err
}

func (t *Turn) publishBinding() {
	binding := cloneBinding(t.binding)
	t.lifecycle.mu.Lock()
	t.thread.binding = &binding
	t.lifecycle.mu.Unlock()
}

func authorizeInput(ctx context.Context, ports TurnPorts, admission TurnAdmission, receipt CommitReceipt) (func(), error) {
	if ports.Committed != nil && receipt.Version > 0 {
		ports.Committed(receipt)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ports.Authorize != nil {
		return ports.Authorize(ctx, admission, receipt)
	}
	return nil, nil
}

// Steer adds one business input to this already-proven native invocation.
// Its receipt describes only the new input; the shared native outcome remains owned by Run.
func (t *Turn) Steer(ctx context.Context, input TurnRequest, ports TurnPorts) (AgentSteerResult, error) {
	if t.thread.chatID == "" || input.HostInvocationID == "" || input.HostInvocationID == t.input.HostInvocationID ||
		input.ExecutionID == "" || input.AssistantMessageID == "" || ports.Admit == nil ||
		(input.Text == "") == (len(input.Content) == 0) {
		return AgentSteerResult{}, status_error.AgentInputInvalid
	}
	t.mu.Lock()
	if t.phase != turnRunning || !t.authorized || t.nativeInvocationID == "" {
		t.mu.Unlock()
		return AgentSteerResult{}, status_error.RuntimeUnavailable
	}
	scope := AgentScope{SessionID: t.binding.Session.SessionID, HostInvocationID: t.input.HostInvocationID, NativeInvocationID: t.nativeInvocationID}
	runCtx := t.ctx
	t.controls.Add(1)
	t.mu.Unlock()
	defer t.controls.Done()
	client, err := t.conn.Client()
	if err != nil {
		return AgentSteerResult{}, err
	}
	control, cancel := context.WithCancel(ctx)
	defer cancel()
	if runCtx.Err() != nil {
		cancel()
	}
	stop := context.AfterFunc(runCtx, cancel)
	defer stop()
	var revoke func()
	result, steerErr := client.Steer(control, AgentSteerRequest{
		SessionID: scope.SessionID, Target: scope, HostInvocationID: input.HostInvocationID,
		Text: input.Text, Content: input.Content,
		BeforeSubmit: func(ctx context.Context, submission AgentPromptSubmission) error {
			if !t.matches(scope) {
				return status_error.RuntimeUnavailable
			}
			admission, receipt, err := t.recordInput(ctx, input, ports, submission)
			if err != nil {
				return err
			}
			revoke, err = authorizeInput(ctx, ports, admission, receipt)
			if err == nil && !t.matches(scope) {
				err = status_error.RuntimeUnavailable
			}
			if err == nil {
				err = ctx.Err()
			}
			if err != nil && revoke != nil {
				revoke()
				revoke = nil
			}
			return err
		},
	})
	if revoke != nil {
		if result.Receipt != nil && result.Receipt.Submission == "not_sent" {
			revoke()
		} else {
			t.mu.Lock()
			t.steerRevokes = append(t.steerRevokes, revoke)
			t.mu.Unlock()
		}
	}
	return result, steerErr
}

func commitConversation(ctx context.Context, store ConversationStore, batch ConversationBatch) (CommitReceipt, error) {
	for {
		receipt, err := store.Commit(ctx, batch)
		if !errors.Is(err, status_error.ChatLogVersionConflict) {
			return receipt, err
		}
		version, err := store.ReadVersion(ctx, batch.ChatID)
		if err != nil {
			return CommitReceipt{}, err
		}
		if version == batch.ExpectedVersion {
			return CommitReceipt{}, status_error.ChatLogVersionConflict
		}
		batch.ExpectedVersion = version
		if err := ctx.Err(); err != nil {
			return CommitReceipt{}, err
		}
	}
}

// Cancel interrupts this Turn without addressing a later invocation by session ID.
func (t *Turn) Cancel() {
	t.cancel()
}

func (t *Turn) cancelPrepared() {
	t.mu.Lock()
	prepared := t.phase == turnPrepared
	if prepared {
		t.phase = turnFinishing
	}
	t.mu.Unlock()
	if prepared {
		t.settle(t.thread.chatID == "" || t.opened)
	}
}

// Close cancels this Turn and waits for settlement, including a never-run preparation.
func (t *Turn) Close(ctx context.Context) error {
	t.Cancel()
	select {
	case <-t.done:
		return t.cleanupErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *Turn) settle(release bool) error {
	t.cancel()
	l, state := t.lifecycle, t.thread
	if release {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.ctx), turnSettlementTimeout)
		t.cleanupErr = l.closeConnection(ctx, state)
		cancel()
	}
	l.mu.Lock()
	if state.active == t {
		state.active = nil
		if !state.closing && state.connection == nil && t.cleanupErr == nil {
			delete(l.threads, state.id)
		}
	}
	close(t.done)
	l.mu.Unlock()
	return t.cleanupErr
}

func (t *Turn) deltaTarget() deltaTarget {
	return deltaTarget{store: t.lifecycle.store, session: t.binding.Session,
		chatID: t.thread.chatID, hostID: t.input.HostInvocationID, messageID: t.input.AssistantMessageID,
		executionID: t.input.ExecutionID, committed: t.ports.Committed}
}

func (l *ThreadLifecycle) matchingTurn(state *threadState, generation uint64, scope AgentScope) *Turn {
	if scope.HostInvocationID == "" || scope.NativeInvocationID == "" {
		return nil
	}
	l.mu.Lock()
	t := state.active
	if state.generation != generation {
		t = nil
	}
	l.mu.Unlock()
	if t == nil {
		return nil
	}
	t.mu.Lock()
	child := t.summary
	t.mu.Unlock()
	if child != nil && child.input.HostInvocationID == scope.HostInvocationID {
		t = child
	}
	if t.matches(scope) {
		return t
	}
	return nil
}

func (t *Turn) matches(scope AgentScope) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.phase != turnRunning || !t.authorized || t.binding.Session.SessionID != scope.SessionID ||
		t.input.HostInvocationID != scope.HostInvocationID {
		return false
	}
	if t.nativeInvocationID == "" {
		t.nativeInvocationID = scope.NativeInvocationID
	}
	return t.nativeInvocationID == scope.NativeInvocationID
}

func (l *ThreadLifecycle) rejectHandoffTool(state *threadState, generation uint64, scope AgentScope) bool {
	if scope.NativeInvocationID == "" {
		return false
	}
	l.mu.Lock()
	active := state.active
	if state.generation != generation {
		active = nil
	}
	l.mu.Unlock()
	if active == nil {
		return false
	}
	active.mu.Lock()
	child := active.summary
	active.mu.Unlock()
	if child == nil {
		return false
	}
	child.mu.Lock()
	reject := child.phase == turnRunning && child.binding.Session.SessionID == scope.SessionID &&
		(scope.HostInvocationID == "" || child.input.HostInvocationID == scope.HostInvocationID)
	if reject {
		child.handoffToolAttempt = true
	}
	child.mu.Unlock()
	if reject {
		child.Cancel()
	}
	return reject
}

func (l *ThreadLifecycle) callbacks(state *threadState, generation uint64) AgentClientCallbacks {
	return AgentClientCallbacks{
		Observe: func(ctx context.Context, scope AgentScope, event AgentEvent) error {
			l.applySessionNotification(state, generation, scope, event)
			// The public user input is the business fact. Native echoes can include
			// private handoff and history material injected before submission.
			if event.Kind == "user" {
				return nil
			}
			if event.Kind == "tool" {
				l.rejectHandoffTool(state, generation, scope)
			}
			if t := l.matchingTurn(state, generation, scope); t != nil {
				if reader := l.sessionReader(state); reader != nil && reader.session == t.binding.Session {
					// An invocation can prove its host input after native-only
					// observations. Preserve the earlier native fragment order.
					if err := reader.flushBeforeHost(ctx, scope.NativeInvocationID); err != nil {
						return err
					}
				}
				if err := t.buffer.observe(ctx, t.deltaTarget(), scope, event); err != nil {
					return err
				}
				t.live.offer(scope, event)
				return nil
			}
			if scope.HostInvocationID == "" && scope.NativeInvocationID != "" {
				l.mu.Lock()
				reader := state.reader
				valid := state.generation == generation && reader != nil && reader.session.SessionID == scope.SessionID
				l.mu.Unlock()
				if valid {
					return reader.observe(ctx, scope, event)
				}
			}
			return nil
		},
		Approve: func(_ context.Context, scope AgentScope, input AgentApproval) (string, error) {
			if l.rejectHandoffTool(state, generation, scope) {
				return "", nil
			}
			t := l.matchingTurn(state, generation, scope)
			if t == nil || t.ports.Approve == nil || t.ctx.Err() != nil {
				return "", nil
			}
			choice, err := t.ports.Approve(t.ctx, scope, input)
			if t.ctx.Err() != nil || !t.matches(scope) {
				return "", nil
			}
			return choice, err
		},
		Input: func(_ context.Context, scope AgentScope, input AgentInputRequest) (AgentInputResponse, error) {
			if l.rejectHandoffTool(state, generation, scope) {
				return AgentInputResponse{Action: "cancel"}, nil
			}
			t := l.matchingTurn(state, generation, scope)
			if t == nil || t.ports.Input == nil || t.ctx.Err() != nil {
				return AgentInputResponse{Action: "cancel"}, nil
			}
			response, err := t.ports.Input(t.ctx, scope, input)
			if t.ctx.Err() != nil || !t.matches(scope) {
				return AgentInputResponse{Action: "cancel"}, nil
			}
			return response, err
		},
		CompleteInput: func(_ context.Context, scope AgentScope, id string) error {
			t := l.matchingTurn(state, generation, scope)
			if t == nil || t.ports.CompleteInput == nil || t.ctx.Err() != nil {
				return nil
			}
			return t.ports.CompleteInput(t.ctx, scope, id)
		},
	}
}

// Compact runs a native control on the idle bound connection under the same thread seat.
func (l *ThreadLifecycle) Compact(ctx context.Context, chatID string) (AgentCompactResult, error) {
	l.mu.Lock()
	state := l.threads[chatID]
	if l.closing || state == nil || state.closing || state.active != nil || state.binding == nil || state.connection == nil {
		l.mu.Unlock()
		return AgentCompactResult{}, status_error.RuntimeUnavailable
	}
	binding, conn := *state.binding, state.connection
	// Claude's current compact operation is an internal Prompt without an
	// input admission gate. It cannot be reconciled as a host action here.
	if binding.Session.Provider != constants.RuntimeIDCodex {
		l.mu.Unlock()
		return AgentCompactResult{}, status_error.RuntimePolicyUnsupported
	}
	control := newTurn(ctx, l, state, TurnRequest{}, TurnPorts{})
	control.phase = turnRunning
	state.active = control
	l.mu.Unlock()
	defer control.settle(false)

	checked, err := l.reconcileHandoff(control.ctx, chatID, binding, l.sessionReader(state))
	if err != nil {
		return AgentCompactResult{}, err
	}
	if checked.Handoff != nil {
		return AgentCompactResult{}, status_error.ChatLogNativeOutcomePending
	}
	client, err := conn.Client()
	if err != nil {
		return AgentCompactResult{}, err
	}
	result, operationErr := client.Compact(control.ctx, binding.Session.SessionID)
	if result.Turn == nil {
		return result, operationErr
	}
	storeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), turnSettlementTimeout)
	defer cancel()
	storeErr := l.commitAndAcknowledge(storeCtx, client, binding.Session, AgentNativeOutcome{Turn: *result.Turn}, nil)
	return result, errors.Join(operationErr, storeErr)
}
