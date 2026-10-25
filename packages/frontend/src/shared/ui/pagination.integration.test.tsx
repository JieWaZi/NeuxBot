import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { Pagination } from "./pagination";

const labels = {
  navigation: "结果分页",
  next: "下一页",
  page: (page: number) => `前往第 ${page} 页`,
  pageSize: (pageSize: number) => `${pageSize} 条/页`,
  pageSizeSelection: "每页条数",
  previous: "上一页",
  total: (totalItems: number) => `共 ${totalItems} 条`,
};

describe("Pagination", () => {
  it("展示总数和当前页并交付翻页意图", async () => {
    const user = userEvent.setup();
    const onPageChange = vi.fn();
    render(
      <Pagination
        labels={labels}
        page={2}
        pageSize={50}
        totalItems={120}
        onPageChange={onPageChange}
      />,
    );

    expect(screen.getByText("共 120 条")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "前往第 2 页" })).toHaveAttribute(
      "aria-current",
      "page",
    );

    await user.click(screen.getByRole("button", { name: "上一页" }));
    await user.click(screen.getByRole("button", { name: "下一页" }));
    await user.click(screen.getByRole("button", { name: "前往第 3 页" }));

    expect(onPageChange.mock.calls).toEqual([[1], [3], [3]]);
  });

  it("在边界页禁用无效方向", () => {
    const { rerender } = render(
      <Pagination
        labels={labels}
        page={1}
        pageSize={50}
        totalItems={120}
        onPageChange={vi.fn()}
      />,
    );
    expect(screen.getByRole("button", { name: "上一页" })).toBeDisabled();

    rerender(
      <Pagination
        labels={labels}
        page={3}
        pageSize={50}
        totalItems={120}
        onPageChange={vi.fn()}
      />,
    );
    expect(screen.getByRole("button", { name: "下一页" })).toBeDisabled();
  });

  it("支持切换每页条数", async () => {
    const user = userEvent.setup();
    const onPageSizeChange = vi.fn();
    render(
      <Pagination
        labels={labels}
        page={1}
        pageSize={10}
        pageSizeOptions={[10, 30, 50]}
        totalItems={90}
        onPageChange={vi.fn()}
        onPageSizeChange={onPageSizeChange}
      />,
    );

    await user.click(screen.getByRole("button", { name: "每页条数" }));
    await user.click(screen.getByRole("menuitemradio", { name: "30 条/页" }));

    expect(onPageSizeChange).toHaveBeenCalledWith(30);
  });
});

it("完整页码交付翻页意图，加载期间禁用", async () => {
  const user = userEvent.setup();
  const move = vi.fn();
  const { rerender } = render(
    <Pagination
      labels={labels}
      page={3}
      pageSize={20}
      totalItems={80}
      onPageChange={move}
    />,
  );
  expect(screen.getByText("共 80 条")).toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: "前往第 1 页" }),
  ).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "上一页" }));
  await user.click(screen.getByRole("button", { name: "下一页" }));
  expect(move.mock.calls).toEqual([[2], [4]]);
  rerender(
    <Pagination
      labels={labels}
      page={3}
      pageSize={20}
      totalItems={45}
      pending
      onPageChange={move}
    />,
  );
  expect(screen.getByRole("button", { name: "下一页" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "上一页" })).toBeDisabled();
});

it("已知总数时展示真实两页页码，数字页交付对应页意图", async () => {
  const user = userEvent.setup();
  const move = vi.fn();
  render(
    <Pagination
      labels={labels}
      page={1}
      pageSize={10}
      totalItems={20}
      onPageChange={move}
    />,
  );
  expect(
    screen.getByRole("button", { name: "前往第 2 页" }),
  ).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "前往第 2 页" }));
  expect(move).toHaveBeenCalledWith(2);
});
