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
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

func TestScopedWorkflowCreationRejectsBeforeNativeDispatch(t *testing.T) {
	runtime := &fixtureRuntime{}
	s, _ := openCoreWorkflowPorts(t, t.TempDir(), runtime, fixtureEnvironment{})
	ctx := t.Context()
	definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "scoped-creation", CreationWakerID: "fixture-waker"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := sendCreationMessage(ctx, workflowAPIDependencies(s), SendCreationMessageCommand{
		DefinitionID: definition.ID, RequestID: "first", Content: "保存工作流",
		Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: "system", Model: "fixture"},
	})
	if !errors.Is(err, status_error.RuntimePolicyUnsupported) || session.ID == "" || session.ChatID != "" || session.AcceptedExecutionID != "" {
		t.Fatalf("scoped creation escaped admission: session=%+v err=%v", session, err)
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
	command.SessionID, command.RequestID, command.Content = failed.ID, "retry", "重试本地流程设计"
	retried, err := sendCreationMessage(ctx, workflowAPIDependencies(s), command)
	if !errors.Is(err, status_error.RuntimePolicyUnsupported) || retried.ID != failed.ID || retried.ChatID != "" || retried.AcceptedExecutionID != "" {
		t.Fatalf("retry did not reach the runtime scope check with the same anchor: %+v %v", retried, err)
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
