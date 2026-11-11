package accountconfig

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// ACPSystemConfigVariable names the settings and session root override for an account-aware ACP runtime.
func ACPSystemConfigVariable(runtimeID string) string {
	switch runtimeID {
	case constants.RuntimeIDWorkBuddy:
		return "CODEBUDDY_CONFIG_DIR"
	case constants.RuntimeIDQoder:
		return "QODER_CONFIG_DIR"
	default:
		return ""
	}
}

// IsACPAccountOverride identifies launch values reserved for the frozen ACP system account.
func IsACPAccountOverride(runtimeID, key string) bool {
	if ACPSystemConfigVariable(runtimeID) == "" {
		return false
	}
	key = strings.ToUpper(key)
	return key == "HOME" || key == "USERPROFILE" || key == "BASE_URL" || inheritedACPOverride(key)
}

func inheritedACPOverride(key string) bool {
	return strings.HasPrefix(key, "CODEBUDDY_") || strings.HasPrefix(key, "WORKBUDDY_") ||
		strings.HasPrefix(key, "QODER_") || strings.HasPrefix(key, "QODERCN_") ||
		key == "ACC_PRODUCT_CONFIG" || strings.HasPrefix(key, "ACC_PRODUCT_CONFIG_") ||
		key == "ENV_PRODUCT_CLI_ACCOUNT_TYPE"
}

// ValidateWorkBuddyCredentialSettings rejects settings that override the frozen account.
func ValidateWorkBuddyCredentialSettings(data []byte) error {
	var settings struct {
		// APIKeyHelper can supply a credential independently of the host selection.
		APIKeyHelper json.RawMessage `json:"apiKeyHelper"`
		// Environment may override credentials and the provider origin from disk.
		Environment map[string]json.RawMessage `json:"env"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return status_error.RuntimeConfigurationInvalid
	}
	if nonemptyJSON(settings.APIKeyHelper) {
		return status_error.RuntimePolicyUnsupported
	}
	for key, value := range settings.Environment {
		key = strings.ToUpper(key)
		switch key {
		case "CODEBUDDY_API_KEY", "CODEBUDDY_AUTH_TOKEN", "CODEBUDDY_CREDENTIALS_IN_MEMORY",
			"CODEBUDDY_API_KEY_DISABLED", "CODEBUDDY_API_KEY_HELPER_DISABLED", "CODEBUDDY_BASE_URL", "CODEBUDDY_CUSTOM_HEADERS",
			"CODEBUDDY_SERVICE_PLATFORM_BASE_URL", "CODEBUDDY_INTERNET_ENVIRONMENT", "CODEBUDDY_INTERNET_ENVIROMENT",
			"CODEBUDDY_CONFIG_DIR", "CODEBUDDY_PROJECT_CONFIG_DIR", "WORKBUDDY_CONFIG_DIR", "BASE_URL", "HOME", "USERPROFILE",
			"ACC_PRODUCT_CONFIG", "ACC_PRODUCT_CONFIG_V2", "ACC_PRODUCT_CONFIG_PATH", "ENV_PRODUCT_CLI_ACCOUNT_TYPE":
			if nonemptyJSON(value) {
				return status_error.RuntimePolicyUnsupported
			}
		}
	}
	return nil
}

func nonemptyJSON(value json.RawMessage) bool {
	value = bytes.TrimSpace(value)
	return len(value) != 0 && !bytes.Equal(value, []byte("null")) && !bytes.Equal(value, []byte(`""`))
}

// QoderNativeScope 根据所选程序确定原生协议、配置变量与凭据变量。
func QoderNativeScope(executable string) (runtimeID, configVariable, tokenVariable string) {
	name := strings.ToLower(filepath.Base(executable))
	for _, suffix := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		name = strings.TrimSuffix(name, suffix)
	}
	if name == "qoderclicn" || strings.HasPrefix(name, "qoderclicn-") {
		return constants.RuntimeQoderCNProtocolID, "QODERCN_CONFIG_DIR", "QODERCN_PERSONAL_ACCESS_TOKEN"
	}
	return constants.RuntimeIDQoder, "QODER_CONFIG_DIR", "QODER_PERSONAL_ACCESS_TOKEN"
}
