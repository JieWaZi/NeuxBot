import { useEffect, useState } from "react";
import type { ExpressionId } from "@neuxbot/bloub/expressions";
import type { CompanionActivity } from "./activity";
import { nextInteractionExpression } from "./interaction-expressions";

const workingFaces: Partial<
  Record<CompanionActivity, readonly ExpressionId[]>
> = {
  thinking: ["attentif", "reveur", "curieux", "determine"],
  working: ["attentif", "determine", "curieux", "fier"],
  searching: ["curieux", "attentif", "mefiant"],
  connecting: ["attentif", "curieux"],
  responding: ["heureux", "complice", "fier", "excite"],
};
/** 编排空闲和工作表情；等待及终态始终由真实业务状态直接决定。 */
export function useWorkingExpression(
  activity: CompanionActivity,
  playing: boolean,
  restingExpression: ExpressionId = "neutre",
): ExpressionId | undefined {
  const resting = activity === "idle" || activity === "resting";
  const faces = workingFaces[activity];
  const key = resting ? `resting:${restingExpression}` : activity;
  const [cycle, setCycle] = useState({
    key,
    index: 0,
    face: restingExpression,
  });
  if (cycle.key !== key) setCycle({ key, index: 0, face: restingExpression });
  const index = cycle.key === key ? cycle.index : 0;
  useEffect(() => {
    if (!playing || (!resting && !faces)) return;
    let timer: ReturnType<typeof setTimeout>;
    const schedule = () => {
      timer = setTimeout(
        () => {
          const seed = resting ? Math.random() : 0;
          setCycle((value) => ({
            ...value,
            index: value.index + 1,
            face: resting
              ? nextInteractionExpression(value.face, seed)
              : value.face,
          }));
          schedule();
        },
        resting ? 10_000 + Math.random() * 8000 : 700 + Math.random() * 2500,
      );
    };
    schedule();
    return () => clearTimeout(timer);
  }, [playing, faces, resting, key]);
  if (resting) return cycle.key === key ? cycle.face : restingExpression;
  return faces?.[index % faces.length];
}
