import { Profiler } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { KnowledgeGraphCanvas } from "./knowledge-graph-canvas";
import { graphEngine } from "./knowledge-force-test-double";
import type { KnowledgeGraph } from "./knowledge-graph-model";

vi.mock("react-force-graph-2d", () => import("./knowledge-force-test-double"));
const graph: KnowledgeGraph = {
  nodes: ["a", "b"].map((slug) => ({
    slug,
    title: slug,
    page_type: "concept",
    link_count: 1,
  })),
  edges: [{ source: "a", target: "b" }],
  meta: { mode: "overview", total: 2, returned: 2, truncated: false },
};
let resize = () => {};
beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(1000);
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(600);
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(callback: () => void) {
        resize = callback;
      }
      observe() {}
      disconnect() {}
    },
  );
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});
function show() {
  const select = vi.fn(),
    focus = vi.fn(),
    bloom = vi.fn();
  const view = render(
    <TooltipProvider>
      <KnowledgeGraphCanvas
        graph={graph}
        selected=""
        onSelect={select}
        onFocus={focus}
        onBloom={bloom}
      />
    </TooltipProvider>,
  );
  return { ...view, select, focus, bloom };
}
it("以 Canvas 引擎绘图，保留独立数据、键盘阅读和展开入口", () => {
  const { select, focus, bloom } = show();
  expect(screen.getByTestId("force-canvas")).toBeInTheDocument();
  expect(
    screen
      .getByRole("group", { name: "知识关系图" })
      .querySelector("svg[data-graph-root]"),
  ).toBeNull();
  expect(graphEngine.props?.graphData?.nodes[0]).not.toBe(graph.nodes[0]);
  const node = screen.getByRole("button", { name: "a" });
  fireEvent.keyDown(node, { key: "Enter" });
  expect(select).toHaveBeenCalledWith("a");
  fireEvent.keyDown(node, { key: "Enter", shiftKey: true });
  expect(bloom).toHaveBeenCalledWith("a");
  fireEvent.keyDown(node, { key: "Enter", ctrlKey: true });
  expect(focus).toHaveBeenCalledWith("a");
});
it("拖动由库处理，结束时固定坐标且不改业务节点", () => {
  show();
  const node = graphEngine.props?.graphData?.nodes[0];
  if (!node) throw new Error("缺少引擎节点");
  node.x = 222;
  node.y = 333;
  act(() => graphEngine.props?.onNodeDragEnd?.(node, { x: 10, y: 20 }));
  expect(node).toMatchObject({ fx: 222, fy: 333, pinned: true });
  expect(graph.nodes[0]).not.toHaveProperty("fx");
});
it("零尺寸等待，隐藏窗口暂停，恢复与卸载清理引擎", () => {
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(0);
  const view = show();
  expect(screen.queryByTestId("force-canvas")).toBeNull();
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(1000);
  act(() => resize());
  expect(screen.getByTestId("force-canvas")).toBeInTheDocument();
  const hidden = vi.spyOn(document, "hidden", "get").mockReturnValue(true);
  act(() => document.dispatchEvent(new Event("visibilitychange")));
  expect(graphEngine.methods.pauseAnimation).toHaveBeenCalled();
  hidden.mockReturnValue(false);
  act(() => document.dispatchEvent(new Event("visibilitychange")));
  expect(graphEngine.methods.resumeAnimation).toHaveBeenCalled();
  const before = graphEngine.methods.pauseAnimation.mock.calls.length;
  view.unmount();
  expect(graphEngine.methods.pauseAnimation).toHaveBeenCalledTimes(before + 1);
});

it.each([400, 1500])("%i 节点的布局帧不触发 React 提交或重建节点", (count) => {
  const onRender = vi.fn();
  const data: KnowledgeGraph = {
    nodes: Array.from({ length: count }, (_, i) => ({
      slug: `n${i}`,
      title: `节点 ${i}`,
      page_type: "concept",
      link_count: 2,
    })),
    edges: Array.from({ length: count }, (_, i) => ({
      source: `n${i}`,
      target: `n${(i + 1) % count}`,
    })),
    meta: { mode: "overview", total: count, returned: count, truncated: false },
  };
  render(
    <Profiler id="graph" onRender={onRender}>
      <TooltipProvider>
        <KnowledgeGraphCanvas graph={data} selected="" onSelect={() => {}} />
      </TooltipProvider>
    </Profiler>,
  );
  const before = onRender.mock.calls.length;
  const firstNode = graphEngine.props?.graphData?.nodes[0];
  for (let i = 0; i < 60; i++) act(() => graphEngine.props?.onEngineTick?.());
  expect(onRender).toHaveBeenCalledTimes(before);
  expect(graphEngine.props?.graphData?.nodes[0]).toBe(firstNode);
  expect(graphEngine.props?.onZoom).toBeUndefined();
});

it("第三方 pointerup 的 detail 为零时仍识别同一节点双击，只聚焦不阅读", () => {
  vi.useFakeTimers();
  try {
    const { select, focus } = show();
    const node = graphEngine.props?.graphData?.nodes[0];
    if (!node) throw new Error("缺少引擎节点");
    const pointerUp = new MouseEvent("pointerup", { detail: 0 });
    act(() => graphEngine.props?.onNodeClick?.(node, pointerUp));
    act(() => graphEngine.props?.onNodeClick?.(node, pointerUp));
    act(() => vi.advanceTimersByTime(250));
    expect(focus).toHaveBeenCalledExactlyOnceWith("a");
    expect(select).not.toHaveBeenCalled();
  } finally {
    vi.useRealTimers();
  }
});

it("选择节点被筛掉时，仍有可 Tab 进入的节点入口", () => {
  render(
    <TooltipProvider>
      <KnowledgeGraphCanvas
        graph={graph}
        selected="removed"
        onSelect={() => {}}
      />
    </TooltipProvider>,
  );
  expect(screen.getByRole("button", { name: "a" })).toHaveAttribute(
    "tabindex",
    "0",
  );
});

it("长时间隐藏不消耗物理步预算，窗口隐藏取消延迟阅读", () => {
  vi.useFakeTimers();
  try {
    const { select } = show();
    expect(graphEngine.props?.cooldownTime).toBe(Infinity);
    const node = graphEngine.props?.graphData?.nodes[0];
    if (!node) throw new Error("缺少引擎节点");
    act(() =>
      graphEngine.props?.onNodeClick?.(node, new MouseEvent("pointerup")),
    );
    vi.spyOn(document, "hidden", "get").mockReturnValue(true);
    act(() => document.dispatchEvent(new Event("visibilitychange")));
    act(() => vi.advanceTimersByTime(10000));
    expect(select).not.toHaveBeenCalled();
  } finally {
    vi.useRealTimers();
  }
});

it("键盘焦点离开节点后清理悬停展开按钮", () => {
  const expanded = {
    ...graph,
    nodes: graph.nodes.map((node) => ({ ...node, link_count: 3 })),
    meta: { ...graph.meta, mode: "ego" },
  };
  render(
    <TooltipProvider>
      <KnowledgeGraphCanvas
        graph={expanded}
        center="a"
        selected=""
        onSelect={() => {}}
        onBloom={() => {}}
      />
    </TooltipProvider>,
  );
  const node = screen.getByRole("button", { name: "b" });
  fireEvent.focus(node);
  expect(
    screen.getByRole("button", { name: "叠加邻居：b" }),
  ).toBeInTheDocument();
  fireEvent.blur(node, {
    relatedTarget: screen.getByRole("button", { name: "放大" }),
  });
  expect(screen.queryByRole("button", { name: "叠加邻居：b" })).toBeNull();
});
