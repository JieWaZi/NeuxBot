import { describe, expect, it } from "vitest";

import { localImagePath, rewriteLocalImageUrls } from "./markdown-local-images";

describe("Markdown local images", () => {
  it("rewrites inline and referenced images but leaves remote images and ordinary links", () => {
    const result = rewriteLocalImageUrls(
      [
        "![本地](<./图 片#1%.png>)",
        "![远程](https://example.com/a.png)",
        "![引用][logo]",
        "[logo]: ../assets/logo.png",
        "[文字][guide]",
        "[guide]: ../guide.md",
      ].join("\n"),
    );
    const urls = [
      ...result.matchAll(
        /https:\/\/ally\.invalid\/__workbench_markdown_image__\?path=[^\s)]+/g,
      ),
    ].map(([url]) => localImagePath(url));
    expect(urls).toEqual(["./图 片#1%.png", "../assets/logo.png"]);
    expect(result).toContain("https://example.com/a.png");
    expect(result).toContain("[guide]: ../guide.md");
  });

  it("does not rewrite code examples", () => {
    const source = [
      "`![x](./inline.png)`",
      "```md",
      "![x](./fenced.png)",
      "```",
      "![actual](./actual.png)",
    ].join("\n");
    const result = rewriteLocalImageUrls(source);
    expect(result).toContain("`![x](./inline.png)`");
    expect(result).toContain("![x](./fenced.png)");
    expect(result).not.toContain(
      "__workbench_markdown_image__?path=.%2Ffenced",
    );
    expect(result).toContain(
      "__workbench_markdown_image__?path=.%2Factual.png",
    );
  });
});
