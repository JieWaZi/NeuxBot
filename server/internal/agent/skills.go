package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

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

func safeSkillEntryName(value string) bool {
	return value != "." && filepath.IsLocal(value) && filepath.Base(value) == value && !strings.ContainsAny(value, "/\\\x00\r\n")
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
