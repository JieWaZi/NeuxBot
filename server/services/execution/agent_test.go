package execution

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
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
	// summaryError 模拟阅读投影无法提供当前摘要。
	summaryError error
}

func (j *agentTestJournal) LoadExecutionContext(context.Context, string) (chatservice.ExecutionContext, error) {
	j.contextReads++
	return chatservice.ExecutionContext{Chat: j.chat}, nil
}

func (j *agentTestJournal) LoadExecutionChat(context.Context, string) (models.Chat, error) {
	return j.chat, j.summaryError
}

func reserveAgentTestExecution(t *testing.T, manager *service, chatID, executionID string) func() {
	t.Helper()
	if manager.executions == nil {
		manager.executions = newExecutionRegistry()
		manager.executions.start(t.Context())
		t.Cleanup(manager.executions.close)
	}
	if _, err := manager.executions.reserve(chatID, executionID, ""); err != nil {
		t.Fatal(err)
	}
	release := sync.OnceFunc(func() { manager.executions.release(chatID, executionID) })
	t.Cleanup(release)
	return release
}

func TestWorkflowMCPUsesCustomRuntimeBase(t *testing.T) {
	registry := capability.New(nil)
	defer registry.Close()
	layout, err := chatstorage.NewLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// 本用例只验证传输装配，不把含竞态检测器的测试程序复制进技能包。
	binary := filepath.Join(t.TempDir(), "botcli")
	if err := os.WriteFile(binary, []byte("test CLI file fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	journal := &agentTestJournal{chat: models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "turn"}}
	manager := &service{agent: registry, agentEndpoint: "http://127.0.0.1:1/agent/v1", journal: journal, storage: layout, skillDataDirectory: t.TempDir(), elicitations: newElicitationCoordinator(journal)}
	manager.systemSkillDirectory = publishedTestSystemSkill(t, manager.skillDataDirectory, binary)
	request := recordedRun{Runtime: runtimeSelection{RuntimeID: "custom-claude"}, Detection: agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{BaseRuntimeID: "claude"}}}
	capabilities := []ToolCapability{{ID: "workflow", Bindings: []capability.Binding{{Operation: "workflow.source.get", Call: func(context.Context, capability.Request) (capability.Result, error) {
		return capability.Result{}, nil
	}}}}}
	_, release, err := request.prepare(manager, journal.chat, "turn", capabilities, &[]string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if len(request.Connectors) != 1 || request.Connectors[0].RuntimeKey != "bot_capabilities" {
		t.Fatalf("custom runtime lost MCP: %+v", request.Connectors)
	}
	if !strings.Contains(agentapi.DeveloperInstructions(request.Instructions), "Use the injected bot_capabilities MCP tools for NeuxBot operations in this execution") || strings.Contains(request.prompt(), "connection.json") || request.CLIContextFile != "" {
		t.Fatalf("MCP workflow received competing CLI instructions: %+v", request)
	}
	for _, instruction := range request.Instructions {
		if instruction.Name == "NeuxBot capabilities" && strings.Contains(instruction.Content, "with capabilities") {
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
			binary := filepath.Join(t.TempDir(), "botcli")
			if err := os.WriteFile(binary, []byte("test CLI file fixture"), 0o700); err != nil {
				t.Fatal(err)
			}
			journal := &agentTestJournal{chat: models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "turn"}}
			manager := &service{agent: registry, agentEndpoint: "http://127.0.0.1:1/agent/v1", journal: journal, skillDataDirectory: t.TempDir(), elicitations: newElicitationCoordinator(journal)}
			manager.systemSkillDirectory = publishedTestSystemSkill(t, manager.skillDataDirectory, binary)
			request := recordedRun{Runtime: runtimeSelection{RuntimeID: runtimeID}}
			capabilities := []ToolCapability{{ID: "result", Bindings: []capability.Binding{{Operation: "workflow.results.schema", Call: func(context.Context, capability.Request) (capability.Result, error) {
				return capability.Result{}, nil
			}}}}}
			_, release, err := request.prepare(manager, journal.chat, "turn", capabilities, &[]string{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			if runtimeID == "codex" {
				if len(request.Connectors) != 1 || !strings.Contains(agentapi.DeveloperInstructions(request.Instructions), "Use the injected bot_capabilities MCP tools") || strings.Contains(request.prompt(), "connection.json") || request.CLIContextFile != "" {
					t.Fatalf("result worker did not receive its MCP transport: %+v", request)
				}
			} else if len(request.Connectors) != 0 || strings.Contains(request.prompt(), "connection.json") || request.thread().Connection.Launch.BotCLIContextFile == "" {
				t.Fatalf("unsupported runtime lost its actual CLI fallback: %+v", request)
			}
		})
	}
}

func TestNativeSessionReusesResidentCredentialAcrossExecutions(t *testing.T) {
	registry := capability.New(nil)
	defer registry.Close()
	endpoint := "http://127.0.0.1:18080"
	layout, err := chatstorage.NewLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// 本用例只检查打包路径和 HTTP 授权；真实 CLI 命令行为由下方独立回归覆盖。
	binary := filepath.Join(t.TempDir(), "botcli")
	if err := os.WriteFile(binary, []byte("test CLI file fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	journal := &agentTestJournal{chat: models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "first"}}
	programDirectory := t.TempDir()
	manager := &service{agent: registry, agentEndpoint: endpoint + constants.AgentCLIPath, journal: journal, storage: layout, skillDataDirectory: programDirectory, elicitations: newElicitationCoordinator(journal)}
	manager.systemSkillDirectory = publishedTestSystemSkill(t, manager.skillDataDirectory, binary)
	request := recordedRun{Runtime: runtimeSelection{RuntimeID: "codex", AccountID: "account"}}
	activate, revoke, err := request.prepare(manager, journal.chat, "first", nil, &[]string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	releaseFirst := reserveAgentTestExecution(t, manager, journal.chat.ID, "first")
	activate()
	cliName := "botcli"
	if runtime.GOOS == "windows" {
		cliName += ".exe"
	}
	skillCLI := filepath.Join(request.Skills[0].Path, "scripts", cliName)
	if builtinskills.CLIPath(request.Skills[0].Path) != skillCLI {
		t.Fatalf("CLI must be directly available inside the advertised skill: got %q, want %q", builtinskills.CLIPath(request.Skills[0].Path), skillCLI)
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
	firstPath := preparedContextPath(t, request)
	if strings.Contains(request.prompt(), firstPath) {
		t.Fatal("host authorization path entered the model input")
	}
	first, err := capability.ReadConnection(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	firstFile, err := os.Stat(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	revoke()
	releaseFirst()
	journal.chat.ActiveExecutionID = "second"
	secondRequest := recordedRun{Runtime: request.Runtime}
	activate, revokeSecond, err := secondRequest.prepare(manager, journal.chat, "second", nil, &[]string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer revokeSecond()
	releaseSecond := reserveAgentTestExecution(t, manager, journal.chat.ID, "second")
	activate()
	if preparedContextPath(t, secondRequest) != firstPath {
		t.Fatal("continued native process lost its stable explicit context path")
	}
	second, err := capability.ReadConnection(firstPath)
	if err != nil || second != first {
		t.Fatalf("resident credential changed between executions: %v", err)
	}
	if secondFile, err := os.Stat(firstPath); err != nil || !os.SameFile(firstFile, secondFile) {
		t.Fatalf("continuation rewrote the resident connection file: %v", err)
	}
	send := func(token string) int {
		request := httptest.NewRequest(http.MethodGet, endpoint+constants.AgentCLIPath+"/capabilities", nil)
		request.RemoteAddr = "127.0.0.1:10000"
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		registry.ServeHTTP(response, request)
		return response.Code
	}
	journal.summaryError = status_error.ReadingProjectionIncomplete
	if send(first.Token) != http.StatusOK {
		t.Fatal("resident credential could not access the current execution")
	}
	releaseSecond()
	reserveAgentTestExecution(t, manager, journal.chat.ID, "other-execution")
	if send(second.Token) != http.StatusUnauthorized {
		t.Fatal("changed active execution retained the old capability grant")
	}
	if journal.contextReads != 0 {
		t.Fatalf("capability validation loaded full message context %d times", journal.contextReads)
	}
	revokeSecond()
	if send(first.Token) != 401 {
		t.Fatal("idle connection retained completed execution capabilities")
	}
	if idle, err := capability.ReadConnection(firstPath); err != nil || idle != first {
		t.Fatal("settling an execution removed or changed the resident credential")
	}
	registry.Close()
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatal("Core shutdown left a resident credential on disk")
	}
}

func TestKnowledgeCLIPreparationKeepsOnlyTheFrozenGrant(t *testing.T) {
	registry := capability.New(nil)
	defer registry.Close()
	endpoint := httptest.NewServer(registry)
	defer endpoint.Close()
	binary := filepath.Join(t.TempDir(), "botcli")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "../../cmd/botcli")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real CLI: %v\n%s", err, output)
	}
	journal := &agentTestJournal{chat: models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "execution"}}
	manager := &service{agent: registry, agentEndpoint: endpoint.URL + constants.AgentCLIPath, journal: journal, skillDataDirectory: t.TempDir(), elicitations: newElicitationCoordinator(journal)}
	manager.systemSkillDirectory = publishedTestSystemSkill(t, manager.skillDataDirectory, binary)
	manager.agentBindings = func(models.Chat, string, []string, string, string) []capability.Binding {
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
	request := recordedRun{Runtime: runtimeSelection{RuntimeID: "codex"}, ReadOnly: true}
	activate, revoke, err := request.prepare(manager, journal.chat, "execution", []ToolCapability{{ID: "knowledge.organize", Bindings: bindings}}, &[]string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer revoke()
	reserveAgentTestExecution(t, manager, journal.chat.ID, "execution")
	activate()
	if len(request.Connectors) != 0 {
		t.Fatalf("knowledge grant added a retired MCP transport: %+v", request.Connectors)
	}
	if preparedContextPath(t, request) == "" || !request.ReadOnly {
		t.Fatal("existing CLI context or workspace policy was lost")
	}
	// 从无技能文件的目录运行，清空 PATH，证明入口与引用均无需搜索。
	command := exec.Command(builtinskills.CLIPath(request.Skills[0].Path), "capabilities")
	command.Dir = t.TempDir()
	command.Env = os.Environ()
	command.Env = append(command.Env, constants.AgentCLIContextEnvironment+"="+request.thread().Connection.Launch.BotCLIContextFile)
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
	binary := filepath.Join(t.TempDir(), "botcli")
	if err := os.WriteFile(binary, []byte("test CLI file fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	journal := &agentTestJournal{chat: models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "turn"}}
	manager := &service{agent: registry, agentEndpoint: "http://127.0.0.1:1/agent/v1", journal: journal, skillDataDirectory: t.TempDir(), elicitations: newElicitationCoordinator(journal)}
	manager.systemSkillDirectory = publishedTestSystemSkill(t, manager.skillDataDirectory, binary)
	return manager
}

func TestExecutionCancellationRevokesToolsBeforeNativeSettlement(t *testing.T) {
	manager := agentPreparationFixture(t)
	manager.executions = newExecutionRegistry()
	manager.executions.start(t.Context())
	t.Cleanup(manager.executions.close)
	conversation := manager.journal.(*agentTestJournal).chat
	executionContext, err := manager.executions.reserve(conversation.ID, "turn", "submission")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.executions.release(conversation.ID, "turn") })
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	request := pendingMessage{runtime: runtimeSelection{RuntimeID: "unsupported"}, allowedAgentDomains: &[]string{},
		toolCapabilities: []ToolCapability{{Bindings: []capability.Binding{{Operation: "im.targets.list", Call: func(ctx context.Context, _ capability.Request) (capability.Result, error) {
			entered <- ctx
			<-release
			return capability.Result{}, ctx.Err()
		}}}}},
	}
	var skills []agentapi.Skill
	var instructions, contextInstructions []agentapi.Instruction
	var connectors []agentapi.ConnectorDeclaration
	sender, senderFinished := context.WithCancel(t.Context())
	defer senderFinished()
	grant, err := manager.prepareAgent(sender, executionContext, models.RuntimeEnvironment{}, agentapi.Workspace{Path: t.TempDir()}, &skills, &instructions, &connectors, &contextInstructions, conversation, "turn", request, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(grant.finish)
	grant.activate()
	connection, err := capability.ReadConnection(grant.cliContextFile)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, constants.AgentCLIPath+path, strings.NewReader(body))
		request.RemoteAddr = "127.0.0.1:12345"
		request.Header.Set("Authorization", "Bearer "+connection.Token)
		response := httptest.NewRecorder()
		manager.agent.ServeHTTP(response, request)
		return response
	}
	// 发送 HTTP 请求结束不会结束已经接纳的执行及工具连接。
	senderFinished()
	if response := call(http.MethodGet, "/capabilities", ""); response.Code != http.StatusOK {
		t.Fatalf("sending caller owned the admitted grant: %d %s", response.Code, response.Body)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		call(http.MethodPost, "/commands", `{"operation":"im.targets.list","params":{}}`)
	}()
	var commandContext context.Context
	select {
	case commandContext = <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("tool call did not enter")
	}
	manager.CancelExecution(conversation.ID, "turn")
	if !manager.IsExecutionActive(conversation.ID, "turn") {
		t.Fatal("test lost the native settlement window")
	}
	if commandContext.Err() != context.Canceled {
		t.Fatal("execution cancellation did not synchronously cancel the in-flight command")
	}
	if response := call(http.MethodGet, "/capabilities", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("cancelled execution retained tools before native settlement: %d %s", response.Code, response.Body)
	}
	unblock()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled command did not finish")
	}
}

func TestAgentPreparationFreezesRealReferencedFilesIndependentOfNativeProjection(t *testing.T) {
	manager := agentPreparationFixture(t)
	chat := manager.journal.(*agentTestJournal).chat
	root := t.TempDir()
	selected := filepath.Join(root, "report.txt")
	if err := os.WriteFile(selected, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	references, err := prepareCodeReferences([]workspace.ProjectLink{{EntryName: "project", SourcePath: root}}, []CodeReferenceInput{{Path: selected, StartLine: 1, EndLine: 1, Content: "original"}})
	if err != nil {
		t.Fatal(err)
	}
	files := []string{references[0].Path}
	var observed []string
	bindings := []ToolCapability{{Bindings: []capability.Binding{{Operation: "im.targets.list", Call: func(ctx context.Context, _ capability.Request) (capability.Result, error) {
		observed = capability.FilePaths(ctx)
		copy := capability.FilePaths(ctx)
		copy[0] = "mutated copy"
		if !reflect.DeepEqual(observed, capability.FilePaths(ctx)) {
			t.Fatal("capability caller mutated frozen files")
		}
		return capability.Result{}, status_error.AgentOperationForbidden
	}}}}}
	request := recordedRun{Runtime: runtimeSelection{RuntimeID: "unsupported"}, Message: agentapi.Message{CodeReferences: []agentapi.CodeReference{{Path: "projects/chat/project/report.txt"}}}}
	activate, revoke, err := request.prepare(manager, chat, "turn", bindings, &[]string{}, files)
	if err != nil {
		t.Fatal(err)
	}
	defer revoke()
	reserveAgentTestExecution(t, manager, chat.ID, "turn")
	activate()
	files[0] = filepath.Join(root, "unreferenced.txt")
	connection, err := capability.ReadConnection(preparedContextPath(t, request))
	if err != nil {
		t.Fatal(err)
	}
	input := httptest.NewRequest(http.MethodPost, constants.AgentCLIPath+"/commands", strings.NewReader(`{"operation":"im.targets.list","params":{}}`))
	input.RemoteAddr = "127.0.0.1:12345"
	input.Header.Set("Authorization", "Bearer "+connection.Token)
	response := httptest.NewRecorder()
	manager.agent.ServeHTTP(response, input)
	if response.Code != http.StatusForbidden || !reflect.DeepEqual(observed, []string{references[0].Path}) {
		t.Fatalf("grant lost real file identity: files=%v response=%s", observed, response.Body)
	}
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

func TestWorkflowAuthoringPreparationKeepsBoundedStableInstructions(t *testing.T) {
	// 创作启动约定进入稳定指令，完整指南由授权工具提供。
	targets := map[string]string{"unsupported": ""}
	for _, base := range agentapi.BaseRuntimeIDs() {
		targets[base] = ""
		targets["custom-"+base] = base
	}
	for runtimeID, baseRuntimeID := range targets {
		t.Run(runtimeID, func(t *testing.T) {
			manager := agentPreparationFixture(t)
			journal := manager.journal.(*agentTestJournal)
			manager.agentBindings = func(models.Chat, string, []string, string, string) []capability.Binding {
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
					request := recordedRun{Runtime: runtimeSelection{RuntimeID: runtimeID}, Detection: agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{BaseRuntimeID: baseRuntimeID}}, Instructions: []agentapi.Instruction{{Name: "PARTNER.md", Content: "Answer simple requests directly."}}, Message: agentapi.Message{Text: message}}
					_, release, err := request.prepare(manager, journal.chat, name, nil, nil, nil)
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
					if len(inline) >= len(guide) || strings.Contains(inline, guide) {
						t.Error("authoring bootstrap must remain smaller than the full guide")
					}
					for _, required := range []string{"a bare business goal", "first message", "native CLI workflow", "Pure explanations", "saved:true", "workflow_authoring_prepare", "workflow-authoring.md"} {
						if !strings.Contains(strings.ToLower(inline), strings.ToLower(required)) {
							t.Errorf("authoring bootstrap lost contract %q", required)
						}
					}
					native := request.thread().Session.DeveloperInstructions
					if native == nil || strings.Count(*native, inline) != 1 {
						t.Error("native developer instructions must carry the authoring bootstrap exactly once")
					}
					if index == 0 {
						stableInstructions = request.Instructions
					} else if !reflect.DeepEqual(request.Instructions, stableInstructions) {
						t.Error("turn content or restored session changed the stable authoring instructions")
					}
					if request.Message.Text != message || strings.Contains(request.prompt(), guide) || strings.Contains(request.prompt(), "<workflow-authoring-context>") {
						t.Error("preparation rewrote user input or repeated the stable authoring role in this turn")
					}
					if runtimeID != "unsupported" {
						if len(request.Connectors) != 1 || request.Connectors[0].RuntimeKey != "bot_capabilities" || strings.Contains(request.prompt(), "connection.json") || request.CLIContextFile != "" {
							t.Errorf("base/custom runtime lost its single MCP authoring transport: %+v", request.Connectors)
						}
					} else if len(request.Connectors) != 0 || strings.Contains(request.prompt(), "connection.json") || request.thread().Connection.Launch.BotCLIContextFile == "" {
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
			request := recordedRun{Runtime: runtimeSelection{RuntimeID: "codex"}, Message: agentapi.Message{Text: "访问多个资讯网，然后汇总AI新闻"}}
			_, release, err := request.prepare(manager, journal.chat, "turn", capabilities, &[]string{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			if strings.Contains(request.prompt(), "Current workflow references") {
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

func TestLocalWorkflowTurnsShareCredentialAndKeepTheCreationPromptLocal(t *testing.T) {
	manager := agentPreparationFixture(t)
	journal := manager.journal.(*agentTestJournal)
	prepare := func(chatID, sessionID string) capability.Connection {
		t.Helper()
		journal.chat.ID, journal.chat.ActiveExecutionID = chatID, "turn-"+chatID
		request := pendingMessage{
			runtime:            runtimeSelection{RuntimeID: "codex"},
			localWorkflowTools: true, workflowCreationSessionID: sessionID,
			toolCapabilities: []ToolCapability{{ID: "workflow.local", Bindings: []capability.Binding{
				agentPreparationBinding(t, "workflow.source.save"), agentPreparationBinding(t, "workflow.runs.start"),
			}}},
		}
		var skills []agentapi.Skill
		var instructions, contextInstructions []agentapi.Instruction
		var connectors []agentapi.ConnectorDeclaration
		grant, err := manager.prepareAgent(t.Context(), t.Context(), models.RuntimeEnvironment{}, agentapi.Workspace{Path: t.TempDir()}, &skills, &instructions, &connectors, &contextInstructions, journal.chat, journal.chat.ActiveExecutionID, request, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(grant.finish)
		hasCreationGuide := slices.ContainsFunc(instructions, func(value agentapi.Instruction) bool { return value.Name == "WORKFLOW_AUTHORING.md" })
		if hasCreationGuide != (sessionID != "") || len(connectors) != 1 || connectors[0].RuntimeKey != "bot_capabilities" {
			t.Fatalf("incorrect prompt or workflow transport for %s", chatID)
		}
		connection, err := capability.ReadConnection(connectors[0].Arguments[3])
		if err != nil {
			t.Fatal(err)
		}
		return connection
	}
	partner := prepare("partner-chat", "")
	creation := prepare("creation-chat", "creation-session")
	if partner.Token != creation.Token || partner.GrantID == creation.GrantID {
		t.Fatal("partner and creation chats did not share one credential with separate connections")
	}
}

func TestResourceManagementInstructionsFollowTrustedChatGrants(t *testing.T) {
	manager := agentPreparationFixture(t)
	chat := manager.journal.(*agentTestJournal).chat
	chat.WakerID = "partner"
	manager.resourceManagementBindings = func(given models.Chat, executionID string, approve func(context.Context, string) error) []capability.Binding {
		if given.WakerID != "partner" || executionID != "turn" || approve == nil {
			t.Fatal("resource authorization lost the chat or approval context")
		}
		return []capability.Binding{agentPreparationBinding(t, "skills.packages.revise"), agentPreparationBinding(t, "mcp.connectors.update")}
	}
	for _, tc := range []struct {
		name, actor string
		automatic   string
		hidden      bool
		domains     *[]string
		wantSkill   bool
		wantMCP     bool
	}{
		{name: "interactive partner", actor: "user", wantSkill: true, wantMCP: true},
		{name: "interactive scoped", actor: "user", domains: &[]string{"skills"}, wantSkill: true},
		{name: "workflow", actor: "workflow"},
		{name: "automatic", actor: "user", automatic: "scheduled-run"},
		{name: "hidden conversation", actor: "user", hidden: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := chat
			current.HiddenFromWakerHistory = tc.hidden
			request := pendingMessage{entryPoint: WorkbenchEntryPoint, message: userMessage{Actor: tc.actor}, automaticRunID: tc.automatic,
				runtime: runtimeSelection{RuntimeID: "unsupported"}, allowedAgentDomains: tc.domains}
			request.entryPoint = trustedWorkbenchEntryPoint(request, messageChat{chat: current})
			var skills []agentapi.Skill
			var instructions, contextInstructions []agentapi.Instruction
			var connectors []agentapi.ConnectorDeclaration
			grant, err := manager.prepareAgent(t.Context(), t.Context(), models.RuntimeEnvironment{}, agentapi.Workspace{Path: t.TempDir()}, &skills, &instructions, &connectors, &contextInstructions, current, "turn", request, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if grant.finish != nil {
				t.Cleanup(grant.finish)
			}
			prompt := agentapi.DeveloperInstructions(instructions)
			if strings.Contains(prompt, "skills.packages operations") != tc.wantSkill || strings.Contains(prompt, "mcp.connectors operations") != tc.wantMCP {
				t.Fatalf("management guidance does not match the grant: %s", prompt)
			}
			if len(contextInstructions) != 0 {
				t.Fatalf("stable management guidance was repeated in dynamic turn context: %#v", contextInstructions)
			}
		})
	}
}

// preparedContextPath 读取宿主实际用于 CLI 或 MCP 的连接路径。
func preparedContextPath(t *testing.T, request recordedRun) string {
	t.Helper()
	if path := request.thread().Connection.Launch.BotCLIContextFile; path != "" {
		return path
	}
	for _, connector := range request.Connectors {
		if connector.RuntimeKey == "bot_capabilities" && len(connector.Arguments) == 4 && connector.Arguments[2] == "--context" {
			return connector.Arguments[3]
		}
	}
	t.Fatal("missing host capability connection")
	return ""
}

func publishedTestSystemSkill(t *testing.T, root, binary string) string {
	t.Helper()
	directory, err := builtinskills.SystemDirectory(root, binary)
	if err != nil {
		t.Fatal(err)
	}
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestNativeWorkbenchFallsBackFromOptionalGrantStorageFailure(t *testing.T) {
	ctx := t.Context()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, _, store, closeRuntime := fixture.open()
	defer closeRuntime()
	manager, tools := core.(*service), agentPreparationFixture(t)
	manager.agent, manager.agentEndpoint, manager.systemSkillDirectory = tools.agent, tools.agentEndpoint, tools.systemSkillDirectory
	root := filepath.Join(filepath.Dir(filepath.Dir(manager.systemSkillDirectory)), constants.AgentCLIContextDirectory)
	if err := os.WriteFile(root, []byte("not a connection directory"), 0600); err != nil {
		t.Fatal(err)
	}
	binding := agentPreparationBinding(t, "workflow.runs.start")
	manager.bindLocalWorkflowTurn = func(_ context.Context, _ models.Chat, _ string, selected *workinput.WorkflowReference, _ string) (WorkflowTurnBinding, error) {
		return WorkflowTurnBinding{Reference: selected,
			Capability: &ToolCapability{ID: "workflow.local", Bindings: []capability.Binding{binding}}}, nil
	}
	options := WorkOptions{RuntimeID: "codex", Model: "fixture-model", PermissionLevel: constants.PermissionLevelAuto, EntryPoint: WorkbenchEntryPoint}
	intent := WorkIntent{ClientRequestID: "optional-grant-storage", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}},
		Input: WorkInput{Actor: "user", Content: "  plain fallback  "}, Options: options}
	work, err := core.Start(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, work, constants.ExecutionStatusCompleted)
	trace, err := os.ReadFile(fixture.trace)
	if err != nil || !strings.Contains(string(trace), `"text":"  plain fallback  "`) {
		t.Fatalf("fallback did not deliver original user text: %v", err)
	}
	if strings.Contains(string(trace), "Workflow tools") || strings.Contains(string(trace), "NeuxBot capabilities") || strings.Contains(string(trace), "bot_capabilities") {
		t.Fatal("fallback still announced the unavailable default tool grant")
	}
	// 同一工作台明确选择工作流时必须交付实际工具。
	intent.ClientRequestID = "selected-grant-storage"
	intent.Input.Workflow = workinput.SelectWorkflow("selected-workflow")
	if _, err := core.Start(ctx, intent); !errors.Is(err, status_error.AgentContextStorageError) {
		t.Fatalf("selected workflow without its grant returned %v", err)
	}
}

func TestMissingSystemBundleRejectsRequiredHostToolGrants(t *testing.T) {
	manager := agentPreparationFixture(t)
	chat := manager.journal.(*agentTestJournal).chat
	manager.systemSkillDirectory = ""
	intent := pendingMessage{entryPoint: WorkbenchEntryPoint, message: userMessage{Actor: "user", Text: "run this work"}}
	for _, tc := range []struct {
		// name 标明必须交付工具的执行来源。
		name string
		// change 设置要求签发宿主工具的业务事实。
		change func(*pendingMessage)
	}{
		{name: "workflow authoring", change: func(input *pendingMessage) { input.workflowCreationSessionID = "authoring" }},
		{name: "selected workflow", change: func(input *pendingMessage) { input.message.Workflow = workinput.SelectWorkflow("workflow") }},
		{name: "explicit host operations", change: func(input *pendingMessage) {
			input.toolCapabilities = []ToolCapability{{ID: "audit", Bindings: []capability.Binding{agentPreparationBinding(t, "im.targets.list")}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := intent
			tc.change(&request)
			var skills []agentapi.Skill
			var instructions, contextInstructions []agentapi.Instruction
			var connectors []agentapi.ConnectorDeclaration
			grant, err := manager.prepareAgent(t.Context(), t.Context(), models.RuntimeEnvironment{}, agentapi.Workspace{Path: t.TempDir()}, &skills, &instructions, &connectors, &contextInstructions, chat, "turn", request, nil, nil)
			if grant.finish != nil {
				t.Cleanup(grant.finish)
			}
			if !errors.Is(err, status_error.AgentContextStorageError) {
				t.Fatalf("missing system bundle returned %v", err)
			}
		})
	}
}
