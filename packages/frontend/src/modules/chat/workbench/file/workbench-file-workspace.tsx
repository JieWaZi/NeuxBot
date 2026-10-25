import { useQueryClient } from "@tanstack/react-query";
import {
  Download,
  Folders,
  FolderOpen,
  MoreHorizontal,
  Trash2,
} from "lucide-react";
import { useEffect, useRef, useState, type DragEvent } from "react";
import { useTranslation } from "react-i18next";

import { WorkspaceFileBrowser } from "../../components/workspace-file-browser";
import { WorkspaceFileViewer } from "../../components/workspace-file-viewer";
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
import { toast } from "@/shared/ui/sonner";

import { workbenchDocumentBuffers } from "../document-buffers";

export type FileWorkspaceSelection = {
  name: string;
  sourcePath?: string;
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

export function WorkbenchFileWorkspace({
  active = true,
  ownerKey,
  rootPath,
  scopeChatId,
  scopePath,
  sourceRoots,
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
  const selectRequest = useRef(0);
  const visitedFiles = useRef(new Map<string, FileWorkspaceSelection>());
  useEffect(() => {
    if (selected) visitedFiles.current.set(selected.canonicalPath, selected);
  }, [selected]);

  const selectFile = (file: { name: string; path: string }) => {
    const candidate = parseReference(file.path, "path");
    if (candidate?.kind !== "file") return;
    const request = ++selectRequest.current;
    void resolveFileReference(candidate, {
      chatId: scopeChatId,
      baseDirectory: rootPath,
      workspaceRoot: rootPath,
      sourceRoots: sourceRoots ?? [],
    })
      .then((result) => {
        if (request !== selectRequest.current) return;
        if (result.status !== "resolved") {
          toast.error(t("fileReference.unavailable"));
          return;
        }
        const next = {
          name: file.name || fileName(result.file.canonicalPath),
          sourcePath: file.path,
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
    for (const opened of visitedFiles.current.values()) {
      if (
        (file?.canonicalPath === opened.canonicalPath ||
          opened.sourcePath === path ||
          opened.sourcePath?.startsWith(`${path}/`) ||
          (!file && !opened.sourcePath)) &&
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
    if (
      selected &&
      (file?.canonicalPath === selected.canonicalPath ||
        selected.sourcePath === path ||
        (directory && selected.sourcePath?.startsWith(`${path}/`)) ||
        (directory && !selected.sourcePath))
    ) {
      setSelected(undefined);
      onSelectedFileChange?.(undefined);
    }
  };

  const handlePathRenamed: NonNullable<
    WorkbenchFileWorkspaceProps["onPathRenamed"]
  > = (oldPath, newPath, name, file, directory, affectedRoot) => {
    onPathRenamed?.(oldPath, newPath, name, file, directory, affectedRoot);
    if (
      !selected ||
      (file?.canonicalPath !== selected.canonicalPath &&
        selected.sourcePath !== oldPath &&
        !(directory && selected.sourcePath?.startsWith(`${oldPath}/`)))
    )
      return;
    const suffix = selected.sourcePath?.slice(oldPath.length) ?? "";
    selectFile({
      name: directory ? selected.name : name,
      path: `${newPath}${suffix}`,
    });
  };

  const handleDrop = (event: DragEvent<HTMLDivElement>) => {
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
      scopePath,
      Array.from(event.dataTransfer.files),
      rootPath,
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
            queryKey: ["workspace-directory", scopeChatId, rootPath],
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
            <div className="relative min-h-0 min-w-0 overflow-hidden">
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
                  <div
                    className={
                      dropActive
                        ? "outline-accent-default outline-2"
                        : undefined
                    }
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
                    onDrop={handleDrop}
                  >
                    <WorkspaceFileBrowser
                      active={active}
                      activePath={selected?.canonicalPath}
                      isPathDirty={isDirtyPath}
                      onOpenFile={selectFile}
                      onPathDeleted={handlePathDeleted}
                      onPathRenamed={handlePathRenamed}
                      query={query}
                      rootPath={rootPath}
                      scopeChatId={scopeChatId}
                      scopePath={scopePath}
                    />
                  </div>
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
                        onDelete={async () => {
                          const deleted = await onDeleteChatFile(file);
                          if (deleted)
                            handlePathDeleted(
                              file.path,
                              undefined,
                              false,
                              rootPath,
                            );
                          return deleted;
                        }}
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

function ChatFileRow({
  file,
  onSelect,
  onDownload,
  onDelete,
}: {
  file: WorkspaceFile;
  onSelect: () => void;
  onDownload: () => Promise<void>;
  onDelete: () => Promise<boolean>;
}) {
  const { t } = useTranslation("chat");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState(false);
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
            className="text-destructive"
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
          if (open) setDeleteError(false);
        }}
      >
        <DialogContent
          size="sm"
          title={t("workspace.deleteFile")}
          description={t("workspace.deleteFileConfirm", { name: file.name })}
        >
          {deleteError ? (
            <p role="alert" className="text-destructive type-body px-6 pb-4">
              {t("workspace.deleteFileError")}
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
                setDeleteError(false);
                void onDelete().then((deleted) => {
                  setDeleting(false);
                  if (deleted) setDeleteOpen(false);
                  else setDeleteError(true);
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
