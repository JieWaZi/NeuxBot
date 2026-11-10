package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	ginxlogx "github.com/shrewx/ginx/pkg/logx"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDistributionRequestErrorsRedactURLAndReportTimeout(t *testing.T) {
	var output bytes.Buffer
	logger := ginxlogx.Instance()
	prior := logger.Out
	logger.SetOutput(&output)
	t.Cleanup(func() { logger.SetOutput(prior) })
	address := "https://private-user:private-password@example.invalid/details?token=private-token"
	logDistributionFailure("details_get", "feishu-office", address, nil, time.Now(), &url.Error{Op: "Get", URL: address, Err: context.DeadlineExceeded})
	log := output.String()
	if !strings.Contains(log, "timeout=true") || !strings.Contains(log, "context deadline exceeded") || !strings.Contains(log, "https://example.invalid/details") {
		t.Fatalf("request timeout lost useful diagnostics: %s", log)
	}
	if strings.Contains(log, "private-") || strings.Contains(log, "token=") {
		t.Fatal("request error exposed URL credentials")
	}
}

func TestDetailsDistributionFailuresKeepSafeUpstreamDiagnostics(t *testing.T) {
	for _, stage := range []string{"status", "body"} {
		t.Run(stage, func(t *testing.T) {
			var output bytes.Buffer
			logger := ginxlogx.Instance()
			prior := logger.Out
			logger.SetOutput(&output)
			t.Cleanup(func() { logger.SetOutput(prior) })
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/details" {
					http.Redirect(w, r, "/asset?signature=private-signed-value", http.StatusFound)
					return
				}
				if stage == "status" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Length", "100")
				_, _ = w.Write([]byte("short"))
			}))
			defer server.Close()
			_, err := ReadDetails(context.Background(), Entry{Key: "feishu-office", DetailsURL: server.URL + "/details?token=private-source-value", DetailsSHA256: strings.Repeat(map[string]string{"status": "a", "body": "b"}[stage], 64), DetailsSize: 100})
			if !errors.Is(err, status_error.PluginDistributionUnavailable) {
				t.Fatal("upstream failure lost its distribution error", err)
			}
			log := output.String()
			want := "upstream_status=503"
			if stage == "body" {
				want = "unexpected EOF"
			}
			if !strings.Contains(log, want) || !strings.Contains(log, "plugin_key=feishu-office") || !strings.Contains(log, "elapsed_ms=") {
				t.Fatalf("upstream failure discarded diagnostic context: %s", log)
			}
			if strings.Contains(log, "private-") || strings.Contains(log, "signature=") || strings.Contains(log, "token=") {
				t.Fatal("diagnostics exposed signed URL parameters")
			}
		})
	}
}

func TestBuiltCatalogDetailsArtifacts(t *testing.T) {
	directory := os.Getenv("NEUXALLY_PLUGIN_DETAILS_ARTIFACT_DIR")
	if directory == "" {
		t.Skip("显式固定插件分发资料验收")
	}
	raw, err := os.ReadFile(filepath.Join(directory, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := parseIndex(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("unexpected real catalog entry count: %d", len(entries))
	}
	for _, entry := range entries {
		t.Run(entry.Key, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(directory, entry.Key+".details.json"))
			if err != nil {
				t.Fatal(err)
			}
			details, err := parseDetails(raw, entry)
			if err != nil {
				t.Fatalf("real published metadata rejected: %v", err)
			}
			for _, locale := range []string{"en", "zh-CN"} {
				content, _, err := details.Readme(locale)
				if err != nil || content == "" || details.ViewPackage(locale).Readme != content {
					t.Fatalf("real localized README unavailable: %s %v", locale, err)
				}
			}
			for _, skill := range details.Package.Skills {
				_, content, err := details.Skill(skill.Key)
				if err != nil || content == "" {
					t.Fatalf("real SKILL unavailable: %s %v", skill.Key, err)
				}
			}
			t.Logf("%s: verified %d byte public metadata, %d complete SKILL bodies, no archive download", entry.Key, len(raw), len(details.SkillContents))
		})
	}
}

func TestStandardDetailsRejectsLegacyAndPrivateFields(t *testing.T) {
	body := []byte(`{"schemaVersion":1,"sourceCommit":"1111111111111111111111111111111111111111","archiveSHA256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","packagePath":"entry","package":{"contractVersion":"neuxbot-plugin/1","manifest":{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"standard","version":"1"},"format":"agent-plugins","presentation":{"defaultPrompt":["Read reports"],"defaultPromptLocalizations":{"zh-CN":["读取报告"]},"capabilities":["Read"],"supportURL":"https://example.com/support","brandColor":{"default":"#123456","dark":"#abcdef"},"icons":{},"setup":{"instructions":{"en":"Connect your account"},"helpURL":"https://example.com/help"}},"skills":[],"servers":[],"diagnostics":[]},"readmes":{"en":"Public README"},"skillContents":{}}`)
	check := func(raw []byte) error {
		sum := sha256.Sum256(raw)
		entry := Entry{SourceCommit: strings.Repeat("1", 40), ArchiveSHA256: strings.Repeat("b", 64), PackagePath: "entry", DetailsSHA256: hex.EncodeToString(sum[:]), DetailsSize: int64(len(raw)), Package: plugins.Package{Manifest: plugins.Manifest{Name: "standard", Version: "1"}}}
		_, err := parseDetails(raw, entry)
		return err
	}
	if err := check(body); err != nil {
		t.Fatal(err)
	}
	for _, changed := range []string{strings.Replace(string(body), `"schemaVersion":1`, `"schemaVersion":2`, 1), strings.Replace(string(body), `"contractVersion":"neuxbot-plugin/1",`, "", 1), strings.Replace(string(body), `"capabilities":["Read"]`, `"capabilities":["Read"],"command":"auth.mjs"`, 1), strings.Replace(string(body), `"setup":{"instructions"`, `"setup":{"command":"auth.mjs","instructions"`, 1), strings.Replace(string(body), `"brandColor":{"default":"#123456"`, `"brandColor":{"default":"url(https://bad.example)"`, 1), strings.Replace(string(body), `"zh-CN":["读取报告"]`, `"zh-CN":["读取报告","错误索引"]`, 1)} {
		if err := check([]byte(changed)); err == nil {
			t.Fatal("unsafe/old details accepted")
		}
	}
}
