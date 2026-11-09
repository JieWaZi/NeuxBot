import {
  createElement,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { BotEngine, type Look } from "@neuxbot/bloub/engine";
import { EXPRESSION_BY_ID } from "@neuxbot/bloub/expressions";
import { SHAPE_BY_ID } from "@neuxbot/bloub/skins";
import { POSES, STATE_BY_ID, type StateId } from "@neuxbot/bloub/states";
import { faceOf, lookOf, shake } from "@neuxbot/bloub/motion";
import { usePreparedAppearance } from "./prepared-appearance";
import { materialViewBox } from "./materials";
import type { CompanionActivity } from "./activity";
import { useWorkingExpression } from "./working-expression";
import {
  companionPlayback,
  companionPresentation,
  COMPLETION_DURATION,
} from "./playback";
import { nextInteractionExpression } from "./interaction-expressions";
import { connectionArcs, bodyMovement } from "@neuxbot/bloub/effects";
import { avatarDrawing, type SvgDrawing } from "@neuxbot/bloub/drawing";

function renderDrawing(
  drawing: SvgDrawing,
  key: number,
): ReturnType<typeof createElement> {
  return createElement(
    drawing.tag,
    { ...drawing.attributes, key },
    ...drawing.children.map(renderDrawing),
  );
}

function useActivity(activity: CompanionActivity, animated: boolean) {
  const input = activity === "resting" ? "idle" : activity;
  const [current, setCurrent] = useState<{
    input: CompanionActivity;
    shown: CompanionActivity;
  }>({ input, shown: input === "completed" ? "idle" : input });
  if (current.input !== input) setCurrent({ input, shown: input });
  useEffect(() => {
    if (!animated) return;
    const delay =
      current.shown === "starting"
        ? 700
        : current.shown === "completed"
          ? COMPLETION_DURATION * 1000
          : current.shown === "stopped"
            ? 1000
            : 0;
    if (!delay) return;
    const timer = setTimeout(
      () =>
        setCurrent((state) => ({
          ...state,
          shown: state.shown === "starting" ? "thinking" : "idle",
        })),
      delay,
    );
    return () => clearTimeout(timer);
  }, [animated, current.shown, current.input]);
  return current.shown;
}

/** React 只拥有可见性和播放时钟，几何、眨眼、形变与状态过渡由 Bloub 计算。 */
export function CompanionAvatar({
  avatar,
  activity = "idle",
  animated = true,
  onReady,
  onError,
  interactionRevision,
  animation: requestedAnimation,
}: {
  avatar: string;
  activity?: CompanionActivity;
  animated?: boolean;
  preview?: boolean;
  interactionRevision?: number;
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
  const [hovered, setHovered] = useState(false);
  const shown = useActivity(activity, animated);
  const presentation = companionPresentation(shown);
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
  const workExpression = useWorkingExpression(
    shown,
    playing && !animation,
    appearance.expression,
  );
  const baseExpression = animation
    ? appearance.expression
    : (workExpression ?? presentation.expression ?? appearance.expression);
  const [reaction, setReaction] = useState<{
    activity: CompanionActivity;
    face: string;
  } | null>(null);
  const allowed =
    !animation &&
    !["starting", "waiting", "completed", "failed", "stopped"].includes(shown);
  const activeReaction =
    allowed && reaction?.activity === shown ? reaction.face : undefined;
  const expression = activeReaction ?? baseExpression;
  const react = useCallback(() => {
    if (!animated || !allowed) return;
    setReaction((previous) => {
      const current =
        previous?.activity === shown ? previous.face : baseExpression;
      return {
        activity: shown,
        face: nextInteractionExpression(
          EXPRESSION_BY_ID.get(current)?.id ?? "neutre",
        ),
      };
    });
  }, [animated, allowed, shown, baseExpression]);
  const previousRevision = useRef(interactionRevision);
  useEffect(() => {
    if (previousRevision.current === interactionRevision) return;
    previousRevision.current = interactionRevision;
    react();
  }, [interactionRevision, react]);
  useEffect(() => {
    if (!reaction) return;
    const timer = setTimeout(() => setReaction(null), 1800);
    return () => clearTimeout(timer);
  }, [reaction]);
  useEffect(() => {
    if (!hovered) return;
    const timer = setTimeout(() => setHovered(false), 1800);
    return () => clearTimeout(timer);
  }, [hovered]);
  const interaction =
    !animation &&
    hovered &&
    animated &&
    !reduced &&
    shown === "idle" &&
    !activeReaction;
  const interactiveState =
    !animation && activeReaction ? "idle" : interaction ? "wink" : undefined;
  const [engine] = useState(
    () =>
      new BotEngine(
        100,
        animation ?? presentation.state,
        SHAPE_BY_ID.get(appearance.shape)?.radii ?? null,
        EXPRESSION_BY_ID.get(appearance.expression) ?? null,
      ),
  );
  const clock = useRef(0);
  const enteredAt = useRef(0);
  const motion = useRef<{
    presentation: typeof presentation;
    shown: CompanionActivity;
    animation: StateId | undefined;
    interactiveState: StateId | undefined;
  }>({ presentation, shown, animation, interactiveState });
  const configured = useRef("");
  const wasFixed = useRef(false);
  const timeline = useRef("");
  const block = useRef("");
  const [{ frame, time: sampleTime, elapsed, nativeState }, setSample] =
    useState(() => ({
      frame: engine.sample(1.4),
      time: 1.4,
      elapsed: 1.4,
      nativeState: animation ?? presentation.state,
    }));
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
    const timelineKey = `${shown}:${animation ?? "business"}`;
    if (timeline.current !== timelineKey) {
      timeline.current = timelineKey;
      enteredAt.current = now;
      block.current = "";
    }
    const key = `${timelineKey}:${appearance.shape}:${expression}:${presentation.motion}:${interactiveState}`;
    motion.current = { presentation, shown, animation, interactiveState };
    let target = companionPlayback(shown, now - enteredAt.current, animation);
    const state = interactiveState ?? target.state;
    const configurationChanged = configured.current !== key;
    if (configurationChanged) {
      configured.current = key;
      const base =
        EXPRESSION_BY_ID.get(expression) ?? EXPRESSION_BY_ID.get("neutre");
      engine.setShape(SHAPE_BY_ID.get(appearance.shape)?.radii ?? null, now);
      engine.setExpression(
        base
          ? animation
            ? base
            : (faceOf(presentation.motion, base) ?? base)
          : null,
        now,
      );
      engine.setLook(
        animation || !STATE_BY_ID.get(state)?.baseFace
          ? null
          : lookOf(presentation.motion),
        now,
      );
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
      elapsed: clock.current - enteredAt.current,
      nativeState: state,
    });
  }, [
    engine,
    appearance.shape,
    shown,
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
        const pose = current.presentation;
        const local = clock.current - enteredAt.current;
        const target = companionPlayback(
          current.shown,
          local,
          current.animation,
        );
        const state = current.interactiveState ?? target.state;
        const blockKey = `${state}:${target.block}`;
        if (block.current !== blockKey) {
          engine.setState(state, clock.current, target.local, true);
          block.current = blockKey;
        }
        const look: Look = { ...lookOf(pose.motion) };
        if (pose.scan) {
          look.yaw = Math.sin(local * 1.5) * 16;
          look.pitch = 8;
          look.mix = 1;
        }
        if (pose.motion === "failed") look.spin += shake(local);
        engine.setLook(
          current.animation || !STATE_BY_ID.get(state)?.baseFace ? null : look,
          clock.current,
          0.05,
        );
        setSample({
          frame: engine.sample(clock.current),
          time: clock.current,
          elapsed: clock.current - enteredAt.current,
          nativeState: state,
        });
        painted = time;
      }
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [engine, playing]);

  const renderedFrame =
    shown === "connecting" && !animation
      ? { ...frame, arcs: connectionArcs(sampleTime) }
      : frame;
  return (
    <span
      ref={host}
      className="relative block size-full"
      aria-hidden="true"
      onClick={interactionRevision === undefined ? react : undefined}
      onPointerEnter={() => {
        if (playing && !animation && shown === "idle") setHovered(true);
      }}
      onPointerLeave={() => setHovered(false)}
    >
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
        viewBox={materialViewBox(appearance.material)}
        width="100%"
        height="100%"
        overflow="visible"
      >
        {avatarDrawing(
          renderedFrame,
          appearance.material,
          appearance.color,
          id,
          prepared.texture,
          [],
          [],
          playing && !animation
            ? bodyMovement(shown, sampleTime, elapsed)
            : undefined,
          appearance.shape,
          sampleTime,
        ).map(renderDrawing)}
      </svg>
    </span>
  );
}
