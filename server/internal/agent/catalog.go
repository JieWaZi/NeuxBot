package agent

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// Catalog stores static declarations for runtime installation, profiles, and launch validation.
type Catalog struct {
	// definitions holds read-only runtime definitions merged with local profiles.
	definitions map[string]runtimeDefinition
}

// CatalogConfig configures local launch profiles in the host catalog.
type CatalogConfig struct {
	// Profiles contains local launch configurations inheriting base runtime capabilities.
	Profiles []ProfileSpec
}

// NewCatalog validates local configuration and builds the runtime catalog without starting background work.
func NewCatalog(config CatalogConfig) (*Catalog, error) {
	definitions, err := newRegistry(config.Profiles)
	if err != nil {
		return nil, err
	}
	return &Catalog{definitions: definitions}, nil
}

// Runtimes returns immutable definition summaries sorted by runtime ID.
func (sdk *Catalog) Runtimes() []RuntimeSummary {
	return definitionSummaries(sdk.definitions)
}

type runtimeDefinition struct {
	// id is the open string identifier of the runtime or profile.
	id string
	// baseRuntimeID identifies the built-in runtime providing protocol semantics.
	baseRuntimeID string
	// name is the display name exposed to callers.
	name string
	// profile reports whether the definition was generated from a local profile.
	profile bool
	// binaryCandidates lists executable candidates in priority order.
	binaryCandidates []string
	// userBinaryCandidates lists official native executable paths relative to the user home, excluding desktop dispatchers.
	userBinaryCandidates []string
	// prefixArgs holds leading arguments shared by probes and invocations.
	prefixArgs []string
	// environment contains environment overrides merged from the definition and profile.
	environment map[string]string
	// models holds model hints available even when detection fails.
	models []ModelOption
	// defaultModel identifies the model used without an explicit selection.
	defaultModel string
	// reasoningOptions lists reasoning effort options explicitly supported by this runtime.
	reasoningOptions []ReasoningOption
	// instructionFileName is the instruction filename at the workspace root.
	instructionFileName string
	// skillDirectoryName is the skill directory name at the workspace root.
	skillDirectoryName string
	// configHomeEnvironmentVariable is the controlled environment variable for the configuration home.
	configHomeEnvironmentVariable string
	// mcpTransports lists connector transports supported by the native runtime.
	mcpTransports []ConnectorTransport
}

var baseRuntimeDefinitions = map[string]runtimeDefinition{
	RuntimeIDCodex: {
		name:                          "Codex CLI",
		binaryCandidates:              []string{"codex"},
		instructionFileName:           "AGENTS.md",
		skillDirectoryName:            ".agents",
		configHomeEnvironmentVariable: "CODEX_HOME",
		mcpTransports:                 []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP},
	},
	RuntimeIDClaude: {
		name:                          "Claude Code",
		binaryCandidates:              []string{"claude"},
		instructionFileName:           "CLAUDE.md",
		skillDirectoryName:            ".claude",
		configHomeEnvironmentVariable: "CLAUDE_CONFIG_DIR",
		mcpTransports:                 []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP, ConnectorTransportSSE},
		models:                        []ModelOption{{ID: "sonnet", Name: "Sonnet"}, {ID: "opus", Name: "Opus"}, {ID: "haiku", Name: "Haiku"}},
		defaultModel:                  "sonnet",
	},
	RuntimeIDKimi: {
		name:                "Kimi",
		binaryCandidates:    []string{"kimi"},
		instructionFileName: "AGENTS.md",
		skillDirectoryName:  ".agents",
		mcpTransports:       []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP},
	},
	RuntimeIDCursor: {
		name:                "Cursor",
		binaryCandidates:    []string{"cursor-agent", "agent"},
		instructionFileName: "AGENTS.md",
		skillDirectoryName:  ".cursor",
		mcpTransports:       []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP},
	},
	RuntimeIDPi: {
		name:                          "Pi",
		binaryCandidates:              []string{"pi"},
		instructionFileName:           "AGENTS.md",
		skillDirectoryName:            ".pi",
		configHomeEnvironmentVariable: "PI_CODING_AGENT_DIR",
		mcpTransports:                 []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP, ConnectorTransportSSE},
	},
	RuntimeIDOpenCode: {
		name:                          "OpenCode",
		binaryCandidates:              []string{"opencode"},
		instructionFileName:           "AGENTS.md",
		skillDirectoryName:            ".opencode",
		configHomeEnvironmentVariable: "OPENCODE_CONFIG_DIR",
		mcpTransports:                 []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP, ConnectorTransportSSE},
	},
	RuntimeIDGemini: {
		name:                          "Gemini CLI",
		binaryCandidates:              []string{"gemini"},
		instructionFileName:           "GEMINI.md",
		skillDirectoryName:            ".gemini",
		configHomeEnvironmentVariable: "GEMINI_CLI_HOME",
		mcpTransports:                 []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP, ConnectorTransportSSE},
	},
	RuntimeIDGrok: {
		name:                          "Grok Build",
		binaryCandidates:              []string{"grok"},
		instructionFileName:           "AGENTS.md",
		skillDirectoryName:            ".grok",
		configHomeEnvironmentVariable: "GROK_HOME",
		mcpTransports:                 []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP, ConnectorTransportSSE},
	},
	RuntimeIDWorkBuddy: {
		name:                          "WorkBuddy",
		binaryCandidates:              []string{"codebuddy", "cbc"},
		instructionFileName:           "CODEBUDDY.md",
		skillDirectoryName:            ".codebuddy",
		configHomeEnvironmentVariable: "CODEBUDDY_CONFIG_DIR",
		mcpTransports:                 []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP, ConnectorTransportSSE},
	},
	RuntimeIDQoder: {
		name:                          "Qoder",
		binaryCandidates:              []string{"qoder", "qodercli", "qoderclicn"},
		userBinaryCandidates:          []string{".qoder/bin/qodercli/qodercli"},
		instructionFileName:           "AGENTS.md",
		skillDirectoryName:            ".qoder",
		configHomeEnvironmentVariable: "QODER_CONFIG_DIR",
		mcpTransports:                 []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP, ConnectorTransportSSE},
	},
}

func newRegistry(profiles []ProfileSpec) (map[string]runtimeDefinition, error) {
	definitions := make(map[string]runtimeDefinition, len(baseRuntimeDefinitions)+len(profiles))
	for id, base := range baseRuntimeDefinitions {
		definition := cloneDefinition(base)
		definition.id, definition.baseRuntimeID = id, id
		definitions[id] = definition
	}

	for _, profile := range profiles {
		resolved, err := resolveProfile(profile, definitions)
		if err != nil {
			return nil, status_error.RuntimeConfigurationInvalid
		}
		if _, exists := definitions[resolved.id]; exists {
			return nil, status_error.RuntimeUnavailable
		}
		definitions[resolved.id] = resolved
	}
	return definitions, nil
}

func resolveProfile(
	profile ProfileSpec,
	definitions map[string]runtimeDefinition,
) (runtimeDefinition, error) {
	id := strings.TrimSpace(profile.ID)
	if id == "" {
		return runtimeDefinition{}, status_error.BadRequest
	}
	base, exists := definitions[profile.BaseRuntimeID]
	if !exists || base.profile {
		return runtimeDefinition{}, status_error.RuntimeUnavailable
	}
	resolved := cloneDefinition(base)
	resolved.id = id
	resolved.baseRuntimeID = base.baseRuntimeID
	resolved.profile = true
	if name := strings.TrimSpace(profile.Name); name != "" {
		resolved.name = name
	}
	if profile.BinaryCandidates != nil {
		resolved.binaryCandidates = cloneStrings(profile.BinaryCandidates)
		resolved.userBinaryCandidates = nil
	}
	if profile.PrefixArgs != nil {
		resolved.prefixArgs = cloneStrings(profile.PrefixArgs)
	}
	for key, value := range profile.Environment {
		resolved.environment[key] = value
	}
	if profile.Models != nil {
		resolved.models = cloneModels(profile.Models)
	}
	if profile.DefaultModel != "" {
		resolved.defaultModel = profile.DefaultModel
	}
	if err := validateRuntimeDefinition(resolved); err != nil {
		return runtimeDefinition{}, status_error.RuntimeConfigurationInvalid
	}
	return resolved, nil
}

// definitionWithLaunchOverride merges persisted launch overrides into an immutable runtime definition copy.
func definitionWithLaunchOverride(
	definition runtimeDefinition,
	override LaunchOverride,
) (runtimeDefinition, error) {
	resolved := cloneDefinition(definition)
	if binary := strings.TrimSpace(override.Binary); binary != "" {
		resolved.binaryCandidates = []string{binary}
		resolved.userBinaryCandidates = nil
	}
	if override.PrefixArgs != nil {
		resolved.prefixArgs = cloneStrings(override.PrefixArgs)
	}
	for key, value := range override.Environment {
		resolved.environment[key] = value
	}
	if err := validateRuntimeDefinition(resolved); err != nil {
		return runtimeDefinition{}, status_error.RuntimeConfigurationInvalid
	}
	return resolved, nil
}

func validateRuntimeDefinition(definition runtimeDefinition) error {
	if len(definition.binaryCandidates) == 0 {
		return status_error.BadRequest
	}
	for _, candidate := range definition.binaryCandidates {
		if strings.TrimSpace(candidate) == "" {
			return status_error.RuntimeUnavailable
		}
		if strings.ContainsRune(candidate, '\x00') {
			return status_error.RuntimeUnavailable
		}
	}
	if err := validateArguments("prefix", definition.prefixArgs); err != nil {
		return err
	}
	for key, value := range definition.environment {
		if err := validateEnvironmentEntry(key, value); err != nil {
			return status_error.RuntimeConfigurationInvalid
		}
	}
	if definition.configHomeEnvironmentVariable != "" {
		if err := validateEnvironmentEntry(definition.configHomeEnvironmentVariable, ""); err != nil {
			return status_error.RuntimeConfigurationInvalid
		}
	}
	modelIDs := make(map[string]struct{}, len(definition.models))
	for _, model := range definition.models {
		if strings.TrimSpace(model.ID) == "" {
			return status_error.BadRequest
		}
		if _, exists := modelIDs[model.ID]; exists {
			return status_error.BadRequest
		}
		modelIDs[model.ID] = struct{}{}
	}
	if definition.defaultModel != "" && len(modelIDs) > 0 {
		if _, exists := modelIDs[definition.defaultModel]; !exists {
			return status_error.RuntimeConfigurationInvalid
		}
	}
	reasoningIDs := make(map[string]struct{}, len(definition.reasoningOptions))
	for _, option := range definition.reasoningOptions {
		if strings.TrimSpace(option.ID) == "" {
			return status_error.BadRequest
		}
		if _, exists := reasoningIDs[option.ID]; exists {
			return status_error.BadRequest
		}
		reasoningIDs[option.ID] = struct{}{}
	}
	if err := validateRuntimeFiles(definition); err != nil {
		return err
	}
	return nil
}

func validateRuntimeFiles(definition runtimeDefinition) error {
	if definition.instructionFileName == "" {
		return status_error.BadRequest
	}
	if filepath.Base(definition.instructionFileName) != definition.instructionFileName ||
		definition.instructionFileName == "." || definition.instructionFileName == ".." ||
		strings.ContainsAny(definition.instructionFileName, "/\\\x00\r\n") {
		return status_error.BadRequest
	}
	if definition.skillDirectoryName == "" {
		return status_error.SkillUnavailable
	}
	if filepath.Base(definition.skillDirectoryName) != definition.skillDirectoryName ||
		definition.skillDirectoryName == "." || definition.skillDirectoryName == ".." ||
		strings.ContainsAny(definition.skillDirectoryName, "/\\\x00\r\n") {
		return status_error.SkillUnavailable
	}
	return nil
}

func validateArguments(name string, arguments []string) error {
	for _, argument := range arguments {
		if strings.ContainsRune(argument, '\x00') {
			return status_error.BadRequest
		}
	}
	return nil
}

func cloneDefinition(source runtimeDefinition) runtimeDefinition {
	cloned := source
	cloned.binaryCandidates = cloneStrings(source.binaryCandidates)
	cloned.userBinaryCandidates = cloneStrings(source.userBinaryCandidates)
	cloned.prefixArgs = cloneStrings(source.prefixArgs)
	cloned.environment = cloneStringMap(source.environment)
	cloned.models = cloneModels(source.models)
	cloned.reasoningOptions = cloneReasoningOptions(source.reasoningOptions)
	cloned.mcpTransports = slices.Clone(source.mcpTransports)
	return cloned
}

func definitionSummaries(
	definitions map[string]runtimeDefinition,
) []RuntimeSummary {
	summaries := make([]RuntimeSummary, 0, len(definitions))
	for _, definition := range definitions {
		summaries = append(summaries, RuntimeSummary{
			ID:                            definition.id,
			BaseRuntimeID:                 definition.baseRuntimeID,
			Name:                          definition.name,
			Profile:                       definition.profile,
			BinaryCandidates:              cloneStrings(definition.binaryCandidates),
			PrefixArgs:                    cloneStrings(definition.prefixArgs),
			Models:                        cloneModels(definition.models),
			DefaultModel:                  definition.defaultModel,
			ReasoningOptions:              cloneReasoningOptions(definition.reasoningOptions),
			InstructionFileName:           definition.instructionFileName,
			SkillDirectoryName:            definition.skillDirectoryName,
			ConfigHomeEnvironmentVariable: definition.configHomeEnvironmentVariable,
			ConfigHome:                    definition.environment[definition.configHomeEnvironmentVariable],
		})
	}
	sort.Slice(summaries, func(left, right int) bool {
		return summaries[left].ID < summaries[right].ID
	})
	return summaries
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func cloneStringMap(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

var protectedPrefixArguments = map[string]map[string]struct{}{
	RuntimeIDQoder:     {"--": {}, "--acp": {}, "--sdk": {}, "--cwd": {}, "-w": {}, "--config-dir": {}, "--add-dir": {}, "--attachment": {}, "--worktree": {}, "--model": {}, "-m": {}, "--reasoning-effort": {}, "--context-window": {}, "--list-models": {}, "--continue": {}, "-c": {}, "--resume": {}, "-r": {}, "--session-id": {}, "--fork-session": {}, "--list-sessions": {}, "--delete-session": {}, "--print": {}, "-p": {}, "--output-format": {}, "-o": {}, "--input-format": {}, "--prompt-interactive": {}, "-i": {}, "--no-session-persistence": {}, "--permission-mode": {}, "--yolo": {}, "--dangerously-skip-permissions": {}, "--tools": {}, "--allowed-tools": {}, "--disallowed-tools": {}, "--mcp-config": {}, "--strict-mcp-config": {}, "--allowed-mcp-server-names": {}, "--agent": {}, "--agents": {}, "--system-prompt": {}, "--append-system-prompt": {}, "--settings": {}, "--setting-sources": {}, "--plugin-dir": {}, "--remote": {}, "--remote-session": {}},
	RuntimeIDWorkBuddy: {"--": {}, "--acp": {}, "--acp-transport": {}, "--serve": {}, "--print": {}, "-p": {}, "--input-format": {}, "--output-format": {}, "--model": {}, "--effort": {}, "--permission-mode": {}, "--permission-mode-before-plan": {}, "--subagent-permission-mode": {}, "--dangerously-skip-permissions": {}, "-y": {}, "--resume": {}, "-r": {}, "--continue": {}, "-c": {}, "--session-id": {}, "--no-session-persistence": {}, "--fork-session": {}, "--add-dir": {}, "--mcp-config": {}, "--strict-mcp-config": {}, "--system-prompt": {}, "--system-prompt-file": {}, "--append-system-prompt": {}, "--agent": {}, "--multitask": {}, "--sandbox": {}, "--tools": {}, "--allowedTools": {}, "--disallowedTools": {}},
	RuntimeIDOpenCode:  {"acp": {}, "--": {}, "--model": {}, "-m": {}, "--agent": {}, "--session": {}, "-s": {}, "--continue": {}, "-c": {}, "--dir": {}, "--port": {}, "--hostname": {}},
	RuntimeIDGemini:    {"--": {}, "--acp": {}, "--experimental-acp": {}, "--model": {}, "-m": {}, "--prompt": {}, "-p": {}, "--approval-mode": {}, "--yolo": {}, "-y": {}, "--resume": {}, "-r": {}, "--sandbox": {}, "-s": {}, "--include-directories": {}, "--allowed-tools": {}, "--allowed-mcp-server-names": {}, "--output-format": {}},
	RuntimeIDGrok:      {"acp": {}, "--": {}, "--model": {}, "-m": {}, "--prompt": {}, "-p": {}, "--config": {}, "-c": {}, "--session": {}, "--resume": {}, "--yolo": {}, "--approval-mode": {}, "--directory": {}, "--cwd": {}},
	RuntimeIDKimi:      {"acp": {}, "--": {}, "--print": {}, "-p": {}, "--model": {}, "-m": {}, "--thinking": {}, "--no-thinking": {}, "--yolo": {}, "-y": {}, "--auto": {}, "--mode": {}, "--work-dir": {}, "-w": {}, "--session": {}, "--continue": {}, "--mcp-config": {}},
	RuntimeIDCursor:    {"acp": {}, "--": {}, "--print": {}, "-p": {}, "--model": {}, "-m": {}, "--mode": {}, "--force": {}, "-f": {}, "--yolo": {}, "--resume": {}, "--workspace": {}, "--output-format": {}, "--stream-partial-output": {}, "--auto-review": {}, "--approve-mcps": {}, "--sandbox": {}, "--trust": {}},
	RuntimeIDPi:        {"--": {}, "--mode": {}, "--model": {}, "--provider": {}, "--session": {}, "--session-dir": {}, "--thinking": {}, "--no-session": {}, "--extension": {}, "-e": {}, "--no-extensions": {}},
	RuntimeIDCodex: {
		"-C":                    {},
		"--cd":                  {},
		"--add-dir":             {},
		"--sandbox":             {},
		"-s":                    {},
		"--model":               {},
		"-m":                    {},
		"--image":               {},
		"-i":                    {},
		"-c":                    {},
		"--config":              {},
		"--json":                {},
		"--skip-git-repo-check": {},
		"--ask-for-approval":    {},
		"-a":                    {},
		"--dangerously-bypass-approvals-and-sandbox": {},
		"exec":       {},
		"resume":     {},
		"app-server": {},
	},
	RuntimeIDClaude: {
		"-p":                             {},
		"--print":                        {},
		"--input-format":                 {},
		"--output-format":                {},
		"--verbose":                      {},
		"--plugin-dir":                   {},
		"--include-partial-messages":     {},
		"--model":                        {},
		"--add-dir":                      {},
		"--resume":                       {},
		"--session-id":                   {},
		"--permission-mode":              {},
		"--dangerously-skip-permissions": {},
		"--allowedTools":                 {},
		"--disallowedTools":              {},
	},
}

// LaunchOverrideArgumentsAllowed checks that leading arguments avoid NeuxBot-managed protocol, model, session, workspace, and permission options.
func LaunchOverrideArgumentsAllowed(baseRuntimeID string, arguments []string) bool {
	baseRuntimeID = strings.TrimSpace(baseRuntimeID)
	protected, exists := protectedPrefixArguments[baseRuntimeID]
	if !exists {
		return false
	}
	for _, argument := range arguments {
		name := strings.TrimSpace(argument)
		if flag, _, found := strings.Cut(name, "="); found {
			name = flag
		}
		if _, blocked := protected[name]; blocked {
			return false
		}
	}
	return true
}

// LaunchDetectInputsEqual checks whether two environment configurations produce identical ACP detection launch inputs.
func LaunchDetectInputsEqual(previous, next LaunchDetectInput) bool {
	return strings.TrimSpace(previous.Binary) == strings.TrimSpace(next.Binary) &&
		strings.TrimSpace(previous.ConfigHome) == strings.TrimSpace(next.ConfigHome) &&
		slices.Equal(previous.PrefixArgs, next.PrefixArgs)
}

// LaunchDetectInput holds controlled launch fields that affect runtime detection.
type LaunchDetectInput struct {
	// Binary is the executable name or absolute path saved by the user.
	Binary string
	// PrefixArgs contains user arguments placed before the runtime subcommand.
	PrefixArgs []string
	// ConfigHome is the configuration home mapped to a controlled environment variable by the runtime definition.
	ConfigHome string
}

// BaseRuntimeIDs returns the base runtimes used by connector compatibility in a stable UI order.
func BaseRuntimeIDs() []string {
	return []string{
		RuntimeIDOpenCode,
		RuntimeIDGemini,
		RuntimeIDGrok,
		RuntimeIDWorkBuddy,
		RuntimeIDQoder,
		RuntimeIDCodex,
		RuntimeIDClaude,
		RuntimeIDKimi,
		RuntimeIDCursor,
		RuntimeIDPi,
	}
}

// ConnectorCompatibility reports whether a base runtime supports the given MCP transport.
func ConnectorCompatibility(
	baseRuntimeID string,
	transport ConnectorTransport,
) (bool, string) {
	definition, exists := baseRuntimeDefinitions[baseRuntimeID]
	if exists && slices.Contains(definition.mcpTransports, transport) {
		return true, ""
	}
	return false, "transport_not_supported"
}

func (sdk *Catalog) definition(id string) (runtimeDefinition, bool) {
	definition, exists := sdk.definitions[id]
	if !exists {
		return runtimeDefinition{}, false
	}
	return cloneDefinition(definition), true
}
