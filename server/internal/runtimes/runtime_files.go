package runtimes

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	managedskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
)

// skillSelection 保存 Runtime 协议需要显式传递的已选技能。
type skillSelection struct {
	// name 是 Runtime 协议使用的技能名称。
	name string
	// path 是 Runtime 协议使用的 SKILL.md 绝对路径。
	path string
}

// runtimeFiles 保存启动 Runtime 时需要的技能目录信息。
type runtimeFiles struct {
	// skillDirectory 是当前 Runtime 可以读取的技能根目录。
	skillDirectory string
	// selectedSkills 是当前消息明确选择的技能。
	selectedSkills []skillSelection
}

// PrepareWorkspace 为注入的执行宿主物化原生指令与技能，不创建进程或提交输入。
func (sdk *SDK) PrepareWorkspace(request RunRequest) error {
	_, err := sdk.prepareRuntimeFiles(request.Detection, request.Workspace, request.Instructions, request.Skills)
	return err
}

// prepareRuntimeFiles 把当前指令和技能准备成所选 Runtime 能直接读取的文件。
func (sdk *SDK) prepareRuntimeFiles(
	detection DetectionResult,
	workspace Workspace,
	instructions []Instruction,
	skills []Skill,
) (runtimeFiles, error) {
	if !workspace.Managed || workspace.Path == "" {
		if len(skills) > 0 {
			return runtimeFiles{}, status_error.BadRequest
		}
		return runtimeFiles{}, nil
	}
	if err := sdk.writeRuntimeInstructions(detection, workspace, instructions); err != nil {
		return runtimeFiles{}, err
	}
	if err := sdk.clearRuntimeSkillDirectories(detection, workspace.Path); err != nil {
		return runtimeFiles{}, err
	}
	if len(skills) == 0 {
		return runtimeFiles{}, nil
	}
	if err := validateRuntimeSkillSupport(detection); err != nil {
		return runtimeFiles{}, err
	}
	if err := validateRuntimeSkills(skills); err != nil {
		return runtimeFiles{}, err
	}

	root := runtimeSkillRoot(detection, workspace.Path)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return runtimeFiles{}, status_error.SkillUnavailable
	}
	selected := make([]skillSelection, 0, len(skills))
	for _, skill := range skills {
		target := runtimeSkillPath(root, skill.Name)
		if err := linkOrCopyRuntimeSkill(skill.Path, target); err != nil {
			return runtimeFiles{}, status_error.SkillUnavailable
		}
		if skill.Selected {
			selected = append(selected, skillSelection{
				name: skill.Name,
				path: filepath.Join(target, "SKILL.md"),
			})
		}
	}
	return runtimeFiles{
		skillDirectory: root,
		selectedSkills: selected,
	}, nil
}

// runtimeSkillRoot 使用运行时注册信息声明的项目级 Skill 目录。
func runtimeSkillRoot(
	detection DetectionResult,
	workspacePath string,
) string {
	return filepath.Join(
		workspacePath,
		detection.Runtime.SkillDirectoryName,
		"skills",
	)
}

func (sdk *SDK) writeRuntimeInstructions(
	detection DetectionResult,
	workspace Workspace,
	instructions []Instruction,
) error {
	workspacePath := workspace.Path
	fileName := detection.Runtime.InstructionFileName
	if !safeWorkspaceEntryName(fileName) {
		return status_error.SkillUnavailable
	}
	var content strings.Builder
	content.WriteString("# NeuxBot Chat Workspace Constraints\n\n")
	appendWorkspaceFileRouting(&content, workspace.ProjectDirectories)
	for _, instruction := range instructions {
		name := strings.TrimSpace(instruction.Name)
		if name == "" {
			return status_error.SkillUnavailable
		}
		if isProfileInstruction(name) {
			content.WriteString(instruction.Content)
			content.WriteString("\n\n")
			continue
		}
		fmt.Fprintf(&content, "## %s\n\n%s\n\n", name, instruction.Content)
	}
	if err := writeWorkspaceTextFile(workspacePath, fileName, content.String()); err != nil {
		return err
	}
	for _, summary := range sdk.Runtimes() {
		knownFile := summary.InstructionFileName
		if knownFile == fileName || !safeWorkspaceEntryName(knownFile) {
			continue
		}
		if err := os.Remove(filepath.Join(workspacePath, knownFile)); err != nil && !os.IsNotExist(err) {
			return status_error.SkillUnavailable
		}
	}
	if err := os.Remove(filepath.Join(workspacePath, constants.TeamInstructionFileName)); err != nil && !os.IsNotExist(err) {
		return status_error.SkillUnavailable
	}
	return nil
}

// isProfileInstruction 判断指令是否为无需文件名标题的伙伴档案。
func isProfileInstruction(name string) bool {
	switch name {
	case constants.ProfileDocumentIdentityFileName,
		constants.ProfileDocumentPersonaFileName,
		constants.ProfileDocumentPlaybookFileName:
		return true
	default:
		return false
	}
}

// writeWorkspaceTextFile 原子写入工作区根目录下的文本文件。
func writeWorkspaceTextFile(workspacePath string, fileName string, content string) error {
	if !safeWorkspaceEntryName(fileName) {
		return status_error.SkillUnavailable
	}
	staging, err := os.CreateTemp(workspacePath, "runtime-instructions-")
	if err != nil {
		return status_error.SkillUnavailable
	}
	stagingPath := staging.Name()
	defer os.Remove(stagingPath)
	if _, err := staging.WriteString(content); err != nil {
		_ = staging.Close()
		return status_error.SkillUnavailable
	}
	if err := staging.Chmod(0o600); err != nil {
		_ = staging.Close()
		return status_error.SkillUnavailable
	}
	if err := staging.Close(); err != nil {
		return status_error.SkillUnavailable
	}
	if err := os.Rename(stagingPath, filepath.Join(workspacePath, fileName)); err != nil {
		return status_error.SkillUnavailable
	}
	return nil
}

// appendWorkspaceFileRouting 写入 Codex 和 Claude 共用的工作区文件归属规则。
func appendWorkspaceFileRouting(
	content *strings.Builder,
	directories []WorkspaceProjectDirectory,
) {
	content.WriteString("## Workspace File Routing\n\n")
	content.WriteString("The current working directory is a NeuxBot-managed Chat workspace, not a single project.\n\n")
	if len(directories) > 0 {
		content.WriteString("Available project directories:\n\n")
		for _, directory := range directories {
			if directory.Default {
				fmt.Fprintf(content, "- `%s` (default project directory)\n", directory.Path)
				continue
			}
			fmt.Fprintf(content, "- `%s`\n", directory.Path)
		}
		content.WriteString("\n")
	}
	content.WriteString("File routing rules:\n\n")
	content.WriteString("- When the user specifies a project or path, use the corresponding project directory.\n")
	content.WriteString("- When modifying an existing file, keep it in its owning project and use a path under that project directory.\n")
	content.WriteString("- New or modified files that clearly belong to a project must be written under that project's directory.\n")
	content.WriteString("- For cross-project work, create or modify files separately in each corresponding project directory.\n")
	fmt.Fprintf(
		content,
		"- Files confirmed to be temporary or unrelated to any project must be created under `%s/%s/`.\n",
		constants.ExecutionWorkspaceAttachmentsDirectory,
		constants.ExecutionWorkspaceGeneratedFilesDirectory,
	)
	fmt.Fprintf(
		content,
		"- Other paths under `%s/` contain user-provided files; do not overwrite them.\n",
		constants.ExecutionWorkspaceAttachmentsDirectory,
	)
	content.WriteString("- Do not create user deliverables in the NeuxBot Chat workspace root.\n\n")
}

func (sdk *SDK) clearRuntimeSkillDirectories(
	detection DetectionResult,
	workspacePath string,
) error {
	for _, path := range []string{
		filepath.Join(workspacePath, detection.Runtime.SkillDirectoryName, detection.Runtime.BaseRuntimeID),
		runtimeSkillRoot(detection, workspacePath),
	} {
		if err := os.RemoveAll(path); err != nil {
			return status_error.SkillUnavailable
		}
	}
	current := detection.Runtime.SkillDirectoryName
	if !safeWorkspaceEntryName(current) {
		return status_error.SkillUnavailable
	}
	for _, summary := range sdk.Runtimes() {
		name := summary.SkillDirectoryName
		if !safeWorkspaceEntryName(name) {
			continue
		}
		path := filepath.Join(workspacePath, name)
		if name == current {
			path = filepath.Join(path, detection.Runtime.BaseRuntimeID)
		}
		if err := os.RemoveAll(path); err != nil {
			return status_error.SkillUnavailable
		}
	}
	return nil
}

func validateRuntimeSkillSupport(detection DetectionResult) error {
	if !detection.Capabilities.Has(CapabilitySkillDirectory) {
		return status_error.SkillUnavailable
	}
	return nil
}

func validateRuntimeSkills(skills []Skill) error {
	seenNames := make(map[string]struct{}, len(skills))
	for _, skill := range skills {
		if !safeWorkspaceEntryName(skill.Name) || !filepath.IsAbs(skill.Path) {
			return status_error.SkillUnavailable
		}
		if _, exists := seenNames[skill.Name]; exists {
			return status_error.SkillUnavailable
		}
		seenNames[skill.Name] = struct{}{}
		info, err := os.Stat(filepath.Join(skill.Path, "SKILL.md"))
		if err != nil {
			return status_error.SkillUnavailable
		}
		if !info.Mode().IsRegular() {
			return status_error.SkillUnavailable
		}
	}
	return nil
}

// linkOrCopyRuntimeSkill 默认链接当前 Skill，操作系统不支持链接时复制完整目录。
func linkOrCopyRuntimeSkill(source string, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	if err := os.Symlink(source, target); err == nil {
		return nil
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	return managedskills.CopyDirectory(source, target)
}

func runtimeSkillPath(root string, name string) string {
	return filepath.Join(root, name)
}

func safeWorkspaceEntryName(value string) bool {
	return value != "" && filepath.Base(value) == value && value != "." &&
		value != ".." && !strings.ContainsAny(value, "/\\\x00\r\n")
}
