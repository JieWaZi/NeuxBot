import type { SceneTab } from "@/shared/ui/scene-tab-strip";

export type AppSceneKind = "route" | "settings" | "workspace";

export type AppSceneTab = SceneTab & {
  href?: string;
  kind: AppSceneKind;
  lastUsed: number;
  mountKey?: string;
  ownerTabId?: string;
  workspaceTabId?: string;
};

export type AppSceneSnapshot = {
  activeTabId?: string;
  tabs: AppSceneTab[];
};

export type AppSceneCommand =
  | { type: "open"; tab: Omit<AppSceneTab, "lastUsed">; now?: number }
  | { type: "select"; tabId: string; now?: number }
  | { type: "close"; tabId: string }
  | { type: "rename"; tabId: string; label: string }
  | {
      type: "sync-workspace";
      activeWorkspaceTabId: string;
      now?: number;
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

export function initialAppSceneSnapshot(
  tab: Omit<AppSceneTab, "lastUsed">,
  now = Date.now(),
): AppSceneSnapshot {
  return {
    activeTabId: tab.id,
    tabs: [{ ...tab, lastUsed: now, mountKey: tab.mountKey ?? tab.id }],
  };
}

function teamDraftChannelId(sceneId: string): string | undefined {
  if (!sceneId.startsWith("team-draft:")) return undefined;
  const rest = sceneId.slice("team-draft:".length);
  const separator = rest.indexOf(":");
  if (separator <= 0) return undefined;
  return rest.slice(0, separator);
}

function replacesTemporaryScene(
  active: AppSceneTab | undefined,
  next: Omit<AppSceneTab, "lastUsed">,
): active is AppSceneTab {
  if (active?.kind !== "route" || next.kind !== "route") return false;
  const replacingTemporaryChat =
    (active.id.startsWith("chat-entry:") ||
      active.id.startsWith("chat-draft:")) &&
    next.id.startsWith("chat:");
  if (replacingTemporaryChat) return true;
  if (
    active.id.startsWith("workflow-draft:") &&
    next.id.startsWith("workflow:")
  )
    return true;
  const channelId = teamDraftChannelId(active.id);
  return Boolean(channelId && next.id.startsWith(`team:${channelId}:thread:`));
}

export function appSceneReducer(
  snapshot: AppSceneSnapshot,
  command: AppSceneCommand,
): AppSceneSnapshot {
  if (command.type === "open") {
    if (command.tab.id.startsWith("workflow:")) {
      const runPrefix = `/workflows/${command.tab.id.slice("workflow:".length)}/runs/`;
      const tabs = snapshot.tabs.filter(
        (tab) =>
          !(
            tab.id.startsWith("workflow-run:") &&
            tab.href?.startsWith(runPrefix)
          ),
      );
      if (tabs.length !== snapshot.tabs.length)
        return appSceneReducer({ ...snapshot, tabs }, command);
    }
    const now = command.now ?? Date.now();
    const activeTab = snapshot.tabs.find(
      (tab) => tab.id === snapshot.activeTabId,
    );
    if (replacesTemporaryScene(activeTab, command.tab)) {
      const existing = snapshot.tabs.some((tab) => tab.id === command.tab.id);
      return {
        activeTabId: command.tab.id,
        tabs: existing
          ? snapshot.tabs
              .filter(
                (tab) =>
                  tab.id !== activeTab.id && tab.ownerTabId !== activeTab.id,
              )
              .map((tab) =>
                tab.id === command.tab.id
                  ? {
                      ...tab,
                      ...command.tab,
                      lastUsed: now,
                      mountKey: activeTab.mountKey ?? activeTab.id,
                    }
                  : tab,
              )
          : snapshot.tabs.flatMap((tab) => {
              if (tab.ownerTabId === activeTab.id) return [];
              return tab.id === activeTab.id
                ? [
                    {
                      ...command.tab,
                      lastUsed: now,
                      mountKey: activeTab.mountKey ?? activeTab.id,
                    },
                  ]
                : [tab];
            }),
      };
    }
    const existing = snapshot.tabs.some((tab) => tab.id === command.tab.id);
    return {
      activeTabId: command.tab.id,
      tabs: existing
        ? snapshot.tabs.map((tab) =>
            tab.id === command.tab.id
              ? {
                  ...tab,
                  ...command.tab,
                  lastUsed: now,
                  mountKey:
                    tab.mountKey ?? command.tab.mountKey ?? command.tab.id,
                }
              : tab,
          )
        : [
            ...snapshot.tabs,
            {
              ...command.tab,
              lastUsed: now,
              mountKey: command.tab.mountKey ?? command.tab.id,
            },
          ],
    };
  }

  if (command.type === "select") {
    if (!snapshot.tabs.some((tab) => tab.id === command.tabId)) return snapshot;
    const now = command.now ?? Date.now();
    return {
      activeTabId: command.tabId,
      tabs: snapshot.tabs.map((tab) =>
        tab.id === command.tabId ? { ...tab, lastUsed: now } : tab,
      ),
    };
  }

  if (command.type === "rename") {
    return {
      ...snapshot,
      tabs: snapshot.tabs.map((tab) =>
        tab.id === command.tabId ? { ...tab, label: command.label } : tab,
      ),
    };
  }

  if (command.type === "sync-workspace") {
    const now = command.now ?? Date.now();
    const incoming = command.tabs.filter(
      (tab) => tab.id !== command.primaryWorkspaceTabId,
    );
    const incomingIds = new Set(incoming.map((tab) => tab.id));
    const retained = snapshot.tabs.filter(
      (tab) =>
        tab.kind !== "workspace" ||
        tab.ownerTabId !== command.ownerTabId ||
        (tab.workspaceTabId ? incomingIds.has(tab.workspaceTabId) : false),
    );
    const nextTabs = [...retained];
    for (const workspaceTab of incoming) {
      const id = workspaceSceneTabId(command.ownerTabId, workspaceTab.id);
      const index = nextTabs.findIndex((tab) => tab.id === id);
      const previous = index >= 0 ? nextTabs[index] : undefined;
      const next: AppSceneTab = {
        closeable: workspaceTab.closeable ?? true,
        draggable: workspaceTab.draggable,
        href: command.ownerHref,
        id,
        kind: "workspace",
        label: workspaceTab.label,
        lastUsed: previous?.lastUsed ?? now,
        mountKey: previous?.mountKey ?? id,
        ownerTabId: command.ownerTabId,
        workspaceTabId: workspaceTab.id,
      };
      if (index >= 0) nextTabs[index] = next;
      else nextTabs.push(next);
    }

    const activeBelongsToOwner =
      snapshot.activeTabId === command.ownerTabId ||
      snapshot.tabs.some(
        (tab) =>
          tab.id === snapshot.activeTabId &&
          tab.kind === "workspace" &&
          tab.ownerTabId === command.ownerTabId,
      );
    const desiredActiveTabId =
      command.activeWorkspaceTabId === command.primaryWorkspaceTabId
        ? command.ownerTabId
        : workspaceSceneTabId(command.ownerTabId, command.activeWorkspaceTabId);
    const activeTabId =
      activeBelongsToOwner &&
      nextTabs.some((tab) => tab.id === desiredActiveTabId)
        ? desiredActiveTabId
        : snapshot.activeTabId;
    const activationChanged = activeTabId !== snapshot.activeTabId;
    const tabs = nextTabs.map((tab) =>
      activationChanged && tab.id === activeTabId
        ? { ...tab, lastUsed: now }
        : tab,
    );
    const unchanged =
      activeTabId === snapshot.activeTabId &&
      tabs.length === snapshot.tabs.length &&
      tabs.every((tab, index) => {
        const previous = snapshot.tabs[index];
        return (
          previous &&
          previous.id === tab.id &&
          previous.label === tab.label &&
          previous.href === tab.href &&
          previous.closeable === tab.closeable &&
          previous.draggable === tab.draggable &&
          previous.lastUsed === tab.lastUsed
        );
      });
    return unchanged ? snapshot : { activeTabId, tabs };
  }

  const closing = snapshot.tabs.find((tab) => tab.id === command.tabId);
  if (!closing?.closeable) return snapshot;
  const tabs = snapshot.tabs.filter(
    (tab) =>
      tab.id !== command.tabId &&
      !(tab.kind === "workspace" && tab.ownerTabId === command.tabId),
  );
  if (snapshot.activeTabId !== command.tabId) return { ...snapshot, tabs };
  const fallback = [...tabs].sort(
    (left, right) => right.lastUsed - left.lastUsed,
  )[0];
  return { activeTabId: fallback?.id, tabs };
}

export function workspaceSceneTabId(
  ownerTabId: string,
  workspaceTabId: string,
) {
  return `workspace:${ownerTabId}:${workspaceTabId}`;
}
