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
	// Mode selects a native execution policy, when the provider exposes modes.
	Mode string
	// ResourceGeneration changes when a selected connector or materialization changes in place.
	ResourceGeneration uint64
	// HistoryTurns overrides the number of complete turns to seed on replacement.
	HistoryTurns int
	// Summarize prefers a hidden handoff on the previous native session.
	Summarize bool
	// Fork requests a real native fork instead of a new session or resume.
	Fork *AgentForkRequest
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
	conn, hot, effective := state.connection, state.binding, state.effective
	l.mu.Unlock()
	compatible := request.Fork == nil && compatibleBinding(stored, request.Connection.Account, cfg)
	if compatible && conn != nil && hot != nil && hot.Session == stored.Session && string(hot.Configuration) == string(stored.Configuration) {
		if _, err := conn.Client(); err == nil {
			t.setSession(conn, *hot)
			t.effective = effective
			return nil
		}
	}
	var handoff string
	if conn != nil {
		if !compatible && request.Summarize && request.Fork == nil && hot != nil && stored != nil && hot.Session == stored.Session {
			handoff = t.summarize(conn, *hot, effective)
		}
		if err := l.closeConnection(ctx, state); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	state.generation++
	generation := state.generation
	l.mu.Unlock()
	conn, err = l.provider.Open(ctx, request.Connection, ProviderConnectionPorts{
		AcquireResources: t.ports.AcquireResources, CloseFiles: closeFiles,
		Callbacks: l.callbacks(state, generation),
	})
	if conn != nil {
		closeFiles = nil
		l.mu.Lock()
		state.connection = conn
		l.mu.Unlock()
		t.opened = true
	}
	if err != nil {
		if conn == nil {
			return err
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), turnSettlementTimeout)
		defer cancel()
		t.opened = false
		cleanupErr := conn.WaitOpenCleanup(cleanup)
		if cleanupErr != nil {
			l.markClosing(state)
			return errors.Join(err, cleanupErr)
		}
		l.mu.Lock()
		state.connection = nil
		l.mu.Unlock()
		return err
	}
	client, err := conn.Client()
	if err != nil {
		return err
	}

	if request.Connection.Launch.RequireNoTools && !client.Capabilities().ToolsDisabled {
		return status_error.RuntimePolicyUnsupported
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
		if errors.Is(err, status_error.RuntimeSessionNotFound) || errors.Is(err, status_error.RuntimePolicyUnsupported) && !client.Capabilities().Resume && !client.Capabilities().Load {
			fresh = true
			session, err = client.NewSession(ctx, request.Session)
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
	t.setSession(conn, binding)
	t.effective = session
	if err := t.applySettings(client, request); err != nil {
		return err
	}
	if fresh && state.chatID != "" && stored != nil {
		if handoff == "" {
			handoff, err = l.recentHistory(ctx, request.ChatID, request.HistoryTurns)
			if err != nil {
				return err
			}
		}
		t.addPrefix(handoff)
	}
	if client.Capabilities().NativeOutcomes {
		l.mu.Lock()
		state.reader = l.startNativeOutcomeReader(client, state, binding.Session)
		l.mu.Unlock()
	}
	return nil
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
	encoded, err := json.Marshal([]any{request.Connection, request.Session, request.Settings, request.Mode, request.ResourceGeneration})
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
	if reader != nil {
		if err := reader.stop(ctx); err != nil {
			l.markClosing(state)
			return err
		}
	}
	if err := l.drainNativeOutcomes(ctx, reader); err != nil {
		l.markClosing(state)
		return err
	}
	if err := conn.Close(ctx); err != nil {
		l.markClosing(state)
		return err
	}
	if err := l.drainNativeOutcomes(ctx, reader); err != nil {
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
