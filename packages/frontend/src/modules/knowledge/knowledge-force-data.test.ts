import { expect, it } from "vitest";
import { buildForceGraphData } from "./knowledge-force-data";
import type { KnowledgeGraph } from "./knowledge-graph-model";

const graph: KnowledgeGraph = {
  nodes: ["a", "b"].map((slug) => ({
    slug,
    title: slug,
    page_type: "concept",
    link_count: 1,
  })),
  edges: [
    { source: "a", target: "b" },
    { source: "b", target: "a" },
  ],
  meta: { mode: "overview", total: 2, returned: 2, truncated: false },
};

it("引擎对象独立于业务图，双向边只绘制一次", () => {
  const original = structuredClone(graph);
  const data = buildForceGraphData(graph, 1000, 600);
  expect(data.links).toHaveLength(1);
  expect(data.links[0]?.bidirectional).toBe(true);
  const node = data.nodes[0];
  if (!node) throw new Error("缺少节点");
  node.x = 999;
  node.fx = 999;
  expect(graph).toEqual(original);
});

it("增量展开保留旧节点身份、位置和固定状态，新节点从邻居附近加入", () => {
  const data = buildForceGraphData(graph, 1000, 600);
  const node = data.nodes[0];
  if (!node) throw new Error("缺少节点");
  node.x = node.fx = 333;
  node.y = node.fy = 222;
  const next = buildForceGraphData(
    {
      ...graph,
      meta: { ...graph.meta, mode: "ego" },
      nodes: [
        ...graph.nodes,
        { slug: "c", title: "新增", page_type: "entity", link_count: 1 },
      ],
      edges: [...graph.edges, { source: "a", target: "c" }],
    },
    1000,
    600,
    data,
  );
  expect(next.nodes[0]).toBe(node);
  expect(node).toMatchObject({ x: 333, y: 222, fx: 333, fy: 222 });
  const added = next.nodes.find((entry) => entry.slug === "c");
  expect(added).toBeDefined();
  expect(Math.hypot((added?.x ?? 0) - 333, (added?.y ?? 0) - 222)).toBeLessThan(
    150,
  );
  expect(next.bySlug.has("c")).toBe(true);
});
