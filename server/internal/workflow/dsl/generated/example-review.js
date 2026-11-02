// 教学示例：全部候选与证据均为虚构数据，不查询或修改真实业务。
// 保存前通过 authoring prepare 解析真实伙伴资源，并替换下方 waker-example；它不是可用资源 ID。
// 需要宿主配置的 Python 解释器；运行时由用户选择审阅范围。
export const meta = {
  sdk: 'ally.workflow',
  title: '候选审阅示例',
  description: '选择虚构候选，逐项审阅并独立复核，明确报告覆盖范围和失败项。',
  phases: [
    {id:'discover',title:'获取候选',detail:'列出可选择的虚构方案。',next:['choose']},
    {id:'choose',title:'选择范围',detail:'由你决定本轮审阅哪些方案。',next:['review']},
    {id:'review',title:'审阅与复核',detail:'逐项提出判断，再对照原始证据反证。',next:['summarize']},
    {id:'summarize',title:'汇总覆盖',detail:'列明已完成、失败和未选择的候选。',canExit:true}
  ],
  actions: [{id:'candidate-list',type:'script',scriptId:'candidates',label:'获取候选',risk:'read'}]
};

export const scripts = {
  candidates: {language:'python',protocol:'json',code:String.raw`import json, sys
request = json.load(sys.stdin)
if request.get("example") is not True:
    raise ValueError("example data must be explicitly requested")
json.dump({"items": [
    {"value": "a", "label": "示例方案甲", "text": "虚构记录：发布检查尚未完成。"},
    {"value": "b", "label": "示例方案乙", "text": "虚构记录：发布检查已完成。"},
    {"value": "c", "label": "示例方案丙", "text": "虚构记录：本轮暂不安排发布。"}
]}, sys.stdout, ensure_ascii=False)
`}
};

const WAKER = {kind:'waker',wakerId:'waker-example'};
const REVIEW = {type:'object',properties:{claim:{type:'string'},evidence:{type:'string'}},required:['claim','evidence']};
const VERIFICATION = {type:'object',properties:{complete:{type:'boolean'},supported:{type:'boolean'},reason:{type:'string'}},required:['complete','supported','reason']};

const listed = await action('candidate-list', {
  id:'list-candidates',label:'获取可审阅方案',description:'读取候选身份和原始证据。',phase:'discover',input:{example:true}
});
if (listed.data.items.length === 0) return {example:true,coverage:{discovered:0,selected:0,completed:0,failed:[],excluded:[]},items:[]};

const answer = await askUser('本轮需要审阅哪些示例方案？', {
  id:'choose-candidates',label:'选择审阅范围',phase:'choose',kind:'multiselect',
  choices:listed.data.items.map(item=>({value:item.value,label:item.label})),allowSkip:false
});
if (answer.status !== 'answered') throw {code:'SELECTION_REQUIRED'};
const selected = listed.data.items.filter(item=>answer.value.includes(item.value));

const results = await pipeline(selected, [
  async (previous,original,index) => await worker(JSON.stringify({
    instruction:'审阅候选的原始证据，提出一条可验证判断；evidence 引用原文。按宿主结果传输契约提交匹配 schema 的结果；已注入 MCP 时使用 workflow_results_submit。',
    candidate:original
  }), {id:'review-candidate',label:'审阅方案',description:'根据原始记录提出可验证判断。',phase:'review',resolve:WAKER,schema:REVIEW}),
  async (previous,original,index) => {
    const verification = await worker(JSON.stringify({
      instruction:'独立对照 candidate.text 反驳 review.claim，不沿用审阅结论。完成证据核对才 complete=true；主张被证伪时 supported=false 并说明理由。按宿主结果传输契约提交；已注入 MCP 时使用 workflow_results_submit。',
      candidate:original,review:previous
    }), {id:'verify-candidate',label:'复核原始证据',description:'尝试反驳审阅判断，保留真实结论。',phase:'review',resolve:WAKER,schema:VERIFICATION,successCondition:{path:'/complete',equals:true}});
    return {id:original.value,review:previous,verification};
  }
], {id:'review-items',failureMode:'collect-null'});

// 成功项始终是对象；null 在本例中只标记实际失败，不静默过滤覆盖缺口。
const items = selected.map((item,index)=>results[index]===null
  ? {id:item.value,status:'failed'}
  : {id:item.value,status:'completed',review:results[index].review,verification:results[index].verification});
const failed = items.filter(item=>item.status==='failed').map(item=>item.id);
const coverage = {
  discovered:listed.data.items.length,selected:selected.length,completed:selected.length-failed.length,failed,
  excluded:listed.data.items.filter(item=>!answer.value.includes(item.value)).map(item=>item.value)
};
log(JSON.stringify(coverage));
if (failed.length > 0) throw {code:'REVIEW_INCOMPLETE',coverage,items};
return {example:true,coverage,items};
