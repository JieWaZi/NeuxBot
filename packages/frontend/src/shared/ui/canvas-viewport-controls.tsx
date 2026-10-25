import { Maximize, Minus, Plus } from "lucide-react";
import { Button } from "./button";

/** 图形画布共用缩放比例与视图操作，拖动始终由所属画布默认提供。 */
export function CanvasViewportControls({
  scale,
  onZoomIn,
  onZoomOut,
  onFit,
  labels,
}: {
  scale: number;
  onZoomIn: () => void;
  onZoomOut: () => void;
  onFit: () => void;
  labels: { zoomIn: string; zoomOut: string; fit: string };
}) {
  return (
    <div
      data-canvas-viewport-controls
      className="bg-surface-raised rounded-control absolute bottom-4 left-4 z-10 flex flex-col items-center gap-1 p-2"
      onPointerDown={(event) => event.stopPropagation()}
    >
      <Button
        size="icon-sm"
        variant="ghost"
        aria-label={labels.zoomOut}
        onClick={onZoomOut}
      >
        <Minus />
      </Button>
      <span className="type-caption min-w-10 text-center tabular-nums">
        {Math.round(scale * 100)}%
      </span>
      <Button
        size="icon-sm"
        variant="ghost"
        aria-label={labels.zoomIn}
        onClick={onZoomIn}
      >
        <Plus />
      </Button>
      <Button
        size="icon-sm"
        variant="ghost"
        aria-label={labels.fit}
        onClick={onFit}
      >
        <Maximize />
      </Button>
    </div>
  );
}
