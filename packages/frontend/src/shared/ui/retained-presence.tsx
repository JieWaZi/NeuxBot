import { useEffect, useState, type ComponentProps } from "react";

import { cn } from "@/shared/lib/cn";

type RetainedPresenceProps = ComponentProps<"div"> & {
  present: boolean;
  exitDurationMs?: number;
};

export function RetainedPresence({
  children,
  className,
  exitDurationMs = 140,
  present,
  ...props
}: RetainedPresenceProps) {
  const [mounted, setMounted] = useState(present);
  if (present && !mounted) setMounted(true);

  useEffect(() => {
    if (present || !mounted) return;
    const reduceMotion = window.matchMedia?.(
      "(prefers-reduced-motion: reduce)",
    ).matches;
    const timer = window.setTimeout(
      () => setMounted(false),
      reduceMotion ? 0 : exitDurationMs,
    );
    return () => window.clearTimeout(timer);
  }, [exitDurationMs, mounted, present]);

  if (!mounted) return null;
  return (
    <div
      data-motion="presence"
      data-state={present ? "open" : "closed"}
      aria-hidden={!present}
      inert={!present}
      className={cn(!present && "pointer-events-none", className)}
      {...props}
    >
      {children}
    </div>
  );
}
