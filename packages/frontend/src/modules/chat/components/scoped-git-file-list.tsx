import { Plus, RotateCcw } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import type { ScopedGitStatus } from "@/shared/platform";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { WorkspaceFileTypeIcon } from "@/shared/workspace/workspace-file-type-icon";

type GitEntry = ScopedGitStatus["entries"][number];
export type GitSelection = { path: string; staged: boolean };

export function ScopedGitFileList({
  title,
  entries,
  staged,
  activePath,
  selection,
  onSelect,
  busy,
  onAction,
  onDiscard,
}: {
  title: string;
  entries: GitEntry[];
  staged: boolean;
  activePath?: string;
  selection?: GitSelection;
  onSelect: (selection: GitSelection) => void;
  busy: boolean;
  onAction: (path: string) => void;
  onDiscard?: (path: string) => void;
}) {
  const { t } = useTranslation("chat");
  const [limit, setLimit] = useState(100);
  return (
    <section aria-label={title} className="mb-3">
      <h3 className="text-content-secondary type-caption min-h-row-compact mb-1 flex items-center gap-2 px-2 font-medium">
        {title}
        <span className={staged ? "text-success" : "text-warning"}>
          {entries.length}
        </span>
      </h3>
      <div>
        {entries.slice(0, limit).map((entry) => (
          <div
            key={entry.path}
            className={cn(
              "group/file hover:bg-muted min-h-row-default rounded-control relative flex w-full items-center gap-1",
              (selection?.path === entry.path && selection.staged === staged) ||
                activePath === entry.path
                ? "bg-selection-surface"
                : undefined,
            )}
          >
            <button
              type="button"
              aria-current={
                selection?.path === entry.path && selection.staged === staged
                  ? "true"
                  : undefined
              }
              className="focus-visible:ring-ring rounded-control absolute inset-0 flex min-w-0 items-center gap-1.5 px-2 text-left outline-none focus-visible:ring-2"
              onClick={() => onSelect({ path: entry.path, staged })}
            >
              <WorkspaceFileTypeIcon
                isDirectory={false}
                name={entry.path}
                size={16}
              />
              <span
                className="type-ui min-w-0 flex-1 truncate"
                title={entry.path}
              >
                {entry.path}
              </span>
              <span
                className={cn(
                  "type-caption flex shrink-0 items-center justify-end gap-1.5 transition-opacity group-focus-within/file:opacity-0 group-hover/file:opacity-0",
                  onDiscard && entry.xy !== "??" ? "min-w-16" : "min-w-8",
                )}
              >
                <span
                  className="text-success"
                  title={
                    !staged && entry.lineCountUnavailable
                      ? t("workspace.git.unavailableLineCount")
                      : undefined
                  }
                >
                  {!staged && entry.lineCountUnavailable
                    ? "—"
                    : `+${staged ? entry.stagedAddedLines : entry.unstagedAddedLines}`}
                </span>
                <span className="text-destructive">
                  -
                  {staged
                    ? entry.stagedDeletedLines
                    : entry.unstagedDeletedLines}
                </span>
              </span>
            </button>
            <div className="pointer-events-none absolute inset-y-0 right-2 flex items-center opacity-0 transition-opacity group-focus-within/file:pointer-events-auto group-focus-within/file:opacity-100 group-hover/file:pointer-events-auto group-hover/file:opacity-100">
              <Button
                size="icon-sm"
                variant="ghost"
                aria-label={t(
                  staged
                    ? "workspace.git.unstageFile"
                    : "workspace.git.stageFile",
                  { path: entry.path },
                )}
                title={t(
                  staged
                    ? "workspace.git.unstageFile"
                    : "workspace.git.stageFile",
                  { path: entry.path },
                )}
                className="text-content-muted hover:text-success shrink-0"
                disabled={busy}
                onClick={() => onAction(entry.path)}
              >
                {staged ? <RotateCcw /> : <Plus />}
              </Button>
              {onDiscard && entry.xy !== "??" ? (
                <Button
                  size="icon-sm"
                  variant="ghost"
                  aria-label={t("workspace.git.discardFile", {
                    path: entry.path,
                  })}
                  title={t("workspace.git.discardFile", { path: entry.path })}
                  className="text-content-muted hover:text-destructive shrink-0"
                  disabled={busy}
                  onClick={() => onDiscard(entry.path)}
                >
                  <RotateCcw />
                </Button>
              ) : null}
            </div>
          </div>
        ))}
        {entries.length > limit ? (
          <Button
            className="mt-1 w-full"
            size="sm"
            variant="ghost"
            onClick={() => setLimit((current) => current + 100)}
          >
            {t("workspace.git.showMoreFiles", {
              count: Math.min(100, entries.length - limit),
            })}
          </Button>
        ) : null}
      </div>
    </section>
  );
}
