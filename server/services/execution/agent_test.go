package execution

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
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

func TestNativeSessionKeepsContextPathButRotatesAndRevokesCredentials(t *testing.T) {
	registry := agentcapability.New(nil)
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
	manager := &service{agent: registry, agentEndpoint: endpoint + constants.AgentCLIPath, agentBinary: binary, agentSessions: &agentSessionFiles{slots: map[string]agentSessionSlot{}}, journal: journal, storage: layout, skillDataDirectory: programDirectory, elicitations: newElicitationCoordinator(journal)}
	request := recordedRun{SessionPlan: runtimecontract.SessionPlan{Target: runtimecontract.SessionTarget{RuntimeID: "codex", AccountID: "account"}}, Session: runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeNew}}
	activate, revoke, settle, err := manager.prepareAgent(&request.SessionPlan, &request.TurnInput, request.Session, journal.chat, "first", nil, &[]string{}, nil)
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
	first, err := agentcapability.ReadConnection(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	settle(runtimecontract.Result{Session: runtimecontract.SessionResult{OutputID: "native", State: runtimecontract.SessionStateCreated}})
	revoke()
	journal.chat.ActiveExecutionID = "second"
	secondRequest := recordedRun{SessionPlan: runtimecontract.SessionPlan{Target: request.Target}, Session: runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeResume, ID: "native"}}
	activate, revokeSecond, _, err := manager.prepareAgent(&secondRequest.SessionPlan, &secondRequest.TurnInput, secondRequest.Session, journal.chat, "second", nil, &[]string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer revokeSecond()
	activate()
	if secondRequest.LaunchEnvironment[constants.AgentCLIContextEnvironment] != firstPath {
		t.Fatal("continued native process lost its original environment file path")
	}
	second, err := agentcapability.ReadConnection(firstPath)
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
	newRequest := recordedRun{SessionPlan: runtimecontract.SessionPlan{Target: request.Target}, Session: runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeNew}}
	_, revokeNew, _, err := manager.prepareAgent(&newRequest.SessionPlan, &newRequest.TurnInput, newRequest.Session, journal.chat, "second", nil, &[]string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer revokeNew()
	if newRequest.LaunchEnvironment[constants.AgentCLIContextEnvironment] == firstPath {
		t.Fatal("new native session reused an old process credential file")
	}
}

func TestKnowledgeCLIPreparationKeepsOnlyTheFrozenGrant(t *testing.T) {
	registry := agentcapability.New(nil)
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
	manager := &service{agent: registry, agentEndpoint: endpoint.URL + constants.AgentCLIPath, agentBinary: binary, agentSessions: &agentSessionFiles{slots: map[string]agentSessionSlot{}}, journal: journal, skillDataDirectory: t.TempDir(), elicitations: newElicitationCoordinator(journal)}
	manager.agentBindings = func(models.Chat, string, []string, string) []agentcapability.Binding {
		t.Fatal("scoped command discovered ordinary resources")
		return nil
	}
	bindings := []agentcapability.Binding{}
	for _, op := range []string{"knowledge.jobs.start"} {
		b := agentcapability.Binding{Operation: op, Call: func(context.Context, agentcapability.Request) (agentcapability.Result, error) {
			return agentcapability.Result{}, nil
		}}
		if op == "knowledge.jobs.start" {
			b.Receipt = func(context.Context, string) (agentcapability.Result, error) { return agentcapability.Result{}, nil }
		}
		bindings = append(bindings, b)
	}
	request := recordedRun{SessionPlan: runtimecontract.SessionPlan{Target: runtimecontract.SessionTarget{RuntimeID: "codex"}, Policy: runtimecontract.RunPolicy{Execution: runtimecontract.ExecutionPolicy{WorkspaceReadOnly: true}}}, Session: runtimecontract.SessionDirective{Mode: runtimecontract.SessionModeNew}}
	activate, revoke, _, err := manager.prepareAgent(&request.SessionPlan, &request.TurnInput, request.Session, journal.chat, "execution", []ToolCapability{{ID: "knowledge.organize", Bindings: bindings}}, &[]string{}, nil)
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
	var response agentcapability.Response
	if err := json.Unmarshal(output, &response); err != nil || !response.OK {
		t.Fatalf("skill-local capabilities failed: %s (%v)", output, err)
	}
	if _, err := os.ReadFile(filepath.Join(request.Skills[0].Path, "references", "wiki-guide.md")); err != nil {
		t.Fatalf("skill-local knowledge reference unavailable: %v", err)
	}
}
