import { useEffect, useRef } from "react";
import { useReducedMotion } from "motion/react";

type Spark = {
  x: number;
  y: number;
  radius: number;
  velocity: number;
  sway: number;
  phase: number;
  life: number;
  span: number;
};

/** 沿用输入框的装饰层；最高档输入时增强粒子，隐藏或减少动态时停止绘制。 */
export function ChatEffortEffects({ active }: { active: boolean }) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const reducedMotion = useReducedMotion();
  useEffect(() => {
    const canvas = canvasRef.current;
    const surface = canvas?.closest("[data-chat-composer-surface]");
    if (!active || reducedMotion || !canvas || !surface) return;
    const context = canvas.getContext("2d");
    if (!context) return;
    let width = 0,
      height = 0,
      frame = 0,
      lastTime = 0,
      due = 0,
      energy = 0,
      pulse = 0,
      speed = 1;
    let color = "";
    let intersecting = true;
    const sparks: Spark[] = [];
    const readColor = () => {
      color = getComputedStyle(canvas).color;
    };
    const resize = () => {
      const rect = canvas.getBoundingClientRect();
      const pixelRatio = Math.min(window.devicePixelRatio || 1, 2);
      width = rect.width;
      height = rect.height;
      canvas.width = Math.round(width * pixelRatio);
      canvas.height = Math.round(height * pixelRatio);
      context.setTransform(pixelRatio, 0, 0, pixelRatio, 0, 0);
    };
    const spawn = (initial = false) => {
      sparks.push({
        x: Math.random() * width,
        y: initial ? height * (0.2 + Math.random() * 0.8) : height + 3,
        radius: 0.9 + Math.random() * 1.1,
        velocity: -(7 + Math.random() * 9),
        sway: (Math.random() - 0.5) * 10,
        phase: Math.random() * Math.PI * 2,
        life: initial ? Math.random() * 1.2 : 0,
        span: 2.4 + Math.random() * 2.4,
      });
    };
    const tick = (now: number) => {
      const elapsed = Math.min((now - lastTime) / 1000, 0.05);
      lastTime = now;
      energy *= Math.exp(-elapsed / 0.8);
      pulse *= Math.exp(-elapsed / 0.16);
      speed += (1 + energy * 6 - speed) * (1 - Math.exp(-elapsed / 0.15));
      due += elapsed;
      while (due > 0.14) {
        due -= 0.14;
        if (sparks.length < 30) spawn();
      }
      context.clearRect(0, 0, width, height);
      context.fillStyle = color;
      context.shadowColor = color;
      context.shadowBlur = 6 + energy * 10 + pulse * 6;
      for (let index = sparks.length - 1; index >= 0; index--) {
        const spark = sparks[index];
        if (!spark) continue;
        spark.life += elapsed;
        if (spark.life > spark.span) {
          sparks.splice(index, 1);
          continue;
        }
        spark.y += spark.velocity * elapsed * speed;
        if (spark.y < -4) {
          spark.y = height + 3;
          spark.x = Math.random() * width;
        }
        const twinkle =
          0.7 + 0.3 * Math.sin((now / 160) * (1 + energy) + spark.phase);
        const edge = Math.min(
          1,
          Math.max(0, spark.y / 14),
          Math.max(0, (height - spark.y) / 14),
        );
        context.globalAlpha =
          Math.min(
            1,
            Math.sin((spark.life / spark.span) * Math.PI) *
              (0.9 + energy * 0.25) *
              twinkle,
          ) * edge;
        context.beginPath();
        context.arc(
          spark.x +
            Math.sin((now / 900) * (1 + energy * 0.8) + spark.phase) *
              spark.sway,
          spark.y,
          spark.radius * twinkle * (1 + energy * 0.35),
          0,
          Math.PI * 2,
        );
        context.fill();
      }
      frame = requestAnimationFrame(tick);
    };
    const syncVisibility = () => {
      if (document.hidden || !intersecting || width <= 0 || height <= 0) {
        cancelAnimationFrame(frame);
        frame = 0;
        return;
      }
      if (!frame) {
        if (!sparks.length) {
          for (let index = 0; index < 26; index++) spawn(true);
        }
        lastTime = performance.now();
        frame = requestAnimationFrame(tick);
      }
    };
    const onInput = (event: Event) => {
      if (
        !(event.target instanceof Element) ||
        !event.target.closest("[data-chat-composer-input]")
      )
        return;
      energy = Math.min(1.6, energy + 0.22);
      pulse = 1;
    };
    readColor();
    resize();
    const resizeObserver = new ResizeObserver(() => {
      resize();
      syncVisibility();
    });
    resizeObserver.observe(canvas);
    const intersectionObserver =
      typeof IntersectionObserver === "undefined"
        ? undefined
        : new IntersectionObserver(([entry]) => {
            intersecting = Boolean(entry?.isIntersecting);
            syncVisibility();
          });
    intersectionObserver?.observe(canvas);
    const themeObserver = new MutationObserver(readColor);
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });
    document.addEventListener("visibilitychange", syncVisibility);
    surface.addEventListener("input", onInput);
    syncVisibility();
    return () => {
      cancelAnimationFrame(frame);
      resizeObserver.disconnect();
      intersectionObserver?.disconnect();
      themeObserver.disconnect();
      document.removeEventListener("visibilitychange", syncVisibility);
      surface.removeEventListener("input", onInput);
      context.clearRect(0, 0, width, height);
    };
  }, [active, reducedMotion]);
  return (
    <div
      aria-hidden
      className="composer-effort-effect"
      data-active={active || undefined}
    >
      <span className="composer-effort-glow" />
      {active && !reducedMotion ? (
        <canvas
          ref={canvasRef}
          className="text-control-highlight absolute inset-0 size-full"
        />
      ) : null}
    </div>
  );
}
