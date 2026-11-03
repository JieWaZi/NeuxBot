package dsl

import (
	"strings"
	"testing"
)

func TestInspectActionSourcesPreservesDynamicCallsAndHandlerText(t *testing.T) {
	source := "// 中文😀：保持 UTF-8 位置\n" + `export const meta={sdk:'ally.workflow',title:'操作',description:'检查操作细节',phases:[{id:'read',title:'读取',detail:'获取数据',canExit:true}],actions:[
{id:'fetch',label:'获取评论',description:'分页读取',risk:'read',type:'builtin',handler:'fetchComments'},
{id:'script',label:'调用脚本',description:'外部脚本',risk:'read',type:'script',executable:'python3',workingDirectory:'workflow_scope'},
{id:'http',label:'请求接口',description:'HTTP',risk:'read',type:'http',allowedOrigins:['https://example.com'],methods:['GET']}]};
const operation='fetch';
const actions={fetchComments:async(ctx,input)=>{
  // 保留说明和嵌套字符串，不执行此函数。
  ctx.log('中文😀');
  return {comments:input.comments, text:'} const actions = {}'};
}};
const result=await action(operation,{id:'fetch-node',label:'获取评论',phase:'read',input:{comments:args.comments}});
await action('script',{id:'script-node',label:'调用脚本',phase:'read',args:['comments.py',JSON.stringify(result)]});
await action('http',{id:'http-node',label:'请求接口',phase:'read',url:args.url,method:'GET'});
return result;`
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatalf("compile: %v %+v", err, diagnostics)
	}
	details, err := InspectActionSources(source, compiled.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	fetch := details["fetch-node"]
	if fetch.ActionID != "fetch" || fetch.CallSource != "action(operation,{id:'fetch-node',label:'获取评论',phase:'read',input:{comments:args.comments}})" {
		t.Fatalf("wrong call or constant reference: %+v", fetch)
	}
	expectedHandler := source[strings.Index(source, "async(ctx,input)") : strings.Index(source, "}};\nconst result")+1]
	if fetch.HandlerSource != expectedHandler {
		t.Fatalf("handler source changed: %q", fetch.HandlerSource)
	}
	if script := details["script-node"]; script.HandlerSource != "" || !strings.Contains(script.CallSource, "JSON.stringify(result)") {
		t.Fatalf("external script body or dynamic argument lost: %+v", script)
	}
	if request := details["http-node"]; request.ActionID != "http" || !strings.Contains(request.CallSource, "url:args.url") {
		t.Fatalf("HTTP configuration lost: %+v", request)
	}
	compiled.Manifest.Nodes[0].Range.Start.Offset++
	if _, err := InspectActionSources(source, compiled.Manifest); err == nil {
		t.Fatal("mismatched source and manifest must not display another node")
	}
}
