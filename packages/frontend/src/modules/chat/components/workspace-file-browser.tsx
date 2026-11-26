import { useWorkspaceConfirmation } from "./use-workspace-confirmation";
import {
  resolveFileReference,
  type ResolvedFileReference,
} from "@/shared/workspace/file-reference";
import { useFileReferenceContext } from "@/shared/workspace/file-reference-link";
import { queryOptions, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Download,
  FileText,
  Folder,
  MoreHorizontal,
  Pencil,
  Trash2,
} from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { workspaceFileDragType } from "../model/workspace-file-drag";
import {
  matchesWorkspacePath,
  workspacePathAtRoot,
} from "../model/project-files-scope";

import { WorkspaceFileTypeIcon } from "@/shared/workspace/workspace-file-type-icon";
import {
  hasPlatformService,
  deleteWorkspaceResource,
  deleteWorkspaceDirectory,
  importDroppedWorkspaceFiles,
  listWorkspaceDirectory,
  listWorkspaceResourceDirectory,
  openWorkspaceResourceScope,
  renameWorkspaceResource,
  renameWorkspaceDirectory,
  saveWorkspaceResourceAs,
  searchWorkspaceFiles,
  searchWorkspaceResourceFiles,
  statWorkspaceResource,
  type WorkspaceDirectoryEntry,
} from "@/shared/platform";
import { useWorkspaceQueryEvents } from "@/shared/api/workspace-events";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { FileTreeRow } from "@/shared/ui/file-tree-row";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { Input } from "@/shared/ui/input";
import { toast } from "@/shared/ui/sonner";
import { Spinner } from "@/shared/ui/spinner";

type WorkspaceFileBrowserProps = {
  active?: boolean;
  activePath?: string;
  folderReveal?: { path: string };
  isPathDirty?: (
    path: string,
    file?: ResolvedFileReference,
    rootPath?: string,
  ) => boolean;
  onOpenFile: (file: { name: string; path: string }) => void;
  onPathDeleted?: (
    path: string,
    file?: ResolvedFileReference,
    directory?: boolean,
    rootPath?: string,
  ) => void;
  onPathRenamed?: (
    oldPath: string,
    newPath: string,
    name: string,
    file?: ResolvedFileReference,
    directory?: boolean,
    rootPath?: string,
  ) => void;
  query: string;
  rootPath: string;
  scopeChatId?: string;
  scopePath: string;
};

export const workspaceFileBrowserKeys = {
  directories: (chatId: string | undefined, root: string) =>
    ["workspace-directory", chatId ?? "", root] as const,
  searches: (chatId: string | undefined, root: string) =>
    ["workspace-file-search", chatId ?? "", root] as const,
};

export function workspaceDirectoryOptions(
  chatId: string | undefined,
  rootPath: string,
  path: string,
) {
  return queryOptions({
    queryKey: [...workspaceFileBrowserKeys.directories(chatId, rootPath), path],
    queryFn: () =>
      chatId && hasPlatformService("workspace")
        ? listWorkspaceResourceDirectory(chatId, path, rootPath)
        : listWorkspaceDirectory(rootPath, path),
  });
}

// Shallow lazy tree, flat search results, and resource selection kept separate
// from preview.
export function WorkspaceFileBrowser({
  active = true,
  activePath,
  folderReveal,
  isPathDirty,
  onOpenFile,
  onPathDeleted,
  onPathRenamed,
  query,
  rootPath,
  scopeChatId,
  scopePath,
}: WorkspaceFileBrowserProps) {
  const { t } = useTranslation("chat");
  const [normalizedQuery, setNormalizedQuery] = useState(query.trim());
  useEffect(() => {
    const timeoutID = window.setTimeout(
      () => setNormalizedQuery(query.trim()),
      300,
    );
    return () => window.clearTimeout(timeoutID);
  }, [query]);
  const rootQuery = useQuery({
    ...workspaceDirectoryOptions(scopeChatId, rootPath, scopePath),
    enabled: active && Boolean(rootPath) && !normalizedQuery,
  });
  const searchQuery = useQuery({
    queryKey: [
      ...workspaceFileBrowserKeys.searches(scopeChatId, rootPath),
      scopePath,
      normalizedQuery,
    ],
    queryFn: ({ signal }) =>
      scopeChatId && hasPlatformService("workspace")
        ? searchWorkspaceResourceFiles(
            scopeChatId,
            normalizedQuery,
            200,
            scopePath,
            rootPath,
            signal,
          )
        : searchWorkspaceFiles(
            rootPath,
            normalizedQuery,
            200,
            scopePath,
            signal,
          ),
    enabled: active && Boolean(rootPath) && Boolean(normalizedQuery),
  });
  const workspaceEvents = useWorkspaceQueryEvents(
    scopeChatId,
    [rootPath],
    ["workspace-directory", "workspace-file-search"],
    active,
  );

  if (!rootPath) {
    return (
      <WorkspaceBrowserState
        icon={Folder}
        title={t("workspace.noProjectDirectory")}
      />
    );
  }
  if (rootQuery.isPending && !normalizedQuery) {
    return <WorkspaceBrowserLoading />;
  }
  if (searchQuery.isPending && normalizedQuery) {
    return <WorkspaceBrowserLoading />;
  }
  const error =
    workspaceEvents.error ??
    (normalizedQuery ? searchQuery.error : rootQuery.error);
  if (error) {
    return (
      <WorkspaceBrowserState
        icon={Folder}
        title={t("workspace.filesLoadError")}
        action={
          <Button
            size="sm"
            variant="secondary"
            onClick={() => {
              workspaceEvents.retry();
              void (normalizedQuery
                ? searchQuery.refetch()
                : rootQuery.refetch());
            }}
          >
            {t("workspace.retry")}
          </Button>
        }
      />
    );
  }
  if (normalizedQuery) {
    const results = searchQuery.data ?? [];
    if (results.length === 0) {
      return (
        <WorkspaceBrowserState
          icon={FileText}
          title={t("workspace.noMatchingProjectFiles")}
        />
      );
    }
    return (
      <div className="mt-2 grid gap-1" role="tree">
        {results.map((entry) => (
          <WorkspaceFileButton
            activePath={activePath}
            canonicalPath={entry.path}
            key={entry.relativePath}
            name={entry.name}
            path={entry.relativePath}
            secondary={entry.relativePath}
            onOpenFile={onOpenFile}
            onPathDeleted={onPathDeleted}
            onPathRenamed={onPathRenamed}
            isPathDirty={isPathDirty}
            rootPath={rootPath}
            scopeChatId={scopeChatId}
          />
        ))}
      </div>
    );
  }

  const entries = rootQuery.data ?? [];
  if (entries.length === 0) {
    return (
      <WorkspaceBrowserState
        icon={Folder}
        title={t("workspace.projectDirectoryEmpty")}
      />
    );
  }
  return (
    <div className="grid" role="tree">
      {entries.map((entry) => (
        <WorkspaceTreeEntry
          active={active}
          activePath={activePath}
          depth={0}
          entry={entry}
          folderReveal={folderReveal}
          key={entry.relativePath}
          onOpenFile={onOpenFile}
          onPathDeleted={onPathDeleted}
          onPathRenamed={onPathRenamed}
          isPathDirty={isPathDirty}
          rootPath={rootPath}
          scopeChatId={scopeChatId}
        />
      ))}
    </div>
  );
}

type WorkspaceTreeEntryProps = {
  active: boolean;
  activePath?: string;
  depth: number;
  entry: WorkspaceDirectoryEntry;
  folderReveal?: { path: string };
  onOpenFile: (file: { name: string; path: string }) => void;
  onPathDeleted?: (
    path: string,
    file?: ResolvedFileReference,
    directory?: boolean,
    rootPath?: string,
  ) => void;
  onPathRenamed?: (
    oldPath: string,
    newPath: string,
    name: string,
    file?: ResolvedFileReference,
    directory?: boolean,
    rootPath?: string,
  ) => void;
  isPathDirty?: (
    path: string,
    file?: ResolvedFileReference,
    rootPath?: string,
  ) => boolean;
  rootPath: string;
  scopeChatId?: string;
};

function WorkspaceTreeEntry(props: WorkspaceTreeEntryProps) {
  const { entry, ...row } = props;
  if (entry.isDirectory) return <WorkspaceDirectoryRow {...props} />;
  return (
    <WorkspaceFileButton
      activePath={row.activePath}
      canonicalPath={entry.canonicalPath}
      depth={row.depth}
      name={entry.name}
      path={entry.relativePath}
      onOpenFile={row.onOpenFile}
      onPathDeleted={row.onPathDeleted}
      onPathRenamed={row.onPathRenamed}
      isPathDirty={row.isPathDirty}
      rootPath={row.rootPath}
      scopeChatId={row.scopeChatId}
    />
  );
}

function WorkspaceDirectoryRow({
  active,
  activePath,
  depth,
  entry,
  folderReveal,
  onOpenFile,
  onPathDeleted,
  onPathRenamed,
  isPathDirty,
  rootPath,
  scopeChatId,
}: WorkspaceTreeEntryProps) {
  const { t } = useTranslation("chat");
  const queryClient = useQueryClient();
  const confirmation = useWorkspaceConfirmation();
  const [override, setOverride] = useState<{
    folderReveal?: { path: string };
    open: boolean;
  }>();
  const directoryPath = workspacePathAtRoot(rootPath, entry.relativePath);
  const revealAncestor = matchesWorkspacePath(
    folderReveal?.path,
    directoryPath,
    true,
  );
  const expanded =
    override && (!revealAncestor || override.folderReveal === folderReveal)
      ? override.open
      : revealAncestor || matchesWorkspacePath(activePath, directoryPath, true);
  const [dropActive, setDropActive] = useState(false);
  const [renameOpen, setRenameOpen] = useState(false);
  const [nextName, setNextName] = useState(entry.name);
  const [renaming, setRenaming] = useState(false);
  const refreshTree = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["workspace-directory"] }),
      queryClient.invalidateQueries({ queryKey: ["workspace-file-search"] }),
    ]);
  };
  const renameDirectory = async () => {
    if (!scopeChatId || renaming) return;
    if (isPathDirty?.(entry.relativePath, undefined, rootPath)) {
      toast.error(t("workspace.saveBeforeFileOperation"));
      return;
    }
    setRenaming(true);
    try {
      const nextPath = await renameWorkspaceDirectory(
        scopeChatId,
        entry.relativePath,
        nextName,
        rootPath,
      );
      onPathRenamed?.(
        entry.relativePath,
        nextPath,
        nextName,
        undefined,
        true,
        rootPath,
      );
      setRenameOpen(false);
      await refreshTree();
    } catch (cause) {
      toast.error(
        cause instanceof Error
          ? cause.message
          : t("workspace.renameDirectoryError"),
      );
    } finally {
      setRenaming(false);
    }
  };
  const deleteDirectory = async () => {
    if (!scopeChatId) return;
    if (isPathDirty?.(entry.relativePath, undefined, rootPath)) {
      toast.error(t("workspace.saveBeforeFileOperation"));
      return;
    }
    if (
      !(await confirmation.confirm({
        kind: "directory",
        path: entry.relativePath,
      }))
    )
      return;
    try {
      if (
        !(await deleteWorkspaceDirectory(
          scopeChatId,
          entry.relativePath,
          rootPath,
        ))
      )
        return;
      onPathDeleted?.(entry.relativePath, undefined, true, rootPath);
      await refreshTree();
    } catch (cause) {
      toast.error(
        cause instanceof Error
          ? cause.message
          : t("workspace.deleteDirectoryError"),
      );
    }
  };
  const childrenQuery = useQuery({
    ...workspaceDirectoryOptions(scopeChatId, rootPath, entry.relativePath),
    enabled: active && expanded,
  });
  return (
    <div role="treeitem" aria-expanded={expanded}>
      {confirmation.dialog}
      <div
        className={cn(
          "group hover:bg-muted flex items-center rounded-sm",
          dropActive && "outline-accent-default outline-2",
        )}
        onDragOver={(event) => {
          if (
            !scopeChatId ||
            !hasPlatformService("workspace") ||
            !event.dataTransfer.types.includes("Files")
          )
            return;
          event.preventDefault();
          event.stopPropagation();
          setDropActive(true);
        }}
        onDragLeave={() => setDropActive(false)}
        onDrop={(event) => {
          if (
            !scopeChatId ||
            !hasPlatformService("workspace") ||
            !event.dataTransfer.types.includes("Files")
          )
            return;
          event.preventDefault();
          event.stopPropagation();
          setDropActive(false);
          const dropped = Array.from(event.dataTransfer.files);
          void importDroppedWorkspaceFiles(
            scopeChatId,
            entry.relativePath,
            dropped,
            rootPath,
          )
            .then(async (result) => {
              if (result.failed.length)
                toast.error(
                  t("workspace.importFileErrorCount", {
                    count: result.failed.length,
                  }),
                );
              if (result.imported.length) await refreshTree();
            })
            .catch((cause: unknown) =>
              toast.error(
                cause instanceof Error
                  ? cause.message
                  : t("workspace.importFileError"),
              ),
            );
        }}
      >
        <FileTreeRow
          role="button"
          density="compact"
          directory
          expanded={expanded}
          depth={depth}
          data-revealed-directory={
            matchesWorkspacePath(folderReveal?.path, directoryPath) || undefined
          }
          onClick={() => setOverride({ folderReveal, open: !expanded })}
        >
          <span className="truncate">{entry.name}</span>
        </FileTreeRow>
        {scopeChatId && hasPlatformService("workspace") ? (
          <>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  aria-label={t("workspace.directoryActions", {
                    name: entry.name,
                  })}
                  className="shrink-0 opacity-0 group-focus-within:opacity-100 group-hover:opacity-100"
                  size="icon-sm"
                  variant="ghost"
                >
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem
                  onSelect={() => {
                    setNextName(entry.name);
                    setRenameOpen(true);
                  }}
                >
                  <Pencil />
                  {t("workspace.renameDirectory")}
                </DropdownMenuItem>
                <DropdownMenuItem
                  variant="destructive"
                  onSelect={() => void deleteDirectory()}
                >
                  <Trash2 />
                  {t("workspace.deleteDirectory")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        ) : null}
      </div>
      {expanded ? (
        <div role="group">
          {childrenQuery.isPending ? (
            <div
              className="text-muted-foreground type-caption min-h-row-compact flex items-center gap-2"
              style={{ paddingLeft: `${40 + depth * 16}px` }}
            >
              <Spinner />
            </div>
          ) : childrenQuery.isError ? (
            <div
              className="text-destructive type-caption min-h-row-compact flex items-center gap-2 pr-2"
              style={{ paddingLeft: `${40 + depth * 16}px` }}
            >
              <span className="min-w-0 flex-1 truncate">
                {t("workspace.directoryLoadError", { name: entry.name })}
              </span>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => void childrenQuery.refetch()}
              >
                {t("workspace.retry")}
              </Button>
            </div>
          ) : childrenQuery.data?.length ? (
            childrenQuery.data.map((child) => (
              <WorkspaceTreeEntry
                active={active}
                activePath={activePath}
                depth={depth + 1}
                entry={child}
                folderReveal={folderReveal}
                key={child.relativePath}
                onOpenFile={onOpenFile}
                onPathDeleted={onPathDeleted}
                onPathRenamed={onPathRenamed}
                isPathDirty={isPathDirty}
                rootPath={rootPath}
                scopeChatId={scopeChatId}
              />
            ))
          ) : null}
        </div>
      ) : null}
      <Dialog open={renameOpen} onOpenChange={setRenameOpen}>
        <DialogContent
          size="sm"
          title={t("workspace.renameDirectory")}
          description={entry.relativePath}
        >
          <div className="px-6 pb-4">
            <Input
              aria-label={t("workspace.newDirectoryName")}
              autoFocus
              disabled={renaming}
              onChange={(event) => setNextName(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") void renameDirectory();
              }}
              value={nextName}
            />
          </div>
          <DialogFooter>
            <Button
              disabled={renaming}
              onClick={() => setRenameOpen(false)}
              variant="secondary"
            >
              {t("workspace.cancelDelete")}
            </Button>
            <Button
              disabled={renaming || !nextName}
              onClick={() => void renameDirectory()}
            >
              {t("workspace.renameDirectory")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function WorkspaceFileButton({
  activePath,
  canonicalPath,
  depth = 0,
  isPathDirty,
  name,
  onOpenFile,
  onPathDeleted,
  onPathRenamed,
  path,
  rootPath,
  scopeChatId,
  secondary,
}: {
  activePath?: string;
  canonicalPath?: string;
  depth?: number;
  isPathDirty?: (
    path: string,
    file?: ResolvedFileReference,
    rootPath?: string,
  ) => boolean;
  name: string;
  onOpenFile: (file: { name: string; path: string }) => void;
  onPathDeleted?: (
    path: string,
    file?: ResolvedFileReference,
    directory?: boolean,
    rootPath?: string,
  ) => void;
  onPathRenamed?: (
    oldPath: string,
    newPath: string,
    name: string,
    file?: ResolvedFileReference,
    directory?: boolean,
    rootPath?: string,
  ) => void;
  path: string;
  rootPath: string;
  scopeChatId?: string;
  secondary?: string;
}) {
  const { t } = useTranslation("chat");
  const queryClient = useQueryClient();
  const confirmation = useWorkspaceConfirmation();
  const [renameOpen, setRenameOpen] = useState(false);
  const [nextName, setNextName] = useState(name);
  const [renaming, setRenaming] = useState(false);
  const referenceContext = useFileReferenceContext();
  const absolutePath = workspacePathAtRoot(rootPath, path);
  const selected = matchesWorkspacePath(
    activePath,
    canonicalPath ?? absolutePath,
  );
  const resolveMutationFile = async () => {
    if (!referenceContext.workspaceRoot || !absolutePath) return undefined;
    const result = await resolveFileReference(
      { kind: "file", path: absolutePath, raw: absolutePath },
      referenceContext,
    );
    return result.status === "resolved" ? result.file : undefined;
  };
  const refreshTree = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["workspace-directory"] }),
      queryClient.invalidateQueries({ queryKey: ["workspace-file-search"] }),
    ]);
  };
  const renameFile = async () => {
    if (!scopeChatId || renaming) return;
    setRenaming(true);
    try {
      const file = await resolveMutationFile();
      if (isPathDirty?.(path, file, rootPath)) {
        toast.error(t("workspace.saveBeforeFileOperation"));
        return;
      }
      const scope = await openWorkspaceResourceScope(scopeChatId);
      const resource = await statWorkspaceResource(scope.id, path, rootPath);
      if (!resource) throw new Error(t("workspace.fileLoadError"));
      const result = await renameWorkspaceResource(
        scopeChatId,
        path,
        nextName,
        resource.version,
        rootPath,
      );
      onPathRenamed?.(
        path,
        result.relativePath,
        nextName,
        file,
        false,
        rootPath,
      );
      setRenameOpen(false);
      await refreshTree();
    } catch (cause) {
      toast.error(
        cause instanceof Error ? cause.message : t("workspace.renameFileError"),
      );
    } finally {
      setRenaming(false);
    }
  };
  const deleteFile = async () => {
    if (!scopeChatId) return;
    try {
      const file = await resolveMutationFile();
      if (isPathDirty?.(path, file, rootPath)) {
        toast.error(t("workspace.saveBeforeFileOperation"));
        return;
      }
      const scope = await openWorkspaceResourceScope(scopeChatId);
      const resource = await statWorkspaceResource(scope.id, path, rootPath);
      if (!resource) throw new Error(t("workspace.fileLoadError"));
      if (!(await confirmation.confirm({ kind: "file", path }))) return;
      if (
        !(await deleteWorkspaceResource(
          scopeChatId,
          path,
          rootPath,
          resource.version,
        ))
      )
        return;
      onPathDeleted?.(path, file, false, rootPath);
      await refreshTree();
    } catch (cause) {
      toast.error(
        cause instanceof Error ? cause.message : t("workspace.deleteFileError"),
      );
    }
  };
  return (
    <div className="group hover:bg-muted flex min-w-0 items-center rounded-sm">
      {confirmation.dialog}
      <FileTreeRow
        density="compact"
        depth={depth}
        selected={selected}
        icon={
          <WorkspaceFileTypeIcon isDirectory={false} name={name} size={16} />
        }
        draggable
        onDragStart={(event) => {
          event.dataTransfer.effectAllowed = "copy";
          event.dataTransfer.setData(
            workspaceFileDragType,
            JSON.stringify({ chatId: scopeChatId, rootPath, path }),
          );
        }}
        title={canonicalPath ?? path}
        aria-current={selected ? "page" : undefined}
        onClick={() => onOpenFile({ name, path })}
      >
        <span className="min-w-0">
          <span className="block truncate">{name}</span>
          {secondary && secondary !== name ? (
            <span className="text-muted-foreground type-caption block truncate">
              {canonicalPath ?? secondary}
            </span>
          ) : null}
        </span>
      </FileTreeRow>
      {scopeChatId && hasPlatformService("workspace") ? (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              aria-label={t("workspace.fileActions", { name })}
              className="shrink-0 opacity-0 group-focus-within:opacity-100 group-hover:opacity-100"
              size="icon-sm"
              variant="ghost"
            >
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              onSelect={() => {
                setNextName(name);
                setRenameOpen(true);
              }}
            >
              <Pencil />
              {t("workspace.renameFile")}
            </DropdownMenuItem>
            <DropdownMenuItem
              onSelect={() => {
                void saveWorkspaceResourceAs(scopeChatId, path, rootPath).catch(
                  () => toast.error(t("workspace.downloadFileError")),
                );
              }}
            >
              <Download />
              {t("workspace.downloadFile")}
            </DropdownMenuItem>
            <DropdownMenuItem
              variant="destructive"
              onSelect={() => void deleteFile()}
            >
              <Trash2 />
              {t("workspace.deleteFile")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      ) : null}
      <Dialog open={renameOpen} onOpenChange={setRenameOpen}>
        <DialogContent
          size="sm"
          title={t("workspace.renameFile")}
          description={path}
        >
          <div className="px-6 pb-4">
            <Input
              aria-label={t("workspace.newFileName")}
              autoFocus
              disabled={renaming}
              onChange={(event) => setNextName(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") void renameFile();
              }}
              value={nextName}
            />
          </div>
          <DialogFooter>
            <Button
              disabled={renaming}
              onClick={() => setRenameOpen(false)}
              variant="secondary"
            >
              {t("workspace.cancelDelete")}
            </Button>
            <Button
              disabled={renaming || !nextName}
              onClick={() => void renameFile()}
            >
              {t("workspace.renameFile")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function WorkspaceBrowserLoading() {
  const { t } = useTranslation("chat");
  return (
    <div
      aria-busy="true"
      aria-label={t("workspace.filesLoading")}
      className="text-muted-foreground type-caption flex min-h-44 items-center justify-center gap-2"
      role="status"
    >
      <Spinner />
      {t("workspace.filesLoading")}
    </div>
  );
}

function WorkspaceBrowserState({
  action,
  icon: Icon,
  title,
}: {
  action?: ReactNode;
  icon: typeof FileText;
  title: string;
}) {
  return (
    <div className="grid min-h-44 place-items-center px-4 text-center">
      <div>
        <Icon
          className="text-muted-foreground size-icon-lg mx-auto"
          strokeWidth={1.5}
        />
        <p className="type-body mt-3 font-medium">{title}</p>
        {action ? <div className="mt-3">{action}</div> : null}
      </div>
    </div>
  );
}
