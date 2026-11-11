package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"

	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

func TestWorkflowCreationForwardsHostCapabilityScope(t *testing.T) {
	runtime := &fixtureRuntime{}
	s, _ := openCoreWorkflowPorts(t, t.TempDir(), runtime, fixtureEnvironment{})
	ctx := t.Context()
	definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "scoped-creation", CreationWakerID: "fixture-waker"})
	if err != nil {
		t.Fatal(err)
	}
	boundary := &creationInputBoundary{Service: s.Executions}
	s.Executions = boundary
	session, err := sendCreationMessage(ctx, workflowAPIDependencies(s), SendCreationMessageCommand{
		DefinitionID: definition.ID, RequestID: "first", Content: "保存工作流",
		Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: "system", Model: "fixture"},
	})
	if err != nil || session.ID == "" || session.ChatID == "" || session.AcceptedExecutionID != session.ActiveExecutionID {
		t.Fatalf("host scope prevented creation admission: session=%+v err=%v", session, err)
	}
	if boundary.intent.Options.AllowedAgentDomains == nil || !slices.Equal(*boundary.intent.Options.AllowedAgentDomains, []string{"memory", "interaction"}) || boundary.intent.Options.AllowedConnectorIDs != nil {
		t.Fatalf("host capability scope was changed into a native connector allowlist: %+v", boundary.intent.Options)
	}
	if len(boundary.intent.Options.ToolCapabilities) != 1 || boundary.intent.Options.ToolCapabilities[0].ID != "workflow.authoring" {
		t.Fatal("creation lost its explicitly granted workflow tools")
	}
}

func TestCreationScopeFailedAdmissionCanRetryNewRequest(t *testing.T) {
	directory := t.TempDir()
	runtime := &fixtureRuntime{}
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
	s.Executions = &creationInputBoundary{Service: s.Executions}
	command.SessionID, command.RequestID, command.Content = failed.ID, "retry", "重试本地流程设计"
	retried, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
	if err != nil || retried.ID != failed.ID || retried.ChatID == "" || retried.AcceptedExecutionID != retried.ActiveExecutionID {
		t.Fatalf("retry did not admit host-scoped creation from the same anchor: %+v %v", retried, err)
	}
}

func TestCreationReceiptFreezesDefaultsBeforeFailedAdmission(t *testing.T) {
	for _, reasoning := range []string{"low", ""} {
		t.Run("reasoning="+reasoning, func(t *testing.T) {
			environment := &mutableAdmissionEnvironment{initialReasoning: reasoning}
			directory := t.TempDir()
			s, _ := openCoreWorkflowPorts(t, directory, &fixtureRuntime{}, environment)
			ctx := t.Context()
			definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "create", CreationWakerID: "fixture-waker"})
			if err != nil {
				t.Fatal(err)
			}
			command := SendCreationMessageCommand{DefinitionID: definition.ID, RequestID: "same-input", Content: "创作冻结配置", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex"}}
			s.DataDirectory = ""
			failed, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
			if workflowstate.ErrorKey(err) != status_error.WorkflowResourceInvalid.Key() || failed.ID == "" {
				t.Fatalf("expected workspace admission refusal after receipt: %+v %v", failed, err)
			}
			if failed.Defaults.AccountID != "system" || failed.Defaults.Model != "fixture" || failed.Defaults.Reasoning != reasoning {
				t.Errorf("creation receipt did not save resolved defaults: %+v", failed.Defaults)
			}
			environment.mutex.Lock()
			environment.changed = true
			environment.mutex.Unlock()
			s.DataDirectory = directory
			s.Executions = &creationInputBoundary{Service: s.Executions, err: status_error.RuntimePolicyUnsupported}
			retried, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
			if !errors.Is(err, status_error.RuntimePolicyUnsupported) || retried.ID != failed.ID || retried.ActiveExecutionID != failed.ActiveExecutionID || !reflect.DeepEqual(retried.Defaults, failed.Defaults) {
				t.Fatalf("same receipt followed changed defaults or changed identity: first=%+v retry=%+v err=%v", failed, retried, err)
			}
			command.Content = "同键不同正文"
			if _, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command); !errors.Is(err, status_error.WorkflowRequestConflict) {
				t.Fatalf("changed content bypassed original request digest: %v", err)
			}
		})
	}
}

func TestCreationDispatchPreservesResolvedEmptyReasoning(t *testing.T) {
	environment := &mutableAdmissionEnvironment{}
	s, _ := openCoreWorkflowPorts(t, t.TempDir(), &fixtureRuntime{}, environment)
	ctx := t.Context()
	definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "create", CreationWakerID: "fixture-waker"})
	if err != nil {
		t.Fatal(err)
	}
	boundary := &creationInputBoundary{Service: s.Executions, err: status_error.RuntimePolicyUnsupported}
	s.Executions = boundary
	command := SendCreationMessageCommand{DefinitionID: definition.ID, RequestID: "native-default", Content: "保留原生默认", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex"}}
	first, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
	if !errors.Is(err, status_error.RuntimePolicyUnsupported) {
		t.Fatal(err)
	}
	environment.mutex.Lock()
	environment.changed = true
	environment.mutex.Unlock()
	if _, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command); !errors.Is(err, status_error.RuntimePolicyUnsupported) {
		t.Fatalf("frozen empty selection was replaced before dispatch: %v", err)
	}
	options := boundary.intent.Options
	if !options.ConfigurationResolved || options.AccountID != "system" || options.Model != "fixture" || options.Reasoning != "" || boundary.intent.ExecutionID != first.ActiveExecutionID {
		t.Fatalf("dispatch lost frozen native default or input identity: %+v", boundary.intent)
	}
}

// creationFastEnvironment 只替代用户可修改的 Runtime Fast 默认值。
type creationFastEnvironment struct {
	// coreTestEnvironment 提供其余真实 Core 准入需要的注册配置。
	coreTestEnvironment
	// fast 是当前用户保存的 Runtime 快速模式默认值。
	fast bool
}

func (e *creationFastEnvironment) Get(ctx context.Context, id string) (models.RuntimeEnvironment, error) {
	environment, err := e.coreTestEnvironment.Get(ctx, id)
	value := e.fast
	environment.DefaultFast = &value
	return environment, err
}

func TestCreationReceiptFreezesFastAcrossDefaultChanges(t *testing.T) {
	for _, fast := range []bool{false, true} {
		t.Run(map[bool]string{false: "false", true: "true"}[fast], func(t *testing.T) {
			directory := t.TempDir()
			environment := &creationFastEnvironment{coreTestEnvironment: fixtureEnvironment{}, fast: fast}
			s, _ := openCoreWorkflowPorts(t, directory, &fixtureRuntime{}, environment)
			ctx := t.Context()
			definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "create", CreationWakerID: "fixture-waker"})
			if err != nil {
				t.Fatal(err)
			}
			command := SendCreationMessageCommand{DefinitionID: definition.ID, RequestID: "fast-frozen", Content: "按接纳配置创作", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex"}}
			s.DataDirectory = ""
			first, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
			if workflowstate.ErrorKey(err) != status_error.WorkflowResourceInvalid.Key() || first.ID == "" {
				t.Fatalf("expected workspace preparation failure after receipt: %+v %v", first, err)
			}
			if first.Defaults.Fast == nil || *first.Defaults.Fast != fast {
				t.Errorf("receipt did not freeze Fast=%v: %+v", fast, first.Defaults)
			}
			environment.fast = !fast
			s.DataDirectory = directory
			boundary := &creationInputBoundary{Service: s.Executions}
			s.Executions = boundary
			retried, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
			if err != nil || retried.ID != first.ID || retried.ActiveExecutionID != first.ActiveExecutionID || retried.Defaults.Fast == nil || *retried.Defaults.Fast != fast || boundary.intent.Options.Fast == nil || *boundary.intent.Options.Fast != fast || !boundary.intent.Options.ConfigurationResolved {
				t.Fatalf("replayed receipt followed new Fast default: saved=%+v dispatched=%+v err=%v", retried.Defaults, boundary.intent.Options, err)
			}
		})
	}
}

func TestWorkerAdmissionRetainsFrozenFastAcrossDefaultChanges(t *testing.T) {
	for _, fast := range []bool{false, true} {
		t.Run(map[bool]string{false: "false", true: "true"}[fast], func(t *testing.T) {
			environment := &creationFastEnvironment{coreTestEnvironment: fixtureEnvironment{}, fast: fast}
			s, _ := openCoreWorkflowPorts(t, t.TempDir(), &fixtureRuntime{}, environment)
			adapter := workflow.CoreWorkerAdapter{Executions: s.Executions}
			admitted, err := adapter.ResolveAdmission(t.Context(), nil, models.WorkflowExecutionDefaults{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelDefault}, false)
			if err != nil || admitted.Fast == nil || *admitted.Fast != fast {
				t.Fatalf("worker admission did not save Fast=%v: %+v %v", fast, admitted, err)
			}
			environment.fast = !fast
			retried, err := adapter.ResolveAdmission(t.Context(), nil, admitted, true)
			if err != nil || retried.Fast == nil || *retried.Fast != fast {
				t.Fatalf("worker retry followed changed Fast default: %+v %v", retried, err)
			}
		})
	}
}

// creationChatSnapshot 只替代原生执行后的权威 Chat 查询，配置解析和创作回执仍使用真实实现。
type creationChatSnapshot struct {
	// Module 保留当前用例未替换的 Chat 端口。
	chatservice.Module
	// previous 是允许下一轮创作的已完成执行。
	previous models.Execution
	// snapshot 保存该 Chat 最近有效的执行选择。
	snapshot models.Chat
}

func (c *creationChatSnapshot) ReadExecution(_ context.Context, chatID, executionID string) (models.Execution, error) {
	if chatID != c.snapshot.ID || executionID != c.previous.ID {
		return models.Execution{}, status_error.ExecutionNotFound
	}
	return c.previous, nil
}

func (c *creationChatSnapshot) LoadExecutionChat(_ context.Context, chatID string) (models.Chat, error) {
	if chatID != c.snapshot.ID {
		return models.Chat{}, status_error.ChatNotFound
	}
	return c.snapshot, nil
}

func TestCreationContinuationKeepsLastSelectionAndAcceptsExplicitChanges(t *testing.T) {
	environment := &mutableAdmissionEnvironment{initialReasoning: "low"}
	fastEnvironment := &creationFastEnvironment{coreTestEnvironment: environment, fast: true}
	s, _ := openCoreWorkflowPorts(t, t.TempDir(), &fixtureRuntime{}, fastEnvironment)
	ctx := t.Context()
	definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "create", CreationWakerID: "fixture-waker"})
	if err != nil {
		t.Fatal(err)
	}
	boundary := &creationInputBoundary{Service: s.Executions}
	s.Executions = boundary
	command := SendCreationMessageCommand{DefinitionID: definition.ID, RequestID: "first", Content: "创建流程", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex"}}
	first, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
	if err != nil {
		t.Fatal(err)
	}
	completed := int64(1)
	lastFast := false
	chats := &creationChatSnapshot{Module: s.Chats,
		previous: models.Execution{ID: first.ActiveExecutionID, CompletedAt: &completed},
		snapshot: models.Chat{ID: first.ChatID, LastRuntimeID: "codex", LastAccountID: "system", LastModel: "fixture", LastReasoning: "low", LastFast: &lastFast},
	}
	s.Chats = chats
	environment.mutex.Lock()
	environment.changed = true
	environment.mutex.Unlock()
	command.SessionID, command.RequestID, command.Content = first.ID, "next", "继续完善"
	next, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
	if err != nil || next.Defaults.AccountID != "system" || next.Defaults.Model != "fixture" || next.Defaults.Reasoning != "low" || next.Defaults.Fast == nil || *next.Defaults.Fast {
		t.Fatalf("continuation inherited changed runtime defaults: %+v %v", next.Defaults, err)
	}
	chats.previous.ID = next.ActiveExecutionID
	command.RequestID, command.Content = "explicit", "换模型继续"
	command.Defaults.AccountID, command.Defaults.Model, command.Defaults.Reasoning = "system", "override", "high"
	explicitFast := true
	command.Defaults.Fast = &explicitFast
	changed, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
	if err != nil || changed.Defaults.Model != "override" || changed.Defaults.Reasoning != "high" || changed.Defaults.Fast == nil || !*changed.Defaults.Fast || !boundary.intent.Options.ConfigurationResolved {
		t.Fatalf("explicit selection was replaced by continuation defaults: %+v %v", changed.Defaults, err)
	}
}

func TestWorkflowConsumerValidationPreservesResolvedNativeDefaults(t *testing.T) {
	environment := &mutableAdmissionEnvironment{}
	s, _ := openCoreWorkflowPorts(t, t.TempDir(), &fixtureRuntime{}, environment)
	storage, _ := workflowTestService(t)
	config := storage.Config
	config.Executions, config.Chats = s.Executions, s.Chats
	consumer := workflowTestNew(config)
	consumer.State.AgentBindings = consumer.AgentBindings
	definition := savedTestWorkflow(t, consumer, workflowTestSource(t))
	environment.mutex.Lock()
	environment.changed = true
	environment.mutex.Unlock()
	intent := execution.WorkIntent{ClientRequestID: "resolved-consumer", Chat: execution.ChatTarget{Create: &execution.CreateChatSpec{WakerID: "fixture-waker"}}, Input: execution.WorkInput{Content: "使用流程"}, Options: execution.WorkOptions{ConfigurationResolved: true, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelDefault}}
	bound, err := consumer.BindConsumer(t.Context(), intent, []string{definition.ID})
	if err != nil || !bound.Options.ConfigurationResolved || bound.Options.AccountID != "" || bound.Options.Model != "" {
		t.Fatalf("consumer validation reinterpreted resolved native defaults: %+v %v", bound.Options, err)
	}
	intent.Options.ConfigurationResolved = false
	if _, err := consumer.BindConsumer(t.Context(), intent, []string{definition.ID}); workflowstate.ErrorKey(err) != status_error.AIAccountNotFound.Key() {
		t.Fatalf("unresolved consumer bypassed current account default validation: %v", err)
	}
}

func TestWorkflowCreationReusesExistingAlly(t *testing.T) {
	directory := t.TempDir()
	runtime := &fixtureRuntime{}
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

}

func TestCreationScopeManagedAndExplicitDirectory(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "managed", true: "explicit"}[explicit], func(t *testing.T) {
			directory := t.TempDir()
			s, _ := openCoreWorkflowPorts(t, directory, &fixtureRuntime{}, fixtureEnvironment{})
			ctx := t.Context()
			scope := models.WorkflowScopeBinding{}
			if explicit {
				var err error
				scope.WorkingDirectory, err = filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
			}
			definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "create", Title: "创作工作区", CreationWakerID: "fixture-waker", ScopeBinding: scope})
			if err != nil {
				t.Fatal(err)
			}
			boundary := &creationInputBoundary{Service: s.Executions}
			s.Executions = boundary
			if _, err := sendCreationMessage(ctx, workflowAPIDependencies(s), SendCreationMessageCommand{
				DefinitionID: definition.ID, RequestID: "first", Content: "设计本地流程",
				Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: "system", Model: "fixture"},
			}); err != nil {
				t.Fatal(err)
			}
			internalRoot, err := filepath.EvalSymlinks(filepath.Join(directory, "workflow-workspaces", definition.ID))
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"scripts", "state", "cache", "tmp", "runs"} {
				if info, err := os.Stat(filepath.Join(internalRoot, name)); err != nil || !info.IsDir() {
					t.Fatalf("missing internal %s directory: %v", name, err)
				}
			}
			expected := internalRoot
			if explicit {
				expected = scope.WorkingDirectory
			}
			target := boundary.intent.Chat.Create
			if target == nil || target.WorkingDirectory != expected || !slices.Equal(target.AdditionalWorkingDirectories, []string{internalRoot}) {
				t.Fatalf("creation lost its workspace request: %+v", target)
			}
			for _, instruction := range boundary.intent.Input.ContextInstructions {
				if strings.Contains(instruction.Content, internalRoot) && strings.Contains(instruction.Content, filepath.Join(internalRoot, "scripts")) {
					return
				}
			}
			t.Fatal("creation input omitted the prepared internal workspace")
		})
	}
}

// creationInputBoundary 只截获中立执行接纳，配置与 Workflow 持久层仍使用真实实现。
type creationInputBoundary struct {
	// Service 提供真实配置校验和其他执行端口。
	execution.Service
	// intent 保存入口向 Core 交付的完整输入与会话选项。
	intent execution.WorkIntent
	// err 模拟执行拒绝，保留可重试的输入锚点。
	err error
}

func (b *creationInputBoundary) Start(_ context.Context, intent execution.WorkIntent) (execution.AcceptedExecution, error) {
	b.intent = intent
	if b.err != nil {
		return execution.AcceptedExecution{}, b.err
	}
	return execution.AcceptedExecution{Chat: models.Chat{ID: "input-chat"}, Execution: models.Execution{ID: intent.ExecutionID}}, nil
}

func TestCreationForwardsCompleteChatInputAndSessionTransition(t *testing.T) {
	s, _ := openCoreWorkflowPorts(t, t.TempDir(), &fixtureRuntime{}, fixtureEnvironment{})
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

func TestCreationProjectKeepsBusinessIdentityAndAddsInternalWorkspace(t *testing.T) {
	directory := t.TempDir()
	s, _ := openCoreWorkflowPorts(t, directory, &fixtureRuntime{}, fixtureEnvironment{})
	ctx := context.Background()
	project := models.Project{ID: util.New(), WakerID: "fixture-waker", Name: "业务项目"}
	if err := s.Projects.Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "project-workspace", CreationWakerID: project.WakerID, ScopeBinding: models.WorkflowScopeBinding{ProjectID: project.ID}})
	if err != nil {
		t.Fatal(err)
	}
	boundary := &creationInputBoundary{Service: s.Executions}
	s.Executions = boundary
	if _, err := sendCreationMessage(ctx, workflowAPIDependencies(s), SendCreationMessageCommand{DefinitionID: definition.ID, RequestID: "project-message", Content: "创作项目流程", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: "system", Model: "fixture"}}); err != nil {
		t.Fatal(err)
	}
	internal := filepath.Join(directory, "workflow-workspaces", definition.ID)
	internal, err = filepath.EvalSymlinks(internal)
	if err != nil {
		t.Fatal(err)
	}
	create := boundary.intent.Chat.Create
	if create.ProjectID != project.ID || create.WorkingDirectory != "" || !slices.Equal(create.AdditionalWorkingDirectories, []string{internal}) {
		t.Fatalf("creation project scope was replaced or omitted its internal workspace: %+v", create)
	}
	if info, err := os.Stat(filepath.Join(internal, "scripts")); err != nil || !info.IsDir() {
		t.Fatalf("project creation did not prepare its internal scripts directory: %v", err)
	}
}
