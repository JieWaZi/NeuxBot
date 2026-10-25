import { expect, it } from "vitest";
import { stripDuplicateWikiTitle } from "./knowledge-wiki-markdown";

it("只省略开头同名一级标题，保留表格与后续同名章节", () => {
  const body =
    "## 范围\n\n| 对象 | 上限 |\n| --- | --- |\n| 地址池 | 20 |\n\n# 网络冲突检查";
  expect(
    stripDuplicateWikiTitle(`\n\n# 网络冲突检查\n\n${body}`, "网络冲突检查"),
  ).toBe(body);
  expect(stripDuplicateWikiTitle("# DNS\n\n正文", "dns")).toBe("正文");
});

it.each([
  "# 不同标题\n\n正文",
  "## 网络冲突检查\n\n正文",
  "```markdown\n# 网络冲突检查\n```\n\n正文",
  "    # 网络冲突检查\n\n正文",
  "> # 网络冲突检查\n\n正文",
  "前言\n\n# 网络冲突检查",
])("保留不同标题、代码块及非首段标题：%s", (source) => {
  expect(stripDuplicateWikiTitle(source, "网络冲突检查")).toBe(source);
});
