/*
 * NeuxBot 的动作库。
 *
 * 新动作只需在这里增加：元数据、总时长和关键帧。`pose` 可以只写变化项，
 * 引擎会继承上一帧；`ease` 描述“进入当前帧”的缓动曲线。
 */
(() => {
  const action=(name,emotion,speech,duration,frames,extra={})=>({name,emotion,speech,duration,frames,...extra});
  const frame=(at,pose={},ease='easeInOutCubic')=>({at,pose,ease});

  AllyActionEngine.register([
    action('sway','10','轻轻晃一下，回应你。',2.4,[
      frame(0),
      frame(.16,{x:2,rotate:-2,scaleX:1.015,scaleY:.985},'easeOutQuint'),
      frame(.38,{x:-1,y:-2,rotate:2,scaleX:.99,scaleY:1.01},'easeOutQuint'),
      frame(.67,{rotate:-2.5}),
      frame(.96,{rotate:2}),
      frame(1.25,{rotate:-2}),
      frame(1.56,{rotate:1.5}),
      frame(1.88,{x:0,y:0,rotate:-1}),
      frame(2.16,{rotate:0},'easeInCubic'),
      frame(2.4,{scaleX:1,scaleY:1},'easeOutQuint')
    ],{reduced:{rotate:-1}}),

    action('jump','13','一、二、三，起跳！',1.55,[
      frame(0),
      frame(.16,{y:4,scaleX:1.09,scaleY:.88},'easeOutQuint'),
      frame(.3,{y:-5,scaleX:.95,scaleY:1.08},'easeInCubic'),
      frame(.68,{y:-53,scaleX:.98,scaleY:1.04},'easeOutCubic'),
      frame(1.02,{y:-15,scaleX:1.01,scaleY:.99},'easeInCubic'),
      frame(1.19,{y:4,scaleX:1.11,scaleY:.87},'easeInCubic'),
      frame(1.36,{y:-3,scaleX:.98,scaleY:1.025},'easeOutQuint'),
      frame(1.55,{y:0,scaleX:1,scaleY:1},'easeOutQuint')
    ],{replace:['y','scaleX','scaleY'],reduced:{y:-3,scaleX:1.01,scaleY:.99}}),

    action('walk','03','走两步，再回到你身边。',3.4,[
      frame(0),
      frame(.18,{x:-3,y:-3,rotate:-2},'easeOutQuint'),
      frame(.48,{x:-18,y:0,rotate:3}),
      frame(.78,{x:-31,y:-5,rotate:-3}),
      frame(1.08,{x:-17,y:0,rotate:3}),
      frame(1.38,{x:0,y:-5,rotate:-3}),
      frame(1.7,{x:18,y:0,rotate:3}),
      frame(2,{x:31,y:-5,rotate:-3}),
      frame(2.3,{x:18,y:0,rotate:3}),
      frame(2.62,{x:2,y:-4,rotate:-2}),
      frame(2.94,{x:-5,y:0,rotate:1.5}),
      frame(3.18,{x:1,y:-1,rotate:-.5},'easeOutQuint'),
      frame(3.4,{x:0,y:0,rotate:0},'easeOutQuint')
    ],{replace:['x','y','rotate'],reduced:{rotate:2}}),

    action('spin','10','转一圈，换个好心情。',2.1,[
      frame(0),
      frame(.18,{y:3,rotate:-4,scaleX:1.04,scaleY:.96,yaw:-.12},'easeOutQuint'),
      frame(.5,{y:-8,rotate:3,scaleX:.98,scaleY:1.02,yaw:.9},'easeInCubic'),
      frame(1.05,{y:-15,rotate:0,yaw:3.2},'linear'),
      frame(1.58,{y:-8,rotate:-2,yaw:5.55},'linear'),
      frame(1.88,{y:2,rotate:3,scaleX:1.06,scaleY:.94,yaw:6.283},'easeOutCubic'),
      frame(2.1,{y:0,rotate:0,scaleX:1,scaleY:1,yaw:6.283},'easeOutQuint')
    ],{replace:['y','yaw'],reduced:{rotate:3,yaw:.14}}),

    action('love','10','嘿嘿，再摸一下嘛。',2.35,[
      frame(0),
      frame(.25,{y:2,rotate:-3,scaleX:1.02,scaleY:.98},'easeOutQuint'),
      frame(.58,{y:-2,rotate:4,scaleX:.98,scaleY:1.02}),
      frame(.92,{y:1,rotate:-4,scaleX:1.025,scaleY:.975}),
      frame(1.28,{y:-2,rotate:3.5,scaleX:.985,scaleY:1.015}),
      frame(1.64,{y:1,rotate:-2.5,scaleX:1.018,scaleY:.982}),
      frame(1.98,{y:0,rotate:1,scaleX:.995,scaleY:1.005},'easeOutQuint'),
      frame(2.35,{rotate:0,scaleX:1,scaleY:1},'easeOutQuint')
    ],{reduced:{rotate:2,scaleX:1.01,scaleY:.99},effects:[{type:'burst',count:14,kind:'heart'}]}),

    action('party','33','每一件小事，都值得庆祝。',2.8,[
      frame(0),
      frame(.14,{y:4,scaleX:1.09,scaleY:.89},'easeOutQuint'),
      frame(.32,{y:-8,scaleX:.96,scaleY:1.06},'easeInCubic'),
      frame(.72,{y:-46,rotate:-4},'easeOutCubic'),
      frame(1.06,{y:3,rotate:3,scaleX:1.11,scaleY:.87},'easeInCubic'),
      frame(1.24,{y:-5,rotate:-2,scaleX:.97,scaleY:1.04},'easeOutQuint'),
      frame(1.62,{y:-38,rotate:4},'easeOutCubic'),
      frame(1.98,{y:3,rotate:-2,scaleX:1.09,scaleY:.9},'easeInCubic'),
      frame(2.2,{y:-3,rotate:1,scaleX:.99,scaleY:1.02},'easeOutQuint'),
      frame(2.52,{y:0,rotate:0,scaleX:1,scaleY:1},'easeInCubic'),
      frame(2.8,{},'easeOutQuint')
    ],{replace:['y','rotate','scaleX','scaleY'],reduced:{y:-2},effects:[{type:'burst',count:30,kind:'spark'}]})
  ]);

})();
