package dsl

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func testMachineSource(body string) string {
	return `export const meta={sdk:'ally.workflow/1',title:'行为测试',description:'测试真实 Go 程序',phases:[{id:'main',title:'主流程',detail:'测试',canExit:true}]};` + body
}

func runTestMachine(t *testing.T, source string, args any, handler func(EffectBoundary) Value) MachineState {
	t.Helper()
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("compile: %v %+v", err, diagnostics)
	}
	state := NewMachine(compiled.Bundle, args, map[string]any{}, 1791100800000)
	for steps := 0; !state.Done && steps < 100000; steps++ {
		boundary, err := Advance(context.Background(), compiled.Bundle, &state, 1)
		if err != nil {
			t.Fatal(err)
		}
		// 每次推进都序列化并恢复，外部效果边界也恢复后再提交回执。
		data, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		var restored MachineState
		if json.Unmarshal(data, &restored) != nil {
			t.Fatal("checkpoint cannot be restored")
		}
		state = restored
		if boundary != nil {
			value := JSONValue(nil)
			if handler != nil {
				value = handler(*boundary)
			}
			if err := ApplyEffect(compiled.Bundle, &state, *boundary, value, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !state.Done {
		t.Fatal("program did not reach a terminal state")
	}
	return state
}

func TestF0ActuallyExecutesInGoAcrossEveryCheckpoint(t *testing.T) {
	data, err := os.ReadFile("testdata/f0.workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	phases := []string{}
	state := runTestMachine(t, string(data), map[string]any{"text": "NeuxBot", "repeat": 2, "uppercase": true, "suffix": "!"}, func(boundary EffectBoundary) Value {
		if boundary.Primitive != "phase" {
			t.Fatalf("unexpected host call %+v", boundary)
		}
		phases = append(phases, boundary.Arguments[0].Data.(string))
		return JSONValue(nil)
	})
	if state.Failure != nil {
		t.Fatalf("program failed: %+v", state.Failure)
	}
	expected := JSONValue(map[string]any{"message": "NEUXBOT NEUXBOT!", "length": 16, "repeat": 2})
	if !reflect.DeepEqual(state.Result, expected) || !reflect.DeepEqual(phases, []string{"prepare", "compose"}) {
		t.Fatalf("result=%+v phases=%v", state.Result, phases)
	}
}

func TestPureCallbackThrowPreservesStructuredReasonAcrossCatch(t *testing.T) {
	state := runTestMachine(t, testMachineSource(`try {[1].map(value=>{throw {code:'ITEM_REJECTED',item:value};});} catch(failure){return failure;} return null;`), nil, nil)
	if state.Failure != nil || !reflect.DeepEqual(state.Result, JSONValue(map[string]any{"code": "ITEM_REJECTED", "item": 1})) {
		t.Fatalf("callback throw became a generic error: result=%+v failure=%+v", state.Result, state.Failure)
	}
}

func TestJSONObjectsAndArraysCanBeCheckedAgainstNull(t *testing.T) {
	state := runTestMachine(t, testMachineSource(`const values=[{ok:true},[],null];return {present:values.filter(value=>value!==null),missing:values.some(value=>value===null),strict:({})===false,loose:({})==null};`), nil, nil)
	expected := JSONValue(map[string]any{"present": []any{map[string]any{"ok": true}, []any{}}, "missing": true, "strict": false, "loose": false})
	if state.Failure != nil || !reflect.DeepEqual(state.Result, expected) {
		t.Fatalf("unambiguous object/null check failed: result=%+v failure=%+v", state.Result, state.Failure)
	}
}

func TestPureFunctionsCollectionsAndJSONValueMutation(t *testing.T) {
	state := runTestMachine(t, testMachineSource(`
const original={count:1,items:[1]};
let copied=original;copied.count=2;copied.items.push(2);
function increment(value){value.count+=1;return value;}
const changed=increment(original);
const list=[1,2,3,4].map(n=>n*2).filter(n=>n>2);
return {original,copied,changed,sum:list.reduce((total,n)=>total+n,0),first:list.find(n=>n>5),every:list.every(n=>n>0),some:list.some(n=>n>7)};
`), nil, nil)
	if state.Failure != nil {
		t.Fatalf("failure %+v", state.Failure)
	}
	expected := map[string]any{"original": map[string]any{"count": 1, "items": []any{1}}, "copied": map[string]any{"count": 2, "items": []any{1, 2}}, "changed": map[string]any{"count": 2, "items": []any{1}}, "sum": 18, "first": 6, "every": true, "some": true}
	if !reflect.DeepEqual(state.Result, JSONValue(expected)) {
		t.Fatalf("value semantics lost: %+v", state.Result)
	}
}

func TestBranchesLoopsDestructuringAndFinallyRecover(t *testing.T) {
	state := runTestMachine(t, testMachineSource(`
let total=0;for (const n of [1,2,3,4]){if(n===2){continue;}if(n===4){break;}total+=n;}
const {present, missing='fallback',...rest}={present:null,extra:'kept'};
let trace=[];
try {throw new Error('expected');} catch(error){trace.push(error.message);} finally {trace.push('finally');}
function result(){try{return 'before';}finally{trace.push('copied capture');}}
const returned=result();
return {total,present,missing,rest,trace,returned};
`), nil, nil)
	if state.Failure != nil {
		t.Fatalf("failure %+v", state.Failure)
	}
	expected := map[string]any{"total": 4, "present": nil, "missing": "fallback", "rest": map[string]any{"extra": "kept"}, "trace": []any{"expected", "finally"}, "returned": "before"}
	if !reflect.DeepEqual(state.Result, JSONValue(expected)) {
		t.Fatalf("control flow failed: %+v", state.Result)
	}
}

func TestShortCircuitDoesNotDispatchUnreachableHumanInput(t *testing.T) {
	calls := 0
	state := runTestMachine(t, testMachineSource(`
const a=false&&await askUser('must not run',{id:'a',label:'A',phase:'main'});
const b=true||await askUser('must not run',{id:'b',label:'B',phase:'main'});
const c='available'??await askUser('must not run',{id:'c',label:'C',phase:'main'});
const d=true?'chosen':await askUser('must not run',{id:'d',label:'D',phase:'main'});
return {a,b,c,d};
`), nil, func(boundary EffectBoundary) Value { calls++; return JSONValue(nil) })
	if state.Failure != nil || calls != 0 || !reflect.DeepEqual(state.Result, JSONValue(map[string]any{"a": false, "b": true, "c": "available", "d": "chosen"})) {
		t.Fatalf("unreachable effect dispatched: calls=%d state=%+v", calls, state)
	}
}

func TestLoopControlUnwindsFinallyAcrossEveryCheckpoint(t *testing.T) {
	calls := 0
	state := runTestMachine(t, testMachineSource(`
let trace=[];
try {
  for(let i=0;i<4;i++) {
    try {
      trace.push('try:'+i);
      try { if(i===0){continue;} break; }
      finally { trace.push('inner:'+i); }
    } finally {
      await askUser('继续',{id:'finallyInput',label:'完成清理',phase:'main'});
      trace.push('finally:'+i);
    }
  }
  throw new Error('outer catch remains active');
} catch(error) { trace.push(error.message); }
finally { trace.push('outer'); }
return trace;
`), nil, func(boundary EffectBoundary) Value {
		if boundary.Primitive != "askUser" {
			t.Fatalf("unexpected effect: %+v", boundary)
		}
		calls++
		return JSONValue(nil)
	})
	expected := JSONValue([]any{"try:0", "inner:0", "finally:0", "try:1", "inner:1", "finally:1", "outer catch remains active", "outer"})
	if state.Failure != nil || calls != 2 || !reflect.DeepEqual(state.Result, expected) || len(state.Handlers) != 0 {
		t.Fatalf("loop control skipped cleanup or leaked handler: calls=%d state=%+v", calls, state)
	}
}

func TestFinallyCanOverrideLoopControl(t *testing.T) {
	state := runTestMachine(t, testMachineSource(`
let trace=[];
for(let i=0;i<3;i++) { try { continue; } finally { trace.push(i); break; } }
function value(){for(let i=0;i<3;i++){try{break;}finally{return 'cleanup return';}}return 'wrong';}
return {trace,value:value()};
`), nil, nil)
	if state.Failure != nil || !reflect.DeepEqual(state.Result, JSONValue(map[string]any{"trace": []any{0}, "value": "cleanup return"})) {
		t.Fatalf("finally override lost: %+v", state)
	}
}

func TestUnicodeRegexAndFrozenDateAreNativeDeterministicValues(t *testing.T) {
	state := runTestMachine(t, testMachineSource(`const text='中文😀abc';return {length:text.length,slice:text.slice(1,3),index:text.indexOf('a'),clean:'周报 A!'.replace(/[^\w\u4e00-\u9fa5-]/g,''),stamp:new Date().toISOString().slice(0,10)};`), nil, nil)
	if state.Failure != nil {
		t.Fatalf("failure %+v", state.Failure)
	}
	expected := map[string]any{"length": 6, "slice": "文😀", "index": 3, "clean": "周报A", "stamp": "2026-10-04"}
	if !reflect.DeepEqual(state.Result, JSONValue(expected)) {
		t.Fatalf("native string/date mismatch %+v", state.Result)
	}
}

func TestUserJSONCannotForgeChildHandleOrModifyFlowContext(t *testing.T) {
	_, diagnostics, err := Compile(testMachineSource(`flowContext.runId='forged';return args;`))
	if err == nil || len(diagnostics) == 0 {
		t.Fatal("read-only context was mutated")
	}
	value := JSONValue(map[string]any{"kind": constants.WorkflowSlotChildHandle})
	if value.Kind != constants.WorkflowSlotJSON {
		t.Fatal("user data became an internal handle")
	}
}

func TestAwaitPreservesArrayObjectAndTemplateEvaluationOrder(t *testing.T) {
	for _, test := range []struct {
		// name 是被观察的表达式类型。
		name string
		// expression 含有暂停前必须发生的更新。
		expression string
	}{{"array", `[n++,await worker('pause:'+n,{id:'pause',phase:'main',label:'暂停',resolve:{kind:'waker',wakerId:'fixture'}})]`}, {"object", `({before:n++,wait:await worker('pause:'+n,{id:'pause',phase:'main',label:'暂停',resolve:{kind:'waker',wakerId:'fixture'}})})`}, {"template", "`${n++}:${await worker('pause:'+n,{id:'pause',phase:'main',label:'暂停',resolve:{kind:'waker',wakerId:'fixture'}})}`"}} {
		t.Run(test.name, func(t *testing.T) {
			state := runTestMachine(t, testMachineSource(`let n=0;const value=`+test.expression+`;return {n,value};`), nil, func(boundary EffectBoundary) Value {
				if boundary.Arguments[0].Data != "pause:1" {
					t.Fatalf("side effect happened after await: %+v", boundary.Arguments)
				}
				return JSONValue("receipt")
			})
			if state.Failure != nil {
				t.Fatalf("%+v", state.Failure)
			}
			value, _ := state.Result.JSON()
			data := value.(map[string]any)
			if data["n"] != float64(1) {
				t.Fatalf("evaluation order: %+v", data)
			}
		})
	}
}

func TestBuiltinHandlerHasIndependentContextAndCannotCaptureMainScope(t *testing.T) {
	source := testMachineSource(`export const actions={echo:async(ctx,input)=>{const result=await ctx.callTool({callId:'echo',connectorId:'fixture',toolName:'echo',input});ctx.log(result);return result}};return null;`)
	source = strings.Replace(source, "phases:", "actions:[{id:'echo-action',label:'echo',description:'echo input',risk:'read',type:'builtin',handler:'echo',connectorIds:['fixture']}],phases:", 1)
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics)
	}
	programID := compiled.Bundle.ActionPrograms["echo"]
	state, err := NewTaskMachine(compiled.Bundle, Value{Kind: constants.WorkflowSlotFunction, ProgramID: programID}, []Value{JSONValue(map[string]any{"flowContext": map[string]any{"runId": "run"}}), JSONValue("input")}, 1)
	if err != nil {
		t.Fatal(err)
	}
	calls := []string{}
	for !state.Done {
		boundary, err := Advance(context.Background(), compiled.Bundle, &state, 1)
		if err != nil {
			t.Fatal(err)
		}
		if boundary != nil {
			calls = append(calls, boundary.Primitive)
			if err = ApplyEffect(compiled.Bundle, &state, *boundary, JSONValue("actual"), nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !reflect.DeepEqual(calls, []string{"callTool", "log"}) || state.Result.Data != "actual" {
		t.Fatalf("%+v %+v", calls, state)
	}
	source = strings.Replace(source, "const result=await ctx.callTool", "const hidden=args;const result=await ctx.callTool", 1)
	if _, diagnostics, err = Compile(source); err == nil {
		t.Fatalf("handler captured main args: %+v", diagnostics)
	}
}
