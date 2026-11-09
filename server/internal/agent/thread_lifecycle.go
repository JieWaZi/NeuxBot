package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
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
	// Selection carries the product selectors applied through the native option catalog.
	Selection *TurnSelection
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
	// admittedAccount identifies the selection whose preparation currently owns the seat.
	admittedAccount providerAccount
	// admittedPlugins identifies resources reserved by the active preparation.
	admittedPlugins map[string]string
	// admittedSkills fences incompatible physical targets during preparation.
	admittedSkills *SkillSelection
	// active is the actual Turn holding the seat from preparation through settlement.
	active *Turn
	// fork owns a no-input native fork while the same Chat seat is reserved.
	fork *threadFork
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

// threadFork reserves the existing Chat seat without inventing a host input or Turn.
type threadFork struct {
	// cancel stops native preparation and the outstanding fork call.
	cancel context.CancelFunc
	// done closes after the operation has attempted cleanup under the same owner.
	done chan struct{}
}

// ForkSession creates native history without admitting or submitting a model input.
// The returned binding is published by the host's durable fork operation.
func (l *ThreadLifecycle) ForkSession(ctx context.Context, request ThreadRequest, acquire func(context.Context, ResourceSelection) (ResourceLease, error)) (binding NativeBinding, resultErr error) {
	if request.ChatID == "" || request.Fork == nil {
		return binding, status_error.AgentInputInvalid
	}
	// A failed cleanup keeps the original owner. Retry that owner before opening
	// the same durable fork receipt; never replace it while resources remain held.
	l.mu.Lock()
	previous := l.threads[request.ChatID]
	retryCleanup := previous != nil && previous.closing && previous.active == nil && previous.fork == nil
	l.mu.Unlock()
	if retryCleanup {
		if err := l.closeCapturedThread(ctx, previous); err != nil {
			return binding, err
		}
	}
	request.Connection = freezeProviderRequest(request.Connection)
	cfg, err := freezeThreadSettings(&request)
	if err != nil {
		return binding, err
	}
	if request.Fork.HistoryIdentity != "" {
		var current threadConfiguration
		if json.Unmarshal(cfg, &current) != nil || current.Identity != request.Fork.HistoryIdentity {
			return binding, status_error.ChatForkUnavailable
		}
	}
	operationContext, cancel := context.WithCancel(ctx)
	operation := &threadFork{cancel: cancel, done: make(chan struct{})}
	l.mu.Lock()
	state := l.threads[request.ChatID]
	if l.closing || state != nil && (state.closing || state.active != nil || state.fork != nil) {
		l.mu.Unlock()
		cancel()
		return binding, status_error.RuntimeUnavailable
	}
	for _, other := range l.threads {
		if other != state && (other.active != nil || other.fork != nil) && skillsConflict(other.admittedSkills, request.Connection.Resources.Skills) {
			l.mu.Unlock()
			cancel()
			return binding, status_error.RuntimeUnavailable
		}
	}
	if state == nil {
		state = &threadState{id: request.ChatID, chatID: request.ChatID}
		l.threads[state.id] = state
	}
	state.fork = operation
	state.admittedAccount = providerAccount{provider: request.Connection.Account.Provider, id: request.Connection.Account.AccountID}
	state.admittedPlugins = maps.Clone(request.Connection.Resources.Plugins)
	state.admittedSkills = request.Connection.Resources.Skills
	l.mu.Unlock()
	defer func() {
		cancel()
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), turnSettlementTimeout)
		defer stop()
		cleanupErr := l.closeConnection(cleanup, state)
		resultErr = errors.Join(resultErr, cleanupErr)
		l.mu.Lock()
		state.fork = nil
		if cleanupErr == nil && l.threads[state.id] == state {
			delete(l.threads, state.id)
		}
		close(operation.done)
		l.mu.Unlock()
	}()
	if err := l.closeConnection(operationContext, state); err != nil {
		return binding, err
	}
	conn, err := l.openConnection(operationContext, state, request.Connection, acquire, nil)
	if err != nil {
		return binding, err
	}
	client, err := conn.Client()
	if err != nil {
		return binding, err
	}
	fork := *request.Fork
	if !fork.ReconcileOnly {
		gate := fork.BeforeFork
		fork.BeforeFork = func(ctx context.Context) error {
			if verify := conn.accountLease.VerifyLogin; verify != nil {
				if err := verify(ctx); err != nil {
					return err
				}
			}
			if gate != nil {
				return gate(ctx)
			}
			return nil
		}
	}
	fork.Session = request.Session
	if request.Connection.Account.Provider == "codex" {
		fork.Session.CodexMode = request.Mode
	}
	session, err := client.ForkSession(operationContext, fork)
	if err != nil {
		return binding, err
	}
	if session.ID == "" || session.ID == fork.SourceSessionID {
		return binding, status_error.ChatForkIndeterminate
	}
	return NativeBinding{Session: NativeSessionKey{Provider: request.Connection.Account.Provider, Subject: request.Connection.Account.Subject, SessionID: session.ID}, Configuration: cfg}, nil
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
	// A prior release failure retains this exact owner. Retry it before
	// reserving a new Turn instead of permanently fencing the public Chat.
	l.mu.Lock()
	previous := l.threads[id]
	retryCleanup := previous != nil && previous.closing && previous.active == nil && previous.fork == nil
	l.mu.Unlock()
	if retryCleanup {
		if err := l.closeCapturedThread(ctx, previous); err != nil {
			if closeFiles != nil {
				_ = closeFiles(context.WithoutCancel(ctx))
			}
			return nil, err
		}
	}
	request.Connection = freezeProviderRequest(request.Connection)
	l.mu.Lock()
	// Check and publish the preparing consumer under one admission lock. Two
	// different Chats cannot both observe an empty target before either registers.
	for _, other := range l.threads {
		if other.id != id && (other.active != nil || other.fork != nil) && skillsConflict(other.admittedSkills, request.Connection.Resources.Skills) {
			l.mu.Unlock()
			if closeFiles != nil {
				_ = closeFiles(context.WithoutCancel(ctx))
			}
			return nil, status_error.RuntimeUnavailable
		}
	}
	state := l.threads[id]
	if l.closing || state != nil && (state.closing || state.active != nil || state.fork != nil) {
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
	state.admittedAccount = providerAccount{provider: request.Connection.Account.Provider, id: request.Connection.Account.AccountID}
	state.admittedPlugins = maps.Clone(request.Connection.Resources.Plugins)
	state.admittedSkills = request.Connection.Resources.Skills
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
	compatible := !request.ForceNew && request.Fork == nil && candidateBinding(stored, request.Connection.Account, cfg)
	processConfiguration := threadProcessConfiguration(request)
	var replacementHandoff *NativeHandoff
	if stored != nil && request.Fork == nil && request.Connection.Account.Provider != stored.Session.Provider {
		replacementHandoff, err = l.failedHandoffForReplacement(ctx, state.chatID, *stored)
		if err != nil {
			return err
		}
	}
	sameOwner := conn != nil && hot != nil && stored != nil && hot.Session == stored.Session && string(hot.Configuration) == string(stored.Configuration)
	hotUsable := false
	if sameOwner {
		_, err := conn.Client()
		hotUsable = err == nil
	}
	if compatible && hotUsable {
		client, err := conn.Client()
		if err != nil {
			hotUsable = false
		} else {
			compatible = resumableConfiguration(stored.Configuration, cfg, client.Capabilities())
		}
	}
	windowChanged := hotUsable && conn.qoderContextWindow != request.Connection.Launch.QoderContextWindow
	if compatible && hotUsable && conn.configuration == processConfiguration && !windowChanged {
		binding, err := l.reconcileHandoff(ctx, state.chatID, *stored, reader)
		if err != nil {
			return err
		}
		t.setSession(conn, binding)
		t.effective = effective
		{
			client, err := conn.Client()
			if err != nil {
				return err
			}
			if err := t.applySettings(client, request); err != nil {
				// A setter may have changed native state before failing. Retire the
				// process, retaining the durable session for an explicit next attempt.
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), turnSettlementTimeout)
				defer cancel()
				return errors.Join(err, l.closeConnection(cleanup, state))
			}
		}
		return t.applyHandoff(binding.Handoff)
	}

	var summary *NativeResultKey
	wantSummary := request.Summarize && request.Fork == nil && !compatible && stored != nil && replacementHandoff == nil
	if hotUsable {
		prior := *stored
		if replacementHandoff == nil {
			prior, err = l.reconcileHandoff(ctx, state.chatID, prior, reader)
			if err != nil {
				return err
			}
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
	if !compatible && stored != nil && replacementHandoff == nil && (wantSummary || handoffClaimed(stored.Handoff)) {
		var prior NativeBinding
		prior, summary, err = l.reopenHandoffSource(t, *stored, wantSummary)
		if err != nil {
			return err
		}
		stored = &prior
	}
	if compatible && !hotUsable && !compatibleBinding(stored, request.Connection.Account, cfg) && (request.Summarize || handoffClaimed(stored.Handoff)) {
		// A cold adapter may reject reloading changed instructions or resources.
		// Resolve the source before acquiring this turn's temporary workspace;
		// retiring a speculative new owner must never delete its input files.
		prior, sourceSummary, sourceErr := l.reopenHandoffSource(t, *stored, request.Summarize)
		if sourceErr != nil {
			return sourceErr
		}
		stored, summary = &prior, sourceSummary
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
	if compatible && !resumableConfiguration(stored.Configuration, cfg, client.Capabilities()) {
		// Cold recovery cannot assume capabilities from a previous process.
		compatible = false
	}
	conn.configuration = processConfiguration
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
		if missing && (windowChanged || request.Connection.Launch.QoderContextWindow != 0) {
			return err
		}
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
	if replacementHandoff != nil {
		t.binding.Handoff = replacementHandoff
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
	// Retire idle consumers before ProviderRuntime reserves and materializes the
	// target. Active and preparing consumers are never canceled for replacement.
	if err := l.closeIdleConsumers(ctx, status_error.RuntimeUnavailable, func(other *threadState) bool {
		return other != state && (other.connection != nil && skillsConflict(other.connection.skills, request.Resources.Skills) || (other.active != nil || other.fork != nil) && skillsConflict(other.admittedSkills, request.Resources.Skills))
	}); err != nil {
		return nil, err
	}
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
	source.Thread.Connection = freezeProviderRequest(source.Thread.Connection)
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

// threadConfiguration retains only nonsecret native-history compatibility facts.
type threadConfiguration struct {
	// Identity covers the selected account, history root and authorized workspace policy.
	Identity string `json:"identity"`
	// Instructions identifies stable host instructions, independently reloadable by some adapters.
	Instructions string `json:"instructions"`
	// Resources identifies the declared tools and skill versions, independently of process startup.
	Resources string `json:"resources"`
}

// HistoryIdentity returns only the compatibility evidence needed to address native history.
func (binding NativeBinding) HistoryIdentity() string {
	var config threadConfiguration
	if json.Unmarshal(binding.Configuration, &config) != nil {
		return ""
	}
	return config.Identity
}

func candidateBinding(binding *NativeBinding, account AccountRequest, config json.RawMessage) bool {
	if binding == nil || binding.Session.Provider != account.Provider || binding.Session.Subject != account.Subject {
		return false
	}
	var before, after threadConfiguration
	return json.Unmarshal(binding.Configuration, &before) == nil && json.Unmarshal(config, &after) == nil && before.Identity == after.Identity
}

func resumableConfiguration(previous, current json.RawMessage, capabilities AgentCapabilities) bool {
	var before, after threadConfiguration
	if json.Unmarshal(previous, &before) != nil || json.Unmarshal(current, &after) != nil || before.Identity != after.Identity {
		return false
	}
	return (before.Resources == after.Resources || capabilities.ReloadResourcesOnResume) &&
		(before.Instructions == after.Instructions || capabilities.ReloadInstructionsOnResume)
}

func configurationDigest(value any) string {
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func threadProcessConfiguration(request ThreadRequest) string {
	account := request.Connection.Account
	account.WaitForLease = false
	if request.Selection != nil {
		// Existing session setters own these models; launch materialization does
		// not make a per-turn model selection a different native history.
		switch account.Provider {
		case "codex", "claude", "kimi", "gemini", "grok", "opencode", "pi":
			account.Model = ""
		}
	}
	return configurationDigest([]any{account, request.Connection.Launch, request.Connection.Resources, request.Session, request.ResourceGeneration})
}

func freezeThreadSettings(request *ThreadRequest) (json.RawMessage, error) {
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
	// Deliver authorized directories through each CLI's actual entry point and include process settings in session compatibility checks.
	if account.Provider == "gemini" {
		if len(request.Session.AdditionalDirectories) > 5 {
			return nil, status_error.RuntimeWorkspaceScopeUnsupported.WithParams(map[string]interface{}{"MaxDirectories": 5})
		}
		for _, directory := range request.Session.AdditionalDirectories {
			// Gemini splits commas and trims whitespace, so authorized paths must be passed unchanged.
			if strings.Contains(directory, ",") || strings.TrimSpace(directory) != directory {
				return nil, status_error.RuntimeWorkspaceScopeUnsupported
			}
		}
	}
	if nativeAdditionalDirectoryFlag(account.Provider) != "" && len(request.Session.AdditionalDirectories) != 0 {
		request.Connection.Launch.ACPAdditionalDirectories = request.Session.AdditionalDirectories
		request.Session.AdditionalDirectories = nil
	}

	if request.Selection != nil && (account.Provider == "qoder" || account.Provider == "qoder-cn") {
		request.Connection.Launch.QoderContextWindow = 0
		if window := request.Selection.ContextWindow; window != "" {
			size, err := strconv.ParseInt(window, 10, 64)
			if err != nil || size <= 0 || strconv.FormatInt(size, 10) != window {
				return nil, status_error.RuntimeConfigurationInvalid
			}
			request.Connection.Launch.QoderContextWindow = size
		}
	}
	if request.Selection != nil {
		settings := *request.Selection
		if settings.Fast != nil {
			fast := *settings.Fast
			settings.Fast = &fast
		}
		if settings.DefaultFast != nil {
			fast := *settings.DefaultFast
			settings.DefaultFast = &fast
		}
		request.Selection = &settings
	}
	// Credential generations and startup environment decide process reuse, not
	// whether a verified subject can access the same native history directory.
	historyHome := account.DataHome
	if historyHome == "" {
		historyHome = account.ConfigHome
		if historyHome == "" {
			historyHome = request.Connection.Launch.ConfigHome
		}
	}
	directories := append(slices.Clone(request.Session.AdditionalDirectories), request.Connection.Launch.ACPAdditionalDirectories...)
	slices.Sort(directories)
	connectors := slices.Clone(request.Connection.Resources.ConnectorIDs)
	slices.Sort(connectors)
	configuration := threadConfiguration{
		Identity: configurationDigest([]any{account.AccountID, historyHome, account.NativeConfigHome, account.CWD, slices.Compact(directories),
			request.Connection.Launch.CursorPermissionMode, request.Connection.Launch.ProcessPermissionMode,
			request.Connection.Launch.Claude, request.Connection.Launch.RequireNoTools}),
		Instructions: configurationDigest(request.Session.DeveloperInstructions),
		Resources:    configurationDigest([]any{request.Connection.Resources.Skills, request.Connection.Resources.Plugins, slices.Compact(connectors), request.ResourceGeneration}),
	}
	encoded, err := json.Marshal(configuration)
	if err != nil {
		return nil, status_error.RuntimeConfigurationInvalid
	}
	return encoded, nil
}

func newTurnID() string {
	id := uuid.New()
	return hex.EncodeToString(id[:])
}

// CloseIdleAccount drains idle consumers while the account authority blocks new leases.
// Active work is checked before withdrawing any idle thread, so a failed mutation
// cannot cancel a conversation or leave unrelated idle owners permanently fenced.
func (l *ThreadLifecycle) CloseIdleAccount(ctx context.Context, accountID string) error {
	return l.closeIdleConsumers(ctx, status_error.AIAccountBusy, func(state *threadState) bool {
		connected := state.connection != nil && state.connection.account.id == accountID
		admitted := (state.active != nil || state.fork != nil) && state.admittedAccount.id == accountID
		return connected || admitted
	})
}

// CloseIdlePlugin releases plugin consumers before the host mutates authorization.
func (l *ThreadLifecycle) CloseIdlePlugin(ctx context.Context, pluginID string) error {
	return l.closeIdleConsumers(ctx, status_error.PluginBusy, func(state *threadState) bool {
		_, admitted := state.admittedPlugins[pluginID]
		connected := false
		if state.connection != nil {
			_, connected = state.connection.plugins[pluginID]
		}
		return connected || (state.active != nil || state.fork != nil) && admitted
	})
}

func (l *ThreadLifecycle) closeIdleConsumers(ctx context.Context, busy error, uses func(*threadState) bool) error {
	l.mu.Lock()
	selected := make([]*threadState, 0)
	for _, state := range l.threads {
		if !uses(state) {
			continue
		}
		if state.active != nil || state.fork != nil {
			l.mu.Unlock()
			return busy
		}
		selected = append(selected, state)
	}
	for _, state := range selected {
		state.closing = true
	}
	l.mu.Unlock()
	var result error
	for _, state := range selected {
		if err := l.closeConnection(ctx, state); err != nil {
			result = errors.Join(result, err)
			continue
		}
		l.mu.Lock()
		if l.threads[state.id] == state {
			delete(l.threads, state.id)
		}
		l.mu.Unlock()
	}
	return result
}

// CloseThread withdraws admission, waits for the captured Turn and retries its process release.
func (l *ThreadLifecycle) CloseThread(ctx context.Context, id string) error {
	l.mu.Lock()
	state := l.threads[id]
	l.mu.Unlock()
	return l.closeCapturedThread(ctx, state)
}

func (l *ThreadLifecycle) closeCapturedThread(ctx context.Context, state *threadState) error {
	if state == nil {
		return nil
	}
	l.mu.Lock()
	// Another cleanup may already have retired this owner and admitted a
	// successor with the same Chat ID. Never look up and stop that successor.
	if l.threads[state.id] != state {
		l.mu.Unlock()
		return nil
	}
	state.closing = true
	active, fork := state.active, state.fork
	l.mu.Unlock()
	if fork != nil {
		fork.cancel()
		select {
		case <-fork.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if active != nil {
		if err := active.Close(ctx); err != nil {
			return err
		}
	}
	if err := l.closeConnection(ctx, state); err != nil {
		return err
	}
	l.mu.Lock()
	if l.threads[state.id] == state {
		delete(l.threads, state.id)
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
	if err := errors.Join(l.drainNativeOutcomes(ctx, reader, true), reader.flushRemaining(ctx)); err != nil {
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
