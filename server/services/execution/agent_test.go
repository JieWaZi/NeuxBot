package execution

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// agentTestJournal 只替代权威 Chat 身份读取，不替代授权注册表和文件系统。
type agentTestJournal struct {
	// Journal 保留不应在本测试调用的完整日志边界。
	chatservice.Journal
	// chat 是测试推进的实际当前执行身份。
	chat models.Chat
	// contextReads 记录误用完整历史准备入口的次数。
	contextReads int
}

func (j *agentTestJournal) LoadExecutionContext(context.Context, string) (chatservice.ExecutionContext, error) {
	j.contextReads++
	return chatservice.ExecutionContext{Chat: j.chat}, nil
}

func (j *agentTestJournal) LoadExecutionChat(context.Context, string) (models.Chat, error) {
	return j.chat, nil
}

func TestWorkflowMCPUsesCustomRuntimeBase(t *testing.T) {
	registry := capability.New(nil)
	defer registry.Close()
	layout, err := chatstorage.NewLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// 本用例只验证传输装配，不把含竞态检测器的测试程序复制进技能包。
	binary := filepath.Join(t.TempDir(), "allycli")
	if err := os.WriteFile(binary, []byte("test CLI file fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	journal := &agentTestJournal{chat: models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "turn"}}
	manager := &service{agent: registry, agentEndpoint: "http://127.0.0.1:1/agent/v1", agentBinary: binary, journal: journal, storage: layout, skillDataDirectory: t.TempDir(), elicitations: newElicitationCoordinator(journal)}
	request := recordedRun{SessionPlan: agentapi.SessionPlan{Target: agentapi.SessionTarget{RuntimeID: "custom-claude"}, Detection: agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{BaseRuntimeID: "claude"}}}}
	capabilities := []ToolCapability{{ID: "workflow", Bindings: []capability.Binding{{Operation: "workflow.source.get", Call: func(context.Context, capability.Request) (capability.Result, error) {
		return capability.Result{}, nil
	}}}}}
	_, release, err := manager.prepareAgent(&request.SessionPlan, &request.TurnInput, journal.chat, "turn", capabilities, &[]string{})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if len(request.Connectors) != 1 || request.Connectors[0].RuntimeKey != "ally_capabilities" {
		t.Fatalf("custom runtime lost MCP: %+v", request.Connectors)
	}
	if !strings.Contains(request.Message.Text, "Use the injected ally_capabilities MCP tools for NeuxBot operations in this execution") || strings.Contains(request.Message.Text, "For all NeuxBot CLI calls") {
		t.Fatalf("MCP workflow received competing CLI instructions: %s", request.Message.Text)
	}
	for _, instruction := range request.Instructions {
		if instruction.Name == "软件能力" && strings.Contains(instruction.Content, "with capabilities") {
			t.Fatalf("MCP workflow still requires CLI discovery: %s", instruction.Content)
		}
	}
}

func TestWorkflowResultSelectsOnlySupportedTransport(t *testing.T) {
	for _, runtimeID := range []string{"codex", "unsupported"} {
		t.Run(runtimeID, func(t *testing.T) {
			registry := capability.New(nil)
			defer registry.Close()
			// 本用例只验证传输装配，CLI 内容不参与执行。
			binary := filepath.Join(t.TempDir(), "allycli")
			if err := os.WriteFile(binary, []byte("test CLI file fixture"), 0o700); err != nil {
				t.Fatal(err)
			}
			journal := &agentTestJournal{chat: models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "turn"}}
			manager := &service{agent: registry, agentEndpoint: "http://127.0.0.1:1/agent/v1", agentBinary: binary, journal: journal, skillDataDirectory: t.TempDir(), elicitations: newElicitationCoordinator(journal)}
			request := recordedRun{SessionPlan: agentapi.SessionPlan{Target: agentapi.SessionTarget{RuntimeID: runtimeID}}}
			capabilities := []ToolCapability{{ID: "result", Bindings: []capability.Binding{{Operation: "workflow.results.schema", Call: func(context.Context, capability.Request) (capability.Result, error) {
				return capability.Result{}, nil
			}}}}}
			_, release, err := manager.prepareAgent(&request.SessionPlan, &request.TurnInput, journal.chat, "turn", capabilities, &[]string{})
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			if runtimeID == "codex" {
				if len(request.Connectors) != 1 || !strings.Contains(request.Message.Text, "Use the injected ally_capabilities MCP tools") || strings.Contains(request.Message.Text, "For all NeuxBot CLI calls") {
					t.Fatalf("result worker did not receive its MCP transport: %+v", request)
				}
			} else if len(request.Connectors) != 0 || !strings.Contains(request.Message.Text, "For all NeuxBot CLI calls") || request.LaunchEnvironment[constants.AgentCLIContextEnvironment] == "" {
				t.Fatalf("unsupported runtime lost its actual CLI fallback: %+v", request)
			}
		})
	}
}

func TestNativeSessionKeepsContextPathButRotatesAndRevokesCredentials(t *testing.T) {
	registry := capability.New(nil)
	defer registry.Close()
	endpoint := "http://127.0.0.1:18080"
	layout, err := chatstorage.NewLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// 本用例只检查打包路径和 HTTP 授权；真实 CLI 命令行为由下方独立回归覆盖。
	binary := filepath.Join(t.TempDir(), "allycli")
	if err := os.WriteFile(binary, []byte("test CLI file fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	journal := &agentTestJournal{chat: models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "first"}}
	programDirectory := t.TempDir()
	manager := &service{agent: registry, agentEndpoint: endpoint + constants.AgentCLIPath, agentBinary: binary, journal: journal, storage: layout, skillDataDirectory: programDirectory, elicitations: newElicitationCoordinator(journal)}
	request := recordedRun{SessionPlan: agentapi.SessionPlan{Target: agentapi.SessionTarget{RuntimeID: "codex", AccountID: "account"}}}
	activate, revoke, err := manager.prepareAgent(&request.SessionPlan, &request.TurnInput, journal.chat, "first", nil, &[]string{})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	cliName := "allycli"
	if runtime.GOOS == "windows" {
		cliName += ".exe"
	}
	skillCLI := filepath.Join(request.Skills[0].Path, "scripts", cliName)
	if request.LaunchEnvironment[constants.AgentCLIBinaryEnvironment] != skillCLI {
		t.Fatalf("CLI must be directly available inside the advertised skill: got %q, want %q", request.LaunchEnvironment[constants.AgentCLIBinaryEnvironment], skillCLI)
	}
	if _, err := os.ReadFile(filepath.Join(request.Skills[0].Path, "references", "wiki-guide.md")); err != nil {
		t.Fatalf("knowledge reference missing beside SKILL.md: %v", err)
	}
	for _, directory := range []string{"system-skills", "agent-contexts"} {
		if _, err := os.Stat(filepath.Join(programDirectory, directory)); err != nil {
			t.Fatalf("program resources were not kept in program data: %s %v", directory, err)
		}
		if _, err := os.Stat(filepath.Join(layout.Root(), directory)); !os.IsNotExist(err) {
			t.Fatalf("program resources leaked into managed files: %s %v", directory, err)
		}
	}
	firstPath := request.LaunchEnvironment[constants.AgentCLIContextEnvironment]
	first, err := capability.ReadConnection(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	revoke()
	journal.chat.ActiveExecutionID = "second"
	secondRequest := recordedRun{SessionPlan: agentapi.SessionPlan{Target: request.Target}}
	activate, revokeSecond, err := manager.prepareAgent(&secondRequest.SessionPlan, &secondRequest.TurnInput, journal.chat, "second", nil, &[]string{})
	if err != nil {
		t.Fatal(err)
	}
	defer revokeSecond()
	activate()
	if secondRequest.LaunchEnvironment[constants.AgentCLIContextEnvironment] != firstPath {
		t.Fatal("continued native process lost its original environment file path")
	}
	second, err := capability.ReadConnection(firstPath)
	if err != nil || second.Token == first.Token || second.GrantID == first.GrantID {
		t.Fatalf("credential did not rotate: %v", err)
	}
	send := func(token string) int {
		request := httptest.NewRequest(http.MethodGet, endpoint+constants.AgentCLIPath+"/capabilities", nil)
		request.RemoteAddr = "127.0.0.1:10000"
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		registry.ServeHTTP(response, request)
		return response.Code
	}
	if send(first.Token) != 401 || send(second.Token) != 200 {
		t.Fatal("old invocation crossed into a later grant")
	}
	journal.chat.WakerID = "other-waker"
	if send(second.Token) != 401 {
		t.Fatal("changed partner retained the old capability grant")
	}
	journal.chat.WakerID = "waker"
	journal.chat.ActiveExecutionID = "other-execution"
	if send(second.Token) != 401 {
		t.Fatal("changed active execution retained the old capability grant")
	}
	journal.chat.ActiveExecutionID = "second"
	if journal.contextReads != 0 {
		t.Fatalf("capability validation loaded full message context %d times", journal.contextReads)
	}
	revokeSecond()
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatal("settled execution left its credential file on disk")
	}
}

func TestKnowledgeCLIPreparationKeepsOnlyTheFrozenGrant(t *testing.T) {
	registry := capability.New(nil)
	defer registry.Close()
	endpoint := httptest.NewServer(registry)
	defer endpoint.Close()
	binary := filepath.Join(t.TempDir(), "allycli")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "../../cmd/allycli")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real CLI: %v\n%s", err, output)
	}
	journal := &agentTestJournal{chat: models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "execution"}}
	manager := &service{agent: registry, agentEndpoint: endpoint.URL + constants.AgentCLIPath, agentBinary: binary, journal: journal, skillDataDirectory: t.TempDir(), elicitations: newElicitationCoordinator(journal)}
	manager.agentBindings = func(models.Chat, string, []string, string) []capability.Binding {
		t.Fatal("scoped command discovered ordinary resources")
		return nil
	}
	bindings := []capability.Binding{}
	for _, op := range []string{"knowledge.jobs.start"} {
		b := capability.Binding{Operation: op, Call: func(context.Context, capability.Request) (capability.Result, error) {
			return capability.Result{}, nil
		}}
		if op == "knowledge.jobs.start" {
			b.Receipt = func(context.Context, string) (capability.Result, error) { return capability.Result{}, nil }
		}
		bindings = append(bindings, b)
	}
	request := recordedRun{SessionPlan: agentapi.SessionPlan{Target: agentapi.SessionTarget{RuntimeID: "codex"}, Policy: agentapi.RunPolicy{Execution: agentapi.ExecutionPolicy{WorkspaceReadOnly: true}}}}
	activate, revoke, err := manager.prepareAgent(&request.SessionPlan, &request.TurnInput, journal.chat, "execution", []ToolCapability{{ID: "knowledge.organize", Bindings: bindings}}, &[]string{})
	if err != nil {
		t.Fatal(err)
	}
	defer revoke()
	activate()
	if len(request.Connectors) != 0 {
		t.Fatalf("knowledge grant added a retired MCP transport: %+v", request.Connectors)
	}
	if request.LaunchEnvironment[constants.AgentCLIContextEnvironment] == "" || !request.Policy.Execution.WorkspaceReadOnly {
		t.Fatal("existing CLI context or workspace policy was lost")
	}
	// 从无技能文件的目录运行，清空 PATH，证明入口与引用均无需搜索。
	command := exec.Command(request.LaunchEnvironment[constants.AgentCLIBinaryEnvironment], "capabilities")
	command.Dir = t.TempDir()
	command.Env = os.Environ()
	for name, value := range request.LaunchEnvironment {
		command.Env = append(command.Env, name+"="+value)
	}
	command.Env = append(command.Env, "PATH=")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("invoke skill-local CLI without PATH: %v\n%s", err, output)
	}
	var response capability.Response
	if err := json.Unmarshal(output, &response); err != nil || !response.OK {
		t.Fatalf("skill-local capabilities failed: %s (%v)", output, err)
	}
	if _, err := os.ReadFile(filepath.Join(request.Skills[0].Path, "references", "wiki-guide.md")); err != nil {
		t.Fatalf("skill-local knowledge reference unavailable: %v", err)
	}
}

// agentPreparationFixture 使用真实授权表、系统技能和连接文件，仅替代 Chat 身份读取。
func agentPreparationFixture(t *testing.T) *service {
	t.Helper()
	registry := capability.New(nil)
	t.Cleanup(registry.Close)
	binary := filepath.Join(t.TempDir(), "allycli")
	if err := os.WriteFile(binary, []byte("test CLI file fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	journal := &agentTestJournal{chat: models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "turn"}}
	return &service{agent: registry, agentEndpoint: "http://127.0.0.1:1/agent/v1", agentBinary: binary, journal: journal, skillDataDirectory: t.TempDir(), elicitations: newElicitationCoordinator(journal)}
}

func agentPreparationBinding(t *testing.T, operation string) capability.Binding {
	t.Helper()
	return capability.Binding{Operation: operation, Call: func(context.Context, capability.Request) (capability.Result, error) {
		t.Fatal("preparation must not execute business operations")
		return capability.Result{}, nil
	}, Receipt: func(context.Context, string) (capability.Result, error) {
		t.Fatal("preparation must not read business receipts")
		return capability.Result{}, nil
	}}
}

func TestWorkflowAuthoringPreparationSuppliesModeAndStableGuideForEveryRuntime(t *testing.T) {
	// 移除共用入口的指南注入或首轮创作角色提醒，应使本回归失败。
	targets := map[string]string{"unsupported": ""}
	for _, base := range agentapi.BaseRuntimeIDs() {
		targets[base] = ""
		targets["custom-"+base] = base
	}
	for runtimeID, baseRuntimeID := range targets {
		t.Run(runtimeID, func(t *testing.T) {
			manager := agentPreparationFixture(t)
			journal := manager.journal.(*agentTestJournal)
			manager.agentBindings = func(models.Chat, string, []string, string) []capability.Binding {
				return []capability.Binding{agentPreparationBinding(t, "workflow.authoring.prepare"), agentPreparationBinding(t, "workflow.source.get"), agentPreparationBinding(t, "workflow.source.save")}
			}
			guide, err := builtinskills.Guide("workflow.authoring")
			if err != nil {
				t.Fatal(err)
			}
			var stableInstructions []agentapi.Instruction
			for index, message := range []string{"访问多个资讯网，然后汇总AI新闻", "改成每周一", "继续", "为什么需要汇总步骤？"} {
				name := []string{"first-business-goal", "hot-short-edit", "restored-short-follow-up", "pure-explanation"}[index]
				t.Run(name, func(t *testing.T) {
					journal.chat.ActiveExecutionID = name
					request := recordedRun{SessionPlan: agentapi.SessionPlan{Target: agentapi.SessionTarget{RuntimeID: runtimeID}, Detection: agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{BaseRuntimeID: baseRuntimeID}}, Instructions: []agentapi.Instruction{{Name: "PARTNER.md", Content: "Answer simple requests directly."}}}, TurnInput: agentapi.TurnInput{Message: agentapi.Message{Text: message}}}
					_, release, err := manager.prepareAgent(&request.SessionPlan, &request.TurnInput, journal.chat, name, nil, nil)
					if err != nil {
						t.Fatal(err)
					}
					defer release()
					inline := ""
					for _, instruction := range request.Instructions {
						if instruction.Name == "WORKFLOW_AUTHORING.md" {
							inline = instruction.Content
						}
					}
					if inline == "" || !strings.Contains(inline, guide) {
						t.Error("authoring preparation did not supply the complete bundled guide in stable instructions")
					}
					for _, required := range []string{"a bare business goal", "first message", "native CLI workflow", "Pure explanations", "saved:true"} {
						if !strings.Contains(strings.ToLower(inline), strings.ToLower(required)) {
							t.Errorf("inlined guide lost authoring contract %q", required)
						}
					}
					if index == 0 {
						stableInstructions = request.Instructions
					} else if !reflect.DeepEqual(request.Instructions, stableInstructions) {
						t.Error("turn content or restored session changed the stable authoring instructions")
					}
					// 热会话不重载 Instructions，新增输入仍须独立表达创作角色。
					for _, required := range []string{"Create or modify this reusable NeuxBot workflow", "first business goal", "Do not perform the business task", "native CLI workflow", "Pure explanations need no save", "structured question", "saved receipt"} {
						if !strings.Contains(request.Message.Text, required) {
							t.Errorf("%s prompt lost mode contract %q", name, required)
						}
					}
					if !strings.HasPrefix(request.Message.Text, message+"\n\n") || strings.Contains(request.Message.Text, guide) {
						t.Error("preparation rewrote user input, copied the full guide into this turn, or changed native continuation")
					}
					if runtimeID != "unsupported" {
						if len(request.Connectors) != 1 || request.Connectors[0].RuntimeKey != "ally_capabilities" || strings.Contains(request.Message.Text, "For all NeuxBot CLI calls") {
							t.Errorf("base/custom runtime lost its single MCP authoring transport: %+v", request.Connectors)
						}
					} else if len(request.Connectors) != 0 || !strings.Contains(request.Message.Text, "For all NeuxBot CLI calls") || request.LaunchEnvironment[constants.AgentCLIContextEnvironment] == "" {
						t.Error("unknown runtime lost the authorized CLI fallback")
					}
				})
			}
		})
	}
}

func TestWorkflowAuthoringPreparationDoesNotOverrideOrdinaryChatOrWorker(t *testing.T) {
	for _, operation := range []string{"", "workflow.results.schema", "workflow.results.submit", "workflow.source.get"} {
		t.Run(operation, func(t *testing.T) {
			manager := agentPreparationFixture(t)
			journal := manager.journal.(*agentTestJournal)
			var capabilities []ToolCapability
			if operation != "" {
				capabilities = []ToolCapability{{ID: "worker", Bindings: []capability.Binding{agentPreparationBinding(t, operation)}}}
			}
			request := recordedRun{SessionPlan: agentapi.SessionPlan{Target: agentapi.SessionTarget{RuntimeID: "codex"}}, TurnInput: agentapi.TurnInput{Message: agentapi.Message{Text: "访问多个资讯网，然后汇总AI新闻"}}}
			_, release, err := manager.prepareAgent(&request.SessionPlan, &request.TurnInput, journal.chat, "turn", capabilities, &[]string{})
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			if strings.Contains(request.Message.Text, "<workflow-authoring-context>") || strings.Contains(request.Message.Text, "<workflow-reference-context>") {
				t.Fatal("ordinary chat or execution worker received the authoring role")
			}
			for _, instruction := range request.Instructions {
				if instruction.Name == "WORKFLOW_AUTHORING.md" {
					t.Fatal("non-authoring grant received the authoring guide")
				}
			}
		})
	}
}
