import { useEffect, useRef, type CSSProperties } from "react";
import {
  defaultCompanionAvatar,
  encodeCompanionAppearance,
  type CompanionAppearance,
} from "@/shared/companion/appearance";
import { cn } from "@/shared/lib/cn";
import { StaticAvatar } from "@/shared/ui/avatar";
import type { dropWorkBoardAvatars } from "./work-board-avatar-physics";

type Partner = { id: string; name: string; avatar?: string };

const sizes = {
  wide: "[--pile-size:clamp(3.5rem,27cqi,8.5rem)]",
  hero: "[--pile-size:clamp(3rem,23cqi,7rem)]",
  medium: "[--pile-size:clamp(2.25rem,17cqi,5rem)]",
  small: "[--pile-size:clamp(1.5rem,12cqi,3.5rem)]",
} as const;

// 两侧分别落定，大小与位置错开，给中间的输入框留出空间。
const slotsBySide = {
  left: [
    { size: "wide", x: 25, lift: 0, angle: -8 },
    { size: "hero", x: 61, lift: 0, angle: 6 },
    { size: "medium", x: 81, lift: 0, angle: -12 },
    { size: "small", x: 7, lift: 0, angle: 7 },
    { size: "small", x: 44, lift: 0, angle: -14 },
    { size: "small", x: 30, lift: 42, angle: 15 },
    { size: "small", x: 72, lift: 28, angle: -8 },
  ],
  right: [
    { size: "hero", x: 36, lift: 0, angle: -5 },
    { size: "wide", x: 71, lift: 0, angle: 9 },
    { size: "medium", x: 16, lift: 0, angle: 10 },
    { size: "small", x: 51, lift: 0, angle: -11 },
    { size: "small", x: 91, lift: 0, angle: -9 },
    { size: "small", x: 24, lift: 32, angle: 13 },
    { size: "small", x: 82, lift: 40, angle: 8 },
  ],
} as const;

// 装饰头像只补足构图，不进入伙伴列表或任务数据。
const accents = [
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
    material: "plush",
    shape: "capsule",
    expression: "somnolent",
    color: "e68ba7",
  },
  {
    material: "plush",
    shape: "triangle",
    expression: "surpris",
    color: "f5c870",
  },
  {
    material: "plush",
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
] satisfies CompanionAppearance[];

export function WorkBoardAvatars({
  partners,
  active,
  side,
}: {
  partners: readonly Partner[];
  active: boolean;
  side: keyof typeof slotsBySide;
}) {
  const stage = useRef<HTMLDivElement>(null);
  const leadPartners = partners
    .slice(0, 6)
    .filter((_, index) => index % 2 === (side === "left" ? 0 : 1));
  const pileKey = JSON.stringify(
    leadPartners.map(({ id, avatar }) => [id, avatar]),
  );

  useEffect(() => {
    const element = stage.current;
    if (!active || !element) return;
    const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
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
      if (reducedMotion.matches || document.hidden || !visible) {
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
          // 动态资源加载失败时仍使用静态构图。
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
    reducedMotion.addEventListener("change", sync);
    sync();
    return () => {
      disposed = true;
      stop?.();
      observer?.disconnect();
      resizeObserver?.disconnect();
      document.removeEventListener("visibilitychange", sync);
      reducedMotion.removeEventListener("change", sync);
    };
  }, [active, pileKey]);

  return (
    <div
      aria-hidden="true"
      data-work-board-avatars={side}
      className="@container/avatar-pile relative isolate hidden min-h-44 min-w-0 overflow-hidden @[52rem]/resource-page:block"
    >
      <div
        key={pileKey}
        ref={stage}
        className="relative h-full min-h-44 w-full"
      >
        <div className="work-board-avatar-light absolute inset-x-0 bottom-1 h-20" />
        {slotsBySide[side].map(({ size, x, lift, angle }, index) => {
          const partner = leadPartners[index];
          const accent =
            accents[(index + (side === "left" ? 0 : 5)) % accents.length];
          return (
            <div
              key={partner?.id ?? index}
              data-pile-avatar
              className={cn("work-board-avatar-piece absolute", sizes[size])}
              style={
                {
                  "--pile-rest-x": `${x}%`,
                  "--pile-lift": `${lift}px`,
                  "--pile-rest-angle": `${angle}deg`,
                } as CSSProperties
              }
            >
              <StaticAvatar
                name={partner?.name ?? ""}
                src={
                  partner
                    ? partner.avatar
                    : accent
                      ? encodeCompanionAppearance(accent)
                      : defaultCompanionAvatar
                }
              />
            </div>
          );
        })}
      </div>
    </div>
  );
}
