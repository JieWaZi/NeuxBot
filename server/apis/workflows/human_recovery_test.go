package workflows

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

func TestHumanWaitIsDurableDefaultNeverApprovesAndFirstAnswerWins(t *testing.T) {
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, humanSource)
	run, err := s.StartRun(context.Background(), workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "run"})
	if err != nil {
		t.Fatal(err)
	}
	result := runTicks(t, s, run.ID, 3)
	if result.Status != constants.WorkflowRunStatusWaiting {
		t.Fatalf("default auto-submitted: %+v", result)
	}
	snapshot, err := s.Runs.LoadRunState(context.Background(), run.ID)
	if err != nil || len(snapshot.Humans) != 1 {
		t.Fatalf("wait: %+v %v", snapshot, err)
	}
	human := snapshot.Humans[0]
	if human.Status != constants.WorkflowStepStatusWaiting || human.Value != nil {
		t.Fatal("default became an answer")
	}
	// 释放旧租约模拟旧进程退出，新 Service 只读取数据库中的检查点和问题。
	now := time.Now().UnixMilli()
	if _, _, err = s.Runs.AcquireLease(context.Background(), run.ID, s.State.Owner, now, now-1); err != nil {
		t.Fatal(err)
	}
	restored := workflowTestNew(workflow.Config{Store: s.Store, Runs: s.Runs, Transactions: s.Transactions})
	if err = restored.Tick(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	answer := AnswerHumanCommand{RunID: run.ID, HumanID: human.ID, RequestID: "answer", Value: "approve"}
	if err = answerHuman(context.Background(), workflowAPIDependencies(restored), answer); err != nil {
		t.Fatal(err)
	}
	if err = answerHuman(context.Background(), workflowAPIDependencies(restored), answer); err != nil {
		t.Fatal("answer replay failed", err)
	}
	answer.RequestID = "late-reject"
	answer.Value = "reject"
	if err = answerHuman(context.Background(), workflowAPIDependencies(restored), answer); !errors.Is(err, status_error.WorkflowStateConflict) {
		t.Fatal("late answer replaced approval", err)
	}
	result = runTicks(t, restored, run.ID, 4)
	object, _ := result.Result.(map[string]any)
	if result.Status != constants.WorkflowRunStatusSucceeded || object["approved"] != true || object["source"] != "user" {
		t.Fatalf("restored result: %+v", result)
	}
}

const humanSource = `export const meta={sdk:'ally.workflow/1',title:'人工关卡',description:'恢复测试',phases:[{id:'approval',title:'确认',detail:'显式批准',canExit:true}]};
const answer=await askUser('是否批准？',{id:'approve',label:'批准结果',phase:'approval',options:[{value:'approve',label:'批准'},{value:'reject',label:'拒绝'}],defaultValue:'reject',allowSkip:false});
return {approved:answer.source==='user' && answer.value==='approve',source:answer.source};`
