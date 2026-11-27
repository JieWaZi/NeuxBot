import { useQueryClient } from "@tanstack/react-query";
import {
  Download,
  FolderOpen,
  FolderTree,
  MoreHorizontal,
  Trash2,
  WrapText,
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
import {
  fileTreeSidebarRatio,
  WorkspaceDetailSplit,
} from "../../components/workspace-detail-split";
import { WorkspaceSurfaceHeader } from "../../components/workspace-surface-header";
import { WorkspaceFileOpenActions } from "./workspace-file-open-actions";
import {
  WorkbenchFileBreadcrumb,
  type BreadcrumbDirectory,
} from "./workbench-file-breadcrumb";
import {
  fileName,
  parseReference,
  resolveFileReference,
  type ResolvedFileReference,
} from "@/shared/workspace/file-reference";
import { FileReferenceProvider } from "@/shared/workspace/file-reference-link";
import { WorkspaceFileTypeIcon } from "@/shared/workspace/workspace-file-type-icon";
import type { WorkspaceFile } from "@/shared/workspace/types";
import { workspaceFileKey } from "../../model/chat-workspace-files";
import {
  importDroppedWorkspaceFiles,
  hasPlatformService,
  isDesktopRuntime,
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
    rootPath: string;
    chatId?: string;
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
  onDeleteChatFile?: (file: WorkspaceFile) => Promise<boolean>;
  onSelectFile?: (
    file: Pick<WorkspaceFile, "name" | "path" | "sourceChatId">,
    root?: string,
  ) => void;
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
  const [folderReveal, setFolderReveal] = useState<{
    path: string;
    root: string;
    fileRevision: number;
  }>();
  const workingRoots = workingDirectoryRoots(
    spaceRootAvailable ? rootPath : "",
    spaceRootAvailable ? sourceRoots : [rootPath, ...(sourceRoots ?? [])],
  );
  const browseWorking =
    (!spaceRootAvailable || directorySource === "working") &&
    workingRoots.length > 0;
  const treeActive = active && treeOpen;
  const isProject = fileSource === "project";
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const visibleChatFiles = chatFiles.filter((file) =>
    `${file.name} ${file.path}`.toLocaleLowerCase().includes(normalizedQuery),
  );

  const spaceDirectory = workspacePathAtRoot(rootPath, scopePath) ?? rootPath;
  const directoryScope = (path: string): BreadcrumbDirectory | undefined => {
    if (!isProject) return undefined;
    const directoryRoot = [
      ...(spaceRootAvailable && rootPath ? [rootPath] : []),
      ...workingRoots,
    ]
      .filter((root) =>
        matchesWorkspacePath(
          path,
          root === rootPath && spaceRootAvailable ? spaceDirectory : root,
          true,
        ),
      )
      .sort((left, right) => right.length - left.length)[0];
    if (!directoryRoot) return undefined;
    return {
      rootPath: directoryRoot,
      path: path
        .slice(directoryRoot.replaceAll("\\", "/").replace(/\/+$/, "").length)
        .replace(/^\/+/, ""),
    };
  };
  const revealDirectory = (path: string, root: string) => {
    if (!file) return;
    setDirectorySource(workingRoots.includes(root) ? "working" : "space");
    setQuery("");
    setTreeOpen(true);
    setFolderReveal({ path, root, fileRevision: file.navigationRevision });
  };

  const revealPath = file?.canonicalPath;
  const revealRevision = file?.navigationRevision;
  const currentFolder =
    folderReveal?.fileRevision === revealRevision ? folderReveal : undefined;
  const treeRoot =
    browseWorking && workingRoots.length === 1
      ? workingRoots[0]
      : browseWorking
        ? undefined
        : spaceDirectory;
  const revealTreeRoot = Boolean(
    currentFolder &&
    (currentFolder.path === treeRoot ||
      matchesWorkspacePath(currentFolder.path, treeRoot)),
  );
  useEffect(() => {
    const tree = treeViewport.current;
    if (!tree || !treeActive || !revealPath) return;
    if (revealTreeRoot) {
      tree.scrollTop = 0;
      return;
    }
    const scrollToRow = () => {
      const row = tree.querySelector<HTMLElement>(
        currentFolder
          ? "button[data-revealed-directory]"
          : 'button[aria-current="page"]',
      );
      if (!row) return false;
      row.scrollIntoView({ block: "nearest", inline: "nearest" });
      return true;
    };
    if (scrollToRow()) return;
    // Ancestors and the selected row can arrive in separate lazy directory reads.
    const observer = new MutationObserver(() => {
      if (scrollToRow()) observer.disconnect();
    });
    observer.observe(tree, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ["aria-current", "data-revealed-directory"],
    });
    return () => observer.disconnect();
  }, [currentFolder, revealTreeRoot, treeActive, revealPath, revealRevision]);

  const deleteChatFile = async (
    file: WorkspaceFile,
  ): Promise<ChatFileDeleteError | undefined> => {
    if (!onDeleteChatFile) return "deleteFileError";
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
          folderReveal={
            currentFolder?.root === directory ? currentFolder : undefined
          }
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
            onValueChange={(source) => {
              setFolderReveal(undefined);
              setDirectorySource(source);
            }}
          />
        ) : null}
        <SearchField
          aria-label={t("workspace.searchFiles")}
          placeholder={t("workspace.searchFilesPlaceholder")}
          size="sm"
          value={query}
          onChange={(event) => {
            setFolderReveal(undefined);
            setQuery(event.target.value);
          }}
        />
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
                    folderReveal={
                      currentFolder?.root === directory
                        ? currentFolder
                        : undefined
                    }
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
                key={workspaceFileKey(file)}
                file={file}
                onSelect={() => onSelectFile?.(file)}
                onDownload={() => onDownloadChatFile(file)}
                onDelete={
                  onDeleteChatFile ? () => deleteChatFile(file) : undefined
                }
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

  return (
    <TooltipProvider>
      <div
        className="bg-surface-scene relative flex h-full min-h-0 min-w-0 flex-col"
        data-workbench-file-workspace
      >
        <WorkspaceSurfaceHeader>
          {file ? (
            <WorkbenchFileBreadcrumb
              active={active}
              file={file}
              scopeChatId={scopeChatId}
              directoryScope={directoryScope}
              onRevealDirectory={revealDirectory}
              onOpenFile={onSelectFile}
            />
          ) : (
            <div className="flex-1" />
          )}
          {file ? (
            <div ref={setToolbarTarget} className="flex min-w-0 items-center" />
          ) : (
            <>
              {isProject && isDesktopRuntime() ? (
                <WorkspaceFileOpenActions active={active} />
              ) : null}
              <FileWorkspaceAction
                label={t("workspace.wrapFileLines")}
                disabled
              >
                <WrapText />
              </FileWorkspaceAction>
            </>
          )}
          <FileWorkspaceAction
            label={t(
              treeOpen ? "workspace.hideFileTree" : "workspace.showFileTree",
            )}
            pressed={treeOpen}
            onClick={() => setTreeOpen((open) => !open)}
          >
            <FolderTree />
          </FileWorkspaceAction>
        </WorkspaceSurfaceHeader>
        <WorkspaceDetailSplit
          defaultSidebarRatio={fileTreeSidebarRatio}
          sidebarOpen={treeOpen}
          resizeLabel={t("workbench.resizeSplit")}
          main={
            <div className="bg-surface relative min-h-0 min-w-0 flex-1 overflow-hidden">
              {file ? (
                file.render(toolbarTarget)
              ) : (
                <EmptyState
                  density="compact"
                  icon={FolderOpen}
                  title={t("workspace.chooseFile")}
                  description={
                    treeOpen ? t("workspace.chooseFileDescription") : undefined
                  }
                  className="h-full min-h-0"
                />
              )}
            </div>
          }
          sidebar={treeOpen ? tree : null}
        />
      </div>
    </TooltipProvider>
  );
}

function FileWorkspaceAction({
  children,
  disabled,
  label,
  onClick,
  pressed,
}: {
  children: ReactNode;
  disabled?: boolean;
  label: string;
  onClick?: () => void;
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
          variant={pressed ? "secondary" : "ghost"}
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
  folderReveal,
  children,
}: {
  directory: string;
  searching: boolean;
  containsFile: boolean;
  folderReveal?: { path: string };
  children: ReactNode;
}) {
  const [override, setOverride] = useState<{
    folderReveal?: { path: string };
    open: boolean;
  }>();
  const expanded =
    override && (!folderReveal || override.folderReveal === folderReveal)
      ? override.open
      : Boolean(folderReveal || containsFile);
  return (
    <div>
      <div className="flex min-w-0">
        <FileTreeRow
          density="compact"
          directory
          expanded={expanded || searching}
          role="button"
          title={directory}
          data-revealed-directory={
            matchesWorkspacePath(folderReveal?.path, directory) || undefined
          }
          onClick={() => setOverride({ folderReveal, open: !expanded })}
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
  onDelete?: () => Promise<ChatFileDeleteError | undefined>;
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
          {onDelete && (
            <DropdownMenuItem
              variant="destructive"
              onSelect={() => setDeleteOpen(true)}
            >
              <Trash2 />
              {t("workspace.deleteFile")}
            </DropdownMenuItem>
          )}
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
                if (!onDelete) return;
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
