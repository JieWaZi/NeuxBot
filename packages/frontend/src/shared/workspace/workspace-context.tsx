import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";

import type { WorkspaceTab } from "./workspace-tab";

export type SceneWorkspaceSnapshot = {
  activeTabId: string;
  tabs: WorkspaceTab[];
};

export type SceneWorkspaceCommand =
  | { type: "clear"; scopeKey?: string }
  | { type: "clear-prefix"; scopePrefix: string }
  | { type: "close-tab"; tabId: string }
  | { type: "migrate"; toScopeKey: string }
  | {
      type: "open-member";
      member: { chatId?: string; label: string; wakerId: string };
    }
  | { type: "reorder-tab"; sourceTabId: string; targetTabId: string }
  | { type: "select-tab"; tabId: string };

export type SceneWorkspace = {
  snapshot: SceneWorkspaceSnapshot;
  dispatch: (command: SceneWorkspaceCommand) => void;
};

type SceneWorkspaceContextValue = {
  dispatch: (scopeKey: string, command: SceneWorkspaceCommand) => void;
  getSnapshot: (scopeKey: string) => SceneWorkspaceSnapshot;
};

const WorkspaceContext = createContext<SceneWorkspaceContextValue | null>(null);

function chatTabID(scopeKey: string) {
  return `chat:${scopeKey}`;
}

function memberTabID(scopeKey: string, wakerId: string) {
  return `member:${scopeKey}:${wakerId}`;
}

function initialSnapshot(scopeKey: string): SceneWorkspaceSnapshot {
  const id = chatTabID(scopeKey);
  return { activeTabId: id, tabs: [{ id, kind: "chat", label: "对话" }] };
}

function remapTab(tab: WorkspaceTab, scopeKey: string): WorkspaceTab {
  switch (tab.kind) {
    case "chat":
      return { ...tab, id: chatTabID(scopeKey) };
    case "member":
      return { ...tab, id: memberTabID(scopeKey, tab.wakerId) };
  }
}

function withoutScopes(
  scopes: Record<string, SceneWorkspaceSnapshot>,
  remove: (key: string) => boolean,
): Record<string, SceneWorkspaceSnapshot> {
  return Object.fromEntries(
    Object.entries(scopes).filter(([key]) => !remove(key)),
  );
}

export function WorkspaceProvider({ children }: { children: ReactNode }) {
  const [scopes, setScopes] = useState<Record<string, SceneWorkspaceSnapshot>>(
    {},
  );

  const dispatch = useCallback(
    (scopeKey: string, command: SceneWorkspaceCommand) => {
      setScopes((current) => {
        if (command.type === "clear") {
          const key = command.scopeKey ?? scopeKey;
          if (!(key in current)) return current;
          return withoutScopes(current, (candidate) => candidate === key);
        }
        if (command.type === "clear-prefix") {
          if (!command.scopePrefix) return current;
          const keys = Object.keys(current).filter((key) =>
            key.startsWith(command.scopePrefix),
          );
          if (keys.length === 0) return current;
          return withoutScopes(current, (key) =>
            key.startsWith(command.scopePrefix),
          );
        }
        if (command.type === "migrate") {
          const target = command.toScopeKey;
          const source = current[scopeKey];
          if (!source || !target || target === scopeKey) return current;
          const tabs = source.tabs.map((tab) => remapTab(tab, target));
          const active = source.tabs.find(
            (tab) => tab.id === source.activeTabId,
          );
          const next = withoutScopes(current, (key) => key === scopeKey);
          next[target] = {
            activeTabId: active
              ? remapTab(active, target).id
              : chatTabID(target),
            tabs,
          };
          return next;
        }

        const snapshot = current[scopeKey] ?? initialSnapshot(scopeKey);
        let next: SceneWorkspaceSnapshot;
        switch (command.type) {
          case "open-member": {
            const tab: WorkspaceTab = {
              chatId: command.member.chatId,
              id: memberTabID(scopeKey, command.member.wakerId),
              kind: "member",
              label: command.member.label,
              wakerId: command.member.wakerId,
            };
            const existing = snapshot.tabs.some((item) => item.id === tab.id);
            next = {
              activeTabId: tab.id,
              tabs: existing
                ? snapshot.tabs.map((item) => (item.id === tab.id ? tab : item))
                : [...snapshot.tabs, tab],
            };
            break;
          }
          case "select-tab":
            if (!snapshot.tabs.some((tab) => tab.id === command.tabId))
              return current;
            if (snapshot.activeTabId === command.tabId) return current;
            next = { ...snapshot, activeTabId: command.tabId };
            break;
          case "close-tab":
            if (
              command.tabId === chatTabID(scopeKey) ||
              !snapshot.tabs.some((tab) => tab.id === command.tabId)
            )
              return current;
            next = {
              activeTabId:
                snapshot.activeTabId === command.tabId
                  ? chatTabID(scopeKey)
                  : snapshot.activeTabId,
              tabs: snapshot.tabs.filter((tab) => tab.id !== command.tabId),
            };
            break;
          case "reorder-tab": {
            const sourceIndex = snapshot.tabs.findIndex(
              (tab) => tab.id === command.sourceTabId && tab.kind === "member",
            );
            const targetIndex = snapshot.tabs.findIndex(
              (tab) => tab.id === command.targetTabId && tab.kind === "member",
            );
            if (
              sourceIndex < 0 ||
              targetIndex < 0 ||
              sourceIndex === targetIndex
            )
              return current;
            const tabs = [...snapshot.tabs];
            const [source] = tabs.splice(sourceIndex, 1);
            if (!source) return current;
            tabs.splice(targetIndex, 0, source);
            next = { ...snapshot, tabs };
            break;
          }
        }
        return { ...current, [scopeKey]: next };
      });
    },
    [],
  );

  const getSnapshot = useCallback(
    (scopeKey: string) => scopes[scopeKey] ?? initialSnapshot(scopeKey),
    [scopes],
  );
  const value = useMemo<SceneWorkspaceContextValue>(
    () => ({ dispatch, getSnapshot }),
    [dispatch, getSnapshot],
  );

  return (
    <WorkspaceContext.Provider value={value}>
      {children}
    </WorkspaceContext.Provider>
  );
}

/** 读取当前页面的工作区控制器，并按会话范围返回主区域 Tab。 */
export function useSceneWorkspace(scopeKey: string): SceneWorkspace {
  const context = useContext(WorkspaceContext);
  if (!context) {
    return { snapshot: initialSnapshot(scopeKey), dispatch: () => undefined };
  }
  return {
    snapshot: context.getSnapshot(scopeKey),
    dispatch: (command) => context.dispatch(scopeKey, command),
  };
}
