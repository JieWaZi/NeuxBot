import { forceCollide, forceLink, forceManyBody, type Force } from "d3-force";
import type {
  ForceGraphData,
  ForceGraphLink,
  ForceGraphNode,
} from "./knowledge-force-data";
import { graphNodeRadius } from "./knowledge-graph-layout";

/** 标准 D3 力与连通分组漂移约束，生产引擎和无浏览器布局验证复用。 */
export function graphForces(
  data: ForceGraphData,
): Record<string, Force<ForceGraphNode, ForceGraphLink>> {
  const components = new Map<object, ForceGraphNode[]>();
  for (const node of data.nodes) {
    const members = components.get(node.center) ?? [];
    members.push(node);
    components.set(node.center, members);
  }
  return {
    link: forceLink<ForceGraphNode, ForceGraphLink>(data.links)
      .id((node) => node.slug)
      .distance(140)
      .strength(0.03),
    charge: forceManyBody<ForceGraphNode>().strength(-300).distanceMax(300),
    collision: forceCollide<ForceGraphNode>(
      (node) => graphNodeRadius(node.link_count) + 8,
    ).iterations(3),
    components: (alpha) => {
      for (const nodes of components.values()) {
        const anchor = nodes[0]?.center;
        if (!anchor || nodes.some((node) => node.fx !== undefined)) continue;
        const dx =
          (anchor.x - nodes.reduce((sum, n) => sum + n.x, 0) / nodes.length) *
          0.03 *
          alpha;
        const dy =
          (anchor.y - nodes.reduce((sum, n) => sum + n.y, 0) / nodes.length) *
          0.03 *
          alpha;
        for (const node of nodes) {
          node.vx = (node.vx ?? 0) + dx;
          node.vy = (node.vy ?? 0) + dy;
        }
      }
    },
  };
}
