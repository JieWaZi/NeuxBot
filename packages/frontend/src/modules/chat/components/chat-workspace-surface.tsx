import type { ComponentProps, CSSProperties, ReactNode } from "react";
import {
  useContext,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import {
  ArrowLeftRight,
  MessageSquare,
  MessageSquareText,
  Plus,
} from "lucide-react";
import { useTranslation } from "react-i18next";

import {
  useOptionalAppSceneWorkspace,
  useRouteSceneActive,
} from "@/shared/workspace/app-scene-workspace";
import { trackHorizontalDrag } from "@/shared/lib/horizontal-drag";
import {
  SceneChromePaneActionsPortal,
  SceneChromePortal,
} from "@/shared/ui/scene-chrome-portal";
import { SceneTabStrip } from "@/shared/ui/scene-tab-strip";
import { Button } from "@/shared/ui/button";
import { CornerLauncherButton } from "@/shared/ui/corner-launcher-button";
import { VerticalResizeHandle } from "@/shared/ui/vertical-resize-handle";
import type { ChatSceneOwner } from "@/shared/workspace/app-scene-model";
import type { SceneWorkspace } from "@/shared/workspace/workspace-context";
import type { WorkspaceTab } from "@/shared/workspace/workspace-tab";
import type { StreamState } from "@/shared/api/events";
import { cn } from "@/shared/lib/cn";

import {
  ChatRightPanelHost,
  type ChatRightPanel,
} from "./chat-right-panel-host";
import { ChatThread } from "./chat-thread";
import { ChatSummary } from "./chat-summary";
import { ChatHistoryContentContext } from "./chat-history-navigation";
import { FloatingChat } from "./floating-chat";
import type { ChatScopeKey } from "../model/conversation-identity";
import type { ChatCodeReference } from "../runtime/chat-queue-store";
import { WorkspaceDetailSplit } from "./workspace-detail-split";
import { WorkbenchOpeningProvider } from "./chat-workspace-host";
import { RightWorkbench } from "../workbench/right-workbench";
import { useWorkbench } from "../workbench/workbench-store";
import type { WorkbenchFileWorkspaceProps } from "../workbench/file/workbench-file-workspace";
import type { ScopedGitChangesProps } from "./scoped-git-changes";
import {
  workbenchMinWidth,
  workbenchPaneWithTab,
} from "../workbench/workbench-model";

export type ChatWorkspaceAdapter = {
  identity: {
    chatId?: string;
    key: ChatScopeKey;
    scopeKey: string;
    /** 仅伙伴与团队会话提供；空字符串表示等待工作目录。 */
    terminalWorkingDirectory?: string;
    workspaceDirectory: string;
  };
  snapshot: {
    isRunning: boolean;
    pendingApprovalCount: number;
    pendingElicitationCount: number;
    streamState: StreamState;
  };
  thread: ComponentProps<typeof ChatThread>;
  tabs?: WorkspaceTab[];
};

/** 主窗口内与共享会话并排的业务内容，不注册为右侧工作台标签。 */
export type ChatWorkspaceMainContent = {
  contentId: string;
  title: string;
  content:
    ReactNode | ((active: boolean, context: { scopeKey: string }) => ReactNode);
  chatExpanded?: boolean;
  resizeLabel?: string;
};

type ChatWorkspaceSurfaceProps = {
  active?: boolean;
  chatOwner?: ChatSceneOwner;
  mainContent?: ChatWorkspaceMainContent;
  canvasInput?: boolean;
  showSummary?: boolean;
  directoryLabel?: string;
  onNewChat?: () => void;
  chatHistory?: ReactNode;
  activeRightPanel?: ChatRightPanel;
  adapter: ChatWorkspaceAdapter;
  onAddCodeReference: (reference: ChatCodeReference) => void;
  onDismissRightPanel: () => void;
  onOpenRightPanel: () => void;
  gitRoots?: readonly string[];
  renderTeamMembers?: (onMemberOpened: () => void) => ReactNode;
  showActivityIndicator?: boolean;
  taskBoard?: ReactNode;
  workspace: SceneWorkspace;
  fileWorkspaceProps?: Omit<
    WorkbenchFileWorkspaceProps,
    "active" | "file" | "onSelectFile"
  >;
  changesProps?: Omit<ScopedGitChangesProps, "active">;
};

// 分屏为消息和输入区保留可读宽度；放不下两个窗格时沿用浮层布局。
const chatMinWidth = 320;

function maximumWorkbenchWidth(availableWidth: number): number {
  if (availableWidth <= 0) return Infinity;
  return availableWidth < chatMinWidth + workbenchMinWidth
    ? availableWidth * 0.88
    : availableWidth - chatMinWidth;
}

/** Chat、Team 与 Workflow 共用会话内容和可交换位置的工作台。 */
export function ChatWorkspaceSurface({
  active = true,
  chatOwner,
  mainContent,
  canvasInput,
  showSummary = true,
  directoryLabel,
  onNewChat,
  chatHistory,
  activeRightPanel,
  adapter,
  onAddCodeReference,
  onDismissRightPanel,
  onOpenRightPanel,
  gitRoots = [],
  renderTeamMembers,
  // showActivityIndicator = true,
  taskBoard,
  workspace,
  fileWorkspaceProps,
  changesProps,
}: ChatWorkspaceSurfaceProps) {
  const { t } = useTranslation("chat");
  const appSceneWorkspace = useOptionalAppSceneWorkspace();
  const routeActive = useRouteSceneActive();
  const sceneActive = active && routeActive;
  const { identity, snapshot } = adapter;
  const gitScopeKey = JSON.stringify([identity.scopeKey, identity.chatId]);
  const { snapshot: workbench, dispatch } = useWorkbench(identity.scopeKey);
  const defaultChatHistory = useContext(ChatHistoryContentContext);
  const history = chatHistory === undefined ? defaultChatHistory : chatHistory;
  const workbenchVisible = !canvasInput && activeRightPanel === "workspace";
  const fullView = workbenchVisible && workbench.fullscreen;
  const historyTab = workbench.tabs.find((tab) => tab.kind === "chat-history");
  const historyVisible =
    workbenchVisible &&
    historyTab !== undefined &&
    workbenchPaneWithTab(workbench.layout, historyTab.id)?.activeTabId ===
      historyTab.id;
  const showHistoryLauncher = Boolean(
    history && sceneActive && !historyVisible && !fullView && !canvasInput,
  );
  const historyTabId = historyTab ? `workbench-tab-${historyTab.id}` : "";
  const historyLauncher = useRef<HTMLButtonElement>(null);
  const historyFocus = useRef<"tab" | "launcher" | null>(null);
  useLayoutEffect(() => {
    if (!sceneActive) {
      historyFocus.current = null;
      return;
    }
    const target =
      historyFocus.current === "tab" && historyVisible
        ? document.getElementById(historyTabId)
        : historyFocus.current === "launcher" && showHistoryLauncher
          ? historyLauncher.current
          : null;
    if (!target) return;
    historyFocus.current = null;
    target.focus({ preventScroll: true });
  }, [historyTabId, historyVisible, sceneActive, showHistoryLauncher]);
  const rightPanelOpen = Boolean(activeRightPanel);
  const layoutRef = useRef<HTMLDivElement>(null);
  const [layoutWidth, setLayoutWidth] = useState(0);
  useLayoutEffect(() => {
    if (!layoutRef.current) return;
    const element = layoutRef.current;
    const update = () =>
      setLayoutWidth(
        element.getBoundingClientRect().width ||
          element.offsetWidth ||
          element.clientWidth,
      );
    update();
    const observer =
      typeof ResizeObserver === "undefined"
        ? undefined
        : new ResizeObserver(update);
    observer?.observe(element);
    return () => observer?.disconnect();
  }, []);
  const preferredWidth = workbench.width;
  const workbenchOverlay =
    layoutWidth > 0
      ? layoutWidth < chatMinWidth + workbenchMinWidth
      : undefined;
  const widthLimit = maximumWorkbenchWidth(layoutWidth);
  const displayWidth = Math.min(preferredWidth, widthLimit);
  const dragCleanup = useRef<(() => void) | undefined>(undefined);
  const lastVisibleWorkbenchScope = useRef<string | null>(null);
  const [isResizing, setIsResizing] = useState(false);
  const [changesTarget, setChangesTarget] = useState<{
    scopeKey: string;
    root: string;
    repository?: string;
  }>();
  const selectedChanges =
    changesTarget?.scopeKey === gitScopeKey ? changesTarget : undefined;
  const [comparisonBases, setComparisonBases] = useState<{
    scopeKey: string;
    repositories: Record<string, string>;
  }>();
  const comparisonBaseByRepository =
    comparisonBases?.scopeKey === gitScopeKey
      ? comparisonBases.repositories
      : undefined;
  useEffect(() => () => dragCleanup.current?.(), []);

  useLayoutEffect(() => {
    const staleTerminalTabs =
      identity.terminalWorkingDirectory === undefined
        ? workbench.tabs.filter((tab) => tab.kind === "terminal")
        : [];
    for (const tab of staleTerminalTabs) {
      // 仅更新保存的布局，底层终端仍由原连接的生命周期回收。
      dispatch({ type: "close", tabId: tab.id });
    }
    if (
      workbenchVisible &&
      lastVisibleWorkbenchScope.current !== identity.scopeKey &&
      workbench.tabs.length === staleTerminalTabs.length
    ) {
      dispatch({
        type: "open-browser",
        title: t("workbench.newTab"),
        newTab: true,
        rootPath: identity.workspaceDirectory,
      });
    }
    lastVisibleWorkbenchScope.current = workbenchVisible
      ? identity.scopeKey
      : null;
  }, [
    dispatch,
    identity.scopeKey,
    identity.terminalWorkingDirectory,
    identity.workspaceDirectory,
    t,
    workbench.tabs,
    workbenchVisible,
  ]);

  const handlePanelDragStart = (event: ReactMouseEvent<HTMLDivElement>) => {
    event.preventDefault();
    setIsResizing(true);
    dragCleanup.current?.();
    const startX = event.clientX;
    const startWidth = displayWidth;
    const layoutWidth =
      layoutRef.current?.getBoundingClientRect().width ||
      layoutRef.current?.offsetWidth ||
      layoutRef.current?.clientWidth ||
      0;
    dragCleanup.current = trackHorizontalDrag({
      initialX: startX,
      initialValue: startWidth,
      valueAt: (clientX) =>
        Math.min(
          maximumWorkbenchWidth(layoutWidth),
          Math.max(
            workbenchMinWidth,
            startWidth +
              (clientX - startX) * (workbench.side === "left" ? 1 : -1),
          ),
        ),
      onChange: (width) => dispatch({ type: "set-width", width }),
      onCommit: () => {},
      onFinish: () => setIsResizing(false),
    });
  };

  const handlePanelKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    const availableWidth =
      layoutRef.current?.getBoundingClientRect().width ||
      layoutRef.current?.offsetWidth ||
      layoutRef.current?.clientWidth ||
      0;
    const direction = workbench.side === "left" ? 1 : -1;
    const width =
      event.key === "Home"
        ? workbenchMinWidth
        : event.key === "End"
          ? availableWidth
          : event.key === "ArrowLeft"
            ? displayWidth - 24 * direction
            : event.key === "ArrowRight"
              ? displayWidth + 24 * direction
              : undefined;
    if (width === undefined) return;
    event.preventDefault();
    dispatch({
      type: "set-width",
      width: Math.min(
        maximumWorkbenchWidth(availableWidth),
        Math.max(workbenchMinWidth, width),
      ),
    });
  };

  const tabs = adapter.tabs ?? workspace.snapshot.tabs;
  const activeTab =
    tabs.find((tab) => tab.id === workspace.snapshot.activeTabId) ?? tabs[0];
  const primaryTab = tabs.find((tab) => tab.kind === "chat");
  const activeTabId = activeTab?.id;
  const primaryTabId = primaryTab?.id;
  const tabSignature = JSON.stringify(
    tabs.map((tab) => ({
      closeable: tab.kind !== "chat",
      draggable: tab.kind === "member",
      id: tab.id,
      label: tab.label,
    })),
  );
  const routeScene = appSceneWorkspace?.snapshot.tabs.find(
    (tab) => tab.id === appSceneWorkspace.routeSceneId,
  );

  useEffect(() => {
    if (!activeTabId || activeTabId === workspace.snapshot.activeTabId) return;
    workspace.dispatch({ type: "select-tab", tabId: activeTabId });
  }, [activeTabId, workspace, workspace.snapshot.activeTabId]);

  useEffect(() => {
    if (!appSceneWorkspace || !primaryTabId || !sceneActive) return;
    appSceneWorkspace.syncWorkspace({
      chatOwner,
      onNewChat,
      activeWorkspaceTabId: activeTabId ?? primaryTabId,
      onClose: (tabId) => workspace.dispatch({ type: "close-tab", tabId }),
      onSelect: (tabId) => workspace.dispatch({ type: "select-tab", tabId }),
      ownerHref: routeScene?.href,
      ownerTabId: appSceneWorkspace.routeSceneId,
      primaryWorkspaceTabId: primaryTabId,
      tabs: JSON.parse(tabSignature) as Array<{
        closeable: boolean;
        draggable: boolean;
        id: string;
        label: string;
      }>,
    });
  }, [
    activeTabId,
    chatOwner,
    appSceneWorkspace,
    primaryTabId,
    routeScene?.href,
    tabSignature,
    workspace,
    onNewChat,
    sceneActive,
  ]);

  const summary = showSummary ? (
    <ChatSummary
      key={gitScopeKey}
      chatId={identity.chatId}
      chatFiles={fileWorkspaceProps?.chatFiles ?? []}
      gitRoots={gitRoots}
      gitBusy={snapshot.isRunning}
      comparisonBaseByRepository={comparisonBaseByRepository}
      workspaceDirectory={identity.workspaceDirectory}
      directoryLabel={directoryLabel}
      renderTeamMembers={renderTeamMembers}
      onOpenChanges={(root, repository) => {
        setChangesTarget({ scopeKey: gitScopeKey, root, repository });
        dispatch({
          type: "open-page",
          kind: "changes",
          title: t("workspace.diffTab"),
        });
        onOpenRightPanel();
      }}
      onOpenFile={(file) =>
        adapter.thread.onOpenFile?.({
          name: file.name,
          path: file.path,
        })
      }
      onViewAllFiles={() => {
        fileWorkspaceProps?.onFileSourceChange?.("chat");
        dispatch({
          type: "open-page",
          kind: "files",
          fileSource: "chat",
          title: t("workspace.files"),
        });
        onOpenRightPanel();
      }}
    />
  ) : undefined;

  return (
    <WorkbenchOpeningProvider
      value={{ scopeKey: identity.scopeKey, onOpen: onOpenRightPanel }}
    >
      <div
        className={cn(
          "relative flex h-full min-h-0 min-w-0 overflow-hidden",
          workbench.side === "left" &&
            (workbenchOverlay === undefined
              ? "lg:flex-row-reverse"
              : "flex-row-reverse"),
        )}
        style={
          {
            "--chat-right-panel-width":
              rightPanelOpen && !fullView && !canvasInput
                ? `${displayWidth}px`
                : "0px",
          } as CSSProperties
        }
        ref={layoutRef}
        data-chat-layout
        data-chat-canvas-layout={canvasInput !== undefined || undefined}
        data-chat-canvas-input={canvasInput || undefined}
        data-chat-full-view={fullView || undefined}
        data-chat-corner-launcher={showHistoryLauncher || undefined}
        data-workbench-side={workbench.side}
        data-testid="chat-page-layout"
      >
        <main
          className={cn(
            "relative flex h-full min-h-0 min-w-0 flex-1 flex-col overflow-hidden",
            canvasInput !== undefined
              ? "bg-surface-canvas"
              : "bg-surface-scene",
          )}
        >
          {canvasInput !== undefined ? (
            <div
              aria-hidden="true"
              className="surface-pattern-dots surface-pattern-dots-canvas pointer-events-none absolute inset-0"
            />
          ) : null}
          {appSceneWorkspace && sceneActive && !fullView ? (
            <SceneChromePaneActionsPortal>
              {summary}
            </SceneChromePaneActionsPortal>
          ) : null}
          {!appSceneWorkspace && sceneActive ? (
            <SceneChromePortal>
              <SceneTabStrip
                activeTabId={activeTab?.id ?? ""}
                paneActions={sceneActive && !fullView ? summary : undefined}
                ariaLabel={t("workspace.content")}
                onClose={(tabId) =>
                  workspace.dispatch({ type: "close-tab", tabId })
                }
                onReorder={(sourceTabId, targetTabId) =>
                  workspace.dispatch({
                    type: "reorder-tab",
                    sourceTabId,
                    targetTabId,
                  })
                }
                onSelect={(tabId) =>
                  workspace.dispatch({ type: "select-tab", tabId })
                }
                tabs={tabs.map((tab) => ({
                  id: tab.id,
                  label: tab.label,
                  icon: MessageSquare,
                  closeable: tab.kind !== "chat",
                  draggable: tab.kind === "member",
                }))}
                trailing={
                  onNewChat ? (
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      aria-label={t("common:partnerNavigation.newChat")}
                      onClick={onNewChat}
                    >
                      <Plus />
                    </Button>
                  ) : undefined
                }
              />
            </SceneChromePortal>
          ) : null}
          <WorkspaceDetailSplit
            side="left"
            sidebarOnly={!mainContent || Boolean(canvasInput)}
            sidebarOpen={
              !mainContent ||
              Boolean(canvasInput) ||
              mainContent.chatExpanded !== false
            }
            defaultSidebarRatio={1 / 3}
            resizeLabel={mainContent?.resizeLabel ?? t("workspace.resizePanel")}
            main={
              <div
                className="flex h-full min-h-0 min-w-0 flex-1 flex-col"
                data-chat-main-content={mainContent?.contentId}
                aria-label={mainContent?.title}
                aria-hidden={!sceneActive || fullView || Boolean(canvasInput)}
                inert={!sceneActive || fullView || Boolean(canvasInput)}
              >
                <div className="min-h-0 flex-1">
                  {typeof mainContent?.content === "function"
                    ? mainContent.content(
                        sceneActive && !fullView && !canvasInput,
                        { scopeKey: identity.scopeKey },
                      )
                    : mainContent?.content}
                </div>
              </div>
            }
            sidebar={
              <div
                className={cn(
                  "relative h-full min-h-0 min-w-0",
                  canvasInput !== undefined &&
                    "bg-surface-scene/15 backdrop-blur-canvas-glass",
                )}
              >
                {/* 暂时隐藏右上角工作状态，保留实现以便恢复。
                {showActivityIndicator && snapshot.isRunning ? (
                  <span
                    className="text-muted-foreground type-caption absolute top-3 right-4 z-20 flex items-center gap-2"
                    data-chat-activity-status
                  >
                    <span
                      aria-hidden="true"
                      className={cn(
                        "block size-1.5 shrink-0 animate-pulse rounded-full",
                        snapshot.pendingApprovalCount > 0 ||
                          snapshot.pendingElicitationCount > 0
                          ? "bg-warning"
                          : "bg-success",
                      )}
                    />
                    {snapshot.pendingElicitationCount > 0
                      ? t("waitingElicitation")
                      : snapshot.pendingApprovalCount > 0
                        ? t("waitingApproval")
                        : snapshot.streamState === "reconnecting"
                          ? t("reconnecting")
                          : snapshot.streamState === "unavailable"
                            ? t("streamUnavailable")
                            : t("working")}
                  </span>
                ) : null} */}
                <FloatingChat
                  chatId={identity.chatId}
                  fullView={fullView && sceneActive}
                  scopeKey={identity.scopeKey}
                  snapshot={snapshot}
                  title={activeTab?.label ?? adapter.thread.wakerName}
                >
                  <ChatThread
                    {...adapter.thread}
                    emptyStatePosition={
                      canvasInput ? "center" : adapter.thread.emptyStatePosition
                    }
                    animateComposerSize={canvasInput !== undefined}
                    fileScopeChatId={identity.chatId}
                    isRunning={snapshot.isRunning}
                  />
                </FloatingChat>
              </div>
            }
          />
        </main>
        <ChatRightPanelHost
          hidden={canvasInput}
          activePanel={activeRightPanel}
          fullscreen={fullView}
          overlay={workbenchOverlay}
          side={workbench.side}
          resizing={isResizing}
          onDismiss={onDismissRightPanel}
          workspaceLeading={
            workbenchOverlay ? undefined : (
              <div
                className={cn(
                  "group/outer-divider absolute inset-y-0 z-20 w-px",
                  workbenchOverlay === undefined && "max-lg:hidden",
                  workbench.side === "left" ? "right-0" : "left-0",
                )}
              >
                <VerticalResizeHandle
                  ariaLabel={t("workspace.resizePanel")}
                  className="h-full"
                  ariaValueMin={workbenchMinWidth}
                  ariaValueMax={
                    Number.isFinite(widthLimit) ? widthLimit : undefined
                  }
                  ariaValueNow={displayWidth}
                  onKeyDown={handlePanelKeyDown}
                  onMouseDown={handlePanelDragStart}
                />
                <span className="bg-surface-scene pointer-events-none absolute top-1/2 left-1/2 z-30 -translate-x-1/2 -translate-y-1/2 rounded-full opacity-0 transition-opacity group-hover/outer-divider:pointer-events-auto group-hover/outer-divider:opacity-100 focus-within:pointer-events-auto focus-within:opacity-100">
                  <Button
                    type="button"
                    size="icon-sm"
                    variant="secondary"
                    shape="circle"
                    aria-label={t("workbench.swapPanels")}
                    title={t("workbench.swapPanels")}
                    onClick={() => dispatch({ type: "swap-side" })}
                  >
                    <ArrowLeftRight />
                  </Button>
                </span>
              </div>
            )
          }
          workspacePanel={
            <RightWorkbench
              chatHistory={history}
              parentBusy={
                adapter.snapshot.isRunning ||
                adapter.snapshot.pendingApprovalCount > 0 ||
                adapter.snapshot.pendingElicitationCount > 0
              }
              displayWidth={displayWidth}
              onAddCodeReference={onAddCodeReference}
              onDismiss={onDismissRightPanel}
              onEmpty={() => {
                if (
                  historyVisible &&
                  document
                    .getElementById(historyTabId)
                    ?.parentElement?.contains(document.activeElement)
                ) {
                  historyFocus.current = "launcher";
                }
                onDismissRightPanel();
              }}
              onOpenFile={(file, sourceChatId) =>
                adapter.thread.onOpenFile?.({
                  ...file,
                  reference: file,
                  sourceChatId,
                })
              }
              onOpenURL={adapter.thread.onOpenLink}
              taskBoard={taskBoard}
              scopeKey={identity.scopeKey}
              sourceRoots={adapter.thread.linkAdditionalWorkingDirectories}
              parentChatId={identity.chatId}
              terminalWorkingDirectory={identity.terminalWorkingDirectory}
              workspaceDirectory={identity.workspaceDirectory}
              fileWorkspaceProps={fileWorkspaceProps}
              changesProps={
                changesProps
                  ? {
                      ...changesProps,
                      rootPath: gitRoots[0] ?? changesProps.rootPath,
                      sourceRoots:
                        gitRoots.length > 0
                          ? gitRoots.slice(1)
                          : changesProps.sourceRoots,
                      preferredRoot: selectedChanges?.root,
                      preferredRepository: selectedChanges?.repository,
                      comparisonBaseByRepository:
                        comparisonBaseByRepository ?? {},
                      onComparisonBaseChange: (repository, base) =>
                        setComparisonBases((previous) => ({
                          scopeKey: gitScopeKey,
                          repositories: {
                            ...(previous?.scopeKey === gitScopeKey
                              ? previous.repositories
                              : {}),
                            [repository]: base,
                          },
                        })),
                    }
                  : undefined
              }
              visible={workbenchVisible && sceneActive}
            />
          }
        />
        {showHistoryLauncher ? (
          <div className="absolute right-0 bottom-0 z-40">
            <CornerLauncherButton
              ref={historyLauncher}
              icon={<MessageSquareText strokeWidth={1.5} />}
              label={t("common:partnerNavigation.viewChatHistory")}
              onClick={(event) => {
                if (event.detail === 0) historyFocus.current = "tab";
                dispatch({
                  type: "open-page",
                  kind: "chat-history",
                  title: t("common:partnerNavigation.conversationList"),
                });
                onOpenRightPanel();
              }}
            />
          </div>
        ) : null}
      </div>
    </WorkbenchOpeningProvider>
  );
}
