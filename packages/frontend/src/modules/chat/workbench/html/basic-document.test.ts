import { describe, expect, it } from "vitest";
import { createBasicHtmlDocument } from "./basic-document";

describe("static HTML document", () => {
  it("移除脚本、跳转和子框架，并把禁止外部资源的 CSP 放在文档头部", () => {
    const bytes = new TextEncoder().encode(
      '<html><head><title>预览</title></head><body><script>parent.alert(1)</script><a href="https://example.com">link</a><iframe src="https://example.com"></iframe><p>安全正文</p></body></html>',
    );
    const html = createBasicHtmlDocument(bytes);
    const document = new DOMParser().parseFromString(html, "text/html");
    const policy = document.head.firstElementChild;
    expect(policy?.getAttribute("http-equiv")).toBe("Content-Security-Policy");
    expect(policy?.getAttribute("content")).toContain("default-src 'none'");
    expect(document.querySelector("script, iframe")).toBeNull();
    expect(document.querySelector("a")?.hasAttribute("href")).toBe(false);
    expect(document.body.textContent).toContain("安全正文");
  });

  it("拒绝损坏的 UTF-8 文档", () => {
    expect(() =>
      createBasicHtmlDocument(new Uint8Array([0xc3, 0x28])),
    ).toThrow();
  });
});
