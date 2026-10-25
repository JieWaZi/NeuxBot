import {
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useReducer,
  useRef,
  useState,
  type Ref,
} from "react";
import { Plus } from "lucide-react";
import { IconAction } from "./knowledge-controls";
import ForceGraph2D, { type ForceGraphMethods } from "react-force-graph-2d";
import { graphForces } from "./knowledge-force-engine";
import { Button } from "@/shared/ui/button";
import { CanvasViewportControls } from "@/shared/ui/canvas-viewport-controls";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import type { KnowledgeGraph } from "./knowledge-graph-model";
import {
  buildForceGraphData,
  type ForceGraphLink,
  type ForceGraphData,
  type ForceGraphNode,
} from "./knowledge-force-data";
import {
  createForceGraphPainter,
  readGraphPalette,
} from "./knowledge-force-paint";
import {
  graphNodeRadius,
  graphPointBounds,
  initialGraphViewport,
  type GraphCluster,
} from "./knowledge-graph-layout";
import { useGraphCopy } from "./use-graph-copy";

export type KnowledgeGraphCanvasControls = {
  fit: () => void;
  focus: (slug: string) => void;
};
type Engine = ForceGraphMethods<ForceGraphNode, ForceGraphLink>;
type Viewport = { x: number; y: number; scale: number };

const emptyForceData: ForceGraphData = {
  nodes: [],
  links: [],
  bySlug: new Map(),
  adjacency: new Map(),
  clusters: [],
};

/** React 装配业务操作；Canvas 绘制、命中检测与布局时钟由第三方引擎管理。 */
export function KnowledgeGraphCanvas({
  graph,
  selected,
  onSelect,
  onFocus,
  showArrows = true,
  controlsRef,
  center,
  onBloom,
}: {
  graph: KnowledgeGraph;
  center?: string;
  onBloom?: (slug: string) => void;
  showArrows?: boolean;
  onFocus?: (slug: string) => void;
  controlsRef?: Ref<KnowledgeGraphCanvasControls>;
  selected: string;
  onSelect: (slug: string) => void;
}) {
  const t = useGraphCopy();
  const active = useRouteSceneActive();
  const host = useRef<HTMLDivElement>(null),
    bloomButton = useRef<HTMLDivElement>(null);
  const engine = useRef<Engine | undefined>(undefined);
  const viewport = useRef<Viewport | undefined>(undefined);
  const clickTimer = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined,
  );
  const pendingClick = useRef<string | undefined>(undefined);
  const hoverTimer = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined,
  );
  const [size, setSize] = useState({ width: 0, height: 0 });
  const [scale, setScale] = useState(1),
    [hovered, setHovered] = useState("");
  const [, redraw] = useReducer((value: number) => value + 1, 0);
  const [group, setGroup] = useState<{
    cluster: GraphCluster;
    members: Set<string>;
    viewport: Viewport;
  } | null>(null);
  const [reduced, setReduced] = useState(
    () =>
      window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false,
  );
  const [palette, setPalette] = useState(() =>
    readGraphPalette(document.documentElement),
  );
  const [model, setModel] = useState<{
    graph: KnowledgeGraph;
    data: ForceGraphData;
  }>();
  if (size.width && size.height && model?.graph !== graph)
    setModel({
      graph,
      data: buildForceGraphData(graph, size.width, size.height, model?.data),
    });
  const data = model?.data ?? emptyForceData;
  const painter = useMemo(
    () => createForceGraphPainter(data, palette),
    [data, palette],
  );
  const duration = reduced ? 0 : 300;
  const visible = Boolean(size.width && size.height);

  useEffect(() => {
    const container = host.current;
    if (!container) return;
    const resize = () => {
      const { clientWidth: width, clientHeight: height } = container;
      setSize((prior) =>
        prior.width === width && prior.height === height
          ? prior
          : { width, height },
      );
    };
    resize();
    const observer = new ResizeObserver(resize);
    observer.observe(container);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    const theme = new MutationObserver(() =>
      setPalette(readGraphPalette(document.documentElement)),
    );
    theme.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme", "class"],
    });
    const media = window.matchMedia?.("(prefers-reduced-motion: reduce)");
    const changed = () => setReduced(media?.matches ?? false);
    media?.addEventListener("change", changed);
    return () => {
      theme.disconnect();
      media?.removeEventListener("change", changed);
    };
  }, []);
  useEffect(() => {
    const api = engine.current;
    if (!api || !visible) return;
    api.d3Force("center", null);
    for (const [name, force] of Object.entries(graphForces(data)))
      api.d3Force(name, force);
    const retained = viewport.current;
    if (retained) {
      api.centerAt(retained.x, retained.y, 0);
      api.zoom(retained.scale, 0);
    } else {
      const initial = initialGraphViewport(
        data.nodes,
        size.width,
        size.height,
        data.bySlug.get(center ?? "") ?? data.nodes[0],
      );
      api.zoom(initial.scale, 0);
      api.centerAt(
        (size.width / 2 - initial.x) / initial.scale,
        (size.height / 2 - initial.y) / initial.scale,
        0,
      );
    }
    const sync = () => {
      if (active && !document.hidden) api.resumeAnimation();
      else {
        api.pauseAnimation();
        clearTimeout(clickTimer.current);
        clearTimeout(hoverTimer.current);
        pendingClick.current = undefined;
      }
    };
    sync();
    document.addEventListener("visibilitychange", sync);
    return () => {
      viewport.current = { ...api.centerAt(), scale: api.zoom() };
      api.pauseAnimation();
      clearTimeout(clickTimer.current);
      pendingClick.current = undefined;
      clearTimeout(hoverTimer.current);
      document.removeEventListener("visibilitychange", sync);
    };
  }, [data, visible, active, center, size.width, size.height]);
  useEffect(
    () => () => {
      clearTimeout(clickTimer.current);
      pendingClick.current = undefined;
      clearTimeout(hoverTimer.current);
    },
    [],
  );

  const focusNode = useCallback(
    (slug: string) => {
      const node = data.bySlug.get(slug);
      if (node)
        engine.current?.centerAt(
          node.x + Math.min(180, size.width / 5) / (engine.current.zoom() || 1),
          node.y,
          duration,
        );
    },
    [data, duration, size.width],
  );
  const fit = useCallback(
    () => engine.current?.zoomToFit(duration, 64),
    [duration],
  );
  useImperativeHandle(controlsRef, () => ({ fit, focus: focusNode }), [
    fit,
    focusNode,
  ]);
  function exitGroup() {
    if (!group) return;
    engine.current?.centerAt(group.viewport.x, group.viewport.y, duration);
    engine.current?.zoom(group.viewport.scale, duration);
    setGroup(null);
  }
  function focusGroup(cluster: GraphCluster) {
    if (group?.cluster.representative === cluster.representative) {
      exitGroup();
      return;
    }
    const api = engine.current;
    if (!api) return;
    const members = new Set(cluster.slugs);
    const points = data.nodes.filter((node) => members.has(node.slug));
    const bounds = graphPointBounds(points);
    setGroup({
      cluster,
      members,
      viewport: group?.viewport ?? { ...api.centerAt(), scale: api.zoom() },
    });
    api.centerAt(
      (bounds.left + bounds.right) / 2,
      (bounds.top + bounds.bottom) / 2,
      duration,
    );
    api.zoomToFit(duration, 80, (node) => members.has(node.slug));
  }
  const prepare = useCallback(
    (ctx: CanvasRenderingContext2D, zoom: number) => {
      painter.setFocus(selected, hovered, group?.members);
      painter.before(ctx, zoom);
    },
    [painter, selected, hovered, group],
  );
  const drawNode = (
    node: ForceGraphNode,
    ctx: CanvasRenderingContext2D,
    zoom: number,
  ) => painter.node(node, ctx, zoom);
  const drawLink = useCallback(
    (link: ForceGraphLink, ctx: CanvasRenderingContext2D, zoom: number) =>
      painter.link(link, ctx, zoom, showArrows),
    [painter, showArrows],
  );
  function clickNode(
    node: ForceGraphNode,
    event: Pick<MouseEvent, "shiftKey" | "detail">,
  ) {
    const repeated = pendingClick.current === node.slug;
    clearTimeout(clickTimer.current);
    pendingClick.current = undefined;
    if (event.shiftKey) onBloom?.(node.slug);
    else if (repeated || event.detail >= 2) onFocus?.(node.slug);
    else {
      pendingClick.current = node.slug;
      clickTimer.current = setTimeout(() => {
        pendingClick.current = undefined;
        onSelect(node.slug);
      }, 220);
    }
  }
  const hoverNode = data.bySlug.get(hovered);
  const canBloom = Boolean(
    center &&
    hoverNode &&
    hoverNode.slug !== center &&
    hoverNode.link_count > (data.adjacency.get(hovered)?.size ?? 0),
  );
  return (
    <div
      ref={host}
      role="group"
      aria-label={t("canvas")}
      tabIndex={0}
      className="bg-surface-canvas surface-pattern-dots surface-pattern-dots-canvas focus-visible:ring-ring relative min-h-0 min-w-0 flex-1 overflow-hidden outline-none focus-visible:ring-2 focus-visible:ring-inset"
      onKeyDown={(event) => {
        const api = engine.current;
        if (!api || event.target !== event.currentTarget) return;
        if (event.key === "Escape") {
          onSelect("");
          exitGroup();
        } else if (event.key === "Home") fit();
        else if (["+", "=", "-"].includes(event.key))
          api.zoom(api.zoom() * (event.key === "-" ? 1 / 1.1 : 1.1), duration);
        else if (event.key.startsWith("Arrow")) {
          const point = api.centerAt(),
            step = 60 / api.zoom();
          api.centerAt(
            point.x +
              (event.key === "ArrowLeft"
                ? -step
                : event.key === "ArrowRight"
                  ? step
                  : 0),
            point.y +
              (event.key === "ArrowUp"
                ? -step
                : event.key === "ArrowDown"
                  ? step
                  : 0),
            0,
          );
        } else return;
        event.preventDefault();
      }}
    >
      {visible && (
        <ForceGraph2D<ForceGraphNode, ForceGraphLink>
          ref={engine}
          width={size.width}
          height={size.height}
          graphData={data}
          nodeId="slug"
          nodeLabel={() => ""}
          nodeCanvasObject={drawNode}
          linkCanvasObject={drawLink}
          nodePointerAreaPaint={(node, color, ctx, zoom) => {
            ctx.fillStyle = color;
            ctx.beginPath();
            ctx.arc(
              node.x,
              node.y,
              Math.max(graphNodeRadius(node.link_count), 18 / zoom),
              0,
              Math.PI * 2,
            );
            ctx.fill();
          }}
          linkHoverPrecision={0}
          linkPointerAreaPaint={() => {}}
          onRenderFramePre={prepare}
          onRenderFramePost={(ctx, zoom) => {
            painter.after(ctx, zoom);
            if (hoverNode && bloomButton.current) {
              const point = engine.current?.graph2ScreenCoords(
                hoverNode.x,
                hoverNode.y,
              );
              if (point)
                bloomButton.current.style.transform = `translate(${point.x + graphNodeRadius(hoverNode.link_count) * zoom}px,${point.y - graphNodeRadius(hoverNode.link_count) * zoom - 36}px)`;
            }
          }}
          onEngineTick={() => painter.tick()}
          onEngineStop={() => {
            painter.invalidate();
            redraw();
          }}
          cooldownTicks={reduced ? 0 : 260}
          cooldownTime={Infinity}
          d3AlphaMin={0.02}
          d3AlphaDecay={0.02}
          autoPauseRedraw
          onNodeHover={(node) => {
            clearTimeout(hoverTimer.current);
            if (node) setHovered(node.slug);
            else
              hoverTimer.current = setTimeout(() => {
                if (!bloomButton.current?.matches(":hover, :focus-within"))
                  setHovered("");
              }, 60);
          }}
          onNodeClick={clickNode}
          onNodeDrag={() => {
            painter.invalidate();
          }}
          onNodeDragEnd={(node) => {
            node.fx = node.x;
            node.fy = node.y;
            node.pinned = true;
            painter.invalidate();
          }}
          onBackgroundClick={(event) => {
            const rect = host.current?.getBoundingClientRect();
            const api = engine.current;
            const point = api?.screen2GraphCoords(
              event.clientX - (rect?.left ?? 0),
              event.clientY - (rect?.top ?? 0),
            );
            const cluster =
              point && api ? painter.groupAt(point, api.zoom()) : undefined;
            if (cluster) focusGroup(cluster);
            else {
              onSelect("");
              if (!selected) exitGroup();
            }
          }}
          onZoomEnd={({ k }) => setScale(k)}
        />
      )}
      <div className="sr-only">
        {data.nodes.map((node, index) => (
          <button
            key={node.slug}
            type="button"
            aria-label={node.title}
            aria-pressed={selected === node.slug}
            tabIndex={
              node.slug === selected ||
              (!data.bySlug.has(selected) && index === 0)
                ? 0
                : -1
            }
            onFocus={() => {
              setHovered(node.slug);
              focusNode(node.slug);
            }}
            onBlur={(event) => {
              const next =
                event.relatedTarget instanceof Node
                  ? event.relatedTarget
                  : null;
              if (!bloomButton.current?.contains(next)) setHovered("");
            }}
            onClick={(event) => clickNode(node, event)}
            onKeyDown={(event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                if (event.ctrlKey || event.metaKey) onFocus?.(node.slug);
                else if (event.shiftKey) onBloom?.(node.slug);
                else onSelect(node.slug);
              } else if (event.key === "Escape") {
                event.preventDefault();
                onSelect("");
                exitGroup();
              } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
                event.preventDefault();
                const next =
                  event.key === "ArrowDown"
                    ? event.currentTarget.nextElementSibling
                    : event.currentTarget.previousElementSibling;
                if (next instanceof HTMLButtonElement) next.focus();
              }
            }}
          />
        ))}
        {data.clusters
          .filter((cluster) => cluster.slugs.length >= 5)
          .map((cluster) => (
            <button
              key={cluster.representative}
              type="button"
              aria-label={`${data.bySlug.get(cluster.anchors[0] ?? "")?.title ?? ""} · ${t("focusGroup")}`}
              onClick={() => focusGroup(cluster)}
            />
          ))}
      </div>
      {canBloom && hoverNode && (
        <div
          ref={bloomButton}
          className="bg-surface-panel rounded-control absolute top-0 left-0"
        >
          <IconAction
            icon={Plus}
            label={`${t("bloom")}：${hoverNode.title}`}
            onClick={() => onBloom?.(hoverNode.slug)}
          />
        </div>
      )}
      <CanvasViewportControls
        scale={scale}
        onZoomIn={() =>
          engine.current?.zoom((engine.current.zoom() || 1) * 1.1, duration)
        }
        onZoomOut={() =>
          engine.current?.zoom((engine.current.zoom() || 1) / 1.1, duration)
        }
        onFit={fit}
        labels={{ zoomIn: t("zoomIn"), zoomOut: t("zoomOut"), fit: t("fit") }}
      />
      {group && (
        <div className="bottom-scene-bottom absolute left-1/2 -translate-x-1/2">
          <Button variant="secondary" onClick={exitGroup}>
            {t("leaveGroup")} · Esc
          </Button>
        </div>
      )}
    </div>
  );
}
