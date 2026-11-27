import {
  Activity,
  lazy,
  Suspense,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import {
  Columns3,
  FileDiff,
  Files,
  Globe,
  Maximize2,
  MessagesSquare,
  Minimize2,
  PanelRight,
  Plus,
  TerminalSquare,
} from "lucide-react";
import { useTranslation } from "react-i18next";

import { ChatBrowserView } from "../components/chat-browser-view";
import { closeTerminal } from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import {
  useSceneChromeAccessoryTarget,
  useSceneChromeAccessoryWidthSetter,
} from "@/shared/ui/scene-chrome-portal";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { ContentLoadingState } from "@/shared/ui/content-loading-state";
import { toast } from "@/shared/ui/sonner";
import { cn } from "@/shared/lib/cn";
import {
  fileName,
  resolveFileReference,
  type ResolvedFileReference,
} from "@/shared/workspace/file-reference";
import {
  matchesWorkspacePath,
  workspacePathAtRoot,
} from "../model/project-files-scope";
import type { ChatCodeReference } from "../runtime/chat-queue-store";
import { useWorkbench } from "./workbench-store";
import {
  workbenchPaneById,
  workbenchPaneWithTab,
  type WorkbenchTab,
} from "./workbench-model";
import type { WorkbenchDropZone, WorkbenchPane } from "./workbench-pane";
import {
  WorkbenchPaneCanvas,
  WorkbenchPaneTabStrip,
} from "./workbench-pane-canvas";
import { paneHeaderGeometry } from "./pane-geometry";
import { workbenchDocumentBuffers } from "./document-buffers";
import type { WorkbenchFileWorkspaceProps } from "./file/workbench-file-workspace";
import type { ScopedGitChangesProps } from "../components/scoped-git-changes";
import { WorkflowRunWorkbenchView } from "./workflow-run-workbench-view";
import { workflowRunContentType } from "./workflow-run-reference";

const WorkspaceFileViewer = lazy(async () => ({
  default: (await import("../components/workspace-file-viewer"))
    .WorkspaceFileViewer,
}));
const WorkbenchFileWorkspace = lazy(async () => ({
  default: (await import("./file/workbench-file-workspace"))
    .WorkbenchFileWorkspace,
}));
const ScopedGitChanges = lazy(async () => ({
  default: (await import("../components/scoped-git-changes")).ScopedGitChanges,
}));
const ChatTerminalPanel = lazy(async () => ({
  default: (await import("../components/chat-terminal-panel"))
    .ChatTerminalPanel,
}));
const SideChatWorkbenchView = lazy(async () => ({
  default: (await import("./side-chat-workbench-view")).SideChatWorkbenchView,
}));

export type WorkbenchContentTab = {
  contentId: string;
  title: string;
  content:
    | ReactNode
    | ((active: boolean, context?: { scopeKey: string }) => ReactNode);
};

type RightWorkbenchProps = {
  parentBusy: boolean;
  chatHistory?: ReactNode;
  contentTab?: WorkbenchContentTab;
  displayWidth?: number;
  animateLayout?: boolean;
  onAddCodeReference?: (reference: ChatCodeReference) => void;
  defaultFileSource?: "project" | "chat";
  allowSideChat?: boolean;
  onDismiss: () => void;
  onOpenFile?: (file: ResolvedFileReference, sourceChatId?: string) => void;
  onOpenURL?: (url: string) => unknown;
  taskBoard?: ReactNode;
  onEmpty: () => void;
  scopeKey: string;
  sourceRoots?: readonly string[];
  parentChatId?: string;
  terminalWorkingDirectory?: string;
  workspaceDirectory: string;
  fileWorkspaceProps?: Omit<
    WorkbenchFileWorkspaceProps,
    "active" | "file" | "onSelectFile"
  >;
  changesProps?: Omit<ScopedGitChangesProps, "active" | "onOpenFile">;
  visible: boolean;
};

export function RightWorkbench({
  chatHistory,
  contentTab,
  displayWidth,
  animateLayout = false,
  onAddCodeReference,
  defaultFileSource = "project",
  allowSideChat = true,
  onDismiss,
  onOpenFile,
  onOpenURL,
  taskBoard,
  onEmpty,
  scopeKey,
  sourceRoots,
  parentChatId,
  parentBusy,
  terminalWorkingDirectory,
  workspaceDirectory,
  fileWorkspaceProps,
  changesProps,
  visible,
}: RightWorkbenchProps) {
  const { t } = useTranslation("chat");
  const chromeAccessoryTarget = useSceneChromeAccessoryTarget();
  const setChromeAccessoryWidth = useSceneChromeAccessoryWidthSetter();
  const { snapshot, getSnapshot, retainScope, dispatch } =
    useWorkbench(scopeKey);
  useLayoutEffect(() => {
    if (!setChromeAccessoryWidth || !visible) return;
    setChromeAccessoryWidth(
      scopeKey,
      displayWidth ?? snapshot.width,
      snapshot.fullscreen,
      snapshot.side,
    );
    return () => setChromeAccessoryWidth(scopeKey, null);
  }, [
    scopeKey,
    setChromeAccessoryWidth,
    snapshot.fullscreen,
    snapshot.side,
    snapshot.width,
    displayWidth,
    visible,
  ]);

  const draggedTabId = useRef("");
  const [draggingTab, setDraggingTab] = useState(false);
  const [pendingClose, setPendingClose] = useState<{
    scopeKey: string;
    tabs: WorkbenchTab[];
    dirtyTabs: WorkbenchTab[];
  }>();
  const closingTerminals = useRef(new Map<string, Promise<void>>());
  const snapshotRef = useRef(snapshot);
  const scopeRef = useRef(scopeKey);
  const visibleRef = useRef(visible);
  const fileOpenRequest = useRef(0);
  const fileScopeChatId = fileWorkspaceProps?.scopeChatId ?? parentChatId;
  const gitScopeChatId = changesProps?.scopeChatId ?? parentChatId;
  useLayoutEffect(
    () => () => {
      fileOpenRequest.current += 1;
    },
    [scopeKey, fileScopeChatId, gitScopeChatId],
  );
  useLayoutEffect(() => {
    snapshotRef.current = snapshot;
    scopeRef.current = scopeKey;
    visibleRef.current = visible;
  }, [scopeKey, snapshot, visible]);
  const chromePanes = paneHeaderGeometry(snapshot.layout);
  const closeConfirmation =
    pendingClose?.scopeKey === scopeKey ? pendingClose : undefined;
  const finishClose = (tab: WorkbenchTab) => {
    const current = getSnapshot();
    if (
      !current.tabs.some(
        (item) =>
          item.id === tab.id &&
          item.occurrenceId === tab.occurrenceId &&
          item.closeable !== false,
      )
    )
      return;
    const draftKey = `${scopeKey}:${tab.id}`;
    workbenchDocumentBuffers.discard(draftKey);
    if (tab.kind === "files")
      workbenchDocumentBuffers.discardPrefix(`${draftKey}:file:`);
    dispatch({ type: "close", tabId: tab.id });
    if (current.tabs.length === 1 && scopeRef.current === scopeKey) onEmpty();
  };
  const closeTabs = (
    requested: readonly WorkbenchTab[],
    confirmed: readonly WorkbenchTab[] = [],
  ) => {
    const current = getSnapshot();
    const tabs = requested.filter((tab) =>
      current.tabs.some(
        (item) =>
          item.id === tab.id &&
          item.occurrenceId === tab.occurrenceId &&
          item.closeable !== false,
      ),
    );
    const dirtyTabs = tabs.filter((tab) => {
      if (confirmed.includes(tab)) return false;
      const draftKey = `${scopeKey}:${tab.id}`;
      return (
        workbenchDocumentBuffers.isDirty(draftKey) ||
        (tab.kind === "files" &&
          workbenchDocumentBuffers.hasDirtyPrefix(`${draftKey}:file:`))
      );
    });
    if (dirtyTabs.length) {
      setPendingClose({
        scopeKey,
        tabs,
        dirtyTabs: [...confirmed, ...dirtyTabs],
      });
      return;
    }

    const terminals: Promise<void>[] = [];
    for (const tab of tabs) {
      if (tab.kind !== "terminal") {
        finishClose(tab);
        continue;
      }
      let close = closingTerminals.current.get(tab.id);
      if (!close) {
        const releaseScope = retainScope();
        close = closeTerminal(tab.id)
          .then(() => finishClose(tab))
          .catch((cause: unknown) => {
            toast.error(cause instanceof Error ? cause.message : String(cause));
          })
          .finally(() => {
            releaseScope();
            closingTerminals.current.delete(tab.id);
          });
        closingTerminals.current.set(tab.id, close);
      }
      terminals.push(close);
    }
    if (terminals.length) return Promise.all(terminals).then(() => undefined);
  };
  const openPage = (kind: "files" | "changes") => {
    if (kind === "files") {
      fileWorkspaceProps?.onFileSourceChange?.(defaultFileSource);
      dispatch({
        type: "open-page",
        kind,
        fileSource: defaultFileSource,
        title: t("workspace.files"),
      });
    } else {
      dispatch({ type: "open-page", kind, title: t("workspace.diffTab") });
    }
  };
  const openScopedFile = async (
    origin: WorkbenchTab,
    file: { name: string; path: string; sourceChatId?: string },
    rootPath?: string,
    replaceBrowser = false,
  ) => {
    if (!file.path) return;
    const path = workspacePathAtRoot(rootPath, file.path) ?? file.path;
    const chatId =
      file.sourceChatId ??
      (origin.kind === "changes" ? gitScopeChatId : fileScopeChatId);
    const request = ++fileOpenRequest.current;
    const pending = () =>
      request === fileOpenRequest.current &&
      snapshotRef.current.tabs.some((tab) => tab.id === origin.id);
    try {
      const result = await resolveFileReference(
        { kind: "file", path, raw: path },
        {
          chatId,
          baseDirectory: rootPath ?? "",
          workspaceRoot: rootPath ?? "",
          sourceRoots: rootPath
            ? (fileWorkspaceProps?.sourceRoots ?? sourceRoots ?? [])
            : [],
        },
      );
      if (!pending()) return;
      if (result.status !== "resolved") {
        toast.error(t("fileReference.unavailable"));
        return;
      }
      const draftKey = `${scopeKey}:${origin.id}`;
      const pane = workbenchPaneWithTab(snapshotRef.current.layout, origin.id);
      const replaceFilesTabId =
        replaceBrowser &&
        pane?.activeTabId === origin.id &&
        !workbenchDocumentBuffers.isDirty(draftKey) &&
        !workbenchDocumentBuffers.hasDirtyPrefix(`${draftKey}:file:`)
          ? origin.id
          : undefined;
      if (replaceFilesTabId) {
        workbenchDocumentBuffers.discard(draftKey);
        workbenchDocumentBuffers.discardPrefix(`${draftKey}:file:`);
      }
      const focus = document.activeElement;
      const focusFromOrigin = document
        .getElementById(`workbench-panel-${origin.id}`)
        ?.contains(focus);
      dispatch({
        type: "open-file",
        path: result.file.path,
        rootPath: result.file.rootPath,
        canonicalPath: result.file.canonicalPath,
        fileChatId: chatId,
        fileSource: rootPath ? "project" : "chat",
        title: file.name || result.file.name,
        replaceFilesTabId,
        paneId: pane?.id,
      });
      if (focusFromOrigin)
        requestAnimationFrame(() => {
          if (
            !visibleRef.current ||
            request !== fileOpenRequest.current ||
            (document.activeElement !== focus &&
              document.activeElement !== document.body)
          )
            return;
          const { layout, focusedPaneId, tabs } = snapshotRef.current;
          const targetId = workbenchPaneById(
            layout,
            focusedPaneId,
          )?.activeTabId;
          if (
            targetId !== origin.id &&
            tabs.some(
              (tab) =>
                tab.id === targetId &&
                tab.kind === "file" &&
                tab.canonicalPath === result.file.canonicalPath,
            )
          )
            document.getElementById(`workbench-tab-${targetId}`)?.focus();
        });
    } catch {
      if (pending()) toast.error(t("fileReference.unavailable"));
    }
  };
  const openBrowser = (paneId: string) =>
    dispatch({
      type: "open-browser",
      title: t("workbench.newTab"),
      newTab: true,
      rootPath: workspaceDirectory,
      paneId,
    });
  const openTerminal =
    parentChatId && terminalWorkingDirectory
      ? () =>
          dispatch({
            type: "open-terminal",
            title: t("terminal"),
            rootPath: terminalWorkingDirectory,
          })
      : undefined;
  const openSideChat =
    parentChatId && allowSideChat
      ? () =>
          dispatch({
            type: "open-sidechat",
            title: t("workbench.sideChat"),
            parentChatId,
          })
      : undefined;
  const openTaskBoard = (paneId?: string) =>
    dispatch({
      type: "open-task-board",
      title: t("team:taskBoard.boardTitle"),
      paneId,
    });
  const focusTab = (tab: WorkbenchTab) => {
    dispatch({ type: "focus", tabId: tab.id });
  };
  const renderPaneTabs = (pane: WorkbenchPane) => (
    <WorkbenchPaneTabStrip
      active={visible && snapshot.focusedPaneId === pane.id}
      scopeKey={scopeKey}
      pane={pane}
      tabs={snapshot.tabs}
      draggedTabId={draggedTabId}
      onClose={(tab) => closeTabs([tab])}
      onCloseOthers={(tab) => {
        void closeTabs(getSnapshot().tabs.filter((item) => item.id !== tab.id));
      }}
      onCloseAll={() => {
        void closeTabs(getSnapshot().tabs);
      }}
      onDragStateChange={setDraggingTab}
      onFocus={focusTab}
      onFocusPane={(paneId) => {
        if (snapshotRef.current.focusedPaneId !== paneId)
          dispatch({ type: "focus-pane", paneId });
      }}
      onMove={(tabId, paneId, beforeTabId) =>
        dispatch({
          type: "move-tab",
          tabId,
          targetPaneId: paneId,
          zone: "center",
          beforeTabId,
        })
      }
      onSplit={(tabId, paneId, zone) =>
        dispatch({
          type: "move-tab",
          tabId,
          targetPaneId: paneId,
          zone,
        })
      }
      trailing={
        <DropdownMenu
          onOpenChange={(open) => {
            if (open) dispatch({ type: "focus-pane", paneId: pane.id });
          }}
        >
          <DropdownMenuTrigger asChild>
            <Button
              aria-label={t("workbench.addTab")}
              title={t("workbench.addTab")}
              size="icon"
              variant="ghost"
              className="shrink-0"
            >
              <Plus />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={() => openBrowser(pane.id)}>
              <Globe />
              {t("browser")}
            </DropdownMenuItem>
            {openTerminal ? (
              <DropdownMenuItem onSelect={openTerminal}>
                <TerminalSquare />
                {t("terminal")}
              </DropdownMenuItem>
            ) : null}
            {fileWorkspaceProps ? (
              <DropdownMenuItem onSelect={() => openPage("files")}>
                <Files />
                {t("workspace.files")}
              </DropdownMenuItem>
            ) : null}
            {changesProps ? (
              <DropdownMenuItem onSelect={() => openPage("changes")}>
                <FileDiff />
                {t("workspace.diffTab")}
              </DropdownMenuItem>
            ) : null}
            {openSideChat ? (
              <DropdownMenuItem onSelect={openSideChat}>
                <MessagesSquare />
                {t("workbench.sideChat")}
              </DropdownMenuItem>
            ) : null}
            {taskBoard ? (
              <DropdownMenuItem onSelect={() => openTaskBoard(pane.id)}>
                <Columns3 />
                {t("team:taskBoard.boardTitle")}
              </DropdownMenuItem>
            ) : null}
          </DropdownMenuContent>
        </DropdownMenu>
      }
    />
  );

  const tabHeader = (
    <header
      className={cn(
        "bg-surface-chrome relative flex h-(--shell-titlebar-height) shrink-0 items-center overflow-hidden",
        chromeAccessoryTarget && visible && "min-w-0 flex-1",
      )}
      data-workbench-chrome
      data-workbench-layout-motion={animateLayout || undefined}
    >
      {chromePanes.map(({ pane, left, width }, index) => (
        <div
          key={pane.id}
          data-workbench-pane-header={pane.id}
          className="workbench-pane-header absolute inset-y-0 flex min-w-0 items-center"
          style={{
            left: `${left}%`,
            width: `${width}%`,
            ...(index === chromePanes.length - 1
              ? {
                  paddingRight: "calc(2 * var(--spacing-control-md) + 12px)",
                }
              : {}),
          }}
        >
          {index > 0 ? (
            <span
              aria-hidden="true"
              className="bg-shell-separator pointer-events-none absolute top-1.5 bottom-1.5 left-0 w-px"
            />
          ) : null}
          {renderPaneTabs(pane)}
        </div>
      ))}
      <div
        className="absolute inset-y-0 right-2 z-20 flex items-center gap-1"
        data-desktop-no-drag-region
      >
        <Button
          aria-label={
            snapshot.fullscreen
              ? t("workbench.exitFullView")
              : t("workbench.fullView")
          }
          aria-pressed={snapshot.fullscreen}
          size="icon"
          title={
            snapshot.fullscreen
              ? t("workbench.exitFullView")
              : t("workbench.fullView")
          }
          variant="ghost"
          onClick={() =>
            dispatch({
              type: "set-fullscreen",
              fullscreen: !snapshot.fullscreen,
            })
          }
        >
          {snapshot.fullscreen ? <Minimize2 /> : <Maximize2 />}
        </Button>
        <Button
          aria-label={t("closeRightPanel")}
          onClick={onDismiss}
          size="icon"
          title={t("closeRightPanel")}
          type="button"
          variant="ghost"
        >
          <PanelRight aria-hidden="true" strokeWidth={1.5} />
        </Button>
      </div>
    </header>
  );

  const renderTab = (tab: WorkbenchTab, active: boolean) => {
    if (tab.kind === "chat-history")
      return active && visible ? chatHistory : null;
    if (tab.kind === "content") {
      if (tab.contentReference?.type === workflowRunContentType)
        return (
          <WorkflowRunWorkbenchView
            active={active && visible}
            navigationRevision={tab.navigationRevision}
            reference={tab.contentReference}
            scopeKey={scopeKey}
            title={tab.title}
          />
        );
      if (tab.contentId !== contentTab?.contentId) return null;
      return typeof contentTab.content === "function"
        ? contentTab.content(active && visible, { scopeKey })
        : contentTab.content;
    }
    if (tab.kind === "files") {
      const fileSource = tab.fileSource ?? fileWorkspaceProps?.fileSource;
      return fileWorkspaceProps && active && visible ? (
        <Suspense
          fallback={
            <ContentLoadingState
              label={t("common:pageLoading")}
              className="h-full w-full"
            />
          }
        >
          <WorkbenchFileWorkspace
            key={fileSource}
            {...fileWorkspaceProps}
            fileSource={fileSource ?? "project"}
            onSelectFile={(file, root) =>
              void openScopedFile(tab, file, root, fileSource === "project")
            }
          />
        </Suspense>
      ) : null;
    }
    if (tab.kind === "changes")
      return changesProps ? (
        <WorkbenchDiff
          key={JSON.stringify([
            scopeKey,
            changesProps.preferredRoot,
            changesProps.preferredRepository,
            tab.navigationRevision,
          ])}
          {...changesProps}
          active={active && visible}
          onOpenFile={(root, path) =>
            void openScopedFile(tab, { name: fileName(path), path }, root)
          }
        />
      ) : null;
    if (tab.kind === "browser")
      return (
        <ChatBrowserView
          active={active && visible}
          chatId={parentChatId}
          onOpenTerminal={openTerminal}
          onOpenSideChat={openSideChat}
          onOpenTool={(kind) => {
            if (kind === "files" || kind === "changes") openPage(kind);
            else
              openTaskBoard(workbenchPaneWithTab(snapshot.layout, tab.id)?.id);
          }}
          showBoardTool={Boolean(taskBoard)}
          onURLChange={(url) =>
            dispatch({ type: "update-browser-url", tabId: tab.id, url })
          }
          onTitleChange={(title) =>
            dispatch({ type: "update-browser-title", tabId: tab.id, title })
          }
          url={tab.url ?? ""}
          workspaceRoot={tab.rootPath}
        />
      );
    if (tab.kind === "task-board") return taskBoard;
    if (
      tab.kind === "terminal" &&
      terminalWorkingDirectory !== undefined &&
      parentChatId &&
      tab.rootPath
    )
      return (
        <Suspense
          fallback={
            <ContentLoadingState
              label={t("common:pageLoading")}
              className="h-full w-full"
            />
          }
        >
          <ChatTerminalPanel
            chatId={parentChatId}
            onSessionBound={(sessionId) =>
              dispatch({ type: "bind-terminal", tabId: tab.id, sessionId })
            }
            ownerKey={tab.id}
            sessionId={tab.terminalSessionId}
            workingDirectory={tab.rootPath}
          />
        </Suspense>
      );
    if (tab.kind === "file" && tab.path && tab.rootPath) {
      if (!active || !visible) return null;
      const { path, rootPath } = tab;
      const chatId = tab.fileChatId ?? fileScopeChatId;
      const canonicalPath =
        tab.canonicalPath ?? workspacePathAtRoot(rootPath, path) ?? path;
      const isProjectFile =
        tab.fileMode !== "diff" &&
        !tab.executionDiff &&
        fileWorkspaceProps &&
        [
          fileWorkspaceProps.rootPath,
          ...(fileWorkspaceProps.sourceRoots ?? []),
        ].some((root) => matchesWorkspacePath(canonicalPath, root, true));
      const renderFile = (toolbarTarget?: HTMLDivElement | null) => (
        <WorkspaceFileViewer
          highlightLines={tab.fileRange}
          onAddCodeReference={onAddCodeReference}
          onOpenFile={onOpenFile && ((file) => onOpenFile(file, chatId))}
          onOpenURL={onOpenURL}
          path={path}
          rootPath={rootPath}
          fileReference={
            tab.canonicalPath
              ? {
                  path,
                  rootPath,
                  canonicalPath,
                  name: tab.title,
                  displayPath: path,
                }
              : undefined
          }
          draftKey={`${scopeKey}:${tab.id}`}
          scopeChatId={chatId}
          sourceRoots={fileWorkspaceProps?.sourceRoots ?? sourceRoots}
          mode={tab.fileMode}
          executionDiff={tab.executionDiff}
          toolbarTarget={toolbarTarget}
          showLineWrap={isProjectFile}
          projectPreview={isProjectFile && tab.fileSource !== "chat"}
        />
      );
      return (
        <Suspense
          fallback={
            <ContentLoadingState
              label={t("common:pageLoading")}
              className="h-full w-full"
            />
          }
        >
          {isProjectFile ? (
            <WorkbenchFileWorkspace
              {...fileWorkspaceProps}
              file={{
                path,
                canonicalPath,
                rootPath,
                chatId,
                navigationRevision: tab.navigationRevision,
                render: renderFile,
              }}
              fileSource="project"
              onSelectFile={(file, root) =>
                void openScopedFile(tab, file, root)
              }
            />
          ) : (
            renderFile()
          )}
        </Suspense>
      );
    }
    if (tab.kind === "sidechat" && openSideChat)
      return (
        <Suspense
          fallback={
            <ContentLoadingState
              label={t("common:pageLoading")}
              className="h-full w-full"
            />
          }
        >
          <SideChatWorkbenchView
            parentBusy={parentBusy}
            tab={tab}
            onChatCreated={(chatId) =>
              dispatch({ type: "bind-sidechat", tabId: tab.id, chatId })
            }
          />
        </Suspense>
      );
    return null;
  };

  return (
    <section
      aria-label={t("workbench.title")}
      className={cn(
        "border-shell-separator bg-surface-scene flex h-full min-h-0 w-full flex-col",
        !snapshot.fullscreen &&
          (snapshot.side === "left" ? "border-r" : "border-l"),
      )}
      data-right-workbench
    >
      {chromeAccessoryTarget && visible
        ? createPortal(tabHeader, chromeAccessoryTarget)
        : tabHeader}
      <WorkbenchPaneCanvas
        layout={snapshot.layout}
        tabs={snapshot.tabs}
        draggedTabId={draggedTabId}
        draggingTab={draggingTab}
        onDragStateChange={setDraggingTab}
        onFocusPane={(paneId) => dispatch({ type: "focus-pane", paneId })}
        onMove={(tabId, paneId, zone: WorkbenchDropZone, beforeTabId) =>
          dispatch({
            type: "move-tab",
            tabId,
            targetPaneId: paneId,
            zone,
            beforeTabId,
          })
        }
        onResize={(splitId, delta) =>
          dispatch({ type: "resize-pane", splitId, delta })
        }
        onSwap={(splitId) => dispatch({ type: "swap-panes", splitId })}
        renderPaneTabs={renderPaneTabs}
        renderTab={renderTab}
      />
      <Dialog
        open={Boolean(closeConfirmation)}
        onOpenChange={(open) => {
          if (!open) setPendingClose(undefined);
        }}
      >
        <DialogContent
          size="sm"
          title={t("workbench.unsavedTitle")}
          description={
            closeConfirmation && closeConfirmation.dirtyTabs.length > 1
              ? t("workbench.unsavedTabsDescription", {
                  titles: closeConfirmation.dirtyTabs
                    .map(({ title }) => title)
                    .join(", "),
                })
              : t("workbench.unsavedDescription", {
                  title: closeConfirmation?.dirtyTabs[0]?.title ?? "",
                })
          }
        >
          <DialogFooter>
            <Button
              variant="secondary"
              onClick={() => setPendingClose(undefined)}
            >
              {t("workbench.keepEditing")}
            </Button>
            <Button
              variant="danger"
              onClick={() => {
                if (!closeConfirmation) return;
                setPendingClose(undefined);
                void closeTabs(
                  closeConfirmation.tabs,
                  closeConfirmation.dirtyTabs,
                );
              }}
            >
              {t("workbench.discardAndClose")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function WorkbenchDiff({
  active,
  ...props
}: ScopedGitChangesProps & { active: boolean }) {
  const { t } = useTranslation("chat");
  const [opened, setOpened] = useState(active);
  if (active && !opened) setOpened(true);
  if (!opened) return null;

  // 隐藏后释放读取与观察的订阅，保留 Diff 的审查状态和滚动容器。
  return (
    <Activity mode={active ? "visible" : "hidden"}>
      <Suspense
        fallback={
          <ContentLoadingState
            label={t("common:pageLoading")}
            className="h-full w-full"
          />
        }
      >
        <ScopedGitChanges {...props} active={active} />
      </Suspense>
    </Activity>
  );
}
