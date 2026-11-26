package builtin

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	managedskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
)

// CLIPath 返回系统技能中当前平台的可执行入口。
func CLIPath(directory string) string {
	name := constants.AgentCLIBinaryName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(directory, constants.AgentCLISkillScriptsDirectory, name)
}

// SystemDirectory 将同包资源和受管 CLI 一起准备为不可变技能，不使用用户版本指针。
func SystemDirectory(dataDirectory, binaryPath string) (string, error) {
	item, err := managedskills.SystemSkillDefinition()
	if err != nil {
		return "", err
	}
	info, err := os.Stat(binaryPath)
	if err != nil || !info.Mode().IsRegular() {
		return "", status_error.AgentConnectionUnavailable
	}
	binary, err := os.ReadFile(binaryPath)
	if err != nil || len(binary) == 0 {
		return "", status_error.AgentConnectionUnavailable
	}
	// CLI 字节和执行位纳入同一内容摘要，更新程序时不会复用旧入口。
	item.Files = append(item.Files, managedskills.BuiltinResource{Path: filepath.ToSlash(CLIPath("")), Data: binary, Executable: true})
	root := filepath.Join(dataDirectory, constants.AgentCLISystemSkillsDirectory)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", status_error.SkillStorageError
	}
	info, err = os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", status_error.SkillContentInvalid
	}
	staging, err := os.MkdirTemp(root, "system-skill-staging-")
	if err != nil {
		return "", status_error.SkillStorageError
	}
	defer os.RemoveAll(staging)
	marker := filepath.Join(staging, systemStagingOwner)
	if err := os.WriteFile(marker, []byte(filepath.Base(staging)), 0600); err != nil {
		return "", status_error.SkillStorageError
	}
	if err := materialize(item, staging); err != nil {
		return "", err
	}
	// 发布目录的摘要只覆盖产品交付内容。
	if err := os.Remove(marker); err != nil {
		return "", status_error.SkillStorageError
	}
	digest, err := managedskills.Digest(staging)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, digest)
	if info, err := os.Lstat(target); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", status_error.SkillContentInvalid
		}
		actual, err := managedskills.Digest(target)
		if err != nil || actual != digest {
			return "", status_error.SkillContentInvalid
		}
		return target, nil
	} else if !os.IsNotExist(err) {
		return "", status_error.SkillStorageError
	}
	if err := os.Rename(staging, target); err != nil {
		actual, lookupErr := managedskills.Digest(target)
		if lookupErr != nil || actual != digest {
			return "", status_error.SkillStorageError
		}
	}
	return target, nil
}

// Guide 返回与实际系统技能完全相同的单个按需指南。
func Guide(id string) (string, error) {
	files := map[string]string{"knowledge.maintenance": "wiki-maintenance.md", "knowledge": "wiki-guide.md", "im.delivery": "im-delivery.md", "workflow.authoring": "workflow-authoring.md", "workflow.execution": "workflow-execution.md", "workflow.results": "workflow-results.md", "workflow.output-delivery": "output-delivery.md", "workflow.api-retrieval": "api-retrieval.md", "team.lead": "team-lead.md", "team.member": "team-member.md", "memory": "memory.md", "interaction.questions": "user-questions.md", constants.WakerProfileOnboardingGuide: "partner-onboarding.md"}
	files[constants.WakerProfileOptimizationGuide] = "partner-profile-optimization.md"
	files["automatic.tasks"] = "automatic-tasks.md"
	files["skills.management"] = "skill-management.md"
	files["mcp.management"] = "mcp-management.md"
	name, ok := files[id]
	if !ok {
		return "", status_error.AgentOperationForbidden
	}
	item, err := managedskills.SystemSkillDefinition()
	if err != nil {
		return "", err
	}
	for _, resource := range item.Files {
		if resource.Path == "references/"+name {
			return strings.TrimSpace(string(resource.Data)), nil
		}
	}
	return "", status_error.SkillContentInvalid
}

// CleanupObsoleteSystemDirectories 回收本次发布以前已结束生命周期的系统技能版本。
// 调用者已独占程序数据根，且新 Core 尚未接受执行。
func CleanupObsoleteSystemDirectories(current string) error {
	parent := filepath.Dir(current)
	root, err := os.OpenRoot(parent)
	if err != nil {
		return status_error.SkillStorageError
	}
	defer root.Close()
	entries, err := os.ReadDir(parent)
	if err != nil {
		return status_error.SkillStorageError
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == filepath.Base(current) || !entry.IsDir() {
			continue
		}
		if knownSystemStaging.MatchString(name) {
			marker := filepath.Join(name, systemStagingOwner)
			info, err := root.Lstat(marker)
			if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(name)) {
				continue
			}
			owner, err := root.ReadFile(marker)
			if err != nil || string(owner) != name {
				continue
			}
		} else {
			if len(name) != 64 {
				continue
			}
			path := filepath.Join(parent, name)
			digest, err := managedskills.Digest(path)
			if err != nil || digest != name {
				continue
			}
			if info, err := os.Lstat(CLIPath(path)); err != nil || !info.Mode().IsRegular() {
				continue
			}
			info, err := os.Lstat(filepath.Join(path, "SKILL.md"))
			if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
				continue
			}
			document, err := os.ReadFile(filepath.Join(path, "SKILL.md"))
			if err != nil || !strings.Contains(string(document), "\nname: "+constants.AgentCLISkillName+"\n") {
				continue
			}
		}
		if err := root.RemoveAll(name); err != nil {
			return status_error.SkillStorageError
		}
	}
	return nil
}

const systemStagingOwner = ".system-skill-owner"

var knownSystemStaging = regexp.MustCompile(`^system-skill-staging-[0-9]+$`)
