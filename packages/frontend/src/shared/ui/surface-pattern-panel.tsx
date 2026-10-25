import type { ComponentProps, ReactNode } from "react";

import { cn } from "@/shared/lib/cn";

export type SurfacePattern =
  | "dots"
  | "grid"
  | "wash"
  | "wash-header"
  | "wash-ridge"
  | "wash-mirror"
  | "none";

type SurfacePatternPanelProps = ComponentProps<"div"> & {
  children: ReactNode;
  /** scene = 白底分组；card = 灰底内容块（落在白底上） */
  surface?: "scene" | "card";
  pattern?: SurfacePattern;
  /** 白底上提高纹理可见度（如顶栏 wash） */
  patternEmphasis?: boolean;
};

function patternLayerClass(
  pattern: Exclude<SurfacePattern, "none">,
  patternEmphasis: boolean,
) {
  if (pattern === "wash-mirror") {
    return "surface-pattern-wash-mirror";
  }
  if (pattern === "wash-header") {
    return patternEmphasis
      ? "surface-pattern-wash-header surface-pattern-wash-header-strong"
      : "surface-pattern-wash-header";
  }
  if (pattern === "wash-ridge") {
    return patternEmphasis
      ? "surface-pattern-wash-ridge surface-pattern-wash-ridge-strong"
      : "surface-pattern-wash-ridge";
  }
  if (pattern === "wash") {
    return patternEmphasis
      ? "surface-pattern-wash surface-pattern-wash-strong"
      : "surface-pattern-wash";
  }
  if (pattern === "grid") {
    return patternEmphasis
      ? "surface-pattern-grid surface-pattern-grid-strong"
      : "surface-pattern-grid";
  }
  return patternEmphasis
    ? "surface-pattern-dots surface-pattern-dots-strong"
    : "surface-pattern-dots";
}

function patternLayerOpacity(
  pattern: Exclude<SurfacePattern, "none">,
  patternEmphasis: boolean,
  isScene: boolean,
) {
  if (
    pattern === "wash-header" ||
    pattern === "wash-ridge" ||
    pattern === "wash-mirror"
  ) {
    return patternEmphasis
      ? "opacity-100"
      : isScene
        ? "opacity-90"
        : "opacity-80";
  }
  if (pattern === "wash") {
    return patternEmphasis
      ? "opacity-100"
      : isScene
        ? "opacity-90"
        : "opacity-80";
  }
  if (isScene) {
    return patternEmphasis ? "opacity-70" : "opacity-40";
  }
  return "opacity-35";
}

export function SurfacePatternPanel({
  children,
  className,
  pattern = "dots",
  patternEmphasis = false,
  surface = "card",
  ...props
}: SurfacePatternPanelProps) {
  const isScene = surface === "scene";

  return (
    <div
      className={cn(
        "relative overflow-hidden rounded-xl",
        isScene ? "bg-surface-scene" : "bg-card",
        className,
      )}
      {...props}
    >
      {pattern !== "none" ? (
        <div
          aria-hidden
          className={cn(
            "pointer-events-none absolute inset-0",
            patternLayerClass(pattern, patternEmphasis),
            patternLayerOpacity(pattern, patternEmphasis, isScene),
          )}
        />
      ) : null}
      <div className="relative min-w-0">{children}</div>
    </div>
  );
}
