package config

import (
	"slices"
	"testing"
)

func TestNormalizeDetectedDefaultsUsesSelectedModelReasoningOptions(t *testing.T) {
	detection := DetectionResult{
		DetectedDefaultModel:     "gpt-fast",
		DetectedDefaultReasoning: "medium",
		Models: []ModelOption{
			{
				ID:   "gpt-fast",
				Name: "GPT Fast",
				ReasoningOptions: []ReasoningOption{
					{ID: "medium", Name: "Medium"},
					{ID: "low", Name: "Low"},
				},
			},
			{
				ID:   "gpt-stable",
				Name: "GPT Stable",
				ReasoningOptions: []ReasoningOption{
					{ID: "high", Name: "High"},
				},
			},
		},
	}

	model, reasoning := NormalizeDetectedDefaults(
		detection,
		nil,
		"gpt-fast",
		"medium",
	)
	if model != "gpt-fast" || reasoning != "medium" {
		t.Fatalf("normalized defaults = %q/%q, want gpt-fast/medium", model, reasoning)
	}
	model, reasoning = NormalizeDetectedDefaults(detection, nil, "missing", "low")
	if model != "gpt-fast" || reasoning != "low" {
		t.Fatalf("stale defaults = %q/%q, want gpt-fast/low", model, reasoning)
	}
}

func TestNormalizeDetectedDefaultsKeepsCustomModel(t *testing.T) {
	detection := DetectionResult{
		DetectedDefaultModel:     "gpt-stable",
		DetectedDefaultReasoning: "high",
		Models: []ModelOption{
			{ID: "gpt-stable", Name: "GPT Stable"},
		},
		ReasoningOptions: []ReasoningOption{
			{ID: "high", Name: "High"},
		},
	}
	model, reasoning := NormalizeDetectedDefaults(
		detection,
		[]string{"gpt-team-custom"},
		"gpt-team-custom",
		"high",
	)
	if model != "gpt-team-custom" || reasoning != "high" {
		t.Fatalf(
			"custom defaults = %q/%q, want gpt-team-custom/high",
			model,
			reasoning,
		)
	}
}

func TestSafePermissionLevelKeepsPreferredWhenCatalogIsEmpty(t *testing.T) {
	got, degraded := SafePermissionLevel(PermissionLevelFullAccess, nil)
	if got != PermissionLevelFullAccess || degraded {
		t.Fatalf("empty catalog = %q degraded=%v, want full-access/false", got, degraded)
	}
}

func TestSafePermissionLevelDowngradesToAutoThenDefault(t *testing.T) {
	got, degraded := SafePermissionLevel(
		PermissionLevelFullAccess,
		[]PermissionLevel{PermissionLevelDefault, PermissionLevelAuto},
	)
	if got != PermissionLevelAuto || !degraded {
		t.Fatalf("full-access fallback = %q degraded=%v, want auto/true", got, degraded)
	}
	got, degraded = SafePermissionLevel(
		PermissionLevelFullAccess,
		[]PermissionLevel{PermissionLevelDefault},
	)
	if got != PermissionLevelDefault || !degraded {
		t.Fatalf("auto missing fallback = %q degraded=%v, want default/true", got, degraded)
	}
	got, degraded = SafePermissionLevel(
		PermissionLevelAuto,
		[]PermissionLevel{PermissionLevelAuto},
	)
	if got != PermissionLevelAuto || degraded {
		t.Fatalf("supported auto = %q degraded=%v, want auto/false", got, degraded)
	}
}

func TestModelPermissionLevelsReturnsDeclaredLevelsOnly(t *testing.T) {
	detection := DetectionResult{
		DetectedDefaultModel: "gpt-stable",
		Models: []ModelOption{{
			ID:   "gpt-stable",
			Name: "GPT Stable",
			PermissionLevels: []PermissionLevel{
				PermissionLevelDefault,
				PermissionLevelAuto,
			},
		}},
	}
	got := ModelPermissionLevels(detection, nil, "gpt-stable")
	if !slices.Equal(got, []PermissionLevel{
		PermissionLevelDefault,
		PermissionLevelAuto,
	}) {
		t.Fatalf("declared levels = %#v", got)
	}
	if got := ModelPermissionLevels(detection, nil, "missing"); len(got) != 0 {
		t.Fatalf("missing model levels = %#v, want none", got)
	}
	if got := ModelPermissionLevels(DetectionResult{}, nil, ""); len(got) != 0 {
		t.Fatalf("empty catalog levels = %#v, want none", got)
	}
}

func TestPermissionLevelSupportedRequiresDeclaredLevel(t *testing.T) {
	if PermissionLevelSupported(nil, PermissionLevelAuto) {
		t.Fatal("empty catalog must not support any product level")
	}
	if !PermissionLevelSupported(
		[]PermissionLevel{PermissionLevelAuto},
		PermissionLevelAuto,
	) {
		t.Fatal("declared auto must be supported")
	}
}
