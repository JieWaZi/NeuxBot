import { EXPRESSIONS, type ExpressionId } from "@neuxbot/bloub/expressions";

/** Adapted from dsh-bloub-mood's full-pool selection; see @neuxbot/bloub/expressions-source.json. */
export function nextInteractionExpression(
  current: ExpressionId,
  seed = Math.random(),
): ExpressionId {
  const candidates = EXPRESSIONS.filter(({ id }) => id !== current);
  const unit = Number.isFinite(seed) ? Math.min(Math.max(seed, 0), 1) : 0;
  const index = Math.min(
    Math.floor(unit * candidates.length),
    candidates.length - 1,
  );
  return candidates[index]?.id ?? "neutre";
}
