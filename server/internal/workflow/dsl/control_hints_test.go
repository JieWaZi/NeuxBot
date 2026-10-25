package dsl

import (
	"encoding/json"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func TestControlHintsPreserveExclusiveBranchesGroupsAndSource(t *testing.T) {
	source := `export const meta={sdk:'ally.workflow/1',title:'画布拓扑',description:'保留真实控制关系',phases:[{id:'work',title:'工作',detail:'任务',canExit:true}]};
phase('work');
if(args.审核通过){
 await parallel([
  async()=>{await askUser('复核',{id:'review',label:'复核',phase:'work'});return true;},
  async()=>await pipeline(args.items,[
   async(item)=>await askUser('读取',{id:'read',label:'读取',phase:'work'}),
   async(item)=>await askUser('整理',{id:'format',label:'整理',phase:'work'})
  ],{id:'items'})
 ],{id:'tasks'});
}else{return false;}
await askUser('交付',{id:'deliver',label:'交付',phase:'work'});return true;`
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("compile: %v %+v", err, diagnostics)
	}
	hints := compiled.Manifest.ControlHints
	if len(hints) != 3 || hints[0].Kind != constants.WorkflowControlKindDecision || hints[0].Condition != "args.审核通过" {
		t.Fatalf("lost decision or source: %+v", hints)
	}
	decision := hints[0]
	if len(decision.Branches) != 2 || decision.Branches[0].Label != constants.WorkflowControlPathTrue || decision.Branches[1].Label != constants.WorkflowControlPathFalse || decision.Branches[1].Hints[0].Kind != constants.WorkflowControlKindReturn {
		t.Fatalf("lost exclusive/terminal paths: %+v", decision)
	}
	parallel := decision.Branches[0].Hints[0]
	if parallel.Kind != constants.WorkflowControlKindParallel || parallel.GroupID != "tasks" || len(parallel.Branches) != 2 || len(parallel.Branches[0].Hints) != 2 || parallel.Branches[0].Hints[1].Kind != constants.WorkflowControlKindTaskReturn {
		t.Fatalf("lost task completion scope: %+v", parallel)
	}
	pipeline := parallel.Branches[1].Hints[0]
	if pipeline.Kind != constants.WorkflowControlKindPipeline || pipeline.GroupID != "items" || pipeline.Condition != "args.items" || len(pipeline.Branches) != 2 {
		t.Fatalf("lost pipeline stages: %+v", pipeline)
	}
	assertControlNodeCoverage(t, compiled.Manifest)
	encoded, _ := json.Marshal(compiled.Manifest)
	var restored Manifest
	if err := json.Unmarshal(encoded, &restored); err != nil || len(restored.ControlHints) != 3 {
		t.Fatal("control projection must survive frozen revision serialization", err)
	}
	for _, hint := range hints {
		if hint.Range.Start.Line < 1 || source[hint.Range.Start.Offset:hint.Range.End.Offset] == "" {
			t.Fatal("control gate lacks original UTF-8 source range")
		}
	}
}

func TestControlHintsTrackShortCircuitLoopAndDynamicTaskTemplates(t *testing.T) {
	source := `export const meta={sdk:'ally.workflow/1',title:'短路与循环',description:'不可伪造运行分支',phases:[{id:'work',title:'工作',detail:'任务',canExit:true}]};
const callbacks=[async()=>args.reference];
await parallel(callbacks,{id:'referenced_tasks'});
args.ready && await askUser('条件任务',{id:'conditional',label:'条件任务',phase:'work'});
for(let i=0;i<args.count;i++){
 if(args.skip){continue;}
 if(args.stop){break;}
 await askUser('循环任务',{id:'loop_task',label:'循环任务',phase:'work'});
}

await parallel(args.items.map(item=>{return async()=>await askUser(item,{id:'mapped',label:'逐项任务',phase:'work'});}),{id:'mapped_tasks'});
return true;`
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("compile: %v %+v", err, diagnostics)
	}
	hints := compiled.Manifest.ControlHints
	if hints[1].Kind != constants.WorkflowControlKindDecision || hints[1].Condition != "args.ready" || len(hints[1].Branches[1].Hints) != 0 {
		t.Fatalf("short-circuit bypass lost: %+v", hints)
	}
	loop := hints[2]
	if loop.Kind != constants.WorkflowControlKindLoop || loop.Condition != "i<args.count" || loop.Branches[0].Label != constants.WorkflowControlPathTest || loop.Branches[1].Hints[0].Branches[0].Hints[0].Kind != constants.WorkflowControlKindContinue || loop.Branches[1].Hints[1].Branches[0].Hints[0].Kind != constants.WorkflowControlKindBreak {
		t.Fatalf("loop control lost: %+v", loop)
	}
	group := hints[3]
	if len(group.Branches) != 1 || group.Branches[0].Label != constants.WorkflowControlPathItems || group.Condition != "args.items" {
		t.Fatalf("dynamic inputs expanded as fictitious instances: %+v", group)
	}
	assertControlNodeCoverage(t, compiled.Manifest)
}

func TestControlHintsKeepForUpdateSeparateFromContinue(t *testing.T) {
	source := `export const meta={sdk:'ally.workflow/1',title:'循环更新',description:'continue 仍执行更新',phases:[{id:'work',title:'工作',detail:'循环',canExit:true}]};
for(let i=0;i<args.count;await askUser('更新',{id:'update',label:'更新',phase:'work'})){
 i++;
 if(args.skip){continue;}
 await askUser('处理',{id:'body',label:'处理',phase:'work'});
}`
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("compile: %v %+v", err, diagnostics)
	}
	loop := compiled.Manifest.ControlHints[0]
	if len(loop.Branches) != 3 || loop.Branches[2].Label != constants.WorkflowControlPathUpdate || loop.Branches[2].Hints[0].NodeID != "update" {
		t.Fatalf("for update must remain a separate continuation target: %+v", loop)
	}
	assertControlNodeCoverage(t, compiled.Manifest)
}

func assertControlNodeCoverage(t *testing.T, manifest Manifest) {
	t.Helper()
	counts := map[string]int{}
	var visit func([]ControlHint)
	visit = func(hints []ControlHint) {
		for _, hint := range hints {
			if hint.NodeID != "" {
				counts[hint.NodeID]++
			}
			for _, branch := range hint.Branches {
				visit(branch.Hints)
			}
		}
	}
	visit(manifest.ControlHints)
	for _, node := range manifest.Nodes {
		if counts[node.ID] != 1 {
			t.Fatalf("node %s appears %d times in control projection", node.ID, counts[node.ID])
		}
	}
}
