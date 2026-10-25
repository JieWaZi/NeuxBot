import { Eye, EyeOff, RefreshCw, Undo2, Network } from "lucide-react";
import { Button } from "@/shared/ui/button";
import { Separator } from "@/shared/ui/separator";
import { cn } from "@/shared/lib/cn";
import type { KnowledgeGraph } from "./knowledge-graph-model";
import { graphNodeColor, graphPageType, useGraphCopy } from "./use-graph-copy";

/** 沿用 WeKnora 右上图例的类型开关、视图操作及覆盖说明。 */
export function KnowledgeGraphLegend({
  graph,
  types,
  hiddenTypes,
  onToggleType,
  showArrows,
  onToggleArrows,
  refresh,
  fetching,
  overview,
  remote,
  frontier,
  grow,
  busy,
}: {
  graph: KnowledgeGraph;
  types: string[];
  hiddenTypes: ReadonlySet<string>;
  onToggleType: (type: string) => void;
  showArrows: boolean;
  onToggleArrows: () => void;
  refresh: () => void;
  fetching: boolean;
  overview: () => void;
  remote: boolean;
  frontier: number;
  grow: () => void;
  busy: boolean;
}) {
  const t = useGraphCopy();
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col">
        {types.map((type) => (
          <Button
            key={type}
            variant="ghost"
            size="sm"
            aria-pressed={!hiddenTypes.has(type)}
            className={cn(
              "justify-start",
              hiddenTypes.has(type) && "opacity-40",
            )}
            onClick={() => onToggleType(type)}
          >
            <span
              className={cn(
                "size-2 shrink-0 rounded-full bg-current",
                graphNodeColor(type),
              )}
            />
            <span
              className={hiddenTypes.has(type) ? "line-through" : undefined}
            >
              {t(graphPageType(type))}
            </span>
          </Button>
        ))}
      </div>
      <Separator />
      <div className="flex flex-col">
        <Button
          variant="ghost"
          size="sm"
          className="justify-start"
          onClick={onToggleArrows}
        >
          {showArrows ? <EyeOff /> : <Eye />}
          {t(showArrows ? "hideArrows" : "showArrows")}
        </Button>
        {frontier > 0 && (
          <Button
            variant="ghost"
            size="sm"
            className="justify-start"
            onClick={grow}
            disabled={busy}
          >
            <Network />
            {t("grow", { count: frontier })}
          </Button>
        )}
        {graph.meta.mode === "ego" && (
          <Button
            variant="ghost"
            size="sm"
            className="justify-start"
            onClick={overview}
          >
            <Undo2 />
            {t("overview")}
          </Button>
        )}
      </div>
      <Separator />
      <div className="max-w-44 space-y-1 px-2">
        <div className="text-content-muted type-caption flex items-center justify-between gap-2">
          <span>
            {t(graph.meta.mode === "ego" ? "neighborhood" : "overviewTitle")}
          </span>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t("refresh")}
            disabled={fetching}
            onClick={refresh}
          >
            <RefreshCw />
          </Button>
        </div>
        <p className="type-body">
          {t("nodeCount", {
            returned: graph.nodes.length,
            total: graph.meta.total,
          })}
        </p>
        <p className="type-caption text-content-muted">
          {t(
            graph.meta.truncated
              ? remote
                ? "remotePartial"
                : "partial"
              : graph.nodes.length === graph.meta.total
                ? "allNodes"
                : "neighborhood",
            { returned: graph.nodes.length, total: graph.meta.total },
          )}
        </p>
      </div>
    </div>
  );
}
