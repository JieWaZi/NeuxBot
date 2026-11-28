package agent

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	native "github.com/JieWaZi/acp-go/pkg/agentclient"
)

// TurnRequest holds the initial host input and its public delivery identity.
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
	// Context holds trusted per-turn facts separate from the business input and media.
	Context []TurnFact
	// CoreMemory is an optional bounded snapshot offered only on verified public workbench turns.
	CoreMemory *CoreMemoryContext
	// freshText seeds a replacement private text session with bounded public context.
	freshText string
	// privateText captures Router text in memory while the native store keeps body-free usage.
	privateText *ephemeralTextStore
	// authorizeText rechecks the private route's principal before retaining any native binding.
	authorizeText func(context.Context) error
}

func validTurnInput(input TurnRequest) bool {
	if input.Text != "" {
		return len(input.Content) == 0
	}
	return len(input.Content) != 0 || len(input.Context) != 0
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
	// AcquireResources materializes selected execution resources during cold startup.
	AcquireResources func(context.Context, ResourceSelection) (ResourceLease, error)
	// ResolveHandoffSource reads authorized handoff material from the previous binding.
	ResolveHandoffSource func(context.Context, NativeBinding) (HandoffSource, error)
	// Admit supplies frozen business facts to write before the first submission.
	Admit func(context.Context, TurnAdmission) (ConversationBatch, error)
	// CommitAdmission optionally wraps the atomic business, binding and input commit in host authorization.
	CommitAdmission func(context.Context, func(context.Context) error) error
	// Authorize enables input authorization after admission is persisted and returns a revocation function.
	Authorize func(context.Context, TurnAdmission, CommitReceipt) (func(), error)
	// Committed delivers committed chat versions; the callback must enqueue without blocking.
	Committed func(CommitReceipt)
	// Observe consumes this turn's native events in order.
	Observe func(context.Context, AgentScope, AgentEvent) error
	// PromptReturned ends in-flight host observation before durable outcome settlement.
	PromptReturned func()
	// AppendLive delivers the same normalized text fragments and coverage versions before persistence.
	AppendLive func(MessageChange)
	// CheckpointLive delivers committed message checkpoints and tail changes.
	CheckpointLive func(context.Context, CommitReceipt, MessageChange)
	// FinishLive releases this turn's live projection and pending delivery windows.
	FinishLive func()
	// Approve handles approval requests for this turn's admitted execution.
	Approve func(context.Context, AgentScope, AgentApproval) (string, error)
	// Input handles structured questions for this turn's admitted execution.
	Input func(context.Context, AgentScope, AgentInputRequest) (AgentInputResponse, error)
	// CompleteInput handles completion of this turn's URL interaction requests.
	CompleteInput func(context.Context, AgentScope, string) error
}

// TurnResult keeps admission, native outcome and local delivery faults separate.
type TurnResult struct {
	// timing records adapter and settlement boundaries for detached callers.
	timing TextTimings
	// BusinessAdmitted is true once the durable BeforeSubmit batch was written.
	BusinessAdmitted bool
	// InteractionRequested records native approval or input requests, including requests denied before a business record exists.
	InteractionRequested bool
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
	// requestedFast is the per-turn selection accepted by the native settings boundary.
	requestedFast *bool
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
	// submissionOpenedAt marks when authorization allows the native prompt to leave Core.
	submissionOpenedAt time.Time
	// phase distinguishes preparing, prepared, running and settling.
	phase turnPhase
	// conn references the actual process owner, as set during preparation.
	conn *providerConnection
	// binding is the pending or committed native session for this Turn.
	binding NativeBinding
	// effective is the native configuration reported before prompt submission.
	effective AgentSession
	// freshSession indicates this native session has no earlier provider inputs.
	freshSession bool
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
	// interactionRequested records requests addressed to this input even when no handler is authorized.
	interactionRequested bool
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

// AddContext records current facts from acquired resources before native submission.
func (t *Turn) AddContext(facts ...TurnFact) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.phase != turnPrepared {
		return status_error.RuntimeUnavailable
	}
	if err := t.ctx.Err(); err != nil {
		return err
	}
	t.input.Context = append(t.input.Context, facts...)
	return nil
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
	// A failed, indeterminate submission leaves the native owner unsafe to reuse.
	// Release drains detached receipts and keeps the durable binding for a later resume.
	unconfirmed := result.BusinessAdmitted && result.OperationError != nil && result.Outcome == nil &&
		result.Admission == string(native.AdmissionUnknown) &&
		(result.Submission == string(native.SubmissionSent) || result.Submission == string(native.SubmissionUnknown))
	incomplete := result.Outcome != nil && incompleteNativeOutcome(*result.Outcome)
	cleanupStarted := time.Now()
	result.CleanupError = t.settle(unconfirmed || incomplete || t.thread.chatID == "" && t.thread.textSession == "" || t.opened && !t.admitted)
	result.timing.Settlement += time.Since(cleanupStarted)
	return result
}

func (t *Turn) runPrompt() TurnResult {
	if t.ports.FinishLive != nil {
		defer t.ports.FinishLive()
	}
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
	prompt, coreMemory := t.promptRequest()
	submissionStarted := time.Now()
	run, err := client.ReservePrompt(t.ctx, prompt)
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
	returnedAt := time.Now()
	if t.ports.PromptReturned != nil {
		t.ports.PromptReturned()
	}
	t.mu.Lock()
	submissionOpenedAt := t.submissionOpenedAt
	t.authorized = false
	t.phase = turnFinishing
	revoke := t.revoke
	res.BusinessAdmitted = t.admitted
	res.InteractionRequested = t.interactionRequested
	t.mu.Unlock()
	if submissionOpenedAt.IsZero() {
		res.timing.BeforeSubmit = returnedAt.Sub(submissionStarted)
	} else {
		res.timing.Submitted = true
		res.timing.BeforeSubmit = submissionOpenedAt.Sub(submissionStarted)
		res.timing.NativeTurn = returnedAt.Sub(submissionOpenedAt)
	}
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
	if res.Outcome != nil && !incompleteNativeOutcome(*res.Outcome) {
		l := t.lifecycle
		reader := l.sessionReader(t.thread)
		outcome := *res.Outcome
		outcome.streamAnswerEligible = t.binding.Session.Provider == constants.RuntimeIDWorkBuddy &&
			outcome.FinalMessage == nil && outcome.StopReason == "end_turn" && res.BusinessAdmitted &&
			!res.InteractionRequested && res.StorageError == nil && res.OperationError == nil && res.DeliveryError == nil
		var err error
		if reader != nil && reader.session == t.binding.Session {
			err = l.persistReceipt(finalize, reader, outcome, t.ports.Committed)
		} else {
			err = l.commitAndAcknowledge(finalize, client, t.binding.Session, outcome, t.ports.Committed, t.thread.textSession != "")
		}
		res.StorageError = errors.Join(res.StorageError, err)
	}
	res.DeliveryError = errors.Join(res.DeliveryError, t.live.finish(finalize))
	if coreMemory != nil {
		t.confirmCoreMemory(*coreMemory, res)
	}
	res.timing.Settlement = time.Since(returnedAt)
	return res
}

func (t *Turn) beforeSubmit(ctx context.Context, submission AgentPromptSubmission) error {
	if verify := t.conn.accountLease.VerifyLogin; verify != nil {
		if err := verify(ctx); err != nil {
			return err
		}
	}
	if t.input.authorizeText != nil {
		if err := t.input.authorizeText(ctx); err != nil {
			return err
		}
	}
	admission, receipt, err := t.recordInput(ctx, t.input, t.ports, submission)
	if err != nil && receipt.Version == 0 {
		return err
	}
	if t.thread.chatID != "" || t.thread.textSession != "" {
		t.publishBinding()
	}
	t.mu.Lock()
	t.admitted = true
	t.buffer.chatVersion = receipt.Version
	if t.ports.AppendLive != nil {
		t.buffer.revision = receipt.Version
	}
	t.adapterInputID = submission.InputID
	t.mu.Unlock()
	if err != nil {
		// The host rejected final authorization after the journal accepted the input.
		// Surface that receipt for projection and settlement without sending the prompt.
		if t.ports.Committed != nil {
			t.ports.Committed(receipt)
		}
		return err
	}
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
	if t.authorized {
		t.submissionOpenedAt = time.Now()
	}
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
	admission.Input.Attribution = &UsageAttribution{ChatID: t.thread.chatID, AccountID: t.conn.account.id,
		WakerID: input.UsageWakerID, Source: input.UsageSource}
	if input.ExecutionID == "" {
		if t.thread.textSession != "" && t.freshSession {
			err = t.lifecycle.store.(textBindingStore).WriteTextBinding(ctx, t.thread.textSession, admission.Binding)
		}
		if err == nil {
			err = t.lifecycle.store.CommitDetachedInput(ctx, admission.Input)
		}
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
		commit := func(ctx context.Context) error {
			var err error
			receipt, err = commitConversation(ctx, t.lifecycle.store, batch)
			return err
		}
		if ports.CommitAdmission != nil {
			err = ports.CommitAdmission(ctx, commit)
			if err == nil && receipt.Version == 0 {
				err = status_error.ChatLogRecordInvalid
			}
		} else {
			err = commit(ctx)
		}
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
		!validTurnInput(input) {
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
		Text: input.Text, Content: input.Content, Context: input.Context,
		BeforeSubmit: func(ctx context.Context, submission AgentPromptSubmission) error {
			if !t.matches(scope) {
				return status_error.RuntimeUnavailable
			}
			if verify := t.conn.accountLease.VerifyLogin; verify != nil {
				if err := verify(ctx); err != nil {
					return err
				}
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
		t.settle(t.thread.chatID == "" && t.thread.textSession == "" || t.opened)
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
		} else if !l.closing && !state.closing && state.textSession != "" && state.connection != nil {
			state.textIdleGeneration++
			generation := state.textIdleGeneration
			state.textIdle = time.AfterFunc(textSessionIdleTimeout, func() { l.expireTextThread(state, generation) })
		}
	}
	close(t.done)
	l.mu.Unlock()
	return t.cleanupErr
}

func (t *Turn) deltaTarget() deltaTarget {
	store := t.lifecycle.store
	if t.input.privateText != nil {
		store = t.input.privateText
	}
	return deltaTarget{store: store, session: t.binding.Session,
		chatID: t.thread.chatID, hostID: t.input.HostInvocationID, messageID: t.input.AssistantMessageID,
		executionID: t.input.ExecutionID, committed: t.ports.Committed,
		appendLive: t.ports.AppendLive, checkpointLive: t.ports.CheckpointLive}
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

// matchingInteraction records native interaction evidence before handler policy can cancel the request.
func (l *ThreadLifecycle) matchingInteraction(state *threadState, generation uint64, scope AgentScope) *Turn {
	t := l.matchingTurn(state, generation, scope)
	if t != nil {
		t.mu.Lock()
		t.interactionRequested = true
		t.mu.Unlock()
	}
	return t
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
			if state.textSession == "" && scope.HostInvocationID == "" && scope.NativeInvocationID != "" {
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
			t := l.matchingInteraction(state, generation, scope)
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
			t := l.matchingInteraction(state, generation, scope)
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
			t := l.matchingInteraction(state, generation, scope)
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
	if l.closing || state == nil || state.closing || (state.active != nil || state.fork != nil) || state.binding == nil || state.connection == nil {
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
	storeErr := l.commitAndAcknowledge(storeCtx, client, binding.Session, AgentNativeOutcome{Turn: *result.Turn}, nil, false)
	return result, errors.Join(operationErr, storeErr)
}
