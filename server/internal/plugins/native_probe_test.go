package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEmitNativeAcceptance(t *testing.T) {
	if os.Getenv("NEUXALLY_NATIVE_ACCEPTANCE") == "" {
		t.Skip("explicit isolated CLI acceptance only")
	}
	root, err := os.MkdirTemp("", "neuxbot-native-artifacts-")
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	fixtureFile(t, source, ".codex-plugin/plugin.json", `{"name":"acceptance","version":"1.0.0","description":"Production derived package"}`)
	fixtureFile(t, source, "skills/native-probe/SKILL.md", "---\nname: native-probe\ndescription: Verify native session loading.\n---\nProbe")
	pkg, err := Load(source)
	if err != nil {
		t.Fatal(err)
	}
	native, err := PrepareNativeTarget(filepath.Join(root, "derived"), "plugin-acceptance", pkg, source, "codex")
	if err != nil {
		t.Fatal(err)
	}
	home, err := PrepareCodexHome(filepath.Join(root, "homes"), "", []NativeTarget{native})
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	if err := PrepareCodexWorkspace(workspace, []NativeTarget{native}); err != nil {
		t.Fatal(err)
	}
	claude, err := PrepareNativeTarget(filepath.Join(root, "derived"), "plugin-acceptance", pkg, source, "claude")
	if err != nil {
		t.Fatal(err)
	}
	bytes, _ := json.Marshal(map[string]string{"root": root, "workspace": workspace, "home": home, "claude": claude.Root})
	t.Log(string(bytes))
}

func TestPrepareNativeTargetWithDigestDoesNotReReadVerifiedRoot(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"verified","version":"1.0.0"}`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := pluginArchive.Digest(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	native, err := PrepareNativeTargetWithDigest(t.TempDir(), "verified", pkg, root, "codex", digest)
	if err != nil {
		t.Fatalf("verified digest should avoid a second root scan: %v", err)
	}
	if native.Digest != digest {
		t.Fatalf("native digest = %q, want %q", native.Digest, digest)
	}
}
