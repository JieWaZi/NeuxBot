package accountconfig

import (
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

// ClaudeAPIEnvironment projects the selected Anthropic endpoint and model roles for one native launch.
func ClaudeAPIEnvironment(spec Spec) map[string]string {
	keyName := "ANTHROPIC_AUTH_TOKEN"
	// The official and OpenCode Go endpoints require an API key header.
	switch strings.TrimRight(spec.BaseURL, "/") {
	case constants.AIAccountAnthropicURL, constants.AIAccountOpenCodeGoAnthropicURL:
		keyName = "ANTHROPIC_API_KEY"
	}
	claudeEnvironment := map[string]string{keyName: spec.APIKey, "ANTHROPIC_BASE_URL": spec.BaseURL}
	if spec.Model != "" {
		claudeEnvironment["ANTHROPIC_MODEL"] = spec.Model
		for role, key := range map[string]string{"opus": "ANTHROPIC_DEFAULT_OPUS_MODEL", "sonnet": "ANTHROPIC_DEFAULT_SONNET_MODEL", "haiku": "ANTHROPIC_DEFAULT_HAIKU_MODEL", "fable": "ANTHROPIC_DEFAULT_FABLE_MODEL", "subagent": "CLAUDE_CODE_SUBAGENT_MODEL"} {
			mapped := claudeRoleModel(spec, role)
			claudeEnvironment[key] = mapped
			if name := spec.ModelDisplayNames[mapped]; name != "" && role != "subagent" {
				claudeEnvironment[key+"_NAME"] = name
			}
		}
	}
	return claudeEnvironment
}

// ClaudeModelTarget validates a selectable native ID against the account's actual models.
// Older CLIs expose role aliases, while newer catalog rows can resolve them to actual IDs.
// Native discovery and settings readback still determine whether an extended-context variant works.
func ClaudeModelTarget(spec Spec, model string) (string, bool) {
	// The literal native alias "default" resets to Claude's recommendation.
	// A managed account's omitted selection keeps its configured model instead.
	if model == "default" {
		return "", false
	}
	if slices.Contains(spec.SelectedModels, model) {
		return model, true
	}
	if model == "" {
		return spec.Model, slices.Contains(spec.SelectedModels, spec.Model)
	}
	role, extended := strings.CutSuffix(model, "[1m]")
	if extended && slices.Contains(spec.SelectedModels, role) {
		return role, true
	}
	switch role {
	case "opus", "sonnet", "haiku", "fable":
		target := claudeRoleModel(spec, role)
		return target, slices.Contains(spec.SelectedModels, target)
	default:
		return "", false
	}
}

func claudeRoleModel(spec Spec, role string) string {
	if model := spec.ClaudeModelRoles[role]; model != "" && slices.Contains(spec.SelectedModels, model) {
		return model
	}
	return spec.Model
}
