package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/acp-go/pkg/agentclient"
)

// Catalog 保存 Runtime 安装、Profile 和启动校验所需的静态声明。
type Catalog struct {
	// definitions 保存已经合并 Local Profile 的只读 Runtime Definition。
	definitions map[string]runtimeDefinition
}

// CatalogConfig 配置宿主目录中的本地启动配置。
type CatalogConfig struct {
	// Profiles 是沿用基础智能体能力的本地启动配置。
	Profiles []ProfileSpec
}

// NewCatalog 校验本地配置并构造不启动后台工作的智能体目录。
func NewCatalog(config CatalogConfig) (*Catalog, error) {
	definitions, err := newRegistry(config.Profiles)
	if err != nil {
		return nil, err
	}
	return &Catalog{definitions: definitions}, nil
}

// Runtimes 返回按 Runtime ID 排序的不可变 Definition 摘要。
func (sdk *Catalog) Runtimes() []RuntimeSummary {
	return definitionSummaries(sdk.definitions)
}

type runtimeDefinition struct {
	// id 是当前 Runtime 或 Profile 的开放字符串标识。
	id string
	// baseRuntimeID 是提供协议语义的内置 Runtime 标识。
	baseRuntimeID string
	// name 是面向调用方展示的名称。
	name string
	// profile 表示该 Definition 是否由 Local Profile 生成。
	profile bool
	// binaryCandidates 是按优先级尝试的可执行文件候选。
	binaryCandidates []string
	// userBinaryCandidates 是相对用户主目录的官方原生程序路径，不包含仅分发命令的桌面入口。
	userBinaryCandidates []string
	// prefixArgs 是所有探测和调用共享的前置参数。
	prefixArgs []string
	// environment 是 Definition 和 Profile 合并后的环境覆盖。
	environment map[string]string
	// models 是检测失败时仍可使用的模型提示。
	models []ModelOption
	// defaultModel 是未显式选择模型时的模型标识。
	defaultModel string
	// reasoningOptions 是该 Runtime 明确支持的推理强度选项。
	reasoningOptions []ReasoningOption
	// instructionFileName 是工作区根目录使用的指令文件名。
	instructionFileName string
	// skillDirectoryName 是工作区根目录使用的技能目录名。
	skillDirectoryName string
	// configHomeEnvironmentVariable 是配置主目录对应的受控环境变量名。
	configHomeEnvironmentVariable string
	// mcpTransports 保存该原生运行时支持的连接器传输。
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
		binaryCandidates:              []string{"qoder", "qodercli"},
		userBinaryCandidates:          []string{".qoder/bin/qodercli/qodercli"},
		instructionFileName:           "AGENTS.md",
		skillDirectoryName:            ".qoder",
		configHomeEnvironmentVariable: "QODER_CONFIG_DIR",
		mcpTransports:                 []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP, ConnectorTransportSSE},
	},
	RuntimeIDQoderCN: {
		name:                          "Qoder CN",
		binaryCandidates:              []string{"qodercn", "qoder-cn", "qoderclicn"},
		userBinaryCandidates:          []string{".qoder-cn/bin/qoderclicn/qoderclicn"},
		instructionFileName:           "AGENTS.md",
		skillDirectoryName:            ".qoder",
		configHomeEnvironmentVariable: "QODERCN_CONFIG_DIR",
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

// definitionWithLaunchOverride 将持久化启动覆盖合并到不可变 Runtime Definition 副本。
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

type environmentSnapshot struct {
	// values 保存规范化键对应的最终环境值。
	values map[string]string
	// names 保存规范化键对应的最终输出键名。
	names map[string]string
}

func buildEnvironment(
	base []string,
	definition map[string]string,
) (environmentSnapshot, error) {
	snapshot := environmentSnapshot{
		values: make(map[string]string, len(base)+len(definition)),
		names:  make(map[string]string, len(base)+len(definition)),
	}
	seenBaseNames := make(map[string]string, len(base))
	for _, item := range base {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			return environmentSnapshot{}, status_error.RuntimeConfigurationInvalid
		}
		if err := validateEnvironmentEntry(key, value); err != nil {
			return environmentSnapshot{}, status_error.RuntimeConfigurationInvalid
		}
		normalized := normalizeEnvironmentKey(key)
		if previous, exists := seenBaseNames[normalized]; exists &&
			runtime.GOOS == "windows" &&
			previous != key {
			return environmentSnapshot{}, status_error.RuntimeConfigurationInvalid
		}
		seenBaseNames[normalized] = key
		snapshot.values[normalized] = value
		snapshot.names[normalized] = key
	}
	for key, value := range definition {
		if err := validateEnvironmentEntry(key, value); err != nil {
			return environmentSnapshot{}, status_error.RuntimeUnavailable
		}
		normalized := normalizeEnvironmentKey(key)
		snapshot.values[normalized] = value
		snapshot.names[normalized] = key
	}
	return snapshot, nil
}

func buildPreparedEnvironment(
	base []string,
	definition map[string]string,
	homeDirectory string,
) (environmentSnapshot, error) {
	snapshot, err := buildEnvironment(base, definition)
	if err != nil {
		return environmentSnapshot{}, err
	}
	if err := supplementExecutablePath(&snapshot, homeDirectory); err != nil {
		return environmentSnapshot{}, err
	}
	return snapshot, nil
}

func supplementExecutablePath(
	snapshot *environmentSnapshot,
	homeDirectory string,
) error {
	if strings.ContainsRune(homeDirectory, '\x00') {
		return status_error.BadRequest
	}
	if homeDirectory != "" && !filepath.IsAbs(homeDirectory) {
		return status_error.BadRequest
	}
	additions := commonExecutableDirectories(runtime.GOOS, homeDirectory, *snapshot)
	pathKey := normalizeEnvironmentKey("PATH")
	existing := filepath.SplitList(snapshot.values[pathKey])
	seen := make(map[string]struct{}, len(existing)+len(additions))
	for _, directory := range existing {
		if directory != "" {
			seen[normalizedPathForComparison(directory)] = struct{}{}
		}
	}
	for _, directory := range additions {
		if directory == "" {
			continue
		}
		comparison := normalizedPathForComparison(directory)
		if _, exists := seen[comparison]; exists {
			continue
		}
		existing = append(existing, directory)
		seen[comparison] = struct{}{}
	}
	snapshot.values[pathKey] = strings.Join(existing, string(filepath.ListSeparator))
	if snapshot.names[pathKey] == "" {
		snapshot.names[pathKey] = "PATH"
	}
	return nil
}

func commonExecutableDirectories(
	goos string,
	homeDirectory string,
	environment environmentSnapshot,
) []string {
	directories := make([]string, 0, 32)
	if homeDirectory != "" {
		directories = append(
			directories,
			commonUserExecutableDirectories(goos, homeDirectory, environment)...,
		)
	}
	return append(directories, commonSystemExecutableDirectories(goos)...)
}

func commonUserExecutableDirectories(
	goos string,
	homeDirectory string,
	environment environmentSnapshot,
) []string {
	miseDataDirectory := environment.get("MISE_DATA_DIR")
	if miseDataDirectory == "" {
		xdgDataDirectory := environment.get("XDG_DATA_HOME")
		miseDataDirectory = filepath.Join(homeDirectory, ".local", "share", "mise")
		if xdgDataDirectory != "" {
			miseDataDirectory = filepath.Join(xdgDataDirectory, "mise")
		}
	}
	directories := []string{
		filepath.Join(homeDirectory, ".local", "bin"),
		filepath.Join(miseDataDirectory, "shims"),
		filepath.Join(homeDirectory, ".mise", "shims"),
		filepath.Join(homeDirectory, ".npm-global", "bin"),
		filepath.Join(homeDirectory, ".npm-packages", "bin"),
		filepath.Join(configuredToolRoot(environment, "VOLTA_HOME", homeDirectory, ".volta"), "bin"),
		filepath.Join(configuredToolRoot(environment, "ASDF_DATA_DIR", homeDirectory, ".asdf"), "shims"),
		filepath.Join(configuredToolRoot(environment, "BUN_INSTALL", homeDirectory, ".bun"), "bin"),
		pnpmExecutableDirectory(goos, homeDirectory, environment),
		filepath.Join(configuredToolRoot(environment, "CARGO_HOME", homeDirectory, ".cargo"), "bin"),
		filepath.Join(configuredToolRoot(environment, "DENO_INSTALL", homeDirectory, ".deno"), "bin"),
		goExecutableDirectory(homeDirectory, environment),
		filepath.Join(homeDirectory, ".pyenv", "shims"),
		filepath.Join(homeDirectory, "bin"),
	}
	if npmPrefix := environment.get("NPM_CONFIG_PREFIX"); npmPrefix != "" {
		if goos == "windows" {
			directories = append(directories, npmPrefix)
		} else {
			directories = append(directories, filepath.Join(npmPrefix, "bin"))
		}
	}
	if goos == "windows" {
		directories = append(directories, windowsUserExecutableDirectories(homeDirectory, environment)...)
	}
	return directories
}

func configuredToolRoot(
	environment environmentSnapshot,
	variable string,
	homeDirectory string,
	fallback string,
) string {
	if directory := environment.get(variable); directory != "" {
		return directory
	}
	return filepath.Join(homeDirectory, fallback)
}

func goExecutableDirectory(homeDirectory string, environment environmentSnapshot) string {
	if directory := environment.get("GOBIN"); directory != "" {
		return directory
	}
	if goPath := environment.get("GOPATH"); goPath != "" {
		return filepath.Join(filepath.SplitList(goPath)[0], "bin")
	}
	return filepath.Join(homeDirectory, "go", "bin")
}

func pnpmExecutableDirectory(
	goos string,
	homeDirectory string,
	environment environmentSnapshot,
) string {
	if directory := environment.get("PNPM_HOME"); directory != "" {
		return directory
	}
	switch goos {
	case "darwin":
		return filepath.Join(homeDirectory, "Library", "pnpm")
	case "windows":
		return filepath.Join(homeDirectory, "AppData", "Local", "pnpm")
	default:
		return filepath.Join(homeDirectory, ".local", "share", "pnpm")
	}
}

func windowsUserExecutableDirectories(
	homeDirectory string,
	environment environmentSnapshot,
) []string {
	directories := []string{
		filepath.Join(homeDirectory, "AppData", "Roaming", "npm"),
		filepath.Join(homeDirectory, "scoop", "shims"),
	}
	if localAppData := environment.get("LOCALAPPDATA"); localAppData != "" {
		directories = append(directories, filepath.Join(localAppData, "Microsoft", "WinGet", "Links"))
	}
	if chocolateyInstall := environment.get("ChocolateyInstall"); chocolateyInstall != "" {
		directories = append(directories, filepath.Join(chocolateyInstall, "bin"))
	}
	return directories
}

func commonSystemExecutableDirectories(goos string) []string {
	switch goos {
	case "darwin":
		return []string{
			"/opt/homebrew/bin",
			"/usr/local/bin",
			"/usr/bin",
			"/bin",
			"/usr/sbin",
			"/sbin",
		}
	case "linux":
		return []string{
			"/home/linuxbrew/.linuxbrew/bin",
			"/usr/local/bin",
			"/usr/bin",
			"/bin",
			"/usr/sbin",
			"/sbin",
			"/snap/bin",
		}
	default:
		return []string{}
	}
}

func normalizedPathForComparison(path string) string {
	normalized := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToUpper(normalized)
	}
	return normalized
}

func validateEnvironmentEntry(key string, value string) error {
	if key == "" {
		return status_error.BadRequest
	}
	if strings.ContainsAny(key, "=\x00") {
		return status_error.BadRequest
	}
	if strings.ContainsRune(value, '\x00') {
		return status_error.BadRequest
	}
	return nil
}

func normalizeEnvironmentKey(key string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(key)
	}
	return key
}

func (snapshot environmentSnapshot) get(key string) string {
	return snapshot.values[normalizeEnvironmentKey(key)]
}

func (snapshot environmentSnapshot) list() []string {
	keys := make([]string, 0, len(snapshot.values))
	for key := range snapshot.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(
			environment,
			snapshot.names[key]+"="+snapshot.values[key],
		)
	}
	return environment
}

// definitionExecutableCandidates 在 PATH 命令之后补充官方用户级原生程序，显式路径覆盖不会回退到其他安装。
func definitionExecutableCandidates(definition runtimeDefinition, homeDirectory string) []string {
	candidates := cloneStrings(definition.binaryCandidates)
	if homeDirectory == "" {
		return candidates
	}
	for _, relative := range definition.userBinaryCandidates {
		path := filepath.Join(homeDirectory, filepath.FromSlash(relative))
		if runtime.GOOS == "windows" {
			path += ".exe"
		}
		candidates = append(candidates, path)
	}
	return candidates
}

func resolveExecutable(
	candidates []string,
	environment environmentSnapshot,
) (string, error) {
	var failures []string
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		resolved, err := resolveExecutableCandidate(candidate, environment)
		if err == nil {
			return resolved, nil
		}
		failures = append(failures, err.Error())
	}
	if len(failures) == 0 {
		return "", status_error.RuntimeUnavailable
	}
	return "", status_error.RuntimeUnavailable
}

func resolveExecutableCandidate(
	candidate string,
	environment environmentSnapshot,
) (string, error) {
	if filepath.IsAbs(candidate) || strings.ContainsAny(candidate, `/\`) {
		return validateExecutablePath(candidate)
	}
	pathValue := environment.get("PATH")
	for _, directory := range filepath.SplitList(pathValue) {
		if directory == "" {
			continue
		}
		if runtime.GOOS == "windows" {
			for _, extension := range windowsExecutableExtensions(candidate, environment) {
				path := filepath.Join(directory, candidate+extension)
				if resolved, err := validateExecutablePath(path); err == nil {
					return resolved, nil
				}
			}
			continue
		}
		path := filepath.Join(directory, candidate)
		if resolved, err := validateExecutablePath(path); err == nil {
			return resolved, nil
		}
	}
	return "", status_error.BadRequest
}

func windowsExecutableExtensions(
	candidate string,
	environment environmentSnapshot,
) []string {
	if filepath.Ext(candidate) != "" {
		return []string{""}
	}
	pathExt := environment.get("PATHEXT")
	if pathExt == "" {
		pathExt = ".COM;.EXE;.BAT;.CMD"
	}
	parts := strings.Split(pathExt, ";")
	extensions := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			extensions = append(extensions, part)
		}
	}
	return extensions
}

func validateExecutablePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", status_error.RuntimeUnavailable
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", status_error.RuntimeUnavailable
	}
	if info.IsDir() {
		return "", status_error.RuntimeUnavailable
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return "", status_error.RuntimeUnavailable
	}
	return absolute, nil
}

// DiscoverInstallations 枚举同一 CLI 的可执行路径，并保留此前保存的失效实例。
func (sdk *Catalog) DiscoverInstallations(
	ctx context.Context,
	request DetectRequest,
	previous []CLIInstallation,
) []CLIInstallation {
	definition, exists := sdk.definition(request.RuntimeID)
	if !exists {
		return append([]CLIInstallation(nil), previous...)
	}
	configured, err := definitionWithLaunchOverride(definition, request.LaunchOverride)
	if err != nil {
		return append([]CLIInstallation(nil), previous...)
	}
	environment, err := buildPreparedEnvironment(request.BaseEnvironment, configured.environment, request.HomeDirectory)
	if err != nil {
		return append([]CLIInstallation(nil), previous...)
	}
	paths := make([]string, 0, 16)
	seen := make(map[string]bool)
	add := func(path string) {
		if path == "" {
			return
		}
		absolute, err := filepath.Abs(path)
		if err == nil && !seen[absolute] {
			paths = append(paths, absolute)
			seen[absolute] = true
		}
	}
	// 当前选择及历史实例优先保留，后续再扫描新发现的 PATH 条目。
	if filepath.IsAbs(request.LaunchOverride.Binary) {
		add(request.LaunchOverride.Binary)
	}
	for _, installation := range previous {
		add(installation.Path)
	}
	for _, candidate := range definitionExecutableCandidates(definition, request.HomeDirectory) {
		if filepath.IsAbs(candidate) || strings.ContainsAny(candidate, `/\`) {
			if _, err := validateExecutablePath(candidate); err == nil {
				add(candidate)
			}
			continue
		}
		// 逐个检查 CLI 自带的用户级安装目录，不改变 PATH 选择顺序。
		if request.HomeDirectory != "" && runtime.GOOS != "windows" {
			vendorPath := filepath.Join(request.HomeDirectory, "."+definition.baseRuntimeID, "bin", candidate)
			if _, err := validateExecutablePath(vendorPath); err == nil {
				add(vendorPath)
			}
		}
		for _, directory := range filepath.SplitList(environment.get("PATH")) {
			if directory == "" {
				continue
			}
			if runtime.GOOS == "windows" {
				for _, extension := range windowsExecutableExtensions(candidate, environment) {
					path := filepath.Join(directory, candidate+extension)
					if _, err := validateExecutablePath(path); err == nil {
						add(path)
					}
				}
				continue
			}
			path := filepath.Join(directory, candidate)
			if _, err := validateExecutablePath(path); err == nil {
				add(path)
			}
		}
	}
	priorVersion := make(map[string]string, len(previous))
	for _, installation := range previous {
		priorVersion[installation.Path] = installation.Version
	}
	probeContext, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	installations := make([]CLIInstallation, len(paths))
	// 限制同时启动的版本命令数量，避免多个 PATH 安装耗尽子进程资源。
	slots := make(chan struct{}, 4)
	var waitGroup sync.WaitGroup
	for index, path := range paths {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			installation := CLIInstallation{Path: path, Version: NormalizeRuntimeVersion(priorVersion[path])}
			defer func() { installations[index] = installation }()
			if _, err := validateExecutablePath(path); err != nil {
				return
			}
			installation.Available = true
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-probeContext.Done():
				return
			}
			if probeContext.Err() != nil {
				return
			}
			versionContext, stop := context.WithTimeout(probeContext, 2*time.Second)
			defer stop()
			args := append(append([]string(nil), configured.prefixArgs...), "--version")
			if version := NormalizeRuntimeVersion(agentclient.RuntimeVersion(versionContext, path, args, environment.list(), "")); version != "" {
				installation.Version = version
			}
		}()
	}
	waitGroup.Wait()
	return installations
}

// NormalizeRuntimeVersion 将空值与未知版本归一为空，保留 CLI 原生版本格式。
func NormalizeRuntimeVersion(version string) string {
	version = strings.TrimSpace(version)
	if strings.EqualFold(version, "unknown") {
		return ""
	}
	return version
}

// protectedPrefixArguments 禁止前置参数覆盖 NeuxBot 管理的原生执行设置。
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

// LaunchOverrideArgumentsAllowed 判断前置参数是否避开 NeuxBot 管理的协议、模型、Session、工作区和权限参数。
func LaunchOverrideArgumentsAllowed(baseRuntimeID string, arguments []string) bool {
	baseRuntimeID = strings.TrimSpace(baseRuntimeID)
	if baseRuntimeID == RuntimeIDQoderCN {
		baseRuntimeID = RuntimeIDQoder
	}
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

// LaunchDetectInputsEqual 判断两次环境配置是否会形成相同的 ACP 探测启动输入。
func LaunchDetectInputsEqual(previous, next LaunchDetectInput) bool {
	return strings.TrimSpace(previous.Binary) == strings.TrimSpace(next.Binary) &&
		strings.TrimSpace(previous.ConfigHome) == strings.TrimSpace(next.ConfigHome) &&
		slices.Equal(previous.PrefixArgs, next.PrefixArgs)
}

// LaunchDetectInput 保存会影响 Runtime 探测的受控启动字段。
type LaunchDetectInput struct {
	// Binary 是用户保存的可执行文件名或绝对路径。
	Binary string
	// PrefixArgs 是放在 Runtime 子命令之前的用户前置参数。
	PrefixArgs []string
	// ConfigHome 是由 Runtime Definition 映射到受控环境变量的配置主目录。
	ConfigHome string
}

// BaseRuntimeIDs 返回连接器兼容矩阵使用的基础 Runtime 列表，顺序稳定便于 UI 展示。
func BaseRuntimeIDs() []string {
	return []string{
		RuntimeIDOpenCode,
		RuntimeIDGemini,
		RuntimeIDGrok,
		RuntimeIDWorkBuddy,
		RuntimeIDQoder,
		RuntimeIDQoderCN,
		RuntimeIDCodex,
		RuntimeIDClaude,
		RuntimeIDKimi,
		RuntimeIDCursor,
		RuntimeIDPi,
	}
}

// ConnectorCompatibility 返回一个基础 Runtime 是否支持指定 MCP 传输方式。
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

func (sdk *Catalog) definitionForRequest(
	request DetectRequest,
) (runtimeDefinition, bool, error) {
	definition, exists := sdk.definition(request.RuntimeID)
	if !exists {
		return runtimeDefinition{}, false, nil
	}
	resolved, err := definitionWithLaunchOverride(definition, request.LaunchOverride)
	if err != nil {
		return runtimeDefinition{}, true, err
	}
	return resolved, true, nil
}

// ResolveExecutable 只解析指定 Runtime 的可执行文件，不启动 ACP 或检查账号。
func (sdk *Catalog) ResolveExecutable(request DetectRequest) (string, error) {
	definition, exists, err := sdk.definitionForRequest(request)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", status_error.RuntimeNotRegistered.WithParams(map[string]interface{}{"RuntimeID": request.RuntimeID})
	}
	environment, err := buildPreparedEnvironment(
		request.BaseEnvironment,
		definition.environment,
		request.HomeDirectory,
	)
	if err != nil {
		return "", err
	}
	return resolveExecutable(definitionExecutableCandidates(definition, request.HomeDirectory), environment)
}
