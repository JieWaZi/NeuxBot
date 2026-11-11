import { useQueryClient } from "@tanstack/react-query";
import {
  Download,
  Folders,
  FolderOpen,
  MoreHorizontal,
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

import { WorkspaceFileBrowser } from "../../components/workspace-file-browser";
import { WorkspaceFileViewer } from "../../components/workspace-file-viewer";
import { WorkspaceDetailSplit } from "../../components/workspace-detail-split";
import {
  fileName,
  isAbsoluteFilePath,
  parseReference,
  resolveFileReference,
  type ResolvedFileReference,
} from "@/shared/workspace/file-reference";
import { FileReferenceProvider } from "@/shared/workspace/file-reference-link";
import { WorkspaceFileTypeIcon } from "@/shared/workspace/workspace-file-type-icon";
import type { WorkspaceFile } from "@/shared/workspace/types";
import type { WorkspaceFileView } from "@/shared/workspace/workspace-tab";
import {
  importDroppedWorkspaceFiles,
  hasPlatformService,
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

import { workbenchDocumentBuffers } from "../document-buffers";
import {
  workingDirectoryRoots,
  workspacePathAtRoot,
  matchesWorkspacePath,
} from "../../model/project-files-scope";

export type FileWorkspaceSelection = {
  name: string;
  path: string;
  rootPath: string;
  canonicalPath: string;
  reference?: ResolvedFileReference;
};

export type WorkbenchFileWorkspaceProps = {
  active?: boolean;
  ownerKey: string;
  rootPath: string;
  scopeChatId?: string;
  scopePath: string;
  sourceRoots?: readonly string[];
  spaceRootAvailable?: boolean;
  initialFile?: FileWorkspaceSelection;
  chatFiles: WorkspaceFile[];
  fileSource: "project" | "chat";
  chatFilesLoading?: boolean;
  chatFilesError?: boolean;
  onRetryChatFiles?: () => void;
  onFileSourceChange?: (source: "project" | "chat") => void;
  onDownloadChatFile: (file: WorkspaceFile) => Promise<void>;
  onDeleteChatFile: (file: WorkspaceFile) => Promise<boolean>;
  onOpenFileReference?: (file: ResolvedFileReference) => void;
  onOpenURL?: (url: string) => unknown;
  onAddCodeReference?: (reference: {
    id: string;
    path: string;
    startLine: number;
    endLine: number;
    content: string;
  }) => void;
  onSelectedFileChange?: (file: FileWorkspaceSelection | undefined) => void;
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

export function fileWorkspaceDraftPrefix(ownerKey: string): string {
  return `${ownerKey}:file:`;
}

export function hasDirtyFileWorkspaceDrafts(ownerKey: string): boolean {
  return workbenchDocumentBuffers.hasDirtyPrefix(
    fileWorkspaceDraftPrefix(ownerKey),
  );
}

export function discardFileWorkspaceDrafts(ownerKey: string): void {
  workbenchDocumentBuffers.discardPrefix(fileWorkspaceDraftPrefix(ownerKey));
}

function draftKey(ownerKey: string, canonicalPath: string): string {
  return `${fileWorkspaceDraftPrefix(ownerKey)}${canonicalPath}`;
}

type ChatFileDeleteError = "deleteFileError" | "saveBeforeFileOperation";

export function WorkbenchFileWorkspace({
  active = true,
  ownerKey,
  rootPath,
  scopeChatId,
  scopePath,
  sourceRoots,
  spaceRootAvailable = true,
  initialFile,
  chatFiles,
  fileSource,
  chatFilesLoading = false,
  chatFilesError = false,
  onRetryChatFiles,
  onDownloadChatFile,
  onDeleteChatFile,
  onOpenFileReference,
  onOpenURL,
  onAddCodeReference,
  onSelectedFileChange,
  onPathDeleted,
  onPathRenamed,
  isPathDirty,
}: WorkbenchFileWorkspaceProps) {
  const { t } = useTranslation("chat");
  const queryClient = useQueryClient();
  const [selected, setSelected] = useState(initialFile);
  const [fileViews, setFileViews] = useState<Map<string, WorkspaceFileView>>(
    () => new Map(),
  );
  const [treeOpen, setTreeOpen] = useState(true);
  const [toolbarTarget, setToolbarTarget] = useState<HTMLDivElement | null>(
    null,
  );
  const [query, setQuery] = useState("");
  const [dropActive, setDropActive] = useState(false);
  const [directorySource, setDirectorySource] = useState<"space" | "working">(
    "space",
  );
  const workingRoots = workingDirectoryRoots(
    spaceRootAvailable ? rootPath : "",
    spaceRootAvailable ? sourceRoots : [rootPath, ...(sourceRoots ?? [])],
  );
  const browseWorking =
    (!spaceRootAvailable || directorySource === "working") &&
    workingRoots.length > 0;
  const selectRequest = useRef(0);
  const visitedFiles = useRef(new Map<string, FileWorkspaceSelection>());
  useEffect(() => {
    if (selected) visitedFiles.current.set(selected.canonicalPath, selected);
  }, [selected]);

  const selectFile = (
    file: { name: string; path: string },
    fileRoot = fileSource === "chat" ? "" : rootPath,
  ) => {
    const candidate = parseReference(
      fileRoot && !isAbsoluteFilePath(file.path)
        ? `${fileRoot.replace(/[\\/]+$/, "")}/${file.path}`
        : file.path,
      "path",
    );
    if (candidate?.kind !== "file") return;
    const request = ++selectRequest.current;
    void resolveFileReference(candidate, {
      chatId: scopeChatId,
      baseDirectory: fileRoot,
      workspaceRoot: fileRoot,
      sourceRoots: fileRoot ? (sourceRoots ?? []) : [],
    })
      .then((result) => {
        if (request !== selectRequest.current) return;
        if (result.status !== "resolved") {
          toast.error(t("fileReference.unavailable"));
          return;
        }
        const next = {
          name: file.name || fileName(result.file.canonicalPath),
          path: result.file.path,
          rootPath: result.file.rootPath,
          canonicalPath: result.file.canonicalPath,
          reference: result.file,
        };
        visitedFiles.current.set(next.canonicalPath, next);
        setSelected(next);
        onSelectedFileChange?.(next);
      })
      .catch(() => {
        if (request === selectRequest.current)
          toast.error(t("fileReference.unavailable"));
      });
  };

  const isDirtyPath = (
    path: string,
    file?: ResolvedFileReference,
    affectedRoot?: string,
  ) => {
    const externalDirty = isPathDirty?.(path, file, affectedRoot);
    if (externalDirty) return true;
    const target =
      file?.canonicalPath ??
      workspacePathAtRoot(affectedRoot ?? rootPath, path);
    for (const opened of visitedFiles.current.values()) {
      if (
        matchesWorkspacePath(opened.canonicalPath, target, true) &&
        workbenchDocumentBuffers.isDirty(
          draftKey(ownerKey, opened.canonicalPath),
        )
      )
        return true;
    }
    return false;
  };

  const handlePathDeleted: NonNullable<
    WorkbenchFileWorkspaceProps["onPathDeleted"]
  > = (path, file, directory, affectedRoot) => {
    onPathDeleted?.(path, file, directory, affectedRoot);
    const target =
      file?.canonicalPath ??
      workspacePathAtRoot(affectedRoot ?? rootPath, path);
    if (
      selected &&
      matchesWorkspacePath(selected.canonicalPath, target, directory)
    ) {
      setSelected(undefined);
      onSelectedFileChange?.(undefined);
    }
  };

  const handlePathRenamed: NonNullable<
    WorkbenchFileWorkspaceProps["onPathRenamed"]
  > = (oldPath, newPath, name, file, directory, affectedRoot) => {
    onPathRenamed?.(oldPath, newPath, name, file, directory, affectedRoot);
    const operationRoot = affectedRoot ?? file?.rootPath ?? rootPath;
    const oldCanonical =
      file?.canonicalPath ?? workspacePathAtRoot(operationRoot, oldPath);
    const newCanonical = workspacePathAtRoot(operationRoot, newPath);
    if (
      !selected ||
      !oldCanonical ||
      !newCanonical ||
      !matchesWorkspacePath(selected.canonicalPath, oldCanonical, directory)
    )
      return;
    const suffix = selected.canonicalPath.slice(oldCanonical.length);
    selectFile(
      {
        name: directory ? selected.name : name,
        path: `${newCanonical}${suffix}`,
      },
      operationRoot,
    );
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
      if (isDirtyPath(file.path, resolved.file))
        return "saveBeforeFileOperation";
      if (!(await onDeleteChatFile(file))) return "deleteFileError";
      handlePathDeleted(
        file.path,
        resolved.file,
        false,
        resolved.file.rootPath,
      );
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
            queryKey: ["workspace-directory", scopeChatId, directory],
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

  const treeToggle = (
    <Button
      aria-label={
        treeOpen ? t("workspace.hideFileTree") : t("workspace.showFileTree")
      }
      title={
        treeOpen ? t("workspace.hideFileTree") : t("workspace.showFileTree")
      }
      size="icon-sm"
      variant="ghost"
      onClick={() => setTreeOpen((current) => !current)}
    >
      <Folders />
    </Button>
  );
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const visibleChatFiles = chatFiles.filter((file) =>
    `${file.name} ${file.path}`.toLocaleLowerCase().includes(normalizedQuery),
  );

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
          active={active}
          activePath={selected?.canonicalPath}
          isPathDirty={isDirtyPath}
          onOpenFile={(file) => selectFile(file, directory)}
          onPathDeleted={handlePathDeleted}
          onPathRenamed={handlePathRenamed}
          query={query}
          rootPath={directory}
          scopeChatId={scopeChatId}
          scopePath={directoryScope}
        />
      </div>
    </FileReferenceProvider>
  );

  return (
    <FileReferenceProvider
      context={{
        chatId: scopeChatId,
        baseDirectory: rootPath,
        workspaceRoot: rootPath,
        sourceRoots: sourceRoots ?? [],
      }}
    >
      <div
        className="bg-surface-scene @container relative flex h-full min-h-0 min-w-0 flex-col"
        data-workbench-file-workspace
      >
        <header className="border-border-subtle h-row-navigation flex min-w-0 shrink-0 items-center gap-1 border-b px-3">
          <span
            className="text-muted-foreground type-ui min-w-0 flex-1 truncate"
            title={selected?.canonicalPath ?? rootPath}
          >
            {selected?.name ?? "/"}
          </span>
          <div
            ref={setToolbarTarget}
            className="scrollbar-hidden flex min-w-0 items-center overflow-x-auto"
          />
          {treeToggle}
        </header>
        <WorkspaceDetailSplit
          sidebarOpen={treeOpen}
          resizeLabel={t("workbench.resizeSplit")}
          main={
            <div className="relative min-h-0 min-w-0 flex-1 overflow-hidden">
              {selected ? (
                <WorkspaceFileViewer
                  key={selected.canonicalPath}
                  active={active}
                  path={selected.path}
                  rootPath={selected.rootPath}
                  fileReference={selected.reference}
                  fileView={fileViews.get(selected.canonicalPath)}
                  onFileViewChange={(view) =>
                    setFileViews((current) =>
                      new Map(current).set(selected.canonicalPath, view),
                    )
                  }
                  draftKey={draftKey(ownerKey, selected.canonicalPath)}
                  scopeChatId={scopeChatId}
                  sourceRoots={sourceRoots}
                  onOpenFile={onOpenFileReference}
                  onOpenURL={onOpenURL}
                  onAddCodeReference={onAddCodeReference}
                  toolbarTarget={toolbarTarget}
                />
              ) : (
                <div className="grid h-full place-items-center">
                  <EmptyState
                    icon={FolderOpen}
                    title={t("workspace.chooseFile")}
                    description={t("workspace.chooseFileDescription")}
                  />
                </div>
              )}
            </div>
          }
          sidebar={
            <aside
              aria-label={t("workspace.fileTree")}
              className="bg-surface-panel flex h-full min-h-0 min-w-0 flex-col"
            >
              <div className="grid shrink-0 gap-2 px-2 py-2">
                {fileSource === "project" &&
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
                      {
                        value: "working",
                        label: t("workspace.workingDirectory"),
                      },
                    ]}
                    onValueChange={setDirectorySource}
                  />
                ) : null}
                <SearchField
                  aria-label={t("workspace.searchFiles")}
                  placeholder={t("workspace.searchFilesPlaceholder")}
                  size="sm"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
              </div>
              <div className="scrollbar-hidden min-h-0 flex-1 overflow-y-auto overscroll-contain px-2 pb-2">
                {fileSource === "project" ? (
                  browseWorking ? (
                    workingRoots.length === 1 ? (
                      directoryTree(workingRoots[0] ?? "")
                    ) : (
                      <div role="tree" className="grid gap-0.5">
                        {workingRoots.map((directory) => (
                          <WorkingDirectoryRoot
                            key={directory}
                            directory={directory}
                            searching={Boolean(normalizedQuery)}
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
                        onSelect={() => selectFile(file)}
                        onDownload={() => onDownloadChatFile(file)}
                        onDelete={() => deleteChatFile(file)}
                      />
                    ))}
                  </div>
                ) : (
                  <EmptyState
                    density="compact"
                    icon={FolderOpen}
                    title={
                      normalizedQuery
                        ? t("workspace.noMatchingChatFiles")
                        : t("workspace.noChatFiles")
                    }
                  />
                )}
              </div>
            </aside>
          }
        />
      </div>
    </FileReferenceProvider>
  );
}

function WorkingDirectoryRoot({
  directory,
  searching,
  children,
}: {
  directory: string;
  searching: boolean;
  children: ReactNode;
}) {
  const [expanded, setExpanded] = useState(false);
  return (
    <div>
      <div className="flex min-w-0">
        <FileTreeRow
          directory
          expanded={expanded || searching}
          role="button"
          title={directory}
          onClick={() => setExpanded((current) => !current)}
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
