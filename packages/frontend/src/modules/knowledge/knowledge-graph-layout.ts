// 来源：Tencent/WeKnora，固定提交 8776a2d92d11ad3e76865d51cf3192e37e2a9976。
// Copyright (C) 2026 Tencent. All rights reserved. MIT 许可与迁入边界见根目录 THIRD_PARTY_NOTICES.md。
// Geometry is independent of SVG so dense and disconnected graphs can be
// exercised without a browser. Coordinates are world units, not screen pixels.
export interface GraphPoint {
  x: number;
  y: number;
}
export interface GraphLink {
  source: string;
  target: string;
}
export interface LayoutInput {
  slug: string;
  link_count: number;
  page_type?: string;
  title?: string;
}
export interface GraphBounds {
  left: number;
  top: number;
  right: number;
  bottom: number;
}
export interface LayoutPlacement extends GraphPoint {
  center: GraphPoint;
  stationary?: boolean;
  degree?: number;
}
export interface GraphCluster extends GraphPoint, GraphBounds {
  slugs: string[];
  representative: string;
  anchors: string[];
}
export interface GraphSection extends GraphBounds {
  kind: "unlinked" | "offscreen";
  count: number;
}
export function graphNodeRadius(linkCount: number): number {
  // Square-root scaling keeps leaves small so hubs read as landmarks.
  return Math.max(8, Math.min(23, 7 + Math.sqrt(Math.max(0, linkCount)) * 1.8));
}
/** Deterministic multi-level Louvain over an undirected, unweighted graph.
 * Returns a community index per node; callers must pass nodes in a stable
 * order because ties are resolved by index.
 */
export function graphCommunities(
  neighbors: readonly (readonly number[])[],
): number[] {
  const n = neighbors.length;
  let membership = Array.from({ length: n }, (_, i) => i);
  // Level graph: weighted adjacency between super-nodes plus the weight of
  // edges already collapsed inside each super-node.
  let weights: Map<number, number>[] = neighbors.map((list) => {
    const row = new Map<number, number>();
    for (const j of list) row.set(j, (row.get(j) ?? 0) + 1);
    return row;
  });
  let inner = new Array<number>(n).fill(0);
  for (;;) {
    const size = weights.length;
    const degree = weights.map(
      (row, i) =>
        [...row.values()].reduce((sum, w) => sum + w, 0) +
        2 * requiredLayoutValue(inner[i]),
    );
    const total = degree.reduce((sum, d) => sum + d, 0);
    if (!total) break;
    const community = Array.from({ length: size }, (_, i) => i);
    const tot = [...degree];
    let movedAny = false;
    for (let pass = 0; pass < 32; pass++) {
      let moved = false;
      for (let i = 0; i < size; i++) {
        const own = requiredLayoutValue(community[i]);
        const links = new Map<number, number>();
        for (const [j, w] of requiredLayoutValue(weights[i]))
          links.set(
            requiredLayoutValue(community[j]),
            (links.get(requiredLayoutValue(community[j])) ?? 0) + w,
          );
        tot[own] =
          requiredLayoutValue(tot[own]) - requiredLayoutValue(degree[i]);
        let best = own,
          bestGain =
            (links.get(own) ?? 0) -
            (requiredLayoutValue(tot[own]) * requiredLayoutValue(degree[i])) /
              total;
        for (const c of [...links.keys()].sort((a, b) => a - b)) {
          const gain =
            requiredLayoutValue(links.get(c)) -
            (requiredLayoutValue(tot[c]) * requiredLayoutValue(degree[i])) /
              total;
          if (gain > bestGain + 1e-9) {
            best = c;
            bestGain = gain;
          }
        }
        tot[best] =
          requiredLayoutValue(tot[best]) + requiredLayoutValue(degree[i]);
        if (best !== own) {
          community[i] = best;
          moved = true;
          movedAny = true;
        }
      }
      if (!moved) break;
    }
    if (!movedAny) break;
    const renumber = new Map<number, number>();
    for (const c of community)
      if (!renumber.has(c)) renumber.set(c, renumber.size);
    const next = Array.from(
      { length: renumber.size },
      () => new Map<number, number>(),
    );
    const nextInner = new Array<number>(renumber.size).fill(0);
    for (let i = 0; i < size; i++) {
      const ci = requiredLayoutValue(
        renumber.get(requiredLayoutValue(community[i])),
      );
      nextInner[ci] =
        requiredLayoutValue(nextInner[ci]) + requiredLayoutValue(inner[i]);
      for (const [j, w] of requiredLayoutValue(weights[i])) {
        const cj = requiredLayoutValue(
          renumber.get(requiredLayoutValue(community[j])),
        );
        // Each undirected edge appears in both rows; count it once inside.
        if (ci === cj)
          nextInner[ci] = requiredLayoutValue(nextInner[ci]) + w / 2;
        else
          requiredLayoutValue(next[ci]).set(
            cj,
            (requiredLayoutValue(next[ci]).get(cj) ?? 0) + w,
          );
      }
    }
    membership = membership.map((c) =>
      requiredLayoutValue(renumber.get(requiredLayoutValue(community[c]))),
    );
    weights = next;
    inner = nextInner;
    if (renumber.size === size) break;
  }
  return membership;
}
/** Landmark caption text: summary pages carry their source file name. */
export function graphLandmarkTitle(title: string): string {
  const name = title
    .replace(/\s*-\s*Summary$/i, "")
    .replace(
      /\.(pdf|docx?|pptx?|xlsx?|csv|md|txt|html?|json|ya?ml|xml|png|jpe?g)$/i,
      "",
    )
    .replace(/\s*[(（]\d+[)）]$/, "")
    .trim();
  return name || title;
}
// A summary is named after its source file, which usually names the document
// well, so it only yields near-ties to entities and concepts. Index pages are
// navigation and a poor name for a region even when they link to all of it.
const anchorTypeQuality: Record<string, number> = {
  entity: 1.15,
  concept: 1,
  synthesis: 0.8,
  comparison: 0.8,
  summary: 0.9,
  index: 0.2,
};
/** Name a connected component and its distinct themes.
 * The first landmark is the component's most prominent readable page, even
 * when it bridges every theme. Communities are the themes; each contributes
 * at most one landmark, chosen by its links inside the theme and how well its
 * title reads as a name. Themes mostly linked to an existing landmark, and
 * pages that repeat one, get no extra name. Later picks favor larger themes
 * far from earlier labels so names spread instead of stacking up. A component
 * built from several documents also names the page most of them share.
 */
export function selectGraphAnchors(
  slugs: string[],
  adjacency: ReadonlyMap<string, ReadonlySet<string>>,
  pages: ReadonlyMap<
    string,
    Pick<LayoutInput, "page_type" | "title">
  > = new Map(),
): string[] {
  if (!slugs.length) return [];
  const order = [...new Set(slugs)].sort();
  const index = new Map(order.map((slug, i) => [slug, i]));
  const neighbors = order.map((slug) =>
    [...(adjacency.get(slug) ?? [])]
      .flatMap((n) =>
        index.has(n) && n !== slug ? [requiredLayoutValue(index.get(n))] : [],
      )
      .sort((a, b) => a - b),
  );
  const membership = graphCommunities(neighbors);
  const groups = new Map<number, number[]>();
  membership.forEach((c, i) => {
    const group = groups.get(c) ?? [];
    group.push(i);
    groups.set(c, group);
  });
  const quality = (i: number) => {
    const page = pages.get(requiredLayoutValue(order[i]));
    const title = graphLandmarkTitle(page?.title ?? "");
    // An identifier such as an arXiv file name ("2412.18925v1") has no word to read.
    const readable = /[\u4e00-\u9fff]{2}|[a-z]{3}/i.test(title) ? 1 : 0.3;
    return (
      (anchorTypeQuality[page?.page_type ?? ""] ?? 1) *
      (title.length > 18 ? 0.6 : 1) *
      readable
    );
  };
  const best = (group: number[], score: (i: number) => number) =>
    group.reduce((top, i) => {
      const a = score(i),
        b = score(top);
      return a > b + 1e-9 ||
        (Math.abs(a - b) <= 1e-9 &&
          requiredLayoutValue(neighbors[i]).length >
            requiredLayoutValue(neighbors[top]).length)
        ? i
        : top;
    });
  // A theme is named by a page that lives in it; links elsewhere still count
  // for some prominence.
  const themes = [...groups.values()]
    .map((group) => {
      const inside = new Set(group);
      const score = (i: number) => {
        const internal = requiredLayoutValue(neighbors[i]).filter((j) =>
          inside.has(j),
        ).length;
        return (
          (internal +
            0.5 * (requiredLayoutValue(neighbors[i]).length - internal) +
            1) *
          quality(i)
        );
      };
      const ranked = [...group].sort(
        (a, b) =>
          score(b) - score(a) ||
          requiredLayoutValue(neighbors[b]).length -
            requiredLayoutValue(neighbors[a]).length ||
          a - b,
      );
      return { size: group.length, ranked, members: inside };
    })
    .sort(
      (a, b) =>
        b.size - a.size ||
        requiredLayoutValue(a.ranked[0]) - requiredLayoutValue(b.ranked[0]),
    );
  const name = best(
    order.map((_, i) => i),
    (i) => (requiredLayoutValue(neighbors[i]).length + 1) * quality(i),
  );
  const home = requiredLayoutValue(
    themes.find((theme) => theme.members.has(name)),
  );
  // Fragments of a theme are not themes of their own.
  const minimum = Math.max(3, requiredLayoutValue(themes[0]).size * 0.3);
  const candidates = themes.filter(
    (theme) => theme !== home && theme.size >= minimum,
  );
  // A second name for something already labeled: nearly the same
  // neighborhood (an author next to their paper) or a title that contains
  // the other ("北京市第三代社保卡" next to "第三代社保卡").
  const closed = (i: number) =>
    new Set([i, ...requiredLayoutValue(neighbors[i])]);
  const normalized = (i: number) =>
    graphLandmarkTitle(pages.get(requiredLayoutValue(order[i]))?.title ?? "")
      .toLowerCase()
      .replace(/[\s《》“”"'‘’（）()·・]/g, "");
  const redundant = (i: number, chosen: number[]) =>
    chosen.some((s) => {
      const a = closed(i),
        b = closed(s);
      const shared = [...a].filter((x) => b.has(x)).length;
      if (shared / (a.size + b.size - shared) >= 0.5) return true;
      const x = normalized(i),
        y = normalized(s);
      return !!x && !!y && (x.includes(y) || y.includes(x));
    });
  const budget =
    order.length < 16
      ? 1
      : order.length < 50
        ? 2
        : order.length < 100
          ? 3
          : order.length < 200
            ? 4
            : 5;
  const bfs = (start: number) => {
    const distance = new Array<number>(order.length).fill(Infinity);
    distance[start] = 0;
    const queue = [start];
    for (let q = 0; q < queue.length; q++) {
      for (const j of requiredLayoutValue(
        neighbors[requiredLayoutValue(queue[q])],
      )) {
        if (distance[j] !== Infinity) continue;
        distance[j] =
          requiredLayoutValue(distance[requiredLayoutValue(queue[q])]) + 1;
        queue.push(j);
      }
    }
    return distance;
  };
  // Labels count as fully separated once they are a fair share of the
  // component's diameter apart; closer than that, a larger theme still wins.
  const nearest = bfs(name);
  const far = nearest.reduce(
    (top, d, i) =>
      d !== Infinity && d > requiredLayoutValue(nearest[top]) ? i : top,
    name,
  );
  const diameter = Math.max(...bfs(far).filter(Number.isFinite));
  const reach = Math.max(2, diameter / budget);
  const selected = [name];
  const open = new Set(candidates);
  while (selected.length < budget && open.size) {
    let pick:
        | {
            theme: (typeof candidates)[number];
            label: number;
          }
        | undefined,
      bestScore = -1;
    for (const theme of open) {
      // Most of the theme already hangs off an existing label (one document
      // around its subject): another name would only be a detail page.
      const covered = selected.some(
        (s) =>
          requiredLayoutValue(neighbors[s]).filter((j) => theme.members.has(j))
            .length *
            2 >=
          theme.size,
      );
      const label = covered
        ? undefined
        : theme.ranked.find((i) => !redundant(i, selected));
      if (label === undefined) {
        open.delete(theme);
        continue;
      }
      const score =
        (Math.sqrt(theme.size) *
          Math.min(requiredLayoutValue(nearest[label]), reach)) /
        reach;
      if (
        score > bestScore + 1e-9 ||
        (Math.abs(score - bestScore) <= 1e-9 &&
          pick &&
          requiredLayoutValue(nearest[label]) >
            requiredLayoutValue(nearest[pick.label]))
      ) {
        pick = { theme, label };
        bestScore = score;
      }
    }
    if (!pick) break;
    open.delete(pick.theme);
    selected.push(pick.label);
    bfs(pick.label).forEach((d, i) => {
      nearest[i] = Math.min(requiredLayoutValue(nearest[i]), d);
    });
  }
  // Themes mostly follow documents, so their names say which documents are
  // here but not why they belong together. A page shared by most of them
  // says that; it is added unless a label already is one.
  const documents = new Set(
    order.flatMap((slug, i) =>
      pages.get(slug)?.page_type === "summary" ? [i] : [],
    ),
  );
  if (documents.size >= 2) {
    const spans = (i: number) =>
      documents.has(i)
        ? 0
        : requiredLayoutValue(neighbors[i]).filter((j) => documents.has(j))
            .length;
    const needed = Math.max(2, Math.ceil((documents.size * 2) / 3));
    if (!selected.some((s) => spans(s) >= needed)) {
      const weakest = Math.min(
        ...selected.map((s) => requiredLayoutValue(neighbors[s]).length),
      );
      const prominence = (i: number) =>
        (requiredLayoutValue(neighbors[i]).length + 1) * quality(i);
      const bridge = order
        .map((_, i) => i)
        .filter(
          (i) =>
            spans(i) >= needed &&
            requiredLayoutValue(neighbors[i]).length >= weakest * 0.6 &&
            !redundant(i, selected),
        )
        .reduce<number | undefined>(
          (top, i) =>
            top === undefined ||
            spans(i) > spans(top) ||
            (spans(i) === spans(top) && prominence(i) > prominence(top) + 1e-9)
              ? i
              : top,
          undefined,
        );
      if (bridge !== undefined) selected.push(bridge);
    }
  }
  return selected.map((i) => requiredLayoutValue(order[i]));
}
/** Pack connected components separately; singletons get two explicit shelves.
 * A missing visible edge is not evidence that the page has no links in the KB.
 */
export function buildGraphLayout(
  nodes: LayoutInput[],
  edges: GraphLink[],
  width: number,
  height: number,
  prior: ReadonlyMap<string, GraphPoint> = new Map(),
) {
  const nodeBySlug = new Map(nodes.map((n) => [n.slug, n]));
  const adjacency = new Map(nodes.map((n) => [n.slug, new Set<string>()]));
  for (const e of edges) {
    if (
      e.source === e.target ||
      !nodeBySlug.has(e.source) ||
      !nodeBySlug.has(e.target)
    )
      continue;
    requiredLayoutValue(adjacency.get(e.source)).add(e.target);
    requiredLayoutValue(adjacency.get(e.target)).add(e.source);
  }
  const components: string[][] = [];
  const unlinked: string[] = [];
  const offscreen: string[] = [];
  const visited = new Set<string>();
  for (const slug of [...nodeBySlug.keys()].sort()) {
    if (visited.has(slug)) continue;
    visited.add(slug);
    if (!requiredLayoutValue(adjacency.get(slug)).size) {
      (requiredLayoutValue(nodeBySlug.get(slug)).link_count > 0
        ? offscreen
        : unlinked
      ).push(slug);
      continue;
    }
    const component = [slug];
    for (let i = 0; i < component.length; i++) {
      for (const neighbor of requiredLayoutValue(
        adjacency.get(requiredLayoutValue(component[i])),
      )) {
        if (visited.has(neighbor)) continue;
        visited.add(neighbor);
        component.push(neighbor);
      }
    }
    components.push(component.sort());
  }
  components.sort(
    (a, b) =>
      b.length - a.length ||
      (requiredLayoutValue(a[0]) < requiredLayoutValue(b[0]) ? -1 : 1),
  );
  // Reserve space from each component's topology, not a circular boundary.
  // These envelopes only place groups initially; the simulation never clips
  // nodes to them or pulls individual nodes towards a common center.
  const aspect = Math.min(1.8, Math.max(0.8, width / Math.max(height, 1)));
  const stretch = Math.sqrt(aspect);
  const goldenAngle = Math.PI * (3 - Math.sqrt(5));
  const clusters: GraphCluster[] = [];
  const seeds = new Map<string, GraphPoint>();
  const cellSize = 500;
  const cells = new Map<string, GraphCluster[]>();
  const keysFor = (bounds: GraphBounds) => {
    const keys: string[] = [];
    for (
      let gx = Math.floor(bounds.left / cellSize);
      gx <= Math.floor(bounds.right / cellSize);
      gx++
    ) {
      for (
        let gy = Math.floor(bounds.top / cellSize);
        gy <= Math.floor(bounds.bottom / cellSize);
        gy++
      )
        keys.push(`${gx},${gy}`);
    }
    return keys;
  };
  for (const slugs of components) {
    const representative = requiredLayoutValue(
      [...slugs].sort(
        (a, b) =>
          requiredLayoutValue(adjacency.get(b)).size -
            requiredLayoutValue(adjacency.get(a)).size || (a < b ? -1 : 1),
      )[0],
    );
    // Seed a spanning tree from the best-connected page. Branches follow
    // their parent direction, so chains and trees are not born as disks.
    const local = new Map<
      string,
      GraphPoint & {
        heading: number;
      }
    >();
    local.set(representative, { x: 0, y: 0, heading: 0 });
    const queue = [representative];
    for (let cursor = 0; cursor < queue.length; cursor++) {
      const parentSlug = requiredLayoutValue(queue[cursor]);
      const parent = requiredLayoutValue(local.get(parentSlug));
      const children = [...requiredLayoutValue(adjacency.get(parentSlug))]
        .filter((slug) => !local.has(slug))
        .sort();
      children.forEach((slug, i) => {
        const heading =
          parent.heading +
          (cursor === 0
            ? i * goldenAngle
            : ((i + 0.5) / children.length - 0.5) * Math.PI * 0.8);
        const distance =
          150 + (children.length > 6 ? Math.sqrt(i) * 30 : (i % 3) * 12);
        local.set(slug, {
          x: parent.x + Math.cos(heading) * distance,
          y: parent.y + Math.sin(heading) * distance,
          heading,
        });
        queue.push(slug);
      });
    }
    const mean = { x: 0, y: 0 };
    for (const point of local.values()) {
      mean.x += point.x / local.size;
      mean.y += point.y / local.size;
    }
    for (const [slug, point] of local)
      seeds.set(slug, { x: point.x - mean.x, y: point.y - mean.y });
    const extent = graphPointBounds(
      slugs.map((slug) => requiredLayoutValue(seeds.get(slug))),
    );
    // Room for repulsion and the caption; this is spacing, not a force limit.
    const padding = 160;
    let x: number, y: number, bounds: GraphBounds;
    for (let attempt = 0; ; attempt++) {
      const distance = Math.sqrt(attempt) * 48;
      x = Math.cos(attempt * goldenAngle) * distance * stretch;
      y = (Math.sin(attempt * goldenAngle) * distance) / stretch;
      bounds = {
        left: x + extent.left - padding,
        right: x + extent.right + padding,
        top: y + extent.top - padding,
        bottom: y + extent.bottom + padding,
      };
      const neighbors = new Set(
        keysFor(bounds).flatMap((key) => cells.get(key) ?? []),
      );
      if (
        [...neighbors].every(
          (c) =>
            bounds.right <= c.left ||
            bounds.left >= c.right ||
            bounds.bottom <= c.top ||
            bounds.top >= c.bottom,
        )
      )
        break;
    }
    const cluster = {
      x,
      y,
      ...bounds,
      slugs,
      representative,
      anchors: selectGraphAnchors(slugs, adjacency, nodeBySlug),
    };
    clusters.push(cluster);
    for (const key of keysFor(bounds)) {
      const bucket = cells.get(key) ?? [];
      bucket.push(cluster);
      cells.set(key, bucket);
    }
  }
  const left = Math.min(0, ...clusters.map((c) => c.left));
  const top = Math.min(0, ...clusters.map((c) => c.top));
  const placements = new Map<string, LayoutPlacement>();
  const sections: GraphSection[] = [];
  let right = 0,
    bottom = 0;
  for (const cluster of clusters) {
    cluster.x -= left;
    cluster.y -= top;
    cluster.left -= left;
    cluster.right -= left;
    cluster.top -= top;
    cluster.bottom -= top;
    const center = { x: cluster.x, y: cluster.y };
    for (const slug of cluster.slugs) {
      const point = requiredLayoutValue(seeds.get(slug));
      placements.set(slug, {
        x: center.x + point.x,
        y: center.y + point.y,
        center,
        degree: requiredLayoutValue(adjacency.get(slug)).size,
      });
    }
    right = Math.max(right, cluster.right);
    bottom = Math.max(bottom, cluster.bottom);
  }
  const gap = 80;
  // Independent pages stay on a readable grid rather than accumulating at a
  // shared force center. The shelves are not semantic clusters.
  for (const [kind, slugs] of [
    ["offscreen", offscreen],
    ["unlinked", unlinked],
  ] as const) {
    if (!slugs.length) continue;
    const shelfWidth = right || Math.sqrt(slugs.length * 180 * 100 * aspect);
    const columns = Math.min(
      slugs.length,
      Math.max(3, Math.floor(shelfWidth / 180)),
    );
    const top = bottom ? bottom + gap : 0;
    const sectionWidth = columns * 180;
    const sectionHeight = Math.ceil(slugs.length / columns) * 100 + 50;
    const left = Math.max(0, (right - sectionWidth) / 2);
    slugs.forEach((slug, i) => {
      const point = {
        x: left + (i % columns) * 180 + 90,
        y: top + 70 + Math.floor(i / columns) * 100,
      };
      placements.set(slug, { ...point, center: point, stationary: true });
    });
    sections.push({
      kind,
      count: slugs.length,
      left,
      top,
      right: left + sectionWidth,
      bottom: top + sectionHeight,
    });
    bottom = top + sectionHeight;
    right = Math.max(right, left + sectionWidth);
  }
  const dx = (width - right) / 2,
    dy = (height - bottom) / 2;
  for (const placement of placements.values()) {
    placement.x += dx;
    placement.y += dy;
    // Centers are shared by a component; translate them only once below.
  }
  for (const center of new Set([...placements.values()].map((p) => p.center))) {
    center.x += dx;
    center.y += dy;
  }
  for (const cluster of clusters) {
    cluster.x += dx;
    cluster.y += dy;
    cluster.left += dx;
    cluster.right += dx;
    cluster.top += dy;
    cluster.bottom += dy;
  }
  for (const section of sections) {
    section.left += dx;
    section.right += dx;
    section.top += dy;
    section.bottom += dy;
  }
  // During expansion, keep the existing mental map. New nodes start near an
  // already visible neighbor; old nodes are held still by the simulation.
  if (prior.size) {
    for (const slugs of components) {
      const existing = slugs.flatMap((slug) =>
        prior.has(slug) ? [requiredLayoutValue(prior.get(slug))] : [],
      );
      if (!existing.length) continue;
      const center = {
        x: existing.reduce((sum, p) => sum + p.x, 0) / existing.length,
        y: existing.reduce((sum, p) => sum + p.y, 0) / existing.length,
      };
      slugs.forEach((slug, i) => {
        const neighbor = [...requiredLayoutValue(adjacency.get(slug))].find(
          (nb) => prior.has(nb),
        );
        const anchor =
          prior.get(slug) ??
          (neighbor ? requiredLayoutValue(prior.get(neighbor)) : center);
        const angle = i * Math.PI * (3 - Math.sqrt(5));
        const point = prior.has(slug)
          ? anchor
          : {
              x: anchor.x + Math.cos(angle) * 90,
              y: anchor.y + Math.sin(angle) * 90,
            };
        placements.set(slug, {
          ...point,
          center,
          degree: requiredLayoutValue(adjacency.get(slug)).size,
        });
      });
    }
    for (const [slug, point] of prior) {
      const placement = placements.get(slug);
      if (placement) {
        placement.x = point.x;
        placement.y = point.y;
      }
    }
  }
  return {
    placements,
    sections: prior.size ? [] : sections,
    clusters: prior.size ? [] : clusters,
  };
}
export interface GraphLabel extends GraphBounds {
  slug: string;
  priority: number;
  required?: boolean;
}
/** Greedy screen-space label placement. Focused labels are never suppressed. */
export function visibleGraphLabels(labels: GraphLabel[]): Set<string> {
  const visible = new Set<string>();
  const occupied = new Map<string, GraphLabel[]>();
  const cells = (label: GraphLabel) => {
    const keys: string[] = [];
    for (
      let x = Math.floor(label.left / 160);
      x <= Math.floor(label.right / 160);
      x++
    ) {
      for (
        let y = Math.floor(label.top / 32);
        y <= Math.floor(label.bottom / 32);
        y++
      )
        keys.push(`${x},${y}`);
    }
    return keys;
  };
  for (const label of [...labels].sort(
    (a, b) =>
      Number(!!b.required) - Number(!!a.required) || b.priority - a.priority,
  )) {
    const keys = cells(label);
    const overlaps = keys.some((key) =>
      (occupied.get(key) ?? []).some(
        (other) =>
          label.left < other.right &&
          label.right > other.left &&
          label.top < other.bottom &&
          label.bottom > other.top,
      ),
    );
    if (overlaps && !label.required) continue;
    visible.add(label.slug);
    for (const key of keys) {
      const bucket = occupied.get(key) ?? [];
      bucket.push(label);
      occupied.set(key, bucket);
    }
  }
  return visible;
}
export function graphPointBounds(points: GraphPoint[]): GraphBounds {
  if (!points.length) return { left: 0, top: 0, right: 0, bottom: 0 };
  return {
    left: Math.min(...points.map((p) => p.x)),
    right: Math.max(...points.map((p) => p.x)),
    top: Math.min(...points.map((p) => p.y)),
    bottom: Math.max(...points.map((p) => p.y)),
  };
}
/** Start at a readable scale. Fitting the entire graph is an explicit action. */
export function initialGraphViewport(
  points: GraphPoint[],
  width: number,
  height: number,
  focus?: GraphPoint,
) {
  const bounds = graphPointBounds(points);
  const fit = Math.min(
    Math.max(100, width - 120) / Math.max(100, bounds.right - bounds.left),
    Math.max(100, height - 120) / Math.max(100, bounds.bottom - bounds.top),
  );
  const scale = Math.max(0.4, Math.min(0.85, fit));
  const center =
    fit < 0.4 && focus
      ? focus
      : {
          x: (bounds.left + bounds.right) / 2,
          y: (bounds.top + bounds.bottom) / 2,
        };
  return {
    scale,
    x: width / 2 - center.x * scale,
    y: height / 2 - center.y * scale,
  };
}
/** Smooth envelope of the node disks with a stable screen-space gutter.
 * The hull is taken over disks already grown by the gutter, then traced with
 * a closed spline whose tangents are limited by the adjacent segment length,
 * so it bows outwards from the hull without loops or cusps.
 */
export function graphClusterOutline(
  points: (GraphPoint & {
    radius?: number;
  })[],
  scale = 1,
): string {
  if (!points.length) return "";
  const zoom = Number.isFinite(scale) && scale > 0 ? scale : 1;
  // Capped in world units so far-out zoom levels do not merge separate groups.
  const padding = Math.min(22 / zoom, 60);
  const samples = 24;
  const expanded = points
    .flatMap((p) => {
      const radius = Math.max(0, p.radius ?? 0) + padding;
      return Array.from({ length: samples }, (_, i) => ({
        x: p.x + Math.cos((i * 2 * Math.PI) / samples) * radius,
        y: p.y + Math.sin((i * 2 * Math.PI) / samples) * radius,
      }));
    })
    .sort((a, b) => a.x - b.x || a.y - b.y);
  const unique = expanded.filter(
    (p, i) =>
      !i ||
      p.x !== requiredLayoutValue(expanded[i - 1]).x ||
      p.y !== requiredLayoutValue(expanded[i - 1]).y,
  );
  const cross = (o: GraphPoint, a: GraphPoint, b: GraphPoint) =>
    (a.x - o.x) * (b.y - o.y) - (a.y - o.y) * (b.x - o.x);
  const half = (list: GraphPoint[]) => {
    const hull: GraphPoint[] = [];
    for (const point of list) {
      while (
        hull.length > 1 &&
        cross(
          requiredLayoutValue(hull[hull.length - 2]),
          requiredLayoutValue(hull[hull.length - 1]),
          point,
        ) <= 0
      )
        hull.pop();
      hull.push(point);
    }
    hull.pop();
    return hull;
  };
  const hull = [...half(unique), ...half([...unique].reverse())];
  const at = (i: number) =>
    requiredLayoutValue(hull[(i + hull.length) % hull.length]);
  const fmt = (p: GraphPoint) =>
    `${Math.round(p.x * 10) / 10},${Math.round(p.y * 10) / 10}`;
  const tangent = (from: GraphPoint, to: GraphPoint, length: number) => {
    const dx = to.x - from.x,
      dy = to.y - from.y,
      norm = Math.hypot(dx, dy) || 1;
    return { x: (dx / norm) * length, y: (dy / norm) * length };
  };
  const path = [`M${fmt(requiredLayoutValue(hull[0]))}`];
  hull.forEach((p, i) => {
    const next = at(i + 1);
    const reach = Math.hypot(next.x - p.x, next.y - p.y) * 0.36;
    const out = tangent(at(i - 1), next, reach),
      into = tangent(p, at(i + 2), reach);
    path.push(
      `C${fmt({ x: p.x + out.x, y: p.y + out.y })} ${fmt({ x: next.x - into.x, y: next.y - into.y })} ${fmt(next)}`,
    );
  });
  return path.join(" ") + " Z";
}

/** 上游基于拓扑索引的已知引用在宿主中显式校验，不使用非空断言。 */
function requiredLayoutValue<T>(value: T | undefined | null): T {
  if (value === undefined || value === null)
    throw new Error("图谱布局内部引用缺失");
  return value;
}
