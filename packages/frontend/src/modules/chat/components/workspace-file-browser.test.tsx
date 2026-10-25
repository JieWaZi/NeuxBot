import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { WorkspaceFileBrowser } from "./workspace-file-browser";
import { workspaceFileDragType } from "../model/workspace-file-drag";

const {
  listWorkspaceDirectory,
  listWorkspaceResourceDirectory,
  searchWorkspaceFiles,
  searchWorkspaceResourceFiles,
  openWorkspaceResourceScope,
  statWorkspaceResource,
  renameWorkspaceResource,
  deleteWorkspaceResource,
  renameWorkspaceDirectory,
  deleteWorkspaceDirectory,
  importWorkspaceResources,
  importDroppedWorkspaceFiles,
  saveWorkspaceResourceAs,
  observeWorkspaceResource,
} = vi.hoisted(() => ({
  listWorkspaceDirectory: vi.fn(async (_root: string, relativePath = "") =>
    relativePath === "projects"
      ? [
          {
            name: "share",
            relativePath: "projects/share",
            isDirectory: true,
          },
        ]
      : relativePath === "projects/share"
        ? [
            {
              name: "src",
              relativePath: "projects/share/src",
              isDirectory: true,
            },
          ]
        : [
            {
              name: "main.ts",
              relativePath: "projects/share/src/main.ts",
              isDirectory: false,
              size: 24,
            },
          ],
  ),
  searchWorkspaceFiles: vi.fn(
    async (...args: [string, string, number?, string?]) => {
      void args;
      return [
        {
          name: "main.ts",
          relativePath: "projects/share/src/main.ts",
          path: "/tmp/projects/share/src/main.ts",
        },
      ];
    },
  ),
  listWorkspaceResourceDirectory: vi.fn(
    async (_chatId: string, _relativePath: string, _root?: string) => {
      void [_chatId, _relativePath, _root];
      return [] as {
        name: string;
        relativePath: string;
        isDirectory: boolean;
      }[];
    },
  ),
  searchWorkspaceResourceFiles: vi.fn(
    async (
      _chatId: string,
      _query: string,
      _limit: number,
      _scopePath: string,
      _root?: string,
    ): Promise<{ name: string; relativePath: string; path: string }[]> => {
      void [_chatId, _query, _limit, _scopePath, _root];
      return [];
    },
  ),
  openWorkspaceResourceScope: vi.fn(async () => ({
    id: "scope",
    root: "/tmp/ally",
  })),
  statWorkspaceResource: vi.fn(async () => ({
    relativePath: "报告 #1.md",
    size: 5,
    version: "v1",
    modifiedAt: 1,
    mediaType: "text/markdown",
  })),
  renameWorkspaceResource: vi.fn(async () => ({
    relativePath: "新报告 %.md",
    size: 5,
    version: "v2",
    modifiedAt: 2,
    mediaType: "text/markdown",
  })),
  deleteWorkspaceResource: vi.fn(async () => true),
  renameWorkspaceDirectory: vi.fn(async () => "projects/新资料"),
  deleteWorkspaceDirectory: vi.fn(async () => true),
  importWorkspaceResources: vi.fn(async () => ({ imported: [], failed: [] })),
  importDroppedWorkspaceFiles: vi.fn(async () => ({
    imported: [],
    failed: [],
  })),
  saveWorkspaceResourceAs: vi.fn(async () => true),
  observeWorkspaceResource: vi.fn(async () => async () => {}),
}));

vi.mock("@/shared/platform", () => ({
  isDesktopRuntime: () => true,
  hasPlatformService: () => true,
  listWorkspaceDirectory: (root: string, relativePath?: string) =>
    listWorkspaceDirectory(root, relativePath),
  listWorkspaceResourceDirectory: (
    chatId: string,
    relativePath: string,
    root?: string,
  ) => listWorkspaceResourceDirectory(chatId, relativePath, root),
  searchWorkspaceFiles: (
    root: string,
    query: string,
    limit?: number,
    scopePath?: string,
  ) => searchWorkspaceFiles(root, query, limit, scopePath),
  searchWorkspaceResourceFiles: (
    chatId: string,
    query: string,
    limit: number,
    scopePath: string,
    root?: string,
  ) => searchWorkspaceResourceFiles(chatId, query, limit, scopePath, root),
  openWorkspaceResourceScope,
  statWorkspaceResource,
  renameWorkspaceResource,
  deleteWorkspaceResource,
  renameWorkspaceDirectory,
  deleteWorkspaceDirectory,
  importWorkspaceResources,
  importDroppedWorkspaceFiles,
  saveWorkspaceResourceAs,
  observeWorkspaceResource,
}));

describe("WorkspaceFileBrowser", () => {
  it("默认展开项目文件夹，只继续懒加载更深目录并保留完整逻辑路径", async () => {
    const user = userEvent.setup();
    const onOpenFile = vi.fn();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <WorkspaceFileBrowser
          onOpenFile={onOpenFile}
          query=""
          rootPath="/tmp/ally"
          scopePath="projects"
        />
      </QueryClientProvider>,
    );

    expect(
      await screen.findByRole("button", { name: "share" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("attachments")).not.toBeInTheDocument();
    expect(
      screen.queryByText("projects", { selector: "button" }),
    ).not.toBeInTheDocument();

    expect(
      (await screen.findByRole("button", { name: "share" })).closest(
        '[role="treeitem"]',
      ),
    ).toHaveAttribute("aria-expanded", "true");
    expect(
      (await screen.findByRole("button", { name: "src" })).closest(
        '[role="treeitem"]',
      ),
    ).toHaveAttribute("aria-expanded", "false");
    expect(listWorkspaceDirectory).not.toHaveBeenCalledWith(
      "/tmp/ally",
      "projects/share/src",
    );
    await user.click(await screen.findByRole("button", { name: "src" }));
    await user.click(await screen.findByRole("treeitem", { name: "main.ts" }));

    expect(listWorkspaceDirectory).toHaveBeenNthCalledWith(
      1,
      "/tmp/ally",
      "projects",
    );
    expect(listWorkspaceDirectory).toHaveBeenNthCalledWith(
      2,
      "/tmp/ally",
      "projects/share",
    );
    expect(listWorkspaceDirectory).toHaveBeenNthCalledWith(
      3,
      "/tmp/ally",
      "projects/share/src",
    );
    expect(onOpenFile).toHaveBeenCalledWith({
      name: "main.ts",
      path: "projects/share/src/main.ts",
    });
  });

  it("搜索时将范围限制在 projects", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <WorkspaceFileBrowser
          onOpenFile={vi.fn()}
          query="main"
          rootPath="/tmp/ally"
          scopePath="projects"
        />
      </QueryClientProvider>,
    );

    await waitFor(() =>
      expect(searchWorkspaceFiles).toHaveBeenCalledWith(
        "/tmp/ally",
        "main",
        200,
        "projects",
      ),
    );
    expect(
      await screen.findByRole("treeitem", { name: /main\.ts/ }),
    ).toBeInTheDocument();
  });

  it("文件树条目提供拖入聊天框所需的文件身份", async () => {
    searchWorkspaceResourceFiles.mockResolvedValueOnce([
      {
        name: "main.ts",
        relativePath: "projects/share/src/main.ts",
        path: "/tmp/projects/share/src/main.ts",
      },
    ]);
    render(
      <QueryClientProvider client={new QueryClient()}>
        <WorkspaceFileBrowser
          onOpenFile={vi.fn()}
          query="main"
          rootPath="/tmp/ally"
          scopeChatId="chat-id"
          scopePath="projects"
        />
      </QueryClientProvider>,
    );
    const item = await screen.findByRole("treeitem", { name: /main\.ts/ });
    const setData = vi.fn();
    fireEvent.dragStart(item, {
      dataTransfer: { setData, effectAllowed: "none" },
    });
    expect(setData).toHaveBeenCalledWith(
      workspaceFileDragType,
      JSON.stringify({
        chatId: "chat-id",
        rootPath: "/tmp/ally",
        path: "projects/share/src/main.ts",
      }),
    );
  });

  it("Chat 工作台只通过受限 bridge 枚举和搜索", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { rerender } = render(
      <QueryClientProvider client={queryClient}>
        <WorkspaceFileBrowser
          onOpenFile={vi.fn()}
          query=""
          rootPath="/tmp/ally"
          scopeChatId="chat-id"
          scopePath="projects"
        />
      </QueryClientProvider>,
    );
    await waitFor(() =>
      expect(listWorkspaceResourceDirectory).toHaveBeenCalledWith(
        "chat-id",
        "projects",
        "/tmp/ally",
      ),
    );
    await waitFor(() =>
      expect(observeWorkspaceResource).toHaveBeenCalledWith(
        "scope",
        "",
        expect.any(Function),
        expect.any(AbortSignal),
        "/tmp/ally",
        true,
      ),
    );
    rerender(
      <QueryClientProvider client={queryClient}>
        <WorkspaceFileBrowser
          onOpenFile={vi.fn()}
          query="source"
          rootPath="/tmp/ally"
          scopeChatId="chat-id"
          scopePath="projects"
        />
      </QueryClientProvider>,
    );
    await waitFor(() =>
      expect(searchWorkspaceResourceFiles).toHaveBeenCalledWith(
        "chat-id",
        "source",
        200,
        "projects",
        "/tmp/ally",
      ),
    );
  });

  it("文件行重命名同步工作台标签路径", async () => {
    const user = userEvent.setup();
    listWorkspaceResourceDirectory.mockResolvedValueOnce([
      {
        name: "报告 #1.md",
        relativePath: "报告 #1.md",
        isDirectory: false,
      },
    ]);
    const onPathRenamed = vi.fn();
    const onPathDeleted = vi.fn();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <WorkspaceFileBrowser
          onOpenFile={vi.fn()}
          onPathRenamed={onPathRenamed}
          onPathDeleted={onPathDeleted}
          query=""
          rootPath="/tmp/ally"
          scopeChatId="chat-id"
          scopePath=""
        />
      </QueryClientProvider>,
    );
    await screen.findByRole("treeitem", { name: "报告 #1.md" });
    await user.click(
      screen.getByRole("button", { name: "报告 #1.md 的文件操作" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "重命名" }));
    await user.clear(screen.getByRole("textbox", { name: "新文件名" }));
    await user.type(
      screen.getByRole("textbox", { name: "新文件名" }),
      "新报告 %.md",
    );
    await user.click(screen.getByRole("button", { name: "重命名" }));
    await waitFor(() =>
      expect(onPathRenamed).toHaveBeenCalledWith(
        "报告 #1.md",
        "新报告 %.md",
        "新报告 %.md",
        undefined,
        false,
        "/tmp/ally",
      ),
    );
    expect(renameWorkspaceResource).toHaveBeenCalledWith(
      "chat-id",
      "报告 #1.md",
      "新报告 %.md",
      "v1",
      "/tmp/ally",
    );
  });

  it("文件删除经共享对话框确认后通知工作台关闭旧标签", async () => {
    const user = userEvent.setup();
    listWorkspaceResourceDirectory.mockResolvedValueOnce([
      {
        name: "报告 #1.md",
        relativePath: "报告 #1.md",
        isDirectory: false,
      },
    ]);
    const onPathDeleted = vi.fn();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <WorkspaceFileBrowser
          onOpenFile={vi.fn()}
          onPathDeleted={onPathDeleted}
          query=""
          rootPath="/tmp/ally"
          scopeChatId="chat-id"
          scopePath=""
        />
      </QueryClientProvider>,
    );
    await screen.findByRole("treeitem", { name: "报告 #1.md" });
    await user.click(
      screen.getByRole("button", { name: "报告 #1.md 的文件操作" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "删除文件" }));
    expect(deleteWorkspaceResource).not.toHaveBeenCalled();
    await user.click(await screen.findByRole("button", { name: "确认删除" }));
    await waitFor(() =>
      expect(deleteWorkspaceResource).toHaveBeenCalledWith(
        "chat-id",
        "报告 #1.md",
        "/tmp/ally",
        "v1",
      ),
    );
    expect(onPathDeleted).toHaveBeenCalledWith(
      "报告 #1.md",
      undefined,
      false,
      "/tmp/ally",
    );
  });

  it("目录重命名与删除通过 Chat 范围调用，并同步后代标签", async () => {
    const user = userEvent.setup();
    listWorkspaceResourceDirectory.mockResolvedValueOnce([
      {
        name: "资料",
        relativePath: "projects/资料",
        isDirectory: true,
      },
    ]);
    const onPathRenamed = vi.fn();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <WorkspaceFileBrowser
          onOpenFile={vi.fn()}
          onPathRenamed={onPathRenamed}
          query=""
          rootPath="/tmp/ally"
          scopeChatId="chat-id"
          scopePath="projects"
        />
      </QueryClientProvider>,
    );
    await user.click(
      await screen.findByRole("button", { name: "资料 的目录操作" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "重命名目录" }));
    await user.clear(screen.getByRole("textbox", { name: "新目录名" }));
    await user.type(
      screen.getByRole("textbox", { name: "新目录名" }),
      "新资料",
    );
    await user.click(screen.getByRole("button", { name: "重命名目录" }));
    await waitFor(() =>
      expect(onPathRenamed).toHaveBeenCalledWith(
        "projects/资料",
        "projects/新资料",
        "新资料",
        undefined,
        true,
        "/tmp/ally",
      ),
    );
    expect(renameWorkspaceDirectory).toHaveBeenCalledWith(
      "chat-id",
      "projects/资料",
      "新资料",
      "/tmp/ally",
    );
  });

  it("删除目录须经共享对话框确认，未保存的后代文件阻止操作", async () => {
    const user = userEvent.setup();
    listWorkspaceResourceDirectory.mockResolvedValueOnce([
      {
        name: "资料",
        relativePath: "projects/资料",
        isDirectory: true,
      },
    ]);
    const onPathDeleted = vi.fn();
    const isPathDirty = vi.fn(() => true);
    render(
      <QueryClientProvider client={new QueryClient()}>
        <WorkspaceFileBrowser
          isPathDirty={isPathDirty}
          onOpenFile={vi.fn()}
          onPathDeleted={onPathDeleted}
          query=""
          rootPath="/tmp/ally"
          scopeChatId="chat-id"
          scopePath="projects"
        />
      </QueryClientProvider>,
    );
    await user.click(
      await screen.findByRole("button", { name: "资料 的目录操作" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "删除目录" }));
    expect(isPathDirty).toHaveBeenCalledWith(
      "projects/资料",
      undefined,
      "/tmp/ally",
    );
    expect(deleteWorkspaceDirectory).not.toHaveBeenCalled();
    isPathDirty.mockReturnValue(false);
    await user.click(screen.getByRole("button", { name: "资料 的目录操作" }));
    await user.click(screen.getByRole("menuitem", { name: "删除目录" }));
    expect(deleteWorkspaceDirectory).not.toHaveBeenCalled();
    await user.click(await screen.findByRole("button", { name: "确认删除" }));
    await waitFor(() =>
      expect(onPathDeleted).toHaveBeenCalledWith(
        "projects/资料",
        undefined,
        true,
        "/tmp/ally",
      ),
    );
    expect(deleteWorkspaceDirectory).toHaveBeenCalledWith(
      "chat-id",
      "projects/资料",
      "/tmp/ally",
    );
  });

  it("目录导入使用受限文件选择器和当前目录路径", async () => {
    const user = userEvent.setup();
    listWorkspaceResourceDirectory.mockResolvedValueOnce([
      {
        name: "资料 #1",
        relativePath: "projects/资料 #1",
        isDirectory: true,
      },
    ]);
    render(
      <QueryClientProvider client={new QueryClient()}>
        <WorkspaceFileBrowser
          onOpenFile={vi.fn()}
          query=""
          rootPath="/tmp/ally"
          scopeChatId="chat-id"
          scopePath="projects"
        />
      </QueryClientProvider>,
    );
    await user.click(
      await screen.findByRole("button", {
        name: "导入文件到“资料 #1”",
      }),
    );
    expect(importWorkspaceResources).toHaveBeenCalledWith(
      "chat-id",
      "projects/资料 #1",
      "/tmp/ally",
    );
  });

  it("拖入目录时将浏览器 File 送到该目录的 Chat 范围", async () => {
    listWorkspaceResourceDirectory.mockResolvedValueOnce([
      {
        name: "资料 #1",
        relativePath: "projects/资料 #1",
        isDirectory: true,
      },
    ]);
    render(
      <QueryClientProvider client={new QueryClient()}>
        <WorkspaceFileBrowser
          onOpenFile={vi.fn()}
          query=""
          rootPath="/tmp/ally"
          scopeChatId="chat-id"
          scopePath="projects"
        />
      </QueryClientProvider>,
    );
    const directory = await screen.findByRole("button", { name: "资料 #1" });
    const file = new File(["hello"], "新文件.txt", { type: "text/plain" });
    const row = directory.parentElement;
    expect(row).not.toBeNull();
    if (!row) throw new Error("目录行不存在");
    fireEvent.drop(row, {
      dataTransfer: { types: ["Files"], files: [file] },
    });
    await waitFor(() =>
      expect(importDroppedWorkspaceFiles).toHaveBeenCalledWith(
        "chat-id",
        "projects/资料 #1",
        [file],
        "/tmp/ally",
      ),
    );
  });
});
