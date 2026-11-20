import type { ReactNode } from "react";

/** 委派和工作流共用居中的过程行及交互入口留白。 */
export function ChatTimelineMarker({ children }: { children: ReactNode }) {
  return (
    <div className="text-content-secondary type-ui my-3 min-w-0 px-6">
      <div className="flex min-w-0 flex-wrap items-center justify-center gap-1">
        {children}
      </div>
    </div>
  );
}
