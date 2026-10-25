/* NeuxBot 傀儡。完整静态身体与风格配色；眼睛共用 48 点闭环，方便逐点混合。 */
(() => {
  const NS = 'http://www.w3.org/2000/svg';
  const eyeScale=1.25;
  const clamp=(v,a,b)=>Math.max(a,Math.min(b,v));
  let uid=0;
  function node(tag,attrs={},parent){const n=document.createElementNS(NS,tag);for(const [k,v] of Object.entries(attrs))n.setAttribute(k,v);if(parent)parent.appendChild(n);return n;}
  function path(ring){let d=`M${ring[0][0]} ${ring[0][1]}`;for(let i=0;i<ring.length;i++){const a=ring[(i-1+ring.length)%ring.length],b=ring[i],c=ring[(i+1)%ring.length],e=ring[(i+2)%ring.length];d+=`C${b[0]+(c[0]-a[0])/6} ${b[1]+(c[1]-a[1])/6} ${c[0]-(e[0]-b[0])/6} ${c[1]-(e[1]-b[1])/6} ${c[0]} ${c[1]}`;}return d+'Z';}
  // 同一 48 点闭环上的眼形，笑眼和扫描眼可以逐点混合。
  function contour(kind,cx,mirror){const ring=[];for(let i=0;i<48;i++){const a=i/48*Math.PI*2,u=(1-Math.cos(a))/2,x=(u-.5)*20;let y;
    if(kind==='calm')y=-Math.sin(a)*16.5;
    else if(kind==='allyWide'){ring.push([cx+x*1.12*mirror,104-Math.sin(a)*17.5]);continue;}
    else if(kind==='allyRound'){ring.push([cx+x*1.25*mirror,104-Math.sin(a)*12.5]);continue;}
    else if(kind==='allyScan'){ring.push([cx+x*1.4*mirror,104-Math.sin(a)*5]);continue;}
    else if(kind==='allyFlat'){ring.push([cx+x*mirror,104-Math.sin(a)*1.7]);continue;}
    else if(kind==='allyReply'){ring.push([cx+x*1.05*mirror,104-Math.sin(a)*11]);continue;}
    else if(kind==='allyBusy')y=Math.max(-3-x*.12,-Math.sin(a)*13);
    else if(kind==='allyRefuse')y=Math.max(-2,-Math.sin(a)*7);
    else if(kind==='allySoft')y=Math.max(-4,-Math.sin(a)*11);
    else if(kind==='allyListen'){ring.push([cx+x*.9*mirror,104-Math.sin(a)*15]);continue;}
    else if(kind==='allyHappy'||kind==='allySleep'){const direction=kind==='allyHappy'?-1:1,outer=i<24,theta=outer?Math.PI*(1-i/23):Math.PI*(i-24)/23;ring.push([cx+Math.cos(theta)*(outer?11:7)*mirror,104+direction*Math.sin(theta)*(outer?9:5)]);continue;}
    else {const upper=-6+x*(kind==='allyAngry'?.56:kind==='allySad'?-.4:.36);const lower=11*Math.sin(Math.PI*u);const blend=Math.pow(Math.sin(Math.PI*u),.38);y=Math.sin(a)>=0?upper:upper+(lower-upper)*blend;}
    ring.push([cx+x*mirror,104+y]);}return ring;}
  const rings={};
  for(const name of ['calm','allyHappy','allySleep','allyFocus','allyAngry','allyWide','allySoft','allyListen','allySad','allyRound','allyScan','allyFlat','allyReply','allyBusy','allyRefuse'])rings[name]=[contour(name,91,1),contour(name,149,-1)];
  rings.allyThink=[contour('allySoft',91,1),contour('calm',149,-1)];
  rings.allyReceive=[contour('calm',91,1),contour('allyHappy',149,-1)];
  rings.allyRecall=[contour('allySleep',91,1),contour('calm',149,-1)];
  Object.assign(rings,window.AllyWorkEyes);
  // 所有表情围绕各自眼心统一放大，保留轮廓混合与左右独立眼神。
  for(const pair of Object.values(rings))pair.forEach((ring,k)=>{const cx=k?149:91;for(const point of ring){point[0]=cx+(point[0]-cx)*eyeScale;point[1]=104+(point[1]-104)*eyeScale;}});
  window.AllyEyeRings=rings;

  window.NeuxBot.createPuppet=function(container,opts){
    const id='ally'+uid++,ch={eyeFamily:rings,defaultEyeRing:rings.calm},material=window.AllyBody.resolve(opts.style);
    const svg=node('svg',{viewBox:'0 0 240 240','aria-hidden':'true',overflow:'visible','pointer-events':'none'},container);
    const defs=node('defs',{},svg);
    const eyeGrad=node('radialGradient',{id:id+'eye',cx:'65%',cy:'25%',r:'90%'},defs);
    [['0%',material.eyes[0]],['45%',material.eyes[1]],['100%',material.eyes[2]]].forEach(([offset,color])=>node('stop',{offset,'stop-color':color},eyeGrad));
    const blushGrad=node('radialGradient',{id:id+'blush'},defs);node('stop',{offset:'0%','stop-color':material.effects.soft,'stop-opacity':'.55'},blushGrad);node('stop',{offset:'100%','stop-color':material.effects.soft,'stop-opacity':'0'},blushGrad);
    const shadowGrad=node('radialGradient',{id:id+'shadow'},defs);node('stop',{offset:'0%','stop-color':material.effects.deep,'stop-opacity':'.20'},shadowGrad);node('stop',{offset:'100%','stop-color':material.effects.deep,'stop-opacity':'0'},shadowGrad);
    for (const [tone, color] of Object.entries(material.effects)) svg.style.setProperty('--companion-effect-'+tone, color);
    const shadow=node('ellipse',{cx:120,cy:204,rx:70,ry:10,fill:`url(#${id}shadow)`},svg);
    const effectsBack=node('g',{},svg);
    const body=node('g',{'class':'ally-body-motion',style:'isolation:isolate'},svg);
    // 身体、手臂与脚均在同一张原画中，逐帧只更新整个身体的变换。
    const image=node('image',{class:'ally-body-image',href:material.images[opts.resolution===512?512:256],x:0,y:0,width:240,height:240,preserveAspectRatio:'xMidYMid meet'},body);
    const setResolution=size=>{if(![256,512].includes(size))return false;const href=material.images[size];if(image.getAttribute('href')!==href)image.setAttribute('href',href);return true;};
    const workTransition=window.AllyWorkMotion.createTransition();
    let renderedWork=window.AllyWorkMotion.idle(),workKey='',previousWork=null;
    const face=node('g',{},body);
    const blushes=[76,164].map(cx=>node('ellipse',{cx,cy:130,rx:17,ry:10,fill:`url(#${id}blush)`,opacity:0},face));
    const eyes=[0,1].map(k=>{const g=node('g',{'class':'ally-eye'},face);const p=node('path',{fill:`url(#${id}eye)`},g);const clip=node('clipPath',{id:id+'eyeclip'+k,clipPathUnits:'userSpaceOnUse'},defs);const clipShape=node('path',{},clip);const highlights=node('g',{'clip-path':`url(#${id}eyeclip${k})`},g);const hi=node('ellipse',{cx:120+(k?29:-29)+3,cy:94,rx:2.5,ry:3.6,fill:'#fff',transform:`rotate(-23 ${120+(k?29:-29)+3} 94)`},highlights);return {g,p,hi,clipShape};});
    const particles=node('g',{'class':'ally-particles','pointer-events':'none'},svg);
    const effects=window.AllyCompanionEffects.create({back:effectsBack,front:svg});
    const halo=node('g',{'pointer-events':'none'},svg);
    const orbitDots=Array.from({length:5},()=>node('circle',{r:4.5,fill:'var(--companion-effect-soft)',opacity:0},halo));
    const sleepTrail=node('g',{'class':'ally-sleep-trail','pointer-events':'none'},svg);
    const zzz=Array.from({length:4},(_,i)=>{const n=node('text',{'font-family':'Georgia,serif','font-weight':600,'font-size':22+i*6,fill:'var(--companion-effect-deep)',opacity:0},sleepTrail);n.textContent='z';return n;});
    let idle=true,lastNow=0,clock=0,emotion=opts.emotion||'02',reduced=false;
    const idleEyes=window.NeuxBot.createIdleEyes();
    const actionPlayer=window.AllyActionEngine.createPlayer();
    let free=true,attentionUntil=0,stateStart=0,stateRevision=0;
    let sparks=[];
    function burst(count=20,kind='spark'){
      if(reduced)return;
      for(let i=0;i<count;i++){
        const n=node('text',{'font-size':kind==='heart'?24+Math.random()*10:16+Math.random()*12,fill:['var(--companion-effect-strong)','var(--companion-effect-soft)','var(--companion-effect-deep)'][i%3],'text-anchor':'middle'},particles);
        n.textContent=kind==='heart'?'♥':i%3?'✦':'·';
        const a=i/count*Math.PI*2+(Math.random()-.5)*.4,s=80+Math.random()*75;
        // 从身体外缘向外散开，避免粒子挤在脸上；结束后从共享时钟自然回收。
        sparks.push({n,x:120+Math.cos(a)*78,y:100+Math.sin(a)*54,vx:Math.cos(a)*s,vy:Math.sin(a)*s*.7-55,born:clock,life:1.8+Math.random()*.6,angle:kind==='heart'?(Math.random()-.5)*24:Math.random()*90});
      }
    }
    function perform(name){
      if(name.startsWith('look:')){actionPlayer.stop(clock);return idleEyes.play(name.slice(5));}
      idleEyes.cancel();
      const definition=actionPlayer.play(name,clock);
      if(!definition)return false;
      for(const effect of definition.effects||[])if(effect.type==='burst')burst(effect.count,effect.kind);
      return true;
    }
    function applyPose(p){
      if(reduced&&sparks.length){sparks.forEach(s=>s.n.remove());sparks=[];}
      const now=performance.now();const dt=lastNow?Math.min((now-lastNow)/1000,.06):0;lastNow=now;clock+=dt;const t=clock;
      const motion=actionPlayer.sample(t,reduced),acting=motion.active;
      const replaces=channel=>acting&&motion.replace.includes(channel);
      let x=replaces('x')?motion.x:p.body.x+motion.x,y=replaces('y')?motion.y:p.body.y+motion.y,rot=replaces('rotate')?motion.rotate:p.body.rotate+motion.rotate;
      let sx=replaces('scaleX')?motion.scaleX:p.body.scale*motion.scaleX,sy=replaces('scaleY')?motion.scaleY:p.body.scale*motion.scaleY;
      const alive=idle&&!reduced&&!['06','41'].includes(emotion);
      const work=window.AllyWorkExpressions[emotion];
      const workActive=work&&(alive||opts.lite)&&!acting&&!reduced;
      const nextWorkKey=`${emotion}:${stateRevision}:${Boolean(workActive)}`;
      if(nextWorkKey!==workKey){
        const previous=previousWork;
        const source=previous?.active?at=>window.AllyWorkGesture(previous.id,at-previous.startedAt):null;
        workTransition.begin(renderedWork,t,source);workKey=nextWorkKey;
      }
      previousWork={id:emotion,startedAt:stateStart,active:Boolean(workActive)};
      const workTarget=workActive?window.AllyWorkGesture(emotion,opts.lite ? .8 : t-stateStart):window.AllyWorkMotion.idle();
      const gesture=renderedWork=reduced?window.AllyWorkMotion.idle():opts.lite?workTarget:workTransition.sample(workTarget,t);
      // 主动动作开始后不启动新的工作节拍；旧工作增量只在短暂过渡期间退出。
      x+=gesture.x;y+=gesture.y;rot+=gesture.rotate;
      sx*=gesture.scaleX;sy*=gesture.scaleY;
      const autonomous=idleEyes.sample(dt,alive&&free&&emotion==='02'&&!acting,now<attentionUntil,reduced);
      if(alive&&!work)rot+=Math.sin(t*1.6)*.65;
      const yaw=(replaces('yaw')?0:(p.body.yaw||0))+gesture.yaw+motion.yaw;
      const cos=Math.cos(yaw);sx*=.78+.22*Math.abs(cos);
      body.setAttribute('transform',`translate(${x} ${y}) translate(120 203) rotate(${rot}) scale(${sx} ${sy}) translate(-120 -203)`);
      face.setAttribute('opacity',clamp((cos-.04)*4,0,1));face.setAttribute('transform',`translate(${Math.sin(yaw)*30} ${gesture.pitch}) translate(120 0) scale(${Math.max(.05,Math.abs(cos))} 1) translate(-120 0)`);
      effects.update({emotion,elapsed:t-stateStart,time:t,reduced,x,y,rotate:rot});
      // 双眼共用柔和反光，随视线和整体转向一起移动。
      const look=(p.left.lookX+p.right.lookX)/2+autonomous.x;
      const lightX=clamp(.5+Math.sin(t*.48+.8)*.17-look*.005-rot*.006-Math.sin(yaw)*.12,.27,.73);
      const lightY=.24+Math.sin(t*.37)*.025;
      eyeGrad.setAttribute('cx',`${lightX*100}%`);eyeGrad.setAttribute('cy',`${lightY*100}%`);
      eyes.forEach((e,k)=>{
        const eye=k?p.right:p.left,cx=120+(k?29:-29),cy=104;
        let ring=eye.ring||ch.defaultEyeRing[k];
        if(autonomous.smile>.0001)ring=ring.map((v,i)=>v.map((n,axis)=>n+(ch.eyeFamily.allyHappy[k][i][axis]-n)*autonomous.smile));
        const eyePath=path(ring);e.p.setAttribute('d',eyePath);e.clipShape.setAttribute('d',eyePath);
        const xs=ring.map(v=>v[0]),ys=ring.map(v=>v[1]),left=Math.min(...xs),top=Math.min(...ys),width=Math.max(...xs)-left,height=Math.max(...ys)-top;
        const hx=left+width*clamp(lightX,.38,.62),hy=top+height*(.36+(lightY-.24)*.5);
        e.hi.setAttribute('cx',hx);e.hi.setAttribute('cy',hy);
        e.hi.setAttribute('rx',clamp(width*.16,2.4,3.4));e.hi.setAttribute('ry',clamp(height*.14,2.6,4.2));
        e.hi.setAttribute('transform',`rotate(${(lightX-.5)*-110} ${hx} ${hy})`);
        const gazeGain=work ? .85 : .45;
        const gx=clamp(eye.lookX*gazeGain*(1-gesture.lookMix)+gesture.lookX*gesture.lookMix+autonomous.x,-15,15);
        const gy=clamp(eye.lookY*(work ? .8 : .5)*(1-gesture.lookMix)+gesture.lookY*gesture.lookMix+autonomous.y,-11,11);
        const opening=eye.open*(1-autonomous.soft)*(1-(k?autonomous.rightWink:autonomous.leftWink)*.96);
        const wide=k?autonomous.rightWide:autonomous.leftWide,tall=k?autonomous.rightTall:autonomous.leftTall;
        const tilt=k?autonomous.rightTilt:autonomous.leftTilt;
        e.g.setAttribute('transform',`translate(${gx+eye.x} ${gy+eye.y}) translate(${cx} ${cy}) rotate(${eye.rotate+tilt}) scale(${eye.scaleX*(1+wide)} ${Math.max(.045,opening*eye.scaleY*(1+tall))}) translate(${-cx} ${-cy})`);
        let area=0;for(let i=0;i<ring.length;i++){const a=ring[i],b=ring[(i+1)%ring.length];area+=a[0]*b[1]-b[0]*a[1];}
        const thick=Math.abs(area)/2/Math.max(1,Math.max(...ring.map(v=>v[0]))-Math.min(...ring.map(v=>v[0])));
        // 高光随实际睁眼厚度淡出，不能按表情硬隐藏；位置向瞳孔内部收拢以免被眼形裁掉。
        const highlight=clamp((thick/eyeScale*opening*eye.scaleY*(1+tall)-5)/3,0,1);
        e.hi.setAttribute('opacity',highlight*.98);
      });
      blushes.forEach(e=>e.setAttribute('opacity',(p.face.blush||0)*.35));
      shadow.setAttribute('transform',`translate(${x*.5} 0) translate(120 204) scale(${clamp(1+y*.009,.55,1.1)} 1) translate(-120 -204)`);shadow.setAttribute('opacity',clamp(1+y*.012,.3,1));
      orbitDots.forEach((n,i)=>{const a=(reduced?0:t*2)+i*Math.PI*2/5;n.setAttribute('cx',120+Math.cos(a)*110);n.setAttribute('cy',5+Math.sin(a)*24);n.setAttribute('opacity',p.body.orbit?(.55+Math.sin(a)*.2):0);});
      zzz.forEach((n,i)=>{const phase=((reduced?.5:t)*.24+i/4)%1;n.setAttribute('x',185+phase*100+Math.sin(phase*Math.PI)*12);n.setAttribute('y',50-phase*130);n.setAttribute('transform',`rotate(${-8+phase*18} ${185+phase*100} ${50-phase*130})`);n.setAttribute('opacity',p.body.zzz?Math.sin(phase*Math.PI)*.9:0);});
      sparks=sparks.filter(s=>{const a=t-s.born;if(a>s.life){s.n.remove();return false;}s.n.setAttribute('transform',`translate(${s.x+s.vx*a} ${s.y+s.vy*a+18*a*a}) rotate(${s.angle+a*18})`);s.n.setAttribute('opacity',Math.sin(Math.PI*a/s.life));return true;});
    }
    return {applyPose,burst,perform,setResolution,cancelAction:()=>{actionPlayer.stop(clock);idleEyes.cancel();},setEmotion:id=>{emotion=id;stateStart=clock;stateRevision++;},setIdle:value=>idle=value,setReduced:value=>reduced=value,setFree:value=>free=value,attend:()=>attentionUntil=performance.now()+1800,signature:()=>perform('sway'),signatureComplete:true,destroy:()=>svg.remove()};
  };
})();
