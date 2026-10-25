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

// TestCursorRealModelRetry 在显式启用时，用隔离配置验证真实账号在同一会话切换模型后继续回复。
func TestCursorRealModelRetry(t *testing.T) {
	if os.Getenv("ALLY_CURSOR_REAL_PROBE") != "1" {
		t.Skip("requires explicit real Cursor probe opt-in")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(home, ".cursor", "cli-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	configDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDirectory, "cli-config.json"), config, 0600); err != nil {
		t.Fatal(err)
	}
	binary := os.Getenv("ALLY_CURSOR_REAL_BINARY")
	if binary == "" {
		binary = "cursor-agent"
	}
	profile := ProfileSpec{ID: "cursor-real-probe", BaseRuntimeID: RuntimeIDCursor, BinaryCandidates: []string{binary}, Environment: map[string]string{"CURSOR_CONFIG_DIR": configDirectory, "CURSOR_DATA_DIR": t.TempDir(), "TMPDIR": t.TempDir(), "NO_OPEN_BROWSER": "1"}}
	sdk, err := New(Config{Profiles: []ProfileSpec{profile}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	defer func() { _ = sdk.Close(context.Background()) }()
	detection := sdk.Detect(ctx, DetectRequest{RuntimeID: profile.ID, BaseEnvironment: os.Environ()})
	if detection.State != DetectionStateReady {
		t.Fatalf("Cursor discovery failed: state=%s", detection.State)
	}
	t.Logf("Cursor version=%s binary=%s", detection.Version, detection.ResolvedBinary)
	models := []string{"grok-4.6", "composer-2.5", "composer-2.5", "composer-2.5"}
	request := RunRequest{Target: RuntimeTarget{RuntimeID: profile.ID}, Detection: detection, Workspace: Workspace{Path: t.TempDir(), Managed: true}, Session: SessionDirective{Mode: SessionModeNew}, Message: Message{Text: "Reply with exactly ALLY_CURSOR_PROBE_OK. Do not use any tools."}, Policy: RunPolicy{Reasoning: "default", Execution: ExecutionPolicy{PermissionLevel: PermissionLevelDefault}}}
	for index, model := range models {
		if index == 3 {
			if err = sdk.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			sdk, err = New(Config{Profiles: []ProfileSpec{profile}})
			if err != nil {
				t.Fatal(err)
			}
		}
		request.Policy.Model = model
		sink := &recordingSink{}
		started := time.Now()
		result := sdk.Run(ctx, request, sink)
		output := sink.textOutput()
		t.Logf("turn=%d model=%s elapsed=%s failure=%+v session=%s marker=%t upgrade=%t", index+1, model, time.Since(started), result.Failure, result.Session.OutputID, strings.Contains(output, "ALLY_CURSOR_PROBE_OK"), strings.Contains(output, "Upgrade your plan"))
		if result.Session.OutputID == "" {
			t.Fatalf("turn %d did not open a session: %+v", index+1, result.Failure)
		}
		if index == 0 && result.Failure != nil && result.Failure.Code != FailureCodeRateLimited {
			t.Fatalf("plan failure lost its stable classification: %+v", result.Failure)
		}
		if index > 0 && (result.Failure != nil || !strings.Contains(output, "ALLY_CURSOR_PROBE_OK")) {
			t.Fatalf("model retry did not complete: failure=%+v output=%q", result.Failure, output)
		}
		if index > 0 {
			var usage *UsageData
			for _, event := range sink.events {
				if event.Type == EventTypeUsage {
					var value UsageData
					if json.Unmarshal(event.Data, &value) == nil && value.Turn != nil {
						usage = &value
					}
				}
			}
			if usage == nil || usage.Turn.OutputTokens <= 0 {
				t.Fatalf("Cursor token usage did not reach NeuxBot: %+v", usage)
			}
			t.Logf("turn=%d usage=%+v", index+1, usage.Turn)
		}
		request.Session = SessionDirective{Mode: SessionModeResume, ID: result.Session.OutputID}
	}
}
