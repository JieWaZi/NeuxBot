import { describe, expect, it } from "vitest";
import { BotEngine } from "@neuxbot/bloub/engine";
import { SHAPE_BY_ID } from "@neuxbot/bloub/skins";
import { EXPRESSION_BY_ID } from "@neuxbot/bloub/expressions";
import { faceOf } from "@neuxbot/bloub/motion";
import { avatarDrawing } from "@neuxbot/bloub/drawing";
import { avatarExpressions, avatarShapes, avatarMaterials } from "./appearance";
import { companionPresentation } from "./playback";
import { POSES, SEQUENCE } from "@neuxbot/bloub/states";
import { radialBodyWarp, silhouetteTransform } from "@neuxbot/bloub/warp";
import type { CompanionActivity } from "./activity";

function required<T>(value: T | undefined): T {
  if (value === undefined) throw new Error("缺少测试目录项");
  return value;
}
const activities: CompanionActivity[] = [
  "idle",
  "resting",
  "starting",
  "thinking",
  "searching",
  "working",
  "connecting",
  "responding",
  "waiting",
  "completed",
  "failed",
  "stopped",
];
describe("Bloub 组合与连续状态", () => {
  it("SVG径向变形把整张身体的横向半径映射为两倍，并保持纵向与自身透明边缘", () => {
    const slices = radialBodyWarp([1, 1, 1, 1], [2, 1, 2, 1]);
    expect(slices).toHaveLength(4);
    for (const slice of slices) {
      const values = slice.matrix.slice(7, -1).split(",").map(Number);
      expect(values[0]).toBeCloseTo(2);
      expect(values[1]).toBeCloseTo(0);
      expect(values[2]).toBeCloseTo(0);
      expect(values[3]).toBeCloseTo(1);
    }
    expect(radialBodyWarp([1, 1, 1, 1], [1, 1, 1, 1])).toEqual([]);
    expect(
      silhouetteTransform({
        radii: [],
        rot: Math.PI / 2,
        sx: 2,
        sy: 0.5,
        cx: 0.1,
        cy: -0.2,
      }),
    ).toBe("translate(10,-20) scale(2,0.5) rotate(90)");
  });
  it("每个形状、默认表情与业务状态都可生成有限轮廓和两只眼睛", () => {
    for (const shape of avatarShapes)
      for (const expression of avatarExpressions)
        for (const activity of activities) {
          const pose = companionPresentation(activity);
          const base = required(
            EXPRESSION_BY_ID.get(pose.expression ?? expression.id),
          );
          const engine = new BotEngine(
            100,
            pose.state,
            shape.radii,
            faceOf(pose.motion, base) ?? base,
          );
          const frame = engine.sample(1.4);
          expect(frame.bodyPath).toMatch(/^M/);
          expect(frame.eyes.length).toBeLessThanOrEqual(2);
          expect(JSON.stringify(frame)).not.toMatch(/NaN|Infinity/);
          function finite(value: unknown): void {
            if (typeof value === "number")
              expect(Number.isFinite(value)).toBe(true);
            else if (Array.isArray(value)) value.forEach(finite);
            else if (value && typeof value === "object")
              Object.values(value).forEach(finite);
          }
          finite(frame);
        }
  });
  it("采样轮廓供完整身体变形使用，调用方修改轮廓不会污染引擎", () => {
    const engine = new BotEngine(100, "egg");
    const first = engine.sample(0.8);
    const original = engine.sample(0.8);
    expect(first.silhouette.radii).toHaveLength(64);
    first.silhouette.radii[0] = 999;
    expect(engine.sample(0.8)).toEqual(original);
  });
  it("原生思考、感叹与爆散的装饰按前后层实际绘制，几何动作变形立体图格", () => {
    for (const state of SEQUENCE) {
      const engine = new BotEngine(100, state);
      const frame = engine.sample(POSES[state]);
      const drawing = avatarDrawing(
        frame,
        "plush",
        "#8b5cf6",
        "native",
        "/body.png",
        [],
        [],
        undefined,
        "cercle",
      );
      const flat = drawing.flatMap(function collect(node): typeof drawing {
        return [node, ...node.children.flatMap(collect)];
      });
      expect(
        flat.filter((node) => node.attributes["data-native-dot"] !== undefined),
      ).toHaveLength(frame.dots.length);
      if (frame.dots.length) {
        const dotIndex = drawing.findIndex(
          (node) => node.attributes["data-native-dot"] !== undefined,
        );
        const bodyIndex = drawing.findIndex(
          (node) => node.attributes["data-body-material"] !== undefined,
        );
        expect(dotIndex < bodyIndex).toBe(frame.dotsBehind);
      }
      if (frame.notif) {
        expect(
          flat.find(
            (node) => node.attributes["data-native-notification"] !== undefined,
          )?.attributes.fill,
        ).toBe("#2496e8");
        expect(flat.some((node) => node.tag === "mask")).toBe(true);
      }
      if (
        [
          "thinking",
          "alert",
          "exclaim",
          "sleep",
          "egg",
          "hexagon",
          "play",
          "orbit",
          "burst",
          "comet",
        ].includes(state)
      ) {
        expect(
          flat.some((node) => node.attributes["data-body-warp"] !== undefined),
        ).toBe(true);
      }
    }
  });
  it("在过渡过程中快速进入等待和失败仍产生连续合法图形", () => {
    const engine = new BotEngine(
      100,
      "idle",
      required(SHAPE_BY_ID.get("hexagone")).radii,
      required(EXPRESSION_BY_ID.get("neutre")),
    );
    let now = 0;
    for (const activity of [
      "working",
      "waiting",
      "failed",
      "stopped",
      "idle",
    ] as const) {
      now += 0.06;
      const pose = companionPresentation(activity);
      engine.setState(pose.state, now);
      const expression = required(
        EXPRESSION_BY_ID.get(pose.expression ?? "neutre"),
      );
      engine.setExpression(faceOf(pose.motion, expression) ?? expression, now);
      expect(JSON.stringify(engine.sample(now))).not.toContain("NaN");
      expect(engine.sample(now)).toEqual(engine.sample(now));
    }
  });
  it.each(avatarMaterials)(
    "%s 的每种身体采样正确图格，身体 SVG 不被按钮图标规则缩小",
    (material) => {
      for (const [index, shape] of avatarShapes.entries()) {
        const engine = new BotEngine(
          100,
          "idle",
          shape.radii,
          required(EXPRESSION_BY_ID.get("curieux")),
        );
        const frame = engine.sample(1.4);
        const drawing = avatarDrawing(
          frame,
          material,
          "#38bdf8",
          "preview",
          "/local-body.png",
          [],
          [],
          undefined,
          shape.id,
        );
        const flat = drawing.flatMap(function collect(node): typeof drawing {
          return [node, ...node.children.flatMap(collect)];
        });
        expect(
          flat.filter((node) => node.attributes["data-eye"] !== undefined),
        ).toHaveLength(2);
        const sprite = flat.find((node) => node.tag === "svg");
        expect(sprite?.attributes.viewBox).toBe(
          `${(index % 4) * 362} ${Math.floor(index / 4) * 362} 362 362`,
        );
        expect(sprite?.attributes["data-companion-avatar"]).toBe("");
        expect(sprite?.attributes["data-body-sprite"]).toBe(shape.id);
        expect(flat.find((node) => node.tag === "image")?.attributes.href).toBe(
          "/local-body.png",
        );
        expect(
          flat.find((node) => node.tag === "image")?.attributes.width,
        ).toBe(1448);
        expect(
          flat.find((node) => node.tag === "image")?.attributes.height,
        ).toBe(1086);
        expect(
          flat.some(
            (node) => node.tag === "pattern" || node.tag === "clipPath",
          ),
        ).toBe(false);
      }
    },
  );
});
