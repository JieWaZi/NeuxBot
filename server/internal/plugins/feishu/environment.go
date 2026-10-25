package feishu

import (
	"os"
	"strings"
)

// ManagedEnvironment 覆盖外部 CLI 的身份、profile、代理和数据目录选择，只使用应用私有配置。
// 智能体 SDK 会继承宿主环境，因此以空值覆盖继承键，而不能仅从覆盖表删除它们。
func ManagedEnvironment(profile string, overrides map[string]string) map[string]string {
	result := make(map[string]string, len(overrides)+4)
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
	result["LARKSUITE_CLI_CONFIG_DIR"] = profile
	result["LARKSUITE_CLI_NO_UPDATE_NOTIFIER"] = "1"
	result["LARKSUITE_CLI_NO_SKILLS_NOTIFIER"] = "1"
	return result
}

func providerEnvironment(key string) bool {
	return strings.HasPrefix(strings.ToUpper(key), "LARKSUITE_CLI_")
}
