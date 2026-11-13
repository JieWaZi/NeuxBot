import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { BotEngine } from "@neuxbot/bloub/engine";
import { avatarExpressionById as EXPRESSION_BY_ID } from "./avatar-expressions";
import { SHAPE_BY_ID } from "@neuxbot/bloub/skins";
import { POSES, type StateId } from "@neuxbot/bloub/states";
import { usePreparedAppearance } from "./prepared-appearance";
import { alignMaterialDrawing, materialViewBox } from "./materials";
import type { CompanionActivity, CompanionAttention } from "./activity";
import { companionPlayback, companionPresentation } from "./playback";
import { nextHoverFace, type HoverFace } from "./interaction-expressions";
import { avatarDrawing } from "@neuxbot/bloub/drawing";
import { enlargedEyes } from "@neuxbot/bloub/eyes";
import { renderDrawing } from "./render-drawing";

/** React 只拥有可见性和播放时钟，几何、眨眼、形变与状态过渡由 Bloub 计算。 */
export function CompanionAvatar({
  avatar,
  activity = "idle",
  attention,
  animated = true,
  interactive = true,
  preview = false,
  onReady,
  onError,
  animation: requestedAnimation,
}: {
  avatar: string;
  activity?: CompanionActivity;
  attention?: CompanionAttention;
  animated?: boolean;
  interactive?: boolean;
  preview?: boolean;
  animation?: StateId;
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
  const baseExpression =
    animation || preview
      ? appearance.expression
      : (presentation.expression ?? appearance.expression);
  const hoverEnabled = interactive && animated && !preview && !animation;
  const interaction =
    hoverEnabled &&
    shown === "idle" &&
    !attentionState &&
    (hover.pointer || hover.focus) &&
    playing;
  const expression =
    (interaction ? hover.face?.expression : undefined) ?? baseExpression;
  const interactiveState = interaction ? hover.face?.state : undefined;

  useEffect(() => {
    if (!hoverEnabled) return;
    const target =
      host.current?.closest<HTMLElement>("[data-avatar-hover]") ?? host.current;
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
  const enteredAt = useRef(0);
  const motion = useRef<{
    shown: CompanionActivity;
    animation: StateId | undefined;
    interactiveState: StateId | undefined;
    attentionState: CompanionAttention["kind"] | undefined;
  }>({ shown, animation, interactiveState, attentionState });
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
    const now = clock.current;
    const timelineKey = `${animation ?? presentation.state}:${animation ? "preview" : "business"}`;
    if (timeline.current !== timelineKey) {
      timeline.current = timelineKey;
      enteredAt.current = now;
      block.current = "";
    }
    const key = `${timelineKey}:${appearance.shape}:${expression}:${presentation.motion}:${interactiveState}`;
    motion.current = {
      shown,
      animation,
      interactiveState,
      attentionState,
    };
    let target = companionPlayback(
      shown,
      now - enteredAt.current,
      animation,
      attentionState,
    );
    const state = interactiveState ?? target.state;
    const configurationChanged = configured.current !== key;
    if (configurationChanged) {
      configured.current = key;
      const base =
        EXPRESSION_BY_ID.get(expression) ?? EXPRESSION_BY_ID.get("neutre");
      engine.setShape(SHAPE_BY_ID.get(renderedShape)?.radii ?? null, now);
      // 业务只选择原生动作与固定表情，不叠加辅助视线或形变。
      engine.setExpression(base ?? null, now);
      engine.setLook(null, now);
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
    setSample({
      frame: engine.sample(clock.current),
      time: clock.current,
      nativeState: state,
    });
  }, [
    engine,
    appearance.shape,
    renderedShape,
    shown,
    attentionState,
    expression,
    presentation,
    interactiveState,
    playing,
    fixedPose,
    animation,
  ]);

  useEffect(() => {
    if (!playing) return;
    let raf = 0;
    let previous = performance.now();
    let painted = previous;
    const tick = (time: number) => {
      clock.current += Math.min(Math.max((time - previous) / 1000, 0), 0.1);
      previous = time;
      if (time - painted >= 1000 / 30) {
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
        setSample({
          frame: engine.sample(clock.current),
          time: clock.current,
          nativeState: state,
        });
        painted = time;
      }
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
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
        {alignMaterialDrawing(
          avatarDrawing(
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
          ),
          appearance.material,
          renderedShape,
        ).map(renderDrawing)}
      </svg>
    </span>
  );
}
