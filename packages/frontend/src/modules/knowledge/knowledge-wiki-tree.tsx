import { useState, type KeyboardEvent, type ReactNode } from "react";
import {
  FolderPlus,
  FilePlus2,
  MoreVertical,
  Pencil,
  Trash2,
} from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@/shared/ui/dropdown-menu";
import { Button } from "@/shared/ui/button";
import { FileTreeRow } from "@/shared/ui/file-tree-row";
import { SearchField } from "@/shared/ui/search-field";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { WorkspaceFileTypeIcon } from "@/shared/workspace/workspace-file-type-icon";
import { IconAction } from "./knowledge-controls";
import type { WikiPageEntry, WikiTheme } from "./knowledge-wiki-queries";
import { useCopy } from "./use-copy";
import { useWikiCopy } from "./use-wiki-copy";

export function KnowledgeWikiTree({
  pages,
  themes,
  selectedPage,
  onPage,
  onFolder,
  onCreate,
  onFolderAction,
  writable,
  tab: controlledTab,
  onTabChange,
  sourceCount = 0,
  feedback,
  sourceDirectory,
  sourceQuery = "",
  onSourceQuery,
}: {
  pages: WikiPageEntry[];
  themes: WikiTheme[];
  selectedPage: string;
  onPage: (id: string) => void;
  onFolder: (id: string) => void;
  onCreate: (kind: "page" | "folder") => void;
  onFolderAction: (
    kind: "create" | "rename" | "delete",
    folder: WikiTheme,
  ) => void;
  writable: boolean;
  tab?: string;
  onTabChange?: (tab: string) => void;
  sourceCount?: number;
  feedback?: ReactNode;
  sourceDirectory?: ReactNode;
  sourceQuery?: string;
  onSourceQuery?: (query: string) => void;
}) {
  const t = useWikiCopy();
  const copy = useCopy();
  const [query, setQuery] = useState("");
  const [localTab, setLocalTab] = useState("knowledge");
  const tab = controlledTab ?? localTab;
  function setTab(next: string) {
    setLocalTab(next);
    onTabChange?.(next);
  }
  const [closed, setClosed] = useState<Set<string>>(() => new Set());
  const summary = (p: WikiPageEntry) => p.pageType === "summary";
  const index = pages.find((p) => p.pageType === "index");
  const contents = pages.filter((p) => p.pageType !== "index");
  const rows = contents.filter(
    (p) =>
      (tab === "summaries" ? summary(p) : !summary(p)) &&
      `${p.title} ${p.summary} ${p.slug}`
        .toLocaleLowerCase()
        .includes(query.trim().toLocaleLowerCase()),
  );
  const folders = themes.filter(
    (theme): theme is WikiTheme & { id: string } =>
      Boolean(theme.id) &&
      (theme.systemKey !== "unclassified" ||
        contents.some((page) => page.themeId === theme.id) ||
        themes.some((child) => child.parentId === theme.id)),
  );
  const folderIds = new Set(folders.map((f) => f.id));
  const visible = new Set<string>();
  const counts = new Map<string, number>();
  for (const page of rows) {
    let id = page.themeId;
    const seen = new Set<string>();
    while (id && !seen.has(id)) {
      seen.add(id);
      visible.add(id);
      counts.set(id, (counts.get(id) ?? 0) + 1);
      id = folders.find((f) => f.id === id)?.parentId;
    }
  }
  function toggle(id: string) {
    setClosed((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }
  function keyboard(event: KeyboardEvent<HTMLDivElement>) {
    const buttons = Array.from(
      event.currentTarget.querySelectorAll<HTMLButtonElement>(
        '[role="treeitem"]',
      ),
    );
    const index = buttons.indexOf(event.target as HTMLButtonElement);
    if (index < 0) return;
    const next =
      event.key === "ArrowDown"
        ? index + 1
        : event.key === "ArrowUp"
          ? index - 1
          : event.key === "Home"
            ? 0
            : event.key === "End"
              ? buttons.length - 1
              : -1;
    if (next >= 0) {
      event.preventDefault();
      buttons[Math.min(next, buttons.length - 1)]?.focus();
    }
  }
  function pageRow(page: WikiPageEntry, depth: number) {
    return (
      <FileTreeRow
        key={page.id}
        className="w-full"
        depth={depth}
        selected={page.id === selectedPage}
        aria-label={page.title || page.slug || ""}
        icon={
          <WorkspaceFileTypeIcon
            isDirectory={false}
            name={`${page.title}.md`}
          />
        }
        onClick={() => {
          onFolder(page.themeId ?? "");
          if (page.id) onPage(page.id);
        }}
      >
        <span className="truncate">{page.title || page.slug}</span>
      </FileTreeRow>
    );
  }
  const rendered = new Set<string>();
  function folderRow(
    folder: WikiTheme & { id: string },
    depth: number,
    ancestors: Set<string>,
  ) {
    if (
      ancestors.has(folder.id) ||
      rendered.has(folder.id) ||
      (query.trim() && !visible.has(folder.id))
    )
      return null;
    rendered.add(folder.id);
    const path = new Set([...ancestors, folder.id]);
    const expanded = query.trim() ? true : !closed.has(folder.id);
    const count = counts.get(folder.id) ?? 0;
    return (
      <div key={folder.id} role="none">
        <div className="group/wiki-folder flex min-w-0 items-center">
          <FileTreeRow
            directory
            expanded={expanded}
            className="w-full"
            depth={depth}
            aria-label={folder.name}
            onClick={() => {
              onFolder(folder.id);
              toggle(folder.id);
            }}
            onKeyDown={(event) => {
              if (
                (event.key === "ArrowRight" && !expanded) ||
                (event.key === "ArrowLeft" && expanded)
              ) {
                event.preventDefault();
                toggle(folder.id);
              }
            }}
            icon={
              <WorkspaceFileTypeIcon
                isDirectory
                isOpen={expanded}
                name={folder.name ?? ""}
              />
            }
          >
            <span className="flex min-w-0 items-center justify-between gap-2">
              <span className="truncate">{folder.name}</span>
              {count > 0 && (
                <span className="type-caption text-muted-foreground">
                  {count}
                </span>
              )}
            </span>
          </FileTreeRow>
          {writable && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t("folderActions", { name: folder.name ?? "" })}
                  className="opacity-0 group-focus-within/wiki-folder:opacity-100 group-hover/wiki-folder:opacity-100 data-[state=open]:opacity-100"
                >
                  <MoreVertical />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem
                  onSelect={() => onFolderAction("create", folder)}
                >
                  <FolderPlus />
                  {t("createSubfolder")}
                </DropdownMenuItem>
                <DropdownMenuItem
                  onSelect={() => onFolderAction("rename", folder)}
                >
                  <Pencil />
                  {t("renameFolder")}
                </DropdownMenuItem>
                <DropdownMenuItem
                  onSelect={() => onFolderAction("delete", folder)}
                >
                  <Trash2 />
                  {t("deleteFolder")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          )}
        </div>
        {expanded && (
          <div role="group">
            {folders
              .filter((f) => f.parentId === folder.id)
              .map((f) => folderRow(f, depth + 1, path))}
            {rows
              .filter((p) => p.themeId === folder.id)
              .map((p) => pageRow(p, depth + 1))}
          </div>
        )}
      </div>
    );
  }
  const roots = folders.filter(
    (f) => !f.parentId || !folderIds.has(f.parentId),
  );
  // 图可达性独立于展开状态；折叠不能把合法后代误判成孤立目录。
  const reachable = new Set<string>();
  function markReachable(id: string) {
    if (reachable.has(id)) return;
    reachable.add(id);
    folders
      .filter((f) => f.parentId === id)
      .forEach((f) => markReachable(f.id));
  }
  roots.forEach((f) => markReachable(f.id));
  const cycleRoots: typeof roots = [];
  for (const folder of folders) {
    if (!reachable.has(folder.id)) {
      cycleRoots.push(folder);
      markReachable(folder.id);
    }
  }
  const tree = [...roots, ...cycleRoots].map((f) => folderRow(f, 0, new Set()));
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <SearchField
        width="full"
        containerClassName="shrink-0"
        aria-label={tab === "raw" ? copy("sourcePageSearch") : t("search")}
        placeholder={tab === "raw" ? copy("sourcePageSearch") : t("search")}
        value={tab === "raw" ? sourceQuery : query}
        clearLabel={t("clearSearch")}
        onChange={(event) =>
          tab === "raw"
            ? onSourceQuery?.(event.target.value)
            : setQuery(event.target.value)
        }
      />

      <div className="min-h-control-md flex min-w-0 shrink-0 flex-wrap items-center justify-between gap-2">
        <SegmentedControl
          size="sm"
          value={tab}
          onValueChange={setTab}
          ariaLabel={t("directory")}
          options={[
            {
              value: "knowledge",
              label: t("knowledge", {
                count:
                  contents.filter((p) => !summary(p)).length + (index ? 1 : 0),
              }),
            },
            {
              value: "summaries",
              label: t("summaries", { count: contents.filter(summary).length }),
            },
            ...(onTabChange
              ? [
                  {
                    value: "raw",
                    label: t("originals", { count: sourceCount }),
                  },
                ]
              : []),
          ]}
        />
        {tab !== "raw" && writable && (
          <div className="flex items-center gap-1">
            <IconAction
              label={t("createFolder")}
              icon={FolderPlus}
              onClick={() => onCreate("folder")}
            />
            <IconAction
              label={t("createPage")}
              icon={FilePlus2}
              onClick={() => onCreate("page")}
            />
          </div>
        )}
      </div>
      {tab === "raw"
        ? sourceDirectory
        : (feedback ?? (
            <div
              className="scrollbar-hidden min-h-0 flex-1 overflow-y-auto"
              role="tree"
              aria-label={t("directory")}
              onKeyDown={keyboard}
            >
              {tab === "knowledge" &&
                (!query.trim() || t("index").includes(query.trim())) && (
                  <FileTreeRow
                    className="w-full"
                    aria-label={t("index")}
                    selected={!selectedPage || selectedPage === index?.id}
                    icon={
                      <WorkspaceFileTypeIcon
                        isDirectory={false}
                        name="index.md"
                      />
                    }
                    onClick={() => {
                      onFolder("");
                      onPage(index?.id ?? "");
                    }}
                  >
                    <span className="truncate">{t("index")}</span>
                  </FileTreeRow>
                )}
              {tab === "summaries" ? (
                rows.map((p) => pageRow(p, 0))
              ) : (
                <>
                  {tree}

                  {rows
                    .filter((p) => !p.themeId || !folderIds.has(p.themeId))
                    .map((p) => pageRow(p, 0))}
                </>
              )}
              {!rows.length && query.trim() && (
                <p className="type-body text-muted-foreground p-3">
                  {t("noMatch")}
                </p>
              )}
            </div>
          ))}
    </div>
  );
}
