import { absoluteFileAddress, sessionFileAddress } from "./file-address";
import {
  adjacentWorkbenchPane,
  clampWorkbenchRatio,
  mapWorkbenchPane,
  mapWorkbenchSplit,
  removeWorkbenchPane,
  workbenchPaneById,
  workbenchPaneWithTab,
  workbenchPanes,
  type WorkbenchDropZone,
  type WorkbenchNode,
  type WorkbenchPane,
} from "./workbench-pane";

export {
  workbenchPaneById,
  workbenchPaneWithTab,
  workbenchPanes,
} from "./workbench-pane";
export type {
  WorkbenchDropZone,
  WorkbenchNode,
  WorkbenchPane,
  WorkbenchSplit,
} from "./workbench-pane";

export type WorkbenchContentReference = {
  type: string;
  ids: Record<string, string>;
};

/** 每个 Chat 有独立工作台；标签身份、内容身份和挂载实例分别维护。 */
export type WorkbenchTab = {
  id: string;
  kind:
    | "files"
    | "changes"
    | "browser"
    | "terminal"
    | "file"
    | "sidechat"
    | "git-diff"
    | "task-board"
    | "content";
  contentId: string;
  occurrenceId: string;
  navigationRevision: number;
  title: string;
  closeable?: boolean;
  contentReference?: WorkbenchContentReference;
  path?: string;
  canonicalPath?: string;
  rootPath?: string;
  fileMode?: "preview" | "diff";
  executionDiff?: { chatId: string; messageId: string };
  url?: string;
  chatId?: string;
  parentChatId?: string;
  createRequestId?: string;
  terminalSessionId?: string;
  gitRepository?: string;
  gitStaged?: boolean;
  gitCommitHash?: string;
};

export type WorkbenchState = {
  tabs: WorkbenchTab[];
  layout: WorkbenchNode;
  focusedPaneId: string;
  fullscreen: boolean;
  width: number;
  /** 用户显式调整后保留宽度，否则由入口采用默认布局。 */
  customWidth: boolean;
  side: "left" | "right";
};

export type WorkbenchCommand =
  | {
      type: "open-content";
      contentId: string;
      title: string;
      reference: WorkbenchContentReference;
    }
  | { type: "register-content"; contentId: string; title: string }
  | { type: "open-page"; kind: "files" | "changes"; title: string }
  | { type: "open-task-board"; title: string; paneId?: string }
  | {
      type: "open-browser";
      title: string;
      url?: string;
      rootPath?: string;
      newTab?: boolean;
      paneId?: string;
    }
  | { type: "open-terminal"; title: string; rootPath: string }
  | {
      type: "open-file";
      path: string;
      canonicalPath?: string;
      rootPath: string;
      title: string;
      mode?: "preview" | "diff";
      executionDiff?: { chatId: string; messageId: string };
    }
  | { type: "open-sidechat"; title: string; parentChatId?: string }
  | {
      type: "open-git-diff";
      title: string;
      rootPath: string;
      repository: string;
      path?: string;
      staged?: boolean;
      commitHash?: string;
    }
  | { type: "bind-sidechat"; tabId: string; chatId: string }
  | { type: "bind-terminal"; tabId: string; sessionId: string }
  | { type: "update-browser-url"; tabId: string; url: string }
  | { type: "update-browser-title"; tabId: string; title: string }
  | { type: "focus"; tabId: string }
  | { type: "focus-pane"; paneId: string }
  | {
      type: "move-tab";
      tabId: string;
      targetPaneId: string;
      zone: WorkbenchDropZone;
      beforeTabId?: string;
    }
  | { type: "set-pane-ratio"; splitId: string; ratio: number }
  | { type: "resize-pane"; splitId: string; delta: number }
  | { type: "swap-panes"; splitId: string }
  | { type: "close"; tabId: string }
  | {
      type: "rename-file";
      rootPath: string;
      oldPath: string;
      newPath: string;
      oldCanonicalPath?: string;
      newCanonicalPath?: string;
      title: string;
      directory?: boolean;
    }
  | {
      type: "delete-file";
      rootPath: string;
      path: string;
      canonicalPath?: string;
      directory?: boolean;
    }
  | { type: "set-fullscreen"; fullscreen: boolean }
  | { type: "set-width"; width: number }
  | { type: "swap-side" };

export const workbenchMinWidth = 240;
export const workbenchDefaultWidth = 320;

export function emptyWorkbenchState(): WorkbenchState {
  return {
    tabs: [],
    layout: { kind: "pane", id: "pane:root", tabIds: [] },
    focusedPaneId: "pane:root",
    fullscreen: false,
    width: workbenchDefaultWidth,
    customWidth: false,
    side: "right",
  };
}

export function projectWorkbench(state: WorkbenchState): WorkbenchState {
  const focused = workbenchPaneById(state.layout, state.focusedPaneId);
  if (focused) return state;
  const first = workbenchPanes(state.layout)[0];
  return first ? { ...state, focusedPaneId: first.id } : state;
}

function updatedPane(
  layout: WorkbenchNode,
  paneId: string,
  update: (pane: WorkbenchPane) => WorkbenchPane,
): WorkbenchNode {
  return mapWorkbenchPane(layout, paneId, update);
}

function removeTabFromPane(
  layout: WorkbenchNode,
  pane: WorkbenchPane,
  tabId: string,
): WorkbenchNode {
  const index = pane.tabIds.indexOf(tabId);
  return updatedPane(layout, pane.id, (current) => {
    const tabIds = current.tabIds.filter((id) => id !== tabId);
    return {
      ...current,
      tabIds,
      activeTabId:
        current.activeTabId === tabId
          ? tabIds[Math.min(index, tabIds.length - 1)]
          : current.activeTabId,
    };
  });
}

function insertTabInPane(
  layout: WorkbenchNode,
  paneId: string,
  tabId: string,
  beforeTabId?: string,
): WorkbenchNode {
  return updatedPane(layout, paneId, (pane) => {
    const index = beforeTabId ? pane.tabIds.indexOf(beforeTabId) : -1;
    const tabIds = [...pane.tabIds];
    tabIds.splice(index >= 0 ? index : tabIds.length, 0, tabId);
    return { ...pane, tabIds, activeTabId: tabId };
  });
}

function moveWorkbenchTab(
  state: WorkbenchState,
  tabId: string,
  targetPaneId: string,
  zone: WorkbenchDropZone,
  mint: () => string,
  beforeTabId?: string,
): WorkbenchState {
  const source = workbenchPaneWithTab(state.layout, tabId);
  const target = workbenchPaneById(state.layout, targetPaneId);
  if (!source || !target) return state;
  if (beforeTabId && !target.tabIds.includes(beforeTabId)) return state;
  if (zone === "center" && beforeTabId === tabId) return state;
  if (
    source.id === target.id &&
    source.tabIds.length === 1 &&
    zone !== "center"
  )
    return state;

  let layout = removeTabFromPane(state.layout, source, tabId);
  if (source.id !== target.id && source.tabIds.length === 1)
    layout = removeWorkbenchPane(layout, source.id) ?? layout;

  if (zone === "center") {
    layout = insertTabInPane(layout, targetPaneId, tabId, beforeTabId);
    return projectWorkbench({ ...state, layout, focusedPaneId: targetPaneId });
  }

  const newPane: WorkbenchPane = {
    kind: "pane",
    id: `pane:${mint()}`,
    tabIds: [tabId],
    activeTabId: tabId,
  };
  const newFirst = zone === "left" || zone === "up";
  layout = mapWorkbenchPane(layout, targetPaneId, (pane) => ({
    kind: "split",
    id: `split:${mint()}`,
    direction: zone === "left" || zone === "right" ? "row" : "column",
    ratio: 0.5,
    first: newFirst ? newPane : pane,
    second: newFirst ? pane : newPane,
  }));
  return projectWorkbench({ ...state, layout, focusedPaneId: newPane.id });
}

function closeWorkbenchTab(
  state: WorkbenchState,
  tabId: string,
): WorkbenchState {
  if (state.tabs.find((tab) => tab.id === tabId)?.closeable === false)
    return state;
  const pane = workbenchPaneWithTab(state.layout, tabId);
  if (!pane) return state;
  let layout = removeTabFromPane(state.layout, pane, tabId);
  const nextFocusedPaneId = adjacentWorkbenchPane(state.layout, pane.id)?.id;
  if (pane.tabIds.length === 1 && layout.kind === "split")
    layout = removeWorkbenchPane(layout, pane.id) ?? layout;
  const tabs = state.tabs.filter((tab) => tab.id !== tabId);
  const focusedPaneId = workbenchPaneById(layout, state.focusedPaneId)
    ? state.focusedPaneId
    : (nextFocusedPaneId ?? workbenchPanes(layout)[0]?.id ?? "pane:root");
  return projectWorkbench({
    ...state,
    layout,
    focusedPaneId,
    tabs,
    fullscreen: tabs.length > 0 && state.fullscreen,
  });
}

function activeWorkbenchTab(state: WorkbenchState): WorkbenchTab | undefined {
  const activeTabId = workbenchPaneById(
    state.layout,
    state.focusedPaneId,
  )?.activeTabId;
  return state.tabs.find((tab) => tab.id === activeTabId);
}

function renamedCanonicalPath(
  tab: WorkbenchTab,
  nextPath: string,
): string | undefined {
  if (!tab.canonicalPath || !tab.path) return undefined;
  const separator = tab.canonicalPath.includes("\\") ? "\\" : "/";
  const oldRelative = tab.path.replace(/[\\/]/g, separator);
  if (!tab.canonicalPath.endsWith(`${separator}${oldRelative}`))
    return undefined;
  return `${tab.canonicalPath.slice(0, -oldRelative.length)}${nextPath.replace(/[\\/]/g, separator)}`;
}

export function reduceWorkbench(
  state: WorkbenchState,
  command: WorkbenchCommand,
  mint: () => string = () => crypto.randomUUID(),
): WorkbenchState {
  const open = (
    kind: WorkbenchTab["kind"],
    contentId: string,
    title: string,
    extra: Partial<WorkbenchTab> = {},
    targetPaneId?: string,
  ): WorkbenchState => {
    const existing = state.tabs.find((tab) => tab.contentId === contentId);
    if (existing) {
      const tab = {
        ...existing,
        ...extra,
        navigationRevision: existing.navigationRevision + 1,
      };
      const pane = workbenchPaneWithTab(state.layout, tab.id);
      if (!pane) return state;
      const layout = updatedPane(state.layout, pane.id, (current) => ({
        ...current,
        activeTabId: tab.id,
      }));
      return projectWorkbench({
        ...state,
        layout,
        focusedPaneId: pane.id,
        tabs: state.tabs.map((item) => (item.id === existing.id ? tab : item)),
      });
    }
    const tab: WorkbenchTab = {
      id: mint(),
      kind,
      contentId,
      occurrenceId: mint(),
      navigationRevision: 1,
      title,
      ...extra,
    };
    const paneId =
      (targetPaneId && workbenchPaneById(state.layout, targetPaneId)?.id) ||
      workbenchPaneById(state.layout, state.focusedPaneId)?.id ||
      workbenchPanes(state.layout)[0]?.id ||
      "pane:root";
    const layout = insertTabInPane(state.layout, paneId, tab.id);
    return projectWorkbench({
      ...state,
      layout,
      focusedPaneId: paneId,
      tabs: [...state.tabs, tab],
    });
  };

  switch (command.type) {
    case "open-content":
      if (
        state.tabs.some(
          (tab) =>
            tab.contentId === command.contentId && tab.closeable === false,
        )
      )
        return state;
      return open("content", command.contentId, command.title, {
        closeable: true,
        contentReference: command.reference,
      });
    case "register-content": {
      // 调用方只贡献一个固定内容槽位，内容身份变化不新增标签。
      const existing = state.tabs.find(
        (tab) => tab.kind === "content" && tab.closeable === false,
      );
      if (!existing)
        return open("content", command.contentId, command.title, {
          closeable: false,
        });
      if (
        existing.contentId === command.contentId &&
        existing.title === command.title &&
        existing.closeable === false
      )
        return state;
      return {
        ...state,
        tabs: state.tabs.map((tab) =>
          tab.id === existing.id
            ? {
                ...tab,
                contentId: command.contentId,
                title: command.title,
                closeable: false,
                navigationRevision:
                  tab.navigationRevision +
                  Number(tab.contentId !== command.contentId),
              }
            : tab,
        ),
      };
    }
    case "open-page":
      return open(command.kind, `page:${command.kind}`, command.title);
    case "open-task-board":
      return open(
        "task-board",
        "page:task-board",
        command.title,
        {},
        command.paneId,
      );
    case "open-browser": {
      const active = activeWorkbenchTab(state);
      if (!command.newTab && active?.kind === "browser") {
        return open("browser", active.contentId, active.title, {
          url: command.url ?? active.url,
          rootPath: command.rootPath ?? active.rootPath,
        });
      }
      const browserId = mint();
      return open(
        "browser",
        `browser:${browserId}`,
        command.title,
        {
          url: command.url ?? "",
          rootPath: command.rootPath,
        },
        command.paneId,
      );
    }
    case "open-terminal": {
      const terminalId = mint();
      return open("terminal", `terminal:${terminalId}`, command.title, {
        rootPath: command.rootPath,
      });
    }
    case "open-file":
      return open(
        "file",
        `${command.canonicalPath ? absoluteFileAddress(command.canonicalPath) : sessionFileAddress(command.rootPath, command.path)}${
          command.executionDiff?.messageId
            ? `?diff=${encodeURIComponent(command.executionDiff.messageId)}`
            : ""
        }`,
        command.title,
        {
          path: command.path,
          canonicalPath: command.canonicalPath,
          rootPath: command.rootPath,
          fileMode: command.mode,
          executionDiff: command.executionDiff,
        },
      );
    case "open-sidechat": {
      const sidechatId = mint();
      return open("sidechat", `sidechat:${sidechatId}`, command.title, {
        parentChatId: command.parentChatId,
        createRequestId: mint(),
      });
    }
    case "open-git-diff":
      return open(
        "git-diff",
        "git-diff:" +
          JSON.stringify([
            command.rootPath,
            command.repository,
            command.path,
            command.staged,
            command.commitHash,
          ]),
        command.title,
        {
          rootPath: command.rootPath,
          gitRepository: command.repository,
          path: command.path,
          gitStaged: command.staged,
          gitCommitHash: command.commitHash,
        },
      );
    case "bind-sidechat":
      return {
        ...state,
        tabs: state.tabs.map((tab) =>
          tab.id === command.tabId && tab.kind === "sidechat"
            ? { ...tab, chatId: command.chatId }
            : tab,
        ),
      };
    case "bind-terminal":
      return {
        ...state,
        tabs: state.tabs.map((tab) =>
          tab.id === command.tabId && tab.kind === "terminal"
            ? { ...tab, terminalSessionId: command.sessionId }
            : tab,
        ),
      };
    case "update-browser-url":
      return {
        ...state,
        tabs: state.tabs.map((tab) =>
          tab.id === command.tabId && tab.kind === "browser"
            ? { ...tab, url: command.url }
            : tab,
        ),
      };
    case "update-browser-title":
      return {
        ...state,
        tabs: state.tabs.map((tab) =>
          tab.id === command.tabId && tab.kind === "browser"
            ? { ...tab, title: command.title }
            : tab,
        ),
      };
    case "focus": {
      const pane = workbenchPaneWithTab(state.layout, command.tabId);
      if (!pane) return state;
      const layout = updatedPane(state.layout, pane.id, (current) => ({
        ...current,
        activeTabId: command.tabId,
      }));
      return projectWorkbench({ ...state, layout, focusedPaneId: pane.id });
    }
    case "focus-pane": {
      if (!workbenchPaneById(state.layout, command.paneId)) return state;
      return projectWorkbench({ ...state, focusedPaneId: command.paneId });
    }
    case "move-tab":
      return moveWorkbenchTab(
        state,
        command.tabId,
        command.targetPaneId,
        command.zone,
        mint,
        command.beforeTabId,
      );
    case "set-pane-ratio": {
      if (!Number.isFinite(command.ratio)) return state;
      const layout = mapWorkbenchSplit(
        state.layout,
        command.splitId,
        (split) =>
          split.ratio === clampWorkbenchRatio(command.ratio)
            ? split
            : { ...split, ratio: clampWorkbenchRatio(command.ratio) },
      );
      return layout === state.layout
        ? state
        : projectWorkbench({ ...state, layout });
    }
    case "resize-pane": {
      if (!Number.isFinite(command.delta)) return state;
      const layout = mapWorkbenchSplit(
        state.layout,
        command.splitId,
        (split) => {
          const ratio = clampWorkbenchRatio(split.ratio + command.delta);
          return ratio === split.ratio ? split : { ...split, ratio };
        },
      );
      return layout === state.layout
        ? state
        : projectWorkbench({ ...state, layout });
    }
    case "swap-panes": {
      const layout = mapWorkbenchSplit(
        state.layout,
        command.splitId,
        (split) =>
          split.direction === "row"
            ? {
                ...split,
                first: split.second,
                second: split.first,
                ratio: 1 - split.ratio,
              }
            : split,
      );
      return layout === state.layout
        ? state
        : projectWorkbench({ ...state, layout });
    }
    case "close":
      return closeWorkbenchTab(state, command.tabId);
    case "rename-file":
      return {
        ...state,
        tabs: state.tabs.map((tab) =>
          tab.kind === "file" &&
          tab.fileMode !== "diff" &&
          ((command.oldCanonicalPath &&
            (tab.canonicalPath === command.oldCanonicalPath ||
              (command.directory &&
                tab.canonicalPath?.startsWith(
                  `${command.oldCanonicalPath}/`,
                )))) ||
            (tab.rootPath === command.rootPath &&
              (tab.path === command.oldPath ||
                (command.directory &&
                  tab.path?.startsWith(`${command.oldPath}/`)))))
            ? (() => {
                const nextPath = command.directory
                  ? `${command.newPath}${tab.path?.slice(command.oldPath.length) ?? ""}`
                  : tab.rootPath === command.rootPath
                    ? command.newPath
                    : tab.path?.replace(/[^/\\]+$/, command.title);
                const nextCanonicalPath = command.directory
                  ? command.oldCanonicalPath && command.newCanonicalPath
                    ? `${command.newCanonicalPath}${tab.canonicalPath?.slice(command.oldCanonicalPath.length) ?? ""}`
                    : renamedCanonicalPath(tab, nextPath ?? command.newPath)
                  : command.newCanonicalPath;
                return {
                  ...tab,
                  path: nextPath,
                  title: command.directory ? tab.title : command.title,
                  canonicalPath: nextCanonicalPath,
                  contentId: nextCanonicalPath
                    ? absoluteFileAddress(nextCanonicalPath)
                    : sessionFileAddress(
                        command.rootPath,
                        nextPath ?? command.newPath,
                      ),
                  navigationRevision: tab.navigationRevision + 1,
                };
              })()
            : tab,
        ),
      };
    case "delete-file": {
      const affected = state.tabs.filter(
        (tab) =>
          tab.kind === "file" &&
          tab.fileMode !== "diff" &&
          ((command.canonicalPath &&
            (tab.canonicalPath === command.canonicalPath ||
              (command.directory &&
                tab.canonicalPath?.startsWith(`${command.canonicalPath}/`)))) ||
            (tab.rootPath === command.rootPath &&
              (tab.path === command.path ||
                (command.directory &&
                  tab.path?.startsWith(`${command.path}/`))))),
      );
      return affected.reduce(
        (current, tab) =>
          reduceWorkbench(current, { type: "close", tabId: tab.id }),
        state,
      );
    }
    case "set-fullscreen":
      return { ...state, fullscreen: command.fullscreen };
    case "set-width":
      if (!Number.isFinite(command.width)) return state;
      return {
        ...state,
        width: Math.max(workbenchMinWidth, command.width),
        customWidth: true,
      };
    case "swap-side":
      return { ...state, side: state.side === "right" ? "left" : "right" };
  }
}
