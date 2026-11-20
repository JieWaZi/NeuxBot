package workflow

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

var chatChildFileMeta = strings.Replace(runtimeTestMeta, "phases:", "outputSchema:{type:'string',format:'file'},phases:", 1)

func TestChatWorkflowRetryKeepsWorkerPermissionsForDescendants(t *testing.T) {
	for _, workbench := range []bool{true, false} {
		mode := "detail"
		if workbench {
			mode = "workbench"
		}
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			s, _ := workflowTestService(t)
			s.worker = &holdingWorker{}
			chat := consumerFixtureChat(t, "chat")
			leaf := savedTestWorkflow(t, s, runtimeTestMeta+`return await worker('produce result',{id:'report',label:'报告',phase:'work',resolve:{kind:'waker',wakerId:'fixture',runtimeId:'fixture-runtime',permissionLevel:'full-access'}});`)
			root := savedTestWorkflow(t, s, awaitChildFileSource(leaf.ID))
			id := startChatChildFixture(t, s, chat, root)
			for _, requestID := range []string{"retry-one", "retry-two"} {
				if err := s.CancelRun(ctx, id); err != nil {
					t.Fatal(err)
				}
				source := runTicks(t, s, id, 2)
				command := StartRunCommand{DefinitionID: source.DefinitionID, RevisionID: source.RevisionID, Args: source.Args,
					Defaults: source.Defaults, WorkerConfigurations: source.WorkerConfigurations, ApprovedScope: &source.ScopeBinding,
					RequestID: requestID, RetrySourceRunID: source.ID, ExpectedSourceVersion: source.Version}
				if workbench {
					command.ChatSource = &ChatRunSource{ChatID: chat.ID, ExecutionID: source.SourceExecutionID, MessageID: source.SourceMessageID, RunID: source.ID, Workbench: true}
				}
				retried, err := s.StartRun(ctx, command)
				if err != nil {
					t.Fatalf("retry was not admitted: %v", err)
				}
				if retried.SourceWorkbench != workbench || (retried.SourceChatID != "") != workbench {
					t.Fatalf("retry acquired the wrong chat ownership: %+v", retried)
				}
				id = retried.ID
			}
			_, err := s.StartRun(ctx, StartRunCommand{DefinitionID: leaf.ID, ParentRunID: id, Depth: 1, RequestID: "retry-child"})
			if !util.HasStatusCode(err, status_error.WorkflowCapabilityUnsupported) {
				t.Fatalf("retry admitted a child worker above the caller's permission: %v", err)
			}
		})
	}
}

func TestChatWorkflowWorkerPermissionCeilingAppliesToDescendants(t *testing.T) {
	s, _ := workflowTestService(t)
	s.worker = &holdingWorker{}
	chat := consumerFixtureChat(t, "chat")
	leaf := savedTestWorkflow(t, s, runtimeTestMeta+`return await worker('produce result',{id:'report',label:'报告',phase:'work',resolve:{kind:'waker',wakerId:'fixture',runtimeId:'fixture-runtime',permissionLevel:'full-access'}});`)
	child := savedTestWorkflow(t, s, awaitChildFileSource(leaf.ID))
	root := savedTestWorkflow(t, s, awaitChildFileSource(child.ID))
	ctx := t.Context()
	chatRun := startChatChildFixture(t, s, chat, root)
	chatChild := dispatchChatFixtureChild(t, s, chatRun, "workflow")
	_, err := s.StartRun(ctx, StartRunCommand{
		DefinitionID: leaf.ID, ParentRunID: chatChild.ChildRunID, Depth: 2,
		RequestID: "chat-grandchild",
	})
	if !util.HasStatusCode(err, status_error.WorkflowCapabilityUnsupported) {
		t.Fatalf("chat descendant with higher worker permission was admitted: %v", err)
	}
	manual, err := s.StartRun(ctx, StartRunCommand{DefinitionID: child.ID, RequestID: "manual-root"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRun(ctx, StartRunCommand{
		DefinitionID: leaf.ID, ParentRunID: manual.ID, Depth: 1, RequestID: "manual-child",
	}); err != nil {
		t.Fatalf("manual workflow worker configuration was rejected: %v", err)
	}
}

func TestChatParentUsesChildFileFromTerminalReceipt(t *testing.T) {
	ctx := t.Context()
	s, _ := workflowTestService(t)
	chat := consumerFixtureChat(t, "chat")
	secondary := consumerFixtureChat(t, "secondary").WorkspaceDirectory
	chat.AdditionalWorkingDirectories = []string{chat.WorkspaceDirectory, secondary}
	path := filepath.Join(secondary, "report.csv")
	encodedPath, _ := json.Marshal(path)
	child := savedTestWorkflow(t, s, chatChildFileMeta+"return "+string(encodedPath)+";")
	parent := savedTestWorkflow(t, s, awaitChildFileSource(child.ID))
	parentRunID := startChatChildFixture(t, s, chat, parent)
	if err := os.WriteFile(path, []byte("本次子流程"), 0o600); err != nil {
		t.Fatal(err)
	}
	started := dispatchChatFixtureChild(t, s, parentRunID, "workflow")
	childRun := runTicks(t, s, started.ChildRunID, 2)
	if childRun.Status != constants.WorkflowRunStatusSucceeded {
		t.Fatalf("child did not settle: %+v", childRun)
	}
	if !childRun.CaptureFilesForChatParent || childRun.SourceChatID != "" || childRun.SourceExecutionID != "" || childRun.SourceThreadID != "" || len(childRun.ChatFiles) != 1 {
		t.Fatalf("child receipts leaked or lost source ownership: %+v", childRun)
	}
	origins, total, err := s.runs.ListVisibleChatRunSources(ctx, chat.ID, 0, 10)
	if err != nil || total != 1 || len(origins) != 1 || origins[0].ID != parentRunID {
		t.Fatalf("chat origins changed when capturing child files: %+v %v", origins, err)
	}
	if err := os.WriteFile(path, []byte("后续运行覆盖"), 0o600); err != nil {
		t.Fatal(err)
	}
	dispatchChatFixtureChild(t, s, parentRunID, "workflow")
	assertChatChildFixtureFile(t, s, parentRunID, "本次子流程")
	assertChatChildFixtureFile(t, s, childRun.ID, "本次子流程")
	parentRun, err := s.runs.GetRun(ctx, parentRunID)
	if err != nil {
		t.Fatal(err)
	}
	if name := parentRun.ChatFiles[""].Name; name != "report.csv" {
		t.Fatalf("forwarded chat attachment name = %q, want report.csv", name)
	}
	legacy := childRun
	legacy.CaptureFilesForChatParent, legacy.ChatFiles = false, nil
	if _, _, err := s.freezeChildResult(ctx, parentRun, started, legacy); !util.HasStatusCode(err, status_error.WorkflowFileInvalid) {
		t.Fatalf("old child with no file receipt was allowed to use project bytes: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s.chatOutputDirectory(childRun), childRun.ChatFiles[""].Path), []byte("收据文件损坏"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.freezeChildResult(ctx, parentRun, started, childRun); !util.HasStatusCode(err, status_error.WorkflowFileInvalid) {
		t.Fatalf("altered child receipt fell back to the project file: %v", err)
	}
}

func TestChatParentFreezesNestedAndExplicitlyJoinedChildFiles(t *testing.T) {
	for _, mode := range []string{"nested", "joined", "detached", "await-detached"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := workflowTestService(t)
			chat := consumerFixtureChat(t, "chat")
			path := filepath.Join(chat.WorkspaceDirectory, "report.csv")
			encodedPath, _ := json.Marshal(path)
			leaf := savedTestWorkflow(t, s, chatChildFileMeta+"return "+string(encodedPath)+";")
			source := chatChildFileMeta + `const handle=startWorkflow('` + leaf.ID + `',{id:'child-start',label:'启动子流程',phase:'work',mode:'` + mode + `'});const child=await joinWorkflow(handle);return child.result;`
			start, collect := "startWorkflow", "joinWorkflow"
			if mode == "nested" || mode == "await-detached" {
				target := leaf.ID
				if mode == "nested" {
					target = savedTestWorkflow(t, s, awaitChildFileSource(leaf.ID)).ID
				}
				source = awaitChildFileSource(target)
				if mode == "await-detached" {
					source = strings.Replace(source, "phase:'work'});", "phase:'work',mode:'detached'});", 1)
				}
				start, collect = "workflow", "workflow"
			}
			parent := savedTestWorkflow(t, s, source)
			parentRunID := startChatChildFixture(t, s, chat, parent)
			if err := os.WriteFile(path, []byte("终态成果"), 0o600); err != nil {
				t.Fatal(err)
			}
			childID := dispatchChatFixtureChild(t, s, parentRunID, start).ChildRunID
			if mode == "nested" {
				leafID := dispatchChatFixtureChild(t, s, childID, "workflow").ChildRunID
				assertChatChildFixtureFile(t, s, leafID, "终态成果")
				if err := os.WriteFile(path, []byte("孙流程同名覆盖"), 0o600); err != nil {
					t.Fatal(err)
				}
				dispatchChatFixtureChild(t, s, childID, "workflow")
			}
			assertChatChildFixtureFile(t, s, childID, "终态成果")
			if err := os.WriteFile(path, []byte("子流程同名覆盖"), 0o600); err != nil {
				t.Fatal(err)
			}
			dispatchChatFixtureChild(t, s, parentRunID, collect)
			assertChatChildFixtureFile(t, s, parentRunID, "终态成果")
		})
	}
}

func TestUncollectedAndOrdinaryChildResultsSkipChatSnapshots(t *testing.T) {
	for _, mode := range []string{"joined", "detached", "ordinary"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := workflowTestService(t)
			chat := consumerFixtureChat(t, "chat")
			path := filepath.Join(chat.WorkspaceDirectory, "report.csv")
			encodedPath, _ := json.Marshal(path)
			leaf := savedTestWorkflow(t, s, chatChildFileMeta+"return "+string(encodedPath)+";")
			source := runtimeTestMeta + `startWorkflow('` + leaf.ID + `',{id:'child-start',label:'启动子流程',phase:'work',mode:'` + mode + `'});return true;`
			if mode == "ordinary" {
				source = chatChildFileMeta + `const handle=startWorkflow('` + leaf.ID + `',{id:'child-start',label:'启动子流程',phase:'work',mode:'detached'});const child=await joinWorkflow(handle);return child.result;`
			}
			parent := savedTestWorkflow(t, s, source)
			if err := os.WriteFile(path, []byte("普通流程文件"), 0o600); err != nil {
				t.Fatal(err)
			}
			var runID string
			var revisions *chatChildRevisionCounter
			if mode == "ordinary" {
				run, err := s.StartRun(t.Context(), StartRunCommand{DefinitionID: parent.ID, RequestID: "manual-start", ApprovedScope: &models.WorkflowScopeBinding{WorkingDirectory: chat.WorkspaceDirectory}})
				if err != nil {
					t.Fatal(err)
				}
				runID = run.ID
				revisions = &chatChildRevisionCounter{WorkflowController: s.store, parentID: parent.ID}
				s.store = revisions
			} else {
				runID = startChatChildFixture(t, s, chat, parent)
			}
			started := dispatchChatFixtureChild(t, s, runID, "startWorkflow")
			if revisions != nil && revisions.calls != 1 {
				t.Fatalf("ordinary child start used %d parent revision lookups, want one for the parent checkpoint", revisions.calls)
			}
			childRun := runTicks(t, s, started.ChildRunID, 2)
			if childRun.Status != constants.WorkflowRunStatusSucceeded || childRun.CaptureFilesForChatParent || len(childRun.ChatFiles) != 0 {
				t.Fatalf("unrelated child captured chat snapshots: %+v", childRun)
			}
			if _, err := os.Stat(s.chatOutputDirectory(childRun)); !os.IsNotExist(err) {
				t.Fatalf("unrelated child touched chat output storage: %v", err)
			}
			if mode == "ordinary" {
				dispatchChatFixtureChild(t, s, runID, "joinWorkflow")
				assertChatChildFixtureFile(t, s, runID, "普通流程文件")
			}
		})
	}
}

type chatChildRevisionCounter struct {
	// WorkflowController 保留原工作流仓储行为。
	controllers.WorkflowController
	// parentID 指定需要观测的父工作流定义。
	parentID string
	// calls 记录对该父工作流的 Revision 查询次数。
	calls int
}

// GetRevision 统计父版本读取后交给原仓储。
func (c *chatChildRevisionCounter) GetRevision(ctx context.Context, definitionID, revisionID string) (models.WorkflowRevision, error) {
	if definitionID == c.parentID {
		c.calls++
	}
	return c.WorkflowController.GetRevision(ctx, definitionID, revisionID)
}

func awaitChildFileSource(childID string) string {
	return chatChildFileMeta + `const child=await workflow('` + childID + `',{id:'child-call',label:'调用子流程',phase:'work'});return child.result;`
}

func startChatChildFixture(t *testing.T, s *Service, chat models.Chat, definition models.WorkflowDefinition) string {
	t.Helper()
	actual, lease := bindConsumerFixture(t, s, chat, definition.ID, "execution", "original-input")
	response, err := s.handleConsumerTool(t.Context(), lease, actual, chat, workflowtool.Input{Action: constants.WorkflowActionRunStart,
		WorkflowID: definition.ID, RevisionID: definition.CurrentRevisionID, RequestID: "start", Args: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	return unwrapWorkflow(response)["id"].(string)
}

func dispatchChatFixtureChild(t *testing.T, s *Service, runID, primitive string) models.WorkflowEffect {
	t.Helper()
	ctx := t.Context()
	run := runTicks(t, s, runID, 1)
	state, err := s.runs.LoadRunState(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	for _, pending := range state.Effects {
		if pending.Boundary.Primitive != primitive || workflowstate.StepTerminal(pending.Status) {
			continue
		}
		s.dispatchChild(ctx, run, pending, false)
		state, err := s.runs.LoadRunState(ctx, runID)
		if err != nil {
			t.Fatal(err)
		}
		for _, effect := range state.Effects {
			if effect.ID == pending.ID && effect.ChildRunID != "" {
				return effect
			}
		}
		t.Fatalf("child effect not recorded: %+v", state.Effects)
	}
	t.Fatalf("no pending %s step: %+v", primitive, state.Effects)
	return models.WorkflowEffect{}
}

func assertChatChildFixtureFile(t *testing.T, s *Service, runID, want string) {
	t.Helper()
	final := runTicks(t, s, runID, 2)
	if final.Status != constants.WorkflowRunStatusSucceeded {
		t.Fatalf("run did not settle successfully: %+v", final)
	}
	file, _, err := s.OpenOutputFile(t.Context(), final.DefinitionID, final.ID, outputFileID(""))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil || string(content) != want {
		t.Fatalf("wrong child file bytes: %q %v", content, err)
	}
}
