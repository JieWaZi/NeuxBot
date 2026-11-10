package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	codex "github.com/JieWaZi/acp-go/pkg/codex"
)

// SystemAccountID identifies selection of the installation's native login.
const SystemAccountID = "system"

// AccountRequest identifies the frozen credential selection and its native launch inputs.
type AccountRequest struct {
	// Executable is the selected CLI used to verify its native account identity.
	Executable string
	// PrefixArgs identifies the selected executable wrapper for account probes.
	PrefixArgs []string
	// Provider identifies the built-in runtime.
	Provider string
	// AccountID identifies the host account, including the explicit system account.
	AccountID string
	// Subject is the verified credential subject or isolated identity used by durable bindings.
	Subject string
	// AuthGeneration increases only when this account's credentials or subject are invalidated.
	AuthGeneration uint64
	// ConfigHome is the frozen configuration root when the account authority selects it.
	ConfigHome string
	// NativeConfigHome preserves a provider's lexical credential scope; empty selects its native default.
	NativeConfigHome string
	// DataHome is the native session storage root, separate from credential identity.
	DataHome string
	// ConfigFingerprint freezes noncredential account settings independently of its subject.
	ConfigFingerprint string
	// WaitForLease makes detached background invocations wait for accounts being modified.
	WaitForLease bool
	// CWD is the absolute, resolved working directory.
	CWD string
	// Model is the selected model when account materialization depends on it.
	Model string
}

// Equal compares the complete frozen selection, including a wrapper's argument vector.
func (r AccountRequest) Equal(other AccountRequest) bool {
	return r.Executable == other.Executable && slices.Equal(r.PrefixArgs, other.PrefixArgs) &&
		r.Provider == other.Provider && r.AccountID == other.AccountID && r.Subject == other.Subject &&
		r.AuthGeneration == other.AuthGeneration && r.ConfigHome == other.ConfigHome && r.NativeConfigHome == other.NativeConfigHome && r.DataHome == other.DataHome &&
		r.CWD == other.CWD && r.Model == other.Model && r.ConfigFingerprint == other.ConfigFingerprint
}

// AccountLease transfers actual account resources, including on acquisition failure.
type AccountLease struct {
	// Environment is the launch environment supplied by the account lease.
	Environment map[string]string
	// ConfigHome is the native configuration root confirmed by the account lease.
	ConfigHome string
	// DataHome is the native session data root confirmed by the account lease.
	DataHome string
	// CodexConfig is the Codex launch and session configuration supplied by the account.
	CodexConfig map[string]json.RawMessage
	// CodexCredentialKind is the Codex credential type required by the lease.
	CodexCredentialKind string
	// VerifyCodexCredential checks Codex process receipts against the selected credentials.
	VerifyCodexCredential func(context.Context, [sha256.Size]byte) error
	// VerifyLogin checks local account selection only; it must not start diagnostics or perform network requests.
	VerifyLogin func(context.Context) error
	// CursorQuota binds quota queries to the current Cursor login identity.
	CursorQuota *CursorQuotaLease
	// QoderUserID is the Qoder user identifier confirmed by the lease for management queries.
	QoderUserID string
	// QoderLoginVerified reports that the lease confirmed Qoder login with the native client.
	QoderLoginVerified bool
	// PrepareLaunchAuth prepares additional launch authentication resources.
	PrepareLaunchAuth func(context.Context) (LaunchAuthLease, error)
	// Close releases the lease after the native process exits and launch authentication resources are cleaned up.
	Close func(context.Context) error
}

// CursorQuotaLease provides explicitly bound Dashboard credentials on demand.
type CursorQuotaLease struct {
	// NativeUserID is the account identifier that the exact Dashboard bearer must prove.
	NativeUserID string
	// Endpoint is the trusted Dashboard origin, or empty for Cursor's default.
	Endpoint string
	// ReadAccessToken resolves the token for this exact lease's native login.
	ReadAccessToken func(context.Context) (string, error)
}

// LaunchAuthLease owns connection-specific authentication material.
type LaunchAuthLease struct {
	// Environment supplies startup authentication without changing account-owned values or the config root.
	Environment map[string]string
	// Close releases startup authentication after native exit.
	Close func(context.Context) error
}

// ResourceSelection identifies host-authorized resources and secret-free compatibility evidence.
type ResourceSelection struct {
	// Skills fixes the exact content and physical discovery target before admission.
	Skills *SkillSelection
	// Plugins fixes the installed version of each selected plugin by ID.
	Plugins map[string]string
	// ConnectorIDs fixes the set of authorized connectors.
	ConnectorIDs []string
}

// ResourceLease transfers the materialized resources needed by the native client.
type ResourceLease struct {
	// Skills confirms the content actually acquired for the reserved native target.
	Skills *SkillSelection
	// Environment contains non-account launch contributions.
	Environment map[string]string
	// MCPServers contains the materialized server configuration.
	MCPServers []AgentMCPServer
	// NativePlugins identifies the isolated, complete plugin roots accepted by this adapter.
	NativePlugins []NativePlugin
	// codexPluginConfig contains only per-process marketplace and enablement settings.
	codexPluginConfig map[string]json.RawMessage
	// Close releases active MCP and other materialized resources.
	Close func(context.Context) error
	// CloseFiles releases temporary files after every native and lease consumer is gone.
	CloseFiles func(context.Context) error
}

// ProviderLaunch contains the facts required to construct one selected native client.
type ProviderLaunch struct {
	// Executable is the detected executable path or a program name in the explicit PATH.
	Executable string
	// PrefixArgs contains launch arguments before the native subcommand.
	PrefixArgs []string
	// BaseEnvironment is the inherited process environment and does not affect session compatibility.
	BaseEnvironment map[string]string `json:",omitempty"`
	// Environment is the explicit launch environment before account lease injection.
	Environment map[string]string
	// ConfigHome is the default native configuration directory.
	ConfigHome string
	// CodexConfig contains native Codex launch configuration overrides.
	CodexConfig map[string]json.RawMessage
	// Claude holds Claude-specific launch requirements.
	Claude ClaudeLaunch
	// CursorPermissionMode is the native Cursor process permission.
	CursorPermissionMode string
	// ProcessPermissionMode is the process permission for ACP adapters and Pi.
	ProcessPermissionMode string
	// ACPAdditionalDirectories contains directories permitted in native launch arguments.
	ACPAdditionalDirectories []string `json:",omitempty"`
	// QoderContextWindow is the window tier frozen at Qoder process startup; zero preserves native settings.
	QoderContextWindow int64 `json:",omitempty"`
	// WorkBuddyCatalogHome is the temporary CodeBuddy configuration directory owned exclusively by the catalog connection.
	WorkBuddyCatalogHome string `json:"-"`
	// RequireVerifiedMCP requires native confirmation of MCP integration.
	RequireVerifiedMCP bool
	// RequireNoTools permits only new native sessions with tools disabled.
	RequireNoTools bool
}

// ClaudeLaunch declares the native settings and tool restrictions for Claude.
type ClaudeLaunch struct {
	// SettingSources names the authorized native settings scopes.
	SettingSources []string
	// Tools is the built-in tool selection. Nil preserves native defaults.
	Tools []string
	// SettingsPath points to an already materialized native settings file.
	SettingsPath string
	// WorkspaceOnly requires the adapter to verify native write restrictions.
	WorkspaceOnly bool
	// ExcludedWriteDirectories identifies protected paths.
	ExcludedWriteDirectories []string
}

// ProviderConnectionRequest freezes the account, startup policy and resource selection.
type ProviderConnectionRequest struct {
	// Account identifies the credentials and real working directory.
	Account AccountRequest
	// Launch selects the native program and startup requirements.
	Launch ProviderLaunch
	// Resources selects host-authorized resources for this connection.
	Resources ResourceSelection
}

// ProviderConnectionPorts contains callbacks supplied by this connection's consumer.
type ProviderConnectionPorts struct {
	// AcquireResources returns all obtained resources, including partial acquisitions.
	AcquireResources func(context.Context, ResourceSelection) (ResourceLease, error)
	// Callbacks routes native observations and interactions to their confirmed owner.
	Callbacks AgentClientCallbacks
	// CloseFiles transfers the already created temporary workspace when Open
	// returns a connection, including on startup failure.
	CloseFiles func(context.Context) error
}

// ProviderClient is a connection adapter with an owned initialization and release.
type ProviderClient interface {
	AgentClient
	Initialize(context.Context) error
	Close(context.Context) error
}

// ProviderRuntimeOptions contains the shared account acquisition port and cleanup bound.
type ProviderRuntimeOptions struct {
	// NewClient selects the process adapter. A nil constructor uses the installed native CLI.
	NewClient func(context.Context, AccountRequest, ProviderLaunch, AccountLease, LaunchAuthLease, ResourceLease, AgentClientCallbacks) (ProviderClient, error)
	// AcquireAccount verifies the requested subject and credential generation against
	// the host authority while acquiring the lease, including partial acquisition.
	AcquireAccount func(context.Context, AccountRequest) (AccountLease, error)
	// CleanupTimeout bounds one cleanup attempt after startup has settled.
	CleanupTimeout time.Duration
}

// ProviderRuntime owns all registered native connections in the application.
type ProviderRuntime struct {
	// systemAuthentication fences new system-account connections during explicit native authentication.
	systemAuthentication map[string]bool
	// restarting fences all new connections while the lifecycle retires one provider's idle owners.
	restarting map[string]bool
	// options freezes the shared acquisition port and cleanup bound.
	options ProviderRuntimeOptions
	// mu guards registration and admission only.
	mu sync.Mutex
	// skillFilesMu serializes root manifest read/validate/publish without holding mu.
	skillFilesMu sync.Mutex
	// closing permanently ends new admission.
	closing bool
	// nextID increases for every admitted attempt.
	nextID uint64
	// connections retains every owner until all of its cleanup has succeeded.
	connections map[uint64]*providerConnection
	// invalidated records the greatest revoked credential generation per account.
	invalidated map[providerAccount]uint64
}

type providerAccount struct {
	// provider is the native runtime identity.
	provider string
	// id is the host account identity.
	id string
}

type providerConnection struct {
	// runtime removes this connection after all cleanup succeeds.
	runtime *ProviderRuntime
	// id is a non-reusable connection number within this runtime.
	id uint64
	// account is the account identity used for connection invalidation.
	account providerAccount
	// generation is the authentication generation frozen for this connection.
	generation uint64
	// plugins contains plugins and frozen versions held by this native connection.
	plugins map[string]string
	// skills is the physical target reservation retained through successful cleanup.
	skills *SkillSelection
	// qoderContextWindow is the actual process launch tier; zero preserves native settings.
	qoderContextWindow int64
	// configuration identifies the actual request whose startup resources this owner acquired.
	configuration string
	// mu protects startup result publication and cleanup attempts, keeping startup and cleanup mutually exclusive.
	mu sync.Mutex
	// cancelOpen signals the startup goroutine to stop without waiting for resource acquisition to return.
	cancelOpen context.CancelFunc
	// opened closes after startup writes and partial lease transfer finish.
	opened chan struct{}
	// openErr is the safe startup error published through opened.
	openErr error
	// closing prevents startup result publication after closure begins.
	closing bool
	// closed reports that all cleanup steps succeeded.
	closed bool
	// firstCloseAttempt retains the cleanup attempt selected on startup failure.
	firstCloseAttempt *connectionCloseAttempt
	// closingAttempt lets concurrent waiters join the same cleanup attempt.
	closingAttempt *connectionCloseAttempt
	// client owns the native process until Close confirms exit.
	client ProviderClient
	// qoderLoginVerified is the native login confirmation published through opened.
	qoderLoginVerified bool
	// accountLease is the acquired account lease.
	accountLease AccountLease
	// launchAuth is the acquired launch authentication lease.
	launchAuth LaunchAuthLease
	// resources is the acquired native resource lease.
	resources ResourceLease
	// closeWorkspace cleans up the adopted working directory after other leases are released.
	closeWorkspace func(context.Context) error
}

type connectionCloseAttempt struct {
	// done closes after the attempt publishes its safe error.
	done chan struct{}
	// err is immutable after done closes.
	err error
}

// NewProviderRuntime constructs the application-level owner without starting any processes.
func NewProviderRuntime(options ProviderRuntimeOptions) *ProviderRuntime {
	if options.CleanupTimeout <= 0 {
		options.CleanupTimeout = 15 * time.Second
	}
	return &ProviderRuntime{
		options: options, connections: make(map[uint64]*providerConnection),
		invalidated: make(map[providerAccount]uint64),
	}
}

// Open registers ownership before starting any acquisition. A non-nil connection
// always identifies the admitted attempt, even if the caller stops waiting.
func (r *ProviderRuntime) Open(ctx context.Context, request ProviderConnectionRequest, ports ProviderConnectionPorts) (*providerConnection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request = freezeProviderRequest(request)
	if skills := request.Resources.Skills; skills != nil {
		cwd, err := resolveAgentDirectory(request.Account.CWD)
		if err != nil || workspacePathKey(cwd) != workspacePathKey(skills.cwd) || !maps.Equal(skills.plugins, request.Resources.Plugins) {
			return nil, status_error.RuntimeConfigurationInvalid
		}
	}

	account := providerAccount{provider: request.Account.Provider, id: request.Account.AccountID}
	openCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	if r.restarting[request.Account.Provider] {
		r.mu.Unlock()
		cancel()
		return nil, status_error.RuntimeRestartBusy
	}
	if r.systemAuthentication[request.Account.Provider] && request.Account.AccountID == SystemAccountID {
		r.mu.Unlock()
		cancel()
		return nil, status_error.AIAccountBusy
	}
	if r.closing {
		r.mu.Unlock()
		cancel()
		return nil, status_error.RuntimeUnavailable
	}
	if generation, invalidated := r.invalidated[account]; invalidated {
		if request.Account.AuthGeneration <= generation {
			r.mu.Unlock()
			cancel()
			return nil, status_error.RuntimeUnavailable
		}
		for _, old := range r.connections {
			if old.account == account && old.generation <= generation {
				r.mu.Unlock()
				cancel()
				return nil, status_error.RuntimeShutdownFailed
			}
		}
	}
	// Admission reserves before any account or resource callback can write files.
	for _, old := range r.connections {
		if skillsConflict(old.skills, request.Resources.Skills) {
			r.mu.Unlock()
			cancel()
			return nil, status_error.RuntimeUnavailable
		}
	}
	r.nextID++
	c := &providerConnection{
		runtime: r, id: r.nextID, account: account, generation: request.Account.AuthGeneration, plugins: request.Resources.Plugins,
		qoderContextWindow: request.Launch.QoderContextWindow, skills: request.Resources.Skills,
		cancelOpen: cancel, opened: make(chan struct{}), closeWorkspace: ports.CloseFiles,
	}
	r.connections[c.id] = c
	r.mu.Unlock()

	go c.open(openCtx, request, ports)
	select {
	case <-c.opened:
	case <-openCtx.Done():
		select {
		case <-c.opened:
		default:
			c.requestClose(false)
			return c, openCtx.Err()
		}
	}
	return c, c.openErr
}

func freezeProviderRequest(request ProviderConnectionRequest) ProviderConnectionRequest {
	request.Account.PrefixArgs = slices.Clone(request.Account.PrefixArgs)
	launch := &request.Launch
	launch.PrefixArgs = slices.Clone(launch.PrefixArgs)
	launch.ACPAdditionalDirectories = slices.Clone(launch.ACPAdditionalDirectories)
	launch.BaseEnvironment = maps.Clone(launch.BaseEnvironment)
	launch.Environment = maps.Clone(launch.Environment)
	launch.CodexConfig = maps.Clone(launch.CodexConfig)
	for key, value := range launch.CodexConfig {
		launch.CodexConfig[key] = bytes.Clone(value)
	}
	launch.Claude.SettingSources = slices.Clone(launch.Claude.SettingSources)
	launch.Claude.Tools = slices.Clone(launch.Claude.Tools)
	launch.Claude.ExcludedWriteDirectories = slices.Clone(launch.Claude.ExcludedWriteDirectories)
	if selection := request.Resources.Skills; selection != nil {
		frozen := *selection
		frozen.entries = slices.Clone(selection.entries)
		frozen.plugins = maps.Clone(selection.plugins)
		request.Resources.Skills = &frozen
	}
	request.Resources.Plugins = maps.Clone(request.Resources.Plugins)
	request.Resources.ConnectorIDs = slices.Clone(request.Resources.ConnectorIDs)
	return request
}

func (c *providerConnection) open(ctx context.Context, request ProviderConnectionRequest, ports ProviderConnectionPorts) {
	err := c.initialize(ctx, request, ports)
	c.mu.Lock()
	if err == nil {
		err = ctx.Err()
	}
	if err == nil && c.closing {
		err = status_error.RuntimeUnavailable
	}
	c.openErr = err
	if err == nil {
		close(c.opened)
		c.mu.Unlock()
		c.cancelOpen()
		return
	}
	c.closing = true
	c.mu.Unlock()
	// Start automatic cleanup without canceling the waiter before it can see
	// the real acquisition or initialization error.
	c.startClose(false)
	close(c.opened)
	c.cancelOpen()
}

// SupportsProvider identifies providers migrated to the single native runtime owner.
func SupportsProvider(provider string) bool {
	switch provider {
	case constants.RuntimeIDCodex, constants.RuntimeIDClaude, constants.RuntimeIDCursor,
		constants.RuntimeIDWorkBuddy, constants.RuntimeIDQoder,
		constants.RuntimeIDKimi, constants.RuntimeIDGemini, constants.RuntimeIDGrok, constants.RuntimeIDOpenCode, constants.RuntimeIDPi:
		return true
	}
	return false
}

func (c *providerConnection) initialize(ctx context.Context, request ProviderConnectionRequest, ports ProviderConnectionPorts) error {
	account := request.Account
	if account.Executable != "" && (account.Executable != request.Launch.Executable || !slices.Equal(account.PrefixArgs, request.Launch.PrefixArgs)) {
		return status_error.AIAccountConfigurationInvalid
	}
	if !SupportsProvider(account.Provider) {
		return status_error.RuntimeNotRegistered
	}
	if request.Launch.RequireNoTools && !SupportsDetachedPolicy(account.Provider) {
		return status_error.RuntimePolicyUnsupported
	}
	if c.runtime.options.AcquireAccount == nil || account.AccountID == "" || account.Subject == "" {
		return status_error.AIAccountConfigurationInvalid
	}
	cwd, err := resolveAgentDirectory(account.CWD)
	if err != nil {
		return err
	}
	account.CWD = cwd

	if err := ctx.Err(); err != nil {
		return err
	}
	c.accountLease, err = c.runtime.options.AcquireAccount(ctx, account)
	if err != nil {
		return safeAgentError(err, status_error.AIAccountConfigurationInvalid)
	}
	if account.ConfigHome != "" && request.Launch.ConfigHome != "" {
		root, err := resolveAgentDirectory(request.Launch.ConfigHome)
		if account.Provider != constants.RuntimeIDCodex && filepath.IsAbs(request.Launch.ConfigHome) && request.Launch.ConfigHome == account.ConfigHome {
			// The account authority admitted this canonical settings root, including an absent leaf.
			root, err = account.ConfigHome, nil
		}
		if err != nil || root != account.ConfigHome {
			return status_error.AIAccountConfigurationInvalid
		}
	}
	if account.ConfigHome != "" && c.accountLease.ConfigHome != account.ConfigHome {
		return status_error.AIAccountConfigurationInvalid
	}
	if account.DataHome != "" && c.accountLease.DataHome != account.DataHome {
		return status_error.AIAccountConfigurationInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.accountLease.PrepareLaunchAuth != nil {
		c.launchAuth, err = c.accountLease.PrepareLaunchAuth(ctx)
		if err != nil {
			return safeAgentError(err, status_error.RuntimeAuthRequired)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if ports.AcquireResources != nil {
		c.resources, err = ports.AcquireResources(ctx, request.Resources)
		if err != nil {
			return safeAgentError(err, status_error.RuntimeConfigurationInvalid)
		}
	} else if request.Resources.Skills != nil || len(request.Resources.Plugins) != 0 || len(request.Resources.ConnectorIDs) != 0 {
		return status_error.RuntimeConfigurationInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if !SameSkills(request.Resources.Skills, c.resources.Skills) {
		return status_error.SkillContentInvalid
	}
	if request.Resources.Skills != nil {
		// File transactions never call back into ThreadLifecycle or acquire runtime mu.
		c.runtime.skillFilesMu.Lock()
		if err = ctx.Err(); err == nil {
			err = prepareSkills(request.Resources.Skills)
		}
		c.runtime.skillFilesMu.Unlock()
		if err != nil {
			return err
		}
	}
	if len(c.resources.NativePlugins) > 0 && account.Provider == constants.RuntimeIDCodex {
		var closeCache func(context.Context) error
		c.resources.codexPluginConfig, closeCache, err = prepareCodexPluginCache(ctx, account, c.accountLease, c.resources.NativePlugins)
		if err != nil {
			return err
		}
		closeViews := c.resources.CloseFiles
		c.resources.CloseFiles = func(ctx context.Context) error {
			if err := closeCache(ctx); err != nil {
				return err
			}
			if closeViews != nil {
				return closeViews(ctx)
			}
			return nil
		}
	}
	c.qoderLoginVerified = c.accountLease.QoderLoginVerified
	if build := c.runtime.options.NewClient; build != nil {
		c.client, err = build(ctx, account, request.Launch, c.accountLease, c.launchAuth, c.resources, ports.Callbacks)
	} else {
		var direct *directAgentClient
		direct, err = newDirectAgentClient(ctx, account, request.Launch, c.accountLease, c.launchAuth, c.resources, ports.Callbacks)
		if direct != nil {
			c.client = direct
		}
	}
	if err != nil {
		return err
	}
	c.mu.Lock()
	closing := c.closing
	c.mu.Unlock()
	if closing {
		return status_error.RuntimeUnavailable
	}
	// Close either withdrew startup before this point, or owns the same client
	// after its one initialization attempt settles.
	if c.client == nil {
		return status_error.RuntimeUnavailable
	}
	if err := c.client.Initialize(ctx); err != nil {
		return err
	}
	if verify := c.accountLease.VerifyCodexCredential; verify != nil {
		return c.verifyCodexCredential(ctx, verify)
	}
	return nil
}

func (c *providerConnection) verifyCodexCredential(ctx context.Context, verify func(context.Context, [sha256.Size]byte) error) error {
	direct, ok := c.client.(*directAgentClient)
	if !ok {
		return status_error.AIAccountConfigurationInvalid
	}
	native, ok := direct.native.(*codex.Client)
	if !ok {
		return status_error.AIAccountConfigurationInvalid
	}
	kind := c.accountLease.CodexCredentialKind
	if kind == "" {
		kind = "account"
	}
	reportedRoot, proof, err := native.CredentialProof(ctx, kind)
	if err != nil {
		return safeAgentError(err, status_error.RuntimeAuthRequired)
	}
	root, err := resolveAgentDirectory(reportedRoot)
	if err != nil || root != c.accountLease.ConfigHome {
		return status_error.AIAccountConfigurationInvalid
	}
	return safeAgentError(verify(ctx, proof), status_error.RuntimeAuthRequired)
}

// Client returns the initialized client while this connection accepts use.
func (c *providerConnection) Client() (AgentClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.opened:
		if !c.closing && c.openErr == nil {
			return c.client, nil
		}
	default:
	}
	return nil, status_error.RuntimeUnavailable
}

// Close stops admission to this connection and waits for a cleanup attempt.
// A caller's deadline never releases the connection's ownership.
func (c *providerConnection) Close(ctx context.Context) error {
	return waitConnectionClose(ctx, c.requestClose(true))
}

// WaitOpenCleanup waits for the attempt already selected by an unsuccessful Open.
// Unlike Close, it does not retry a failed release or hide its original error.
func (c *providerConnection) WaitOpenCleanup(ctx context.Context) error {
	c.mu.Lock()
	attempt := c.firstCloseAttempt
	c.mu.Unlock()
	return waitConnectionClose(ctx, attempt)
}

func (c *providerConnection) requestClose(retry bool) *connectionCloseAttempt {
	attempt := c.startClose(retry)
	c.cancelOpen()
	return attempt
}

func (c *providerConnection) markClosing() {
	c.mu.Lock()
	c.closing = true
	c.mu.Unlock()
}

func (c *providerConnection) startClose(retry bool) *connectionCloseAttempt {
	c.mu.Lock()
	c.closing = true
	if !retry && c.firstCloseAttempt != nil {
		attempt := c.firstCloseAttempt
		c.mu.Unlock()
		return attempt
	}
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	if attempt := c.closingAttempt; attempt != nil {
		select {
		case <-attempt.done:
		default:
			c.mu.Unlock()
			return attempt
		}
	}
	attempt := &connectionCloseAttempt{done: make(chan struct{})}
	if c.firstCloseAttempt == nil {
		c.firstCloseAttempt = attempt
	}
	c.closingAttempt = attempt
	c.mu.Unlock()
	go c.clean(attempt)
	return attempt
}

func (c *providerConnection) clean(attempt *connectionCloseAttempt) {
	<-c.opened
	ctx, cancel := context.WithTimeout(context.Background(), c.runtime.options.CleanupTimeout)
	defer cancel()
	err := c.release(ctx)
	if err == nil {
		c.runtime.mu.Lock()
		delete(c.runtime.connections, c.id)
		c.runtime.mu.Unlock()
	}
	c.mu.Lock()
	c.closed = err == nil
	attempt.err = err
	close(attempt.done)
	c.mu.Unlock()
}

func (c *providerConnection) release(ctx context.Context) error {
	if c.client != nil {
		if err := c.client.Close(ctx); err != nil {
			return status_error.RuntimeShutdownFailed
		}
		c.client = nil
	}
	for _, release := range []*func(context.Context) error{
		&c.launchAuth.Close, &c.resources.Close, &c.accountLease.Close, &c.resources.CloseFiles, &c.closeWorkspace,
	} {
		if *release != nil {
			if err := (*release)(ctx); err != nil {
				return status_error.RuntimeShutdownFailed
			}
			*release = nil
		}
	}
	c.accountLease = AccountLease{}
	c.launchAuth = LaunchAuthLease{}
	c.resources = ResourceLease{}
	return nil
}

func waitConnectionClose(ctx context.Context, attempt *connectionCloseAttempt) error {
	if attempt == nil {
		return nil
	}
	select {
	case <-attempt.done:
		return attempt.err
	default:
	}
	select {
	case <-attempt.done:
		return attempt.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close permanently stops new admission and starts closing every registered owner.
func (r *ProviderRuntime) Close(ctx context.Context) error {
	r.mu.Lock()
	r.closing = true
	connections := make([]*providerConnection, 0, len(r.connections))
	for _, c := range r.connections {
		c.markClosing()
		connections = append(connections, c)
	}
	r.mu.Unlock()
	return closeProviderConnections(ctx, connections)
}

// InvalidateAccount closes all owners at or below the revoked auth generation.
func (r *ProviderRuntime) InvalidateAccount(ctx context.Context, provider, accountID string, throughGeneration uint64) error {
	account := providerAccount{provider: provider, id: accountID}
	r.mu.Lock()
	generation := max(r.invalidated[account], throughGeneration)
	r.invalidated[account] = generation
	var connections []*providerConnection
	for _, c := range r.connections {
		if c.account == account && c.generation <= generation {
			c.markClosing()
			connections = append(connections, c)
		}
	}
	r.mu.Unlock()
	return closeProviderConnections(ctx, connections)
}

func closeProviderConnections(ctx context.Context, connections []*providerConnection) error {
	attempts := make([]*connectionCloseAttempt, 0, len(connections))
	for _, c := range connections {
		attempts = append(attempts, c.requestClose(true))
	}
	var result error
	for _, attempt := range attempts {
		if err := waitConnectionClose(ctx, attempt); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			result = err
		}
	}
	return result
}

func resolveAgentDirectory(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", status_error.RuntimeConfigurationInvalid
	}
	directory, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", status_error.RuntimeConfigurationInvalid
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		return "", status_error.RuntimeConfigurationInvalid
	}
	return directory, nil
}
