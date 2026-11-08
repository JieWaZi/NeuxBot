import {
  animate,
  motion,
  useMotionValue,
  useReducedMotion,
} from "motion/react";
import {
  useContext,
  useLayoutEffect,
  useRef,
  type ReactNode,
  type RefObject,
} from "react";

import { ContentVisibility } from "@/shared/lib/content-visibility";
import { readLayoutMotionTokens } from "@/shared/lib/ui-motion-tokens";

/** 高亮只同步选项的真实布局；业务选择仍由 SegmentedControl 的受控值持有。 */
export function SegmentedControlHighlight({
  optionRefs,
  value,
  layoutKey,
  className,
  children,
}: {
  optionRefs: RefObject<Map<string, HTMLButtonElement>>;
  value: string;
  layoutKey: string;
  className: string;
  children: ReactNode;
}) {
  const visible = useContext(ContentVisibility);
  const highlightRef = useRef<HTMLDivElement>(null);
  const reducedMotion = useReducedMotion();
  const left = useMotionValue(0);
  const right = useMotionValue(0);
  const top = useMotionValue(0);
  const bottom = useMotionValue(0);
  const width = useMotionValue(0);
  const opacity = useMotionValue(0);
  const clipPath = useMotionValue("inset(50%)");
  const previous = useRef<{
    value: string;
    left: number;
    right: number;
    top: number;
    bottom: number;
    width: number;
  } | null>(null);

  useLayoutEffect(() => {
    const update = () => {
      clipPath.set(
        `inset(${top.get()}px ${Math.max(0, width.get() - right.get())}px ${bottom.get()}px ${Math.max(0, left.get())}px round var(--segmented-highlight-radius, var(--radius-control-inner)))`,
      );
    };
    // StrictMode 重放布局 Effect 时恢复订阅，并用已测量的边界同步裁切。
    const unsubscribe = [top, right, bottom, left, width].map((edge) =>
      edge.on("change", update),
    );
    update();
    return () => unsubscribe.forEach((stop) => stop());
  }, [clipPath, top, right, bottom, left, width]);

  useLayoutEffect(() => {
    if (!visible) {
      previous.current = null;
      opacity.jump(0);
      return;
    }
    const track = highlightRef.current?.parentElement;
    if (!track) return;
    let active = true;
    let generation = 0;
    let handoff: ReturnType<typeof setTimeout> | undefined;
    const stop = () => {
      generation += 1;
      clearTimeout(handoff);
      left.stop();
      right.stop();
    };
    const measure = (allowAnimation: boolean) => {
      if (!active) return;
      const option = optionRefs.current.get(value);
      const trackBox = track.getBoundingClientRect();
      const optionBox = option?.getBoundingClientRect();
      if (!optionBox?.width || !trackBox.width || !trackBox.height) {
        stop();
        opacity.jump(0);
        previous.current = null;
        return;
      }
      // 弹窗开场会缩放祖先；视口坐标须还原为本地 CSS 像素，避免动画后残留偏移。
      const style = getComputedStyle(track);
      const localWidth =
        Number.parseFloat(style.width) || track.offsetWidth || trackBox.width;
      const localHeight =
        Number.parseFloat(style.height) ||
        track.offsetHeight ||
        trackBox.height;
      const scaleX = localWidth / trackBox.width;
      const scaleY = localHeight / trackBox.height;
      const target = {
        value,
        left: (optionBox.left - trackBox.left) * scaleX,
        right: (optionBox.right - trackBox.left) * scaleX,
        top: (optionBox.top - trackBox.top) * scaleY,
        bottom: (trackBox.bottom - optionBox.bottom) * scaleY,
        width: localWidth,
      };
      const last = previous.current;
      const unchanged =
        last &&
        Object.entries(target).every(
          ([key, entry]) => last[key as keyof typeof last] === entry,
        );
      // ResizeObserver 的首次通知不应打断刚开始的切换。
      if (
        unchanged &&
        !allowAnimation &&
        !document.hidden &&
        opacity.get() === 1
      )
        return;
      stop();
      previous.current = target;
      width.jump(target.width);
      top.jump(target.top);
      bottom.jump(target.bottom);
      opacity.jump(1);
      const jump = () => {
        left.jump(target.left);
        right.jump(target.right);
      };
      if (
        !allowAnimation ||
        !last ||
        last.value === value ||
        reducedMotion ||
        document.hidden
      ) {
        jump();
        return;
      }
      const standard = readLayoutMotionTokens("--duration-standard");
      const quick = readLayoutMotionTokens("--duration-quick");
      if (standard.duration === 0) {
        jump();
        return;
      }
      // 两条边独立移动，先展开覆盖来处与去处，再由前缘带动尾缘回弹。
      const fromLeft = left.get();
      const fromRight = right.get();
      const direction =
        Math.sign(target.left + target.right - fromLeft - fromRight) || 1;
      const stretch = {
        ...standard,
        ease: standard.ease ?? ([0.23, 1, 0.32, 1] as const),
      };
      animate(left, Math.min(fromLeft, target.left), stretch);
      animate(right, Math.max(fromRight, target.right), stretch);
      const currentGeneration = generation;
      handoff = setTimeout(() => {
        const spring = {
          type: "spring" as const,
          duration: standard.duration,
          bounce: 0,
        };
        const lead = direction > 0 ? right : left;
        const trail = direction > 0 ? left : right;
        const leadTo = direction > 0 ? target.right : target.left;
        const trailTo = direction > 0 ? target.left : target.right;
        const squash = Math.min(target.top, (target.right - target.left) / 4);
        animate(lead, leadTo, spring);
        void animate(trail, trailTo + direction * squash, spring).then(() => {
          if (active && currentGeneration === generation) {
            animate(trail, trailTo, { ...spring, duration: quick.duration });
          }
        });
      }, quick.duration * 1000);
    };
    measure(true);
    const resize = () => measure(false);
    const observer =
      typeof ResizeObserver === "undefined"
        ? undefined
        : new ResizeObserver(resize);
    observer?.observe(track);
    for (const option of optionRefs.current.values()) observer?.observe(option);
    const visibility = () => {
      if (document.hidden) {
        stop();
        previous.current = null;
      }
      measure(false);
    };
    const intersection =
      typeof IntersectionObserver === "undefined"
        ? undefined
        : new IntersectionObserver((entries) => {
            if (entries.some((entry) => !entry.isIntersecting)) {
              stop();
              previous.current = null;
              measure(false);
            }
          });
    intersection?.observe(track);
    document.addEventListener("visibilitychange", visibility);
    window.addEventListener("resize", resize);
    void document.fonts?.ready.then(resize);
    return () => {
      active = false;
      stop();
      observer?.disconnect();
      intersection?.disconnect();
      document.removeEventListener("visibilitychange", visibility);
      window.removeEventListener("resize", resize);
    };
  }, [
    value,
    visible,
    layoutKey,
    reducedMotion,
    optionRefs,
    left,
    right,
    top,
    bottom,
    width,
    opacity,
  ]);

  return (
    <motion.div
      ref={highlightRef}
      aria-hidden="true"
      data-segmented-highlight
      className={className}
      style={{ clipPath, opacity }}
    >
      {children}
    </motion.div>
  );
}
