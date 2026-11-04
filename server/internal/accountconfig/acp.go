package accountconfig

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

// ACPSystemConfigVariable names the settings and session root override for an account-aware ACP runtime.
func ACPSystemConfigVariable(runtimeID string) string {
	switch runtimeID {
	case constants.RuntimeIDWorkBuddy:
		return "CODEBUDDY_CONFIG_DIR"
	case constants.RuntimeIDQoder:
		return "QODER_CONFIG_DIR"
	case constants.RuntimeIDQoderCN:
		return "QODERCN_CONFIG_DIR"
	default:
		return ""
	}
}

// QoderPeerRuntimeID identifies the other Qoder edition, which owns a separate native root.
func QoderPeerRuntimeID(runtimeID string) string {
	switch runtimeID {
	case constants.RuntimeIDQoder:
		return constants.RuntimeIDQoderCN
	case constants.RuntimeIDQoderCN:
		return constants.RuntimeIDQoder
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
