package accountconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// ClaudeSystemProvider contains the explicit provider settings selected for a system launch.
type ClaudeSystemProvider struct {
	// Environment projects only the selected provider's credential and model settings.
	Environment map[string]string
	// CredentialVariable identifies the exact credential mechanism, or empty for native login.
	CredentialVariable string
	// ModelFingerprint separates model configuration changes from credential identity.
	ModelFingerprint string
}

// SelectClaudeSystemProvider overlays native user settings on the host's provider
// environment. Claude 2.1.159 assigns settings.env after the inherited environment.
func SelectClaudeSystemProvider(settings []byte, lookup func(string) string) (ClaudeSystemProvider, error) {
	var native struct {
		// Environment contains native values scoped to the selected user settings file.
		Environment map[string]json.RawMessage `json:"env"`
	}
	if len(settings) != 0 {
		if bytes.Equal(bytes.TrimSpace(settings), []byte("null")) || json.Unmarshal(settings, &native) != nil {
			return ClaudeSystemProvider{}, status_error.AIAccountConfigurationInvalid
		}
	}
	read := func(key string) (string, error) {
		raw, selected := native.Environment[key]
		if !selected {
			return lookup(key), nil
		}
		var value *string
		if json.Unmarshal(raw, &value) != nil || value == nil {
			return "", status_error.AIAccountConfigurationInvalid
		}
		return *value, nil
	}

	result := ClaudeSystemProvider{Environment: make(map[string]string)}
	for _, key := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN"} {
		value, err := read(key)
		if err != nil {
			return ClaudeSystemProvider{}, err
		}
		if value == "" {
			continue
		}
		if result.CredentialVariable != "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n") {
			return ClaudeSystemProvider{}, status_error.AIAccountConfigurationInvalid
		}
		result.CredentialVariable = key
		result.Environment[key] = value
	}
	baseURL, err := read("ANTHROPIC_BASE_URL")
	if err != nil {
		return ClaudeSystemProvider{}, err
	}
	if baseURL != "" {
		if result.CredentialVariable == "" || strings.TrimSpace(baseURL) != baseURL {
			return ClaudeSystemProvider{}, status_error.AIAccountConfigurationInvalid
		}
		if err := ValidateEndpoint(constants.AccountProtocolAnthropicMessages, baseURL); err != nil {
			return ClaudeSystemProvider{}, err
		}
		result.Environment["ANTHROPIC_BASE_URL"] = baseURL
	}
	if result.CredentialVariable != "" {
		// Claude's host-owned provider mode prevents global or scoped settings
		// from replacing the projected key, endpoint or default model.
		result.Environment["CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST"] = "1"
	}

	modelKeys := []string{"ANTHROPIC_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL",
		"ANTHROPIC_SMALL_FAST_MODEL", "ANTHROPIC_CUSTOM_MODEL_OPTION",
		"ANTHROPIC_CUSTOM_MODEL_OPTION_NAME", "ANTHROPIC_CUSTOM_MODEL_OPTION_DESCRIPTION",
		"ANTHROPIC_CUSTOM_MODEL_OPTION_SUPPORTED_CAPABILITIES"}
	for _, role := range []string{"OPUS", "SONNET", "HAIKU", "FABLE"} {
		key := "ANTHROPIC_DEFAULT_" + role + "_MODEL"
		modelKeys = append(modelKeys, key, key+"_NAME", key+"_DESCRIPTION", key+"_SUPPORTED_CAPABILITIES")
	}
	models := make(map[string]string)
	for _, key := range modelKeys {
		value, err := read(key)
		if err != nil {
			return ClaudeSystemProvider{}, err
		}
		if value != "" {
			models[key] = value
			result.Environment[key] = value
		}
	}
	if len(models) != 0 {
		encoded, _ := json.Marshal(models)
		sum := sha256.Sum256(encoded)
		result.ModelFingerprint = hex.EncodeToString(sum[:])
	}
	return result, nil
}
