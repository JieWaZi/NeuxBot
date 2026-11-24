package agent_test

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == agent.EphemeralClaudeMode {
		os.Exit(agent.RunEphemeralClaude(os.Args[2:]))
	}
	os.Exit(m.Run())
}

func TestInstalledClaudeReviewStoresOnlyCanonicalUsage(t *testing.T) {
	executable, endpoint := os.Getenv("NEUX_TEST_CLAUDE"), os.Getenv("NEUX_TEST_CLAUDE_LOOPBACK")
	if executable == "" || endpoint == "" {
		t.Skip("installed Claude and isolated loopback fixture are required")
	}
	const prompt = "NEUX_REVIEW_EPHEMERAL_INPUT_324"
	const answer = "NEUX_REVIEW_EPHEMERAL_OUTPUT_324"
	root := t.TempDir()
	scratch, settings := filepath.Join(root, "review"), filepath.Join(root, "config")
	if err := agent.RecoverEphemeralScratch(scratch); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(settings, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := conversation.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	env := map[string]string{
		"PATH":               "/usr/bin:/bin:/opt/homebrew/bin",
		"CLAUDE_CONFIG_DIR":  settings,
		"ANTHROPIC_BASE_URL": endpoint, "ANTHROPIC_API_KEY": "review-loopback-fixture-key",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1", "DISABLE_TELEMETRY": "1",
		"NO_PROXY": "127.0.0.1", "HTTP_PROXY": "http://127.0.0.1:9", "HTTPS_PROXY": "http://127.0.0.1:9",
	}
	provider := agent.NewProviderRuntime(agent.ProviderRuntimeOptions{AcquireAccount: func(context.Context, agent.AccountRequest) (agent.AccountLease, error) {
		return agent.AccountLease{ConfigHome: settings, Environment: env}, nil
	}})
	lifecycle := agent.NewThreadLifecycle(provider, store)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	result, err := lifecycle.GenerateText(ctx, agent.TextRequest{
		Connection: agent.ProviderConnectionRequest{Account: agent.AccountRequest{Provider: "claude", Executable: executable,
			AccountID: "review-fixture-account", Subject: "review-fixture-subject", AuthGeneration: 1, ConfigHome: settings, Model: "sonnet"},
			Launch: agent.ProviderLaunch{Executable: executable, ConfigHome: settings, BaseEnvironment: env}},
		Messages: []agent.TextMessage{{Role: "user", Content: prompt}}, Ephemeral: true, ScratchRoot: scratch,
		UsageSource: "memory_review", UsageWakerID: "review-fixture-waker",
	})
	if err != nil || result.Text != answer || !result.ToolsDisabled {
		t.Fatalf("real private generation: answer matches=%v, tool policy=%v, error=%v", result.Text == answer, result.ToolsDisabled, err)
	}
	if err = provider.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err == nil && (bytes.Contains(data, []byte(prompt)) || bytes.Contains(data, []byte(answer))) {
			t.Fatalf("review body persisted in %s", path)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	uses := 0
	for shard := 0; shard < conversation.UsageShards; shard++ {
		end, err := store.UsageHighWater(ctx, shard)
		if err != nil {
			t.Fatal(err)
		}
		if end == 0 {
			continue
		}
		items, _, err := store.ReadUsageTerminals(ctx, shard, 0, end)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			v := item.Result
			if v.Attribution != nil && v.Attribution.WakerID == "review-fixture-waker" && v.Attribution.Source == "memory_review" && v.Turn.Usage != nil && v.FinalMessage == nil {
				uses++
			}
		}
	}
	if uses != 1 {
		t.Fatalf("canonical private usage terminals = %d, want 1", uses)
	}
}
