package installation

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func TestCheckVersionReadsMetadataWithoutArtifactOrProcess(t *testing.T) {
	p := publisher(t)
	p.put("https://api.github.com/repos/openai/codex/releases/latest", map[string]any{"tag_name": "rust-v1.12.3", "assets": []any{map[string]any{"name": "codex-package-aarch64-apple-darwin.tar.gz", "digest": "sha256:" + strings.Repeat("a", 64)}}})
	var requests atomic.Int32
	p.handler = func(w http.ResponseWriter, request *http.Request) bool {
		requests.Add(1)
		if strings.Contains(request.Header.Get("X-Official-URL"), "/releases/download/") {
			t.Error("version check downloaded a CLI payload")
		}
		return false
	}
	i := NewWithOptions(t.TempDir(), Options{Client: p.client, OS: "darwin", Arch: "arm64", Runner: fakeRunner{run: func(context.Context, Command) (string, error) {
		t.Error("version check executed a process")
		return "", nil
	}}})
	release, err := i.CheckVersion(t.Context(), constants.RuntimeIDCodex)
	if err != nil || release.Version != "1.12.3" || requests.Load() != 1 {
		t.Fatalf("metadata-only check = %+v requests=%d error=%v", release, requests.Load(), err)
	}
}

func TestCompareNativeVersionsDoesNotOfferFalseUpdates(t *testing.T) {
	for _, sample := range []struct {
		// provider selects the native release format.
		provider string
		// local is an actual CLI version receipt.
		local string
		// latest is the official release identity.
		latest string
		// order describes the expected local-to-publisher ordering.
		order int
		// known rejects ambiguous builds instead of guessing.
		known bool
	}{
		{constants.RuntimeIDCodex, "codex-cli 0.119.0", "0.119.0", 0, true},
		{constants.RuntimeIDClaude, "2.1.226 (Claude Code)", "2.1.232", -1, true},
		{constants.RuntimeIDClaude, "2.10.0", "2.9.0", 1, true},
		{constants.RuntimeIDCodex, "rust-v1.2.3", "1.2.3", 0, true},
		{constants.RuntimeIDPi, "v1.2.3+build.1", "1.2.3+build.2", 0, true},
		{constants.RuntimeIDGemini, "0.20.0-preview.1", "0.20.0", -1, true},
		{constants.RuntimeIDOpenCode, "1.2.3", "1.2.3-beta.1", 1, true},
		{constants.RuntimeIDOpenCode, "opencode v2.2.6", "2.2.6", 0, true},
		{constants.RuntimeIDGrok, "grok 1.0.77 (50ca579fa996)", "1.0.77", 0, true},
		{constants.RuntimeIDCursor, "2026.11.05-abc123", "2026.11.05-abc123", 0, true},
		{constants.RuntimeIDCursor, "2026.11.05-abc123", "2026.11.06-def456", -1, true},
		{constants.RuntimeIDCursor, "2026.11.05-abc123", "2026.11.05-def456", 0, false},
		{constants.RuntimeIDCursor, "2026.02.30-abc123", "2026.03.01-def456", 0, false},
		{constants.RuntimeIDCodex, "unknown", "1.2.3", 0, false},
		{constants.RuntimeIDCodex, "1.2.3 4.5.6", "1.2.3", 0, false},
	} {
		order, known := CompareVersions(sample.provider, sample.local, sample.latest)
		if order != sample.order || known != sample.known {
			t.Errorf("%s %q vs %q = %d/%v want %d/%v", sample.provider, sample.local, sample.latest, order, known, sample.order, sample.known)
		}
	}
}
