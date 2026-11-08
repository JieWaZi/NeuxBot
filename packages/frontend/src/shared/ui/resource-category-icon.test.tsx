import { render } from "@testing-library/react";
import { expect, it } from "vitest";

import { ResourceCategoryIcon } from "./resource-category-icon";

it("十种常见分类具有不同的颜色与图标", () => {
  const { container } = render(
    <>
      {[
        "编程",
        "设计",
        "数据与分析",
        "写作",
        "研究",
        "网络",
        "自动化",
        "办公",
        "沟通",
        "工具",
      ].map((category) => (
        <ResourceCategoryIcon key={category} category={category} />
      ))}
    </>,
  );
  const tiles = [...container.children];
  expect(new Set(tiles.map((tile) => tile.className)).size).toBe(10);
  expect(
    new Set(
      tiles.map((tile) => tile.querySelector("svg")?.getAttribute("class")),
    ).size,
  ).toBe(10);
  for (const tile of tiles) {
    expect(tile).toHaveClass("text-resource-category-content");
    expect(tile).toHaveAttribute("aria-hidden", "true");
  }
});

it("中英文常见分类采用相同标识", () => {
  const view = render(<ResourceCategoryIcon category="设计" />);
  const initial = view.container.innerHTML;
  view.rerender(<ResourceCategoryIcon category="Design" />);
  expect(view.container.innerHTML).toBe(initial);
});

it("自设分类按规范化名称稳定映射，其他分类的渲染不影响标识", () => {
  const view = render(<ResourceCategoryIcon category=" Ｍy Team 🧭 " />);
  const initial = view.container.innerHTML;
  view.rerender(<ResourceCategoryIcon category="另一个分类" />);
  view.rerender(<ResourceCategoryIcon category="my team 🧭" />);
  expect(view.container.innerHTML).toBe(initial);
  expect(view.container.firstElementChild?.className).toMatch(
    /bg-resource-category-/,
  );
});

it("所有未分类资源使用同一组颜色与积木图标", () => {
  const view = render(
    <>
      <ResourceCategoryIcon />
      <ResourceCategoryIcon category="" />
      <ResourceCategoryIcon category=" " />
    </>,
  );
  const tiles = [...view.container.children];
  expect(new Set(tiles.map((tile) => tile.innerHTML)).size).toBe(1);
  for (const tile of tiles) {
    expect(tile).toHaveClass("bg-resource-category-tools");
    expect(tile.querySelector("svg")).toHaveClass("lucide-blocks");
  }
});

it("详情与列表使用同一分类标识，使用区别于参考图的 Lucide 图标", () => {
  const view = render(<ResourceCategoryIcon category="编程" />);
  expect(view.container.firstElementChild).toHaveClass(
    "bg-resource-category-code",
    "size-resource-icon",
  );
  view.rerender(<ResourceCategoryIcon category="编程" size="detail" />);
  expect(view.container.firstElementChild).toHaveClass(
    "bg-resource-category-code",
    "size-resource-detail-icon",
  );
  expect(view.container.querySelector("svg")).toHaveClass(
    "lucide-file-code-corner",
  );
});
