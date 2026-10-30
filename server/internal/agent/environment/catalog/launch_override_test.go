package catalog

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"
)

import "testing"

func TestLaunchOverrideArgumentsAllowedRejectsAllyManagedArguments(t *testing.T) {
	tests := []struct {
		// name 是测试场景名称。
		name string
		// runtimeID 是参数所属的内置 Runtime。
		runtimeID string
		// arguments 是用户提交的前置参数。
		arguments []string
		// allowed 是期望的参数校验结论。
		allowed bool
	}{
		{name: "codex safe profile", runtimeID: runtimeconfig.RuntimeIDCodex, arguments: []string{"--profile", "ally"}, allowed: true},
		{name: "codex workspace", runtimeID: runtimeconfig.RuntimeIDCodex, arguments: []string{"--cd=/tmp/project"}, allowed: false},
		{name: "codex permission", runtimeID: runtimeconfig.RuntimeIDCodex, arguments: []string{"--sandbox", "read-only"}, allowed: false},
		{name: "claude safe setting", runtimeID: runtimeconfig.RuntimeIDClaude, arguments: []string{"--setting-sources", "user"}, allowed: true},
		{name: "claude protocol", runtimeID: runtimeconfig.RuntimeIDClaude, arguments: []string{"--output-format=stream-json"}, allowed: false},
		{name: "claude permission", runtimeID: runtimeconfig.RuntimeIDClaude, arguments: []string{"--permission-mode", "plan"}, allowed: false},
		{name: "unknown runtime", runtimeID: "unknown", arguments: nil, allowed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := LaunchOverrideArgumentsAllowed(test.runtimeID, test.arguments); actual != test.allowed {
				t.Fatalf("LaunchOverrideArgumentsAllowed() = %v, want %v", actual, test.allowed)
			}
		})
	}
}

func TestLaunchDetectInputsEqualIgnoresNonLaunchFields(t *testing.T) {
	previous := LaunchDetectInput{
		Binary:     "codex",
		PrefixArgs: []string{"--profile", "ally"},
		ConfigHome: "/tmp/codex-home",
	}
	next := previous
	if !LaunchDetectInputsEqual(previous, next) {
		t.Fatal("identical launch inputs should compare equal")
	}
	next.PrefixArgs = []string{"--profile", "other"}
	if LaunchDetectInputsEqual(previous, next) {
		t.Fatal("prefix args change should require detect refresh")
	}
}
