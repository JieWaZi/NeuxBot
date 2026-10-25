package ai_accounts

import (
	"regexp"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// validateModelMappings 保证原生 CLI 映射只引用本账号已选择的实际模型。
func validateModelMappings(selected []string, names map[string]string, levels map[string][]string, roles map[string]string) error {
	available := make(map[string]bool, len(selected))
	for _, model := range selected {
		available[model] = true
	}
	for model, name := range names {
		if !available[model] || strings.TrimSpace(name) != name || len(name) > 200 {
			return status_error.AIAccountConfigurationInvalid
		}
	}
	validLevel := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,79}$`)
	for model, entries := range levels {
		if !available[model] || len(entries) > 32 {
			return status_error.AIAccountConfigurationInvalid
		}
		seen := map[string]bool{}
		for _, level := range entries {
			if !validLevel.MatchString(level) || seen[level] {
				return status_error.AIAccountConfigurationInvalid
			}
			seen[level] = true
		}
	}
	validRoles := map[string]bool{"sonnet": true, "opus": true, "haiku": true, "fable": true, "subagent": true}
	for role, model := range roles {
		if !validRoles[role] || !available[model] {
			return status_error.AIAccountConfigurationInvalid
		}
	}
	return nil
}
