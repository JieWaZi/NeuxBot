package workflows

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// creationScopeRuntime 只替代外部模型协议，保留真实创作接纳和工作区准备。
type creationScopeRuntime struct {
	// requests 记录实际派发请求，检验续聊不会更换获准目录。
	requests chan fixtureInvocation
	// duringRun 在真实执行仍活跃时探测创作 MCP，默认不附加操作。
	duringRun func(context.Context, fixtureInvocation)
}

func TestCreationCLIQueriesGuidanceAndSavesThroughAuthorizedCommands(t *testing.T) {
	source := workflowTestSource(t)
	outcomes := make(chan error, 1)
	runtime := &creationScopeRuntime{requests: make(chan fixtureInvocation, 1)}
	runtime.duringRun = func(ctx context.Context, request fixtureInvocation) {
		for _, command := range []struct {
			// args 是真实 CLI 的结构化参数列表。
			args []string
			// input 是仅包含业务参数的 JSON。
			input map[string]any
		}{
			{[]string{"capabilities"}, nil},
			{[]string{"workflow", "sdk", "get"}, map[string]any{"topic": "worker"}},
			{[]string{"workflow", "sdk", "get"}, map[string]any{"topic": "f0", "kind": "example"}},
			{[]string{"workflow", "resources", "list"}, map[string]any{"kind": "wakers"}},
			{[]string{"workflow", "resources", "list"}, map[string]any{"kind": "imTargets"}},
			{[]string{"workflow", "source", "get"}, nil},
			{[]string{"workflow", "source", "validate"}, map[string]any{"source": source}},
			{[]string{"workflow", "source", "save", "--request-id", "query-save"}, map[string]any{"source": source, "sdkVersion": constants.WorkflowSDKVersion, "expectedRevisionId": ""}},
		} {
			if _, err := fixtureCLI(ctx, request, command.args, command.input); err != nil {
				outcomes <- err
				return
			}
		}
		for _, args := range [][]string{{"workflow", "results", "schema"}, {"workflow", "runs", "start", "--request-id", "forbidden", "--definition-id", "other", "--revision-id", "other", "--input", "-"}} {
			if _, err := fixtureCLI(ctx, request, args, nil); err == nil {
				outcomes <- status_error.WorkflowStateInvalid
				return
			}
		}

		outcomes <- nil
	}
	s, _ := openCoreWorkflowPorts(t, t.TempDir(), runtime, fixtureEnvironment{})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "query-create", CreationWakerID: "fixture-waker"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sendCreationMessage(ctx, workflowAPIDependencies(s), SendCreationMessageCommand{DefinitionID: definition.ID, RequestID: "query-message", Content: "查询能力并保存流程", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: "system", Model: "fixture", PermissionLevel: constants.PermissionLevelDefault}}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-outcomes:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	stored, err := s.Store.GetDefinition(ctx, definition.ID)
	if err != nil || stored.CurrentRevisionID == "" {
		t.Fatalf("single-tool save did not publish a revision: %+v %v", stored, err)
	}
	revision, err := s.Store.GetRevision(ctx, definition.ID, stored.CurrentRevisionID)
	if err != nil || revision.Source != source {
		t.Fatalf("saved source changed: %v", err)
	}
}

func TestCreationScopeFailedAdmissionCanRetryNewRequest(t *testing.T) {
	directory := t.TempDir()
	runtime := &creationScopeRuntime{requests: make(chan fixtureInvocation, 4)}
	s, _ := openCoreWorkflowPorts(t, directory, runtime, fixtureEnvironment{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "create", Title: "创作重试验收", CreationWakerID: "fixture-waker"})
	if err != nil {
		t.Fatal(err)
	}
	command := SendCreationMessageCommand{DefinitionID: definition.ID, RequestID: "first", Content: "设计本地流程", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: "system", Model: "fixture", PermissionLevel: constants.PermissionLevelDefault}}
	s.DataDirectory = ""
	failed, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
	if workflowstate.ErrorKey(err) != status_error.WorkflowResourceInvalid.Key() || failed.ID == "" || failed.ChatID != "" {
		t.Fatalf("expected a durable input anchor without a committed Chat: %v", err)
	}
	s.DataDirectory = directory
	command.SessionID, command.RequestID, command.Content = failed.ID, "retry", "重试本地流程设计"
	accepted, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
	if err != nil || accepted.ID != failed.ID || accepted.ChatID == "" || accepted.AcceptedExecutionID == "" {
		t.Fatalf("failed initial creation could not retry after configuration recovery: %v", err)
	}
	encoded, err := json.Marshal(accepted)
	if err != nil {
		t.Fatal(err)
	}
	var reply map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &reply); err != nil {
		t.Fatal(err)
	}
	var snapshot models.Chat
	if err := json.Unmarshal(reply["chat"], &snapshot); err != nil || snapshot.ID != accepted.ChatID || snapshot.LatestSequence <= 0 {
		t.Fatalf("accepted workflow reply discarded authoritative chat snapshot: %s %v", encoded, err)
	}
	select {
	case <-runtime.requests:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-runtime.requests:
		t.Fatal("recovery dispatched more than one model execution")
	default:
	}
}

func TestWorkflowReusesAllyAndStartsAnOrdinaryChat(t *testing.T) {
	directory := t.TempDir()
	runtime := &creationScopeRuntime{requests: make(chan fixtureInvocation, 2)}
	s, _ := openCoreWorkflowPorts(t, directory, runtime, fixtureEnvironment{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "no-form"})
	if err != nil || definition.CreationWakerID != builtinrolepreset.PersonalAssistantWakerID || definition.Title != "" {
		t.Fatalf("built-in creation: %+v %v", definition, err)
	}
	waker, err := s.Wakers.Get(ctx, definition.CreationWakerID)
	if err != nil || waker.Name != "lumi" || waker.RoleName != "私人助理" {
		t.Fatalf("workflow did not reuse lumi: %+v %v", waker, err)
	}
	listed, err := s.Wakers.List(ctx, "", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	visible := false
	for _, item := range listed {
		if item.Name == "Workflow" {
			t.Fatal("workflow created a separate partner")
		}
		if item.ID == definition.CreationWakerID {
			visible = true
		}
	}
	if !visible {
		t.Fatal("existing lumi partner missing from the partner page")
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	var public map[string]any
	if json.Unmarshal(encoded, &public) != nil || public["creationWakerId"] != nil {
		t.Fatal("creation exposed an creation partner selector identity")
	}
	session, err := sendCreationMessage(ctx, workflowAPIDependencies(s), SendCreationMessageCommand{DefinitionID: definition.ID, RequestID: "first-message", Content: "从目标生成标题和流程", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: "system", Model: "fixture", PermissionLevel: constants.PermissionLevelDefault}})
	if err != nil || session.ChatID == "" || session.AcceptedExecutionID == "" {
		t.Fatalf("internal conversation admission: %+v %v", session, err)
	}
	select {
	case request := <-runtime.requests:
		if request.Workspace.Path == "" {
			t.Fatal("no managed workspace")
		}
		if request.Resources.Target.Environment[constants.AgentCLIContextEnvironment] == "" || len(request.Connectors) != 0 {
			t.Fatal("workflow creation missing CLI context or retained product MCP")
		}
		if len(request.Skills) != 1 || request.Skills[0].Name != constants.AgentCLISkillName {
			t.Fatalf("workflow creation skill not delivered: %+v", request.Skills)
		}
		for _, relative := range []string{"SKILL.md", "references/api-retrieval.md"} {
			if _, err := os.ReadFile(filepath.Join(request.Skills[0].Path, relative)); err != nil {
				t.Fatalf("runtime cannot read complete authoring skill: %v", err)
			}
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func (*creationScopeRuntime) ForkSession(context.Context, runtimecontract.ForkSessionRequest) (runtimecontract.ForkSessionResult, error) {
	return runtimecontract.ForkSessionResult{}, status_error.WorkflowCapabilityUnsupported
}
func (*creationScopeRuntime) Runtimes() []runtimeconfig.RuntimeSummary {
	return []runtimeconfig.RuntimeSummary{{ID: "codex", BaseRuntimeID: "codex", InstructionFileName: "AGENTS.md", SkillDirectoryName: ".agents"}}
}
func (r *creationScopeRuntime) Run(ctx context.Context, request fixtureInvocation, sink runtimecontract.EventSink) runtimecontract.Result {
	id := util.New()
	data, _ := json.Marshal(runtimecontract.InvocationStartedData{InvocationID: id})
	if err := sink.Accept(ctx, runtimecontract.Event{Type: runtimecontract.EventTypeInvocationStarted, Data: data}); err != nil {
		return runtimecontract.Result{ExitCode: 1}
	}
	r.requests <- request
	if r.duringRun != nil {
		r.duringRun(ctx, request)
	}
	return runtimecontract.Result{ProtocolHealthy: true, Facts: runtimecontract.InvocationFacts{InvocationID: id, RuntimeID: "codex", BaseRuntimeID: "codex"}}
}

func TestCreationScopeManagedAndExplicitDirectory(t *testing.T) {
	for _, managed := range []bool{true, false} {
		t.Run(map[bool]string{true: "managed", false: "explicit"}[managed], func(t *testing.T) {
			directory := t.TempDir()
			runtime := &creationScopeRuntime{requests: make(chan fixtureInvocation, 4)}
			finishRuntime := make(chan struct{})
			runtime.duringRun = func(ctx context.Context, _ fixtureInvocation) {
				select {
				case <-finishRuntime:
				case <-ctx.Done():
				}
			}
			s, _ := openCoreWorkflowPorts(t, directory, runtime, fixtureEnvironment{})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			scope := models.WorkflowScopeBinding{}
			if !managed {
				scope.WorkingDirectory = t.TempDir()
			}
			definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "create", Title: "创作工作区验收", CreationWakerID: "fixture-waker", ScopeBinding: scope})
			if err != nil {
				t.Fatal(err)
			}
			command := SendCreationMessageCommand{DefinitionID: definition.ID, RequestID: "first", Content: "设计本地验收流程", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: "system", Model: "fixture", PermissionLevel: constants.PermissionLevelDefault}}
			session, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
			if err != nil {
				t.Fatalf("creation admission rejected the creation form's scope: %v", err)
			}
			select {
			case <-runtime.requests:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			expected := scope.WorkingDirectory
			if managed {
				expected = filepath.Join(directory, "workflow-workspaces", definition.ID)
			}
			expected, err = filepath.EvalSymlinks(expected)
			if err != nil {
				t.Fatal(err)
			}
			// CompletedAt 先于执行占位释放写入；先订阅，再结束 Runtime，只以公开终态允许续发。
			subscription, err := s.Chats.SubscribeEvents(ctx, session.ChatID, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer subscription.Close()
			close(finishRuntime)
		waitForTerminal:
			for {
				select {
				case event, open := <-subscription.Events:
					if !open {
						t.Fatal("creation subscription closed before execution settled")
					}
					if event.ExecutionID != session.AcceptedExecutionID {
						continue
					}
					switch event.Type {
					case constants.ChatEventTypeExecutionCompleted:
						break waitForTerminal
					case constants.ChatEventTypeExecutionFailed, constants.ChatEventTypeExecutionCanceled:
						t.Fatalf("creation execution did not complete: %+v", event)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			summary, err := s.Chats.GetChatSummary(ctx, session.ChatID)
			if err != nil || len(summary.Chat.AdditionalWorkingDirectories) != 1 || summary.Chat.AdditionalWorkingDirectories[0] != expected {
				t.Fatalf("creation scope mismatch: %+v %v", summary.Chat.AdditionalWorkingDirectories, err)
			}
			replayed, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
			if err != nil || replayed.ID != session.ID || replayed.ChatID != session.ChatID || replayed.AcceptedExecutionID != session.AcceptedExecutionID {
				t.Fatalf("creation replay changed its identity: %v", err)
			}
			if managed {
				// 模拟 Core 已提交、创作确认写入丢失的进程中断窗口。
				unconfirmed := session
				unconfirmed.ChatID, unconfirmed.AcceptedExecutionID = "", ""
				if err := s.CreationSessions.UpdateCreationSession(ctx, unconfirmed.WorkflowCreationSession); err != nil {
					t.Fatal(err)
				}
			}
			command.SessionID, command.RequestID, command.Content = session.ID, "second", "增加一个审批步骤"
			continued, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
			if err != nil || continued.ChatID != session.ChatID {
				t.Fatalf("creation continuation changed its scope or Chat: %v", err)
			}
			select {
			case <-runtime.requests:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			summary, err = s.Chats.GetChatSummary(ctx, continued.ChatID)
			if err != nil || len(summary.Chat.AdditionalWorkingDirectories) != 1 || summary.Chat.AdditionalWorkingDirectories[0] != expected {
				t.Fatalf("continuation did not retain the approved directory: %v", err)
			}
		})
	}
}

func TestCreationSessionsUseChatWorkspacesAndShareDefinitionDirectory(t *testing.T) {
	directory := t.TempDir()
	runtime := &creationScopeRuntime{requests: make(chan fixtureInvocation, 3)}
	s, _ := openCoreWorkflowPorts(t, directory, runtime, fixtureEnvironment{})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var requests []fixtureInvocation
	var sessions []models.WorkflowCreationSession
	for index, count := range []int{2, 1} {
		definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: []string{"workflow-a", "workflow-b"}[index], CreationWakerID: "fixture-waker"})
		if err != nil {
			t.Fatal(err)
		}
		for turn := 0; turn < count; turn++ {
			session, err := sendCreationMessage(ctx, workflowAPIDependencies(s), SendCreationMessageCommand{DefinitionID: definition.ID, RequestID: util.New(), Content: "设计共享目录中的工作流", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: "system", Model: "fixture", PermissionLevel: constants.PermissionLevelDefault}})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case request := <-runtime.requests:
				requests = append(requests, request)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			for {
				completed, err := s.Chats.ReadExecution(ctx, session.ChatID, session.AcceptedExecutionID)
				if err != nil {
					t.Fatal(err)
				}
				if completed.CompletedAt != nil {
					break
				}
				select {
				case <-time.After(5 * time.Millisecond):
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			sessions = append(sessions, session.WorkflowCreationSession)
		}
	}
	for index, request := range requests {
		if len(request.Workspace.AdditionalWritableDirectories) != 1 {
			t.Fatalf("creation session %d lost its business directory", index)
		}
		assertWorkflowChatWorkspace(t, request.Workspace, request.Workspace.AdditionalWritableDirectories[0])
		for _, previous := range requests[:index] {
			if request.Workspace.Path == previous.Workspace.Path {
				t.Fatal("creation sessions must use independent managed chat workspaces")
			}
		}
	}
	firstDirectory := requests[0].Workspace.AdditionalWritableDirectories[0]
	if firstDirectory != requests[1].Workspace.AdditionalWritableDirectories[0] || firstDirectory == requests[2].Workspace.AdditionalWritableDirectories[0] {
		t.Fatal("business directories must be shared within one workflow and isolated across workflows")
	}
	sharedFile := filepath.Join(firstDirectory, "shared-result.txt")
	if err := os.WriteFile(sharedFile, []byte("keep workflow files"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Chats.DeleteChatsAtomically(ctx, []string{sessions[0].ChatID}, func(ctx context.Context) error {
		return s.CreationSessions.DeleteCreationSessionsByChatID(ctx, sessions[0].ChatID)
	}); err != nil {
		t.Fatal(err)
	}
	remaining, err := s.CreationSessions.ListCreationSessions(ctx, sessions[0].DefinitionID)
	if err != nil || len(remaining) != 1 || remaining[0].ID != sessions[1].ID {
		t.Fatalf("creation relationship deletion was not scoped to its Chat: %+v %v", remaining, err)
	}
	if content, err := os.ReadFile(sharedFile); err != nil || string(content) != "keep workflow files" {
		t.Fatalf("deleting an creation Chat removed workflow files: %v", err)
	}
	project := requests[1].Workspace.ProjectDirectories[0]
	if content, err := os.ReadFile(filepath.Join(requests[1].Workspace.Path, project.Path, "shared-result.txt")); err != nil || string(content) != "keep workflow files" {
		t.Fatalf("remaining creation Chat cannot access shared business files: %v", err)
	}
	if _, err := s.Chats.GetChatSummary(ctx, sessions[1].ChatID); err != nil {
		t.Fatalf("deleting one creation damaged another Chat: %v", err)
	}
}

// creationInputBoundary 只截获中立执行接纳，配置与 Workflow 持久层仍使用真实实现。
type creationInputBoundary struct {
	// Service 提供真实配置校验和其他执行端口。
	execution.Service
	// intent 保存入口向 Core 交付的完整输入与会话选项。
	intent execution.WorkIntent
}

func (b *creationInputBoundary) Start(_ context.Context, intent execution.WorkIntent) (execution.AcceptedExecution, error) {
	b.intent = intent
	return execution.AcceptedExecution{Chat: models.Chat{ID: "input-chat"}, Execution: models.Execution{ID: intent.ExecutionID}}, nil
}

func TestCreationForwardsCompleteChatInputAndSessionTransition(t *testing.T) {
	s, _ := openCoreWorkflowPorts(t, t.TempDir(), &creationScopeRuntime{requests: make(chan fixtureInvocation, 1)}, fixtureEnvironment{})
	ctx := context.Background()
	definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "complete-input", CreationWakerID: "fixture-waker"})
	if err != nil {
		t.Fatal(err)
	}
	boundary := &creationInputBoundary{Service: s.Executions}
	s.Executions = boundary
	command := SendCreationMessageCommand{
		DefinitionID: definition.ID, RequestID: strings.Repeat("r", 128), Content: "引用材料修改工作流",
		Attachments:    []workinput.Attachment{{SourcePath: "/tmp/material.txt"}},
		CodeReferences: []workinput.CodeReference{{Path: "/tmp/source.go", StartLine: 1, EndLine: 2, Content: "package main"}},
		SkillIDs:       []string{"skill-one"}, ConnectorIDs: []string{"connector-one"},
		StartNewSession: true, HistoryTurns: 7, HistoryMode: constants.HistoryModeSummary, SummaryModel: "summary-model", SummaryReasoning: "high",
		Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: "system", Model: "fixture", PermissionLevel: constants.PermissionLevelDefault},
	}
	if _, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command); err != nil {
		t.Fatal(err)
	}
	input := boundary.intent.Input
	if input.SubmissionID != command.RequestID || input.Content != command.Content || !reflect.DeepEqual(input.Attachments, command.Attachments) || !reflect.DeepEqual(input.CodeReferences, command.CodeReferences) || !reflect.DeepEqual(input.SkillIDs, command.SkillIDs) || !reflect.DeepEqual(input.ConnectorIDs, command.ConnectorIDs) {
		t.Fatalf("creation admission lost common chat input: %+v", input)
	}
	options := boundary.intent.Options
	if options.SessionPolicy != constants.RuntimeSessionPolicyNew || options.HistoryTurns != 7 || options.HistoryMode != constants.HistoryModeSummary || options.SummaryModel != command.SummaryModel || options.SummaryReasoning != command.SummaryReasoning {
		t.Fatalf("creation admission lost session transition: %+v", options)
	}
	if len(options.ToolCapabilities) != 1 || options.ToolCapabilities[0].ID != "workflow.authoring" {
		t.Fatal("common chat admission lost workflow-scoped tool authority")
	}
}
