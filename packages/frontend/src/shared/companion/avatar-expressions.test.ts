import { expect, it } from "vitest";
import { EXPRESSION_BY_ID } from "@neuxbot/bloub/expressions";
import { avatarExpressionById } from "./avatar-expressions";

function required<T>(value: T | undefined): T {
  if (value === undefined) throw new Error("缺少表情数据");
  return value;
}

it("收敛大圆眼、小圆点和细眯眼，同时保留表情姿态", () => {
  for (const id of ["surpris", "excite", "effraye"]) {
    const face = required(avatarExpressionById.get(id));
    expect(face.eyes.every((eye) => eye.w <= 0.34)).toBe(true);
    expect(face.eyes.every((eye) => eye.h > eye.w)).toBe(true);
  }
  const dreamy = required(avatarExpressionById.get("reveur"));
  expect(dreamy.eyes[0].w).toBeGreaterThan(0.13);
  expect(dreamy.eyes[0].h).toBeGreaterThan(dreamy.eyes[0].w);
  for (const id of ["timide", "determine", "inquiet"])
    expect(
      required(avatarExpressionById.get(id)).eyes[0].w,
    ).toBeGreaterThanOrEqual(0.21);
  for (const id of ["meditatif", "gene", "blase", "hilare"])
    expect(
      required(avatarExpressionById.get(id)).eyes.every((eye) => eye.h >= 0.14),
    ).toBe(true);
  for (const [id, face] of avatarExpressionById) {
    const native = required(EXPRESSION_BY_ID.get(id));
    expect(face.gaze).toEqual(native.gaze);
    expect(face.split).toBe(native.split);
    face.eyes.forEach((eye, index) => {
      expect(eye.open).toBe(required(native.eyes[index]).open);
      expect(eye.tilt).toBe(required(native.eyes[index]).tilt);
    });
  }
  expect(avatarExpressionById.get("heureux")).toBe(
    EXPRESSION_BY_ID.get("heureux"),
  );
  expect(required(EXPRESSION_BY_ID.get("reveur")).eyes[0].w).toBe(0.13);
});
