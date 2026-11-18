import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { BotEngine } from "@neuxbot/bloub/engine";
import type { ExpressionId } from "@neuxbot/bloub/expressions";
import { avatarExpressionById as EXPRESSION_BY_ID } from "./avatar-expressions";
import { SHAPE_BY_ID } from "@neuxbot/bloub/skins";
import { POSES, type StateId } from "@neuxbot/bloub/states";
import { usePreparedAppearance } from "./prepared-appearance";
import { materialViewBox } from "./materials";
import type { CompanionActivity, CompanionAttention } from "./activity";
import { companionPlayback, companionPresentation } from "./playback";
import { nextHoverFace, type HoverFace } from "./interaction-expressions";
import { avatarDrawing } from "@neuxbot/bloub/drawing";
import { enlargedEyes } from "@neuxbot/bloub/eyes";
import { REST_LOOK } from "@neuxbot/bloub/motion";
import { renderDrawing } from "./render-drawing";

/** React 只拥有可见性和播放时钟，几何、眨眼、形变与状态过渡由 Bloub 计算。 */
export function CompanionAvatar({
  avatar,
  activity = "idle",
  attention,
  animated = true,
  interactive = true,
  terminalHover = false,
  preview = false,
  onReady,
  onError,
  animation: requestedAnimation,
  expression: requestedExpression,
}: {
  avatar: string;
  activity?: CompanionActivity;
  attention?: CompanionAttention;
  animated?: boolean;
  interactive?: boolean;
  terminalHover?: boolean;
  preview?: boolean;
  animation?: StateId;
  expression?: ExpressionId;
  onReady?: () => void;
  onError?: () => void;
}) {
  const animation =
    requestedAnimation === "sleep" ? "idle" : requestedAnimation;
  const host = useRef<HTMLSpanElement>(null);
  const id = useId().replace(/:/g, "");
  const prepared = usePreparedAppearance(avatar);
  const { appearance } = prepared;
  const [hover, setHover] = useState<{
    pointer: boolean;
    focus: boolean;
    face: HoverFace | null;
  }>({ pointer: false, focus: false, face: null });
  const shown = activity === "resting" ? "idle" : activity;
  const attentionState = attention?.kind;
  const presentation = companionPresentation(shown, attentionState);
  // 通知参考使用 Bloub 自带的斜卵石；其余有身体的状态保留伙伴形状。
  const renderedShape =
    (animation ?? presentation.state) === "notify" ? "galet" : appearance.shape;
  const [visible, setVisible] = useState(
    typeof IntersectionObserver === "undefined",
  );
  const [foreground, setForeground] = useState(!document.hidden);
  const [reduced, setReduced] = useState(
    () =>
      window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false,
  );
  const playing = animated && visible && foreground && !reduced;
  const fixedPose = !animated || reduced;
  const decorativeExpression =
    shown === "idle" && !attentionState ? requestedExpression : undefined;
  const baseExpression =
    animation || preview
      ? appearance.expression
      : (presentation.expression ??
        decorativeExpression ??
        appearance.expression);
  const hoverEnabled = interactive && animated && !preview && !animation;
  const interaction =
    hoverEnabled &&
    (shown === "idle" ||
      (terminalHover &&
        (shown === "completed" ||
          shown === "failed" ||
          shown === "stopped"))) &&
    !attentionState &&
    (hover.pointer || hover.focus) &&
    !reduced;
  const expression =
    (interaction ? hover.face?.expression : undefined) ?? baseExpression;
  const interactiveState = interaction ? hover.face?.state : undefined;

  useEffect(() => {
    if (!hoverEnabled) return;
    const target =
      host.current?.closest<HTMLElement>("[data-avatar-hover-scope]") ??
      host.current?.closest<HTMLElement>("[data-avatar-hover]") ??
      host.current;
    if (!target) return;
    const change = (source: "pointer" | "focus", active: boolean) => {
      const seed = active ? Math.random() : undefined;
      setHover((previous) => {
        if (previous[source] === active) return previous;
        const entered = active && !previous.pointer && !previous.focus;
        return {
          ...previous,
          [source]: active,
          face: entered
            ? nextHoverFace(baseExpression, previous.face, seed)
            : previous.face,
        };
      });
    };
    const enter = () => change("pointer", true);
    const leave = () => change("pointer", false);
    const focus = (event: FocusEvent) => {
      if (
        event.target instanceof Element &&
        event.target.matches(":focus-visible")
      )
        change("focus", true);
    };
    const blur = (event: FocusEvent) => {
      if (
        !(event.relatedTarget instanceof Node) ||
        !target.contains(event.relatedTarget)
      )
        change("focus", false);
    };
    target.addEventListener("pointerenter", enter);
    target.addEventListener("pointerleave", leave);
    target.addEventListener("focusin", focus);
    target.addEventListener("focusout", blur);
    return () => {
      target.removeEventListener("pointerenter", enter);
      target.removeEventListener("pointerleave", leave);
      target.removeEventListener("focusin", focus);
      target.removeEventListener("focusout", blur);
    };
  }, [hoverEnabled, baseExpression]);
  const [engine] = useState(
    () =>
      new BotEngine(
        100,
        animation ?? presentation.state,
        SHAPE_BY_ID.get(renderedShape)?.radii ?? null,
        EXPRESSION_BY_ID.get(appearance.expression) ?? null,
      ),
  );
  const clock = useRef(0);
  const displayedAt = useRef(0);
  const wakePlayback = useRef<(() => void) | undefined>(undefined);
  const enteredAt = useRef(0);
  const motion = useRef<{
    shown: CompanionActivity;
    animation: StateId | undefined;
    interactiveState: StateId | undefined;
    attentionState: CompanionAttention["kind"] | undefined;
    continuous: boolean;
  }>({ shown, animation, interactiveState, attentionState, continuous: false });
  const configured = useRef("");
  const wasFixed = useRef(false);
  const timeline = useRef("");
  const block = useRef("");
  const [{ frame, time: sampleTime, nativeState }, setSample] = useState(
    () => ({
      frame: engine.sample(1.4),
      time: 1.4,
      nativeState: animation ?? presentation.state,
    }),
  );
  const callbacks = useRef({ onReady, onError });
  useLayoutEffect(() => {
    callbacks.current = { onReady, onError };
  }, [onReady, onError]);

  useEffect(() => {
    if (prepared.request !== avatar || prepared.status === "pending") return;
    if (prepared.status === "ready") callbacks.current.onReady?.();
    else callbacks.current.onError?.();
  }, [avatar, prepared.request, prepared.status]);

  useEffect(() => {
    if (!animated) return;
    const media = window.matchMedia?.("(prefers-reduced-motion: reduce)");
    const visibility = () => setForeground(!document.hidden);
    const preference = () => setReduced(media?.matches ?? false);
    const observer =
      typeof IntersectionObserver === "undefined"
        ? undefined
        : new IntersectionObserver(([entry]) =>
            setVisible(entry?.isIntersecting ?? false),
          );
    if (host.current) observer?.observe(host.current);
    document.addEventListener("visibilitychange", visibility);
    media?.addEventListener("change", preference);
    visibility();
    preference();
    return () => {
      observer?.disconnect();
      document.removeEventListener("visibilitychange", visibility);
      media?.removeEventListener("change", preference);
    };
  }, [animated]);

  useLayoutEffect(() => {
    const timelineKey = `${animation ?? presentation.state}:${animation ? "preview" : "business"}`;
    const key = `${timelineKey}:${appearance.shape}:${expression}:${presentation.motion}:${interactiveState}`;
    const configurationChanged = configured.current !== key;
    // 输入接续屏幕上最后一帧，不能从尚未绘制的采样时刻跳到下一段过渡。
    if (
      configured.current &&
      (configurationChanged || (!playing && !fixedPose))
    )
      clock.current = displayedAt.current;
    const now = clock.current;
    if (timeline.current !== timelineKey) {
      timeline.current = timelineKey;
      enteredAt.current = now;
      block.current = "";
    }
    motion.current = {
      shown,
      animation,
      interactiveState,
      attentionState,
      continuous: Boolean(
        animation ||
        preview ||
        interaction ||
        attentionState ||
        presentation.state !== "idle",
      ),
    };
    let target = companionPlayback(
      shown,
      now - enteredAt.current,
      animation,
      attentionState,
    );
    const state = interactiveState ?? target.state;
    if (configurationChanged) {
      configured.current = key;
      const base =
        EXPRESSION_BY_ID.get(expression) ?? EXPRESSION_BY_ID.get("neutre");
      engine.setShape(SHAPE_BY_ID.get(renderedShape)?.radii ?? null, now);
      // 普通头像沿用毛绒的小幅自然游移；显式原生预览保留原生视线。
      engine.setExpression(base ?? null, now);
      engine.setLook(animation ? null : REST_LOOK, now);
    }
    if (
      !playing &&
      (configurationChanged || (fixedPose && !wasFixed.current))
    ) {
      // A native thumbnail is sampled at its documented readable pose. Advance
      // this same clock so resuming cannot replay the already displayed morph.
      engine.reset(state, now);
      clock.current = now + (animation ? POSES[state] : 1.4);
      // A globally scheduled blink may coincide with this readable local pose.
      // Settle on an open, completed sample while advancing the same clock.
      while (!engine.quiet(clock.current)) clock.current += 1 / 30;
      target = companionPlayback(
        shown,
        clock.current - enteredAt.current,
        animation,
        attentionState,
      );
    }
    wasFixed.current = fixedPose;
    const blockKey = `${state}:${target.block}`;
    if (block.current !== blockKey) {
      if (playing) engine.setState(state, now, target.local, true);
      block.current = blockKey;
    }
    displayedAt.current = clock.current;
    setSample({
      frame: engine.sample(clock.current),
      time: clock.current,
      nativeState: state,
    });
    if (playing && (configurationChanged || motion.current.continuous))
      wakePlayback.current?.();
  }, [
    engine,
    appearance.shape,
    renderedShape,
    shown,
    attentionState,
    expression,
    presentation,
    interactiveState,
    interaction,
    preview,
    playing,
    fixedPose,
    animation,
  ]);

  useEffect(() => {
    if (!playing) return;
    let raf = 0;
    let previous = performance.now();
    let painted = previous;
    const interval = 1000 / 30;
    const tolerance = 0.0001;
    const tick = (time: number) => {
      raf = 0;
      clock.current += Math.min(Math.max((time - previous) / 1000, 0), 0.1);
      previous = time;
      const settled = !motion.current.continuous && engine.quiet(clock.current);
      const elapsed = time - painted;
      if (elapsed + tolerance >= interval || settled) {
        const current = motion.current;
        const local = clock.current - enteredAt.current;
        const target = companionPlayback(
          current.shown,
          local,
          current.animation,
          current.attentionState,
        );
        const state = current.interactiveState ?? target.state;
        const blockKey = `${state}:${target.block}`;
        if (block.current !== blockKey) {
          engine.setState(state, clock.current, target.local, true);
          block.current = blockKey;
        }
        displayedAt.current = clock.current;
        setSample({
          frame: engine.sample(clock.current),
          time: clock.current,
          nativeState: state,
        });
        // 保留不足一帧的余量，避免30fps被刷新率量化为20或25fps。
        painted += Math.floor((elapsed + tolerance) / interval) * interval;
      }
      // 完成 morph 与眨眼后才停稳，保留当前姿态和可暂停时钟。
      if (!settled) raf = requestAnimationFrame(tick);
    };
    const wake = () => {
      if (raf) return;
      previous = performance.now();
      painted = previous;
      raf = requestAnimationFrame(tick);
    };
    wakePlayback.current = wake;
    wake();
    return () => {
      wakePlayback.current = undefined;
      if (raf) cancelAnimationFrame(raf);
    };
  }, [engine, playing]);

  const renderedAttention = frame.notif?.attention?.kind ?? attentionState;
  return (
    <span ref={host} className="relative block size-full" aria-hidden="true">
      <svg
        visibility={prepared.status === "pending" ? "hidden" : undefined}
        data-companion-avatar
        data-texture-state={prepared.status}
        data-material={appearance.material}
        data-shape={appearance.shape}
        data-activity={shown}
        data-motion={presentation.motion}
        data-expression={expression}
        data-animation={nativeState}
        viewBox={materialViewBox()}
        width="100%"
        height="100%"
        overflow="visible"
      >
        {avatarDrawing(
          // 小头像仍需清晰的眼睛；沿用原生扩眼以保留眨眼和脸部余量。
          { ...frame, eyes: enlargedEyes(frame.eyes, 1.3) },
          appearance.material,
          appearance.color,
          id,
          prepared.texture,
          [],
          [],
          undefined,
          renderedShape,
          sampleTime,
          {
            notificationColor:
              renderedAttention === "unread"
                ? "var(--accent-secondary-hover)"
                : renderedAttention
                  ? appearance.color
                  : "var(--accent-default)",
            unreadCount:
              attention?.kind === "unread" ? attention.count : undefined,
          },
        ).map(renderDrawing)}
      </svg>
    </span>
  );
}
