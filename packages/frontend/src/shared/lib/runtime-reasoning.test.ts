import { describe, expect, it } from "vitest";

import { isMaximumReasoning, minimumReasoning } from "./runtime-reasoning";

describe("具体思考强度的最高档判断", () => {
  it.each(["enabled", "disabled", "on", "off", "default", "auto"])(
    "%s 排在末尾也不作为最高档，保留实际 Max 档位",
    (id) => {
      const options = [
        { id: "minimal", name: "Minimal" },
        { id: "max", name: "Max" },
        { id, name: "On (default)" },
      ];
      expect(isMaximumReasoning(options, id)).toBe(false);
      expect(isMaximumReasoning(options, "", id)).toBe(false);
      expect(isMaximumReasoning(options, "max")).toBe(true);
      expect(isMaximumReasoning(options, "", "max")).toBe(true);
    },
  );

  it("只有开关项时不展示最高强度效果", () => {
    expect(
      isMaximumReasoning([{ id: "disabled" }, { id: "enabled" }], "enabled"),
    ).toBe(false);
  });

  it("唯一的具体强度也是最高档，默认继承该档时保持一致", () => {
    const options = [{ id: "high", name: "High" }];
    expect(isMaximumReasoning(options, "high")).toBe(true);
    expect(isMaximumReasoning(options, "", "high")).toBe(true);
    expect(isMaximumReasoning(options, "enabled")).toBe(false);
    expect(isMaximumReasoning([], "high")).toBe(false);
  });
});

it("最低档取真实选项，不受目录顺序影响，支持关闭与单档模型", () => {
  expect(minimumReasoning([{ id: "high" }, { id: "low" }])).toBe("low");
  expect(minimumReasoning([{ id: "high" }, { id: "off" }])).toBe("off");
  expect(minimumReasoning([{ id: "high" }])).toBe("high");
  expect(minimumReasoning([])).toBe("");
});
