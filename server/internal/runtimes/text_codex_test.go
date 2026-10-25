package runtimes

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/shrewx/ginx/pkg/statuserror"
)

func TestGenerateTextCodexIsolatesNativeHomeAndUsesOriginalSystemInstructions(t *testing.T) {
	source := t.TempDir()
	t.Setenv("CODEX_HOME", source)
	original := []byte(`{"access_token":"fixture-credential"}`)
	if err := os.WriteFile(filepath.Join(source, "auth.json"), original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "config.toml"), []byte("model_provider = \"custom\"\ndeveloper_instructions = \"GLOBAL_PRIVATE_RULE\"\nmodel_instructions_file=\"/private/source-system.md\"\n[model_providers.custom]\nname=\"Fixture\"\nbase_url=\"https://example.invalid/v1\"\nwire_api=\"responses\"\n[mcp_servers.private]\ncommand=\"private-tool\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "AGENTS.md"), []byte("GLOBAL_PRIVATE_RULE"), 0600); err != nil {
		t.Fatal(err)
	}
	sdk, _ := New(Config{})
	defer sdk.Close(context.Background())
	var captured acpAdapterConfig
	var agent *textACPAgent
	replaceACPAgentFactory(sdk.acp.(*acpBackend), RuntimeIDCodex, func(_ context.Context, config acpAdapterConfig) (acp.Agent, error) {
		captured = config
		env := map[string]string{}
		for _, pair := range config.Environment {
			k, v, _ := strings.Cut(pair, "=")
			env[k] = v
		}
		home := env["CODEX_HOME"]
		if home == "" || home == source || env["HOME"] == "" || env["HOME"] == source {
			return nil, fmt.Errorf("native home was not isolated")
		}
		auth, err := os.ReadFile(filepath.Join(home, "auth.json"))
		if err != nil || string(auth) != string(original) {
			return nil, fmt.Errorf("authentication seed missing: %v", err)
		}
		info, _ := os.Stat(filepath.Join(home, "auth.json"))
		if info.Mode().Perm() != 0600 {
			return nil, fmt.Errorf("credential mode is not0600")
		}
		configBytes, err := os.ReadFile(filepath.Join(home, "config.toml"))
		if err != nil {
			return nil, err
		}
		if strings.Contains(string(configBytes), "GLOBAL_PRIVATE_RULE") || strings.Contains(string(configBytes), "mcp_servers") || !strings.Contains(string(configBytes), "example.invalid") {
			return nil, fmt.Errorf("provider allowlist did not preserve only connection configuration")
		}
		if _, err = os.Stat(filepath.Join(home, "AGENTS.md")); !os.IsNotExist(err) {
			return nil, fmt.Errorf("global instructions were copied")
		}
		instructions, err := os.ReadFile(filepath.Join(home, "model-instructions.md"))
		if err != nil || string(instructions) != "ORIGINAL_SYSTEM_RULES" {
			return nil, fmt.Errorf("original system instructions changed: %q %v", instructions, err)
		}
		args := strings.Join(config.PrefixArgs, " ")
		if !strings.Contains(args, "model_instructions_file=") || !strings.Contains(args, "project_doc_max_bytes=0") || !strings.Contains(args, `developer_instructions=""`) || !strings.Contains(args, "skills.bundled.enabled=false") || !strings.Contains(args, "skills.include_instructions=false") {
			return nil, fmt.Errorf("native instruction overrides missing")
		}
		agent = &textACPAgent{&fakeCodexACPAgent{selectedConfigs: map[acp.SessionConfigId]acp.SessionConfigValueId{}, configOptions: textTestOptions("none"), scriptedUpdates: []acp.SessionUpdate{acp.UpdateAgentMessageText("answer")}, scriptedResponse: &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}}}
		return agent, nil
	})
	binary, _ := os.Executable()
	result := sdk.GenerateText(context.Background(), TextRequest{Target: RuntimeTarget{RuntimeID: RuntimeIDCodex}, Detection: readyTextDetectionForRun(t, sdk, RuntimeIDCodex, binary), Model: "model-a", Messages: []TextMessage{{Role: "system", Content: "ORIGINAL_SYSTEM_RULES"}, {Role: "user", Content: "EXACT_USER_CONTENT"}}})
	if result.Failure != nil {
		t.Fatal(result.Failure)
	}
	if agent.promptRequest.Prompt[0].Text.Text != "EXACT_USER_CONTENT" {
		t.Fatal("native system remained flattened into user text")
	}
	for _, pair := range captured.Environment {
		if k, v, _ := strings.Cut(pair, "="); k == "CODEX_HOME" {
			if _, err := os.Stat(v); !os.IsNotExist(err) {
				t.Fatal("sensitive home retained")
			}
		}
	}
}

func TestCodexTextProfileFormsPreserveProviderOnly(t *testing.T) {
	for _, args := range [][]string{{"--profile", "team"}, {"--profile=team"}, {"-p", "team"}, {"-pteam"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			source := t.TempDir()
			workspace := t.TempDir()
			if err := os.WriteFile(filepath.Join(source, "team.config.toml"), []byte("model_provider=\"team-provider\"\ndeveloper_instructions=\"PRIVATE_RULE\"\n[model_providers.team-provider]\nname=\"Team\"\nbase_url=\"https://example.invalid\"\nexperimental_bearer_token=\"fixture\"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			env, err := buildEnvironment(nil, map[string]string{"CODEX_HOME": source})
			if err != nil {
				t.Fatal(err)
			}
			prepared := preparedRuntime{definition: runtimeDefinition{prefixArgs: args}, environment: env}
			prompt, persist, err := prepareCodexText(&prepared, workspace, []TextMessage{{Role: "user", Content: "EXACT"}})
			if err != nil || prompt != "EXACT" || persist == nil {
				t.Fatalf("profile failed %v", err)
			}
			instructions, err := os.ReadFile(filepath.Join(prepared.environment.get("CODEX_HOME"), "model-instructions.md"))
			if err != nil || string(instructions) != codexTextNeutralInstructions {
				t.Fatal("user-only call did not install the explicit neutral native shim")
			}
			data, err := os.ReadFile(filepath.Join(prepared.environment.get("CODEX_HOME"), "team.config.toml"))
			if err != nil || !strings.Contains(string(data), "team-provider") || strings.Contains(string(data), "PRIVATE_RULE") {
				t.Fatalf("selected provider profile lost or contaminated: %v", err)
			}
		})
	}
}

func TestCodexTextCredentialRotationUsesCompareBeforeWrite(t *testing.T) {
	for _, concurrentChange := range []bool{false, true} {
		t.Run(fmt.Sprint(concurrentChange), func(t *testing.T) {
			source := t.TempDir()
			original := []byte(`{"access_token":"before"}`)
			path := filepath.Join(source, "auth.json")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			env, _ := buildEnvironment(nil, map[string]string{"CODEX_HOME": source})
			prepared := preparedRuntime{environment: env}
			_, persist, err := prepareCodexText(&prepared, t.TempDir(), []TextMessage{{Role: "user", Content: "text"}})
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(prepared.environment.get("CODEX_HOME"), "auth.json"), []byte(`{"access_token":"refreshed"}`), 0600); err != nil {
				t.Fatal(err)
			}
			if concurrentChange {
				if err = os.WriteFile(path, []byte(`{"access_token":"other-session"}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err = persist(); err != nil {
				t.Fatal(err)
			}
			result, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			expected := "refreshed"
			if concurrentChange {
				expected = "other-session"
			}
			if !strings.Contains(string(result), expected) {
				t.Fatal("credential rotation overwrote a concurrent refresh or lost its own refresh")
			}
		})
	}
}

func TestCodexTextRefusesUntransferableKeyringAndSymlinkCredentials(t *testing.T) {
	for _, kind := range []string{"keyring", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			source := t.TempDir()
			env, _ := buildEnvironment(nil, map[string]string{"CODEX_HOME": source})
			prepared := preparedRuntime{environment: env}
			if kind == "keyring" {
				if err := os.WriteFile(filepath.Join(source, "config.toml"), []byte("cli_auth_credentials_store=\"keyring\""), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				outside := filepath.Join(t.TempDir(), "auth.json")
				if err := os.WriteFile(outside, []byte(`{"access_token":"private"}`), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(source, "auth.json")); err != nil {
					t.Skip(err)
				}
			}
			if _, _, err := prepareCodexText(&prepared, t.TempDir(), []TextMessage{{Role: "user", Content: "text"}}); err == nil {
				t.Fatal("credential isolation silently used unsupported ownership")
			} else if kind == "keyring" {
				var classified *statuserror.StatusErr
				if !errors.As(err, &classified) || !strings.Contains(fmt.Sprint(classified.Params["Detail"]), "keyring-only") {
					t.Fatal("keyring authentication failure lost the isolation reason")
				}
			}
		})
	}
}

func TestCodexTextConcurrentCredentialRefreshDoesNotOverwriteFirstWriter(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "auth.json")
	original := []byte(`{"access_token":"original"}`)
	if err := os.WriteFile(source, original, 0600); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(directory, "first.json")
	second := filepath.Join(directory, "second.json")
	for path, text := range map[string]string{first: `{"access_token":"first"}`, second: `{"access_token":"second"}`} {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan error, 2)
	writer := func(path string, data []byte) error {
		entered <- struct{}{}
		<-release
		return os.WriteFile(path, data, 0600)
	}
	go func() { done <- persistCodexTextAuth(source, original, first, writer) }()
	<-entered
	secondStarted := make(chan struct{})
	go func() { close(secondStarted); done <- persistCodexTextAuth(source, original, second, writer) }()
	<-secondStarted
	select {
	case <-entered:
		close(release)
		t.Fatal("second text refresh compared before the first writer completed")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(source)
	if err != nil || !strings.Contains(string(data), "first") {
		t.Fatalf("first refresh overwritten: %v", err)
	}
}

func TestCodexTextCredentialRefreshPreservesAccountIdentity(t *testing.T) {
	directory := t.TempDir()
	source, temporary := filepath.Join(directory, "auth.json"), filepath.Join(directory, "refreshed.json")
	original := []byte(`{"tokens":{"account_id":"original","access_token":"old"}}`)
	if err := os.WriteFile(source, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temporary, []byte(`{"tokens":{"account_id":"different","access_token":"new"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := persistCodexTextAuth(source, original, temporary, func(path string, data []byte) error { return os.WriteFile(path, data, 0600) }); err == nil {
		t.Fatal("refreshed credential changed account identity")
	}
	after, err := os.ReadFile(source)
	if err != nil || string(after) != string(original) {
		t.Fatal("source authentication changed")
	}
}

func TestGenerateTextOAuthCallsOverlap(t *testing.T) {
	source := t.TempDir()
	t.Setenv("CODEX_HOME", source)
	if err := os.WriteFile(filepath.Join(source, "auth.json"), []byte(`{"tokens":{"account_id":"fixture","refresh_token":"old"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	sdk, _ := New(Config{})
	defer sdk.Close(context.Background())
	firstStarted := make(chan struct{})
	var calls atomic.Int32
	replaceACPAgentFactory(sdk.acp.(*acpBackend), RuntimeIDCodex, func(_ context.Context, config acpAdapterConfig) (acp.Agent, error) {
		count := calls.Add(1)
		var home string
		for _, pair := range config.Environment {
			key, value, _ := strings.Cut(pair, "=")
			if key == "CODEX_HOME" {
				home = value
			}
		}
		agent := &fakeCodexACPAgent{selectedConfigs: map[acp.SessionConfigId]acp.SessionConfigValueId{}, configOptions: textTestOptions("none"), scriptedUpdates: []acp.SessionUpdate{acp.UpdateAgentMessageText("answer")}, scriptedResponse: &acp.PromptResponse{StopReason: acp.StopReasonEndTurn}}
		if count == 1 {
			if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"tokens":{"account_id":"fixture","refresh_token":"refreshed"}}`), 0600); err != nil {
				return nil, err
			}
			agent.blockPrompt = true
			agent.promptStarted = firstStarted
		}
		return &textACPAgent{agent}, nil
	})
	binary, _ := os.Executable()
	request := TextRequest{Target: RuntimeTarget{RuntimeID: RuntimeIDCodex}, Detection: readyTextDetectionForRun(t, sdk, RuntimeIDCodex, binary), Model: "model-a", Messages: []TextMessage{{Role: "user", Content: "source"}}}
	firstCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	firstDone := make(chan TextResult, 1)
	go func() { firstDone <- sdk.GenerateText(firstCtx, request) }()
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first model never started")
	}
	waitingCtx, stopWaiting := context.WithTimeout(context.Background(), time.Second)
	defer stopWaiting()
	second := sdk.GenerateText(waitingCtx, request)
	if second.Failure != nil || calls.Load() != 2 {
		cancel()
		<-firstDone
		t.Fatal("second OAuth invocation waited for the first model to finish")
	}
	cancel()
	<-firstDone
	third := sdk.GenerateText(context.Background(), request)
	if third.Failure != nil || calls.Load() != 3 {
		t.Fatalf("lease was not released with refreshed seed: %+v calls=%d", third, calls.Load())
	}
}
