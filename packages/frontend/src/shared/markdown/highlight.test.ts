// Adapted behavior cases from DeepSeek Harness ce7073d51254c26cd63b22a60835b8f948deb447.
// Copyright (c) 2026 DeepSeek. MIT License; see THIRD_PARTY_NOTICES.md.
import { describe, expect, it } from "vitest";
import {
  StreamingHighlightSession,
  grammarForHint,
  highlightLines,
  patchGoGrammar,
  highlightToHtml,
} from "./highlight";

describe("流式代码高亮", () => {
  it("Go struct 规则保留上游回溯修复且不修改原语法", async () => {
    const { default: grammars } = await import("shiki/langs/go.mjs");
    const original = JSON.stringify(grammars);
    const patched = patchGoGrammar(grammars);
    expect(JSON.stringify(grammars)).toBe(original);
    expect(JSON.stringify(patched)).not.toBe(original);
    expect(
      patched
        .find((grammar) => grammar.name === "go")
        ?.repository.struct_variables_types_fields?.patterns?.some(
          (rule) =>
            rule.match === '(?:(?<=\\{)([^/\\`"]*[^\\s/\\`"])\\s*(?=\\}))',
        ),
    ).toBe(true);
  });
  it("仅追加完成行，保持旧行身份并等价于完整高亮", () => {
    const session = new StreamingHighlightSession();
    const first = session.update(
      "/* comment\ncontinued */\nconst a = 1\n",
      "ts",
    );
    const code = "/* comment\ncontinued */\nconst a = 1\nconst b = 2";
    const next = session.update(code, "ts");
    expect(next?.[0]).toBe(first?.[0]);
    const characters = (lines: ReturnType<typeof highlightLines>) =>
      lines?.map((line) =>
        line.flatMap((span) =>
          [...span.text]
            .filter((character) => !/\s/.test(character))
            .map((character) => ({ character, color: span.style.color })),
        ),
      );
    expect(characters(next ? [...next] : undefined)).toEqual(
      characters(highlightLines(code, "ts")),
    );
    expect(session.update(code, "ts")).toBe(next);
  });

  it("替换、清空和语言变化丢弃旧的语法状态", () => {
    const session = new StreamingHighlightSession();
    session.update("/* open comment\nmore", "ts");
    const replacement = session.update("const replacement = true", "ts");
    expect(replacement).toEqual(
      new StreamingHighlightSession().update("const replacement = true", "ts"),
    );
    expect(session.update("", "ts")).toEqual(
      new StreamingHighlightSession().update("", "ts"),
    );
    expect(session.update('{"done":true}', "json")).toEqual(
      new StreamingHighlightSession().update('{"done":true}', "json"),
    );
  });

  it("原型属性和未知语言安全回退为原文", () => {
    for (const hint of [
      "constructor",
      "__proto__",
      "not-a-grammar",
      undefined,
    ]) {
      expect(grammarForHint(hint)).toBeUndefined();
      expect(
        highlightToHtml("<script>unsafe()</script>", hint),
      ).toBeUndefined();
      expect(
        new StreamingHighlightSession().update("source", hint),
      ).toBeUndefined();
    }
    expect(grammarForHint("HTM")).toBe("html");
  });

  it("超长单行保持完整原文，同时为相邻普通行高亮", () => {
    const large = "x".repeat(100_000);
    const code = `const before = 1\n${large}\nconst after = 2`;
    const spans = new StreamingHighlightSession().update(code, "ts");
    expect(spans?.[1]?.map((span) => span.text).join("")).toBe(large);
    expect(spans?.[1]).toHaveLength(1);
    expect(spans?.[0]?.length).toBeGreaterThan(1);
    expect(spans?.[2]?.length).toBeGreaterThan(1);
  });
});
