package ai_accounts

import (
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// TestAccountDefaultModelMustBeSelected 验证默认值不会引用其他账号或已移除的模型。
func TestAccountDefaultModelMustBeSelected(t *testing.T) {
	for _, model := range []string{"", "selected"} {
		if err := validateDefaultModel([]string{"selected"}, model); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateDefaultModel([]string{"selected"}, "another-account-model"); !errors.Is(err, status_error.AIAccountConfigurationInvalid) {
		t.Fatalf("unselected default accepted: %v", err)
	}
}

func TestModelMappingsReferenceOnlySelectedAccountModels(t *testing.T) {
	selected := []string{"fast", "strong"}
	if err := validateModelMappings(selected,
		map[string]string{"fast": "快速模型"},
		map[string][]string{"strong": {"minimal", "low", "high"}},
		map[string]string{"opus": "strong"}); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []struct {
		// Names 是待校验的模型显示名映射。
		Names map[string]string
		// Levels 是待校验的模型推理等级映射。
		Levels map[string][]string
		// Roles 是待校验的 Claude 角色映射。
		Roles map[string]string
	}{
		{Names: map[string]string{"other": "外部账号模型"}},
		{Levels: map[string][]string{"fast": {"bad\nvalue"}}},
		{Roles: map[string]string{"opus": "other"}},
		{Roles: map[string]string{"unknown": "fast"}},
	} {
		if err := validateModelMappings(selected, invalid.Names, invalid.Levels, invalid.Roles); err == nil {
			t.Fatalf("accepted invalid account model mapping: %+v", invalid)
		}
	}
}

// TestOpaqueNativeReasoningIDs 保证原生预算、自定义输入 ID 可保存，同时拒绝控制字符和过长值。
func TestOpaqueNativeReasoningIDs(t *testing.T) {
	for _, level := range []string{"default", "dynamic", "off", "budget:8192", "LOW", "provider-fast"} {
		if err := validateModelMappings([]string{"model"}, nil, map[string][]string{"model": {level}}, nil); err != nil {
			t.Fatalf("native choice %q rejected: %v", level, err)
		}
	}
	for _, level := range []string{"", "bad\nvalue", " value", "bad/value"} {
		if err := validateModelMappings([]string{"model"}, nil, map[string][]string{"model": {level}}, nil); err == nil {
			t.Fatalf("unsafe choice %q accepted", level)
		}
	}
}
