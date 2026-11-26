import { RotateCcw, ZoomIn, ZoomOut } from "lucide-react";
import {
  useCallback,
  useContext,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
} from "react";
import { useTranslation } from "react-i18next";
import { ContentVisibility } from "@/shared/lib/content-visibility";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";

type PreviewSize = { width: number; height: number };

export function useChatPreviewFit(
  contentRef: RefObject<HTMLDivElement | null>,
  getNaturalSize: (content: HTMLDivElement) => PreviewSize | undefined,
  {
    fitHeight = false,
    contentVersion,
  }: {
    fitHeight?: boolean;
    contentVersion?: string;
  } = {},
) {
  const visible = useContext(ContentVisibility);
  const viewportRef = useRef<HTMLDivElement>(null);
  const [fittedSize, setFittedSize] = useState<PreviewSize>();
  const fit = useCallback(() => {
    const viewport = viewportRef.current;
    const content = contentRef.current;
    if (!visible || !viewport || !content) return;
    const natural = getNaturalSize(content);
    if (!natural || !(natural.width > 0 && natural.height > 0)) return;

    // 以不随滚动条变化的外尺寸作为适配基准，放大不会反向改变适配比例。
    const style = getComputedStyle(viewport);
    const width =
      viewport.offsetWidth -
      (Number.parseFloat(style.paddingLeft) || 0) -
      (Number.parseFloat(style.paddingRight) || 0);
    const height =
      viewport.offsetHeight -
      (Number.parseFloat(style.paddingTop) || 0) -
      (Number.parseFloat(style.paddingBottom) || 0);
    if (width <= 0 || (fitHeight && height <= 0)) return;
    const scale = Math.min(
      1,
      width / natural.width,
      fitHeight ? height / natural.height : Infinity,
    );
    const fitted = {
      width: natural.width * scale,
      height: natural.height * scale,
    };
    setFittedSize((current) =>
      current?.width === fitted.width && current.height === fitted.height
        ? current
        : fitted,
    );
  }, [contentRef, fitHeight, getNaturalSize, visible]);

  useLayoutEffect(() => {
    const viewport = viewportRef.current;
    if (!visible || !viewport) return;
    fit();
    if (typeof ResizeObserver === "undefined") {
      window.addEventListener("resize", fit);
      return () => window.removeEventListener("resize", fit);
    }
    const observer = new ResizeObserver(fit);
    observer.observe(viewport, { box: "border-box" });
    return () => observer.disconnect();
  }, [contentVersion, fit, visible]);

  return { fittedSize, fit, viewportRef };
}

const minimumZoom = 0.5;
const defaultMaximumZoom = 4;
const zoomStep = 0.25;

export function ChatPreviewViewport({
  children,
  fittedSize,
  viewportRef,
  naturalWidth,
}: {
  children: (zoom: number) => ReactNode;
  fittedSize?: PreviewSize;
  viewportRef: RefObject<HTMLDivElement | null>;
  naturalWidth?: number;
}) {
  const { t } = useTranslation("chat");
  const [requestedZoom, setZoom] = useState(1);
  const maximumZoom = Math.max(
    defaultMaximumZoom,
    naturalWidth && fittedSize ? naturalWidth / fittedSize.width : 1,
  );
  const zoom = Math.min(requestedZoom, maximumZoom);
  const step = Math.max(zoomStep, zoom > defaultMaximumZoom ? zoom / 4 : 0);

  return (
    <div className="relative min-w-0">
      <div
        className="absolute top-4 right-4 z-10 flex items-center gap-1"
        onPointerDown={(event) => event.stopPropagation()}
      >
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          title={t("previewZoomIn")}
          aria-label={t("previewZoomIn")}
          disabled={zoom >= maximumZoom}
          onClick={() => setZoom(Math.min(maximumZoom, zoom + step))}
        >
          <ZoomIn />
        </Button>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          title={t("previewZoomOut")}
          aria-label={t("previewZoomOut")}
          disabled={zoom <= minimumZoom}
          onClick={() => setZoom(Math.max(minimumZoom, zoom - step))}
        >
          <ZoomOut />
        </Button>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          title={t("previewZoomReset")}
          aria-label={t("previewZoomReset")}
          onClick={() => setZoom(1)}
        >
          <RotateCcw />
        </Button>
      </div>
      <div
        ref={viewportRef}
        className={cn(
          "bg-muted h-[calc(100dvh-144px)] rounded-lg p-4 pt-16",
          zoom > 1 ? "overflow-auto" : "overflow-hidden",
        )}
      >
        <div className="flex min-h-full w-max min-w-full items-center justify-center">
          <div
            className="duration-quick ease-ui relative shrink-0"
            style={{
              width: fittedSize && fittedSize.width * zoom,
              height: fittedSize && fittedSize.height * zoom,
            }}
          >
            {children(zoom)}
          </div>
        </div>
      </div>
    </div>
  );
}
