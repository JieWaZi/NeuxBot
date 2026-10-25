import type {
  KnowledgeGraph,
  KnowledgeGraphNode,
} from "./knowledge-graph-model";
import { graphAdjacency } from "./knowledge-graph-model";
import {
  buildGraphLayout,
  type GraphCluster,
  type GraphPoint,
} from "./knowledge-graph-layout";

export type ForceGraphNode = KnowledgeGraphNode & {
  x: number;
  y: number;
  vx?: number;
  vy?: number;
  fx?: number;
  fy?: number;
  pinned: boolean;
  stationary: boolean;
  center: GraphPoint;
};
export type ForceGraphLink = {
  source: ForceGraphNode;
  target: ForceGraphNode;
  bidirectional: boolean;
};
export type ForceGraphData = {
  nodes: ForceGraphNode[];
  links: ForceGraphLink[];
  bySlug: Map<string, ForceGraphNode>;
  adjacency: Map<string, Set<string>>;
  clusters: GraphCluster[];
};

/** 查询结果只读；D3 的坐标、固定位置和边端点仅写入独立的渲染对象。 */
export function buildForceGraphData(
  graph: KnowledgeGraph,
  width: number,
  height: number,
  prior?: ForceGraphData,
): ForceGraphData {
  const incremental = graph.meta.mode === "ego";
  const positions = new Map(prior?.nodes.map((node) => [node.slug, node]));
  const layout = buildGraphLayout(
    graph.nodes,
    graph.edges,
    width,
    height,
    incremental ? positions : undefined,
  );
  const nodes = graph.nodes.map((page): ForceGraphNode => {
    const previous = positions.get(page.slug);
    const placement = layout.placements.get(page.slug);
    const node = previous ?? {
      ...page,
      x: placement?.x ?? 0,
      y: placement?.y ?? 0,
      center: placement?.center ?? { x: 0, y: 0 },
      pinned: false,
      stationary: Boolean(placement?.stationary),
    };
    Object.assign(node, page);
    if (node.pinned || node.stationary || (incremental && previous)) {
      node.fx = node.x;
      node.fy = node.y;
    } else {
      delete node.fx;
      delete node.fy;
    }
    return node;
  });
  const bySlug = new Map(nodes.map((node) => [node.slug, node]));
  const directed = new Map<string, Set<string>>();
  for (const edge of graph.edges) {
    const targets = directed.get(edge.source) ?? new Set<string>();
    targets.add(edge.target);
    directed.set(edge.source, targets);
  }
  const links: ForceGraphLink[] = [];
  const seen = new Map<string, Set<string>>();
  for (const edge of graph.edges) {
    const source = bySlug.get(edge.source),
      target = bySlug.get(edge.target);
    if (
      !source ||
      !target ||
      source === target ||
      seen.get(source.slug)?.has(target.slug) ||
      seen.get(target.slug)?.has(source.slug)
    )
      continue;
    const targets = seen.get(source.slug) ?? new Set<string>();
    targets.add(target.slug);
    seen.set(source.slug, targets);
    links.push({
      source,
      target,
      bidirectional: directed.get(target.slug)?.has(source.slug) ?? false,
    });
  }
  return {
    nodes,
    links,
    bySlug,
    adjacency: graphAdjacency(graph),
    clusters: incremental ? [] : layout.clusters,
  };
}
