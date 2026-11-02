package agent

import (
	"context"
	"errors"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	native "github.com/JieWaZi/NeuxBot/server/internal/agent/native"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	"github.com/JieWaZi/acp-go/pkg/nativeacp"
	acp "github.com/JieWaZi/acp-go/sdk"
	"github.com/google/uuid"
	"github.com/shrewx/ginx/pkg/statuserror"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Catalog 提供智能体探测、启动准备、工作区物化与原生分叉。
type Catalog struct {
	// definitions 保存已经合并 Local Profile 的只读 Runtime Definition。
	definitions map[string]runtimeDefinition
	// acp 只接纳探测和分叉所需的短生命周期管理操作。
	acp acpManager
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
	return &Catalog{
		definitions: definitions,
		acp:         newACPBackend(),
	}, nil
}

// Close 停止接纳新的探测和分叉；各管理操作负责关闭自己的 ACP 连接。
func (sdk *Catalog) Close(ctx context.Context) error {
	if sdk == nil || sdk.acp == nil {
		return nil
	}
	err := sdk.acp.Close(ctx)
	var fault *native.Fault
	if errors.As(err, &fault) && fault.Code == native.FaultConfigurationInvalid {
		return status_error.RuntimeConfigurationInvalid
	}
	return err
}

// Runtimes 返回按 Runtime ID 排序的不可变 Definition 摘要。
func (sdk *Catalog) Runtimes() []RuntimeSummary {
	return definitionSummaries(sdk.definitions)
}

// PrepareLaunch 复用既有启动准备规则，仅输出事实，不执行探测、会话或模型请求。
func (sdk *Catalog) PrepareLaunch(target RuntimeTarget, detection DetectionResult) (LaunchFacts, error) {
	if sdk == nil {
		return LaunchFacts{}, status_error.RuntimeUnavailable
	}
	prepared, err := sdk.prepareFromDetection(sdk.prepareRequestForTarget(target), detection)
	if err != nil {
		return LaunchFacts{}, err
	}
	return LaunchFacts{BaseRuntimeID: prepared.definition.baseRuntimeID, Executable: prepared.detection.ResolvedBinary, PrefixArgs: cloneStrings(prepared.definition.prefixArgs), Environment: prepared.environment.list()}, nil
}

// preparedRuntime 保存同一次探测得到且可直接启动的不可变运行信息。
type preparedRuntime struct {
	// definition 保存已经解析 Profile 的有效 Runtime Definition。
	definition runtimeDefinition
	// detection 保存调用方可以持久化或展示的探测结论。
	detection DetectionResult
	// environment 保存探测和后续启动共享的显式环境快照。
	environment environmentSnapshot
}

// prepareFromDetection 使用调用方已经持久化的探测快照准备 Runtime，不执行新的探测。
func (sdk *Catalog) prepareFromDetection(
	request DetectRequest,
	detection DetectionResult,
) (preparedRuntime, error) {
	if detection.State != DetectionStateReady {
		return preparedRuntime{}, status_error.RuntimeUnavailable
	}
	definition, exists, err := sdk.definitionForRequest(request)
	if !exists {
		return preparedRuntime{}, status_error.RuntimeUnavailable
	}
	if err != nil {
		return preparedRuntime{}, err
	}
	if strings.TrimSpace(detection.Runtime.ID) != definition.id ||
		strings.TrimSpace(detection.ResolvedBinary) == "" {
		return preparedRuntime{}, status_error.RuntimeUnavailable
	}
	environment, err := buildPreparedEnvironment(
		request.BaseEnvironment,
		definition.environment,
		request.HomeDirectory,
	)
	if err != nil {
		return preparedRuntime{}, err
	}
	detection = cloneDetectionResult(detection)
	detection.Runtime = definitionSummaries(map[string]runtimeDefinition{
		definition.id: definition,
	})[0]
	if detection.Capabilities == nil {
		detection.Capabilities = CapabilitySet{}
	}
	return preparedRuntime{
		definition:  definition,
		detection:   detection,
		environment: environment,
	}, nil
}

func cloneDetectionResult(source DetectionResult) DetectionResult {
	clone := source
	clone.Runtime = cloneRuntimeSummary(source.Runtime)
	clone.Models = cloneModels(source.Models)
	clone.ReasoningOptions = cloneReasoningOptions(source.ReasoningOptions)
	clone.Capabilities = cloneCapabilitySet(source.Capabilities)
	clone.Diagnostics = append([]Diagnostic(nil), source.Diagnostics...)
	clone.Installations = append([]CLIInstallation(nil), source.Installations...)
	clone.DetectedDefaultModel = source.DetectedDefaultModel
	clone.DetectedDefaultReasoning = source.DetectedDefaultReasoning
	return clone
}

func cloneCapabilitySet(source CapabilitySet) CapabilitySet {
	cloned := make(CapabilitySet, len(source))
	for capability, enabled := range source {
		cloned[capability] = enabled
	}
	return cloned
}

// cloneRuntimeSummary 深拷贝 Runtime 摘要中的全部可变集合。
func cloneRuntimeSummary(summary RuntimeSummary) RuntimeSummary {
	clone := summary
	clone.BinaryCandidates = append([]string(nil), summary.BinaryCandidates...)
	clone.PrefixArgs = append([]string(nil), summary.PrefixArgs...)
	clone.Models = cloneModels(summary.Models)
	clone.ReasoningOptions = append([]ReasoningOption(nil), summary.ReasoningOptions...)
	return clone
}

// prepareRequestForTarget 合并所选账号隔离、Profile 与显式启动覆盖。
func (sdk *Catalog) prepareRequestForTarget(target RuntimeTarget) DetectRequest {
	environment := make(map[string]string)
	if definition, exists := sdk.definitions[target.RuntimeID]; exists &&
		target.ConfigHome != "" &&
		definition.configHomeEnvironmentVariable != "" {
		environment[definition.configHomeEnvironmentVariable] = target.ConfigHome
	}
	for key, value := range target.Environment {
		environment[key] = value
	}
	baseEnvironment := os.Environ()
	if target.Isolated {
		baseEnvironment = accountconfig.Environment(baseEnvironment)
	}
	homeDirectory, _ := os.UserHomeDir()
	return DetectRequest{
		RuntimeID:       target.RuntimeID,
		UseNativeAuth:   !target.Isolated,
		BaseEnvironment: baseEnvironment,
		HomeDirectory:   homeDirectory,
		LaunchOverride: LaunchOverride{
			Binary:      target.Binary,
			PrefixArgs:  append([]string(nil), target.PrefixArgs...),
			Environment: environment,
		},
	}
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
}

func newRegistry(profiles []ProfileSpec) (map[string]runtimeDefinition, error) {
	definitions := make(map[string]runtimeDefinition)
	for _, descriptor := range native.Descriptors() {
		definition := runtimeDefinition{id: descriptor.ID, baseRuntimeID: descriptor.ID, name: descriptor.Name, binaryCandidates: descriptor.BinaryCandidates, userBinaryCandidates: descriptor.UserBinaryCandidates, environment: map[string]string{}, instructionFileName: descriptor.InstructionFileName, skillDirectoryName: descriptor.SkillDirectoryName, configHomeEnvironmentVariable: descriptor.ConfigHomeEnvironmentVariable}
		if descriptor.ID == RuntimeIDClaude {
			definition.models = []ModelOption{{ID: "sonnet", Name: "Sonnet"}, {ID: "opus", Name: "Opus"}, {ID: "haiku", Name: "Haiku"}}
			definition.defaultModel = "sonnet"
		}
		definitions[descriptor.ID] = definition
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
			if version := NormalizeRuntimeVersion(nativeacp.RuntimeVersion(versionContext, path, args, environment.list(), "")); version != "" {
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
	RuntimeIDQoder:     {"--": {}, "--acp": {}, "--cwd": {}, "-w": {}, "--config-dir": {}, "--add-dir": {}, "--attachment": {}, "--worktree": {}, "--model": {}, "-m": {}, "--reasoning-effort": {}, "--context-window": {}, "--list-models": {}, "--continue": {}, "-c": {}, "--resume": {}, "-r": {}, "--session-id": {}, "--fork-session": {}, "--list-sessions": {}, "--delete-session": {}, "--print": {}, "-p": {}, "--output-format": {}, "-o": {}, "--input-format": {}, "--prompt-interactive": {}, "-i": {}, "--no-session-persistence": {}, "--permission-mode": {}, "--yolo": {}, "--dangerously-skip-permissions": {}, "--tools": {}, "--allowed-tools": {}, "--disallowed-tools": {}, "--mcp-config": {}, "--strict-mcp-config": {}, "--allowed-mcp-server-names": {}, "--agent": {}, "--agents": {}, "--system-prompt": {}, "--append-system-prompt": {}, "--settings": {}, "--setting-sources": {}, "--plugin-dir": {}, "--remote": {}, "--remote-session": {}},
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
	descriptor, exists := native.Lookup(baseRuntimeID)
	nativeTransport := native.MCPTransport(transport)
	if transport == ConnectorTransportStreamableHTTP {
		nativeTransport = native.MCPHTTP
	}
	if exists && slices.Contains(descriptor.MCPTransports, nativeTransport) {
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

// Detect 对指定 Runtime 执行有界且不触发模型推理的可用性探测。
func (sdk *Catalog) Detect(
	ctx context.Context,
	request DetectRequest,
) (result DetectionResult) {
	release := request.ReleaseResources
	defer func() {
		if release != nil {
			cleanupContext := ctx
			if cleanupContext == nil {
				cleanupContext = context.Background()
			}
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(cleanupContext), acpManagementCloseTimeout)
			defer cancel()
			if err := sdk.acp.ReleaseResources(cleanup, release); err != nil {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: DiagnosticCodeACPOptionsUnavailable, Level: DiagnosticLevelError, Message: FailureDisplayMessage(err)})
			}
		}
	}()
	if ctx == nil {
		return DetectionResult{
			State: DetectionStateUnavailable,
			Auth:  AuthStatusUnknown,
			Diagnostics: []Diagnostic{{
				Code:    string(FailureCodeInvalidRequest),
				Level:   DiagnosticLevelError,
				Message: "context is required",
			}},
		}
	}
	definition, exists, definitionErr := sdk.definitionForRequest(request)
	if !exists {
		return DetectionResult{
			State: DetectionStateUnavailable,
			Auth:  AuthStatusUnknown,
			Diagnostics: []Diagnostic{{
				Code:    "RUNTIME_NOT_FOUND",
				Level:   DiagnosticLevelError,
				Message: fmt.Sprintf("runtime %q is not registered", request.RuntimeID),
			}},
		}
	}
	if definitionErr != nil {
		return DetectionResult{
			Runtime: RuntimeSummary{ID: request.RuntimeID},
			State:   DetectionStateUnavailable,
			Auth:    AuthStatusUnknown,
			Diagnostics: []Diagnostic{{
				Code:    string(FailureCodeInvalidRequest),
				Level:   DiagnosticLevelError,
				Message: definitionErr.Error(),
			}},
		}
	}
	result = DetectionResult{
		Runtime: definitionSummaries(map[string]runtimeDefinition{definition.id: definition})[0],
		State:   DetectionStateUnavailable,
		Auth:    AuthStatusUnknown,
		Models:  cloneModels(definition.models),
		ReasoningOptions: cloneReasoningOptions(
			definition.reasoningOptions,
		),
		Capabilities: CapabilitySet{},
	}
	environment, err := buildPreparedEnvironment(
		request.BaseEnvironment,
		definition.environment,
		request.HomeDirectory,
	)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code:    "ENVIRONMENT_INVALID",
			Level:   DiagnosticLevelError,
			Message: err.Error(),
		})
		return result
	}
	binary, err := resolveExecutable(definitionExecutableCandidates(definition, request.HomeDirectory), environment)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code:    "RUNTIME_UNAVAILABLE",
			Level:   DiagnosticLevelError,
			Message: err.Error(),
		})
		return result
	}
	result.ResolvedBinary = binary

	probeDirectory := request.WorkingDirectory
	if probeDirectory == "" {
		probeDirectory, err = os.MkdirTemp("", "ally-runtime-probe-")
	} else {
		probeDirectory, err = ValidateDirectory(probeDirectory)
	}
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code:    "PROBE_DIRECTORY_FAILED",
			Level:   DiagnosticLevelError,
			Message: err.Error(),
		})
		return result
	}
	if request.WorkingDirectory == "" {
		externalRelease := release
		release = func(ctx context.Context) error {
			if externalRelease != nil {
				if err := externalRelease(ctx); err != nil {
					return err
				}
				externalRelease = nil
			}
			if err := os.RemoveAll(probeDirectory); err != nil {
				logx.Errorf("remove runtime probe directory failed: error=%v", err)
				return status_error.RuntimeShutdownFailed
			}
			return nil
		}
	}

	probeRelease := release
	release = nil
	catalog, catalogErr := sdk.acp.DetectOptions(ctx, acpOptionsRequest{
		ReleaseResources:   probeRelease,
		RefreshCredentials: request.RefreshCredentials,
		UseNativeAuth:      request.UseNativeAuth,
		RuntimeID:          definition.id,
		Model:              request.Model,
		Prepared: preparedRuntime{
			definition:  definition,
			detection:   result,
			environment: environment,
		},
		WorkingDirectory: probeDirectory,
	})
	result.Version = catalog.Version
	result.Capabilities = cloneCapabilitySet(catalog.Capabilities)
	if result.Capabilities == nil {
		result.Capabilities = CapabilitySet{}
	}
	if len(catalog.Models) > 0 || catalog.SessionReady {
		result.Models, result.ReasoningOptions = acpCatalogOptions(catalog)
		result.DetectedDefaultModel = catalog.DefaultModel
		result.DetectedDefaultReasoning = catalog.DefaultReasoning
		result.Capabilities[CapabilityDynamicModels] = len(catalog.Models) > 0
	}
	authMissing := catalogErr != nil && ACPFailureCode(catalogErr) == FailureCodeAuthRequired
	if catalog.SessionReady || authMissing {
		// 已解析的 CLI 返回鉴权错误时保留可选择性，由执行阶段报告具体鉴权失败。
		result.State = DetectionStateReady
	}
	if authMissing {
		result.Auth = AuthStatusMissing
	}
	if catalogErr != nil {
		level := DiagnosticLevelWarning
		if !catalog.AgentConnected || !catalog.SessionReady {
			level = DiagnosticLevelError
		}
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code:    DiagnosticCodeACPOptionsUnavailable,
			Level:   level,
			Message: FailureDisplayMessage(catalogErr),
		})
	}
	return result
}

const acpManagementCloseTimeout = 5 * time.Second

// acpManager 是目录服务使用的无模型输入管理端口。
type acpManager interface {
	DetectOptions(context.Context, acpOptionsRequest) (acpOptionCatalog, error)
	ForkSession(context.Context, preparedRuntime, ForkRequest) (ForkResult, error)
	Close(context.Context) error
	ReleaseResources(context.Context, func(context.Context) error) error
}

// acpOptionsRequest 提供账号和已冻结的启动事实。
type acpOptionsRequest struct {
	ReleaseResources   func(context.Context) error // ReleaseResources 在管理使用者确认退出后释放宿主探测依赖。
	UseNativeAuth      bool                        // UseNativeAuth 明确使用系统账号的原生认证文件。
	RefreshCredentials CredentialRefresh           // RefreshCredentials 指向账号刷新权威。
	Model              string                      // Model 是需要实际确认的原生模型。
	RuntimeID          string                      // RuntimeID 是产品选择的基础或本地配置身份。
	Prepared           preparedRuntime             // Prepared 是已有安装和环境事实。
	WorkingDirectory   string                      // WorkingDirectory 是宿主拥有的临时工作目录。
}

// acpOptionCatalog 是原始协议目录经过产品规则解释后的结果。
type acpOptionCatalog struct {
	AgentConnected   bool              // AgentConnected 表示标准握手成功。
	SessionReady     bool              // SessionReady 表示临时会话创建成功。
	Version          string            // Version 是原生CLI版本。
	Capabilities     CapabilitySet     // Capabilities 是本次握手确认的产品能力。
	Models           []ModelOption     // Models 是可选择的模型目录。
	ReasoningOptions []ReasoningOption // ReasoningOptions 是当前模型的强度目录。
	DefaultModel     string            // DefaultModel 是最初或显式选择的模型。
	DefaultReasoning string            // DefaultReasoning 是该模型当前强度。
	PermissionLevels []PermissionLevel // PermissionLevels 是产品权限映射。
	Modes            []acp.SessionMode // Modes 是原始模式目录。
}

// acpBackend 只装配短生命周期管理操作，不实现协议或厂商进程。
type acpBackend struct {
	mutex     sync.Mutex                                                                      // mutex 保护关闭门禁、测试工厂和仍持有依赖的管理所有者。
	bindings  map[string]acpRuntimeBinding                                                    // bindings 是冻结的产品目录解释规则。
	openAgent func(context.Context, *native.LaunchLease, acpAdapterConfig) (acp.Agent, error) // openAgent 在统一启动所有者上构造原生消费者。
	closing   bool                                                                            // closing 阻止应用关闭后继续接纳。
	owned     map[*managementOwner]struct{}                                                   // owned 保留未确认关闭的原生使用者。
	pending   int                                                                             // pending 统计尚未返回技术所有者的准备操作。
	changed   chan struct{}                                                                   // changed 在所有者或准备计数变化时唤醒关闭。

}

// managementOwner 保证原生使用者退出后才释放刷新入口与账号配置。
type managementOwner struct {
	// releaseResources 保存尚未交给统一启动所有者的宿主账号和目录。
	releaseResources func(context.Context) error
	// resources 是原生使用者退出后才可关闭的统一启动所有者。
	resources *native.LaunchLease
	// manager 是原生与协议连接的唯一技术所有者。
	manager *native.Management
	// closeGate 串行化单次关闭，允许并发关闭等待被取消。
	closeGate chan struct{}
}

func newACPBackend() *acpBackend {
	AdapterLogger(context.Background())
	return &acpBackend{openAgent: func(ctx context.Context, prepared *native.LaunchLease, _ acpAdapterConfig) (acp.Agent, error) {
		return prepared.New(ctx, nil)
	}, bindings: defaultACPRuntimeBindings(), owned: make(map[*managementOwner]struct{}), changed: make(chan struct{})}
}
func (backend *acpBackend) binding(id string) (acpRuntimeBinding, error) {
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	binding, ok := backend.bindings[id]
	if backend.closing || !ok {
		return acpRuntimeBinding{}, status_error.RuntimeUnavailable
	}
	return binding, nil
}
func (backend *acpBackend) Close(ctx context.Context) error {
	backend.mutex.Lock()
	backend.closing = true
	backend.mutex.Unlock()
	for {
		backend.mutex.Lock()
		owners := make([]*managementOwner, 0, len(backend.owned))
		for owner := range backend.owned {
			owners = append(owners, owner)
		}
		backend.mutex.Unlock()
		var result error
		for _, owner := range owners {
			result = errors.Join(result, backend.closeOwner(ctx, owner))
		}
		if result != nil {
			return result
		}
		backend.mutex.Lock()
		if backend.pending == 0 && len(backend.owned) == 0 {
			backend.mutex.Unlock()
			return nil
		}
		changed := backend.changed
		backend.mutex.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return errors.Join(status_error.RuntimeShutdownFailed, ctx.Err())
		}
	}
}
func (backend *acpBackend) closeOwner(ctx context.Context, owner *managementOwner) error {
	select {
	case owner.closeGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-owner.closeGate }()
	if owner.manager != nil {
		if err := owner.manager.Close(ctx); err != nil {
			return err
		}
		owner.manager = nil
	}
	if owner.resources != nil {
		if err := owner.resources.Close(ctx); err != nil {
			return err
		}
		owner.resources = nil
	}
	if owner.releaseResources != nil {
		if err := owner.releaseResources(ctx); err != nil {
			return err
		}
		owner.releaseResources = nil
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	delete(backend.owned, owner)
	close(backend.changed)
	backend.changed = make(chan struct{})
	return nil
}

// ReleaseResources 保留尚未启动消费者也未能成功释放的租约，应用关闭时可重试。
func (backend *acpBackend) ReleaseResources(ctx context.Context, release func(context.Context) error) error {
	if release == nil {
		return nil
	}
	owner := &managementOwner{releaseResources: release, closeGate: make(chan struct{}, 1)}
	backend.mutex.Lock()
	backend.owned[owner] = struct{}{}
	backend.mutex.Unlock()
	return backend.closeOwner(ctx, owner)
}
func (backend *acpBackend) open(ctx context.Context, config acpAdapterConfig, binding acpRuntimeBinding, releaseResources func(context.Context) error) (*managementOwner, error) {
	owner := &managementOwner{releaseResources: releaseResources, closeGate: make(chan struct{}, 1)}
	backend.mutex.Lock()
	if backend.closing {
		backend.mutex.Unlock()
		return nil, status_error.RuntimeUnavailable
	}
	backend.pending++
	backend.mutex.Unlock()
	defer func() {
		backend.mutex.Lock()
		backend.pending--
		backend.owned[owner] = struct{}{}
		close(backend.changed)
		backend.changed = make(chan struct{})
		backend.mutex.Unlock()
	}()

	var err error
	owner.resources, err = native.PrepareLaunch(ctx, native.LaunchConfig{Native: native.AgentConfig{RuntimeID: config.BaseRuntimeID, Executable: config.Executable, PrefixArgs: config.PrefixArgs, Environment: config.Environment, WorkingDirectory: config.WorkingDirectory, StateRootDirectory: config.StateRootDirectory, PermissionMode: config.PermissionMode, Logger: AdapterLogger(ctx), CodexConfiguration: config.CodexPluginConfiguration, ClaudeSettingsPath: config.ClaudeSettingsPath, NativePluginDirectories: config.NativePluginDirectories, DefaultModeRequestUserInput: constants.RuntimeCodexDefaultModeRequestUserInput}, UseNativeAuth: config.UseNativeAuth, Refresh: config.RefreshCredentials, Release: releaseResources})
	if owner.resources != nil {
		owner.releaseResources = nil
	}
	if err != nil {
		return owner, err
	}
	owner.manager, err = native.OpenManagement(ctx, func(ctx context.Context) (acp.Agent, error) { return backend.openAgent(ctx, owner.resources, config) }, native.ManagementConfig{CleanupTimeout: acpManagementCloseTimeout})
	return owner, err
}
func (backend *acpBackend) finish(ctx context.Context, owner *managementOwner) error {
	if owner == nil {
		return nil
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), acpManagementCloseTimeout)
	defer cancel()
	err := backend.closeOwner(cleanup, owner)
	if err != nil {
		AdapterLogger(ctx).Error("close ACP management resources failed", "error", err)
	}
	return err
}
func (backend *acpBackend) DetectOptions(ctx context.Context, request acpOptionsRequest) (catalog acpOptionCatalog, err error) {
	release := request.ReleaseResources
	defer func() {
		if release != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), acpManagementCloseTimeout)
			defer cancel()
			if closeErr := backend.ReleaseResources(cleanup, release); err == nil {
				err = closeErr
			}
		}
	}()
	binding, err := backend.binding(request.Prepared.definition.baseRuntimeID)
	if err != nil {
		return catalog, err
	}
	config := acpAdapterConfigForManagement(request.Prepared, RuntimeTarget{RuntimeID: request.RuntimeID, RefreshCredentials: request.RefreshCredentials}, native.WorkspaceScope{Root: request.WorkingDirectory}, false, constants.PermissionLevelDefault, binding)
	config.DetectionID = uuid.NewString()
	config.UseNativeAuth = request.UseNativeAuth
	config.StateRootDirectory = request.WorkingDirectory
	ctx = acpAgentLogContext(ctx, config, binding)
	defer func() {
		if err != nil && !errors.Is(ctx.Err(), context.Canceled) {
			AdapterLogger(ctx).Warn("ACP option detection failed", "error", FailureDisplayMessage(err))
		}
	}()
	owner, err := backend.open(ctx, config, binding, release)
	if owner != nil {
		release = nil
	}
	defer func() {
		if closeErr := backend.finish(ctx, owner); err == nil {
			err = closeErr
		}
	}()
	if err != nil {
		return catalog, err
	}
	var skipModels []string
	if binding.descriptor.ID == RuntimeIDWorkBuddy {
		skipModels = []string{constants.RuntimeWorkBuddyFastModelID, constants.RuntimeWorkBuddyBalancedModelID, constants.RuntimeWorkBuddyDeepModelID}
	}
	raw, err := owner.manager.Probe(ctx, native.ProbeRequest{Workspace: native.WorkspaceScope{Root: request.WorkingDirectory}, Model: request.Model, ModelConfigID: binding.descriptor.ModelConfigID, InspectModelOptions: binding.descriptor.InspectModelOptions, SkipModelOptions: skipModels, AllowEmptyModelCatalog: binding.descriptor.AllowEmptyModelCatalog})
	catalog = acpInitializeCatalog(binding, raw.Initialize)
	catalog.SessionReady = raw.SessionReady
	if raw.Modes != nil {
		catalog.Modes = append([]acp.SessionMode(nil), raw.Modes.AvailableModes...)
	}
	if !raw.SessionReady {
		return catalog, err
	}
	decoder := catalogDecoder{binding: binding, models: map[string][]acp.SessionConfigOption{}}
	for _, model := range raw.Models {
		if model.ConfigOptions != nil {
			decoder.models[model.Model] = model.ConfigOptions
		}
	}
	options, decodeErr := decoder.detectSessionOptions(ctx, "", raw.ConfigOptions, catalog.Modes)
	catalog.Models, catalog.ReasoningOptions = options.Models, options.ReasoningOptions
	catalog.DefaultModel, catalog.DefaultReasoning = options.DefaultModel, options.DefaultReasoning
	catalog.PermissionLevels = options.PermissionLevels
	if err == nil {
		err = decodeErr
	}
	return catalog, err
}

// acpAdapterConfig 只保存本次管理连接已冻结的宿主启动事实。
type acpAdapterConfig struct {
	// RefreshCredentials 指向共享账号凭据权威。
	RefreshCredentials CredentialRefresh
	// UseNativeAuth 明确本次使用系统账号原生认证文件。
	UseNativeAuth bool
	// BaseRuntimeID 是公开原生工厂的基础身份。
	BaseRuntimeID string
	// RuntimeID 是基础智能体或本地配置身份。
	RuntimeID string
	// DetectionID 关联本次能力探测。
	DetectionID string
	// Executable 是已经解析的原生程序。
	Executable string
	// PrefixArgs 是 Profile 的受控前置参数。
	PrefixArgs []string
	// NativePluginDirectories 是固定的原生插件目录。
	NativePluginDirectories []string
	// CodexPluginConfiguration 是本次进程的原生点路径覆盖。
	CodexPluginConfiguration map[string]string
	// ClaudeSettingsPath 是本次进程的受管设置文件。
	ClaudeSettingsPath string
	// Environment 是本次进程的完整环境。
	Environment []string
	// WorkingDirectory 是管理会话的工作目录。
	WorkingDirectory string
	// StateRootDirectory 是宿主拥有的持久原生状态根。
	StateRootDirectory string
	// PermissionMode 是产品已经授权的原生权限语义。
	PermissionMode string
}

// acpRuntimeBinding 仅保留技术声明副本与产品模型解释，不拥有原生工厂。
type acpRuntimeBinding struct {
	// descriptor 来自独立运行时的唯一技术注册表。
	descriptor native.Descriptor
	// modelDisplayName 根据目录事实补充产品展示名称。
	modelDisplayName func(acp.SessionConfigSelectOption, string) string
	// catalogOptions 过滤产品可选的模型与推理目录。
	catalogOptions func(acpOptionCatalog) acpOptionCatalog
}

// defaultACPRuntimeBindings 从技术注册表投影产品目录规则。
func defaultACPRuntimeBindings() map[string]acpRuntimeBinding {
	bindings := make(map[string]acpRuntimeBinding)
	for _, descriptor := range native.Descriptors() {
		binding := acpRuntimeBinding{descriptor: descriptor}
		if descriptor.ID == RuntimeIDWorkBuddy {
			binding.modelDisplayName, binding.catalogOptions = workBuddyModelDisplayName, workBuddyCatalogOptions
		}
		bindings[descriptor.ID] = binding
	}
	return bindings
}

// acpAdapterConfigForManagement 只冻结账号、工作范围和产品已经授权的权限。
func acpAdapterConfigForManagement(prepared preparedRuntime, target RuntimeTarget, workspace native.WorkspaceScope, managed bool, permission constants.PermissionLevel, binding acpRuntimeBinding) acpAdapterConfig {
	config := acpAdapterConfig{
		RefreshCredentials: target.RefreshCredentials, UseNativeAuth: !target.Isolated,
		BaseRuntimeID: binding.descriptor.ID, RuntimeID: target.RuntimeID, Executable: prepared.detection.ResolvedBinary,
		PrefixArgs: cloneStrings(prepared.definition.prefixArgs), Environment: prepared.environment.list(), WorkingDirectory: workspace.Root,
		PermissionMode: string(permission), NativePluginDirectories: cloneStrings(target.NativePluginDirectories),
		CodexPluginConfiguration: maps.Clone(target.CodexPluginConfiguration), ClaudeSettingsPath: target.ClaudeSettingsPath,
	}
	if managed {
		config.StateRootDirectory = filepath.Dir(workspace.Root)
	}
	return config
}

// acpAvailableModeIDs 合并标准模式目录与 mode 类配置中的原生标识。
func acpAvailableModeIDs(options []acp.SessionConfigOption, modes []acp.SessionMode) map[string]struct{} {
	available := make(map[string]struct{})
	for _, mode := range modes {
		available[string(mode.Id)] = struct{}{}
	}
	for _, option := range options {
		if option.Select == nil || option.Select.Category == nil || *option.Select.Category != acp.SessionConfigOptionCategoryMode {
			continue
		}
		for _, value := range acpSelectValues(option.Select.Options) {
			available[string(value.Value)] = struct{}{}
		}
	}
	return available
}

// permissionLevels 把技术模式语义映射为产品权限，不另存厂商原生标识。
func (binding acpRuntimeBinding) permissionLevels(options []acp.SessionConfigOption, modes []acp.SessionMode) []PermissionLevel {
	ids := binding.descriptor.Modes
	all := []PermissionLevel{PermissionLevelDefault, PermissionLevelAuto, PermissionLevelFullAccess}
	if binding.descriptor.ManagedPermissions || len(ids.Default)+len(ids.Automatic)+len(ids.Unrestricted) == 0 {
		return all
	}
	available := acpAvailableModeIDs(options, modes)
	if ids.SingleModeFallback != "" && len(modes) == 1 && string(modes[0].Id) == ids.SingleModeFallback && !hasAnyMode(available, append(append(cloneStrings(ids.Automatic), ids.Unrestricted...), ids.ReadOnly...)...) {
		return all
	}
	result := []PermissionLevel{}
	for index, candidates := range [][]string{ids.Default, ids.Automatic, ids.Unrestricted} {
		if hasAnyMode(available, candidates...) {
			result = append(result, all[index])
		}
	}
	return result
}

// workBuddyCatalogOptions 只开放具体模型及其思考档位，不开放自动路由入口与思考开关。
func workBuddyCatalogOptions(catalog acpOptionCatalog) acpOptionCatalog {
	models := make([]ModelOption, 0, len(catalog.Models))
	currentModelVisible := false
	for _, model := range catalog.Models {
		switch model.ID {
		case constants.RuntimeWorkBuddyFastModelID, constants.RuntimeWorkBuddyBalancedModelID, constants.RuntimeWorkBuddyDeepModelID:
			continue
		}
		model.ReasoningOptions = workBuddyReasoningOptions(model.ReasoningOptions)
		models = append(models, model)
		currentModelVisible = currentModelVisible || model.ID == catalog.DefaultModel
	}
	catalog.Models = models
	// 强度仅绑定到已探测的模型，避免无具体档位的模型借用全局目录。
	catalog.ReasoningOptions = nil
	if !currentModelVisible {
		// 原生默认模型被隐藏时，不能把它的思考目录套到另一个具体模型上。
		catalog.DefaultModel, catalog.DefaultReasoning = "", ""
		catalog.ReasoningOptions = nil
	}
	return catalog
}

// workBuddyReasoningOptions 保留上游具体强度的标识、名称和顺序，不将思考开关作为强度。
func workBuddyReasoningOptions(options []ReasoningOption) []ReasoningOption {
	if options == nil {
		return nil
	}
	result := make([]ReasoningOption, 0, len(options))
	for _, option := range options {
		if option.ID != constants.RuntimeWorkBuddyThinkingEnabled && option.ID != constants.RuntimeWorkBuddyThinkingDisabled {
			result = append(result, option)
		}
	}
	return result
}

// workBuddyModelDisplayName 仅将 ACP 明确声明零积分的模型标为免费，不推测限时优惠。
func workBuddyModelDisplayName(option acp.SessionConfigSelectOption, name string) string {
	if option.Description == nil || strings.TrimSpace(*option.Description) != constants.RuntimeWorkBuddyZeroCreditDescription ||
		strings.HasSuffix(name, constants.RuntimeModelFreeNameSuffix) {
		return name
	}
	return name + constants.RuntimeModelFreeNameSuffix
}

// acpCatalogOptions 返回 ACP 探测得到的模型与推理目录副本，不再注入 Default 占位项。
func acpCatalogOptions(catalog acpOptionCatalog) ([]ModelOption, []ReasoningOption) {
	return cloneModels(catalog.Models), cloneReasoningOptions(catalog.ReasoningOptions)
}

// acpInitializeCatalog 消费 ACP initialize 与通用 Runtime 元数据，并补充 Binding 声明的 Skill 能力。
func acpInitializeCatalog(
	binding acpRuntimeBinding,
	response acp.InitializeResponse,
) acpOptionCatalog {
	capabilities := response.AgentCapabilities
	result := CapabilitySet{
		CapabilityPartialMessages: true,
	}
	if capabilities.LoadSession || capabilities.SessionCapabilities.Resume != nil {
		result[CapabilityResumeSession] = true
	}
	if capabilities.SessionCapabilities.AdditionalDirectories != nil {
		result[CapabilityAddDirectory] = true
	}
	if capabilities.PromptCapabilities.Image {
		result[CapabilityImagePath] = true
	}
	if binding.descriptor.SkillDirectory {
		result[CapabilitySkillDirectory] = true
	}
	if capabilities.McpCapabilities.Http {
		result[CapabilityMCPStreamableHTTP] = true
	}
	if capabilities.McpCapabilities.Sse {
		result[CapabilityMCPSSE] = true
	}
	if acpSteeringSupported(response.Meta) {
		result[CapabilityStreamingUserMessages] = true
	}
	version := ""
	if response.AgentInfo != nil {
		version = acpmeta.RuntimeVersion(response.AgentInfo.Meta)
		if version == "" && binding.descriptor.NativeProcess && !binding.descriptor.NativeVersionMetadataOnly {
			version = response.AgentInfo.Version
		}
	}
	return acpOptionCatalog{
		AgentConnected: true,
		Version:        version,
		Capabilities:   result,
	}
}

// acpSteeringSupported 读取 upstream Adapter 通过 ACP 扩展元数据声明的 steering 能力。
func acpSteeringSupported(meta map[string]any) bool {
	steering, ok := meta[constants.RuntimeACPMetaKeySteering].(map[string]any)
	if !ok {
		return false
	}
	supported, _ := steering[constants.RuntimeACPMetaKeySupported].(bool)
	return supported
}

// detectSessionOptions 优先读取模型专属目录；非原生适配器通过切换模型取得配置。
func (agent *catalogDecoder) detectSessionOptions(
	ctx context.Context,
	sessionID acp.SessionId,
	initialOptions []acp.SessionConfigOption,
	modes []acp.SessionMode,
) (acpOptionCatalog, error) {
	modelConfig, ok := acpSelectConfig(initialOptions, agent.binding.descriptor.ModelConfigID, acp.SessionConfigOptionCategoryModel)
	if !ok {
		if agent.binding.descriptor.NativeProcess {
			return acpOptionCatalog{PermissionLevels: agent.binding.permissionLevels(initialOptions, modes)}, nil
		}
		return acpOptionCatalog{}, status_error.RuntimeUnavailable
	}
	modelOptions := acpSelectValues(modelConfig.Options)
	if len(modelOptions) == 0 {
		return acpOptionCatalog{}, status_error.RuntimeUnavailable
	}

	catalog := acpOptionCatalog{
		Models:           make([]ModelOption, 0, len(modelOptions)),
		PermissionLevels: agent.binding.permissionLevels(initialOptions, modes),
		ReasoningOptions: acpReasoningOptions(
			initialOptions,
			agent.binding.descriptor.ReasoningConfigID,
		),
		DefaultModel:     strings.TrimSpace(string(modelConfig.CurrentValue)),
		DefaultReasoning: detectedDefaultReasoning(initialOptions, agent.binding.descriptor.ReasoningConfigID, modelConfig),
	}
	seen := make(map[string]struct{}, len(modelOptions))
	var detectionErr error
	for _, option := range modelOptions {
		modelID := strings.TrimSpace(string(option.Value))
		if modelID == "" {
			continue
		}
		if _, exists := seen[modelID]; exists {
			continue
		}
		seen[modelID] = struct{}{}

		configOptions := initialOptions
		if values, ok := agent.models[modelID]; ok {
			configOptions = values
		}

		name := strings.TrimSpace(option.Name)
		if name == "" {
			name = modelID
		}
		if agent.binding.modelDisplayName != nil {
			name = agent.binding.modelDisplayName(option, name)
		}
		reasoning := acpReasoningOptions(configOptions, agent.binding.descriptor.ReasoningConfigID)
		if modelOptions, known := acpmeta.ModelConfigOptions(option.Meta); known {
			reasoning = acpReasoningOptions(modelOptions, agent.binding.descriptor.ReasoningConfigID)
		} else if _, known := agent.models[modelID]; !known && option.Value != modelConfig.CurrentValue {
			// 未提供模型专属目录时，不把当前模型的强度套用于其他模型。
			reasoning = nil
		}
		catalog.Models = append(catalog.Models, ModelOption{
			ID:               modelID,
			Name:             name,
			ReasoningOptions: reasoning,
			PermissionLevels: agent.binding.permissionLevels(configOptions, modes),
		})
	}
	if agent.binding.catalogOptions != nil {
		catalog = agent.binding.catalogOptions(catalog)
	}
	if len(catalog.Models) == 0 {
		return catalog, status_error.RuntimeUnavailable
	}
	return catalog, detectionErr
}

// detectedDefaultReasoning 读取探测 Session 当前模型生效的推理强度。
func detectedDefaultReasoning(
	options []acp.SessionConfigOption,
	reasoningConfigID acp.SessionConfigId,
	modelConfig *acp.SessionConfigOptionSelect,
) string {
	if config, ok := acpSelectConfig(options, reasoningConfigID, acp.SessionConfigOptionCategoryThoughtLevel); ok {
		if value := strings.TrimSpace(string(config.CurrentValue)); value != "" {
			return value
		}
	}
	for _, option := range acpSelectValues(modelConfig.Options) {
		if option.Value != modelConfig.CurrentValue {
			continue
		}
		if configs, known := acpmeta.ModelConfigOptions(option.Meta); known {
			if config, ok := acpSelectConfig(configs, reasoningConfigID, acp.SessionConfigOptionCategoryThoughtLevel); ok {
				return strings.TrimSpace(string(config.CurrentValue))
			}
		}
		break
	}
	return ""
}

func hasAnyMode(available map[string]struct{}, ids ...string) bool {
	for _, id := range ids {
		if _, exists := available[id]; exists {
			return true
		}
	}
	return false
}

// acpReasoningOptions 读取当前 Adapter 推理配置中的平铺选项。
func acpReasoningOptions(
	options []acp.SessionConfigOption,
	configID acp.SessionConfigId,
) []ReasoningOption {
	config, ok := acpSelectConfig(options, configID, acp.SessionConfigOptionCategoryThoughtLevel)
	if !ok {
		return nil
	}
	values := acpSelectValues(config.Options)
	result := make([]ReasoningOption, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, option := range values {
		id := strings.TrimSpace(string(option.Value))
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		name := strings.TrimSpace(option.Name)
		if name == "" {
			name = id
		}
		result = append(result, ReasoningOption{ID: id, Name: name})
	}
	return result
}

// acpSelectConfig 按 Adapter 声明的稳定配置标识查找 select 配置。
func acpSelectConfig(
	options []acp.SessionConfigOption,
	id acp.SessionConfigId,
	category acp.SessionConfigOptionCategory,
) (*acp.SessionConfigOptionSelect, bool) {
	for _, option := range options {
		if option.Select != nil && option.Select.Category != nil && *option.Select.Category == category {
			return option.Select, true
		}
	}
	for _, option := range options {
		if option.Select != nil && option.Select.Id == id && option.Select.Category == nil {
			return option.Select, true
		}
	}
	return nil, false
}

// acpSelectValues 把 ACP 平铺或分组的 select 值转换为同一有序列表。
func acpSelectValues(
	options acp.SessionConfigSelectOptions,
) []acp.SessionConfigSelectOption {
	if options.Ungrouped != nil {
		return append([]acp.SessionConfigSelectOption(nil), (*options.Ungrouped)...)
	}
	if options.Grouped == nil {
		return nil
	}
	var result []acp.SessionConfigSelectOption
	for _, group := range *options.Grouped {
		result = append(result, group.Options...)
	}
	return result
}

// acpConfigCurrentValue 读取 Agent 确认的配置值，避免将本地选择误当作生效结果。
func acpConfigCurrentValue(options []acp.SessionConfigOption, id acp.SessionConfigId) acp.SessionConfigValueId {
	for _, option := range options {
		if option.Select != nil && option.Select.Id == id {
			return option.Select.CurrentValue
		}
	}
	return ""
}

// catalogDecoder只解释已读取目录，不拥有协议连接。
type catalogDecoder struct {
	// binding 是产品展示和权限映射规则。
	binding acpRuntimeBinding
	// models 是管理连接已确认的模型专属原始配置。
	models map[string][]acp.SessionConfigOption
}

// ForkRequest 只携带原生分叉所需的启动、工作范围和协议资源事实。
type ForkRequest struct {
	// Target 是已经选择的本地启动与账号配置。
	Target RuntimeTarget
	// Detection 是已持久化的有效探测快照。
	Detection DetectionResult
	// Workspace 是目标原生会话的工作范围。
	Workspace native.WorkspaceScope
	// ManagedWorkspace 表示宿主管理目标工作区相邻的持久状态。
	ManagedWorkspace bool
	// PermissionLevel 是产品已经授权的执行权限。
	PermissionLevel constants.PermissionLevel
	// MCPServers 是宿主已校验和序列化的协议资源，不带资源领域对象。
	MCPServers []acp.McpServer
	// SourceSessionID 是来源原生句柄。
	SourceSessionID string
	// Position 是原生不透明回合位置。
	Position string
	// SourceWorkspace 是来源原生会话绑定的目录。
	SourceWorkspace string
	// ReceiptDirectory 是宿主持有的私有持久操作目录。
	ReceiptDirectory string
	// ReconcileOnly 只对账已经派发的原生操作。
	ReconcileOnly bool
}

// ForkResult 保存已经建立的原生分支事实。
type ForkResult struct {
	// SessionID 是新原生句柄。
	SessionID string
	// Mode 是原生声明的实际分叉精度。
	Mode string
}

// ForkSession 只创建原生分支，不发送模型 Prompt，也不编译复制的历史。
func (sdk *Catalog) ForkSession(ctx context.Context, request ForkRequest) (result ForkResult, err error) {
	defer func() { err = normalizeForkError(request, err) }()
	if ctx == nil || request.SourceSessionID == "" {
		return ForkResult{}, status_error.BadRequest
	}
	if !constants.SelectablePermissionLevel(request.PermissionLevel) {
		return ForkResult{}, status_error.BadRequest
	}
	prepared, err := sdk.prepareFromDetection(sdk.prepareRequestForTarget(request.Target), request.Detection)
	if err != nil {
		return ForkResult{}, err
	}
	if sdk.acp == nil {
		return ForkResult{}, status_error.RuntimeUnavailable
	}
	return sdk.acp.ForkSession(ctx, prepared, request)
}

// ForkSession 把已冻结宿主参数交给独立管理连接，不提交模型输入。
func (backend *acpBackend) ForkSession(ctx context.Context, prepared preparedRuntime, request ForkRequest) (result ForkResult, err error) {
	binding, err := backend.binding(prepared.definition.baseRuntimeID)
	if err != nil {
		return result, err
	}
	source := native.WorkspaceScope{Root: request.SourceWorkspace}
	owner, err := backend.open(ctx, acpAdapterConfigForManagement(prepared, request.Target, source, request.ManagedWorkspace, request.PermissionLevel, binding), binding, nil)
	defer func() {
		if closeErr := backend.finish(ctx, owner); err == nil {
			err = closeErr
		}
	}()
	if err != nil {
		return result, err
	}
	// 产品资源已由宿主校验；这里仅按当前握手的原生传输能力投影。
	capabilities := owner.manager.InitializeResponse().AgentCapabilities.McpCapabilities
	servers := make([]acp.McpServer, 0, len(request.MCPServers))
	for _, server := range request.MCPServers {
		if server.Stdio != nil || server.Http != nil && capabilities.Http || server.Sse != nil && capabilities.Sse {
			servers = append(servers, server)
		}
	}
	raw, err := owner.manager.Fork(ctx, native.ForkRequest{SourceSessionID: request.SourceSessionID, Position: request.Position, SourceWorkspace: request.SourceWorkspace, ReceiptDirectory: request.ReceiptDirectory, ReconcileOnly: request.ReconcileOnly, Workspace: request.Workspace, MCPServers: servers, CloseForkSession: !binding.descriptor.NativeProcess})
	return ForkResult{SessionID: raw.SessionID, Mode: raw.Mode}, err
}

// normalizeForkError 只在 Runtime 外部边界转换原生错误，公开调用者不再重复映射。
func normalizeForkError(request ForkRequest, err error) error {
	if err == nil {
		return nil
	}
	var business statuserror.CommonError
	if errors.As(err, &business) {
		return err
	}
	var fault *native.Fault
	if errors.As(err, &fault) {
		switch fault.Code {
		case native.FaultConfigurationInvalid:
			return status_error.RuntimeConfigurationInvalid
		case native.FaultUnsupported:
			return status_error.RuntimePolicyUnsupported
		}
	}
	logx.Errorf("native fork failed: runtime_id=%s reconcile_only=%t error=%v", request.Target.RuntimeID, request.ReconcileOnly, err)
	code := ACPFailureCode(err)
	if request.ReconcileOnly || errors.Is(err, acpmeta.ErrForkUnconfirmed) || code == FailureCodeContextCanceled || code == constants.RuntimeFailureCodeRequestTimeout {
		return status_error.ChatForkIndeterminate
	}
	switch code {
	case FailureCodeSessionNotFound:
		return status_error.RuntimeSessionNotFound
	case FailureCodeAuthRequired:
		return status_error.RuntimeAuthRequired
	default:
		return status_error.RuntimeExecutionFailed.WithParams(map[string]interface{}{"Detail": FailureDisplayMessage(err)})
	}
}

type mcpPreparation struct {
	// compatible 是经过校验且可以注入当前 Runtime 的连接器。
	compatible []Connector
}

// PrepareForkMCPServers 校验产品连接器并生成无领域身份的原生分叉资源。
func PrepareForkMCPServers(connectors []Connector) ([]acp.McpServer, error) {
	prepared, err := prepareMCPConnectorSet(connectors, func(ConnectorTransport) bool { return true })
	if err != nil {
		return nil, err
	}
	return acpMCPServers(prepared.compatible)
}

// prepareMCPConnectorSet 建立 Runtime 无关的 MCP 连接器事实。
func prepareMCPConnectorSet(
	connectors []Connector,
	supported func(ConnectorTransport) bool,
) (mcpPreparation, error) {
	compatible := make([]Connector, 0, len(connectors))
	seenIDs := make(map[string]struct{}, len(connectors))
	seenRuntimeKeys := make(map[string]struct{}, len(connectors))
	for _, connector := range connectors {
		if strings.TrimSpace(connector.ID) == "" ||
			strings.TrimSpace(connector.Name) == "" ||
			ValidateConnectorConfiguration(connector) != nil {
			return mcpPreparation{}, status_error.BadRequest
		}
		if _, exists := seenIDs[connector.ID]; exists {
			return mcpPreparation{}, status_error.BadRequest
		}
		seenIDs[connector.ID] = struct{}{}
		if _, exists := seenRuntimeKeys[connector.RuntimeKey]; exists {
			return mcpPreparation{}, status_error.BadRequest
		}
		seenRuntimeKeys[connector.RuntimeKey] = struct{}{}
		if !supported(connector.Transport) {
			continue
		}
		if err := ValidateConnectorConfiguration(connector); err != nil {
			return mcpPreparation{}, err
		}
		compatible = append(compatible, connector)
	}

	return mcpPreparation{
		compatible: compatible,
	}, nil
}

// acpMCPServers 转换经过兼容性校验的 Connector。
func acpMCPServers(
	connectors []Connector,
) ([]acp.McpServer, error) {
	servers := make([]acp.McpServer, 0, len(connectors))
	for _, connector := range connectors {
		switch connector.Transport {
		case ConnectorTransportStdio:
			environmentKeys := make([]string, 0, len(connector.Environment))
			for key := range connector.Environment {
				environmentKeys = append(environmentKeys, key)
			}
			sort.Strings(environmentKeys)
			environment := make([]acp.EnvVariable, 0, len(environmentKeys))
			for _, key := range environmentKeys {
				environment = append(environment, acp.EnvVariable{
					Name:  key,
					Value: connector.Environment[key],
				})
			}
			servers = append(servers, acp.McpServer{
				Stdio: &acp.McpServerStdio{
					Name:    connector.RuntimeKey,
					Command: connector.Command,
					Args:    append([]string{}, connector.Arguments...),
					Env:     environment,
				},
			})
		case ConnectorTransportStreamableHTTP:
			servers = append(servers, acp.McpServer{
				Http: &acp.McpServerHttpInline{
					Name:    connector.RuntimeKey,
					Url:     connector.URL,
					Headers: acpHTTPHeaders(connector.Headers),
				},
			})
		case ConnectorTransportSSE:
			servers = append(servers, acp.McpServer{
				Sse: &acp.McpServerSseInline{
					Name:    connector.RuntimeKey,
					Url:     connector.URL,
					Headers: acpHTTPHeaders(connector.Headers),
				},
			})
		default:
			return nil, status_error.SkillUnavailable
		}
	}
	return servers, nil
}

// acpHTTPHeaders 把 Connector header 转为顺序稳定的 ACP 列表。
func acpHTTPHeaders(values map[string]string) []acp.HttpHeader {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	headers := make([]acp.HttpHeader, 0, len(keys))
	for _, key := range keys {
		headers = append(headers, acp.HttpHeader{Name: key, Value: values[key]})
	}
	return headers
}

// NewForkHandler 将产品分叉请求转换为独立原生管理连接所需的窄协议事实。
func NewForkHandler(catalog *Catalog) func(context.Context, ForkSessionRequest) (ForkSessionResult, error) {
	return func(ctx context.Context, request ForkSessionRequest) (ForkSessionResult, error) {
		if err := ValidateExecutionPolicy(request.Plan.Policy.Execution); err != nil {
			return ForkSessionResult{}, err
		}
		workspace, err := Validate(request.Plan.Workspace)
		if err != nil {
			return ForkSessionResult{}, err
		}
		connectors := make([]Connector, 0, len(request.Plan.Connectors))
		for _, declaration := range request.Plan.Connectors {
			connectors = append(connectors, Connector{ID: declaration.ID, Name: declaration.Name, RuntimeKey: declaration.RuntimeKey,
				Transport: declaration.Transport, Command: declaration.Command, Arguments: declaration.Arguments, URL: declaration.URL, TimeoutMS: declaration.TimeoutMS})
		}
		servers, err := PrepareForkMCPServers(connectors)
		if err != nil {
			return ForkSessionResult{}, err
		}
		result, err := catalog.ForkSession(ctx, ForkRequest{
			Target: request.Plan.Target.LaunchTarget(), Detection: request.Plan.Detection,
			Workspace:        native.WorkspaceScope{Root: workspace.Path, Directories: workspace.AdditionalWritableDirectories},
			ManagedWorkspace: workspace.Managed, PermissionLevel: request.Plan.Policy.Execution.PermissionLevel, MCPServers: servers,
			SourceSessionID: request.SourceSessionID, Position: request.Position, SourceWorkspace: request.SourceWorkspace,
			ReceiptDirectory: request.ReceiptDirectory, ReconcileOnly: request.ReconcileOnly,
		})
		if err != nil {
			return ForkSessionResult{}, err
		}
		return ForkSessionResult{SessionID: result.SessionID, Mode: result.Mode}, nil
	}

}
