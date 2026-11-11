import { describe, expect, it } from "vitest";

import { selectableRuntimes } from "./selectable-runtimes";

describe("selectableRuntimes", () => {
  it("聊天允许尝试所有已启用的运行时", () => {
    const runtimes = [
      { id: "codex", enabled: true, state: "ready" },
      { id: "claude", enabled: true, state: "unavailable" },
      { id: "pi", enabled: false, state: "ready" },
    ];

    expect(selectableRuntimes(runtimes).map((runtime) => runtime.id)).toEqual([
      "codex",
      "claude",
    ]);
  });
});
