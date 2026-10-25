import { describe, expect, it } from "vitest";
import {
  emptyWorkbenchState,
  reduceWorkbench,
  workbenchPaneById,
  workbenchPaneWithTab,
  workbenchPanes,
  type WorkbenchState,
} from "./workbench-model";

function focusedTabId(state: WorkbenchState) {
  return workbenchPaneById(state.layout, state.focusedPaneId)?.activeTabId;
}

function mintSequence() {
  let next = 0;
  return () => `id-${++next}`;
}

describe("right workbench tab domain", () => {
  it("同一工作台更新内容贡献身份时仍只保留一个固定画布", () => {
    const mint = mintSequence();
    const first = reduceWorkbench(
      emptyWorkbenchState(),
      { type: "register-content", contentId: "canvas:first", title: "画布" },
      mint,
    );
    const browser = reduceWorkbench(
      first,
      { type: "open-browser", title: "工具", newTab: true },
      mint,
    );
    const next = reduceWorkbench(
      browser,
      { type: "register-content", contentId: "canvas:next", title: "画布" },
      mint,
    );
    expect(next.tabs.filter((tab) => tab.kind === "content")).toHaveLength(1);
    expect(next.tabs[0]).toMatchObject({
      id: first.tabs[0]?.id,
      occurrenceId: first.tabs[0]?.occurrenceId,
      contentId: "canvas:next",
      closeable: false,
    });
    expect(next.layout).toBe(browser.layout);
    expect(next.tabs[1]).toBe(browser.tabs[1]);
    expect(focusedTabId(next)).toBe(focusedTabId(browser));
  });
  it("固定内容不可关闭，重复注册保留工具选择，关闭工具后回到固定内容", () => {
    const mint = mintSequence();
    const command = {
      type: "register-content" as const,
      contentId: "canvas:workflow",
      title: "画布",
    };
    const canvas = reduceWorkbench(emptyWorkbenchState(), command, mint);
    const canvasId = focusedTabId(canvas) ?? "";
    expect(reduceWorkbench(canvas, { type: "close", tabId: canvasId })).toBe(
      canvas,
    );
    const browser = reduceWorkbench(
      canvas,
      { type: "open-browser", title: "工具", newTab: true },
      mint,
    );
    const browserId = focusedTabId(browser) ?? "";
    const registered = reduceWorkbench(browser, command, mint);
    expect(registered).toBe(browser);
    expect(focusedTabId(registered)).toBe(browserId);
    const closed = reduceWorkbench(registered, {
      type: "close",
      tabId: browserId,
    });
    expect(closed.tabs).toHaveLength(1);
    expect(focusedTabId(closed)).toBe(canvasId);
  });
  it("重复打开看板复用同一个标签与 pane", () => {
    const mint = mintSequence();
    const opened = reduceWorkbench(
      emptyWorkbenchState(),
      { type: "open-task-board", title: "看板", paneId: "pane:root" },
      mint,
    );
    const tab = opened.tabs[0];
    const reopened = reduceWorkbench(
      opened,
      { type: "open-task-board", title: "看板" },
      mint,
    );
    expect(reopened.tabs).toHaveLength(1);
    expect(reopened.tabs[0]).toMatchObject({ id: tab?.id, kind: "task-board" });
    expect(reopened.layout).toEqual(opened.layout);
  });

  it("关闭最后一个标签时退出完整视图", () => {
    const opened = reduceWorkbench(
      emptyWorkbenchState(),
      { type: "open-browser", title: "新标签页", newTab: true },
      mintSequence(),
    );
    const fullscreen = reduceWorkbench(opened, {
      type: "set-fullscreen",
      fullscreen: true,
    });
    const closed = reduceWorkbench(fullscreen, {
      type: "close",
      tabId: focusedTabId(opened) ?? "",
    });
    expect(closed.tabs).toHaveLength(0);
    expect(closed.fullscreen).toBe(false);
  });

  it("文件复用内容身份，导航 revision 增长而 occurrence 保持", () => {
    const mint = mintSequence();
    const first = reduceWorkbench(
      emptyWorkbenchState(),
      {
        type: "open-file",
        title: "a.txt",
        rootPath: "/workspace",
        path: "a.txt",
      },
      mint,
    );
    const initial = first.tabs[0];
    const second = reduceWorkbench(
      first,
      {
        type: "open-file",
        title: "a.txt",
        rootPath: "/workspace",
        path: "a.txt",
      },
      mint,
    );
    expect(second.tabs).toHaveLength(1);
    expect(second.tabs[0]).toMatchObject({
      id: initial?.id,
      contentId: initial?.contentId,
      occurrenceId: initial?.occurrenceId,
      navigationRevision: 2,
    });
  });

  it("Git 差异标签按仓库、文件和暂存状态区分并复用身份", () => {
    const mint = mintSequence();
    const command = {
      type: "open-git-diff" as const,
      title: "src/报告 #1.ts",
      rootPath: "/workspace",
      repository: "/workspace/project",
      path: "src/报告 #1.ts",
      staged: false,
    };
    const first = reduceWorkbench(emptyWorkbenchState(), command, mint);
    const reopened = reduceWorkbench(first, command, mint);
    const staged = reduceWorkbench(
      reopened,
      { ...command, staged: true },
      mint,
    );
    expect(reopened.tabs).toHaveLength(1);
    expect(reopened.tabs[0]).toMatchObject({
      kind: "git-diff",
      occurrenceId: first.tabs[0]?.occurrenceId,
      navigationRevision: 2,
    });
    expect(staged.tabs).toHaveLength(2);
    expect(staged.tabs[1]).toMatchObject({
      gitRepository: "/workspace/project",
      gitStaged: true,
    });
  });

  it("浏览器、终端和侧聊每次新建都得到独立内容身份", () => {
    const mint = mintSequence();
    const first = reduceWorkbench(
      emptyWorkbenchState(),
      { type: "open-browser", title: "浏览器", newTab: true },
      mint,
    );
    const second = reduceWorkbench(
      first,
      { type: "open-browser", title: "浏览器", newTab: true },
      mint,
    );
    const third = reduceWorkbench(
      second,
      { type: "open-terminal", title: "终端", rootPath: "/workspace" },
      mint,
    );
    const fourth = reduceWorkbench(
      third,
      { type: "open-sidechat", title: "侧聊", parentChatId: "parent" },
      mint,
    );
    const identities = fourth.tabs.map((tab) => tab.contentId);
    expect(new Set(identities).size).toBe(4);
    expect(fourth.tabs[3]).toMatchObject({
      kind: "sidechat",
      parentChatId: "parent",
    });
    expect(fourth.tabs[3]?.createRequestId).toBeTruthy();
  });

  it("浏览器真实标题更新时保留标签身份和地址", () => {
    const opened = reduceWorkbench(
      emptyWorkbenchState(),
      { type: "open-browser", title: "浏览器", url: "https://example.com" },
      mintSequence(),
    );
    const updated = reduceWorkbench(opened, {
      type: "update-browser-title",
      tabId: focusedTabId(opened) ?? "",
      title: "Example",
    });
    expect(updated.tabs[0]).toMatchObject({
      id: opened.tabs[0]?.id,
      occurrenceId: opened.tabs[0]?.occurrenceId,
      title: "Example",
      url: "https://example.com",
    });
  });

  it("关闭选中标签后聚焦邻项，全视图与宽度独立于标签", () => {
    const mint = mintSequence();
    const files = reduceWorkbench(
      emptyWorkbenchState(),
      { type: "open-page", kind: "files", title: "文件" },
      mint,
    );
    const changes = reduceWorkbench(
      files,
      { type: "open-page", kind: "changes", title: "变更" },
      mint,
    );
    const full = reduceWorkbench(
      changes,
      { type: "set-fullscreen", fullscreen: true },
      mint,
    );
    const resized = reduceWorkbench(
      full,
      { type: "set-width", width: 999 },
      mint,
    );
    const closed = reduceWorkbench(
      resized,
      { type: "close", tabId: focusedTabId(changes) ?? "" },
      mint,
    );
    expect(focusedTabId(closed)).toBe(focusedTabId(files));
    expect(closed.tabs).toHaveLength(1);
    expect(closed.fullscreen).toBe(true);
    expect(closed.width).toBe(999);
  });

  it("交换左右窗格时保留标签身份和各自宽度", () => {
    const mint = mintSequence();
    const files = reduceWorkbench(
      emptyWorkbenchState(),
      { type: "open-page", kind: "files", title: "文件" },
      mint,
    );
    const browser = reduceWorkbench(
      files,
      { type: "open-browser", title: "浏览器", newTab: true },
      mint,
    );
    const split = reduceWorkbench(browser, {
      type: "move-tab",
      tabId: focusedTabId(files) ?? "",
      targetPaneId: "pane:root",
      zone: "left",
    });
    expect(split.layout.kind).toBe("split");
    if (split.layout.kind !== "split") return;
    const resized = reduceWorkbench(split, {
      type: "set-pane-ratio",
      splitId: split.layout.id,
      ratio: 0.3,
    });
    const swapped = reduceWorkbench(resized, {
      type: "swap-panes",
      splitId: split.layout.id,
    });
    expect(swapped.layout).toMatchObject({
      kind: "split",
      first: { id: split.layout.second.id },
      second: { id: split.layout.first.id },
      ratio: 0.7,
    });
    expect(swapped.tabs).toBe(browser.tabs);
    expect(swapped.focusedPaneId).toBe(split.focusedPaneId);
    expect(focusedTabId(swapped)).toBe(focusedTabId(split));
  });

  it("交换聊天与工作台所在侧时保留工作台内容和宽度", () => {
    const opened = reduceWorkbench(
      emptyWorkbenchState(),
      { type: "open-browser", title: "浏览器", newTab: true },
      mintSequence(),
    );
    const left = reduceWorkbench(opened, { type: "swap-side" });
    expect(left.side).toBe("left");
    expect(left.tabs).toBe(opened.tabs);
    expect(left.width).toBe(opened.width);
    expect(reduceWorkbench(left, { type: "swap-side" }).side).toBe("right");
  });

  it("重排 pane 标签保留各自内容身份", () => {
    const mint = mintSequence();
    const files = reduceWorkbench(
      emptyWorkbenchState(),
      { type: "open-page", kind: "files", title: "文件" },
      mint,
    );
    const browser = reduceWorkbench(
      files,
      { type: "open-browser", title: "浏览器", newTab: true },
      mint,
    );
    const terminal = reduceWorkbench(
      browser,
      { type: "open-terminal", title: "终端", rootPath: "/workspace" },
      mint,
    );
    const reordered = reduceWorkbench(terminal, {
      type: "move-tab",
      tabId: terminal.tabs[2]?.id ?? "",
      targetPaneId: "pane:root",
      zone: "center",
      beforeTabId: terminal.tabs[0]?.id,
    });
    expect(workbenchPanes(reordered.layout)[0]?.tabIds).toEqual([
      terminal.tabs[2]?.id,
      terminal.tabs[0]?.id,
      terminal.tabs[1]?.id,
    ]);
    expect(reordered.tabs).toEqual(terminal.tabs);
  });

  it("重命名同步打开的文件标签，删除后关闭该文件", () => {
    const opened = reduceWorkbench(
      emptyWorkbenchState(),
      {
        type: "open-file",
        title: "旧 #1.txt",
        rootPath: "/workspace",
        path: "目录/旧 #1.txt",
      },
      mintSequence(),
    );
    const renamed = reduceWorkbench(opened, {
      type: "rename-file",
      rootPath: "/workspace",
      oldPath: "目录/旧 #1.txt",
      newPath: "目录/新 #2.txt",
      title: "新 #2.txt",
    });
    expect(renamed.tabs[0]).toMatchObject({
      id: opened.tabs[0]?.id,
      occurrenceId: opened.tabs[0]?.occurrenceId,
      path: "目录/新 #2.txt",
      title: "新 #2.txt",
      navigationRevision: 2,
    });
    const deleted = reduceWorkbench(renamed, {
      type: "delete-file",
      rootPath: "/workspace",
      path: "目录/新 #2.txt",
    });
    expect(deleted.tabs).toEqual([]);
  });

  it("目录操作同步所有后代文件标签，且不影响同名前缀的其他目录", () => {
    const mint = mintSequence();
    let state = emptyWorkbenchState();
    for (const path of ["资料/a.md", "资料/子目录/b.md", "资料-备份/c.md"]) {
      state = reduceWorkbench(
        state,
        {
          type: "open-file",
          title: path.split("/").at(-1) ?? path,
          path,
          rootPath: "/workspace",
          canonicalPath: `/workspace/${path}`,
        },
        mint,
      );
    }
    const renamed = reduceWorkbench(state, {
      type: "rename-file",
      rootPath: "/workspace",
      oldPath: "资料",
      newPath: "文档",
      title: "文档",
      directory: true,
    });
    expect(renamed.tabs.map((tab) => tab.path)).toEqual([
      "文档/a.md",
      "文档/子目录/b.md",
      "资料-备份/c.md",
    ]);
    expect(renamed.tabs.map((tab) => tab.canonicalPath)).toEqual([
      "/workspace/文档/a.md",
      "/workspace/文档/子目录/b.md",
      "/workspace/资料-备份/c.md",
    ]);
    expect(renamed.tabs.map((tab) => tab.title)).toEqual([
      "a.md",
      "b.md",
      "c.md",
    ]);
    const deleted = reduceWorkbench(renamed, {
      type: "delete-file",
      rootPath: "/workspace",
      path: "文档",
      directory: true,
    });
    expect(deleted.tabs.map((tab) => tab.path)).toEqual(["资料-备份/c.md"]);
  });
});

describe("递归工作台 pane", () => {
  function threeTabs() {
    const mint = mintSequence();
    let state = emptyWorkbenchState();
    for (let index = 0; index < 3; index += 1) {
      state = reduceWorkbench(
        state,
        { type: "open-browser", title: `浏览器 ${index}`, newTab: true },
        mint,
      );
    }
    return { state, mint };
  }

  it("四边移动构成嵌套树并保留 tab 与挂载身份", () => {
    const { state, mint } = threeTabs();
    const [first, second, third] = state.tabs;
    const left = reduceWorkbench(
      state,
      {
        type: "move-tab",
        tabId: first?.id ?? "",
        targetPaneId: "pane:root",
        zone: "left",
      },
      mint,
    );
    expect(left.layout).toMatchObject({
      kind: "split",
      direction: "row",
      first: { kind: "pane", tabIds: [first?.id] },
      second: { kind: "pane", tabIds: [second?.id, third?.id] },
    });
    const thirdPane = workbenchPaneWithTab(left.layout, third?.id ?? "");
    const nested = reduceWorkbench(
      left,
      {
        type: "move-tab",
        tabId: second?.id ?? "",
        targetPaneId: thirdPane?.id ?? "",
        zone: "up",
      },
      mint,
    );
    expect(nested.layout).toMatchObject({
      kind: "split",
      direction: "row",
      second: { kind: "split", direction: "column" },
    });
    expect(workbenchPanes(nested.layout)).toHaveLength(3);
    expect(nested.tabs).toEqual(state.tabs);
    expect(nested.focusedPaneId).toBe(
      workbenchPaneWithTab(nested.layout, second?.id ?? "")?.id,
    );
  });

  it("各 pane 的加号只在对应 pane 创建浏览器标签", () => {
    const { state, mint } = threeTabs();
    const firstTab = state.tabs[0];
    const split = reduceWorkbench(
      state,
      {
        type: "move-tab",
        tabId: firstTab?.id ?? "",
        targetPaneId: "pane:root",
        zone: "left",
      },
      mint,
    );
    const target = workbenchPaneWithTab(split.layout, firstTab?.id ?? "");
    const next = reduceWorkbench(
      split,
      {
        type: "open-browser",
        title: "新标签页",
        newTab: true,
        paneId: target?.id,
      },
      mint,
    );
    const newTab = next.tabs.at(-1);
    expect(newTab?.kind).toBe("browser");
    expect(workbenchPaneWithTab(next.layout, newTab?.id ?? "")?.id).toBe(
      target?.id,
    );
    expect(next.focusedPaneId).toBe(target?.id);
    expect(
      workbenchPaneWithTab(next.layout, state.tabs[1]?.id ?? "")?.tabIds,
    ).toHaveLength(2);
  });

  it("跨 pane 中心合并和标签前插后收敛空 pane", () => {
    const { state, mint } = threeTabs();
    const [first, second, third] = state.tabs;
    const split = reduceWorkbench(
      state,
      {
        type: "move-tab",
        tabId: first?.id ?? "",
        targetPaneId: "pane:root",
        zone: "right",
      },
      mint,
    );
    const merged = reduceWorkbench(
      split,
      {
        type: "move-tab",
        tabId: first?.id ?? "",
        targetPaneId: "pane:root",
        zone: "center",
        beforeTabId: third?.id,
      },
      mint,
    );
    expect(merged.layout).toMatchObject({
      kind: "pane",
      id: "pane:root",
      tabIds: [second?.id, first?.id, third?.id],
      activeTabId: first?.id,
    });
    expect(merged.tabs.map((tab) => tab.occurrenceId)).toEqual(
      state.tabs.map((tab) => tab.occurrenceId),
    );
  });

  it("比例按分割节点独立更新，焦点与关闭操作按 pane 隔离", () => {
    const { state, mint } = threeTabs();
    const [first, second, third] = state.tabs;
    const split = reduceWorkbench(
      state,
      {
        type: "move-tab",
        tabId: first?.id ?? "",
        targetPaneId: "pane:root",
        zone: "left",
      },
      mint,
    );
    const splitId = split.layout.id;
    const resized = reduceWorkbench(split, {
      type: "set-pane-ratio",
      splitId,
      ratio: 0.99,
    });
    expect(resized.layout).toMatchObject({ ratio: 0.92 });
    const nudged = reduceWorkbench(resized, {
      type: "resize-pane",
      splitId,
      delta: -0.12,
    });
    expect(nudged.layout).toMatchObject({ ratio: 0.8 });
    const focused = reduceWorkbench(nudged, {
      type: "focus-pane",
      paneId: "pane:root",
    });
    expect(focusedTabId(focused)).toBe(third?.id);
    const closed = reduceWorkbench(focused, {
      type: "close",
      tabId: second?.id ?? "",
    });
    expect(closed.tabs.map((tab) => tab.id)).toEqual([first?.id, third?.id]);
    expect(workbenchPaneWithTab(closed.layout, first?.id ?? "")).toBeTruthy();
    expect(
      workbenchPaneWithTab(closed.layout, second?.id ?? ""),
    ).toBeUndefined();
  });

  it("自身唯一标签拖到边缘不创建空 pane，未知目标不改状态", () => {
    const mint = mintSequence();
    const state = reduceWorkbench(
      emptyWorkbenchState(),
      { type: "open-browser", title: "浏览器", newTab: true },
      mint,
    );
    const command = {
      type: "move-tab" as const,
      tabId: focusedTabId(state) ?? "",
      targetPaneId: "pane:root",
      zone: "up" as const,
    };
    expect(reduceWorkbench(state, command, mint)).toBe(state);
    expect(
      reduceWorkbench(state, { ...command, targetPaneId: "unknown" }, mint),
    ).toBe(state);
  });

  it("关闭嵌套焦点 pane 时优先聚焦相邻 pane", () => {
    const { state, mint } = threeTabs();
    const [first, second, third] = state.tabs;
    const left = reduceWorkbench(
      state,
      {
        type: "move-tab",
        tabId: first?.id ?? "",
        targetPaneId: "pane:root",
        zone: "left",
      },
      mint,
    );
    const nested = reduceWorkbench(
      left,
      {
        type: "move-tab",
        tabId: second?.id ?? "",
        targetPaneId: "pane:root",
        zone: "up",
      },
      mint,
    );
    const closed = reduceWorkbench(nested, {
      type: "close",
      tabId: second?.id ?? "",
    });
    expect(focusedTabId(closed)).toBe(third?.id);
    expect(workbenchPanes(closed.layout)).toHaveLength(2);
  });
});

it("多个产出 Tab 与固定画布共存，重开聚焦，关闭产出保留画布", () => {
  const mint = mintSequence();
  const canvas = reduceWorkbench(
    emptyWorkbenchState(),
    { type: "register-content", contentId: "workflow-canvas", title: "画布" },
    mint,
  );
  const reference = {
    type: "workflow-node-result",
    ids: {
      workflowId: "workflow",
      runId: "run",
      instanceId: "instance",
      attemptId: "old",
    },
  };
  const first = reduceWorkbench(
    canvas,
    { type: "open-content", contentId: "output:old", title: "产出", reference },
    mint,
  );
  const second = reduceWorkbench(
    first,
    {
      type: "open-content",
      contentId: "output:new",
      title: "新产出",
      reference: { ...reference, ids: { ...reference.ids, attemptId: "new" } },
    },
    mint,
  );
  const reopened = reduceWorkbench(
    second,
    { type: "open-content", contentId: "output:old", title: "产出", reference },
    mint,
  );
  expect(reopened.tabs).toHaveLength(3);
  expect(focusedTabId(reopened)).toBe(first.tabs[1]?.id);
  const registered = reduceWorkbench(
    reopened,
    { type: "register-content", contentId: "workflow-canvas", title: "画布" },
    mint,
  );
  expect(registered).toBe(reopened);
  const closed = reduceWorkbench(registered, {
    type: "close",
    tabId: first.tabs[1]?.id ?? "",
  });
  expect(closed.tabs.map((tab) => tab.contentId)).toEqual([
    "workflow-canvas",
    "output:new",
  ]);
});
