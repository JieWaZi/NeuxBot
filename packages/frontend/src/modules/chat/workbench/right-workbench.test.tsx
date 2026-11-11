import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ComponentProps } from "react";

import "@/shared/i18n";
import * as platform from "@/shared/platform";
import { workbenchDocumentBuffers } from "./document-buffers";
import { RightWorkbench } from "./right-workbench";
import { WorkbenchProvider, useWorkbench } from "./workbench-store";

// Native views are outside this header contract; the real store and close guards stay mounted.
vi.mock("../components/chat-browser-view", () => ({
  ChatBrowserView: () => <div />,
}));
vi.mock("../components/chat-terminal-panel", () => ({
  ChatTerminalPanel: () => <div />,
}));
vi.mock("../components/workspace-file-viewer", () => ({
  WorkspaceFileViewer: () => <div />,
}));

const scopeKey = "chat:right-header";
const storageKey = `ally.right-workbench.${scopeKey}`;
const onEmpty = vi.fn();
const onFileSourceChange = vi.fn();

function State() {
  const { snapshot } = useWorkbench(scopeKey);
  return <output data-testid="state">{JSON.stringify(snapshot)}</output>;
}

function state() {
  return JSON.parse(screen.getByTestId("state").textContent ?? "{}");
}

function Harness(overrides: Partial<ComponentProps<typeof RightWorkbench>>) {
  return (
    <WorkbenchProvider>
      <State />
      <RightWorkbench
        scopeKey={scopeKey}
        parentBusy={false}
        visible
        workspaceDirectory="/workspace"
        parentChatId="chat-1"
        terminalWorkingDirectory="/workspace"
        fileWorkspaceProps={{
          rootPath: "/workspace",
          scopePath: "/workspace",
          chatFiles: [],
          fileSource: "chat",
          onFileSourceChange,
          onDownloadChatFile: async () => {},
          onDeleteChatFile: async () => true,
        }}
        changesProps={{ query: "", rootPath: "/workspace" }}
        taskBoard={<div />}
        onAddCodeReference={() => {}}
        onDismiss={() => {}}
        onEmpty={onEmpty}
        {...overrides}
      />
    </WorkbenchProvider>
  );
}

function mount(overrides: Partial<ComponentProps<typeof RightWorkbench>> = {}) {
  return render(<Harness {...overrides} />);
}

beforeEach(() => {
  onEmpty.mockClear();
  onFileSourceChange.mockClear();
  localStorage.setItem(
    storageKey,
    JSON.stringify({
      state: {
        tabs: [
          {
            id: "one",
            kind: "browser",
            title: "First",
            contentId: "browser:one",
            occurrenceId: "one",
            navigationRevision: 1,
          },
          {
            id: "two",
            kind: "browser",
            title: "Second",
            contentId: "browser:two",
            occurrenceId: "two",
            navigationRevision: 1,
          },
        ],
        layout: {
          kind: "pane",
          id: "pane:root",
          tabIds: ["one", "two"],
          activeTabId: "one",
        },
        focusedPaneId: "pane:root",
        fullscreen: false,
        side: "right",
        width: 320,
        customWidth: false,
      },
    }),
  );
});

afterEach(() => {
  localStorage.removeItem(storageKey);
  workbenchDocumentBuffers.discardPrefix(`${scopeKey}:`);
});

describe("右侧工作台顶栏", () => {
  it("加号打开已有工具菜单，按确认顺序显示且不直接创建浏览器", async () => {
    const user = userEvent.setup();
    mount();
    await user.click(screen.getByRole("button", { name: "添加工作台标签" }));
    expect(state().tabs).toHaveLength(2);
    const menu = screen.getByRole("menu");
    expect(
      within(menu)
        .getAllByRole("menuitem")
        .map((item) => item.textContent),
    ).toEqual(["浏览器", "终端", "文件", "改动", "侧聊", "看板"]);
    await user.click(within(menu).getByRole("menuitem", { name: "浏览器" }));
    expect(screen.getByRole("tab", { name: "新标签页" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(state().tabs).toHaveLength(3);
  });

  it("新增入口跟随页签处于同一滚动区，可从末tab经键盘打开菜单", async () => {
    const user = userEvent.setup();
    mount();
    const track = screen.getByRole("region", { name: "工作台标签" });
    const add = within(track).getByRole("button", { name: "添加工作台标签" });
    expect(
      within(track).getByRole("tab", { name: "Second" }),
    ).toBeInTheDocument();
    expect(
      within(track).queryByRole("button", { name: "完整视图" }),
    ).not.toBeInTheDocument();
    screen.getByRole("tab", { name: "First" }).focus();
    await user.keyboard("{End}");
    expect(screen.getByRole("tab", { name: "Second" })).toHaveFocus();
    await user.tab();
    expect(screen.getByRole("button", { name: "关闭“Second”" })).toHaveFocus();
    await user.tab();
    expect(add).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(
      screen.getByRole("menuitem", { name: "浏览器" }),
    ).toBeInTheDocument();
  });

  it("缺少原能力条件时不展示对应工具", async () => {
    const user = userEvent.setup();
    mount({
      parentChatId: undefined,
      terminalWorkingDirectory: undefined,
      fileWorkspaceProps: undefined,
      changesProps: undefined,
      taskBoard: undefined,
    });
    await user.click(screen.getByRole("button", { name: "添加工作台标签" }));
    expect(
      screen.getAllByRole("menuitem").map((item) => item.textContent),
    ).toEqual(["浏览器"]);
  });

  it("在未聚焦pane的加号打开终端时，使用该pane与原工作目录", async () => {
    const user = userEvent.setup();
    const persisted = JSON.parse(localStorage.getItem(storageKey) ?? "{}");
    persisted.state.layout = {
      kind: "split",
      id: "split:one",
      direction: "row",
      ratio: 0.5,
      first: {
        kind: "pane",
        id: "pane:root",
        tabIds: ["one"],
        activeTabId: "one",
      },
      second: {
        kind: "pane",
        id: "pane:other",
        tabIds: ["two"],
        activeTabId: "two",
      },
    };
    localStorage.setItem(storageKey, JSON.stringify(persisted));
    mount();
    const otherPaneAdd = screen.getAllByRole("button", {
      name: "添加工作台标签",
    })[1];
    if (!otherPaneAdd) throw new Error("Missing second pane header");
    await user.click(otherPaneAdd);
    await user.click(screen.getByRole("menuitem", { name: "终端" }));
    const terminal = state().tabs.find(
      (tab: { kind: string }) => tab.kind === "terminal",
    );
    expect(terminal.rootPath).toBe("/workspace");
    expect(state().layout.first.tabIds).toEqual(["one"]);
    expect(state().layout.second.tabIds).toContain(terminal.id);
  });

  it("Tab进入未聚焦pane后同步原pane焦点，Delete关闭恢复其选中tab", async () => {
    const user = userEvent.setup();
    const persisted = JSON.parse(localStorage.getItem(storageKey) ?? "{}");
    persisted.state.tabs.push({
      id: "three",
      kind: "browser",
      title: "Third",
      contentId: "browser:three",
      occurrenceId: "three",
      navigationRevision: 1,
    });
    persisted.state.layout = {
      kind: "split",
      id: "split:one",
      direction: "row",
      ratio: 0.5,
      first: {
        kind: "pane",
        id: "pane:root",
        tabIds: ["one", "two"],
        activeTabId: "one",
      },
      second: {
        kind: "pane",
        id: "pane:other",
        tabIds: ["three"],
        activeTabId: "three",
      },
    };
    persisted.state.focusedPaneId = "pane:other";
    localStorage.setItem(storageKey, JSON.stringify(persisted));
    mount();
    await user.tab();
    expect(screen.getByRole("tab", { name: "First" })).toHaveFocus();
    expect(state().focusedPaneId).toBe("pane:root");
    await user.keyboard("{Delete}");
    await waitFor(() =>
      expect(screen.getByRole("tab", { name: "Second" })).toHaveFocus(),
    );
    expect(screen.getByRole("tab", { name: "Second" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  it.each(["Delete", "Backspace"])(
    "%s关闭当前tab并将焦点交给原有相邻tab",
    async (key) => {
      const user = userEvent.setup();
      mount();
      screen.getByRole("tab", { name: "First" }).focus();
      await user.keyboard(`{${key}}`);
      expect(
        screen.queryByRole("tab", { name: "First" }),
      ).not.toBeInTheDocument();
      await waitFor(() =>
        expect(screen.getByRole("tab", { name: "Second" })).toHaveFocus(),
      );
      expect(onEmpty).not.toHaveBeenCalled();
    },
  );

  it("终端的键盘关闭等待原生释放后才移除tab并恢复焦点", async () => {
    const user = userEvent.setup();
    const persisted = JSON.parse(localStorage.getItem(storageKey) ?? "{}");
    persisted.state.tabs[0] = {
      ...persisted.state.tabs[0],
      kind: "terminal",
      rootPath: "/workspace",
    };
    localStorage.setItem(storageKey, JSON.stringify(persisted));
    let resolveClose: ((closed: boolean) => void) | undefined;
    const close = vi.spyOn(platform, "closeTerminal").mockImplementation(
      () =>
        new Promise<boolean>((resolve) => {
          resolveClose = resolve;
        }),
    );
    try {
      mount();
      screen.getByRole("tab", { name: "First" }).focus();
      await user.keyboard("{Delete}");
      await act(
        async () =>
          new Promise<void>((resolve) =>
            requestAnimationFrame(() => resolve()),
          ),
      );
      expect(close).toHaveBeenCalledWith("one");
      expect(screen.getByRole("tab", { name: "First" })).toHaveFocus();
      await act(async () => resolveClose?.(true));
      await waitFor(() =>
        expect(screen.getByRole("tab", { name: "Second" })).toHaveFocus(),
      );
      expect(
        screen.queryByRole("tab", { name: "First" }),
      ).not.toBeInTheDocument();
    } finally {
      close.mockRestore();
    }
  });

  it.each(["同pane选择其他tab", "切换其他pane", "隐藏Scene"])(
    "终端关闭等待期间%s后不抢回焦点",
    async (action) => {
      const user = userEvent.setup();
      const persisted = JSON.parse(localStorage.getItem(storageKey) ?? "{}");
      persisted.state.tabs[0] = {
        ...persisted.state.tabs[0],
        kind: "terminal",
        rootPath: "/workspace",
      };
      persisted.state.tabs.push({
        id: "three",
        kind: "browser",
        title: "Third",
        contentId: "browser:three",
        occurrenceId: "three",
        navigationRevision: 1,
      });
      persisted.state.layout.tabIds.push("three");
      if (action === "切换其他pane") {
        persisted.state.layout = {
          kind: "split",
          id: "split:one",
          direction: "row",
          ratio: 0.5,
          first: {
            kind: "pane",
            id: "pane:root",
            tabIds: ["one", "two"],
            activeTabId: "one",
          },
          second: {
            kind: "pane",
            id: "pane:other",
            tabIds: ["three"],
            activeTabId: "three",
          },
        };
      }
      localStorage.setItem(storageKey, JSON.stringify(persisted));
      let resolveClose: ((closed: boolean) => void) | undefined;
      const close = vi.spyOn(platform, "closeTerminal").mockImplementation(
        () =>
          new Promise<boolean>((resolve) => {
            resolveClose = resolve;
          }),
      );
      try {
        const view = mount();
        screen.getByRole("tab", { name: "First" }).focus();
        await user.keyboard("{Delete}");
        if (action === "隐藏Scene") view.rerender(<Harness visible={false} />);
        else await user.click(screen.getByRole("tab", { name: "Third" }));
        await act(async () => resolveClose?.(true));
        await act(
          async () =>
            new Promise<void>((resolve) =>
              requestAnimationFrame(() => resolve()),
            ),
        );
        expect(
          screen.queryByRole("tab", { name: "First" }),
        ).not.toBeInTheDocument();
        if (action === "隐藏Scene") {
          expect(document.body).toHaveFocus();
        } else {
          expect(screen.getByRole("tab", { name: "Third" })).toHaveAttribute(
            "aria-selected",
            "true",
          );
          expect(screen.getByRole("tab", { name: "Third" })).toHaveFocus();
        }
      } finally {
        close.mockRestore();
      }
    },
  );

  it("中键关闭非选中tab，关闭最后tab仍调用原空态回调", () => {
    mount();
    fireEvent(
      screen.getByRole("tab", { name: "Second" }),
      new MouseEvent("auxclick", { button: 1, bubbles: true }),
    );
    expect(
      screen.queryByRole("tab", { name: "Second" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "First" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    fireEvent(
      screen.getByRole("tab", { name: "First" }),
      new MouseEvent("auxclick", { button: 1, bubbles: true }),
    );
    expect(onEmpty).toHaveBeenCalledOnce();
  });

  it("Delete关闭仍经过脏文件确认，继续编辑保留原tab", async () => {
    const user = userEvent.setup();
    const persisted = JSON.parse(localStorage.getItem(storageKey) ?? "{}");
    persisted.state.tabs[0] = {
      ...persisted.state.tabs[0],
      kind: "file",
      path: "a.md",
      rootPath: "/workspace",
    };
    localStorage.setItem(storageKey, JSON.stringify(persisted));
    workbenchDocumentBuffers.begin(`${scopeKey}:one`, "saved");
    workbenchDocumentBuffers.change(`${scopeKey}:one`, "dirty");
    mount();
    screen.getByRole("tab", { name: "First" }).focus();
    await user.keyboard("{Delete}");
    expect(
      screen.getByRole("dialog", { name: "文件有未保存的修改" }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "继续编辑" }));
    expect(screen.getByRole("tab", { name: "First" })).toBeInTheDocument();
    expect(workbenchDocumentBuffers.isDirty(`${scopeKey}:one`)).toBe(true);
  });
});
