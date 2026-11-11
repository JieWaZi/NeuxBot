import { build } from "vite";
import tailwindcss from "@tailwindcss/vite";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, beforeAll, expect, it } from "vitest";
import { AssistantMarkdown, UserMarkdown } from "./chat-message-content";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { FileReferenceProvider } from "@/shared/workspace/file-reference-link";
import { emptyFileReferenceContext } from "@/shared/workspace/file-reference";

const styles = document.createElement("style");
const originalRootFontSize = document.documentElement.style.fontSize;

function isLayer(rule: CSSRule): rule is CSSGroupingRule {
  return rule.constructor.name === "CSSLayerBlockRule" && "cssRules" in rule;
}

function unlayer(rules: CSSRuleList): string {
  return [...rules]
    .flatMap((rule) => {
      if (rule.type === CSSRule.STYLE_RULE) return [rule.cssText];
      return isLayer(rule) ? [unlayer(rule.cssRules)] : [];
    })
    .join("\n");
}

beforeAll(async () => {
  // 与产品默认浏览器字号一致；jsdom 不会为未显式声明的 medium 返回根像素值。
  document.documentElement.style.fontSize = "16px";
  const root = process.cwd();
  const output = await build({
    configFile: false,
    root,
    publicDir: false,
    logLevel: "silent",
    plugins: [tailwindcss()],
    build: {
      write: false,
      cssMinify: false,
      cssTarget: "chrome105",
      rollupOptions: { input: `${root}/src/shared/styles/index.css` },
    },
  });
  for (const bundle of Array.isArray(output) ? output : [output]) {
    if (!("output" in bundle)) continue;
    for (const asset of bundle.output) {
      if (asset.type !== "asset" || !asset.fileName.endsWith(".css")) continue;
      styles.textContent =
        typeof asset.source === "string"
          ? asset.source
          : new TextDecoder().decode(asset.source);
    }
  }
  document.head.append(styles);
  if (!styles.sheet)
    throw new Error("Production typography CSS did not compile");
  // jsdom does not apply cascade layers. Preserve the generated rule order while
  // removing only that wrapper; this checks production selectors, not copied CSS.
  styles.textContent = unlayer(styles.sheet.cssRules);
}, 20_000);

afterAll(() => {
  styles.remove();
  document.documentElement.style.fontSize = originalRootFontSize;
});

it("生产 CSS 在用户正文和表格内保留文件类型色，外链仍继承气泡文字色", () => {
  const client = new QueryClient();
  const { unmount } = render(
    <QueryClientProvider client={client}>
      <FileReferenceProvider context={emptyFileReferenceContext}>
        <div className="bg-user-message-background">
          <UserMarkdown
            text={
              "[报告](/repo/report.md) [网页](https://example.com)\n\n| 文件 |\n| --- |\n| [表格文件](/repo/table.md) |"
            }
          />
        </div>
        <UserMarkdown text="[成员报告](/repo/member.md)" />
      </FileReferenceProvider>
    </QueryClientProvider>,
  );
  const sheet = styles.sheet;
  if (!sheet) throw new Error("缺少生产样式");
  const matchingValue = (element: Element, property: string) => {
    let value = "";
    for (const rule of sheet.cssRules) {
      if (!(rule instanceof CSSStyleRule)) continue;
      const declaration = rule.style.getPropertyValue(property);
      if (declaration && element.matches(rule.selectorText))
        value = declaration;
    }
    return value.trim();
  };
  for (const label of ["报告", "表格文件", "成员报告"]) {
    const link = screen.getByRole("link", { name: label });
    expect(link).toHaveAttribute("data-file-tone", "blue");
    expect(matchingValue(link, "color")).toBe("var(--file-reference-color)");
    expect(matchingValue(link, "--file-reference-color")).toBe(
      label === "报告"
        ? "var(--file-reference-user-color)"
        : "var(--file-reference-default-color)",
    );
  }
  expect(
    matchingValue(screen.getByRole("link", { name: "网页" }), "color"),
  ).toBe("inherit");
  unmount();
  client.clear();
});

it("为六级标题使用本地字号，strong继承标题字重，行内代码随标题缩放", () => {
  render(
    <AssistantMarkdown
      allowFileReferences={false}
      text={Array.from(
        { length: 6 },
        (_, index) =>
          `${"#".repeat(index + 1)} **层级${index + 1}** \`inlineValue\``,
      ).join("\n\n")}
    />,
  );
  const sizes = [24, 20, 17, 16, 15, 15];
  screen.getAllByRole("heading").forEach((heading, index) => {
    const strong = heading.querySelector("strong");
    const code = heading.querySelector("code");
    expect(strong).not.toBeNull();
    expect(code).not.toBeNull();
    const headingStyle = getComputedStyle(heading);
    expect(parseFloat(headingStyle.fontSize)).toBe(sizes[index]);
    if (strong)
      expect(getComputedStyle(strong).fontWeight).toBe(headingStyle.fontWeight);
    if (code)
      expect(parseFloat(getComputedStyle(code).fontSize)).toBeCloseTo(
        parseFloat(headingStyle.fontSize) * 0.875,
      );
  });
});

it("嵌套有序列表保留内部标记，去除首尾段落的多余留白", () => {
  const { container } = render(
    <AssistantMarkdown
      text={
        "- 外层项目\n\n  第一段说明\n\n  1. 内层步骤\n\n     内层说明\n\n  2. 后续步骤\n\n- 第二项目"
      }
    />,
  );
  const nested = container.querySelector("ul ol");
  expect(nested).not.toBeNull();
  if (!nested) return;
  expect(getComputedStyle(nested).listStylePosition).toBe("inside");
  expect(getComputedStyle(nested).paddingLeft).toBe("0px");
  expect(getComputedStyle(nested).marginBottom).toBe("0px");
  const firstParagraph = nested.querySelector("li > p");
  expect(firstParagraph).not.toBeNull();
  if (firstParagraph) {
    expect(getComputedStyle(firstParagraph).display).toBe("inline");
    expect(getComputedStyle(firstParagraph).marginTop).toBe("0px");
  }
});

it("少于四列表铺满，四列表保留自然宽度，引用内的四列表仍铺满", () => {
  const source = [
    "| 短列一 | 短列二 | 短列三 |\n| --- | --- | --- |\n| 甲 | 乙 | 丙 |",
    "| 宽列一 | 宽列二 | 宽列三 | 宽列四 |\n| --- | --- | --- | --- |\n| alpha | beta | gamma | delta |",
    "> | 引用一 | 引用二 | 引用三 | 引用四 |\n> | --- | --- | --- | --- |\n> | 一 | 二 | 三 | 四 |",
  ].join("\n\n");
  render(<AssistantMarkdown text={source} />);
  const [small, wide, quoted] = screen.getAllByRole("table");
  expect(small && getComputedStyle(small).width).toBe("100%");
  expect(wide && getComputedStyle(wide).width).toBe("max-content");
  expect(quoted && getComputedStyle(quoted).width).toBe("100%");
  for (const table of [small, wide, quoted]) {
    if (!table) continue;
    expect(getComputedStyle(table).tableLayout).toBe("auto");
    expect(getComputedStyle(table).fontSize).toBe("13px");
    const first = table.querySelector("th:first-child");
    const last = table.querySelector("th:last-child");
    for (const cell of [first, last, ...table.querySelectorAll("td")]) {
      expect(cell).not.toBeNull();
      if (!cell) continue;
      const style = getComputedStyle(cell);
      expect(style.paddingInline).toBe("calc(var(--spacing) * 3)");
      expect(style.paddingLeft).not.toBe("0px");
      expect(style.paddingRight).not.toBe("0px");
    }
    if (first) {
      // jsdom 不把 logical padding 映射到 physical used value；核对实际生成规则和 Token。
      const style = getComputedStyle(first);
      expect(style.paddingBlock).toBe("calc(var(--spacing) * 1.5)");
      expect(style.getPropertyValue("--spacing")).toBe(".25rem");
      expect(
        parseFloat(getComputedStyle(document.documentElement).fontSize) *
          0.25 *
          1.5,
      ).toBe(6);
    }
    const scroller = table.parentElement;
    expect(scroller).not.toBeNull();
    if (scroller) {
      expect(getComputedStyle(scroller).overflowX).toBe("auto");
      expect(scroller).toHaveAttribute("tabindex", "0");
    }
    const frame = table.closest("[data-streamdown=table-wrapper]");
    expect(frame).not.toBeNull();
    if (frame) {
      expect(parseFloat(getComputedStyle(frame).paddingLeft)).toBe(0);
      expect(parseFloat(getComputedStyle(frame).paddingRight)).toBe(0);
      expect(getComputedStyle(frame).marginBlock).toBe(
        "calc(var(--spacing) * 2)",
      );
    }
  }
});

it("表格操作悬浮在右上角，不为工具栏占据独立布局行", () => {
  render(
    <AssistantMarkdown
      text={"| 项目 | 内容 |\n| --- | --- |\n| 版本 | 26.1 |"}
    />,
  );
  const table = screen.getByRole("table");
  const frame = table.closest("section");
  const actions = frame?.querySelector("header");
  expect(frame && getComputedStyle(frame).position).toBe("relative");
  expect(actions).not.toBeNull();
  if (!actions) return;
  const style = getComputedStyle(actions);
  expect(style.position).toBe("absolute");
  expect(style.top).toBe("var(--spacing)");
  expect(style.right).toBe("var(--spacing)");
  expect(style.opacity).toBe("0");
  expect(style.pointerEvents).toBe("none");
});

it("宽表按键盘顺序到达横向滚动区，列对齐与完整预览不受影响", async () => {
  const user = userEvent.setup();
  render(
    <AssistantMarkdown
      allowFileReferences={false}
      text={
        "| 左 | 中 | 右 | 说明 |\n| :--- | :---: | ---: | --- |\n| alpha | beta | 123 | 完整内容不会由固定等分列挤碎 |"
      }
    />,
  );
  const header = screen.getAllByRole("columnheader");
  expect(header[1] && getComputedStyle(header[1]).textAlign).toBe("center");
  expect(header[2] && getComputedStyle(header[2]).textAlign).toBe("right");
  await user.tab();
  await user.tab();
  await user.tab();
  expect(screen.getByRole("button", { name: "全屏查看" })).toHaveFocus();
  await user.tab();
  expect(screen.getByRole("region", { name: "表格" })).toHaveFocus();
  await user.tab({ shift: true });
  await user.keyboard("{Enter}");
  const dialog = screen.getByRole("dialog", { name: "表格" });
  expect(within(dialog).getAllByRole("columnheader")).toHaveLength(4);
  expect(
    within(dialog).getByText("完整内容不会由固定等分列挤碎"),
  ).toBeVisible();
});
