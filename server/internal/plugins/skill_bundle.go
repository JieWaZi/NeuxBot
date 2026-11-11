package plugins

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/skills"
)

// BuildSkillBundle 从已冻结扫描批次生成标准技能插件，不继承来源根的执行声明。
// 扫描批次由 OpenStagedImport 核验；这里只复制所选技能树与必要许可。
func BuildSkillBundle(parent string, batch skills.StagedBatch, ids []string, pluginID, name, description string) (StagedPackage, map[string]any, error) {
	var empty StagedPackage
	if len(ids) < 2 || len(ids) > 100 || strings.TrimSpace(name) == "" {
		return empty, nil, status_error.BadRequest
	}
	contentRoot := filepath.Join(batch.Root, "content")
	for _, directory := range []string{batch.Root, contentRoot} {
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return empty, nil, status_error.SkillImportIntegrityInvalid
		}
	}
	available := map[string]skills.Package{}
	for _, candidate := range batch.Packages {
		available[candidate.RelativePath] = candidate
	}
	selected := []skills.Package{}
	seenIDs, seenNames := map[string]bool{}, map[string]bool{}
	for _, id := range ids {
		candidate, exists := available[id]
		if !exists || seenIDs[id] {
			return empty, nil, status_error.BadRequest
		}
		seenIDs[id] = true
		if seenNames[candidate.Name] {
			return empty, nil, status_error.PluginSkillSelectionConflict
		}
		seenNames[candidate.Name] = true
		if _, err := bundleCandidateDirectory(contentRoot, candidate.RelativePath); err != nil {
			return empty, nil, err
		}
		for other := range available {
			if other != id && bundleContainsPath(candidate.RelativePath, other) {
				return empty, nil, status_error.PluginSkillSelectionConflict
			}
		}
		selected = append(selected, candidate)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].RelativePath < selected[j].RelativePath })
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return empty, nil, storageFailure(err)
	}
	work, err := os.MkdirTemp(parent, "skill-bundle-")
	if err != nil {
		return empty, nil, storageFailure(err)
	}
	defer os.RemoveAll(work)
	snapshot := contentRoot
	root := filepath.Join(work, "bundle")
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o700); err != nil {
		return empty, nil, storageFailure(err)
	}
	licensePaths := map[string]string{}
	components := []map[string]any{}
	for _, candidate := range selected {
		directory, err := bundleCandidateDirectory(snapshot, candidate.RelativePath)
		if err != nil {
			return empty, nil, err
		}
		if err := validateBundleSkillTree(directory, true); err != nil {
			return empty, nil, err
		}
		target := filepath.Join(root, "skills", candidate.Name)
		if err := os.Mkdir(target, 0o700); err != nil {
			return empty, nil, storageFailure(err)
		}
		if err := pluginArchive.CopyDirectory(directory, target); err != nil {
			return empty, nil, err
		}
		digest, err := ContentDigest(target)
		if err != nil {
			return empty, nil, err
		}
		components = append(components, map[string]any{"sourcePath": candidate.RelativePath, "path": "skills/" + candidate.Name, "name": candidate.Name, "digest": digest})
		if err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return storageFailure(err)
			}
			if !bundleLicenseName(entry.Name()) {
				return nil
			}
			relative, err := filepath.Rel(directory, path)
			if err != nil {
				return status_error.SkillContentInvalid
			}
			licensePaths[filepath.ToSlash(filepath.Join(candidate.RelativePath, relative))] = filepath.ToSlash(filepath.Join("skills", candidate.Name, relative))
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}); err != nil {
			return empty, nil, err
		}
		if err := copyBundleLicenses(snapshot, filepath.Dir(directory), root, licensePaths); err != nil {
			return empty, nil, err
		}
	}
	source := map[string]any{"kind": batch.Source.Kind}
	if batch.Source.Kind == constants.SkillSourceGit {
		address, err := url.Parse(batch.Source.Location)
		if err != nil || address.User != nil || (address.Scheme != "https" && address.Scheme != "http") || address.Host == "" {
			return empty, nil, status_error.SkillImportIntegrityInvalid
		}
		address.RawQuery, address.Fragment = "", ""
		source["url"], source["requestedRef"], source["subpath"] = address.String(), batch.Source.Ref, batch.Source.Subpath
	} else if batch.Source.Kind != constants.SkillSourceZip {
		return empty, nil, status_error.SkillImportSourceInvalid
	}
	provenance := map[string]any{"schemaVersion": 1, "kind": constants.PluginSkillBundleSourceKind, "source": source, "skills": components, "licenses": licensePaths}
	record, err := json.MarshalIndent(provenance, "", "  ")
	if err != nil {
		return empty, nil, status_error.PluginContentInvalid
	}
	technicalName := "skill-bundle-" + pluginID
	openai, _ := json.Marshal(map[string]any{"interface": map[string]string{"displayName": name, "shortDescription": description}})
	neuxbot, _ := json.Marshal(map[string]int{"schemaVersion": constants.PluginExtensionSchemaVersion})
	manifest := map[string]any{"$schema": constants.PluginManifestSchema, "name": technicalName, "version": constants.PluginSkillBundleInitialVersion, "description": description, "extensions": map[string]json.RawMessage{"com.openai": openai, constants.PluginExtensionNamespace: neuxbot}}
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return empty, nil, status_error.PluginContentInvalid
	}
	if err := os.MkdirAll(filepath.Join(root, ".neuxbot"), 0o700); err != nil {
		return empty, nil, storageFailure(err)
	}
	for path, value := range map[string][]byte{"plugin.json": body, constants.PluginSkillBundleSourcePath: record} {
		if err := os.WriteFile(filepath.Join(root, path), append(value, '\n'), 0o600); err != nil {
			return empty, nil, storageFailure(err)
		}
	}
	stage, err := Stage(parent, root)
	if err != nil {
		return empty, nil, err
	}
	if len(stage.Package.Skills) != len(selected) || ValidateSkillBundle(constants.PluginLocalSkillSourcePrefix+pluginID, pluginID, stage.Package, provenance) != nil {
		_ = os.RemoveAll(filepath.Join(parent, stage.Token))
		return empty, nil, status_error.PluginContentInvalid
	}
	return stage, provenance, nil
}

// ValidateSkillBundle 仅为宿主明确生成的技能插件开放本地安装，不接受任意内容来源包。
func ValidateSkillBundle(sourceKey, pluginID string, pkg Package, provenance map[string]any) error {
	if pluginID == "" || sourceKey != constants.PluginLocalSkillSourcePrefix+pluginID || provenance["kind"] != constants.PluginSkillBundleSourceKind || pkg.SourcePath != "" {
		return status_error.PluginForbidden
	}
	if err := ValidateContract(pkg); err != nil {
		return err
	}
	extension, err := ClientExtension(pkg)
	if err != nil {
		return err
	}
	if len(pkg.Skills) < 2 || len(pkg.Servers) != 0 || pkg.Integration.Authentication != nil || len(extension.Config) != 0 {
		return status_error.PluginForbidden
	}
	return nil
}

func bundleContainsPath(parent, child string) bool {
	relative, err := filepath.Rel(filepath.FromSlash(parent), filepath.FromSlash(child))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

// bundleCandidateDirectory 不允许通过候选根或祖先链接读取扫描摘要之外的内容。
func bundleCandidateDirectory(root, relative string) (string, error) {
	if !safeRelative(relative) && relative != "." {
		return "", status_error.SkillImportIntegrityInvalid
	}
	current := root
	for _, component := range strings.Split(filepath.Clean(filepath.FromSlash(relative)), string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", status_error.SkillImportIntegrityInvalid
		}
	}
	return current, nil
}

func validateBundleSkillTree(root string, allowEntry bool) error {
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return storageFailure(err)
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return storageFailure(err)
		}
		if strings.EqualFold(entry.Name(), "SKILL.md") && (!allowEntry || path != filepath.Join(root, "SKILL.md")) {
			return status_error.PluginSkillSelectionConflict
		}
		if entry.Type()&os.ModeSymlink == 0 {
			return nil
		}
		link, err := os.Readlink(path)
		if err != nil {
			return storageFailure(err)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || filepath.IsAbs(link) || !bundleContainsPath(canonical, resolved) {
			return status_error.SkillContentInvalid
		}
		return nil
	})
}

func bundleLicenseName(name string) bool {
	name = strings.ToUpper(name)
	for _, prefix := range []string{"LICENSE", "LICENCE", "COPYING", "COPYRIGHT", "NOTICE", "THIRD_PARTY_NOTICES", "THIRD-PARTY-NOTICES"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func copyBundleLicenses(snapshot, directory, root string, paths map[string]string) error {
	for bundleContainsPath(snapshot, directory) {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return storageFailure(err)
		}
		for _, entry := range entries {
			if !bundleLicenseName(entry.Name()) {
				continue
			}
			relative, err := filepath.Rel(snapshot, filepath.Join(directory, entry.Name()))
			if err != nil {
				return status_error.SkillContentInvalid
			}
			relative = filepath.ToSlash(relative)
			if _, exists := paths[relative]; exists {
				continue
			}
			target := filepath.Join("docs", "source-licenses", filepath.FromSlash(relative))
			if entry.IsDir() {
				if err := validateBundleSkillTree(filepath.Join(snapshot, relative), false); err != nil {
					return err
				}
				if err := os.MkdirAll(filepath.Join(root, target), 0o700); err != nil {
					return storageFailure(err)
				}
				if err := pluginArchive.CopyDirectory(filepath.Join(snapshot, relative), filepath.Join(root, target)); err != nil {
					return err
				}
			} else {
				info, err := entry.Info()
				if err != nil || !info.Mode().IsRegular() {
					return status_error.SkillContentInvalid
				}
				body, err := readBounded(snapshot, relative, 50*1024*1024)
				if err != nil {
					return err
				}
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, target)), 0o700); err != nil {
					return storageFailure(err)
				}
				if err := os.WriteFile(filepath.Join(root, target), body, 0o600); err != nil {
					return storageFailure(err)
				}
			}
			paths[relative] = filepath.ToSlash(target)
		}
		if directory == snapshot {
			break
		}
		directory = filepath.Dir(directory)
	}
	return nil
}
