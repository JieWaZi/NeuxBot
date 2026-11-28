package global

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	ginxlogx "github.com/shrewx/ginx/pkg/logx"
)

type routerTestJournal struct {
	// NativeJournal 只满足 Execution 的构造契约，路由过程无需访问 Chat 日志。
	chatservice.NativeJournal
}

func TestMainConversationRouterUsesSelectedCLIInPrivateScratch(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var timingLogs bytes.Buffer
	logger := ginxlogx.Instance()
	originalOutput := logger.Out
	logger.SetOutput(&timingLogs)
	t.Cleanup(func() { logger.SetOutput(originalOutput) })
	root := t.TempDir()
	trace := filepath.Join(root, "native-wire")
	t.Setenv("NEUX_ROUTER_TEST_NATIVE_WIRE", trace)
	previousConfig, previousAccounts, previousThreads := Config, AIAccountService, AgentThreads
	previousEnvironments, previousExecution := RuntimeEnvironmentControllerManager, ExecutionService
	t.Cleanup(func() {
		Config, AIAccountService, AgentThreads = previousConfig, previousAccounts, previousThreads
		RuntimeEnvironmentControllerManager, ExecutionService = previousEnvironments, previousExecution
	})
	Config = &Configuration{DataDirectory: root, ManagedFilesDirectory: t.TempDir()}
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(root, "router.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	store, err := conversation.NewStore(Config.ManagedFilesDirectory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	AIAccountService = aiaccount.New(controllers.NewAIAccountController(database.DB))
	providers := agent.NewProviderRuntime(agent.ProviderRuntimeOptions{AcquireAccount: AIAccountService.AcquireAccount})
	t.Cleanup(func() { _ = providers.Close(context.WithoutCancel(ctx)) })
	AgentThreads = agent.NewThreadLifecycle(providers, store)
	firstLifecycle := AgentThreads
	t.Cleanup(func() { _ = firstLifecycle.Close(context.WithoutCancel(ctx)) })
	RuntimeEnvironmentControllerManager = controllers.NewRuntimeEnvironmentController(database.DB)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	definition := agent.RuntimeSummary{
		ID: "router-codex", BaseRuntimeID: constants.RuntimeIDCodex, InstructionFileName: "AGENTS.md",
		PrefixArgs: []string{"-test.run=^TestMainConversationRouterNativeChild$", "--"}, ConfigHome: t.TempDir(),
	}
	if err := RuntimeEnvironmentControllerManager.Ensure(ctx, models.RuntimeEnvironment{
		ID: "router-codex", RuntimeID: definition.ID, Enabled: true, DefaultModel: "selected-model", DefaultReasoning: "high",
		Detection: agent.DetectionResult{Runtime: definition, State: agent.DetectionStateUnavailable, ResolvedBinary: executable},
	}); err != nil {
		t.Fatal(err)
	}
	ExecutionService, err = execution.New(execution.Config{
		Journal: routerTestJournal{}, StorageDirectory: Config.ManagedFilesDirectory,
		Threads: AgentThreads, ResolveAccount: AIAccountService.ResolveAccount, Accounts: AIAccountService,
		RuntimeEnvironments: RuntimeEnvironmentControllerManager, RuntimeSummaries: []agent.RuntimeSummary{definition},
		Wakers: controllers.NewWakerController(database.DB), Profiles: controllers.NewProfileDocumentController(database.DB),
		Projects: controllers.NewProjectController(database.DB),
	})
	if err != nil {
		t.Fatal(err)
	}
	const instruction = "router-system-rule"
	const input = `{"content":"router-user-input","history":[{"content":"cold-bootstrap-history"}]}`
	const followUp = `{"content":"router-second-input","history":[{"content":"do-not-replay-hot-history"}]}`
	const decision = `{"action":"answer","content":"ready"}`
	options := execution.WorkOptions{
		RuntimeID: definition.ID, PermissionLevel: constants.PermissionLevelFullAccess,
		SupplementalInstructions: []agent.Instruction{{Name: "worker instruction", Content: "for-worker-only"}},
	}
	answer, firstErr := GenerateMainConversationRoute(ctx, "conversation-a", "owner-waker", options, instruction, input, false, nil)
	second, secondErr := GenerateMainConversationRoute(ctx, "conversation-a", "owner-waker", options, instruction, followUp, false, nil)
	if err := AgentThreads.Close(ctx); err != nil {
		t.Fatal(err)
	}
	store.Close()
	reopened, err := conversation.NewStore(Config.ManagedFilesDirectory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	AgentThreads = agent.NewThreadLifecycle(providers, reopened)
	t.Cleanup(func() { _ = AgentThreads.Close(context.WithoutCancel(ctx)) })
	third, thirdErr := GenerateMainConversationRoute(ctx, "conversation-a", "owner-waker", options, instruction,
		`{"content":"router-after-restart","history":[{"content":"do-not-replay-resumed-history"}]}`, false, nil)
	replayed, replayErr := GenerateMainConversationRoute(ctx, "conversation-a", "owner-waker", options, instruction,
		`{"content":"router-after-interrupted-publication","history":[{"content":"canonical-history-after-interruption"}]}`, true, nil)
	other, otherErr := GenerateMainConversationRoute(ctx, "conversation-a", "another-waker", options, instruction,
		`{"content":"new-responsible-partner","history":[{"content":"canonical-history-for-new-partner"}]}`, false, nil)
	_, staleErr := GenerateMainConversationRoute(ctx, "conversation-a", "departed-waker", options, instruction,
		`{"content":"forbidden-stale-input"}`, false, func(context.Context) error { return status_error.ChatReplyTargetUnavailable })
	wire, wireErr := os.ReadFile(trace)
	if firstErr != nil || answer != decision || secondErr != nil || second != decision || thirdErr != nil || third != decision ||
		replayErr != nil || replayed != decision || otherErr != nil || other != decision || wireErr != nil {
		t.Fatalf("router errors = %v, %v, %v, %v, %v, native trace = %s (%v)", firstErr, secondErr, thirdErr, replayErr, otherErr, wire, wireErr)
	}
	if !errors.Is(staleErr, status_error.ChatReplyTargetUnavailable) {
		t.Fatalf("stale partner prompt should fail at the host gate: %v", staleErr)
	}
	log := string(wire)
	if strings.Count(log, `"method":"thread/start"`) != 4 || strings.Count(log, `"method":"thread/resume"`) != 1 || strings.Count(log, `"method":"turn/start"`) != 5 {
		t.Fatalf("wrong Router native session transitions: %s", log)
	}
	scratch, err := filepath.EvalSymlinks(filepath.Join(root, "main-conversation-router-scratch"))
	if err != nil || !strings.Contains(log, "cwd="+scratch+string(filepath.Separator)) {
		t.Fatalf("router did not use private scratch: %s, %v", log, err)
	}
	for _, token := range []string{instruction, "router-user-input", "router-second-input", "router-after-restart", "cold-bootstrap-history", "canonical-history-after-interruption", "canonical-history-for-new-partner", `"model":"selected-model"`, `"effort":"high"`, `"sandbox":"read-only"`,
		`"features.shell_tool":false`, `"features.apply_patch_freeform":false`, `"web_search":"disabled"`, `"id":"router-approval","result":{"decision":"cancel"}`} {
		if !strings.Contains(log, token) {
			t.Errorf("native input omitted %q: %s", token, log)
		}
	}
	for _, token := range []string{"for-worker-only", "do-not-replay-hot-history", "do-not-replay-resumed-history", "forbidden-stale-input", `"ephemeral":true`, `"method":"account/read"`, `"method":"account/rateLimits/read"`} {
		if strings.Contains(log, token) {
			t.Errorf("native input unexpectedly contained %q: %s", token, log)
		}
	}
	if strings.Count(log, `"method":"model/list"`) > 5 {
		t.Fatalf("router sent extra turns or independent model reads: %s", log)
	}
	if entries, err := os.ReadDir(scratch); err != nil || len(entries) != 3 {
		t.Fatalf("Router did not retain an isolated directory per conversation and serving partner: %v, %v", entries, err)
	}
	if saved, err := os.ReadDir(filepath.Join(Config.ManagedFilesDirectory, "native-text-bindings")); err != nil || len(saved) != 2 {
		t.Fatalf("a rejected prompt retained a private binding: %v (%v)", saved, err)
	}
	uses := 0
	for shard := range conversation.UsageShards {
		high, err := reopened.UsageHighWater(ctx, shard)
		if err != nil {
			t.Fatal(err)
		}
		if high == 0 {
			continue
		}
		items, _, err := reopened.ReadUsageTerminals(ctx, shard, 0, high)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			result := item.Result
			if result.Attribution != nil && result.Attribution.WakerID == "owner-waker" && result.Attribution.Source == "main_conversation_router" && result.Turn.Usage != nil && result.FinalMessage == nil {
				uses++
			}
		}
	}
	if uses != 4 {
		t.Fatalf("private router usage entries = %d, want 4", uses)
	}
	if err := AgentThreads.Close(ctx); err != nil {
		t.Fatal(err)
	}
	logs := timingLogs.String()
	if strings.Count(logs, "main conversation router timing:") != 6 || !strings.Contains(logs, "action=reuse reason=compatible_session") ||
		!strings.Contains(logs, "action=resume reason=connection_reopened") || !strings.Contains(logs, "action=start reason=explicit_new") ||
		!strings.Contains(logs, "native_session_preparation=") || !strings.Contains(logs, "native_turn=") || !strings.Contains(logs, "settlement=") ||
		strings.Contains(logs, "canonical-history-after-interruption") {
		t.Fatalf("Router timing or decision reports are incomplete or contain the prompt: %s", logs)
	}
}

func TestMainConversationRouterNativeChild(t *testing.T) {
	path := os.Getenv("NEUX_ROUTER_TEST_NATIVE_WIRE")
	if path == "" {
		return
	}
	trace, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		os.Exit(2)
	}
	cwd, _ := os.Getwd()
	fmt.Fprintf(trace, "cwd=%s argv=%v\n", cwd, os.Args)
	threadID := fmt.Sprintf("router-thread-%d", os.Getpid())
	turnNumber := 0
	turnID := ""
	output := json.NewEncoder(os.Stdout)
	input := bufio.NewScanner(os.Stdin)
	emit := func(method string, params any) { _ = output.Encode(map[string]any{"method": method, "params": params}) }
	turn := func(status string) map[string]any {
		return map[string]any{"threadId": threadID, "turn": map[string]any{"id": turnID, "status": status}}
	}
	for input.Scan() {
		fmt.Fprintln(trace, input.Text())
		var frame map[string]any
		if err := json.Unmarshal(input.Bytes(), &frame); err != nil {
			os.Exit(2)
		}
		respond := func(value any) { _ = output.Encode(map[string]any{"id": frame["id"], "result": value}) }
		if frame["id"] == "router-approval" {
			tokens := map[string]any{"inputTokens": 19, "outputTokens": 7, "totalTokens": 26}
			emit("thread/tokenUsage/updated", map[string]any{"threadId": threadID, "turnId": turnID, "tokenUsage": map[string]any{"last": tokens, "total": tokens}})
			emit("item/completed", map[string]any{"threadId": threadID, "turnId": turnID, "item": map[string]any{
				"id": "message", "type": "agentMessage", "phase": "final_answer", "text": `{"action":"answer","content":"ready"}`,
			}})
			emit("turn/completed", turn("completed"))
			continue
		}
		switch frame["method"] {
		case "initialize":
			respond(map[string]any{"userAgent": "codex/0.163.0"})
		case "model/list":
			respond(map[string]any{"data": []any{map[string]any{"id": "selected-model", "model": "selected-model", "displayName": "Selected", "isDefault": true,
				"supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": "high"}}}}})
		case "thread/start", "thread/resume":
			params, _ := frame["params"].(map[string]any)
			if restored, ok := params["threadId"].(string); ok {
				threadID = restored
			}
			respond(map[string]any{"thread": map[string]any{"id": threadID, "ephemeral": params["ephemeral"]}, "model": "selected-model"})
		case "turn/start":
			turnNumber++
			turnID = fmt.Sprintf("router-turn-%d-%d", os.Getpid(), turnNumber)
			respond(turn("inProgress"))
			emit("turn/started", turn("inProgress"))
			_ = output.Encode(map[string]any{"id": "router-approval", "method": "item/commandExecution/requestApproval", "params": map[string]any{
				"threadId": threadID, "turnId": turnID, "itemId": "router-tool", "command": "fixture command",
			}})
		default:
			if frame["id"] != nil {
				respond(map[string]any{})
			}
		}
	}
	_ = trace.Close()
	os.Exit(0)
}
