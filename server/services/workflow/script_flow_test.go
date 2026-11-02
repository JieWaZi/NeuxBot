package workflow

import (
	"context"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// reviewExampleWorker 仅替代模型，通过真实候选提交和执行收据驱动持久管道。
type reviewExampleWorker struct {
	// retryWorker 复用测试端口的接纳、查询和取消行为。
	retryWorker
	// service 提供当前节点专属的结构化结果提交能力。
	service *Service
	// failCandidate 指定审阅时失败的候选，检验缺项不能冒充完整结果。
	failCandidate string
}

func (w *reviewExampleWorker) Dispatch(ctx context.Context, input WorkerDispatch) (WorkerReceipt, error) {
	receipt := WorkerReceipt{ExecutionID: input.Effect.ExecutionID, Status: constants.ExecutionStatusCompleted}
	var prompt struct {
		// Candidate 是本次独立读取的原始候选证据。
		Candidate struct {
			// Value 是跨节点保留的稳定候选身份。
			Value string `json:"value"`
			// Text 是示例提供的原始业务证据。
			Text string `json:"text"`
		} `json:"candidate"`
		// Review 是复核节点需要反驳的上一阶段主张。
		Review struct {
			// Claim 是待核实的业务判断。
			Claim string `json:"claim"`
		} `json:"review"`
	}
	text, _ := input.Effect.Boundary.Arguments[0].Data.(string)
	if err := json.Unmarshal([]byte(text), &prompt); err != nil {
		return receipt, err
	}
	if input.Effect.Boundary.NodeID == "review-candidate" && prompt.Candidate.Value == w.failCandidate {
		receipt.Status, receipt.ErrorCode = constants.ExecutionStatusFailed, "EXAMPLE_READ_FAILED"
		return receipt, nil
	}
	data := map[string]any{"claim": "候选存在未完成检查", "evidence": prompt.Candidate.Text}
	if input.Effect.Boundary.NodeID == "verify-candidate" {
		data = map[string]any{"complete": prompt.Review.Claim != "" && prompt.Candidate.Text != "", "supported": strings.Contains(prompt.Candidate.Text, "尚未完成"), "reason": prompt.Candidate.Text}
	}
	scope := workflowtool.Scope{Key: input.Effect.ID, Token: input.Effect.AuthToken, Kind: constants.WorkflowCapabilityScopeResult}
	_, err := w.service.handleTool(ctx, scope, workflowtool.Input{Action: constants.WorkflowActionResultSubmit, Data: data})
	return receipt, err
}

func TestWorkflowReviewExampleReportsCoverageAndRejectsMissingItem(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip(err)
	}
	source, err := os.ReadFile("../../../skills/ally/references/workflow-review-example.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{"", "b"} {
		t.Run("failed="+failure, func(t *testing.T) {
			s, _ := workflowTestService(t)
			s.pythonExecutable = python
			s.worker = &reviewExampleWorker{service: s, failCandidate: failure}
			directory, _ := filepath.EvalSymlinks(t.TempDir())
			definition := scopedProcessDefinition(t, s, string(source), directory)
			run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "start", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "fixture", PermissionLevel: constants.PermissionLevelFullAccess}})
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(s.Close)
			state := waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return len(state.Humans) == 1 })
			choices, _ := state.Humans[0].Options["choices"].([]any)
			if len(choices) != 3 || state.Humans[0].SnapshotDigest == "" {
				t.Fatalf("Python candidates did not become frozen choices: %+v", state.Humans[0])
			}
			s.state.Commands.Lock()
			err = s.transactions.Transaction(context.Background(), func(ctx context.Context) error {
				current, err := s.state.OwnedRun(ctx, run.ID)
				if err != nil {
					return err
				}
				current.Humans[0].Value = []any{"a", "b"}
				current.Humans[0].Source = "user"
				current.Humans[0].Status = constants.WorkflowStepStatusAnswered
				return s.state.CommitRun(ctx, current)
			})
			s.state.Commands.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			final := waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
			var data map[string]any
			if failure == "" {
				if final.Run.Status != constants.WorkflowRunStatusSucceeded {
					t.Fatalf("normal review failed: %+v", final.Run)
				}
				data = final.Run.Result.(map[string]any)
				items := data["items"].([]any)
				if len(items) != 2 || items[0].(map[string]any)["verification"].(map[string]any)["supported"] != true || items[1].(map[string]any)["verification"].(map[string]any)["supported"] != false || items[1].(map[string]any)["verification"].(map[string]any)["reason"] != "虚构记录：发布检查已完成。" {
					t.Fatalf("refuted claim was lost or treated as execution failure: %+v", items)
				}
			} else {
				if final.Run.Status != constants.WorkflowRunStatusFailed || final.Run.Failure == nil {
					t.Fatalf("missing review reported success: %+v", final.Run)
				}
				data = final.Run.Failure.Data.(map[string]any)
				if data["code"] != "REVIEW_INCOMPLETE" {
					t.Fatalf("missing business failure: %+v", data)
				}
			}
			coverage := data["coverage"].(map[string]any)
			wantCompleted := float64(2)
			wantFailed := []any{}
			if failure != "" {
				wantCompleted, wantFailed = 1, []any{"b"}
			}
			if coverage["discovered"] != float64(3) || coverage["selected"] != float64(2) || coverage["completed"] != wantCompleted || !reflect.DeepEqual(coverage["failed"], wantFailed) || !reflect.DeepEqual(coverage["excluded"], []any{"c"}) {
				t.Fatalf("coverage lost original candidate identities: %+v", coverage)
			}
		})
	}
}

// textResultWorker 只替代模型边界，脚本与持久调度使用真实实现。
type textResultWorker struct {
	// retryWorker 提供配置解析与取消测试端口。
	retryWorker
}

func (*textResultWorker) Dispatch(_ context.Context, input WorkerDispatch) (WorkerReceipt, error) {
	return WorkerReceipt{ExecutionID: input.Effect.ExecutionID, Status: constants.ExecutionStatusCompleted, FinalPresent: true, Text: `{"analysisText":"a 分析"}`}, nil
}
func TestScriptHumanWorkerBusinessCheckFlow(t *testing.T) {
	s, _ := workflowTestService(t)
	s.worker = &textResultWorker{}
	directory, _ := filepath.EvalSymlinks(t.TempDir())
	source := `export const meta={sdk:'ally.workflow',title:'工作流闭环',description:'脚本候选',phases:[{id:'work',title:'工作',detail:'执行'}],actions:[{id:'list',type:'script',scriptId:'list',label:'查询',risk:'read'},{id:'verify',type:'script',scriptId:'verify',label:'检查',risk:'read'}]};
 export const scripts={list:{language:'python',protocol:'json',code:'import json,sys\njson.dump({"items":[{"value":"a","label":"候选甲"}]},sys.stdout)'},verify:{language:'python',protocol:'json',code:'import json,sys\nx=json.load(sys.stdin)\njson.dump({"valid":x["selected"] in x["text"]},sys.stdout)'}};
 const listed=await action('list',{id:'list-node',label:'查询',phase:'work',input:false});
 const selected=await askUser('选择',{id:'choose',label:'选择',phase:'work',kind:'select',choices:listed.data.items});
 const analysis=await worker(selected.value,{id:'analyze',label:'分析',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:{type:'object',properties:{analysisText:{type:'string'}},required:['analysisText']}});
 const verified=await action('verify',{id:'verify-node',label:'校验',phase:'work',input:{selected:selected.value,text:analysis.analysisText},successCondition:{path:'/valid',equals:true}});
 return {analysisText:analysis.analysisText,valid:verified.data.valid};`
	definition := scopedProcessDefinition(t, s, source, directory)
	run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "start", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "fixture", PermissionLevel: constants.PermissionLevelFullAccess}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	state := waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return len(state.Humans) == 1 })
	if state.Humans[0].SnapshotDigest == "" {
		t.Fatal("candidate snapshot missing")
	}
	s.state.Commands.Lock()
	err = s.transactions.Transaction(context.Background(), func(ctx context.Context) error {
		current, err := s.state.OwnedRun(ctx, run.ID)
		if err != nil {
			return err
		}
		current.Humans[0].Value = "a"
		current.Humans[0].Source = "user"
		current.Humans[0].Status = constants.WorkflowStepStatusAnswered
		return s.state.CommitRun(ctx, current)
	})
	s.state.Commands.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	final := waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
	data, _ := final.Run.Result.(map[string]any)
	if final.Run.Status != constants.WorkflowRunStatusSucceeded || data["valid"] != true {
		t.Fatalf("flow failed %+v %+v", final.Run, final.Effects)
	}
}
