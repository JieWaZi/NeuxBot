import type { ComponentProps, CSSProperties, ReactNode } from "react";
import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import { ArrowLeftRight, MessageSquare, Plus } from "lucide-react";
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
import { VerticalResizeHandle } from "@/shared/ui/vertical-resize-handle";
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
import { FloatingChat } from "./floating-chat";
import type { ChatScopeKey } from "../model/conversation-identity";
import type { ChatCodeReference } from "../runtime/chat-queue-store";
import { WorkspaceDetailSplit } from "./workspace-detail-split";
import {
  ChatContributionScope,
  WorkbenchOpeningProvider,
} from "./chat-contributions";
import { RightWorkbench } from "../workbench/right-workbench";
import { useWorkbench } from "../workbench/workbench-store";
import type { WorkbenchFileWorkspaceProps } from "../workbench/file/workbench-file-workspace";
import type { ScopedGitChangesProps } from "./scoped-git-changes";
import { workbenchMinWidth } from "../workbench/workbench-model";

export type ChatWorkspaceAdapter = {
  identity: {
    chatId?: string;
    messageContributions?: boolean;
    key: ChatScopeKey;
    scopeKey: string;
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
  mainContent?: ChatWorkspaceMainContent;
  canvasInput?: boolean;
  showSummary?: boolean;
  directoryLabel?: string;
  onNewChat?: () => void;
  history?: ReactNode;
  activeRightPanel?: ChatRightPanel;
  adapter: ChatWorkspaceAdapter;
  onAddCodeReference: (reference: ChatCodeReference) => void;
  onDismissRightPanel: () => void;
  onOpenRightPanel: () => void;
  openedCodeReference?: ChatCodeReference;
  gitRoots?: readonly string[];
  renderTeamMembers?: (onMemberOpened: () => void) => ReactNode;
  showActivityIndicator?: boolean;
  taskBoard?: ReactNode;
  workspace: SceneWorkspace;
  fileWorkspaceProps?: Omit<WorkbenchFileWorkspaceProps, "active" | "ownerKey">;
  changesProps?: Omit<ScopedGitChangesProps, "active">;
};

/** Chat、Team 与 Workflow 共用会话内容和可交换位置的工作台。 */
export function ChatWorkspaceSurface({
  active = true,
  mainContent,
  canvasInput,
  showSummary = true,
  directoryLabel,
  onNewChat,
  history,
  activeRightPanel,
  adapter,
  onAddCodeReference,
  onDismissRightPanel,
  onOpenRightPanel,
  openedCodeReference,
  gitRoots = [],
  renderTeamMembers,
  showActivityIndicator = true,
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
  const { snapshot: workbench, dispatch } = useWorkbench(identity.scopeKey);
  const workbenchVisible = !canvasInput && activeRightPanel === "workspace";
  const fullView = workbenchVisible && workbench.fullscreen;
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
  const displayWidth =
    layoutWidth > 0
      ? Math.min(preferredWidth, layoutWidth * 0.79)
      : preferredWidth;
  const dragCleanup = useRef<(() => void) | undefined>(undefined);
  const lastVisibleWorkbenchScope = useRef<string | null>(null);
  const [isResizing, setIsResizing] = useState(false);
  const [selectedChangesRoot, setSelectedChangesRoot] = useState<string>();
  useEffect(() => () => dragCleanup.current?.(), []);

  useEffect(() => {
    if (
      workbenchVisible &&
      lastVisibleWorkbenchScope.current !== identity.scopeKey &&
      workbench.tabs.length === 0
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
    identity.workspaceDirectory,
    t,
    workbench.tabs.length,
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
    let dragFullscreen = false;
    dragCleanup.current = trackHorizontalDrag({
      initialX: startX,
      initialValue: startWidth,
      valueAt: (clientX) =>
        Math.max(
          workbenchMinWidth,
          Math.min(
            layoutWidth || Infinity,
            startWidth +
              (clientX - startX) * (workbench.side === "left" ? 1 : -1),
          ),
        ),
      onChange: (width) => {
        const fullscreen = layoutWidth > 0 && width >= layoutWidth * 0.8;
        if (fullscreen !== dragFullscreen) {
          dragFullscreen = fullscreen;
          dispatch({ type: "set-fullscreen", fullscreen });
        }
        if (!fullscreen) dispatch({ type: "set-width", width });
      },
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
    const fullscreen = availableWidth > 0 && width >= availableWidth * 0.8;
    dispatch({ type: "set-fullscreen", fullscreen });
    if (!fullscreen) dispatch({ type: "set-width", width });
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
      key={identity.scopeKey}
      chatId={identity.chatId}
      chatFiles={fileWorkspaceProps?.chatFiles ?? []}
      gitRoots={gitRoots}
      workspaceDirectory={identity.workspaceDirectory}
      directoryLabel={directoryLabel}
      renderTeamMembers={renderTeamMembers}
      onOpenChanges={(root) => {
        setSelectedChangesRoot(root);
        dispatch({
          type: "open-page",
          kind: "changes",
          title: t("workspace.changes"),
        });
        onOpenRightPanel();
      }}
      onOpenFile={(file) =>
        adapter.thread.onOpenFile?.({
          name: file.name,
          path: file.path,
          rootPath: identity.workspaceDirectory,
        })
      }
      onViewAllFiles={() => {
        fileWorkspaceProps?.onFileSourceChange?.("chat");
        dispatch({
          type: "open-page",
          kind: "files",
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
          workbench.side === "left" && "lg:flex-row-reverse",
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
        data-workbench-side={workbench.side}
        data-testid="chat-page-layout"
      >
        <main className="bg-surface-scene relative flex h-full min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
          {canvasInput !== undefined ? (
            <div
              aria-hidden="true"
              className={cn(
                "surface-pattern-dots duration-slow ease-ui pointer-events-none absolute inset-0 transition-opacity",
                canvasInput ? "opacity-100" : "opacity-0",
              )}
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
                className="h-full min-h-0 min-w-0 flex-1"
                data-chat-main-content={mainContent?.contentId}
                aria-label={mainContent?.title}
                aria-hidden={!sceneActive || fullView || Boolean(canvasInput)}
                inert={!sceneActive || fullView || Boolean(canvasInput)}
              >
                {typeof mainContent?.content === "function"
                  ? mainContent.content(
                      sceneActive && !fullView && !canvasInput,
                      { scopeKey: identity.scopeKey },
                    )
                  : mainContent?.content}
              </div>
            }
            sidebar={
              <div className="relative h-full min-h-0 min-w-0">
                {showActivityIndicator &&
                identity.chatId &&
                snapshot.isRunning ? (
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
                ) : null}
                <FloatingChat
                  chatId={identity.chatId}
                  fullView={fullView && sceneActive}
                  scopeKey={identity.scopeKey}
                  snapshot={snapshot}
                  title={activeTab?.label ?? adapter.thread.wakerName}
                >
                  <ChatContributionScope
                    chatId={identity.chatId}
                    scopeKey={identity.scopeKey}
                    isRunning={adapter.snapshot.isRunning}
                    active={active && sceneActive}
                    enabled={identity.messageContributions ?? false}
                  >
                    <ChatThread
                      {...adapter.thread}
                      emptyStatePosition={
                        canvasInput
                          ? "center"
                          : adapter.thread.emptyStatePosition
                      }
                      animateComposerSize={canvasInput !== undefined}
                      fileScopeChatId={identity.chatId}
                      isRunning={snapshot.isRunning}
                    />
                  </ChatContributionScope>
                </FloatingChat>
              </div>
            }
          />
        </main>
        <ChatRightPanelHost
          hidden={canvasInput}
          activePanel={activeRightPanel}
          fullscreen={fullView}
          side={workbench.side}
          resizing={isResizing}
          onDismiss={onDismissRightPanel}
          workspaceLeading={
            <div
              className={cn(
                "group/outer-divider absolute inset-y-0 z-20 w-px",
                "max-lg:hidden",
                workbench.side === "left" ? "right-0" : "left-0",
              )}
            >
              <VerticalResizeHandle
                ariaLabel={t("workspace.resizePanel")}
                className="h-full"
                ariaValueMin={workbenchMinWidth}
                ariaValueMax={layoutWidth || undefined}
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
          }
          workspacePanel={
            <RightWorkbench
              displayWidth={displayWidth}
              onAddCodeReference={onAddCodeReference}
              onEmpty={onDismissRightPanel}
              onOpenFile={(file) =>
                adapter.thread.onOpenFile?.({ ...file, reference: file })
              }
              onOpenURL={adapter.thread.onOpenLink}
              taskBoard={taskBoard}
              openedCodeReference={openedCodeReference}
              scopeKey={identity.scopeKey}
              sourceRoots={adapter.thread.linkAdditionalWorkingDirectories}
              parentChatId={identity.chatId}
              terminalWorkingDirectory={identity.terminalWorkingDirectory}
              workspaceDirectory={identity.workspaceDirectory}
              fileWorkspaceProps={fileWorkspaceProps}
              changesProps={
                changesProps
                  ? { ...changesProps, preferredRoot: selectedChangesRoot }
                  : undefined
              }
              visible={workbenchVisible && sceneActive}
            />
          }
        />
        {history && sceneActive && !fullView ? (
          <div className="absolute right-0 bottom-0 z-30">{history}</div>
        ) : null}
      </div>
    </WorkbenchOpeningProvider>
  );
}
