package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"golang.org/x/sync/semaphore"
)

type hostPathSnapshot struct {
	// home identifies the host user whose shell or registry supplied the paths.
	home string
	// inherited is the original host PATH before platform discovery.
	inherited string
	// directories holds the immutable, platform-discovered paths.
	directories []string
}

var (
	hostExecutablePath        atomic.Pointer[hostPathSnapshot]
	hostExecutablePathRefresh = semaphore.NewWeighted(1)
)

// RefreshHostExecutablePath reads the shell or registry during runtime discovery.
// Native launches use the last completed snapshot without starting a probe.
func (*Catalog) RefreshHostExecutablePath(ctx context.Context) {
	if err := hostExecutablePathRefresh.Acquire(ctx, 1); err != nil {
		return
	}
	defer hostExecutablePathRefresh.Release(1)

	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return
	}
	environment, err := buildEnvironment(accountconfig.Environment(os.Environ()), nil)
	if err != nil {
		return
	}
	paths, ok := readHostExecutablePath(ctx, home, environment)
	if ok && ctx.Err() == nil {
		hostExecutablePath.Store(&hostPathSnapshot{home: home, inherited: environment.get("PATH"), directories: absolutePathEntries(paths)})
	}
}

// PrepareNativeHostEnvironment augments PATH for a known host home after stripping inherited provider credentials.
// Callers apply their selected account and resource overrides after this step.
func PrepareNativeHostEnvironment(base []string) ([]string, error) {
	environment, err := buildEnvironment(accountconfig.Environment(base), nil)
	if err != nil {
		return nil, err
	}
	home := environment.get("HOME")
	if runtime.GOOS == "windows" {
		home = environment.get("USERPROFILE")
	}
	// A caller without a host home supplied an isolated process environment.
	if !filepath.IsAbs(home) {
		return environment.list(), nil
	}
	if err := supplementExecutablePath(&environment, home); err != nil {
		return nil, err
	}
	return environment.list(), nil
}

func hostPathGroups(home, inherited string) [][]string {
	base := filepath.SplitList(inherited)
	discovered := hostExecutablePath.Load()
	if discovered == nil || inherited != discovered.inherited || normalizedPathForComparison(home) != normalizedPathForComparison(discovered.home) {
		return [][]string{base}
	}
	if runtime.GOOS == "windows" {
		// Append discovered Windows paths after the inherited search order.
		return [][]string{base, discovered.directories}
	}
	// OpenBot gives the login shell's active tool versions first choice.
	return [][]string{discovered.directories, base}
}

func mergeExecutableDirectories(groups ...[]string) string {
	var result []string
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, directory := range group {
			if directory == "" {
				continue
			}
			key := normalizedPathForComparison(directory)
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				if runtime.GOOS == "windows" && strings.ContainsRune(directory, filepath.ListSeparator) {
					directory = `"` + directory + `"`
				}
				result = append(result, directory)
			}
		}
	}
	return strings.Join(result, string(filepath.ListSeparator))
}

// Only inherited or explicitly selected PATH may name relative directories.
func absolutePathEntries(paths []string) []string {
	invalid := filepath.ListSeparator
	if runtime.GOOS == "windows" {
		invalid = '"'
	}
	var absolute []string
	for _, directory := range paths {
		if runtime.GOOS == "windows" {
			directory = strings.TrimSpace(directory)
		}
		if filepath.IsAbs(directory) && !strings.ContainsRune(directory, 0) && !strings.ContainsRune(directory, invalid) {
			absolute = append(absolute, directory)
		}
	}
	return absolute
}
