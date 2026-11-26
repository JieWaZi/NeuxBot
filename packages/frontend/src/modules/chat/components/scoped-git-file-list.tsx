import {
  ChevronDown,
  ChevronRight,
  Folder,
  Plus,
  RotateCcw,
} from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import type { WorkspaceGitComparison } from "@/shared/platform";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { WorkspaceFileTypeIcon } from "@/shared/workspace/workspace-file-type-icon";

export type DiffEntry = WorkspaceGitComparison["entries"][number];
export type DiffGroup = {
  id: string;
  title?: string;
  files: DiffEntry[];
};

export function diffFileKey(group: string, path: string) {
  return JSON.stringify([group, path]);
}

export function DiffLineCounts({
  added,
  deleted,
  unavailable,
}: {
  added: number;
  deleted: number;
  unavailable?: boolean;
}) {
  const { t } = useTranslation("chat");
  return (
    <span className="type-caption flex shrink-0 items-center gap-1.5 tabular-nums">
      {!unavailable || added !== 0 || deleted !== 0 ? (
        <>
          <span className="text-success">+{added}</span>
          <span className="text-destructive">−{deleted}</span>
        </>
      ) : null}
      {unavailable ? (
        <span
          className="text-content-muted"
          aria-label={t("workspace.git.unavailableLineCount")}
          title={t("workspace.git.unavailableLineCount")}
        >
          {added === 0 && deleted === 0 ? "—" : "···"}
        </span>
      ) : null}
    </span>
  );
}

export function ScopedGitFileList({
  group,
  expanded,
  focused,
  onToggle,
  onOpenFile,
  onStage,
  onDiscard,
  busy,
  renderPatch,
}: {
  group: DiffGroup;
  expanded: (key: string) => boolean;
  focused?: string;
  onToggle: (key: string) => void;
  onOpenFile?: (path: string) => void;
  onStage?: (path: string) => void;
  onDiscard?: (path: string) => void;
  busy: boolean;
  renderPatch: (entry: DiffEntry) => ReactNode;
}) {
  const { t } = useTranslation("chat");
  return (
    <section aria-label={group.title}>
      {group.title ? (
        <h3 className="text-content-secondary type-caption min-h-row-compact flex items-center gap-2 px-3 font-medium">
          {group.title}
          <span className="text-content-muted">{group.files.length}</span>
        </h3>
      ) : null}
      {group.files.map((entry) => {
        const key = diffFileKey(group.id, entry.path);
        const open = expanded(key);
        const toggleLabel = t(
          open ? "workspace.git.collapseFile" : "workspace.git.expandFile",
          { path: entry.path },
        );
        const stageLabel = t(
          group.id === ":staged"
            ? "workspace.git.unstageFile"
            : "workspace.git.stageFile",
          { path: entry.path },
        );
        const discardLabel = t("workspace.git.discardFile", {
          path: entry.path,
        });
        const statusLabel = t("workspace.git.diffStatus." + entry.status, {
          defaultValue: entry.status,
        });
        const label = entry.oldPath
          ? entry.oldPath + " → " + entry.path
          : entry.path;
        return (
          <section
            key={key}
            data-git-file={key}
            className="border-border-subtle border-b"
          >
            <div
              className={cn(
                "min-h-row-default group/file flex min-w-0 items-center gap-1 px-2",
                focused === key && "bg-selection-surface",
              )}
            >
              <Button
                size="icon-sm"
                variant="ghost"
                aria-label={toggleLabel}
                title={toggleLabel}
                aria-expanded={open}
                data-diff-toggle
                onClick={() => onToggle(key)}
              >
                {open ? <ChevronDown /> : <ChevronRight />}
              </Button>
              <WorkspaceFileTypeIcon
                isDirectory={false}
                name={entry.path}
                size={16}
              />
              <button
                type="button"
                className="focus-visible:ring-ring rounded-control type-ui min-h-row-compact min-w-0 flex-1 truncate px-1 text-left outline-none hover:underline focus-visible:ring-2"
                aria-label={entry.path}
                title={label}
                onClick={() => {
                  if (onOpenFile) onOpenFile(entry.path);
                  else onToggle(key);
                }}
              >
                {label}
              </button>
              <span
                className={cn(
                  "type-caption shrink-0 px-1",
                  entry.status === "A"
                    ? "text-success"
                    : entry.status === "D"
                      ? "text-destructive"
                      : "text-content-muted",
                )}
                aria-label={statusLabel}
                title={statusLabel}
              >
                {entry.status}
              </span>
              <DiffLineCounts
                added={entry.addedLines}
                deleted={entry.deletedLines}
                unavailable={entry.lineCountUnavailable}
              />
              {onStage ? (
                <Button
                  size="icon-sm"
                  variant="ghost"
                  aria-label={stageLabel}
                  title={stageLabel}
                  disabled={busy}
                  onClick={() => onStage(entry.path)}
                >
                  {group.id === ":staged" ? <RotateCcw /> : <Plus />}
                </Button>
              ) : null}
              {onDiscard && entry.status !== "A" ? (
                <Button
                  size="icon-sm"
                  variant="ghost"
                  aria-label={discardLabel}
                  title={discardLabel}
                  disabled={busy}
                  onClick={() => onDiscard(entry.path)}
                >
                  <RotateCcw />
                </Button>
              ) : null}
            </div>
            {open ? renderPatch(entry) : null}
          </section>
        );
      })}
    </section>
  );
}

type ChangedDirectory = {
  folders: Map<string, ChangedDirectory>;
  files: { path: string; name: string; key: string }[];
};

function changedDirectory(): ChangedDirectory {
  return { folders: new Map(), files: [] };
}

function buildChangedTree(group: DiffGroup) {
  const root = changedDirectory();
  for (const entry of group.files) {
    const parts = entry.path.split("/");
    const name = parts.pop() ?? entry.path;
    let directory = root;
    for (const folder of parts) {
      let child = directory.folders.get(folder);
      if (!child) {
        child = changedDirectory();
        directory.folders.set(folder, child);
      }
      directory = child;
    }
    directory.files.push({
      path: entry.path,
      name,
      key: diffFileKey(group.id, entry.path),
    });
  }
  return root;
}

export function ScopedGitChangedTree({
  groups,
  focused,
  onJump,
}: {
  groups: DiffGroup[];
  focused?: string;
  onJump: (key: string) => void;
}) {
  const trees = groups.map((group) => ({
    ...group,
    tree: buildChangedTree(group),
  }));
  return trees.map(({ id, title, tree }) => (
    <section key={id} aria-label={title}>
      {title ? (
        <h3 className="type-caption text-content-muted px-2 py-1">{title}</h3>
      ) : null}
      <ChangedTreeDirectory
        directory={tree}
        focused={focused}
        onJump={onJump}
      />
    </section>
  ));
}

function ChangedTreeDirectory({
  directory,
  focused,
  onJump,
}: {
  directory: ChangedDirectory;
  focused?: string;
  onJump: (key: string) => void;
}) {
  return (
    <ul>
      {[...directory.folders].map(([folder, child]) => (
        <li key={folder}>
          <details open>
            <summary className="hover:bg-muted focus-visible:ring-ring rounded-control type-ui min-h-row-compact flex cursor-pointer list-none items-center gap-1.5 px-2 outline-none focus-visible:ring-2">
              <Folder className="text-content-muted size-icon-sm shrink-0" />
              <span className="truncate" title={folder}>
                {folder}
              </span>
            </summary>
            <div className="ml-2">
              <ChangedTreeDirectory
                directory={child}
                focused={focused}
                onJump={onJump}
              />
            </div>
          </details>
        </li>
      ))}
      {directory.files.map(({ path, name, key }) => (
        <li key={key}>
          <button
            type="button"
            aria-current={focused === key ? "true" : undefined}
            className={cn(
              "hover:bg-muted focus-visible:ring-ring rounded-control type-ui min-h-row-compact flex w-full min-w-0 items-center gap-1.5 px-2 text-left outline-none focus-visible:ring-2",
              focused === key && "bg-selection-surface",
            )}
            title={path}
            onClick={() => onJump(key)}
          >
            <WorkspaceFileTypeIcon isDirectory={false} name={name} size={16} />
            <span className="truncate">{name}</span>
          </button>
        </li>
      ))}
    </ul>
  );
}
