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
	// WaitForLease 表示独立后台调用等待正在修改的账号。
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
	// Environment is the selected credential's authoritative launch environment.
	Environment map[string]string
	// ConfigHome is the account's authoritative native configuration root when supplied.
	ConfigHome string
	// CodexConfig fixes account-owned native provider configuration for startup and sessions.
	CodexConfig map[string]json.RawMessage
	// CodexCredentialKind selects the native account kind required by this lease.
	CodexCredentialKind string
	// VerifyCodexCredential compares the initialized Codex process proof with the selected credential.
	VerifyCodexCredential func(context.Context, [sha256.Size]byte) error
	// VerifyLogin checks observable external login changes immediately before submission.
	// Providers without a same-process identity receipt cannot eliminate external switch races.
	VerifyLogin func(context.Context) error
	// CursorQuota optionally binds management reads to the selected native login.
	CursorQuota *CursorQuotaLease
	// QoderUserID binds management reads to the native identity verified by the lease.
	QoderUserID string
	// QoderLoginVerified carries positive native sign-in evidence from acquisition.
	QoderLoginVerified bool
	// PrepareLaunchAuth obtains any additional startup authentication resources.
	PrepareLaunchAuth func(context.Context) (LaunchAuthLease, error)
	// Close releases the account after native exit and startup authentication cleanup.
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

// ResourceSelection identifies host-authorized resources before reading any secrets.
type ResourceSelection struct {
	// Plugins fixes the installed version of each selected plugin by ID.
	Plugins map[string]string
	// ConnectorIDs fixes the set of authorized connectors.
	ConnectorIDs []string
}

// ResourceLease transfers the materialized resources needed by the native client.
type ResourceLease struct {
	// Environment contains non-account launch contributions.
	Environment map[string]string
	// MCPServers contains the materialized server configuration.
	MCPServers []AgentMCPServer
	// ClaudePluginDirectories identifies physical native plugin directories.
	ClaudePluginDirectories []string
	// Close releases active MCP and other materialized resources.
	Close func(context.Context) error
	// CloseFiles releases temporary files after every native and lease consumer is gone.
	CloseFiles func(context.Context) error
}

// ProviderLaunch contains the facts required to construct one selected native client.
type ProviderLaunch struct {
	// Executable is the detected program path or a name on the explicit PATH.
	Executable string
	// PrefixArgs precedes the native subcommand.
	PrefixArgs []string
	// BaseEnvironment is the inherited child environment, independent of thread compatibility.
	BaseEnvironment map[string]string `json:",omitempty"`
	// Environment is the explicit child environment before lease contributions.
	Environment map[string]string
	// ConfigHome supplies the default native configuration root.
	ConfigHome string
	// CodexConfig contains native launch overrides for Codex.
	CodexConfig map[string]json.RawMessage
	// Claude declares Claude-specific startup requirements.
	Claude ClaudeLaunch
	// CursorPermissionMode selects Cursor's native startup policy.
	CursorPermissionMode string
	// ProcessPermissionMode freezes the approval policy for ACP profiles and Pi.
	ProcessPermissionMode string
	// ACPAdditionalDirectories carries authorized roots for native CLI startup flags.
	ACPAdditionalDirectories []string `json:",omitempty"`
	// WorkBuddyCatalogHome is an isolated CodeBuddy root owned by the catalog connection.
	WorkBuddyCatalogHome string `json:"-"`
	// RequireVerifiedMCP rejects MCP handoff without native verification.
	RequireVerifiedMCP bool
	// RequireNoTools admits only a native fresh-session launch with tools disabled.
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
	// options freezes the shared acquisition port and cleanup bound.
	options ProviderRuntimeOptions
	// mu guards registration and admission only.
	mu sync.Mutex
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
	// runtime removes this entry only after successful cleanup.
	runtime *ProviderRuntime
	// id is allocated before any startup work and never reused within the runtime.
	id uint64
	// account and generation identify this connection for account invalidation.
	account    providerAccount
	generation uint64
	// plugins identifies the frozen versions retained by this native owner.
	plugins map[string]string
	// mu guards publication and close attempts. Startup and cleanup never overlap.
	mu sync.Mutex
	// cancelOpen signals the startup worker without waiting for acquisition to return.
	cancelOpen context.CancelFunc
	// opened closes after all startup writes, including partial lease handoff.
	opened chan struct{}
	// openErr is the safe startup failure published with opened.
	openErr error
	// closing prevents a startup result from being published after close intent.
	closing bool
	// closed confirms all cleanup steps have succeeded.
	closed bool
	// firstCloseAttempt preserves the cleanup selected by a failed Open.
	firstCloseAttempt *connectionCloseAttempt
	// closingAttempt lets concurrent waiters join the same cleanup.
	closingAttempt *connectionCloseAttempt
	// client owns native consumers until Close confirms their exit.
	client ProviderClient
	// qoderLoginVerified retains only the immutable evidence published with opened.
	qoderLoginVerified bool
	// accountLease, launchAuth and resources retain actual releases as they arrive.
	accountLease AccountLease
	launchAuth   LaunchAuthLease
	resources    ResourceLease
	// closeWorkspace releases an adopted working directory after other leases.
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
	account := providerAccount{provider: request.Account.Provider, id: request.Account.AccountID}
	openCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
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
	r.nextID++
	c := &providerConnection{
		runtime: r, id: r.nextID, account: account, generation: request.Account.AuthGeneration, plugins: request.Resources.Plugins,
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
		constants.RuntimeIDWorkBuddy, constants.RuntimeIDQoder, constants.RuntimeIDQoderCN,
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
	if account.DataHome != "" && c.accountLease.Environment["CURSOR_DATA_DIR"] != account.DataHome {
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
	} else if len(request.Resources.Plugins) != 0 || len(request.Resources.ConnectorIDs) != 0 {
		return status_error.RuntimeConfigurationInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
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
	account, err := c.client.ReadAccount(ctx)
	if err != nil {
		return err
	}
	kind := c.accountLease.CodexCredentialKind
	if kind == "" {
		kind = "account"
	}
	if account.Account == nil || account.Account.Kind != kind {
		return status_error.RuntimeAuthRequired
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
