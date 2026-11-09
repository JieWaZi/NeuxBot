package agent

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

const (
	// RuntimeIDCodex identifies the built-in Codex CLI runtime.
	RuntimeIDCodex = constants.RuntimeIDCodex
	// RuntimeIDClaude identifies the built-in Claude Code runtime.
	RuntimeIDClaude = constants.RuntimeIDClaude
	// RuntimeIDKimi identifies the Kimi CLI runtime.
	RuntimeIDKimi = constants.RuntimeIDKimi
	// RuntimeIDCursor identifies the Cursor CLI runtime.
	RuntimeIDCursor = constants.RuntimeIDCursor
	// RuntimeIDPi identifies the Pi runtime.
	RuntimeIDPi = constants.RuntimeIDPi
	// RuntimeIDOpenCode identifies the OpenCode runtime.
	RuntimeIDOpenCode = constants.RuntimeIDOpenCode
	// RuntimeIDGemini identifies the Gemini CLI runtime.
	RuntimeIDGemini = constants.RuntimeIDGemini
	// RuntimeIDGrok identifies the Grok Build runtime.
	RuntimeIDGrok = constants.RuntimeIDGrok
	// RuntimeIDWorkBuddy identifies the WorkBuddy standard ACP runtime.
	RuntimeIDWorkBuddy = constants.RuntimeIDWorkBuddy
	// RuntimeIDQoder identifies the Qoder CLI standard ACP runtime.
	RuntimeIDQoder = constants.RuntimeIDQoder
	// RuntimeIDQoderCN identifies the Chinese Qoder CLI standard ACP runtime.
	RuntimeIDQoderCN = constants.RuntimeIDQoderCN
)

// DetectRequest selects the installation or profile to inspect.
type DetectRequest struct {
	// RuntimeID is the open string ID of a built-in runtime or local profile.
	RuntimeID string
	// BaseEnvironment is the base process environment explicitly supplied by the caller.
	BaseEnvironment []string
	// HomeDirectory fills common executable directories that GUI processes may lack.
	HomeDirectory string
	// LaunchOverride holds caller-permitted command, leading argument, and environment overrides.
	LaunchOverride LaunchOverride
}

// LaunchOverride holds the limited launch overrides users may edit in runtime environment entries.
type LaunchOverride struct {
	// Binary is a single executable name or absolute path; empty inherits definition candidates.
	Binary string
	// PrefixArgs precedes runtime subcommands; nil inherits definition arguments.
	PrefixArgs []string
	// Environment contains controlled runtime environment overrides, such as CODEX_HOME.
	Environment map[string]string
}

// ModelOption describes a model selectable by the runtime.
type ModelOption struct {
	// ID is the model identifier passed through ACP session configuration.
	ID string
	// Name is the model display name exposed to callers.
	Name string
	// ReasoningOptions lists reasoning efforts declared by the model through the runtime protocol.
	ReasoningOptions []ReasoningOption
	// PermissionLevels lists product permission levels the model can currently express accurately.
	PermissionLevels []PermissionLevel
}

// ReasoningOption describes a reasoning effort selectable by the runtime.
type ReasoningOption struct {
	// ID is the reasoning effort identifier passed through ACP session configuration.
	ID string
	// Name is the reasoning effort display name exposed to callers.
	Name string
}

// ProfileSpec describes limited overrides of a built-in runtime definition.
type ProfileSpec struct {
	// ID is the profile's own open string runtime ID.
	ID string
	// BaseRuntimeID identifies the inherited built-in runtime.
	BaseRuntimeID string
	// Name overrides the runtime display name; empty inherits it.
	Name string
	// BinaryCandidates overrides executable candidates in priority order; nil inherits them.
	BinaryCandidates []string
	// PrefixArgs overrides arguments placed before every subcommand; nil inherits them.
	PrefixArgs []string
	// Environment merges into the base definition environment, overriding matching keys.
	Environment map[string]string
	// Models overrides model hints; nil inherits them.
	Models []ModelOption
	// DefaultModel overrides the default model identifier; empty inherits it.
	DefaultModel string
}

// RuntimeSummary is a runtime definition summary safe for callers to display.
type RuntimeSummary struct {
	// ID is the open string identifier of the runtime or local profile.
	ID string
	// BaseRuntimeID identifies the profile's base runtime; built-ins use their own ID.
	BaseRuntimeID string
	// Name is the runtime display name.
	Name string
	// Profile reports whether the entry was generated from a caller-supplied local profile.
	Profile bool
	// BinaryCandidates lists executable candidates in priority order.
	BinaryCandidates []string
	// PrefixArgs precedes the runtime subcommand in every invocation.
	PrefixArgs []string
	// Models contains model hints supplied by the current definition.
	Models []ModelOption
	// DefaultModel identifies the model used without an explicit selection.
	DefaultModel string
	// ReasoningOptions lists reasoning efforts explicitly supported by this runtime.
	ReasoningOptions []ReasoningOption
	// InstructionFileName is the runtime's instruction filename at the workspace root.
	InstructionFileName string
	// SkillDirectoryName is the runtime's skill directory name at the workspace root.
	SkillDirectoryName string
	// ConfigHomeEnvironmentVariable is the controlled environment variable for the configuration home.
	ConfigHomeEnvironmentVariable string
}

// DetectionState aliases the shared runtime detection-state enumeration.
type DetectionState = constants.RuntimeDetectionState

const (
	// DetectionStateUnavailable means the latest probe could not find or invoke the runtime.
	DetectionStateUnavailable = constants.RuntimeDetectionStateUnavailable
	// DetectionStateIncompatible means the latest probe did not confirm required protocol capabilities.
	DetectionStateIncompatible = constants.RuntimeDetectionStateIncompatible
	// DetectionStateReady means the latest probe confirmed required runtime capabilities, not invocation eligibility.
	DetectionStateReady = constants.RuntimeDetectionStateReady
)

// AuthStatus aliases the shared runtime authentication-state enumeration.
type AuthStatus = constants.RuntimeAuthStatus

const (
	// AuthStatusUnknown means the SDK cannot reliably determine authentication status.
	AuthStatusUnknown = constants.RuntimeAuthStatusUnknown
	// AuthStatusOK means authentication probing found no missing credentials.
	AuthStatusOK = constants.RuntimeAuthStatusOK
	// AuthStatusMissing means the ACP agent explicitly reported a missing login or credentials.
	AuthStatusMissing = constants.RuntimeAuthStatusMissing
)

// Capability aliases the shared runtime capability enumeration.
type Capability = constants.RuntimeCapability

const (
	// CapabilityStructuredOutput reports support for the required structured output format.
	CapabilityStructuredOutput = constants.RuntimeCapabilityStructuredOutput
	// CapabilityResumeSession reports support for resuming native sessions.
	CapabilityResumeSession = constants.RuntimeCapabilityResumeSession
	// CapabilityDynamicModels reports that detection obtained a dynamic model list.
	CapabilityDynamicModels = constants.RuntimeCapabilityDynamicModels
	// CapabilityStreamingUserMessages reports that active invocations can receive additional user messages.
	CapabilityStreamingUserMessages = constants.RuntimeCapabilityStreamingUserMessages
	// CapabilityAddDirectory reports support for declaring additional working directories.
	CapabilityAddDirectory = constants.RuntimeCapabilityAddDirectory
	// CapabilityPartialMessages reports support for finer-grained streaming messages.
	CapabilityPartialMessages = constants.RuntimeCapabilityPartialMessages
	// CapabilityImagePath reports support for native image submission through local paths.
	CapabilityImagePath = constants.RuntimeCapabilityImagePath
	// CapabilitySkillDirectory reports support for native skill loading from caller-specified directories.
	CapabilitySkillDirectory = constants.RuntimeCapabilitySkillDirectory
	// CapabilityMCPStreamableHTTP reports support for Streamable HTTP MCP.
	CapabilityMCPStreamableHTTP = constants.RuntimeCapabilityMCPStreamableHTTP
	// CapabilityMCPSSE reports support for legacy SSE MCP.
	CapabilityMCPSSE = constants.RuntimeCapabilityMCPSSE
)

// CapabilitySet holds runtime capabilities confirmed by a detection pass.
type CapabilitySet map[Capability]bool

// Has reports whether the capability is available in the current snapshot.
func (set CapabilitySet) Has(capability Capability) bool {
	return set[capability]
}

// DiagnosticLevel aliases the shared runtime diagnostic-level enumeration.
type DiagnosticLevel = constants.RuntimeDiagnosticLevel

const (
	// DiagnosticLevelInfo indicates supplementary information that does not affect invocation.
	DiagnosticLevelInfo = constants.RuntimeDiagnosticLevelInfo
	// DiagnosticLevelWarning indicates degraded behavior that may still permit invocation.
	DiagnosticLevelWarning = constants.RuntimeDiagnosticLevelWarning
	// DiagnosticLevelError indicates that the runtime cannot currently start safely.
	DiagnosticLevelError = constants.RuntimeDiagnosticLevelError
)

// Diagnostic is a stable runtime diagnostic without secret values.
type Diagnostic struct {
	// Code is a stable diagnostic code for caller localization and aggregation.
	Code string
	// Level indicates the diagnostic's impact on the current operation.
	Level DiagnosticLevel
	// Message is a concise technical explanation for development and logging.
	Message string
}

// DetectionResult is the complete outcome of a runtime probe without side effects.
type DetectionResult struct {
	// Runtime summarizes the effective definition used for detection.
	Runtime RuntimeSummary
	// State reports runtime availability and protocol compatibility.
	State DetectionState
	// ResolvedBinary is the executable path actually probed and used for Run.
	ResolvedBinary string
	// Installations contains newly discovered and previously selected CLI installations.
	Installations []CLIInstallation
	// Version is the runtime CLI version wrapped by the ACP agent, or empty when unknown.
	Version string
	// Auth is the authentication probe result, independent of installation state.
	Auth AuthStatus
	// Models contains dynamic probe results or definition-supplied model hints.
	Models []ModelOption
	// ReasoningOptions lists reasoning efforts declared by the current default model through the runtime protocol.
	ReasoningOptions []ReasoningOption
	// DetectedDefaultModel is the effective ACP session model identifier during detection.
	DetectedDefaultModel string
	// DetectedDefaultReasoning is the effective ACP session reasoning effort identifier during detection.
	DetectedDefaultReasoning string
	// Capabilities is the capability snapshot confirmed by help and protocol probing.
	Capabilities CapabilitySet
	// Diagnostics contains detection failures, unknown states, and capability degradation information.
	Diagnostics []Diagnostic
}

// CLIInstallation identifies a CLI installation by its absolute executable path.
type CLIInstallation struct {
	// Path is the installation's absolute executable path.
	Path string
	// Version is the CLI version reported by this path; failed probes leave it empty or retain the previous result.
	Version string
	// Available reports whether this path remains executable during the current refresh.
	Available bool
}

// PermissionLevel is NeuxBot's three-level product permission contract across runtimes.
type PermissionLevel = constants.PermissionLevel

const (
	// PermissionLevelDefault requires individual user confirmation for sensitive operations.
	PermissionLevelDefault = constants.PermissionLevelDefault
	// PermissionLevelAuto permits automatic execution of operations with acceptable risk.
	PermissionLevelAuto = constants.PermissionLevelAuto
	// PermissionLevelFullAccess explicitly permits the runtime's maximum available access.
	PermissionLevelFullAccess = constants.PermissionLevelFullAccess
)

// ConnectorTransport aliases the shared runtime MCP transport enumeration.
type ConnectorTransport = constants.RuntimeConnectorTransport

const (
	// ConnectorTransportStdio is local standard input/output transport.
	ConnectorTransportStdio = constants.RuntimeConnectorTransportStdio
	// ConnectorTransportStreamableHTTP is Streamable HTTP transport.
	ConnectorTransportStreamableHTTP = constants.RuntimeConnectorTransportStreamableHTTP
	// ConnectorTransportSSE is legacy HTTP SSE transport.
	ConnectorTransportSSE = constants.RuntimeConnectorTransportSSE
)

const (
	// DiagnosticCodeACPOptionsUnavailable indicates that the ACP model catalog could not be fully refreshed.
	DiagnosticCodeACPOptionsUnavailable = "ACP_OPTIONS_UNAVAILABLE"
	// DiagnosticCodeACPOptionsFallback indicates reuse of the last successful model catalog during detection.
	DiagnosticCodeACPOptionsFallback = "ACP_OPTIONS_FALLBACK"
)

// PreservePreviousOptionsOnFailure retains the last successful catalog when ACP model catalog refresh fails.
func PreservePreviousOptionsOnFailure(
	previous DetectionResult,
	current DetectionResult,
) DetectionResult {
	previousCatalogAvailable := previous.Capabilities[CapabilityDynamicModels] ||
		hasDiagnostic(previous.Diagnostics, DiagnosticCodeACPOptionsFallback)
	if !hasDiagnostic(current.Diagnostics, DiagnosticCodeACPOptionsUnavailable) ||
		!previousCatalogAvailable || len(previous.Models) == 0 {
		return current
	}
	current.Models = cloneModels(previous.Models)
	current.ReasoningOptions = cloneReasoningOptions(previous.ReasoningOptions)
	current.DetectedDefaultModel = previous.DetectedDefaultModel
	current.DetectedDefaultReasoning = previous.DetectedDefaultReasoning
	current.Diagnostics = append(current.Diagnostics, Diagnostic{
		Code:    DiagnosticCodeACPOptionsFallback,
		Level:   DiagnosticLevelWarning,
		Message: "using the previous model catalog after ACP option refresh failed",
	})
	return current
}

// AvailableModels merges the detected runtime catalog with user-maintained custom model identifiers.
func AvailableModels(
	detection DetectionResult,
	customModelIDs []string,
) []ModelOption {
	models := cloneModels(detection.Models)
	seen := make(map[string]struct{}, len(models)+len(customModelIDs))
	for _, model := range models {
		seen[model.ID] = struct{}{}
	}
	for _, rawID := range customModelIDs {
		modelID := strings.TrimSpace(rawID)
		if modelID == "" {
			continue
		}
		if _, exists := seen[modelID]; exists {
			continue
		}
		seen[modelID] = struct{}{}
		permissionLevels := []PermissionLevel(nil)
		if len(models) > 0 {
			permissionLevels = append(permissionLevels, models[0].PermissionLevels...)
		}
		models = append(models, ModelOption{
			ID:               modelID,
			Name:             modelID,
			PermissionLevels: permissionLevels,
		})
	}
	return models
}

func hasDiagnostic(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func cloneModels(values []ModelOption) []ModelOption {
	cloned := make([]ModelOption, len(values))
	for index, model := range values {
		cloned[index] = model
		cloned[index].ReasoningOptions = cloneReasoningOptions(model.ReasoningOptions)
		cloned[index].PermissionLevels = append([]PermissionLevel(nil), model.PermissionLevels...)
	}
	return cloned
}

func cloneReasoningOptions(values []ReasoningOption) []ReasoningOption {
	return append([]ReasoningOption(nil), values...)
}

// AdditionalDirectorySupport uses verified launch entrypoints or an explicit native discovery result.
// A missing capability key remains unknown; it is not a refusal or a fallback selection.
func AdditionalDirectorySupport(detection DetectionResult) (supported, known bool) {
	provider := detection.Runtime.BaseRuntimeID
	if provider == "" {
		provider = detection.Runtime.ID
	}
	if nativeAdditionalDirectoryFlag(provider) != "" {
		return true, true
	}
	supported, known = detection.Capabilities[CapabilityAddDirectory]
	return supported, known
}

// nativeAdditionalDirectoryFlag is the sole mapping for already verified native launch entrypoints.
func nativeAdditionalDirectoryFlag(provider string) string {
	switch provider {
	case RuntimeIDGemini:
		return "--include-directories"
	case RuntimeIDWorkBuddy, RuntimeIDQoder, RuntimeIDQoderCN:
		return "--add-dir"
	default:
		return ""
	}
}
