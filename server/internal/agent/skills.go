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

	"github.com/JieWaZi/NeuxBot/server/constants"
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
	// optional allows chat to omit an unavailable skill without weakening file ownership checks.
	optional bool
	// owner identifies the partner authorized to use a shared verified discovery target.
	owner string
	// retained defers projection changes while another turn is using this target.
	retained bool
	// catalogUnavailable distinguishes a read failure from a successfully empty catalogue.
	catalogUnavailable bool
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
	return freezeSkills(detection, workspace, skills, plugins, false)
}

// FreezeOptionalSkills keeps valid declarations when an independent chat skill is unavailable.
func FreezeOptionalSkills(detection DetectionResult, workspace Workspace, skills []Skill, plugins map[string]string, owner string, catalogUnavailable bool) (*SkillSelection, error) {
	selection, err := freezeSkills(detection, workspace, skills, plugins, true)
	if selection != nil {
		selection.owner = owner
		selection.catalogUnavailable = catalogUnavailable
	}
	return selection, err
}

// Retained reports that concurrent same-partner turns must continue to use the current projection.
func (s *SkillSelection) Retained() bool { return s != nil && s.retained }

func (s *SkillSelection) retainWhileActive(active *SkillSelection) *SkillSelection {
	if s == nil || active == nil || !s.optional || !active.optional || s.owner == "" || s.owner != active.owner ||
		!maps.Equal(s.plugins, active.plugins) || workspacePathKey(s.cwd) != workspacePathKey(active.cwd) ||
		workspacePathKey(s.directory) != workspacePathKey(active.directory) || s.systemSkillDigest() != active.systemSkillDigest() {
		return nil
	}
	copy := *active
	copy.retained = true
	return &copy
}

func (s *SkillSelection) systemSkillDigest() string {
	for _, skill := range s.entries {
		if skill.name == constants.AgentCLISkillName {
			return skill.digest
		}
	}
	return ""
}

func freezeSkills(detection DetectionResult, workspace Workspace, skills []Skill, plugins map[string]string, optional bool) (*SkillSelection, error) {
	if !workspace.Managed || workspace.Path == "" {
		if len(skills) != 0 && !optional {
			return nil, status_error.SkillUnavailable
		}
		return nil, nil
	}
	directory := detection.Runtime.SkillDirectoryName
	if !safeSkillEntryName(directory) {
		if len(skills) != 0 && !optional {
			return nil, status_error.SkillUnavailable
		}
		return nil, nil
	}
	cwd, err := resolveAgentDirectory(workspace.Path)
	if err != nil {
		return nil, err
	}
	selection := &SkillSelection{cwd: cwd, directory: directory, plugins: maps.Clone(plugins), optional: optional}
	if selection.plugins == nil {
		selection.plugins = map[string]string{}
	}
	names := map[string]bool{}
	identities := []any{}
	for _, skill := range skills {
		decoded, err := hex.DecodeString(skill.Digest)
		if !safeSkillEntryName(skill.Name) || !filepath.IsAbs(skill.Path) || names[skill.Name] || err != nil || len(decoded) != sha256.Size {
			if optional {
				continue
			}
			return nil, status_error.SkillContentInvalid
		}
		names[skill.Name] = true
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
