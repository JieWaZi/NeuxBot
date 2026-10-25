import { expect, it } from "vitest";
import {
  buildLocalGraph,
  graphNeighborhood,
  graphFrontier,
  mergeGraphData,
} from "./knowledge-graph-model";

it("图谱只连接真实页面，去重定向链接并保留孤立页面", () => {
  const graph = buildLocalGraph([
    {
      id: "a",
      slug: "a",
      title: "甲",
      pageType: "concept",
      outLinks: ["b", "b", "missing", "a"],
    },
    { id: "b", slug: "b", title: "乙", pageType: "entity", outLinks: ["a"] },
    { id: "c", slug: "c", title: "孤立", pageType: "summary" },
  ]);
  expect(graph.edges).toEqual([
    { source: "a", target: "b" },
    { source: "b", target: "a" },
  ]);
  expect(graph.nodes.map((node) => [node.slug, node.link_count])).toEqual([
    ["a", 1],
    ["b", 1],
    ["c", 0],
  ]);
});

it("上游叠加语义去重链接，前沿排除索引，容量回收保留初始节点和当前锚点", () => {
  const full = buildLocalGraph([
    { slug: "a", outLinks: ["b"] },
    { slug: "b", outLinks: ["c"] },
    { slug: "c", outLinks: ["d"] },
    { slug: "d", outLinks: ["e"] },
    { slug: "e" },
  ]);
  const base = graphNeighborhood(full, "a", 1);
  expect(graphFrontier(base, "a")).toEqual(["b"]);
  expect(
    graphFrontier(
      {
        ...base,
        nodes: base.nodes.map((node) => ({ ...node, page_type: "index" })),
      },
      "a",
    ),
  ).toEqual([]);
  const merged = mergeGraphData(
    base,
    [graphNeighborhood(full, "b", 1), graphNeighborhood(full, "d", 1)],
    new Set(["d"]),
    4,
  );
  expect(merged.nodes.map((node) => node.slug).toSorted()).toEqual([
    "a",
    "b",
    "d",
    "e",
  ]);
  expect(merged.edges).toEqual([
    { source: "a", target: "b" },
    { source: "d", target: "e" },
  ]);
});

it("概览明确截断，邻域可以找到概览之外的页面且保留真实关联数", () => {
  const graph = buildLocalGraph([
    { slug: "a", outLinks: ["b"] },
    { slug: "b", outLinks: ["c"] },
    { slug: "c", outLinks: ["d"] },
    { slug: "d" },
    { slug: "isolated" },
  ]);
  const overview = graphNeighborhood(graph, "", 1, 2);
  expect(overview.nodes.map((node) => node.slug)).toEqual(["b", "c"]);
  expect(overview.meta).toMatchObject({
    total: 5,
    returned: 2,
    truncated: true,
  });
  const ego = graphNeighborhood(graph, "d", 1, 2);
  expect(ego.nodes.map((node) => node.slug).toSorted()).toEqual(["c", "d"]);
  expect(ego.nodes.find((node) => node.slug === "c")?.link_count).toBe(2);
  expect(graphNeighborhood(graph, "isolated", 2).nodes).toHaveLength(1);
});

it("概览沿用 WeKnora 的 500 节点预算，超出部分明确截断", () => {
  const graph = buildLocalGraph(
    Array.from({ length: 501 }, (_, i) => ({ slug: `page-${i}` })),
  );
  const overview = graphNeighborhood(graph);
  expect(overview.nodes).toHaveLength(500);
  expect(overview.meta).toMatchObject({
    total: 501,
    returned: 500,
    truncated: true,
  });
});
