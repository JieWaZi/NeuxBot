export const meta = {
  sdk:'ally.workflow/2', title:'环境检查与审批', description:'三环境逐项检查、建议、批准后执行与独立验证；通知只生成草稿',
  inputSchema:{type:'object',properties:{environments:{type:'array',items:{type:'string'},default:['A','B','C']}},required:['environments']},
  phases:[{id:'inspect',title:'环境检查',detail:'每项独立检查',next:['recommend']},{id:'recommend',title:'提出建议',detail:'建议不表示已执行',next:['approve']},{id:'approve',title:'逐项审批',detail:'缺失输入和操作决定分别等待',next:['execute']},{id:'execute',title:'执行与验证',detail:'仅显式批准的项实际执行，然后独立验证',canExit:true}]
};
const WAKER={kind:'waker',wakerId:'fixture',runtimeId:'fixture-runtime'};
const environments=await pipeline(args.environments,[
  async (previous,original,index)=>await worker(original,{id:'inspect-environment',label:'检查 '+original,phase:'inspect',resolve:WAKER}),
  async (previous,original,index)=>{
    let configuration='provided';
    if(original==='A'){
      const input=await askUser('补充 '+original+' 的缺失配置',{id:'missing-input',label:'补充配置 '+original,phase:'approve',allowSkip:true});
      if(input.status !== 'answered' || input.value==='')throw {code:'INPUT_REQUIRED',environment:original};
      configuration=input.value;
    }
    return {inspection:previous,configuration};
  },
  async (previous,original,index)=>{
    const recommendation=await worker(original+'-recommend',{id:'recommend-environment',label:'形成建议 '+original,phase:'recommend',resolve:WAKER});
    return {inspection:previous.inspection,configuration:previous.configuration,recommendation};
  },
  async (previous,original,index)=>{
    const decision=await askUser('批准 '+original+' 的具体建议？\n'+previous.recommendation,{id:'approve-environment',label:'审批 '+original,phase:'approve',kind:'select',choices:[{value:'approve',label:'批准'},{value:'deny',label:'否决'}],defaultValue:'deny',allowSkip:true});
    return {environment:original,inspection:previous.inspection,recommendation:previous.recommendation,decision,executed:false,verified:false};
  },
  async (previous,original,index)=>{
    if(previous.decision.source!=='user' || previous.decision.value!=='approve')return previous;
    const execution=await worker(original+'-execute',{id:'execute-environment',label:'实际执行 '+original,phase:'execute',resolve:WAKER});
    const verification=await worker(original+'-verify',{id:'verify-environment',label:'独立验证 '+original,phase:'execute',resolve:WAKER});
    return {environment:original,inspection:previous.inspection,recommendation:previous.recommendation,decision:previous.decision,executed:true,execution,verification,verified:verification===original+'-verify-result'};
  }
],{id:'environment-pipeline',failureMode:'collect-null',concurrency:3});
const executedCount=environments.filter(item=>item && item.executed).length;
const verifiedCount=environments.filter(item=>item && item.verified).length;
const recommendationCount=environments.filter(item=>item && item.recommendation).length;
const reportMarkdown=environments.map((item,index)=>item?`${args.environments[index]}：建议已生成，执行=${item.executed}，验证=${item.verified}，审批来源=${item.decision.source}`:`${args.environments[index]}：检查或输入失败，未执行`).join('\n');
return {environments,recommendationCount,executedCount,verifiedCount,reportMarkdown,notificationDraft:{text:reportMarkdown,sent:false}};
