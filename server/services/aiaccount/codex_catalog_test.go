package aiaccount

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/JieWaZi/NeuxBot/server/constants"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestManagedCodexCatalogChangeReopensProcessAndResumesNextTurn(t *testing.T) {
	for _, change := range []string{"model", "reasoning"} {
		t.Run(change, func(t *testing.T) {
			service, _, _ := subscriptionFixture(t)
			service.dataDirectory = t.TempDir()
			account := models.AIAccount{ID: "fedcba9876543210fedcba9876543210", Name: "managed", AuthMode: constants.AccountAuthModeAPIKey, Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: "https://example.invalid/v1"}, SelectedModels: []string{"first"}, ModelReasoningLevels: map[string][]string{"first": {"low"}}}
			if err := service.accounts.Create(t.Context(), account, "catalog-key"); err != nil {
				t.Fatal(err)
			}
			provider := agent.NewProviderRuntime(agent.ProviderRuntimeOptions{AcquireAccount: service.AcquireAccount})
			store, err := conversation.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			lifecycle := agent.NewThreadLifecycle(provider, store)
			t.Cleanup(func() {
				if err := lifecycle.Close(context.Background()); err != nil {
					t.Error(err)
				}
				if err := provider.Close(context.Background()); err != nil {
					t.Error(err)
				}
				store.Close()
			})
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			trace := filepath.Join(t.TempDir(), "wire")
			input := agent.AccountRequest{Provider: "codex", AccountID: account.ID, CWD: t.TempDir(), Model: "first", Executable: executable, PrefixArgs: []string{"-test.run=^TestManagedCodexCatalogNativeChild$", "--"}}
			first, err := service.ResolveAccount(t.Context(), input, "")
			if err != nil {
				t.Fatal(err)
			}
			run := func(index int, selected agent.AccountRequest, reasoning string) agent.TurnResult {
				t.Helper()
				turnInput := agent.TurnRequest{HostInvocationID: fmt.Sprintf("host-%d", index), ExecutionID: fmt.Sprintf("execution-%d", index), AssistantMessageID: fmt.Sprintf("assistant-%d", index), Text: fmt.Sprintf("only-input-%d", index)}
				request := agent.ThreadRequest{ChatID: "codex-catalog-chat", Connection: agent.ProviderConnectionRequest{Account: selected, Launch: agent.ProviderLaunch{Executable: executable, PrefixArgs: input.PrefixArgs, Environment: map[string]string{"ACCOUNT_CATALOG_NATIVE_CHILD": "1", "ACCOUNT_CATALOG_TRACE": trace}}}, Selection: &agent.TurnSelection{Reasoning: reasoning}}
				ports := agent.TurnPorts{Admit: func(_ context.Context, admission agent.TurnAdmission) (agent.ConversationBatch, error) {
					rows := []agent.ConversationChange{}
					if index == 0 {
						rows = append(rows, agent.ConversationChange{Fact: &agent.ChatFact{Type: constants.ChatEventTypeCreated}})
					}
					body, _ := json.Marshal(map[string]string{"role": "user", "content": turnInput.Text})
					execution, _ := json.Marshal(map[string]string{"userMessageId": "user-" + turnInput.HostInvocationID})
					rows = append(rows, agent.ConversationChange{Fact: &agent.ChatFact{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "user-" + turnInput.HostInvocationID, Data: body}}, agent.ConversationChange{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: turnInput.ExecutionID, MessageID: turnInput.AssistantMessageID, InvocationID: admission.Input.HostInvocationID, Data: execution}})
					return agent.ConversationBatch{ChatID: request.ChatID, Key: "admit-" + turnInput.HostInvocationID, Changes: rows}, nil
				}}
				turn, err := lifecycle.Prepare(t.Context(), request, turnInput, ports)
				if err != nil {
					wire, _ := os.ReadFile(trace)
					t.Fatalf("prepare: %v; wire=%s selected=%+v", err, wire, selected)
				}
				result := turn.Run(t.Context())
				if result.OperationError != nil || result.StorageError != nil || result.Outcome == nil {
					t.Fatalf("native turn failed: %+v", result)
				}
				version, err := store.ReadVersion(t.Context(), request.ChatID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := store.Commit(t.Context(), agent.ConversationBatch{ChatID: request.ChatID, Key: "complete-" + turnInput.ExecutionID, ExpectedVersion: version, Changes: []agent.ConversationChange{{Fact: &agent.ChatFact{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: turnInput.ExecutionID, MessageID: turnInput.AssistantMessageID}}}}); err != nil {
					t.Fatal(err)
				}
				return result
			}
			original := run(0, first, "low")
			current, err := service.accounts.Get(t.Context(), account.ID)
			if err != nil {
				t.Fatal(err)
			}
			requestedModel, requestedReasoning := "first", "high"
			if change == "model" {
				current.SelectedModels = append(current.SelectedModels, "second")
				current.ModelReasoningLevels["second"] = []string{"high"}
				requestedModel = "second"
			} else {
				current.ModelReasoningLevels["first"] = []string{"low", "high"}
			}
			if err := service.accounts.Update(t.Context(), current, ""); err != nil {
				t.Fatal(err)
			}
			input.Model = requestedModel
			next, err := service.ResolveAccount(t.Context(), input, "")
			if err != nil {
				t.Fatal(err)
			}
			completed := run(1, next, requestedReasoning)
			wire, err := os.ReadFile(trace)
			if err != nil {
				t.Fatal(err)
			}
			if first.ConfigHome != next.ConfigHome || first.Subject != next.Subject || first.AuthGeneration != next.AuthGeneration || completed.Binding.Session != original.Binding.Session || strings.Count(string(wire), `"method":"thread/start"`) != 1 || strings.Count(string(wire), `"method":"thread/resume"`) != 1 {
				t.Fatalf("catalog refresh failed to reopen and resume same history: %s", wire)
			}
			if strings.Count(string(wire), "only-input-0") != 1 || strings.Count(string(wire), "only-input-1") != 1 {
				t.Fatalf("catalog refresh replayed input: %s", wire)
			}
			for _, line := range strings.Split(string(wire), "\n") {
				if strings.Contains(line, "only-input-1") && (!strings.Contains(line, `"model":"`+requestedModel+`"`) || !strings.Contains(line, `"effort":"`+requestedReasoning+`"`)) {
					t.Fatalf("startup seed replaced the caller's explicit turn selection: %s", line)
				}
			}
		})
	}
}

// TestManagedCodexCatalogNativeChild 以受控原生协议子进程验证锁定版本的 Codex 适配器。
func TestManagedCodexCatalogNativeChild(t *testing.T) {
	if os.Getenv("ACCOUNT_CATALOG_NATIVE_CHILD") == "" {
		return
	}
	trace, err := os.OpenFile(os.Getenv("ACCOUNT_CATALOG_TRACE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(2)
	}
	var launch struct {
		// CatalogPath 是本次原生进程通过启动参数固定的不可变模型目录。
		CatalogPath string `toml:"model_catalog_json"`
		// Model 是配置文件与启动参数共同确定的原生启动模型种子。
		Model string `toml:"model"`
	}
	if _, err := toml.DecodeFile(filepath.Join(os.Getenv("CODEX_HOME"), "config.toml"), &launch); err != nil {
		os.Exit(3)
	}
	for _, argument := range os.Args {
		if strings.HasPrefix(argument, "model_catalog_json=") || strings.HasPrefix(argument, "model=") {
			if _, err := toml.Decode(argument, &launch); err != nil {
				os.Exit(3)
			}
		}
	}
	fmt.Fprintf(trace, "startupCatalog=%s startupModel=%s\n", launch.CatalogPath, launch.Model)
	raw, err := os.ReadFile(launch.CatalogPath)
	if err != nil {
		os.Exit(3)
	}
	var catalog struct {
		// Models 保存该原生进程启动时固定的受管模型目录。
		Models []struct {
			// Slug 是账号配置生成的实际模型标识。
			Slug string `json:"slug"`
			// Default 是模型声明的原生默认推理档位。
			Default string `json:"default_reasoning_level"`
			// Levels 保存该模型实际交付的推理档位。
			Levels []struct {
				// Effort 是原生协议接受的推理档位标识。
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &catalog) != nil {
		os.Exit(4)
	}
	rows := []any{}
	for i, row := range catalog.Models {
		efforts := []any{}
		for _, level := range row.Levels {
			efforts = append(efforts, map[string]string{"reasoningEffort": level.Effort})
		}
		rows = append(rows, map[string]any{"id": row.Slug, "model": row.Slug, "displayName": row.Slug, "isDefault": i == 0, "defaultReasoningEffort": row.Default, "supportedReasoningEfforts": efforts})
	}
	out := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	session := "managed-catalog-session"
	sequence := 0
	for scanner.Scan() {
		fmt.Fprintln(trace, scanner.Text())
		var frame map[string]any
		if json.Unmarshal(scanner.Bytes(), &frame) != nil {
			os.Exit(5)
		}
		ack := func(result any) { _ = out.Encode(map[string]any{"id": frame["id"], "result": result}) }
		emit := func(method string, params any) { _ = out.Encode(map[string]any{"method": method, "params": params}) }
		switch frame["method"] {
		case "initialize":
			ack(map[string]any{"userAgent": "codex/0.163.0", "codexHome": os.Getenv("CODEX_HOME")})
		case "account/read":
			ack(map[string]any{"account": map[string]any{"type": "apiKey"}, "requiresOpenaiAuth": true})
		case "getAuthStatus":
			ack(map[string]string{"authMethod": "apikey", "authToken": os.Getenv("NEUX_CODEX_ACCOUNT_API_KEY")})
		case "model/list":
			ack(map[string]any{"data": rows})
		case "thread/start", "thread/resume":
			ack(map[string]any{"thread": map[string]any{"id": session}, "model": launch.Model})
		case "turn/start":
			sequence++
			turn := map[string]any{"threadId": session, "turn": map[string]any{"id": fmt.Sprintf("native-%d-%d", os.Getpid(), sequence), "status": "inProgress"}}
			ack(turn)
			emit("turn/started", turn)
			emit("item/completed", map[string]any{"threadId": session, "turnId": turn["turn"].(map[string]any)["id"], "item": map[string]any{"id": "answer", "type": "agentMessage", "phase": "final_answer", "text": "answer"}})
			turn["turn"].(map[string]any)["status"] = "completed"
			emit("turn/completed", turn)
		default:
			if frame["id"] != nil {
				ack(map[string]any{})
			}
		}
	}
	_ = trace.Close()
	os.Exit(0)
}

func TestManagedCodexPreparedLeaseKeepsCatalogAndStartupSeed(t *testing.T) {
	service, _, _ := subscriptionFixture(t)
	service.dataDirectory = t.TempDir()
	account := models.AIAccount{ID: "fedcba9876543210fedcba9876543210", Name: "managed", AuthMode: constants.AccountAuthModeAPIKey, Endpoint: models.AIAccountEndpoint{Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: "https://example.invalid/v1"}, SelectedModels: []string{"first"}}
	if err := service.accounts.Create(t.Context(), account, "catalog-key"); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	input := agent.AccountRequest{Provider: "codex", AccountID: account.ID, CWD: t.TempDir(), Executable: executable, PrefixArgs: []string{"-test.run=^TestManagedCodexCatalogNativeChild$", "--"}}
	first, err := service.ResolveAccount(t.Context(), input, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := service.AcquireAccount(t.Context(), first)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeAccountLease(t, lease.Close) })
	var originalCatalog string
	if err := json.Unmarshal(lease.CodexConfig["model_catalog_json"], &originalCatalog); err != nil {
		t.Fatal(err)
	}
	current, err := service.accounts.Get(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	current.SelectedModels = []string{"second"}
	if err := service.accounts.Update(t.Context(), current, ""); err != nil {
		t.Fatal(err)
	}
	second, err := service.ResolveAccount(t.Context(), input, "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := service.AcquireAccount(t.Context(), second)
	if err != nil {
		t.Fatal(err)
	}
	closeAccountLease(t, other.Close)
	// 第二份配置先覆盖共享文件，随后才启动第一份已取得租约的原生进程。
	var shared struct {
		// Model 是另一份配置当前写入共享文件的启动模型种子。
		Model string `toml:"model"`
	}
	if _, err := toml.DecodeFile(filepath.Join(first.ConfigHome, "config.toml"), &shared); err != nil || shared.Model != "second" {
		t.Fatalf("race fixture did not overwrite shared model: %+v err=%v", shared, err)
	}
	provider := agent.NewProviderRuntime(agent.ProviderRuntimeOptions{AcquireAccount: func(context.Context, agent.AccountRequest) (agent.AccountLease, error) { return lease, nil }})
	t.Cleanup(func() {
		if err := provider.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	trace := filepath.Join(t.TempDir(), "wire")
	connection, err := provider.Open(t.Context(), agent.ProviderConnectionRequest{Account: first, Launch: agent.ProviderLaunch{Executable: executable, PrefixArgs: input.PrefixArgs, Environment: map[string]string{"ACCOUNT_CATALOG_NATIVE_CHILD": "1", "ACCOUNT_CATALOG_TRACE": trace}}}, agent.ProviderConnectionPorts{})
	if err != nil {
		t.Fatal(err)
	}
	client, err := connection.Client()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.NewSession(t.Context(), agent.AgentSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	expected := "startupCatalog=" + originalCatalog + " startupModel=first"
	if !strings.Contains(string(wire), expected) {
		t.Fatalf("prepared catalog and native default model came from different configurations: expected=%s wire=%s", expected, wire)
	}
}
