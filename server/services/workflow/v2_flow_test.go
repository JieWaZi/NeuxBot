package workflow

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"path/filepath"
	"testing"
)

// v2TextWorker 只替代模型边界，脚本与持久调度使用真实实现。
type v2TextWorker struct {
	// retryWorker 提供配置解析与取消测试端口。
	retryWorker
}

func (*v2TextWorker) Dispatch(_ context.Context, input WorkerDispatch) (WorkerReceipt, error) {
	return WorkerReceipt{ExecutionID: input.Effect.ExecutionID, Status: constants.ExecutionStatusCompleted, FinalPresent: true, Text: `{"analysisText":"a 分析"}`}, nil
}
func TestV2ScriptHumanWorkerBusinessCheckFlow(t *testing.T) {
	s, _ := workflowTestService(t)
	s.worker = &v2TextWorker{}
	directory, _ := filepath.EvalSymlinks(t.TempDir())
	source := `export const meta={sdk:'ally.workflow/2',title:'V2闭环',description:'脚本候选',phases:[{id:'work',title:'工作',detail:'执行'}],actions:[{id:'list',type:'script',scriptId:'list',label:'查询',risk:'read'},{id:'verify',type:'script',scriptId:'verify',label:'检查',risk:'read'}]};
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
