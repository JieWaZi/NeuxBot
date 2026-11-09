import type { ExpressionId } from "@neuxbot/bloub/expressions";
import { STATE_BY_ID, type StateId } from "@neuxbot/bloub/states";
import type { PlushState } from "@neuxbot/bloub/plush-catalogue";
import type { CompanionActivity } from "./activity";

export type AvatarPresentation = {
  state: StateId;
  motion: PlushState;
  expression?: ExpressionId;
  scan?: boolean;
};
const presentations: Record<CompanionActivity, AvatarPresentation> = {
  resting: { state: "idle", motion: "idle" },
  idle: { state: "idle", motion: "idle" },
  starting: { state: "wide", motion: "wide" },
  thinking: {
    state: "thinking",
    motion: "thinking",
    expression: "attentif",
  },
  searching: {
    state: "egg",
    motion: "idle",
    expression: "curieux",
    scan: true,
  },
  working: {
    state: "thinking",
    motion: "thinking",
    expression: "attentif",
  },
  connecting: { state: "swirl", motion: "idle", expression: "attentif" },
  responding: {
    state: "play",
    motion: "idle",
    expression: "heureux",
  },
  waiting: { state: "wide", motion: "notify", expression: "curieux" },
  completed: { state: "exclaim", motion: "idle", expression: "heureux" },
  failed: { state: "idle", motion: "failed" },
  stopped: { state: "idle", motion: "idle", expression: "neutre" },
};

/** 工作状态由调用方拥有，这里只选择原生动作及业务辅助姿态。 */
export function companionPresentation(
  activity: CompanionActivity,
): AvatarPresentation {
  return presentations[activity];
}

const timelines: Partial<Record<CompanionActivity, readonly StateId[]>> = {
  // 原生动作拥有自己的眼睛；交错的空闲段呈现轮换表情，
  // 同时保留原生动作的轮廓和完整时长。
  thinking: ["idle", "thinking", "idle", "orbit", "idle", "comet"],
  working: ["idle", "thinking", "idle", "orbit", "idle", "comet"],
  searching: ["idle", "egg", "idle", "hexagon", "idle", "orbit"],
  responding: ["play", "idle"],
  waiting: ["wide", "alert", "notify"],
  completed: ["exclaim", "notify", "burst"],
};
const duration = (state: StateId) => STATE_BY_ID.get(state)?.duration ?? 2.4;
export const COMPLETION_DURATION = (timelines.completed ?? []).reduce(
  (sum, state) => sum + duration(state),
  0,
);

/** 原生动作的分段计时只使用头像的可暂停时钟，不反向改变业务状态。 */
export function companionPlayback(
  activity: CompanionActivity,
  elapsed: number,
  animation?: StateId,
): { state: StateId; local: number; block: string } {
  const sequence = animation
    ? [animation === "sleep" ? "idle" : animation]
    : timelines[activity];
  const time = Math.max(0, elapsed);
  if (!sequence)
    return {
      state: presentations[activity].state,
      local: time,
      block: "steady",
    };
  const total = sequence.reduce((sum, state) => sum + duration(state), 0);
  if (!animation && activity === "completed" && time >= total)
    return { state: "idle", local: time - total, block: "done" };
  const cycle = Math.floor(time / total);
  let local = time % total;
  for (const [index, state] of sequence.entries()) {
    if (local < duration(state))
      return { state, local, block: `${cycle}:${index}` };
    local -= duration(state);
  }
  return { state: "idle", local: 0, block: "done" };
}
