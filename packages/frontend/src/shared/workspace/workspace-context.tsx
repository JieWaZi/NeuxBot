import { i18n } from "@/shared/i18n";
import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";

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

const WorkspaceContext = createContext<SceneWorkspaceStore | null>(null);

function chatTabID(scopeKey: string) {
  return `chat:${scopeKey}`;
}

function memberTabID(scopeKey: string, wakerId: string) {
  return `member:${scopeKey}:${wakerId}`;
}

function initialSnapshot(
  scopeKey: string,
  label = i18n.t("chat:conversationTab"),
): SceneWorkspaceSnapshot {
  const id = chatTabID(scopeKey);
  return {
    activeTabId: id,
    tabs: [{ id, kind: "chat", label }],
  };
}

function remapTab(tab: WorkspaceTab, scopeKey: string): WorkspaceTab {
  switch (tab.kind) {
    case "chat":
      return { ...tab, id: chatTabID(scopeKey) };
    case "member":
      return { ...tab, id: memberTabID(scopeKey, tab.wakerId) };
  }
}

/** Context 只提供稳定的 UI 状态容器；成员标签的变化只通知所属范围。 */
class SceneWorkspaceStore {
  private readonly scopes = new Map<string, SceneWorkspaceSnapshot>();
  private readonly listeners = new Map<string, Set<() => void>>();

  getSnapshot(scopeKey: string) {
    return this.scopes.get(scopeKey);
  }

  subscribe(scopeKey: string, listener: () => void) {
    const listeners = this.listeners.get(scopeKey) ?? new Set<() => void>();
    listeners.add(listener);
    this.listeners.set(scopeKey, listeners);
    return () => {
      listeners.delete(listener);
      if (listeners.size === 0 && this.listeners.get(scopeKey) === listeners) {
        this.listeners.delete(scopeKey);
      }
    };
  }

  private emit(scopeKey: string) {
    for (const listener of [...(this.listeners.get(scopeKey) ?? [])]) {
      listener();
    }
  }

  dispatch(scopeKey: string, command: SceneWorkspaceCommand) {
    const current = this.scopes;
    if (command.type === "clear") {
      const key = command.scopeKey ?? scopeKey;
      if (current.delete(key)) this.emit(key);
      return;
    }
    if (command.type === "clear-prefix") {
      if (!command.scopePrefix) return;
      const keys = [...current.keys()].filter((key) =>
        key.startsWith(command.scopePrefix),
      );
      for (const key of keys) current.delete(key);
      for (const key of keys) this.emit(key);
      return;
    }
    if (command.type === "migrate") {
      const target = command.toScopeKey;
      const source = current.get(scopeKey);
      if (!source || !target || target === scopeKey) return;
      const tabs = source.tabs.map((tab) => remapTab(tab, target));
      const active = source.tabs.find((tab) => tab.id === source.activeTabId);
      current.delete(scopeKey);
      current.set(target, {
        activeTabId: active ? remapTab(active, target).id : chatTabID(target),
        tabs,
      });
      // 先完成两端的身份迁移，再通知消费者，避免读取中间状态。
      this.emit(scopeKey);
      this.emit(target);
      return;
    }

    const snapshot = current.get(scopeKey) ?? initialSnapshot(scopeKey);
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
        const existing = snapshot.tabs.find((item) => item.id === tab.id);
        if (
          existing?.kind === "member" &&
          existing.chatId === tab.chatId &&
          existing.label === tab.label &&
          snapshot.activeTabId === tab.id
        )
          return;
        next = {
          activeTabId: tab.id,
          tabs: existing
            ? snapshot.tabs.map((item) => (item.id === tab.id ? tab : item))
            : [...snapshot.tabs, tab],
        };
        break;
      }
      case "select-tab":
        if (!snapshot.tabs.some((tab) => tab.id === command.tabId)) return;
        if (snapshot.activeTabId === command.tabId) return;
        next = { ...snapshot, activeTabId: command.tabId };
        break;
      case "close-tab":
        if (
          command.tabId === chatTabID(scopeKey) ||
          !snapshot.tabs.some((tab) => tab.id === command.tabId)
        )
          return;
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
        if (sourceIndex < 0 || targetIndex < 0 || sourceIndex === targetIndex)
          return;
        const tabs = [...snapshot.tabs];
        const [source] = tabs.splice(sourceIndex, 1);
        if (!source) return;
        tabs.splice(targetIndex, 0, source);
        next = { ...snapshot, tabs };
        break;
      }
    }
    current.set(scopeKey, next);
    this.emit(scopeKey);
  }
}

export function WorkspaceProvider({ children }: { children: ReactNode }) {
  const [store] = useState(() => new SceneWorkspaceStore());

  return (
    <WorkspaceContext.Provider value={store}>
      {children}
    </WorkspaceContext.Provider>
  );
}

/** 读取当前页面的工作区控制器，并按会话范围返回主区域 Tab。 */
export function useSceneWorkspace(scopeKey: string): SceneWorkspace {
  const context = useContext(WorkspaceContext);
  const { t } = useTranslation("chat");
  const chatLabel = t("conversationTab");
  // 空范围不写入全局 Map；默认快照随消费者释放，并满足外部 Store 的身份要求。
  const fallback = useMemo(
    () => initialSnapshot(scopeKey, chatLabel),
    [scopeKey, chatLabel],
  );
  const subscribe = useCallback(
    (listener: () => void) =>
      context?.subscribe(scopeKey, listener) ?? (() => {}),
    [context, scopeKey],
  );
  const getSnapshot = useCallback(
    () => context?.getSnapshot(scopeKey) ?? fallback,
    [context, fallback, scopeKey],
  );
  const storedSnapshot = useSyncExternalStore(
    subscribe,
    getSnapshot,
    getSnapshot,
  );
  const snapshot = useMemo(() => {
    if (
      !storedSnapshot.tabs.some(
        (tab) => tab.kind === "chat" && tab.label !== chatLabel,
      )
    )
      return storedSnapshot;
    return {
      ...storedSnapshot,
      tabs: storedSnapshot.tabs.map((tab) =>
        tab.kind === "chat" ? { ...tab, label: chatLabel } : tab,
      ),
    };
  }, [storedSnapshot, chatLabel]);
  const dispatch = useCallback(
    (command: SceneWorkspaceCommand) => context?.dispatch(scopeKey, command),
    [context, scopeKey],
  );
  return useMemo(() => ({ snapshot, dispatch }), [snapshot, dispatch]);
}
