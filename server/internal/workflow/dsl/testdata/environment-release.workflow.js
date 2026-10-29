export const meta = {
  sdk:'ally.workflow/2',title:'多客户环境发布预审',
  description:'材料补全、逐环境分析与处置、返修和人工审批、真实本地验收与报告；通知只生成草稿。',
  inputSchema:{type:'object',required:['requirements','defects'],properties:{
    requirements:{type:'string'},defects:{type:'string'},environments:{type:'array',items:{type:'string'},default:[]},
    iterationName:{type:'string',default:''},releaseDate:{type:'string',default:''},riskThreshold:{type:'integer',minimum:1,default:1},
    localAcceptance:{type:'boolean',default:false}
  }},
  outputSchema:{type:'object',required:['decision','reportMarkdown','envResults','approvalRecords','stats'],properties:{decision:{type:'string',enum:['批准发布','有条件放行','不予发布']},reportMarkdown:{type:'string'},envResults:{type:'array'},approvalRecords:{type:'array'},stats:{type:'object'},notificationDraft:{type:['object','null']}}},
  phases:[
    {id:'check',title:'材料核查与信息补全',detail:'缺字段须写回结构化上下文',next:['analyse']},
    {id:'analyse',title:'逐环境并行预审分析',detail:'每个环境并行分析需求与缺陷，环境之间无屏障',next:['mitigate']},
    {id:'mitigate',title:'逐环境高风险项处置',detail:'建议、执行和验证是独立事实',next:['approve']},
    {id:'approve',title:'逐项确认与返修',detail:'未解决阻塞不能借空队列放行；默认否决',next:['mitigate','report']},
    {id:'report',title:'报告与通知草稿',detail:'权威结论取自实际审批和验证；不发送通知',canExit:true}
  ]
};
const WAKER={kind:'waker',wakerId:'fixture-waker'};
const CHECK_SCHEMA={type:'object',required:['iteration','releaseDate','severityConvention','priorityConvention','missingFields','summary'],properties:{iteration:{type:'string'},releaseDate:{type:'string'},severityConvention:{type:'string'},priorityConvention:{type:'string'},missingFields:{type:'array',items:{type:'string'}},summary:{type:'string'}}};
const COVERAGE_SCHEMA={type:'object',required:['total','coveredCount','highPriorityGaps','summary'],properties:{total:{type:'integer'},coveredCount:{type:'integer'},highPriorityGaps:{type:'array',items:{type:'object',required:['requirement','reason'],properties:{requirement:{type:'string'},reason:{type:'string'}}}},summary:{type:'string'}}};
const RISK_SCHEMA={type:'object',required:['total','highRiskItems','blocking','summary'],properties:{total:{type:'integer'},highRiskItems:{type:'array',items:{type:'object',required:['title','severity','impact','reason'],properties:{title:{type:'string'},severity:{type:'string',enum:['critical','high']},impact:{type:'string'},reason:{type:'string'}}}},blocking:{type:'boolean'},summary:{type:'string'}}};
const MITIGATION_SCHEMA={type:'object',required:['action','residualRisk','note'],properties:{action:{type:'string'},residualRisk:{type:'string',enum:['high','medium','low']},note:{type:'string'}}};
const EXECUTION_SCHEMA={type:'object',required:['written','artifactPath'],properties:{written:{type:'boolean'},artifactPath:{type:'string'}}};
const VERIFICATION_SCHEMA={type:'object',required:['exists','matches','verified'],properties:{exists:{type:'boolean'},matches:{type:'boolean'},verified:{type:'boolean'}}};
const REPORT_SCHEMA={type:'object',required:['decision','markdown'],properties:{decision:{type:'string',enum:['批准发布','有条件放行','不予发布']},markdown:{type:'string'}}};
const NOTIFY_SCHEMA={type:'object',required:['subject','body','recipients'],properties:{subject:{type:'string'},body:{type:'string'},recipients:{type:'array',items:{type:'string'}}}};
const GUARD='只依据本提示中的虚构材料；分析与建议不得读取文件、联网、修改系统或发送消息。通过 workflow(action=result.submit, params.data=分析结果) 提交真实结构化分析，不把建议写成已执行。';
phase('check');
let check=await worker(`核查发布材料，明确迭代、日期、缺陷口径和需求优先级。输入已有迭代=${args.iterationName}；日期=${args.releaseDate}。未能确定的字段用英文属性名 iteration/releaseDate/severityConvention/priorityConvention 列入 missingFields。需求：${args.requirements}\n缺陷：${args.defects}\n${GUARD}`,{id:'check-materials',label:'材料核查',phase:'check',schema:CHECK_SCHEMA,resolve:WAKER});
if(!check)throw {code:'MATERIAL_CHECK_FAILED'};
if(args.iterationName)check.iteration=args.iterationName;
if(args.releaseDate)check.releaseDate=args.releaseDate;
const requiredFields=['iteration','releaseDate','severityConvention','priorityConvention'];
let missing=requiredFields.filter(field=>typeof check[field]!=='string'||check[field].trim()==='');
if(missing.length>0){
  const fill=await askUser(`缺失字段：${missing.join(', ')}。请提交完整 JSON 对象，字段为 iteration、releaseDate、severityConvention、priorityConvention。`,{id:'fill-materials',label:'结构化补全材料',phase:'check',allowSkip:true});
  if(fill.source!=='user')throw {code:'MATERIAL_INPUT_REQUIRED'};
  const fields=JSON.parse(String(fill.value));
  for(const field of missing){
    if(typeof fields[field]!=='string'||fields[field].trim()==='')throw {code:'MATERIAL_FIELD_INVALID',field};
    check[field]=fields[field].trim();
  }
  check.missingFields=[];
}
let envList=args.environments.map(value=>value.trim()).filter(value=>value.length>0);
if(envList.length===0){
  const fill=await askUser('请填写环境数组 JSON，例如 ["A","B","C"]。',{id:'fill-environments',label:'补全客户环境',phase:'check',allowSkip:false});
  const parsed=JSON.parse(String(fill.value));
  if(!Array.isArray(parsed)||parsed.length===0||parsed.some(value=>typeof value!=='string'||value.trim()===''))throw {code:'ENVIRONMENT_INPUT_INVALID'};
  envList=parsed.map(value=>value.trim());
}
const context={iteration:check.iteration,releaseDate:check.releaseDate,severityConvention:check.severityConvention,priorityConvention:check.priorityConvention,requirements:args.requirements,defects:args.defects};
const threshold=args.riskThreshold;
const results=await pipeline(envList,[
  async(previous,environment,index)=>{
    const analyses=await parallel([
      ()=>worker(`环境=${environment}；需求覆盖分析。上下文=${JSON.stringify(context)}。逐条统计，列出该环境的高优缺口并保留依据。${GUARD}`,{id:'coverage-environment',label:'分析需求覆盖',phase:'analyse',schema:COVERAGE_SCHEMA,resolve:WAKER}),
      ()=>worker(`环境=${environment}；缺陷风险分析。上下文=${JSON.stringify(context)}。列出高风险、发布阻塞及具体原因；缺少事实明确写出。${GUARD}`,{id:'risk-environment',label:'分析缺陷风险',phase:'analyse',schema:RISK_SCHEMA,resolve:WAKER})
    ],{id:'analyse-pair',failureMode:'collect-null'});
    if(!analyses[0]||!analyses[1])throw {code:'ENVIRONMENT_ANALYSIS_FAILED',environment};
    return {environment,coverage:analyses[0],risk:analyses[1]};
  },
  async(previous,environment,index)=>{
    const items=[
      ...previous.risk.highRiskItems.map((value,i)=>({id:environment+'-defect-'+i,item:value.title,reason:value.reason,impact:value.impact})),
      ...previous.coverage.highPriorityGaps.map((value,i)=>({id:environment+'-gap-'+i,item:value.requirement,reason:value.reason,impact:'需求未覆盖'}))
    ];
    const suggestions=await parallel(items.map(item=>async()=>{
      const suggestion=await worker(`环境=${environment}；单项处置建议。条目=${JSON.stringify(item)}。上下文=${JSON.stringify(context)}。输出建议和预测剩余风险；当前尚未执行任何处置。${GUARD}`,{id:'recommend-item',label:'形成处置建议',phase:'mitigate',schema:MITIGATION_SCHEMA,resolve:WAKER});
      if(!suggestion)throw {code:'MITIGATION_FAILED',environment,item:item.id};
      return {id:item.id,item:item.item,reason:item.reason,action:suggestion.action,residualRisk:suggestion.residualRisk,note:suggestion.note,recommended:true,executed:false,verified:false};
    }),{id:'mitigate-items',failureMode:'collect-null'});
    if(suggestions.some(value=>value===null))throw {code:'MITIGATION_INCOMPLETE',environment};
    const residual=suggestions.filter(value=>value.residualRisk==='high');
    const needsApproval=residual.length>=threshold||previous.risk.blocking;
    let approvalItems=needsApproval?residual:[];
    const emptyBlocking=previous.risk.blocking&&residual.length===0;
    if(emptyBlocking){
      approvalItems=[{id:environment+'-blocking',item:'发布阻塞原因未解决',reason:previous.risk.summary,action:'确认阻塞原因和是否接受风险',residualRisk:'high',note:previous.risk.summary,recommended:false,executed:false,verified:false}];
    }
    return {environment,coverage:previous.coverage,risk:previous.risk,suggestions,approvalItems,emptyBlocking,approvalTriggered:needsApproval};
  },
  async(previous,environment,index)=>{
    const records=[];
    const executions=[];
    for(let position=0;position<previous.approvalItems.length;position++){
      let item=previous.approvalItems[position];
      let resolved=false;
      let round=0;
      while(!resolved&&round<3){
        round++;
        const answer=await askUser(`环境=${environment}；条目=${item.item}；建议=${item.action}；剩余风险=${item.residualRisk}；原因=${item.reason}；说明=${item.note}。只有显式批准才允许后续本地验收。也可提出修改或否决理由。`,{id:'approve-item',label:'逐项审批建议',phase:'approve',kind:'select',choices:[{value:'approve',label:'批准'},{value:'deny',label:'否决'},{value:'rework',label:'返修'}],defaultValue:'deny',allowSkip:true});
        if(answer.source==='user'&&answer.value==='rework'&&round<3){
          const note=await askUser('请填写本条处置建议的具体修改意见。',{id:'rework-note',label:'填写返修说明',phase:'approve',allowSkip:false});
          const old=item;
          try{
            const revised=await worker(`环境=${environment}；返修条目=${JSON.stringify(old)}；修改意见=${String(note.value)}。上下文=${JSON.stringify(context)}。保留事实，不声称已执行。${GUARD}`,{id:'revise-item',label:'返修处置建议',phase:'mitigate',schema:MITIGATION_SCHEMA,resolve:WAKER});
            if(revised)item={id:old.id,item:old.item,reason:old.reason,action:revised.action,residualRisk:revised.residualRisk,note:revised.note,recommended:old.recommended,executed:false,verified:false};
          }catch(failure){item=old;log({environment,message:'返修失败，保留原建议',failure});}
          continue;
        }
        const approved=answer.source==='user'&&answer.value==='approve';
        records.push({environment,item:item.item,itemId:item.id,approved,source:answer.source,remark:approved?'显式批准':String(answer.value??'默认否决'),reviewRounds:round,blocking:previous.emptyBlocking});
        resolved=true;
        if(approved&&args.localAcceptance&&!previous.emptyBlocking){
          const payload={environment,itemId:item.id,action:item.action,accepted:true};
          const path=flowContext.scope.workingDirectory+'/acceptance/'+item.id+'.json';
          const execution=await worker(`只在工作流获准目录实际写入 ${path}，内容必须精确为 ${JSON.stringify(payload)}。需要时创建目录，使用实际文件工具，禁止修改任何其他目录或调用外部系统。提交真实 written 与 artifactPath。`,{id:'execute-item',label:'本地执行处置',phase:'mitigate',schema:EXECUTION_SCHEMA,resolve:WAKER});
          let verification=null;
          if(execution&&execution.written){
            verification=await worker(`独立用真实文件工具读取 ${path}。精确内容应为 ${JSON.stringify(payload)}。仅在文件存在且实际内容相符时报告 exists/matches/verified=true。不得修改文件或联系外部系统。`,{id:'verify-item',label:'独立验证结果',phase:'mitigate',schema:VERIFICATION_SCHEMA,resolve:WAKER});
          }
          executions.push({environment,itemId:item.id,executed:!!(execution&&execution.written),verified:!!(verification&&verification.exists&&verification.matches&&verification.verified),execution,verification});
        }
      }
    }
    return {environment,coverage:previous.coverage,risk:previous.risk,suggestions:previous.suggestions,approvalTriggered:previous.approvalTriggered,emptyBlocking:previous.emptyBlocking,records,executions};
  }
],{id:'release-environments',failureMode:'collect-null',concurrency:3});
const approvalRecords=results.reduce((all,result)=>all.concat(result?result.records:[]),[]);
const executions=results.reduce((all,result)=>all.concat(result?result.executions:[]),[]);
const failedEnvironments=results.map((result,index)=>result?null:envList[index]).filter(value=>value!==null);
const anyRejected=approvalRecords.some(record=>!record.approved)||failedEnvironments.length>0||executions.some(value=>!value.verified);
const decision=anyRejected?'不予发布':approvalRecords.length>0?'有条件放行':'批准发布';
const stats={environmentsEvaluated:results.filter(value=>value!==null).length,riskThreshold:threshold,recommendationCount:results.reduce((total,result)=>total+(result?result.suggestions.length:0),0),approvedCount:approvalRecords.filter(record=>record.approved).length,rejectedCount:approvalRecords.filter(record=>!record.approved).length,executedCount:executions.filter(value=>value.executed).length,verifiedCount:executions.filter(value=>value.verified).length,failedEnvironments,anyRejected};
phase('report');
let report=await worker(`仅依据真实数据生成分环境预审报告。最终结论固定为 ${decision}，不得修改。结构化上下文=${JSON.stringify(context)}；逐环境结果=${JSON.stringify(results)}；审批=${JSON.stringify(approvalRecords)}；统计=${JSON.stringify(stats)}。执行与建议必须分别表述；失败环境未放行。${GUARD}`,{id:'release-report',label:'分环境预审报告',phase:'report',schema:REPORT_SCHEMA,resolve:WAKER});
let reportRounds=0;
while(report&&report.decision!==decision&&reportRounds<2){
  reportRounds++;
  report=await worker(`返修报告结论：权威结论=${decision}，原报告=${JSON.stringify(report)}。实际数据=${JSON.stringify({context,results,approvalRecords,stats})}。不得把建议数量写成执行数量。${GUARD}`,{id:'revise-report-facts',label:'修正报告与事实一致性',phase:'report',schema:REPORT_SCHEMA,resolve:WAKER});
}
const factsMarkdown=`# 发布预审\n## 最终结论\n${decision}\n\n基于用户提供的虚构材料；未向外发送或发布。\n\n迭代：${context.iteration}；日期：${context.releaseDate}；缺陷口径：${context.severityConvention}；需求优先级：${context.priorityConvention}\n\n建议 ${stats.recommendationCount}；显式批准 ${stats.approvedCount}；否决 ${stats.rejectedCount}；实际执行 ${stats.executedCount}；独立验证 ${stats.verifiedCount}。\n失败环境：${failedEnvironments.join(', ')||'无'}\n\n${approvalRecords.map(record=>record.environment+' / '+record.item+'：'+(record.approved?'批准':'否决')+'，来源='+record.source).join('\n')}`;
const reportMarkdown=factsMarkdown+(report&&report.decision===decision?'\n\n'+report.markdown:'\n\n模型报告未通过结论一致性检查，仅展示权威事实。');
let notificationDraft=null;
if(decision!=='不予发布'){
  const draft=await worker(`仅起草通知文本，无发送能力。结论=${decision}；报告=${reportMarkdown}。只列建议收件角色，不声称已发送。${GUARD}`,{id:'notification-draft',label:'通知草稿，不发送',phase:'report',schema:NOTIFY_SCHEMA,resolve:WAKER});
  if(draft)notificationDraft={subject:draft.subject,body:draft.body,recipients:draft.recipients,sent:false};
}
return {decision,reportMarkdown,notificationDraft,envResults:results,approvalRecords,stats,context};
