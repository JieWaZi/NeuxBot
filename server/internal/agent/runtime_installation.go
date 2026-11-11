package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/acp-go/pkg/agentclient"
)

type environmentSnapshot struct {
	// values maps normalized keys to final environment values.
	values map[string]string
	// names maps normalized keys to final output key names.
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

// definitionExecutableCandidates adds official user-level native programs after PATH commands; explicit paths never fall back to other installations.
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

// DiscoverInstallations enumerates executable paths for a CLI while retaining previously saved unavailable instances.
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
	// Preserve current and historical selections first, then scan newly discovered PATH entries.
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
		// Check each CLI's own user-level installation directories without changing PATH selection order.
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
	// Bound concurrent version commands so multiple PATH installations cannot exhaust subprocess resources.
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

// NormalizeRuntimeVersion normalizes empty and unknown versions to empty while preserving native CLI version formats.
func NormalizeRuntimeVersion(version string) string {
	version = strings.TrimSpace(version)
	if strings.EqualFold(version, "unknown") {
		return ""
	}
	return version
}

// ProbeInstallationVersion reads only the selected executable's version without accounts or model discovery.
func (sdk *Catalog) ProbeInstallationVersion(ctx context.Context, request DetectRequest) (CLIInstallation, error) {
	definition, exists, err := sdk.definitionForRequest(request)
	if err != nil {
		return CLIInstallation{}, err
	}
	if !exists {
		return CLIInstallation{}, status_error.RuntimeNotRegistered
	}
	path, err := sdk.ResolveExecutable(request)
	if err != nil {
		return CLIInstallation{}, err
	}
	environment, err := buildPreparedEnvironment(request.BaseEnvironment, definition.environment, request.HomeDirectory)
	if err != nil {
		return CLIInstallation{}, err
	}
	args := append(append([]string(nil), definition.prefixArgs...), "--version")
	version := agentclient.RuntimeVersion(ctx, path, args, environment.list(), "")
	return CLIInstallation{Path: path, Version: NormalizeRuntimeVersion(version), Available: true}, nil
}

// protectedPrefixArguments prevents leading arguments from overriding native execution settings managed by NeuxBot.

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

// ResolveExecutable resolves only the specified runtime executable without starting ACP or checking accounts.
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
