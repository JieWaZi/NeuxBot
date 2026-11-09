import { describe, expect, it } from "vitest";
import { runOriginKey } from "./run-presentation";

// These inputs mirror the public Run contract; origin no longer requires DSL context.
describe("runOriginKey", () => {
  it.each([
    ["manual", "manualRunOrigin"],
    ["retry", "retryRunOrigin"],
    ["agent", "chatRunOrigin"],
    ["automatic_task", "automaticRunOrigin"],
    ["schedule", "automaticRunOrigin"],
    ["api", "automaticRunOrigin"],
    ["unknown", "otherRunOrigin"],
  ])(
    "shows %s origin from public business facts",
    (triggerSource, expected) => {
      expect(runOriginKey({ triggerSource })).toBe(expected);
    },
  );

  it("keeps child origin distinct from its trigger", () => {
    expect(
      runOriginKey({ parentRunId: "parent", triggerSource: "agent" }),
    ).toBe("childRunOrigin");
  });
});
