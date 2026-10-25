import { useEffect, useRef, useState, type ReactNode } from "react";

const initialSidebarWidth = 240;
const separatorWidth = 8;
const keyboardStep = 16;

function widthBounds(containerWidth: number) {
  if (containerWidth <= 0)
    return { minimum: initialSidebarWidth, maximum: initialSidebarWidth };
  const available = Math.max(0, containerWidth - separatorWidth);
  return {
    minimum: Math.min(160, Math.floor(available * 0.4)),
    maximum: Math.floor(available * 0.6),
  };
}

function clampWidth(width: number, containerWidth: number) {
  const { minimum, maximum } = widthBounds(containerWidth);
  return Math.min(maximum, Math.max(minimum, width));
}

export function WorkspaceDetailSplit({
  main,
  sidebar,
  sidebarOpen,
  resizeLabel,
  side = "right",
  sidebarOnly = false,
  defaultSidebarRatio,
}: {
  main: ReactNode;
  sidebar: ReactNode;
  sidebarOpen: boolean;
  resizeLabel: string;
  side?: "left" | "right";
  sidebarOnly?: boolean;
  defaultSidebarRatio?: number;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const lastPointerX = useRef<number | undefined>(undefined);
  const [containerWidth, setContainerWidth] = useState(0);
  const [resizing, setResizing] = useState(false);
  const [sidebarWidth, setSidebarWidth] = useState<number | undefined>(
    undefined,
  );
  const defaultWidth =
    containerWidth > 0
      ? defaultSidebarRatio !== undefined
        ? containerWidth * defaultSidebarRatio
        : Math.min(initialSidebarWidth, Math.floor(containerWidth * 0.45))
      : initialSidebarWidth;
  const width = clampWidth(sidebarWidth ?? defaultWidth, containerWidth);
  const { minimum, maximum } = widthBounds(containerWidth);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    const measure = () =>
      setContainerWidth(container.getBoundingClientRect().width);
    measure();
    if (typeof ResizeObserver === "undefined") {
      window.addEventListener("resize", measure);
      return () => window.removeEventListener("resize", measure);
    }
    const observer = new ResizeObserver(([entry]) => {
      if (entry) setContainerWidth(entry.contentRect.width);
    });
    observer.observe(container);
    return () => observer.disconnect();
  }, []);

  const resize = (clientX: number) => {
    const previous = lastPointerX.current;
    if (previous === undefined) return;
    lastPointerX.current = clientX;
    setSidebarWidth((current) =>
      clampWidth(
        (current ?? width) + (previous - clientX) * (side === "left" ? -1 : 1),
        containerWidth,
      ),
    );
  };

  return (
    <div
      ref={containerRef}
      data-workspace-detail-split
      className={`duration-layout ease-ui grid min-h-0 min-w-0 flex-1 ${resizing ? "transition-none" : "transition-[grid-template-columns]"}`}
      style={{
        gridTemplateColumns: sidebarOnly
          ? side === "left"
            ? "minmax(0, 1fr) 0px 0px"
            : "0px 0px minmax(0, 1fr)"
          : side === "left"
            ? sidebarOpen
              ? `${width}px ${separatorWidth}px minmax(0, 1fr)`
              : "0px 0px minmax(0, 1fr)"
            : sidebarOpen
              ? `minmax(0, 1fr) ${separatorWidth}px ${width}px`
              : "minmax(0, 1fr) 0px 0px",
      }}
    >
      <div
        className={
          side === "left"
            ? "order-3 flex min-h-0 min-w-0 overflow-hidden"
            : "order-1 flex min-h-0 min-w-0 overflow-hidden"
        }
        aria-hidden={sidebarOnly}
        inert={sidebarOnly}
      >
        {main}
      </div>
      <div
        className="order-2 min-w-0 overflow-hidden"
        aria-hidden={!sidebarOpen || sidebarOnly}
        inert={!sidebarOpen || sidebarOnly}
      >
        <div
          role="separator"
          tabIndex={0}
          aria-label={resizeLabel}
          aria-orientation="vertical"
          aria-valuemin={minimum}
          aria-valuemax={maximum}
          aria-valuenow={width}
          className="after:bg-border hover:after:bg-border-strong focus-visible:ring-ring relative z-10 h-full w-2 cursor-col-resize touch-none outline-none after:absolute after:inset-y-0 after:left-1/2 after:w-px focus-visible:ring-2"
          onPointerDown={(event) => {
            event.preventDefault();
            event.currentTarget.focus();
            event.currentTarget.setPointerCapture(event.pointerId);
            lastPointerX.current = event.clientX;
            setResizing(true);
          }}
          onPointerMove={(event) => resize(event.clientX)}
          onPointerUp={(event) => {
            resize(event.clientX);
            lastPointerX.current = undefined;
            setResizing(false);
            if (event.currentTarget.hasPointerCapture(event.pointerId))
              event.currentTarget.releasePointerCapture(event.pointerId);
          }}
          onPointerCancel={() => {
            lastPointerX.current = undefined;
            setResizing(false);
          }}
          onLostPointerCapture={() => {
            lastPointerX.current = undefined;
            setResizing(false);
          }}
          onKeyDown={(event) => {
            if (event.key === "ArrowLeft")
              setSidebarWidth((current) =>
                clampWidth(
                  (current ?? width) +
                    keyboardStep * (side === "left" ? -1 : 1),
                  containerWidth,
                ),
              );
            else if (event.key === "ArrowRight")
              setSidebarWidth((current) =>
                clampWidth(
                  (current ?? width) -
                    keyboardStep * (side === "left" ? -1 : 1),
                  containerWidth,
                ),
              );
            else if (event.key === "Home") setSidebarWidth(minimum);
            else if (event.key === "End") setSidebarWidth(maximum);
            else return;
            event.preventDefault();
          }}
        />
      </div>
      <div
        aria-hidden={!sidebarOpen}
        inert={!sidebarOpen}
        className={`duration-layout ease-ui min-h-0 min-w-0 overflow-hidden transition-opacity ${side === "left" ? "order-1" : "order-3"} ${sidebarOpen ? "opacity-100" : "opacity-0"}`}
      >
        {sidebar}
      </div>
    </div>
  );
}
