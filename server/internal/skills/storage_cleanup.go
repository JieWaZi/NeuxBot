package skills

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// CleanupTemporaryArtifacts 删除上一个 NeuxBot 进程留下的导入和版本 staging。
func CleanupTemporaryArtifacts(dataDirectory string) error {
	dataDirectory = strings.TrimSpace(dataDirectory)
	if dataDirectory == "" {
		return status_error.SkillStorageError
	}
	skillsRoot := filepath.Join(dataDirectory, "skills")
	if err := os.RemoveAll(filepath.Join(skillsRoot, "tmp")); err != nil {
		return status_error.SkillStorageError
	}
	entries, err := os.ReadDir(skillsRoot)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return status_error.SkillStorageError
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "version-staging-") {
			continue
		}
		if err := os.RemoveAll(filepath.Join(skillsRoot, entry.Name())); err != nil {
			return status_error.SkillStorageError
		}
	}
	return nil
}

// RemoveVersion 删除数据库已经解除引用的一份权威技能版本目录。
func RemoveVersion(dataDirectory string, contentPath string) error {
	root := filepath.Join(dataDirectory, "skills", "versions")
	relative := filepath.Clean(contentPath)
	if strings.TrimSpace(dataDirectory) == "" || filepath.IsAbs(relative) || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return status_error.SkillContentInvalid
	}
	path := filepath.Join(dataDirectory, relative)
	inside, err := filepath.Rel(root, path)
	if err != nil || inside == "." || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return status_error.SkillContentInvalid
	}
	if err := os.RemoveAll(path); err != nil {
		return status_error.SkillStorageError
	}
	packageDirectory := filepath.Dir(path)
	if empty, err := directoryIsEmpty(packageDirectory); err == nil && empty {
		_ = os.Remove(packageDirectory)
	}
	return nil
}

// CleanupUnreferencedVersions 删除数据库没有引用的技能版本和 staging 残留。
func CleanupUnreferencedVersions(dataDirectory string, referencedPaths []string) error {
	dataDirectory = strings.TrimSpace(dataDirectory)
	if dataDirectory == "" {
		return status_error.SkillStorageError
	}
	root := filepath.Join(dataDirectory, "skills", "versions")
	referenced := make(map[string]struct{}, len(referencedPaths))
	for _, contentPath := range referencedPaths {
		relative := filepath.Clean(contentPath)
		if filepath.IsAbs(relative) || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return status_error.SkillContentInvalid
		}
		absolute := filepath.Join(dataDirectory, relative)
		inside, err := filepath.Rel(root, absolute)
		if err != nil || inside == "." || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
			return status_error.SkillContentInvalid
		}
		referenced[filepath.Clean(absolute)] = struct{}{}
	}
	packages, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return status_error.SkillStorageError
	}
	for _, packageEntry := range packages {
		packagePath := filepath.Join(root, packageEntry.Name())
		if !packageEntry.IsDir() || strings.HasPrefix(packageEntry.Name(), ".") {
			if err := os.RemoveAll(packagePath); err != nil {
				return status_error.SkillStorageError
			}
			continue
		}
		versions, err := os.ReadDir(packagePath)
		if err != nil {
			return status_error.SkillStorageError
		}
		for _, versionEntry := range versions {
			versionPath := filepath.Join(packagePath, versionEntry.Name())
			if _, exists := referenced[filepath.Clean(versionPath)]; exists {
				continue
			}
			if err := os.RemoveAll(versionPath); err != nil {
				return status_error.SkillStorageError
			}
		}
		if empty, err := directoryIsEmpty(packagePath); err != nil {
			return err
		} else if empty {
			if err := os.Remove(packagePath); err != nil && !os.IsNotExist(err) {
				return status_error.SkillStorageError
			}
		}
	}
	return nil
}

func directoryIsEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, status_error.SkillStorageError
	}
	return len(entries) == 0, nil
}
