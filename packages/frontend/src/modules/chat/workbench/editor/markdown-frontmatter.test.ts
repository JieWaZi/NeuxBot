import { describe, expect, it } from "vitest";

import { markdownPreviewSource } from "./markdown-frontmatter";

describe("Markdown frontmatter 预览", () => {
  it("只隐藏开头已闭合的元数据块，正文保持原样", () => {
    expect(
      markdownPreviewSource("---\r\nname: example\r\n---\r\n# 标题\r\n正文"),
    ).toBe("# 标题\r\n正文");
    const unclosed = "---\nname: example\n# 标题";
    expect(markdownPreviewSource(unclosed)).toBe(unclosed);
    const nonLeading = "# 标题\n---\nname: example\n---";
    expect(markdownPreviewSource(nonLeading)).toBe(nonLeading);
  });
});
