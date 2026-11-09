import { describe, expect, it } from "vitest";
import {
  BotEngine,
  type BotFrame,
  type RenderedEye,
} from "@neuxbot/bloub/engine";
import { enlargedEyes } from "@neuxbot/bloub/eyes";
import { avatarExpressions, avatarShapes } from "./appearance";

const pair: BotFrame["eyes"] = [-10, 10].map((x) => ({
  d: "M-2,-4h4a4,4 0 0 1 0,8h-4a4,4 0 0 1 0,-8Z",
  matrix: `matrix(1,0,0,1,${x},-30)`,
  w: 12,
  h: 8,
  alpha: 1,
  open: 1,
  rest: 1,
}));

function matrix(eye: RenderedEye) {
  const [a, b, c, d, x, y] = eye.matrix.slice(7, -1).split(",").map(Number);
  if (
    a === undefined ||
    b === undefined ||
    c === undefined ||
    d === undefined ||
    x === undefined ||
    y === undefined
  )
    throw new Error("眼睛矩阵缺少分量");
  return { a, b, c, d, x, y };
}

describe("统一眼睛尺寸", () => {
  it("默认放大15%并保留倾斜和眨眼压缩轴", () => {
    const tilted = pair.map((eye, index) => ({
      ...eye,
      matrix:
        index === 0
          ? "matrix(0.8,0.6,-0.3,0.4,-10,-30)"
          : "matrix(0.6,-0.8,0.016,0.012,10,-30)",
    }));
    const expected = [
      { a: 0.92, b: 0.69, c: -0.345, d: 0.46 },
      { a: 0.69, b: -0.92, c: 0.0184, d: 0.0138 },
    ];
    enlargedEyes(tilted).forEach((eye, index) => {
      const actual = matrix(eye);
      const axes = expected[index];
      if (!axes) throw new Error("缺少预期眼轴");
      for (const axis of ["a", "b", "c", "d"] as const)
        expect(actual[axis]).toBeCloseTo(axes[axis], 10);
    });
  });

  it("向身体内侧增长，保留原外侧边缘并同步扩大双眼间隙", () => {
    const eyes = enlargedEyes(pair);
    const [left, right] = eyes.map(matrix);
    if (!left || !right) throw new Error("缺少双眼");
    // 原上边缘为 -30 - 8/2 = -34，原内侧间隙为 20 - 12 = 8。
    expect(left.y - (8 / 2) * left.d).toBeCloseTo(-34, 10);
    expect(right.y - (8 / 2) * right.d).toBeCloseTo(-34, 10);
    expect(left.y).toBeGreaterThan(-30);
    expect(right.y).toBeGreaterThan(-30);
    expect(right.x - left.x - (12 / 2) * (left.a + right.a)).toBeCloseTo(
      9.2,
      10,
    );
  });

  it("不重写原生路径、透明度、闭眼程度或默认表情尺寸", () => {
    const native = pair.map((eye, index) => ({
      ...eye,
      alpha: index ? 0 : 0.35,
      open: 0.01,
      rest: 0.6,
      matrix: `matrix(1,0,0,0.01,${index ? 10 : -10},-30)`,
    }));
    const saved = structuredClone(native);
    const result = enlargedEyes(native);
    expect(native).toEqual(saved);
    result.forEach((eye, index) => {
      expect(eye).toMatchObject({
        d: pair[0]?.d,
        w: 12,
        h: 8,
        alpha: index ? 0 : 0.35,
        open: 0.01,
        rest: 0.6,
      });
      expect(matrix(eye).d).toBeCloseTo(0.0115, 10);
    });
  });

  it("无放大时保留原几何，空眼睛仍保持为空", () => {
    expect(enlargedEyes(pair, 1)).toEqual(pair);
    expect(enlargedEyes([])).toEqual([]);
  });

  it("外侧眼睛交替经过最高点时位置连续，不切换离散锚点", () => {
    const before = pair.map((eye, index) => ({
      ...eye,
      matrix: `matrix(1,0,0,1,${index ? 10 : -10},${-30 + (index ? 1e-6 : -1e-6)})`,
    }));
    const after = pair.map((eye, index) => ({
      ...eye,
      matrix: `matrix(1,0,0,1,${index ? 10 : -10},${-30 + (index ? -1e-6 : 1e-6)})`,
    }));
    const first = enlargedEyes(before).map(matrix);
    enlargedEyes(after).forEach((eye, index) => {
      const prior = first[index];
      if (!prior) throw new Error("缺少前一帧眼睛");
      const next = matrix(eye);
      expect(Math.hypot(next.x - prior.x, next.y - prior.y)).toBeLessThan(
        0.00001,
      );
    });
  });

  it("24种表情与10种身体的原生采样放大后保持有限、连续", () => {
    for (const shape of avatarShapes) {
      for (const expression of avatarExpressions) {
        const engine = new BotEngine(100, "idle", shape.radii, expression);
        for (const time of [0.1, 0.8, 1.4, 3.6, 6.2]) {
          const first = enlargedEyes(engine.sample(time).eyes).map(matrix);
          const next = enlargedEyes(engine.sample(time + 0.00001).eyes);
          next.forEach((eye, index) => {
            const prior = first[index];
            if (!prior) throw new Error("缺少前一帧眼睛");
            const current = matrix(eye);
            expect(Object.values(current).every(Number.isFinite)).toBe(true);
            expect(
              Math.hypot(current.x - prior.x, current.y - prior.y),
              `${shape.id}/${expression.id}@${time}`,
            ).toBeLessThan(0.1);
            for (const axis of ["a", "b", "c", "d"] as const)
              expect(Math.abs(current[axis] - prior[axis])).toBeLessThan(0.02);
          });
        }
      }
    }
  });
});
