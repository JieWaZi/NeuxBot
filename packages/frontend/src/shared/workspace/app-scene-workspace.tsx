import { createContext, useContext, useMemo, type ReactNode } from "react";

import { type AppSceneCommand, type AppSceneSnapshot } from "./app-scene-model";

export type AppWorkspaceContribution = {
  onNewChat?: () => void;
  activeWorkspaceTabId: string;
  onClose: (tabId: string) => void;
  onSelect: (tabId: string) => void;
  ownerHref?: string;
  ownerTabId: string;
  primaryWorkspaceTabId: string;
  tabs: Array<{
    closeable?: boolean;
    draggable?: boolean;
    id: string;
    label: string;
  }>;
};

export type AppSceneWorkspace = {
  openNewChat?: () => boolean;
  dispatch: (command: AppSceneCommand) => void;
  routeSceneId: string;
  snapshot: AppSceneSnapshot;
  syncWorkspace: (contribution: AppWorkspaceContribution) => void;
};

const AppSceneWorkspaceContext = createContext<AppSceneWorkspace | null>(null);

export function AppSceneWorkspaceContextProvider({
  children,
  value,
}: {
  children: ReactNode;
  value: AppSceneWorkspace;
}) {
  return (
    <AppSceneWorkspaceContext.Provider value={value}>
      {children}
    </AppSceneWorkspaceContext.Provider>
  );
}

export function useAppSceneWorkspace() {
  const workspace = useContext(AppSceneWorkspaceContext);
  if (!workspace) {
    throw new Error(
      "useAppSceneWorkspace 必须在 AppSceneWorkspaceContextProvider 内使用",
    );
  }
  return workspace;
}

export function useOptionalAppSceneWorkspace() {
  return useContext(AppSceneWorkspaceContext);
}

/** 呈现生命周期沿用 Scene 的挂载身份；嵌入入口显式提供所属工作区身份。 */
export function useScenePresentationKey(explicitKey?: string): string {
  const workspace = useOptionalAppSceneWorkspace();
  if (explicitKey) return explicitKey;
  const scene = workspace?.snapshot.tabs.find(
    (tab) => tab.id === workspace.routeSceneId,
  );
  if (!scene?.mountKey) {
    throw new Error("聊天呈现需要 Scene 或工作区身份");
  }
  return scene.mountKey;
}

/** 当前渲染树是否对应用户选中的 Scene Tab。 */
export function useRouteSceneActive() {
  const workspace = useOptionalAppSceneWorkspace();
  if (!workspace) return true;
  return workspace.snapshot.activeTabId === workspace.routeSceneId;
}

export function AppSceneRouteScope({
  children,
  routeSceneId,
}: {
  children: ReactNode;
  routeSceneId: string;
}) {
  const workspace = useAppSceneWorkspace();
  const value = useMemo(
    () => ({ ...workspace, routeSceneId }),
    [routeSceneId, workspace],
  );

  return (
    <AppSceneWorkspaceContext.Provider value={value}>
      {children}
    </AppSceneWorkspaceContext.Provider>
  );
}
