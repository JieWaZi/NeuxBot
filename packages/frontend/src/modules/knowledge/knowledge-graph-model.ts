import type { WikiPage } from "./knowledge-wiki-queries";

export type KnowledgeGraphNode = {
  slug: string;
  title: string;
  page_type: string;
  link_count: number;
  pageId?: string;
};
export type KnowledgeGraph = {
  nodes: KnowledgeGraphNode[];
  edges: { source: string; target: string }[];
  meta: { mode: string; total: number; returned: number; truncated: boolean };
};

/** 完整分页读取后才构建链接图；悬空链接不虚构为知识节点。 */
export function buildLocalGraph(pages: WikiPage[]): KnowledgeGraph {
  const known = new Set(
    pages.flatMap((page) => (page.slug ? [page.slug] : [])),
  );
  const neighbors = new Map(
    [...known].map((slug) => [slug, new Set<string>()]),
  );
  const edges: KnowledgeGraph["edges"] = [];
  for (const page of pages) {
    if (!page.slug) continue;
    for (const target of new Set(page.outLinks)) {
      if (target === page.slug || !known.has(target)) continue;
      edges.push({ source: page.slug, target });
      neighbors.get(page.slug)?.add(target);
      neighbors.get(target)?.add(page.slug);
    }
  }
  const nodes = pages.flatMap((page) =>
    page.slug
      ? [
          {
            slug: page.slug,
            title: page.title || page.slug,
            page_type: page.pageType || "concept",
            pageId: page.id,
            link_count: neighbors.get(page.slug)?.size ?? 0,
          },
        ]
      : [],
  );
  return {
    nodes,
    edges,
    meta: {
      mode: "overview",
      total: nodes.length,
      returned: nodes.length,
      truncated: false,
    },
  };
}

/** 从完整关系中选择概览或双向邻域；节点大小始终反映完整关联数。 */
export function graphNeighborhood(
  graph: KnowledgeGraph,
  center = "",
  depth = 1,
  limit = 500,
): KnowledgeGraph {
  const neighbors = new Map(
    graph.nodes.map((node) => [node.slug, new Set<string>()]),
  );
  for (const edge of graph.edges) {
    neighbors.get(edge.source)?.add(edge.target);
    neighbors.get(edge.target)?.add(edge.source);
  }
  const selected = new Set<string>();
  if (center && neighbors.has(center)) {
    selected.add(center);
    let frontier = [center];
    for (let step = 0; step < depth; step++) {
      const next: string[] = [];
      for (const slug of frontier)
        for (const neighbor of neighbors.get(slug) ?? []) {
          if (!selected.has(neighbor)) {
            selected.add(neighbor);
            next.push(neighbor);
          }
        }
      frontier = next;
    }
  }
  const candidates = graph.nodes.filter(
    (node) => !center || selected.has(node.slug),
  );
  const nodes = candidates
    .toSorted(
      (a, b) =>
        Number(b.slug === center) - Number(a.slug === center) ||
        b.link_count - a.link_count ||
        a.slug.localeCompare(b.slug),
    )
    .slice(0, limit);
  const included = new Set(nodes.map((node) => node.slug));
  return {
    nodes,
    edges: graph.edges.filter(
      (edge) => included.has(edge.source) && included.has(edge.target),
    ),
    meta: {
      mode: center ? "ego" : "overview",
      total: graph.meta.total,
      returned: nodes.length,
      truncated: candidates.length > nodes.length,
    },
  };
}

export function graphAdjacency(graph: KnowledgeGraph) {
  const adjacency = new Map(
    graph.nodes.map((node) => [node.slug, new Set<string>()]),
  );
  for (const edge of graph.edges) {
    adjacency.get(edge.source)?.add(edge.target);
    adjacency.get(edge.target)?.add(edge.source);
  }
  return adjacency;
}

/** 迁自 WeKnora 的 isFrontierCandidate；索引页不参与批量展开。 */
export function graphFrontier(graph: KnowledgeGraph, center: string) {
  const adjacency = graphAdjacency(graph);
  return graph.nodes
    .filter(
      (node) =>
        node.slug !== center &&
        node.page_type !== "index" &&
        node.link_count > (adjacency.get(node.slug)?.size ?? 0),
    )
    .map((node) => node.slug);
}

/** 迁自 WeKnora mergeGraphData/evictBloomOverflow；原始节点与最新操作锚点始终保留。 */
export function mergeGraphData(
  base: KnowledgeGraph,
  additions: KnowledgeGraph[],
  protect: ReadonlySet<string>,
  limit = 1500,
): KnowledgeGraph {
  const nodes = new Map(base.nodes.map((node) => [node.slug, node]));
  const generations = new Map(base.nodes.map((node) => [node.slug, 0]));
  additions.forEach((graph, index) => {
    for (const node of graph.nodes)
      if (!nodes.has(node.slug)) {
        nodes.set(node.slug, node);
        generations.set(node.slug, index + 1);
      }
  });
  if (nodes.size > limit) {
    const candidates = [...nodes.keys()]
      .filter((slug) => (generations.get(slug) ?? 0) > 0 && !protect.has(slug))
      .sort((a, b) => (generations.get(a) ?? 0) - (generations.get(b) ?? 0));
    for (const slug of candidates) {
      if (nodes.size <= limit) break;
      nodes.delete(slug);
    }
  }
  const edges = new Map<string, KnowledgeGraph["edges"][number]>();
  for (const graph of [base, ...additions])
    for (const edge of graph.edges)
      if (nodes.has(edge.source) && nodes.has(edge.target))
        edges.set(JSON.stringify([edge.source, edge.target]), edge);
  return {
    nodes: [...nodes.values()],
    edges: [...edges.values()],
    meta: {
      mode: base.meta.mode,
      total: Math.max(
        base.meta.total,
        ...additions.map((graph) => graph.meta.total),
      ),
      returned: nodes.size,
      truncated: [base, ...additions].some((graph) => graph.meta.truncated),
    },
  };
}
