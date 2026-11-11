import type { ExpressionId } from "@neuxbot/bloub/expressions";

export type HoverFace = {
  state: "idle" | "wink";
  expression?: ExpressionId;
};

// 悬停只借用友好表情；眼睛和身体的默认选择仍由伙伴外观拥有。
const hoverFaces: readonly HoverFace[] = [
  { state: "wink" },
  { state: "idle", expression: "surpris" },
  { state: "idle", expression: "excite" },
  { state: "idle", expression: "attentif" },
  { state: "idle", expression: "curieux" },
];

/** 每次进入选一次，避开保存的眼睛与上次反馈；离开由视图恢复默认表情。 */
export function nextHoverFace(
  resting: ExpressionId,
  previous: HoverFace | null,
  seed = Math.random(),
): HoverFace {
  const choices = hoverFaces.filter(
    (face) => face !== previous && face.expression !== resting,
  );
  const unit = Number.isFinite(seed) ? Math.min(Math.max(seed, 0), 1) : 0;
  const index = Math.min(Math.floor(unit * choices.length), choices.length - 1);
  return choices[index] ?? { state: "wink" };
}
