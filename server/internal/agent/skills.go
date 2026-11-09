package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	managedskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
)

const skillManifestName = ".neuxbot-skills.json"

// SkillSelection freezes one native discovery target without acquiring resources.
// Its private fields prevent callers from changing the reserved identity.
type SkillSelection struct {
	// cwd is the canonical process working directory.
	cwd string
	// directory is the native discovery root relative to cwd.
	directory string
	// entries fixes the name, source, and complete content digest of every skill.
	entries []skillEntry
	// plugins fixes immutable plugin versions contributing to this target.
	plugins map[string]string
	// fingerprint excludes source paths, runtime names, and per-turn identities.
	fingerprint string
}

type skillEntry struct {
	// name is the native discovery name.
	name string
	// path is the absolute source directory read during materialization.
	path string
	// digest is the exact complete content hash frozen before admission.
	digest string
}

type skillManifest struct {
	// Version rejects manifests from any other storage contract.
	Version int `json:"version"`
	// Targets records independent physical discovery roots and their managed names.
	Targets map[string]map[string]string `json:"targets"`
}

// FreezeSkills reserves the published content identity without reading source trees.
func FreezeSkills(detection DetectionResult, workspace Workspace, skills []Skill, plugins map[string]string) (*SkillSelection, error) {
	if !workspace.Managed || workspace.Path == "" {
		if len(skills) != 0 {
			return nil, status_error.SkillUnavailable
		}
		return nil, nil
	}
	directory := detection.Runtime.SkillDirectoryName
	if !safeSkillEntryName(directory) {
		if len(skills) != 0 {
			return nil, status_error.SkillUnavailable
		}
		return nil, nil
	}
	cwd, err := resolveAgentDirectory(workspace.Path)
	if err != nil {
		return nil, err
	}
	selection := &SkillSelection{cwd: cwd, directory: directory, plugins: maps.Clone(plugins)}
	if selection.plugins == nil {
		selection.plugins = map[string]string{}
	}
	names := map[string]bool{}
	identities := []any{}
	for _, skill := range skills {
		if !safeSkillEntryName(skill.Name) || !filepath.IsAbs(skill.Path) || names[skill.Name] {
			return nil, status_error.SkillContentInvalid
		}
		names[skill.Name] = true
		decoded, err := hex.DecodeString(skill.Digest)
		if err != nil || len(decoded) != sha256.Size {
			return nil, status_error.SkillContentInvalid
		}
		selection.entries = append(selection.entries, skillEntry{name: skill.Name, path: skill.Path, digest: hex.EncodeToString(decoded)})
	}
	slices.SortFunc(selection.entries, func(a, b skillEntry) int { return strings.Compare(a.name, b.name) })
	for _, entry := range selection.entries {
		identities = append(identities, []string{entry.name, entry.digest})
	}
	data, err := json.Marshal([]any{identities, selection.plugins})
	if err != nil {
		return nil, status_error.SkillContentInvalid
	}
	digest := sha256.Sum256(data)
	selection.fingerprint = hex.EncodeToString(digest[:])
	return selection, nil
}

// MarshalJSON contributes only stable compatibility evidence, never source paths.
func (s *SkillSelection) MarshalJSON() ([]byte, error) {
	return json.Marshal([]string{s.cwd, s.directory, s.fingerprint})
}

func skillsConflict(a, b *SkillSelection) bool {
	return a != nil && b != nil && workspacePathKey(a.cwd) == workspacePathKey(b.cwd) && workspacePathKey(a.directory) == workspacePathKey(b.directory) && a.fingerprint != b.fingerprint
}

// SameSkills confirms that materialized resources match the pre-admission snapshot.
func SameSkills(a, b *SkillSelection) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return workspacePathKey(a.cwd) == workspacePathKey(b.cwd) && workspacePathKey(a.directory) == workspacePathKey(b.directory) && a.fingerprint == b.fingerprint
}

// prepareSkills publishes one reserved physical target. ProviderRuntime serializes
// root manifest transactions; only recorded and unchanged files may be replaced.
func prepareSkills(selection *SkillSelection) error {
	if selection == nil {
		return nil
	}
	root, directory := selection.cwd, selection.directory
	skills := selection.entries
	if info, err := os.Lstat(root); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return status_error.SkillContentInvalid
	}

	manifest := filepath.Join(root, skillManifestName)
	installed := skillManifest{Version: 1, Targets: map[string]map[string]string{}}
	if info, err := os.Lstat(manifest); err == nil {
		if !info.Mode().IsRegular() {
			return status_error.SkillContentInvalid
		}
		data, err := os.ReadFile(manifest)
		if err != nil {
			return skillStorageError(err)
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if len(data) > 1<<20 || decoder.Decode(&installed) != nil || decoder.Decode(new(any)) != io.EOF || installed.Version != 1 || installed.Targets == nil {
			return status_error.SkillContentInvalid
		}
	} else if !os.IsNotExist(err) {
		return skillStorageError(err)
	}
	for target, entries := range installed.Targets {
		parts := strings.Split(target, "/")
		if len(parts) != 2 || !safeSkillEntryName(parts[0]) || parts[1] != "skills" || entries == nil {
			return status_error.SkillContentInvalid
		}
		for name, digest := range entries {
			decoded, err := hex.DecodeString(digest)
			if !safeSkillEntryName(name) || err != nil || len(decoded) != 32 {
				return status_error.SkillContentInvalid
			}
		}
	}
	targetKey := directory + "/skills"
	previous := map[string]string{}
	for name, digest := range installed.Targets[targetKey] {
		if err := ensureSkillParents(root, []string{directory, "skills"}, false); err != nil {
			return err
		}
		rel := targetKey + "/" + name
		actual, err := managedskills.Digest(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil || actual != digest {
			return status_error.SkillContentInvalid
		}
		previous[rel] = digest
	}

	if len(previous) == 0 && len(skills) == 0 {
		return nil
	}
	stage, err := os.MkdirTemp(root, ".neuxbot-skills-stage-")
	if err != nil {
		return skillStorageError(err)
	}
	keepStage := false
	defer func() {
		if !keepStage {
			if err := os.RemoveAll(stage); err != nil {
				logx.Errorf("clean managed skill staging: error=%v", err)
			}
		}
	}()
	next := map[string]string{}
	for _, skill := range skills {
		if !safeSkillEntryName(skill.name) || !filepath.IsAbs(skill.path) {
			return status_error.SkillContentInvalid
		}
		rel := directory + "/skills/" + skill.name
		if _, exists := next[rel]; exists {
			return status_error.SkillContentInvalid
		}
		if info, err := os.Stat(filepath.Join(skill.path, "SKILL.md")); err != nil || !info.Mode().IsRegular() {
			return status_error.SkillContentInvalid
		}
		if err := ensureSkillParents(root, []string{directory, "skills"}, true); err != nil {
			return err
		}
		if _, owned := previous[rel]; !owned {
			if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
				return status_error.SkillContentInvalid
			}
		}

		target := filepath.Join(stage, "new", filepath.FromSlash(rel))
		if err := managedskills.CopyDirectory(skill.path, target); err != nil {
			return skillStorageError(err)
		}
		digest, err := managedskills.Digest(target)
		if err != nil {
			return skillStorageError(err)
		}
		if digest != skill.digest {
			return status_error.SkillContentInvalid
		}
		next[rel] = digest
	}
	installed.Targets[targetKey] = map[string]string{}
	for rel, digest := range next {
		installed.Targets[targetKey][strings.TrimPrefix(rel, targetKey+"/")] = digest
	}
	data, err := json.Marshal(installed)
	if err != nil {
		return status_error.SkillContentInvalid
	}
	if err := os.WriteFile(filepath.Join(stage, "manifest"), data, 0600); err != nil {
		return skillStorageError(err)
	}
	// Keep all backups until the new manifest commits; failures restore the old tree.
	changed := []string{}
	rollback := func() {
		for _, rel := range slices.Backward(changed) {
			target := filepath.Join(root, filepath.FromSlash(rel))
			if _, installed := next[rel]; installed {
				if err := os.RemoveAll(target); err != nil {
					keepStage = true
					logx.Errorf("rollback managed skill install: error=%v", err)
					continue
				}
			}
			if _, existed := previous[rel]; existed {
				if err := os.Rename(filepath.Join(stage, "old", filepath.FromSlash(rel)), target); err != nil {
					keepStage = true
					logx.Errorf("restore managed skill backup: error=%v", err)
				}
			}
		}
	}
	names := make([]string, 0, len(previous)+len(next))
	for rel := range previous {
		names = append(names, rel)
	}
	for rel := range next {
		if _, exists := previous[rel]; !exists {
			names = append(names, rel)
		}
	}
	slices.Sort(names)
	for _, rel := range names {
		if old, exists := previous[rel]; exists && old != "" && old == next[rel] {
			continue
		}
		target := filepath.Join(root, filepath.FromSlash(rel))
		if _, existed := previous[rel]; existed {
			backup := filepath.Join(stage, "old", filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(backup), 0700); err != nil {
				rollback()
				return skillStorageError(err)
			}
			if err := os.Rename(target, backup); err != nil {
				rollback()
				return skillStorageError(err)
			}
		}
		changed = append(changed, rel)
		if _, install := next[rel]; install {
			if err := os.Rename(filepath.Join(stage, "new", filepath.FromSlash(rel)), target); err != nil {
				rollback()
				return skillStorageError(err)
			}
		}
	}
	if err := os.Rename(filepath.Join(stage, "manifest"), manifest); err != nil {
		rollback()
		return skillStorageError(err)
	}
	return nil
}

// ensureSkillParents rejects symlink discovery roots before any copy or cleanup.
func ensureSkillParents(root string, parts []string, create bool) error {
	for _, part := range parts {
		root = filepath.Join(root, part)
		info, err := os.Lstat(root)
		if os.IsNotExist(err) && create {
			if err := os.Mkdir(root, 0700); err != nil {
				return skillStorageError(err)
			}
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return status_error.SkillContentInvalid
		}
	}
	return nil
}

func safeSkillEntryName(value string) bool {
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value && !strings.ContainsAny(value, "/\\\x00\r\n")
}

func skillStorageError(err error) error {
	logx.Errorf("prepare managed skills: error=%v", err)
	return status_error.SkillStorageError
}

func workspacePathKey(path string) string {
	value := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(value)
	}
	return value
}
