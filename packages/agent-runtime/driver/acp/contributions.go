package acp

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/agent-runtime/contract"
)

// maxContributionBytes 只限制新增静态前缀的构建，避免宿主贡献造成无界分配；不改变本轮输入或事件预算。
const maxContributionBytes = 1 << 20

// contributionPrefix 将冻结的描述及宿主租约目录转换为首次 Prompt 的普通文本。
// ACP 没有可移植的系统角色插入；技能正文与文件生命周期仍由宿主资源租约负责。
func contributionPrefix(c contract.Contributions, resources []contract.ResourceBinding) (string, error) {
	invalid := func() *contract.Fault {
		return fault(contract.FaultInvalidSpec, "contributions", "ACP static contribution prefix exceeds its byte budget", nil)
	}
	var prefix strings.Builder
	write := func(parts ...string) error {
		for _, part := range parts {
			if len(part) > maxContributionBytes-prefix.Len() {
				return invalid()
			}
			prefix.WriteString(part)
		}
		return nil
	}
	// 先检查原始字段，避免转义超大元数据时先进行无界复制。
	for _, instruction := range c.Instructions {
		if len(instruction.Name) > maxContributionBytes || len(instruction.Version) > maxContributionBytes || len(instruction.Text) > maxContributionBytes {
			return "", invalid()
		}
		if strings.TrimSpace(instruction.Name) == "" {
			return "", fault(contract.FaultInvalidSpec, "contributions", "ACP instruction identity is missing", nil)
		}
	}
	skills := make(map[string]contract.Skill)
	requested := make(map[string]bool)
	for _, skill := range c.Skills {
		if len(skill.Name) > maxContributionBytes || len(skill.Version) > maxContributionBytes || len(skill.ResourceID) > maxContributionBytes {
			return "", invalid()
		}
		if strings.TrimSpace(skill.Name) == "" || strings.TrimSpace(skill.ResourceID) == "" {
			return "", fault(contract.FaultInvalidSpec, "contributions", "ACP skill identity is missing", nil)
		}
		if old, ok := skills[skill.Name]; ok && old != skill {
			return "", fault(contract.FaultInvalidSpec, "contributions", "ACP skill contributions conflict", nil)
		}
		skills[skill.Name] = skill
		requested[skill.ResourceID] = true
	}
	bindings := make(map[string]string)
	for _, binding := range resources {
		if binding.MCP != nil {
			continue
		}
		if len(binding.ID) > maxContributionBytes || len(binding.Path) > maxContributionBytes {
			return "", invalid()
		}
		if binding.ID == "" || !filepath.IsAbs(binding.Path) {
			return "", fault(contract.FaultInvalidSpec, "resources", "ACP skill binding requires an identity and an absolute directory path", nil)
		}
		if !requested[binding.ID] {
			return "", fault(contract.FaultUnsupported, "resources", "ACP resource binding does not belong to a requested skill", nil)
		}
		if old, ok := bindings[binding.ID]; ok && old != binding.Path {
			return "", fault(contract.FaultPreparation, "resources", "ACP skill resource bindings conflict", nil)
		}
		bindings[binding.ID] = binding.Path
	}
	instructions := make(map[string]contract.Instruction)
	for _, instruction := range c.Instructions {
		if old, ok := instructions[instruction.Name]; ok {
			if old != instruction {
				return "", fault(contract.FaultInvalidSpec, "contributions", "ACP instruction contributions conflict", nil)
			}
			continue
		}
		if len(instructions) == 0 {
			if err := write("Runtime instructions:\n\n"); err != nil {
				return "", err
			}
		}
		instructions[instruction.Name] = instruction
		if err := write("### ", strconv.Quote(instruction.Name), " (version ", strconv.Quote(instruction.Version), ")\n", instruction.Text, "\n\n"); err != nil {
			return "", err
		}
	}
	included := make(map[string]bool)
	for _, skill := range c.Skills {
		path := bindings[skill.ResourceID]
		if path == "" {
			if skill.Optional {
				continue
			}
			return "", fault(contract.FaultPreparation, "resources", "ACP required skill resource is missing", nil)
		}
		if included[skill.Name] {
			continue
		}
		if len(included) == 0 {
			if err := write("Runtime skills:\nRead each skill's SKILL.md before using it. These paths refer to host-provided skill directories.\n\n"); err != nil {
				return "", err
			}
		}
		included[skill.Name] = true
		if err := write("- ", strconv.Quote(skill.Name), " (version ", strconv.Quote(skill.Version), "): ", strconv.Quote(filepath.Join(path, "SKILL.md")), "\n"); err != nil {
			return "", err
		}
	}
	if prefix.Len() == 0 {
		return "", nil
	}
	if err := write("\nTask:\n\n"); err != nil {
		return "", err
	}
	return prefix.String(), nil
}
