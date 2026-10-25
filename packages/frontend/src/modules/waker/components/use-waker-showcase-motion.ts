import { useEffect, useRef, type RefObject } from "react";

type CardPose = {
  rotateX: number;
  rotateY: number;
};

const restingPose: CardPose = {
  rotateX: 0,
  rotateY: 0,
};

const poseKeys = Object.keys(restingPose) as (keyof CardPose)[];
const smoothstep = (value: number) => {
  const progress = Math.min(1, Math.max(0, value));
  return progress * progress * (3 - 2 * progress);
};

export function useWakerShowcaseMotion(
  cardRef: RefObject<HTMLElement | null>,
  closing: boolean,
) {
  const motion = useRef<((pose?: CardPose) => void) | null>(null);

  useEffect(() => {
    const node = cardRef.current;
    if (!node || closing) return;
    if (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) return;

    const tilter = node.querySelector<HTMLElement>(".ally-holo-card__tilter");
    let frame = 0;
    let showcase = true;
    const startedAt = performance.now();
    let lastFrame = startedAt;
    const current = { ...restingPose };
    let target = restingPose;

    const tick = (now: number) => {
      frame = 0;
      const elapsed = now - startedAt;
      // 展开与展示共用时间轴，幅度渐入渐出，不在翻面结束后重新起步。
      if (showcase && elapsed < 2800) {
        const strength =
          smoothstep(elapsed / 420) * smoothstep((2800 - elapsed) / 600);
        const radian = elapsed / 400;
        target = {
          rotateX: Math.sin(radian) * 16 * strength,
          rotateY: Math.cos(radian) * 16 * strength,
        };
      } else if (showcase) {
        showcase = false;
        target = restingPose;
      }

      // 按真实帧间隔平滑追随；指针事件只更新目标，同一帧只写一次样式。
      const blend = 1 - Math.exp(-Math.min(now - lastFrame, 64) / 80);
      lastFrame = now;
      let settled = true;
      for (const key of poseKeys) {
        const delta = target[key] - current[key];
        if (Math.abs(delta) > 0.01) settled = false;
        const next =
          Math.abs(delta) <= 0.01 ? target[key] : current[key] + delta * blend;
        if (next === current[key]) continue;
        current[key] = next;
      }
      // 只更新独立合成层，避免在根节点改变量让整个头像 SVG 重算样式与重绘渐变。
      if (tilter)
        tilter.style.transform = `rotateY(${current.rotateX}deg) rotateX(${current.rotateY}deg)`;
      if (showcase || !settled) frame = requestAnimationFrame(tick);
    };

    motion.current = (pose = restingPose) => {
      showcase = false;
      target = pose;
      if (!frame) {
        lastFrame = performance.now();
        frame = requestAnimationFrame(tick);
      }
    };
    frame = requestAnimationFrame(tick);
    return () => {
      cancelAnimationFrame(frame);
      motion.current = null;
    };
  }, [cardRef, closing]);

  return motion;
}
