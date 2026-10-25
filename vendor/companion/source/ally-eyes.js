/* NeuxBot-owned eye grammar: the user's 20 × 33 ivory-character oval is the root.
 * Variants preserve rounded silhouettes, eye spacing and highlight room. */
(() => {
  function oval(cx,mirror,{width=20,height=33,lid=0,slope=0,tilt=0,lift=0}={}){
    const ring=[],angle=tilt*Math.PI/180,c=Math.cos(angle),s=Math.sin(angle);
    for(let i=0;i<48;i++){
      const a=i/48*Math.PI*2,x=-Math.cos(a)*width/2*mirror;
      let y=-Math.sin(a)*height/2;
      if(Math.sin(a)>0&&lid)y=Math.max(y,-height/2*(1-lid)+slope*x);
      ring.push([cx+x*c-y*s,104+lift+x*s+y*c]);
    }
    return ring;
  }
  const pair=(left={},right=left)=>[oval(91,1,left),oval(149,-1,right)];
  window.AllyWorkEyes={
    allyListenOpen:pair({height:34,width:20}),
    allyListenSoft:pair({height:31,lid:.12}),
    allyListenBright:pair({height:35,width:21}),
    allyReady:pair({height:33,width:20.5}),
    allyReadyWarm:pair({height:31,lid:.08,tilt:-3},{height:32,tilt:3}),
    allyThinkLeft:pair({tilt:11,lift:-4},{tilt:8,lift:2,height:31}),
    allyThinkRight:pair({tilt:-7,lift:1,height:31},{tilt:-10,lift:-4}),
    allyThinkQuiet:pair({tilt:7,lift:-2,lid:.18},{tilt:7,height:32}),
    allyReadOpen:pair({height:32}),
    allyReadNarrow:pair({height:30,lid:.24}),
    allyReadAlert:pair({height:34,width:21}),
    allyReadCheck:pair({height:31,lid:.12,tilt:-5},{height:33,tilt:-5}),
    allyBusyFocus:pair({height:32,lid:.42,slope:.22},{height:32,lid:.42,slope:-.22}),
    allyBusyResolve:pair({height:31,lid:.26,slope:.13},{height:31,lid:.26,slope:-.13}),
    allyBusyOpen:pair({height:32,lid:.12}),
    allyConnectOpen:pair(),
    allyConnectSoft:pair({height:31,lid:.12}),
    allyRecallUp:pair({tilt:-9,lift:-3},{tilt:-7,lift:-4,height:34}),
    allyRecallQuiet:pair({tilt:-5,height:30,lid:.2},{tilt:-5,height:32,lid:.1}),
    allyRecallFound:pair({height:35,width:21,tilt:-4}),
    allySpeakOpen:pair(),
    allySpeakWarm:pair({height:30,width:21,lid:.1}),
    allySpeakBright:pair({height:34,width:21}),
    allyErrorStart:pair({height:35,width:22}),
    allyErrorWorry:pair({tilt:-6,height:33,lid:.15},{tilt:6,height:31,lid:.1}),
    allyRefuseFirm:pair({height:31,lid:.42,slope:.2},{height:31,lid:.42,slope:-.2}),
    allyRefuseSoft:pair({height:31,lid:.23}),
  };
})();
