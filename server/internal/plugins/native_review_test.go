package plugins

import (
	"encoding/json"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestGeneratedEntrypointCannotWriteThroughSourceSymlink(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "safe-link"})
	if err := os.Rename(filepath.Join(root, "plugin.json"), filepath.Join(root, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "manifest.json"), filepath.Join(root, "plugin.json")); err != nil {
		t.Skip(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	before, err := ContentDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	target, prepareErr := PrepareNativeTarget(t.Context(), t.TempDir(), "p", pkg, root, "grok")
	after, err := ContentDigest(root)
	if err != nil || after != before {
		t.Fatalf("derived write modified immutable source: %v", err)
	}
	if prepareErr != nil {
		t.Fatalf("valid internal absolute link not normalized: %v", prepareErr)
	}
	if info, err := os.Lstat(filepath.Join(target.Root, "plugin.json")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("generated entry was not atomically replaced", err)
	}
	stage := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(stage, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := writeTargetJSON(stage, "alias/generated.json", map[string]any{"format": "test"}); err == nil {
		t.Fatal("generated parent alias followed outside root")
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatal("failed generation modified linked directory", err)
	}
}

func TestPiCommandReferencesAndUnsupportedTransportUseProjection(t *testing.T) {
	for _, transport := range []string{"streamable-http", "sse"} {
		root := t.TempDir()
		standardInput(t, root, map[string]any{"name": "pi-literal"})
		server := map[string]any{"type": transport, "url": "https://example.test/mcp", "headers": map[string]string{"X-Literal": "!never-execute"}}
		data, _ := json.Marshal(map[string]any{"$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json", "mcpServers": map[string]any{"remote": server}})
		fixtureFile(t, root, "mcp.json", string(data))
		pkg, err := Load(root)
		if err != nil {
			t.Fatal(err)
		}
		target, err := PrepareNativeTarget(t.Context(), t.TempDir(), "p", pkg, root, "pi")
		if err != nil {
			t.Fatal(err)
		}
		if target.Format != "projection" {
			t.Fatalf("Pi would interpret literal data as a command: %s", target.Format)
		}
	}
}

func TestPiRuntimeEnvironmentCannotIntroduceCommandReferences(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "pi-env"})
	fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"streamable-http","url":"https://example.test/mcp","headers":{"Authorization":"${TOKEN}"}}}}`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	target, err := PrepareNativeTarget(t.Context(), t.TempDir(), "p", pkg, root, "pi")
	if err != nil {
		t.Fatal(err)
	}
	code := `const {pathToFileURL}=await import('node:url');const m=await import(pathToFileURL(process.argv[1]));let calls=0;try{m.default({registerMcpServer:()=>calls++});}catch{}process.stdout.write(String(calls));`
	cmd := exec.Command("node", "--input-type=module", "-e", code, filepath.Join(target.Root, "extensions/mcp.mjs"))
	cmd.Env = append(os.Environ(), "TOKEN=!never-execute")
	body, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err, string(body))
	}
	if string(body) != "0" {
		t.Fatal("runtime data reached Pi executable configuration", string(body))
	}
}

func TestPiIdentityCannotBeOverwrittenByNodeMetadata(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "stable-plugin", "version": "1.2.3"})
	fixtureFile(t, root, "package.json", `{"name":"helper-module","version":"9.9.9","dependencies":{"helper":"1.0.0"}}`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	target, err := PrepareNativeTarget(t.Context(), t.TempDir(), "p", pkg, root, "pi")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readObject(filepath.Join(target.Root, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !stringEquals(raw["name"], target.Name) || !stringEquals(raw["version"], target.Version) {
		t.Fatal("Pi and release identities disagree")
	}
}

func TestGrokUnsupportedTechnicalNameUsesProjection(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "vendor.plugin"})
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	target, err := PrepareNativeTarget(t.Context(), t.TempDir(), "p", pkg, root, "grok")
	if err != nil {
		t.Fatal(err)
	}
	if target.Format != "projection" || target.Name != "vendor.plugin" {
		t.Fatal("Grok invalid identity mislabeled native", target)
	}
}

func TestGeneratorRefusesStagingInsideRealSource(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "boundary"})
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	before, err := ContentDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareNativeTarget(t.Context(), filepath.Join(alias, "cache"), "p", pkg, root, "claude"); err == nil {
		t.Fatal("staging recursively created inside source")
	}
	after, err := ContentDigest(root)
	if err != nil || after != before {
		t.Fatal("rejected preparation changed source", err)
	}
}

func TestImplicitNativeRulesPoliciesAndWorkBuddyAliasAreReviewed(t *testing.T) {
	for path, body := range map[string]string{"rules/hidden.mdc": "extra rule", "policies/hidden.toml": "extra policy", ".workbuddy-plugin/plugin.json": `{"name":"reviewed","hooks":{"onStart":"execute"}}`} {
		root := t.TempDir()
		standardInput(t, root, map[string]any{"name": "reviewed"})
		fixtureFile(t, root, path, body)
		if _, err := Load(root); err == nil {
			t.Fatal("unmodeled auto-discovery accepted", path)
		}
	}
}

func TestGeminiSidecarIsRejectedAndNativeDefaultsAreResolved(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "gemini-invalid"})
	fixtureFile(t, root, "gemini-extension.json", `{"name":"gemini-invalid","mcpServers":"./sidecar.json"}`)
	fixtureFile(t, root, "sidecar.json", `{"mcpServers":{}}`)
	if _, err := Load(root); err == nil {
		t.Fatal("Gemini sidecar string accepted")
	}
	for _, cli := range []string{"cursor", "workbuddy"} {
		source := t.TempDir()
		standardInput(t, source, map[string]any{"name": "defaults"})
		fixtureFile(t, source, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"streamable-http","url":"https://example.test/mcp"}}}`)
		entry, _ := targetEntry(cli)
		fixtureFile(t, source, entry, `{"name":"defaults"}`)
		if cli == "workbuddy" {
			fixtureFile(t, source, ".mcp.json", `{"mcpServers":{"remote":{"type":"http","url":"https://example.test/mcp"}}}`)
		}
		if _, err := Load(source); err != nil {
			t.Fatal("valid default MCP rejected", cli, err)
		}
	}
}

func TestTargetBridgeHeadersUseFinalTargetSyntax(t *testing.T) {
	root := t.TempDir()
	binding := AuthenticationBinding{Server: "remote", URL: "https://example.com/mcp", Header: "Authorization"}
	authenticationInput(t, root, []AuthenticationBinding{binding}, nil)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	target, err := PrepareNativeTarget(t.Context(), t.TempDir(), "p", pkg, root, "opencode", binding)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(target.Root, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "${NEUXALLY_") {
		t.Fatal("bridge header kept foreign interpolation syntax")
	}
	var config map[string]any
	if json.Unmarshal(body, &config) != nil {
		t.Fatal("invalid JSON")
	}
	mcp, ok := config["mcp"].(map[string]any)
	if !ok || mcp["servers"] == nil {
		t.Fatal("OpenCode current MCP layout missing servers")
	}
	target, err = PrepareNativeTarget(t.Context(), t.TempDir(), "p", pkg, root, "gemini", binding)
	if err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(filepath.Join(target.Root, "gemini-extension.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"settings"`) {
		t.Fatal("Gemini private variables missing explicit environment settings")
	}
}

func TestCodexAbsoluteLinksBecomeRelocatableTargets(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "relocatable"})
	fixtureFile(t, root, "assets/content.txt", "content")
	if err := os.Symlink(filepath.Join(root, "assets/content.txt"), filepath.Join(root, "assets/alias.txt")); err != nil {
		t.Skip(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	sourceDigest, err := ContentDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	target, err := PrepareNativeTarget(t.Context(), t.TempDir(), "p", pkg, root, "codex")
	if err != nil {
		t.Fatal(err)
	}
	relocated := filepath.Join(t.TempDir(), "copy")
	if err := os.Mkdir(relocated, 0700); err != nil {
		t.Fatal(err)
	}
	if err := pluginArchive.CopyDirectory(target.Root, relocated); err != nil {
		t.Fatal(err)
	}
	if err := VerifyContent(relocated, target.Digest); err != nil {
		t.Fatal("returned native target cannot be relocated", err)
	}
	if target.SourceDigest != sourceDigest || target.Digest == sourceDigest {
		t.Fatal("normalized target lost distinct original source digest")
	}
}

func TestNativeSidecarOwnershipDoesNotDependOnEntryOrder(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "cross-sidecar"})
	fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"streamable-http","url":"https://example.test/mcp"}}}`)
	fixtureFile(t, root, ".claude-plugin/mcp.json", `{"mcpServers":{"remote":{"type":"http","url":"https://example.test/mcp"}}}`)
	fixtureFile(t, root, ".claude-plugin/plugin.json", `{"name":"cross-sidecar","mcpServers":{"remote":{"type":"http","url":"https://example.test/mcp"}}}`)
	fixtureFile(t, root, ".cursor-plugin/plugin.json", `{"name":"cross-sidecar","mcpServers":"./.claude-plugin/mcp.json"}`)
	for i := 0; i < 32; i++ {
		if _, err := Load(root); err != nil {
			t.Fatalf("cross-entry sidecar ownership depended on iteration order: %v", err)
		}
	}
}

func TestPositionDependentRelativeLinksAreRejectedAtSourceBoundary(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "relative-boundary"})
	fixtureFile(t, root, "assets/content.txt", "content")
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := ContentDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", filepath.Base(root), "assets/content.txt"), filepath.Join(root, "alias.txt")); err != nil {
		t.Skip(err)
	}
	if _, err := Load(root); err == nil {
		t.Error("Load accepted a path leaving the source root")
	}
	if _, err := ContentDigest(root); err == nil {
		t.Error("digest accepted a path leaving the source root")
	}
	if _, err := PrepareNativeTargetWithDigest(t.Context(), t.TempDir(), "p", pkg, root, "codex", digest); err == nil {
		t.Error("generator accepted a path leaving the source root")
	}
}

func TestKimiSSETransportAndExplicitNativeValidation(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "kimi-sse"})
	fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"sse","url":"https://example.test/sse"}}}`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	target, err := PrepareNativeTarget(t.Context(), t.TempDir(), "p", pkg, root, "kimi")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readObject(filepath.Join(target.Root, "kimi.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var servers map[string]map[string]any
	if json.Unmarshal(raw["mcpServers"], &servers) != nil {
		t.Fatal("missing Kimi servers")
	}
	// Kimi's consumer picks SSE from transport; a URL without it means HTTP.
	if servers["remote"]["transport"] != "sse" {
		t.Fatal("Kimi consumer would select HTTP instead of SSE", servers)
	}
	fixtureFile(t, root, "kimi.plugin.json", `{"name":"kimi-sse","mcpServers":{"remote":{"transport":"sse","url":"https://example.test/sse"}}}`)
	if _, err := Load(root); err != nil {
		t.Fatal("valid native Kimi SSE declaration rejected", err)
	}
	fixtureFile(t, root, "kimi.plugin.json", `{"name":"kimi-sse","mcpServers":{"remote":{"transport":"sse","url":"https://example.test/sse","authentication":{"type":"oauth"}}}}`)
	if _, err := Load(root); err == nil {
		t.Fatal("extra native authentication semantics silently discarded")
	}
	if err := os.Remove(filepath.Join(root, "kimi.plugin.json")); err != nil {
		t.Fatal(err)
	}
	target, err = PrepareNativeTarget(t.Context(), t.TempDir(), "p", pkg, root, "opencode")
	if err == nil && target.Format != "projection" {
		t.Fatal("OpenCode remote incorrectly exposed SSE as HTTP")
	}
}

func TestOpenCodeV2SSEUsesProjection(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "opencode-sse"})
	fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"sse","url":"https://example.test/sse"}}}`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	target, err := PrepareNativeTarget(t.Context(), t.TempDir(), "p", pkg, root, "opencode")
	if err != nil {
		t.Fatal(err)
	}
	if target.Format != "projection" {
		t.Fatal("OpenCode V2 consumer would treat SSE as Streamable HTTP")
	}
}

func TestKimiNativeAuthenticationDifferenceIsRejected(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "kimi-auth-difference"})
	fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"streamable-http","url":"https://example.test/mcp"}}}`)
	fixtureFile(t, root, "kimi.plugin.json", `{"name":"kimi-auth-difference","mcpServers":{"remote":{"url":"https://example.test/mcp","authentication":{"type":"oauth"}}}}`)
	if _, err := Load(root); err == nil {
		t.Fatal("native authentication differences were silently removed")
	}
}

func TestVerifiedDigestCannotHideUnreadableLinkTarget(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "broken-relative"})
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := ContentDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing.txt", filepath.Join(root, "broken.txt")); err != nil {
		t.Skip(err)
	}
	if _, err := PrepareNativeTargetWithDigest(t.Context(), t.TempDir(), "p", pkg, root, "codex", digest); err == nil {
		t.Fatal("failed actual target resolution was treated as a relocatable source")
	}
}

func TestDirectoryAliasCannotHideLeavingSourceRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pkg")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	standardInput(t, root, map[string]any{"name": "alias-boundary"})
	fixtureFile(t, root, "content.txt", "content")
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	before, err := ContentDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".", filepath.Join(root, "dir-alias")); err != nil {
		t.Skip(err)
	}
	if err := os.Symlink("dir-alias/../pkg/content.txt", filepath.Join(root, "returning.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Error("Load accepted a link that leaves the real source root")
	}
	if _, err := ContentDigest(root); err == nil {
		t.Error("ContentDigest accepted a link that leaves the real source root")
	}
	if _, err := PrepareNativeTargetWithDigest(t.Context(), t.TempDir(), "p", pkg, root, "codex", before); err == nil {
		t.Error("Codex returned a position-dependent alias path")
	}
}

func TestCopiedLinkMappingDoesNotReadChangedLiveSource(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "frozen-link-map"})
	fixtureFile(t, root, "000-asset.bin", strings.Repeat("a", 16*1024*1024))
	fixtureFile(t, root, "a.txt", "accepted-A")
	fixtureFile(t, root, "b.txt", "later-B")
	if err := os.Symlink("a.txt", filepath.Join(root, "target-alias")); err != nil {
		t.Skip(err)
	}
	if err := os.Symlink(filepath.Join(root, "target-alias"), filepath.Join(root, "guide")); err != nil {
		t.Fatal(err)
	}
	expected, err := ContentDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	changed := make(chan error, 1)
	stop := make(chan struct{})
	go func() {
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case <-stop:
				return
			case <-deadline.C:
				changed <- status_error.PluginNotReady
				return
			default:
				if target, err := os.Readlink(filepath.Join(destination, "target-alias")); err == nil && target == "a.txt" {
					if err := os.Remove(filepath.Join(root, "target-alias")); err != nil {
						changed <- err
						return
					}
					changed <- os.Symlink("b.txt", filepath.Join(root, "target-alias"))
					return
				}
				runtime.Gosched()
			}
		}
	}()
	err = CopyDeliveryTreeWithDigest(root, destination, expected)
	close(stop)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-changed:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("source mapping did not change after raw copy")
	}
	content, err := os.ReadFile(filepath.Join(destination, "guide"))
	if err != nil || string(content) != "accepted-A" {
		t.Fatal("frozen mapping consulted changed live source", string(content), err)
	}
}

func TestSourceCannotAdmitLinkChainOSCannotResolve(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "native-link-limit"})
	fixtureFile(t, root, "content.txt", "content")
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := ContentDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	target := "content.txt"
	for index := 127; index >= 0; index-- {
		name := fmt.Sprintf("link-%03d", index)
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Skip(err)
		}
		target = name
	}
	if _, err := os.Stat(filepath.Join(root, "link-000")); err == nil {
		t.Skip("platform resolves this entire chain")
	}
	if _, err := Load(root); err == nil {
		t.Error("Load accepted an OS-unreadable link chain")
	}
	if _, err := ContentDigest(root); err == nil {
		t.Error("digest accepted an OS-unreadable link chain")
	}
	if _, err := PrepareNativeTargetWithDigest(t.Context(), t.TempDir(), "p", pkg, root, "codex", digest); err == nil {
		t.Error("Codex accepted an OS-unreadable link chain")
	}
}
