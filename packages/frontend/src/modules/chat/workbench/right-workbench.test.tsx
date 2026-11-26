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
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import "@/shared/i18n";
import * as platform from "@/shared/platform";
import { parseReference } from "@/shared/workspace/file-reference";
import {
  FileReferenceProvider,
  ReferenceLink,
} from "@/shared/workspace/file-reference-link";
import { useChatWorkspaceBindings } from "../components/use-chat-workspace-bindings";
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
  WorkspaceFileViewer: ({
    scopeChatId,
    highlightLines,
  }: {
    scopeChatId?: string;
    highlightLines?: { startLine: number; endLine: number };
  }) => (
    <div
      data-testid="file-preview"
      data-chat-id={scopeChatId}
      data-start-line={highlightLines?.startLine}
      data-end-line={highlightLines?.endLine}
    />
  ),
}));
vi.mock("../components/workspace-file-browser", () => ({
  WorkspaceFileBrowser: ({
    onOpenFile,
  }: {
    onOpenFile: (file: { name: string; path: string }) => void;
  }) => (
    <button
      type="button"
      onClick={() => onOpenFile({ name: "note.md", path: "docs/note.md" })}
    >
      note.md
    </button>
  ),
}));

const scopeKey = "chat:right-header";
const storageKey = `bot.right-workbench.${scopeKey}`;
const onEmpty = vi.fn();
const onFileSourceChange = vi.fn();

function State() {
  const { snapshot } = useWorkbench(scopeKey);
  return <output data-testid="state">{JSON.stringify(snapshot)}</output>;
}

function state() {
  return JSON.parse(screen.getByTestId("state").textContent ?? "{}");
}

function Harness({
  withMarkdownReferences,
  ...overrides
}: Partial<ComponentProps<typeof RightWorkbench>> & {
  withMarkdownReferences?: boolean;
}) {
  const queryClient = new QueryClient();
  return (
    <QueryClientProvider client={queryClient}>
      <WorkbenchProvider>
        <State />
        {withMarkdownReferences ? <MarkdownReferences /> : null}
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
            fileSource: "project",
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
    </QueryClientProvider>
  );
}

function mount(overrides: Parameters<typeof Harness>[0] = {}) {
  return render(<Harness {...overrides} />);
}

function MarkdownReferences() {
  const binding = useChatWorkspaceBindings({
    activeChatId: "chat-1",
    fileSource: "project",
    query: "",
    updateViewState: () => {},
    workspaceDirectory: "/workspace",
    workspaceFiles: [],
    workspacePanelOpen: true,
    workspaceScopeKey: scopeKey,
  });
  return (
    <FileReferenceProvider
      context={{
        chatId: "chat-1",
        baseDirectory: "/workspace",
        workspaceRoot: "/workspace",
        sourceRoots: [],
      }}
      onOpenFile={(file) =>
        binding.openAttachmentFile({ ...file, reference: file })
      }
    >
      {["#L7-L9", "#L12"].map((location) => {
        const reference = parseReference(`docs/note.md${location}`);
        if (!reference) throw new Error("缺少测试文件引用");
        return (
          <ReferenceLink key={location} reference={reference}>
            {location}
          </ReferenceLink>
        );
      })}
    </FileReferenceProvider>
  );
}

function teamFiles(chatId: string) {
  return {
    scopeChatId: chatId,
    rootPath: "/workspace",
    scopePath: "",
    chatFiles: [
      {
        name: "note.md",
        path: "attachments/turn/note.md",
        source: "chat" as const,
      },
    ],
    fileSource: "chat" as const,
    onDownloadChatFile: async () => {},
    onDeleteChatFile: async () => true,
  };
}

function restoreChatFilesPage() {
  const persisted = JSON.parse(localStorage.getItem(storageKey) ?? "{}");
  persisted.state.tabs[0] = {
    ...persisted.state.tabs[0],
    kind: "files",
    title: "文件",
    contentId: "page:files:chat",
    fileSource: "chat",
  };
  localStorage.setItem(storageKey, JSON.stringify(persisted));
}

const memberAttachmentResolution = {
  status: "resolved" as const,
  canonicalPath: "/conversations/member-a/attachments/turn/note.md",
  baseDirectory: "/conversations/member-a/attachments",
  workspaceRoot: "/conversations/member-a/attachments",
  sourceRoots: [],
};

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
    ).toEqual(["浏览器", "终端", "文件", "差异", "侧聊", "看板"]);
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

  it("文件浏览器打开规范路径后复用已编辑的文件标签", async () => {
    const user = userEvent.setup();
    const stored = JSON.parse(localStorage.getItem(storageKey) ?? "{}");
    stored.state.tabs[0] = {
      ...stored.state.tabs[0],
      kind: "file",
      title: "note.md",
      contentId: "neuxbot-resource://file/absolute/workspace/docs/note.md",
      path: "docs/note.md",
      canonicalPath: "/workspace/docs/note.md",
      rootPath: "/workspace",
    };
    localStorage.setItem(storageKey, JSON.stringify(stored));
    const key = `${scopeKey}:one`;
    workbenchDocumentBuffers.begin(key, "saved");
    workbenchDocumentBuffers.change(key, "dirty");
    const resolve = vi
      .spyOn(platform, "resolveWorkspaceResourceReference")
      .mockResolvedValue({
        status: "resolved",
        canonicalPath: "/workspace/docs/note.md",
        baseDirectory: "/workspace",
        workspaceRoot: "/workspace",
        sourceRoots: [],
      });
    try {
      mount();
      await user.click(screen.getByRole("button", { name: "添加工作台标签" }));
      await user.click(screen.getByRole("menuitem", { name: "文件" }));
      await user.click(await screen.findByRole("button", { name: "note.md" }));
      await waitFor(() =>
        expect(state().tabs.map(({ id }: { id: string }) => id)).toEqual([
          "one",
          "two",
        ]),
      );
      expect(screen.getByRole("tab", { name: "note.md" })).toHaveAttribute(
        "aria-selected",
        "true",
      );
      await waitFor(() =>
        expect(screen.getByRole("tab", { name: "note.md" })).toHaveFocus(),
      );
      expect(workbenchDocumentBuffers.get(key)?.text).toBe("dirty");
    } finally {
      resolve.mockRestore();
    }
  });

  it("聊天 Markdown 文件链接保留行范围，并复用同一文件标签", async () => {
    const user = userEvent.setup();
    const resolve = vi
      .spyOn(platform, "resolveWorkspaceResourceReference")
      .mockResolvedValue({
        status: "resolved",
        canonicalPath: "/workspace/docs/note.md",
        workspaceRoot: "/workspace",
        baseDirectory: "/workspace",
        sourceRoots: [],
      });
    try {
      mount({ withMarkdownReferences: true });
      await user.click(screen.getByRole("link", { name: "#L7-L9" }));
      const preview = await screen.findByTestId("file-preview");
      expect(preview).toHaveAttribute("data-start-line", "7");
      expect(preview).toHaveAttribute("data-end-line", "9");
      const fileTab = screen.getByRole("tab", { name: "note.md" });

      await user.click(screen.getByRole("link", { name: "#L12" }));

      await waitFor(() =>
        expect(preview).toHaveAttribute("data-start-line", "12"),
      );
      expect(preview).toHaveAttribute("data-end-line", "12");
      expect(screen.getByRole("tab", { name: "note.md" })).toBe(fileTab);
    } finally {
      resolve.mockRestore();
    }
  });

  it("团队工作台切换成员后，已打开的附件仍以原成员会话读取", async () => {
    const user = userEvent.setup();
    restoreChatFilesPage();
    const resolve = vi
      .spyOn(platform, "resolveWorkspaceResourceReference")
      .mockResolvedValue(memberAttachmentResolution);
    try {
      const view = mount({
        parentChatId: "member-a",
        fileWorkspaceProps: teamFiles("member-a"),
      });
      await user.click(
        await screen.findByRole("treeitem", { name: "note.md" }),
      );
      expect(await screen.findByTestId("file-preview")).toHaveAttribute(
        "data-chat-id",
        "member-a",
      );

      view.rerender(
        <Harness
          parentChatId="member-b"
          fileWorkspaceProps={teamFiles("member-b")}
        />,
      );

      expect(screen.getByTestId("file-preview")).toHaveAttribute(
        "data-chat-id",
        "member-a",
      );
    } finally {
      resolve.mockRestore();
    }
  });

  it("团队工作台切换成员时丢弃原成员尚未完成的附件打开请求", async () => {
    const user = userEvent.setup();
    restoreChatFilesPage();
    let finish: (result: typeof memberAttachmentResolution) => void = () => {};
    const pending = new Promise<typeof memberAttachmentResolution>(
      (resolve) => {
        finish = resolve;
      },
    );
    const resolve = vi
      .spyOn(platform, "resolveWorkspaceResourceReference")
      .mockReturnValue(pending);
    try {
      const view = mount({
        parentChatId: "member-a",
        fileWorkspaceProps: teamFiles("member-a"),
      });
      await user.click(
        await screen.findByRole("treeitem", { name: "note.md" }),
      );
      expect(resolve).toHaveBeenCalledWith(
        "member-a",
        "attachments/turn/note.md",
        "",
      );

      view.rerender(
        <Harness
          parentChatId="member-b"
          fileWorkspaceProps={teamFiles("member-b")}
        />,
      );
      await act(async () => finish(memberAttachmentResolution));

      expect(screen.queryByRole("tab", { name: "note.md" })).toBeNull();
      expect(screen.getByRole("tab", { name: "会话文件" })).toHaveAttribute(
        "aria-selected",
        "true",
      );
    } finally {
      resolve.mockRestore();
    }
  });
});
