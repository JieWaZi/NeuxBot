import { useEffect, useImperativeHandle, useRef, type Ref } from "react";
import type { CompanionEngine } from "../../../../../vendor/companion/renderer";
import { logger } from "@/shared/diagnostics/logger";
import { companionAppearance } from "./appearance";
import { companionPlayback } from "./playback";
import type { CompanionActivity } from "./activity";
import type { CompanionMotion } from "./roaming";

export function CompanionAvatar({
  avatar,
  activity = "idle",
  preview = false,
  animated = true,
  motion,
  interactiveLabel,
  interactiveTitle,
  onReady,
  onError,
  ref,
}: {
  avatar: string;
  activity?: CompanionActivity;
  preview?: boolean;
  /** 关闭动画时使用同一形象的固定姿态，不启动待机或互动播放。 */
  animated?: boolean;
  motion?: CompanionMotion;
  interactiveLabel?: string;
  interactiveTitle?: string;
  /** 身体图片加载并完整绘制后，通知品牌入场等需要顺序交接的调用方。 */
  onReady?: () => void;
  /** 绘制失败后允许启动品牌等调用方使用静态降级。 */
  onError?: () => void;
  ref?: Ref<CompanionControl>;
}) {
  const host = useRef<HTMLSpanElement>(null);
  const playback = useRef<ReturnType<typeof companionPlayback> | null>(null);
  const latestActivity = useRef(activity);
  const readyCallback = useRef(onReady);
  const errorCallback = useRef(onError);
  useEffect(() => {
    errorCallback.current = onError;
  }, [onError]);
  useEffect(() => {
    readyCallback.current = onReady;
  }, [onReady]);
  const pendingInteraction = useRef(false);
  useImperativeHandle(
    ref,
    () => ({
      interact() {
        if (!animated) return;
        if (playback.current) playback.current.interact();
        else pendingInteraction.current = true;
      },
    }),
    [animated],
  );

  useEffect(() => {
    latestActivity.current = activity;
    playback.current?.update(activity);
  }, [activity]);

  useEffect(() => {
    const element = host.current;
    if (!element) return;
    let disposed = false;
    let visible = typeof IntersectionObserver === "undefined";
    let loading = false;
    let engine: CompanionEngine | undefined;
    let cancelImageWait: (() => void) | undefined;
    const media = window.matchMedia?.("(prefers-reduced-motion: reduce)");
    // 卡片飞入时会暂时缩小，按布局尺寸预先选择清晰的目标图片。
    const resolution = (): 256 | 512 =>
      Math.max(element.clientWidth, element.getBoundingClientRect().width) *
        (window.devicePixelRatio || 1) >
      256
        ? 512
        : 256;
    const updateResolution = () => engine?.ball.setResolution(resolution());
    const resizeObserver =
      typeof ResizeObserver === "undefined"
        ? undefined
        : new ResizeObserver(updateResolution);
    resizeObserver?.observe(element);
    window.addEventListener("resize", updateResolution);
    const updateVisibility = () => {
      playback.current?.visibility(
        visible && !document.hidden,
        media?.matches ?? false,
      );
    };
    const load = async () => {
      if (loading || disposed) return;
      loading = true;
      try {
        const { createCompanion, companionReactions } =
          await import("../../../../../vendor/companion/renderer");
        if (disposed) return;
        const appearance = companionAppearance(avatar);
        engine = createCompanion(element, {
          character: "ally",
          emotion: "02",
          style: appearance.style,
          resolution: resolution(),
          autostart: false,
          lite: false,
          idle: false,
        });
        const svg = element.querySelector("svg");
        svg?.setAttribute("data-companion-avatar", "");
        svg?.setAttribute("overflow", "visible");
        svg?.setAttribute("width", "100%");
        svg?.setAttribute("height", "100%");
        svg?.setAttribute(
          "pointer-events",
          animated && interactiveLabel ? "visiblePainted" : "none",
        );
        const body = element.querySelector("image.ally-body-image");
        if (!svg || !body) throw new Error("伙伴形象缺少身体图层");
        // SVG 眼睛同步创建，身体 PNG 异步加载；完整图片就绪前不能露出局部形象。
        svg.setAttribute("visibility", "hidden");
        await new Promise<void>((resolve, reject) => {
          const release = () => {
            body.removeEventListener("load", loaded);
            body.removeEventListener("error", failed);
            cancelImageWait = undefined;
          };
          const loaded = () => {
            release();
            resolve();
          };
          const failed = () => {
            release();
            reject(new Error("伙伴身体图片加载失败"));
          };
          cancelImageWait = () => {
            release();
            resolve();
          };
          body.addEventListener("load", loaded);
          body.addEventListener("error", failed);
        });
        if (disposed) return;
        if (!animated) {
          engine.ball.setReduced(true);
          engine.renderStatic();
          svg.removeAttribute("visibility");
          readyCallback.current?.();
          pendingInteraction.current = false;
          return;
        }
        engine.renderStatic();
        svg.removeAttribute("visibility");
        readyCallback.current?.();
        playback.current = companionPlayback(engine, latestActivity.current, {
          reactions: companionReactions,
          preview,
          motion,
        });
        updateVisibility();
        if (pendingInteraction.current) {
          pendingInteraction.current = false;
          playback.current.interact();
        }
      } catch (error) {
        if (!disposed) {
          engine?.destroy();
          engine = undefined;
          logger.warn("companion renderer failed", { error });
          errorCallback.current?.();
        }
        // 加载失败保留稳定占位；重新进入可视区域时可以重试。
        loading = false;
      }
    };
    const observer =
      typeof IntersectionObserver === "undefined"
        ? undefined
        : new IntersectionObserver((entries) => {
            visible = entries.some((entry) => entry.isIntersecting);
            if (visible) void load();
            updateVisibility();
          });
    observer?.observe(element);
    if (visible) void load();
    media?.addEventListener("change", updateVisibility);
    document.addEventListener("visibilitychange", updateVisibility);
    return () => {
      disposed = true;
      cancelImageWait?.();
      observer?.disconnect();
      resizeObserver?.disconnect();
      window.removeEventListener("resize", updateResolution);
      media?.removeEventListener("change", updateVisibility);
      document.removeEventListener("visibilitychange", updateVisibility);
      if (playback.current) playback.current.destroy();
      else engine?.destroy();
      playback.current = null;
    };
  }, [avatar, preview, motion, interactiveLabel, animated]);

  return (
    <span
      ref={host}
      role={animated && interactiveLabel ? "button" : undefined}
      tabIndex={animated && interactiveLabel ? 0 : undefined}
      aria-label={animated ? interactiveLabel : undefined}
      title={interactiveTitle}
      onPointerEnter={() => playback.current?.wake()}
      onPointerDown={() => {
        if (animated && interactiveLabel) host.current?.focus();
      }}
      onClick={() => {
        if (animated && interactiveLabel) {
          if (playback.current) playback.current.interact();
          else pendingInteraction.current = true;
        }
      }}
      onKeyDown={(event) => {
        if (
          !animated ||
          !interactiveLabel ||
          (event.key !== "Enter" && event.key !== " ")
        )
          return;
        event.preventDefault();
        if (playback.current) playback.current.interact();
        else pendingInteraction.current = true;
      }}
      className={
        animated && interactiveLabel
          ? "focus-visible:ring-ring pointer-events-none block size-full rounded-lg outline-none focus-visible:ring-2"
          : "block size-full"
      }
      data-companion-activity={animated ? activity : undefined}
      data-companion-animated={animated}
    />
  );
}

export type CompanionControl = {
  interact: () => void;
};
