package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectFile 是映射到 Chat 冻结项目入口的一份文件。
type ProjectFile struct {
	// Name 是文件的展示名称。
	Name string
	// Path 是相对于 Chat 执行工作区的稳定路径。
	Path string
}

type resolvedProjectLink struct {
	// entryName 是工作区 projects/ 下的稳定入口名。
	entryName string
	// sourcePath 是经过软链接解析的真实项目根目录。
	sourcePath string
}

// ResolveProjectFiles 校验文件属于冻结工作范围，并映射为 projects/ 路径。
func ResolveProjectFiles(
	projects []ProjectLink,
	sourcePaths []string,
) ([]ProjectFile, error) {
	if len(sourcePaths) == 0 {
		return nil, nil
	}
	resolvedProjects := make([]resolvedProjectLink, 0, len(projects))
	for _, project := range projects {
		if !isSafeDirectoryName(project.EntryName) || strings.TrimSpace(project.SourcePath) == "" {
			return nil, status_error.ChatLogError
		}
		resolved, err := filepath.EvalSymlinks(project.SourcePath)
		if err != nil {
			return nil, status_error.ProjectSourceUnavailable
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			return nil, status_error.ProjectSourceUnavailable
		}
		resolvedProjects = append(resolvedProjects, resolvedProjectLink{
			entryName:  project.EntryName,
			sourcePath: filepath.Clean(resolved),
		})
	}
	// 嵌套来源优先匹配最具体的根目录，避免同一文件映射到外围项目入口。
	sort.SliceStable(resolvedProjects, func(left, right int) bool {
		return len(resolvedProjects[left].sourcePath) > len(resolvedProjects[right].sourcePath)
	})

	files := make([]ProjectFile, 0, len(sourcePaths))
	for _, input := range sourcePaths {
		sourcePath := strings.TrimSpace(input)
		if sourcePath == "" || !filepath.IsAbs(sourcePath) {
			return nil, status_error.BadRequest
		}
		resolved, err := filepath.EvalSymlinks(sourcePath)
		if err != nil {
			return nil, status_error.ProjectSourceUnavailable
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			return nil, status_error.ProjectSourceUnavailable
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() {
			return nil, status_error.BadRequest
		}
		file, err := os.Open(resolved)
		if err != nil {
			return nil, status_error.ProjectSourceUnavailable
		}
		if err := file.Close(); err != nil {
			return nil, status_error.ProjectSourceUnavailable
		}

		matched := false
		for _, project := range resolvedProjects {
			relative, relErr := filepath.Rel(project.sourcePath, resolved)
			if relErr != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
				continue
			}
			files = append(files, ProjectFile{
				Name: filepath.Base(resolved),
				Path: filepath.ToSlash(filepath.Join(
					constants.ExecutionWorkspaceProjectsDirectory,
					project.entryName,
					relative,
				)),
			})
			matched = true
			break
		}
		if !matched {
			return nil, status_error.ProjectSourceUnavailable
		}
	}
	return files, nil
}
