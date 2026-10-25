package runtimes

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCursorRealContextUsage 验证真实生成结束后把原生上下文窗口送入 NeuxBot 事件流。
func TestCursorRealContextUsage(t *testing.T) {
	if os.Getenv("ALLY_CURSOR_REAL_PROBE") != "1" {
		t.Skip("requires explicit real Cursor probe opt-in")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".cursor", "cli-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	configDirectory := t.TempDir()
	if err = os.WriteFile(
		filepath.Join(configDirectory, "cli-config.json"),
		data,
		0600,
	); err != nil {
		t.Fatal(err)
	}
	profile := ProfileSpec{
		ID:               "cursor-context-real",
		BaseRuntimeID:    RuntimeIDCursor,
		BinaryCandidates: []string{"cursor-agent"},
		Environment: map[string]string{
			"CURSOR_CONFIG_DIR": configDirectory,
			"TMPDIR":            t.TempDir(),
			"NO_OPEN_BROWSER":   "1",
		},
	}
	sdk, err := New(Config{Profiles: []ProfileSpec{profile}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sdk.Close(context.Background()) }()
	discoveryContext, cancelDiscovery := context.WithTimeout(
		context.Background(),
		60*time.Second,
	)
	detection := sdk.Detect(discoveryContext, DetectRequest{
		RuntimeID:       profile.ID,
		BaseEnvironment: os.Environ(),
	})
	cancelDiscovery()
	if detection.State != DetectionStateReady {
		t.Fatalf("discovery failed: %+v", detection)
	}
	runContext, cancelRun := context.WithTimeout(
		context.Background(),
		120*time.Second,
	)
	defer cancelRun()
	sink := &recordingSink{}
	result := sdk.Run(runContext, RunRequest{
		Target:    RuntimeTarget{RuntimeID: profile.ID},
		Detection: detection,
		Workspace: Workspace{Path: t.TempDir(), Managed: true},
		Session:   SessionDirective{Mode: SessionModeNew},
		Message: Message{
			Text: "Reply exactly CURSOR_CONTEXT_REAL_OK. Do not use tools.",
		},
		Policy: RunPolicy{
			Model:     "composer-2.5",
			Reasoning: "default",
			Execution: ExecutionPolicy{
				PermissionLevel: PermissionLevelDefault,
			},
		},
	}, sink)
	if result.Failure != nil ||
		!strings.Contains(sink.textOutput(), "CURSOR_CONTEXT_REAL_OK") {
		t.Fatalf("run failed: %+v output=%s", result.Failure, sink.textOutput())
	}
	var contextUsage *ContextUsageData
	for _, event := range sink.events {
		if event.Type != EventTypeUsage {
			continue
		}
		var usage UsageData
		if json.Unmarshal(event.Data, &usage) == nil && usage.Context != nil {
			contextUsage = usage.Context
		}
	}
	invalidContext := contextUsage == nil ||
		contextUsage.UsedTokens <= 0 ||
		contextUsage.WindowTokens <= 0 ||
		contextUsage.UsedTokens > contextUsage.WindowTokens
	if invalidContext {
		t.Fatalf("context usage missing or invalid: %+v", contextUsage)
	}
	t.Logf(
		"version=%s context=%d/%d",
		detection.Version,
		contextUsage.UsedTokens,
		contextUsage.WindowTokens,
	)
}
