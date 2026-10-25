package runtimes

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

type environmentSnapshot struct {
	// values 保存规范化键对应的最终环境值。
	values map[string]string
	// names 保存规范化键对应的最终输出键名。
	names map[string]string
	// fingerprint 是完整环境内容的不可逆摘要。
	fingerprint string
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
	snapshot.fingerprint = fingerprintEnvironment(snapshot)
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
	snapshot.fingerprint = fingerprintEnvironment(*snapshot)
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

func fingerprintEnvironment(snapshot environmentSnapshot) string {
	keys := make([]string, 0, len(snapshot.values))
	for key := range snapshot.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	digest := sha256.New()
	for _, key := range keys {
		_, _ = digest.Write([]byte(key))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write([]byte(snapshot.values[key]))
		_, _ = digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}
