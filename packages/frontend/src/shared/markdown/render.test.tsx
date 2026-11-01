import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MarkdownText } from "./MarkdownText";

describe("Markdown 语法与安全边界", () => {
  it("保留中文标点强调、嵌套列表、表格列对齐和双波浪删除", () => {
    const { container } = render(
      <MarkdownText
        text={
          "**重点。**后续\n\n- 第一项\n  - 嵌套项\n\n| 左 | 右 |\n| :--- | ---: |\n| 一 | 二 |\n\n10~20 与 ~~删除~~"
        }
      />,
    );
    expect(container.querySelector("strong")?.textContent).toBe("重点。");
    expect(container.querySelector("ul ul li")?.textContent).toBe("嵌套项");
    expect(container.querySelector("th:last-child")).toHaveClass("text-right");
    expect(container.querySelector("del")?.textContent).toBe("删除");
    expect(container.textContent).toContain("10~20");
  });

  it("完成时恢复跨冻结边界的引用、脚注、美元和 TeX 分隔公式", () => {
    const text =
      "[报告][target] 与注释[^n]。\n\n第一段。\n\n第二段。\n\n$E=mc^2$ 和 \\(x+1\\)。\n\n[target]: https://example.com/report\n\n[^n]: 完整脚注";
    const view = render(<MarkdownText text={text.slice(0, 30)} streaming />);
    view.rerender(<MarkdownText text={text} streaming />);
    expect(view.container.querySelector(".katex")).toBeNull();
    view.rerender(<MarkdownText text={text} />);
    expect(view.container.querySelector("a")?.getAttribute("href")).toBe(
      "https://example.com/report",
    );
    expect(
      view.container.querySelector("[data-footnotes]")?.textContent,
    ).toContain("完整脚注");
    expect(view.container.querySelectorAll(".katex")).toHaveLength(2);
  });

  it("默认不解析原始 HTML、不加载远程图像，拒绝可执行链接", () => {
    const { container } = render(
      <MarkdownText
        text={
          '<script>alert(1)</script>\n\n<img src="https://example.com/tracker">\n\n![远程图](https://example.com/image.png)\n\n[不安全](javascript:alert%281%29)'
        }
      />,
    );
    expect(container.querySelector("script, img, a")).toBeNull();
    expect(container.textContent).toContain("远程图");
    expect(container.textContent).toContain("不安全");
  });

  it("图像仅由宿主授权适配器接管，保留原始本地路径与说明", () => {
    const { container } = render(
      <MarkdownText
        text="![本地图](/workspace/my image.png)"
        components={{
          image: ({ src, alt }) => <span data-image-source={src}>{alt}</span>,
        }}
      />,
    );
    expect(container.querySelector("[data-image-source]")).toHaveAttribute(
      "data-image-source",
      "/workspace/my image.png",
    );
    expect(container.textContent).toBe("本地图");
    expect(container.querySelector("img")).toBeNull();
  });

  it("为内联和引用式链接保留作者标题，默认链接使用本地语义样式", () => {
    const { container } = render(
      <MarkdownText
        text={
          '[内联](https://example.com/a "作者标题") 与 [引用][doc]\n\n[doc]: https://example.com/b "引用标题"'
        }
      />,
    );
    const links = container.querySelectorAll("a");
    expect(links[0]).toHaveAttribute("title", "作者标题");
    expect(links[1]).toHaveAttribute("title", "引用标题");
    expect(links[0]).toHaveClass("text-link", "underline");
  });

  it("将内联和引用式链接与图片标题透传给宿主适配器", () => {
    const { container } = render(
      <MarkdownText
        text={
          '[文档](https://example.com/doc "文档说明")\n\n[引用文档][doc]\n\n![本地图](/workspace/chart.png "图片说明")\n\n![引用图][pic]\n\n[doc]: https://example.com/ref "引用文档说明"\n\n[pic]: /workspace/reference.png "引用图片说明"'
        }
        components={{
          link: ({ href, title, children }) => (
            <a href={href} title={title}>
              {children}
            </a>
          ),
          image: ({ src, alt, title }) => (
            <span data-image-source={src} title={title}>
              {alt}
            </span>
          ),
        }}
      />,
    );
    expect(
      container.querySelector('a[href="https://example.com/doc"]'),
    ).toHaveAttribute("title", "文档说明");
    expect(
      container.querySelector('a[href="https://example.com/ref"]'),
    ).toHaveAttribute("title", "引用文档说明");
    expect(
      container.querySelector('[data-image-source="/workspace/chart.png"]'),
    ).toHaveAttribute("title", "图片说明");
    expect(
      container.querySelector('[data-image-source="/workspace/reference.png"]'),
    ).toHaveAttribute("title", "引用图片说明");
    expect(container.querySelector("img")).toBeNull();
  });
});
