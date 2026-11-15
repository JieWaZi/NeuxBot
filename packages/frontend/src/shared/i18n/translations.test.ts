import { describe, expect, it } from "vitest";
import { i18n } from "./index";

const modules = import.meta.glob<Record<string, unknown>>(
  ["./*.ts", "!./*.test.ts", "!./format.ts", "!./language.ts"],
  { eager: true },
);

function strings(value: unknown, prefix = ""): Map<string, string> {
  const result = new Map<string, string>();
  if (!value || typeof value !== "object") return result;
  for (const [key, entry] of Object.entries(value)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (typeof entry === "string") result.set(path, entry);
    else
      for (const [key, value] of strings(entry, path)) result.set(key, value);
  }
  return result;
}

function placeholders(value: string) {
  return [...value.matchAll(/\{\{\s*([^{}]+?)\s*\}\}/g)]
    .map((match) => match[1])
    .sort();
}

function check(zh: unknown, en: unknown) {
  const chinese = strings(zh);
  const english = strings(en);
  expect(chinese.size).toBeGreaterThan(0);
  for (const [key, value] of chinese) {
    const translated = english.get(key);
    expect(translated, key).toBeDefined();
    expect(translated?.trim(), key).not.toBe("");
    if (!key.endsWith("languages.chinese"))
      expect(translated, key).not.toMatch(/\p{Script=Han}/u);
    expect(placeholders(translated ?? ""), key).toEqual(placeholders(value));
  }
}

describe("中英文文案对齐", () => {
  it("基础界面没有中文回退且保留全部模板参数", () => {
    check(i18n.store.data["zh-CN"], i18n.store.data["en-US"]);
  });
  for (const [file, module] of Object.entries(modules)) {
    for (const key of Object.keys(module).filter((key) => key.endsWith("Zh"))) {
      it(`${file}:${key} 的英文覆盖中文键及模板参数`, () => {
        check(module[key], module[key.replace(/Zh$/, "En")]);
      });
    }
  }
});
