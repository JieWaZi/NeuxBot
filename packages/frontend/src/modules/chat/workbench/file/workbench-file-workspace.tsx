import { useQueryClient } from "@tanstack/react-query";
import {
  ChevronsDownUp,
  ChevronsUpDown,
  Download,
  Folders,
  FolderOpen,
  MoreHorizontal,
  RefreshCw,
  Trash2,
} from "lucide-react";
import {
  useEffect,
  useRef,
  useState,
  type DragEvent,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";

import {
  WorkspaceFileBrowser,
  workspaceFileBrowserKeys,
} from "../../components/workspace-file-browser";
import { WorkspaceDetailSplit } from "../../components/workspace-detail-split";
import {
  fileName,
  parseReference,
  resolveFileReference,
  type ResolvedFileReference,
} from "@/shared/workspace/file-reference";
import { FileReferenceProvider } from "@/shared/workspace/file-reference-link";
import { WorkspaceFileTypeIcon } from "@/shared/workspace/workspace-file-type-icon";
import type { WorkspaceFile } from "@/shared/workspace/types";
import {
  importDroppedWorkspaceFiles,
  hasPlatformService,
  type WorkspaceDirectoryEntry,
} from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { EmptyState } from "@/shared/ui/empty-state";
import { SearchField } from "@/shared/ui/search-field";
import { Spinner } from "@/shared/ui/spinner";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { FileTreeRow } from "@/shared/ui/file-tree-row";
import { toast } from "@/shared/ui/sonner";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";

import {
  workingDirectoryRoots,
  workspacePathAtRoot,
  matchesWorkspacePath,
} from "../../model/project-files-scope";

export type WorkbenchFileWorkspaceProps = {
  active?: boolean;
  rootPath: string;
  scopeChatId?: string;
  scopePath: string;
  sourceRoots?: readonly string[];
  spaceRootAvailable?: boolean;
  file?: {
    path: string;
    canonicalPath: string;
    navigationRevision: number;
    render: (toolbarTarget: HTMLDivElement | null) => ReactNode;
  };
  chatFiles: WorkspaceFile[];
  fileSource: "project" | "chat";
  chatFilesLoading?: boolean;
  chatFilesError?: boolean;
  onRetryChatFiles?: () => void;
  onFileSourceChange?: (source: "project" | "chat") => void;
  onDownloadChatFile: (file: WorkspaceFile) => Promise<void>;
  onDeleteChatFile: (file: WorkspaceFile) => Promise<boolean>;
  onSelectFile?: (file: { name: string; path: string }, root?: string) => void;
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
};

type ChatFileDeleteError = "deleteFileError" | "saveBeforeFileOperation";

export function WorkbenchFileWorkspace({
  active = true,
  rootPath,
  scopeChatId,
  scopePath,
  sourceRoots,
  spaceRootAvailable = true,
  file,
  chatFiles,
  fileSource,
  chatFilesLoading = false,
  chatFilesError = false,
  onRetryChatFiles,
  onDownloadChatFile,
  onDeleteChatFile,
  onSelectFile,
  onPathDeleted,
  onPathRenamed,
  isPathDirty,
}: WorkbenchFileWorkspaceProps) {
  const { t } = useTranslation("chat");
  const queryClient = useQueryClient();
  const [treeOpen, setTreeOpen] = useState(true);
  const treeViewport = useRef<HTMLDivElement>(null);
  const [toolbarTarget, setToolbarTarget] = useState<HTMLDivElement | null>(
    null,
  );
  const [query, setQuery] = useState("");
  const [dropActive, setDropActive] = useState(false);
  const [directorySource, setDirectorySource] = useState<"space" | "working">(
    () =>
      file && !matchesWorkspacePath(file.canonicalPath, rootPath, true)
        ? "working"
        : "space",
  );
  const [treeExpansion, setTreeExpansion] = useState<{
    paths: ReadonlySet<string>;
    workingRootsExpanded: boolean;
  }>();
  const workingRoots = workingDirectoryRoots(
    spaceRootAvailable ? rootPath : "",
    spaceRootAvailable ? sourceRoots : [rootPath, ...(sourceRoots ?? [])],
  );
  const browseWorking =
    (!spaceRootAvailable || directorySource === "working") &&
    workingRoots.length > 0;
  const directoryRoots = browseWorking ? workingRoots : [rootPath];
  const treeActive = active && (!file || treeOpen);
  const isProject = fileSource === "project";
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const visibleChatFiles = chatFiles.filter((file) =>
    `${file.name} ${file.path}`.toLocaleLowerCase().includes(normalizedQuery),
  );

  const revealPath = file?.canonicalPath;
  const revealRevision = file?.navigationRevision;
  useEffect(() => {
    const tree = treeViewport.current;
    if (!tree || !treeActive || !revealPath) return;
    const scrollSelectedFile = () => {
      const row = tree.querySelector<HTMLElement>(
        'button[aria-current="page"]',
      );
      if (!row) return false;
      row.scrollIntoView({ block: "nearest", inline: "nearest" });
      return true;
    };
    if (scrollSelectedFile()) return;
    // Ancestors and the selected row can arrive in separate lazy directory reads.
    const observer = new MutationObserver(() => {
      if (scrollSelectedFile()) observer.disconnect();
    });
    observer.observe(tree, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ["aria-current"],
    });
    return () => observer.disconnect();
  }, [treeActive, revealPath, revealRevision]);

  const refreshTree = () => {
    for (const root of directoryRoots) {
      void queryClient.invalidateQueries({
        queryKey: workspaceFileBrowserKeys.directories(scopeChatId, root),
      });
      void queryClient.invalidateQueries({
        queryKey: workspaceFileBrowserKeys.searches(scopeChatId, root),
      });
    }
  };
  const expandFileTree = () => {
    const paths = new Set<string>();
    // 展开已读取的目录。子目录继续按需读取，避免递归遍历整个工作区。
    for (const root of directoryRoots) {
      const queries = queryClient.getQueriesData<WorkspaceDirectoryEntry[]>({
        queryKey: workspaceFileBrowserKeys.directories(scopeChatId, root),
      });
      for (const [, entries] of queries) {
        for (const entry of entries ?? []) {
          const path = workspacePathAtRoot(root, entry.relativePath);
          if (entry.isDirectory && path) paths.add(path);
        }
      }
    }
    setTreeExpansion({ paths, workingRootsExpanded: true });
  };

  const deleteChatFile = async (
    file: WorkspaceFile,
  ): Promise<ChatFileDeleteError | undefined> => {
    try {
      const candidate = parseReference(file.path, "path");
      if (candidate?.kind !== "file") return "deleteFileError";
      const resolved = await resolveFileReference(candidate, {
        chatId: scopeChatId,
        baseDirectory: "",
        workspaceRoot: "",
        sourceRoots: [],
      });
      if (resolved.status !== "resolved") return "deleteFileError";
      if (isPathDirty?.(file.path, resolved.file))
        return "saveBeforeFileOperation";
      if (!(await onDeleteChatFile(file))) return "deleteFileError";
      onPathDeleted?.(file.path, resolved.file, false, resolved.file.rootPath);
    } catch {
      return "deleteFileError";
    }
  };

  const handleDrop = (
    event: DragEvent<HTMLDivElement>,
    directory: string,
    directoryScope: string,
  ) => {
    if (
      !scopeChatId ||
      !hasPlatformService("workspace") ||
      !event.dataTransfer.types.includes("Files")
    )
      return;
    event.preventDefault();
    event.stopPropagation();
    setDropActive(false);
    void importDroppedWorkspaceFiles(
      scopeChatId,
      directoryScope,
      Array.from(event.dataTransfer.files),
      directory,
    )
      .then(async (result) => {
        if (result.failed.length)
          toast.error(
            t("workspace.importFileErrorCount", {
              count: result.failed.length,
            }),
          );
        if (result.imported.length)
          await queryClient.invalidateQueries({
            queryKey: workspaceFileBrowserKeys.directories(
              scopeChatId,
              directory,
            ),
          });
      })
      .catch((cause: unknown) =>
        toast.error(
          cause instanceof Error
            ? cause.message
            : t("workspace.importFileError"),
        ),
      );
  };

  const directoryTree = (directory: string, directoryScope = "") => (
    <FileReferenceProvider
      key={directory}
      context={{
        chatId: scopeChatId,
        baseDirectory: directory,
        workspaceRoot: directory,
        sourceRoots: sourceRoots ?? [],
      }}
    >
      <div
        className={dropActive ? "outline-accent-default outline-2" : undefined}
        onDragOver={(event) => {
          if (
            !scopeChatId ||
            !hasPlatformService("workspace") ||
            !event.dataTransfer.types.includes("Files")
          )
            return;
          event.preventDefault();
          setDropActive(true);
        }}
        onDragLeave={() => setDropActive(false)}
        onDrop={(event) => handleDrop(event, directory, directoryScope)}
      >
        <WorkspaceFileBrowser
          active={treeActive}
          activePath={file?.canonicalPath}
          expandedPaths={treeExpansion?.paths}
          isPathDirty={isPathDirty}
          onOpenFile={(entry) => onSelectFile?.(entry, directory)}
          onPathDeleted={onPathDeleted}
          onPathRenamed={onPathRenamed}
          query={query}
          rootPath={directory}
          scopeChatId={scopeChatId}
          scopePath={directoryScope}
        />
      </div>
    </FileReferenceProvider>
  );

  const tree = (
    <aside
      aria-label={t(isProject ? "workspace.fileTree" : "workspace.chatFiles")}
      className="bg-surface-panel flex h-full min-h-0 min-w-0 flex-col"
    >
      <div className="grid shrink-0 gap-2 px-2 py-2">
        {isProject &&
        spaceRootAvailable &&
        rootPath &&
        workingRoots.length > 0 ? (
          <SegmentedControl
            ariaLabel={t("workspace.directories")}
            size="sm"
            distribution="fill"
            value={browseWorking ? "working" : "space"}
            options={[
              { value: "space", label: t("workspace.spaceDirectory") },
              { value: "working", label: t("workspace.workingDirectory") },
            ]}
            onValueChange={setDirectorySource}
          />
        ) : null}
        <div className="flex min-w-0 flex-wrap items-center gap-1">
          <div className="basis-field-search-min min-w-0 grow">
            <SearchField
              aria-label={t("workspace.searchFiles")}
              placeholder={t("workspace.searchFilesPlaceholder")}
              size="sm"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
          </div>
          {isProject ? (
            <div className="ml-auto flex min-w-0 flex-wrap items-center">
              <FileTreeAction
                label={t("workspace.refreshFileTree")}
                onClick={refreshTree}
              >
                <RefreshCw />
              </FileTreeAction>
              <FileTreeAction
                disabled={Boolean(normalizedQuery)}
                label={t("workspace.expandFileTree")}
                onClick={expandFileTree}
              >
                <ChevronsUpDown />
              </FileTreeAction>
              <FileTreeAction
                disabled={Boolean(normalizedQuery)}
                label={t("workspace.collapseFileTree")}
                onClick={() =>
                  setTreeExpansion({
                    paths: new Set(),
                    workingRootsExpanded: false,
                  })
                }
              >
                <ChevronsDownUp />
              </FileTreeAction>
            </div>
          ) : null}
        </div>
      </div>
      <div
        ref={treeViewport}
        className="scrollbar-hidden min-h-0 flex-1 overflow-y-auto overscroll-contain px-2 pb-2"
      >
        {isProject ? (
          browseWorking ? (
            workingRoots.length === 1 ? (
              directoryTree(workingRoots[0] ?? "")
            ) : (
              <div className="grid gap-0.5">
                {workingRoots.map((directory) => (
                  <WorkingDirectoryRoot
                    key={directory}
                    directory={directory}
                    searching={Boolean(normalizedQuery)}
                    containsFile={matchesWorkspacePath(
                      file?.canonicalPath,
                      directory,
                      true,
                    )}
                    expansion={treeExpansion}
                  >
                    {directoryTree(directory)}
                  </WorkingDirectoryRoot>
                ))}
              </div>
            )
          ) : (
            directoryTree(rootPath, scopePath)
          )
        ) : chatFilesLoading ? (
          <div
            role="status"
            className="flex min-h-44 items-center justify-center"
          >
            <Spinner aria-label={t("workspace.chatFilesLoading")} />
          </div>
        ) : chatFilesError ? (
          <EmptyState
            density="compact"
            icon={FolderOpen}
            title={t("workspace.chatFilesLoadError")}
            actions={
              onRetryChatFiles ? (
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={onRetryChatFiles}
                >
                  {t("workspace.retry")}
                </Button>
              ) : undefined
            }
          />
        ) : visibleChatFiles.length ? (
          <div role="tree" className="grid gap-1">
            {visibleChatFiles.map((file) => (
              <ChatFileRow
                key={file.path}
                file={file}
                onSelect={() => onSelectFile?.(file)}
                onDownload={() => onDownloadChatFile(file)}
                onDelete={() => deleteChatFile(file)}
              />
            ))}
          </div>
        ) : (
          <EmptyState
            density="compact"
            icon={FolderOpen}
            title={t(
              normalizedQuery
                ? "workspace.noMatchingChatFiles"
                : "workspace.noChatFiles",
            )}
          />
        )}
      </div>
    </aside>
  );

  if (!file) return <TooltipProvider>{tree}</TooltipProvider>;

  return (
    <TooltipProvider>
      <div
        className="bg-surface-scene relative flex h-full min-h-0 min-w-0 flex-col"
        data-workbench-file-workspace
      >
        <header className="border-border-subtle h-row-navigation flex min-w-0 shrink-0 items-center gap-1 border-b px-3">
          <nav
            aria-label={t("workspace.fileBreadcrumb")}
            className="text-muted-foreground type-ui min-w-0 flex-1 truncate"
            title={file.canonicalPath}
          >
            {file.path
              .replaceAll("\\", "/")
              .split("/")
              .filter(Boolean)
              .join(" / ")}
          </nav>
          <div ref={setToolbarTarget} className="flex min-w-0 items-center" />
          <FileTreeAction
            label={t(
              treeOpen ? "workspace.hideFileTree" : "workspace.showFileTree",
            )}
            pressed={treeOpen}
            onClick={() => setTreeOpen((open) => !open)}
          >
            <Folders />
          </FileTreeAction>
        </header>
        <WorkspaceDetailSplit
          sidebarOpen={treeOpen}
          resizeLabel={t("workbench.resizeSplit")}
          main={
            <div className="relative min-h-0 min-w-0 flex-1 overflow-hidden">
              {file.render(toolbarTarget)}
            </div>
          }
          sidebar={tree}
        />
      </div>
    </TooltipProvider>
  );
}

function FileTreeAction({
  children,
  disabled,
  label,
  onClick,
  pressed,
}: {
  children: ReactNode;
  disabled?: boolean;
  label: string;
  onClick: () => void;
  pressed?: boolean;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          aria-label={label}
          aria-pressed={pressed}
          disabled={disabled}
          size="icon-sm"
          variant="ghost"
          className="shrink-0"
          onClick={onClick}
        >
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

function WorkingDirectoryRoot({
  directory,
  searching,
  containsFile,
  expansion,
  children,
}: {
  directory: string;
  searching: boolean;
  containsFile: boolean;
  expansion?: { workingRootsExpanded: boolean };
  children: ReactNode;
}) {
  const [override, setOverride] = useState<{
    expansion?: typeof expansion;
    open: boolean;
  }>();
  const expanded =
    override && override.expansion === expansion
      ? override.open
      : (expansion?.workingRootsExpanded ?? containsFile);
  return (
    <div>
      <div className="flex min-w-0">
        <FileTreeRow
          directory
          expanded={expanded || searching}
          role="button"
          title={directory}
          onClick={() => setOverride({ expansion, open: !expanded })}
        >
          <span className="truncate">
            {fileName(directory.replace(/[\\/]+$/, ""))}
          </span>
        </FileTreeRow>
      </div>
      {expanded || searching ? <div className="pl-4">{children}</div> : null}
    </div>
  );
}

function ChatFileRow({
  file,
  onSelect,
  onDownload,
  onDelete,
}: {
  file: WorkspaceFile;
  onSelect: () => void;
  onDownload: () => Promise<void>;
  onDelete: () => Promise<ChatFileDeleteError | undefined>;
}) {
  const { t } = useTranslation("chat");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<ChatFileDeleteError>();
  return (
    <div className="group hover:bg-muted flex min-w-0 items-center rounded-sm">
      <button
        type="button"
        role="treeitem"
        title={file.path}
        className="focus-visible:ring-ring type-body min-h-row-default flex min-w-0 flex-1 items-center gap-2 rounded-sm px-2 text-left outline-none focus-visible:ring-2"
        onClick={onSelect}
      >
        <WorkspaceFileTypeIcon isDirectory={false} name={file.name} size={16} />
        <span className="min-w-0 truncate">{file.name}</span>
      </button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            aria-label={t("workspace.fileActions", { name: file.name })}
            className="shrink-0 opacity-0 group-focus-within:opacity-100 group-hover:opacity-100"
            size="icon-sm"
            variant="ghost"
          >
            <MoreHorizontal />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onSelect={() => void onDownload()}>
            <Download />
            {t("workspace.downloadFile")}
          </DropdownMenuItem>
          <DropdownMenuItem
            variant="destructive"
            onSelect={() => setDeleteOpen(true)}
          >
            <Trash2 />
            {t("workspace.deleteFile")}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <Dialog
        open={deleteOpen}
        onOpenChange={(open) => {
          if (!deleting) setDeleteOpen(open);
          if (open) setDeleteError(undefined);
        }}
      >
        <DialogContent
          size="sm"
          title={t("workspace.deleteFile")}
          description={t("workspace.deleteFileConfirm", { name: file.name })}
        >
          {deleteError ? (
            <p role="alert" className="text-destructive type-body px-6 pb-4">
              {t(`workspace.${deleteError}`)}
            </p>
          ) : null}
          <DialogFooter>
            <Button
              variant="secondary"
              disabled={deleting}
              onClick={() => setDeleteOpen(false)}
            >
              {t("workspace.cancelDelete")}
            </Button>
            <Button
              variant="danger"
              disabled={deleting}
              onClick={() => {
                setDeleting(true);
                setDeleteError(undefined);
                void onDelete().then((error) => {
                  setDeleting(false);
                  if (error) setDeleteError(error);
                  else setDeleteOpen(false);
                });
              }}
            >
              {t("workspace.deleteFile")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
