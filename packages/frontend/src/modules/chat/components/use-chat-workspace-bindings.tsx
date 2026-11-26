import { i18n } from "@/shared/i18n";
import { useWorkspaceQueryEvents } from "@/shared/api/workspace-events";
import { useQuery } from "@tanstack/react-query";
import { useCallback, useLayoutEffect, useRef } from "react";

import { useTranslation } from "react-i18next";
import { toast } from "@/shared/ui/sonner";

import type { ChatFileOpenRequest } from "./chat-attachment";
import type { ChatFileChangeOpenRequest } from "./chat-thread";
import type { ScopedGitChangesProps } from "./scoped-git-changes";
import {
  fileName,
  parseReference,
  resolveFileReference,
} from "@/shared/workspace/file-reference";

import { workspaceFilesWithGenerated } from "../model/chat-workspace-files";
import {
  matchesWorkspacePath,
  workspacePathAtRoot,
} from "../model/project-files-scope";
import type { ChatViewState } from "../model/use-chat-view-state";
import type { ChatCodeReference } from "../runtime/chat-queue-store";
import {
  deleteChatWorkspaceFile,
  downloadChatWorkspaceFile,
  listChatWorkspaceFiles,
} from "@/shared/platform";
import type { WorkspaceFile } from "@/shared/workspace/types";
import { useWorkbench } from "../workbench/workbench-store";
import { workbenchDocumentBuffers } from "../workbench/document-buffers";
import type { WorkbenchFileWorkspaceProps } from "../workbench/file/workbench-file-workspace";

type ChatWorkspaceBindingsOptions = {
  activeChatId?: string;
  additionalWorkingDirectories?: readonly string[];
  draftWorkingDirectories?: readonly string[];
  fileSource: ChatViewState["fileSource"];
  query: string;
  updateViewState: (patch: Partial<ChatViewState>) => void;
  workspaceDirectory: string;
  workspaceFiles: WorkspaceFile[];
  workspacePanelOpen: boolean;
  workspaceScopeKey: string;
  onOpenWorkbench?: () => void;
};

/** 普通与 Team 会话共用文件、链接与代码引用的 workspace adapter。 */
export function useChatWorkspaceBindings({
  activeChatId,
  additionalWorkingDirectories,
  draftWorkingDirectories,
  fileSource,
  query,
  updateViewState,
  workspaceDirectory,
  workspaceFiles,
  workspacePanelOpen,
  workspaceScopeKey,
  onOpenWorkbench,
}: ChatWorkspaceBindingsOptions) {
  const { t } = useTranslation("chat");
  const { snapshot: workbench, dispatch: dispatchWorkbench } =
    useWorkbench(workspaceScopeKey);
  const openRequest = useRef(0);
  const shouldLoadGeneratedFiles = Boolean(
    activeChatId &&
    workspaceDirectory &&
    workspacePanelOpen &&
    workbench.tabs.some(
      (tab) =>
        tab.kind === "files" && (tab.fileSource ?? fileSource) === "chat",
    ),
  );
  const chatFilesQuery = useQuery({
    queryKey: ["chat-workspace-files", activeChatId, workspaceDirectory],
    queryFn: () => {
      if (!activeChatId) throw new Error(i18n.t("common:errors.chatNotReady"));
      return listChatWorkspaceFiles(activeChatId, workspaceDirectory);
    },
    enabled: shouldLoadGeneratedFiles,
  });
  const workspaceEvents = useWorkspaceQueryEvents(
    activeChatId,
    [workspaceDirectory],
    ["chat-workspace-files"],
    shouldLoadGeneratedFiles,
  );
  const chatFiles = workspaceFilesWithGenerated(
    workspaceFiles,
    chatFilesQuery.data?.generated ?? [],
    chatFilesQuery.data?.removedPaths ?? [],
  );
  const downloadChatFile = useCallback(
    async (file: WorkspaceFile) => {
      try {
        if (!activeChatId)
          throw new Error(i18n.t("common:errors.chatNotReady"));
        await downloadChatWorkspaceFile(activeChatId, "", file.path);
      } catch {
        toast.error(t("workspace.downloadFileError"));
      }
    },
    [activeChatId, t],
  );
  const deleteChatFile = useCallback(
    async (file: WorkspaceFile) => {
      try {
        if (!activeChatId)
          throw new Error(i18n.t("common:errors.chatNotReady"));
        await deleteChatWorkspaceFile(activeChatId, "", file.path);
        await chatFilesQuery.refetch();
        return true;
      } catch {
        return false;
      }
    },
    [activeChatId, chatFilesQuery],
  );
  useLayoutEffect(() => {
    openRequest.current += 1;
    return () => {
      openRequest.current += 1;
    };
  }, [workspaceScopeKey, activeChatId]);
  const openWorkspaceFile = useCallback(
    (
      file: ChatFileOpenRequest & {
        mode?: "preview" | "diff";
        executionDiff?: { chatId: string; messageId: string };
        layout?: "tab" | "split";
        line?: number;
        endLine?: number;
      },
    ) => {
      const rootPath = file.rootPath ?? workspaceDirectory;
      const request = ++openRequest.current;
      if (file.mode === "diff") {
        dispatchWorkbench({
          type: "open-file",
          path: file.path,
          rootPath,
          title: file.name || fileName(file.path),
          mode: "diff",
          executionDiff: file.executionDiff,
        });
        onOpenWorkbench?.();
        return;
      }
      const candidate = parseReference(file.path, "path");
      if (candidate?.kind !== "file") return;
      const fileChatId = file.sourceChatId ?? activeChatId;
      const result = file.reference
        ? Promise.resolve({ status: "resolved" as const, file: file.reference })
        : resolveFileReference(candidate, {
            chatId: fileChatId,
            baseDirectory: rootPath,
            workspaceRoot: rootPath,
            sourceRoots: additionalWorkingDirectories ?? [],
          });
      void result
        .then((result) => {
          if (request !== openRequest.current) return;
          if (result.status === "directory") return;
          if (result.status !== "resolved") {
            toast.error(t(`fileReference.${result.status}`));
            return;
          }
          const startLine = file.line ?? result.file.line;
          const endLine = file.endLine ?? result.file.endLine;
          const fileRange =
            startLine !== undefined &&
            Number.isSafeInteger(startLine) &&
            startLine > 0
              ? {
                  startLine,
                  endLine:
                    endLine !== undefined && Number.isSafeInteger(endLine)
                      ? Math.max(startLine, endLine)
                      : startLine,
                }
              : undefined;
          dispatchWorkbench({
            type: "open-file",
            path: result.file.path,
            canonicalPath: result.file.canonicalPath,
            rootPath: result.file.rootPath,
            title: file.name || fileName(result.file.canonicalPath),
            mode: file.mode,
            fileRange,
            fileChatId,
          });
          onOpenWorkbench?.();
        })
        .catch(() => {
          if (request === openRequest.current)
            toast.error(t("fileReference.unavailable"));
        });
    },
    [
      dispatchWorkbench,
      activeChatId,
      onOpenWorkbench,
      workspaceDirectory,
      additionalWorkingDirectories,
      t,
    ],
  );
  const openAttachmentFile = useCallback(
    (file: ChatFileOpenRequest) => {
      openWorkspaceFile({ ...file, rootPath: file.rootPath ?? "" });
    },
    [openWorkspaceFile],
  );
  const openExecutionFileChange = useCallback(
    (file: ChatFileChangeOpenRequest) => {
      const sourceChatId = file.sourceChatId ?? activeChatId;
      if (!sourceChatId) return;
      openWorkspaceFile({
        name: file.name,
        path: file.path,
        rootPath: file.rootPath,
        mode: "diff",
        executionDiff: {
          chatId: sourceChatId,
          messageId: file.messageId,
        },
      });
    },
    [activeChatId, openWorkspaceFile],
  );
  const openBrowserLink = useCallback(
    (url: string) => {
      const target = parseReference(url, "link");
      if (!target) return false;
      if (target.kind === "web") {
        openRequest.current += 1;
        dispatchWorkbench({
          type: "open-browser",
          title: t("browser"),
          url: target.url,
          rootPath: workspaceDirectory,
        });
        onOpenWorkbench?.();
        return true;
      }
      return false;
    },
    [dispatchWorkbench, onOpenWorkbench, t, workspaceDirectory],
  );
  const addCodeReference = useCallback(
    (reference: ChatCodeReference) => {
      updateViewState({ codeReferenceRequest: reference });
    },
    [updateViewState],
  );
  const openCodeReference = useCallback(
    (reference: ChatCodeReference) => {
      if (!reference.path) return;
      openWorkspaceFile({
        name: fileName(reference.path),
        path: reference.path,
        mode: "preview",
        line: reference.startLine,
        endLine: reference.endLine,
      });
    },
    [openWorkspaceFile],
  );
  const fileWorkspaceProps: Omit<
    WorkbenchFileWorkspaceProps,
    "active" | "file" | "onSelectFile"
  > = {
    rootPath: workspaceDirectory,
    scopeChatId: activeChatId,
    scopePath: "",
    sourceRoots: activeChatId
      ? additionalWorkingDirectories
      : (draftWorkingDirectories ?? additionalWorkingDirectories),
    spaceRootAvailable: Boolean(activeChatId),
    chatFiles,
    fileSource,
    chatFilesLoading: shouldLoadGeneratedFiles && chatFilesQuery.isPending,
    chatFilesError:
      shouldLoadGeneratedFiles &&
      (chatFilesQuery.isError || Boolean(workspaceEvents.error)),
    onRetryChatFiles: () => {
      workspaceEvents.retry();
      void chatFilesQuery.refetch();
    },
    onFileSourceChange: (source) => updateViewState({ fileSource: source }),
    onDownloadChatFile: downloadChatFile,
    onDeleteChatFile: deleteChatFile,
    isPathDirty: (path, file, affectedRoot) =>
      workbench.tabs.some(
        (tab) =>
          tab.kind === "file" &&
          matchesWorkspacePath(
            tab.canonicalPath ?? workspacePathAtRoot(tab.rootPath, tab.path),
            file?.canonicalPath ??
              workspacePathAtRoot(affectedRoot ?? workspaceDirectory, path),
            true,
          ) &&
          workbenchDocumentBuffers.isDirty(`${workspaceScopeKey}:${tab.id}`),
      ),
    onPathRenamed: (oldPath, newPath, name, file, directory, affectedRoot) =>
      dispatchWorkbench({
        type: "rename-file",
        rootPath: file?.rootPath ?? affectedRoot ?? workspaceDirectory,
        oldPath: file?.path ?? oldPath,
        newPath: directory
          ? newPath
          : (file?.path.replace(/[^/\\]+$/, name) ?? newPath),
        oldCanonicalPath:
          file?.canonicalPath ??
          workspacePathAtRoot(affectedRoot ?? workspaceDirectory, oldPath),
        newCanonicalPath:
          file?.canonicalPath.replace(/[^/\\]+$/, name) ??
          workspacePathAtRoot(affectedRoot ?? workspaceDirectory, newPath),
        title: name,
        directory,
      }),
    onPathDeleted: (path, file, directory, affectedRoot) =>
      dispatchWorkbench({
        type: "delete-file",
        rootPath: file?.rootPath ?? affectedRoot ?? workspaceDirectory,
        path: file?.path ?? path,
        canonicalPath:
          file?.canonicalPath ??
          workspacePathAtRoot(affectedRoot ?? workspaceDirectory, path),
        directory,
      }),
  };
  const changesProps: Omit<ScopedGitChangesProps, "active"> = {
    query,
    onQueryChange: (nextQuery) => updateViewState({ query: nextQuery }),
    rootPath: workspaceDirectory,
    scopeChatId: activeChatId,
    sourceRoots: activeChatId
      ? additionalWorkingDirectories
      : (draftWorkingDirectories ?? additionalWorkingDirectories),
  };

  return {
    addCodeReference,
    openAttachmentFile,
    openBrowserLink,
    openCodeReference,
    openExecutionFileChange,
    fileWorkspaceProps,
    changesProps,
  };
}
