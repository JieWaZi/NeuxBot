import { describe, expect, it } from "vitest";
import { BotEngine, type RenderedEye } from "@neuxbot/bloub/engine";
import {
  avatarExpressions as EXPRESSIONS,
  avatarExpressionById as EXPRESSION_BY_ID,
} from "./avatar-expressions";
import {
  avatarShapes,
  companionAppearance,
  encodeCompanionAppearance,
} from "./appearance";

const originalIds = [
  "neutre",
  "attentif",
  "surpris",
  "excite",
  "heureux",
  "hilare",
  "colere",
  "triste",
  "effraye",
  "mefiant",
  "confus",
  "curieux",
  "fier",
  "timide",
  "blase",
  "somnolent",
];
const addedIds = [
  "complice",
  "determine",
  "reveur",
  "inquiet",
  "espiegle",
  "meditatif",
  "ebloui",
  "gene",
];
type Point = { x: number; y: number };

function required<T>(value: T | undefined): T {
  if (value === undefined) throw new Error("缺少测试几何数据");
  return value;
}

function bodyContour(path: string): Point[] {
  return path
    .slice(1)
    .split("C")
    .map((segment) => {
      const values = segment.match(/-?\d+\.?\d*/g)?.map(Number) ?? [];
      return values.length >= 6
        ? { x: required(values[4]), y: required(values[5]) }
        : { x: required(values[0]), y: required(values[1]) };
    });
}

function contains(points: Point[], point: Point): boolean {
  let inside = false;
  for (
    let index = 0, previous = points.length - 1;
    index < points.length;
    previous = index++
  ) {
    const a = required(points[index]);
    const b = required(points[previous]);
    if (
      a.y > point.y !== b.y > point.y &&
      point.x < ((b.x - a.x) * (point.y - a.y)) / (b.y - a.y) + a.x
    )
      inside = !inside;
  }
  return inside;
}

/** Sample both round caps of the rendered capsule, including horizontal eyes. */
function eyeContour(eye: RenderedEye): Point[] {
  const matrix = required(eye.matrix.match(/-?\d+\.?\d*/g) ?? undefined).map(
    Number,
  );
  const [a, b, c, d, e, f] = matrix;
  if (
    a === undefined ||
    b === undefined ||
    c === undefined ||
    d === undefined ||
    e === undefined ||
    f === undefined
  )
    throw new Error("眼睛矩阵缺少分量");
  expect(
    [...matrix, eye.w, eye.h, eye.alpha, eye.open, eye.rest].every(
      Number.isFinite,
    ),
  ).toBe(true);
  const vertical = eye.h > eye.w;
  const radius = Math.min(eye.w, eye.h) / 2;
  const halfAxis = Math.max(eye.w, eye.h) / 2 - radius;
  return Array.from({ length: 48 }, (_, index) => {
    const angle = (index / 48) * Math.PI * 2;
    const x =
      radius * Math.cos(angle) +
      (vertical ? 0 : Math.sign(Math.cos(angle)) * halfAxis);
    const y =
      radius * Math.sin(angle) +
      (vertical ? Math.sign(Math.sin(angle)) * halfAxis : 0);
    return { x: a * x + c * y + e, y: b * x + d * y + f };
  });
}

describe("扩充表情目录", () => {
  it("保留旧配置并将八种独立眼型纳入同一目录", () => {
    expect(EXPRESSIONS.map(({ id }) => id).slice(0, 16)).toEqual(originalIds);
    expect(EXPRESSIONS).toHaveLength(24);
    expect(
      new Set(
        EXPRESSIONS.map(({ gaze, split, eyes }) =>
          JSON.stringify([gaze, split, eyes]),
        ),
      ).size,
    ).toBe(24);
    for (const id of [...originalIds, ...addedIds]) {
      const expression = EXPRESSION_BY_ID.get(id);
      expect(expression, id).toBeDefined();
      if (!expression) continue;
      const config = {
        material: "plush",
        shape: "cercle",
        expression: expression.id,
        color: "#8b5cf6",
      } as const;
      expect(companionAppearance(encodeCompanionAppearance(config))).toEqual(
        config,
      );
    }
  });

  it("校准表情在十种身体和视线漂移中保持两眼有限且完整落在身体内", () => {
    const overflow: string[] = [];
    for (const shape of avatarShapes) {
      for (const { id } of EXPRESSIONS) {
        const expression = EXPRESSION_BY_ID.get(id);
        expect(expression, id).toBeDefined();
        if (!expression) continue;
        for (const state of ["idle", "swirl"] as const) {
          const engine = new BotEngine(100, state, shape.radii, expression);
          for (let time = 0; time < 8; time += 0.2) {
            const frame = engine.sample(time);
            expect(
              JSON.stringify(frame),
              `${shape.id}/${id}/${state}`,
            ).not.toMatch(/NaN|Infinity/);
            expect(frame.eyes).toHaveLength(2);
            const body = bodyContour(frame.bodyPath);
            for (const eye of frame.eyes) {
              for (const point of eyeContour(eye)) {
                if (!contains(body, point))
                  overflow.push(
                    `${shape.id}/${id}/${state}@${time.toFixed(1)}`,
                  );
              }
            }
          }
        }
      }
    }
    expect([...new Set(overflow)]).toEqual([]);
  });
});
