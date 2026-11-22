import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
} from "react";
import {
  companionAppearance,
  defaultCompanionAvatar,
  encodeCompanionAppearance,
  type CompanionAppearance,
} from "@/shared/companion/appearance";
import { cn } from "@/shared/lib/cn";
import { Avatar } from "@/shared/ui/avatar";
import type { dropWorkBoardAvatars } from "./work-board-avatar-physics";
import { useWorkBoardAvatarExpression } from "./work-board-avatar-expressions";

const sizes = {
  wide: "[--pile-size:clamp(3.5rem,27cqi,8.5rem)]",
  hero: "[--pile-size:clamp(3rem,23cqi,7rem)]",
  medium: "[--pile-size:clamp(2.25rem,17cqi,5rem)]",
  small: "[--pile-size:clamp(1.5rem,12cqi,3.5rem)]",
} as const;

// 静态落点用于减少动态效果偏好与加载失败时的完整构图。
const slots = [
  { size: "hero", x: 36, lift: 0, angle: -5 },
  { size: "wide", x: 71, lift: 0, angle: 9 },
  { size: "medium", x: 16, lift: 0, angle: 10 },
  { size: "small", x: 51, lift: 0, angle: -11 },
  { size: "small", x: 91, lift: 0, angle: -9 },
  { size: "small", x: 24, lift: 32, angle: 13 },
  { size: "small", x: 82, lift: 40, angle: 8 },
  { size: "medium", x: 58, lift: 55, angle: -12 },
  { size: "small", x: 8, lift: 60, angle: 8 },
  { size: "small", x: 35, lift: 70, angle: -6 },
  { size: "small", x: 72, lift: 85, angle: 10 },
  { size: "small", x: 92, lift: 75, angle: -8 },
] as const;

// 固定外观让动画可在伙伴和任务数据加载期间播放。
const accents = [
  {
    material: "plush",
    shape: "triangle",
    expression: "surpris",
    color: "f5c870",
  },
  {
    material: "balloon",
    shape: "galet",
    expression: "espiegle",
    color: "80c5b7",
  },
  {
    material: "particles",
    shape: "losange",
    expression: "timide",
    color: "c7a0e8",
  },
  { material: "plush", shape: "carre", expression: "hilare", color: "f0ab87" },
  {
    material: "plush",
    shape: "pentagone",
    expression: "attentif",
    color: "abc4ef",
  },
  {
    material: "plush",
    shape: "squircle",
    expression: "heureux",
    color: "a58bdf",
  },
  {
    material: "plush",
    shape: "galet",
    expression: "complice",
    color: "8cbdb0",
  },
  { material: "plush", shape: "nuage", expression: "reveur", color: "f3ac86" },
  {
    material: "particles",
    shape: "hexagone",
    expression: "curieux",
    color: "91abe7",
  },
  {
    material: "balloon",
    shape: "capsule",
    expression: "somnolent",
    color: "e68ba7",
  },
] satisfies CompanionAppearance[];

export function WorkBoardAvatars({ active }: { active: boolean }) {
  const slot = useRef<HTMLDivElement>(null);
  const [hasRoom, setHasRoom] = useState(false);
  if (!active && hasRoom) setHasRoom(false);
  // 容器查询决定何时显示；实际可用尺寸决定何时创建头像与动画资源。
  useLayoutEffect(() => {
    const element = slot.current;
    if (!element || !active) return;
    const measure = () => setHasRoom(element.clientWidth > 0);
    measure();
    if (typeof ResizeObserver === "undefined") {
      window.addEventListener("resize", measure);
      return () => window.removeEventListener("resize", measure);
    }
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [active]);

  return (
    <div
      ref={slot}
      aria-hidden="true"
      data-work-board-avatars="right"
      className="@container/avatar-pile relative isolate hidden min-h-44 min-w-0 overflow-hidden @[52rem]/resource-page:block"
    >
      {active && hasRoom ? <WorkBoardAvatarPile /> : null}
    </div>
  );
}

function WorkBoardAvatarPile() {
  const stage = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const element = stage.current;
    if (!element) return;
    const reducedMotion = window.matchMedia?.(
      "(prefers-reduced-motion: reduce)",
    );
    let disposed = false;
    let visible = !("IntersectionObserver" in window);
    let loading = false;
    let attach: typeof dropWorkBoardAvatars | undefined;
    let played = false;
    let stop: (() => void) | undefined;
    let width = element.clientWidth;
    let height = element.clientHeight;

    function sync() {
      if (!element || disposed) return;
      if (reducedMotion?.matches || document.hidden || !visible) {
        stop?.();
        stop = undefined;
        return;
      }
      if (stop || loading || !element.clientWidth) return;
      if (attach) {
        stop = attach(element, !played);
        played = true;
        return;
      }
      loading = true;
      void import("./work-board-avatar-physics")
        .then(({ dropWorkBoardAvatars }) => {
          loading = false;
          attach = dropWorkBoardAvatars;
          sync();
        })
        .catch(() => {
          // 动态资源加载失败时恢复静态构图。
          if (!disposed) element.dataset.pileStatic = "";
        });
    }
    const observer =
      "IntersectionObserver" in window
        ? new IntersectionObserver(([entry]) => {
            visible = Boolean(entry?.isIntersecting);
            sync();
          })
        : undefined;
    observer?.observe(element);
    const resizeObserver =
      typeof ResizeObserver === "undefined"
        ? undefined
        : new ResizeObserver(() => {
            const nextWidth = element.clientWidth;
            const nextHeight = element.clientHeight;
            if (width === nextWidth && height === nextHeight) return;
            width = nextWidth;
            height = nextHeight;
            stop?.();
            stop = undefined;
            sync();
          });
    resizeObserver?.observe(element);
    document.addEventListener("visibilitychange", sync);
    reducedMotion?.addEventListener("change", sync);
    sync();
    return () => {
      disposed = true;
      stop?.();
      observer?.disconnect();
      resizeObserver?.disconnect();
      document.removeEventListener("visibilitychange", sync);
      reducedMotion?.removeEventListener("change", sync);
    };
  }, []);

  return (
    <div ref={stage} className="relative h-full min-h-44 w-full">
      <div className="work-board-avatar-light absolute inset-x-0 bottom-1 h-20" />
      {slots.map(({ size, x, lift, angle }, index) => {
        const accent = accents[index % accents.length];
        return (
          <WorkBoardAvatarPiece
            key={x}
            avatar={
              accent
                ? encodeCompanionAppearance(accent)
                : defaultCompanionAvatar
            }
            className={cn("work-board-avatar-piece absolute", sizes[size])}
            style={
              {
                "--pile-rest-x": `${x}%`,
                "--pile-lift": `${lift}px`,
                "--pile-rest-angle": `${angle}deg`,
              } as CSSProperties
            }
          />
        );
      })}
    </div>
  );
}

function WorkBoardAvatarPiece({
  avatar,
  className,
  style,
}: {
  avatar: string;
  className: string;
  style: CSSProperties;
}) {
  const host = useRef<HTMLDivElement>(null);
  const expression = useWorkBoardAvatarExpression(
    host,
    companionAppearance(avatar).expression,
  );
  return (
    <div ref={host} data-pile-avatar className={className} style={style}>
      <Avatar
        interactive={false}
        name=""
        src={avatar}
        expression={expression}
      />
    </div>
  );
}
