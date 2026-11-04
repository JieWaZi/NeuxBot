package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/google/uuid"
)

const turnSettlementTimeout = 15 * time.Second

// ThreadRequest freezes the public owner, session settings and authorized resources.
type ThreadRequest struct {
	// ChatID identifies a public conversation. Empty creates private detached work.
	ChatID string
	// Connection describes the actual executable, account and resource selection.
	Connection ProviderConnectionRequest
	// Session describes native settings scoped to this session.
	Session AgentSessionOptions
	// Settings lists exact native selectors to apply before submission, in dependency order.
	Settings []SessionSetting
	// CodexSettings carries product selectors that the native client can change in place.
	CodexSettings *CodexTurnSelection
	// Mode selects a native execution policy, when the provider exposes modes.
	Mode string
	// ResourceGeneration changes when a selected connector or materialization changes in place.
	ResourceGeneration uint64
	// HistoryTurns overrides the number of complete turns to seed on replacement.
	HistoryTurns int
	// ForceNew replaces the native session for this turn without changing binding compatibility.
	ForceNew bool
	// Summarize prefers a hidden handoff on the previous native session.
	Summarize bool
	// Fork requests a real native fork instead of a new session or resume.
	Fork *AgentForkRequest
}

// HandoffSource is the host-verified launch plan for an earlier durable binding.
type HandoffSource struct {
	// Thread carries the prior account, authorization generation, and native settings.
	Thread ThreadRequest
	// AcquireResources reacquires only the resources authorized for that binding.
	AcquireResources func(context.Context, ResourceSelection) (ResourceLease, error)
}

// ThreadLifecycle holds one seat per public Chat and coordinates the durable binding.
type ThreadLifecycle struct {
	// provider exclusively owns process and resource acquisition and release.
	provider *ProviderRuntime
	// store owns public and detached admissions, messages and terminal evidence.
	store ConversationStore
	// mu protects index membership and the published runtime fields of each thread.
	mu sync.Mutex
	// threads maps public Chat IDs and private work IDs to their current owner.
	threads map[string]*threadState
	// closing stops new admissions while root cleanup settles.
	closing bool
}

type threadState struct {
	// id is the index key for this thread.
	id string
	// chatID is the durable public Chat identity, empty for detached work.
	chatID string
	// active is the actual Turn holding the seat from preparation through settlement.
	active *Turn
	// binding caches a committed native session address and its compatibility key.
	binding *NativeBinding
	// effective retains the session configuration needed to reuse a compatible process.
	effective AgentSession
	// optionsRevision prevents a synchronous request from overwriting newer native observations.
	optionsRevision uint64
	// modeRevision prevents a mode receipt from overwriting newer native mode notifications.
	modeRevision uint64
	// generation fences callbacks emitted by a prior connection using the same native session ID.
	generation uint64
	// connection is a reference to the process owner in ProviderRuntime.
	connection *providerConnection
	// reader owns the wait for independently arriving native results.
	reader *nativeOutcomeReader
	// closing prevents replacements from overtaking cleanup.
	closing bool
	// closeMu serializes cleanup attempts on the same connection reference.
	closeMu sync.Mutex
}

// NewThreadLifecycle wires the concrete process owner to the shared conversation store.
func NewThreadLifecycle(provider *ProviderRuntime, store ConversationStore) *ThreadLifecycle {
	return &ThreadLifecycle{provider: provider, store: store, threads: make(map[string]*threadState)}
}

// Prepare reserves the actual Turn before acquiring an account or opening a native session.
func (l *ThreadLifecycle) Prepare(ctx context.Context, request ThreadRequest, input TurnRequest, ports TurnPorts) (*Turn, error) {
	return l.prepare(ctx, request, input, ports, nil)
}

func (l *ThreadLifecycle) prepare(ctx context.Context, request ThreadRequest, input TurnRequest, ports TurnPorts, closeFiles func(context.Context) error) (*Turn, error) {
	if request.ChatID == "" && input.HostInvocationID == "" {
		input.HostInvocationID = newTurnID()
	}
	if input.HostInvocationID == "" || input.Text == "" && len(input.Content) == 0 || input.Text != "" && len(input.Content) != 0 ||
		request.ChatID != "" && (input.ExecutionID == "" || input.AssistantMessageID == "" || ports.Admit == nil) ||
		request.ChatID == "" && (input.ExecutionID != "" || input.AssistantMessageID != "" || ports.Admit != nil) {
		if closeFiles != nil {
			_ = closeFiles(context.WithoutCancel(ctx))
		}
		return nil, status_error.AgentInputInvalid
	}
	input.Content = slices.Clone(input.Content)
	id := request.ChatID
	if id == "" {
		id = "private:" + input.HostInvocationID
	}
	l.mu.Lock()
	state := l.threads[id]
	if l.closing || state != nil && (state.closing || state.active != nil) {
		l.mu.Unlock()
		if closeFiles != nil {
			_ = closeFiles(context.WithoutCancel(ctx))
		}
		return nil, status_error.RuntimeUnavailable
	}
	if state == nil {
		state = &threadState{id: id, chatID: request.ChatID}
		l.threads[id] = state
	}
	t := newTurn(context.WithoutCancel(ctx), l, state, input, ports)
	state.active = t
	l.mu.Unlock()

	stopPrepare := context.AfterFunc(ctx, t.cancel)
	if ctx.Err() != nil {
		t.cancel()
	}
	err := l.openTurn(t, request, closeFiles)
	stopPrepare()
	t.mu.Lock()
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = t.ctx.Err()
	}
	if err == nil {
		t.phase = turnPrepared
	}
	t.mu.Unlock()
	if err != nil {
		return nil, errors.Join(err, t.settle(t.opened))
	}
	return t, nil
}

func (l *ThreadLifecycle) openTurn(t *Turn, request ThreadRequest, closeFiles func(context.Context) error) error {
	ctx, state := t.ctx, t.thread
	defer func() {
		if closeFiles != nil {
			_ = closeFiles(context.WithoutCancel(ctx))
		}
	}()
	cfg, err := freezeThreadSettings(&request)
	if err != nil {
		return err
	}
	var stored *NativeBinding
	if state.chatID != "" {
		stored, err = l.store.ReadBinding(ctx, state.chatID)
		if err != nil {
			return err
		}
	}
	l.mu.Lock()
	conn, hot, effective, reader := state.connection, state.binding, state.effective, state.reader
	l.mu.Unlock()
	compatible := !request.ForceNew && request.Fork == nil && compatibleBinding(stored, request.Connection.Account, cfg)
	sameOwner := conn != nil && hot != nil && stored != nil && hot.Session == stored.Session && string(hot.Configuration) == string(stored.Configuration)
	hotUsable := false
	if sameOwner {
		_, err := conn.Client()
		hotUsable = err == nil
	}
	if compatible && hotUsable {
		binding, err := l.reconcileHandoff(ctx, state.chatID, *stored, reader)
		if err != nil {
			return err
		}
		t.setSession(conn, binding)
		t.effective = effective
		if request.CodexSettings != nil {
			client, err := conn.Client()
			if err != nil {
				return err
			}
			if err := t.applySettings(client, request); err != nil {
				return err
			}
		}
		return t.applyHandoff(binding.Handoff)
	}

	var summary *NativeResultKey
	wantSummary := request.Summarize && request.Fork == nil && !compatible && stored != nil
	if hotUsable && !compatible {
		prior, err := l.reconcileHandoff(ctx, state.chatID, *stored, reader)
		if err != nil {
			return err
		}
		if wantSummary && prior.Handoff == nil {
			summary = t.summarize(conn, prior, effective, nil)
		}
		wantSummary = false
		stored = &prior
	}
	if conn != nil {
		if err := l.closeConnection(ctx, state); err != nil {
			return err
		}
	}
	if !compatible && stored != nil && (wantSummary || handoffClaimed(stored.Handoff)) {
		var prior NativeBinding
		prior, summary, err = l.reopenHandoffSource(t, *stored, wantSummary)
		if err != nil {
			return err
		}
		stored = &prior
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	conn, err = l.openConnection(ctx, state, request.Connection, t.ports.AcquireResources, closeFiles)
	if conn != nil {
		closeFiles = nil
	}
	if err != nil {
		return err
	}
	t.opened = true
	client, err := conn.Client()
	if err != nil {
		return err
	}
	if request.Connection.Launch.RequireNoTools && !client.Capabilities().ToolsDisabled {
		return status_error.RuntimePolicyUnsupported
	}
	if request.Connection.Account.Provider == "codex" {
		request.Session.CodexMode = request.Mode
	}
	var session AgentSession
	fresh := !compatible
	switch {
	case request.Fork != nil:
		fork := *request.Fork
		fork.Session = request.Session
		session, err = client.ForkSession(ctx, fork)
		if err == nil && fork.ReconcileOnly {
			session, err = openBoundSession(ctx, client, session.ID, request.Session)
		}
		fresh = false // Native forks already carry provider history.
	case compatible:
		session, err = openBoundSession(ctx, client, stored.Session.SessionID, request.Session)
		missing := errors.Is(err, status_error.RuntimeSessionNotFound) ||
			errors.Is(err, status_error.RuntimePolicyUnsupported) && !client.Capabilities().Resume && !client.Capabilities().Load
		if missing && !handoffClaimed(stored.Handoff) {
			fresh = true
			session, err = client.NewSession(ctx, request.Session)
		}
		if missing && handoffClaimed(stored.Handoff) {
			return status_error.ChatLogNativeOutcomePending
		}
	default:
		session, err = client.NewSession(ctx, request.Session)
	}
	if err != nil {
		return err
	}
	if session.ID == "" || compatible && !fresh && session.ID != stored.Session.SessionID {
		return status_error.RuntimeProtocolIncompatible
	}
	binding := NativeBinding{
		Session:       NativeSessionKey{Provider: request.Connection.Account.Provider, Subject: request.Connection.Account.Subject, SessionID: session.ID},
		Configuration: cfg,
	}
	if compatible && !fresh {
		binding.Handoff = stored.Handoff
	}
	t.setSession(conn, binding)
	t.effective = session
	l.attachSession(state, client, binding.Session, session)
	if binding, err = l.reconcileHandoff(ctx, state.chatID, binding, l.sessionReader(state)); err != nil {
		return err
	}
	t.binding = binding
	if err := t.applySettings(client, request); err != nil {
		return err
	}
	if !fresh || state.chatID == "" {
		return t.applyHandoff(t.binding.Handoff)
	}
	if summary != nil {
		t.binding.Handoff = &NativeHandoff{Summary: summary}
		return t.applyHandoff(t.binding.Handoff)
	}
	version, err := l.store.ReadVersion(ctx, state.chatID)
	if err != nil || version == 0 {
		return err
	}
	pairs, prefix, err := l.selectHistory(ctx, state.chatID, version, request.HistoryTurns)
	if err != nil {
		return err
	}
	if len(pairs) != 0 {
		t.binding.Handoff = &NativeHandoff{History: pairs}
		t.addPrefix(prefix)
	}
	return nil
}

func (l *ThreadLifecycle) openConnection(ctx context.Context, state *threadState, request ProviderConnectionRequest, acquire func(context.Context, ResourceSelection) (ResourceLease, error), closeFiles func(context.Context) error) (*providerConnection, error) {
	l.mu.Lock()
	state.generation++
	generation := state.generation
	l.mu.Unlock()
	conn, err := l.provider.Open(ctx, request, ProviderConnectionPorts{
		AcquireResources: acquire, CloseFiles: closeFiles,
		Callbacks: l.callbacks(state, generation),
	})
	if conn == nil {
		return nil, err
	}
	l.mu.Lock()
	state.connection = conn
	l.mu.Unlock()
	if err == nil {
		return conn, nil
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), turnSettlementTimeout)
	defer cancel()
	if cleanupErr := conn.WaitOpenCleanup(cleanup); cleanupErr != nil {
		l.markClosing(state)
		return conn, errors.Join(err, cleanupErr)
	}
	l.mu.Lock()
	state.connection = nil
	l.mu.Unlock()
	return conn, err
}

func (l *ThreadLifecycle) attachSession(state *threadState, client AgentClient, key NativeSessionKey, effective AgentSession) {
	l.mu.Lock()
	state.effective = effective
	state.optionsRevision = 0
	state.modeRevision = 0
	if client.Capabilities().NativeOutcomes {
		state.reader = l.startNativeOutcomeReader(client, state, key)
	}
	l.mu.Unlock()
}

func (l *ThreadLifecycle) sessionReader(state *threadState) *nativeOutcomeReader {
	l.mu.Lock()
	defer l.mu.Unlock()
	return state.reader
}

func (l *ThreadLifecycle) reopenHandoffSource(t *Turn, binding NativeBinding, summarize bool) (NativeBinding, *NativeResultKey, error) {
	ctx, state := t.ctx, t.thread
	checked, err := l.reconcileHandoff(ctx, state.chatID, binding, nil)
	if err != nil && !errors.Is(err, status_error.ChatLogNativeOutcomePending) {
		return binding, nil, err
	}
	if err == nil && (!summarize || checked.Handoff != nil) {
		return checked, nil, nil
	}
	if t.ports.ResolveHandoffSource == nil {
		return checked, nil, err
	}
	source, resolveErr := t.ports.ResolveHandoffSource(ctx, binding)
	if resolveErr != nil {
		return checked, nil, err
	}
	cfg, freezeErr := freezeThreadSettings(&source.Thread)
	if freezeErr != nil || !compatibleBinding(&binding, source.Thread.Connection.Account, cfg) {
		return checked, nil, err
	}
	conn, openErr := l.openConnection(ctx, state, source.Thread.Connection, source.AcquireResources, nil)
	if openErr != nil {
		l.mu.Lock()
		closing := state.closing
		l.mu.Unlock()
		if closing {
			return checked, nil, openErr
		}
		return checked, nil, err
	}
	var summary *NativeResultKey
	client, clientErr := conn.Client()
	if clientErr == nil {
		if source.Thread.Connection.Account.Provider == "codex" {
			source.Thread.Session.CodexMode = source.Thread.Mode
		}
		session, resumeErr := openBoundSession(ctx, client, binding.Session.SessionID, source.Thread.Session)
		if resumeErr == nil && session.ID == binding.Session.SessionID {
			l.attachSession(state, client, binding.Session, session)
			checked, err = l.reconcileHandoff(ctx, state.chatID, binding, l.sessionReader(state))
			if err == nil && summarize && checked.Handoff == nil {
				summary = t.summarize(conn, checked, session, &source.Thread)
			}
		}
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), turnSettlementTimeout)
	defer cancel()
	return checked, summary, errors.Join(err, l.closeConnection(cleanup, state))
}

func openBoundSession(ctx context.Context, client AgentClient, sessionID string, options AgentSessionOptions) (AgentSession, error) {
	if client.Capabilities().Resume {
		return client.ResumeSession(ctx, sessionID, options)
	}
	// Session-only replay cannot reach the reserved Turn's scoped observers.
	return client.LoadSession(ctx, sessionID, options)
}

func compatibleBinding(binding *NativeBinding, account AccountRequest, config json.RawMessage) bool {
	return binding != nil && binding.Session.Provider == account.Provider && binding.Session.Subject == account.Subject &&
		string(binding.Configuration) == string(config)
}

func freezeThreadSettings(request *ThreadRequest) (json.RawMessage, error) {
	request.Connection = freezeProviderRequest(request.Connection)
	request.Session.AdditionalDirectories = slices.Clone(request.Session.AdditionalDirectories)
	request.Settings = slices.Clone(request.Settings)
	if request.Fork != nil {
		fork := *request.Fork
		request.Fork = &fork
	}
	if request.Session.DeveloperInstructions != nil {
		instructions := *request.Session.DeveloperInstructions
		request.Session.DeveloperInstructions = &instructions
	}
	for i := range request.Settings {
		if request.Settings[i].Value.Bool != nil {
			value := *request.Settings[i].Value.Bool
			request.Settings[i].Value.Bool = &value
		}
	}
	account := &request.Connection.Account
	cwd, err := resolveAgentDirectory(account.CWD)
	if err != nil {
		return nil, err
	}
	account.CWD = cwd
	for i, path := range request.Session.AdditionalDirectories {
		resolved, err := resolveAgentDirectory(path)
		if err != nil {
			return nil, err
		}
		request.Session.AdditionalDirectories[i] = resolved
	}
	// The inherited process environment reaches the child without selecting a
	// thread. Managed overrides and the verified account still determine compatibility.
	connection := request.Connection
	connection.Launch.BaseEnvironment = nil
	if request.CodexSettings != nil && connection.Account.Provider == "codex" {
		settings := *request.CodexSettings
		if settings.Fast != nil {
			fast := *settings.Fast
			settings.Fast = &fast
		}
		if settings.DefaultFast != nil {
			fast := *settings.DefaultFast
			settings.DefaultFast = &fast
		}
		request.CodexSettings = &settings
		// The native client applies model, effort and tier together before each turn.
		connection.Account.Model = ""
	}
	encoded, err := json.Marshal([]any{connection, request.Session, request.Settings, request.Mode, request.ResourceGeneration})
	if err != nil {
		return nil, status_error.RuntimeConfigurationInvalid
	}
	// Only compatibility evidence is persisted. Environment and launch overrides can contain secrets.
	sum := sha256.Sum256(encoded)
	return json.RawMessage(`"` + hex.EncodeToString(sum[:]) + `"`), nil
}

func newTurnID() string {
	id := uuid.New()
	return hex.EncodeToString(id[:])
}

// CloseThread withdraws admission, waits for the captured Turn and retries its process release.
func (l *ThreadLifecycle) CloseThread(ctx context.Context, id string) error {
	l.mu.Lock()
	state := l.threads[id]
	if state == nil {
		l.mu.Unlock()
		return nil
	}
	state.closing = true
	active := state.active
	l.mu.Unlock()
	if active != nil {
		if err := active.Close(ctx); err != nil {
			return err
		}
	}
	if err := l.closeConnection(ctx, state); err != nil {
		return err
	}
	l.mu.Lock()
	if l.threads[id] == state {
		delete(l.threads, id)
	}
	l.mu.Unlock()
	return nil
}

func (l *ThreadLifecycle) closeConnection(ctx context.Context, state *threadState) error {
	state.closeMu.Lock()
	defer state.closeMu.Unlock()
	l.mu.Lock()
	conn, reader := state.connection, state.reader
	l.mu.Unlock()
	if conn == nil {
		return nil
	}
	// Stopping the actual native producer never depends on storage being writable.
	// The reader's borrowed client retains the unacknowledged receipt ledger
	// after the connection and account resources have been released.
	var stopErr error
	if reader != nil {
		stopErr = reader.stop(ctx)
	}
	closeErr := conn.Close(ctx)
	if err := errors.Join(stopErr, closeErr); err != nil {
		l.markClosing(state)
		return err
	}
	if err := errors.Join(l.drainNativeOutcomes(ctx, reader), reader.flushRemaining(ctx)); err != nil {
		l.markClosing(state)
		return err
	}
	l.mu.Lock()
	state.connection, state.binding, state.reader = nil, nil, nil
	state.effective = AgentSession{}
	l.mu.Unlock()
	return nil
}

func (l *ThreadLifecycle) markClosing(state *threadState) {
	l.mu.Lock()
	state.closing = true
	l.mu.Unlock()
}

// Close stops new Turns and cleans up all threads. Failed owners remain indexed for retry.
func (l *ThreadLifecycle) Close(ctx context.Context) error {
	l.mu.Lock()
	l.closing = true
	ids := make([]string, 0, len(l.threads))
	for id := range l.threads {
		ids = append(ids, id)
	}
	l.mu.Unlock()
	var err error
	for _, id := range ids {
		err = errors.Join(err, l.CloseThread(ctx, id))
	}
	return err
}
