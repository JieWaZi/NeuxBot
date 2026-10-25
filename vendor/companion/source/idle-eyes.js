/* 眼神与身体动作分开编排；八向张望和双眼独立形变共用渲染时钟。 */
(() => {
  const directions = {
    left: [-8, 0], right: [8, 0], up: [0, -7], down: [0, 6],
    'up-left': [-7, -6], 'up-right': [7, -6],
    'down-left': [-7, 5], 'down-right': [7, 5]
  };
  const gestures = [...Object.keys(directions), 'curious-left', 'curious-right', 'wide', 'soft', 'smile', 'wink-left', 'wink-right'];
  window.NeuxBot = window.NeuxBot || {};
  window.NeuxBot.idleEyeGestures = gestures;
  window.NeuxBot.createIdleEyes = (random = Math.random) => {
    const rest = { x:0, y:0, leftWide:0, rightWide:0, leftTall:0, rightTall:0,
      leftTilt:0, rightTilt:0, soft:0, smile:0, leftWink:0, rightWink:0 };
    const state = { ...rest };
    let time=0, next=.9, beat=null, bag=[], previous=null, wasEnabled=false;
    const ease = x => { x=Math.max(0,Math.min(1,x)); return x*x*(3-2*x); };
    function choose() {
      if (!bag.length) {
        bag=[...gestures];
        for (let i=bag.length-1;i>0;i--) { const j=Math.floor(random()*(i+1)); [bag[i],bag[j]]=[bag[j],bag[i]]; }
        if (bag[bag.length-1]===previous) [bag[0],bag[bag.length-1]]=[bag[bag.length-1],bag[0]];
      }
      previous=bag.pop();
      return previous;
    }
    function pose(type, strength, age, still=false) {
      const target={...rest}, direction=directions[type];
      if (direction) {
        target.x=direction[0]*strength; target.y=direction[1]*strength;
        // 视线侧面的眼睛稍窄，保留面部转向感，避免两只眼睛机械平移。
        const turn=direction[0]/8*strength;
        target.leftWide=.035*strength+turn*.09;
        target.rightWide=.035*strength-turn*.09;
        target.leftTall=target.rightTall=(direction[1]<0?.1:-.035)*strength;
      } else if (type==='curious-left'||type==='curious-right') {
        const left=type==='curious-left';
        target.x=(left?-2:2)*strength; target.y=-2*strength;
        target.leftWide=(left?.2:-.12)*strength;
        target.rightWide=(left?-.12:.2)*strength;
        target.leftTall=(left?.24:-.32)*strength;
        target.rightTall=(left?-.32:.24)*strength;
        target.leftTilt=(left?-8:5)*strength;
        target.rightTilt=(left?5:-8)*strength;
      } else if (type==='wide') {
        target.leftWide=target.rightWide=.22*strength;
        target.leftTall=target.rightTall=.24*strength;
        target.y=-2*strength;
      } else if (type==='soft') { target.soft=.38*strength; target.y=1.5*strength; }
      else if (type==='smile') target.smile=strength;
      else if (type==='wink-left'||type==='wink-right') {
        target[type==='wink-left'?'leftWink':'rightWink']=still?1:ease((age-.45)/.07)*ease((.85-age)/.1);
        target.x=(type==='wink-left'?1.5:-1.5)*strength;
      }
      return target;
    }
    return {
      play(type) {
        if (!gestures.includes(type)) return false;
        beat={type,start:time,end:time+2.7,forced:true};
        return true;
      },
      cancel() { beat=null; next=time+.9; },
      sample(dt, enabled, attentive=false, still=false) {
        // 减少动态时，点按保留眼神的静态姿态，不启动自动张望。
        if (still) {
          Object.assign(state,rest,beat?.forced?pose(beat.type,1,.6,true):{});
          return state;
        }
        time+=dt;
        if (!enabled) {
          beat=null; next=time+.9; wasEnabled=false;
          Object.assign(state,rest);
          return state;
        }
        if (!wasEnabled) { next=time+.9; wasEnabled=true; }
        let target={...rest};
        if (attentive && !beat?.forced) { beat=null; next=time+1.1; }
        else {
          if (beat && time>=beat.end) { beat=null; next=time+1+random()*1.6; }
          if (!beat && time>=next) {
            const duration=1.8+random()*.9;
            beat={type:choose(),start:time,end:time+duration};
          }
          if (beat) {
            const age=time-beat.start;
            const strength=ease(age/.3)*ease((beat.end-time)/.45);
            target=pose(beat.type,strength,age);
          }
        }
        for (const key of Object.keys(state)) {
          const speed=key.endsWith('Wink')?35:10;
          state[key]+=(target[key]-state[key])*(1-Math.exp(-dt*speed));
        }
        return state;
      }
    };
  };
})();
