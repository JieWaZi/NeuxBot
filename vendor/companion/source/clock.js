/* Pose clock. Transitions follow bloub's rule: keep sampling the outgoing
 * state, and freeze the composite only when a change lands inside a fade.
 * Eye contours blend point-by-point on the shared 48-point loop. */
(() => {
  const clamp = (v, a, b) => Math.max(a, Math.min(b, v));
  const ease = (t) => 1 - Math.pow(1 - clamp(t, 0, 1), 5);
  const rand = (a, b) => a + Math.random() * (b - a);
  const eye = () => ({ x: 0, y: 0, scaleX: 1, scaleY: 1, rotate: 0, open: 1, lookX: 0, lookY: 0 });
  const body = () => ({ x: 0, y: 0, scale: 1, rotate: 0, yaw: 0, breathe: 0.008, zzz: 0, orbit: 0 });

  function mixPart(a, b, k) {
    const out = {};
    for (const key of new Set([...Object.keys(a), ...Object.keys(b)])) {
      const vb = b[key];
      const va = a[key];
      out[key] = typeof vb === "number" && typeof va === "number" ? va + (vb - va) * k : vb;
    }
    return out;
  }
  function mixPose(a, b, k) {
    return {
      body: mixPart(a.body, b.body, k),
      left: mixPart(a.left, b.left, k),
      right: mixPart(a.right, b.right, k),
      face: mixPart(a.face, b.face, k),
    };
  }
  function mixRing(a, b, k) {
    return a.map((point, index) => [
      point[0] + (b[index][0] - point[0]) * k,
      point[1] + (b[index][1] - point[1]) * k,
    ]);
  }
  function basePose(def) {
    const both = { ...eye(), ...def.eyes?.both };
    return {
      body: { ...body(), ...def.body },
      left: { ...both, ...def.eyes?.left },
      right: { ...both, ...def.eyes?.right },
      face: { blush: 0, ...def.face },
    };
  }
  function applyFragment(pose, fragment) {
    if (!fragment) return pose;
    if (fragment.body) Object.assign(pose.body, fragment.body);
    if (fragment.face) Object.assign(pose.face, fragment.face);
    if (fragment.eyes?.both) {
      Object.assign(pose.left, fragment.eyes.both);
      Object.assign(pose.right, fragment.eyes.both);
    }
    if (fragment.eyes?.left) Object.assign(pose.left, fragment.eyes.left);
    if (fragment.eyes?.right) Object.assign(pose.right, fragment.eyes.right);
    return pose;
  }
  function wave(kind, anim, t, seed) {
    if (kind === "sine") return anim.amp * Math.sin((Math.PI * 2 * t) / (anim.period || 2000) + (anim.phase || 0));
    if (kind === "pulse") return anim.amp * 0.5 * (1 - Math.cos((Math.PI * 2 * t) / (anim.period || 1000) + (anim.phase || 0)));
    if (kind === "scan") {
      const span = anim.period || 800;
      const p = ((t + (anim.phaseMs || 0)) % span) / span;
      return anim.amp * (p < 0.5 ? p * 4 - 1 : 3 - p * 4);
    }
    if (kind === "glance") {
      const span = anim.period || 3600;
      const p = ((t + (anim.phaseMs || 0)) % span) / span;
      return anim.amp * Math.tanh(2.2 * Math.sin(p * Math.PI * 2));
    }
    if (kind === "jitter") {
      const s = (t / 1000) * (anim.speed || 8);
      let v = (Math.sin(s * 2.7 + seed) + Math.sin(s * 4.9 + seed * 1.7) + Math.sin(s * 8.3 + seed * 3.1)) / 3 * anim.amp;
      if (anim.decay) v *= clamp(1 - t / anim.decay, 0, 1);
      return v;
    }
    if (kind === "blink") {
      const interval = anim.interval || 1600;
      const dur = anim.dur || 400;
      const p = (t + (anim.phaseMs || 0) + seed * 80) % interval;
      return p >= dur ? 0 : -Math.sin(Math.PI * (p / dur));
    }
    return 0;
  }
  function applyAnims(pose, def, t, seed) {
    for (const anim of def.anims || []) {
      const value = wave(anim.type, anim, t, seed);
      const targets = anim.target === "eyes" ? [pose.left, pose.right]
        : anim.target === "left" ? [pose.left]
        : anim.target === "right" ? [pose.right]
        : anim.target === "body" ? [pose.body]
        : anim.target === "face" ? [pose.face] : [];
      for (const target of targets) {
        if (anim.prop === "scale") {
          if (target === pose.body) target.scale += value;
          else { target.scaleX += value; target.scaleY += value; }
        } else if (anim.prop in target) target[anim.prop] += value;
      }
    }
  }
  function sampleDef(def, elapsed, now, seed) {
    const pose = basePose(def);
    const frames = def.sequence?.frames;
    if (frames?.length) {
      const last = frames[frames.length - 1];
      const settle = def.sequence.settle;
      const hold = settle === "hold" || (settle && settle.next);
      if (elapsed >= last.at) {
        if (hold) applyFragment(pose, last);
      } else {
        let index = frames.findIndex((frame) => frame.at > elapsed);
        if (index < 0) index = frames.length - 1;
        const next = frames[index];
        const prev = frames[Math.max(0, index - 1)];
        const span = Math.max(1, next.at - prev.at);
        const keyed = mixPose(
          applyFragment(basePose(def), prev),
          applyFragment(basePose(def), next),
          ease((elapsed - prev.at) / span),
        );
        pose.body = keyed.body;
        pose.left = keyed.left;
        pose.right = keyed.right;
        pose.face = keyed.face;
      }
    }
    applyAnims(pose, def, elapsed, seed);
    const breathe = pose.body.breathe || 0;
    if (breathe) {
      const phase = now / 1000;
      pose.body.scale += breathe * Math.sin(phase * 1.75);
      pose.body.y += breathe * 36 * Math.sin(phase * 1.75 + 0.5);
    }
    if (def.gaze !== false) {
      const w = now / 1000;
      pose.left.lookX += 1.2 * Math.sin(0.37 * w);
      pose.right.lookX += 1.2 * Math.sin(0.37 * w + 0.8);
      pose.left.lookY += 0.7 * Math.sin(0.51 * w);
      pose.right.lookY += 0.7 * Math.sin(0.51 * w + 0.6);
    }
    return pose;
  }

  const loops = new Set();
  let raf = 0;
  function pump(now) {
    raf = 0;
    loops.forEach((engine) => engine._frame(now));
    if (loops.size) raf = requestAnimationFrame(pump);
  }

  function Clock(target, opts) {
    opts = opts || {};
    const element = typeof target === "string" ? document.querySelector(target) : target;
    if (!element) throw new Error("NeuxBot.create：找不到容器");
    const catalog = window.AllyExpressions || {};
    const rings = window.AllyEyeRings || {};
    const pair = (name) => rings[name] || rings.calm;
    this.ball = window.NeuxBot.createPuppet(element, opts);
    this._events = {};
    this._catalog = catalog;
    this._pair = pair;
    this._seed = Math.random() * 20;
    this._id = "02";
    this._started = 0;
    this._blendFrom = null;
    this._blendAt = -1e9;
    this._blendMs = 0;
    this._frozen = null;
    this._slot = "calm";
    this._poolAt = 0;
    this._ringFrom = pair("calm");
    this._ringAt = -1e9;
    this._ringMs = 1;
    this._blinkAt = -1e9;
    this._blinkNext = Infinity;
    this._active = false;
    this._shown = false;
    this.setEmotion(opts.emotion || "02", { auto: true });
    if (opts.autostart !== false) this.setActive(true);
    else this.renderStatic();
  }

  Clock.prototype = {
    on(name, cb) {
      (this._events[name] = this._events[name] || []).push(cb);
      return this;
    },
    _emit(name, payload) {
      for (const cb of this._events[name] || []) cb(payload);
    },
    _def(id) {
      return this._catalog[id] || this._catalog["02"];
    },
    _rings(now, settled) {
      const target = this._pair(this._slot);
      const k = settled ? 1 : ease((now - this._ringAt) / this._ringMs);
      if (k >= 1) return target;
      return [mixRing(this._ringFrom[0], target[0], k), mixRing(this._ringFrom[1], target[1], k)];
    },
    _retargetRings(slot, ms, now) {
      const current = this._rings(now);
      this._ringFrom = current;
      this._slot = slot;
      this._ringAt = now;
      this._ringMs = ms;
    },
    _pose(now, skipBlend) {
      const def = this._def(this._id);
      const elapsed = now - this._started;
      let pose = sampleDef(def, elapsed, now, this._seed);
      const k = skipBlend ? 1 : ease((now - this._blendAt) / this._blendMs);
      if (k < 1 && this._blendFrom) {
        const from = this._frozen || sampleDef(this._blendFrom.def, now - this._blendFrom.started, now, this._seed);
        pose = mixPose(from, pose, k);
      }
      const blink = now - this._blinkAt;
      const lid = blink >= 0 && blink < 220 ? 1 - 0.92 * Math.sin((Math.PI * blink) / 220) : 1;
      const open = (def.openness == null ? 1 : def.openness) * lid;
      pose.left.open *= open;
      pose.right.open *= open;
      const shaped = this._rings(now, skipBlend);
      pose.left.ring = shaped[0];
      pose.right.ring = shaped[1];
      return pose;
    },
    _frame(now) {
      const def = this._def(this._id);
      const seq = def.sequence;
      const last = seq?.frames?.[seq.frames.length - 1];
      if (seq?.settle?.next && last && now - this._started >= last.at) {
        this.setEmotion(seq.settle.next, { auto: true });
        return;
      }
      if (this._active && def.pool.length > 1 && now >= this._poolAt) {
        const index = Math.floor(rand(0, def.pool.length));
        this._retargetRings(def.pool[index % def.pool.length], def.poolMs ? 2400 / (def.poolSpeed || 6) : 420, now);
        const span = def.poolMs || [4000, 7000];
        this._poolAt = now + rand(span[0], span[1]);
      }
      if (this._active && def.blinkMs && now >= this._blinkNext) {
        this._blinkAt = now;
        this._blinkNext = now + rand(def.blinkMs[0], def.blinkMs[1]);
      }
      this.ball.applyPose(this._pose(now, false));
    },
    setEmotion(id, options) {
      const def = this._catalog[id] ? this._catalog[id] : this._catalog["02"];
      if (!def) return false;
      const now = performance.now();
      const previous = this._shown ? this._id : "";
      this._shown = true;
      const fading = now - this._blendAt < this._blendMs && this._blendFrom;
      this._frozen = fading ? this._pose(now, false) : null;
      if (!fading && previous) this._blendFrom = { def: this._def(previous), started: this._started };
      else if (fading) this._blendFrom = { def: null, started: now };
      this._id = def.id;
      this._started = now;
      this._blendAt = now;
      this._blendMs = previous && previous !== def.id ? (def.transition || 480) : 0;
      if (!this._blendMs) this._blendFrom = null;
      this._retargetRings(def.pool[0] || "calm", 2400 / (def.poolSpeed || 6), now);
      const span = def.poolMs || [4000, 7000];
      this._poolAt = now + rand(span[0], span[1]);
      this._blinkNext = def.blinkMs ? now + rand(def.blinkMs[0], def.blinkMs[1]) : Infinity;
      if (previous && previous !== def.id && def.blinkMs) this._blinkAt = now;
      this._emit("change", { id: def.id, auto: !!(options && options.auto) });
      if (this._active && def.celebrate && this.ball.burst) this.ball.burst(16);
      if (!this._active) this.renderStatic();
      return true;
    },
    setActive(on) {
      if (on === this._active) return;
      this._active = on;
      if (on) {
        loops.add(this);
        if (!raf) raf = requestAnimationFrame(pump);
      } else loops.delete(this);
    },
    renderStatic() {
      this.ball.applyPose(this._pose(performance.now(), true));
    },
    destroy() {
      this.setActive(false);
      this._events = {};
      this.ball.destroy();
    },
  };

  window.NeuxBot = window.NeuxBot || {};
  window.NeuxBot.create = (element, opts) => new Clock(element, opts);
})();
