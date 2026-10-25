package github

import (
	"os"
	"strings"
)

// ManagedEnvironment 清除宿主与智能体的外部 GitHub 身份，固定应用私有 profile。
func ManagedEnvironment(profile string, overrides map[string]string) map[string]string {
	result := make(map[string]string, len(overrides)+8)
	for key, value := range overrides {
		result[key] = value
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if providerEnvironment(key) {
			result[key] = ""
		}
	}
	for key := range result {
		if providerEnvironment(key) {
			result[key] = ""
		}
	}
	result["GH_CONFIG_DIR"] = profile
	result["GH_HOST"] = "github.com"
	result["GH_PROMPT_DISABLED"] = "1"
	result["GH_NO_UPDATE_NOTIFIER"] = "1"
	result["GH_NO_EXTENSION_UPDATE_NOTIFIER"] = "1"
	result["NO_COLOR"] = "1"
	return result
}
func providerEnvironment(key string) bool {
	upper := strings.ToUpper(key)
	return strings.HasPrefix(upper, "GH_") || strings.HasPrefix(upper, "GITHUB_")
}
