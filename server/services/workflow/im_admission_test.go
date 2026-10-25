package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestIMTargetFailureBlocksAdmissionBeforeAnyNode(t *testing.T) {
	s, _ := workflowTestService(t)
	source := `export const meta={sdk:'ally.workflow/1',title:'发送前校验',description:'在昂贵节点前检查',phases:[{id:'delivery-phase',title:'发送',detail:'交付',canExit:true}],actions:[{id:'send',label:'发送',description:'发送微信',risk:'external',type:'builtin',handler:'send',connectorIds:['b17d96aa55314c33ab46c172e8cab012']}]}; export const actions={send:async(ctx,input)=>await ctx.callTool({callId:'send',connectorId:'b17d96aa55314c33ab46c172e8cab012',toolName:'im_delivery',input:{action:'deliveries.create',targetId:'target',content:'正文'}})};await askUser('输入',{id:'first',label:'业务步骤',phase:'delivery-phase'});return await action('send',{id:'deliver',label:'发送微信',phase:'delivery-phase'});`
	d := savedTestWorkflow(t, s, source)
	s.imValidate = func(_ context.Context, id string) error {
		if id != "target" {
			t.Fatalf("校验了错误目标: %s", id)
		}
		return status_error.IMTargetUnavailable
	}
	_, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: d.ID, RequestID: "start"})
	if !errors.Is(err, status_error.IMTargetUnavailable) {
		t.Fatalf("发送依赖失效仍运行: %v", err)
	}
	runs, err := s.runs.ListRuns(context.Background(), d.ID, 0, "", 50)
	if err != nil || len(runs) != 0 {
		t.Fatalf("接纳前失败仍创建运行: %+v %v", runs, err)
	}
	checks := 0
	s.imValidate = func(context.Context, string) error {
		checks++
		if checks == 2 {
			return status_error.IMWorkflowDeliveryNotAllowed
		}
		return nil
	}
	_, err = s.StartRun(context.Background(), StartRunCommand{DefinitionID: d.ID, RequestID: "raced"})
	if !errors.Is(err, status_error.IMWorkflowDeliveryNotAllowed) || checks != 2 {
		t.Fatalf("提交事务未复核变化中的权限: %v, %d", err, checks)
	}
	runs, err = s.runs.ListRuns(context.Background(), d.ID, 0, "", 50)
	if err != nil || len(runs) != 0 {
		t.Fatalf("并发校验失败留下运行: %+v %v", runs, err)
	}
}

func TestParentAdmissionChecksChildIMDependenciesBeforeItsFirstNode(t *testing.T) {
	s, _ := workflowTestService(t)
	childSource := `export const meta={sdk:'ally.workflow/1',title:'子发送流程',description:'发送',phases:[{id:'delivery-phase',title:'发送',detail:'发送',canExit:true}],actions:[{id:'send',label:'发送',description:'发送微信',risk:'external',type:'builtin',handler:'send',connectorIds:['b17d96aa55314c33ab46c172e8cab012']}]};export const actions={send:async(ctx,input)=>await ctx.callTool({callId:'send',connectorId:'b17d96aa55314c33ab46c172e8cab012',toolName:'im_delivery',input:{action:'deliveries.create',targetId:'target',content:'正文'}})};return await action('send',{id:'deliver',label:'发送微信',phase:'delivery-phase'});`
	child := savedTestWorkflow(t, s, childSource)
	parentSource := strings.Replace(`export const meta={sdk:'ally.workflow/1',title:'父流程',description:'先校验子发送依赖',phases:[{id:'parent-phase',title:'运行',detail:'调用子流程',canExit:true}]};await askUser('输入',{id:'first',label:'业务步骤',phase:'parent-phase'});return await workflow('CHILD',{id:'child-call',label:'子流程发送',phase:'parent-phase'});`, "CHILD", child.ID, 1)
	parent := savedTestWorkflow(t, s, parentSource)
	s.imValidate = func(context.Context, string) error { return status_error.IMTargetUnavailable }
	_, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: parent.ID, RequestID: "start"})
	if !errors.Is(err, status_error.IMTargetUnavailable) {
		t.Fatalf("未在父流程接纳前发现子发送依赖失效: %v", err)
	}
	runs, err := s.runs.ListRuns(context.Background(), parent.ID, 0, "", 50)
	if err != nil || len(runs) != 0 {
		t.Fatalf("父流程开始后才校验: %+v %v", runs, err)
	}
}
