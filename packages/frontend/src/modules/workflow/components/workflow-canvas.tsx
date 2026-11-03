import {
  Bot,
  FileCode2,
  MousePointerClick,
  Settings2,
  Workflow,
  Zap,
} from "lucide-react";
import {
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type FocusEvent,
  type PointerEvent,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";
import { CanvasViewportControls } from "@/shared/ui/canvas-viewport-controls";
import { Card } from "@/shared/ui/card";
import { Badge } from "@/shared/ui/badge";
import { Dialog, DialogTrigger } from "@/shared/ui/dialog";
import { workflowStatusAppearance } from "../model/status-presentation";
import { cn } from "@/shared/lib/cn";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import {
  workflowCanvasLayout,
  workflowPhaseEdges,
  workflowInputNodeWidth,
  type CanvasItem,
} from "../model/canvas-layout";
import type {
  Manifest,
  WorkflowNode,
  WorkflowPhase,
  WorkflowNodeCount,
} from "../model/types";
import { WorkerReference } from "./worker-reference";
import { CanvasDetail } from "./canvas-detail";
import { workflowConnectionPath } from "../model/canvas-routing";

const nodeIcons: Record<string, typeof Bot> = {
  worker: Bot,
  askUser: MousePointerClick,
  functionTask: Settings2,
  action: Zap,
  workflow: Workflow,
  startWorkflow: Workflow,
  joinWorkflow: Workflow,
};

export function NodeKind({
  kind,
  label,
  actionType,
}: {
  kind: string;
  label?: string;
  actionType?: string;
}) {
  const { t } = useTranslation("workflow");
  const Icon =
    actionType === "script" ? FileCode2 : (nodeIcons[kind] ?? Workflow);
  const names: Record<string, string> = {
    worker: t("kind.worker"),
    askUser: t("kind.askUser"),
    functionTask: t("kind.functionTask"),
    action: t("kind.action"),
    workflow: t("kind.workflow"),
    startWorkflow: t("kind.startWorkflow"),
    joinWorkflow: t("kind.joinWorkflow"),
  };
  return (
    <span className="text-muted-foreground type-caption inline-flex items-center gap-1.5">
      <Icon className="size-icon-sm" aria-hidden="true" strokeWidth={1.5} />
      {label ?? names[kind] ?? kind}
    </span>
  );
}

export function WorkflowCanvas({
  active: contentActive = true,
  manifest,
  inputNode,
  inputDetails,
  hasDock = false,
  selected,
  onSelect,
  phaseEditor,
  nodeContent,
  nodeHeading,
  onDismiss,
  listMode = false,
  nodeCounts,
  onSelectInstance,
  selectedTargets = [],
  onSelectTargets,
}: {
  active?: boolean;
  manifest: Manifest;
  inputNode?: ReactNode;
  hasDock?: boolean;
  inputDetails?: {
    title: string;
    open: boolean;
    onOpenChange: (open: boolean) => void;
    content: ReactNode;
  };
  selectedTargets?: { kind: "phase" | "node"; id: string }[];
  onSelectTargets?: (targets: { kind: "phase" | "node"; id: string }[]) => void;
  selected?: string;
  onSelect: (node: WorkflowNode) => void;
  phaseEditor?: (phase: WorkflowPhase, close: () => void) => ReactNode;
  nodeContent?: (node: WorkflowNode) => ReactNode;
  nodeHeading?: (node: WorkflowNode) => { title?: string; status?: string };
  onDismiss?: () => void;
  listMode?: boolean;
  nodeCounts?: WorkflowNodeCount[];
  onSelectInstance?: (instanceId: string) => void;
}) {
  const { t } = useTranslation("workflow");
  const active = useRouteSceneActive() && contentActive;
  const interactionHintId = useId();
  const [visible, setVisible] = useState(
    () => document.visibilityState !== "hidden",
  );
  const host = useRef<HTMLDivElement>(null);
  const [adjustingPhase, setAdjustingPhase] = useState<string>();
  const [detailsContainer, setDetailsContainer] =
    useState<HTMLDivElement | null>(null);
  const contentHost = useRef<HTMLDivElement>(null);
  const dragging = useRef<
    | {
        x: number;
        y: number;
        originX: number;
        originY: number;
      }
    | undefined
  >(undefined);
  const [view, setView] = useState({ x: 24, y: 24, scale: 1 });
  const [viewportWidth, setViewportWidth] = useState(1440);
  const [inputHeight, setInputHeight] = useState(336);
  const [phaseHeights, setPhaseHeights] = useState<Record<string, number>>({});
  const hasInput = Boolean(inputNode);
  // 连线寻路只取决于业务拓扑与实测尺寸，平移、缩放和选择不重复执行 A*。
  const { phaseLayouts, width, height, inputPath, connections } =
    useMemo(() => {
      const phaseLayouts = workflowCanvasLayout(manifest, hasInput, {
        maxWidth: viewportWidth,
        phaseHeights,
        inputHeight,
      });
      const inputBounds = {
        left: 24,
        top: 24,
        width: workflowInputNodeWidth,
        height: inputHeight,
      };
      const bounds = hasInput ? [inputBounds, ...phaseLayouts] : phaseLayouts;
      const width = Math.max(
        328,
        ...bounds.map((item) => item.left + item.width + 24),
      );
      const contentBottom = Math.max(
        24,
        ...bounds.map((item) => item.top + item.height),
      );
      const connections = workflowPhaseEdges(manifest).flatMap((edge) => {
        const source = phaseLayouts.find((item) => item.phase.id === edge.from);
        const target = phaseLayouts.find((item) => item.phase.id === edge.to);
        return source && target
          ? [{ ...edge, path: workflowConnectionPath(source, target, bounds) }]
          : [];
      });
      return {
        phaseLayouts,
        width,
        height: Math.max(360, contentBottom + 48),
        inputPath:
          hasInput && phaseLayouts[0]
            ? workflowConnectionPath(inputBounds, phaseLayouts[0], bounds)
            : undefined,
        connections,
      };
    }, [manifest, hasInput, viewportWidth, phaseHeights, inputHeight]);
  const initialFit = useRef(false);
  const userMoved = useRef(false);
  useEffect(() => {
    const update = () => setVisible(document.visibilityState !== "hidden");
    document.addEventListener("visibilitychange", update);
    return () => document.removeEventListener("visibilitychange", update);
  }, []);
  const fit = (minimumScale = 0.25) => {
    const box = host.current?.getBoundingClientRect();
    if (!box) return;
    const scale = Math.min(
      1.2,
      Math.max(
        minimumScale,
        Math.min((box.width - 48) / width, (box.height - 48) / height),
      ),
    );
    setView({
      x: Math.max(24, (box.width - width * scale) / 2),
      y: Math.max(24, (box.height - height * scale) / 2),
      scale,
    });
  };
  const fitRef = useRef(fit);
  useEffect(() => {
    fitRef.current = fit;
  });
  useEffect(() => {
    const element = host.current;
    if (!element || listMode) return;
    let fitted = false;
    const observer = new ResizeObserver((entries) => {
      const box = entries[0]?.contentRect;
      if (box && box.width > 0) setViewportWidth(box.width);
      if (
        box &&
        box.width > 0 &&
        box.height > 0 &&
        (!fitted || !userMoved.current)
      ) {
        fitted = true;
        initialFit.current = true;
        requestAnimationFrame(() => {
          if (!userMoved.current) fitRef.current(0.65);
        });
      }
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [listMode]);
  useEffect(() => {
    const element = contentHost.current;
    if (!element || listMode) return;
    const phases = Array.from(
      element.querySelectorAll<HTMLElement>("[data-workflow-phase]"),
    );
    const items = Array.from(
      element.querySelectorAll<HTMLElement>("[data-workflow-layout-item]"),
    );
    const observer = new ResizeObserver(() => {
      setPhaseHeights(
        Object.fromEntries(
          phases.map((phase) => [
            phase.dataset.workflowPhase ?? "",
            phase.offsetHeight,
          ]),
        ),
      );
      const input = element.querySelector<HTMLElement>("[data-workflow-input]");
      if (input) setInputHeight(input.offsetHeight);
    });
    for (const item of items) observer.observe(item);
    return () => observer.disconnect();
  }, [manifest, listMode, hasInput]);
  useEffect(() => {
    if (!initialFit.current || userMoved.current || listMode) return;
    const frame = requestAnimationFrame(() => fitRef.current(0.65));
    return () => cancelAnimationFrame(frame);
  }, [height, width, listMode]);
  useEffect(() => {
    const element = host.current;
    if (!element || listMode) return;
    const wheel = (event: WheelEvent) => {
      if (
        event.target instanceof Element &&
        event.target.closest("[data-workflow-canvas-details]")
      )
        return;
      event.preventDefault();
      userMoved.current = true;
      if (event.ctrlKey || event.metaKey) {
        const box = element.getBoundingClientRect();
        const x = event.clientX - box.left,
          y = event.clientY - box.top;
        setView((previous) => {
          const scale = Math.min(
            2,
            Math.max(0.25, previous.scale * Math.exp(-event.deltaY * 0.01)),
          );
          const ratio = scale / previous.scale;
          return {
            scale,
            x: x - (x - previous.x) * ratio,
            y: y - (y - previous.y) * ratio,
          };
        });
      } else
        setView((previous) => ({
          ...previous,
          x: previous.x - event.deltaX,
          y: previous.y - event.deltaY,
        }));
    };
    element.addEventListener("wheel", wheel, { passive: false });
    return () => element.removeEventListener("wheel", wheel);
  }, [listMode]);
  const zoom = (delta: number) => {
    userMoved.current = true;
    const box = host.current?.getBoundingClientRect();
    setView((previous) => {
      const scale = Math.min(2, Math.max(0.25, previous.scale + delta));
      const x = (box?.width ?? 0) / 2,
        y = (box?.height ?? 0) / 2;
      const ratio = scale / previous.scale;
      return {
        scale,
        x: x - (x - previous.x) * ratio,
        y: y - (y - previous.y) * ratio,
      };
    });
  };
  const [selectionRect, setSelectionRect] = useState<{
    x: number;
    y: number;
    width: number;
    height: number;
  }>();
  const spacePan = useRef(false);
  useEffect(() => {
    const reset = () => {
      spacePan.current = false;
    };
    window.addEventListener("blur", reset);
    return () => window.removeEventListener("blur", reset);
  }, []);
  const selectionOrigin = useRef<
    { x: number; y: number; extend: boolean } | undefined
  >(undefined);
  const finishSelection = () => {
    const origin = selectionOrigin.current;
    const box = host.current?.getBoundingClientRect();
    if (origin && selectionRect && box && onSelectTargets) {
      const targets = Array.from(
        host.current?.querySelectorAll<HTMLElement>("[data-workflow-phase]") ??
          [],
      )
        .filter((element) => {
          const r = element.getBoundingClientRect();
          return (
            r.left < box.left + selectionRect.x + selectionRect.width &&
            r.right > box.left + selectionRect.x &&
            r.top < box.top + selectionRect.y + selectionRect.height &&
            r.bottom > box.top + selectionRect.y
          );
        })
        .flatMap((element) =>
          element.dataset.workflowPhase
            ? [{ kind: "phase" as const, id: element.dataset.workflowPhase }]
            : [],
        );
      onSelectTargets(
        origin.extend
          ? [
              ...selectedTargets,
              ...targets.filter(
                (target) =>
                  !selectedTargets.some(
                    (item) =>
                      item.kind === target.kind && item.id === target.id,
                  ),
              ),
            ]
          : targets,
      );
    }
    selectionOrigin.current = undefined;
    setSelectionRect(undefined);
  };
  const down = (event: PointerEvent<HTMLDivElement>) => {
    // React Portal 的事件仍沿组件树冒泡；只允许真实画布 DOM 参与平移。
    if (
      !(event.target instanceof Node) ||
      !event.currentTarget.contains(event.target)
    )
      return;
    if (
      event.target instanceof Element &&
      event.target.closest(
        "[data-workflow-canvas-details], [data-canvas-viewport-controls], button, input, textarea, select, a, [role=button], [contenteditable=true]",
      )
    )
      return;
    event.currentTarget.focus({ preventScroll: true });
    if (
      onSelectTargets &&
      event.button === 0 &&
      event.shiftKey &&
      !event.altKey &&
      !spacePan.current
    ) {
      const box = event.currentTarget.getBoundingClientRect();
      selectionOrigin.current = {
        x: event.clientX - box.left,
        y: event.clientY - box.top,
        extend: event.shiftKey,
      };
      setSelectionRect({
        x: event.clientX - box.left,
        y: event.clientY - box.top,
        width: 0,
        height: 0,
      });
      event.currentTarget.setPointerCapture(event.pointerId);
      event.preventDefault();
      return;
    }
    if (event.button !== 0 && event.button !== 1) return;
    userMoved.current = true;
    event.currentTarget.setPointerCapture(event.pointerId);
    dragging.current = {
      x: event.clientX,
      y: event.clientY,
      originX: view.x,
      originY: view.y,
    };
    event.preventDefault();
  };
  const revealFocusedItem = (event: FocusEvent<HTMLElement>) => {
    if (listMode) return;
    const box = host.current?.getBoundingClientRect();
    const nodeBox = event.currentTarget.getBoundingClientRect();
    if (!box) return;
    const dx =
      nodeBox.left < box.left + 24
        ? box.left + 24 - nodeBox.left
        : nodeBox.right > box.right - 24
          ? box.right - 24 - nodeBox.right
          : 0;
    const dy =
      nodeBox.top < box.top + 24
        ? box.top + 24 - nodeBox.top
        : nodeBox.bottom > box.bottom - 24
          ? box.bottom - 24 - nodeBox.bottom
          : 0;
    if (dx || dy) {
      userMoved.current = true;
      setView((previous) => ({
        ...previous,
        x: previous.x + dx,
        y: previous.y + dy,
      }));
    }
  };
  const inputAnchor = hasInput ? (
    <div
      data-workflow-input
      data-workflow-layout-item
      onFocus={revealFocusedItem}
      className="min-w-0"
      style={
        listMode
          ? undefined
          : {
              position: "absolute",
              left: 24,
              top: 24,
              width: workflowInputNodeWidth,
            }
      }
    >
      {inputDetails ? (
        <DialogTrigger asChild>{inputNode}</DialogTrigger>
      ) : (
        inputNode
      )}
    </div>
  ) : null;
  const input = inputDetails ? (
    <Dialog
      modal={false}
      open={active && inputDetails.open}
      onOpenChange={inputDetails.onOpenChange}
    >
      {inputAnchor}
      {detailsContainer ? (
        <CanvasDetail
          container={detailsContainer}
          title={inputDetails.title}
          data-workflow-input-details
          data-motion={active && inputDetails.open ? "popup" : "none"}
          scrollable={false}
        >
          {inputDetails.content}
        </CanvasDetail>
      ) : null}
    </Dialog>
  ) : (
    inputAnchor
  );
  const renderNode = (node: WorkflowNode) => {
    const heading = nodeHeading?.(node);
    const title = heading?.title || node.label || t("nodeDetails");
    const running =
      nodeCounts?.some(
        (item) =>
          item.nodeId === node.id &&
          item.status === "running" &&
          (item.count ?? 0) > 0,
      ) ?? false;
    const targetSelected = selectedTargets.some(
      (target) => target.kind === "node" && target.id === node.id,
    );
    const behavior = node.description;
    const action =
      node.kind === "action"
        ? manifest.actions?.find(
            (action) => action.id === node.execution.actionId,
          )
        : undefined;
    const description =
      typeof behavior === "string" ? behavior : action?.description;
    const card = (
      <Card
        asChild
        variant="resource"
        data-state={selected === node.id ? "selected" : undefined}
        data-running-shimmer={running}
        className={cn(
          targetSelected && "ring-accent-border ring-1",
          "data-[state=selected]:not-focus-visible:ring-0",
        )}
        data-workflow-node={node.id}
      >
        <button
          type="button"
          aria-label={t("selectNode", { label: node.label })}
          aria-pressed={selected === node.id}
          onClick={() => {
            onSelect(node);
            const actual = nodeCounts?.find(
              (item) => item.nodeId === node.id && item.instanceId,
            );
            if (actual?.instanceId) onSelectInstance?.(actual.instanceId);
          }}
          className="focus-visible:ring-ring relative grid w-full min-w-0 gap-1.5 px-3 py-2 text-left outline-none focus-visible:ring-2"
          onFocus={revealFocusedItem}
        >
          <span className="type-body line-clamp-2 font-medium break-words whitespace-pre-wrap">
            {node.label}
          </span>
          {node.kind === "worker" ? (
            <WorkerReference
              resolve={node.execution.resolve}
              showModel={false}
            />
          ) : (
            <NodeKind
              kind={node.kind}
              actionType={
                node.kind === "action" ? node.execution.actionType : undefined
              }
              label={action?.type === "script" ? t("localScript") : undefined}
            />
          )}
          {description ? (
            <span className="text-muted-foreground type-caption line-clamp-2 break-words whitespace-pre-wrap">
              {description}
            </span>
          ) : null}
          {nodeCounts ? (
            <span className="type-caption flex flex-wrap gap-2">
              {!nodeCounts.some((item) => item.nodeId === node.id) ? (
                <span>{t("unexecuted")}</span>
              ) : null}
              {nodeCounts
                .filter((item) => item.nodeId === node.id)
                .map((item) => (
                  <Badge
                    key={item.status}
                    variant={workflowStatusAppearance(item.status).badge}
                  >
                    {item.status ? t(`status.${item.status}`) : ""} ·{" "}
                    {item.count}
                  </Badge>
                ))}
            </span>
          ) : null}
        </button>
      </Card>
    );
    const surface = nodeContent ? (
      <Dialog
        modal={false}
        key={node.id}
        open={active && selected === node.id}
        onOpenChange={(open) => {
          if (!open) onDismiss?.();
        }}
      >
        <DialogTrigger asChild>{card}</DialogTrigger>
        {detailsContainer ? (
          <CanvasDetail
            container={detailsContainer}
            title={title}
            status={heading?.status}
            data-motion={active && selected === node.id ? "popup" : "none"}
            onFocusOutside={(event) => event.preventDefault()}
            onInteractOutside={(event) => {
              // 导航和局部重试保留浮层，避免关闭回调清空目标路由或未提交输入。
              if (
                event.target instanceof Element &&
                event.target.closest("a[href], [data-workflow-query-feedback]")
              )
                event.preventDefault();
            }}
          >
            {nodeContent(node)}
          </CanvasDetail>
        ) : null}
      </Dialog>
    ) : (
      <div key={node.id}>{card}</div>
    );
    return (
      <div key={node.id} className="relative">
        {surface}
      </div>
    );
  };
  const phaseHeader = (phase: WorkflowPhase, index: number) => (
    <>
      <span className="text-muted-foreground type-caption mb-1 block">
        {t("phaseOrdinal", { ordinal: String(index + 1).padStart(2, "0") })}
      </span>
      <span className="type-label block font-semibold break-words">
        {phase.title}
      </span>
      <span className="text-muted-foreground type-caption mt-1 line-clamp-2 block break-words whitespace-pre-wrap">
        {phase.detail}
      </span>
    </>
  );
  const connector = (path = "M50 0V24", kind = "sequence", stretch = false) => {
    const line = (
      <svg
        aria-hidden="true"
        data-workflow-connector={kind}
        viewBox="0 0 100 24"
        preserveAspectRatio="none"
        className={cn(
          "text-border-default pointer-events-none block w-full shrink-0",
          stretch ? "absolute inset-0 h-full" : "h-4",
        )}
      >
        <path
          d={path}
          className="workflow-connection-base"
          fill="none"
          stroke="currentColor"
          strokeWidth={1.5}
          vectorEffect="non-scaling-stroke"
        />
        <path
          d={path}
          fill="none"
          className="workflow-connection-flow stroke-accent"
          strokeWidth={1.5}
          vectorEffect="non-scaling-stroke"
        />
      </svg>
    );
    return stretch ? (
      <div className="relative min-h-0 flex-1">{line}</div>
    ) : (
      line
    );
  };
  const renderItems = (items: CanvasItem[]): ReactNode => (
    <div className="flex min-w-0 flex-col">
      {items.map((item, index) => (
        <div key={item.id} className="min-w-0">
          {index > 0 ? connector() : null}
          {item.kind === "node" ? renderNode(item.node) : renderGroup(item)}
        </div>
      ))}
    </div>
  );
  const renderGroup = (
    item: Exclude<CanvasItem, { kind: "node" }>,
  ): ReactNode => {
    const sideBySide = item.kind === "decision" || item.kind === "parallel";
    if (!sideBySide || item.branches.length === 1)
      return renderItems(item.branches.flatMap((branch) => branch.items));
    const branchWidths = item.branches.map(
      (branch) => branch.columns * 248 + (branch.columns - 1) * 24,
    );
    const totalWidth =
      branchWidths.reduce((total, width) => total + width, 0) +
      (branchWidths.length - 1) * 24;
    let offset = 0;
    const centers = branchWidths.map((width) => {
      const center = ((offset + width / 2) / totalWidth) * 100;
      offset += width + 24;
      return center;
    });
    const split = centers.map((center) => `M50 0V12H${center}V24`).join(" ");
    const join = centers.map((center) => `M${center} 0V12H50V24`).join(" ");
    return (
      <div
        data-workflow-group={item.id}
        data-flow-kind={item.kind}
        className="min-w-0"
      >
        {connector(split, "split")}
        <div
          data-workflow-branches="parallel"
          className="grid min-w-0 gap-6"
          style={{
            gridTemplateColumns: branchWidths
              .map((width) => `${width}fr`)
              .join(" "),
          }}
        >
          {item.branches.map((branch) => (
            <div
              key={branch.id}
              data-workflow-branch={branch.label}
              className="flex min-w-0 flex-col"
            >
              {renderItems(branch.items)}
              {connector("M50 0V24", "branch-tail", true)}
            </div>
          ))}
        </div>
        {connector(join, "join")}
      </div>
    );
  };
  const content = phaseLayouts.map(
    ({ phase, left, top, width: phaseWidth, items }, index) => {
      const card = (
        <section
          key={phase.id}
          data-workflow-phase={phase.id}
          data-workflow-layout-item
          className={cn(
            "bg-surface-raised relative grid min-w-0 content-start gap-2 rounded-xl p-3",
            selectedTargets.some(
              (target) => target.kind === "phase" && target.id === phase.id,
            ) && "ring-accent-border ring-1",
            adjustingPhase === phase.id && "ring-accent-border ring-1",
          )}
          style={
            listMode
              ? undefined
              : {
                  position: "absolute",
                  left,
                  top,
                  width: phaseWidth,
                }
          }
        >
          {phaseEditor ? (
            <DialogTrigger asChild>
              <button
                type="button"
                aria-label={phase.title}
                className="focus-visible:ring-ring rounded-control mb-1 min-w-0 text-left outline-none focus-visible:ring-2"
              >
                {phaseHeader(phase, index)}
              </button>
            </DialogTrigger>
          ) : (
            <div className="mb-1 min-w-0">{phaseHeader(phase, index)}</div>
          )}
          {renderItems(items)}
        </section>
      );
      return phaseEditor ? (
        <Dialog
          modal={false}
          key={phase.id}
          open={active && adjustingPhase === phase.id}
          onOpenChange={(open) =>
            setAdjustingPhase(open ? phase.id : undefined)
          }
        >
          {card}
          {detailsContainer ? (
            <CanvasDetail
              container={detailsContainer}
              title={phase.title || t("phaseAdjustment")}
              aria-label={t("phaseAdjustment")}
              data-motion={
                active && adjustingPhase === phase.id ? "popup" : "none"
              }
            >
              {phaseEditor(phase, () => setAdjustingPhase(undefined))}
            </CanvasDetail>
          ) : null}
        </Dialog>
      ) : (
        card
      );
    },
  );
  const detailsRegion = (
    <div
      ref={setDetailsContainer}
      data-workflow-details-region
      className={cn(
        "pointer-events-none absolute inset-x-0 top-0 bottom-0",
        hasDock && "bottom-(--shell-detail-dock-clearance)",
      )}
    />
  );
  if (listMode)
    return (
      <div
        ref={host}
        data-workflow-motion={active && visible}
        className="relative min-h-0 min-w-0 flex-1"
      >
        <div className="grid h-full gap-8 overflow-auto p-6">
          {input}
          {content}
        </div>
        {detailsRegion}
      </div>
    );
  return (
    <div
      className={cn(
        "bg-surface-canvas surface-pattern-dots surface-pattern-dots-canvas group/workflow-canvas relative min-h-0 min-w-0 flex-1 overflow-clip",
        selectionRect
          ? "cursor-crosshair"
          : "cursor-grab active:cursor-grabbing",
      )}
      style={{
        backgroundPosition: `${view.x}px ${view.y}px`,
        backgroundSize: `${20 * view.scale}px ${20 * view.scale}px`,
      }}
      ref={host}
      data-workflow-motion={active && visible}
      tabIndex={0}
      aria-label={t("graph")}
      aria-describedby={interactionHintId}
      onPointerDown={down}
      onKeyUp={(event) => {
        if (event.code === "Space") spacePan.current = false;
      }}
      onPointerMove={(event) => {
        if (selectionOrigin.current) {
          const box = event.currentTarget.getBoundingClientRect();
          const x = event.clientX - box.left,
            y = event.clientY - box.top;
          const origin = selectionOrigin.current;
          setSelectionRect({
            x: Math.min(x, origin.x),
            y: Math.min(y, origin.y),
            width: Math.abs(x - origin.x),
            height: Math.abs(y - origin.y),
          });
          return;
        }
        const drag = dragging.current;
        if (drag)
          setView((previous) => ({
            ...previous,
            x: drag.originX + event.clientX - drag.x,
            y: drag.originY + event.clientY - drag.y,
          }));
      }}
      onPointerUp={() => {
        finishSelection();
        dragging.current = undefined;
      }}
      onPointerCancel={() => {
        selectionOrigin.current = undefined;
        setSelectionRect(undefined);
        dragging.current = undefined;
      }}
      onKeyDown={(event) => {
        if (event.target !== event.currentTarget) return;
        if (
          onSelectTargets &&
          (event.ctrlKey || event.metaKey) &&
          event.key.toLowerCase() === "a"
        ) {
          onSelectTargets(
            manifest.phases.map((phase) => ({ kind: "phase", id: phase.id })),
          );
          event.preventDefault();
          return;
        }
        if (onSelectTargets && event.key === "Escape") {
          selectionOrigin.current = undefined;
          setSelectionRect(undefined);
          onSelectTargets([]);
          event.preventDefault();
          return;
        }
        if (event.code === "Space") {
          spacePan.current = true;
          event.preventDefault();
          return;
        }
        if (event.key === "+" || event.key === "=") {
          zoom(0.1);
          event.preventDefault();
        } else if (event.key === "-") {
          zoom(-0.1);
          event.preventDefault();
        } else if (event.key === "0") {
          fit();
          event.preventDefault();
        } else if (
          ["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(
            event.key,
          )
        ) {
          const x =
            event.key === "ArrowLeft"
              ? 32
              : event.key === "ArrowRight"
                ? -32
                : 0;
          const y =
            event.key === "ArrowUp" ? 32 : event.key === "ArrowDown" ? -32 : 0;
          setView((previous) => ({
            ...previous,
            x: previous.x + x,
            y: previous.y + y,
          }));
          userMoved.current = true;
          event.preventDefault();
        }
      }}
      onBlur={() => {
        spacePan.current = false;
        dragging.current = undefined;
      }}
    >
      <div
        ref={contentHost}
        style={{
          transform: `translate(${view.x}px,${view.y}px) scale(${view.scale})`,
          transformOrigin: "0 0",
          width,
          height,
        }}
        className="relative"
      >
        <svg
          className="text-border-default pointer-events-none absolute inset-0"
          width={width}
          height={height}
          aria-hidden="true"
        >
          {inputPath ? (
            <g data-workflow-connector="input">
              <path
                d={inputPath}
                className="workflow-connection-base"
                stroke="currentColor"
                strokeWidth="1.5"
                fill="none"
              />
              <path
                d={inputPath}
                className="workflow-connection-flow stroke-accent"
                strokeWidth="1.5"
                strokeLinecap="round"
                fill="none"
              />
            </g>
          ) : null}
          {connections.map(({ from, to, path }) => (
            <g key={`${from}:${to}`}>
              <path
                d={path}
                className="workflow-connection-base"
                stroke="currentColor"
                strokeWidth="1.5"
                fill="none"
              />
              <path
                d={path}
                className="workflow-connection-flow stroke-accent"
                strokeWidth="1.5"
                strokeLinecap="round"
                fill="none"
              />
            </g>
          ))}
        </svg>
        {input}
        {content}
      </div>
      {selectionRect ? (
        <div
          aria-hidden="true"
          className="border-accent-border bg-accent-surface pointer-events-none absolute border"
          style={{
            left: selectionRect.x,
            top: selectionRect.y,
            width: selectionRect.width,
            height: selectionRect.height,
          }}
        />
      ) : null}
      {detailsRegion}
      <CanvasViewportControls
        scale={view.scale}
        onZoomOut={() => zoom(-0.1)}
        onZoomIn={() => zoom(0.1)}
        onFit={() => fit()}
        labels={{ zoomIn: t("zoomIn"), zoomOut: t("zoomOut"), fit: t("fit") }}
      />
      <div className="h-control-sm pointer-events-none absolute right-4 bottom-6 left-24 z-10 flex items-center">
        <span
          id={interactionHintId}
          className="text-muted-foreground type-caption group-focus-visible/workflow-canvas:text-content-primary group-focus-visible/workflow-canvas:font-medium"
        >
          {t(onSelectTargets ? "canvasSelectionHint" : "canvasPanHint")}
        </span>
      </div>
    </div>
  );
}
