import type {
  CompanionEngine,
  CompanionReaction,
} from "../../../../../vendor/companion/renderer";
import { companionEmotions, type CompanionActivity } from "./activity";
import type { CompanionMotion } from "./roaming";

const restAfter = 90_000;
const idleGestures = ["sway", "walk", "love"] as const;

/** 抽完一轮再洗牌，轮次交界也不连续重复。 */
function reactionDeck(reactions: readonly CompanionReaction[]) {
  let remaining: CompanionReaction[] = [];
  let previous: string | undefined;
  return () => {
    if (!remaining.length) {
      remaining = [...reactions];
      for (let index = remaining.length - 1; index > 0; index--) {
        const other = Math.floor(Math.random() * (index + 1));
        const current = remaining[index];
        const replacement = remaining[other];
        if (current && replacement) {
          remaining[index] = replacement;
          remaining[other] = current;
        }
      }
      if (remaining.length > 1 && remaining.at(-1)?.id === previous)
        remaining.reverse();
    }
    const next = remaining.pop();
    previous = next?.id;
    return next;
  };
}

/** 单次强调不在历史重挂载时重播，任务状态立即打断待机和休息。 */
export function companionPlayback(
  engine: CompanionEngine,
  initial: CompanionActivity,
  {
    reactions,
    preview = false,
    motion,
  }: {
    reactions: readonly CompanionReaction[];
    preview?: boolean;
    motion?: CompanionMotion;
  },
) {
  let current = initial;
  let display: CompanionActivity = initial === "completed" ? "idle" : initial;
  let emphasisTimer: ReturnType<typeof setTimeout> | undefined;
  let idleTimer: ReturnType<typeof setTimeout> | undefined;
  let idleReactionTimer: ReturnType<typeof setTimeout> | undefined;
  let active = false;
  let reduced = false;
  let idleSince = Date.now();
  let interactionTimer: ReturnType<typeof setTimeout> | undefined;
  let interacting = false;
  let strollNext = true;
  const nextReaction = reactionDeck(
    reactions.filter((reaction) => preview || reaction.group !== "work"),
  );
  const nextGesture = reactionDeck(
    reactions.filter(
      (reaction) =>
        reaction.group === "action" && !reaction.action?.startsWith("look:"),
    ),
  );
  const nextStroll = reactionDeck(
    reactions.filter(({ action }) => action === "walk" || action === "jump"),
  );
  const nextIdleMood = reactionDeck(
    reactions.filter(({ group, emotion, action }) =>
      group === "action"
        ? action !== "walk" && action !== "jump"
        : group !== "work" && !["00", "01", "02", "05", "06"].includes(emotion),
    ),
  );
  const render = (activity: CompanionActivity) => {
    clearTimeout(idleReactionTimer);
    display = activity;
    if (activity !== "idle") motion?.stop();
    if (interacting) return;
    engine.setEmotion(companionEmotions[activity]);
    if (!active || reduced) engine.renderStatic();
  };
  const scheduleIdle = () => {
    clearTimeout(idleTimer);
    if (!active || interacting || display !== "idle") return;
    const remaining = restAfter - (Date.now() - idleSince);
    if (remaining <= 0) {
      engine.ball.cancelAction();
      render("resting");
      return;
    }
    idleTimer = setTimeout(
      () => {
        if (Date.now() - idleSince >= restAfter) {
          engine.ball.cancelAction();
          render("resting");
        } else {
          if (!reduced) {
            if (motion) {
              // 走动与表情轮流出现，保留安静停顿；自动表演不重置 90 秒休息时钟。
              const reaction = strollNext ? nextStroll() : nextIdleMood();
              strollNext = !strollNext;
              const action = reaction?.action;
              if (
                reaction &&
                ((action !== "walk" && action !== "jump") ||
                  motion.move(action))
              ) {
                engine.setEmotion(reaction.emotion);
                if (action) engine.ball.perform(action);
                idleReactionTimer = setTimeout(() => {
                  engine.ball.cancelAction();
                  render(display);
                }, reaction.duration);
              }
            } else if (preview) {
              const reaction = nextIdleMood();
              if (reaction) {
                engine.setEmotion(reaction.emotion);
                if (reaction.action) engine.ball.perform(reaction.action);
                idleReactionTimer = setTimeout(() => {
                  engine.ball.cancelAction();
                  render(display);
                }, reaction.duration);
              }
            } else
              engine.ball.perform(
                idleGestures[Math.floor(Math.random() * idleGestures.length)] ??
                  "sway",
              );
          }
          scheduleIdle();
        }
      },
      reduced
        ? remaining
        : Math.min(
            remaining,
            motion || preview
              ? 6000 + Math.random() * 6000
              : 12_000 + Math.random() * 13_000,
          ),
    );
  };
  const settle = () => {
    idleSince = Date.now();
    render("idle");
    scheduleIdle();
  };
  const finishInteraction = () => {
    clearTimeout(interactionTimer);
    interacting = false;
    engine.ball.cancelAction();
    render(display);
    if (display === "idle") idleSince = Date.now();
    scheduleIdle();
  };
  const emphasize = (activity: CompanionActivity) => {
    if (activity === "completed" || activity === "stopped") {
      emphasisTimer = setTimeout(
        settle,
        activity === "completed" ? 2000 : 1000,
      );
    } else if (activity === "starting") {
      emphasisTimer = setTimeout(() => render("thinking"), 700);
    }
  };
  render(display);
  if (display !== "idle") emphasize(display);
  return {
    update(activity: CompanionActivity) {
      if (activity === current) return;
      const previous = current;
      current = activity;
      clearTimeout(emphasisTimer);
      clearTimeout(idleTimer);
      clearTimeout(interactionTimer);
      clearTimeout(idleReactionTimer);
      interacting = false;
      motion?.stop();
      engine.ball.cancelAction();
      const wasRunning = ![
        "idle",
        "resting",
        "completed",
        "failed",
        "stopped",
      ].includes(previous);
      if (activity === "idle" || (activity === "completed" && !wasRunning)) {
        settle();
        return;
      }
      render(activity);
      emphasize(activity);
    },
    wake() {
      if (display === "resting") settle();
    },
    interact() {
      if (!active) return;
      motion?.stop();
      clearTimeout(idleReactionTimer);
      clearTimeout(interactionTimer);
      clearTimeout(idleTimer);
      engine.ball.cancelAction();
      if (display === "idle" || display === "resting") {
        display = "idle";
        idleSince = Date.now();
      }
      const canShowExpression = preview || display === "idle";
      const reaction = canShowExpression ? nextReaction() : nextGesture();
      if (!reaction) return;
      interacting = true;
      // 大形象可以体验全部状态；工作中的小头像仅叠加动作，不伪装成完成或失败。
      if (canShowExpression) engine.setEmotion(reaction.emotion);
      if (reaction.action) engine.ball.perform(reaction.action);
      if (reduced) engine.renderStatic();
      interactionTimer = setTimeout(finishInteraction, reaction.duration);
    },
    visibility(visible: boolean, reduceMotion: boolean) {
      active = visible;
      reduced = reduceMotion;
      if (!active || reduced) motion?.stop();
      if ((!active || reduced) && !interacting) render(display);
      engine.ball.setReduced(reduced);
      engine.setActive(active && !reduced);
      if (!active) {
        if (interacting) finishInteraction();
        else engine.ball.cancelAction();
      }
      if (reduced) engine.renderStatic();
      scheduleIdle();
    },
    destroy() {
      motion?.stop();
      clearTimeout(emphasisTimer);
      clearTimeout(idleTimer);
      clearTimeout(interactionTimer);
      clearTimeout(idleReactionTimer);
      engine.destroy();
    },
  };
}
