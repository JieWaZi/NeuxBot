import { Folder } from "lucide-react";
import type { ReactNode } from "react";

import { cn } from "@/shared/lib/cn";
import { OverflowText } from "@/shared/ui/overflow-text";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";

export const composerContextControlClassName =
  "text-muted-foreground type-caption hover:bg-action-quiet-hover hover:text-content-secondary duration-quick ease-ui inline-flex h-[18px] min-w-0 items-center justify-center gap-1 rounded-full border-0 bg-transparent px-1.5 py-0 font-normal whitespace-nowrap transition-[background-color,color] outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 disabled:cursor-default disabled:opacity-55 [&_svg]:size-icon-xs [&_svg]:shrink-0";

type ChatComposerWorkspaceStripProps = {
  branchControl?: ReactNode;
  className?: string;
  permissionControl?: ReactNode;
  workspaceControl?: ReactNode;
  workspaceDirectory?: string;
  workspaceLabel?: string;
};

/** Composer 上下文条：目录、Git 分支与权限。 */
export function ChatComposerWorkspaceStrip({
  branchControl,
  className,
  permissionControl,
  workspaceControl,
  workspaceDirectory,
  workspaceLabel,
}: ChatComposerWorkspaceStripProps) {
  const label = workspaceLabel?.trim();
  const hasContextRail = Boolean(label || workspaceControl);
  const hasNextRail = Boolean(permissionControl);
  if (!hasContextRail && !hasNextRail) return null;

  return (
    <div
      data-chat-composer-context-strip
      className={cn(
        "text-muted-foreground type-caption relative z-[1] flex min-h-5 w-full min-w-0 items-center justify-between gap-3 select-text",
        className,
      )}
    >
      {hasContextRail ? (
        <div
          data-chat-composer-context-rail
          className="flex h-[18px] min-w-0 flex-1 items-center justify-start gap-2.5 overflow-hidden"
        >
          <span className="inline-flex min-w-0 items-center gap-1">
            {workspaceControl ??
              (label ? (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span
                      data-chat-composer-workspace
                      className="inline-flex h-[18px] max-w-45 min-w-0 items-center gap-1 rounded-full px-1.5 font-medium"
                    >
                      <Folder aria-hidden className="size-icon-xs shrink-0" />
                      <OverflowText className="min-w-0">{label}</OverflowText>
                    </span>
                  </TooltipTrigger>
                  <TooltipContent side="top">
                    {workspaceDirectory?.trim() || label}
                  </TooltipContent>
                </Tooltip>
              ) : null)}
            {branchControl}
          </span>
        </div>
      ) : (
        <span className="min-w-0 flex-1" />
      )}
      {permissionControl ? (
        <div
          data-chat-composer-next-rail
          className="flex h-[18px] shrink-0 items-center justify-end gap-2.5"
        >
          {permissionControl}
        </div>
      ) : null}
    </div>
  );
}
