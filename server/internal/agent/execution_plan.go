package agent

import (
	"context"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

const (
	// EventTypeInvocationStarted means the runtime has fully received the first user input of this invocation.
	EventTypeInvocationStarted = constants.RuntimeEventTypeInvocationStarted
	// EventTypeStatus represents a nonterminal runtime protocol status.
	EventTypeStatus = constants.RuntimeEventTypeStatus
	// EventTypeTextDelta represents incremental user-facing text.
	EventTypeTextDelta = constants.RuntimeEventTypeTextDelta
	// EventTypeCommentaryDelta represents incremental user-visible progress commentary.
	EventTypeCommentaryDelta = constants.RuntimeEventTypeCommentaryDelta
	// EventTypeThinkingStart marks the start of a visible thinking phase.
	EventTypeThinkingStart = constants.RuntimeEventTypeThinkingStart
	// EventTypeThinkingDelta represents incremental visible thinking content.
	EventTypeThinkingDelta = constants.RuntimeEventTypeThinkingDelta
	// EventTypeToolUse means the runtime requests tool execution.
	EventTypeToolUse = constants.RuntimeEventTypeToolUse
	// EventTypeToolInputSnapshot is the cumulative argument state of the same tool call, not another execution.
	EventTypeToolInputSnapshot = constants.RuntimeEventTypeToolInputSnapshot
	// EventTypeToolUpdate is nonterminal progress, content, or diff updates for the same tool call, not another execution request.
	EventTypeToolUpdate = constants.RuntimeEventTypeToolUpdate
	// EventTypeToolResult means a tool call has returned.
	EventTypeToolResult = constants.RuntimeEventTypeToolResult
	// EventTypeUsage means the runtime reports unified turn token usage and session context usage.
	EventTypeUsage = constants.RuntimeEventTypeUsage
	// EventTypeSession means the runtime created or captured a native session.
	EventTypeSession = constants.RuntimeEventTypeSession
	// EventTypeError means the runtime protocol reported an error.
	EventTypeError = constants.RuntimeEventTypeError
	// EventTypeDiagnostic means the SDK produced a diagnostic without secret values.
	EventTypeDiagnostic = constants.RuntimeEventTypeDiagnostic
	// EventTypeRaw means the SDK retained unrecognized raw output.
	EventTypeRaw = constants.RuntimeEventTypeRaw
)

// EventType aliases the shared runtime event-type enumeration.
type EventType = constants.RuntimeEventType

// Event is the complete unified event envelope delivered to the host event sink.
type Event struct {
	// Type is the stable unified event type.
	Type EventType
	// Data is the complete JSON payload for this event type.
	Data json.RawMessage
}

// ApprovalOption holds an original approval choice supplied by the ACP agent.
type ApprovalOption struct {
	// ID is the option identifier that must be returned unchanged to the ACP agent.
	ID string
	// Name is the user-visible name supplied by the ACP agent.
	Name string
	// Kind is the option category supplied by the ACP agent; unknown values are preserved and returned unchanged.
	Kind string
}

// ApprovalLocation holds a safe file location declared by ACP.
type ApprovalLocation struct {
	// Path is the file path supplied by ACP.
	Path string
	// Line is the optional unsigned line number supplied by ACP; nil means absent.
	Line *int
}

// ApprovalRequest is a safe tool approval request delivered by the runtime to the host.
type ApprovalRequest struct {
	// InvocationID identifies the requesting runtime invocation only for internal host attribution.
	InvocationID string `json:"-"`
	// ToolCallID is the stable identifier of the tool call within the ACP session.
	ToolCallID string
	// Title is the safe operation title supplied by the ACP agent.
	Title string
	// Kind is the tool category supplied by the ACP agent.
	Kind string
	// Summary is a safe summary supplied by the ACP agent or derived from standard fields.
	Summary string
	// Command is a length-bounded display summary extracted from standard ACP command fields.
	Command string
	// Locations contains safe file locations supplied by the ACP agent.
	Locations []ApprovalLocation
	// Options contains all valid choices the ACP agent allows the user to select.
	Options []ApprovalOption
}

// ApprovalDecision holds the host's decision for a tool approval request.
type ApprovalDecision struct {
	// OptionID is the selected option identifier to return unchanged to the ACP agent.
	OptionID string
	// Cancelled means execution was cancelled or interaction unavailable, with no option selected.
	Cancelled bool
}

// ApprovalHandler is the bidirectional port for runtime requests for user tool authorization.
type ApprovalHandler interface {
	// Request blocks until user selection, execution cancellation, or interaction expiry. Valid choices must acquire admission before ctx expires;
	// persistence and publication of admitted choices may cross the deadline, and successful return still represents the same decision.
	Request(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error)
}

// ElicitationRequest holds a structured input request for the host to display.
type ElicitationRequest struct {
	// InvocationID identifies the requesting runtime invocation only for internal host attribution.
	InvocationID string `json:"-"`
	// Mode is form or url.
	Mode string
	// Message is the user-visible explanation supplied by the agent.
	Message string
	// RequestedSchema is the JSON Schema object for form mode.
	RequestedSchema map[string]any
	// URL is the address the user must visit in url mode.
	URL string
}

// ElicitationDecision holds the host's decision for a structured input request.
type ElicitationDecision struct {
	// Accepted means the user submitted the form or confirmed URL interaction.
	Accepted bool
	// Declined means the user explicitly declined this input request.
	Declined bool
	// Cancelled means execution was cancelled or interaction unavailable, with no valid input produced.
	Cancelled bool
	// Content is the form content submitted by the user.
	Content map[string]any
}

// ElicitationHandler is the bidirectional port for runtime requests for structured user input.
type ElicitationHandler interface {
	// Request blocks until submission, rejection, execution cancellation, or interaction expiry. Submission or rejection must acquire admission before ctx expires;
	// persistence and publication of admitted decisions may cross the deadline, and successful return still represents the same decision.
	Request(ctx context.Context, request ElicitationRequest) (ElicitationDecision, error)
}

// Instruction holds a structured instruction needed by the current runtime invocation.
type Instruction struct {
	// Name is the instruction source's stable display name.
	Name string
	// Content is the complete instruction content read for this invocation.
	Content string
}

// HistoryMessage holds an authoritative message used to seed a new native session.
type HistoryMessage struct {
	// Role is the message's stable conversation role.
	Role string
	// Content is the message body after display projection.
	Content string
	// CodeReferences contains content references frozen when the historical user message was sent.
	CodeReferences []CodeReference
}

// Attachment holds a managed attachment reference passed to the runtime with this message.
type Attachment struct {
	// Name is the attachment name displayed to the agent.
	Name string
	// Path is the durable reference relative to the private conversation file root.
	Path string
	// MediaType is the attachment media type identified by the backend.
	MediaType string
	// RuntimePath is the absolute path used by runtimes supporting native file input.
	RuntimePath string
}

// CodeReference holds a whole-file reference or code snippet snapshot frozen for this message.
type CodeReference struct {
	// Path is the original absolute whole-file path or the path supplied with the snippet.
	Path string
	// StartLine is the one-based snippet start line, or zero for whole-file references.
	StartLine int
	// EndLine is the one-based snippet end line, or zero for whole-file references.
	EndLine int
	// Content is the snippet text snapshot at send time, or empty for whole-file references.
	Content string
}

// Message holds the user input currently handled by a runtime invocation.
type Message struct {
	// Text is the body submitted by the user.
	Text string
	// Attachments contains attachments in user-selected order.
	Attachments []Attachment
	// CodeReferences contains content references in user-selected order.
	CodeReferences []CodeReference
}

// Skill holds a skill the current runtime invocation may read on demand.
type Skill struct {
	// Name is the skill name displayed to the agent.
	Name string
	// Path is the absolute path of the skill directory currently available to NeuxBot.
	Path string
}

// Connector holds an MCP connection available to the current runtime invocation.
type Connector struct {
	// ID is the MCP connector's stable identifier.
	ID string
	// Name is the MCP connector name displayed to the agent.
	Name string
	// RuntimeKey is the stable connector name written to native runtime configuration.
	RuntimeKey string
	// Transport is the MCP connector's transport.
	Transport ConnectorTransport
	// Command is the executable command for a STDIO connector.
	Command string
	// Arguments contains ordered STDIO command arguments.
	Arguments []string
	// Environment contains environment variables required by the STDIO subprocess.
	Environment map[string]string
	// URL is the Streamable HTTP or SSE service address.
	URL string
	// Headers contains headers required by remote MCP requests.
	Headers map[string]string
	// TimeoutMS is the connection test timeout, also injected as the MCP startup timeout when natively supported.
	TimeoutMS int64
}

// RunPolicy holds the invocation's model selection and execution scope corresponding to product permissions.
type RunPolicy struct {
	// Model identifies the model selected for this invocation.
	Model string
	// Reasoning is the reasoning effort selected for this invocation.
	Reasoning string
	// Thinking is an explicit thinking toggle for detached tasks; nil preserves the native default.
	Thinking *bool
	// Execution holds product permissions frozen by the caller.
	Execution ExecutionPolicy
}

// UserInputState describes host-created input waits for the inactivity timer.
type UserInputState interface {
	// Pending reports unresolved host input in the current execution.
	Pending() bool
	// Changed wakes observers without blocking when input-wait state changes.
	Changed() <-chan struct{}
}

// Workspace holds the filesystem scope of a runtime invocation.
type Workspace struct {
	// Path is the subprocess's actual working directory.
	Path string
	// Managed reports whether NeuxBot manages this directory and permits SDK integration-file writes.
	Managed bool
	// AdditionalWritableDirectories contains extra writable directories authorized by the caller.
	AdditionalWritableDirectories []string
	// ProjectDirectories contains agent-visible project directories and the default used when ownership is unclear.
	ProjectDirectories []WorkspaceProjectDirectory
}

// WorkspaceProjectDirectory holds a user project directory entry in the runtime workspace.
type WorkspaceProjectDirectory struct {
	// Path is the canonical absolute project directory, independent of the runtime cwd.
	Path string
	// Default selects this directory when file ownership cannot be inferred from task semantics.
	Default bool
}

// ExecutionPolicy holds product permissions frozen by the caller.
type ExecutionPolicy struct {
	// PermissionLevel is the cross-runtime product permission level frozen by the caller.
	PermissionLevel PermissionLevel
	// WorkspaceReadOnly means the host requires read-only workspace access.
	WorkspaceReadOnly bool
}

// UsageData is the complete normalized usage delivered by the runtime SDK to the host.
type UsageData struct {
	// Turn holds cumulative token usage for the current turn; nil for context-only reports.
	Turn *TurnUsageData `json:"turn,omitempty"`
	// Context holds the current native session context snapshot; nil when unsupported.
	Context *ContextUsageData `json:"context,omitempty"`
}

// TurnUsageData holds token usage normalized to shared semantics within a turn.
type TurnUsageData struct {
	// InputTokens counts uncached input tokens, excluding cache reads and writes.
	InputTokens int64 `json:"input_tokens,omitempty"`
	// CacheReadTokens counts input tokens served from the runtime cache.
	CacheReadTokens int64 `json:"cache_read_tokens,omitempty"`
	// CacheCreationTokens counts input tokens written to the runtime cache during this turn.
	CacheCreationTokens int64 `json:"cache_creation_tokens,omitempty"`
	// OutputTokens counts output tokens reported by the runtime.
	OutputTokens int64 `json:"output_tokens,omitempty"`
	// ReasoningOutputTokens is a separately reported subset of output tokens and is not added again.
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens,omitempty"`
	// KnownFields lists natively reported components using existing token JSON keys; historical records are not backfilled.
	KnownFields []string `json:"known_fields,omitempty"`
	// Complete reports coverage of the entire turn; nil means an older record made no declaration.
	Complete *bool `json:"complete,omitempty"`
	// Source identifies the native cumulative snapshot source.
	Source string `json:"source,omitempty"`
	// Coverage is the consumption coverage declared by the native client.
	Coverage string `json:"coverage,omitempty"`
	// Model stores only the model explicitly reported by the native client.
	Model string `json:"model,omitempty"`
}

// ContextUsageData holds the current native session's context usage snapshot.
type ContextUsageData struct {
	// UsedTokens counts tokens currently occupying the native session context.
	UsedTokens int64 `json:"used_tokens,omitempty"`
	// WindowTokens is the current model's context window limit.
	WindowTokens int64 `json:"window_tokens,omitempty"`
}

// ConnectorDeclaration is the fixed MCP declaration used for session compatibility, without connection secrets.
type ConnectorDeclaration struct {
	// ID is the connector's stable identifier.
	ID string
	// Name is the fixed name displayed to the agent.
	Name string
	// RuntimeKey is the stable name used in native configuration.
	RuntimeKey string
	// Transport is the fixed transport.
	Transport ConnectorTransport
	// Command is the fixed STDIO service executable.
	Command string
	// Arguments contains fixed executable arguments.
	Arguments []string
	// URL is the fixed remote service address.
	URL string
	// TimeoutMS is the fixed startup timeout.
	TimeoutMS int64
}

// SessionTarget holds secret-free runtime and native launch selections without owning account or process resources.
type SessionTarget struct {
	// RuntimeID is the runtime or local profile selected by the user.
	RuntimeID string
	// AccountID is the frozen account principal.
	AccountID string
	// Binary is the fixed executable selection.
	Binary string
	// PrefixArgs contains fixed leading launch arguments.
	PrefixArgs []string
	// ConfigHome is the fixed native configuration directory.
	ConfigHome string
	// NativePluginDirectories contains fixed native plugin directories.
	NativePluginDirectories []string
	// NativePluginScope is the fixed native plugin scope fact.
	NativePluginScope string
	// CodexPluginConfiguration contains native configuration overrides without secrets.
	CodexPluginConfiguration map[string]string
	// ClaudeSettingsPath is the fixed model settings file.
	ClaudeSettingsPath string
}

// SessionPlan holds static business facts needed for session compatibility, excluding turn input and resource acquisition callbacks.
type SessionPlan struct {
	// Target contains frozen runtime, account, and native launch selections.
	Target SessionTarget
	// Detection is the current runnable detection snapshot.
	Detection DetectionResult
	// Workspace is the authorized work scope.
	Workspace Workspace
	// Instructions contains fixed profile, project, or task instructions.
	Instructions []Instruction
	// Skills contains fixed skills declared for the current scope.
	Skills []Skill
	// Connectors contains fixed connectors declared for the current scope.
	Connectors []ConnectorDeclaration
	// Policy contains fixed model, reasoning, and permission selections.
	Policy RunPolicy
	// ResourceKey is the secret-free compatibility basis for account and plugin configuration.
	ResourceKey string
	// PluginIDs identifies plugins requiring protection before resource binding.
	PluginIDs []string
	// ReadOnlyHandoff requires read-only handoff generation in the native context and closure of temporary consumers.
	ReadOnlyHandoff bool
}

// TurnInput holds only new messages, explicit selections, and interaction ports for this turn.
type TurnInput struct {
	// Message is the current user input.
	Message Message
	// ContextInstructions contains this turn's task paths and business role contracts, never session compatibility data.
	ContextInstructions []Instruction
	// History seeds bounded history only when a session is actually created.
	History []HistoryMessage
	// SelectedSkills contains fixed skill names explicitly selected for this turn.
	SelectedSkills []string
	// SelectedConnectors contains fixed connector identifiers explicitly selected for this turn.
	SelectedConnectors []string
	// UserInputState is this turn's capability question-and-answer wait state.
	UserInputState UserInputState
	// ApprovalHandler is this turn's tool approval port.
	ApprovalHandler ApprovalHandler
	// ElicitationHandler is this turn's structured input port.
	ElicitationHandler ElicitationHandler
}

// DetectedConnectorTransportSupported uses only the same detection snapshot to determine MCP transport availability for new executions.
func DetectedConnectorTransportSupported(
	detection DetectionResult,
	transport ConnectorTransport,
) bool {
	switch transport {
	case ConnectorTransportStdio:
		return detection.State == DetectionStateReady
	case ConnectorTransportStreamableHTTP:
		return detection.Capabilities.Has(CapabilityMCPStreamableHTTP)
	case ConnectorTransportSSE:
		return detection.Capabilities.Has(CapabilityMCPSSE)
	default:
		return false
	}
}
