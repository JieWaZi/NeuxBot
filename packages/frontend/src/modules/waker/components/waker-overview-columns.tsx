import type { ReactNode } from "react";

import { cn } from "@/shared/lib/cn";

import { WakerPatternPanel } from "./waker-pattern-panel";

type WakerOverviewColumnsProps = {
  about: ReactNode;
  aside: ReactNode;
  className?: string;
};

export function WakerOverviewColumns({
  about,
  aside,
  className,
}: WakerOverviewColumnsProps) {
  return (
    <div
      className={cn(
        "grid items-start gap-8 @[60rem]/resource-page:grid-cols-[minmax(0,1.65fr)_minmax(20rem,1fr)] @[60rem]/resource-page:items-stretch",
        className,
      )}
    >
      <WakerPatternPanel
        className="h-full min-w-0 rounded-xl p-5"
        pattern="wash"
        surface="scene"
      >
        {about}
      </WakerPatternPanel>
      <WakerPatternPanel
        className="h-full min-w-0 rounded-xl p-5"
        pattern="wash-mirror"
        surface="scene"
      >
        <div className="grid gap-8">{aside}</div>
      </WakerPatternPanel>
    </div>
  );
}
