import { describe, expect, it } from "vitest";

import { i18n } from "./index";

function strings(value: unknown, prefix = ""): Map<string, string> {
  const result = new Map<string, string>();
  if (!value || typeof value !== "object" || Array.isArray(value))
    return result;
  for (const [key, entry] of Object.entries(value)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (typeof entry === "string") result.set(path, entry);
    else for (const pair of strings(entry, path)) result.set(...pair);
  }
  return result;
}

function placeholders(value: string): string[] {
  return [...value.matchAll(/\{\{\s*([^{}]+?)\s*\}\}/g)]
    .map((match) => match[1] ?? "")
    .sort();
}

describe("workspace translations", () => {
  it("英文覆盖所有工作区文案并保持插值参数一致", () => {
    const zh = strings(i18n.getResource("zh-CN", "chat", "workspace"));
    const en = strings(i18n.getResource("en-US", "chat", "workspace"));
    expect([...en.keys()].sort()).toEqual([...zh.keys()].sort());
    for (const [key, english] of en) {
      expect(english, key).not.toMatch(/[\u3400-\u9fff]/);
      expect(placeholders(english), key).toEqual(
        placeholders(zh.get(key) ?? ""),
      );
    }
  });
});
