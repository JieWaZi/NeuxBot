import { describe, expect, it, vi } from "vitest";
import { readingTurnIndex } from "./chat-turn-geometry";

describe("reading turn geometry", () => {
  it("uses one DOM scan and logarithmic geometry reads for 200 turns", () => {
    const viewport = document.createElement("div");
    Object.defineProperty(viewport, "clientHeight", { value: 1000 });
    const measurements = vi.fn();
    const ids = Array.from({ length: 200 }, (_, index) => `message-${index}`);
    for (const [index, id] of ids.entries()) {
      const row = document.createElement("div");
      row.dataset.messageId = id;
      row.getBoundingClientRect = () => {
        measurements();
        return new DOMRect(0, index * 100 - 10_000, 800, 100);
      };
      viewport.append(row);
    }
    const scan = vi.spyOn(viewport, "querySelectorAll");
    expect(readingTurnIndex(viewport, ids)).toBe(103);
    expect(scan).toHaveBeenCalledTimes(1);
    expect(measurements.mock.calls.length).toBeLessThanOrEqual(8);
    viewport.replaceChildren();
    expect(readingTurnIndex(viewport, ids)).toBe(0);
  });
});
