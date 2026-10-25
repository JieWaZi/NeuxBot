import {
  createContext,
  useContext,
  useEffect,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import { z } from "zod";

import {
  emptyWorkbenchState,
  projectWorkbench,
  reduceWorkbench,
  type WorkbenchCommand,
  type WorkbenchState,
} from "./workbench-model";
import { workbenchPanes, type WorkbenchNode } from "./workbench-pane";
import { preventUnloadWithDirtyDrafts } from "./document-buffers";

const storagePrefix = "ally.right-workbench.v2.";
const tabSchema = z.object({
  id: z.string().min(1),
  kind: z.enum([
    "files",
    "changes",
    "browser",
    "terminal",
    "file",
    "sidechat",
    "git-diff",
    "task-board",
    "content",
  ]),
  contentId: z.string().min(1),
  occurrenceId: z.string().min(1),
  navigationRevision: z.int().positive(),
  title: z.string(),
  closeable: z.boolean().optional(),
  contentReference: z
    .object({
      type: z.string().min(1).max(64),
      ids: z.record(z.string().max(64), z.string().max(128)),
    })
    .optional(),
  path: z.string().optional(),
  canonicalPath: z.string().optional(),
  rootPath: z.string().optional(),
  fileMode: z.enum(["preview", "diff"]).optional(),
  executionDiff: z
    .object({ chatId: z.string(), messageId: z.string() })
    .optional(),
  url: z.string().optional(),
  chatId: z.string().optional(),
  parentChatId: z.string().optional(),
  createRequestId: z.string().optional(),
  terminalSessionId: z.string().optional(),
  gitRepository: z.string().optional(),
  gitStaged: z.boolean().optional(),
  gitCommitHash: z.string().optional(),
});
const nodeSchema: z.ZodType<WorkbenchNode> = z.lazy(() =>
  z.union([
    z.object({
      kind: z.literal("pane"),
      id: z.string().min(1),
      tabIds: z.array(z.string().min(1)).max(64),
      activeTabId: z.string().optional(),
    }),
    z.object({
      kind: z.literal("split"),
      id: z.string().min(1),
      direction: z.enum(["row", "column"]),
      ratio: z.number().finite().min(0.08).max(0.92),
      first: nodeSchema,
      second: nodeSchema,
    }),
  ]),
);

const stateSchema = z.object({
  tabs: z.array(tabSchema).max(64),
  layout: nodeSchema,
  focusedPaneId: z.string().min(1),
  fullscreen: z.boolean(),
  width: z.number().finite().min(240),
  customWidth: z.boolean(),
  side: z.enum(["left", "right"]).optional(),
});
const persistedSchema = z.object({ version: z.literal(2), state: stateSchema });

export function restoreWorkbenchState(
  raw: unknown,
): WorkbenchState | undefined {
  let result;
  try {
    result = persistedSchema.safeParse(raw);
  } catch {
    return undefined;
  }
  if (!result.success) return undefined;
  const state = result.data.state;
  const nodeIds = new Set<string>();
  let nodeCount = 0;
  const visit = (node: WorkbenchNode): boolean => {
    nodeCount += 1;
    if (nodeCount > 127 || nodeIds.has(node.id)) return false;
    nodeIds.add(node.id);
    return node.kind === "pane" || (visit(node.first) && visit(node.second));
  };
  if (!visit(state.layout)) return undefined;
  const panes = workbenchPanes(state.layout);
  const tabIds = state.tabs.map((tab) => tab.id);
  const placedIds = panes.flatMap((pane) => pane.tabIds);
  if (
    state.tabs.some(
      (tab) =>
        tab.kind === "content" &&
        (tab.closeable === false
          ? Boolean(tab.contentReference)
          : tab.closeable !== true || !tab.contentReference),
    ) ||
    new Set(tabIds).size !== tabIds.length ||
    new Set(state.tabs.map((tab) => tab.contentId)).size !== tabIds.length ||
    state.tabs.filter(
      (tab) => tab.kind === "content" && tab.closeable === false,
    ).length > 1 ||
    new Set(placedIds).size !== placedIds.length ||
    tabIds.length !== placedIds.length ||
    tabIds.some((id) => !placedIds.includes(id)) ||
    !panes.some((pane) => pane.id === state.focusedPaneId) ||
    panes.some(
      (pane) =>
        (pane.activeTabId !== undefined &&
          !pane.tabIds.includes(pane.activeTabId)) ||
        (pane.tabIds.length > 0 && pane.activeTabId === undefined),
    ) ||
    (state.layout.kind === "split" &&
      panes.some((pane) => pane.tabIds.length === 0)) ||
    (tabIds.length === 0 && state.fullscreen)
  )
    return undefined;

  return projectWorkbench({
    ...emptyWorkbenchState(),
    ...state,
    side: state.side ?? "right",
    tabs: state.tabs.map((tab) => ({
      ...tab,
      occurrenceId: crypto.randomUUID(),
    })),
  });
}

function readState(scopeKey: string): WorkbenchState {
  try {
    const raw = localStorage.getItem(storagePrefix + scopeKey);
    if (!raw) return emptyWorkbenchState();
    const restored = restoreWorkbenchState(JSON.parse(raw));
    if (!restored) throw new Error("Invalid workbench layout");
    return restored;
  } catch {
    try {
      localStorage.removeItem(storagePrefix + scopeKey);
    } catch {
      // An unavailable storage backend still leaves a valid in-memory state.
    }
    return emptyWorkbenchState();
  }
}

export class WorkbenchStore {
  private readonly states = new Map<string, WorkbenchState>();
  private readonly listeners = new Map<string, Set<() => void>>();

  getSnapshot = (scopeKey: string): WorkbenchState => {
    let state = this.states.get(scopeKey);
    if (!state) {
      state = readState(scopeKey);
      this.states.set(scopeKey, state);
    }
    return state;
  };

  subscribe = (scopeKey: string, listener: () => void): (() => void) => {
    const listeners = this.listeners.get(scopeKey) ?? new Set();
    listeners.add(listener);
    this.listeners.set(scopeKey, listeners);
    return () => {
      listeners.delete(listener);
      if (listeners.size === 0) this.listeners.delete(scopeKey);
    };
  };

  private persist(scopeKey: string, state: WorkbenchState): void {
    try {
      localStorage.setItem(
        storagePrefix + scopeKey,
        JSON.stringify({
          version: 2,
          state: {
            tabs: state.tabs,
            layout: state.layout,
            focusedPaneId: state.focusedPaneId,
            fullscreen: state.fullscreen,
            width: state.width,
            customWidth: state.customWidth,
            side: state.side,
          },
        }),
      );
    } catch {
      // Tabs remain usable while persistence is unavailable.
    }
  }

  /** 首次接纳将当前草稿绑定至 Chat，沿用标签与原生挂载身份。 */
  bindScope = (fromScopeKey: string, toScopeKey: string): void => {
    if (fromScopeKey === toScopeKey) return;
    const state = this.getSnapshot(fromScopeKey);
    this.states.set(toScopeKey, state);
    this.persist(toScopeKey, state);
    try {
      localStorage.removeItem(storagePrefix + fromScopeKey);
    } catch {
      // 内存中的绑定不依赖本地存储可用性。
    }
    // 源快照保留至当前组件完成范围切换，避免接纳期间出现空工作台。
    for (const listener of this.listeners.get(toScopeKey) ?? []) listener();
  };

  dispatch = (scopeKey: string, command: WorkbenchCommand): void => {
    const previous = this.getSnapshot(scopeKey);
    const next = reduceWorkbench(previous, command);
    if (next === previous) return;
    this.states.set(scopeKey, next);
    this.persist(scopeKey, next);
    for (const listener of this.listeners.get(scopeKey) ?? []) listener();
  };
}

const WorkbenchContext = createContext<WorkbenchStore | null>(null);

export function WorkbenchProvider({ children }: { children: ReactNode }) {
  const [store] = useState(() => new WorkbenchStore());
  useEffect(() => {
    window.addEventListener("beforeunload", preventUnloadWithDirtyDrafts);
    return () =>
      window.removeEventListener("beforeunload", preventUnloadWithDirtyDrafts);
  }, []);
  return (
    <WorkbenchContext.Provider value={store}>
      {children}
    </WorkbenchContext.Provider>
  );
}

export function useWorkbench(scopeKey: string) {
  const store = useContext(WorkbenchContext);
  if (!store) throw new Error("Right workbench provider is missing");
  const snapshot = useSyncExternalStore(
    (listener) => store.subscribe(scopeKey, listener),
    () => store.getSnapshot(scopeKey),
    emptyWorkbenchState,
  );
  return {
    snapshot,
    dispatch: (command: WorkbenchCommand) => store.dispatch(scopeKey, command),
    bindToChat: (chatId: string) => store.bindScope(scopeKey, `chat:${chatId}`),
  };
}
