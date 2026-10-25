import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { WorkspaceDetailSplit } from "./workspace-detail-split";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it("文件预览与目录共享容器宽度，拖动和键盘可调整分隔线", () => {
  let containerWidth = 600;
  vi.stubGlobal("ResizeObserver", undefined);
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(
    () => new DOMRect(0, 0, containerWidth, 400),
  );
  const { container } = render(
    <WorkspaceDetailSplit
      main={<main>预览</main>}
      sidebar={<aside>目录</aside>}
      sidebarOpen
      resizeLabel="调整分屏比例"
    />,
  );
  const split = container.querySelector<HTMLElement>(
    "[data-workspace-detail-split]",
  );
  if (!split) throw new Error("缺少分屏容器");
  expect(split).toHaveStyle({
    gridTemplateColumns: "minmax(0, 1fr) 8px 240px",
  });
  const separator = screen.getByRole("separator", { name: "调整分屏比例" });
  Object.defineProperties(separator, {
    setPointerCapture: { value: vi.fn() },
    hasPointerCapture: { value: vi.fn(() => true) },
    releasePointerCapture: { value: vi.fn() },
  });
  fireEvent.pointerDown(separator, { pointerId: 1, clientX: 360 });
  expect(split).toHaveClass("transition-none");
  fireEvent.pointerMove(separator, { pointerId: 1, clientX: 320 });
  fireEvent.pointerUp(separator, { pointerId: 1, clientX: 320 });
  expect(separator).toHaveAttribute("aria-valuenow", "280");
  expect(split).toHaveClass("transition-[grid-template-columns]");
  fireEvent.keyDown(separator, { key: "ArrowRight" });
  expect(separator).toHaveAttribute("aria-valuenow", "264");

  containerWidth = 360;
  fireEvent.resize(window);
  expect(separator).toHaveAttribute("aria-valuenow", "211");
  expect(split).toHaveStyle({
    gridTemplateColumns: "minmax(0, 1fr) 8px 211px",
  });
});

it("窄工作台按容器宽度收窄目录，给预览保留空间", () => {
  vi.stubGlobal("ResizeObserver", undefined);
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(
    new DOMRect(0, 0, 420, 400),
  );
  const { container } = render(
    <WorkspaceDetailSplit
      main={<main>预览</main>}
      sidebar={<aside>目录</aside>}
      sidebarOpen
      resizeLabel="调整分屏比例"
    />,
  );
  expect(container.querySelector("[data-workspace-detail-split]")).toHaveStyle({
    gridTemplateColumns: "minmax(0, 1fr) 8px 189px",
  });
});

it("收起时保留侧栏以完成统一布局动效，并阻止隐藏区域交互", () => {
  const props = {
    main: <main>预览</main>,
    sidebar: (
      <aside>
        <button>选择文件</button>
      </aside>
    ),
    resizeLabel: "调整分屏比例",
  };
  const { container, rerender } = render(
    <WorkspaceDetailSplit {...props} sidebarOpen />,
  );
  const sidebar = screen.getByRole("complementary");
  const split = container.querySelector("[data-workspace-detail-split]");
  expect(split).toHaveClass(
    "duration-layout",
    "ease-ui",
    "transition-[grid-template-columns]",
  );
  rerender(<WorkspaceDetailSplit {...props} sidebarOpen={false} />);
  expect(sidebar).toBeInTheDocument();
  expect(sidebar.parentElement).toHaveAttribute("inert");
  expect(sidebar.parentElement).toHaveAttribute("aria-hidden", "true");
  expect(screen.queryByRole("button", { name: "选择文件" })).toBeNull();
  expect(split).toHaveStyle({ gridTemplateColumns: "minmax(0, 1fr) 0px 0px" });
  rerender(<WorkspaceDetailSplit {...props} sidebarOpen />);
  expect(screen.getByRole("complementary")).toBe(sidebar);
  expect(sidebar.parentElement).not.toHaveAttribute("inert");
});

it("左侧会话拖动方向与键盘一致，居中输入和收起不重建子组件", () => {
  vi.stubGlobal("ResizeObserver", undefined);
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(
    new DOMRect(0, 0, 900, 600),
  );
  const props = {
    main: <main>画布</main>,
    sidebar: <input aria-label="共享输入" defaultValue="未发送" />,
    sidebarOpen: true,
    side: "left" as const,
    defaultSidebarRatio: 1 / 3,
    resizeLabel: "调整会话",
  };
  const { container, rerender } = render(
    <WorkspaceDetailSplit {...props} sidebarOnly />,
  );
  const input = screen.getByRole("textbox", { name: "共享输入" });
  expect(screen.queryByRole("main")).toBeNull();
  rerender(<WorkspaceDetailSplit {...props} />);
  expect(screen.getByRole("textbox", { name: "共享输入" })).toBe(input);
  const divider = screen.getByRole("separator", { name: "调整会话" });
  expect(divider).toHaveAttribute("aria-valuenow", "300");
  Object.defineProperties(divider, {
    setPointerCapture: { value: vi.fn() },
    hasPointerCapture: { value: vi.fn(() => true) },
    releasePointerCapture: { value: vi.fn() },
  });
  fireEvent.pointerDown(divider, { pointerId: 1, clientX: 300 });
  fireEvent.pointerMove(divider, { pointerId: 1, clientX: 330 });
  fireEvent.pointerUp(divider, { pointerId: 1, clientX: 330 });
  expect(divider).toHaveAttribute("aria-valuenow", "330");
  fireEvent.keyDown(divider, { key: "ArrowLeft" });
  expect(divider).toHaveAttribute("aria-valuenow", "314");
  rerender(<WorkspaceDetailSplit {...props} sidebarOpen={false} />);
  expect(container.querySelector("[data-workspace-detail-split]")).toHaveStyle({
    gridTemplateColumns: "0px 0px minmax(0, 1fr)",
  });
  expect(input).toBeInTheDocument();
  rerender(<WorkspaceDetailSplit {...props} />);
  expect(screen.getByRole("textbox", { name: "共享输入" })).toBe(input);
  expect(input).toHaveValue("未发送");
});
