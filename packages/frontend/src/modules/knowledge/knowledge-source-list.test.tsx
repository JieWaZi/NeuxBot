import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { KnowledgeSourceList } from "./knowledge-source-list";
import { toSource } from "./knowledge-queries";
it("分页失败保留禁用的加载按钮，不在目录展示重试", () => {
  render(
    <KnowledgeSourceList
      rows={[]}
      onOpen={vi.fn()}
      hasMore
      failed
      onLoadMore={vi.fn()}
    />,
  );
  expect(
    screen.queryByRole("button", { name: "重试" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "加载更多原件" })).toBeDisabled();
});

it("原件目录仅展示图标与可截断文件名，完整名称可悬停查看", async () => {
  const name = "很长的源文件名称用于确认截断和完整可访问名称.md";
  const source = toSource({
    id: "one",
    filename: name,
    state: "failed",
    diagnostic: "internal failure",
  });
  const open = vi.fn();
  render(
    <KnowledgeSourceList
      rows={[source]}
      selectedId="one"
      onOpen={open}
      onLoadMore={vi.fn()}
    />,
  );
  const row = screen.getByRole("treeitem", { name });
  expect(row).toHaveAttribute("title", name);
  expect(row).toHaveAttribute("aria-selected", "true");
  expect(screen.getByText(name)).toHaveClass("truncate");
  expect(row.querySelector("svg")).not.toBeNull();
  expect(screen.queryByRole("table")).not.toBeInTheDocument();
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(screen.queryByText("internal failure")).not.toBeInTheDocument();
  await userEvent.click(row);
  expect(open).toHaveBeenCalledWith(source);
});
