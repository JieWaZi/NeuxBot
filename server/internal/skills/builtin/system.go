package builtin

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	managedskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
)

func systemDefinition() (definition, error) {
	items, err := definitions()
	if err != nil {
		return definition{}, err
	}
	for _, item := range items {
		if item.System && item.Directory == constants.AgentCLISkillName {
			return item, nil
		}
	}
	return definition{}, status_error.SkillUnavailable
}

// CLIPath 返回系统技能中当前平台的可执行入口。
func CLIPath(directory string) string {
	name := "allycli"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(directory, "scripts", name)
}

// SystemDirectory 将同包资源和受管 CLI 一起准备为不可变技能，不使用用户版本指针。
func SystemDirectory(dataDirectory, binaryPath string) (string, error) {
	item, err := systemDefinition()
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
	item.Files = append(item.Files, resource{Path: filepath.ToSlash(CLIPath("")), Data: binary, Executable: true})
	root := filepath.Join(dataDirectory, "system-skills")
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
	if err := materialize(item, staging); err != nil {
		return "", err
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
	files := map[string]string{"knowledge.maintenance": "wiki-maintenance.md", "knowledge": "knowledge.md", "im.delivery": "im-delivery.md", "workflow.authoring": "workflow-authoring.md", "workflow.execution": "workflow-execution.md", "workflow.results": "workflow-results.md", "workflow.output-delivery": "output-delivery.md", "workflow.api-retrieval": "api-retrieval.md", "team.lead": "team-lead.md", "team.member": "team-member.md", "memory": "memory.md", "interaction.questions": "user-questions.md"}
	name, ok := files[id]
	if !ok {
		return "", status_error.AgentOperationForbidden
	}
	item, err := systemDefinition()
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
