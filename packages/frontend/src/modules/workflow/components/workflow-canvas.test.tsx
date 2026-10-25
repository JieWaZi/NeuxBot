import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { StrictMode, useState } from "react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import "@/shared/i18n";
import { WorkflowCanvas } from "./workflow-canvas";
import type { Manifest } from "../model/types";
import { installCanvasDOM } from "../test/canvas-dom";
import { WorkflowInputNode } from "./workflow-input-node";

const manifest: Manifest = {
  meta: { phases: [{ id: "work", title: "校验", detail: "根据结果分别推进" }] },
  nodes: [
    {
      id: "left",
      phaseId: "work",
      kind: "askUser",
      labelTemplate: "人工复核",
      groupId: "fork",
      loopDepth: 1,
    },
    {
      id: "right",
      phaseId: "work",
      kind: "functionTask",
      labelTemplate: "计算结果",
    },
  ],
  groups: [{ id: "fork", kind: "parallel" }],
  controlHints: [
    {
      id: "tasks",
      kind: "parallel",
      groupId: "fork",
      phaseId: "work",
      concurrency: 2,
      failureMode: "collect-null",
      branches: [
        {
          label: "1",
          hints: [
            { id: "call:left", nodeId: "left", phaseId: "work", kind: "node" },
          ],
        },
        {
          label: "2",
          hints: [
            {
              id: "call:right",
              nodeId: "right",
              phaseId: "work",
              kind: "node",
            },
          ],
        },
      ],
    },
  ],
};

it("默认拖动空白即可移动画布，节点与缩放按钮保留点击行为", async () => {
  const restore = installCanvasDOM();
  vi.stubGlobal("PointerEvent", MouseEvent);
  const select = vi.fn();
  const view = render(<WorkflowCanvas manifest={manifest} onSelect={select} />);
  try {
    const canvas = screen.getByLabelText("画布");
    Object.defineProperty(canvas, "setPointerCapture", {
      configurable: true,
      value: () => {},
    });
    const content = canvas.querySelector<HTMLElement>(
      "div[style*='transform']",
    );
    const before = content?.style.transform;
    fireEvent.pointerDown(canvas, { button: 0, clientX: 100, clientY: 100 });
    fireEvent.pointerMove(canvas, { clientX: 150, clientY: 140 });
    fireEvent.pointerUp(canvas);
    expect(content?.style.transform).not.toBe(before);
    expect(
      screen.queryByRole("button", { name: "平移画布" }),
    ).not.toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "选择 人工复核" }),
    );
    expect(select).toHaveBeenCalledTimes(1);
    const moved = content?.style.transform;
    await userEvent.click(screen.getByRole("button", { name: "放大" }));
    expect(content?.style.transform).not.toBe(moved);
  } finally {
    view.unmount();
    restore();
  }
});

it("输入连线沿用节点连线的流动层，隐藏画布时同步暂停", () => {
  const restore = installCanvasDOM();
  const props = {
    manifest,
    onSelect: vi.fn(),
    inputNode: <button type="button">输入参数</button>,
  };
  const view = render(<WorkflowCanvas {...props} />);
  try {
    const connector = view.container.querySelector(
      '[data-workflow-connector="input"]',
    );
    expect(
      connector?.querySelector(".workflow-connection-base"),
    ).toBeInTheDocument();
    expect(connector?.querySelector(".workflow-connection-flow")).toHaveClass(
      "stroke-accent",
    );
    expect(
      view.container.querySelector('[data-workflow-motion="true"]'),
    ).toBeInTheDocument();
    view.rerender(<WorkflowCanvas {...props} active={false} />);
    expect(
      view.container.querySelector('[data-workflow-motion="false"]'),
    ).toBeInTheDocument();
  } finally {
    view.unmount();
    restore();
    vi.unstubAllGlobals();
  }
});

it("参数浮层在严格模式下挂在画布容器内，居中且不进入节点缩放层", async () => {
  let canvasWidth = 800;
  const restore = installCanvasDOM(() => canvasWidth);
  function Canvas() {
    const [open, setOpen] = useState(false);
    return (
      <WorkflowCanvas
        manifest={manifest}
        onSelect={vi.fn()}
        inputNode={<WorkflowInputNode manifest={manifest} open={open} />}
        inputDetails={{
          title: "输入参数",
          open,
          onOpenChange: setOpen,
          content: <p>参数内容</p>,
        }}
      />
    );
  }
  const user = userEvent.setup();
  const view = render(
    <StrictMode>
      <Canvas />
    </StrictMode>,
  );
  try {
    await user.click(screen.getByRole("button", { name: "输入参数" }));
    const surface = screen.getByRole("dialog", { name: "输入参数" });
    const canvas = screen.getByLabelText("画布");
    const region = canvas.querySelector("[data-workflow-details-region]");
    expect(surface.parentElement).toBe(region);
    expect(region?.parentElement).toBe(canvas);
    expect(region).not.toHaveClass("bottom-(--shell-detail-dock-clearance)");
    expect(surface).toHaveClass(
      "absolute",
      "top-1/2",
      "left-1/2",
      "-translate-x-1/2",
      "-translate-y-1/2",
      "max-w-[720px]",
      "w-[calc(100%-48px)]",
      "max-h-[calc(100%-48px)]",
    );
    expect(surface).not.toHaveClass("fixed");
    expect(surface.closest("[data-radix-popper-content-wrapper]")).toBeNull();
    fireEvent.keyDown(canvas, { key: "ArrowRight" });
    expect(surface.parentElement).toBe(region);
    canvasWidth = 320;
    fireEvent.resize(window);
    expect(surface.parentElement).toBe(region);
    await user.click(screen.getByRole("button", { name: "关闭" }));
    await user.click(screen.getByRole("button", { name: "输入参数" }));
    expect(screen.getByRole("dialog", { name: "输入参数" }).parentElement).toBe(
      region,
    );
  } finally {
    view.unmount();
    restore();
    vi.unstubAllGlobals();
  }
});
it.each(["节点", "阶段"])(
  "%s详情与参数共用画布居中区域并避让 Dock",
  async (kind) => {
    const restore = installCanvasDOM();
    function Canvas() {
      const [selected, setSelected] = useState<string>();
      return (
        <WorkflowCanvas
          manifest={manifest}
          hasDock
          selected={selected}
          onSelect={(node) => setSelected(node.id)}
          onDismiss={() => setSelected(undefined)}
          nodeContent={() => <p>节点表单</p>}
          phaseEditor={() => <p>阶段表单</p>}
        />
      );
    }
    const user = userEvent.setup();
    const view = render(
      <StrictMode>
        <Canvas />
      </StrictMode>,
    );
    try {
      await user.click(
        screen.getByRole("button", {
          name: kind === "节点" ? "选择 计算结果" : "校验",
        }),
      );
      const surface = screen.getByRole("dialog");
      const canvas = screen.getByLabelText("画布");
      const region = canvas.querySelector("[data-workflow-details-region]");
      expect(surface.parentElement).toBe(region);
      expect(region?.parentElement).toBe(canvas);
      expect(region).toHaveClass("bottom-(--shell-detail-dock-clearance)");
      expect(surface).toHaveClass(
        "absolute",
        "pointer-events-auto",
        "top-1/2",
        "left-1/2",
      );
      expect(surface.closest("[data-radix-popper-content-wrapper]")).toBeNull();
      expect(document.body).not.toHaveAttribute("data-scroll-locked");
      fireEvent.keyDown(canvas, { key: "ArrowRight" });
      const graph = canvas.querySelector('[style*="scale"]');
      const transform = (graph as HTMLElement).style.transform;
      fireEvent.wheel(surface, { deltaY: 100 });
      expect((graph as HTMLElement).style.transform).toBe(transform);
      fireEvent.wheel(canvas, { deltaY: 100 });
      expect((graph as HTMLElement).style.transform).not.toBe(transform);
      expect(surface.parentElement).toBe(region);
      await user.click(screen.getByRole("button", { name: "关闭" }));
      await waitFor(() =>
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
      );
    } finally {
      view.unmount();
      restore();
      vi.unstubAllGlobals();
    }
  },
);

it("shows parallel branches side by side while preserving node actions", async () => {
  const onSelect = vi.fn();
  const user = userEvent.setup();
  const { container } = render(
    <WorkflowCanvas manifest={manifest} onSelect={onSelect} listMode />,
  );
  expect(screen.getByText("校验")).toBeInTheDocument();
  expect(
    screen
      .getAllByRole("button")
      .map((button) => button.getAttribute("aria-label")),
  ).toEqual(["选择 人工复核", "选择 计算结果"]);
  expect(screen.queryByText("并行分发")).not.toBeInTheDocument();
  expect(
    screen.queryByText("等待全部任务 · 按输入顺序汇总"),
  ).not.toBeInTheDocument();
  expect(
    container.querySelector('[data-workflow-branches="parallel"]'),
  ).toHaveStyle({ gridTemplateColumns: "248fr 248fr" });
  expect(screen.queryByText("并行 · 等待全部")).not.toBeInTheDocument();
  expect(
    container.querySelector('[data-workflow-connector="split"]'),
  ).toBeInTheDocument();
  expect(
    container.querySelector('[data-workflow-connector="join"]'),
  ).toBeInTheDocument();
  expect(container.querySelector("[data-flow-port]")).toBeNull();
  await user.click(screen.getByRole("button", { name: "选择 人工复核" }));
  expect(onSelect).toHaveBeenCalledWith(manifest.nodes?.[0]);
  await user.click(screen.getByRole("button", { name: "选择 计算结果" }));
  expect(onSelect).toHaveBeenLastCalledWith(manifest.nodes?.[1]);
});

it("shimmers only nodes with an actual running instance and stops when they finish", () => {
  const { rerender } = render(
    <WorkflowCanvas
      manifest={manifest}
      onSelect={vi.fn()}
      listMode
      nodeCounts={[
        { nodeId: "left", status: "running", count: 1 },
        { nodeId: "left", status: "succeeded", count: 3 },
        { nodeId: "right", status: "waiting", count: 1 },
        { nodeId: "right", status: "running", count: 0 },
      ]}
    />,
  );
  const left = screen.getByRole("button", { name: "选择 人工复核" });
  const right = screen.getByRole("button", { name: "选择 计算结果" });
  expect(left).toHaveAttribute("data-running-shimmer", "true");
  expect(right).toHaveAttribute("data-running-shimmer", "false");
  expect(screen.getByText(/等待处理/)).toBeInTheDocument();
  rerender(
    <WorkflowCanvas
      manifest={manifest}
      onSelect={vi.fn()}
      listMode
      nodeCounts={[{ nodeId: "left", status: "succeeded", count: 4 }]}
    />,
  );
  expect(left).toHaveAttribute("data-running-shimmer", "false");
  expect(left).toHaveAttribute("data-workflow-completed-flow", "true");
  expect(left).toHaveClass("bg-status-success-surface");
  expect(right).not.toHaveClass("bg-status-success-surface");
});

it("完成、失败与待处理使用消息区语义色，混合实例不会误显示全部完成", () => {
  render(
    <WorkflowCanvas
      manifest={manifest}
      onSelect={vi.fn()}
      listMode
      nodeCounts={[
        { nodeId: "left", status: "waiting", count: 1 },
        { nodeId: "left", status: "succeeded", count: 3 },
        { nodeId: "right", status: "failed", count: 1 },
      ]}
    />,
  );
  const waiting = screen.getByRole("button", { name: "选择 人工复核" });
  expect(waiting).toHaveClass("bg-status-warning-surface");
  expect(waiting).toHaveAttribute("data-workflow-completed-flow", "false");
  expect(screen.getByRole("button", { name: "选择 计算结果" })).toHaveClass(
    "bg-status-danger-surface",
  );
});

it("shows only real cards and their sequence when a condition has no visible else branch", () => {
  const conditional: Manifest = {
    ...manifest,
    controlHints: [
      {
        id: "condition",
        kind: "decision",
        condition: "prepared.chunks.length > 1",
        branches: [
          {
            label: "true",
            hints: [
              {
                id: "pipeline",
                kind: "pipeline",
                condition: "prepared.chunks",
                branches: [
                  {
                    label: "1",
                    hints: [{ id: "call:left", kind: "node", nodeId: "left" }],
                  },
                ],
              },
            ],
          },
          { label: "false", hints: [{ id: "return", kind: "return" }] },
        ],
      },
      { id: "call:right", kind: "node", nodeId: "right" },
    ],
  };
  const { container } = render(
    <WorkflowCanvas manifest={conditional} onSelect={vi.fn()} listMode />,
  );
  expect(
    screen.getByRole("button", { name: "选择 人工复核" }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: "选择 计算结果" }),
  ).toBeInTheDocument();
  for (const label of [
    "条件分支",
    "满足条件",
    "否则",
    "prepared.chunks.length > 1",
    "prepared.chunks",
    "管道 · 逐项推进",
    "此处无执行节点",
  ])
    expect(screen.queryByText(label)).not.toBeInTheDocument();
  expect(
    container.querySelector('[data-workflow-branches="parallel"]'),
  ).toBeNull();
  expect(container.querySelector('[data-workflow-branch="false"]')).toBeNull();
  expect(
    container.querySelectorAll('[data-workflow-connector="sequence"]'),
  ).toHaveLength(1);
});
