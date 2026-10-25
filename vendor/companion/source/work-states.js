/* NeuxBot 工作表情。身体节拍在下面单独采样，眼神轮廓见 ally-eyes.js。 */
(() => {
  const both=(extra={})=>({x:0,y:0,scaleX:1,scaleY:1,rotate:0,open:1,lookX:0,lookY:0,...extra});
  const eyes=(shared,left={},right={})=>({both:shared,left:{...shared,...left},right:{...shared,...right}});
  const enter=(...items)=>({settle:'base',frames:items});
  const expressions={
    '30':{id:'30',pool:['allyThinkLeft','allyThinkQuiet','allyThinkRight'],poolMs:[2300,3700],blinkMs:[3500,7000],gaze:false,body:{rotate:-2,breathe:.005},eyes:eyes(both({lookY:-6,y:-2})),anims:[{target:'eyes',prop:'lookX',type:'sine',amp:9,period:2600}]},
    '31':{id:'31',pool:['allyReady','allyReadyWarm','calm'],poolMs:[3800,5700],blinkMs:null,gaze:true,transition:220,body:{breathe:.008},eyes:eyes(both()),sequence:enter({at:0},{at:100,eyes:{both:{open:.08}},body:{y:5}},{at:280,eyes:{both:{open:1,scaleX:1.12,scaleY:1.12,y:-2}},body:{y:-5}},{at:510,body:{y:2}},{at:850,body:{y:0}})},
    '32':{id:'32',pool:['allyBusyFocus','allyBusyResolve','allyBusyOpen'],poolMs:[1600,2600],blinkMs:[2800,5500],gaze:false,body:{y:1,breathe:.005},eyes:eyes(both()),anims:[{target:'eyes',prop:'lookX',type:'sine',amp:6,period:1200},{target:'eyes',prop:'lookY',type:'sine',amp:4,period:900,phase:.8}]},
    '33':{id:'33',pool:['allyHappy'],poolMs:[4000,6000],blinkMs:null,gaze:false,celebrate:true,transition:240,body:{y:-2,breathe:.008},eyes:eyes(both({y:-3})),sequence:enter({at:0,body:{y:3,scale:.98}},{at:160,body:{y:6,scale:.96}},{at:440,body:{y:-27,scale:1.035}},{at:780,body:{y:2,scale:.985}},{at:1100,body:{y:-2,scale:1}})},
    '34':{id:'34',pool:['allyErrorStart','allyErrorWorry'],poolMs:[2400,3700],blinkMs:null,gaze:false,transition:220,body:{y:3,rotate:-6,breathe:.008},eyes:eyes(both(),{y:-3,rotate:-6},{y:4,rotate:8}),anims:[{target:'eyes',prop:'lookX',type:'jitter',amp:1.4,speed:8,decay:800}],sequence:enter({at:0,eyes:{both:{scaleX:1.25,scaleY:1.25}},body:{y:-8}},{at:180,body:{x:-5,rotate:-9}},{at:350,body:{x:4,rotate:5}},{at:520,body:{x:-3,rotate:-7}},{at:820,body:{x:0,y:3,rotate:-5}})},
    '35':{id:'35',pool:['allyListenOpen','allyListenSoft','allyListenBright'],poolMs:[2700,4400],blinkMs:[3000,7000],gaze:true,body:{rotate:-2,breathe:.005},eyes:eyes(both()),anims:[{target:'eyes',prop:'lookY',type:'sine',amp:6,period:2200}]},
    '36':{id:'36',pool:['allyConnectOpen','allyConnectSoft'],poolMs:[3700,5200],blinkMs:null,gaze:false,body:{breathe:.005},eyes:eyes(both({scaleX:.75,scaleY:.75})),anims:[
      {target:'left',prop:'open',type:'blink',interval:1200,dur:380},
      {target:'right',prop:'open',type:'blink',interval:1200,dur:380,phaseMs:600},
      {target:'eyes',prop:'lookX',type:'sine',amp:2,period:2400}
    ]},
    '37':{id:'37',pool:['allyRecallUp','allyRecallQuiet','allyRecallFound'],poolMs:[3200,5100],blinkMs:null,gaze:false,body:{rotate:1,breathe:.005},eyes:eyes(both({lookY:-9,lookX:3,y:-3})),anims:[{target:'eyes',prop:'lookY',type:'sine',amp:2.4,period:4000}]},
    '38':{id:'38',pool:['allyRefuseFirm','allyRefuseSoft'],poolMs:[2700,4100],blinkMs:[4500,8000],gaze:false,openness:.6,body:{y:2,breathe:.008},eyes:eyes(both({lookY:3,y:2})),sequence:enter({at:0,body:{x:0}},{at:130,body:{x:-9,rotate:-7}},{at:300,body:{x:9,rotate:6}},{at:470,body:{x:-7,rotate:-5}},{at:640,body:{x:5,rotate:3}},{at:900,body:{x:0,rotate:0}})},
    '39':{id:'39',pool:['allySpeakOpen','allySpeakWarm','allySpeakBright'],poolMs:[2000,3300],blinkMs:null,gaze:true,body:{breathe:.005},eyes:eyes(both({y:-2})),anims:[{target:'eyes',prop:'scale',type:'pulse',amp:.06,period:740}]},
    '40':{id:'40',pool:['allyReadOpen','allyReadNarrow','allyReadCheck','allyReadAlert'],poolMs:[1200,1900],poolSpeed:9,blinkMs:[1600,4000],gaze:false,body:{breathe:.005},eyes:eyes(both()),anims:[{target:'eyes',prop:'lookX',type:'scan',amp:11,period:700}]},
    '41':{id:'41',pool:['allySleep'],poolMs:[6000,9000],blinkMs:null,gaze:false,anims:[],body:{y:6,breathe:0},eyes:eyes(both({scaleX:.9,scaleY:.9,y:3})),sequence:{settle:'hold',frames:[{at:0,body:{y:0,scale:1},eyes:{both:{scaleX:1,scaleY:1,y:0}}},{at:1500,body:{y:6,scale:.985},eyes:{both:{scaleX:.9,scaleY:.9,y:3}}}]}},
  };
  Object.assign(window.AllyExpressions,expressions);
  window.AllyWorkExpressions=expressions;

  const smooth=x=>{x=Math.max(0,Math.min(1,x));return x*x*(3-2*x);};
  const idle=()=>({x:0,y:0,rotate:0,scaleX:1,scaleY:1,yaw:0,lookX:0,lookY:0,lookMix:0,pitch:0});
  const keys=Object.keys(idle());
  const mix=(a,b,k)=>Object.fromEntries(keys.map(key=>[key,a[key]+(b[key]-a[key])*k]));
  const frame=(at,pose={})=>({at,pose:{...idle(),...pose}});
  // 动作有起势、重音和停顿，不用一条不停摆动的正弦覆盖全部工作状态。
  // 坐标沿用 240 单位画布；缩放只用于轻微重心压缩，不改变局部轮廓。
  const loops={
    '35':[frame(0),frame(.65,{x:-2,rotate:-3,yaw:-.08}),frame(1.25,{x:-2,rotate:-3,yaw:-.08}),
      frame(1.55,{x:-2,y:5,rotate:-4,scaleX:1.01,scaleY:.99,yaw:-.08}),frame(1.86,{x:-2,y:-1,rotate:-2,yaw:-.08}),
      frame(2.15,{x:-2,y:3.5,rotate:-3,scaleX:1.008,scaleY:.992,yaw:-.08}),frame(2.5,{x:-2,rotate:-2,yaw:-.06}),frame(3.65),frame(4.5)],
    '30':[frame(0),frame(1.05,{x:-4,y:-2,rotate:-5,yaw:-.1}),frame(1.85,{x:-4,y:-2,rotate:-5,yaw:-.1}),
      frame(3),frame(4.05,{x:3,y:-3,rotate:4,yaw:.1}),frame(4.8,{x:3,y:-3,rotate:4,yaw:.1}),frame(5.8),frame(6.2)],
    '40':[frame(0),frame(.65,{x:-6,y:-1,rotate:-3.5,yaw:-.2}),frame(1.12,{x:-6,y:-1,rotate:-3.5,yaw:-.2}),
      frame(1.8),frame(2.45,{x:6,y:-1,rotate:3.5,yaw:.2}),frame(2.92,{x:6,y:-1,rotate:3.5,yaw:.2}),frame(3.6)],
    '32':[frame(0),frame(.38,{x:-3,y:3,rotate:-3,scaleX:1.012,scaleY:.988}),
      frame(.78,{x:3,y:-2,rotate:3}),frame(1.18,{x:-3,y:3,rotate:-3,scaleX:1.012,scaleY:.988}),
      frame(1.58,{x:3,y:-2,rotate:3}),frame(2.05),frame(2.8)],
    '36':[frame(0),frame(.6,{x:-3,y:-1,rotate:-4,yaw:-.06}),frame(1.2),
      frame(1.8,{x:3,y:-1,rotate:4,yaw:.06}),frame(2.4),frame(3.2)],
    '37':[frame(0),frame(1.05,{x:4,y:-5,rotate:5,yaw:.14}),frame(2.05,{x:4,y:-5,rotate:5,yaw:.14}),
      frame(2.75,{y:-1,rotate:1,yaw:.04}),frame(3.1,{y:3,rotate:1,scaleX:1.01,scaleY:.99}),frame(3.5),frame(5.2)],
    '39':[frame(0),frame(.45,{x:-2,y:-3,rotate:-3}),frame(.82,{x:-2,y:2,rotate:-1,scaleX:1.008,scaleY:.992}),
      frame(1.2,{x:2,y:-3,rotate:3}),frame(1.58,{x:2,y:2,rotate:1,scaleX:1.008,scaleY:.992}),
      frame(2.12,{x:-1,y:-2,rotate:-2}),frame(2.7),frame(3.4)]
  };
  for(const id of Object.keys(loops))expressions[id].body.breathe=.005;
  function sampleLoop(frames,t){
    const phase=t%frames.at(-1).at;
    const end=frames.findIndex(f=>f.at>phase),a=frames[end-1],b=frames[end];
    return mix(a.pose,b.pose,smooth((phase-a.at)/(b.at-a.at)));
  }
  function sample(id,seconds){
    const t=Number.isFinite(seconds)?Math.max(0,seconds):0;
    const pose=loops[id]?sampleLoop(loops[id],t):idle();
    if(loops[id]){
      // 眼神与身体共用节拍，视线稍早到达，身体随后跟上。
      const lead=sampleLoop(loops[id],t+.12);
      const ownsGaze=!expressions[id].gaze;
      pose.lookMix=ownsGaze?1:0;
      pose.lookX=lead.yaw*32+lead.x*.4;
      pose.lookY=id==='30'?-4:id==='37'?-6:id==='32'?3:0;
      pose.pitch=pose.y*(id==='35'?.55:.3);
    }
    return pose;
  }
  // 普通切换让旧姿态继续运动；连续打断时冻结当前合成姿态。
  // 过渡采用有界 ease-out，不让身体因弹簧超调改变比例。
  function createTransition(duration=.32){
    let from=idle(),start=-Infinity,movingFrom=null;
    return {
      begin(pose,t,source=null){movingFrom=t-start>=duration?source:null;from={...pose};start=t;},
      sample(target,t){
        const progress=Math.max(0,Math.min(1,(t-start)/duration));
        return mix(movingFrom&&progress<1?movingFrom(t):from,target,1-Math.pow(1-progress,5));
      }
    };
  }
  window.AllyWorkGesture=sample;
  window.AllyWorkMotion=Object.freeze({sample,idle,createTransition});
  const descriptions={
    '35':'眼形柔和地轮换，侧身聆听并轻轻点头', '31':'眨眼点头，轻轻起伏确认后回稳',
    '30':'歪身想一想，停顿后换一侧寻找思路', '40':'身体左右探看，转向后稍停一下扫读',
    '32':'左右交替重心，轻点两拍再停一下核对', '36':'轻轻左右摇摆，配合两眼交替等待连接',
    '37':'侧身抬头回忆，停顿后轻轻点头', '39':'身体随语句轻轻起伏，眼神配合讲述',
    '33':'笑眼、蓄力弹起，再自然落下', '34':'突然睁大眼睛，后缩一怔再轻晃回稳',
    '38':'压低眼皮，身体连续轻摇', '41':'眼睛合成柔和弧线，身体慢慢落定'
  };
  for(const entry of window.AllyStates)if(descriptions[entry.id])entry.description=descriptions[entry.id];
})();
