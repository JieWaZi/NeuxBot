import { expect, it } from "vitest";
import { workflowConnectionRoute, type CanvasBounds } from "./canvas-routing";

const stage = (
  left: number,
  top: number,
  width = 280,
  height = 200,
): CanvasBounds => ({ left, top, width, height });
const length = (points: { x: number; y: number }[]) =>
  points.slice(1).reduce((total, point, index) => {
    const before = points[index];
    if (!before) throw new Error("缺少路径起点");
    return total + Math.abs(point.x - before.x) + Math.abs(point.y - before.y);
  }, 0);

it("相邻行对齐阶段直接竖连，不受其他宽阶段的右边界影响", () => {
  const source = stage(24, 24);
  const target = stage(24, 320);
  const route = workflowConnectionRoute(source, target, [
    source,
    target,
    stage(368, 24, 824),
  ]);
  expect(route).toEqual([
    { x: 164, y: 228 },
    { x: 164, y: 316 },
  ]);
});

it("相邻行错开阶段在行间留白中转弯，保持最短曼哈顿距离", () => {
  const source = stage(368, 24);
  const target = stage(24, 320);
  const route = workflowConnectionRoute(source, target, [source, target]);
  expect(route[0]).toEqual({ x: 508, y: 228 });
  expect(route.at(-1)).toEqual({ x: 164, y: 316 });
  expect(length(route)).toBe(432);
  expect(
    route.every(({ x, y }) => x >= 164 && x <= 508 && y >= 228 && y <= 316),
  ).toBe(true);
});

it.each([false, true])(
  "跨多行依赖和回边仅绕过挡路阶段，不绕行整张画布（回边 %s）",
  (back) => {
    const first = stage(24, 24);
    const middle = stage(24, 320);
    const last = stage(24, 616);
    const route = workflowConnectionRoute(
      back ? last : first,
      back ? first : last,
      [first, middle, last, stage(368, 24, 824)],
    );
    expect(length(route)).toBe(696);
    for (let index = 1; index < route.length; index++) {
      const start = route[index - 1];
      const end = route[index];
      if (!start || !end) throw new Error("缺少路径端点");
      expect(start.x === end.x || start.y === end.y).toBe(true);
      for (const box of [first, middle, last]) {
        const intersects =
          start.x === end.x
            ? start.x > box.left &&
              start.x < box.left + box.width &&
              Math.max(start.y, end.y) > box.top &&
              Math.min(start.y, end.y) < box.top + box.height
            : start.y > box.top &&
              start.y < box.top + box.height &&
              Math.max(start.x, end.x) > box.left &&
              Math.min(start.x, end.x) < box.left + box.width;
        expect(intersects).toBe(false);
      }
    }
  },
);

it("同一行跳过中间阶段时也避开卡片", () => {
  const source = stage(24, 24);
  const middle = stage(368, 24);
  const target = stage(712, 24);
  const route = workflowConnectionRoute(source, target, [
    source,
    middle,
    target,
  ]);
  expect(route[0]).toEqual({ x: 308, y: 56 });
  expect(route.at(-1)).toEqual({ x: 708, y: 56 });
  expect(length(route)).toBe(496);
});
