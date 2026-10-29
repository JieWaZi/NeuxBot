package catalog

import (
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"

	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestPrepareLaunchPreservesProfileAndAccountFacts 防止新宿主跳过 Profile、账号覆盖和真实探测程序。
func TestPrepareLaunchPreservesProfileAndAccountFacts(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "inherited-secret")
	sdk, err := New(Config{Profiles: []runtimecontract.ProfileSpec{{ID: "qoder-cn-profile", BaseRuntimeID: runtimecontract.RuntimeIDQoderCN, BinaryCandidates: []string{"profile-command"}, PrefixArgs: []string{"profile-prefix"}, Environment: map[string]string{"PROFILE_FACT": "retained"}}}})
	if err != nil {
		t.Fatal(err)
	}
	binary, _ := os.Executable()
	detection := readyDetectionForRun(t, sdk, "qoder-cn-profile", binary)
	home := filepath.Join(t.TempDir(), "home")
	facts, err := sdk.PrepareLaunch(runtimecontract.RuntimeTarget{RuntimeID: "qoder-cn-profile", ConfigHome: home, Isolated: true, Environment: map[string]string{"ACCOUNT_FACT": "selected"}}, detection)
	if err != nil {
		t.Fatal(err)
	}
	if facts.BaseRuntimeID != runtimecontract.RuntimeIDQoderCN || facts.Executable != binary || !slices.Equal(facts.PrefixArgs, []string{"profile-prefix"}) || !slices.Contains(facts.Environment, "PROFILE_FACT=retained") || !slices.Contains(facts.Environment, "ACCOUNT_FACT=selected") {
		t.Fatalf("launch facts lost: %+v", facts)
	}
	for _, value := range facts.Environment {
		if value == "OPENAI_API_KEY=inherited-secret" {
			t.Fatal("inherited credential retained")
		}
	}
	if !slices.Contains(facts.Environment, "QODERCN_CONFIG_DIR="+home) {
		t.Fatalf("CN config root missing: %+v", facts.Environment)
	}
}

// TestPrepareLaunchAllCLIProfilesMapConfigHomes 验证所有现有 CLI 与 Profile 保留各自配置根和启动覆盖。
func TestPrepareLaunchAllCLIProfilesMapConfigHomes(t *testing.T) {
	for _, tt := range []struct {
		base     string // base 是内置协议身份。
		variable string // variable 是该 CLI 原有配置根变量，空值表示无目录映射。
	}{{"codex", "CODEX_HOME"}, {"claude", "CLAUDE_CONFIG_DIR"}, {"kimi", ""}, {"cursor", ""}, {"pi", "PI_CODING_AGENT_DIR"}, {"opencode", "OPENCODE_CONFIG_DIR"}, {"gemini", "GEMINI_CLI_HOME"}, {"grok", "GROK_HOME"}, {"workbuddy", "CODEBUDDY_CONFIG_DIR"}, {"qoder", "QODER_CONFIG_DIR"}, {"qoder-cn", "QODERCN_CONFIG_DIR"}} {
		t.Run(tt.base, func(t *testing.T) {
			id := "profile-" + tt.base
			sdk, err := New(Config{Profiles: []runtimecontract.ProfileSpec{{ID: id, BaseRuntimeID: tt.base, PrefixArgs: []string{"profile-prefix"}, Environment: map[string]string{"PROFILE_FACT": "retained"}}}})
			if err != nil {
				t.Fatal(err)
			}
			binary, _ := os.Executable()
			home := t.TempDir()
			facts, err := sdk.PrepareLaunch(runtimecontract.RuntimeTarget{RuntimeID: id, PrefixArgs: []string{"selected-prefix"}, ConfigHome: home, Environment: map[string]string{"ACCOUNT_FACT": "selected"}}, readyDetectionForRun(t, sdk, id, binary))
			if err != nil {
				t.Fatal(err)
			}
			if facts.BaseRuntimeID != tt.base || facts.Executable != binary || !slices.Equal(facts.PrefixArgs, []string{"selected-prefix"}) || !slices.Contains(facts.Environment, "PROFILE_FACT=retained") || !slices.Contains(facts.Environment, "ACCOUNT_FACT=selected") {
				t.Fatalf("launch=%+v", facts)
			}
			if tt.variable != "" && !slices.Contains(facts.Environment, tt.variable+"="+home) {
				t.Fatalf("config home lost: %+v", facts.Environment)
			}
		})
	}
}
