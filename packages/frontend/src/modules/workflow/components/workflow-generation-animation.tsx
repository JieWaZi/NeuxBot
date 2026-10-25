import { Player, type PlayerRef } from "@remotion/player";
import { useReducedMotion } from "motion/react";
import { useEffect, useMemo, useRef, useState } from "react";
import { readLayoutMotionTokens } from "@/shared/lib/ui-motion-tokens";
import { WorkflowGenerationComposition } from "./workflow-generation-composition";
import {
  workflowGenerationLayout,
  workflowGenerationTiming,
} from "./workflow-generation-layout";

/** 嵌入式 Player 随 Scene、窗口可见性和减少动态效果同步启停，不提供视频控件。 */
export function WorkflowGenerationAnimation({ active }: { active: boolean }) {
  const host = useRef<HTMLDivElement>(null);
  const player = useRef<PlayerRef>(null);
  const reducedMotion = useReducedMotion();
  const motionTokens = useMemo(() => readLayoutMotionTokens(), []);
  const [viewport, setViewport] = useState({ width: 720, height: 400 });
  const [visible, setVisible] = useState(
    () => document.visibilityState !== "hidden",
  );
  const playing = active && visible && !reducedMotion;
  const layout = workflowGenerationLayout(viewport.width, viewport.height);
  useEffect(() => {
    const element = host.current;
    if (!element) return;
    const observer = new ResizeObserver(([entry]) => {
      if (entry && entry.contentRect.width > 0 && entry.contentRect.height > 0)
        setViewport({
          width: entry.contentRect.width,
          height: entry.contentRect.height,
        });
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    const update = () => setVisible(document.visibilityState !== "hidden");
    document.addEventListener("visibilitychange", update);
    return () => document.removeEventListener("visibilitychange", update);
  }, []);
  useEffect(() => {
    const instance = player.current;
    if (playing) instance?.play();
    else instance?.pause();
    return () => instance?.pause();
  }, [playing]);
  return (
    <div
      ref={host}
      aria-hidden="true"
      data-workflow-generation-playing={playing}
      className="pointer-events-none flex h-full min-h-0 w-full max-w-6xl items-center justify-center"
    >
      <Player
        ref={player}
        component={WorkflowGenerationComposition}
        inputProps={{
          motionEnabled: !reducedMotion,
          ease: motionTokens.ease,
          enterFrames: Math.max(
            1,
            Math.round(motionTokens.duration * workflowGenerationTiming.fps),
          ),
        }}
        compositionWidth={layout.width}
        compositionHeight={layout.height}
        durationInFrames={workflowGenerationTiming.durationInFrames}
        fps={workflowGenerationTiming.fps}
        loop
        controls={false}
        clickToPlay={false}
        doubleClickToFullscreen={false}
        spaceKeyToPlayOrPause={false}
        allowFullscreen={false}
        numberOfSharedAudioTags={0}
        className="max-w-full"
        style={{ width: layout.displayWidth }}
      />
    </div>
  );
}
