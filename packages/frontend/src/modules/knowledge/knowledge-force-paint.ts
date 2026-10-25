import {
  graphClusterOutline,
  graphLandmarkTitle,
  graphNodeRadius,
  visibleGraphLabels,
  type GraphCluster,
  type GraphLabel,
  type GraphPoint,
} from "./knowledge-graph-layout";
import type {
  ForceGraphData,
  ForceGraphLink,
  ForceGraphNode,
} from "./knowledge-force-data";

export type GraphPalette = {
  text: string;
  muted: string;
  surface: string;
  edge: string;
  outline: string;
  primary: string;
  types: Record<string, string>;
  font: string;
};

/** Canvas 不继承 SVG class；在主题变化时一次性解析已有语义颜色。 */
export function readGraphPalette(host: HTMLElement): GraphPalette {
  const style = getComputedStyle(host);
  const color = (token: string) =>
    style.getPropertyValue(token).trim() || style.color;
  return {
    text: color("--content-primary"),
    muted: color("--content-secondary"),
    surface: color("--surface-panel"),
    edge: color("--border-strong"),
    outline: color("--border-default"),
    primary: color("--primary"),
    font: style.fontFamily,
    types: {
      entity: color("--status-success-content"),
      concept: color("--status-warning-content"),
      summary: color("--primary"),
      synthesis: color("--status-info-content"),
      comparison: color("--status-danger-content"),
      index: color("--content-secondary"),
    },
  };
}

type Caption = {
  node: ForceGraphNode;
  text: string;
  width: number;
  cluster: GraphCluster;
  box: GraphLabel;
};

/** 只在坐标、缩放或强调对象变化时重算几何；平移复用已有标签与轮廓。 */
export function createForceGraphPainter(
  data: ForceGraphData,
  palette: GraphPalette,
) {
  let selected = "",
    hovered = "",
    group: ReadonlySet<string> | undefined;
  let focusNeighbors = new Set<string>();
  let dirty = true,
    geometryDirty = true,
    frames = 0,
    lastScale = NaN;
  let visible = new Set<string>();
  let captions: Caption[] = [];
  let paths: Path2D[] = [];
  const widths = new Map<string, number>();
  const important = new Set(
    [...data.nodes]
      .sort((a, b) => b.link_count - a.link_count)
      .slice(0, Math.max(1, Math.min(12, Math.ceil(data.nodes.length / 30))))
      .map((n) => n.slug),
  );
  const label = (node: ForceGraphNode) =>
    node.title.length > 14 ? node.title.slice(0, 14) + "…" : node.title;
  const radius = (node: ForceGraphNode) => graphNodeRadius(node.link_count);
  const highlighted = (node: ForceGraphNode) =>
    node.slug === selected || node.slug === hovered;
  const opacity = (node: ForceGraphNode) =>
    selected || hovered
      ? focusNeighbors.has(node.slug)
        ? 1
        : 0.2
      : group && !group.has(node.slug)
        ? 0.12
        : 1;
  const textWidth = (ctx: CanvasRenderingContext2D, text: string) => {
    let width = widths.get(text);
    if (width === undefined) {
      width = ctx.measureText(text).width;
      widths.set(text, width);
    }
    return width;
  };
  function prepare(ctx: CanvasRenderingContext2D, scale: number) {
    if (!dirty && scale === lastScale) return;
    const geometryChanged = geometryDirty || scale !== lastScale;
    dirty = false;
    geometryDirty = false;
    lastScale = scale;
    ctx.font = `12px ${palette.font}`;
    captions = data.clusters.flatMap((cluster) =>
      cluster.slugs.length >= 5
        ? cluster.anchors.flatMap((slug) => {
            const node = data.bySlug.get(slug);
            const outside = Boolean(group && !group.has(slug));
            if (!node || (outside ? slug !== cluster.anchors[0] : scale >= 1.2))
              return [];
            const text = graphLandmarkTitle(node.title).slice(0, 20),
              width = textWidth(ctx, text);
            const y = (node.y + radius(node)) * scale + 18;
            return [
              {
                node,
                text,
                width,
                cluster,
                box: {
                  slug: `anchor:${slug}`,
                  priority: 100000 + node.link_count,
                  required: outside || highlighted(node),
                  left: node.x * scale - width / 2 - 8,
                  right: node.x * scale + width / 2 + 8,
                  top: y - 16,
                  bottom: y + 5,
                },
              },
            ];
          })
        : [],
    );
    const captioned = new Set(captions.map((c) => c.node.slug));
    const candidates = data.nodes.flatMap((node): GraphLabel[] => {
      if (
        captioned.has(node.slug) ||
        (!highlighted(node) &&
          scale < 0.5 &&
          !(group?.has(node.slug) && scale >= 0.4) &&
          !important.has(node.slug))
      )
        return [];
      const half = textWidth(ctx, label(node)) / 2 + 5,
        y = (node.y + radius(node)) * scale + 16;
      return [
        {
          slug: node.slug,
          priority: node.link_count,
          required: highlighted(node),
          left: node.x * scale - half,
          right: node.x * scale + half,
          top: y - 15,
          bottom: y + 4,
        },
      ];
    });
    visible = visibleGraphLabels([
      ...captions.map((c) => c.box),
      ...candidates,
    ]);
    captions = captions.filter((c) => visible.has(c.box.slug));
    if (geometryChanged)
      paths =
        typeof Path2D === "undefined"
          ? []
          : data.clusters.map(
              (cluster) =>
                new Path2D(
                  graphClusterOutline(
                    cluster.slugs.flatMap((slug) => {
                      const node = data.bySlug.get(slug);
                      return node
                        ? [{ x: node.x, y: node.y, radius: radius(node) }]
                        : [];
                    }),
                    scale,
                  ),
                ),
            );
  }
  function before(ctx: CanvasRenderingContext2D, scale: number) {
    prepare(ctx, scale);
    ctx.save();
    ctx.fillStyle = palette.outline;
    ctx.globalAlpha = 0.45;
    for (const path of paths) ctx.fill(path);
    ctx.restore();
  }
  function drawText(
    ctx: CanvasRenderingContext2D,
    text: string,
    x: number,
    y: number,
    scale: number,
    color: string,
  ) {
    ctx.font = `12px ${palette.font}`;
    ctx.textAlign = "center";
    ctx.textBaseline = "alphabetic";
    ctx.save();
    ctx.translate(x, y);
    ctx.scale(1 / scale, 1 / scale);
    ctx.strokeStyle = palette.surface;
    ctx.lineWidth = 3;
    ctx.strokeText(text, 0, 0);
    ctx.fillStyle = color;
    ctx.fillText(text, 0, 0);
    ctx.restore();
  }
  function node(
    node: ForceGraphNode,
    ctx: CanvasRenderingContext2D,
    scale: number,
  ) {
    ctx.save();
    ctx.globalAlpha = opacity(node);
    const r = radius(node),
      color = palette.types[node.page_type] ?? palette.primary;
    ctx.beginPath();
    ctx.arc(node.x, node.y, r + (highlighted(node) ? 3 : 0), 0, Math.PI * 2);
    ctx.fillStyle = color;
    ctx.fill();
    if (node.slug === selected) {
      ctx.beginPath();
      ctx.arc(node.x, node.y, r + 4 / scale, 0, Math.PI * 2);
      ctx.strokeStyle = color;
      ctx.lineWidth = 1.5 / scale;
      ctx.stroke();
    }
    if (visible.has(node.slug))
      drawText(
        ctx,
        label(node),
        node.x,
        node.y + r + 16 / scale,
        scale,
        palette.muted,
      );
    ctx.restore();
  }
  function link(
    edge: ForceGraphLink,
    ctx: CanvasRenderingContext2D,
    scale: number,
    arrows: boolean,
  ) {
    const a = edge.source,
      b = edge.target;
    const dx = b.x - a.x,
      dy = b.y - a.y,
      distance = Math.hypot(dx, dy) || 1;
    const ux = dx / distance,
      uy = dy / distance;
    const active = highlighted(a) || highlighted(b);
    const start = {
      x: a.x + ux * (radius(a) + 2),
      y: a.y + uy * (radius(a) + 2),
    };
    const end = {
      x: b.x - ux * (radius(b) + 2),
      y: b.y - uy * (radius(b) + 2),
    };
    ctx.save();
    ctx.strokeStyle = ctx.fillStyle = active ? palette.primary : palette.edge;
    ctx.globalAlpha =
      selected || hovered
        ? active
          ? 0.85
          : 0.08
        : group && (!group.has(a.slug) || !group.has(b.slug))
          ? 0.05
          : 0.42;
    ctx.lineWidth = (active ? 1.3 : 0.8) / scale;
    ctx.beginPath();
    ctx.moveTo(start.x, start.y);
    ctx.lineTo(end.x, end.y);
    ctx.stroke();
    if (arrows && scale >= 0.8) {
      const arrow = (tip: GraphPoint, direction: number) => {
        const length = 6 / scale,
          half = 2.5 / scale;
        ctx.beginPath();
        ctx.moveTo(tip.x, tip.y);
        ctx.lineTo(
          tip.x - ux * length * direction - uy * half,
          tip.y - uy * length * direction + ux * half,
        );
        ctx.lineTo(
          tip.x - ux * length * direction + uy * half,
          tip.y - uy * length * direction - ux * half,
        );
        ctx.closePath();
        ctx.fill();
      };
      arrow(end, 1);
      if (edge.bidirectional) arrow(start, -1);
    }
    ctx.restore();
  }
  return {
    before,
    node,
    link,
    after(ctx: CanvasRenderingContext2D, scale: number) {
      for (const caption of captions)
        drawText(
          ctx,
          caption.text,
          caption.node.x,
          caption.node.y + radius(caption.node) + 18 / scale,
          scale,
          palette.text,
        );
    },
    setFocus(
      nextSelected: string,
      nextHovered: string,
      nextGroup?: ReadonlySet<string>,
    ) {
      if (
        selected === nextSelected &&
        hovered === nextHovered &&
        group === nextGroup
      )
        return;
      selected = nextSelected;
      hovered = nextHovered;
      group = nextGroup;
      focusNeighbors = new Set([
        selected,
        hovered,
        ...(data.adjacency.get(selected) ?? []),
        ...(data.adjacency.get(hovered) ?? []),
      ]);
      dirty = true;
    },
    tick() {
      if (++frames % 6 === 0) dirty = geometryDirty = true;
    },
    invalidate() {
      dirty = geometryDirty = true;
    },
    groupAt(point: GraphPoint, scale: number) {
      return captions.find(
        ({ box }) =>
          point.x * scale >= box.left &&
          point.x * scale <= box.right &&
          point.y * scale >= box.top &&
          point.y * scale <= box.bottom,
      )?.cluster;
    },
  };
}
