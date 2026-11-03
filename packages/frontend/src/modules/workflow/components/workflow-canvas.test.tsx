import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/shared/ui/select";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { StrictMode, useState } from "react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import "@/shared/i18n";
import { WorkflowCanvas } from "./workflow-canvas";
import type { Manifest } from "../model/types";
import { installCanvasDOM } from "../test/canvas-dom";
import { WorkflowInputNode } from "./workflow-input-node";
import * as canvasRouting from "../model/canvas-routing";

const manifest: Manifest = {
  sdk: "ally.workflow",
  title: "测试工作流",
  description: "",
  edges: [],
  scripts: [],
  phases: [{ id: "work", title: "校验", detail: "根据结果分别推进" }],
  nodes: [
    {
      execution: { kind: "text" },
      inputs: [],
      id: "left",
      phaseId: "work",
      kind: "askUser",
      label: "人工复核",
    },
    {
      execution: {
        codeDigest: "fixture",
        executionMode: "pure",
        language: "javascript",
      },
      inputs: [],
      id: "right",
      phaseId: "work",
      kind: "functionTask",
      label: "计算结果",
    },
  ],

  controls: [
    {
      id: "tasks",
      kind: "parallel",

      branches: [
        {
          label: "1",
          children: [
            { branches: [], id: "call:left", nodeId: "left", kind: "call" },
          ],
        },
        {
          label: "2",
          children: [
            {
              branches: [],
              id: "call:right",
              nodeId: "right",

              kind: "call",
            },
          ],
        },
      ],
    },
  ],
};

it("容器缩窄后阶段换行，跨行依赖与输入连线跟随新位置", async () => {
  let width = 1200;
  const restore = installCanvasDOM(() => width);
  const source: Manifest = {
    ...manifest,
    controls: [],
    phases: [
      { id: "first", title: "采集", detail: "" },
      { id: "second", title: "汇总", detail: "" },
    ],
    nodes: manifest.nodes.map((node, index) => ({
      ...node,
      phaseId: index === 0 ? "first" : "second",
    })),
    edges: [{ id: "next", kind: "control", from: "left", to: "right" }],
  };
  const view = render(
    <WorkflowCanvas
      manifest={source}
      onSelect={vi.fn()}
      inputNode={<button>输入参数</button>}
    />,
  );
  try {
    const first = view.container.querySelector<HTMLElement>(
      '[data-workflow-phase="first"]',
    );
    const second = view.container.querySelector<HTMLElement>(
      '[data-workflow-phase="second"]',
    );
    if (!first || !second) throw new Error("阶段未挂载");
    await waitFor(() => expect(second.style.top).toBe(first.style.top));
    width = 640;
    fireEvent(window, new Event("resize"));
    await waitFor(() => {
      expect(Number.parseFloat(second.style.top)).toBeGreaterThan(
        Number.parseFloat(first.style.top) + first.offsetHeight,
      );
      expect(first.style.left).toBe("24px");
      expect(second.style.left).toBe("24px");
    });
    const paths = [
      ...view.container.querySelectorAll<SVGPathElement>(
        "svg > g > .workflow-connection-base",
      ),
    ];
    expect(paths).toHaveLength(2);
    for (const path of paths) {
      expect(path.getAttribute("d")).toContain("V");
      expect(path.getAttribute("d")).not.toContain("H");
      expect(path.getAttribute("d")).not.toMatch(/NaN|undefined/);
    }
    const graph = second.parentElement;
    if (!graph) throw new Error("画布内容未挂载");
    expect(Number.parseFloat(graph.style.height)).toBeGreaterThan(
      Number.parseFloat(second.style.top) + second.offsetHeight,
    );
  } finally {
    view.unmount();
    restore();
    vi.unstubAllGlobals();
  }
});

it("平移缩放与选择复用连线路径，容器布局变化才重新计算", async () => {
  let width = 400;
  const restore = installCanvasDOM(() => width);
  vi.stubGlobal("PointerEvent", MouseEvent);
  const routing = vi.spyOn(canvasRouting, "workflowConnectionPath");
  const source: Manifest = {
    ...manifest,
    controls: [],
    phases: [
      { id: "first", title: "采集", detail: "" },
      { id: "second", title: "汇总", detail: "" },
    ],
    nodes: manifest.nodes.map((node, index) => ({
      ...node,
      phaseId: index === 0 ? "first" : "second",
    })),
    edges: [{ id: "next", kind: "control", from: "left", to: "right" }],
  };
  const props = { manifest: source, onSelect: vi.fn() };
  const view = render(<WorkflowCanvas {...props} />);
  try {
    await waitFor(() =>
      expect(routing.mock.calls.at(-1)?.[0].height).toBe(600),
    );
    const canvas = screen.getByLabelText("画布");
    Object.defineProperty(canvas, "setPointerCapture", { value: () => {} });
    const calls = routing.mock.calls.length;
    fireEvent.pointerDown(canvas, { button: 0, clientX: 100, clientY: 100 });
    fireEvent.pointerMove(canvas, { clientX: 160, clientY: 140 });
    fireEvent.pointerUp(canvas);
    fireEvent.wheel(canvas, { deltaY: 20 });
    await userEvent.click(screen.getByRole("button", { name: "放大" }));
    view.rerender(
      <WorkflowCanvas
        {...props}
        selectedTargets={[{ kind: "phase", id: "first" }]}
      />,
    );
    expect(routing).toHaveBeenCalledTimes(calls);
    width = 1200;
    fireEvent(window, new Event("resize"));
    await waitFor(() =>
      expect(routing.mock.calls.length).toBeGreaterThan(calls),
    );
  } finally {
    view.unmount();
    routing.mockRestore();
    restore();
    vi.unstubAllGlobals();
  }
});

it("跨多行依赖和回边绕开中间阶段，回边从上方就近返回", async () => {
  const restore = installCanvasDOM(() => 400);
  const source: Manifest = {
    ...manifest,
    controls: [],
    phases: [
      { id: "first", title: "第一阶段", detail: "" },
      { id: "middle", title: "第二阶段", detail: "" },
      { id: "last", title: "第三阶段", detail: "" },
    ],
    nodes: manifest.nodes.map((node, index) => ({
      ...node,
      phaseId: index === 0 ? "first" : "last",
    })),
    edges: [
      { id: "skip", kind: "control", from: "left", to: "right" },
      { id: "retry", kind: "control", from: "right", to: "left" },
    ],
  };
  const view = render(<WorkflowCanvas manifest={source} onSelect={vi.fn()} />);
  try {
    await waitFor(() => {
      const paths = [
        ...view.container.querySelectorAll<SVGPathElement>(
          "svg > g > .workflow-connection-base",
        ),
      ];
      expect(paths).toHaveLength(2);
      expect(paths[0]?.getAttribute("d")).toMatch(/^M164 628 /);
      expect(paths[0]?.getAttribute("d")).toMatch(/V1412$/);
      expect(paths[1]?.getAttribute("d")).toMatch(/^M164 1412 /);
      expect(paths[1]?.getAttribute("d")).toMatch(/V628$/);
    });
  } finally {
    view.unmount();
    restore();
    vi.unstubAllGlobals();
  }
});

it("默认拖动空白即可移动画布，节点与缩放按钮保留点击行为", async () => {
  const restore = installCanvasDOM();
  vi.stubGlobal("PointerEvent", MouseEvent);
  const select = vi.fn();
  const view = render(<WorkflowCanvas manifest={manifest} onSelect={select} />);
  try {
    const canvas = screen.getByLabelText("画布");
    expect(screen.getByText("拖动空白处平移")).toBeVisible();
    expect(screen.queryByText(/Shift＋拖动框选/)).not.toBeInTheDocument();
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
      "max-w-[400px]",
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
  expect(left).not.toHaveAttribute("data-workflow-completed-flow");
  expect(left).not.toHaveClass("bg-status-success-surface");
  expect(right).not.toHaveClass("bg-status-success-surface");
});

it("完成、失败与待处理以状态标签表达，节点保持中性背景", () => {
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
  expect(waiting).not.toHaveClass("bg-status-warning-surface");
  expect(waiting).not.toHaveAttribute("data-workflow-completed-flow");
  expect(screen.getByText(/等待处理/)).toBeInTheDocument();
  expect(screen.getByText(/失败/)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "选择 计算结果" })).not.toHaveClass(
    "bg-status-danger-surface",
  );
});

it("shows only real cards and their sequence when a condition has no visible else branch", () => {
  const conditional: Manifest = {
    ...manifest,
    controls: [
      {
        id: "condition",
        kind: "decision",
        conditionExpression: "prepared.chunks.length > 1",
        branches: [
          {
            label: "true",
            children: [
              {
                id: "pipeline",
                kind: "pipeline",
                conditionExpression: "prepared.chunks",
                branches: [
                  {
                    label: "1",
                    children: [
                      {
                        branches: [],
                        id: "call:left",
                        kind: "call",
                        nodeId: "left",
                      },
                    ],
                  },
                ],
              },
            ],
          },
          {
            label: "false",
            children: [{ branches: [], id: "return", kind: "sequence" }],
          },
        ],
      },
      { branches: [], id: "call:right", kind: "call", nodeId: "right" },
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

it("下拉候选在外部Portal内点击时不会被画布平移抢走指针", async () => {
  const restore = installCanvasDOM();
  vi.stubGlobal("PointerEvent", MouseEvent);
  const capture = vi.fn();
  const pointerMethods = {
    has: HTMLElement.prototype.hasPointerCapture,
    set: HTMLElement.prototype.setPointerCapture,
    release: HTMLElement.prototype.releasePointerCapture,
  };
  HTMLElement.prototype.hasPointerCapture = () => false;
  HTMLElement.prototype.setPointerCapture = () => {};
  HTMLElement.prototype.releasePointerCapture = () => {};
  function Candidate() {
    const [value, setValue] = useState("");
    return (
      <Select value={value} onValueChange={setValue}>
        <SelectTrigger aria-label="选择候选">
          <SelectValue placeholder="待选择" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="alpha">方案甲</SelectItem>
          <SelectItem value="beta">方案乙</SelectItem>
        </SelectContent>
      </Select>
    );
  }
  const view = render(
    <WorkflowCanvas
      manifest={manifest}
      selected="left"
      onSelect={() => {}}
      nodeContent={() => <Candidate />}
    />,
  );
  try {
    const canvas = screen.getByLabelText("画布");
    Object.defineProperty(canvas, "setPointerCapture", {
      configurable: true,
      value: capture,
    });
    await userEvent.click(screen.getByRole("combobox", { name: "选择候选" }));
    const option = await screen.findByRole("option", { name: "方案甲" });
    fireEvent.pointerDown(option, { button: 0, pointerId: 1 });
    expect(capture).not.toHaveBeenCalled();
    await userEvent.click(option);
    expect(
      screen.getByRole("combobox", { name: "选择候选" }),
    ).toHaveTextContent("方案甲");
  } finally {
    view.unmount();
    restore();
    vi.unstubAllGlobals();
    HTMLElement.prototype.hasPointerCapture = pointerMethods.has;
    HTMLElement.prototype.setPointerCapture = pointerMethods.set;
    HTMLElement.prototype.releasePointerCapture = pointerMethods.release;
  }
});

it.each(["canvas", "window"])(
  "Space在%s失焦后左键平移，Shift左键框选，操作提示可直接看到",
  async (surface) => {
    const restore = installCanvasDOM();
    vi.stubGlobal("PointerEvent", MouseEvent);
    const selected = vi.fn();
    const view = render(
      <WorkflowCanvas
        manifest={{
          ...manifest,
          phases: [
            ...manifest.phases,
            { id: "result", title: "结果", detail: "" },
          ],
        }}
        onSelect={() => {}}
        onSelectTargets={selected}
      />,
    );
    try {
      const canvas = screen.getByLabelText("画布");
      Object.defineProperty(canvas, "getBoundingClientRect", {
        value: () => new DOMRect(0, 0, 1000, 700),
      });
      Object.defineProperty(canvas, "setPointerCapture", { value: () => {} });
      for (const element of document.querySelectorAll<HTMLElement>(
        "[data-workflow-phase]",
      )) {
        Object.defineProperty(element, "getBoundingClientRect", {
          value: () =>
            new DOMRect(
              element.dataset.workflowPhase === "work" ? 100 : 450,
              100,
              250,
              400,
            ),
        });
      }
      expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
      fireEvent.keyDown(canvas, { key: " ", code: "Space" });
      if (surface === "canvas") fireEvent.blur(canvas);
      else window.dispatchEvent(new Event("blur"));
      const graph = canvas.querySelector<HTMLElement>(
        'div[style*="transform"]',
      );
      const before = graph?.style.transform;
      fireEvent.pointerDown(canvas, { button: 0, clientX: 50, clientY: 50 });
      fireEvent.pointerMove(canvas, { clientX: 100, clientY: 80 });
      fireEvent.pointerUp(canvas);
      expect(selected).not.toHaveBeenCalled();
      expect(graph?.style.transform).not.toBe(before);
      const beforeWheel = graph?.style.transform.match(
        /translate\(([-\d.]+)px,([-\d.]+)px\)/,
      );
      if (!beforeWheel) throw new Error("画布平移位置缺失");
      fireEvent.wheel(canvas, { deltaX: 24, deltaY: 32 });
      expect(graph?.style.transform).toContain(
        `translate(${Number(beforeWheel[1]) - 24}px,${Number(beforeWheel[2]) - 32}px)`,
      );
      expect(selected).not.toHaveBeenCalled();
      const moved = graph?.style.transform;
      fireEvent.pointerDown(canvas, {
        button: 0,
        shiftKey: true,
        clientX: 50,
        clientY: 50,
      });
      fireEvent.pointerMove(canvas, { clientX: 800, clientY: 600 });
      expect(canvas).toHaveClass("cursor-crosshair");
      fireEvent.pointerUp(canvas);
      expect(graph?.style.transform).toBe(moved);
      expect(selected).toHaveBeenLastCalledWith([
        { kind: "phase", id: "work" },
        { kind: "phase", id: "result" },
      ]);
      const hint = screen.getByText("拖动空白处平移 · Shift＋拖动框选");
      expect(canvas).toHaveAttribute("aria-describedby", hint.id);
      expect(hint).toBeVisible();
    } finally {
      view.unmount();
      restore();
      vi.unstubAllGlobals();
    }
  },
);

it("框选中按Escape取消后松开鼠标不会重新选中阶段", () => {
  const restore = installCanvasDOM();
  vi.stubGlobal("PointerEvent", MouseEvent);
  const selected = vi.fn();
  const view = render(
    <WorkflowCanvas
      manifest={manifest}
      onSelect={() => {}}
      onSelectTargets={selected}
    />,
  );
  try {
    const canvas = screen.getByLabelText("画布");
    Object.defineProperty(canvas, "getBoundingClientRect", {
      value: () => new DOMRect(0, 0, 1000, 700),
    });
    Object.defineProperty(canvas, "setPointerCapture", { value: () => {} });
    const phase = document.querySelector<HTMLElement>("[data-workflow-phase]");
    if (!phase) throw new Error("missing phase");
    Object.defineProperty(phase, "getBoundingClientRect", {
      value: () => new DOMRect(100, 100, 250, 400),
    });
    fireEvent.pointerDown(canvas, {
      button: 0,
      shiftKey: true,
      clientX: 50,
      clientY: 50,
    });
    fireEvent.pointerMove(canvas, { clientX: 800, clientY: 600 });
    fireEvent.keyDown(canvas, { key: "Escape" });
    expect(selected).toHaveBeenCalledExactlyOnceWith([]);
    fireEvent.pointerUp(canvas);
    expect(selected).toHaveBeenCalledExactlyOnceWith([]);
  } finally {
    view.unmount();
    restore();
    vi.unstubAllGlobals();
  }
});
