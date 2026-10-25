import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { expect, it, vi } from "vitest";
import { KnowledgeChunkingFields } from "./knowledge-chunking-fields";
import {
  defaultLibraryChunkingConfig,
  type LibraryChunkingConfig,
} from "./knowledge-model";
vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
);
function show(initial = defaultLibraryChunkingConfig()) {
  let current = initial;
  function Harness() {
    const [value, setValue] = useState<LibraryChunkingConfig>(initial);
    current = value;
    return <KnowledgeChunkingFields value={value} onChange={setValue} />;
  }
  render(<Harness />);
  return () => current;
}
it("按固定上游次序展示slider与父子展开，step和边界保持一致", async () => {
  const user = userEvent.setup();
  show();
  const sliders = screen.getAllByRole("slider");
  expect(sliders.map((slider) => slider.getAttribute("aria-label"))).toEqual([
    "分块大小",
    "重叠大小",
    "父块大小",
    "子块大小",
  ]);
  expect(sliders.map((slider) => slider.getAttribute("aria-valuenow"))).toEqual(
    ["512", "80", "4096", "384"],
  );
  expect(
    screen.queryByRole("spinbutton", { name: "分块大小" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("switch", { name: "父子分块" }));
  expect(
    screen.queryByRole("slider", { name: "父块大小" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("switch", { name: "父子分块" }));
  expect(screen.getByRole("slider", { name: "父块大小" })).toHaveAttribute(
    "aria-valuenow",
    "4096",
  );
  const overlap = screen.getByRole("slider", { name: "重叠大小" });
  overlap.focus();
  await user.keyboard("{ArrowRight}");
  expect(overlap).toHaveAttribute("aria-valuenow", "100");
});
it("传统策略禁用高级字段并保留数值，重叠过高显示上游警告", async () => {
  const user = userEvent.setup();
  show({
    ...defaultLibraryChunkingConfig(),
    strategy: "legacy",
    chunk_size: 100,
    chunk_overlap: 80,
    token_limit: 256,
  });
  expect(screen.getByText(/重叠大小已达到分块大小的一半/)).toBeVisible();
  await user.click(screen.getByRole("button", { name: "高级选项" }));
  expect(screen.getByRole("spinbutton", { name: "Token 上限" })).toBeDisabled();
  expect(screen.getByRole("spinbutton", { name: "Token 上限" })).toHaveValue(
    256,
  );
  expect(screen.getByRole("button", { name: "自动检测" })).toBeDisabled();
});

it("不提供清除策略操作，保留已有空策略且高级选项可展开和收起", async () => {
  const user = userEvent.setup();
  const current = show({ ...defaultLibraryChunkingConfig(), strategy: "" });
  expect(
    screen.queryByRole("button", { name: "清除分块策略" }),
  ).not.toBeInTheDocument();
  expect(current().strategy).toBe("");
  const advanced = screen.getByRole("button", { name: "高级选项" });
  expect(advanced).toHaveAttribute("aria-expanded", "false");
  expect(screen.queryByText("语言提示")).not.toBeInTheDocument();
  await user.click(advanced);
  expect(advanced).toHaveAttribute("aria-expanded", "true");
  expect(screen.getByText("语言提示")).toBeVisible();
  expect(screen.getByRole("spinbutton", { name: "Token 上限" })).toBeEnabled();
  await user.click(advanced);
  expect(advanced).toHaveAttribute("aria-expanded", "false");
  expect(screen.queryByText("语言提示")).not.toBeInTheDocument();
});
