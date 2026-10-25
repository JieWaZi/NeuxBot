import { describe, expect, it } from "vitest";

import {
  emptyWorkbenchState,
  reduceWorkbench,
  workbenchPaneById,
} from "./workbench-model";
import { restoreWorkbenchState, WorkbenchStore } from "./workbench-store";

function persistedState() {
  let next = 0;
  const mint = () => `id-${++next}`;
  const first = reduceWorkbench(
    emptyWorkbenchState(),
    { type: "open-browser", title: "浏览器", newTab: true },
    mint,
  );
  const second = reduceWorkbench(
    first,
    { type: "open-terminal", title: "终端", rootPath: "/workspace" },
    mint,
  );
  const split = reduceWorkbench(
    second,
    {
      type: "move-tab",
      tabId: first.tabs[0]?.id ?? "",
      targetPaneId: "pane:root",
      zone: "left",
    },
    mint,
  );
  return {
    version: 2,
    state: {
      tabs: split.tabs,
      layout: split.layout,
      focusedPaneId: split.focusedPaneId,
      fullscreen: split.fullscreen,
      width: split.width,
      customWidth: split.customWidth,
    },
  };
}

describe("工作台 v2 状态恢复", () => {
  it("首条接纳绑定正式 Chat 时保留工具身份、布局和选择并保存正式范围", () => {
    const store = new WorkbenchStore();
    const draft = "draft:workflow:first";
    const chat = "chat:accepted";
    store.dispatch(draft, {
      type: "register-content",
      contentId: "workflow-canvas",
      title: "画布",
    });
    store.dispatch(draft, {
      type: "open-browser",
      title: "浏览器",
      newTab: true,
    });
    store.dispatch(draft, { type: "set-width", width: 520 });
    const before = store.getSnapshot(draft);
    store.bindScope(draft, chat);
    expect(store.getSnapshot(chat)).toBe(before);
    expect(store.getSnapshot(draft)).toBe(before);
    const reopened = new WorkbenchStore().getSnapshot(chat);
    expect(reopened.tabs.map((tab) => tab.id)).toEqual(
      before.tabs.map((tab) => tab.id),
    );
    expect(reopened.layout).toEqual(before.layout);
    expect(reopened.width).toBe(520);
    expect(reopened.customWidth).toBe(true);
    expect(localStorage.getItem(`ally.right-workbench.v2.${draft}`)).toBeNull();
    localStorage.removeItem(`ally.right-workbench.v2.${chat}`);
  });
  it("恢复固定内容及所选工具时保持不可关闭规则", () => {
    const content = reduceWorkbench(emptyWorkbenchState(), {
      type: "register-content",
      contentId: "canvas:workflow",
      title: "画布",
    });
    const tools = reduceWorkbench(content, {
      type: "open-browser",
      title: "工具",
      newTab: true,
    });
    const restored = restoreWorkbenchState({ version: 2, state: tools });
    expect(restored).toBeDefined();
    if (!restored) return;
    expect(restored.tabs[0]?.closeable).toBe(false);
    expect(
      reduceWorkbench(restored, {
        type: "close",
        tabId: restored.tabs[0]?.id ?? "",
      }),
    ).toBe(restored);
    expect(
      workbenchPaneById(restored.layout, restored.focusedPaneId)?.activeTabId,
    ).toBe(tools.tabs[1]?.id);
  });

  it("不恢复含多个固定内容槽位的无效工作台", () => {
    const content = reduceWorkbench(emptyWorkbenchState(), {
      type: "register-content",
      contentId: "canvas:first",
      title: "画布",
    });
    const tab = content.tabs[0];
    expect(tab).toBeDefined();
    if (!tab) return;
    const duplicate = {
      ...tab,
      id: "second",
      occurrenceId: "second-instance",
      contentId: "canvas:next",
    };
    expect(
      restoreWorkbenchState({
        version: 2,
        state: {
          ...content,
          tabs: [tab, duplicate],
          layout: {
            kind: "pane",
            id: "pane:root",
            tabIds: [tab.id, duplicate.id],
            activeTabId: duplicate.id,
          },
        },
      }),
    ).toBeUndefined();
  });

  it("恢复超过旧固定上限的工作台宽度", () => {
    const persisted = persistedState();
    expect(
      restoreWorkbenchState({
        ...persisted,
        state: { ...persisted.state, width: 1_080 },
      })?.width,
    ).toBe(1_080);
  });

  it("旧状态默认工作台在右侧，新状态恢复左侧布局", () => {
    const persisted = persistedState();
    expect(restoreWorkbenchState(persisted)?.side).toBe("right");
    expect(
      restoreWorkbenchState({
        ...persisted,
        state: { ...persisted.state, side: "left" },
      })?.side,
    ).toBe("left");
  });

  it("恢复嵌套布局和原生身份，只更新前端挂载身份", () => {
    const persisted = persistedState();
    const restored = restoreWorkbenchState(persisted);
    expect(restored?.layout).toEqual(persisted.state.layout);
    expect(restored?.focusedPaneId).toBe(persisted.state.focusedPaneId);
    expect(restored?.tabs.map((tab) => tab.id)).toEqual(
      persisted.state.tabs.map((tab) => tab.id),
    );
    expect(restored?.tabs[0]?.occurrenceId).not.toBe(
      persisted.state.tabs[0]?.occurrenceId,
    );
    expect(
      restored &&
        workbenchPaneById(restored.layout, restored.focusedPaneId)?.activeTabId,
    ).toBe(persisted.state.tabs[0]?.id);
  });

  it("拒绝旧版和重复节点、遗漏标签、失效焦点", () => {
    const persisted = persistedState();
    expect(restoreWorkbenchState({ ...persisted, version: 1 })).toBeUndefined();
    expect(
      restoreWorkbenchState({
        ...persisted,
        state: { ...persisted.state, focusedPaneId: "missing" },
      }),
    ).toBeUndefined();
    expect(
      restoreWorkbenchState({
        ...persisted,
        state: { ...persisted.state, tabs: persisted.state.tabs.slice(1) },
      }),
    ).toBeUndefined();
    if (persisted.state.layout.kind !== "split") return;
    expect(
      restoreWorkbenchState({
        ...persisted,
        state: {
          ...persisted.state,
          layout: {
            ...persisted.state.layout,
            second: {
              ...persisted.state.layout.second,
              id: persisted.state.layout.first.id,
            },
          },
        },
      }),
    ).toBeUndefined();
  });

  it("异常布局不抛错，也不恢复悬空或重复的标签身份", () => {
    const persisted = persistedState();
    expect(
      restoreWorkbenchState({
        ...persisted,
        state: {
          ...persisted.state,
          tabs: persisted.state.tabs.map((tab) => ({
            ...tab,
            contentId: "duplicate",
          })),
        },
      }),
    ).toBeUndefined();
    const cyclic: Record<string, unknown> = { kind: "split", id: "cycle" };
    cyclic.first = cyclic;
    cyclic.second = persisted.state.layout;
    cyclic.direction = "row";
    cyclic.ratio = 0.5;
    expect(
      restoreWorkbenchState({
        ...persisted,
        state: { ...persisted.state, layout: cyclic },
      }),
    ).toBeUndefined();
  });
});

it("持久化多个产出引用和固定画布，恢复后保留独立尝试且不保存页面数据", () => {
  let sequence = 0;
  const mint = () => `result-${++sequence}`;
  const canvas = reduceWorkbench(
    emptyWorkbenchState(),
    { type: "register-content", contentId: "workflow-canvas", title: "画布" },
    mint,
  );
  const reference = {
    type: "workflow-node-result",
    ids: { workflowId: "w", runId: "r", instanceId: "i", attemptId: "old" },
  };
  const first = reduceWorkbench(
    canvas,
    { type: "open-content", contentId: "result:old", title: "产出", reference },
    mint,
  );
  const second = reduceWorkbench(
    first,
    {
      type: "open-content",
      contentId: "result:new",
      title: "产出",
      reference: { ...reference, ids: { ...reference.ids, attemptId: "new" } },
    },
    mint,
  );
  const restored = restoreWorkbenchState({ version: 2, state: second });
  expect(restored?.tabs).toHaveLength(3);
  expect(restored?.tabs[1]?.contentReference?.ids.attemptId).toBe("old");
  expect(restored?.tabs[2]?.contentReference?.ids.attemptId).toBe("new");
  expect(
    reduceWorkbench(second, {
      type: "open-content",
      contentId: "workflow-canvas",
      title: "产出",
      reference,
    }),
  ).toBe(second);
});
