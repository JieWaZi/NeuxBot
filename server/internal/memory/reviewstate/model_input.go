package reviewstate

import (
	"strconv"
	"strings"
)

// modelReferences maps batch-local references back to their authorized identities.
type modelReferences map[string]string

func (r modelReferences) add(prefix, id string) string {
	if id == "" {
		return ""
	}
	ref := prefix + strconv.Itoa(len(r)+1)
	r[ref] = id
	return ref
}

func (r modelReferences) restore(refs []string) []string {
	ids := make([]string, len(refs))
	for i, ref := range refs {
		ids[i] = r[ref]
	}
	return ids
}

// modelTurn 是会话证据的模型输入投影。
type modelTurn struct {
	// UserRef 是用户正文的证据引用。
	UserRef string `json:"userRef,omitempty"`
	// User 是符合资格且已脱敏的用户原文。
	User string `json:"user,omitempty"`
	// AssistantRef 是用于技能学习的最终答复引用。
	AssistantRef string `json:"assistantRef,omitempty"`
	// Assistant 是符合资格且已脱敏的最终答复。
	Assistant string `json:"assistant,omitempty"`
	// Steps 保存符合资格的有界工具结果。
	Steps []ToolStep `json:"steps,omitempty"`
	// Memory 表示本轮可用于记忆整理。
	Memory bool `json:"memory,omitempty"`
	// Skill 表示本轮可用于技能学习。
	Skill bool `json:"skill,omitempty"`
}

// modelEvidence excludes local IDs and eligibility fields that the current task does not use.
func modelEvidence(turns []Turn, memory, skill bool, references modelReferences) []modelTurn {
	projected := make([]modelTurn, 0, len(turns))
	for _, turn := range turns {
		useMemory, useSkill := memory && turn.Memory, skill && turn.Skill
		if !useMemory && !useSkill {
			continue
		}
		item := modelTurn{Memory: memory && skill && useMemory, Skill: memory && skill && useSkill}
		if strings.TrimSpace(turn.User) != "" {
			item.User = turn.User
			if references != nil {
				item.UserRef = references.add("e", turn.UserID)
			}
		}
		if useSkill {
			if strings.TrimSpace(turn.Assistant) != "" {
				item.Assistant = turn.Assistant
				if references != nil {
					item.AssistantRef = references.add("e", turn.AssistantID)
				}
			}
			for _, step := range turn.Steps {
				if references != nil {
					step.ID = references.add("e", step.ID)
				} else {
					step.ID = ""
				}
				item.Steps = append(item.Steps, step)
			}
		}
		if item.User != "" || item.Assistant != "" || len(item.Steps) != 0 {
			projected = append(projected, item)
		}
	}
	return projected
}
