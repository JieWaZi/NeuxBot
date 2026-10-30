package workflows

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	workspace "github.com/JieWaZi/NeuxBot/server/internal/agent/host/workspace"

	catalog "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/catalog"

	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/host"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

// liveWorkflowEnvironment 使用生产探测快照，既不伪造模型目录也不读取凭据到日志。
type liveWorkflowEnvironment struct {
	// environment 是当前本机 SDK 探测的可用环境。
	environment models.RuntimeEnvironment
}

// observedLiveRuntime 保存共享宿主的稳定失败阶段，真实验收与生产使用同一独立 Runtime。
type observedLiveRuntime struct {
	// ChatHost 是生产协议与原生会话所有者。
	*runtimehost.ChatHost
	// test 只记录没有凭据的验收身份与阶段。
	test *testing.T
}

func newObservedLiveRuntime(t *testing.T, sdk *catalog.Catalog) observedLiveRuntime {
	t.Helper()
	host := runtimehost.NewChat(runtimehost.ChatConfig{PrepareWorkspace: workspace.New(sdk.Runtimes()).Prepare, Launch: sdk.PrepareLaunch, Runtimes: sdk.Runtimes, Fork: runtimehost.NewForkHandler(sdk)})
	t.Cleanup(func() {
		if err := host.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return observedLiveRuntime{ChatHost: host, test: t}
}

// observedPreparedRun 保留真实准备与资源生命周期，仅记录执行失败阶段。
type observedPreparedRun struct {
	// PreparedChatRun 是生产宿主返回的唯一准备句柄。
	runtimecontract.PreparedChatRun
	// plan 是不含本轮输入的会话声明。
	plan runtimecontract.SessionPlan
	// test 接收脱敏验收诊断。
	test *testing.T
}

func (r observedLiveRuntime) PrepareSession(ctx context.Context, plan runtimecontract.SessionPlan, session runtimecontract.SessionDirective) (runtimecontract.PreparedChatRun, error) {
	prepared, err := r.ChatHost.PrepareSession(ctx, plan, session)
	if err != nil {
		return prepared, err
	}
	return observedPreparedRun{PreparedChatRun: prepared, plan: plan, test: r.test}, nil
}
func (r observedPreparedRun) Run(ctx context.Context, turn runtimecontract.TurnInput, sink runtimecontract.EventSink) runtimecontract.Result {
	result := r.PreparedChatRun.Run(ctx, turn, sink)
	if result.Failure != nil {
		r.test.Logf("actual failure: stage=%s code=%s runtime=%s detection=%s detectionRuntime=%s", result.Failure.Stage, result.Failure.Code, r.plan.Target.RuntimeID, r.plan.Detection.State, r.plan.Detection.Runtime.ID)
	}
	return result
}

func (r liveWorkflowEnvironment) Get(_ context.Context, id string) (models.RuntimeEnvironment, error) {
	if id != r.environment.RuntimeID {
		return models.RuntimeEnvironment{}, status_error.RuntimeUnavailable
	}
	return r.environment, nil
}

func awaitLiveWorkflow(t *testing.T, s *workflowFixture, id string, ready func(controllers.WorkflowRunState) bool) controllers.WorkflowRunState {
	t.Helper()
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		state, err := s.Runs.LoadRunState(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if ready(state) {
			return state
		}
		if workflowstate.TerminalRun(state.Run.Status) {
			t.Fatalf("unexpected live final state: status=%s failure=%v", state.Run.Status, state.Run.Failure)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("live workflow exceeded ten minute acceptance limit")
	return controllers.WorkflowRunState{}
}

func openLiveWorkflow(t *testing.T, ctx context.Context, directory string) (*workflowFixture, func(), models.WorkflowExecutionDefaults, *catalog.Catalog, models.RuntimeEnvironment) {
	t.Helper()
	if os.Getenv("ALLY_WORKFLOW_LIVE_CODEX") != "1" {
		t.Skip("set ALLY_WORKFLOW_LIVE_CODEX=1 to run an actual billable model in a temporary workspace")
	}
	sdk, err := catalog.New(catalog.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer closeCancel()
		if err := sdk.Close(closeCtx); err != nil {
			t.Error(err)
		}
	})
	homeDirectory, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	detection := sdk.Detect(ctx, runtimeconfig.DetectRequest{RuntimeID: "codex", BaseEnvironment: os.Environ(), HomeDirectory: homeDirectory})
	if detection.State != runtimeconfig.DetectionStateReady {
		t.Fatalf("real runtime unavailable: state=%s diagnostics=%v", detection.State, detection.Diagnostics)
	}
	model := os.Getenv("ALLY_WORKFLOW_LIVE_MODEL")
	if model == "" {
		model = detection.DetectedDefaultModel
	}
	environment := models.RuntimeEnvironment{RuntimeID: "codex", Enabled: true, Binary: detection.ResolvedBinary, DefaultModel: model, Detection: detection}
	service, close := openCoreWorkflowPorts(t, directory, newObservedLiveRuntime(t, sdk), liveWorkflowEnvironment{environment: environment})
	return service, close, models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: "system", Model: model, PermissionLevel: constants.PermissionLevelFullAccess}, sdk, environment
}

func recordLiveEvidence(t *testing.T, name string, state controllers.WorkflowRunState, revision models.WorkflowRevision, model string, creationChat string, artifact []byte, verificationFacts ...map[string]any) {
	t.Helper()
	directory := os.Getenv("ALLY_WORKFLOW_EVIDENCE_DIR")
	if directory == "" {
		return
	}
	workers := []map[string]any{}
	for _, effect := range state.Effects {
		if effect.Boundary.Primitive == "worker" {
			workers = append(workers, map[string]any{"nodeId": effect.Boundary.NodeID, "chatId": effect.ChatID, "executionId": effect.ExecutionID, "status": effect.Status})
		}
	}
	evidence := map[string]any{"benchmark": name, "time": time.Now().UTC().Format(time.RFC3339), "runtime": "codex", "model": model, "runId": state.Run.ID, "revisionId": revision.ID, "sourceDigest": revision.SourceDigest, "creationChatId": creationChat, "status": state.Run.Status, "result": state.Run.Result, "workers": workers, "artifactBytes": len(artifact), "artifact": string(artifact), "testBoundary": "production Core, Runtime Host, independent Agent Runtime, Codex CLI and actual model; temporary local filesystem; no model or Worker replacement"}
	if creationChat == "" {
		evidence["creationBoundary"] = "canonical fixture saved through production SaveSource; actual model creation save is a separate acceptance case"
	} else {
		evidence["creationBoundary"] = "actual model saved through scoped workflow CLI"
	}
	if len(verificationFacts) > 0 {
		evidence["verificationFacts"] = verificationFacts[0]
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, name+".json"), append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestLiveRuntimeCreationSaveAndIndependentWorkersWriteRead(t *testing.T) {
	if os.Getenv("ALLY_WORKFLOW_LIVE_CODEX") != "1" {
		t.Skip("set ALLY_WORKFLOW_LIVE_CODEX=1 to run an actual billable model in a temporary workspace")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	sdk, err := catalog.New(catalog.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer closeCancel()
		if err := sdk.Close(closeCtx); err != nil {
			t.Error(err)
		}
	})
	homeDirectory, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	detection := sdk.Detect(ctx, runtimeconfig.DetectRequest{RuntimeID: "codex", BaseEnvironment: os.Environ(), HomeDirectory: homeDirectory})
	if detection.State != runtimeconfig.DetectionStateReady {
		t.Fatalf("real runtime unavailable: state=%s diagnostics=%v", detection.State, detection.Diagnostics)
	}
	model := os.Getenv("ALLY_WORKFLOW_LIVE_MODEL")
	if model == "" {
		model = detection.DetectedDefaultModel
	}
	directory := t.TempDir()
	service, _ := openCoreWorkflowPorts(t, directory, newObservedLiveRuntime(t, sdk), liveWorkflowEnvironment{environment: models.RuntimeEnvironment{RuntimeID: "codex", Enabled: true, Binary: detection.ResolvedBinary, DefaultModel: model, Detection: detection}})
	defaults := models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: "system", Model: model, PermissionLevel: constants.PermissionLevelFullAccess}
	definition, err := createDefinition(ctx, workflowAPIDependencies(service), CreateDefinitionCommand{RequestID: "live", Title: "实际协议验收", CreationWakerID: "fixture-waker", ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: directory}})
	if err != nil {
		t.Fatal(err)
	}
	source := `export const meta={sdk:'ally.workflow/1',title:'真实保存与写入回读',description:'只在验收临时目录写入文本',nodeResultPageOmissions:{'write-file':'只提交协议验收临时文件的写入标记','read-file':'只提交协议验收固定字节的回读标记'},phases:[{id:'approve',title:'审批',detail:'明确批准',next:['write']},{id:'write',title:'写入',detail:'实际工具写文件',next:['verify']},{id:'verify',title:'回读',detail:'独立工具核验',canExit:true}]};
const answer=await askUser('允许在当前验收工作目录写入 live.txt 吗？',{id:'approve-file',label:'测试审批',phase:'approve',options:[{value:'approve',label:'允许'},{value:'deny',label:'拒绝'}],defaultValue:'deny',allowSkip:true});
if(answer.source!=='user'||answer.value!=='approve')return {written:false,verified:false};
const path=flowContext.scope.workingDirectory+'/live.txt';
const write=await worker('Use your actual filesystem tool to write exactly ALLY ALLY! to the approved workflow file '+path+'. Do not simulate the write. Submit the actual result through workflow with action result.submit and params.data.',{id:'write-file',label:'写文件',phase:'write',resolve:{kind:'waker',wakerId:'fixture-waker'},schema:{type:'object',properties:{written:{type:'boolean'}},required:['written'],additionalProperties:false}});
const verify=await worker('Independently use an actual file read tool to read '+path+'. Verify the exact content is ALLY ALLY! and its string length is 10. Submit the actual result through workflow with action result.submit and params.data.',{id:'read-file',label:'回读核验',phase:'verify',resolve:{kind:'waker',wakerId:'fixture-waker'},schema:{type:'object',properties:{verified:{type:'boolean'},content:{type:'string'}},required:['verified','content'],additionalProperties:false}});
return {written:write.written,verified:verify.verified,content:verify.content};`
	creation, err := sendCreationMessage(ctx, workflowAPIDependencies(service), SendCreationMessageCommand{DefinitionID: definition.ID, RequestID: "creation", Defaults: defaults, Content: "Using allycli, discover capabilities and the needed workflow SDK topics, validate the complete source below, and save it with allycli workflow source save (sdkVersion ally.workflow/1, --request-id live-source-save). Preserve the entire source exactly. No filesystem tools and no workflow execution. Your final message must be based on the actual save receipt.\n\n" + source})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		actual, err := service.Chats.ReadExecution(ctx, creation.ChatID, creation.ActiveExecutionID)
		if err != nil {
			t.Fatal(err)
		}
		if workflowstate.ExecutionTerminal(actual.Status) {
			if actual.Status != constants.ExecutionStatusCompleted {
				t.Fatalf("actual creation failed: status=%s code=%s", actual.Status, actual.ErrorCode)
			}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	current, err := service.Store.GetDefinition(ctx, definition.ID)
	if err != nil || current.CurrentRevisionID == "" {
		t.Fatal("actual model did not save via creation CLI", err)
	}
	revision, err := service.Store.GetRevision(ctx, definition.ID, current.CurrentRevisionID)
	if err != nil || revision.Source != source {
		t.Fatal("creation did not preserve requested complete source", err)
	}
	run, err := service.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "run", Defaults: defaults})
	if err != nil {
		t.Fatal(err)
	}
	state := awaitLiveWorkflow(t, service, run.ID, func(state controllers.WorkflowRunState) bool {
		return len(state.Humans) == 1 && state.Humans[0].Status == constants.WorkflowStepStatusWaiting
	})
	if _, err := os.Stat(filepath.Join(directory, "live.txt")); !os.IsNotExist(err) {
		t.Fatal("default approval unexpectedly wrote file", err)
	}
	if err := answerHuman(ctx, workflowAPIDependencies(service), AnswerHumanCommand{RunID: run.ID, HumanID: state.Humans[0].ID, RequestID: "explicit-test-approval", Value: "approve"}); err != nil {
		t.Fatal(err)
	}
	state = awaitLiveWorkflow(t, service, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
	result, _ := state.Run.Result.(map[string]any)
	data, err := os.ReadFile(filepath.Join(directory, "live.txt"))
	if err != nil || string(data) != "ALLY ALLY!" || state.Run.Status != constants.WorkflowRunStatusSucceeded || result["verified"] != true || result["written"] != true || result["content"] != "ALLY ALLY!" {
		t.Fatalf("actual write/read failed: status=%s result=%v file=%q error=%v", state.Run.Status, result, data, err)
	}
	if len(state.Effects) != 3 || state.Effects[1].ExecutionID == state.Effects[2].ExecutionID {
		t.Fatal("workers reused execution identity")
	}
	t.Logf("actual Runtime=codex model=%s run=%s revision=%s sourceDigest=%s creationChat=%s fileBytes=%d", model, run.ID, run.RevisionID, revision.SourceDigest, creation.ChatID, len(data))
	recordLiveEvidence(t, "live-creation-write-read", state, revision, model, creation.ChatID, data)
}

func saveLiveSource(t *testing.T, ctx context.Context, s *workflowFixture, definition models.WorkflowDefinition, defaults models.WorkflowExecutionDefaults, source string) (models.WorkflowRevision, string) {
	t.Helper()
	creation, err := sendCreationMessage(ctx, workflowAPIDependencies(s), SendCreationMessageCommand{DefinitionID: definition.ID, RequestID: util.New(), Defaults: defaults, Content: "Use allycli capabilities and the needed workflow SDK topics, validate the complete supplied source, and save exactly that source using allycli workflow source save, sdkVersion ally.workflow/1 and a stable requestId. Do not execute the workflow or edit files. Preserve every line. Base your final message on the actual save receipt.\n\n" + source})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Minute)
	for time.Now().Before(deadline) {
		actual, err := s.Chats.ReadExecution(ctx, creation.ChatID, creation.ActiveExecutionID)
		if err != nil {
			t.Fatal(err)
		}
		if workflowstate.ExecutionTerminal(actual.Status) {
			if actual.Status != constants.ExecutionStatusCompleted {
				t.Fatalf("actual creation failed: status=%s code=%s", actual.Status, actual.ErrorCode)
			}
			current, err := s.Store.GetDefinition(ctx, definition.ID)
			if err != nil || current.CurrentRevisionID == "" {
				t.Fatal("actual creation did not save source", err)
			}
			revision, err := s.Store.GetRevision(ctx, definition.ID, current.CurrentRevisionID)
			if err != nil {
				t.Fatal(err)
			}
			if revision.Source != source && !sameLiveProgram(source, revision.Source) {
				if directory := os.Getenv("ALLY_WORKFLOW_EVIDENCE_DIR"); directory != "" {
					if err := os.MkdirAll(directory, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(directory, "creation-unexpected-source.js"), []byte(revision.Source), 0600); err != nil {
						t.Fatal(err)
					}
				}
				t.Fatal("actual creation changed the supplied workflow behavior")
			}
			return revision, creation.ChatID
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("creation source save timed out")
	return models.WorkflowRevision{}, ""
}

// sameLiveProgram 仅允许创作改变排版，忽略编译源码范围和由位置派生的内部循环身份。
func sameLiveProgram(expected, actual string) bool {
	normalize := func(source string) (workflowdsl.ProgramBundle, bool) {
		compiled, _, err := workflowdsl.Compile(source)
		if err != nil {
			return workflowdsl.ProgramBundle{}, false
		}
		bundle := compiled.Bundle
		for id, program := range bundle.Programs {
			for index := range program.Code {
				program.Code[index].Range = workflowdsl.SourceRange{}
				if program.Code[index].Op == constants.WorkflowOpLoop {
					program.Code[index].NodeID = "loop." + strconv.Itoa(index)
				}
			}
			bundle.Programs[id] = program
		}
		return bundle, true
	}
	before, beforeOK := normalize(expected)
	after, afterOK := normalize(actual)
	return beforeOK && afterOK && reflect.DeepEqual(before, after)
}

func TestLiveSourceComparisonAllowsFormattingButRejectsBehaviorChanges(t *testing.T) {
	source := `export const meta={sdk:'ally.workflow/1',title:'comparison',description:'formatting',phases:[{id:'main',title:'main',detail:'calculate',canExit:true}]}; let value=0; while(value<2){value++;}return value;`
	formatted := strings.ReplaceAll(source, ";", ";\n")
	if !sameLiveProgram(source, formatted) || sameLiveProgram(source, strings.Replace(source, "value<2", "value<3", 1)) {
		t.Fatal("formatting comparison accepted changed execution behavior")
	}
}

func TestLiveF1WeeklyCreationRestartRevisionWriteRead(t *testing.T) {
	runLiveF1(t, true)
}

// TestLiveF1WeeklyCanonicalSourceWithActualWorkers 将创作网络与完整执行接缝分别验收。
func TestLiveF1WeeklyCanonicalSourceWithActualWorkers(t *testing.T) {
	runLiveF1(t, false)
}

// TestLiveF0CreationSaveAndActualGoResult 验证真实创作保存后的纯计算与参数默认值。
func TestLiveF0CreationSaveAndActualGoResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	directory := t.TempDir()
	s, _, defaults, _, _ := openLiveWorkflow(t, ctx, directory)
	source, err := os.ReadFile("../../internal/workflow/dsl/testdata/f0.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "live-compute", Title: "真实创作纯计算", CreationWakerID: "fixture-waker", ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: directory}})
	if err != nil {
		t.Fatal(err)
	}
	revision, creationChat := saveLiveSource(t, ctx, s, definition, defaults, string(source))
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "live-run", Defaults: defaults, Args: map[string]any{"text": "ally"}})
	if err != nil {
		t.Fatal(err)
	}
	state := awaitLiveWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
	result, _ := state.Run.Result.(map[string]any)
	if state.Run.Status != constants.WorkflowRunStatusSucceeded || result["message"] != "ALLY ALLY!" || result["length"] != float64(10) || result["repeat"] != float64(2) || state.Run.RevisionID != revision.ID {
		t.Fatalf("actual created F0 result mismatch: status=%s result=%v", state.Run.Status, result)
	}
	recordLiveEvidence(t, "live-f0-compute", state, revision, defaults.Model, creationChat, nil)
	t.Logf("actual F0 run=%s revision=%s message=%v length=%v", run.ID, revision.ID, result["message"], result["length"])
}

func runLiveF1(t *testing.T, creationSave bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	directory := t.TempDir()
	s, close, defaults, sdk, environment := openLiveWorkflow(t, ctx, directory)
	source, err := os.ReadFile("../../internal/workflow/dsl/testdata/weekly-report.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "live-weekly", Title: "真实周报闭环", CreationWakerID: "fixture-waker", ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: directory}})
	if err != nil {
		t.Fatal(err)
	}
	var revision models.WorkflowRevision
	var creationChat string
	if creationSave {
		revision, creationChat = saveLiveSource(t, ctx, s, definition, defaults, string(source))
	} else {
		receipt, diagnostics, err := s.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: definition.ID, RequestID: "canonical-source", Source: string(source), SDKVersion: constants.WorkflowSDKVersion})
		if err != nil {
			t.Fatalf("canonical source save: %v diagnostics=%v", err, diagnostics)
		}
		revision, err = s.Store.GetRevision(ctx, definition.ID, receipt.RevisionID)
		if err != nil {
			t.Fatal(err)
		}
	}
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "live-run", Args: map[string]any{"weekNotes": "本周完成订单导出接口联调，10%灰度验证通过。第三方依赖版本仍待供应方确认，可能影响全量发布时间。下周目标是确认依赖版本并提交全量发布方案。", "team": "本地验收", "weekRange": "2026-09-28 ~ 2026-10-04"}, Defaults: defaults})
	if err != nil {
		t.Fatal(err)
	}
	pending := func(state controllers.WorkflowRunState) models.WorkflowHumanRequest {
		for _, human := range state.Humans {
			if human.Status == constants.WorkflowStepStatusWaiting {
				return human
			}
		}
		return models.WorkflowHumanRequest{}
	}
	state := awaitLiveWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return pending(state).ID != "" })
	human := pending(state)
	if !strings.Contains(human.Prompt, "## 本周进展") {
		t.Fatal("actual model did not produce the required three-section draft")
	}
	if _, err := os.Stat(filepath.Join(directory, "reports")); !os.IsNotExist(err) {
		t.Fatal("file was written before explicit approval", err)
	}
	close()
	s, _ = openCoreWorkflowPorts(t, directory, newObservedLiveRuntime(t, sdk), liveWorkflowEnvironment{environment: environment})
	state = awaitLiveWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return pending(state).ID != "" })
	if pending(state).ID != human.ID || state.Run.RevisionID != revision.ID {
		t.Fatal("restart changed the pending human request or frozen revision")
	}
	if !strings.Contains(human.Prompt, "第 3 轮") {
		if err := answerHuman(ctx, workflowAPIDependencies(s), AnswerHumanCommand{RunID: run.ID, HumanID: human.ID, RequestID: "live-revise", Value: "在总结中明确标注这是临时本地验收周报；保留所有已有事实，不新增指标。"}); err != nil {
			t.Fatal(err)
		}
		state = awaitLiveWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool {
			return pending(state).ID != "" && pending(state).ID != human.ID
		})
		human = pending(state)
	}
	if err := answerHuman(ctx, workflowAPIDependencies(s), AnswerHumanCommand{RunID: run.ID, HumanID: human.ID, RequestID: "live-approve", Value: "approve"}); err != nil {
		t.Fatal(err)
	}
	state = awaitLiveWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
	result, _ := state.Run.Result.(map[string]any)
	reportPath, _ := result["reportPath"].(string)
	if !filepath.IsAbs(reportPath) {
		reportPath = filepath.Join(directory, reportPath)
	}
	report, err := os.ReadFile(reportPath)
	if err != nil || state.Run.Status != constants.WorkflowRunStatusSucceeded || result["verified"] != true || string(report) != result["report"] {
		t.Fatalf("actual F1 file/result mismatch: status=%s result=%v fileError=%v", state.Run.Status, result, err)
	}
	chats, executions := map[string]bool{}, map[string]bool{}
	for _, effect := range state.Effects {
		if effect.Boundary.Primitive == "worker" {
			if chats[effect.ChatID] || executions[effect.ExecutionID] || effect.ChatID == "" || effect.ExecutionID == "" {
				t.Fatal("actual F1 reused Worker identities")
			}
			chats[effect.ChatID], executions[effect.ExecutionID] = true, true
		}
	}
	evidenceName := "live-f1-weekly"
	if !creationSave {
		evidenceName = "live-f1-weekly-canonical-source"
	}
	recordLiveEvidence(t, evidenceName, state, revision, defaults.Model, creationChat, report)
	t.Logf("actual F1 run=%s revision=%s workers=%d humanRequests=%d reportBytes=%d verified=%v", run.ID, revision.ID, len(chats), len(state.Humans), len(report), result["verified"])
}

// TestLiveF2CanonicalMaterialsReworkApprovalAndRealFiles 以真实模型和取消协议验证完整预审执行。
func TestLiveF2CanonicalMaterialsReworkApprovalAndRealFiles(t *testing.T) {
	runLiveF2(t, false)
}

// TestLiveF2CreationMaterialsReworkApprovalAndRealFiles 从真实创作保存开始验证完整预审执行。
func TestLiveF2CreationMaterialsReworkApprovalAndRealFiles(t *testing.T) {
	runLiveF2(t, true)
}

func runLiveF2(t *testing.T, creationSave bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	directory := t.TempDir()
	s, _, defaults, _, _ := openLiveWorkflow(t, ctx, directory)
	source, err := os.ReadFile("../../internal/workflow/dsl/testdata/environment-release.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "live-release", Title: "真实发布预审", CreationWakerID: "fixture-waker", ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: directory}})
	if err != nil {
		t.Fatal(err)
	}
	var revision models.WorkflowRevision
	var creationChat string
	if creationSave {
		revision, creationChat = saveLiveSource(t, ctx, s, definition, defaults, string(source))
	} else {
		receipt, diagnostics, err := s.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: definition.ID, RequestID: "canonical-source", Source: string(source), SDKVersion: constants.WorkflowSDKVersion})
		if err != nil {
			t.Fatalf("canonical source save: %v diagnostics=%v", err, diagnostics)
		}
		revision, err = s.Store.GetRevision(ctx, definition.ID, receipt.RevisionID)
		if err != nil {
			t.Fatal(err)
		}
	}
	requirements := "迭代为本地验收；环境 A/B/C 均要求订单导出可用，当前需求已经覆盖。发布日期 2026-10-05。优先级口径 P0=发布必须项，P1=可延后；覆盖分析按每环境一项统计。"
	if creationSave {
		requirements = "环境 A/B/C 均要求订单导出可用，当前需求已经覆盖。优先级口径 P0=发布必须项，P1=可延后；覆盖分析按每环境一项统计。材料未提供迭代名称和发布日期，禁止推断或使用当前日期；核查时 iteration 和 releaseDate 必须为空字符串，并将两者列入 missingFields，等待用户补全。"
	}
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "live-release", Defaults: defaults, Args: map[string]any{
		"requirements": requirements,
		"defects":      "缺陷口径 critical=未修复且阻断发布，high=存在严重风险。环境 A、B 均存在一个未修复 critical：第三方服务缺少关键导出权限，不能在本地修复，供应方尚未承诺修复时间；任何未执行建议都不能降低现存风险，预测剩余风险应保持 high。环境 C 无缺陷。本材料仅为虚构验收，不联系真实第三方。",
		"environments": []string{"A", "B", "C"}, "riskThreshold": 1, "localAcceptance": true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	answered := map[string]bool{}
	reworked, cancelledC, filledMaterials := false, false, false
	deadline := time.Now().Add(20 * time.Minute)
	var final controllers.WorkflowRunState
	for time.Now().Before(deadline) {
		state, err := s.Runs.LoadRunState(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if workflowstate.TerminalRun(state.Run.Status) {
			final = state
			break
		}
		for _, effect := range state.Effects {
			prompt := ""
			if len(effect.Boundary.Arguments) > 0 {
				prompt, _ = effect.Boundary.Arguments[0].Data.(string)
			}
			if !cancelledC && effect.Boundary.NodeID == "coverage-environment" && strings.Contains(prompt, "环境=C") && effect.ChatID != "" && !workflowstate.StepTerminal(effect.Status) {
				if err := s.Worker.Cancel(ctx, effect); err != nil {
					t.Fatal(err)
				}
				cancelledC = true
			}
		}
		for _, human := range state.Humans {
			if human.Status != constants.WorkflowStepStatusWaiting || answered[human.ID] {
				continue
			}
			var node string
			for _, effect := range state.Effects {
				if effect.ID == human.EffectID {
					node = effect.Boundary.NodeID
				}
			}
			command := AnswerHumanCommand{RunID: run.ID, HumanID: human.ID, RequestID: "live-answer-" + human.ID}
			switch node {
			case "fill-materials":
				command.Value = `{"iteration":"本地验收","releaseDate":"2026-10-05","severityConvention":"critical 是未修复发布阻塞；high 是严重风险","priorityConvention":"P0 必须；P1 可延后"}`
				filledMaterials = true
			case "rework-note":
				command.Value = "注明这只是临时目录验收，第三方缺陷未实际修复；建议不能使剩余风险降级，保持 high，不增加新事实。"
			case "approve-item":
				if strings.Contains(human.Prompt, "环境=A") {
					if !reworked {
						command.Value = "rework"
						reworked = true
					} else {
						command.Value = "approve"
					}
				} else {
					command.Skip = true
				}
			default:
				t.Fatalf("unexpected live F2 human node %s", node)
			}
			if err := answerHuman(ctx, workflowAPIDependencies(s), command); err != nil {
				t.Fatal(err)
			}
			answered[human.ID] = true
		}
		time.Sleep(200 * time.Millisecond)
	}
	result, _ := final.Run.Result.(map[string]any)
	stats, _ := result["stats"].(map[string]any)
	materialContext, _ := result["context"].(map[string]any)
	failedEnvironments, _ := stats["failedEnvironments"].([]any)
	if final.Run.Status != constants.WorkflowRunStatusSucceeded || result["decision"] != "不予发布" || !reworked || !cancelledC || len(failedEnvironments) != 1 || failedEnvironments[0] != "C" || result["notificationDraft"] != nil || creationSave && (!filledMaterials || materialContext["iteration"] != "本地验收" || materialContext["releaseDate"] != "2026-10-05") {
		t.Fatalf("live F2 did not satisfy actual facts: status=%s reworked=%v cancelledC=%v filledMaterials=%v result=%v", final.Run.Status, reworked, cancelledC, filledMaterials, result)
	}
	artifact, actualFileCount := assertLiveF2Artifacts(t, directory, final)
	if stats["verifiedCount"] != float64(actualFileCount) || stats["executedCount"] != float64(actualFileCount) {
		t.Fatalf("actual F2 statistics disagreed with filesystem and independent verifications: files=%d stats=%v", actualFileCount, stats)
	}
	evidenceName := "live-f2-release"
	if !creationSave {
		evidenceName = "live-f2-release-canonical-source"
	}
	recordLiveEvidence(t, evidenceName, final, revision, defaults.Model, creationChat, artifact, map[string]any{"actualFileCount": actualFileCount, "allPayloadsMatchFrozenInstructions": true, "independentVerificationCount": actualFileCount, "filledMaterials": filledMaterials, "reworked": reworked, "cancelledC": cancelledC})
	t.Logf("actual F2 run=%s revision=%s firstArtifactBytes=%d actualFiles=%d answeredRequests=%d verified=%v", run.ID, revision.ID, len(artifact), actualFileCount, len(answered), stats["verifiedCount"])
}

// assertLiveF2Artifacts 逐项核对冻结写入指令、实际文件和独立验证，不预设模型列出的条目数。
func assertLiveF2Artifacts(t *testing.T, directory string, state controllers.WorkflowRunState) ([]byte, int) {
	t.Helper()
	written, verified := map[string]bool{}, map[string]bool{}
	approved := map[string]bool{}
	result, _ := state.Run.Result.(map[string]any)
	records, _ := result["approvalRecords"].([]any)
	for _, value := range records {
		record, _ := value.(map[string]any)
		if record["approved"] != true {
			continue
		}
		id, _ := record["itemId"].(string)
		if record["environment"] != "A" || record["source"] != "user" || id == "" {
			t.Fatal("actual F2 approval did not come from explicit user consent for environment A")
		}
		approved[id] = true
	}
	var firstArtifact []byte
	for _, effect := range state.Effects {
		node := effect.Boundary.NodeID
		if node != "execute-item" && node != "verify-item" {
			continue
		}
		if effect.Result == nil {
			t.Fatalf("actual F2 file Worker has no verified result: node=%s status=%s", node, effect.Status)
		}
		result, _ := effect.Result.Data.(map[string]any)
		if effect.Status != constants.WorkflowStepStatusSucceeded || node == "execute-item" && result["written"] != true || node == "verify-item" && (result["exists"] != true || result["matches"] != true || result["verified"] != true) {
			t.Fatalf("actual F2 file Worker did not complete its effect: node=%s status=%s", node, effect.Status)
		}
		if len(effect.Boundary.Arguments) == 0 {
			t.Fatal("actual F2 file instruction was not frozen")
		}
		prompt, _ := effect.Boundary.Arguments[0].Data.(string)
		marker := "内容必须精确为 "
		if node == "verify-item" {
			marker = "精确内容应为 "
		}
		_, suffix, found := strings.Cut(prompt, marker)
		var expected map[string]any
		if !found || json.NewDecoder(strings.NewReader(suffix)).Decode(&expected) != nil {
			t.Fatal("actual F2 file instruction lost its exact JSON payload")
		}
		id, _ := expected["itemId"].(string)
		if expected["environment"] != "A" || expected["accepted"] != true || !approved[id] || strings.ContainsAny(id, `/\`) {
			t.Fatal("actual F2 attempted a file outside explicitly approved environment A")
		}
		data, err := os.ReadFile(filepath.Join(directory, "acceptance", id+".json"))
		var actual map[string]any
		if err != nil || json.Unmarshal(data, &actual) != nil || !reflect.DeepEqual(expected, actual) {
			t.Fatalf("actual F2 file differs from its frozen instruction: item=%s fileError=%v", id, err)
		}
		seen := written
		if node == "verify-item" {
			seen = verified
		}
		if seen[id] {
			t.Fatalf("actual F2 repeated a file effect: node=%s item=%s", node, id)
		}
		seen[id] = true
		if node == "execute-item" && firstArtifact == nil {
			firstArtifact = data
		}
	}
	if len(written) == 0 || !reflect.DeepEqual(written, verified) {
		t.Fatal("actual F2 writes and independent verifications do not match")
	}
	entries, err := os.ReadDir(filepath.Join(directory, "acceptance"))
	if err != nil || len(entries) != len(written) {
		t.Fatal("actual F2 created files beyond its recorded approved writes", err)
	}
	return firstArtifact, len(written)
}
