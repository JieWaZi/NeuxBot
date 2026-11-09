import { useEffect, useState } from "react";
import type { ExpressionId } from "@neuxbot/bloub/expressions";
import type { CompanionActivity } from "./activity";

const workingFaces: Partial<
  Record<CompanionActivity, readonly ExpressionId[]>
> = {
  thinking: ["attentif", "reveur", "curieux", "determine"],
  working: ["attentif", "determine", "curieux", "fier"],
  searching: ["curieux", "attentif", "mefiant"],
  connecting: ["attentif", "curieux"],
  responding: ["heureux", "complice", "fier", "excite"],
};
const restingFaces: readonly ExpressionId[] = [
  "curieux",
  "reveur",
  "heureux",
  "complice",
  "fier",
  "attentif",
];

/** 编排空闲和工作表情；等待及终态始终由真实业务状态直接决定。 */
export function useWorkingExpression(
  activity: CompanionActivity,
  playing: boolean,
  restingExpression: ExpressionId = "neutre",
): ExpressionId | undefined {
  const resting = activity === "idle" || activity === "resting";
  const faces = resting ? restingFaces : workingFaces[activity];
  const key = resting ? `resting:${restingExpression}` : activity;
  const [cycle, setCycle] = useState({ key, index: 0 });
  if (cycle.key !== key) setCycle({ key, index: 0 });
  const index = cycle.key === key ? cycle.index : 0;
  useEffect(() => {
    if (!playing || !faces) return;
    let timer: ReturnType<typeof setTimeout>;
    const schedule = () => {
      timer = setTimeout(
        () => {
          setCycle((value) => ({ ...value, index: value.index + 1 }));
          schedule();
        },
        (resting ? 2200 : 700) + Math.random() * 2500,
      );
    };
    schedule();
    return () => clearTimeout(timer);
  }, [playing, faces, resting, key]);
  if (resting) {
    const alternatives = restingFaces.filter(
      (face) => face !== restingExpression,
    );
    const position = index % (alternatives.length + 1);
    return position === 0 ? restingExpression : alternatives[position - 1];
  }
  return faces?.[index % faces.length];
}
