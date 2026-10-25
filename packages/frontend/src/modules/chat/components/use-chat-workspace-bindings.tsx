import { useWorkspaceQueryEvents } from "@/shared/api/workspace-events";
import { useQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useRef } from "react";

import { useTranslation } from "react-i18next";
import { toast } from "@/shared/ui/sonner";

import type { ChatFileOpenRequest } from "./chat-attachment";
import type { ChatFileChangeOpenRequest } from "./chat-thread";
import type { ScopedGitChangesProps } from "./scoped-git-changes";
import {
  fileName,
  parseReference,
  resolveFileReference,
  type ResolvedFileReference,
} from "@/shared/workspace/file-reference";

import { projectFilesScopePath } from "../model/project-files-scope";
import { workspaceFilesWithGenerated } from "../model/chat-workspace-files";
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
  fileSource: ChatViewState["fileSource"];
  query: string;
  terminalWorkingDirectory?: string;
  updateViewState: (patch: Partial<ChatViewState>) => void;
  workspaceDirectory: string;
  workspaceFiles: WorkspaceFile[];
  workspacePanelOpen: boolean;
  workspaceScopeKey: string;
  onOpenWorkbench?: () => void;
};

/** 普通与 Team 会话共用文件、链接、代码引用和终端的 workspace adapter。 */
export function useChatWorkspaceBindings({
  activeChatId,
  additionalWorkingDirectories,
  fileSource,
  query,
  terminalWorkingDirectory,
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
    fileSource === "chat",
  );
  const chatFilesQuery = useQuery({
    queryKey: ["chat-workspace-files", activeChatId, workspaceDirectory],
    queryFn: () => {
      if (!activeChatId) throw new Error("Chat 尚未就绪");
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
        if (!activeChatId) throw new Error("Chat 尚未就绪");
        await downloadChatWorkspaceFile(
          activeChatId,
          workspaceDirectory,
          file.path,
        );
      } catch {
        toast.error(t("workspace.downloadFileError"));
      }
    },
    [activeChatId, t, workspaceDirectory],
  );
  const deleteChatFile = useCallback(
    async (file: WorkspaceFile) => {
      try {
        if (!activeChatId) throw new Error("Chat 尚未就绪");
        await deleteChatWorkspaceFile(
          activeChatId,
          workspaceDirectory,
          file.path,
        );
        await chatFilesQuery.refetch();
        return true;
      } catch {
        return false;
      }
    },
    [activeChatId, chatFilesQuery, workspaceDirectory],
  );
  useEffect(() => {
    openRequest.current += 1;
    return () => {
      openRequest.current += 1;
    };
  }, [workspaceScopeKey]);
  const openWorkspaceFile = useCallback(
    (
      file: ChatFileOpenRequest & {
        mode?: "preview" | "diff";
        executionDiff?: { chatId: string; messageId: string };
        layout?: "tab" | "split";
      },
      onResolved?: (reference: ResolvedFileReference) => void,
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
      const result = file.reference
        ? Promise.resolve({ status: "resolved" as const, file: file.reference })
        : resolveFileReference(candidate, {
            chatId: activeChatId,
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
          onResolved?.(result.file);
          dispatchWorkbench({
            type: "open-file",
            path: result.file.path,
            canonicalPath: result.file.canonicalPath,
            rootPath: result.file.rootPath,
            title: file.name || fileName(result.file.canonicalPath),
            mode: file.mode,
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
      openWorkspaceFile(file);
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
  const openChatTerminal = useCallback(() => {
    if (!terminalWorkingDirectory) return;
    openRequest.current += 1;
    dispatchWorkbench({
      type: "open-terminal",
      rootPath: terminalWorkingDirectory,
      title: t("terminal"),
    });
    onOpenWorkbench?.();
  }, [dispatchWorkbench, onOpenWorkbench, t, terminalWorkingDirectory]);
  const addCodeReference = useCallback(
    (reference: ChatCodeReference) => {
      updateViewState({ codeReferenceRequest: reference });
    },
    [updateViewState],
  );
  const openCodeReference = useCallback(
    (reference: ChatCodeReference) => {
      if (!reference.path) return;
      openWorkspaceFile(
        {
          name: fileName(reference.path),
          path: reference.path,
          mode: "preview",
        },
        (file) => {
          updateViewState({
            openedCodeReference: { ...reference, path: file.canonicalPath },
          });
        },
      );
    },
    [openWorkspaceFile, updateViewState],
  );
  const fileWorkspaceProps: Omit<
    WorkbenchFileWorkspaceProps,
    "active" | "ownerKey"
  > = {
    rootPath: workspaceDirectory,
    scopeChatId: activeChatId,
    scopePath: projectFilesScopePath(activeChatId),
    sourceRoots: additionalWorkingDirectories,
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
    onOpenFileReference: (file) =>
      openWorkspaceFile({
        name: fileName(file.canonicalPath),
        path: file.path,
        rootPath: file.rootPath,
        reference: file,
      }),
    onOpenURL: openBrowserLink,
    onAddCodeReference: addCodeReference,
    isPathDirty: (path, file, affectedRoot) =>
      workbench.tabs.some(
        (tab) =>
          tab.kind === "file" &&
          ((file && tab.canonicalPath === file.canonicalPath) ||
            (tab.rootPath === (affectedRoot ?? workspaceDirectory) &&
              (tab.path === path || tab.path?.startsWith(`${path}/`)))) &&
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
        oldCanonicalPath: file?.canonicalPath,
        newCanonicalPath: file?.canonicalPath.replace(/[^/\\]+$/, name),
        title: name,
        directory,
      }),
    onPathDeleted: (path, file, directory, affectedRoot) =>
      dispatchWorkbench({
        type: "delete-file",
        rootPath: file?.rootPath ?? affectedRoot ?? workspaceDirectory,
        path: file?.path ?? path,
        canonicalPath: file?.canonicalPath,
        directory,
      }),
  };
  const changesProps: Omit<ScopedGitChangesProps, "active"> = {
    query,
    onQueryChange: (nextQuery) => updateViewState({ query: nextQuery }),
    rootPath: workspaceDirectory,
    scopeChatId: activeChatId,
    sourceRoots: additionalWorkingDirectories,
  };

  return {
    addCodeReference,
    openAttachmentFile,
    openBrowserLink,
    openChatTerminal,
    openCodeReference,
    openExecutionFileChange,
    fileWorkspaceProps,
    changesProps,
  };
}
