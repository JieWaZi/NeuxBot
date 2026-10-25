import { afterEach, expect, it, vi } from "vitest";
import type * as GraphLayout from "./knowledge-graph-layout";
import { buildForceGraphData } from "./knowledge-force-data";
import {
  createForceGraphPainter,
  type GraphPalette,
} from "./knowledge-force-paint";
import {
  graphClusterOutline,
  visibleGraphLabels,
} from "./knowledge-graph-layout";

vi.mock("./knowledge-graph-layout", async (load) => {
  const actual = await load<typeof GraphLayout>();
  return {
    ...actual,
    graphClusterOutline: vi.fn(actual.graphClusterOutline),
    visibleGraphLabels: vi.fn(actual.visibleGraphLabels),
  };
});
afterEach(() => {
  vi.clearAllMocks();
  vi.unstubAllGlobals();
});
const palette: GraphPalette = {
  text: "black",
  muted: "gray",
  surface: "white",
  edge: "gray",
  outline: "gray",
  primary: "blue",
  types: {},
  font: "sans-serif",
};
function setup() {
  vi.stubGlobal(
    "Path2D",
    class {
      constructor(public path: string) {}
    },
  );
  const data = buildForceGraphData(
    {
      nodes: Array.from({ length: 20 }, (_, i) => ({
        slug: `n${i}`,
        title: `中文页面 ${i}`,
        link_count: i ? 1 : 19,
        page_type: "concept",
      })),
      edges: Array.from({ length: 19 }, (_, i) => ({
        source: "n0",
        target: `n${i + 1}`,
      })),
      meta: { mode: "overview", total: 20, returned: 20, truncated: false },
    },
    1000,
    600,
  );
  const measureText = vi.fn((text: string) => ({ width: text.length * 12 }));
  const ctx = {
    save: vi.fn(),
    restore: vi.fn(),
    fill: vi.fn(),
    measureText,
  } as unknown as CanvasRenderingContext2D;
  return {
    data,
    ctx,
    measureText,
    painter: createForceGraphPainter(data, palette),
  };
}
it("平移与悬停复用簇轮廓，文本宽度不在每帧重复测量", () => {
  const { ctx, painter, measureText } = setup();
  painter.before(ctx, 1);
  const outlines = vi.mocked(graphClusterOutline).mock.calls.length;
  const measurements = measureText.mock.calls.length;
  for (let i = 0; i < 60; i++) painter.before(ctx, 1);
  expect(graphClusterOutline).toHaveBeenCalledTimes(outlines);
  painter.setFocus("", "n1");
  painter.before(ctx, 1);
  expect(graphClusterOutline).toHaveBeenCalledTimes(outlines);
  expect(measureText).toHaveBeenCalledTimes(measurements);
});
it("布局每六帧更新几何，缩放立即刷新；高亮不在每个节点内扫描关系", () => {
  const { ctx, painter } = setup();
  painter.before(ctx, 1);
  const count = vi.mocked(visibleGraphLabels).mock.calls.length;
  for (let i = 0; i < 5; i++) {
    painter.tick();
    painter.before(ctx, 1);
  }
  expect(visibleGraphLabels).toHaveBeenCalledTimes(count);
  painter.tick();
  painter.before(ctx, 1);
  expect(visibleGraphLabels).toHaveBeenCalledTimes(count + 1);
  painter.before(ctx, 2);
  expect(visibleGraphLabels).toHaveBeenCalledTimes(count + 2);
});
