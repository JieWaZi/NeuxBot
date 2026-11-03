export const meta = {
  sdk: 'ally.workflow',
  title: '虚构候选评估与多步骤验收',
  description: '三个阶段各含三个步骤：生成候选、人工选择与伙伴分析、核验与可控重试。仅使用虚构数据。',
  inputSchema: {type:'object',properties:{
    topic:{type:'string',title:'评估主题',description:'用于生成虚构候选的主题。',default:'演示门店体验优化'},
    simulateFailure:{type:'boolean',title:'模拟首次失败',description:'在独立验收目录首次失败，原版本重试后通过。',default:true},
    demoKey:{type:'string',title:'验收批次',description:'同一批次共享一次失败标记；新验收请更换。',default:'multistep-example'}
  },required:['topic','simulateFailure','demoKey']},
  phases:[
    {id:'prepare',title:'准备候选',detail:'整理输入、生成虚构候选并检查完整性',next:['evaluate']},
    {id:'evaluate',title:'选择与评估',detail:'用户选择候选，整理材料后交给已有伙伴分析',next:['verify']},
    {id:'verify',title:'核验与归档',detail:'核验分析内容，演示一次失败后重试并汇总结果',canExit:true}
  ],
  actions:[
    {id:'candidates',type:'script',scriptId:'candidates',label:'生成虚构候选',description:'根据主题构建三个仅供演示的候选。',risk:'read'},
    {id:'checkCandidates',type:'script',scriptId:'checkCandidates',label:'检查候选完整性',description:'检查候选标识与展示名称是否完整。',risk:'read'},
    {id:'verifyAnalysis',type:'script',scriptId:'verifyAnalysis',label:'检查分析结果',description:'确认伙伴已经返回包含所选候选的实际分析。',risk:'read'},
    {id:'retryGate',type:'script',scriptId:'retryGate',label:'执行可控验收',description:'仅在专属临时目录记录一次失败标记，下一次验收通过。',risk:'write'}
  ]
};
export const scripts = {
  candidates:{language:'python',protocol:'json',code:String.raw`import json,sys
x=json.load(sys.stdin)
items=[{"value":"demo-A","label":"方案甲：优化引导","detail":"虚构门店引导体验，预算10点"},{"value":"demo-B","label":"方案乙：缩短等待","detail":"虚构排队体验，预算20点"},{"value":"demo-C","label":"方案丙：提升反馈","detail":"虚构意见收集体验，预算15点"}]
json.dump({"topic":x["topic"],"items":items},sys.stdout,ensure_ascii=False)`},
  checkCandidates:{language:'python',protocol:'json',code:String.raw`import json,sys
x=json.load(sys.stdin)
json.dump({"valid":len(x["items"])==3 and all(v["value"] and v["label"] for v in x["items"]),"count":len(x["items"])},sys.stdout)`},
  verifyAnalysis:{language:'python',protocol:'json',code:String.raw`import json,sys
x=json.load(sys.stdin)
json.dump({"valid":isinstance(x["analysis"],str) and len(x["analysis"].strip())>10 and x["selected"] in x["analysis"],"selected":x["selected"],"length":len(x["analysis"])},sys.stdout)`},
  retryGate:{language:'python',protocol:'json',code:String.raw`import json,sys,pathlib,re,tempfile
x=json.load(sys.stdin)
key=re.sub(r'[^a-zA-Z0-9_-]','_',x["demoKey"])[:80]
root=pathlib.Path(tempfile.gettempdir())/'workflow-multistep-acceptance'
root.mkdir(parents=True,exist_ok=True)
marker=root/(key+'.once-failed')
first=bool(x["simulateFailure"]) and not marker.exists()
if first:
    marker.write_text('fictional acceptance marker only\n',encoding='utf-8')
json.dump({"valid":not first,"simulatedFirstFailure":first,"demoKey":key},sys.stdout)`}
};
const normalized=await functionTask({id:'normalize',label:'整理评估主题',description:'整理本次虚构演示的主题与批次。',phase:'prepare',language:'javascript',executionMode:'pure',input:args,code:'function main(input) { return {topic:input.topic.trim(),demoKey:input.demoKey,simulateFailure:input.simulateFailure}; }'});
const listed=await action('candidates',{id:'list',label:'生成虚构候选',phase:'prepare',input:{topic:normalized.topic}});
const checked=await action('checkCandidates',{id:'check-list',label:'检查候选完整性',phase:'prepare',input:listed.data,successCondition:{path:'/valid',equals:true}});
const answer=await askUser('请从脚本本次生成的三个虚构方案中选择一个，随后交给 示例伙伴 分析。',{id:'choose',label:'选择评估方案',description:'选择一个虚构方案作为本次分析对象。',phase:'evaluate',kind:'select',choices:listed.data.items,allowSkip:false});
const material=await functionTask({id:'material',label:'整理评估材料',description:'将选项与主题整理为伙伴可直接阅读的材料。',phase:'evaluate',language:'javascript',executionMode:'pure',input:{topic:normalized.topic,selected:answer.value,items:listed.data.items},code:'function main(input) { return {topic:input.topic,selected:input.selected,candidate:input.items.filter(x=>x.value===input.selected)[0]}; }'});
const analysis=await worker('你正在参与安全验收，只分析以下虚构材料。不要联网，不读任何本地文件，不使用外部工具，不执行真实业务操作。请用中文直接给出100至200字分析，包含方案价值、风险与一条建议，正文必须逐字包含候选标识 '+answer.value+'。材料：'+JSON.stringify(material),{id:'analyze',label:'伙伴分析方案',description:'由已有伙伴 示例伙伴 对所选虚构方案给出价值、风险和建议。',phase:'evaluate',resolve:{kind:'waker',wakerId:'waker-example'}});
const verification=await action('verifyAnalysis',{id:'check-analysis',label:'检查分析结果',phase:'verify',input:{selected:answer.value,analysis},successCondition:{path:'/valid',equals:true}});
const gate=await action('retryGate',{id:'acceptance-gate',label:'执行可控验收',phase:'verify',input:{simulateFailure:normalized.simulateFailure,demoKey:normalized.demoKey},successCondition:{path:'/valid',equals:true}});
const report=await functionTask({id:'summary',label:'汇总验收报告',description:'汇总选择、伙伴分析与核验结果。',phase:'verify',language:'javascript',executionMode:'pure',input:{topic:normalized.topic,selected:answer.value,analysis,verification:verification.data,gate:gate.data},code:'function main(input) { return {reportMarkdown:"# 虚构候选评估报告\\n\\n主题："+input.topic+"\\n\\n选择："+input.selected+"\\n\\n"+input.analysis+"\\n\\n内容检查与可控验收均已通过。",selected:input.selected,analysis:input.analysis,verified:input.verification.valid,acceptance:input.gate.valid}; }'});
return report;
