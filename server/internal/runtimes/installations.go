package runtimes

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/acp-go/pkg/nativeacp"
)

// DiscoverInstallations 枚举同一 CLI 的可执行路径，并保留此前保存的失效实例。
func (sdk *SDK) DiscoverInstallations(
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
