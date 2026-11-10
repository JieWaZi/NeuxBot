import { expect, it } from "vitest";
import { companionPlayback } from "./playback";

it("准备中只循环原生彗星的圆点轨迹段，不进入头像恢复段", () => {
  const samples = [0, 0.6, 1.2, 1.9, 2.1, 2.5, 30].map((time) =>
    companionPlayback("connecting", time),
  );
  expect(samples.every((sample) => sample.state === "comet")).toBe(true);
  expect(samples[0]?.block).not.toEqual(samples[2]?.block);
  for (const sample of samples) {
    expect(sample.local).toBeGreaterThanOrEqual(0.55);
    expect(sample.local).toBeLessThan(1.65);
  }
});
