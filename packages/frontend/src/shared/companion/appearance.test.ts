import { describe, expect, it } from "vitest";
import {
  companionAppearance,
  companionAvatar,
  encodeCompanionAppearance,
  randomCompanionAvatar,
} from "./appearance";

describe("Bloub 头像配置", () => {
  it("持久化各属性，改名不改变保存的选择", () => {
    const config = {
      material: "particles",
      shape: "hexagone",
      expression: "curieux",
      color: "#38bdf8",
    } as const;
    const saved = encodeCompanionAppearance(config);
    expect(saved).toBe("bloub:particles:hexagone:curieux:38bdf8");
    expect(companionAppearance(saved)).toEqual(config);
    expect(companionAvatar(saved, "新名字")).toBe(saved);
  });
  it("随机种子稳定映射到通用形状，改名保持保存的身份", () => {
    const seed = "companion:0123456789abcdef0123456789abcdef";
    expect(companionAvatar(seed, "改名")).toBe(seed);
    expect(companionAppearance(seed)).toHaveProperty("shape");
    expect(companionAppearance(seed)).not.toHaveProperty("style");
  });
  it("未知配置不执行外部地址，回退可重复的合法配置", () => {
    const value = companionAvatar(
      "https://example.invalid/avatar.svg",
      "Alice",
    );
    expect(value).toMatch(/^bloub:/);
    expect(companionAvatar(value, "Bob")).toBe(value);
    expect(companionAppearance("bloub:unknown:nope:nope:red").material).toBe(
      "plush",
    );
  });
  it("随机组合可以往返保存", () => {
    const saved = randomCompanionAvatar();
    expect(encodeCompanionAppearance(companionAppearance(saved))).toBe(saved);
    expect(companionAppearance(saved).material).toBe("plush");
  });
});
