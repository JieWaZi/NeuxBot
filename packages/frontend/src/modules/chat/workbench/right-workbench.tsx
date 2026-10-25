import {
  lazy,
  Suspense,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { Maximize2, Minimize2, Plus } from "lucide-react";
import { useTranslation } from "react-i18next";

import { WorkbenchContribution } from "../components/chat-contributions";
import { ChatBrowserView } from "../components/chat-browser-view";
import { closeTerminal } from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import {
  useSceneChromeAccessoryTarget,
  useSceneChromeAccessoryWidthSetter,
} from "@/shared/ui/scene-chrome-portal";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { ContentLoadingState } from "@/shared/ui/content-loading-state";
import { toast } from "@/shared/ui/sonner";
import { cn } from "@/shared/lib/cn";
import type { ResolvedFileReference } from "@/shared/workspace/file-reference";
import type { ChatCodeReference } from "../runtime/chat-queue-store";
import { useWorkbench } from "./workbench-store";
import { workbenchPaneWithTab, type WorkbenchTab } from "./workbench-model";
import type { WorkbenchDropZone, WorkbenchPane } from "./workbench-pane";
import {
  WorkbenchPaneCanvas,
  WorkbenchPaneTabStrip,
} from "./workbench-pane-canvas";
import { paneHeaderGeometry } from "./pane-geometry";
import { workbenchDocumentBuffers } from "./document-buffers";
import type { WorkbenchFileWorkspaceProps } from "./file/workbench-file-workspace";
import type { ScopedGitChangesProps } from "../components/scoped-git-changes";

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
const GitDiffWorkbenchView = lazy(async () => ({
  default: (await import("./git-diff-workbench-view")).GitDiffWorkbenchView,
}));

export type WorkbenchContentTab = {
  contentId: string;
  title: string;
  content:
    | ReactNode
    | ((active: boolean, context?: { scopeKey: string }) => ReactNode);
};

type RightWorkbenchProps = {
  contentTab?: WorkbenchContentTab;
  displayWidth?: number;
  animateLayout?: boolean;
  onAddCodeReference: (reference: ChatCodeReference) => void;
  onOpenFile?: (file: ResolvedFileReference) => void;
  onOpenURL?: (url: string) => unknown;
  taskBoard?: ReactNode;
  onEmpty: () => void;
  openedCodeReference?: ChatCodeReference;
  scopeKey: string;
  sourceRoots?: readonly string[];
  parentChatId?: string;
  terminalWorkingDirectory?: string;
  workspaceDirectory: string;
  fileWorkspaceProps?: Omit<WorkbenchFileWorkspaceProps, "active" | "ownerKey">;
  changesProps?: Omit<ScopedGitChangesProps, "active">;
  visible: boolean;
};

export function RightWorkbench({
  contentTab,
  displayWidth,
  animateLayout = false,
  onAddCodeReference,
  onOpenFile,
  onOpenURL,
  taskBoard,
  onEmpty,
  openedCodeReference,
  scopeKey,
  sourceRoots,
  parentChatId,
  terminalWorkingDirectory,
  workspaceDirectory,
  fileWorkspaceProps,
  changesProps,
  visible,
}: RightWorkbenchProps) {
  const { t } = useTranslation("chat");
  const chromeAccessoryTarget = useSceneChromeAccessoryTarget();
  const setChromeAccessoryWidth = useSceneChromeAccessoryWidthSetter();
  const { snapshot, dispatch } = useWorkbench(scopeKey);
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
  const [pendingCloseTabId, setPendingCloseTabId] = useState("");
  const snapshotRef = useRef(snapshot);
  const scopeRef = useRef(scopeKey);
  useLayoutEffect(() => {
    snapshotRef.current = snapshot;
    scopeRef.current = scopeKey;
  }, [scopeKey, snapshot]);
  const chromePanes = paneHeaderGeometry(snapshot.layout);
  const pendingCloseTab = snapshot.tabs.find(
    (tab) => tab.id === pendingCloseTabId,
  );
  const finishClose = (tabId: string) => {
    if (scopeRef.current !== scopeKey) {
      dispatch({ type: "close", tabId });
      return;
    }
    const current = snapshotRef.current;
    if (
      !current.tabs.some((tab) => tab.id === tabId && tab.closeable !== false)
    )
      return;
    dispatch({ type: "close", tabId });
    if (current.tabs.length === 1) onEmpty();
  };
  const closeTab = (tab: WorkbenchTab) => {
    if (tab.closeable === false) return;
    if (tab.kind === "terminal") {
      void closeTerminal(tab.id)
        .then(() => finishClose(tab.id))
        .catch((cause: unknown) =>
          toast.error(cause instanceof Error ? cause.message : String(cause)),
        );
      return;
    }
    const draftKey = `${scopeKey}:${tab.id}`;
    if (
      workbenchDocumentBuffers.isDirty(draftKey) ||
      (tab.kind === "files" &&
        workbenchDocumentBuffers.hasDirtyPrefix(`${draftKey}:file:`))
    ) {
      setPendingCloseTabId(tab.id);
      return;
    }
    workbenchDocumentBuffers.discard(draftKey);
    if (tab.kind === "files")
      workbenchDocumentBuffers.discardPrefix(`${draftKey}:file:`);
    finishClose(tab.id);
  };
  const openPage = (kind: "files" | "changes") => {
    if (kind === "files") fileWorkspaceProps?.onFileSourceChange?.("project");
    dispatch({
      type: "open-page",
      kind,
      title: kind === "files" ? t("workspace.files") : t("workspace.changes"),
    });
  };
  const openBrowser = (paneId: string) =>
    dispatch({
      type: "open-browser",
      title: t("workbench.newTab"),
      newTab: true,
      rootPath: workspaceDirectory,
      paneId,
    });
  const openTerminal = () => {
    if (!terminalWorkingDirectory) return;
    dispatch({
      type: "open-terminal",
      title: t("terminal"),
      rootPath: terminalWorkingDirectory,
    });
  };
  const openSideChat = () =>
    dispatch({
      type: "open-sidechat",
      title: t("workbench.sideChat"),
      parentChatId,
    });
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
    <>
      <WorkbenchPaneTabStrip
        pane={pane}
        tabs={snapshot.tabs}
        draggedTabId={draggedTabId}
        onClose={closeTab}
        onDragStateChange={setDraggingTab}
        onFocus={focusTab}
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
      />
      <Button
        aria-label={t("workbench.addTab")}
        title={t("workbench.addTab")}
        size="icon-sm"
        variant="ghost"
        onClick={() => openBrowser(pane.id)}
      >
        <Plus />
      </Button>
    </>
  );

  const tabHeader = (
    <header
      className={cn(
        "border-border-subtle bg-surface-scene relative flex h-(--shell-titlebar-height) shrink-0 items-center overflow-hidden border-b",
        chromeAccessoryTarget && visible && "min-w-0 flex-1 border-b-0",
      )}
      data-workbench-chrome
      data-workbench-layout-motion={animateLayout || undefined}
    >
      {chromePanes.map(({ pane, left, width }, index) => (
        <div
          key={pane.id}
          data-workbench-pane-header={pane.id}
          className={cn(
            "absolute inset-y-0 flex min-w-0 items-center gap-1 pr-2",
            index > 0 && "pl-3",
            index === 0 &&
              (!chromeAccessoryTarget ||
                (!snapshot.fullscreen && snapshot.side !== "left")) &&
              "pl-2",
          )}
          style={{
            left: `${left}%`,
            width: `${width}%`,
            ...(index === 0 &&
            chromeAccessoryTarget &&
            (snapshot.fullscreen || snapshot.side === "left")
              ? { paddingLeft: "var(--scene-chrome-left-inset, 8px)" }
              : {}),
            ...(index === chromePanes.length - 1
              ? {
                  paddingRight:
                    "calc(var(--scene-chrome-actions-width, 0px) + 40px)",
                }
              : {}),
          }}
        >
          {index > 0 ? (
            <span
              aria-hidden="true"
              className="bg-border pointer-events-none absolute top-1.5 bottom-1.5 left-0 w-px"
            />
          ) : null}
          {renderPaneTabs(pane)}
        </div>
      ))}
      <Button
        aria-label={
          snapshot.fullscreen
            ? t("workbench.exitFullView")
            : t("workbench.fullView")
        }
        className="absolute z-20"
        style={{ right: "calc(var(--scene-chrome-actions-width, 0px) + 8px)" }}
        size="icon-sm"
        title={
          snapshot.fullscreen
            ? t("workbench.exitFullView")
            : t("workbench.fullView")
        }
        variant="ghost"
        onClick={() =>
          dispatch({ type: "set-fullscreen", fullscreen: !snapshot.fullscreen })
        }
      >
        {snapshot.fullscreen ? <Minimize2 /> : <Maximize2 />}
      </Button>
    </header>
  );

  const renderTab = (tab: WorkbenchTab, active: boolean) => {
    if (tab.kind === "content") {
      if (tab.contentReference)
        return (
          <WorkbenchContribution
            reference={tab.contentReference}
            active={active && visible}
            scopeKey={scopeKey}
          />
        );
      if (tab.contentId !== contentTab?.contentId) return null;
      return typeof contentTab.content === "function"
        ? contentTab.content(active && visible, { scopeKey })
        : contentTab.content;
    }
    if (tab.kind === "files")
      return fileWorkspaceProps ? (
        <Suspense
          fallback={
            <ContentLoadingState
              label={t("common:pageLoading")}
              className="h-full w-full"
            />
          }
        >
          <WorkbenchFileWorkspace
            key={fileWorkspaceProps.fileSource}
            {...fileWorkspaceProps}
            active={active && visible}
            ownerKey={`${scopeKey}:${tab.id}`}
          />
        </Suspense>
      ) : null;
    if (tab.kind === "changes")
      return changesProps ? (
        <Suspense
          fallback={
            <ContentLoadingState
              label={t("common:pageLoading")}
              className="h-full w-full"
            />
          }
        >
          <ScopedGitChanges
            key={changesProps.preferredRoot}
            {...changesProps}
            active={active && visible}
          />
        </Suspense>
      ) : null;
    if (tab.kind === "browser")
      return (
        <ChatBrowserView
          active={active && visible}
          chatId={parentChatId}
          onOpenTool={(kind) => {
            if (kind === "files" || kind === "changes") openPage(kind);
            else if (kind === "terminal") openTerminal();
            else if (kind === "sidechat") openSideChat();
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
    if (tab.kind === "terminal" && tab.rootPath)
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
    if (tab.kind === "file" && tab.path && tab.rootPath)
      return (
        <Suspense
          fallback={
            <ContentLoadingState
              label={t("common:pageLoading")}
              className="h-full w-full"
            />
          }
        >
          <WorkspaceFileViewer
            active={active && visible}
            highlightLines={
              openedCodeReference?.path === tab.path &&
              openedCodeReference.startLine !== undefined &&
              openedCodeReference.endLine !== undefined
                ? {
                    startLine: openedCodeReference.startLine,
                    endLine: openedCodeReference.endLine,
                  }
                : undefined
            }
            onAddCodeReference={onAddCodeReference}
            onOpenFile={onOpenFile}
            onOpenURL={onOpenURL}
            path={tab.path}
            rootPath={tab.rootPath}
            draftKey={`${scopeKey}:${tab.id}`}
            scopeChatId={parentChatId}
            sourceRoots={sourceRoots}
            mode={tab.fileMode}
            executionDiff={tab.executionDiff}
          />
        </Suspense>
      );
    if (tab.kind === "sidechat")
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
            tab={tab}
            onChatCreated={(chatId) =>
              dispatch({ type: "bind-sidechat", tabId: tab.id, chatId })
            }
          />
        </Suspense>
      );
    if (tab.kind === "git-diff")
      return (
        <Suspense
          fallback={
            <ContentLoadingState
              label={t("common:pageLoading")}
              className="h-full w-full"
            />
          }
        >
          <GitDiffWorkbenchView chatId={parentChatId} tab={tab} />
        </Suspense>
      );
    return null;
  };

  return (
    <section
      aria-label={t("workbench.title")}
      className={cn(
        "border-border-subtle bg-surface-panel flex h-full min-h-0 w-full flex-col",
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
        open={Boolean(pendingCloseTab)}
        onOpenChange={(open) => {
          if (!open) setPendingCloseTabId("");
        }}
      >
        <DialogContent
          size="sm"
          title={t("workbench.unsavedTitle")}
          description={t("workbench.unsavedDescription", {
            title: pendingCloseTab?.title ?? "",
          })}
        >
          <DialogFooter>
            <Button
              variant="secondary"
              onClick={() => setPendingCloseTabId("")}
            >
              {t("workbench.keepEditing")}
            </Button>
            <Button
              variant="danger"
              onClick={() => {
                if (!pendingCloseTab) return;
                workbenchDocumentBuffers.discard(
                  `${scopeKey}:${pendingCloseTab.id}`,
                );
                if (pendingCloseTab.kind === "files")
                  workbenchDocumentBuffers.discardPrefix(
                    `${scopeKey}:${pendingCloseTab.id}:file:`,
                  );
                finishClose(pendingCloseTab.id);
                setPendingCloseTabId("");
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
