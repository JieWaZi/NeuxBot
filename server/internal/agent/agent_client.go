package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	acp "github.com/JieWaZi/acp-go-sdk"
	"github.com/JieWaZi/acp-go/pkg/acpclient"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	native "github.com/JieWaZi/acp-go/pkg/agentclient"
	"github.com/JieWaZi/acp-go/pkg/claude"
	"github.com/JieWaZi/acp-go/pkg/codex"
	"github.com/JieWaZi/acp-go/pkg/cursor"
	"github.com/JieWaZi/acp-go/pkg/pi"
	"github.com/JieWaZi/acp-go/pkg/qoder"
	"github.com/JieWaZi/acp-go/pkg/standardacp"
	"github.com/JieWaZi/acp-go/pkg/workbuddy"
)

// AgentClient is the direct execution boundary used by Thread and Turn.
// The provider runtime keeps the client's process ownership.
type AgentClient interface {
	Capabilities() AgentCapabilities
	NewSession(context.Context, AgentSessionOptions) (AgentSession, error)
	ResumeSession(context.Context, string, AgentSessionOptions) (AgentSession, error)
	LoadSession(context.Context, string, AgentSessionOptions) (AgentSession, error)
	ListSessions(context.Context, string) (AgentSessionPage, error)
	ForkSession(context.Context, AgentForkRequest) (AgentSession, error)
	SetSessionOption(context.Context, string, string, AgentOptionValue) ([]AgentSessionOption, error)
	SetSessionMode(context.Context, string, string) error
	ReservePrompt(context.Context, AgentPromptRequest) (func() AgentPromptResult, error)
	Cancel(string) error
	Steer(context.Context, AgentSteerRequest) (AgentSteerResult, error)
	Compact(context.Context, string) (AgentCompactResult, error)
	ReadNativeOutcomes(context.Context, string, []string, bool) (AgentOutcomePage, error)
	ReadModels(context.Context) (AgentModelCatalog, error)
	ReadAccount(context.Context) (acpmeta.AccountInfo, error)
	ReadAccountUsage(context.Context) (acpmeta.AccountUsage, error)
	CloseSession(context.Context, string) error
}

// AgentCapabilities reports negotiated features used to prepare a request.
type AgentCapabilities struct {
	// Version is the native CLI version confirmed by the adapter.
	Version string
	// ImageInput indicates native support for image blocks.
	ImageInput bool
	// AudioInput indicates native support for audio blocks.
	AudioInput bool
	// EmbeddedContext indicates native support for inline resource blocks.
	EmbeddedContext bool
	// AdditionalDirectories indicates native support for extra authorized workspace roots.
	AdditionalDirectories bool
	// MCPHTTP is the native handshake's support for streamable HTTP MCP servers.
	MCPHTTP bool
	// DeveloperInstructionDelivery is native, prompt_prefix or empty when unavailable.
	DeveloperInstructionDelivery string
	// Load indicates history-replaying session load support.
	Load bool
	// Resume indicates session recovery without history replay.
	Resume bool
	// ReloadResourcesOnResume confirms that a new process reads current tools and skills for an existing session.
	ReloadResourcesOnResume bool
	// ReloadInstructionsOnResume confirms replacement of saved instructions on an existing session.
	ReloadInstructionsOnResume bool
	// List indicates paged discovery of existing native sessions.
	List bool
	// ForkMode preserves the negotiated latest or exact-position fork precision.
	ForkMode string
	// Steer indicates native steering supports an exact target and a host admission gate.
	Steer bool
	// Compact indicates the native adapter supports compaction.
	Compact bool
	// NativeOutcomes indicates detached results can be read and acknowledged.
	NativeOutcomes bool
	// ToolsDisabled indicates an enforced no-tools startup policy for a fresh session.
	ToolsDisabled bool
}

// AgentMCPServer is an authorized, materialized server configuration.
type AgentMCPServer struct {
	// Name is the unique server key presented to the provider.
	Name string
	// Transport is stdio, http or sse.
	Transport string
	// Command starts a stdio server.
	Command string
	// Arguments are passed to the stdio server.
	Arguments []string
	// Environment is private to the stdio server.
	Environment map[string]string
	// URL is the endpoint for http or sse.
	URL string
	// Headers are the authorized request headers for http or sse.
	Headers map[string]string
}

// AgentSessionOptions provides settings that are scoped to opening a session.
type AgentSessionOptions struct {
	// AdditionalDirectories lists authorized real directories beyond the connection cwd.
	AdditionalDirectories []string
	// DeveloperInstructions overrides native instructions. Nil preserves native defaults.
	DeveloperInstructions *string
	// CodexMode selects the sandbox before the native thread starts.
	CodexMode string `json:",omitempty"`
}

// AgentSession reports the actual session ID and current configuration.
type AgentSession struct {
	// ID is the provider-confirmed native session identity.
	ID string
	// Options contains the provider's effective configuration options.
	Options []AgentSessionOption
	// Mode identifies the currently effective session mode.
	Mode string
	// Modes lists modes supported by the session.
	Modes []AgentChoice
	// MCPStatus is not_requested, load_unverified or a provider-confirmed readiness status.
	MCPStatus string
	// ObservedCodexSettings is the last native thread settings receipt, independent of requested intent.
	ObservedCodexSettings *ObservedCodexSettings
}

// ObservedCodexSettings is the last settings receipt actually reported by Codex for this thread.
type ObservedCodexSettings struct {
	// Source identifies the native response or notification.
	Source string
	// Model is the native model when reported.
	Model *string
	// Reasoning is the native reasoning effort when reported.
	Reasoning *string
	// ServiceTier is the actual tier ID, nil when unknown or explicitly null.
	ServiceTier *string
	// ServiceTierKnown distinguishes explicit null from omitted evidence.
	ServiceTierKnown bool
}

// AgentSessionInfo contains the native identity and workspace of a listed session.
type AgentSessionInfo struct {
	// ID is the native session identity.
	ID string
	// CWD is the reported real working directory.
	CWD string
	// AdditionalDirectories reports the other native workspace roots.
	AdditionalDirectories []string
	// Title is the native label when supplied.
	Title string
	// UpdatedAt is the provider's timestamp when supplied.
	UpdatedAt string
}

// AgentSessionPage is a page of native sessions under the connection cwd.
type AgentSessionPage struct {
	// Sessions contains the provider's ordered rows.
	Sessions []AgentSessionInfo
	// NextCursor is opaque and is empty at the end of the listing.
	NextCursor string
}

// AgentForkRequest targets a native source and freezes receipt reconciliation.
type AgentForkRequest struct {
	// HistoryIdentity carries the source binding's verified native history boundary.
	HistoryIdentity string
	// BeforeFork durably records the host side-effect boundary after cold preparation.
	BeforeFork func(context.Context) error
	// SourceSessionID is the existing native session to fork.
	SourceSessionID string
	// SourceCWD is the source's real directory, distinct from the child connection cwd.
	SourceCWD string
	// Position is the provider-confirmed position, empty only for a latest fork.
	Position string
	// ReceiptDirectory is a dedicated absolute path retained by the host across retries.
	ReceiptDirectory string
	// ReconcileOnly only reads existing fork evidence and never opens a replacement child.
	ReconcileOnly bool
	// Session configures the newly opened native session.
	Session AgentSessionOptions
}

// AgentSessionOption describes the effective native setting and allowed values.
type AgentSessionOption struct {
	// ID identifies the native configuration option.
	ID string
	// Category identifies the native configuration category, such as model or reasoning effort.
	Category string
	// Name is the native option name.
	Name string
	// Description describes the native option.
	Description string
	// Value is the currently observed native value.
	Value AgentOptionValue
	// Choices lists the allowed enumeration values.
	Choices []AgentChoice
	// acknowledgedID retains Claude's last acknowledged selection separately from the observed value.
	acknowledgedID string
}

// AgentOptionValue holds a string selection or an explicit boolean.
type AgentOptionValue struct {
	// ID is the value ID for a select setting.
	ID string
	// Bool is non-nil for a boolean setting, including explicit false.
	Bool *bool
}

// AgentChoice describes a native selection or mode.
type AgentChoice struct {
	// ID identifies a native model, setting value, or mode.
	ID string
	// Name is the native display name.
	Name string
	// Description describes the native option.
	Description string
	// Group identifies the native option group.
	Group string
	// GroupName is the native group display name.
	GroupName string
	// Config is the acknowledged per-model settings catalog.
	Config *AgentModelConfig
	// AutoMode reports native model support for automatic approval.
	AutoMode bool
	// FastServiceTier identifies the native Fast service tier.
	FastServiceTier string
	// DefaultServiceTier is the native default service tier.
	DefaultServiceTier *string
	// InputModalities lists the model's supported input types.
	InputModalities []string
	// PermissionModes is the permission menu published by ACP.
	PermissionModes []string
	// Free reports whether the native catalog confirms that the model is free.
	Free bool
	// CreditMultiplier is the credit multiplier declared by the native catalog; nil means unknown.
	CreditMultiplier *float64
}

// AgentModelConfig distinguishes a reported model setting catalog from unknown rows.
type AgentModelConfig struct {
	// Options lists the settings described for this model.
	Options []AgentSessionOption
	// UnknownOptionIDs names settings for which the provider supplied no catalog.
	UnknownOptionIDs []string
}

// AgentModelCatalog is the SDK-independent result of one provider model read.
type AgentModelCatalog struct {
	// DefaultID identifies the acknowledged default model.
	DefaultID string
	// Models preserves native model and group ordering; nil means no catalog was returned.
	Models []AgentChoice
}

// AgentScope identifies the execution proven to own an observation or interaction.
type AgentScope struct {
	// SessionID identifies the verified session.
	SessionID string
	// HostInvocationID identifies the associated host input when proven.
	HostInvocationID string
	// NativeInvocationID identifies the adapter's execution when available.
	NativeInvocationID string
}

// AgentClientCallbacks projects native activity into consumer-owned callbacks.
type AgentClientCallbacks struct {
	// Observe receives ordered observations for their confirmed scope.
	Observe func(context.Context, AgentScope, AgentEvent) error
	// Approve returns a provided option ID or an empty value to cancel the request.
	Approve func(context.Context, AgentScope, AgentApproval) (string, error)
	// Input resolves a form or URL input requested by the provider.
	Input func(context.Context, AgentScope, AgentInputRequest) (AgentInputResponse, error)
	// CompleteInput observes completion of an existing URL input.
	CompleteInput func(context.Context, AgentScope, string) error
}

// AgentEvent carries scoped execution evidence consumed by the Turn.
type AgentEvent struct {
	// Kind distinguishes text, reasoning, tools, settings and usage.
	Kind string
	// MessageID identifies a native message segment when supplied.
	MessageID string
	// Message contains phase, completion and authoritative replacement evidence when supplied.
	Message *acpmeta.MessageInfo
	// Text contains the observed text delta.
	Text string
	// Content contains observed non-text media or a resource.
	Content *AgentContent
	// Tool carries a native tool update when present.
	Tool *AgentTool
	// Plan carries a native plan update when present.
	Plan *AgentPlan
	// Options carries a provider configuration update.
	Options []AgentSessionOption
	// Mode carries a provider mode update.
	Mode string
	// Usage is the actual provider observation, when supplied.
	Usage *acpmeta.TurnUsage
	// ContextUsedTokens reports used context independently of consumption.
	ContextUsedTokens *int64
	// ContextWindowTokens reports context capacity when known.
	ContextWindowTokens *int64
	// NativeTurn records execution evidence independently of any live output.
	NativeTurn *acpmeta.NativeTurn
	// NativeBackground describes the provider's independent background activity.
	NativeBackground *acpmeta.NativeBackground
}

// AgentTool carries an observed tool call or an incremental update.
type AgentTool struct {
	// ID identifies the tool call within the native session.
	ID string `json:"id"`
	// Kind identifies the tool category when supplied.
	Kind *string `json:"kind,omitempty"`
	// Title is the native tool label when supplied.
	Title *string `json:"title,omitempty"`
	// Status is the reported status when supplied.
	Status *string `json:"status,omitempty"`
	// Input is the tool input payload when supplied.
	Input json.RawMessage `json:"input,omitempty"`
	// Output is the tool output payload when supplied.
	Output json.RawMessage `json:"output,omitempty"`
	// Content is the structured tool result when supplied.
	Content json.RawMessage `json:"content,omitempty"`
	// Update distinguishes an incremental tool change from its initial announcement.
	Update bool `json:"update,omitempty"`
	// Locations omits unknown locations; a non-nil empty slice explicitly clears them.
	Locations []AgentToolLocation `json:"locations,omitempty"`
	// TerminalDelta contains incremental terminal output when supplied independently.
	TerminalDelta string `json:"terminalDelta,omitempty"`
}

// MarshalJSON preserves collection presence through the partial tool boundary.
func (t AgentTool) MarshalJSON() ([]byte, error) {
	type fields AgentTool
	var locations *[]AgentToolLocation
	if t.Locations != nil {
		locations = &t.Locations
	}
	return json.Marshal(struct {
		// fields carries the ordinary optional scalar and raw JSON fields.
		fields
		// Locations overrides omitempty's empty-slice omission with explicit presence.
		Locations *[]AgentToolLocation `json:"locations,omitempty"`
	}{fields: fields(t), Locations: locations})
}

// AgentToolLocation identifies a file observed in a native tool call.
type AgentToolLocation struct {
	// Path is the path reported by the provider.
	Path string `json:"path"`
	// Line is the optional reported line.
	Line *int `json:"line,omitempty"`
}

// AgentPlan represents an observed plan snapshot or removal.
type AgentPlan struct {
	// ID identifies the plan when the provider supports explicit identity.
	ID string
	// Entries contains the ordered checklist for an item-based plan.
	Entries []AgentPlanEntry
	// Markdown is the plan body when delivered as Markdown.
	Markdown string
	// URI points to a plan delivered as a resource.
	URI string
	// Removed indicates that the identified native plan was removed.
	Removed bool
}

// AgentPlanEntry describes a task in the provider's current plan snapshot.
type AgentPlanEntry struct {
	// Content describes the task.
	Content string
	// Priority indicates the task's relative importance.
	Priority string
	// Status is the task's reported execution status.
	Status string
}

// AgentApproval holds the tool requiring approval and its native options.
type AgentApproval struct {
	// NativeToolName is the adapter-confirmed native tool name, or empty when unknown.
	NativeToolName string
	// Tool is the tool call requiring approval.
	Tool AgentTool
	// Options is the set of decisions allowed by the native client.
	Options []AgentApprovalOption
}

// AgentApprovalOption carries a possible native permission decision.
type AgentApprovalOption struct {
	// ID is returned to the provider if selected.
	ID string
	// Kind describes whether the option allows or rejects.
	Kind string
	// Name is the display label.
	Name string
}

// AgentInputRequest describes native form or URL elicitation.
type AgentInputRequest struct {
	// ID is the provider-issued identity for URL input.
	ID string
	// Message explains what the native agent needs.
	Message string
	// URL is present only for URL input.
	URL string
	// Schema is the form's JSON Schema and is absent for URL input.
	Schema json.RawMessage
}

// AgentInputResponse carries the user's native elicitation decision.
type AgentInputResponse struct {
	// Action is accept, decline or cancel.
	Action string
	// Content is the user's submitted form data for accept.
	Content map[string]any
}

// AgentContent is one supported text, media or resource input block.
type AgentContent struct {
	// Kind is text, image, audio, resource_link, resource_text or resource_blob.
	Kind string
	// Text is the plain text or the embedded resource's text.
	Text string
	// Data is the base64 body of a media block or blob resource.
	Data string
	// MIMEType is the media or resource type.
	MIMEType string
	// URI is the resource identity or the optional image source.
	URI string
	// Name is the display name for a resource link.
	Name string
	// Title is the optional human-readable title of a resource link.
	Title string
	// Description is the optional description of a resource link.
	Description string
	// Size is the optional resource size in bytes.
	Size int
	// HasTitle distinguishes an explicitly empty title from an absent title.
	HasTitle bool
	// HasDescription distinguishes an explicitly empty description from absence.
	HasDescription bool
	// HasSize distinguishes an explicitly empty resource from unknown size.
	HasSize bool
}

// AgentSteerRequest submits one additional host input to an observed execution.
type AgentSteerRequest struct {
	// SessionID identifies the native session and must match Target.
	SessionID string
	// Target is the exact observed scope, including its opening host identity.
	Target AgentScope
	// HostInvocationID identifies this additional input.
	HostInvocationID string
	// Text is used for simple text inputs.
	Text string
	// Content supplies the ordered structured input when present.
	Content []AgentContent
	// Prepare acquires optional per-input resources before admission.
	Prepare func(context.Context) (func(), error)
	// BeforeSubmit durably admits the exact input before native bytes are allowed.
	BeforeSubmit func(context.Context, AgentPromptSubmission) error
}

// AgentSteerResult preserves the adapter's native control result.
type AgentSteerResult struct {
	// Outcome is the adapter's native operation classification.
	Outcome string
	// Reason is the adapter's stable reason when supplied.
	Reason string
	// Receipt contains independent input evidence and may accompany an error.
	Receipt *AgentPromptReceipt
}

// AgentCompactResult contains the native compaction outcome.
type AgentCompactResult struct {
	// TurnID is the provider-issued compaction identity when supplied.
	TurnID string
	// Status is the observed terminal classification.
	Status string
	// Turn is the canonical native execution observation when supplied.
	Turn *acpmeta.NativeTurn
}

// AgentPromptRequest carries one input with a gate before native submission.
type AgentPromptRequest struct {
	// SessionID is the confirmed target session.
	SessionID string
	// HostInvocationID identifies the host input.
	HostInvocationID string
	// Text is the simple text input used when Content is empty.
	Text string
	// Content is the complete ordered structured input, mutually exclusive with Text.
	Content []AgentContent
	// BeforeSubmit is called before native input bytes can be written.
	BeforeSubmit func(context.Context, AgentPromptSubmission) error
}

// AgentPromptSubmission is the adapter-frozen input identity used at admission.
type AgentPromptSubmission struct {
	// SessionID is the target session.
	SessionID string
	// HostInvocationID identifies the host input.
	HostInvocationID string
	// InputID identifies the reserved adapter input.
	InputID string
	// NativeInputID is known only when the native provider accepts an input ID.
	NativeInputID *string
}

// AgentPromptReceipt reports one input independently of native execution completion.
type AgentPromptReceipt struct {
	// Input is the same identity supplied to BeforeSubmit.
	Input AgentPromptSubmission
	// Submission reports whether native input bytes are known to have been sent.
	Submission string
	// Admission reports whether native acceptance is known.
	Admission string
	// OperationError is a safe error concerning this input.
	OperationError error
}

// AgentPromptResult separates submission evidence from the native outcome.
type AgentPromptResult struct {
	// Input is the same identity supplied to BeforeSubmit.
	Input AgentPromptSubmission
	// Submission reports whether native input bytes are known to have been sent.
	Submission string
	// Admission reports whether native acceptance is known.
	Admission string
	// OperationError is a safe error concerning this input.
	OperationError error
	// Outcome is absent when no native execution is associated.
	Outcome *AgentNativeOutcome
}

// AgentNativeOutcome contains native completion, independent of observer success.
type AgentNativeOutcome struct {
	// Turn records native identity, consumption, terminal state and usage.
	Turn acpmeta.NativeTurn
	// StopReason contains a provider-confirmed completion reason when present.
	StopReason string
	// ForkPosition is the verified native fork position when supplied.
	ForkPosition string
	// FinalMessage contains a provider-confirmed complete answer when supplied.
	FinalMessage *AgentFinalMessage
	// ContextUsedTokens measures context size, independent of consumption.
	ContextUsedTokens *int64
	// ContextWindowTokens measures the effective context window.
	ContextWindowTokens *int64
	// ObservedOutput reports whether execution output was actually observed.
	ObservedOutput bool
	// PossibleSideEffects prevents treating a failed wait as safe to replay.
	PossibleSideEffects bool
	// NativeFailure is a provider-reported failure mapped to a product status.
	NativeFailure error
	// NativeErrorCode is a structured protocol code without native error text.
	NativeErrorCode int
	// NativeErrorKind is an allowlisted classification supplied by the adapter.
	NativeErrorKind string
	// OperationError is a transport or wait failure for the native execution.
	OperationError error
	// DeliveryError concerns observation or interaction delivery.
	DeliveryError error
	// Unavailable identifies omitted outcome fields.
	Unavailable []string
}

// AgentFinalMessage is provider-confirmed text, including its reconciliation ID.
type AgentFinalMessage struct {
	// ID matches the observed message identity.
	ID string
	// Text is the complete native message.
	Text string
}

// AgentOutcomePage holds unacknowledged native outcomes in completion order.
type AgentOutcomePage struct {
	// Outcomes contains the bounded page of completed results.
	Outcomes []AgentNativeOutcome
	// More indicates further results beyond this page.
	More bool
}

type directAgentClient struct {
	// native owns the provider's protocol without the legacy agent manager.
	native native.Client
	// cwd is the real directory frozen at acquisition.
	cwd string
	// mcp holds the materialized configuration for native session opening.
	mcp []acp.McpServer
	// capabilities and instructionSupport are frozen before the connection becomes available.
	capabilities       AgentCapabilities
	instructionSupport map[string]any
	// mcpCapabilities is the native support for configured network transports.
	mcpCapabilities acp.McpCapabilities
	// requireVerifiedMCP gates session publication on proven connection readiness.
	requireVerifiedMCP bool
	// requireNoTools limits session setup to a fresh, tool-free Claude launch.
	requireNoTools bool
	// supportsElicitation reflects the installed callback capabilities.
	supportsElicitation *acp.ElicitationCapabilities
}

func newDirectAgentClient(ctx context.Context, account AccountRequest, launch ProviderLaunch, lease AccountLease, auth LaunchAuthLease, resources ResourceLease, callbacks AgentClientCallbacks) (*directAgentClient, error) {
	if strings.TrimSpace(launch.Executable) == "" {
		return nil, status_error.RuntimeConfigurationInvalid
	}
	if launch.RequireNoTools {
		claude := &launch.Claude
		if len(resources.MCPServers) != 0 || len(resources.NativePlugins) != 0 || len(resources.Environment) != 0 ||
			len(claude.Tools) != 0 || len(claude.SettingSources) != 0 || claude.SettingsPath != "" || claude.WorkspaceOnly {
			return nil, status_error.RuntimePolicyUnsupported
		}
		claude.SettingSources = []string{}
		claude.Tools = []string{}
		callbacks.Approve, callbacks.Input, callbacks.CompleteInput = nil, nil, nil
	}
	env, err := nativeLaunchEnvironment(account.Provider, launch, lease, auth, resources)
	if err != nil {
		return nil, err
	}
	mcp, err := nativeMCP(resources.MCPServers)
	if err != nil {
		return nil, err
	}
	directories := make([]string, 0, len(resources.NativePlugins))
	for _, plugin := range resources.NativePlugins {
		if plugin.Provider != account.Provider || !filepath.IsAbs(plugin.Directory) {
			return nil, status_error.RuntimeConfigurationInvalid
		}
		directories = append(directories, plugin.Directory)
	}
	host := nativeHostCallbacks(callbacks)
	client := &directAgentClient{
		cwd: account.CWD, mcp: mcp, requireVerifiedMCP: launch.RequireVerifiedMCP, requireNoTools: launch.RequireNoTools,
	}
	if callbacks.Input != nil {
		client.supportsElicitation = &acp.ElicitationCapabilities{Form: &acp.ElicitationFormCapabilities{}}
		if callbacks.CompleteInput != nil {
			client.supportsElicitation.Url = &acp.ElicitationUrlCapabilities{}
		}
	}
	switch account.Provider {
	case constants.RuntimeIDCodex:
		if lease.CodexCredentialKind != "" {
			for _, arg := range launch.PrefixArgs {
				if arg == "-c" || strings.HasPrefix(arg, "-c=") || strings.HasPrefix(arg, "--config") || arg == "-p" || strings.HasPrefix(arg, "--profile") {
					return nil, status_error.AIAccountConfigurationInvalid
				}
			}
		}
		config := maps.Clone(launch.CodexConfig)
		if config == nil {
			config = make(map[string]json.RawMessage)
		}
		startup := maps.Clone(lease.CodexConfig)
		if startup == nil {
			startup = map[string]json.RawMessage{}
		}
		for key, value := range resources.codexPluginConfig {
			startup[key] = value
		}
		for key, value := range startup {
			config[key] = value
		}
		client.native, err = codex.NewClient(ctx, codex.Options{
			Executable: launch.Executable, PrefixArgs: launch.PrefixArgs, WorkingDirectory: account.CWD,
			Environment: env, StartupConfig: startup, ConfigOverrides: config,
		}, host)
	case constants.RuntimeIDClaude:
		if lease.ConfigHome != "" {
			// Account settings have been projected into the selected lease. Native
			// user/project JSON settings must not replace that frozen credential.
			launch.Claude.SettingSources = []string{}
		}
		client.native, err = claude.NewClient(ctx, claude.Options{
			Executable: launch.Executable, PrefixArgs: launch.PrefixArgs, WorkingDirectory: account.CWD,
			// The adapter sanitizes process diagnostics; preserve them when the stream ends without a native result.
			Logger:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})).With("runtime_id", account.Provider),
			Environment: env, Launch: claude.LaunchOptions{
				SettingSources: launch.Claude.SettingSources, Tools: launch.Claude.Tools,
				SettingsPath: launch.Claude.SettingsPath, WorkspaceOnly: launch.Claude.WorkspaceOnly,
				ExcludedWriteDirectories: launch.Claude.ExcludedWriteDirectories,
				PluginDirectories:        directories, LoadPluginMCP: len(directories) > 0,
			},
		}, host)
	case constants.RuntimeIDCursor:
		options := cursor.Options{
			Executable: launch.Executable, PrefixArgs: launch.PrefixArgs, WorkingDirectory: account.CWD,
			Environment: env, PermissionMode: launch.CursorPermissionMode, RequireVerifiedMCP: launch.RequireVerifiedMCP,
			PluginDirectories: directories, UsageHook: true,
		}
		if quota := lease.CursorQuota; quota != nil && quota.ReadAccessToken != nil {
			binding := cursor.CredentialSelection{ID: account.AccountID, Source: cursor.DefaultLogin}
			readToken := quota.ReadAccessToken
			options.Credential = binding
			options.Dashboard = &cursor.Dashboard{
				Endpoint:       quota.Endpoint,
				ExpectedUserID: quota.NativeUserID,
				Credential: func(ctx context.Context, requested cursor.CredentialSelection) (cursor.DashboardCredential, error) {
					if requested != binding {
						return cursor.DashboardCredential{}, status_error.RuntimeAuthRequired
					}
					token, err := readToken(ctx)
					if err != nil {
						return cursor.DashboardCredential{}, safeAgentError(err, status_error.RuntimeAuthRequired)
					}
					return cursor.DashboardCredential{Selection: binding, AccessToken: token}, nil
				},
			}
		}
		client.native, err = cursor.NewClient(ctx, options, host)
	case constants.RuntimeIDWorkBuddy:
		client.native, err = workbuddy.NewClient(ctx, workbuddy.Options{
			Executable: launch.Executable, PrefixArgs: acpDirectoryArguments(launch, account.Provider), WorkingDirectory: account.CWD,
			PluginDirectories: directories, Environment: env, CatalogConfigHome: launch.WorkBuddyCatalogHome, ExplicitCredential: lease.Environment["CODEBUDDY_AUTH_TOKEN"] != "" || lease.Environment["CODEBUDDY_API_KEY"] != "",
		}, host)
	case constants.RuntimeIDQoder:
		nativeID, _, _ := accountconfig.QoderNativeScope(launch.Executable)
		client.native, err = qoder.NewClient(ctx, qoder.Options{
			RuntimeID: nativeID, Executable: launch.Executable, PrefixArgs: acpDirectoryArguments(launch, account.Provider),
			WorkingDirectory: account.CWD, Environment: env, ExpectedUserID: lease.QoderUserID, Model: account.Model,
			ContextWindow: launch.QoderContextWindow, PluginDirectories: directories,
		}, host)
	case constants.RuntimeIDKimi, constants.RuntimeIDGemini, constants.RuntimeIDGrok, constants.RuntimeIDOpenCode:
		if account.Provider == constants.RuntimeIDGrok && launch.ProcessPermissionMode == string(constants.PermissionLevelAuto) {
			// The catalog has no native automatic-approval receipt; avoid letting Grok downgrade an unconfirmed request to ask mode.
			return nil, status_error.RuntimePolicyUnsupported
		}
		args := launch.PrefixArgs
		if account.Provider == constants.RuntimeIDGemini {
			args = acpDirectoryArguments(launch, account.Provider)
		}
		client.native, err = standardacp.NewClient(ctx, standardacp.Options{
			Provider: standardacp.Provider(account.Provider), PluginDirectories: directories, Executable: launch.Executable, PrefixArgs: args,
			WorkingDirectory: account.CWD, Environment: env, PermissionMode: launch.ProcessPermissionMode,
			RequireVerifiedMCP: launch.RequireVerifiedMCP,
		}, host)
	case constants.RuntimeIDPi:
		module := ""
		for _, entry := range env {
			if value, ok := strings.CutPrefix(entry, "PI_MCP_MODULE_PATH="); ok {
				module = value
			}
		}
		client.native, err = pi.NewClient(ctx, pi.Options{
			Executable: launch.Executable, PrefixArgs: launch.PrefixArgs, WorkingDirectory: account.CWD,
			Environment: env, PermissionMode: launch.ProcessPermissionMode, MCPModulePath: module, PluginDirectories: directories,
		}, host)
	default:
		return nil, status_error.RuntimeNotRegistered
	}
	if err != nil {
		return nil, safeAgentError(err, status_error.RuntimeConfigurationInvalid)
	}
	return client, nil
}

func acpDirectoryArguments(launch ProviderLaunch, provider string) []string {
	flag := nativeAdditionalDirectoryFlag(provider)
	args := slices.Clone(launch.PrefixArgs)
	for _, directory := range launch.ACPAdditionalDirectories {
		args = append(args, flag, directory)
	}
	return args
}

func nativeLaunchEnvironment(provider string, launch ProviderLaunch, lease AccountLease, auth LaunchAuthLease, resources ResourceLease) ([]string, error) {
	profileScope := provider == constants.RuntimeIDKimi || provider == constants.RuntimeIDGemini ||
		provider == constants.RuntimeIDGrok || provider == constants.RuntimeIDOpenCode || provider == constants.RuntimeIDPi
	var homeVariable string
	switch provider {
	case constants.RuntimeIDCodex:
		homeVariable = "CODEX_HOME"
	case constants.RuntimeIDClaude:
		homeVariable = "CLAUDE_CONFIG_DIR"
	case constants.RuntimeIDCursor:
		homeVariable = "CURSOR_CONFIG_DIR"
	default:
		homeVariable = accountconfig.ACPSystemConfigVariable(provider)
		if !profileScope && homeVariable == "" && (lease.ConfigHome != "" || launch.ConfigHome != "") {
			return nil, status_error.RuntimeConfigurationInvalid
		}
	}

	environment := make(map[string]string, len(launch.BaseEnvironment)+len(launch.Environment)+len(lease.Environment))
	resourcePath := ""
	merge := func(source map[string]string, resource bool) error {
		var names map[string]struct{}
		if runtime.GOOS == "windows" {
			names = make(map[string]struct{}, len(source))
		}
		for key, value := range source {
			// CLI bootstrap configuration belongs to Core, never the native agent process.
			if strings.HasPrefix(strings.ToUpper(key), "ALLYCLI_") {
				continue
			}
			if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, 0) {
				return status_error.RuntimeConfigurationInvalid
			}
			if names != nil {
				key = strings.ToUpper(key)
				if _, duplicate := names[key]; duplicate {
					return status_error.RuntimeConfigurationInvalid
				}
				names[key] = struct{}{}
			}
			if resource && key == "PATH" {
				// Resource launchers extend the selected runtime path; they never
				// replace the interpreter path that starts the native CLI itself.
				resourcePath = value
				continue
			}
			environment[key] = value
		}
		return nil
	}
	applyResourcePath := func() {
		if resourcePath == "" {
			return
		}
		seen := map[string]bool{}
		paths := []string{}
		for _, directory := range append(filepath.SplitList(resourcePath), filepath.SplitList(environment["PATH"])...) {
			key := normalizedPathForComparison(directory)
			if directory != "" && !seen[key] {
				seen[key] = true
				paths = append(paths, directory)
			}
		}
		environment["PATH"] = strings.Join(paths, string(filepath.ListSeparator))
	}

	if err := merge(launch.BaseEnvironment, false); err != nil {
		return nil, err
	}
	base := make([]string, 0, len(environment))
	for key, value := range environment {
		base = append(base, key+"="+value)
	}
	clear(environment)
	for _, entry := range accountconfig.Environment(base) {
		key, value, _ := strings.Cut(entry, "=")
		environment[key] = value
	}
	if err := merge(launch.Environment, false); err != nil {
		return nil, err
	}
	home := launch.ConfigHome
	if home == "" {
		home = environment[homeVariable]
	}
	if err := merge(resources.Environment, true); err != nil {
		return nil, err
	}
	if err := merge(auth.Environment, false); err != nil {
		return nil, err
	}
	if profileScope {
		// These providers have different native root semantics. Only the account
		// authority may supply roots and credentials; ConfigHome is comparison data.
		entries := make([]string, 0, len(environment))
		for key, value := range environment {
			entries = append(entries, key+"="+value)
		}
		clear(environment)
		for _, entry := range accountconfig.Environment(entries) {
			key, value, _ := strings.Cut(entry, "=")
			environment[key] = value
		}
		if err := merge(lease.Environment, false); err != nil {
			return nil, err
		}
		applyResourcePath()
		result := make([]string, 0, len(environment))
		for _, key := range slices.Sorted(maps.Keys(environment)) {
			result = append(result, key+"="+environment[key])
		}
		return result, nil
	}
	// Managed credentials are selected by the lease, not inherited API credentials.
	if provider == constants.RuntimeIDCodex && lease.CodexCredentialKind != "" {
		for key := range environment {
			upper := strings.ToUpper(key)
			if strings.HasPrefix(upper, "OPENAI_") || upper == "CODEX_API_KEY" || upper == "NEUX_CODEX_ACCOUNT_API_KEY" {
				delete(environment, key)
			}
		}
	}
	// Only the selected launch and account can supply the provider's config root.
	if provider == constants.RuntimeIDClaude && lease.ConfigHome != "" {
		for key := range environment {
			upper := strings.ToUpper(key)
			if strings.HasPrefix(upper, "ANTHROPIC_") || strings.HasPrefix(upper, "CLAUDE_CODE_HOST_") ||
				upper == "CLAUDE_CODE_OAUTH_TOKEN" || upper == "CLAUDE_SECURESTORAGE_CONFIG_DIR" ||
				upper == "CLAUDE_CODE_ACCOUNT_UUID" || upper == "CLAUDE_CODE_ORGANIZATION_UUID" ||
				upper == "CLAUDE_CODE_USE_BEDROCK" || upper == "CLAUDE_CODE_USE_VERTEX" || upper == "CLAUDE_CODE_USE_FOUNDRY" {
				delete(environment, key)
			}
		}
	}
	if provider == constants.RuntimeIDCursor && lease.ConfigHome != "" {
		delete(environment, "CURSOR_API_KEY")
		delete(environment, "CURSOR_AUTH_TOKEN")
	}
	if accountconfig.ACPSystemConfigVariable(provider) != "" {
		for key := range environment {
			if accountconfig.IsACPAccountOverride(provider, key) {
				delete(environment, key)
			}
		}
	}
	delete(environment, homeVariable)
	if err := merge(lease.Environment, false); err != nil {
		return nil, err
	}
	if selected := environment[homeVariable]; selected != "" {
		home = selected
	}
	_, nativeScope := lease.Environment[homeVariable]
	nativeScope = nativeScope && (provider == constants.RuntimeIDClaude || accountconfig.ACPSystemConfigVariable(provider) != "")
	if nativeScope {
		// Claude derives its Keychain scope from the lexical override. Explicitly
		// setting its default directory selects a different account than no override.
		home = lease.Environment[homeVariable]
		// The account lease already validated the canonical root and preserves this
		// lexical spelling. Requiring the directory to exist would reject first use
		// with an environment credential before the CLI has created its settings.
		delete(environment, homeVariable)
	} else if lease.ConfigHome != "" {
		home = lease.ConfigHome
	}
	if home != "" {
		if !filepath.IsAbs(home) || strings.ContainsRune(home, 0) {
			return nil, status_error.RuntimeConfigurationInvalid
		}
		environment[homeVariable] = home
	}
	if provider == constants.RuntimeIDWorkBuddy {
		// Chat titles belong to NeuxBot; the CLI's title request can delay prompt completion.
		environment["CODEBUDDY_CODE_DISABLE_TERMINAL_TITLE"] = "1"
	}

	applyResourcePath()
	env := make([]string, 0, len(environment))
	for _, key := range slices.Sorted(maps.Keys(environment)) {
		env = append(env, key+"="+environment[key])
	}
	return env, nil
}

func (c *directAgentClient) Initialize(ctx context.Context) error {
	response, err := c.native.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientInfo:      &acp.Implementation{Name: "neuxbot", Version: "unknown"},
		ClientCapabilities: acp.ClientCapabilities{
			Meta:        map[string]any{acpmeta.NativeTurnReceiptsCapability: true, "terminal_output": true},
			Elicitation: c.supportsElicitation,
		},
	})
	if err != nil {
		return safeAgentError(err, status_error.RuntimeUnavailable)
	}
	if response.ProtocolVersion != acp.ProtocolVersionNumber {
		return status_error.RuntimeProtocolIncompatible
	}
	session := response.AgentCapabilities.SessionCapabilities
	prompt := response.AgentCapabilities.PromptCapabilities
	c.instructionSupport, _ = response.Meta["developerInstructions"].(map[string]any)
	var delivery string
	if enabled, _ := c.instructionSupport["new"].(bool); enabled {
		delivery, _ = c.instructionSupport["delivery"].(string)
		if delivery == "" {
			delivery = "native"
		}
	}
	_, steer := c.native.(native.SteeringClient)
	if metadata, ok := response.Meta["steering"].(map[string]any); ok {
		if supported, ok := metadata["supported"].(bool); ok {
			steer = steer && supported
		}
	}
	_, compact := c.native.(native.CompactingClient)
	_, outcomes := c.native.(native.NativeOutcomeReader)
	c.capabilities = AgentCapabilities{
		ImageInput: prompt.Image, AudioInput: prompt.Audio, EmbeddedContext: prompt.EmbeddedContext,
		AdditionalDirectories: session.AdditionalDirectories != nil, DeveloperInstructionDelivery: delivery,
		MCPHTTP: response.AgentCapabilities.McpCapabilities.Http,
		Load:    response.AgentCapabilities.LoadSession, Resume: session.Resume != nil, List: session.List != nil,
		ForkMode: acpmeta.ForkMode(response.Meta), Steer: steer, Compact: compact, NativeOutcomes: outcomes,
		ToolsDisabled: c.requireNoTools,
	}
	if _, ok := c.native.(*claude.Client); ok {
		// The Claude adapter constructs every resumed process from its current
		// resource request. Instruction replacement has a separate version gate.
		c.capabilities.ReloadResourcesOnResume = true
		c.capabilities.ReloadInstructionsOnResume, _ = c.instructionSupport["resume"].(bool)
	}
	if c.requireNoTools {
		c.capabilities.Load, c.capabilities.Resume, c.capabilities.List = false, false, false
		c.capabilities.AdditionalDirectories, c.capabilities.Steer, c.capabilities.Compact = false, false, false
		c.capabilities.ForkMode = acpmeta.ForkUnsupported
	}
	if response.AgentInfo != nil {
		c.capabilities.Version = response.AgentInfo.Version
		if version := acpmeta.RuntimeVersion(response.AgentInfo.Meta); version != "" {
			c.capabilities.Version = version
		}
	}
	c.mcpCapabilities = response.AgentCapabilities.McpCapabilities
	return nil
}

func (c *directAgentClient) Capabilities() AgentCapabilities {
	return c.capabilities
}

func (c *directAgentClient) Close(ctx context.Context) error {
	return safeAgentError(c.native.Close(ctx), status_error.RuntimeShutdownFailed)
}

func (c *directAgentClient) NewSession(ctx context.Context, options AgentSessionOptions) (AgentSession, error) {
	directories, meta, err := c.sessionOptions("new", options)
	if err != nil {
		return AgentSession{}, err
	}
	result, err := c.native.NewSession(ctx, acp.NewSessionRequest{
		Cwd: c.cwd, AdditionalDirectories: directories, McpServers: c.mcp, Meta: meta,
	})
	return c.session(string(result.SessionId), result.ConfigOptions, result.Modes, result.Meta, err)
}

func (c *directAgentClient) ResumeSession(ctx context.Context, sessionID string, options AgentSessionOptions) (AgentSession, error) {
	if sessionID == "" {
		return AgentSession{}, status_error.RuntimeConfigurationInvalid
	}
	if !c.capabilities.Resume {
		return AgentSession{}, status_error.RuntimePolicyUnsupported
	}
	directories, meta, err := c.sessionOptions("resume", options)
	if err != nil {
		return AgentSession{}, err
	}
	result, err := c.native.ResumeSession(ctx, acp.ResumeSessionRequest{
		SessionId: acp.SessionId(sessionID), Cwd: c.cwd, AdditionalDirectories: directories, McpServers: c.mcp, Meta: meta,
	})
	if err != nil {
		return AgentSession{}, safeAgentError(err, status_error.RuntimeExecutionFailed)
	}
	return c.session(sessionID, result.ConfigOptions, result.Modes, result.Meta, nil)
}

func (c *directAgentClient) sessionOptions(operation string, options AgentSessionOptions) ([]string, map[string]any, error) {
	if len(options.AdditionalDirectories) != 0 && !c.capabilities.AdditionalDirectories {
		return nil, nil, status_error.RuntimePolicyUnsupported
	}
	for _, server := range c.mcp {
		if server.Http != nil && !c.mcpCapabilities.Http || server.Sse != nil && !c.mcpCapabilities.Sse {
			return nil, nil, status_error.RuntimePolicyUnsupported
		}
	}
	directories := slices.Clone(options.AdditionalDirectories)
	for i, path := range directories {
		dir, err := resolveAgentDirectory(path)
		if err != nil {
			return nil, nil, err
		}
		directories[i] = dir
	}
	if options.DeveloperInstructions != nil {
		if supported, _ := c.instructionSupport[operation].(bool); !supported {
			return nil, nil, status_error.RuntimePolicyUnsupported
		}
	}
	meta := acpmeta.DeveloperInstructionsMetadata(options.DeveloperInstructions)
	if options.CodexMode != "" {
		if _, ok := c.native.(*codex.Client); !ok {
			return nil, nil, status_error.RuntimePolicyUnsupported
		}
		if meta == nil {
			meta = make(map[string]any)
		}
		maps.Copy(meta, codex.OpenSettingsMetadata(codex.OpenSettings{Mode: options.CodexMode}))
	}
	return directories, meta, nil
}

func (c *directAgentClient) session(id string, options []acp.SessionConfigOption, mode *acp.SessionModeState, meta map[string]any, err error) (AgentSession, error) {
	session := AgentSession{ID: id}
	if err != nil {
		return session, safeAgentError(err, status_error.RuntimeExecutionFailed)
	}
	if id == "" {
		return session, status_error.RuntimeProtocolIncompatible
	}
	session.Options = agentSessionOptions(options)
	if mode != nil {
		session.Mode = string(mode.CurrentModeId)
		for _, item := range mode.AvailableModes {
			session.Modes = append(session.Modes, AgentChoice{ID: string(item.Id), Name: item.Name, Description: stringValue(item.Description)})
		}
	}
	session.MCPStatus = "not_requested"
	if len(c.mcp) != 0 {
		session.MCPStatus = "load_unverified"
	}
	switch c.native.(type) {
	case *cursor.Client:
		if observation, ok := cursor.ReadMCPObservation(meta); ok && observation.Requested == len(c.mcp) {
			session.MCPStatus = observation.Status
		}
	case *claude.Client:
		if len(c.mcp) != 0 && verifiedClaudeMCP(meta, c.mcp) {
			session.MCPStatus = "verified"
		}
	}
	if c.requireVerifiedMCP && session.MCPStatus == "load_unverified" {
		return session, status_error.RuntimePolicyUnsupported
	}
	return session, nil
}

func verifiedClaudeMCP(meta map[string]any, servers []acp.McpServer) bool {
	launch, _ := meta["claude.launch"].(map[string]any)
	if launch == nil {
		return false
	}
	raw, err := json.Marshal(launch["mcp_servers"])
	var status []struct {
		// Name is the CLI-reported MCP server name.
		Name string `json:"name"`
		// Status is the observed native connection status.
		Status string `json:"status"`
	}
	if err != nil || json.Unmarshal(raw, &status) != nil {
		return false
	}
	connected := make(map[string]bool, len(status))
	for _, entry := range status {
		connected[entry.Name] = entry.Status == "connected"
	}
	for _, server := range servers {
		var name string
		switch {
		case server.Stdio != nil:
			name = server.Stdio.Name
		case server.Http != nil:
			name = server.Http.Name
		case server.Sse != nil:
			name = server.Sse.Name
		}
		if !connected[name] {
			return false
		}
	}
	return true
}

func (c *directAgentClient) SetSessionOption(ctx context.Context, id, optionID string, value AgentOptionValue) ([]AgentSessionOption, error) {
	invalidSetting := status_error.RuntimeConfigurationInvalid
	if optionID == "model" {
		invalidSetting = status_error.RuntimeModelSelectionFailed
	}
	var request acp.SetSessionConfigOptionRequest
	switch {
	case value.Bool != nil && value.ID == "":
		request.Boolean = &acp.SetSessionConfigOptionBoolean{
			SessionId: acp.SessionId(id), ConfigId: acp.SessionConfigId(optionID), Type: "boolean", Value: *value.Bool,
		}
	case value.Bool == nil && value.ID != "":
		request.ValueId = &acp.SetSessionConfigOptionValueId{
			SessionId: acp.SessionId(id), ConfigId: acp.SessionConfigId(optionID), Value: acp.SessionConfigValueId(value.ID),
		}
	default:
		return nil, invalidSetting
	}
	result, err := c.native.SetSessionConfigOption(ctx, request)
	return agentSessionOptions(result.ConfigOptions), safeAgentError(err, invalidSetting)
}

func (c *directAgentClient) SetSessionMode(ctx context.Context, id, mode string) error {
	_, err := c.native.SetSessionMode(ctx, acp.SetSessionModeRequest{SessionId: acp.SessionId(id), ModeId: acp.SessionModeId(mode)})
	return safeAgentError(err, status_error.RuntimeConfigurationInvalid)
}

func (c *directAgentClient) ReservePrompt(ctx context.Context, input AgentPromptRequest) (func() AgentPromptResult, error) {
	blocks, err := c.promptContent(input.Text, input.Content)
	if err != nil {
		return nil, err
	}
	options := promptOptions(input.HostInvocationID, input.BeforeSubmit)
	run, err := c.native.ReservePrompt(ctx, acp.PromptRequest{
		SessionId: acp.SessionId(input.SessionID),
		Prompt:    blocks,
	}, options)
	if err != nil {
		return nil, safeAgentError(err, status_error.RuntimeExecutionFailed)
	}
	return func() AgentPromptResult {
		result := run()
		out := AgentPromptResult{
			Input: promptSubmission(result.Receipt.Input), Submission: string(result.Receipt.Submission),
			Admission: string(result.Receipt.Admission), OperationError: safeAgentError(result.Receipt.OperationError, status_error.RuntimeExecutionFailed),
		}
		if result.Outcome != nil {
			nativeOutcome := agentOutcome(*result.Outcome)
			out.Outcome = &nativeOutcome
		}
		return out
	}, nil
}

func promptOptions(hostID string, gate func(context.Context, AgentPromptSubmission) error) native.PromptOptions {
	options := native.PromptOptions{HostInvocationID: hostID}
	if gate != nil {
		options.BeforeSubmit = func(ctx context.Context, submission native.PromptSubmission) error {
			return gate(ctx, promptSubmission(submission))
		}
	}
	return options
}

func promptSubmission(input native.PromptSubmission) AgentPromptSubmission {
	input = input.Snapshot()
	return AgentPromptSubmission{
		SessionID: string(input.SessionID), HostInvocationID: input.HostInvocationID,
		InputID: input.InputID, NativeInputID: input.NativeInputID,
	}
}

func (c *directAgentClient) Cancel(id string) error {
	return safeAgentError(c.native.Cancel(acp.CancelNotification{SessionId: acp.SessionId(id)}), status_error.RuntimeExecutionFailed)
}

func (c *directAgentClient) ReadNativeOutcomes(ctx context.Context, id string, ack []string, wait bool) (AgentOutcomePage, error) {
	reader, ok := c.native.(native.NativeOutcomeReader)
	if !ok {
		return AgentOutcomePage{}, status_error.RuntimePolicyUnsupported
	}
	result, err := reader.ReadNativeOutcomes(ctx, acpmeta.NativeTurnReceiptsRequest{SessionID: id, AckInvocationIDs: ack}, wait)
	page := AgentOutcomePage{More: result.More}
	for _, outcome := range result.Outcomes {
		page.Outcomes = append(page.Outcomes, agentOutcome(outcome))
	}
	return page, safeAgentError(err, status_error.RuntimeExecutionFailed)
}

func (c *directAgentClient) observeCodexSettings(ctx context.Context, sessionID string) *ObservedCodexSettings {
	native, ok := c.native.(*codex.Client)
	if !ok {
		return nil
	}
	settings, err := native.ReadSessionSettings(ctx, acp.SessionId(sessionID))
	if err != nil || settings.Observed.Source == "" {
		return nil
	}
	observed := settings.Observed
	result := &ObservedCodexSettings{
		Source: observed.Source, Model: observed.Model, Reasoning: observed.ReasoningEffort,
		ServiceTierKnown: observed.ServiceTier.IsPresent(),
	}
	if tier, ok := observed.ServiceTier.Value(); ok {
		result.ServiceTier = &tier
	}
	return result
}

func (c *directAgentClient) ConfigureCodexTurn(ctx context.Context, sessionID, model, effort string, fast *bool) ([]AgentSessionOption, error) {
	native, ok := c.native.(*codex.Client)
	if !ok {
		return nil, status_error.RuntimePolicyUnsupported
	}
	options, err := native.ConfigureTurn(ctx, acp.SessionId(sessionID), model, effort, fast)
	return convertSessionOptions(options, true), safeAgentError(err, status_error.RuntimeConfigurationInvalid)
}

func (c *directAgentClient) ReadModels(ctx context.Context) (AgentModelCatalog, error) {
	reader, ok := c.native.(interface {
		ReadModels(context.Context) (acpmeta.ModelCatalog, error)
	})
	if !ok {
		return AgentModelCatalog{}, status_error.RuntimePolicyUnsupported
	}
	if native, ok := c.native.(*qoder.Client); ok {
		// Qoder model discovery uses its ACP session options without an SDK catalog union.
		reader = native.Client
	}
	result, err := reader.ReadModels(ctx)
	if err != nil {
		return AgentModelCatalog{}, safeAgentError(err, status_error.AIAccountModelsUnavailable)
	}
	catalog := AgentModelCatalog{DefaultID: stringValue(result.DefaultID), Models: agentChoices(result.Options, true)}
	for i := range catalog.Models {
		for _, mode := range result.Modes {
			catalog.Models[i].PermissionModes = append(catalog.Models[i].PermissionModes, string(mode.Id))
		}
	}
	return catalog, nil
}

func (c *directAgentClient) CloseSession(ctx context.Context, id string) error {
	_, err := c.native.CloseSession(ctx, acp.CloseSessionRequest{SessionId: acp.SessionId(id)})
	return safeAgentError(err, status_error.RuntimeShutdownFailed)
}

func agentOutcome(o native.NativeOutcome) AgentNativeOutcome {
	result := AgentNativeOutcome{
		Turn: o.Turn, StopReason: stringValue(o.StopReason), ForkPosition: o.ForkPosition,
		ContextUsedTokens: o.ContextUsage.UsedTokens, ContextWindowTokens: o.ContextUsage.WindowTokens,
		ObservedOutput: o.ObservedOutput, PossibleSideEffects: o.PossibleSideEffects, Unavailable: o.Unavailable,
		NativeFailure:  safeAgentError(o.NativeFailure, status_error.RuntimeExecutionFailed),
		OperationError: safeAgentError(o.OperationError, status_error.RuntimeExecutionFailed),
		DeliveryError:  safeAgentError(o.DeliveryError, status_error.RuntimeExecutionFailed),
	}
	var request *acp.RequestError
	if errors.As(errors.Join(o.NativeFailure, o.OperationError), &request) && request != nil {
		result.NativeErrorCode = request.Code
		result.NativeErrorKind = safeNativeErrorKind(request)
	}
	if o.FinalMessage != nil {
		result.FinalMessage = &AgentFinalMessage{ID: o.FinalMessage.ID, Text: o.FinalMessage.Text}
	}
	return result
}

func safeAgentError(err error, fallback status_error.StatusError) error {
	if err == nil {
		return nil
	}
	if public := status_error.PublicError(err); public != nil {
		return public
	}
	var request *acp.RequestError
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, acpmeta.ErrForkUnconfirmed):
		return status_error.ChatForkIndeterminate
	case errors.Is(err, acpclient.ErrRestartRequired):
		return status_error.RuntimeUnavailable
	case errors.Is(err, cursor.ErrPermissionModeUnsupported):
		return status_error.RuntimePolicyUnsupported
	case errors.Is(err, qoder.ErrContextWindowUnsupported):
		return status_error.RuntimeConfigurationInvalid
	case errors.Is(err, qoder.ErrModelUnavailable):
		if errors.As(err, &request) && request != nil {
			return nativeStatusError(status_error.RuntimeExecutionFailed, request)
		}
		return status_error.RuntimeExecutionFailed.WithParams(map[string]interface{}{"ModelUnavailable": true})
	case errors.Is(err, cursor.ErrQuotaUnauthorized), errors.Is(err, cursor.ErrQuotaCredential):
		return status_error.RuntimeAuthRequired
	case errors.As(err, &request) && request != nil:
		mapped := fallback
		switch safeNativeErrorKind(request) {
		case "quota_exhausted":
			mapped = status_error.RuntimeQuotaExhausted
		case "rate_limit":
			mapped = status_error.RuntimeRateLimited
		case "session_restore_unverified":
			mapped = status_error.RuntimeProtocolIncompatible
		case "session_missing":
			mapped = status_error.RuntimeSessionNotFound
		default:
			switch request.Code {
			case -32000:
				mapped = status_error.RuntimeAuthRequired
			case -32800:
				return context.Canceled
			case -32700:
				mapped = status_error.RuntimeProtocolIncompatible
			case -32002:
				mapped = status_error.RuntimeSessionNotFound
			case -32601:
				mapped = status_error.RuntimePolicyUnsupported
			}
		}
		return nativeStatusError(mapped, request)
	}
	return fallback
}

func nativeStatusError(mapped status_error.StatusError, request *acp.RequestError) error {
	if message := native.NativeFailureMessage(request.Message); message != "" && message != "ACP agent request failed" && message != "Internal error" {
		return mapped.WithMessage(message)
	}
	return mapped
}

func safeNativeErrorKind(request *acp.RequestError) string {
	data, _ := request.Data.(map[string]any)
	for _, key := range []string{"classification", "errorKind"} {
		kind, _ := data[key].(string)
		switch kind {
		case "session_missing", "session_restore_unverified", "authentication_lost", "authentication", "billing_error", "rate_limit", "quota_exhausted", "native_execution_failed":
			return kind
		}
	}
	return ""
}

func stringValue[T ~string](p *T) string {
	if p != nil {
		return string(*p)
	}
	return ""
}

func nativeMCP(servers []AgentMCPServer) ([]acp.McpServer, error) {
	result := make([]acp.McpServer, 0, len(servers))
	names := make(map[string]struct{}, len(servers))
	for _, server := range servers {
		if server.Name == "" {
			return nil, status_error.RuntimeConfigurationInvalid
		}
		if _, exists := names[server.Name]; exists {
			return nil, status_error.RuntimeConfigurationInvalid
		}
		names[server.Name] = struct{}{}
		if server.Transport == "stdio" {
			if server.Command == "" {
				return nil, status_error.RuntimeConfigurationInvalid
			}
			env := make([]acp.EnvVariable, 0, len(server.Environment))
			for _, key := range slices.Sorted(maps.Keys(server.Environment)) {
				env = append(env, acp.EnvVariable{Name: key, Value: server.Environment[key]})
			}
			result = append(result, acp.McpServer{Stdio: &acp.McpServerStdio{
				Name: server.Name, Command: server.Command, Args: append([]string{}, server.Arguments...), Env: env,
			}})
			continue
		}
		if server.URL == "" {
			return nil, status_error.RuntimeConfigurationInvalid
		}
		headers := make([]acp.HttpHeader, 0, len(server.Headers))
		for _, key := range slices.Sorted(maps.Keys(server.Headers)) {
			headers = append(headers, acp.HttpHeader{Name: key, Value: server.Headers[key]})
		}
		switch server.Transport {
		case "http":
			result = append(result, acp.McpServer{Http: &acp.McpServerHttpInline{
				Type: "http", Name: server.Name, Url: server.URL, Headers: headers,
			}})
		case "sse":
			result = append(result, acp.McpServer{Sse: &acp.McpServerSseInline{
				Type: "sse", Name: server.Name, Url: server.URL, Headers: headers,
			}})
		default:
			return nil, status_error.RuntimeConfigurationInvalid
		}
	}
	return result, nil
}

func agentSessionOptions(options []acp.SessionConfigOption) []AgentSessionOption {
	return convertSessionOptions(options, true)
}

func convertSessionOptions(options []acp.SessionConfigOption, withModelConfig bool) []AgentSessionOption {
	result := make([]AgentSessionOption, 0, len(options))
	for _, option := range options {
		if value := option.Boolean; value != nil {
			effective := value.CurrentValue
			result = append(result, AgentSessionOption{
				ID: string(value.Id), Category: stringValue(value.Category), Name: value.Name,
				Description: stringValue(value.Description), Value: AgentOptionValue{Bool: &effective},
			})
		}
		if value := option.Select; value != nil {
			evidence, _ := value.Meta["claude.configuration"].(map[string]any)
			acknowledged, _ := evidence["acknowledged"].(string)
			result = append(result, AgentSessionOption{
				ID: string(value.Id), Category: stringValue(value.Category), Name: value.Name,
				Description: stringValue(value.Description), Value: AgentOptionValue{ID: string(value.CurrentValue)},
				Choices:        agentChoices(value.Options, withModelConfig && stringValue(value.Category) == "model"),
				acknowledgedID: acknowledged,
			})
		}
	}
	return result
}

func agentChoices(options acp.SessionConfigSelectOptions, withModelConfig bool) []AgentChoice {
	if options.Ungrouped == nil && options.Grouped == nil {
		return nil
	}
	choices := []AgentChoice{}
	addChoices := func(rows []acp.SessionConfigSelectOption, group, groupName string) {
		for _, row := range rows {
			choice := AgentChoice{
				ID: string(row.Value), Name: row.Name, Description: stringValue(row.Description),
				Group: group, GroupName: groupName,
			}
			if withModelConfig {
				choice.Free, _ = row.Meta[acpmeta.ModelFreeMetaKey].(bool)
				choice.CreditMultiplier = acpmeta.ModelCreditMultiplier(row.Meta)
				choice.AutoMode = claude.ModelAutoModeSupported(row.Meta)
				choice.FastServiceTier, _ = codex.FastServiceTier(row.Meta)
				choice.InputModalities, choice.DefaultServiceTier = codex.ModelNativeMetadata(row.Meta)
				if config, known := acpmeta.ReadModelConfigCatalog(row.Meta); known {
					choice.Config = &AgentModelConfig{Options: convertSessionOptions(config.Options, false)}
					for _, unknown := range config.UnknownConfigIDs {
						choice.Config.UnknownOptionIDs = append(choice.Config.UnknownOptionIDs, string(unknown))
					}
				}
			}
			choices = append(choices, choice)
		}
	}
	if options.Ungrouped != nil {
		addChoices(*options.Ungrouped, "", "")
	}
	if options.Grouped != nil {
		for _, group := range *options.Grouped {
			addChoices(group.Options, string(group.Group), group.Name)
		}
	}
	return choices
}

func agentScope(scope native.Scope) AgentScope {
	return AgentScope{SessionID: string(scope.SessionID), HostInvocationID: scope.HostInvocationID, NativeInvocationID: scope.NativeInvocationID}
}

func nativeHostCallbacks(callbacks AgentClientCallbacks) native.HostCallbacks {
	host := native.HostCallbacks{
		RequestPermission: func(ctx context.Context, scope native.Scope, request acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
			cancelled := acp.RequestPermissionResponse{Outcome: acp.RequestPermissionOutcome{
				Cancelled: &acp.RequestPermissionOutcomeCancelled{Outcome: "cancelled"},
			}}
			if callbacks.Approve == nil {
				return cancelled, nil
			}
			tool := request.ToolCall
			name, _ := tool.Meta[claude.PermissionToolNameMetaKey].(string)
			approval := AgentApproval{NativeToolName: name, Tool: nativeTool(string(tool.ToolCallId), stringPointer(tool.Kind), tool.Title, stringPointer(tool.Status), tool.RawInput, tool.RawOutput, tool.Content, tool.Locations)}
			for _, option := range request.Options {
				approval.Options = append(approval.Options, AgentApprovalOption{ID: string(option.OptionId), Kind: string(option.Kind), Name: option.Name})
			}
			choice, err := callbacks.Approve(ctx, agentScope(scope), approval)
			if err != nil || choice == "" {
				return cancelled, err
			}
			for _, option := range request.Options {
				if choice == string(option.OptionId) {
					return acp.RequestPermissionResponse{Outcome: acp.RequestPermissionOutcome{
						Selected: &acp.RequestPermissionOutcomeSelected{OptionId: option.OptionId, Outcome: "selected"},
					}}, nil
				}
			}
			return cancelled, status_error.AgentInputInvalid
		},
	}
	if callbacks.Observe != nil {
		host.Observe = func(ctx context.Context, scope native.Scope, update acp.SessionNotification) error {
			event, ok := agentEvent(update)
			if !ok {
				return nil
			}
			return callbacks.Observe(ctx, agentScope(scope), event)
		}
	}
	if callbacks.Input != nil {
		host.CreateElicitation = func(ctx context.Context, scope native.Scope, request acp.UnstableCreateElicitationRequest) (acp.UnstableCreateElicitationResponse, error) {
			cancelled := acp.UnstableCreateElicitationResponse{Cancel: &acp.UnstableCreateElicitationCancel{Action: "cancel"}}
			var input AgentInputRequest
			switch {
			case request.Form != nil:
				schema, err := json.Marshal(request.Form.RequestedSchema)
				if err != nil {
					return cancelled, status_error.RuntimeProtocolIncompatible
				}
				input = AgentInputRequest{Message: request.Form.Message, Schema: schema}
			case request.Url != nil && callbacks.CompleteInput != nil:
				input = AgentInputRequest{ID: string(request.Url.ElicitationId), Message: request.Url.Message, URL: request.Url.Url}
			default:
				return cancelled, status_error.RuntimePolicyUnsupported
			}
			reply, err := callbacks.Input(ctx, agentScope(scope), input)
			if err != nil {
				return cancelled, err
			}
			switch reply.Action {
			case "accept":
				return acp.UnstableCreateElicitationResponse{Accept: &acp.UnstableCreateElicitationAccept{Action: "accept", Content: reply.Content}}, nil
			case "decline":
				return acp.UnstableCreateElicitationResponse{Decline: &acp.UnstableCreateElicitationDecline{Action: "decline"}}, nil
			case "cancel":
				return cancelled, nil
			default:
				return cancelled, status_error.AgentInputInvalid
			}
		}
		if callbacks.CompleteInput != nil {
			host.CompleteElicitation = func(ctx context.Context, scope native.Scope, notification acp.UnstableCompleteElicitationNotification) error {
				return callbacks.CompleteInput(ctx, agentScope(scope), string(notification.ElicitationId))
			}
		}
	}
	return host
}

func agentEvent(notification acp.SessionNotification) (AgentEvent, bool) {
	event, known := agentSessionEvent(notification)
	if turn, ok := acpmeta.ReadNativeTurn(notification.Meta); ok {
		event.NativeTurn = &turn
	}
	if background, ok := acpmeta.ReadNativeBackground(notification.Meta); ok {
		event.NativeBackground = &background
	}
	if !known && (event.NativeTurn != nil || event.NativeBackground != nil) {
		event.Kind = "native_state"
		known = true
	}
	return event, known
}

func agentSessionEvent(notification acp.SessionNotification) (AgentEvent, bool) {
	update := notification.Update
	switch {
	case update.UserMessageChunk != nil:
		chunk := update.UserMessageChunk
		return textAgentEvent("user", chunk.MessageId, chunk.Content, chunk.Meta)
	case update.AgentMessageChunk != nil:
		chunk := update.AgentMessageChunk
		return textAgentEvent("text", chunk.MessageId, chunk.Content, chunk.Meta)
	case update.AgentThoughtChunk != nil:
		chunk := update.AgentThoughtChunk
		return textAgentEvent("reasoning", chunk.MessageId, chunk.Content, chunk.Meta)
	case update.ToolCall != nil:
		call := update.ToolCall
		title, kind, status := call.Title, string(call.Kind), string(call.Status)
		tool := nativeTool(string(call.ToolCallId), &kind, &title, &status, call.RawInput, call.RawOutput, call.Content, call.Locations)
		return AgentEvent{Kind: "tool", Tool: &tool}, true
	case update.ToolCallUpdate != nil:
		call := update.ToolCallUpdate
		tool := nativeTool(string(call.ToolCallId), stringPointer(call.Kind), call.Title, stringPointer(call.Status), call.RawInput, call.RawOutput, call.Content, call.Locations)
		tool.Update = true
		if delta, ok := call.Meta["terminal_output_delta"].(map[string]any); ok && len(call.Content) == 0 && call.RawOutput == nil {
			status := stringValue(call.Status)
			if delta["terminal_id"] == tool.ID && status != "completed" && status != "failed" && status != "cancelled" {
				tool.TerminalDelta, _ = delta["data"].(string)
			}
		}
		return AgentEvent{Kind: "tool", Tool: &tool}, true
	case update.Plan != nil:
		return AgentEvent{Kind: "plan", Plan: &AgentPlan{Entries: agentPlanEntries(update.Plan.Entries)}}, true
	case update.PlanUpdate != nil:
		plan := update.PlanUpdate.Plan
		switch {
		case plan.Items != nil:
			return AgentEvent{Kind: "plan", Plan: &AgentPlan{ID: string(plan.Items.Id), Entries: agentPlanEntries(plan.Items.Entries)}}, true
		case plan.Markdown != nil:
			return AgentEvent{Kind: "plan", Plan: &AgentPlan{ID: string(plan.Markdown.Id), Markdown: plan.Markdown.Content}}, true
		case plan.File != nil:
			return AgentEvent{Kind: "plan", Plan: &AgentPlan{ID: string(plan.File.Id), URI: plan.File.Uri}}, true
		}
	case update.PlanRemoved != nil:
		return AgentEvent{Kind: "plan", Plan: &AgentPlan{ID: string(update.PlanRemoved.Id), Removed: true}}, true
	case update.ConfigOptionUpdate != nil:
		return AgentEvent{Kind: "settings", Options: agentSessionOptions(update.ConfigOptionUpdate.ConfigOptions)}, true
	case update.CurrentModeUpdate != nil:
		return AgentEvent{Kind: "mode", Mode: string(update.CurrentModeUpdate.CurrentModeId)}, true
	case update.UsageUpdate != nil:
		event := AgentEvent{Kind: "usage"}
		usage, reported := acpmeta.ReadTurnUsage(update.UsageUpdate.Meta)
		if !reported {
			usage, reported = acpmeta.ReadTurnUsage(notification.Meta)
		}
		if reported {
			event.Usage = &usage
		}
		if (!reported || usage.ContextKnown) && update.UsageUpdate.Size >= 0 && update.UsageUpdate.Used >= 0 {
			used, size := int64(update.UsageUpdate.Used), int64(update.UsageUpdate.Size)
			event.ContextUsedTokens, event.ContextWindowTokens = &used, &size
		}
		return event, event.Usage != nil || event.ContextUsedTokens != nil
	}
	return AgentEvent{}, false
}

func agentPlanEntries(entries []acp.PlanEntry) []AgentPlanEntry {
	result := make([]AgentPlanEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, AgentPlanEntry{Content: entry.Content, Priority: string(entry.Priority), Status: string(entry.Status)})
	}
	return result
}

func textAgentEvent(kind string, id *string, content acp.ContentBlock, meta map[string]any) (AgentEvent, bool) {
	part, ok := agentContent(content)
	if !ok {
		return AgentEvent{}, false
	}
	event := AgentEvent{Kind: kind, MessageID: stringValue(id)}
	if part.Kind == "text" {
		event.Text = part.Text
	} else {
		event.Content = &part
	}
	if info, ok := acpmeta.ReadMessageInfo(meta); ok {
		if kind != "text" || event.MessageID == "" || part.Kind != "text" || event.Text != "" {
			info.ReplacementText = nil
		}
		event.Message = &info
	}
	return event, true
}

func agentContent(content acp.ContentBlock) (AgentContent, bool) {
	switch {
	case content.Text != nil:
		return AgentContent{Kind: "text", Text: content.Text.Text}, true
	case content.Image != nil:
		p := content.Image
		return AgentContent{Kind: "image", Data: p.Data, MIMEType: p.MimeType, URI: stringValue(p.Uri)}, true
	case content.Audio != nil:
		p := content.Audio
		return AgentContent{Kind: "audio", Data: p.Data, MIMEType: p.MimeType}, true
	case content.ResourceLink != nil:
		p := content.ResourceLink
		link := AgentContent{
			Kind: "resource_link", Name: p.Name, URI: p.Uri, MIMEType: stringValue(p.MimeType),
			Title: stringValue(p.Title), HasTitle: p.Title != nil,
			Description: stringValue(p.Description), HasDescription: p.Description != nil,
		}
		if p.Size != nil {
			link.Size, link.HasSize = *p.Size, true
		}
		return link, true
	case content.Resource != nil:
		resource := content.Resource.Resource
		if p := resource.TextResourceContents; p != nil {
			return AgentContent{Kind: "resource_text", Text: p.Text, URI: p.Uri, MIMEType: stringValue(p.MimeType)}, true
		}
		if p := resource.BlobResourceContents; p != nil {
			return AgentContent{Kind: "resource_blob", Data: p.Blob, URI: p.Uri, MIMEType: stringValue(p.MimeType)}, true
		}
	}
	return AgentContent{}, false
}

func stringPointer[T ~string](value *T) *string {
	if value == nil {
		return nil
	}
	text := string(*value)
	return &text
}

func nativeTool(id string, kind, title, status *string, input, output any, content []acp.ToolCallContent, locations []acp.ToolCallLocation) AgentTool {
	result := AgentTool{ID: id, Kind: kind, Title: title, Status: status}
	if input != nil {
		result.Input, _ = json.Marshal(input)
	}
	if output != nil {
		result.Output, _ = json.Marshal(output)
	}
	if content != nil {
		result.Content, _ = json.Marshal(content)
	}
	if locations != nil {
		result.Locations = make([]AgentToolLocation, len(locations))
		for i, location := range locations {
			result.Locations[i] = AgentToolLocation{Path: location.Path, Line: location.Line}
		}
	}
	return result
}

func (c *directAgentClient) promptContent(text string, content []AgentContent) ([]acp.ContentBlock, error) {
	if len(content) == 0 {
		return []acp.ContentBlock{acp.TextBlock(text)}, nil
	}
	if text != "" {
		return nil, status_error.AgentInputInvalid
	}
	blocks := make([]acp.ContentBlock, 0, len(content))
	for _, part := range content {
		var block acp.ContentBlock
		switch part.Kind {
		case "text":
			block = acp.TextBlock(part.Text)
		case "image":
			if !c.capabilities.ImageInput {
				return nil, status_error.RuntimePolicyUnsupported
			}
			if part.Data == "" || part.MIMEType == "" {
				return nil, status_error.AgentInputInvalid
			}
			block = acp.ImageBlock(part.Data, part.MIMEType)
			block.Image.Uri = optionalString(part.URI)
		case "audio":
			if !c.capabilities.AudioInput {
				return nil, status_error.RuntimePolicyUnsupported
			}
			if part.Data == "" || part.MIMEType == "" {
				return nil, status_error.AgentInputInvalid
			}
			block.Audio = &acp.ContentBlockAudio{Type: "audio", Data: part.Data, MimeType: part.MIMEType}
		case "resource_link":
			if part.URI == "" || part.Name == "" {
				return nil, status_error.AgentInputInvalid
			}
			block = acp.ResourceLinkBlock(part.Name, part.URI)
			block.ResourceLink.MimeType = optionalString(part.MIMEType)
			if part.HasTitle {
				block.ResourceLink.Title = &part.Title
			}
			if part.HasDescription {
				block.ResourceLink.Description = &part.Description
			}
			if part.HasSize {
				block.ResourceLink.Size = &part.Size
			}
		case "resource_text", "resource_blob":
			if !c.capabilities.EmbeddedContext {
				return nil, status_error.RuntimePolicyUnsupported
			}
			if part.URI == "" {
				return nil, status_error.AgentInputInvalid
			}
			block.Resource = &acp.ContentBlockResource{Type: "resource"}
			if part.Kind == "resource_text" {
				block.Resource.Resource.TextResourceContents = &acp.TextResourceContents{
					Text: part.Text, Uri: part.URI, MimeType: optionalString(part.MIMEType),
				}
			} else {
				block.Resource.Resource.BlobResourceContents = &acp.BlobResourceContents{
					Blob: part.Data, Uri: part.URI, MimeType: optionalString(part.MIMEType),
				}
			}
		default:
			return nil, status_error.AgentInputInvalid
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (c *directAgentClient) LoadSession(ctx context.Context, id string, options AgentSessionOptions) (AgentSession, error) {
	if id == "" {
		return AgentSession{}, status_error.RuntimeConfigurationInvalid
	}
	loader, ok := c.native.(acp.AgentLoader)
	if !ok || !c.capabilities.Load {
		return AgentSession{}, status_error.RuntimePolicyUnsupported
	}
	directories, meta, err := c.sessionOptions("load", options)
	if err != nil {
		return AgentSession{}, err
	}
	result, err := loader.LoadSession(ctx, acp.LoadSessionRequest{
		SessionId: acp.SessionId(id), Cwd: c.cwd, AdditionalDirectories: directories, McpServers: c.mcp, Meta: meta,
	})
	if err != nil {
		return AgentSession{}, safeAgentError(err, status_error.RuntimeExecutionFailed)
	}
	return c.session(id, result.ConfigOptions, result.Modes, result.Meta, nil)
}

func (c *directAgentClient) ListSessions(ctx context.Context, next string) (AgentSessionPage, error) {
	if !c.capabilities.List {
		return AgentSessionPage{}, status_error.RuntimePolicyUnsupported
	}
	result, err := c.native.ListSessions(ctx, acp.ListSessionsRequest{Cwd: &c.cwd, Cursor: optionalString(next)})
	if err != nil {
		return AgentSessionPage{}, safeAgentError(err, status_error.RuntimeExecutionFailed)
	}
	page := AgentSessionPage{NextCursor: stringValue(result.NextCursor), Sessions: make([]AgentSessionInfo, 0, len(result.Sessions))}
	for _, row := range result.Sessions {
		page.Sessions = append(page.Sessions, AgentSessionInfo{
			ID: string(row.SessionId), CWD: row.Cwd, AdditionalDirectories: row.AdditionalDirectories,
			Title: stringValue(row.Title), UpdatedAt: stringValue(row.UpdatedAt),
		})
	}
	return page, nil
}

func (c *directAgentClient) ForkSession(ctx context.Context, input AgentForkRequest) (AgentSession, error) {
	if c.requireNoTools {
		return AgentSession{}, status_error.RuntimePolicyUnsupported
	}
	if input.SourceSessionID == "" || !filepath.IsAbs(input.ReceiptDirectory) || filepath.Clean(input.ReceiptDirectory) == string(filepath.Separator) {
		return AgentSession{}, status_error.RuntimeConfigurationInvalid
	}
	forker, ok := c.native.(interface {
		UnstableForkSession(context.Context, acp.UnstableForkSessionRequest) (acp.UnstableForkSessionResponse, error)
	})
	if !ok {
		return AgentSession{}, status_error.RuntimePolicyUnsupported
	}
	request := acp.UnstableForkSessionRequest{SessionId: acp.SessionId(input.SourceSessionID), Cwd: c.cwd, Meta: map[string]any{
		"forkReceiptDirectory": input.ReceiptDirectory, "forkPosition": input.Position,
	}}
	if input.ReconcileOnly {
		request.Meta["reconcileOnly"] = true
		result, err := forker.UnstableForkSession(ctx, request)
		if err != nil {
			return AgentSession{}, safeAgentError(err, status_error.ChatForkIndeterminate)
		}
		if result.SessionId == "" || result.SessionId == request.SessionId {
			return AgentSession{}, status_error.ChatForkIndeterminate
		}
		return AgentSession{ID: string(result.SessionId)}, nil
	}
	if c.capabilities.ForkMode == acpmeta.ForkUnsupported || input.Position != "" && c.capabilities.ForkMode != acpmeta.ForkTurn {
		return AgentSession{}, status_error.ChatForkUnavailable
	}
	source, err := resolveAgentDirectory(input.SourceCWD)
	if err != nil {
		return AgentSession{}, err
	}
	directories, meta, err := c.sessionOptions("fork", input.Session)
	if err != nil {
		return AgentSession{}, err
	}
	maps.Copy(request.Meta, meta)
	request.Meta["sourceCwd"] = source
	request.AdditionalDirectories = directories
	if err := translateNativeShape(c.mcp, &request.McpServers); err != nil {
		return AgentSession{}, err
	}
	if input.BeforeFork != nil {
		if err := input.BeforeFork(ctx); err != nil {
			return AgentSession{}, err
		}
	}
	result, err := forker.UnstableForkSession(ctx, request)
	if err != nil {
		return AgentSession{ID: string(result.SessionId)}, safeAgentError(err, status_error.ChatForkIndeterminate)
	}
	if result.SessionId == request.SessionId {
		return AgentSession{}, status_error.RuntimeProtocolIncompatible
	}
	var options []acp.SessionConfigOption
	if err := translateNativeShape(result.ConfigOptions, &options); err != nil {
		return AgentSession{ID: string(result.SessionId)}, err
	}
	return c.session(string(result.SessionId), options, result.Modes, result.Meta, nil)
}

// Stable and unstable session methods use equivalent SDK unions with different Go names.
func translateNativeShape(source any, target any) error {
	body, err := json.Marshal(source)
	if err != nil || json.Unmarshal(body, target) != nil {
		return status_error.RuntimeProtocolIncompatible
	}
	return nil
}

func (c *directAgentClient) Steer(ctx context.Context, input AgentSteerRequest) (AgentSteerResult, error) {
	steering, ok := c.native.(native.SteeringClient)
	if !ok || !c.capabilities.Steer {
		return AgentSteerResult{}, status_error.RuntimePolicyUnsupported
	}
	if input.SessionID == "" || input.Target.SessionID != input.SessionID || input.Target.NativeInvocationID == "" ||
		input.HostInvocationID == "" || input.HostInvocationID == input.Target.HostInvocationID || input.BeforeSubmit == nil {
		return AgentSteerResult{}, status_error.RuntimeConfigurationInvalid
	}
	content, err := c.promptContent(input.Text, input.Content)
	if err != nil {
		return AgentSteerResult{}, err
	}
	options := promptOptions(input.HostInvocationID, input.BeforeSubmit)
	options.Prepare = input.Prepare
	result, err := steering.Steer(ctx, native.SteerRequest{
		SessionID: acp.SessionId(input.SessionID), Prompt: content, PromptRequired: true,
		Target: native.Scope{SessionID: acp.SessionId(input.Target.SessionID), HostInvocationID: input.Target.HostInvocationID,
			NativeInvocationID: input.Target.NativeInvocationID},
		Options: options,
	})
	out := AgentSteerResult{Outcome: result.Outcome, Reason: result.Reason}
	if receipt := result.Receipt; receipt != nil {
		out.Receipt = &AgentPromptReceipt{
			Input: promptSubmission(receipt.Input), Submission: string(receipt.Submission), Admission: string(receipt.Admission),
			OperationError: safeAgentError(receipt.OperationError, status_error.RuntimeExecutionFailed),
		}
	}
	return out, safeAgentError(err, status_error.RuntimeExecutionFailed)
}

func (c *directAgentClient) Compact(ctx context.Context, id string) (AgentCompactResult, error) {
	client, ok := c.native.(native.CompactingClient)
	if !ok || !c.capabilities.Compact {
		return AgentCompactResult{}, status_error.RuntimePolicyUnsupported
	}
	result, err := client.Compact(ctx, acpmeta.CompactRequest{SessionID: id})
	out := AgentCompactResult{TurnID: result.TurnID, Status: result.Status}
	if turn, ok := acpmeta.ReadNativeTurn(result.Meta); ok {
		out.Turn = &turn
	}
	return out, safeAgentError(err, status_error.RuntimeExecutionFailed)
}

func (c *directAgentClient) ReadAccount(ctx context.Context) (acpmeta.AccountInfo, error) {
	reader, ok := c.native.(interface {
		ReadAccount(context.Context) (acpmeta.AccountInfo, error)
	})
	if !ok {
		return acpmeta.AccountInfo{}, status_error.RuntimePolicyUnsupported
	}
	result, err := reader.ReadAccount(ctx)
	return result, safeAgentError(err, status_error.RuntimeUnavailable)
}

func (c *directAgentClient) ReadAccountUsage(ctx context.Context) (acpmeta.AccountUsage, error) {
	reader, ok := c.native.(interface {
		ReadAccountUsage(context.Context) (acpmeta.AccountUsage, error)
	})
	if !ok {
		return acpmeta.AccountUsage{}, status_error.RuntimePolicyUnsupported
	}
	result, err := reader.ReadAccountUsage(ctx)
	return result, safeAgentError(err, status_error.RuntimeUnavailable)
}
