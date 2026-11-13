import type { ExpressionId } from "@neuxbot/bloub/expressions";
import { STATE_BY_ID, type StateId } from "@neuxbot/bloub/states";
import type { PlushState } from "@neuxbot/bloub/plush-catalogue";
import type { CompanionActivity, CompanionAttention } from "./activity";

export type AvatarPresentation = {
  state: StateId;
  motion: PlushState;
  expression?: ExpressionId;
};
const idle: AvatarPresentation = { state: "idle", motion: "idle" };
const thinking: AvatarPresentation = { state: "thinking", motion: "thinking" };
const presentations: Record<CompanionActivity, AvatarPresentation> = {
  resting: idle,
  idle,
  starting: thinking,
  thinking,
  searching: thinking,
  working: thinking,
  connecting: thinking,
  responding: { state: "wink", motion: "idle" },
  waiting: idle,
  completed: { state: "notify", motion: "notify" },
  failed: { state: "alert", motion: "failed" },
  stopped: idle,
};
const attentionPresentations: Record<
  CompanionAttention["kind"],
  AvatarPresentation
> = {
  working: { state: "working", motion: "thinking" },
  "waiting-answer": { state: "waiting-answer", motion: "idle" },
  "waiting-approval": { state: "waiting-approval", motion: "idle" },
  unread: { state: "unread", motion: "notify" },
};

/** 工作状态由调用方拥有，这里只选择对应的原生动作和固定表情。 */
export function companionPresentation(
  activity: CompanionActivity,
  attention?: CompanionAttention["kind"],
): AvatarPresentation {
  return attention
    ? attentionPresentations[attention]
    : presentations[activity];
}

const duration = (state: StateId) => STATE_BY_ID.get(state)?.duration ?? 2.4;

/** 原生动作的分段计时只使用头像的可暂停时钟，不反向改变业务状态。 */
export function companionPlayback(
  activity: CompanionActivity,
  elapsed: number,
  animation?: StateId,
  attention?: CompanionAttention["kind"],
): { state: StateId; local: number; block: string } {
  const time = Math.max(0, elapsed);
  const state =
    animation === "sleep"
      ? "idle"
      : (animation ?? companionPresentation(activity, attention).state);
  // 业务状态持续保持同一原生动作；只有显式动作预览播放完整循环。
  if (!animation) return { state, local: time, block: "steady" };
  const total = duration(state);
  return { state, local: time % total, block: `${Math.floor(time / total)}:0` };
}
