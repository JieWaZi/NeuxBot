import { ChevronDown, ChevronRight, Plus, RotateCcw } from "lucide-react";
import { memo, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import type { WorkspaceGitComparison } from "@/shared/platform";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { FileTreeRow } from "@/shared/ui/file-tree-row";
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
  format = "plain",
}: {
  added: number;
  deleted: number;
  unavailable?: boolean;
  format?: "plain" | "compact" | "grouped";
}) {
  const { t, i18n } = useTranslation("chat");
  const formatter =
    format === "plain"
      ? undefined
      : new Intl.NumberFormat(
          i18n.resolvedLanguage,
          format === "compact"
            ? { notation: "compact", maximumFractionDigits: 1 }
            : undefined,
        );
  return (
    <span
      className="type-caption flex shrink-0 items-center gap-1.5 tabular-nums"
      title={
        format === "compact" && (added !== 0 || deleted !== 0)
          ? `+${added} / −${deleted}`
          : undefined
      }
    >
      {!unavailable || added !== 0 || deleted !== 0 ? (
        <>
          <span className="text-success">
            +{formatter?.format(added) ?? added}
          </span>
          {format === "grouped" ? (
            <span aria-hidden="true" className="text-content-muted">
              /
            </span>
          ) : null}
          <span className="text-destructive">
            −{formatter?.format(deleted) ?? deleted}
          </span>
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
        return (
          <section
            key={key}
            data-git-file={key}
            className="border-border-subtle border-b [contain-intrinsic-block-size:auto_var(--spacing-row-default)] [content-visibility:auto]"
          >
            <ScopedGitFileHeader
              entry={entry}
              fileKey={key}
              groupId={group.id}
              open={open}
              focused={focused === key}
              busy={busy}
              onToggle={onToggle}
              onOpenFile={onOpenFile}
              onStage={onStage}
              onDiscard={onDiscard}
            />
            {open ? renderPatch(entry) : null}
          </section>
        );
      })}
    </section>
  );
}

// 展开或选中单个文件时，其余文件标题继续使用原来的渲染结果。
const ScopedGitFileHeader = memo(function ScopedGitFileHeader({
  entry,
  fileKey,
  groupId,
  open,
  focused,
  busy,
  onToggle,
  onOpenFile,
  onStage,
  onDiscard,
}: {
  entry: DiffEntry;
  fileKey: string;
  groupId: string;
  open: boolean;
  focused: boolean;
  busy: boolean;
  onToggle: (key: string) => void;
  onOpenFile?: (path: string) => void;
  onStage?: (path: string) => void;
  onDiscard?: (path: string) => void;
}) {
  const { t } = useTranslation("chat");
  const toggleLabel = t(
    open ? "workspace.git.collapseFile" : "workspace.git.expandFile",
    { path: entry.path },
  );
  const stageLabel = t(
    groupId === ":staged"
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
  const label = entry.oldPath ? entry.oldPath + " → " + entry.path : entry.path;
  return (
    <div
      className={cn(
        "min-h-row-default group/file flex min-w-0 items-center gap-1 px-2",
        focused && "bg-selection-surface",
      )}
    >
      <Button
        size="icon-sm"
        variant="ghost"
        aria-label={toggleLabel}
        title={toggleLabel}
        aria-expanded={open}
        data-diff-toggle
        onClick={() => onToggle(fileKey)}
      >
        {open ? <ChevronDown /> : <ChevronRight />}
      </Button>
      <WorkspaceFileTypeIcon isDirectory={false} name={entry.path} size={16} />
      <button
        type="button"
        className="focus-visible:ring-ring rounded-control type-ui min-h-row-compact min-w-0 flex-1 truncate px-1 text-left outline-none hover:underline focus-visible:ring-2"
        aria-label={entry.path}
        title={label}
        onClick={() => {
          if (onOpenFile) onOpenFile(entry.path);
          else onToggle(fileKey);
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
          {groupId === ":staged" ? <RotateCcw /> : <Plus />}
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
  );
});

type ChangedDirectory = {
  folders: Map<string, ChangedDirectory>;
  files: { entry: DiffEntry; name: string; key: string }[];
};

const diffTreeStatusColors = new Map([
  ["A", "text-success"],
  ["C", "text-success"],
  ["D", "text-destructive"],
  ["M", "text-status-info-content"],
  ["R", "text-status-info-content"],
  ["T", "text-status-info-content"],
  ["U", "text-warning"],
]);

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
      entry,
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
  const { t } = useTranslation("chat");

  function renderEntries(directory: ChangedDirectory, depth: number) {
    return (
      <>
        {[...directory.folders].map(([name, child]) => (
          <ChangedTreeFolder key={name} name={name} depth={depth}>
            {renderEntries(child, depth + 1)}
          </ChangedTreeFolder>
        ))}
        {directory.files.map(({ entry, name, key }) => {
          const statusLabel = t("workspace.git.diffStatus." + entry.status, {
            defaultValue: entry.status,
          });
          return (
            <FileTreeRow
              key={key}
              density="compact"
              depth={depth}
              selected={focused === key}
              className="w-full"
              aria-current={focused === key ? "true" : undefined}
              aria-description={statusLabel}
              title={entry.path}
              icon={
                <WorkspaceFileTypeIcon
                  isDirectory={false}
                  name={name}
                  size={16}
                />
              }
              onClick={() => onJump(key)}
            >
              <span
                className={cn(
                  "flex min-w-0 items-center gap-2",
                  diffTreeStatusColors.get(entry.status) ??
                    "text-content-muted",
                )}
              >
                <span className="min-w-0 flex-1 truncate">{name}</span>
                <span
                  aria-hidden="true"
                  title={statusLabel}
                  className="type-caption w-4 shrink-0 text-center"
                >
                  {entry.status}
                </span>
              </span>
            </FileTreeRow>
          );
        })}
      </>
    );
  }

  return groups.map((group) => (
    <section key={group.id}>
      {group.title ? (
        <h3 className="type-caption text-content-muted px-2 py-1">
          {group.title}
        </h3>
      ) : null}
      <div role="tree" aria-label={group.title} className="grid">
        {renderEntries(buildChangedTree(group), 0)}
      </div>
    </section>
  ));
}

function ChangedTreeFolder({
  name,
  depth,
  children,
}: {
  name: string;
  depth: number;
  children: ReactNode;
}) {
  const [expanded, setExpanded] = useState(true);
  return (
    <div role="treeitem" aria-label={name} aria-expanded={expanded}>
      <FileTreeRow
        role="button"
        density="compact"
        directory
        expanded={expanded}
        depth={depth}
        className="w-full"
        title={name}
        onClick={() => setExpanded((open) => !open)}
      >
        <span className="flex min-w-0 items-center gap-2">
          <span className="min-w-0 flex-1 truncate">{name}</span>
          <span
            aria-hidden="true"
            className="grid w-4 shrink-0 place-items-center"
          >
            <span className="bg-status-info-content size-1.5 rounded-full opacity-55" />
          </span>
        </span>
      </FileTreeRow>
      <div role="group" hidden={!expanded}>
        {children}
      </div>
    </div>
  );
}
