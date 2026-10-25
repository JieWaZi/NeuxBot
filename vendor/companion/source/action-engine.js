/* NeuxBot 动作引擎：动作只是一组可保存、可测试的代码数据，采样不读取真实时钟。 */
(() => {
  'use strict';

  const DEFAULT_POSE=Object.freeze({
    x:0,y:0,rotate:0,scaleX:1,scaleY:1,yaw:0
  });
  const easings=Object.freeze({
    linear:t=>t,
    easeInCubic:t=>t*t*t,
    easeOutCubic:t=>1-Math.pow(1-t,3),
    easeInOutCubic:t=>t<.5?4*t*t*t:1-Math.pow(-2*t+2,3)/2,
    easeOutQuint:t=>1-Math.pow(1-t,5),
    smooth:t=>t*t*(3-2*t)
  });
  const actions=new Map();
  const channelNames=Object.keys(DEFAULT_POSE);
  const clamp=(value,min,max)=>Math.max(min,Math.min(max,value));

  function freeze(value){
    if(!value||typeof value!=='object'||Object.isFrozen(value))return value;
    Object.freeze(value);
    for(const item of Object.values(value))freeze(item);
    return value;
  }

  function normalize(raw){
    if(!raw||typeof raw.name!=='string'||!raw.name)throw new TypeError('动作必须有名称');
    if(!Number.isFinite(raw.duration)||raw.duration<=0)throw new TypeError(`${raw.name} 的时长无效`);
    if(!Array.isArray(raw.frames)||raw.frames.length<2)throw new TypeError(`${raw.name} 至少需要两个关键帧`);
    let previousAt=-Infinity,pose={...DEFAULT_POSE};
    const frames=raw.frames.map((frame,index)=>{
      if(!frame||!Number.isFinite(frame.at)||frame.at<0||frame.at>raw.duration||(index>0&&frame.at<=previousAt)){
        throw new TypeError(`${raw.name} 的第 ${index+1} 个关键帧时间无效`);
      }
      if(!frame.pose||typeof frame.pose!=='object')throw new TypeError(`${raw.name} 的第 ${index+1} 个关键帧姿态无效`);
      for(const key of Object.keys(frame.pose))if(!channelNames.includes(key))throw new TypeError(`${raw.name} 使用了未知通道 ${key}`);
      previousAt=frame.at;
      pose={...pose,...(frame.pose||{})};
      for(const [key,value] of Object.entries(pose)){
        if(!Number.isFinite(value))throw new TypeError(`${raw.name}.${key} 必须是有限数值`);
      }
      const ease=frame.ease||'easeInOutCubic';
      if(!easings[ease])throw new TypeError(`${raw.name} 使用了未知缓动 ${ease}`);
      return {at:frame.at,ease,pose:{...pose}};
    });
    if(frames[0].at!==0)throw new TypeError(`${raw.name} 的首帧必须从 0 开始`);
    if(frames.at(-1).at!==raw.duration)throw new TypeError(`${raw.name} 的末帧必须等于动作时长`);
    for(const key of Object.keys(raw.reduced||{}))if(!channelNames.includes(key))throw new TypeError(`${raw.name} 的低动态姿态使用了未知通道 ${key}`);
    const reduced={...DEFAULT_POSE,...(raw.reduced||{})};
    for(const [key,value] of Object.entries(reduced))if(!Number.isFinite(value))throw new TypeError(`${raw.name}.reduced.${key} 必须是有限数值`);
    const replace=[...new Set(raw.replace||[])];
    for(const channel of replace)if(!channelNames.includes(channel))throw new TypeError(`${raw.name} 接管了未知通道 ${channel}`);
    return freeze({...raw,frames,reduced,replace});
  }

  function register(definitions){
    for(const raw of definitions){
      const action=normalize(raw);
      if(actions.has(action.name))throw new TypeError(`动作 ${action.name} 已注册`);
      actions.set(action.name,action);
    }
  }

  function sample(name,elapsed,reducedMotion=false){
    const action=actions.get(name);
    if(!action)return {...DEFAULT_POSE,name:null,duration:0,progress:0,active:false,replace:[]};
    const time=Number.isFinite(elapsed)?Math.max(0,elapsed):0;
    if(time>=action.duration)return {...DEFAULT_POSE,name,duration:action.duration,progress:1,active:false,replace:[]};
    if(reducedMotion){
      return {...action.reduced,name,duration:action.duration,progress:time/action.duration,active:true,replace:action.replace};
    }
    let right=action.frames.findIndex(frame=>frame.at>=time);
    if(right<=0)right=1;
    const from=action.frames[right-1],to=action.frames[right];
    const span=Math.max(.000001,to.at-from.at);
    const ratio=easings[to.ease](clamp((time-from.at)/span,0,1));
    const pose={};
    for(const key of channelNames)pose[key]=from.pose[key]+(to.pose[key]-from.pose[key])*ratio;
    return {...pose,name,duration:action.duration,progress:time/action.duration,active:true,replace:action.replace};
  }

  function blendPose(from,to,ratio){
    const t=easings.easeOutQuint(clamp(ratio,0,1)),pose={};
    for(const key of channelNames)pose[key]=from[key]+(to[key]-from[key])*t;
    return pose;
  }

  /*
   * 播放器只记录“哪个动作从何时开始”。动作帧本身仍由 sample(name, t) 纯采样。
   * 切换动作时先冻结当前合成姿态，再短暂混入新动作，避免整体姿态瞬移。
   */
  function createPlayer(transition=.16){
    let current=null,startedAt=0,blendAt=0,blendFrom={...DEFAULT_POSE};
    const read=(now,reducedMotion=false)=>{
      const target=current?sample(current,now-startedAt,reducedMotion):{...DEFAULT_POSE,name:null,duration:0,progress:0,active:false,replace:[]};
      // 减少动态时直接显示静态反馈，取消后也立即恢复，不等待不会推进的过渡帧。
      if(reducedMotion)return target;
      const ratio=transition<=0?1:(now-blendAt)/transition;
      if(ratio>=1)return target;
      return {...target,...blendPose(blendFrom,target,ratio),active:target.active||!!blendFrom.active,replace:[...new Set([...target.replace,...(blendFrom.replace||[])])]};
    };
    return Object.freeze({
      play(name,now=0){
        const definition=actions.get(name);
        if(!definition)return null;
        blendFrom=read(now,false);
        current=name;
        startedAt=now;
        blendAt=now;
        return definition;
      },
      stop(now=0){blendFrom=read(now,false);current=null;blendAt=now;},
      sample:(now,reducedMotion=false)=>read(now,reducedMotion),
      name:()=>current
    });
  }

  window.AllyActionEngine=Object.freeze({
    register,
    sample,
    createPlayer,
    get:name=>actions.get(name)||null,
    list:()=>[...actions.values()],
    idle:()=>({...DEFAULT_POSE,name:null,duration:0,progress:0,active:false,replace:[]})
  });
})();
