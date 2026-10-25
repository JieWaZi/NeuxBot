package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeAuthenticationOnlyReplacesBoundHeaderConfiguration(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"logical"}`)
	fixtureFile(t, root, "bridge.mjs", "// entry")
	fixtureFile(t, root, ".neuxally/integration.json", `{"nativePlugins":{"claude":{"path":"native"}},"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"node","args":["bridge.mjs"],"bindings":[{"runtime":"claude","server":"service","url":"https://example.invalid/mcp/","header":"Authorization"}]}}`)
	fixtureFile(t, root, "native/.claude-plugin/plugin.json", `{"name":"target"}`)
	fixtureFile(t, root, "native/.mcp.json", `{"service":{"type":"http","url":"https://example.invalid/mcp/","headers":{"Authorization":"Bearer ${OLD_TOKEN}","X-Other":"${OTHER}"}},"other":{"type":"http","url":"https://other.invalid/mcp","headers":{"Authorization":"Bearer ${SHARED}"}}}`)
	pkg, err := LoadSource(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	ext, err := ClientExtension(pkg)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, f := range ext.Config {
		keys[f.Key] = true
	}
	if keys["OLD_TOKEN"] || !keys["OTHER"] || !keys["SHARED"] {
		t.Fatalf("incorrect bound config: %+v", ext.Config)
	}
	selected, selectedRoot, err := SelectTarget(root, pkg, "claude")
	expectedRoot, _ := filepath.EvalSymlinks(filepath.Join(root, "native"))
	if err != nil || selectedRoot != expectedRoot {
		t.Fatalf("selection: %s %v", selectedRoot, err)
	}
	for _, server := range selected.Servers {
		if server.Key == "service" && server.Headers["Authorization"] != "" {
			t.Fatal("bound header not removed from runtime resolution")
		}
	}
}

func TestCanonicalPackageBindsOwnMCPForBothNativeTargets(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "plugins/github/.codex-plugin/plugin.json", `{"name":"github","version":"0.1.0"}`)
	fixtureFile(t, root, "plugins/github/.mcp.json", `{"mcpServers":{"github":{"type":"http","url":"https://api.githubcopilot.com/mcp/","headers":{"X-Business":"one"}}}}`)
	fixtureFile(t, root, "plugins/github/bridge.mjs", "// entry")
	fixtureFile(t, root, "plugins/github/.neuxally/integration.json", `{"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"node","args":["bridge.mjs"],"bindings":[{"runtime":"codex","server":"github","url":"https://api.githubcopilot.com/mcp/","header":"Authorization"},{"runtime":"claude","server":"github","url":"https://api.githubcopilot.com/mcp/","header":"Authorization"}]}}`)
	guide := "---\nname: review\ndescription: Review changes\n---\n# Canonical skill\nComplete business instructions.\n"
	fixtureFile(t, root, "plugins/github/skills/review/SKILL.md", guide)
	pkg, err := LoadSource(root, "plugins/github")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Servers) != 1 || len(pkg.Skills) != 1 || len(pkg.Integration.NativePlugins) != 0 || len(pkg.NativeImplementations) != 0 {
		t.Fatal("canonical capability collection was duplicated or discarded")
	}
	extension, err := ClientExtension(pkg)
	if err != nil || len(extension.Config) != 0 {
		t.Fatal("canonical bridge unexpectedly needs PAT configuration", err)
	}
	for _, cli := range []string{"codex", "claude"} {
		t.Run(cli, func(t *testing.T) {
			selected, selectedRoot, err := SelectTarget(root, pkg, cli)
			if err != nil || len(selected.Servers) != 1 {
				t.Fatal("canonical target selection failed", err)
			}
			bindings := TargetAuthenticationBindings(pkg, cli)
			target, err := PrepareNativeTarget(t.TempDir(), "canonical", selected, selectedRoot, cli, bindings...)
			if err != nil {
				t.Fatal(err)
			}
			derived, err := Load(target.Root)
			if err != nil || len(derived.Servers) != 1 {
				t.Fatal("canonical native target missing its own MCP", err)
			}
			_, body, err := ReadSkill(target.Root, derived, "skills/review")
			if err != nil || body != guide {
				t.Fatal("canonical native target lost full skill text", err)
			}
			manifest, _ := os.ReadFile(filepath.Join(target.Root, "."+cli+"-plugin/plugin.json"))
			mcp, _ := os.ReadFile(filepath.Join(target.Root, ".mcp.json"))
			joined := string(manifest) + string(mcp)
			key := AuthenticationEnvironmentKey("canonical", bindings[0])
			if !strings.Contains(joined, key) || (cli == "codex" && !strings.Contains(joined, "env_http_headers")) || (cli == "claude" && !strings.Contains(joined, "${"+key+"}")) {
				t.Fatal("canonical native authentication uses the wrong target syntax")
			}
		})
	}
}

func TestCanonicalBindingValidationAndUnboundConfiguration(t *testing.T) {
	for _, binding := range []string{
		`{"runtime":"codex","server":"missing","url":"https://example.invalid/mcp/","header":"Authorization"}`,
		`{"runtime":"claude","server":"service","url":"https://other.invalid/mcp/","header":"Authorization"}`,
		`{"runtime":"unknown","server":"service","url":"https://example.invalid/mcp/","header":"Authorization"}`,
		`{"runtime":"codex","server":"service","url":"https://example.invalid/mcp/","header":"invalid header"}`,
	} {
		t.Run(binding, func(t *testing.T) {
			root := t.TempDir()
			fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"canonical"}`)
			fixtureFile(t, root, "bridge.mjs", "// entry")
			fixtureFile(t, root, ".mcp.json", `{"mcpServers":{"service":{"type":"http","url":"https://example.invalid/mcp/"}}}`)
			fixtureFile(t, root, ".neuxally/integration.json", `{"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"node","args":["bridge.mjs"],"bindings":[`+binding+`]}}`)
			if _, err := LoadSource(root, "."); err == nil {
				t.Fatal("canonical binding escaped its actual runtime/server/endpoint/header")
			}
		})
	}
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"canonical"}`)
	fixtureFile(t, root, "bridge.mjs", "// entry")
	fixtureFile(t, root, ".mcp.json", `{"mcpServers":{"service":{"type":"http","url":"https://example.invalid/mcp/","headers":{"Authorization":"Bearer ${BOUND}","X-Unbound":"${UNBOUND}"}}}}`)
	fixtureFile(t, root, ".neuxally/integration.json", `{"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"node","args":["bridge.mjs"],"bindings":[{"runtime":"codex","server":"service","url":"https://example.invalid/mcp/","header":"Authorization"}]}}`)
	pkg, err := LoadSource(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	selected, _, err := SelectTarget(root, pkg, "codex")
	if err != nil || selected.Servers[0].Headers["Authorization"] != "" || selected.Servers[0].Headers["X-Unbound"] != "${UNBOUND}" {
		t.Fatal("self binding did not exclusively replace its own header", err)
	}
	if err = ValidateValues(selected, nil, nil); err == nil {
		t.Fatal("unbound configuration was exempted")
	}
	other, _, err := SelectTarget(root, pkg, "claude")
	if err != nil || other.Servers[0].Headers["Authorization"] != "Bearer ${BOUND}" {
		t.Fatal("another runtime borrowed a self binding", err)
	}
}

func TestAuthenticationRejectsUnknownPreferredAction(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"logical"}`)
	fixtureFile(t, root, "bridge.mjs", "// entry")
	fixtureFile(t, root, ".neuxally/integration.json", `{"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"node","args":["bridge.mjs"],"preferredAction":"invented"}}`)
	if _, err := Load(root); err == nil {
		t.Fatal("unknown interaction accepted")
	}
}

func TestNativeAuthenticationDerivesTemplatesWithoutChangingSourceOrApps(t *testing.T) {
	for _, cli := range []string{"codex", "claude"} {
		t.Run(cli, func(t *testing.T) {
			root := t.TempDir()
			fixtureFile(t, root, "."+cli+"-plugin/plugin.json", `{"name":"target","version":"1"}`)
			original := `{"mcpServers":{"service":{"type":"http","url":"https://example.invalid/mcp/","headers":{"Authorization":"Bearer ${OLD_TOKEN}","X-Other":"fixed"},"oauth":{"clientId":"public-client"}}}}`
			fixtureFile(t, root, ".mcp.json", original)
			apps := `{"apps":{"other":{"id":"hosted-app","required":false}}}`
			fixtureFile(t, root, ".app.json", apps)
			pkg, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			binding := AuthenticationBinding{Runtime: cli, Server: "service", URL: "https://example.invalid/mcp/", Header: "Authorization"}
			target, err := PrepareNativeTarget(t.TempDir(), "resource", pkg, root, cli, binding)
			if err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(filepath.Join(target.Root, ".mcp.json"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), "OLD_TOKEN") || strings.Contains(string(body), "oauth") || !strings.Contains(string(body), "ALLY_PLUGIN_AUTH_") || !strings.Contains(string(body), "X-Other") {
				t.Fatalf("invalid derived template: %s", body)
			}
			var document struct {
				// Servers 是派生目标实际读取的服务声明。
				Servers map[string]struct {
					// Headers 是 Claude 解释的请求头模板。
					Headers map[string]string `json:"headers"`
					// EnvironmentHeaders 是 Codex 解释的请求头环境变量引用。
					EnvironmentHeaders map[string]string `json:"env_http_headers"`
				} `json:"mcpServers"`
			}
			if err := json.Unmarshal(body, &document); err != nil {
				t.Fatal(err)
			}
			server := document.Servers["service"]
			environmentKey := AuthenticationEnvironmentKey("resource", binding)
			if cli == "codex" && (server.EnvironmentHeaders["Authorization"] != environmentKey || server.Headers["Authorization"] != "") {
				t.Fatal("Codex must resolve a named environment header, not a Claude template")
			}
			if cli == "claude" && (server.Headers["Authorization"] != "${"+environmentKey+"}" || len(server.EnvironmentHeaders) != 0) {
				t.Fatal("Claude must resolve its standard header template")
			}
			source, _ := os.ReadFile(filepath.Join(root, ".mcp.json"))
			if string(source) != original {
				t.Fatal("source changed")
			}
			derivedApps, _ := os.ReadFile(filepath.Join(target.Root, ".app.json"))
			if string(derivedApps) != apps {
				t.Fatal("unrelated hosted app changed")
			}
		})
	}
}

func TestCodexAuthenticationPreservesUnboundNativeHeaders(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"target","version":"1"}`)
	original := `{"mcpServers":{"service":{"type":"http","url":"https://example.invalid/mcp/","headers":{"authorization":"legacy","X-Legacy":"fixed"},"http_headers":{"AUTHORIZATION":"stale","X-Native":"native-fixed"},"env_http_headers":{"Authorization":"OLD_NATIVE_ENV","X-Environment":"OTHER_NATIVE_ENV"},"bearer_token_env_var":"OLD_BEARER"}}}`
	fixtureFile(t, root, ".mcp.json", original)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	binding := AuthenticationBinding{Runtime: "codex", Server: "service", URL: "https://example.invalid/mcp/", Header: "Authorization"}
	target, err := PrepareNativeTarget(t.TempDir(), "resource", pkg, root, "codex", binding)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(target.Root, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, stale := range []string{"legacy\"", "stale", "OLD_NATIVE_ENV", "OLD_BEARER"} {
		if strings.Contains(string(body), stale) {
			t.Fatalf("bound native credential remains: %s", stale)
		}
	}
	for _, preserved := range []string{"X-Legacy", "X-Native", "native-fixed", "X-Environment", "OTHER_NATIVE_ENV"} {
		if !strings.Contains(string(body), preserved) {
			t.Fatalf("unbound header lost: %s", preserved)
		}
	}
	var document map[string]map[string]map[string]json.RawMessage
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatal(err)
	}
	var headers map[string]string
	if err := json.Unmarshal(document["mcpServers"]["service"]["env_http_headers"], &headers); err != nil || headers["Authorization"] != AuthenticationEnvironmentKey("resource", binding) {
		t.Fatal("native environment header missing")
	}
	source, _ := os.ReadFile(filepath.Join(root, ".mcp.json"))
	if string(source) != original {
		t.Fatal("source changed")
	}
}

func TestAuthenticationBindingRejectsTargetAndEndpointMismatch(t *testing.T) {
	for _, value := range []string{
		`{"runtime":"claude","server":"missing","url":"https://example.invalid/mcp/","header":"Authorization"}`,
		`{"runtime":"claude","server":"service","url":"https://other.invalid/mcp/","header":"Authorization"}`,
		`{"runtime":"codex","server":"service","url":"https://example.invalid/mcp/","header":"Authorization"}`,
		`{"runtime":"claude","server":"service","url":"http://example.invalid/mcp/","header":"Authorization"}`,
		`{"runtime":"claude","server":"service","url":"https://example.invalid/mcp/","header":"bad header"}`,
	} {
		t.Run(value, func(t *testing.T) {
			root := t.TempDir()
			fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"logical"}`)
			fixtureFile(t, root, "bridge.mjs", "// entry")
			fixtureFile(t, root, ".neuxally/integration.json", `{"nativePlugins":{"claude":{"path":"native"}},"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"node","args":["bridge.mjs"],"bindings":[`+value+`]}}`)
			fixtureFile(t, root, "native/.claude-plugin/plugin.json", `{"name":"target","mcpServers":{"service":{"type":"http","url":"https://example.invalid/mcp/"}}}`)
			if _, err := LoadSource(root, "."); err == nil {
				t.Fatal("invalid auth target accepted")
			}
		})
	}
}

func TestBoundHeaderDoesNotExemptSamePlaceholderElsewhere(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"logical"}`)
	fixtureFile(t, root, "bridge.mjs", "// entry")
	fixtureFile(t, root, ".neuxally/integration.json", `{"nativePlugins":{"claude":{"path":"native"}},"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"node","args":["bridge.mjs"],"bindings":[{"runtime":"claude","server":"service","url":"https://example.invalid/mcp/","header":"Authorization"}]}}`)
	fixtureFile(t, root, "native/.claude-plugin/plugin.json", `{"name":"target","mcpServers":{"service":{"type":"http","url":"https://example.invalid/mcp/","headers":{"Authorization":"Bearer ${SHARED}","X-Other":"${SHARED}"}}}}`)
	logical, err := LoadSource(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	selected, _, err := SelectTarget(root, logical, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateValues(selected, nil, nil); err == nil {
		t.Fatal("same variable in unbound header became optional")
	}
}

func TestBoundAuthenticationRejectsCompetingTargetBridge(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"logical"}`)
	fixtureFile(t, root, "bridge.mjs", "// entry")
	fixtureFile(t, root, ".neuxally/integration.json", `{"nativePlugins":{"claude":{"path":"native"}},"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"node","args":["bridge.mjs"],"bindings":[{"runtime":"claude","server":"service","url":"https://example.invalid/mcp/","header":"Authorization"}]}}`)
	fixtureFile(t, root, "native/.claude-plugin/plugin.json", `{"name":"target","mcpServers":{"service":{"type":"http","url":"https://example.invalid/mcp/"}}}`)
	fixtureFile(t, root, "native/bridge.mjs", "// different identity")
	fixtureFile(t, root, "native/.neuxally/integration.json", `{"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"node","args":["bridge.mjs"]}}`)
	if _, err := LoadSource(root, "."); err == nil {
		t.Fatal("target authentication silently replaced")
	}
}

func TestNativeAuthenticationPreservesExplicitConversionAndInlineMCP(t *testing.T) {
	for _, layout := range []string{"file", "inline"} {
		t.Run(layout, func(t *testing.T) {
			root := t.TempDir()
			servers := `{"service":{"type":"http","url":"https://example.invalid/mcp/","headers":{"Authorization":"Bearer ${OLD_TOKEN}"},"oauth":{"clientId":"public-client"}}}`
			manifest := `{"name":"target","version":"1"}`
			if layout == "inline" {
				manifest = `{"name":"target","version":"1","mcpServers":` + servers + `}`
			} else {
				fixtureFile(t, root, ".mcp.json", `{"mcpServers":`+servers+`}`)
			}
			fixtureFile(t, root, ".codex-plugin/plugin.json", manifest)
			pkg, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			target, err := PrepareNativeTarget(t.TempDir(), "p", pkg, root, "claude", AuthenticationBinding{Runtime: "claude", Server: "service", URL: "https://example.invalid/mcp/", Header: "Authorization"})
			if err != nil {
				t.Fatal(err)
			}
			manifestRaw, err := readObject(filepath.Join(target.Root, ".claude-plugin/plugin.json"))
			if err != nil {
				t.Fatal(err)
			}
			var serversRaw map[string]Server
			if err := json.Unmarshal(manifestRaw["mcpServers"], &serversRaw); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(serversRaw["service"].Headers["Authorization"], "${ALLY_PLUGIN_AUTH_") {
				t.Fatal("converted authentication missing")
			}
		})
	}
}

func TestSelfBoundHeaderDoesNotRemainRequiredConfiguration(t *testing.T) {
	for _, tc := range []struct {
		// Name 是位置覆盖场景。
		Name string
		// Extra 是仍须手填的同名变量使用位置。
		Extra string
		// Both 表示两个自身运行目标均接管请求头。
		Both bool
		// Required 表示逻辑安装仍须手填变量。
		Required bool
	}{
		{Name: "both bound", Both: true},
		{Name: "one target unbound", Required: true},
		{Name: "same variable in other header", Both: true, Extra: `,"X-Other":"${OLD_TOKEN}"`, Required: true},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			root := t.TempDir()
			fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"canonical"}`)
			fixtureFile(t, root, "bridge.mjs", "// entry")
			fixtureFile(t, root, ".mcp.json", `{"mcpServers":{"service":{"type":"http","url":"https://example.invalid/mcp/","headers":{"Authorization":"Bearer ${OLD_TOKEN}"`+tc.Extra+`}}}}`)
			binding := `{"runtime":"codex","server":"service","url":"https://example.invalid/mcp/","header":"Authorization"}`
			if tc.Both {
				binding += `,{"runtime":"claude","server":"service","url":"https://example.invalid/mcp/","header":"Authorization"}`
			}
			fixtureFile(t, root, ".neuxally/integration.json", `{"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"node","args":["bridge.mjs"],"bindings":[`+binding+`]}}`)
			pkg, err := LoadSource(root, ".")
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateValues(pkg, nil, nil); (err != nil) != tc.Required {
				t.Fatalf("required=%v, validation=%v", tc.Required, err)
			}
		})
	}
}

func TestCodexUnboundHeaderVariableUsesEnvironmentField(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"canonical"}`)
	original := `{"mcpServers":{"service":{"type":"http","url":"https://example.invalid/mcp/","headers":{"X-Key":"${OTHER}","X-Composite":"prefix ${OTHER}:${MORE}","X-Static":"fixed","X-Priority":"${OTHER}","X-Env-Priority":"${OTHER}"},"http_headers":{"x-priority":"native-fixed"},"env_http_headers":{"x-env-priority":"EXISTING"}}}}`
	fixtureFile(t, root, ".mcp.json", original)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	native, err := PrepareNativeTarget(t.TempDir(), "p", pkg, root, "codex", AuthenticationBinding{Runtime: "codex", Server: "service", URL: "https://example.invalid/mcp/", Header: "Authorization"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(native.Root, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var headers, env map[string]string
	if err := json.Unmarshal(doc["mcpServers"]["service"]["http_headers"], &headers); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(doc["mcpServers"]["service"]["env_http_headers"], &env); err != nil {
		t.Fatal(err)
	}
	if headers["X-Key"] != "" || headers["X-Composite"] != "" || headers["X-Static"] != "fixed" || headers["x-priority"] != "native-fixed" || headers["X-Priority"] != "" || headers["X-Env-Priority"] != "" {
		t.Fatalf("incorrect header routing: %v", headers)
	}
	if !strings.HasPrefix(env["X-Key"], "ALLY_PLUGIN_AUTH_") || !strings.HasPrefix(env["X-Composite"], "ALLY_PLUGIN_AUTH_") || env["x-env-priority"] != "EXISTING" {
		t.Fatalf("incorrect environment headers: %v", env)
	}
	environment, err := native.ConfigurationEnvironment(map[string]string{"OTHER": "secret-header-sentinel", "MORE": "second-value"})
	if err != nil {
		t.Fatal(err)
	}
	if environment[env["X-Key"]] != "secret-header-sentinel" || environment[env["X-Composite"]] != "prefix secret-header-sentinel:second-value" {
		t.Fatal("runtime template lost its value or composition")
	}
	if _, err := native.ConfigurationEnvironment(map[string]string{"OTHER": "one"}); err == nil {
		t.Fatal("missing composite variable did not fail")
	}
	if strings.Contains(string(raw), "secret-header-sentinel") {
		t.Fatal("secret persisted in native file")
	}
	serialized, err := json.Marshal(native)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), "headerTemplates") || strings.Contains(string(serialized), "secret-header-sentinel") {
		t.Fatal("private template state serialized")
	}
	source, _ := os.ReadFile(filepath.Join(root, ".mcp.json"))
	if string(source) != original {
		t.Fatal("immutable source changed")
	}
}

func TestCodexMultipleBoundHeadersDoNotRetainConfigurationTemplates(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"canonical"}`)
	fixtureFile(t, root, ".mcp.json", `{"mcpServers":{"service":{"type":"http","url":"https://example.invalid/mcp/","headers":{"Authorization":"Bearer ${OLD_TOKEN}","X-Key":"${OLD_KEY}"}}}}`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	native, err := PrepareNativeTarget(t.TempDir(), "p", pkg, root, "codex", AuthenticationBinding{Runtime: "codex", Server: "service", URL: "https://example.invalid/mcp/", Header: "Authorization"}, AuthenticationBinding{Runtime: "codex", Server: "service", URL: "https://example.invalid/mcp/", Header: "X-Key"})
	if err != nil {
		t.Fatal(err)
	}
	environment, err := native.ConfigurationEnvironment(nil)
	if err != nil || len(environment) > 0 {
		t.Fatalf("bound header leaked into ordinary configuration: %v %v", environment, err)
	}
}
