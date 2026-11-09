import { describe, expect, it } from "vitest";
import { EXPRESSIONS } from "@neuxbot/bloub/expressions";
import { nextInteractionExpression } from "./interaction-expressions";

describe("点击表情", () => {
  it("从整个表情目录选择，每次排除当前表情", () => {
    for (const current of EXPRESSIONS) {
      const reached = new Set(
        Array.from({ length: 240 }, (_, index) =>
          nextInteractionExpression(current.id, (index + 0.5) / 240),
        ),
      );
      expect(reached.has(current.id)).toBe(false);
      expect(reached.size).toBe(23);
      expect(
        [...reached].every((id) => EXPRESSIONS.some((face) => face.id === id)),
      ).toBe(true);
    }
  });

  it("边界或无效随机值仍返回另一种合法表情", () => {
    for (const seed of [0, 1, -1, NaN, Infinity, -Infinity]) {
      const next = nextInteractionExpression("neutre", seed);
      expect(next).not.toBe("neutre");
      expect(EXPRESSIONS.some((face) => face.id === next)).toBe(true);
    }
  });
});
