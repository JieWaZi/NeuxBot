import type { ExpressionId } from "@neuxbot/bloub/expressions";
import { useEffect, useState, type RefObject } from "react";

const expressions = [
  "heureux",
  "curieux",
  "complice",
  "attentif",
  "espiegle",
  "surpris",
  "reveur",
  "hilare",
] as const satisfies readonly ExpressionId[];

/** 装饰头像只轮换轻松、友好的表情，每次避开当前表情。 */
export function nextWorkBoardAvatarExpression(
  current: ExpressionId,
  seed: number,
): ExpressionId {
  const choices = expressions.filter((expression) => expression !== current);
  const unit = Number.isFinite(seed) ? Math.max(0, Math.min(1, seed)) : 0;
  const index = Math.min(choices.length - 1, Math.floor(unit * choices.length));
  return choices[index] ?? "heureux";
}

/** 每个装饰头像独立计时；恢复可见时重新等待，不补播隐藏期间的变化。 */
export function useWorkBoardAvatarExpression(
  host: RefObject<HTMLElement | null>,
  initial: ExpressionId,
) {
  const [expression, setExpression] = useState(initial);
  useEffect(() => {
    const element = host.current;
    if (!element) return;
    const reducedMotion = window.matchMedia?.(
      "(prefers-reduced-motion: reduce)",
    );
    let disposed = false;
    let visible = typeof IntersectionObserver === "undefined";
    let timer: number | undefined;
    const playing = () =>
      !disposed && visible && !document.hidden && !reducedMotion?.matches;
    const sync = () => {
      if (!playing()) {
        window.clearTimeout(timer);
        timer = undefined;
      } else if (timer === undefined) {
        timer = window.setTimeout(
          () => {
            timer = undefined;
            if (!playing()) return;
            const seed = Math.random();
            setExpression((previous) =>
              nextWorkBoardAvatarExpression(previous, seed),
            );
            sync();
          },
          3000 + Math.random() * 3000,
        );
      }
    };
    const observer =
      typeof IntersectionObserver === "undefined"
        ? undefined
        : new IntersectionObserver(([entry]) => {
            visible = Boolean(entry?.isIntersecting);
            sync();
          });
    observer?.observe(element);
    document.addEventListener("visibilitychange", sync);
    reducedMotion?.addEventListener("change", sync);
    sync();
    return () => {
      disposed = true;
      window.clearTimeout(timer);
      observer?.disconnect();
      document.removeEventListener("visibilitychange", sync);
      reducedMotion?.removeEventListener("change", sync);
    };
  }, [host]);
  return expression;
}
