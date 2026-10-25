import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { SearchableMultiSelect } from "@/shared/ui/searchable-multi-select";

describe("SearchableMultiSelect", () => {
  it("展开层跟随完整字段宽度，不被固定宽度截断", () => {
    render(
      <SearchableMultiSelect
        options={[{ value: "model", label: "model" }]}
        value={[]}
        onValueChange={() => undefined}
        triggerLabel="选择模型"
        searchLabel="搜索模型"
        emptyLabel="无匹配"
        clearSearchLabel="清空搜索"
      />,
    );

    const trigger = screen.getByRole("button", { name: "选择模型" });
    expect(trigger).toHaveClass("bg-field", "rounded-md");
    fireEvent.click(trigger);
    expect(screen.getByRole("dialog")).toHaveClass(
      "w-(--radix-popover-trigger-width)",
      "max-w-[calc(100vw-32px)]",
    );
  });

  it("滚轮在列表上改变 scrollTop", () => {
    render(
      <SearchableMultiSelect
        options={Array.from({ length: 30 }, (_, index) => ({
          value: `model-${index}`,
          label: `model-${index}`,
        }))}
        value={[]}
        onValueChange={() => undefined}
        triggerLabel="选择模型"
        searchLabel="搜索模型"
        emptyLabel="无匹配"
        clearSearchLabel="清空搜索"
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "选择模型" }));
    const list = screen.getByRole("group", { name: "选择模型" });
    Object.defineProperties(list, {
      scrollHeight: { value: 800, configurable: true },
      clientHeight: { value: 224, configurable: true },
      scrollTop: { value: 0, writable: true, configurable: true },
    });
    fireEvent.wheel(list, { deltaY: 48 });
    expect(list.scrollTop).toBe(48);
  });
});

it("允许在当前搜索中创建自定义选项，普通选择仍保留原行为", () => {
  let selected: string[] = [];
  render(
    <SearchableMultiSelect
      options={[]}
      value={[]}
      onValueChange={(value) => {
        selected = value;
      }}
      triggerLabel="分隔符"
      searchLabel="搜索"
      emptyLabel="没有匹配"
      clearSearchLabel="清空"
      createLabel="添加分隔符"
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "分隔符" }));
  fireEvent.change(screen.getByRole("searchbox", { name: "搜索" }), {
    target: { value: "===章节===" },
  });
  fireEvent.click(screen.getByRole("button", { name: "添加分隔符" }));
  expect(selected).toEqual(["===章节==="]);
});
