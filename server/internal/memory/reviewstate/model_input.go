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

// modelTurn is the outbound projection of local conversation evidence.
type modelTurn struct {
	// UserRef points to the user text for evidence citations.
	UserRef string `json:"userRef,omitempty"`
	// User is the eligible, redacted user message.
	User string `json:"user,omitempty"`
	// AssistantRef points to the eligible final answer for skill evidence.
	AssistantRef string `json:"assistantRef,omitempty"`
	// Assistant is the eligible, redacted final answer.
	Assistant string `json:"assistant,omitempty"`
	// Steps contains the eligible, bounded tool outcomes.
	Steps []ToolStep `json:"steps,omitempty"`
	// Memory marks a memory-eligible turn in a combined review.
	Memory bool `json:"memory,omitempty"`
	// Skill marks a skill-eligible turn in a combined review.
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
