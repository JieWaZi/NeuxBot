import { expect, it } from "vitest";
import { workflowGenerationLayout } from "./workflow-generation-layout";

it.each([
  [280, 120],
  [400, 600],
  [607, 400],
  [608, 300],
  [1024, 400],
  [1600, 900],
])("画布 %sx%s 时三个阶段保持横向且整体适配播放区域", (width, height) => {
  const layout = workflowGenerationLayout(width, height);
  expect(layout.displayWidth).toBeLessThanOrEqual(width);
  expect(
    (layout.displayWidth * layout.height) / layout.width,
  ).toBeLessThanOrEqual(height);
  expect(layout.phases).toHaveLength(3);
  for (const [index, phase] of layout.phases.entries()) {
    expect(phase.x).toBeGreaterThanOrEqual(0);
    expect(phase.y).toBeGreaterThanOrEqual(0);
    expect(phase.x + layout.phaseWidth).toBeLessThanOrEqual(layout.width);
    expect(phase.y + phase.height).toBeLessThanOrEqual(layout.height);
    const next = layout.phases[index + 1];
    if (!next) continue;
    expect(next.x).toBeGreaterThan(phase.x + layout.phaseWidth);
    expect(next.y).toBe(phase.y);
  }
});
