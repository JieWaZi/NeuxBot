package dsl

import (
	"context"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

const imDependencySource = `export const meta={sdk:'ally.workflow/1',title:'微信依赖',description:'发送',phases:[{id:'delivery-phase',title:'发送',detail:'交付',canExit:true}],actions:[{id:'send',label:'发送微信',description:'交付结果',risk:'external',type:'builtin',handler:'send',connectorIds:['b17d96aa55314c33ab46c172e8cab012']}]}; export const actions={send:async(ctx,input)=>{const target='chat'; return await ctx.callTool({callId:'send',connectorId:'b17d96aa55314c33ab46c172e8cab012',toolName:'im_delivery',input:{action:'deliveries.create',targetId:target,content:input.content}})}};const operation='send';return await action(operation,{id:'deliver',label:'发送结果',phase:'delivery-phase',input:args});`

func TestIMDependenciesUseCompiledCallsAndRejectUndeclaredDynamicTargets(t *testing.T) {
	compiled, diagnostics, err := Compile(imDependencySource)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	dependencies, err := IMTargetDependencies(compiled.Manifest, compiled.Bundle)
	if err != nil || len(dependencies) != 1 || dependencies[0].TargetID != "chat" || dependencies[0].NodeID != "deliver" {
		t.Fatalf("未识别常量目标及动作别名: %+v %v", dependencies, err)
	}
	dynamic := strings.Replace(imDependencySource, "targetId:target", "targetId:input.targetId", 1)
	if _, diagnostics, err := Compile(dynamic); err == nil || len(diagnostics) == 0 {
		t.Fatal("动态目标未声明依赖也通过编译")
	}
	declared := strings.Replace(dynamic, "handler:'send',", "handler:'send',imTargetIds:['chat','other'],", 1)
	compiled, diagnostics, err = Compile(declared)
	if err != nil {
		t.Fatalf("动态目标显式范围不可编译: %v %+v", err, diagnostics)
	}
	dependencies, err = IMTargetDependencies(compiled.Manifest, compiled.Bundle)
	if err != nil || len(dependencies) != 2 {
		t.Fatalf("动态目标未形成依赖: %+v %v", dependencies, err)
	}
	lookups := strings.Replace(imDependencySource, "action:'deliveries.create',targetId:target,content:input.content", "action:'targets.list'", 1)
	compiled, _, err = Compile(lookups)
	if err != nil {
		t.Fatal(err)
	}
	dependencies, err = IMTargetDependencies(compiled.Manifest, compiled.Bundle)
	if err != nil || len(dependencies) != 0 {
		t.Fatalf("只查询目录被当作投递依赖: %+v %v", dependencies, err)
	}
}

func TestIMDependenciesPreserveLexicalBindingsAndTaskCaptures(t *testing.T) {
	for _, body := range []string{
		"const operation='send';{const operation='unused';}return await action(operation,{id:'deliver',label:'发送结果',phase:'delivery-phase',input:args});",
		"const operation='send';return await parallel([async()=>await action(operation,{id:'deliver',label:'发送结果',phase:'delivery-phase',input:args})],{id:'deliveries'});",
		"const operation=`send`;return await action(operation,{id:'deliver',label:'发送结果',phase:'delivery-phase',input:args});",
	} {
		source := imDependencySource[:strings.Index(imDependencySource, "const operation=")] + body
		compiled, diagnostics, err := Compile(source)
		if err != nil {
			t.Fatalf("词法绑定源码不可编译: %v %+v", err, diagnostics)
		}
		dependencies, err := IMTargetDependencies(compiled.Manifest, compiled.Bundle)
		if err != nil || len(dependencies) != 1 || dependencies[0].TargetID != "chat" || dependencies[0].ActionID != "send" {
			t.Fatalf("局部同名变量或异步捕获改变实际依赖: %+v %v", dependencies, err)
		}
	}
}

func TestChildDependenciesMatchRealLexicalInvocation(t *testing.T) {
	for _, call := range []string{"workflow", "startWorkflow"} {
		source := `export const meta={sdk:'ally.workflow/1',title:'父流程',description:'调用子流程',phases:[{id:'phase',title:'调用',detail:'调用子流程',canExit:true}]};const child='actual';{const child='unrelated';}return `
		if call == "workflow" {
			source += "await "
		}
		mode := ""
		if call == "startWorkflow" {
			mode = ",mode:'joined'"
		}
		source += call + `(child,{id:'child-call',label:'子流程',phase:'phase'` + mode + `});`
		compiled, diagnostics, err := Compile(source)
		if err != nil {
			t.Fatalf("子流程源码不可编译: %v %+v", err, diagnostics)
		}
		dependencies := ChildWorkflowDependencies(compiled.Manifest, compiled.Bundle)
		boundary := dependencyTestBoundary(t, compiled.Bundle, NewMachine(compiled.Bundle, nil, nil, 1))
		if len(dependencies) != 1 || dependencies[0].DefinitionID != boundary.Arguments[0].Data || dependencies[0].DefinitionID != "actual" {
			t.Fatalf("静态依赖与实际调用不同: %+v %+v", dependencies, boundary)
		}
	}
	source := `export const meta={sdk:'ally.workflow/1',title:'父流程',description:'异步调用子流程',phases:[{id:'phase',title:'调用',detail:'调用子流程',canExit:true}]};const child='actual';{const child='unrelated';}return await parallel([async()=>await workflow(child,{id:'child-call',label:'子流程',phase:'phase'})],{id:'children'});`
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("异步子流程源码不可编译: %v %+v", err, diagnostics)
	}
	group := dependencyTestBoundary(t, compiled.Bundle, NewMachine(compiled.Bundle, nil, nil, 1))
	tasks, err := ArrayValues(group.Arguments[0])
	if err != nil || len(tasks) != 1 {
		t.Fatalf("无法读取真实任务: %+v %v", group, err)
	}
	frame, err := NewTaskMachine(compiled.Bundle, tasks[0], nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	boundary := dependencyTestBoundary(t, compiled.Bundle, frame)
	dependencies := ChildWorkflowDependencies(compiled.Manifest, compiled.Bundle)
	if len(dependencies) != 1 || dependencies[0].DefinitionID != boundary.Arguments[0].Data || dependencies[0].DefinitionID != "actual" {
		t.Fatalf("异步捕获依赖与实际子调用不同: %+v %+v", dependencies, boundary)
	}
}

func TestIMObjectMutationRequiresExplicitDynamicScope(t *testing.T) {
	for _, mutation := range []string{
		"request.targetId=input.targetId;",
		"({targetId:request.targetId}=input);",
		"if(input.change){request.targetId=input.targetId;}",
		"for(let index=0;index<2;index++){request.targetId=input.targetId;}",
	} {
		source := strings.Replace(imDependencySource, "const target='chat';", "const request={action:'deliveries.create',targetId:'chat',content:'正文'};"+mutation, 1)
		source = strings.Replace(source, "input:{action:'deliveries.create',targetId:target,content:input.content}", "input:request", 1)
		if _, _, err := Compile(source); err == nil {
			t.Fatalf("const 对象修改后仍冒充固定目标: %s", mutation)
		}
		source = strings.Replace(source, "handler:'send',", "handler:'send',imTargetIds:['dynamic'],", 1)
		compiled, diagnostics, err := Compile(source)
		if err != nil {
			t.Fatalf("明确声明的动态目标不可编译: %v %+v", err, diagnostics)
		}
		frame, err := NewTaskMachine(compiled.Bundle, Value{Kind: constants.WorkflowSlotFunction, ProgramID: compiled.Bundle.ActionPrograms["send"]}, []Value{JSONValue(nil), JSONValue(map[string]any{"targetId": "dynamic", "change": true})}, 1)
		if err != nil {
			t.Fatal(err)
		}
		boundary := dependencyTestBoundary(t, compiled.Bundle, frame)
		input := boundary.Arguments[0].Data.(map[string]any)["input"].(map[string]any)
		dependencies, err := IMTargetDependencies(compiled.Manifest, compiled.Bundle)
		if err != nil || len(dependencies) != 1 || dependencies[0].TargetID != input["targetId"] || dependencies[0].TargetID != "dynamic" {
			t.Fatalf("动态依赖与真实调用不符: %+v %+v %v", dependencies, input, err)
		}
	}
}

func TestIMMutationInsideLoopCannotHideLaterDelivery(t *testing.T) {
	source := strings.Replace(imDependencySource, "const target='chat'; return await ctx.callTool", "const request={action:'targets.list'};for(let index=0;index<2;index++){await ctx.callTool", 1)
	source = strings.Replace(source, "input:{action:'deliveries.create',targetId:target,content:input.content}})}};", "input:request});request.action='deliveries.create';request.targetId=input.targetId;request.content='正文';}return null;}};", 1)
	if _, _, err := Compile(source); err == nil {
		t.Fatal("后续循环从查询变成投递，未声明动态范围仍编译成功")
	}
	declared := strings.Replace(source, "handler:'send',", "handler:'send',imTargetIds:['dynamic'],", 1)
	if _, diagnostics, err := Compile(declared); err != nil {
		t.Fatalf("明确声明后循环投递不可编译: %v %+v", err, diagnostics)
	}
}

func TestIMCopiedAndShadowedObjectMutationPreservesOriginalTarget(t *testing.T) {
	for _, mutation := range []string{
		"const alias=request;alias.targetId=input.targetId;",
		"{const request={targetId:'other'};request.targetId=input.targetId;}",
	} {
		source := strings.Replace(imDependencySource, "const target='chat';", "const request={action:'deliveries.create',targetId:'chat',content:'正文'};"+mutation, 1)
		source = strings.Replace(source, "input:{action:'deliveries.create',targetId:target,content:input.content}", "input:request", 1)
		compiled, diagnostics, err := Compile(source)
		if err != nil {
			t.Fatalf("独立副本或同名局部修改影响原对象识别: %v %+v", err, diagnostics)
		}
		frame, err := NewTaskMachine(compiled.Bundle, Value{Kind: constants.WorkflowSlotFunction, ProgramID: compiled.Bundle.ActionPrograms["send"]}, []Value{JSONValue(nil), JSONValue(map[string]any{"targetId": "dynamic"})}, 1)
		if err != nil {
			t.Fatal(err)
		}
		boundary := dependencyTestBoundary(t, compiled.Bundle, frame)
		input := boundary.Arguments[0].Data.(map[string]any)["input"].(map[string]any)
		dependencies, err := IMTargetDependencies(compiled.Manifest, compiled.Bundle)
		if err != nil || len(dependencies) != 1 || dependencies[0].TargetID != input["targetId"] || dependencies[0].TargetID != "chat" {
			t.Fatalf("副本修改传播到原始投递或其依赖: %+v %+v %v", dependencies, input, err)
		}
	}
}

func TestResourceIdentityValidationRejectsDynamicLexicalShadows(t *testing.T) {
	for _, body := range []string{
		"const child='actual';{const child=args.child;return await workflow(child,{id:'child-call',label:'子流程',phase:'delivery-phase'});}",
		"const child='actual';return await pipeline(args.items,[async(child)=>await workflow(child,{id:'child-call',label:'子流程',phase:'delivery-phase'})],{id:'children'});",
		"const operation='send';{const operation=args.operation;return await action(operation,{id:'deliver',label:'发送',phase:'delivery-phase'});}",
		"const operation='send';return await pipeline(args.items,[async(operation)=>await action(operation,{id:'deliver',label:'发送',phase:'delivery-phase'})],{id:'deliveries'});",
		"const revision='fixed';{const revision=args.revision;return await workflow('actual',{id:'child-call',label:'子流程',phase:'delivery-phase',revisionId:revision});}",
	} {
		source := imDependencySource[:strings.Index(imDependencySource, "const operation=")] + body
		_, diagnostics, err := Compile(source)
		if err == nil {
			t.Fatalf("实际动态资源被外层同名常量误接纳: %s", body)
		}
		expected := "CHILD_ID_STATIC_REQUIRED"
		if strings.Contains(body, "action(") {
			expected = "ACTION_NOT_DECLARED"
		}
		if strings.Contains(body, "revisionId:") {
			expected = "STATIC_OPTION_REQUIRED"
		}
		found := false
		for _, diagnostic := range diagnostics {
			found = found || diagnostic.Code == expected
		}
		if !found {
			t.Fatalf("资源错误未保留稳定诊断 %s: %+v", expected, diagnostics)
		}
	}
}

func TestChildLexicalIdentityAndPinnedRevisionMatchManifestAndMachine(t *testing.T) {
	for _, body := range []string{
		"const child='unused';const revision='unused-revision';{const child='actual';const revision='fixed';return await workflow(child,{id:'child-call',label:'子流程',phase:'phase',revisionId:revision});}",
		"{const child='actual';const revision='fixed';return await workflow(child,{id:'child-call',label:'子流程',phase:'phase',revisionId:revision});}",
	} {
		source := `export const meta={sdk:'ally.workflow/1',title:'父流程',description:'固定子版本',phases:[{id:'phase',title:'调用',detail:'调用子流程',canExit:true}]};` + body
		compiled, diagnostics, err := Compile(source)
		if err != nil {
			t.Fatalf("真正固定的局部资源被拒绝: %v %+v", err, diagnostics)
		}
		boundary := dependencyTestBoundary(t, compiled.Bundle, NewMachine(compiled.Bundle, nil, nil, 1))
		dependencies := ChildWorkflowDependencies(compiled.Manifest, compiled.Bundle)
		actualRevision := boundary.Arguments[1].Data.(map[string]any)["revisionId"]
		if len(dependencies) != 1 || dependencies[0].DefinitionID != boundary.Arguments[0].Data || dependencies[0].DefinitionID != "actual" || dependencies[0].RevisionID != actualRevision || dependencies[0].RevisionID != "fixed" || compiled.Manifest.Nodes[0].Options["revisionId"] != actualRevision {
			t.Fatalf("子身份、冻结版本或元数据与实际调用不同: %+v %+v %+v", dependencies, boundary, compiled.Manifest.Nodes)
		}
	}
}

func TestActionLexicalIdentitySelectsActualDependencyAndHandler(t *testing.T) {
	source := `export const meta={sdk:'ally.workflow/1',title:'发送',description:'实际动作',phases:[{id:'phase',title:'发送',detail:'发送消息',canExit:true}],actions:[{id:'send',label:'原发送',description:'原目标',risk:'external',type:'builtin',handler:'send',connectorIds:['b17d96aa55314c33ab46c172e8cab012']},{id:'other',label:'实际发送',description:'实际目标',risk:'external',type:'builtin',handler:'other',connectorIds:['b17d96aa55314c33ab46c172e8cab012']}]};export const actions={send:async(ctx,input)=>await ctx.callTool({callId:'send',connectorId:'b17d96aa55314c33ab46c172e8cab012',toolName:'im_delivery',input:{action:'deliveries.create',targetId:'original',content:'正文'}}),other:async(ctx,input)=>await ctx.callTool({callId:'send',connectorId:'b17d96aa55314c33ab46c172e8cab012',toolName:'im_delivery',input:{action:'deliveries.create',targetId:'actual',content:'正文'}})};const operation='send';{const operation='other';return await action(operation,{id:'deliver',label:'发送',phase:'phase'});}`
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("合法局部动作不可编译: %v %+v", err, diagnostics)
	}
	boundary := dependencyTestBoundary(t, compiled.Bundle, NewMachine(compiled.Bundle, nil, nil, 1))
	dependencies, err := IMTargetDependencies(compiled.Manifest, compiled.Bundle)
	if err != nil || len(dependencies) != 1 || dependencies[0].ActionID != boundary.Arguments[0].Data || dependencies[0].TargetID != "actual" {
		t.Fatalf("动作依赖不同于真正派发: %+v %+v %v", dependencies, boundary, err)
	}
	details, err := InspectActionSources(source, compiled.Manifest)
	if err != nil || details["deliver"].ActionID != "other" || !strings.Contains(details["deliver"].HandlerSource, "targetId:'actual'") {
		t.Fatalf("操作详情展示了外层同名动作实现: %+v %v", details, err)
	}
}

func dependencyTestBoundary(t *testing.T, bundle ProgramBundle, frame MachineState) EffectBoundary {
	t.Helper()
	for steps := 0; steps < 100 && !frame.Done; steps++ {
		boundary, err := Advance(context.Background(), bundle, &frame, 10)
		if err != nil {
			t.Fatal(err)
		}
		if boundary != nil {
			return *boundary
		}
	}
	t.Fatalf("实际执行未到达预期边界: %+v", frame)
	return EffectBoundary{}
}
